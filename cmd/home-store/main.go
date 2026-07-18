package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/codegamc/home-store/internal/api"
	"github.com/codegamc/home-store/internal/config"
	"github.com/codegamc/home-store/internal/server"
	"github.com/codegamc/home-store/internal/storage/fs"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config validation error: %v\n", err)
		os.Exit(1)
	}

	backend, err := fs.NewBackendWithOptions(cfg.DataDir, fs.Options{MaxObjectSize: cfg.MaxObjectSize, MaxStorageSize: cfg.MaxStorageSize, MultipartExpiry: cfg.MultipartExpiry})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing storage backend: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = backend.Close() }()

	handler := api.NewHandler(backend, api.AuthConfig{AccessKey: cfg.AccessKey, SecretKey: cfg.SecretKey, Region: cfg.Region})
	srv := server.New(cfg.Addr, handler, server.Options{
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	})

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		slog.Info("starting server", "addr", cfg.Addr, "data_dir", cfg.DataDir)
		var startErr error
		if cfg.TLSCertFile != "" {
			startErr = srv.StartTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			startErr = srv.Start()
		}
		if startErr != nil && startErr != http.ErrServerClosed {
			slog.Error("server error", "err", startErr)
			os.Exit(1)
		}
	}()

	<-sigChan
	slog.Info("shutdown signal received")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "err", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}
