package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"lite-mail/internal/audit"
	"lite-mail/internal/auth"
	"lite-mail/internal/config"
	"lite-mail/internal/storage"
)

type AttachmentHandler struct {
	db      *sql.DB
	storage *storage.Storage
	config  *config.Config
	audit   *audit.Logger
}

// SetAudit wires the audit log; nil disables it.
func (h *AttachmentHandler) SetAudit(l *audit.Logger) { h.audit = l }

func NewAttachmentHandler(db *sql.DB, storage *storage.Storage, cfg *config.Config) *AttachmentHandler {
	return &AttachmentHandler{db: db, storage: storage, config: cfg}
}

func (h *AttachmentHandler) GetAttachment(w http.ResponseWriter, r *http.Request) {
	messageID, ok := numericURLParam(w, r, "id")
	if !ok {
		return
	}
	idx, ok := numericURLParamAllowZero(w, r, "idx")
	if !ok {
		return
	}
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		http.NotFound(w, r)
		return
	}

	var storageKey, originalFilename, mimeType string
	var sizeBytes int64
	query, args := attachmentAuthQuery(session, messageID, idx)
	err := h.db.QueryRow(query, args...).Scan(&storageKey, &originalFilename, &mimeType, &sizeBytes)
	if errors.Is(err, sql.ErrNoRows) {
		recordDenied(h.audit, h.db, r, session, messageID, "attachment")
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	data, err := h.storage.ReadAttachment(messageID, storageKey)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	filename := sanitizeFilename(originalFilename)
	if filename == "" {
		filename = "attachment"
	}
	recordAccess(h.audit, h.db, r, session, audit.AttachmentDownload, messageID,
		audit.WithDetail("attachment %d: %s", idx, filename))
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(sizeBytes, 10))
	_, _ = w.Write(data)
}

func attachmentAuthQuery(session *auth.Session, messageID int64, idx int64) (string, []any) {
	scope, scopeArgs := scopeClause(session, "")
	query := `SELECT a.storage_key, COALESCE(a.original_filename,''), a.mime_type, a.size_bytes
FROM attachments a JOIN messages m ON m.id = a.message_id` + whereClause("a.message_id = ?", scope) + " ORDER BY a.id LIMIT 1 OFFSET ?"
	args := append([]any{messageID}, scopeArgs...)
	return query, append(args, idx)
}

func numericURLParamAllowZero(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	value := chi.URLParam(r, name)
	if value == "" {
		http.NotFound(w, r)
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 || strconv.FormatInt(parsed, 10) != value {
		http.NotFound(w, r)
		return 0, false
	}
	return parsed, true
}

func sanitizeFilename(filename string) string {
	filename = strings.Map(func(r rune) rune {
		switch r {
		case '"', '\'', '\r', '\n', '/', '\\':
			return -1
		default:
			return r
		}
	}, filename)
	return strings.TrimSpace(filename)
}
