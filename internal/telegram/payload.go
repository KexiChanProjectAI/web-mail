package telegram

import (
	"fmt"
	"strings"
	"time"
)

// SummaryInput holds the email data needed to build a Telegram summary message.
type SummaryInput struct {
	From       string
	To         string
	Subject    string
	TextBody   string
	ReceivedAt time.Time
}

const maxMessageLen = 3500
const bodyPreviewLen = 300

// EscapeHTML escapes <, >, & for Telegram HTML parse mode.
func EscapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// BuildSummary builds an HTML-escaped, truncated summary text for a Telegram message.
func BuildSummary(msg *SummaryInput, baseURL, token string) string {
	// Truncate body at preview length if too long
	bodyPreview := msg.TextBody
	if len(bodyPreview) > bodyPreviewLen {
		bodyPreview = bodyPreview[:bodyPreviewLen]
	}

	// Build header lines without blank lines
	parts := []string{
		fmt.Sprintf("From: %s", EscapeHTML(msg.From)),
		fmt.Sprintf("To: %s", EscapeHTML(msg.To)),
		fmt.Sprintf("Subject: %s", EscapeHTML(msg.Subject)),
		fmt.Sprintf("Date: %s", EscapeHTML(msg.ReceivedAt.Format(time.RFC1123))),
	}

	bodyPreview = EscapeHTML(bodyPreview)
	if bodyPreview != "" {
		parts = append(parts, bodyPreview)
	}

	text := strings.Join(parts, "\n")

	if len(text) > maxMessageLen {
		text = text[:maxMessageLen]
	}

	return text
}

// BuildReplyMarkup builds an inline keyboard with TXT and HTML view buttons.
func BuildReplyMarkup(baseURL, token string) *ReplyMarkup {
	return &ReplyMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "View as TXT", URL: fmt.Sprintf("%s/share/%s/txt", baseURL, token)},
				{Text: "View as HTML", URL: fmt.Sprintf("%s/share/%s/html", baseURL, token)},
			},
		},
	}
}

// NewSendMessageRequest assembles the full sendMessage request.
func NewSendMessageRequest(chatID, text string, markup *ReplyMarkup) *SendMessageRequest {
	return &SendMessageRequest{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: markup,
	}
}
