// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package control

import (
	"context"
	cryptotls "crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	tlscerts "github.com/holomush/holomush/internal/tls"
	controlv1 "github.com/holomush/holomush/pkg/proto/holomush/control/v1"
)

func TestGRPCServer_NewGRPCServer(t *testing.T) {
	s, err := NewGRPCServer("core", nil)
	require.NoError(t, err)

	assert.True(t, s.running.Load(), "server should be running after creation")
	assert.Equal(t, "core", s.component)
}

// TestGRPCServer_NewGRPCServer_EmptyComponent tests that NewGRPCServer returns
// an error when component is empty.
func TestGRPCServer_NewGRPCServer_EmptyComponent(t *testing.T) {
	_, err := NewGRPCServer("", nil)
	assert.Error(t, err, "NewGRPCServer() should fail with empty component")
}

func TestGRPCServer_Status_ReturnsCorrectInfo(t *testing.T) {
	s, err := NewGRPCServer("test-component", nil)
	require.NoError(t, err)
	// Wait a bit to ensure uptime > 0
	time.Sleep(10 * time.Millisecond)

	resp, err := s.Status(context.Background(), &controlv1.StatusRequest{})
	require.NoError(t, err)

	assert.True(t, resp.Running, "running should be true")

	//nolint:gosec // G115: PID values never exceed int32 range
	expectedPID := int32(os.Getpid())
	assert.Equal(t, expectedPID, resp.Pid)
	assert.GreaterOrEqual(t, resp.UptimeSeconds, int64(0), "uptime_seconds should be non-negative")
	assert.Equal(t, "test-component", resp.Component)
}

func TestGRPCServer_Shutdown_TriggersCallback(t *testing.T) {
	var shutdownCalled atomic.Bool

	s, err := NewGRPCServer("core", func() {
		shutdownCalled.Store(true)
	})
	require.NoError(t, err)

	resp, err := s.Shutdown(context.Background(), &controlv1.ShutdownRequest{Graceful: true})
	require.NoError(t, err)

	assert.Equal(t, "shutdown initiated", resp.Message)

	// Wait for async shutdown callback
	time.Sleep(50 * time.Millisecond)

	assert.True(t, shutdownCalled.Load(), "shutdown callback was not called")
}

func TestGRPCServer_Shutdown_NilCallback(t *testing.T) {
	s, err := NewGRPCServer("core", nil)
	require.NoError(t, err)

	// Should not panic with nil callback
	resp, err := s.Shutdown(context.Background(), &controlv1.ShutdownRequest{Graceful: true})
	require.NoError(t, err)

	assert.Equal(t, "shutdown initiated", resp.Message)
}

func TestGRPCServer_Stop_SetsRunningFalse(t *testing.T) {
	s, err := NewGRPCServer("test", nil)
	require.NoError(t, err)

	assert.True(t, s.running.Load(), "server should be running after creation")

	err = s.Stop(context.Background())
	assert.NoError(t, err)

	assert.False(t, s.running.Load(), "server should not be running after Stop()")
}

// TestGRPCServer_IntegrationWithInsecure tests the gRPC server with an insecure
// connection (no TLS) for easier testing without certificate setup.
func TestGRPCServer_IntegrationWithInsecure(t *testing.T) {
	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to find available port")
	addr := listener.Addr().String()
	_ = listener.Close()

	var shutdownCalled atomic.Bool
	s, err := NewGRPCServer("integration-test", func() {
		shutdownCalled.Store(true)
	})
	require.NoError(t, err)

	// Start server without TLS for testing
	serverListener, err := net.Listen("tcp", addr)
	require.NoError(t, err, "failed to listen")

	s.listener = serverListener
	s.grpcServer = grpc.NewServer()
	controlv1.RegisterControlServer(s.grpcServer, s)

	go func() {
		_ = s.grpcServer.Serve(serverListener)
	}()

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Create client
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err, "failed to create gRPC client")
	defer func() { _ = conn.Close() }()

	client := controlv1.NewControlClient(conn)

	t.Run("Status", func(t *testing.T) {
		resp, err := client.Status(context.Background(), &controlv1.StatusRequest{})
		require.NoError(t, err)

		assert.True(t, resp.Running, "running should be true")
		assert.Equal(t, "integration-test", resp.Component)
		assert.Greater(t, resp.Pid, int32(0), "pid should be positive")
		assert.GreaterOrEqual(t, resp.UptimeSeconds, int64(0), "uptime_seconds should be non-negative")
	})

	t.Run("Shutdown", func(t *testing.T) {
		resp, err := client.Shutdown(context.Background(), &controlv1.ShutdownRequest{Graceful: true})
		require.NoError(t, err)

		assert.Equal(t, "shutdown initiated", resp.Message)

		// Wait for async shutdown callback
		time.Sleep(50 * time.Millisecond)

		assert.True(t, shutdownCalled.Load(), "shutdown callback was not called")
	})
}

func TestGRPCServer_Stop_HandlesNilServer(t *testing.T) {
	s, err := NewGRPCServer("test", nil)
	require.NoError(t, err)
	// grpcServer and listener are nil

	err = s.Stop(context.Background())
	assert.NoError(t, err, "Stop should succeed with nil server components")
}

func TestGRPCServer_ConcurrentStatusRequests(t *testing.T) {
	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to find available port")
	addr := listener.Addr().String()
	_ = listener.Close()

	s, err := NewGRPCServer("concurrent-test", nil)
	require.NoError(t, err)

	// Start server without TLS for testing
	serverListener, err := net.Listen("tcp", addr)
	require.NoError(t, err, "failed to listen")

	s.listener = serverListener
	s.grpcServer = grpc.NewServer()
	controlv1.RegisterControlServer(s.grpcServer, s)

	go func() {
		_ = s.grpcServer.Serve(serverListener)
	}()

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Create client
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err, "failed to create gRPC client")
	defer func() { _ = conn.Close() }()

	client := controlv1.NewControlClient(conn)

	const numRequests = 50
	results := make(chan error, numRequests)

	// Launch concurrent status requests
	for i := 0; i < numRequests; i++ {
		go func() {
			resp, err := client.Status(context.Background(), &controlv1.StatusRequest{})
			if err != nil {
				results <- err
				return
			}

			if !resp.Running {
				results <- err
				return
			}

			results <- nil
		}()
	}

	// Collect results
	var errors []error
	for i := 0; i < numRequests; i++ {
		if err := <-results; err != nil {
			errors = append(errors, err)
		}
	}

	assert.Empty(t, errors, "concurrent requests failed")
}

func TestLoadControlServerTLS_FailsWithMissingCerts(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := LoadControlServerTLS(tmpDir, "core")
	require.Error(t, err, "LoadControlServerTLS should fail with missing certificates")
}

func TestLoadControlServerTLS_FailsWithInvalidCertContent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files with valid paths but invalid certificate content
	certPath := filepath.Join(tmpDir, "core.crt")
	keyPath := filepath.Join(tmpDir, "core.key")
	caPath := filepath.Join(tmpDir, "root-ca.crt")

	// Write corrupted/invalid certificate data
	err := os.WriteFile(certPath, []byte("not a valid certificate"), 0o600)
	require.NoError(t, err, "failed to write cert file")
	err = os.WriteFile(keyPath, []byte("not a valid key"), 0o600)
	require.NoError(t, err, "failed to write key file")
	err = os.WriteFile(caPath, []byte("not a valid CA"), 0o600)
	require.NoError(t, err, "failed to write CA file")

	_, err = LoadControlServerTLS(tmpDir, "core")
	require.Error(t, err, "LoadControlServerTLS should fail with invalid certificate content")

	// Error should be about certificate loading failure (oops wraps underlying error)
	assert.Contains(t, err.Error(), "PEM")
}

func TestLoadControlServerTLS_FailsWithMalformedCAPEM(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate valid server certificate first
	gameID := "test-malformed-ca"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	// Save valid server certificates
	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save certs")

	// Overwrite CA with malformed PEM data that looks like PEM but isn't valid
	caPath := filepath.Join(tmpDir, "root-ca.crt")
	malformedPEM := `-----BEGIN CERTIFICATE-----
not-valid-base64-data-here!!!
-----END CERTIFICATE-----`
	err = os.WriteFile(caPath, []byte(malformedPEM), 0o600)
	require.NoError(t, err, "failed to write malformed CA")

	_, err = LoadControlServerTLS(tmpDir, "core")
	require.Error(t, err, "LoadControlServerTLS should fail with malformed CA PEM")

	// Error should mention CA pool failure
	assert.Contains(t, err.Error(), "failed to add CA certificate to pool")
}

func TestLoadControlServerTLS_FailsWithEmptyCAPEM(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate valid server certificate first
	gameID := "test-empty-ca"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	// Save valid server certificates
	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save certs")

	// Overwrite CA with empty content (causes AppendCertsFromPEM to return false)
	caPath := filepath.Join(tmpDir, "root-ca.crt")
	err = os.WriteFile(caPath, []byte(""), 0o600)
	require.NoError(t, err, "failed to write empty CA")

	_, err = LoadControlServerTLS(tmpDir, "core")
	require.Error(t, err, "LoadControlServerTLS should fail with empty CA file")

	// Error should mention CA pool failure
	assert.Contains(t, err.Error(), "failed to add CA certificate to pool")
}

func TestLoadControlServerTLS_FailsWithMissingCAFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate valid server certificate
	gameID := "test-missing-ca"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	// Save server certificates using the tls package, then remove the CA
	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save certs")

	// Remove the CA file
	caPath := filepath.Join(tmpDir, "root-ca.crt")
	err = os.Remove(caPath)
	require.NoError(t, err, "failed to remove CA file")

	_, err = LoadControlServerTLS(tmpDir, "core")
	require.Error(t, err, "LoadControlServerTLS should fail with missing CA file")

	// Error should be about CA read failure (oops wraps underlying error)
	assert.Contains(t, err.Error(), "no such file or directory")
}

func TestLoadControlClientTLS_FailsWithMissingCerts(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := LoadControlClientTLS(tmpDir, "core", "test-game")
	require.Error(t, err, "LoadControlClientTLS should fail with missing certificates")
}

func TestExtractGameIDFromCA_FailsWithMissingCA(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := ExtractGameIDFromCA(tmpDir)
	require.Error(t, err, "ExtractGameIDFromCA should fail with missing CA certificate")
}

func TestExtractGameIDFromCA_FailsWithInvalidPEM(t *testing.T) {
	tmpDir := t.TempDir()

	// Write invalid PEM data
	caPath := tmpDir + "/root-ca.crt"
	err := os.WriteFile(caPath, []byte("not valid PEM data"), 0o600)
	require.NoError(t, err, "failed to write test file")

	_, err = ExtractGameIDFromCA(tmpDir)
	require.Error(t, err, "ExtractGameIDFromCA should fail with invalid PEM")
}

func TestExtractGameIDFromCA_FailsWithWrongCNPrefix(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a PEM certificate with wrong CN prefix
	// We'll create a self-signed cert with wrong CN for testing
	caPath := tmpDir + "/root-ca.crt"
	// Use a valid PEM structure but the CN extraction should fail
	pemData := `-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIRAJJ3z4EJmJpMcOGM35xdMOIwCgYIKoZIzj0EAwIwFTET
MBEGA1UEAxMKV3JvbmcgUHJlZml4MB4XDTI2MDExNzIxMTIwM1oXDTM2MDExNjIx
MTIwM1owFTETMBEGA1UEAxMKV3JvbmcgUHJlZml4MFkwEwYHKoZIzj0CAQYIKoZI
zj0DAQcDQgAE4yNhZUGTsmZ+eHdNIRPPbR67/CQdMy0gUUGEQ/jvqI0mDKhAaJZH
5PJr2rDqKn/pPIGlNZIcM1uB0yGUCVYC1qNFMEMwDgYDVR0PAQH/BAQDAgEGMBIG
A1UdEwEB/wQIMAYBAf8CAQEwHQYDVR0OBBYEFC+WzLPcVjgMRBKQmFjCCRh5jPvE
MAoGCCqGSM49BAMCA0gAMEUCIFJdkxsZ0I1p5tSyPgMqsyLTQI+bfK0hv0GJm7Yf
Rg2YAiEA2c7q5J3wBxjNn6LpnQXIhwP6NLQxNIuMqI8B9XK3Fkk=
-----END CERTIFICATE-----`
	err := os.WriteFile(caPath, []byte(pemData), 0o600)
	require.NoError(t, err, "failed to write test file")

	_, err = ExtractGameIDFromCA(tmpDir)
	require.Error(t, err, "ExtractGameIDFromCA should fail with wrong CN prefix")

	// Check error message is not empty
	assert.NotEmpty(t, err.Error(), "error should have a message")
}

func TestExtractGameIDFromCA_ExtractsCorrectGameID(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate a proper CA using the tls package
	expectedGameID := "test-game-abc123"
	ca, err := tlscerts.GenerateCA(expectedGameID)
	require.NoError(t, err, "failed to generate CA")

	// Save the CA to the temp directory
	err = tlscerts.SaveCertificates(tmpDir, ca, nil)
	require.NoError(t, err, "failed to save CA")

	// Extract the game ID
	gotGameID, err := ExtractGameIDFromCA(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, expectedGameID, gotGameID)
}

func TestLoadControlClientTLS_WithValidCerts(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate CA and client certificate
	gameID := "test-game-xyz"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	// Generate server cert (core) - client will verify against this
	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	// Generate client cert (gateway)
	clientCert, err := tlscerts.GenerateClientCert(ca, "gateway")
	require.NoError(t, err, "failed to generate client cert")

	// Save all certs
	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save server certs")
	err = tlscerts.SaveClientCert(tmpDir, clientCert)
	require.NoError(t, err, "failed to save client cert")

	// Load client TLS config
	config, err := LoadControlClientTLS(tmpDir, "gateway", gameID)
	require.NoError(t, err)

	require.NotNil(t, config, "LoadControlClientTLS() returned nil config")

	// Verify ServerName is set correctly
	expectedServerName := "holomush-" + gameID
	assert.Equal(t, expectedServerName, config.ServerName)

	// Verify TLS 1.3 minimum
	assert.Equal(t, uint16(0x0304), config.MinVersion, "MinVersion should be TLS 1.3")
}

// TestGRPCServer_Start_FailsOnInvalidAddress tests that Start() returns an error
// when the address is invalid or already in use.
func TestGRPCServer_Start_FailsOnInvalidAddress(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate valid certificates
	gameID := "test-listen-fail"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save certs")

	tlsConfig, err := LoadControlServerTLS(tmpDir, "core")
	require.NoError(t, err, "failed to load TLS config")

	s, err := NewGRPCServer("test", nil)
	require.NoError(t, err)

	// Try to start on an invalid address
	errCh, err := s.Start("invalid-address:99999999", tlsConfig)
	if err == nil {
		// Clean up if it somehow succeeded
		if errCh != nil {
			_ = s.Stop(context.Background())
		}
	}
	assert.Error(t, err, "Start() should fail with invalid address")
	assert.Nil(t, errCh, "Start() should return nil error channel on failure")
}

// TestGRPCServer_Start_ReturnsErrorChannel tests that Start() returns an error channel
// that can be used to detect server failures.
func TestGRPCServer_Start_ReturnsErrorChannel(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate valid certificates
	gameID := "test-errch"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save certs")

	tlsConfig, err := LoadControlServerTLS(tmpDir, "core")
	require.NoError(t, err, "failed to load TLS config")

	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to find available port")
	addr := listener.Addr().String()
	_ = listener.Close()

	s, err := NewGRPCServer("test", nil)
	require.NoError(t, err)

	// Start should return an error channel
	errCh, err := s.Start(addr, tlsConfig)
	require.NoError(t, err)
	require.NotNil(t, errCh, "Start() should return a non-nil error channel")

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	// Server should be running, errCh should be open but not have an error yet
	select {
	case err := <-errCh:
		// This means server stopped immediately - could be an error
		require.NoError(t, err, "unexpected immediate error")
	case <-time.After(100 * time.Millisecond):
		// Good - server is running
	}
}

// TestGRPCServer_Start_PropagatesServerError tests that when the gRPC server
// encounters an error, it is sent to the error channel.
func TestGRPCServer_Start_PropagatesServerError(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate valid certificates
	gameID := "test-prop-err"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save certs")

	tlsConfig, err := LoadControlServerTLS(tmpDir, "core")
	require.NoError(t, err, "failed to load TLS config")

	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to find available port")
	addr := listener.Addr().String()
	_ = listener.Close()

	s, err := NewGRPCServer("test", nil)
	require.NoError(t, err)

	errCh, err := s.Start(addr, tlsConfig)
	require.NoError(t, err)

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Force close the listener to trigger a server error
	if s.listener != nil {
		_ = s.listener.Close()
	}

	// Now the error channel should receive an error (or nil if graceful stop)
	select {
	case err := <-errCh:
		// Got the notification - server stopped
		// Note: The error might be nil if GracefulStop was called, or non-nil if listener closed
		t.Logf("received from error channel: %v", err)
	case <-time.After(2 * time.Second):
		t.Error("expected to receive from error channel after listener closed")
	}
}

// TestGRPCServer_Integration_mTLS tests full end-to-end mTLS handshake between
// client and server using generated certificates.
func TestGRPCServer_Integration_mTLS(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate CA and certificates
	gameID := "mtls-integration-test"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	clientCert, err := tlscerts.GenerateClientCert(ca, "gateway")
	require.NoError(t, err, "failed to generate client cert")

	// Save certificates
	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save server certs")
	err = tlscerts.SaveClientCert(tmpDir, clientCert)
	require.NoError(t, err, "failed to save client cert")

	// Load TLS configs
	serverTLSConfig, err := LoadControlServerTLS(tmpDir, "core")
	require.NoError(t, err, "failed to load server TLS config")

	clientTLSConfig, err := LoadControlClientTLS(tmpDir, "gateway", gameID)
	require.NoError(t, err, "failed to load client TLS config")

	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to find available port")
	addr := listener.Addr().String()
	_ = listener.Close()

	// Start server with mTLS
	var shutdownCalled atomic.Bool
	s, err := NewGRPCServer("core", func() {
		shutdownCalled.Store(true)
	})
	require.NoError(t, err)

	errCh, err := s.Start(addr, serverTLSConfig)
	require.NoError(t, err)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
		// Drain error channel
		<-errCh
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Connect client with mTLS
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(credentials.NewTLS(clientTLSConfig)))
	require.NoError(t, err, "failed to create gRPC client")
	defer func() { _ = conn.Close() }()

	client := controlv1.NewControlClient(conn)

	t.Run("Status_via_mTLS", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp, err := client.Status(ctx, &controlv1.StatusRequest{})
		require.NoError(t, err)

		assert.True(t, resp.Running, "running should be true")
		assert.Equal(t, "core", resp.Component)
		assert.Greater(t, resp.Pid, int32(0), "pid should be positive")
	})

	t.Run("Shutdown_via_mTLS", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp, err := client.Shutdown(ctx, &controlv1.ShutdownRequest{Graceful: true})
		require.NoError(t, err)

		assert.Equal(t, "shutdown initiated", resp.Message)

		// Wait for async shutdown callback
		time.Sleep(50 * time.Millisecond)

		assert.True(t, shutdownCalled.Load(), "shutdown callback was not called")
	})
}

// TestGRPCServer_mTLS_RejectsUnauthenticatedClient tests that the server rejects
// clients that don't present valid client certificates.
func TestGRPCServer_mTLS_RejectsUnauthenticatedClient(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate CA and server certificate
	gameID := "mtls-reject-test"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	// Save certificates (no client cert saved)
	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save server certs")

	// Load server TLS config
	serverTLSConfig, err := LoadControlServerTLS(tmpDir, "core")
	require.NoError(t, err, "failed to load server TLS config")

	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to find available port")
	addr := listener.Addr().String()
	_ = listener.Close()

	// Start server with mTLS
	s, err := NewGRPCServer("core", nil)
	require.NoError(t, err)

	errCh, err := s.Start(addr, serverTLSConfig)
	require.NoError(t, err)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
		// Drain error channel
		<-errCh
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Try to connect without client certificate - should fail
	// Create a TLS config that trusts the CA but has no client cert
	caCertPEM, err := os.ReadFile(filepath.Join(tmpDir, "root-ca.crt"))
	require.NoError(t, err, "failed to read CA cert")
	caPool := x509.NewCertPool()
	require.True(t, caPool.AppendCertsFromPEM(caCertPEM), "failed to add CA to pool")

	noClientCertTLS := &cryptotls.Config{
		RootCAs:    caPool,
		ServerName: "holomush-" + gameID,
		MinVersion: cryptotls.VersionTLS13,
		// Note: No client certificate
	}

	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(credentials.NewTLS(noClientCertTLS)))
	require.NoError(t, err, "failed to create gRPC client")
	defer func() { _ = conn.Close() }()

	client := controlv1.NewControlClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This should fail because no client certificate was provided
	_, err = client.Status(ctx, &controlv1.StatusRequest{})
	assert.Error(t, err, "expected error when connecting without client certificate")
	t.Logf("correctly rejected: %v", err)
}

// TestGRPCServer_Start_ErrorChannelOnGracefulStop tests that the error channel
// receives nil when the server is gracefully stopped.
func TestGRPCServer_Start_ErrorChannelOnGracefulStop(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate valid certificates
	gameID := "test-stop-ch"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save certs")

	tlsConfig, err := LoadControlServerTLS(tmpDir, "core")
	require.NoError(t, err, "failed to load TLS config")

	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to find available port")
	addr := listener.Addr().String()
	_ = listener.Close()

	s, err := NewGRPCServer("test", nil)
	require.NoError(t, err)

	errCh, err := s.Start(addr, tlsConfig)
	require.NoError(t, err)

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Gracefully stop the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = s.Stop(ctx)
	require.NoError(t, err)

	// Error channel should receive nil on graceful stop
	select {
	case err := <-errCh:
		// GracefulStop causes Serve to return nil
		assert.NoError(t, err, "expected nil error on graceful stop")
	case <-time.After(2 * time.Second):
		t.Error("expected to receive from error channel after Stop()")
	}
}

// TestGRPCServer_Start_DoubleStartReturnsError tests that calling Start() twice
// returns an error instead of leaking the first listener (e55.57).
func TestGRPCServer_Start_DoubleStartReturnsError(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate valid certificates
	gameID := "test-double-start"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save certs")

	tlsConfig, err := LoadControlServerTLS(tmpDir, "core")
	require.NoError(t, err, "failed to load TLS config")

	// Find two available ports
	listener1, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to find available port")
	addr1 := listener1.Addr().String()
	_ = listener1.Close()

	listener2, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to find available port")
	addr2 := listener2.Addr().String()
	_ = listener2.Close()

	s, err := NewGRPCServer("test", nil)
	require.NoError(t, err)

	// First start should succeed
	errCh, err := s.Start(addr1, tlsConfig)
	require.NoError(t, err, "First Start() should succeed")

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
		<-errCh
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Second start should fail
	errCh2, err := s.Start(addr2, tlsConfig)
	if err == nil && errCh2 != nil {
		// Clean up if it somehow succeeded
		_ = s.Stop(context.Background())
	}
	assert.Error(t, err, "Second Start() should fail when server is already running")
	assert.Nil(t, errCh2, "Second Start() should return nil error channel on failure")
}

// TestGRPCServer_Stop_ConcurrentCalls tests that calling Stop() concurrently
// does not cause a race condition (e55.68).
func TestGRPCServer_Stop_ConcurrentCalls(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate valid certificates
	gameID := "test-concurrent-stop"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save certs")

	tlsConfig, err := LoadControlServerTLS(tmpDir, "core")
	require.NoError(t, err, "failed to load TLS config")

	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to find available port")
	addr := listener.Addr().String()
	_ = listener.Close()

	s, err := NewGRPCServer("test", nil)
	require.NoError(t, err)

	errCh, err := s.Start(addr, tlsConfig)
	require.NoError(t, err)

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Call Stop concurrently from multiple goroutines
	const numCallers = 10
	var wg sync.WaitGroup
	wg.Add(numCallers)

	for i := 0; i < numCallers; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.Stop(ctx)
		}()
	}

	wg.Wait()

	// After all Stop calls complete, running should be false
	assert.False(t, s.running.Load(), "server should not be running after concurrent Stop() calls")

	// Drain error channel
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Error("error channel should have received value after Stop()")
	}
}

// TestGRPCServer_Stop_DuringStart tests that calling Stop() during Start()
// initialization does not cause a race condition (e55.95).
func TestGRPCServer_Stop_DuringStart(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate valid certificates
	gameID := "test-stop-during-start"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save certs")

	tlsConfig, err := LoadControlServerTLS(tmpDir, "core")
	require.NoError(t, err, "failed to load TLS config")

	// Run multiple iterations to increase chance of hitting race windows
	const iterations = 100
	for i := 0; i < iterations; i++ {
		// Find available port for each iteration
		listener, err := net.Listen("tcp", "localhost:0")
		require.NoError(t, err, "iteration %d: failed to find available port", i)
		addr := listener.Addr().String()
		_ = listener.Close()

		s, err := NewGRPCServer("test", nil)
		require.NoError(t, err, "iteration %d: NewGRPCServer() failed", i)

		// Start server and immediately try to stop it
		var wg sync.WaitGroup
		wg.Add(2)

		var errCh <-chan error
		var startErr error

		go func() {
			defer wg.Done()
			errCh, startErr = s.Start(addr, tlsConfig)
		}()

		go func() {
			defer wg.Done()
			// Small random delay to vary timing
			time.Sleep(time.Duration(i%10) * time.Microsecond)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.Stop(ctx)
		}()

		wg.Wait()

		// Either start succeeded and we stopped it, or start failed - both are OK
		// The key is that we don't panic or have a data race
		if startErr == nil && errCh != nil {
			// Drain error channel if start succeeded
			select {
			case <-errCh:
			case <-time.After(2 * time.Second):
				// Server might already be stopped
			}
		}

		// After both operations complete, server should not be running
		assert.False(t, s.running.Load(), "iteration %d: server should not be running after Stop()", i)
	}
}

// TestGRPCServer_Stop_RunningStateAfterGracefulStop tests that running state is
// false only after GracefulStop completes, not before (e55.59).
func TestGRPCServer_Stop_RunningStateAfterGracefulStop(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate valid certificates
	gameID := "test-stop-timing"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "failed to save certs")

	tlsConfig, err := LoadControlServerTLS(tmpDir, "core")
	require.NoError(t, err, "failed to load TLS config")

	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to find available port")
	addr := listener.Addr().String()
	_ = listener.Close()

	s, err := NewGRPCServer("test", nil)
	require.NoError(t, err)

	errCh, err := s.Start(addr, tlsConfig)
	require.NoError(t, err)

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Verify server is running
	require.True(t, s.running.Load(), "server should be running before Stop()")

	// Stop the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = s.Stop(ctx)
	require.NoError(t, err)

	// After Stop returns, running should be false
	assert.False(t, s.running.Load(), "server should not be running after Stop() returns")

	// Drain error channel
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Error("error channel should have received value after Stop()")
	}
}
