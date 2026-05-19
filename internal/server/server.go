package server

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"lite-mail/internal/api"
	"lite-mail/internal/auth"
	"lite-mail/internal/config"
	"lite-mail/internal/ingest"
	"lite-mail/internal/middleware"
	"lite-mail/internal/storage"
)

const (
	rateLimitLogin  = 10
	rateLimitIngest = 60
	maxBodyBytes    = 1 << 20 // 1MB for general API endpoints
)

// Server owns the HTTP router and shared application dependencies.
type Server struct {
	config *config.Config
	db     *sql.DB
	store  *storage.Storage
	router *chi.Mux
	http   *http.Server

	sessionStore auth.SessionStore
	authHandler  *auth.AuthHandler

	messageHandler    *api.MessageHandler
	attachmentHandler *api.AttachmentHandler
	ingestHandler     *ingest.IngestHandler
}

// New wires middleware, routes, and dependencies into a Server.
func New(cfg *config.Config, db *sql.DB) *Server {
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

	r.Route("/api/login", func(r chi.Router) {
		r.Use(middleware.RateLimit(rateLimitLogin, rateLimitLogin))
		r.Use(middleware.MaxBodySize(maxBodyBytes))
		r.Post("/", s.authHandler.LoginHandler)
	})

	r.Post("/api/logout", s.authHandler.LogoutHandler)
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(s.sessionStore, s.config))
		r.Get("/api/messages", s.messageHandler.ListMessages)
		r.Get("/api/messages/{id}", s.messageHandler.GetMessage)
		r.Get("/api/messages/{id}/raw", s.messageHandler.GetRawMIME)
		r.Get("/api/messages/{id}/attachments/{idx}", s.attachmentHandler.GetAttachment)
	})
	r.Get("/", spaHandler)
	r.Get("/login", spaHandler)
	r.Get("/messages/{id}", spaHandler)
	r.Handle("/static/*", staticHandler)

	return s
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
