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
	// HandlersProvider resolves all five RPC handlers together at Start,
	// after the disabled-mode (SocketPath == "") early return — a provider,
	// not live values, since the handlers depend on wiring outputs
	// that only exist once Auth/ABAC/EventBus have started (07-09 item 9).
	// The five concrete *Handler fields above stay as a dual path for tests
	// and backward-compat; the provider wins when non-nil.
	HandlersProvider func() (Handlers, error)
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

// Handlers bundles the five admin RPC handlers HandlersProvider resolves
// together at Start (07-09 item 9). All five field types are already
// package-owned.
type Handlers struct {
	Authenticate AuthenticateHandler
	Approve      ApproveHandler
	ResetTOTP    ResetTOTPHandler
	Rekey        RekeyRPCHandler
	ReadStream   ReadStreamRPCHandler
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

// DependsOn returns [Database, Auth, ABAC, EventBus, CryptoChainVerifier].
// The first four are THE RULE's wiring consumer superset (this
// subsystem holds a HandlersProvider backed by the memoized wiring
// builder — whichever consumer resolves the provider first builds it, so
// every consumer must declare its full dependency set). CryptoChainVerifier
// is the T-07-51 re-scope (07-09 item 8): admin.sock binds its listener
// only after the INV-CRYPTO-102 chain walk has run.
func (s *AdminSocketSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemAuth,
		lifecycle.SubsystemABAC,
		lifecycle.SubsystemEventBus,
		lifecycle.SubsystemCryptoChainVerifier,
	}
}

// Start creates the server, acquires admin.lock, binds admin.sock, and
// begins serving. If SocketPath is empty (XDG runtime dir unavailable at
// startup), Start is a no-op: the admin socket is disabled but the server
// continues serving normally — the disabled-mode early return runs BEFORE
// HandlersProvider is ever resolved, so a disabled admin socket never
// triggers the wiring build.
// codecov:ignore — tested by integration and E2E tests
func (s *AdminSocketSubsystem) Start(ctx context.Context) error {
	if s.cfg.SocketPath == "" {
		slog.WarnContext(ctx, "admin socket subsystem: disabled — no socket path configured; break-glass unavailable")
		return nil
	}

	handlers := Handlers{
		Authenticate: s.cfg.AuthenticateHandler,
		Approve:      s.cfg.ApproveHandler,
		ResetTOTP:    s.cfg.ResetTOTPHandler,
		Rekey:        s.cfg.RekeyHandler,
		ReadStream:   s.cfg.ReadStreamHandler,
	}
	if s.cfg.HandlersProvider != nil {
		resolved, err := s.cfg.HandlersProvider()
		if err != nil {
			return err
		}
		handlers = resolved
	}

	srv := NewServer(Config{
		SocketPath:          s.cfg.SocketPath,
		LockPath:            s.cfg.LockPath,
		Version:             s.cfg.Version,
		AuthenticateHandler: handlers.Authenticate,
		ApproveHandler:      handlers.Approve,
		ResetTOTPHandler:    handlers.ResetTOTP,
		RekeyHandler:        handlers.Rekey,
		ReadStreamHandler:   handlers.ReadStream,
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
