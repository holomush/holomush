// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"context"
	cryptotls "crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/control"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/observability"
	"github.com/holomush/holomush/pkg/errutil"
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

func (m *mockObservabilityServer) MustRegister(_ ...prometheus.Collector) {
	// No-op for testing - metrics registration is not needed in unit tests
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

// mockMigrator implements AutoMigrator for testing.
type mockMigrator struct {
	upFunc    func() error
	closeFunc func() error
}

func (m *mockMigrator) Up() error {
	if m.upFunc != nil {
		return m.upFunc()
	}
	return nil
}

func (m *mockMigrator) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

// noOpMigratorFactory returns an AutoMigrator that does nothing, for use in tests
// that don't care about migration behavior.
func noOpMigratorFactory(_ string) (AutoMigrator, error) {
	return &mockMigrator{}, nil
}

// disableAutoMigrate returns false, disabling auto-migration for tests.
func disableAutoMigrate() bool {
	return false
}

// noOpBootstrapper skips seed policy bootstrap in tests.
func noOpBootstrapper(_ context.Context, _ bool) error {
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
		CommonDeps: CommonDeps{
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
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		MigratorFactory:    noOpMigratorFactory,
		AutoMigrateGetter:  disableAutoMigrate,
		PolicyBootstrapper: noOpBootstrapper,
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
		require.NoError(t, err, "runCoreWithDeps() returned unexpected error")
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
	require.Error(t, err, "expected validation error")
	assert.Contains(t, err.Error(), "grpc-addr")
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
	require.Error(t, err, "expected DATABASE_URL error")
	assert.Contains(t, err.Error(), "DATABASE_URL")
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
		MigratorFactory:    noOpMigratorFactory,
		AutoMigrateGetter:  disableAutoMigrate,
		PolicyBootstrapper: noOpBootstrapper,
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected event store error")
	assert.Contains(t, err.Error(), "connection refused")
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
		MigratorFactory:    noOpMigratorFactory,
		AutoMigrateGetter:  disableAutoMigrate,
		PolicyBootstrapper: noOpBootstrapper,
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected init game ID error")
	assert.Contains(t, err.Error(), "game ID")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "", fmt.Errorf("failed to get certs directory")
			},
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		MigratorFactory:    noOpMigratorFactory,
		AutoMigrateGetter:  disableAutoMigrate,
		PolicyBootstrapper: noOpBootstrapper,
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected certs dir error")
	assert.Contains(t, err.Error(), "certs directory")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
			},
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return nil, fmt.Errorf("failed to load TLS certificates")
		},
		MigratorFactory:    noOpMigratorFactory,
		AutoMigrateGetter:  disableAutoMigrate,
		PolicyBootstrapper: noOpBootstrapper,
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected TLS error")
	assert.Contains(t, err.Error(), "TLS")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
			},
			ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
				return nil, fmt.Errorf("failed to load control TLS")
			},
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		MigratorFactory:    noOpMigratorFactory,
		AutoMigrateGetter:  disableAutoMigrate,
		PolicyBootstrapper: noOpBootstrapper,
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected control TLS error")
	assert.Contains(t, err.Error(), "control TLS")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
			},
			ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
				return testTLSConfig(), nil
			},
			ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
				return nil, fmt.Errorf("failed to create control server")
			},
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		MigratorFactory:    noOpMigratorFactory,
		AutoMigrateGetter:  disableAutoMigrate,
		PolicyBootstrapper: noOpBootstrapper,
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected control server error")
	assert.Contains(t, err.Error(), "failed to create control server")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
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
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		MigratorFactory:    noOpMigratorFactory,
		AutoMigrateGetter:  disableAutoMigrate,
		PolicyBootstrapper: noOpBootstrapper,
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected control server start error")
	assert.Contains(t, err.Error(), "address already in use")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
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
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		MigratorFactory:    noOpMigratorFactory,
		AutoMigrateGetter:  disableAutoMigrate,
		PolicyBootstrapper: noOpBootstrapper,
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected observability server start error")
	assert.Contains(t, err.Error(), "address already in use")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
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
		require.NoError(t, err, "runGatewayWithDeps() returned unexpected error")
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
	require.Error(t, err, "expected validation error")
	assert.Contains(t, err.Error(), "telnet-addr")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "", fmt.Errorf("failed to get certs directory")
			},
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected certs dir error")
	assert.Contains(t, err.Error(), "certs directory")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
			},
		},
		GameIDExtractor: func(_ string) (string, error) {
			return "", fmt.Errorf("failed to extract game_id")
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected game ID error")
	assert.Contains(t, err.Error(), "game_id")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
			},
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
	require.Error(t, err, "expected TLS error")
	assert.Contains(t, err.Error(), "TLS")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
			},
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
	require.Error(t, err, "expected gRPC client error")
	assert.Contains(t, err.Error(), "connection refused")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
			},
			ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
				return nil, fmt.Errorf("failed to load control TLS")
			},
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
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected control TLS error")
	assert.Contains(t, err.Error(), "control TLS")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
			},
			ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
				return testTLSConfig(), nil
			},
			ControlServerFactory: func(_ string, _ control.ShutdownFunc) (ControlServer, error) {
				return nil, fmt.Errorf("failed to create control server")
			},
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
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected control server error")
	assert.Contains(t, err.Error(), "failed to create control server")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
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
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected control server start error")
	assert.Contains(t, err.Error(), "address already in use")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
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
		ListenerFactory: func(_, _ string) (net.Listener, error) {
			return nil, fmt.Errorf("address already in use")
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected listener error")
	assert.Contains(t, err.Error(), "address already in use")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
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
		ListenerFactory: func(_, _ string) (net.Listener, error) {
			return ml, nil
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected observability server start error")
	assert.Contains(t, err.Error(), "address already in use")
}

// TestRunCoreWithDeps_BootstrapRequiresPostgresEventStore tests that bootstrap fails fatally
// when PolicyBootstrapper is nil and the event store is not *store.PostgresEventStore (ADR #92).
func TestRunCoreWithDeps_BootstrapRequiresPostgresEventStore(t *testing.T) {
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
		MigratorFactory:   noOpMigratorFactory,
		AutoMigrateGetter: disableAutoMigrate,
		// PolicyBootstrapper intentionally nil — triggers late-binding path
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, cmd, deps)
	require.Error(t, err, "expected BOOTSTRAP_FAILED when event store is not PostgresEventStore")
	errutil.AssertErrorCode(t, err, "BOOTSTRAP_FAILED")
	assert.Contains(t, err.Error(), "PostgresEventStore")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
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
		},
		EventStoreFactory: func(_ context.Context, _ string) (EventStore, error) {
			return &mockEventStore{}, nil
		},
		TLSCertEnsurer: func(_, _ string) (*cryptotls.Config, error) {
			return testTLSConfig(), nil
		},
		DatabaseURLGetter: func() string {
			return "postgres://test:test@localhost/test"
		},
		MigratorFactory:    noOpMigratorFactory,
		AutoMigrateGetter:  disableAutoMigrate,
		PolicyBootstrapper: noOpBootstrapper,
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
		require.NoError(t, err, "runCoreWithDeps() returned unexpected error")
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
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
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
		require.NoError(t, err, "runGatewayWithDeps() returned unexpected error")
	case <-time.After(5 * time.Second):
		t.Fatal("runGatewayWithDeps() did not return within timeout")
	}
}
