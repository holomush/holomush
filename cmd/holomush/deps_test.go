package main

import (
	"bytes"
	"context"
	cryptotls "crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/control"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/observability"
)

// mockEventStore implements EventStore for testing.
type mockEventStore struct {
	initGameIDFunc func(ctx context.Context) (string, error)
	closeFunc      func()
}

func (m *mockEventStore) Close() {
	if m.closeFunc != nil {
		m.closeFunc()
	}
}

func (m *mockEventStore) InitGameID(ctx context.Context) (string, error) {
	if m.initGameIDFunc != nil {
		return m.initGameIDFunc(ctx)
	}
	return "test-game-id", nil
}

// mockControlServer implements ControlServer for testing.
type mockControlServer struct {
	startFunc func(addr string, tlsConfig *cryptotls.Config) (<-chan error, error)
	stopFunc  func(ctx context.Context) error
}

func (m *mockControlServer) Start(addr string, tlsConfig *cryptotls.Config) (<-chan error, error) {
	if m.startFunc != nil {
		return m.startFunc(addr, tlsConfig)
	}
	ch := make(chan error, 1)
	return ch, nil
}

func (m *mockControlServer) Stop(ctx context.Context) error {
	if m.stopFunc != nil {
		return m.stopFunc(ctx)
	}
	return nil
}

// mockObservabilityServer implements ObservabilityServer for testing.
type mockObservabilityServer struct {
	startFunc func() (<-chan error, error)
	stopFunc  func(ctx context.Context) error
	addrFunc  func() string
}

func (m *mockObservabilityServer) Start() (<-chan error, error) {
	if m.startFunc != nil {
		return m.startFunc()
	}
	ch := make(chan error, 1)
	return ch, nil
}

func (m *mockObservabilityServer) Stop(ctx context.Context) error {
	if m.stopFunc != nil {
		return m.stopFunc(ctx)
	}
	return nil
}

func (m *mockObservabilityServer) Addr() string {
	if m.addrFunc != nil {
		return m.addrFunc()
	}
	return "127.0.0.1:9100"
}

// mockGRPCClient implements GRPCClient for testing.
type mockGRPCClient struct {
	closeFunc func() error
}

func (m *mockGRPCClient) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

// mockListener implements net.Listener for testing.
type mockListener struct {
	acceptFunc func() (net.Conn, error)
	closeFunc  func() error
	addrFunc   func() net.Addr
	mu         sync.Mutex
	closed     bool
}

func (m *mockListener) Accept() (net.Conn, error) {
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return nil, errors.New("listener closed")
	}
	if m.acceptFunc != nil {
		return m.acceptFunc()
	}
	// Block until closed
	for {
		m.mu.Lock()
		closed = m.closed
		m.mu.Unlock()
		if closed {
			return nil, errors.New("listener closed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (m *mockListener) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func (m *mockListener) Addr() net.Addr {
	if m.addrFunc != nil {
		return m.addrFunc()
	}
	return &mockAddr{addr: "127.0.0.1:4201"}
}

// mockAddr implements net.Addr for testing.
type mockAddr struct {
	addr string
}

func (m *mockAddr) Network() string {
	return "tcp"
}

func (m *mockAddr) String() string {
	return m.addr
}

// Helper function to create a mock command for testing.
func newMockCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	return cmd
}

// testTLSConfig returns a minimal TLS config for testing.
// Uses TLS 1.3 to satisfy gosec requirements.
func testTLSConfig() *cryptotls.Config {
	return &cryptotls.Config{
		MinVersion: cryptotls.VersionTLS13,
	}
}

// TestRunCoreWithDeps_HappyPath tests the core process with all mocked dependencies.
func TestRunCoreWithDeps_HappyPath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		metricsAddr: "", // Disable observability server for simplicity
		logFormat:   "json",
		gameID:      "test-game-id", // Skip InitGameID call
	}

	controlErrChan := make(chan error, 1)
	deps := &CoreDeps{
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
			return &mockControlServer{
				startFunc: func(_ string, _ *cryptotls.Config) (<-chan error, error) {
					return controlErrChan, nil
				},
			}, nil
		},
		ObservabilityServerFactory: func(_ string, _ observability.ReadinessChecker) ObservabilityServer {
			return &mockObservabilityServer{}
		},
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
	}

	cmd := newMockCmd()

	// Run in goroutine and cancel after a short delay
	errChan := make(chan error, 1)
	go func() {
		errChan <- runCoreWithDeps(ctx, cfg, cmd, deps)
	}()

	// Let it start, then cancel
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("runCoreWithDeps() returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runCoreWithDeps() did not return within timeout")
	}
}

// TestRunCoreWithDeps_ValidationError tests that validation errors are returned.
func TestRunCoreWithDeps_ValidationError(t *testing.T) {
	ctx := context.Background()
	cfg := &coreConfig{
		grpcAddr:    "", // Invalid - required
		controlAddr: "127.0.0.1:9001",
		logFormat:   "json",
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, nil)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "grpc-addr") {
		t.Errorf("expected error to mention grpc-addr, got: %v", err)
	}
}

// TestRunCoreWithDeps_DatabaseURLMissing tests missing DATABASE_URL error.
func TestRunCoreWithDeps_DatabaseURLMissing(t *testing.T) {
	ctx := context.Background()
	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		logFormat:   "json",
	}

	deps := &CoreDeps{
		DatabaseURLGetter: func() string {
			return "" // Empty DATABASE_URL
		},
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected DATABASE_URL error, got nil")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("expected error to mention DATABASE_URL, got: %v", err)
	}
}

// TestRunCoreWithDeps_EventStoreFactoryError tests event store creation failure.
func TestRunCoreWithDeps_EventStoreFactoryError(t *testing.T) {
	ctx := context.Background()
	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		logFormat:   "json",
	}

	deps := &CoreDeps{
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected event store error, got nil")
	}
	if !strings.Contains(err.Error(), "database") {
		t.Errorf("expected error to mention database, got: %v", err)
	}
}

// TestRunCoreWithDeps_InitGameIDError tests game ID initialization failure.
func TestRunCoreWithDeps_InitGameIDError(t *testing.T) {
	ctx := context.Background()
	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		logFormat:   "json",
		gameID:      "", // Will call InitGameID
	}

	deps := &CoreDeps{
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{
				initGameIDFunc: func(_ context.Context) (string, error) {
					return "", fmt.Errorf("failed to init game ID")
				},
			}, nil
		},
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected init game ID error, got nil")
	}
	if !strings.Contains(err.Error(), "game ID") {
		t.Errorf("expected error to mention game ID, got: %v", err)
	}
}

// TestRunCoreWithDeps_CertsDirError tests certificates directory error.
func TestRunCoreWithDeps_CertsDirError(t *testing.T) {
	ctx := context.Background()
	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		logFormat:   "json",
		gameID:      "test-game-id",
	}

	deps := &CoreDeps{
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		CertsDirGetter: func() (string, error) {
			return "", fmt.Errorf("failed to get certs directory")
		},
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected certs dir error, got nil")
	}
	if !strings.Contains(err.Error(), "certs directory") {
		t.Errorf("expected error to mention certs directory, got: %v", err)
	}
}

// TestRunCoreWithDeps_TLSCertError tests TLS certificate setup error.
func TestRunCoreWithDeps_TLSCertError(t *testing.T) {
	ctx := context.Background()
	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		logFormat:   "json",
		gameID:      "test-game-id",
	}

	deps := &CoreDeps{
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return nil, fmt.Errorf("failed to load TLS certificates")
		},
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected TLS error, got nil")
	}
	if !strings.Contains(err.Error(), "TLS") {
		t.Errorf("expected error to mention TLS, got: %v", err)
	}
}

// TestRunCoreWithDeps_ControlTLSLoadError tests control TLS loading error.
func TestRunCoreWithDeps_ControlTLSLoadError(t *testing.T) {
	ctx := context.Background()
	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		logFormat:   "json",
		gameID:      "test-game-id",
	}

	deps := &CoreDeps{
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
			return nil, fmt.Errorf("failed to load control TLS")
		},
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected control TLS error, got nil")
	}
	if !strings.Contains(err.Error(), "control TLS") {
		t.Errorf("expected error to mention control TLS, got: %v", err)
	}
}

// TestRunCoreWithDeps_ControlServerFactoryError tests control server creation error.
func TestRunCoreWithDeps_ControlServerFactoryError(t *testing.T) {
	ctx := context.Background()
	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		logFormat:   "json",
		gameID:      "test-game-id",
	}

	deps := &CoreDeps{
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
			return nil, fmt.Errorf("failed to create control server")
		},
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected control server error, got nil")
	}
	if !strings.Contains(err.Error(), "control gRPC server") {
		t.Errorf("expected error to mention control gRPC server, got: %v", err)
	}
}

// TestRunCoreWithDeps_ControlServerStartError tests control server start error.
func TestRunCoreWithDeps_ControlServerStartError(t *testing.T) {
	ctx := context.Background()
	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		logFormat:   "json",
		gameID:      "test-game-id",
	}

	deps := &CoreDeps{
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
			return &mockControlServer{
				startFunc: func(_ string, _ *cryptotls.Config) (<-chan error, error) {
					return nil, fmt.Errorf("address already in use")
				},
			}, nil
		},
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected control server start error, got nil")
	}
	if !strings.Contains(err.Error(), "start control gRPC server") {
		t.Errorf("expected error to mention start control gRPC server, got: %v", err)
	}
}

// TestRunCoreWithDeps_ObservabilityServerStartError tests observability server start error.
func TestRunCoreWithDeps_ObservabilityServerStartError(t *testing.T) {
	ctx := context.Background()
	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		metricsAddr: "127.0.0.1:9100",
		logFormat:   "json",
		gameID:      "test-game-id",
	}

	controlErrChan := make(chan error, 1)
	deps := &CoreDeps{
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
			return &mockControlServer{
				startFunc: func(_ string, _ *cryptotls.Config) (<-chan error, error) {
					return controlErrChan, nil
				},
			}, nil
		},
		ObservabilityServerFactory: func(_ string, _ observability.ReadinessChecker) ObservabilityServer {
			return &mockObservabilityServer{
				startFunc: func() (<-chan error, error) {
					return nil, fmt.Errorf("address already in use")
				},
			}
		},
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected observability server start error, got nil")
	}
	if !strings.Contains(err.Error(), "observability server") {
		t.Errorf("expected error to mention observability server, got: %v", err)
	}
}

// TestRunGatewayWithDeps_HappyPath tests the gateway process with all mocked dependencies.
func TestRunGatewayWithDeps_HappyPath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := &gatewayConfig{
		telnetAddr:  ":4201",
		coreAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9002",
		metricsAddr: "", // Disable observability server for simplicity
		logFormat:   "json",
	}

	controlErrChan := make(chan error, 1)
	ml := &mockListener{}

	deps := &GatewayDeps{
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		GameIDExtractor: func(_ string) (string, error) {
			return "test-game-id", nil
		},
		ClientTLSLoader: func(_, _, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		GRPCClientFactory: func(_ context.Context, _ holoGRPC.ClientConfig) (GRPCClient, error) {
			return &mockGRPCClient{}, nil
		},
		ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
			return &mockControlServer{
				startFunc: func(_ string, _ *cryptotls.Config) (<-chan error, error) {
					return controlErrChan, nil
				},
			}, nil
		},
		ObservabilityServerFactory: func(_ string, _ observability.ReadinessChecker) ObservabilityServer {
			return &mockObservabilityServer{}
		},
		ListenerFactory: func(_, _ string) (net.Listener, error) {
			return ml, nil
		},
	}

	cmd := newMockCmd()

	// Run in goroutine and cancel after a short delay
	errChan := make(chan error, 1)
	go func() {
		errChan <- runGatewayWithDeps(ctx, cfg, cmd, deps)
	}()

	// Let it start, then cancel
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("runGatewayWithDeps() returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runGatewayWithDeps() did not return within timeout")
	}
}

// TestRunGatewayWithDeps_ValidationError tests that validation errors are returned.
func TestRunGatewayWithDeps_ValidationError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		telnetAddr:  "", // Invalid - required
		coreAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9002",
		logFormat:   "json",
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, nil)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "telnet-addr") {
		t.Errorf("expected error to mention telnet-addr, got: %v", err)
	}
}

// TestRunGatewayWithDeps_CertsDirError tests certificates directory error.
func TestRunGatewayWithDeps_CertsDirError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		telnetAddr:  ":4201",
		coreAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9002",
		logFormat:   "json",
	}

	deps := &GatewayDeps{
		CertsDirGetter: func() (string, error) {
			return "", fmt.Errorf("failed to get certs directory")
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected certs dir error, got nil")
	}
	if !strings.Contains(err.Error(), "certs directory") {
		t.Errorf("expected error to mention certs directory, got: %v", err)
	}
}

// TestRunGatewayWithDeps_GameIDExtractorError tests game ID extraction error.
func TestRunGatewayWithDeps_GameIDExtractorError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		telnetAddr:  ":4201",
		coreAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9002",
		logFormat:   "json",
	}

	deps := &GatewayDeps{
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		GameIDExtractor: func(_ string) (string, error) {
			return "", fmt.Errorf("failed to extract game_id")
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected game ID error, got nil")
	}
	if !strings.Contains(err.Error(), "game_id") {
		t.Errorf("expected error to mention game_id, got: %v", err)
	}
}

// TestRunGatewayWithDeps_ClientTLSLoaderError tests client TLS loading error.
func TestRunGatewayWithDeps_ClientTLSLoaderError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		telnetAddr:  ":4201",
		coreAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9002",
		logFormat:   "json",
	}

	deps := &GatewayDeps{
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		GameIDExtractor: func(_ string) (string, error) {
			return "test-game-id", nil
		},
		ClientTLSLoader: func(_, _, _ string) (*cryptotls.Config, error) {
			return nil, fmt.Errorf("failed to load TLS certificates")
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected TLS error, got nil")
	}
	if !strings.Contains(err.Error(), "TLS") {
		t.Errorf("expected error to mention TLS, got: %v", err)
	}
}

// TestRunGatewayWithDeps_GRPCClientFactoryError tests gRPC client creation error.
func TestRunGatewayWithDeps_GRPCClientFactoryError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		telnetAddr:  ":4201",
		coreAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9002",
		logFormat:   "json",
	}

	deps := &GatewayDeps{
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		GameIDExtractor: func(_ string) (string, error) {
			return "test-game-id", nil
		},
		ClientTLSLoader: func(_, _, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		GRPCClientFactory: func(_ context.Context, _ holoGRPC.ClientConfig) (GRPCClient, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected gRPC client error, got nil")
	}
	if !strings.Contains(err.Error(), "gRPC client") {
		t.Errorf("expected error to mention gRPC client, got: %v", err)
	}
}

// TestRunGatewayWithDeps_ControlTLSLoadError tests control TLS loading error.
func TestRunGatewayWithDeps_ControlTLSLoadError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		telnetAddr:  ":4201",
		coreAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9002",
		logFormat:   "json",
	}

	deps := &GatewayDeps{
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		GameIDExtractor: func(_ string) (string, error) {
			return "test-game-id", nil
		},
		ClientTLSLoader: func(_, _, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		GRPCClientFactory: func(_ context.Context, _ holoGRPC.ClientConfig) (GRPCClient, error) {
			return &mockGRPCClient{}, nil
		},
		ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
			return nil, fmt.Errorf("failed to load control TLS")
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected control TLS error, got nil")
	}
	if !strings.Contains(err.Error(), "control TLS") {
		t.Errorf("expected error to mention control TLS, got: %v", err)
	}
}

// TestRunGatewayWithDeps_ControlServerFactoryError tests control server creation error.
func TestRunGatewayWithDeps_ControlServerFactoryError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		telnetAddr:  ":4201",
		coreAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9002",
		logFormat:   "json",
	}

	deps := &GatewayDeps{
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		GameIDExtractor: func(_ string) (string, error) {
			return "test-game-id", nil
		},
		ClientTLSLoader: func(_, _, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		GRPCClientFactory: func(_ context.Context, _ holoGRPC.ClientConfig) (GRPCClient, error) {
			return &mockGRPCClient{}, nil
		},
		ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
			return nil, fmt.Errorf("failed to create control server")
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected control server error, got nil")
	}
	if !strings.Contains(err.Error(), "control gRPC server") {
		t.Errorf("expected error to mention control gRPC server, got: %v", err)
	}
}

// TestRunGatewayWithDeps_ControlServerStartError tests control server start error.
func TestRunGatewayWithDeps_ControlServerStartError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		telnetAddr:  ":4201",
		coreAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9002",
		logFormat:   "json",
	}

	deps := &GatewayDeps{
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		GameIDExtractor: func(_ string) (string, error) {
			return "test-game-id", nil
		},
		ClientTLSLoader: func(_, _, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		GRPCClientFactory: func(_ context.Context, _ holoGRPC.ClientConfig) (GRPCClient, error) {
			return &mockGRPCClient{}, nil
		},
		ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
			return &mockControlServer{
				startFunc: func(_ string, _ *cryptotls.Config) (<-chan error, error) {
					return nil, fmt.Errorf("address already in use")
				},
			}, nil
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected control server start error, got nil")
	}
	if !strings.Contains(err.Error(), "start control gRPC server") {
		t.Errorf("expected error to mention start control gRPC server, got: %v", err)
	}
}

// TestRunGatewayWithDeps_ListenerFactoryError tests telnet listener creation error.
func TestRunGatewayWithDeps_ListenerFactoryError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		telnetAddr:  ":4201",
		coreAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9002",
		logFormat:   "json",
	}

	controlErrChan := make(chan error, 1)
	deps := &GatewayDeps{
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		GameIDExtractor: func(_ string) (string, error) {
			return "test-game-id", nil
		},
		ClientTLSLoader: func(_, _, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		GRPCClientFactory: func(_ context.Context, _ holoGRPC.ClientConfig) (GRPCClient, error) {
			return &mockGRPCClient{}, nil
		},
		ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
			return &mockControlServer{
				startFunc: func(_ string, _ *cryptotls.Config) (<-chan error, error) {
					return controlErrChan, nil
				},
			}, nil
		},
		ListenerFactory: func(_, _ string) (net.Listener, error) {
			return nil, fmt.Errorf("address already in use")
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected listener error, got nil")
	}
	if !strings.Contains(err.Error(), "listen on") {
		t.Errorf("expected error to mention listen on, got: %v", err)
	}
}

// TestRunGatewayWithDeps_ObservabilityServerStartError tests observability server start error.
func TestRunGatewayWithDeps_ObservabilityServerStartError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		telnetAddr:  ":4201",
		coreAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9002",
		metricsAddr: "127.0.0.1:9101",
		logFormat:   "json",
	}

	controlErrChan := make(chan error, 1)
	ml := &mockListener{}

	deps := &GatewayDeps{
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		GameIDExtractor: func(_ string) (string, error) {
			return "test-game-id", nil
		},
		ClientTLSLoader: func(_, _, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		GRPCClientFactory: func(_ context.Context, _ holoGRPC.ClientConfig) (GRPCClient, error) {
			return &mockGRPCClient{}, nil
		},
		ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
			return &mockControlServer{
				startFunc: func(_ string, _ *cryptotls.Config) (<-chan error, error) {
					return controlErrChan, nil
				},
			}, nil
		},
		ListenerFactory: func(_, _ string) (net.Listener, error) {
			return ml, nil
		},
		ObservabilityServerFactory: func(_ string, _ observability.ReadinessChecker) ObservabilityServer {
			return &mockObservabilityServer{
				startFunc: func() (<-chan error, error) {
					return nil, fmt.Errorf("address already in use")
				},
			}
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	if err == nil {
		t.Fatal("expected observability server start error, got nil")
	}
	if !strings.Contains(err.Error(), "observability server") {
		t.Errorf("expected error to mention observability server, got: %v", err)
	}
}

// TestRunCoreWithDeps_WithObservability tests the happy path with observability server enabled.
func TestRunCoreWithDeps_WithObservability(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := &coreConfig{
		grpcAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9001",
		metricsAddr: "127.0.0.1:9100",
		logFormat:   "json",
		gameID:      "test-game-id",
	}

	controlErrChan := make(chan error, 1)
	obsErrChan := make(chan error, 1)

	deps := &CoreDeps{
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
			return &mockControlServer{
				startFunc: func(_ string, _ *cryptotls.Config) (<-chan error, error) {
					return controlErrChan, nil
				},
			}, nil
		},
		ObservabilityServerFactory: func(_ string, _ observability.ReadinessChecker) ObservabilityServer {
			return &mockObservabilityServer{
				startFunc: func() (<-chan error, error) {
					return obsErrChan, nil
				},
			}
		},
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
	}

	cmd := newMockCmd()

	errChan := make(chan error, 1)
	go func() {
		errChan <- runCoreWithDeps(ctx, cfg, cmd, deps)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("runCoreWithDeps() returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runCoreWithDeps() did not return within timeout")
	}
}

// TestRunGatewayWithDeps_WithObservability tests the happy path with observability server enabled.
func TestRunGatewayWithDeps_WithObservability(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := &gatewayConfig{
		telnetAddr:  ":4201",
		coreAddr:    "localhost:9000",
		controlAddr: "127.0.0.1:9002",
		metricsAddr: "127.0.0.1:9101",
		logFormat:   "json",
	}

	controlErrChan := make(chan error, 1)
	obsErrChan := make(chan error, 1)
	ml := &mockListener{}

	deps := &GatewayDeps{
		CertsDirGetter: func() (string, error) {
			return "/tmp/certs", nil
		},
		GameIDExtractor: func(_ string) (string, error) {
			return "test-game-id", nil
		},
		ClientTLSLoader: func(_, _, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		GRPCClientFactory: func(_ context.Context, _ holoGRPC.ClientConfig) (GRPCClient, error) {
			return &mockGRPCClient{}, nil
		},
		ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
			return &mockControlServer{
				startFunc: func(_ string, _ *cryptotls.Config) (<-chan error, error) {
					return controlErrChan, nil
				},
			}, nil
		},
		ObservabilityServerFactory: func(_ string, _ observability.ReadinessChecker) ObservabilityServer {
			return &mockObservabilityServer{
				startFunc: func() (<-chan error, error) {
					return obsErrChan, nil
				},
			}
		},
		ListenerFactory: func(_, _ string) (net.Listener, error) {
			return ml, nil
		},
	}

	cmd := newMockCmd()

	errChan := make(chan error, 1)
	go func() {
		errChan <- runGatewayWithDeps(ctx, cfg, cmd, deps)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("runGatewayWithDeps() returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runGatewayWithDeps() did not return within timeout")
	}
}
