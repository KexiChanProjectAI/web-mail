package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"lite-mail/internal/auth"
)

func TestGetAttachmentValidIndex(t *testing.T) {
	f := newAPIFixture(t)
	messageID := insertMessage(t, f, "alice@example.test", "Attach", "body", []string{"owner@example.test"})
	insertAttachment(t, f, messageID, "file.txt", "text/plain", []byte("attachment body"))

	rr := serveAttachmentRequest(f.ah.GetAttachment, "/api/messages/"+itoa(messageID)+"/attachments/0", &auth.Session{Email: "owner@example.test"})
	if rr.Code != http.StatusOK || rr.Body.String() != "attachment body" || rr.Header().Get("Content-Type") != "text/plain" {
		t.Fatalf("attachment response status=%d content-type=%q body=%q", rr.Code, rr.Header().Get("Content-Type"), rr.Body.String())
	}
}

func TestGetAttachmentOutOfRangeReturns404(t *testing.T) {
	f := newAPIFixture(t)
	messageID := insertMessage(t, f, "alice@example.test", "Attach", "body", []string{"owner@example.test"})
	insertAttachment(t, f, messageID, "file.txt", "text/plain", []byte("attachment body"))

	rr := serveAttachmentRequest(f.ah.GetAttachment, "/api/messages/"+itoa(messageID)+"/attachments/1", &auth.Session{Email: "owner@example.test"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestGetAttachmentAuthorization(t *testing.T) {
	f := newAPIFixture(t)
	messageID := insertMessage(t, f, "alice@example.test", "Attach", "body", []string{"owner@example.test"})
	insertAttachment(t, f, messageID, "file.txt", "text/plain", []byte("attachment body"))

	if rr := serveAttachmentRequest(f.ah.GetAttachment, "/api/messages/"+itoa(messageID)+"/attachments/0", &auth.Session{Email: "other@example.test"}); rr.Code != http.StatusNotFound {
		t.Fatalf("other status = %d, want 404", rr.Code)
	}
	if rr := serveAttachmentRequest(f.ah.GetAttachment, "/api/messages/"+itoa(messageID)+"/attachments/0", &auth.Session{Email: "admin", IsAdmin: true}); rr.Code != http.StatusOK {
		t.Fatalf("admin status = %d", rr.Code)
	}
}

func TestGetAttachmentContentDispositionSanitizationAndBadID(t *testing.T) {
	f := newAPIFixture(t)
	messageID := insertMessage(t, f, "alice@example.test", "Attach", "body", []string{"owner@example.test"})
	insertAttachment(t, f, messageID, "../bad\r\n\"name.txt", "text/plain", []byte("body"))

	rr := serveAttachmentRequest(f.ah.GetAttachment, "/api/messages/"+itoa(messageID)+"/attachments/0", &auth.Session{Email: "owner@example.test"})
	cd := rr.Header().Get("Content-Disposition")
	if rr.Code != http.StatusOK || !strings.HasPrefix(cd, "attachment; filename=") || strings.ContainsAny(cd, "\r\n/\\") || strings.Contains(cd, "bad\"") {
		t.Fatalf("unsafe content-disposition: status=%d cd=%q", rr.Code, cd)
	}
	if rr := serveAttachmentRequest(f.ah.GetAttachment, "/api/messages/1/attachments/../x", &auth.Session{Email: "owner@example.test"}); rr.Code != http.StatusNotFound {
		t.Fatalf("path traversal status = %d, want 404", rr.Code)
	}
}

func serveAttachmentRequest(handler http.HandlerFunc, target string, session *auth.Session) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithSession(req.Context(), session)))
		})
	}).Get("/api/messages/{id}/attachments/{idx}", handler)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func insertAttachment(t *testing.T, f *apiFixture, messageID int64, filename, mimeType string, data []byte) {
	t.Helper()
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	storageKey := hash + ".bin"
	if err := f.st.SaveAttachment(messageID, storageKey, data); err != nil {
		t.Fatalf("save attachment: %v", err)
	}
	mustExec(t, f.db, `INSERT INTO attachments (message_id, storage_key, original_filename, mime_type, size_bytes, content_hash)
VALUES (?, ?, ?, ?, ?, ?)`, messageID, storageKey, filename, mimeType, len(data), hash)
}
