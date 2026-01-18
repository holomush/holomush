// Package control provides HTTP and gRPC control interfaces for process management.
package control

import (
	"context"
	cryptotls "crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	controlv1 "github.com/holomush/holomush/internal/proto/holomush/control/v1"
)

// GRPCServer runs a gRPC control server with mTLS.
type GRPCServer struct {
	controlv1.UnimplementedControlServer

	component    string
	startTime    time.Time
	listener     net.Listener
	grpcServer   *grpc.Server
	shutdownFunc ShutdownFunc
	running      atomic.Bool
}

// NewGRPCServer creates a new gRPC control server.
// component is the name of the process (e.g., "core" or "gateway").
func NewGRPCServer(component string, shutdownFunc ShutdownFunc) *GRPCServer {
	s := &GRPCServer{
		component:    component,
		startTime:    time.Now(),
		shutdownFunc: shutdownFunc,
	}
	s.running.Store(true)
	return s
}

// Start begins listening on the specified address with mTLS.
func (s *GRPCServer) Start(addr string, tlsConfig *cryptotls.Config) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = listener

	creds := credentials.NewTLS(tlsConfig)
	s.grpcServer = grpc.NewServer(grpc.Creds(creds))
	controlv1.RegisterControlServer(s.grpcServer, s)

	go func() {
		if err := s.grpcServer.Serve(listener); err != nil {
			slog.Error("control gRPC server error",
				"component", s.component,
				"error", err,
			)
		}
	}()

	return nil
}

// Stop gracefully shuts down the control gRPC server.
func (s *GRPCServer) Stop(_ context.Context) error {
	s.running.Store(false)

	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}

	return nil
}

// Shutdown implements the Control.Shutdown RPC.
func (s *GRPCServer) Shutdown(_ context.Context, req *controlv1.ShutdownRequest) (*controlv1.ShutdownResponse, error) {
	slog.Info("shutdown requested via gRPC",
		"component", s.component,
		"graceful", req.Graceful,
	)

	// Trigger shutdown asynchronously
	if s.shutdownFunc != nil {
		go s.shutdownFunc()
	}

	return &controlv1.ShutdownResponse{
		Message: "shutdown initiated",
	}, nil
}

// Status implements the Control.Status RPC.
func (s *GRPCServer) Status(_ context.Context, _ *controlv1.StatusRequest) (*controlv1.StatusResponse, error) {
	// PIDs are always positive and fit in int32 on all supported platforms
	//nolint:gosec // G115: PID values never exceed int32 range
	pid := int32(os.Getpid())
	return &controlv1.StatusResponse{
		Running:       s.running.Load(),
		Pid:           pid,
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
		Component:     s.component,
	}, nil
}

// LoadControlServerTLS loads TLS config for the control gRPC server with mTLS.
// Uses the same certificates as the main server (identified by serverName).
func LoadControlServerTLS(certsDir string, serverName string) (*cryptotls.Config, error) {
	certPath := filepath.Clean(filepath.Join(certsDir, serverName+".crt"))
	keyPath := filepath.Clean(filepath.Join(certsDir, serverName+".key"))
	caPath := filepath.Clean(filepath.Join(certsDir, "root-ca.crt"))

	// Load server certificate
	cert, err := cryptotls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	// Load CA for client verification
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to add CA certificate to pool")
	}

	return &cryptotls.Config{
		Certificates: []cryptotls.Certificate{cert},
		ClientCAs:    caPool,
		ClientAuth:   cryptotls.RequireAndVerifyClientCert,
		MinVersion:   cryptotls.VersionTLS13,
	}, nil
}
