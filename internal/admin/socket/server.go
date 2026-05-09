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
	return &Server{
		cfg:   cfg,
		errCh: make(chan error, 1),
	}
}

// Start acquires admin.lock, removes stale admin.sock, binds the UDS, and
// begins serving. Returns a channel that carries at most one server error.
func (s *Server) Start() (<-chan error, error) {
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

	oldUmask := syscall.Umask(0o177)
	ln, listenErr := net.Listen("unix", s.cfg.SocketPath)
	syscall.Umask(oldUmask)
	if listenErr != nil {
		_ = lockFile.Close() //nolint:errcheck // best-effort cleanup before returning error
		return nil, oops.Code("ADMIN_SOCKET_LISTEN_FAILED").
			With("path", s.cfg.SocketPath).Wrap(listenErr)
	}
	s.listener = ln

	mux := s.buildMux()
	s.httpServer = &http.Server{
		Handler:           PeerCredMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ConnContext:       StoreUnixConn,
	}

	go func() {
		defer close(s.errCh)
		if serveErr := s.httpServer.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			slog.Error("admin: HTTP server error", "error", serveErr)
			s.errCh <- serveErr
		}
	}()

	slog.Info("admin socket server started", "path", s.cfg.SocketPath)
	return s.errCh, nil
}

// Stop shuts down the HTTP server, removes admin.sock, and releases admin.lock.
// admin.lock itself is NOT removed — it is a permanent fixture.
func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer != nil {
		if shutdownErr := s.httpServer.Shutdown(ctx); shutdownErr != nil {
			return oops.Code("ADMIN_SOCKET_SHUTDOWN_FAILED").Wrap(shutdownErr)
		}
	}
	if rmErr := os.Remove(s.cfg.SocketPath); rmErr != nil && !os.IsNotExist(rmErr) {
		slog.Warn("admin: failed to remove admin.sock", "path", s.cfg.SocketPath, "error", rmErr)
	}
	if s.lockFile != nil {
		_ = s.lockFile.Close() //nolint:errcheck // best-effort release; flock is dropped when fd closes
		s.lockFile = nil
	}
	return nil
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

// buildMux constructs the ConnectRPC handler mux. Handlers for sub-epics
// D, E, and F will be registered here as they are implemented.
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	path, handler := adminv1connect.NewAdminServiceHandler(&statusHandler{version: s.cfg.Version})
	mux.Handle(path, handler)
	return mux
}
