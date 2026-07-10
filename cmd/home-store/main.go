package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/codegamc/home-store/internal/api"
	"github.com/codegamc/home-store/internal/auth"
	"github.com/codegamc/home-store/internal/config"
	"github.com/codegamc/home-store/internal/server"
	"github.com/codegamc/home-store/internal/storage/fs"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "home-store: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-version" {
			fmt.Printf("home-store %s (%s)\n", version, commit)
			return nil
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	configureLogging(cfg.LogLevel)

	backend, err := fs.NewBackend(cfg.DataDir, cfg.DBPath, cfg.Location)
	if err != nil {
		return fmt.Errorf("initialize storage backend: %w", err)
	}
	defer func() {
		if err := backend.Close(); err != nil {
			slog.Error("failed to close storage backend", "err", err)
		}
	}()

	apiHandler := api.NewHandler(backend)
	handler := auth.New(cfg.AccessKey, cfg.SecretKey, cfg.AuthDisabled, apiHandler)
	srv := server.New(cfg.Addr, handler)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigChan)
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- srv.Start() }()

	slog.Info("starting server", "addr", cfg.Addr, "data_dir", cfg.DataDir, "db_path", cfg.DBPath, "location", cfg.Location, "auth_disabled", cfg.AuthDisabled, "version", version)
	select {
	case signal := <-sigChan:
		slog.Info("shutdown signal received", "signal", signal.String())
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server failed: %w", err)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}
	if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server stopped unexpectedly: %w", err)
	}
	slog.Info("server stopped")
	return nil
}

func configureLogging(value string) {
	level := slog.LevelInfo
	switch strings.ToLower(value) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}
