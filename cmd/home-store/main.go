package main

import (
	"context"
	"fmt"
	"log/slog"
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

	backend, err := fs.NewBackend(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing storage backend: %v\n", err)
		os.Exit(1)
	}

	handler := api.NewHandler(backend)
	srv := server.New(cfg.Addr, handler)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		slog.Info("starting server", "addr", cfg.Addr, "data_dir", cfg.DataDir)
		if err := srv.Start(); err != nil {
			slog.Error("server error", "err", err)
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
