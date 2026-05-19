package auth

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"lite-mail/internal/config"
	"lite-mail/internal/middleware"
)

type AuthHandler struct {
	sessionStore SessionStore
	config       *config.Config
}

func NewAuthHandler(sessionStore SessionStore, cfg *config.Config) *AuthHandler {
	return &AuthHandler{sessionStore: sessionStore, config: cfg}
}

type loginRequest struct {
	PSK   string `json:"psk"`
	Email string `json:"email"`
}

type loginResponse struct {
	Status  string `json:"status"`
	IsAdmin bool   `json:"is_admin"`
	Email   string `json:"email"`
}

func (h *AuthHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	requestID := middleware.GetRequestIDFromContext(r.Context())
	clientIP := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			clientIP = strings.TrimSpace(xff[:idx])
		} else {
			clientIP = xff
		}
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("login failed: invalid json",
			"request_id", requestID,
			"ip", clientIP,
		)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	cfg := h.config
	if cfg == nil {
		cfg = &config.Config{}
	}

	var session Session
	var pskType string
	switch {
	case cfg.AdminPSK != "" && subtle.ConstantTimeCompare([]byte(req.PSK), []byte(cfg.AdminPSK)) == 1:
		session.Email = "admin"
		session.IsAdmin = true
		pskType = "admin"
	case cfg.NormalUserPSK != "" && subtle.ConstantTimeCompare([]byte(req.PSK), []byte(cfg.NormalUserPSK)) == 1:
		email := strings.ToLower(strings.TrimSpace(req.Email))
		if email == "" {
			slog.Warn("login failed: email required for normal user",
				"request_id", requestID,
				"ip", clientIP,
				"psk_type", "normal",
			)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		session.Email = email
		pskType = "normal"
	default:
		slog.Warn("login failed: invalid credentials",
			"request_id", requestID,
			"ip", clientIP,
			"psk_type", "invalid",
		)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	session.ExpiresAt = time.Now().Add(sessionTTL(cfg))
	if err := h.sessionStore.Create(&session); err != nil {
		slog.Error("login failed: failed to create session",
			"request_id", requestID,
			"ip", clientIP,
			"psk_type", pskType,
			"email", session.Email,
		)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}

	slog.Info("login successful",
		"request_id", requestID,
		"ip", clientIP,
		"psk_type", pskType,
		"email", session.Email,
	)
	http.SetCookie(w, NewSessionCookie(cfg, session.Token, session.ExpiresAt))
	writeJSON(w, http.StatusOK, loginResponse{Status: "ok", IsAdmin: session.IsAdmin, Email: session.Email})
}

func (h *AuthHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	if cookie, err := r.Cookie(sessionCookieName(h.config)); err == nil {
		_ = h.sessionStore.Delete(cookie.Value)
	}
	http.SetCookie(w, ExpiredSessionCookie(h.config))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func sessionTTL(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.SessionTTLHours <= 0 {
		return 24 * time.Hour
	}
	return cfg.SessionTTL()
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
