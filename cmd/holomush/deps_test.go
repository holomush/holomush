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

	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/control"
	"github.com/holomush/holomush/internal/eventbus"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/observability"
	contentv1 "github.com/holomush/holomush/pkg/proto/holomush/content/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	sceneaccessv1 "github.com/holomush/holomush/pkg/proto/holomush/sceneaccess/v1"
)

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

func (m *mockObservabilityServer) Registerer() prometheus.Registerer {
	// A fresh registry per call keeps unit tests free of duplicate-registration
	// panics; the production server returns its own /metrics-served registry.
	return prometheus.NewRegistry()
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

func (m *mockGRPCClient) HandleCommand(_ context.Context, _ *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) Subscribe(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
	return nil, nil
}

func (m *mockGRPCClient) Disconnect(_ context.Context, _ *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) GetCommandHistory(_ context.Context, _ *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error) {
	return &corev1.GetCommandHistoryResponse{Success: true}, nil
}

func (m *mockGRPCClient) AuthenticatePlayer(_ context.Context, _ *corev1.AuthenticatePlayerRequest) (*corev1.AuthenticatePlayerResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) SelectCharacter(_ context.Context, _ *corev1.SelectCharacterRequest) (*corev1.SelectCharacterResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) CreatePlayer(_ context.Context, _ *corev1.CreatePlayerRequest) (*corev1.CreatePlayerResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) CreateCharacter(_ context.Context, _ *corev1.CreateCharacterRequest) (*corev1.CreateCharacterResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) ListCharacters(_ context.Context, _ *corev1.ListCharactersRequest) (*corev1.ListCharactersResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) ListAllCharacters(_ context.Context, _ *corev1.ListAllCharactersRequest) (*corev1.ListAllCharactersResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) RequestPasswordReset(_ context.Context, _ *corev1.RequestPasswordResetRequest) (*corev1.RequestPasswordResetResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) ConfirmPasswordReset(_ context.Context, _ *corev1.ConfirmPasswordResetRequest) (*corev1.ConfirmPasswordResetResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) Logout(_ context.Context, _ *corev1.LogoutRequest) (*corev1.LogoutResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) CheckPlayerSession(_ context.Context, _ *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) CreateGuest(_ context.Context, _ *corev1.CreateGuestRequest) (*corev1.CreateGuestResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) QueryStreamHistory(_ context.Context, _ *corev1.QueryStreamHistoryRequest) (*corev1.QueryStreamHistoryResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) ListSessionStreams(_ context.Context, _ *corev1.ListSessionStreamsRequest) (*corev1.ListSessionStreamsResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) ListPlayerSessions(_ context.Context, _ *corev1.ListPlayerSessionsRequest) (*corev1.ListPlayerSessionsResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) RevokePlayerSession(_ context.Context, _ *corev1.RevokePlayerSessionRequest) (*corev1.RevokePlayerSessionResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) RevokeOtherPlayerSessions(_ context.Context, _ *corev1.RevokeOtherPlayerSessionsRequest) (*corev1.RevokeOtherPlayerSessionsResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) ListFocusPresence(_ context.Context, _ *corev1.ListFocusPresenceRequest) (*corev1.ListFocusPresenceResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) ListAvailableCommands(_ context.Context, _ *corev1.ListAvailableCommandsRequest) (*corev1.ListAvailableCommandsResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) RefreshConnection(_ context.Context, _ *corev1.RefreshConnectionRequest) (*corev1.RefreshConnectionResponse, error) {
	return &corev1.RefreshConnectionResponse{}, nil
}

func (m *mockGRPCClient) GetContent(_ context.Context, _ *contentv1.GetContentRequest) (*contentv1.GetContentResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) ListContent(_ context.Context, _ *contentv1.ListContentRequest) (*contentv1.ListContentResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) ListScenesForViewer(_ context.Context, _ *sceneaccessv1.ListScenesForViewerRequest) (*sceneaccessv1.ListScenesForViewerResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) GetSceneForViewer(_ context.Context, _ *sceneaccessv1.GetSceneForViewerRequest) (*sceneaccessv1.GetSceneForViewerResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) ListMyScenes(_ context.Context, _ *sceneaccessv1.ListMyScenesRequest) (*sceneaccessv1.ListMyScenesResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) WatchScene(_ context.Context, _ *sceneaccessv1.WatchSceneRequest) (*sceneaccessv1.WatchSceneResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) CreateScene(_ context.Context, _ *sceneaccessv1.CreateSceneRequest) (*sceneaccessv1.CreateSceneResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) EndScene(_ context.Context, _ *sceneaccessv1.EndSceneRequest) (*sceneaccessv1.EndSceneResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) PauseScene(_ context.Context, _ *sceneaccessv1.PauseSceneRequest) (*sceneaccessv1.PauseSceneResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) ResumeScene(_ context.Context, _ *sceneaccessv1.ResumeSceneRequest) (*sceneaccessv1.ResumeSceneResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) MuteScene(_ context.Context, _ *sceneaccessv1.MuteSceneRequest) (*sceneaccessv1.MuteSceneResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) SetSceneNotifyPref(_ context.Context, _ *sceneaccessv1.SetSceneNotifyPrefRequest) (*sceneaccessv1.SetSceneNotifyPrefResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) UpdateScene(_ context.Context, _ *sceneaccessv1.UpdateSceneRequest) (*sceneaccessv1.UpdateSceneResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) ExportScene(_ context.Context, _ *sceneaccessv1.ExportSceneRequest) (*sceneaccessv1.ExportSceneResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) SetSceneFocus(_ context.Context, _ *sceneaccessv1.SetSceneFocusRequest) (*sceneaccessv1.SetSceneFocusResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) ListPublishedScenes(_ context.Context, _ *sceneaccessv1.ListPublishedScenesRequest) (*sceneaccessv1.ListPublishedScenesResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) GetPublicSceneArchive(_ context.Context, _ *sceneaccessv1.GetPublicSceneArchiveRequest) (*sceneaccessv1.GetPublicSceneArchiveResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) DownloadPublicSceneArchive(_ context.Context, _ *sceneaccessv1.DownloadPublicSceneArchiveRequest) (*sceneaccessv1.DownloadPublicSceneArchiveResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) InviteToScene(_ context.Context, _ *sceneaccessv1.InviteToSceneRequest) (*sceneaccessv1.InviteToSceneResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) KickFromScene(_ context.Context, _ *sceneaccessv1.KickFromSceneRequest) (*sceneaccessv1.KickFromSceneResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) TransferOwnership(_ context.Context, _ *sceneaccessv1.TransferOwnershipRequest) (*sceneaccessv1.TransferOwnershipResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) LeaveScene(_ context.Context, _ *sceneaccessv1.LeaveSceneRequest) (*sceneaccessv1.LeaveSceneResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) StartScenePublish(_ context.Context, _ *sceneaccessv1.StartScenePublishRequest) (*sceneaccessv1.StartScenePublishResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) CastPublishSceneVote(_ context.Context, _ *sceneaccessv1.CastPublishSceneVoteRequest) (*sceneaccessv1.CastPublishSceneVoteResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) WithdrawScenePublish(_ context.Context, _ *sceneaccessv1.WithdrawScenePublishRequest) (*sceneaccessv1.WithdrawScenePublishResponse, error) {
	return nil, nil
}

func (m *mockGRPCClient) GetPublishedScene(_ context.Context, _ *sceneaccessv1.GetPublishedSceneRequest) (*sceneaccessv1.GetPublishedSceneResponse, error) {
	return nil, nil
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

// TestRunCoreWithDeps_ValidationError tests that validation errors are returned.
func TestRunCoreWithDeps_ValidationError(t *testing.T) {
	ctx := context.Background()
	cfg := &coreConfig{
		GRPCAddr:    "", // Invalid - required
		ControlAddr: "127.0.0.1:9001",
		LogFormat:   "json",
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, config.GameConfig{}, config.DefaultAuthConfig(), eventbus.Config{StoreDir: t.TempDir()}, config.DefaultCryptoConfig(), config.DefaultLoggingConfig(), cmd, nil)
	require.Error(t, err, "expected validation error")
	assert.Contains(t, err.Error(), "grpc-addr")
}

// TestRunCoreWithDeps_DatabaseURLMissing tests missing DATABASE_URL error.
func TestRunCoreWithDeps_DatabaseURLMissing(t *testing.T) {
	ctx := context.Background()
	cfg := &coreConfig{
		GRPCAddr:           "localhost:9000",
		ControlAddr:        "127.0.0.1:9001",
		LogFormat:          "json",
		LuaTimeout:         1 * time.Second,
		LuaRegistryMaxSize: 65536,
	}

	deps := &CoreDeps{
		DatabaseURLGetter: func() string {
			return "" // Empty DATABASE_URL
		},
	}

	cmd := newMockCmd()
	err := runCoreWithDeps(ctx, cfg, config.GameConfig{}, config.DefaultAuthConfig(), eventbus.Config{StoreDir: t.TempDir()}, config.DefaultCryptoConfig(), config.DefaultLoggingConfig(), cmd, deps)
	require.Error(t, err, "expected DATABASE_URL error")
	assert.Contains(t, err.Error(), "DATABASE_URL")
}

// NOTE: TestRunCoreWithDeps_EventStoreFactoryError, TestRunCoreWithDeps_InitGameIDError,
// TestRunCoreWithDeps_CertsDirError, TestRunCoreWithDeps_TLSCertError,
// TestRunCoreWithDeps_ControlTLSLoadError, TestRunCoreWithDeps_ControlServerFactoryError,
// TestRunCoreWithDeps_ControlServerStartError, and TestRunCoreWithDeps_ObservabilityServerStartError
// were removed during the subsystem rewrite. EventStoreFactory and PolicyBootstrapper
// moved into subsystem configs and are no longer injectable via CoreDeps. Tests for
// post-DB-start error paths now require a running database and belong in integration tests.

// TestRunGatewayWithDeps_HappyPath tests the gateway process with all mocked dependencies.
func TestRunGatewayWithDeps_HappyPath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := &gatewayConfig{
		TelnetAddr:           ":4201",
		CoreAddr:             "localhost:9000",
		ControlAddr:          "127.0.0.1:9002",
		MetricsAddr:          "", // Disable observability server for simplicity
		LogFormat:            "json",
		TelnetMaxConns:       defaultTelnetMaxConns,
		TelnetIdleTimeout:    defaultTelnetIdleTimeout,
		TelnetWriteTimeout:   defaultTelnetWriteTimeout,
		TelnetPreAuthTimeout: defaultTelnetPreAuthTimeout,
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
		errChan <- runGatewayWithDeps(ctx, cfg, config.DefaultLoggingConfig(), cmd, deps)
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
		TelnetAddr:           "", // Invalid - required
		CoreAddr:             "localhost:9000",
		ControlAddr:          "127.0.0.1:9002",
		LogFormat:            "json",
		TelnetMaxConns:       defaultTelnetMaxConns,
		TelnetIdleTimeout:    defaultTelnetIdleTimeout,
		TelnetWriteTimeout:   defaultTelnetWriteTimeout,
		TelnetPreAuthTimeout: defaultTelnetPreAuthTimeout,
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, config.DefaultLoggingConfig(), cmd, nil)
	require.Error(t, err, "expected validation error")
	assert.Contains(t, err.Error(), "telnet-addr")
}

// TestRunGatewayWithDeps_CertsDirError tests certificates directory error.
func TestRunGatewayWithDeps_CertsDirError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		TelnetAddr:           ":4201",
		CoreAddr:             "localhost:9000",
		ControlAddr:          "127.0.0.1:9002",
		LogFormat:            "json",
		TelnetMaxConns:       defaultTelnetMaxConns,
		TelnetIdleTimeout:    defaultTelnetIdleTimeout,
		TelnetWriteTimeout:   defaultTelnetWriteTimeout,
		TelnetPreAuthTimeout: defaultTelnetPreAuthTimeout,
	}

	deps := &GatewayDeps{
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "", fmt.Errorf("failed to get certs directory")
			},
		},
	}

	cmd := newMockCmd()
	err := runGatewayWithDeps(ctx, cfg, config.DefaultLoggingConfig(), cmd, deps)
	require.Error(t, err, "expected certs dir error")
	assert.Contains(t, err.Error(), "certs directory")
}

// TestRunGatewayWithDeps_GameIDExtractorError tests game ID extraction error.
func TestRunGatewayWithDeps_GameIDExtractorError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		TelnetAddr:           ":4201",
		CoreAddr:             "localhost:9000",
		ControlAddr:          "127.0.0.1:9002",
		LogFormat:            "json",
		TelnetMaxConns:       defaultTelnetMaxConns,
		TelnetIdleTimeout:    defaultTelnetIdleTimeout,
		TelnetWriteTimeout:   defaultTelnetWriteTimeout,
		TelnetPreAuthTimeout: defaultTelnetPreAuthTimeout,
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
	err := runGatewayWithDeps(ctx, cfg, config.DefaultLoggingConfig(), cmd, deps)
	require.Error(t, err, "expected game ID error")
	assert.Contains(t, err.Error(), "game_id")
}

// TestRunGatewayWithDeps_ClientTLSLoaderError tests client TLS loading error.
func TestRunGatewayWithDeps_ClientTLSLoaderError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		TelnetAddr:           ":4201",
		CoreAddr:             "localhost:9000",
		ControlAddr:          "127.0.0.1:9002",
		LogFormat:            "json",
		TelnetMaxConns:       defaultTelnetMaxConns,
		TelnetIdleTimeout:    defaultTelnetIdleTimeout,
		TelnetWriteTimeout:   defaultTelnetWriteTimeout,
		TelnetPreAuthTimeout: defaultTelnetPreAuthTimeout,
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
	err := runGatewayWithDeps(ctx, cfg, config.DefaultLoggingConfig(), cmd, deps)
	require.Error(t, err, "expected TLS error")
	assert.Contains(t, err.Error(), "TLS")
}

// TestRunGatewayWithDeps_GRPCClientFactoryError tests gRPC client creation error.
func TestRunGatewayWithDeps_GRPCClientFactoryError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		TelnetAddr:           ":4201",
		CoreAddr:             "localhost:9000",
		ControlAddr:          "127.0.0.1:9002",
		LogFormat:            "json",
		TelnetMaxConns:       defaultTelnetMaxConns,
		TelnetIdleTimeout:    defaultTelnetIdleTimeout,
		TelnetWriteTimeout:   defaultTelnetWriteTimeout,
		TelnetPreAuthTimeout: defaultTelnetPreAuthTimeout,
	}

	deps := &GatewayDeps{
		CommonDeps: CommonDeps{
			CertsDirGetter: func() (string, error) {
				return "/tmp/certs", nil
			},
			ControlTLSLoader: func(_, _ string) (*cryptotls.Config, error) {
				return testTLSConfig(), nil
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
	err := runGatewayWithDeps(ctx, cfg, config.DefaultLoggingConfig(), cmd, deps)
	require.Error(t, err, "expected gRPC client error")
	assert.Contains(t, err.Error(), "connection refused")
}

// TestRunGatewayWithDeps_ControlTLSLoadError tests control TLS loading error.
func TestRunGatewayWithDeps_ControlTLSLoadError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		TelnetAddr:           ":4201",
		CoreAddr:             "localhost:9000",
		ControlAddr:          "127.0.0.1:9002",
		LogFormat:            "json",
		TelnetMaxConns:       defaultTelnetMaxConns,
		TelnetIdleTimeout:    defaultTelnetIdleTimeout,
		TelnetWriteTimeout:   defaultTelnetWriteTimeout,
		TelnetPreAuthTimeout: defaultTelnetPreAuthTimeout,
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
	err := runGatewayWithDeps(ctx, cfg, config.DefaultLoggingConfig(), cmd, deps)
	require.Error(t, err, "expected control TLS error")
	assert.Contains(t, err.Error(), "control TLS")
}

// TestRunGatewayWithDeps_ControlServerFactoryError tests control server creation error.
func TestRunGatewayWithDeps_ControlServerFactoryError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		TelnetAddr:           ":4201",
		CoreAddr:             "localhost:9000",
		ControlAddr:          "127.0.0.1:9002",
		LogFormat:            "json",
		TelnetMaxConns:       defaultTelnetMaxConns,
		TelnetIdleTimeout:    defaultTelnetIdleTimeout,
		TelnetWriteTimeout:   defaultTelnetWriteTimeout,
		TelnetPreAuthTimeout: defaultTelnetPreAuthTimeout,
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
	err := runGatewayWithDeps(ctx, cfg, config.DefaultLoggingConfig(), cmd, deps)
	require.Error(t, err, "expected control server error")
	assert.Contains(t, err.Error(), "failed to create control server")
}

// TestRunGatewayWithDeps_ControlServerStartError tests control server start error.
func TestRunGatewayWithDeps_ControlServerStartError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		TelnetAddr:           ":4201",
		CoreAddr:             "localhost:9000",
		ControlAddr:          "127.0.0.1:9002",
		LogFormat:            "json",
		TelnetMaxConns:       defaultTelnetMaxConns,
		TelnetIdleTimeout:    defaultTelnetIdleTimeout,
		TelnetWriteTimeout:   defaultTelnetWriteTimeout,
		TelnetPreAuthTimeout: defaultTelnetPreAuthTimeout,
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
	err := runGatewayWithDeps(ctx, cfg, config.DefaultLoggingConfig(), cmd, deps)
	require.Error(t, err, "expected control server start error")
	assert.Contains(t, err.Error(), "address already in use")
}

// TestRunGatewayWithDeps_ListenerFactoryError tests telnet listener creation error.
func TestRunGatewayWithDeps_ListenerFactoryError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		TelnetAddr:           ":4201",
		CoreAddr:             "localhost:9000",
		ControlAddr:          "127.0.0.1:9002",
		LogFormat:            "json",
		TelnetMaxConns:       defaultTelnetMaxConns,
		TelnetIdleTimeout:    defaultTelnetIdleTimeout,
		TelnetWriteTimeout:   defaultTelnetWriteTimeout,
		TelnetPreAuthTimeout: defaultTelnetPreAuthTimeout,
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
	err := runGatewayWithDeps(ctx, cfg, config.DefaultLoggingConfig(), cmd, deps)
	require.Error(t, err, "expected listener error")
	assert.Contains(t, err.Error(), "address already in use")
}

// TestRunGatewayWithDeps_ObservabilityServerStartError tests observability server start error.
func TestRunGatewayWithDeps_ObservabilityServerStartError(t *testing.T) {
	ctx := context.Background()
	cfg := &gatewayConfig{
		TelnetAddr:           ":4201",
		CoreAddr:             "localhost:9000",
		ControlAddr:          "127.0.0.1:9002",
		MetricsAddr:          "127.0.0.1:9101",
		LogFormat:            "json",
		TelnetMaxConns:       defaultTelnetMaxConns,
		TelnetIdleTimeout:    defaultTelnetIdleTimeout,
		TelnetWriteTimeout:   defaultTelnetWriteTimeout,
		TelnetPreAuthTimeout: defaultTelnetPreAuthTimeout,
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
	err := runGatewayWithDeps(ctx, cfg, config.DefaultLoggingConfig(), cmd, deps)
	require.Error(t, err, "expected observability server start error")
	assert.Contains(t, err.Error(), "address already in use")
}

// TestRunGatewayWithDeps_WithObservability tests the happy path with observability server enabled.
func TestRunGatewayWithDeps_WithObservability(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := &gatewayConfig{
		TelnetAddr:           ":4201",
		CoreAddr:             "localhost:9000",
		ControlAddr:          "127.0.0.1:9002",
		MetricsAddr:          "127.0.0.1:9101",
		LogFormat:            "json",
		TelnetMaxConns:       defaultTelnetMaxConns,
		TelnetIdleTimeout:    defaultTelnetIdleTimeout,
		TelnetWriteTimeout:   defaultTelnetWriteTimeout,
		TelnetPreAuthTimeout: defaultTelnetPreAuthTimeout,
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
		errChan <- runGatewayWithDeps(ctx, cfg, config.DefaultLoggingConfig(), cmd, deps)
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

// TestCoreDepsApplyDefaultsSetsAllNilFields verifies that nil dependency fields
// are populated with non-nil default implementations.
func TestCoreDepsApplyDefaultsSetsAllNilFields(t *testing.T) {
	d := &CoreDeps{}

	d.applyDefaults()

	assert.NotNil(t, d.TLSCertEnsurer)
	assert.NotNil(t, d.ControlTLSLoader)
	assert.NotNil(t, d.ControlServerFactory)
	assert.NotNil(t, d.ObservabilityServerFactory)
	assert.NotNil(t, d.CertsDirGetter)
	assert.NotNil(t, d.DatabaseURLGetter)
	assert.NotNil(t, d.MigratorFactory)
	assert.NotNil(t, d.AutoMigrateGetter)
}

// TestCoreDepsApplyDefaultsPreservesNonNilFields verifies that custom dependency
// functions provided by the caller are not overwritten by applyDefaults.
func TestCoreDepsApplyDefaultsPreservesNonNilFields(t *testing.T) {
	sentinel := "custom-impl"
	customGetter := func() string { return sentinel }

	d := &CoreDeps{
		DatabaseURLGetter: customGetter,
	}

	d.applyDefaults()

	require.NotNil(t, d.DatabaseURLGetter)
	assert.Equal(t, sentinel, d.DatabaseURLGetter())
}

// TestCoreDepsApplyDefaultsIsIdempotent verifies that calling applyDefaults
// twice in succession does not panic or overwrite previously set defaults.
func TestCoreDepsApplyDefaultsIsIdempotent(t *testing.T) {
	d := &CoreDeps{}

	d.applyDefaults()
	first := d.DatabaseURLGetter

	d.applyDefaults()
	second := d.DatabaseURLGetter

	// Both should be non-nil; the second call must not have replaced them.
	assert.NotNil(t, first)
	assert.NotNil(t, second)
}

// TestCoreDepsApplyDefaultsDatabaseURLReadsEnv verifies that the default
// DatabaseURLGetter reads from the DATABASE_URL environment variable.
func TestCoreDepsApplyDefaultsDatabaseURLReadsEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test:pass@localhost/mydb")

	d := &CoreDeps{}
	d.applyDefaults()

	got := d.DatabaseURLGetter()
	assert.Equal(t, "postgres://test:pass@localhost/mydb", got)
}
