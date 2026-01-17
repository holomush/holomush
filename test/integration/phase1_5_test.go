//go:build integration

// Package integration provides end-to-end integration tests for HoloMUSH.
package integration

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/holomush/holomush/internal/control"
	"github.com/holomush/holomush/internal/core"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	corev1 "github.com/holomush/holomush/internal/proto/holomush/core/v1"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/tls"
)

// testEnv holds all the resources needed for integration tests.
type testEnv struct {
	ctx           context.Context
	cancel        context.CancelFunc
	container     testcontainers.Container
	store         *store.PostgresEventStore
	certsDir      string
	runtimeDir    string
	gameID        string
	grpcAddr      string
	grpcServer    *grpc.Server
	grpcListener  net.Listener
	controlServer *control.Server
}

// setupTestEnv creates a complete test environment with PostgreSQL, TLS certs, and directories.
func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)

	env := &testEnv{
		ctx:    ctx,
		cancel: cancel,
	}

	// Create temp directories
	tmpDir := t.TempDir()
	env.certsDir = filepath.Join(tmpDir, "certs")
	env.runtimeDir = filepath.Join(tmpDir, "run")

	if err := os.MkdirAll(env.certsDir, 0o700); err != nil {
		t.Fatalf("failed to create certs dir: %v", err)
	}
	if err := os.MkdirAll(env.runtimeDir, 0o700); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	// Set XDG env vars for control socket
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Start PostgreSQL container
	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("holomush_test"),
		postgres.WithUsername("holomush"),
		postgres.WithPassword("holomush"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		cancel()
		t.Fatalf("failed to start postgres container: %v", err)
	}
	env.container = container

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	// Create and migrate store
	env.store, err = store.NewPostgresEventStore(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to create event store: %v", err)
	}

	if err := env.store.Migrate(ctx); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	return env
}

// cleanup releases all test resources.
func (env *testEnv) cleanup(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if env.controlServer != nil {
		if err := env.controlServer.Stop(ctx); err != nil {
			t.Logf("failed to stop control server: %v", err)
		}
	}

	if env.grpcServer != nil {
		env.grpcServer.GracefulStop()
	}

	if env.grpcListener != nil {
		_ = env.grpcListener.Close()
	}

	if env.store != nil {
		env.store.Close()
	}

	if env.container != nil {
		if err := env.container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}

	env.cancel()
}

// TestPhase1_5_DatabaseMigrations verifies migrations run successfully and system_info table exists.
func TestPhase1_5_DatabaseMigrations(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup(t)

	// Test that we can use system_info table
	err := env.store.SetSystemInfo(env.ctx, "test_key", "test_value")
	if err != nil {
		t.Fatalf("SetSystemInfo failed: %v", err)
	}

	value, err := env.store.GetSystemInfo(env.ctx, "test_key")
	if err != nil {
		t.Fatalf("GetSystemInfo failed: %v", err)
	}
	if value != "test_value" {
		t.Errorf("GetSystemInfo() = %q, want %q", value, "test_value")
	}
}

// TestPhase1_5_GameIDGeneration verifies game_id is generated and persisted.
func TestPhase1_5_GameIDGeneration(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup(t)

	// Initialize game_id
	gameID, err := env.store.InitGameID(env.ctx)
	if err != nil {
		t.Fatalf("InitGameID failed: %v", err)
	}

	// Verify it's a valid ULID (26 characters)
	if len(gameID) != 26 {
		t.Errorf("game_id has invalid length: %d, want 26", len(gameID))
	}

	// Verify it persists (calling again returns same ID)
	gameID2, err := env.store.InitGameID(env.ctx)
	if err != nil {
		t.Fatalf("second InitGameID failed: %v", err)
	}
	if gameID != gameID2 {
		t.Errorf("game_id changed: %q vs %q", gameID, gameID2)
	}

	// Verify it's stored in database
	storedID, err := env.store.GetSystemInfo(env.ctx, "game_id")
	if err != nil {
		t.Fatalf("GetSystemInfo(game_id) failed: %v", err)
	}
	if storedID != gameID {
		t.Errorf("stored game_id %q != returned game_id %q", storedID, gameID)
	}
}

// TestPhase1_5_TLSCertificateGeneration verifies TLS certs are generated with correct game_id in SAN.
func TestPhase1_5_TLSCertificateGeneration(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup(t)

	// Initialize game_id
	gameID, err := env.store.InitGameID(env.ctx)
	if err != nil {
		t.Fatalf("InitGameID failed: %v", err)
	}
	env.gameID = gameID

	// Generate CA
	ca, err := tls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Generate server cert
	serverCert, err := tls.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert failed: %v", err)
	}

	// Save certificates
	if err := tls.SaveCertificates(env.certsDir, ca, serverCert); err != nil {
		t.Fatalf("SaveCertificates failed: %v", err)
	}

	// Generate and save client cert
	clientCert, err := tls.GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert failed: %v", err)
	}
	if err := tls.SaveClientCert(env.certsDir, clientCert); err != nil {
		t.Fatalf("SaveClientCert failed: %v", err)
	}

	// Verify server cert has correct SAN
	expectedSAN := "holomush-" + gameID
	found := false
	for _, name := range serverCert.Certificate.DNSNames {
		if name == expectedSAN {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("server cert missing expected SAN %q, got %v", expectedSAN, serverCert.Certificate.DNSNames)
	}

	// Verify CA has game_id in CN
	expectedCN := "HoloMUSH CA " + gameID
	if ca.Certificate.Subject.CommonName != expectedCN {
		t.Errorf("CA CN = %q, want %q", ca.Certificate.Subject.CommonName, expectedCN)
	}

	// Verify CA has game_id in URI SAN
	expectedURI := "holomush://game/" + gameID
	foundURI := false
	for _, uri := range ca.Certificate.URIs {
		if uri.String() == expectedURI {
			foundURI = true
			break
		}
	}
	if !foundURI {
		var uris []string
		for _, uri := range ca.Certificate.URIs {
			uris = append(uris, uri.String())
		}
		t.Errorf("CA missing expected URI SAN %q, got %v", expectedURI, uris)
	}

	// Verify client cert CN
	if clientCert.Certificate.Subject.CommonName != "holomush-gateway" {
		t.Errorf("client cert CN = %q, want %q", clientCert.Certificate.Subject.CommonName, "holomush-gateway")
	}

	// Verify files were created
	files := []string{"root-ca.crt", "root-ca.key", "core.crt", "core.key", "gateway.crt", "gateway.key"}
	for _, f := range files {
		path := filepath.Join(env.certsDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", path)
		}
	}
}

// TestPhase1_5_GRPCServerMTLS verifies gRPC server starts and accepts mTLS connections.
func TestPhase1_5_GRPCServerMTLS(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup(t)

	// Initialize game_id
	gameID, err := env.store.InitGameID(env.ctx)
	if err != nil {
		t.Fatalf("InitGameID failed: %v", err)
	}
	env.gameID = gameID

	// Generate and save certificates
	ca, err := tls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}
	serverCert, err := tls.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert failed: %v", err)
	}
	if err := tls.SaveCertificates(env.certsDir, ca, serverCert); err != nil {
		t.Fatalf("SaveCertificates failed: %v", err)
	}
	clientCert, err := tls.GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert failed: %v", err)
	}
	if err := tls.SaveClientCert(env.certsDir, clientCert); err != nil {
		t.Fatalf("SaveClientCert failed: %v", err)
	}

	// Load server TLS config
	serverTLS, err := tls.LoadServerTLS(env.certsDir, "core")
	if err != nil {
		t.Fatalf("LoadServerTLS failed: %v", err)
	}

	// Create core components
	eventStore := core.NewMemoryEventStore()
	broadcaster := core.NewBroadcaster()
	sessions := core.NewSessionManager()
	engine := core.NewEngine(eventStore, sessions, broadcaster)

	// Create gRPC server with mTLS
	coreServer := grpcpkg.NewCoreServer(engine, sessions, broadcaster)
	env.grpcServer = grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	corev1.RegisterCoreServer(env.grpcServer, coreServer)

	// Start listening
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	env.grpcListener = listener
	env.grpcAddr = listener.Addr().String()

	go func() {
		_ = env.grpcServer.Serve(listener)
	}()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	// Load client TLS config
	clientTLS, err := tls.LoadClientTLS(env.certsDir, "gateway", gameID)
	if err != nil {
		t.Fatalf("LoadClientTLS failed: %v", err)
	}

	// Create gRPC client with mTLS
	client, err := grpcpkg.NewClient(env.ctx, grpcpkg.ClientConfig{
		Address:   env.grpcAddr,
		TLSConfig: clientTLS,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	// Test connection by attempting to authenticate (will fail auth but confirms connection works)
	resp, err := client.Authenticate(env.ctx, &corev1.AuthRequest{
		Username: "test",
		Password: "test",
	})
	if err != nil {
		t.Fatalf("Authenticate RPC failed: %v", err)
	}

	// We expect auth to fail (no authenticator configured) but the RPC should succeed
	if resp.Success {
		t.Error("expected auth to fail (no authenticator), but it succeeded")
	}
	if resp.Error != "authentication not configured" {
		t.Logf("auth error: %s", resp.Error)
	}
}

// TestPhase1_5_GRPCServerRejectsBadCert verifies gRPC server rejects connections with invalid certs.
func TestPhase1_5_GRPCServerRejectsBadCert(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup(t)

	// Initialize game_id
	gameID, err := env.store.InitGameID(env.ctx)
	if err != nil {
		t.Fatalf("InitGameID failed: %v", err)
	}
	env.gameID = gameID

	// Generate and save certificates
	ca, err := tls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}
	serverCert, err := tls.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert failed: %v", err)
	}
	if err := tls.SaveCertificates(env.certsDir, ca, serverCert); err != nil {
		t.Fatalf("SaveCertificates failed: %v", err)
	}

	// Load server TLS config
	serverTLS, err := tls.LoadServerTLS(env.certsDir, "core")
	if err != nil {
		t.Fatalf("LoadServerTLS failed: %v", err)
	}

	// Create core components
	eventStore := core.NewMemoryEventStore()
	broadcaster := core.NewBroadcaster()
	sessions := core.NewSessionManager()
	engine := core.NewEngine(eventStore, sessions, broadcaster)

	// Create gRPC server with mTLS
	coreServer := grpcpkg.NewCoreServer(engine, sessions, broadcaster)
	env.grpcServer = grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	corev1.RegisterCoreServer(env.grpcServer, coreServer)

	// Start listening
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	env.grpcListener = listener
	env.grpcAddr = listener.Addr().String()

	go func() {
		_ = env.grpcServer.Serve(listener)
	}()

	time.Sleep(100 * time.Millisecond)

	// Generate a different CA (attacker's CA)
	badCA, err := tls.GenerateCA("bad-game-id")
	if err != nil {
		t.Fatalf("GenerateCA (bad) failed: %v", err)
	}
	badCertsDir := filepath.Join(env.certsDir, "bad")
	if err := os.MkdirAll(badCertsDir, 0o700); err != nil {
		t.Fatalf("failed to create bad certs dir: %v", err)
	}
	badClientCert, err := tls.GenerateClientCert(badCA, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert (bad) failed: %v", err)
	}
	if err := tls.SaveCertificates(badCertsDir, badCA, nil); err != nil {
		t.Fatalf("SaveCertificates (bad CA) failed: %v", err)
	}
	if err := tls.SaveClientCert(badCertsDir, badClientCert); err != nil {
		t.Fatalf("SaveClientCert (bad) failed: %v", err)
	}

	// Try to connect with bad cert
	badClientTLS, err := tls.LoadClientTLS(badCertsDir, "gateway", "bad-game-id")
	if err != nil {
		t.Fatalf("LoadClientTLS (bad) failed: %v", err)
	}

	client, err := grpcpkg.NewClient(env.ctx, grpcpkg.ClientConfig{
		Address:   env.grpcAddr,
		TLSConfig: badClientTLS,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	// Attempt RPC - should fail due to cert mismatch
	_, err = client.Authenticate(env.ctx, &corev1.AuthRequest{
		Username: "test",
		Password: "test",
	})
	if err == nil {
		t.Error("expected RPC to fail with bad cert, but it succeeded")
	}
}

// TestPhase1_5_ControlSocketHealthCheck verifies control socket responds to health check.
func TestPhase1_5_ControlSocketHealthCheck(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup(t)

	shutdownCalled := false
	shutdownFunc := func() {
		shutdownCalled = true
	}

	// Create and start control server
	env.controlServer = control.NewServer("core", shutdownFunc)
	if err := env.controlServer.Start(); err != nil {
		t.Fatalf("control server Start failed: %v", err)
	}

	// Wait for socket to be ready
	time.Sleep(100 * time.Millisecond)

	// Get socket path
	socketPath, err := control.SocketPath("core")
	if err != nil {
		t.Fatalf("SocketPath failed: %v", err)
	}

	// Create HTTP client using Unix socket
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	// Test /health endpoint
	resp, err := client.Get("http://localhost/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health check status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var healthResp control.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}

	if healthResp.Status != "healthy" {
		t.Errorf("health status = %q, want %q", healthResp.Status, "healthy")
	}

	// Test /status endpoint
	resp, err = client.Get("http://localhost/status")
	if err != nil {
		t.Fatalf("status check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status check status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var statusResp control.StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		t.Fatalf("failed to decode status response: %v", err)
	}

	if !statusResp.Running {
		t.Error("expected Running = true")
	}
	if statusResp.Component != "core" {
		t.Errorf("Component = %q, want %q", statusResp.Component, "core")
	}

	// Test /shutdown endpoint
	resp, err = client.Post("http://localhost/shutdown", "application/json", nil)
	if err != nil {
		t.Fatalf("shutdown request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("shutdown status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Wait for shutdown to be triggered
	time.Sleep(100 * time.Millisecond)

	if !shutdownCalled {
		t.Error("expected shutdown function to be called")
	}
}

// TestPhase1_5_EndToEndWorkflow tests the complete Phase 1.5 workflow.
func TestPhase1_5_EndToEndWorkflow(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup(t)

	// Step 1: Run migrations (already done in setupTestEnv)
	t.Log("Step 1: Database migrations - PASSED (done in setup)")

	// Step 2: Initialize game_id
	gameID, err := env.store.InitGameID(env.ctx)
	if err != nil {
		t.Fatalf("Step 2: InitGameID failed: %v", err)
	}
	if len(gameID) != 26 {
		t.Fatalf("Step 2: Invalid game_id length: %d", len(gameID))
	}
	env.gameID = gameID
	t.Logf("Step 2: game_id initialized - %s", gameID)

	// Step 3: Generate TLS certs with game_id
	ca, err := tls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("Step 3: GenerateCA failed: %v", err)
	}
	serverCert, err := tls.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("Step 3: GenerateServerCert failed: %v", err)
	}
	if err := tls.SaveCertificates(env.certsDir, ca, serverCert); err != nil {
		t.Fatalf("Step 3: SaveCertificates failed: %v", err)
	}
	clientCert, err := tls.GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("Step 3: GenerateClientCert failed: %v", err)
	}
	if err := tls.SaveClientCert(env.certsDir, clientCert); err != nil {
		t.Fatalf("Step 3: SaveClientCert failed: %v", err)
	}

	// Verify SAN contains game_id
	sanFound := false
	for _, san := range serverCert.Certificate.DNSNames {
		if san == "holomush-"+gameID {
			sanFound = true
			break
		}
	}
	if !sanFound {
		t.Fatalf("Step 3: Server cert missing game_id in SAN")
	}
	t.Log("Step 3: TLS certificates generated with game_id in SAN")

	// Step 4: Start gRPC server with mTLS
	serverTLS, err := tls.LoadServerTLS(env.certsDir, "core")
	if err != nil {
		t.Fatalf("Step 4: LoadServerTLS failed: %v", err)
	}

	eventStore := core.NewMemoryEventStore()
	broadcaster := core.NewBroadcaster()
	sessions := core.NewSessionManager()
	engine := core.NewEngine(eventStore, sessions, broadcaster)

	coreServer := grpcpkg.NewCoreServer(engine, sessions, broadcaster)
	env.grpcServer = grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	corev1.RegisterCoreServer(env.grpcServer, coreServer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Step 4: failed to listen: %v", err)
	}
	env.grpcListener = listener
	env.grpcAddr = listener.Addr().String()

	go func() {
		_ = env.grpcServer.Serve(listener)
	}()

	time.Sleep(100 * time.Millisecond)
	t.Logf("Step 4: gRPC server started on %s", env.grpcAddr)

	// Step 5: Connect gRPC client
	clientTLS, err := tls.LoadClientTLS(env.certsDir, "gateway", gameID)
	if err != nil {
		t.Fatalf("Step 5: LoadClientTLS failed: %v", err)
	}

	client, err := grpcpkg.NewClient(env.ctx, grpcpkg.ClientConfig{
		Address:   env.grpcAddr,
		TLSConfig: clientTLS,
	})
	if err != nil {
		t.Fatalf("Step 5: NewClient failed: %v", err)
	}
	defer client.Close()
	t.Log("Step 5: gRPC client connected with mTLS")

	// Step 6: Verify authentication works (RPC succeeds, auth fails as expected)
	resp, err := client.Authenticate(env.ctx, &corev1.AuthRequest{
		Username: "test",
		Password: "test",
	})
	if err != nil {
		t.Fatalf("Step 6: Authenticate RPC failed: %v", err)
	}
	if resp.Success {
		t.Error("Step 6: Expected auth to fail (no authenticator)")
	}
	t.Log("Step 6: Authentication RPC works (auth fails as expected - no authenticator)")

	// Step 7: Start control socket
	shutdownCh := make(chan struct{})
	env.controlServer = control.NewServer("core", func() {
		close(shutdownCh)
	})
	if err := env.controlServer.Start(); err != nil {
		t.Fatalf("Step 7: control server Start failed: %v", err)
	}
	t.Log("Step 7: Control socket started")

	// Step 8: Verify health check
	socketPath, err := control.SocketPath("core")
	if err != nil {
		t.Fatalf("Step 8: SocketPath failed: %v", err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	healthResp, err := httpClient.Get("http://localhost/health")
	if err != nil {
		t.Fatalf("Step 8: health check failed: %v", err)
	}
	defer healthResp.Body.Close()

	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("Step 8: health check status = %d, want %d", healthResp.StatusCode, http.StatusOK)
	}

	var health control.HealthResponse
	if err := json.NewDecoder(healthResp.Body).Decode(&health); err != nil {
		t.Fatalf("Step 8: failed to decode health response: %v", err)
	}
	if health.Status != "healthy" {
		t.Fatalf("Step 8: health status = %q, want %q", health.Status, "healthy")
	}
	t.Log("Step 8: Health check passed")

	// Step 9: Clean shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := env.controlServer.Stop(ctx); err != nil {
		t.Fatalf("Step 9: control server stop failed: %v", err)
	}
	env.controlServer = nil // Prevent double-stop in cleanup

	env.grpcServer.GracefulStop()
	env.grpcServer = nil // Prevent double-stop in cleanup
	t.Log("Step 9: Clean shutdown completed")

	t.Log("All Phase 1.5 integration tests passed!")
}

// TestPhase1_5_CertificateChainValidation verifies the certificate chain is properly validated.
func TestPhase1_5_CertificateChainValidation(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup(t)

	gameID, err := env.store.InitGameID(env.ctx)
	if err != nil {
		t.Fatalf("InitGameID failed: %v", err)
	}

	// Generate CA and certs
	ca, err := tls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}
	serverCert, err := tls.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert failed: %v", err)
	}
	clientCert, err := tls.GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert failed: %v", err)
	}

	// Verify server cert is signed by CA
	caPool := x509.NewCertPool()
	caPool.AddCert(ca.Certificate)

	chains, err := serverCert.Certificate.Verify(x509.VerifyOptions{
		Roots:     caPool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	if err != nil {
		t.Errorf("server cert verification failed: %v", err)
	}
	if len(chains) == 0 {
		t.Error("server cert has no valid chains to CA")
	}

	// Verify client cert is signed by CA
	chains, err = clientCert.Certificate.Verify(x509.VerifyOptions{
		Roots:     caPool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		t.Errorf("client cert verification failed: %v", err)
	}
	if len(chains) == 0 {
		t.Error("client cert has no valid chains to CA")
	}
}
