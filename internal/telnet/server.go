// Package telnet provides the telnet protocol adapter.
package telnet

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/holomush/holomush/internal/core"
)

// Server is a telnet server.
type Server struct {
	addr     string
	listener net.Listener
	engine   *core.Engine
	sessions *core.SessionManager
	mu       sync.RWMutex
}

// NewServer creates a new telnet server.
func NewServer(addr string, engine *core.Engine, sessions *core.SessionManager) *Server {
	return &Server{
		addr:     addr,
		engine:   engine,
		sessions: sessions,
	}
}

// Addr returns the server's listen address.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Run starts the server and blocks until context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()

	slog.Info("Telnet server started", "addr", listener.Addr())

	go func() {
		<-ctx.Done()
		if err := listener.Close(); err != nil {
			slog.Debug("error closing listener", "error", err)
		}
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				slog.Error("Accept failed", "error", err)
				continue
			}
		}
		handler := NewConnectionHandler(conn, s.engine, s.sessions)
		go handler.Handle(ctx)
	}
}
