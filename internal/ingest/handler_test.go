package ingest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"lite-mail/internal/config"
	"lite-mail/internal/storage"
	"lite-mail/internal/testutil"
)

func TestIngestHandlerPSKValidation(t *testing.T) {
	h := NewIngestHandler(nil, nil, &config.Config{WorkerIngestPSK: "secret", MaxMessageBytes: 1024})

	for _, tc := range []struct {
		name string
		psk  string
		want int
	}{
		{name: "missing", want: http.StatusUnauthorized},
		{name: "invalid", psk: "wrong", want: http.StatusUnauthorized},
		{name: "valid reaches dependencies", psk: "secret", want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/ingest", bytes.NewReader([]byte("test")))
			if tc.psk != "" {
				req.Header.Set(ingestPSKHeader, tc.psk)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d", rr.Code, tc.want)
			}
		})
	}
}

func TestIngestHandlerBodySizeLimit(t *testing.T) {
	h := NewIngestHandler(nil, nil, &config.Config{WorkerIngestPSK: "secret", MaxMessageBytes: 3})
	req := httptest.NewRequest(http.MethodPost, "/api/ingest", bytes.NewReader([]byte("toolong")))
	req.Header.Set(ingestPSKHeader, "secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestIngestHandlerSuccessfulFlowAndDeduplication(t *testing.T) {
	database := testutil.SetupTestDB(t)
	defer testutil.TeardownTestDB(t, database)

	s, err := storage.NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	h := NewIngestHandler(database, s, &config.Config{WorkerIngestPSK: "secret", MaxMessageBytes: testutil.MaxMessageBytes})
	raw := loadFixture(t, "attachment-multipart.eml")

	server := httptest.NewServer(h)
	defer server.Close()

	resp := postIngest(t, server.URL, "secret", raw)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d", resp.StatusCode)
	}
	var accepted struct {
		Status    string `json:"status"`
		MessageID int64  `json:"message_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
		t.Fatalf("decode accepted response: %v", err)
	}
	resp.Body.Close()
	if accepted.Status != "accepted" || accepted.MessageID == 0 {
		t.Fatalf("accepted response = %#v", accepted)
	}

	if _, err := s.ReadRawMIME(HashBytes(raw)); err != nil {
		t.Fatalf("ReadRawMIME: %v", err)
	}
	var sender, subject, textBody string
	if err := database.QueryRow("SELECT sender, subject, text_body FROM messages WHERE id = ?", accepted.MessageID).Scan(&sender, &subject, &textBody); err != nil {
		t.Fatalf("query message: %v", err)
	}
	if sender != "sender@example.com" || subject != "Email with Attachment" || textBody == "" {
		t.Fatalf("message metadata = %q %q %q", sender, subject, textBody)
	}

	resp = postIngest(t, server.URL, "secret", raw)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("duplicate status = %d", resp.StatusCode)
	}
	var duplicate struct{ Status string }
	if err := json.NewDecoder(resp.Body).Decode(&duplicate); err != nil {
		t.Fatalf("decode duplicate response: %v", err)
	}
	if duplicate.Status != "duplicate" {
		t.Fatalf("duplicate status body = %#v", duplicate)
	}
}

func postIngest(t *testing.T, url, psk string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set(ingestPSKHeader, psk)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}
