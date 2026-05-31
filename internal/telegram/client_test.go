package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSendWithFakeServer(t *testing.T) {
	var capturedBody []byte
	var capturedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"ok":true,"result":{}}`)
	}))
	defer srv.Close()

	cfg := Config{
		BotToken: "123456:ABC-DEF",
		ChatID:   "999",
		BaseURL:  "https://example.com",
	}

	client := NewClient(cfg)
	// Override apiURL to point to test server
	client.apiURL = srv.URL + "/bot123456:ABC-DEF/sendMessage"

	msg := &SummaryInput{
		From:       "test@example.com",
		Subject:    "Hello",
		TextBody:   "World",
		ReceivedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	text := BuildSummary(msg, cfg.BaseURL, "sharetoken")
	markup := BuildReplyMarkup(cfg.BaseURL, "sharetoken")
	req := NewSendMessageRequest(cfg.ChatID, text, markup)

	err := client.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	if !strings.Contains(capturedPath, "123456:ABC-DEF") {
		t.Errorf("request path %q should contain bot token", capturedPath)
	}

	var parsed SendMessageRequest
	if err := json.Unmarshal(capturedBody, &parsed); err != nil {
		t.Fatalf("failed to unmarshal captured body: %v", err)
	}

	if parsed.ChatID != "999" {
		t.Errorf("chat_id = %q, want %q", parsed.ChatID, "999")
	}
	if parsed.ParseMode != "HTML" {
		t.Errorf("parse_mode = %q, want %q", parsed.ParseMode, "HTML")
	}
	if !strings.Contains(parsed.Text, "test@example.com") {
		t.Errorf("text should contain sender: %s", parsed.Text)
	}
	if parsed.ReplyMarkup == nil {
		t.Fatal("reply_markup should not be nil")
	}
	if len(parsed.ReplyMarkup.InlineKeyboard) != 1 || len(parsed.ReplyMarkup.InlineKeyboard[0]) != 2 {
		t.Errorf("expected 1 row with 2 buttons, got %v", parsed.ReplyMarkup.InlineKeyboard)
	}
}

func TestSendServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"ok":false,"description":"Internal Server Error"}`)
	}))
	defer srv.Close()

	cfg := Config{
		BotToken: "123456:ABC",
		ChatID:   "999",
		BaseURL:  "https://example.com",
	}

	client := NewClient(cfg)
	client.apiURL = srv.URL + "/bot123456:ABC/sendMessage"

	req := NewSendMessageRequest("999", "test", nil)

	err := client.Send(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500: %v", err)
	}
}

func TestSendContextCancel(t *testing.T) {
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked // Block until test cleanup
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	defer close(blocked) // Unblock server on cleanup

	cfg := Config{
		BotToken: "123456:ABC",
		ChatID:   "999",
		BaseURL:  "https://example.com",
	}

	client := NewClient(cfg)
	client.apiURL = srv.URL + "/bot123456:ABC/sendMessage"

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	req := NewSendMessageRequest("999", "test", nil)

	err := client.Send(ctx, req)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}
