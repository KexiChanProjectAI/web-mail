package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"lite-mail/internal/auth"
	"lite-mail/internal/config"
	"lite-mail/internal/storage"
)

type MessageHandler struct {
	db      *sql.DB
	storage *storage.Storage
	config  *config.Config
}

func NewMessageHandler(db *sql.DB, storage *storage.Storage, cfg *config.Config) *MessageHandler {
	return &MessageHandler{db: db, storage: storage, config: cfg}
}

type recipientDTO struct {
	Email string `json:"email"`
	Type  string `json:"type"`
}

type attachmentDTO struct {
	ID               int64  `json:"id"`
	OriginalFilename string `json:"original_filename"`
	MimeType         string `json:"mime_type"`
	SizeBytes        int64  `json:"size_bytes"`
}

type messageDTO struct {
	ID          int64            `json:"id"`
	Sender      string           `json:"sender"`
	Subject     string           `json:"subject"`
	MessageDate time.Time        `json:"message_date"`
	ReceivedAt  time.Time        `json:"received_at"`
	TextBody    string           `json:"text_body,omitempty"`
	HTMLBody    string           `json:"html_body,omitempty"`
	ParserStatus string          `json:"parser_status"`
	Recipients  []recipientDTO   `json:"recipients,omitempty"`
	Attachments []attachmentDTO  `json:"attachments,omitempty"`
}

func (h *MessageHandler) GetRawMIME(w http.ResponseWriter, r *http.Request) {
	messageID, ok := numericURLParam(w, r, "id")
	if !ok {
		return
	}
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		http.NotFound(w, r)
		return
	}

	var contentHash string
	rawQuery, rawArgs := rawMessageAuthQuery(session, messageID)
	err := h.db.QueryRow(rawQuery, rawArgs...).Scan(&contentHash)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	data, err := h.storage.ReadRawMIME(contentHash)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "message/rfc822")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"message-%d.eml\"", messageID))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

func (h *MessageHandler) GetMessage(w http.ResponseWriter, r *http.Request) {
	messageID, ok := numericURLParam(w, r, "id")
	if !ok {
		return
	}
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		http.NotFound(w, r)
		return
	}
	msg, err := h.getMessage(messageID, session)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, msg)
}

func (h *MessageHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		http.NotFound(w, r)
		return
	}
	page := parsePositiveInt(r.URL.Query().Get("page"), 1)
	perPage := parsePositiveInt(r.URL.Query().Get("per_page"), 50)
	if perPage > 100 {
		perPage = 100
	}
	offset := (page - 1) * perPage
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	where, args := listWhere(session, q)
	var total int
	if err := h.db.QueryRow("SELECT COUNT(DISTINCT m.id) FROM messages m "+where, args...).Scan(&total); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	args = append(args, perPage, offset)
	rows, err := h.db.Query(`SELECT DISTINCT m.id, m.sender, COALESCE(m.subject,''), m.message_date, m.received_at, COALESCE(m.text_body,''), COALESCE(m.html_body,'')
FROM messages m `+where+` ORDER BY m.received_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	messages := make([]messageDTO, 0)
	for rows.Next() {
		var msg messageDTO
		if err := rows.Scan(&msg.ID, &msg.Sender, &msg.Subject, &msg.MessageDate, &msg.ReceivedAt, &msg.TextBody, &msg.HTMLBody); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": messages, "total": total, "page": page, "per_page": perPage})
}

func (h *MessageHandler) getMessage(messageID int64, session *auth.Session) (*messageDTO, error) {
	var msg messageDTO
	query, args := messageAuthQuery(session, messageID)
	err := h.db.QueryRow(query, args...).Scan(&msg.ID, &msg.Sender, &msg.Subject, &msg.MessageDate, &msg.ReceivedAt, &msg.TextBody, &msg.HTMLBody, &msg.ParserStatus)
	if err != nil {
		return nil, err
	}
	recipients, err := h.recipients(messageID)
	if err != nil {
		return nil, err
	}
	attachments, err := h.attachments(messageID)
	if err != nil {
		return nil, err
	}
	msg.Recipients = recipients
	msg.Attachments = attachments
	return &msg, nil
}

func (h *MessageHandler) recipients(messageID int64) ([]recipientDTO, error) {
	rows, err := h.db.Query("SELECT recipient_email, recipient_type FROM message_recipients WHERE message_id = ? ORDER BY id", messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var recipients []recipientDTO
	for rows.Next() {
		var recipient recipientDTO
		if err := rows.Scan(&recipient.Email, &recipient.Type); err != nil {
			return nil, err
		}
		recipients = append(recipients, recipient)
	}
	return recipients, rows.Err()
}

func (h *MessageHandler) attachments(messageID int64) ([]attachmentDTO, error) {
	rows, err := h.db.Query("SELECT id, COALESCE(original_filename,''), mime_type, size_bytes FROM attachments WHERE message_id = ? ORDER BY id", messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var attachments []attachmentDTO
	for rows.Next() {
		var attachment attachmentDTO
		if err := rows.Scan(&attachment.ID, &attachment.OriginalFilename, &attachment.MimeType, &attachment.SizeBytes); err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, rows.Err()
}

func rawMessageAuthQuery(session *auth.Session, messageID int64) (string, []any) {
	if session.IsAdmin {
		return "SELECT content_hash FROM messages WHERE id = ?", []any{messageID}
	}
	return "SELECT m.content_hash FROM messages m WHERE m.id = ? AND EXISTS (SELECT 1 FROM message_recipients mr WHERE mr.message_id = m.id AND mr.recipient_email = ?)", []any{messageID, session.Email}
}

func messageAuthQuery(session *auth.Session, messageID int64) (string, []any) {
	if session.IsAdmin {
		return "SELECT id, sender, COALESCE(subject,''), message_date, received_at, COALESCE(text_body,''), COALESCE(html_body,''), parser_status FROM messages WHERE id = ?", []any{messageID}
	}
	return "SELECT m.id, m.sender, COALESCE(m.subject,''), m.message_date, m.received_at, COALESCE(m.text_body,''), COALESCE(m.html_body,''), m.parser_status FROM messages m WHERE m.id = ? AND EXISTS (SELECT 1 FROM message_recipients mr WHERE mr.message_id = m.id AND mr.recipient_email = ?)", []any{messageID, session.Email}
}

func listWhere(session *auth.Session, q string) (string, []any) {
	clauses := []string{}
	args := []any{}
	if !session.IsAdmin {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM message_recipients mr WHERE mr.message_id = m.id AND mr.recipient_email = ?)")
		args = append(args, session.Email)
	}
	if q != "" {
		clauses = append(clauses, "MATCH(m.sender, m.subject, m.text_body) AGAINST (? IN BOOLEAN MODE)")
		args = append(args, q)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func numericURLParam(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	value := chi.URLParam(r, name)
	if value == "" {
		http.NotFound(w, r)
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		http.NotFound(w, r)
		return 0, false
	}
	return parsed, true
}

func parsePositiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
