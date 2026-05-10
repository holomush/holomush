// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
)

// ErrAdminSocketAlreadyHeld is returned by Start when another holomush
// process is already holding admin.lock.
var ErrAdminSocketAlreadyHeld = errors.New("admin socket already held by another process")

// Config holds configuration for the admin socket server.
type Config struct {
	SocketPath string
	LockPath   string
	Version    string
	// Optional: when non-nil, the corresponding RPC dispatches to this
	// handler. When nil, the RPC returns connect.CodeUnimplemented.
	// Production wiring sets all three; tests may leave any/all nil.
	AuthenticateHandler AuthenticateHandler
	ApproveHandler      ApproveHandler
	ResetTOTPHandler    ResetTOTPHandler
}

// Server is the admin-socket ConnectRPC server. Binds exclusively to a
// UNIX domain socket — TCP is structurally impossible.
type Server struct {
	cfg        Config
	httpServer *http.Server
	listener   net.Listener
	lockFile   *os.File
	errCh      chan error
}

// NewServer creates a Server. No live resources are allocated until Start.
func NewServer(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// Start acquires admin.lock, removes stale admin.sock, binds the UDS, and
// begins serving. Returns a channel that carries at most one server error.
func (s *Server) Start() (<-chan error, error) {
	s.errCh = make(chan error, 1)

	lockFile, err := acquireLock(s.cfg.LockPath)
	if err != nil {
		return nil, err
	}
	s.lockFile = lockFile

	if _, statErr := os.Stat(s.cfg.SocketPath); statErr == nil {
		slog.Warn("admin: removing stale admin.sock", "path", s.cfg.SocketPath)
		if rmErr := os.Remove(s.cfg.SocketPath); rmErr != nil {
			_ = lockFile.Close() //nolint:errcheck // best-effort cleanup before returning error
			return nil, oops.Code("ADMIN_SOCKET_STALE_REMOVE_FAILED").
				With("path", s.cfg.SocketPath).Wrap(rmErr)
		}
	}

	ln, listenErr := net.Listen("unix", s.cfg.SocketPath)
	if listenErr != nil {
		_ = lockFile.Close() //nolint:errcheck // best-effort cleanup before returning error
		return nil, oops.Code("ADMIN_SOCKET_LISTEN_FAILED").
			With("path", s.cfg.SocketPath).Wrap(listenErr)
	}
	// Set explicit 0600 permission via os.Chmod rather than a process-wide
	// umask mutation. The parent directory (XDG runtime dir, mode 0700) is the
	// primary access gate; os.Chmod adds the supplementary socket-level restriction.
	if chmodErr := os.Chmod(s.cfg.SocketPath, 0o600); chmodErr != nil {
		_ = ln.Close()       //nolint:errcheck // best-effort cleanup
		_ = lockFile.Close() //nolint:errcheck // best-effort cleanup
		return nil, oops.Code("ADMIN_SOCKET_CHMOD_FAILED").
			With("path", s.cfg.SocketPath).Wrap(chmodErr)
	}
	s.listener = ln

	mux := s.buildMux()
	s.httpServer = &http.Server{
		Handler:           PeerCredMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ConnContext:       StoreUnixConn,
	}

	go func(errCh chan error, srv *http.Server) {
		defer close(errCh)
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			slog.Error("admin: HTTP server error", "error", serveErr)
			errCh <- serveErr
		}
	}(s.errCh, s.httpServer)

	slog.Info("admin socket server started", "path", s.cfg.SocketPath)
	return s.errCh, nil
}

// Stop shuts down the HTTP server, removes admin.sock, and releases admin.lock.
// admin.lock itself is NOT removed — it is a permanent fixture. Cleanup (socket
// removal and lock release) always runs, even if Shutdown returns an error.
func (s *Server) Stop(ctx context.Context) error {
	var retErr error
	if s.httpServer != nil {
		if shutdownErr := s.httpServer.Shutdown(ctx); shutdownErr != nil {
			retErr = oops.Code("ADMIN_SOCKET_SHUTDOWN_FAILED").Wrap(shutdownErr)
		}
		s.httpServer = nil
	}
	if rmErr := os.Remove(s.cfg.SocketPath); rmErr != nil && !os.IsNotExist(rmErr) {
		slog.Warn("admin: failed to remove admin.sock", "path", s.cfg.SocketPath, "error", rmErr)
	}
	if s.lockFile != nil {
		_ = s.lockFile.Close() //nolint:errcheck // best-effort release; flock is dropped when fd closes
		s.lockFile = nil
	}
	s.listener = nil
	return retErr
}

// acquireLock opens/creates the lock file and acquires an exclusive
// non-blocking flock. The caller MUST hold the returned *os.File open.
func acquireLock(lockPath string) (*os.File, error) {
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, oops.Code("ADMIN_LOCK_OPEN_FAILED").With("path", lockPath).Wrap(err)
	}
	fd := int(f.Fd()) //nolint:gosec // G115: fd fits in int on all supported platforms (file descriptor is always small)
	if flockErr := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); flockErr != nil {
		_ = f.Close() //nolint:errcheck // best-effort cleanup before returning error
		if errors.Is(flockErr, syscall.EWOULDBLOCK) {
			return nil, ErrAdminSocketAlreadyHeld
		}
		return nil, oops.Code("ADMIN_LOCK_FLOCK_FAILED").With("path", lockPath).Wrap(flockErr)
	}
	return f, nil
}

// buildMux constructs the ConnectRPC handler mux using a compositeHandler
// that routes each RPC to the registered handler, or returns Unimplemented
// when the handler is nil.
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	h := &compositeHandler{
		version:             s.cfg.Version,
		authenticateHandler: s.cfg.AuthenticateHandler,
		approveHandler:      s.cfg.ApproveHandler,
		resetTOTPHandler:    s.cfg.ResetTOTPHandler,
	}
	path, handler := adminv1connect.NewAdminServiceHandler(h)
	mux.Handle(path, handler)
	return mux
}
