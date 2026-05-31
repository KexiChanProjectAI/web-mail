package server

import (
	"database/sql"
	"fmt"
	"html"
	"net/http"

	"github.com/go-chi/chi/v5"

	"lite-mail/internal/config"
	"lite-mail/internal/db"
	"lite-mail/internal/storage"
)

// ShareHandler serves public share views without authentication.
type ShareHandler struct {
	db     *sql.DB
	store  *storage.Storage
	config *config.Config
}

// NewShareHandler wires share handler dependencies.
func NewShareHandler(database *sql.DB, store *storage.Storage, cfg *config.Config) *ShareHandler {
	return &ShareHandler{db: database, store: store, config: cfg}
}

// ServeHTML returns the shared message as HTML.
// GET /share/{token} and GET /share/{token}/html
func (h *ShareHandler) ServeHTML(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	msg, err := h.lookupMessage(r, token)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'")

	if msg.HTMLBody != "" {
		_, _ = w.Write([]byte(msg.HTMLBody))
		return
	}

	_, _ = w.Write([]byte(renderShareHTML(msg.Sender, msg.Subject, msg.MessageDate, msg.TextBody)))
}

// ServeTXT returns the shared message as plain text.
// GET /share/{token}/txt
func (h *ShareHandler) ServeTXT(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	msg, err := h.lookupMessage(r, token)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if msg.TextBody != "" {
		_, _ = w.Write([]byte(msg.TextBody))
		return
	}

	// Fallback: metadata-only plain text
	fallback := fmt.Sprintf("From: %s\nSubject: %s\nDate: %s\n\n(No text content available)",
		msg.Sender, msg.Subject, msg.MessageDate)
	_, _ = w.Write([]byte(fallback))
}

type shareMessage struct {
	Sender      string
	Subject     string
	MessageDate string
	TextBody    string
	HTMLBody    string
}

// lookupMessage finds a message by share token. Returns a non-nil error for
// any failure, ensuring no metadata leaks on 404.
func (h *ShareHandler) lookupMessage(r *http.Request, token string) (*shareMessage, error) {
	messageID, err := db.FindMessageIDByToken(r.Context(), h.db, token)
	if err != nil {
		return nil, err
	}

	var msg shareMessage
	var messageDate sql.NullTime
	err = h.db.QueryRowContext(r.Context(),
		"SELECT sender, subject, message_date, text_body, html_body FROM messages WHERE id = ?",
		messageID,
	).Scan(&msg.Sender, &msg.Subject, &messageDate, &msg.TextBody, &msg.HTMLBody)
	if err != nil {
		return nil, err
	}

	if messageDate.Valid {
		msg.MessageDate = messageDate.Time.Format("2006-01-02 15:04:05")
	}

	return &msg, nil
}

// renderShareHTML produces a minimal HTML5 document for messages without html_body.
func renderShareHTML(sender, subject, messageDate, textBody string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>%s</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>body{font-family:sans-serif;max-width:720px;margin:2em auto;padding:0 1em;line-height:1.6}
h1{font-size:1.2em;color:#333}.meta{color:#666;font-size:0.9em;margin-bottom:1em}
pre{white-space:pre-wrap;word-wrap:break-word;background:#f5f5f5;padding:1em;border-radius:4px}
hr{border:none;border-top:1px solid #ddd;margin:1em 0}
a{color:#1a73e8}</style></head>
<body>
<h1>%s</h1>
<div class="meta">From: %s<br>Date: %s</div>
<hr>
<pre>%s</pre>
</body></html>`,
		html.EscapeString(subject),
		html.EscapeString(subject),
		html.EscapeString(sender),
		html.EscapeString(messageDate),
		html.EscapeString(textBody),
	)
}
