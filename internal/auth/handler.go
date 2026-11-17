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

// AccessLogoutPath ends a Cloudflare Access session on a protected hostname.
const AccessLogoutPath = "/cdn-cgi/access/logout"

type AuthHandler struct {
	sessionStore SessionStore
	config       *config.Config
}

func NewAuthHandler(sessionStore SessionStore, cfg *config.Config) *AuthHandler {
	if cfg == nil {
		cfg = &config.Config{}
	}
	return &AuthHandler{sessionStore: sessionStore, config: cfg}
}

type loginRequest struct {
	PSK string `json:"psk"`
}

type loginResponse struct {
	Status  string `json:"status"`
	IsAdmin bool   `json:"is_admin"`
	Email   string `json:"email"`
}

// LoginHandler is the emergency admin login. It only accepts ADMIN_PSK; normal
// users sign in with Google or Cloudflare Access.
func (h *AuthHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	requestID := middleware.GetRequestIDFromContext(r.Context())
	clientIP := clientIPFromRequest(r)

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("login failed: invalid json", "request_id", requestID, "ip", clientIP)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	cfg := h.config
	if cfg.AdminPSK == "" || subtle.ConstantTimeCompare([]byte(req.PSK), []byte(cfg.AdminPSK)) != 1 {
		slog.Warn("login failed: invalid credentials", "request_id", requestID, "ip", clientIP, "psk_enabled", cfg.AdminPSK != "")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	session := Session{Email: "admin", IsAdmin: true, Provider: ProviderAdminPSK}
	if err := StartSession(w, r, h.sessionStore, cfg, &session); err != nil {
		slog.Error("login failed: failed to create session", "request_id", requestID, "ip", clientIP)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}

	slog.Warn("emergency admin login via ADMIN_PSK", "request_id", requestID, "ip", clientIP)
	writeJSON(w, http.StatusOK, loginResponse{Status: "ok", IsAdmin: true, Email: session.Email})
}

// StartSession discards any session referenced by the incoming cookie (so a
// planted token can never be upgraded), stores a fresh one and sets its cookie.
func StartSession(w http.ResponseWriter, r *http.Request, store SessionStore, cfg *config.Config, session *Session) error {
	if cookie, err := r.Cookie(sessionCookieName(cfg)); err == nil && cookie.Value != "" {
		_ = store.Delete(cookie.Value)
	}
	session.Token = ""
	session.ExpiresAt = time.Now().Add(sessionTTL(cfg))
	if err := store.Create(session); err != nil {
		return err
	}
	http.SetCookie(w, NewSessionCookie(cfg, session.Token, session.ExpiresAt))
	return nil
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
	resp := map[string]string{"status": "ok"}
	if h.config.CFAccessEnabled() {
		resp["access_logout"] = AccessLogoutPath
	}
	writeJSON(w, http.StatusOK, resp)
}

type grantDTO struct {
	Pattern string `json:"pattern"`
	Kind    string `json:"kind"`
}

type meResponse struct {
	Email      string     `json:"email"`
	IsAdmin    bool       `json:"is_admin"`
	IsEnvAdmin bool       `json:"is_env_admin"`
	Provider   string     `json:"provider"`
	Grants     []grantDTO `json:"grants"`
}

// MeHandler describes the signed-in principal. It must run behind RequireAuth.
func (h *AuthHandler) MeHandler(w http.ResponseWriter, r *http.Request) {
	session := SessionFromContext(r.Context())
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	grants := make([]grantDTO, 0, len(session.Grants))
	for _, g := range session.Grants {
		grants = append(grants, grantDTO{Pattern: g.Pattern, Kind: string(g.Kind)})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, meResponse{
		Email:      session.Email,
		IsAdmin:    session.IsAdmin,
		IsEnvAdmin: session.IsEnvAdmin,
		Provider:   session.Provider,
		Grants:     grants,
	})
}

// ProvidersHandler tells the login page which sign-in methods are configured.
func (h *AuthHandler) ProvidersHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"google":    h.config.GoogleEnabled(),
		"cf_access": h.config.CFAccessEnabled(),
		"admin_psk": h.config.AdminPSK != "",
	})
}

func clientIPFromRequest(r *http.Request) string {
	clientIP := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			clientIP = strings.TrimSpace(xff[:idx])
		} else {
			clientIP = xff
		}
	}
	return clientIP
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
