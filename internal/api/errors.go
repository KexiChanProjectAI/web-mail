package api

import (
	"context"
	"encoding/json"
	"net/http"
)

// ErrorResponse represents a consistent error response format.
type ErrorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

// WriteError writes a consistent JSON error response.
func WriteError(w http.ResponseWriter, status int, message string, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := ErrorResponse{Error: message}
	if requestID != "" {
		resp.RequestID = requestID
	}
	json.NewEncoder(w).Encode(resp)
}

// requestIDKey is the context key for request ID.
type requestIDKey struct{}

// WithRequestID adds a request ID to context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

// RequestIDFromContext extracts request ID from context.
func RequestIDFromContext(ctx context.Context) string {
	if v := ctx.Value(requestIDKey{}); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
