package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateOrUpdateDeliveryInsertAndRetrieve(t *testing.T) {
	database := setupTestDB(t)
	defer teardownTestDB(t, database)

	ctx := context.Background()
	messageID := insertTestMessage(t, ctx, database, "cf-id-delim-1", "hash-delim-1")

	err := CreateOrUpdateDelivery(ctx, database, messageID, "pending", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateOrUpdateDelivery: %v", err)
	}

	d, err := GetDeliveryStatus(ctx, database, messageID)
	if err != nil {
		t.Fatalf("GetDeliveryStatus: %v", err)
	}
	if d.MessageID != messageID {
		t.Fatalf("expected message_id %d, got %d", messageID, d.MessageID)
	}
	if d.Status != "pending" {
		t.Fatalf("expected status pending, got %s", d.Status)
	}
	if d.LastError != "" {
		t.Fatalf("expected empty last_error, got %q", d.LastError)
	}
}

func TestCreateOrUpdateDeliveryFailureWithRetry(t *testing.T) {
	database := setupTestDB(t)
	defer teardownTestDB(t, database)

	ctx := context.Background()
	messageID := insertTestMessage(t, ctx, database, "cf-id-delim-2", "hash-delim-2")

	err := CreateOrUpdateDelivery(ctx, database, messageID, "pending", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateOrUpdateDelivery pending: %v", err)
	}

	attemptedAt := time.Date(2025, 1, 15, 12, 30, 0, 0, time.UTC)
	err = CreateOrUpdateDelivery(ctx, database, messageID, "failed", "connection refused", &attemptedAt, nil)
	if err != nil {
		t.Fatalf("CreateOrUpdateDelivery failed: %v", err)
	}

	d, err := GetDeliveryStatus(ctx, database, messageID)
	if err != nil {
		t.Fatalf("GetDeliveryStatus: %v", err)
	}
	if d.Status != "failed" {
		t.Fatalf("expected status failed, got %s", d.Status)
	}
	if d.LastError != "connection refused" {
		t.Fatalf("expected last_error 'connection refused', got %q", d.LastError)
	}
	if d.AttemptedAt == nil {
		t.Fatal("expected attempted_at to be set")
	}
	if !d.AttemptedAt.Equal(attemptedAt) {
		t.Fatalf("expected attempted_at %v, got %v", attemptedAt, d.AttemptedAt)
	}
	if d.SentAt != nil {
		t.Fatalf("expected sent_at nil, got %v", d.SentAt)
	}
}

func TestGetDeliveryStatusNotFound(t *testing.T) {
	database := setupTestDB(t)
	defer teardownTestDB(t, database)

	ctx := context.Background()

	_, err := GetDeliveryStatus(ctx, database, 99999)
	if !errors.Is(err, ErrDeliveryNotFound) {
		t.Fatalf("expected ErrDeliveryNotFound, got %v", err)
	}
}

func TestCreateOrUpdateDeliverySentStatus(t *testing.T) {
	database := setupTestDB(t)
	defer teardownTestDB(t, database)

	ctx := context.Background()
	messageID := insertTestMessage(t, ctx, database, "cf-id-delim-3", "hash-delim-3")

	sentAt := time.Date(2025, 1, 15, 12, 35, 0, 0, time.UTC)
	attemptedAt := time.Date(2025, 1, 15, 12, 34, 0, 0, time.UTC)
	err := CreateOrUpdateDelivery(ctx, database, messageID, "sent", "", &attemptedAt, &sentAt)
	if err != nil {
		t.Fatalf("CreateOrUpdateDelivery sent: %v", err)
	}

	d, err := GetDeliveryStatus(ctx, database, messageID)
	if err != nil {
		t.Fatalf("GetDeliveryStatus: %v", err)
	}
	if d.Status != "sent" {
		t.Fatalf("expected status sent, got %s", d.Status)
	}
	if d.SentAt == nil {
		t.Fatal("expected sent_at to be set")
	}
	if !d.SentAt.Equal(sentAt) {
		t.Fatalf("expected sent_at %v, got %v", sentAt, d.SentAt)
	}
}
