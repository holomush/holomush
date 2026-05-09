// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"
	"log/slog"

	"github.com/holomush/holomush/internal/lifecycle"
)

// AdminSocketSubsystemConfig configures the admin-socket subsystem.
type AdminSocketSubsystemConfig struct {
	SocketPath string
	LockPath   string
	// Version is the binary version string returned by the Status RPC.
	// Pass the package-level version var from cmd/holomush/ (ldflag-set).
	Version string
}

// AdminSocketSubsystem manages the admin UNIX domain socket lifecycle.
type AdminSocketSubsystem struct {
	cfg    AdminSocketSubsystemConfig
	server *Server
	errCh  <-chan error
}

// NewAdminSocketSubsystem creates an AdminSocketSubsystem. No live resources
// are allocated until Start is called.
func NewAdminSocketSubsystem(cfg AdminSocketSubsystemConfig) *AdminSocketSubsystem {
	return &AdminSocketSubsystem{cfg: cfg}
}

// ID returns SubsystemAdminSocket.
func (s *AdminSocketSubsystem) ID() lifecycle.SubsystemID {
	return lifecycle.SubsystemAdminSocket
}

// DependsOn returns nil — the substrate has no subsystem dependencies.
func (s *AdminSocketSubsystem) DependsOn() []lifecycle.SubsystemID {
	return nil
}

// Start creates the server, acquires admin.lock, binds admin.sock, and
// begins serving. If SocketPath is empty (XDG runtime dir unavailable at
// startup), Start is a no-op: the admin socket is disabled but the server
// continues serving normally.
// codecov:ignore — tested by integration and E2E tests
func (s *AdminSocketSubsystem) Start(_ context.Context) error {
	if s.cfg.SocketPath == "" {
		slog.Warn("admin socket subsystem: disabled — no socket path configured; break-glass unavailable")
		return nil
	}
	srv := NewServer(Config{
		SocketPath: s.cfg.SocketPath,
		LockPath:   s.cfg.LockPath,
		Version:    s.cfg.Version,
	})

	errCh, err := srv.Start()
	if err != nil {
		return err
	}

	s.server = srv
	s.errCh = errCh

	go func() {
		if serveErr, ok := <-s.errCh; ok {
			// Admin-socket failure is intentionally non-fatal: the socket is the
			// operator break-glass path (sub-epics D/E/F) and an error here does
			// not affect player connections or game state. Log loudly but let the
			// server continue serving.
			slog.Error("admin socket subsystem: server error — admin break-glass unavailable", "error", serveErr)
		}
	}()

	return nil
}

// Stop shuts down the admin socket server and releases admin.lock.
func (s *AdminSocketSubsystem) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Stop(ctx)
}
