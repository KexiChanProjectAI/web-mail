package server

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"lite-mail/internal/api"
	"lite-mail/internal/audit"
	"lite-mail/internal/auth"
	"lite-mail/internal/config"
	maildb "lite-mail/internal/db"
	"lite-mail/internal/ingest"
	"lite-mail/internal/middleware"
	"lite-mail/internal/storage"
)

const (
	rateLimitLogin  = 10
	rateLimitIngest = 60
	maxBodyBytes    = 1 << 20 // 1MB for general API endpoints
	maxAdminBody    = 64 << 10
)

// Option customises Server construction; used by tests to inject fakes.
type Option func(*options)

type options struct {
	accessVerifier *auth.AccessVerifier
	googleIssuer   string
	httpClient     *http.Client
}

// WithAccessVerifier replaces the Cloudflare Access verifier built from config.
func WithAccessVerifier(v *auth.AccessVerifier) Option {
	return func(o *options) { o.accessVerifier = v }
}

// WithGoogleIssuer points Google sign-in at another OIDC issuer (tests).
func WithGoogleIssuer(issuer string, client *http.Client) Option {
	return func(o *options) {
		o.googleIssuer = issuer
		o.httpClient = client
	}
}

// Server owns the HTTP router and shared application dependencies.
type Server struct {
	config *config.Config
	db     *sql.DB
	store  *storage.Storage
	router *chi.Mux
	http   *http.Server

	sessionStore  auth.SessionStore
	authHandler   *auth.AuthHandler
	authenticator *auth.Authenticator
	googleHandler *auth.GoogleHandler
	adminHandler  *api.AdminHandler
	auditLog      *audit.Logger

	messageHandler    *api.MessageHandler
	attachmentHandler *api.AttachmentHandler
	ingestHandler     *ingest.IngestHandler
}

// New wires middleware, routes, and dependencies into a Server.
func New(cfg *config.Config, db *sql.DB, opts ...Option) *Server {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	if cfg.SessionCookieName == "" {
		cfg.SessionCookieName = "lite_mail_session"
	}
	if cfg.SessionTTLHours <= 0 {
		cfg.SessionTTLHours = 24
	}
	r := chi.NewRouter()
	sessionStore := auth.NewCookieSessionStore(cfg.SessionCookieName)
	authHandler := auth.NewAuthHandler(sessionStore, cfg)
	store, err := storage.NewStorage(cfg.DataDir)
	if err != nil {
		panic(err)
	}
	s := &Server{config: cfg, db: db, store: store, router: r, sessionStore: sessionStore, authHandler: authHandler}
	s.messageHandler = api.NewMessageHandler(db, store, cfg)
	s.attachmentHandler = api.NewAttachmentHandler(db, store, cfg)

	s.ingestHandler = ingest.NewIngestHandler(db, store, cfg)

	userStore := maildb.NewUserStore(db)
	resolver := auth.NewResolver(userStore, cfg)
	access := o.accessVerifier
	if access == nil && cfg.CFAccessEnabled() {
		access = auth.NewAccessVerifier(cfg.CFAccessIssuer(), cfg.CFAccessAUD)
	}
	s.authenticator = auth.NewAuthenticator(sessionStore, cfg, resolver, access, userStore)
	if cfg.GoogleEnabled() {
		s.googleHandler = auth.NewGoogleHandler(cfg, sessionStore, userStore, o.googleIssuer, o.httpClient)
	}
	s.adminHandler = api.NewAdminHandler(userStore, cfg)

	if db != nil {
		s.auditLog = audit.New(db)
	}
	s.authenticator.SetAuditor(auditorOrNil(s.auditLog))
	s.authHandler.SetAudit(s.authenticator, auditorOrNil(s.auditLog))
	if s.googleHandler != nil {
		s.googleHandler.SetAuditor(auditorOrNil(s.auditLog))
	}
	s.messageHandler.SetAudit(s.auditLog)
	s.attachmentHandler.SetAudit(s.auditLog)
	s.adminHandler.SetAudit(s.auditLog)
	sameOrigin := middleware.RequireSameOrigin(func() string { return cfg.PublicBaseURL })

	r.Use(middleware.RequestID("X-Request-ID"))
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.RequestLogging(slog.Default()))
	r.Use(middleware.CORS(cfg.PublicBaseURL))

	subFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(subFS)))

	r.Get("/healthz", s.healthHandler)

	r.Route("/api/ingest", func(r chi.Router) {
		r.Use(middleware.RateLimit(rateLimitIngest, rateLimitIngest))
		r.Use(middleware.MaxBodySize(cfg.MaxMessageBytes))
		r.Post("/", s.ingestHandler.ServeHTTP)
	})

	r.Get("/api/auth/providers", s.authHandler.ProvidersHandler)

	r.Route("/api/login", func(r chi.Router) {
		r.Use(middleware.RateLimit(rateLimitLogin, rateLimitLogin))
		r.Use(middleware.MaxBodySize(maxBodyBytes))
		r.Use(sameOrigin)
		r.Post("/", s.authHandler.LoginHandler)
	})

	r.With(sameOrigin).Post("/api/logout", s.authHandler.LogoutHandler)

	if s.googleHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RateLimit(rateLimitLogin, rateLimitLogin))
			r.Get("/auth/google/start", s.googleHandler.Start)
			r.Get("/auth/google/callback", s.googleHandler.Callback)
		})
	}

	r.Group(func(r chi.Router) {
		r.Use(s.authenticator.RequireAuth)
		r.Get("/api/me", s.authHandler.MeHandler)
		r.Get("/api/messages", s.messageHandler.ListMessages)
		r.Get("/api/messages/{id}", s.messageHandler.GetMessage)
		r.Get("/api/messages/{id}/raw", s.messageHandler.GetRawMIME)
		r.Get("/api/messages/{id}/attachments/{idx}", s.attachmentHandler.GetAttachment)

		r.Route("/api/admin", func(r chi.Router) {
			r.Use(auth.RequireAdmin())
			r.Use(sameOrigin)
			r.Use(middleware.MaxBodySize(maxAdminBody))
			r.Get("/users", s.adminHandler.ListUsers)
			r.Post("/users", s.adminHandler.CreateUser)
			r.Patch("/users/{id}", s.adminHandler.UpdateUser)
			r.Delete("/users/{id}", s.adminHandler.DeleteUser)
			r.Post("/users/{id}/grants", s.adminHandler.AddGrant)
			r.Delete("/users/{id}/grants/{grantId}", s.adminHandler.DeleteGrant)
			r.Get("/mailboxes", s.adminHandler.ListMailboxes)
			r.Get("/audit", s.adminHandler.ListAudit)
		})
	})

	// Note: the public /share/{token} preview routes were intentionally
	// retired in favor of the Cloudflare Worker, which now hosts
	// /email/:id?mode=text|html as the canonical preview surface. The
	// Go share handler, db.CreateShareToken / FindMessageIDByToken helpers,
	// share_tokens table, and Telegram delivery table have all been
	// removed (see migration 003_drop_share_and_delivery_tables). Any
	// previously-issued /share/{token} URLs will return 404 from the
	// SPA catch-all below; new previews are minted by the Worker.
	r.Get("/", spaHandler)
	r.Get("/login", spaHandler)
	r.Get("/messages/{id}", spaHandler)
	r.Handle("/static/*", staticHandler)

	return s
}

// auditorOrNil avoids handing auth a non-nil interface holding a nil logger.
func auditorOrNil(l *audit.Logger) auth.Auditor {
	if l == nil {
		return nil
	}
	return l
}

// StartBackground runs periodic jobs (audit retention) until ctx ends.
func (s *Server) StartBackground(ctx context.Context) {
	s.auditLog.StartPruner(ctx, s.config.AuditRetention())
}

// Start serves HTTP requests on addr. TLS termination is handled by the reverse proxy.
func (s *Server) Start(addr string) error {
	s.http = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}
	return s.http.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

// Handler exposes the router for tests and graceful server setup.
func (s *Server) Handler() http.Handler {
	return s.router
}
