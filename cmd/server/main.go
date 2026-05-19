package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lite-mail/internal/config"
	"lite-mail/internal/db"
	"lite-mail/internal/server"
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

	app := server.New(cfg, database)
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
