package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lite-mail/internal/config"
	"lite-mail/internal/db"
	"lite-mail/internal/ingest"
	"lite-mail/internal/server"
	"lite-mail/internal/storage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	migrations, err := db.LoadMigrations(db.MigrationsFS)
	if err != nil {
		logger.Error("failed to load migrations", "error", err)
		os.Exit(1)
	}
	if err := db.RunMigrations(database, migrations); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 && os.Args[1] == "backfill-envelope" {
		os.Exit(runBackfillEnvelope(cfg, database, os.Args[2:]))
	}

	app := server.New(cfg, database)
	bgCtx, stopBackground := context.WithCancel(context.Background())
	defer stopBackground()
	app.StartBackground(bgCtx)
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("starting server", "addr", cfg.ServerAddr)
		if err := app.Start(cfg.ServerAddr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
		close(serverErrors)
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-signals:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-serverErrors:
		if err != nil {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	if err := <-serverErrors; err != nil {
		logger.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped")
}

// runBackfillEnvelope records the envelope recipient of messages stored before
// it was tracked: lite-mail backfill-envelope [--dry-run]
func runBackfillEnvelope(cfg *config.Config, database *sql.DB, args []string) int {
	dryRun := false
	for _, arg := range args {
		switch arg {
		case "--dry-run", "-dry-run", "-n":
			dryRun = true
		default:
			slog.Error("usage: lite-mail backfill-envelope [--dry-run]", "unknown_arg", arg)
			return 2
		}
	}
	store, err := storage.NewStorage(cfg.DataDir)
	if err != nil {
		slog.Error("open storage", "error", err)
		return 1
	}
	res, err := ingest.BackfillEnvelopeRecipients(context.Background(), database, store, dryRun)
	if res != nil {
		slog.Info("envelope backfill",
			"dry_run", dryRun,
			"scanned", res.Scanned,
			"added", res.Added,
			"no_envelope_ids", res.NoEnvelope,
			"read_error_ids", res.ReadErrors,
		)
	}
	if err != nil {
		slog.Error("envelope backfill failed", "error", err)
		return 1
	}
	return 0
}
