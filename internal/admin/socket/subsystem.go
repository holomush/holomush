// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"
	"log/slog"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/pkg/errutil"
)

// AdminSocketSubsystemConfig configures the admin-socket subsystem.
type AdminSocketSubsystemConfig struct {
	SocketPath string
	LockPath   string
	// Version is the binary version string returned by the Status RPC.
	// Pass the package-level version var from cmd/holomush/ (ldflag-set).
	Version string
	// Optional RPC handlers. When non-nil, forwarded into Config so the
	// ConnectRPC composite handler dispatches to real implementations.
	// When nil, each RPC returns connect.CodeUnimplemented (backward-compat).
	AuthenticateHandler AuthenticateHandler
	ApproveHandler      ApproveHandler
	ResetTOTPHandler    ResetTOTPHandler
	// RekeyHandler dispatches the Rekey / RekeyResume / RekeyAbort /
	// RekeyStatus / RekeyList RPCs. Sub-epic E T44 production wiring
	// (holomush-jxo8.7.44).
	RekeyHandler RekeyRPCHandler
	// ReadStreamHandler dispatches the AdminReadStream RPC (holomush-jxo8.8.36).
	// When nil, AdminReadStream returns connect.CodeUnimplemented until R.15 wires it.
	ReadStreamHandler ReadStreamRPCHandler
	// Shutdown is invoked when the admin socket server returns a post-startup
	// error on its errCh (e.g., UDS accept loop dies, corrupted listener
	// state). Production wires this to the parent context's cancel func so
	// admin-socket failure triggers graceful shutdown of the entire process,
	// matching obsServer/controlGRPCServer at cmd/holomush/core.go:298,914.
	// Per holomush-jxo8.9: silent log-only was a P1 gap (operators lose
	// break-glass with no downstream signal). When nil, the monitor logs and
	// returns; this is the test/dev default.
	Shutdown func(error)
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
		SocketPath:          s.cfg.SocketPath,
		LockPath:            s.cfg.LockPath,
		Version:             s.cfg.Version,
		AuthenticateHandler: s.cfg.AuthenticateHandler,
		ApproveHandler:      s.cfg.ApproveHandler,
		ResetTOTPHandler:    s.cfg.ResetTOTPHandler,
		RekeyHandler:        s.cfg.RekeyHandler,
		ReadStreamHandler:   s.cfg.ReadStreamHandler,
	})

	errCh, err := srv.Start()
	if err != nil {
		return err
	}

	s.server = srv
	s.errCh = errCh

	go runErrMonitor(s.errCh, s.cfg.Shutdown)

	return nil
}

// runErrMonitor watches errCh for post-startup server errors. On a non-nil
// error delivery it logs at Error level and invokes shutdown (if non-nil) so
// the parent process can cancel its context and exit gracefully. On normal
// channel close (the Stop path) it returns without invoking shutdown.
//
// Mirrors cmd/holomush/core.go:1117 monitorServerErrors. Extracted as a
// package-private function so the wiring is unit-testable without spinning
// a real UDS listener.
func runErrMonitor(errCh <-chan error, shutdown func(error)) {
	serveErr, ok := <-errCh
	if !ok {
		// Channel closed during normal Stop — not a fatal event.
		return
	}
	if serveErr == nil {
		// Defensive: a nil error delivery is not a fatal event.
		return
	}
	errutil.LogError(
		slog.Default(),
		"admin socket subsystem: server error — triggering graceful shutdown",
		serveErr,
	)
	if shutdown != nil {
		shutdown(serveErr)
	}
}

// Stop shuts down the admin socket server and releases admin.lock.
func (s *AdminSocketSubsystem) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Stop(ctx)
}
