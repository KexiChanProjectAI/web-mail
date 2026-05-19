package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"lite-mail/internal/auth"
	"lite-mail/internal/config"
	"lite-mail/internal/storage"
	"lite-mail/internal/testutil"
)

type apiFixture struct {
	db *sql.DB
	mh *MessageHandler
	ah *AttachmentHandler
	st *storage.Storage
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	database := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.TeardownTestDB(t, database) })
	mustExec(t, database, "DELETE FROM attachments")
	mustExec(t, database, "DELETE FROM message_recipients")
	mustExec(t, database, "DELETE FROM messages")
	store, err := storage.NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	cfg := &config.Config{DataDir: t.TempDir(), SessionTTLHours: 24}
	return &apiFixture{db: database, mh: NewMessageHandler(database, store, cfg), ah: NewAttachmentHandler(database, store, cfg), st: store}
}

func TestListMessagesPaginationDefaultsAndLimits(t *testing.T) {
	f := newAPIFixture(t)
	insertMessage(t, f, "alice@example.test", "One", "body one", []string{"user@example.test"})
	insertMessage(t, f, "bob@example.test", "Two", "body two", []string{"user@example.test"})

	rr := serveMessageRequest(f.mh.ListMessages, "/api/messages", &auth.Session{Email: "user@example.test"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Messages []messageDTO `json:"messages"`
		Total    int          `json:"total"`
		Page     int          `json:"page"`
		PerPage  int          `json:"per_page"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Total != 2 || got.Page != 1 || got.PerPage != 50 || len(got.Messages) != 2 {
		t.Fatalf("unexpected list response: %+v", got)
	}

	rr = serveMessageRequest(f.mh.ListMessages, "/api/messages?per_page=1000", &auth.Session{Email: "user@example.test"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	got = struct {
		Messages []messageDTO `json:"messages"`
		Total    int          `json:"total"`
		Page     int          `json:"page"`
		PerPage  int          `json:"per_page"`
	}{}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.PerPage != 100 {
		t.Fatalf("per_page = %d, want 100", got.PerPage)
	}
}

func TestListMessagesSearchQuery(t *testing.T) {
	f := newAPIFixture(t)
	insertMessage(t, f, "alice@example.test", "FindMe", "uniquealpha searchable", []string{"user@example.test"})
	insertMessage(t, f, "bob@example.test", "Other", "ordinary text", []string{"user@example.test"})

	rr := serveMessageRequest(f.mh.ListMessages, "/api/messages?q=uniquealpha", &auth.Session{Email: "user@example.test"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Messages []messageDTO `json:"messages"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Subject != "FindMe" {
		t.Fatalf("unexpected search result: %+v", got.Messages)
	}
}

func TestGetMessageAuthorization(t *testing.T) {
	f := newAPIFixture(t)
	id := insertMessage(t, f, "alice@example.test", "Private", "body", []string{"owner@example.test"})

	if rr := serveMessageRequest(f.mh.GetMessage, "/api/messages/"+itoa(id), &auth.Session{Email: "owner@example.test"}); rr.Code != http.StatusOK {
		t.Fatalf("owner status = %d", rr.Code)
	}
	if rr := serveMessageRequest(f.mh.GetMessage, "/api/messages/"+itoa(id), &auth.Session{Email: "other@example.test"}); rr.Code != http.StatusNotFound {
		t.Fatalf("other status = %d, want 404", rr.Code)
	}
	if rr := serveMessageRequest(f.mh.GetMessage, "/api/messages/"+itoa(id), &auth.Session{Email: "admin", IsAdmin: true}); rr.Code != http.StatusOK {
		t.Fatalf("admin status = %d", rr.Code)
	}
}

func TestGetRawMIMEAuthorizationContentTypeAndBadID(t *testing.T) {
	f := newAPIFixture(t)
	id := insertMessage(t, f, "alice@example.test", "Raw", "body", []string{"owner@example.test"})

	rr := serveMessageRequest(f.mh.GetRawMIME, "/api/messages/"+itoa(id)+"/raw", &auth.Session{Email: "owner@example.test"})
	if rr.Code != http.StatusOK || rr.Header().Get("Content-Type") != "message/rfc822" || rr.Body.String() == "" {
		t.Fatalf("raw response status=%d content-type=%q body=%q", rr.Code, rr.Header().Get("Content-Type"), rr.Body.String())
	}
	if rr := serveMessageRequest(f.mh.GetRawMIME, "/api/messages/"+itoa(id)+"/raw", &auth.Session{Email: "other@example.test"}); rr.Code != http.StatusNotFound {
		t.Fatalf("other status = %d, want 404", rr.Code)
	}
	if rr := serveMessageRequest(f.mh.GetRawMIME, "/api/messages/../raw", &auth.Session{Email: "owner@example.test"}); rr.Code != http.StatusNotFound {
		t.Fatalf("path traversal status = %d, want 404", rr.Code)
	}
}

func serveMessageRequest(handler http.HandlerFunc, target string, session *auth.Session) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithSession(req.Context(), session)))
		})
	}).Get("/api/messages", handler)
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithSession(req.Context(), session)))
		})
	}).Get("/api/messages/{id}", handler)
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithSession(req.Context(), session)))
		})
	}).Get("/api/messages/{id}/raw", handler)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func insertMessage(t *testing.T, f *apiFixture, sender, subject, textBody string, recipients []string) int64 {
	t.Helper()
	raw := []byte("From: " + sender + "\r\nSubject: " + subject + "\r\n\r\n" + textBody)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	if err := f.st.SaveRawMIME(hash, raw); err != nil {
		t.Fatalf("save raw: %v", err)
	}
	res, err := f.db.Exec(`INSERT INTO messages (cloudflare_message_id, content_hash, sender, subject, message_date, received_at, text_body, html_body, raw_mime_path, parser_status)
VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, 'success')`, hash, hash, sender, subject, time.Now(), time.Now(), textBody, "raw/"+hash)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	id, _ := res.LastInsertId()
	for _, recipient := range recipients {
		mustExec(t, f.db, "INSERT INTO message_recipients (message_id, recipient_email, recipient_type) VALUES (?, ?, 'to')", id, recipient)
	}
	return id
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
