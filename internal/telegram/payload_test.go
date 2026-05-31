package telegram

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no escaping needed", "hello world", "hello world"},
		{"escape ampersand", "a&b", "a&amp;b"},
		{"escape less than", "a<b", "a&lt;b"},
		{"escape greater than", "a>b", "a&gt;b"},
		{"escape all", "<b>&</b>", "&lt;b&gt;&amp;&lt;/b&gt;"},
		{"ampersand first", "&&<><>", "&amp;&amp;&lt;&gt;&lt;&gt;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EscapeHTML(tt.input)
			if got != tt.want {
				t.Errorf("EscapeHTML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPayloadEscaping(t *testing.T) {
	msg := &SummaryInput{
		From:       "Alice <alice@example.com>",
		Subject:    "Test & <Important>",
		TextBody:   "Hello <world> & goodbye",
		ReceivedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	text := BuildSummary(msg, "https://example.com", "abc123")

	if !strings.Contains(text, "Alice &lt;alice@example.com&gt;") {
		t.Errorf("From not properly escaped in summary: %s", text)
	}
	if !strings.Contains(text, "Test &amp; &lt;Important&gt;") {
		t.Errorf("Subject not properly escaped in summary: %s", text)
	}
	if !strings.Contains(text, "Hello &lt;world&gt; &amp; goodbye") {
		t.Errorf("Body not properly escaped in summary: %s", text)
	}
}

func TestPayloadTruncation(t *testing.T) {
	longBody := strings.Repeat("x", 5000)
	msg := &SummaryInput{
		From:       "sender@test.com",
		Subject:    "Big email",
		TextBody:   longBody,
		ReceivedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	text := BuildSummary(msg, "https://example.com", "abc123")

	if len(text) > maxMessageLen {
		t.Errorf("BuildSummary returned text of length %d, exceeds max %d", len(text), maxMessageLen)
	}
}

func TestPayloadHasTwoURLButtons(t *testing.T) {
	markup := BuildReplyMarkup("https://example.com", "tok123")

	if len(markup.InlineKeyboard) != 1 {
		t.Fatalf("expected 1 row, got %d", len(markup.InlineKeyboard))
	}
	row := markup.InlineKeyboard[0]
	if len(row) != 2 {
		t.Fatalf("expected 2 buttons, got %d", len(row))
	}

	txtBtn := row[0]
	htmlBtn := row[1]

	if txtBtn.Text != "View as TXT" {
		t.Errorf("first button text = %q, want %q", txtBtn.Text, "View as TXT")
	}
	if txtBtn.URL != "https://example.com/share/tok123/txt" {
		t.Errorf("first button URL = %q, want %q", txtBtn.URL, "https://example.com/share/tok123/txt")
	}

	if htmlBtn.Text != "View as HTML" {
		t.Errorf("second button text = %q, want %q", htmlBtn.Text, "View as HTML")
	}
	if htmlBtn.URL != "https://example.com/share/tok123/html" {
		t.Errorf("second button URL = %q, want %q", htmlBtn.URL, "https://example.com/share/tok123/html")
	}
}

func TestNewSendMessageRequest(t *testing.T) {
	markup := BuildReplyMarkup("https://example.com", "tok")
	req := NewSendMessageRequest("12345", "hello", markup)

	if req.ChatID != "12345" {
		t.Errorf("ChatID = %q, want %q", req.ChatID, "12345")
	}
	if req.Text != "hello" {
		t.Errorf("Text = %q, want %q", req.Text, "hello")
	}
	if req.ParseMode != "HTML" {
		t.Errorf("ParseMode = %q, want %q", req.ParseMode, "HTML")
	}
	if req.ReplyMarkup != markup {
		t.Error("ReplyMarkup not set correctly")
	}
}

func TestBuildSummaryEmptyFields(t *testing.T) {
	msg := &SummaryInput{
		From:       "",
		Subject:    "",
		TextBody:   "",
		ReceivedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	text := BuildSummary(msg, "https://example.com", "abc123")
	if text == "" {
		t.Error("BuildSummary with empty fields should still produce output")
	}
	// Should contain the label even if values are empty
	if !strings.Contains(text, "From:") {
		t.Error("BuildSummary should contain 'From:' label")
	}
	if !strings.Contains(text, "Subject:") {
		t.Error("BuildSummary should contain 'Subject:' label")
	}
}

func TestButtonsHaveNoCallbackData(t *testing.T) {
	markup := BuildReplyMarkup("https://example.com", "tok123")
	row := markup.InlineKeyboard[0]
	for i, btn := range row {
		// InlineKeyboardButton struct only has Text and URL fields.
		// Verify the button has a non-empty URL and empty string for any
		// callback-like field (the struct doesn't have one, but we verify
		// the JSON serialization omits callback_data).
		if btn.URL == "" {
			t.Errorf("button %d has empty URL", i)
		}
	}

	// Verify JSON serialization has url but no callback_data
	data, err := json.Marshal(markup)
	if err != nil {
		t.Fatalf("marshal markup: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "callback_data") {
		t.Errorf("serialized markup should not contain callback_data: %s", s)
	}
	if !strings.Contains(s, "\"url\"") {
		t.Errorf("serialized markup should contain url field: %s", s)
	}
}
