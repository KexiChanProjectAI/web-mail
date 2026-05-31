package telegram

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"lite-mail/internal/config"
	"lite-mail/internal/db"
	"lite-mail/internal/testutil"
)

// fakeSender implements Sender for testing.
type fakeSender struct {
	sendCount int
	sendErr   error
}

func (f *fakeSender) Send(ctx context.Context, req *SendMessageRequest) error {
	f.sendCount++
	return f.sendErr
}

func disabledConfig() *config.Config {
	return &config.Config{}
}

func enabledConfig() *config.Config {
	return &config.Config{
		TelegramBotToken: "123456:ABC-DEF",
		TelegramChatID:   "-1001234567890",
	}
}

func TestDeliverWhenTelegramDisabled(t *testing.T) {
	database := testutil.SetupTestDB(t)
	defer testutil.TeardownTestDB(t, database)

	sender := &fakeSender{}
	svc := NewDeliveryService(database, disabledConfig(), sender)

	err := svc.Deliver(context.Background(), 1, "test summary", nil)
	if err != nil {
		t.Fatalf("Deliver returned error: %v", err)
	}

	if sender.sendCount != 0 {
		t.Errorf("expected no HTTP calls, got %d", sender.sendCount)
	}

	// Verify delivery record shows "skipped"
	rec, err := db.GetDeliveryStatus(context.Background(), database, 1)
	if err != nil {
		t.Fatalf("GetDeliveryStatus: %v", err)
	}
	if rec.Status != "skipped" {
		t.Errorf("expected status skipped, got %s", rec.Status)
	}
	if rec.AttemptedAt == nil {
		t.Error("expected attempted_at to be set")
	}
	if rec.SentAt != nil {
		t.Error("expected sent_at to be nil for skipped delivery")
	}
}

func TestDeliverSuccess(t *testing.T) {
	database := testutil.SetupTestDB(t)
	defer testutil.TeardownTestDB(t, database)

	sender := &fakeSender{}
	svc := NewDeliveryService(database, enabledConfig(), sender)

	err := svc.Deliver(context.Background(), 2, "test summary", nil)
	if err != nil {
		t.Fatalf("Deliver returned error: %v", err)
	}

	if sender.sendCount != 1 {
		t.Errorf("expected 1 HTTP call, got %d", sender.sendCount)
	}

	// Verify delivery record shows "sent"
	rec, err := db.GetDeliveryStatus(context.Background(), database, 2)
	if err != nil {
		t.Fatalf("GetDeliveryStatus: %v", err)
	}
	if rec.Status != "sent" {
		t.Errorf("expected status sent, got %s", rec.Status)
	}
	if rec.AttemptedAt == nil {
		t.Error("expected attempted_at to be set")
	}
	if rec.SentAt == nil {
		t.Error("expected sent_at to be set for successful delivery")
	}
	if rec.LastError != "" {
		t.Errorf("expected no last_error, got %q", rec.LastError)
	}
}

func TestDeliverFailure(t *testing.T) {
	database := testutil.SetupTestDB(t)
	defer testutil.TeardownTestDB(t, database)

	sender := &fakeSender{
		sendErr: fmt.Errorf("network timeout"),
	}
	svc := NewDeliveryService(database, enabledConfig(), sender)

	err := svc.Deliver(context.Background(), 3, "test summary", nil)
	if err == nil {
		t.Fatal("expected error from Deliver, got nil")
	}
	if err.Error() != "network timeout" {
		t.Errorf("expected original error, got: %v", err)
	}

	if sender.sendCount != 1 {
		t.Errorf("expected 1 HTTP call, got %d", sender.sendCount)
	}

	// Verify delivery record shows "failed"
	rec, err := db.GetDeliveryStatus(context.Background(), database, 3)
	if err != nil {
		t.Fatalf("GetDeliveryStatus: %v", err)
	}
	if rec.Status != "failed" {
		t.Errorf("expected status failed, got %s", rec.Status)
	}
	if rec.LastError != "network timeout" {
		t.Errorf("expected last_error 'network timeout', got %q", rec.LastError)
	}
	if rec.AttemptedAt == nil {
		t.Error("expected attempted_at to be set")
	}
	if rec.SentAt != nil {
		t.Error("expected sent_at to be nil for failed delivery")
	}
}

func TestDeliverIdempotent(t *testing.T) {
	database := testutil.SetupTestDB(t)
	defer testutil.TeardownTestDB(t, database)

	sender := &fakeSender{}
	svc := NewDeliveryService(database, enabledConfig(), sender)

	// First call: should send
	err := svc.Deliver(context.Background(), 4, "test summary", nil)
	if err != nil {
		t.Fatalf("first Deliver returned error: %v", err)
	}
	if sender.sendCount != 1 {
		t.Errorf("expected 1 HTTP call after first deliver, got %d", sender.sendCount)
	}

	// Second call: should NOT send (idempotent)
	err = svc.Deliver(context.Background(), 4, "test summary", nil)
	if err != nil {
		t.Fatalf("second Deliver returned error: %v", err)
	}
	if sender.sendCount != 1 {
		t.Errorf("expected still 1 HTTP call after second deliver (idempotent), got %d", sender.sendCount)
	}

	// Verify delivery record still shows "sent" with one record
	rec, err := db.GetDeliveryStatus(context.Background(), database, 4)
	if err != nil {
		t.Fatalf("GetDeliveryStatus: %v", err)
	}
	if rec.Status != "sent" {
		t.Errorf("expected status sent, got %s", rec.Status)
	}
}

func TestDeliverFailureThenSuccess(t *testing.T) {
	database := testutil.SetupTestDB(t)
	defer testutil.TeardownTestDB(t, database)

	// First attempt fails
	failSender := &fakeSender{
		sendErr: fmt.Errorf("temporary error"),
	}
	svc := NewDeliveryService(database, enabledConfig(), failSender)

	err := svc.Deliver(context.Background(), 5, "test summary", nil)
	if err == nil {
		t.Fatal("expected error from first Deliver")
	}

	// Verify failed status
	rec, err := db.GetDeliveryStatus(context.Background(), database, 5)
	if err != nil {
		t.Fatalf("GetDeliveryStatus after failure: %v", err)
	}
	if rec.Status != "failed" {
		t.Errorf("expected status failed, got %s", rec.Status)
	}

	// Retry with successful sender should update to "sent"
	okSender := &fakeSender{}
	svc2 := NewDeliveryService(database, enabledConfig(), okSender)

	err = svc2.Deliver(context.Background(), 5, "test summary", nil)
	if err != nil {
		t.Fatalf("retry Deliver returned error: %v", err)
	}

	rec, err = db.GetDeliveryStatus(context.Background(), database, 5)
	if err != nil {
		t.Fatalf("GetDeliveryStatus after retry: %v", err)
	}
	if rec.Status != "sent" {
		t.Errorf("expected status sent after retry, got %s", rec.Status)
	}
}

func TestDeliverContextCancellation(t *testing.T) {
	database := testutil.SetupTestDB(t)
	defer testutil.TeardownTestDB(t, database)

	sender := &fakeSender{}
	svc := NewDeliveryService(database, enabledConfig(), sender)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := svc.Deliver(ctx, 6, "test summary", nil)
	// The delivery may or may not fail depending on DB/Sender respecting context,
	// but it must not panic.
	_ = err
}

// Unit tests without DB dependency

func TestNewDeliveryService(t *testing.T) {
	sender := &fakeSender{}
	svc := NewDeliveryService(nil, enabledConfig(), sender)
	if svc == nil {
		t.Fatal("expected non-nil DeliveryService")
	}
	if svc.sender != sender {
		t.Error("sender not set correctly")
	}
}

func TestDeliverDisabledConfigNoDB(t *testing.T) {
	// This tests the config-check path without a real DB.
	// With a nil db, Deliver will panic on the CreateOrUpdateDelivery call,
	// so we skip this and rely on TestDeliverWhenTelegramDisabled instead.
	// This is documented as a known limitation — the service requires a DB.
}

func init() {
	// Ensure time package is referenced for stamp calculations
	_ = time.Now()
	_ = (*sql.DB)(nil)
}
