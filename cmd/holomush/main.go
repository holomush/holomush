// Package main is the entry point for the HoloMUSH server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/telnet"
)

// Version information set at build time.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Server error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	slog.Info("HoloMUSH starting",
		"version", version,
		"commit", commit,
		"date", date,
	)

	// Setup
	store := core.NewMemoryEventStore()
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	// Telnet server
	addr := os.Getenv("TELNET_ADDR")
	if addr == "" {
		addr = ":4201"
	}
	srv := telnet.NewServer(addr, engine, sessions, broadcaster)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("Shutting down...")
		cancel()
	}()

	// Run
	if err := srv.Run(ctx); err != nil {
		return fmt.Errorf("server run failed: %w", err)
	}

	slog.Info("Server stopped")
	return nil
}
