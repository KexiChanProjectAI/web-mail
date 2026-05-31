package telegram

import (
	"fmt"
	"strings"
	"time"
)

// SummaryInput holds the email data needed to build a Telegram summary message.
type SummaryInput struct {
	From       string
	Subject    string
	TextBody   string
	ReceivedAt time.Time
}

const maxMessageLen = 3500
const bodyPreviewLen = 500

// EscapeHTML escapes <, >, & for Telegram HTML parse mode.
func EscapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// BuildSummary builds an HTML-escaped, truncated summary text for a Telegram message.
func BuildSummary(msg *SummaryInput, baseURL, token string) string {
	bodyPreview := msg.TextBody
	if len(bodyPreview) > bodyPreviewLen {
		bodyPreview = bodyPreview[:bodyPreviewLen]
	}

	text := fmt.Sprintf("📧 New Email\nFrom: %s\nSubject: %s\nReceived: %s\n\n%s",
		EscapeHTML(msg.From),
		EscapeHTML(msg.Subject),
		EscapeHTML(msg.ReceivedAt.Format(time.RFC1123)),
		EscapeHTML(bodyPreview),
	)

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
