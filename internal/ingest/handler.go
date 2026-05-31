package ingest

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"lite-mail/internal/config"
	"lite-mail/internal/db"
	"lite-mail/internal/middleware"
	"lite-mail/internal/storage"
	"lite-mail/internal/telegram"
)

const ingestPSKHeader = "X-Lite-Mail-Ingest-PSK"

// IngestHandler accepts authenticated raw MIME messages from the Cloudflare Worker.
type IngestHandler struct {
	db              *sql.DB
	storage         *storage.Storage
	config          *config.Config
	telegramService *telegram.DeliveryService
}

// NewIngestHandler wires ingest dependencies.
func NewIngestHandler(db *sql.DB, s *storage.Storage, cfg *config.Config, telegramService *telegram.DeliveryService) *IngestHandler {
	return &IngestHandler{db: db, storage: s, config: cfg, telegramService: telegramService}
}

func (h *IngestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetRequestIDFromContext(r.Context())
	clientIP := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			clientIP = strings.TrimSpace(xff[:idx])
		} else {
			clientIP = xff
		}
	}

	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if h.config == nil || h.config.WorkerIngestPSK == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get(ingestPSKHeader)), []byte(h.config.WorkerIngestPSK)) != 1 {
		slog.Warn("ingest rejected: unauthorized",
			"request_id", requestID,
			"ip", clientIP,
			"status", "rejected",
		)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	limit := h.config.MaxMessageBytes
	if limit <= 0 {
		limit = 26214400
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read request body"})
		return
	}
	if int64(len(raw)) > limit {
		slog.Warn("ingest rejected: message too large",
			"request_id", requestID,
			"ip", clientIP,
			"status", "rejected",
			"message_size", len(raw),
		)
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "message too large"})
		return
	}
	if h.db == nil || h.storage == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "ingest dependencies unavailable"})
		return
	}

	contentHash := HashBytes(raw)
	duplicate, err := h.isDuplicate(r.Context(), contentHash)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "check duplicate"})
		return
	}
	if duplicate {
		slog.Info("ingest duplicate",
			"request_id", requestID,
			"ip", clientIP,
			"content_hash", contentHash,
			"status", "duplicate",
			"message_size", len(raw),
		)
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}

	parsed, parseErr := ParseMIME(raw)
	parserStatus := "success"
	parserError := sql.NullString{}
	if parseErr != nil {
		parserStatus = "failed"
		parserError = sql.NullString{String: parseErr.Error(), Valid: true}
		parsed = fallbackParsedMessage(contentHash)
	}
	if parsed.ContentHash == "" {
		parsed.ContentHash = contentHash
	}

	if err := h.storage.SaveRawMIME(contentHash, raw); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "save raw MIME"})
		return
	}

	messageID, err := h.persist(r.Context(), parsed, parserStatus, parserError)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "persist message"})
		return
	}
	slog.Info("ingest accepted",
		"request_id", requestID,
		"ip", clientIP,
		"content_hash", contentHash,
		"status", "accepted",
		"message_size", len(raw),
	)
	writeJSON(w, http.StatusOK, map[string]any{"status": "accepted", "message_id": messageID})

	// After successful persist, attempt Telegram notification (non-blocking)
	if h.telegramService != nil && h.config.TelegramEnabled() {
		go func(mid int64, p *ParsedMessage) {
			ctx := context.Background()
			// Create or reuse share token
			token, err := db.CreateShareToken(ctx, h.db, mid)
			if err != nil {
				slog.Warn("telegram: create share token", "message_id", mid, "error", err)
				return
			}

			// Build summary from parsed message
			summary := telegram.BuildSummary(&telegram.SummaryInput{
				From:       p.Sender,
				Subject:    p.Subject,
				TextBody:   p.TextBody,
				ReceivedAt: p.MessageDate,
			}, h.config.PublicBaseURL, token)

			// Build reply markup with URL buttons
			markup := telegram.BuildReplyMarkup(h.config.PublicBaseURL, token)

			// Send via delivery service (handles idempotency, failure recording)
			if err := h.telegramService.Deliver(ctx, mid, summary, markup); err != nil {
				slog.Warn("telegram: delivery failed",
					"message_id", mid,
					"error", err,
				)
			}
		}(messageID, parsed)
	}
}

func (h *IngestHandler) isDuplicate(ctx context.Context, contentHash string) (bool, error) {
	var id int64
	err := h.db.QueryRowContext(ctx, "SELECT id FROM ingest_events WHERE content_hash = ? LIMIT 1", contentHash).Scan(&id)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (h *IngestHandler) persist(ctx context.Context, parsed *ParsedMessage, parserStatus string, parserError sql.NullString) (int64, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	messageDate := parsed.MessageDate
	if messageDate.IsZero() {
		messageDate = time.Now().UTC()
	}
	rawPath := fmt.Sprintf("raw/%s", parsed.ContentHash)
	result, err := tx.ExecContext(ctx, `INSERT INTO messages (cloudflare_message_id, content_hash, sender, subject, message_date, received_at, text_body, html_body, raw_mime_path, parser_status, parser_error) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, parsed.ContentHash, parsed.ContentHash, parsed.Sender, parsed.Subject, messageDate, time.Now().UTC(), parsed.TextBody, parsed.HTMLBody, rawPath, parserStatus, parserError)
	if err != nil {
		return 0, err
	}
	messageID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	for _, recipient := range parsed.Recipients {
		if _, err := tx.ExecContext(ctx, `INSERT INTO message_recipients (message_id, recipient_email, recipient_type, received_at) VALUES (?, ?, ?, ?)`, messageID, recipient.Email, recipient.Type, time.Now().UTC()); err != nil {
			return 0, err
		}
	}
	for _, attachment := range parsed.Attachments {
		safeKey := attachment.ContentHash
		if err := h.storage.SaveAttachment(messageID, safeKey, attachment.Content); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO attachments (message_id, storage_key, original_filename, mime_type, size_bytes, content_hash) VALUES (?, ?, ?, ?, ?, ?)`, messageID, safeKey, nullableString(attachment.OriginalFilename), attachment.MimeType, attachment.Size, attachment.ContentHash); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ingest_events (cloudflare_message_id, content_hash, status) VALUES (?, ?, 'accepted')`, parsed.ContentHash, parsed.ContentHash); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return messageID, nil
}

func fallbackParsedMessage(contentHash string) *ParsedMessage {
	return &ParsedMessage{ContentHash: contentHash, MessageDate: time.Now().UTC()}
}

func nullableString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
