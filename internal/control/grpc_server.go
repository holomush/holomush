// Package control provides gRPC control interfaces for process management.
package control

import (
	"context"
	cryptotls "crypto/tls"
	"crypto/x509"
	"encoding/pem"
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

// ShutdownFunc is called when a shutdown is requested via the control interface.
type ShutdownFunc func()

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
// Returns an error if component is empty.
func NewGRPCServer(component string, shutdownFunc ShutdownFunc) (*GRPCServer, error) {
	if component == "" {
		return nil, fmt.Errorf("component name cannot be empty")
	}
	s := &GRPCServer{
		component:    component,
		startTime:    time.Now(),
		shutdownFunc: shutdownFunc,
	}
	s.running.Store(true)
	return s, nil
}

// Start begins listening on the specified address with mTLS.
// It returns an error channel that will receive the server's exit error (or nil on graceful stop).
// The channel will receive exactly one value when the server stops.
// Returns an error if the server is already running (double-start prevention).
func (s *GRPCServer) Start(addr string, tlsConfig *cryptotls.Config) (<-chan error, error) {
	// Prevent double-start which would leak the first listener
	if s.listener != nil {
		return nil, fmt.Errorf("server is already running")
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = listener

	creds := credentials.NewTLS(tlsConfig)
	s.grpcServer = grpc.NewServer(grpc.Creds(creds))
	controlv1.RegisterControlServer(s.grpcServer, s)

	errCh := make(chan error, 1)
	go func() {
		err := s.grpcServer.Serve(listener)
		if err != nil {
			slog.Error("control gRPC server error",
				"component", s.component,
				"error", err,
			)
		}
		errCh <- err
	}()

	return errCh, nil
}

// Stop gracefully shuts down the control gRPC server.
// The running state is set to false only after GracefulStop completes.
func (s *GRPCServer) Stop(_ context.Context) error {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}

	// Set running to false only after GracefulStop completes to avoid race condition
	s.running.Store(false)

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

// LoadControlClientTLS loads TLS config for the control gRPC client with mTLS.
// clientName identifies the client certificate to use (e.g., "core" or "gateway").
// gameID is used for ServerName verification against the server's SAN (holomush-<gameID>).
func LoadControlClientTLS(certsDir string, clientName string, gameID string) (*cryptotls.Config, error) {
	certPath := filepath.Clean(filepath.Join(certsDir, clientName+".crt"))
	keyPath := filepath.Clean(filepath.Join(certsDir, clientName+".key"))
	caPath := filepath.Clean(filepath.Join(certsDir, "root-ca.crt"))

	// Load client certificate
	cert, err := cryptotls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Load CA for server verification
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
		RootCAs:      caPool,
		MinVersion:   cryptotls.VersionTLS13,
		ServerName:   "holomush-" + gameID,
	}, nil
}

// ExtractGameIDFromCA extracts the game_id from a CA certificate's Common Name.
// The CA's CN format is "HoloMUSH CA {game_id}".
func ExtractGameIDFromCA(certsDir string) (string, error) {
	caPath := filepath.Clean(filepath.Join(certsDir, "root-ca.crt"))

	caCertPEM, err := os.ReadFile(caPath)
	if err != nil {
		return "", fmt.Errorf("failed to read CA certificate: %w", err)
	}

	block, _ := pem.Decode(caCertPEM)
	if block == nil {
		return "", fmt.Errorf("failed to decode CA certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Extract game_id from CN "HoloMUSH CA {game_id}"
	const prefix = "HoloMUSH CA "
	cn := cert.Subject.CommonName
	if len(cn) <= len(prefix) || cn[:len(prefix)] != prefix {
		return "", fmt.Errorf("CA CN %q does not have expected prefix %q", cn, prefix)
	}

	return cn[len(prefix):], nil
}
