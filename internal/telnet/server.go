// Package telnet provides the telnet protocol adapter.
package telnet

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
)

// Server is a telnet server.
type Server struct {
	addr     string
	listener net.Listener
	mu       sync.RWMutex
}

// NewServer creates a new telnet server.
func NewServer(addr string) *Server {
	return &Server{addr: addr}
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
		listener.Close()
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
		go s.handleConnection(ctx, conn)
	}
}

func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	slog.Info("New connection", "remote", conn.RemoteAddr())

	// Send welcome
	fmt.Fprintln(conn, "Welcome to HoloMUSH!")
	fmt.Fprintln(conn, "Use: connect <username> <password>")

	reader := bufio.NewReader(conn)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			slog.Info("Connection closed", "remote", conn.RemoteAddr())
			return
		}

		// Echo for now (will be replaced with command handling)
		fmt.Fprintf(conn, "You said: %s", line)
	}
}
