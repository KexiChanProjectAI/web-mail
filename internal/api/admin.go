package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"lite-mail/internal/auth"
	"lite-mail/internal/config"
	"lite-mail/internal/db"
	"lite-mail/internal/emailaddr"
	"lite-mail/internal/middleware"
)

const knownMailboxLimit = 200

// AdminHandler manages users, admin flags and mailbox grants.
type AdminHandler struct {
	users *db.UserStore
	cfg   *config.Config
}

func NewAdminHandler(users *db.UserStore, cfg *config.Config) *AdminHandler {
	return &AdminHandler{users: users, cfg: cfg}
}

type adminGrantDTO struct {
	ID        int64     `json:"id"`
	Pattern   string    `json:"pattern"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy *string   `json:"created_by"`
}

type adminUserDTO struct {
	ID                int64           `json:"id"`
	Email             string          `json:"email"`
	IsAdmin           bool            `json:"is_admin"`
	IsEnvAdmin        bool            `json:"is_env_admin"`
	CreatedAt         time.Time       `json:"created_at"`
	LastLoginAt       *time.Time      `json:"last_login_at"`
	LastLoginProvider *string         `json:"last_login_provider"`
	Grants            []adminGrantDTO `json:"grants"`
}

func (h *AdminHandler) userDTO(u db.User, grants []db.MailboxGrant) adminUserDTO {
	dto := adminUserDTO{
		ID:         u.ID,
		Email:      u.Email,
		IsAdmin:    u.IsAdmin,
		IsEnvAdmin: h.cfg.IsEnvAdmin(u.Email),
		CreatedAt:  u.CreatedAt,
		Grants:     []adminGrantDTO{},
	}
	if u.LastLoginAt.Valid {
		t := u.LastLoginAt.Time
		dto.LastLoginAt = &t
	}
	if u.LastLoginProvider.Valid {
		p := u.LastLoginProvider.String
		dto.LastLoginProvider = &p
	}
	for _, g := range grants {
		if g.UserID == u.ID {
			dto.Grants = append(dto.Grants, grantDTO(g))
		}
	}
	return dto
}

func grantDTO(g db.MailboxGrant) adminGrantDTO {
	dto := adminGrantDTO{ID: g.ID, Pattern: g.Pattern, Kind: string(auth.GrantExact), CreatedAt: g.CreatedAt}
	if strings.HasPrefix(g.Pattern, "*@") {
		dto.Kind = string(auth.GrantDomain)
	}
	if g.CreatedBy.Valid {
		by := g.CreatedBy.String
		dto.CreatedBy = &by
	}
	return dto
}

// ListUsers: GET /api/admin/users
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.users.ListUsers(r.Context())
	if err != nil {
		h.internalError(w, r, "list users", err)
		return
	}
	grants, err := h.users.ListGrants(r.Context())
	if err != nil {
		h.internalError(w, r, "list grants", err)
		return
	}
	out := make([]adminUserDTO, 0, len(users))
	seen := map[string]bool{}
	for _, u := range users {
		out = append(out, h.userDTO(u, grants))
		seen[u.Email] = true
	}
	envOnly := []string{}
	for _, email := range h.cfg.AdminEmails {
		if !seen[email] {
			envOnly = append(envOnly, email)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out, "env_admins": envOnly})
}

// CreateUser: POST /api/admin/users {"email"}
func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	email := emailaddr.Canonical(req.Email)
	if !validLoginEmail(email) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid email"})
		return
	}
	u, err := h.users.CreateUser(r.Context(), email)
	if errors.Is(err, db.ErrUserExists) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "user already exists"})
		return
	}
	if err != nil {
		h.internalError(w, r, "create user", err)
		return
	}
	h.audit(r, "user created", "target", email)
	writeJSON(w, http.StatusCreated, h.userDTO(*u, nil))
}

// UpdateUser: PATCH /api/admin/users/{id} {"is_admin"}
func (h *AdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := numericURLParam(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		IsAdmin *bool `json:"is_admin"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.IsAdmin == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "is_admin is required"})
		return
	}
	u, err := h.users.FindUser(r.Context(), id)
	if errors.Is(err, db.ErrUserNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if err != nil {
		h.internalError(w, r, "find user", err)
		return
	}
	if !*req.IsAdmin && h.cfg.IsEnvAdmin(u.Email) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "cannot demote an ADMIN_EMAILS admin"})
		return
	}
	if err := h.users.SetAdmin(r.Context(), id, *req.IsAdmin); err != nil {
		h.internalError(w, r, "set admin", err)
		return
	}
	h.audit(r, "user admin flag changed", "target", u.Email, "is_admin", *req.IsAdmin)
	h.writeUser(w, r, id, http.StatusOK)
}

// DeleteUser: DELETE /api/admin/users/{id}
func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := numericURLParam(w, r, "id")
	if !ok {
		return
	}
	u, err := h.users.FindUser(r.Context(), id)
	if errors.Is(err, db.ErrUserNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if err != nil {
		h.internalError(w, r, "find user", err)
		return
	}
	if err := h.users.DeleteUser(r.Context(), id); err != nil && !errors.Is(err, db.ErrUserNotFound) {
		h.internalError(w, r, "delete user", err)
		return
	}
	h.audit(r, "user deleted", "target", u.Email)
	w.WriteHeader(http.StatusNoContent)
}

// AddGrant: POST /api/admin/users/{id}/grants {"pattern"}
func (h *AdminHandler) AddGrant(w http.ResponseWriter, r *http.Request) {
	id, ok := numericURLParam(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Pattern string `json:"pattern"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	grant, err := auth.ParseGrant(req.Pattern)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	createdBy := ""
	if session := auth.SessionFromContext(r.Context()); session != nil {
		createdBy = session.Email
	}
	g, err := h.users.AddGrant(r.Context(), id, grant.Pattern, createdBy)
	switch {
	case errors.Is(err, db.ErrUserNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	case errors.Is(err, db.ErrGrantExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "grant already exists"})
		return
	case err != nil:
		h.internalError(w, r, "add grant", err)
		return
	}
	h.audit(r, "grant added", "user_id", id, "pattern", grant.Pattern)
	writeJSON(w, http.StatusCreated, grantDTO(*g))
}

// DeleteGrant: DELETE /api/admin/users/{id}/grants/{grantId}
func (h *AdminHandler) DeleteGrant(w http.ResponseWriter, r *http.Request) {
	id, ok := numericURLParam(w, r, "id")
	if !ok {
		return
	}
	grantID, ok := numericURLParam(w, r, "grantId")
	if !ok {
		return
	}
	err := h.users.DeleteGrant(r.Context(), id, grantID)
	if errors.Is(err, db.ErrGrantNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "grant not found"})
		return
	}
	if err != nil {
		h.internalError(w, r, "delete grant", err)
		return
	}
	h.audit(r, "grant removed", "user_id", id, "grant_id", grantID)
	w.WriteHeader(http.StatusNoContent)
}

// ListMailboxes: GET /api/admin/mailboxes?q=
func (h *AdminHandler) ListMailboxes(w http.ResponseWriter, r *http.Request) {
	addresses, domains, err := h.users.KnownMailboxes(r.Context(), r.URL.Query().Get("q"), knownMailboxLimit)
	if err != nil {
		h.internalError(w, r, "list mailboxes", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"addresses": addresses, "domains": domains})
}

func (h *AdminHandler) writeUser(w http.ResponseWriter, r *http.Request, id int64, status int) {
	u, err := h.users.FindUser(r.Context(), id)
	if err != nil {
		h.internalError(w, r, "find user", err)
		return
	}
	grants, err := h.users.ListGrants(r.Context())
	if err != nil {
		h.internalError(w, r, "list grants", err)
		return
	}
	writeJSON(w, status, h.userDTO(*u, grants))
}

func (h *AdminHandler) audit(r *http.Request, msg string, attrs ...any) {
	actor := ""
	if session := auth.SessionFromContext(r.Context()); session != nil {
		actor = session.Email
	}
	attrs = append([]any{"request_id", middleware.GetRequestIDFromContext(r.Context()), "actor", actor}, attrs...)
	slog.Info("admin: "+msg, attrs...)
}

func (h *AdminHandler) internalError(w http.ResponseWriter, r *http.Request, op string, err error) {
	slog.Error("admin operation failed", "request_id", middleware.GetRequestIDFromContext(r.Context()), "op", op, "error", err.Error())
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return false
	}
	return true
}

// validLoginEmail accepts a single local@domain with a dotted domain.
func validLoginEmail(email string) bool {
	if len(email) > 320 || strings.Count(email, "@") != 1 || strings.ContainsAny(email, " \t\r\n*\"\\,") {
		return false
	}
	local, domain, _ := strings.Cut(email, "@")
	return local != "" && strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}
