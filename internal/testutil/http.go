package testutil

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func NewTestServer(handler http.Handler) *httptest.Server {
	return httptest.NewServer(handler)
}

func NewTestServerWithRouter() *httptest.Server {
	return httptest.NewServer(http.NewServeMux())
}

type PSKTransport struct {
	PSK string
}

func (t *PSKTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.PSK != "" {
		req.Header.Set("X-PSK", t.PSK)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func NewPSKClient(psk string) *http.Client {
	return &http.Client{
		Transport: &PSKTransport{PSK: psk},
	}
}

func AuthRequest(req *http.Request, psk string) *http.Request {
	req = req.Clone(req.Context())
	if psk != "" {
		req.Header.Set("X-PSK", psk)
	}
	return req
}

func DoPSKRequest(t *testing.T, method, url string, psk string, body interface{}) *http.Response {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req = AuthRequest(req, psk)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}
