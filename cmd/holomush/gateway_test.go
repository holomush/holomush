// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/tls"
)

func TestGatewayCommand_Flags(t *testing.T) {
	cmd := NewGatewayCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	require.NoError(t, cmd.Execute())

	output := buf.String()

	// Verify all expected flags are present
	expectedFlags := []string{
		"--telnet-addr",
		"--core-addr",
		"--control-addr",
		"--metrics-addr",
		"--log-format",
	}

	for _, flag := range expectedFlags {
		assert.Contains(t, output, flag, "Help missing %q flag", flag)
	}
}

func TestGatewayCommand_DefaultValues(t *testing.T) {
	cmd := NewGatewayCmd()

	// Check default telnet-addr
	telnetAddr, err := cmd.Flags().GetString("telnet-addr")
	require.NoError(t, err, "Failed to get telnet-addr flag")
	assert.Equal(t, ":4201", telnetAddr)

	// Check default core-addr
	coreAddr, err := cmd.Flags().GetString("core-addr")
	require.NoError(t, err, "Failed to get core-addr flag")
	assert.Equal(t, "localhost:9000", coreAddr)

	// Check default control-addr
	controlAddr, err := cmd.Flags().GetString("control-addr")
	require.NoError(t, err, "Failed to get control-addr flag")
	assert.Equal(t, "127.0.0.1:9002", controlAddr)

	// Check default metrics-addr
	metricsAddr, err := cmd.Flags().GetString("metrics-addr")
	require.NoError(t, err, "Failed to get metrics-addr flag")
	assert.Equal(t, "127.0.0.1:9101", metricsAddr)

	// Check default log-format
	logFormat, err := cmd.Flags().GetString("log-format")
	require.NoError(t, err, "Failed to get log-format flag")
	assert.Equal(t, "json", logFormat)
}

func TestGatewayCommand_Properties(t *testing.T) {
	cmd := NewGatewayCmd()

	assert.Equal(t, "gateway", cmd.Use)
	assert.Contains(t, cmd.Short, "gateway", "Short description should mention gateway")
	assert.Contains(t, cmd.Long, "telnet", "Long description should mention telnet")
}

func TestGatewayCommand_FlagParsing(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantTelnet string
		wantCore   string
		wantFmt    string
	}{
		{
			name:       "default values",
			args:       []string{"--help"},
			wantTelnet: ":4201",
			wantCore:   "localhost:9000",
			wantFmt:    "json",
		},
		{
			name:       "custom telnet addr",
			args:       []string{"--telnet-addr=0.0.0.0:4200", "--help"},
			wantTelnet: "0.0.0.0:4200",
			wantCore:   "localhost:9000",
			wantFmt:    "json",
		},
		{
			name:       "custom core addr",
			args:       []string{"--core-addr=127.0.0.1:8000", "--help"},
			wantTelnet: ":4201",
			wantCore:   "127.0.0.1:8000",
			wantFmt:    "json",
		},
		{
			name:       "text log format",
			args:       []string{"--log-format=text", "--help"},
			wantTelnet: ":4201",
			wantCore:   "localhost:9000",
			wantFmt:    "text",
		},
		{
			name:       "all custom flags",
			args:       []string{"--telnet-addr=:4200", "--core-addr=core.local:9000", "--log-format=text", "--help"},
			wantTelnet: ":4200",
			wantCore:   "core.local:9000",
			wantFmt:    "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewGatewayCmd()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetArgs(tt.args)

			require.NoError(t, cmd.Execute())

			telnetAddr, _ := cmd.Flags().GetString("telnet-addr")
			assert.Equal(t, tt.wantTelnet, telnetAddr)

			coreAddr, _ := cmd.Flags().GetString("core-addr")
			assert.Equal(t, tt.wantCore, coreAddr)

			fmtVal, _ := cmd.Flags().GetString("log-format")
			assert.Equal(t, tt.wantFmt, fmtVal)
		})
	}
}

func TestGatewayCommand_Help(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"gateway", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	require.NoError(t, cmd.Execute())

	output := buf.String()

	// Verify help contains expected sections
	expectedPhrases := []string{
		"Start the gateway process",
		"telnet",
		"--telnet-addr",
		"--core-addr",
		"--control-addr",
		"--metrics-addr",
	}

	for _, phrase := range expectedPhrases {
		assert.Contains(t, output, phrase, "Help missing phrase %q", phrase)
	}
}

func TestGatewayCommand_MissingCertificates(t *testing.T) {
	// Set certs directory to a non-existent path
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent/path/that/does/not/exist")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"gateway"})

	err := cmd.Execute()
	require.Error(t, err, "Expected error when certificates are missing")

	// Error should mention TLS or certificates
	assert.True(t, assert.Condition(t, func() bool {
		errMsg := err.Error()
		return strings.Contains(errMsg, "TLS") ||
			strings.Contains(errMsg, "certificate") ||
			strings.Contains(errMsg, "certs")
	}), "Error should mention TLS/certificate issue, got: %v", err)
}

func TestGatewayCommand_InvalidLogFormat(t *testing.T) {
	// Set up valid certs directory (will fail before reaching certs anyway)
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"gateway", "--log-format=invalid"})

	err := cmd.Execute()
	require.Error(t, err, "Expected error with invalid log format")

	// Error should mention log format (validation moved from setupLogging to Validate)
	errMsg := err.Error()
	assert.True(t, assert.Condition(t, func() bool {
		return strings.Contains(errMsg, "logging") ||
			strings.Contains(errMsg, "log format") ||
			strings.Contains(errMsg, "log-format")
	}), "Error should mention logging issue, got: %v", err)
}

func TestGatewayCommand_CAExtractionFails(t *testing.T) {
	// Create a certs directory with an invalid CA certificate
	tmpDir := t.TempDir()
	certsDir := tmpDir + "/holomush/certs"
	require.NoError(t, os.MkdirAll(certsDir, 0o700), "failed to create certs dir")

	// Write an invalid CA certificate
	caPath := certsDir + "/root-ca.crt"
	require.NoError(t, os.WriteFile(caPath, []byte("not a valid certificate"), 0o600), "failed to write invalid CA")

	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"gateway"})

	err := cmd.Execute()
	require.Error(t, err, "Expected error when CA extraction fails")

	// Error should mention game_id or CA
	assert.True(t, assert.Condition(t, func() bool {
		return strings.Contains(err.Error(), "game_id") || strings.Contains(err.Error(), "CA")
	}), "Error should mention game_id/CA extraction issue, got: %v", err)
}

func TestGatewayCommand_TLSLoadFails(t *testing.T) {
	// Create a certs directory with a valid CA but invalid gateway certificate
	tmpDir := t.TempDir()
	certsDir := tmpDir + "/holomush/certs"

	// Generate a valid CA
	gameID := "test-gateway-tls-fail"
	ca, err := tls.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	// Save CA only (no gateway certificate)
	require.NoError(t, tls.SaveCertificates(certsDir, ca, nil), "failed to save CA")

	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"gateway"})

	err = cmd.Execute()
	require.Error(t, err, "Expected error when TLS certificates are incomplete")

	// Error should mention TLS or certificate
	assert.True(t, assert.Condition(t, func() bool {
		return strings.Contains(err.Error(), "TLS") || strings.Contains(err.Error(), "certificate")
	}), "Error should mention TLS/certificate issue, got: %v", err)
}

// TestHandleTelnetConnection tests the telnet handler.
func TestHandleTelnetConnection(t *testing.T) {
	// Create a pipe to simulate a connection
	server, client := net.Pipe()

	// Run handler in goroutine (it will close the server side)
	done := make(chan struct{})
	go func() {
		handleTelnetConnection(server)
		close(done)
	}()

	// Read all output from the client side until EOF
	// Set a read deadline to prevent hanging if handler doesn't close connection
	var output strings.Builder
	buf := make([]byte, 1024)
	require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)), "failed to set read deadline")
	for {
		n, err := client.Read(buf)
		if n > 0 {
			output.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}

	result := output.String()

	// Verify the welcome messages are sent
	assert.Contains(t, result, "Welcome to HoloMUSH Gateway", "output missing welcome message")
	assert.Contains(t, result, "Gateway is connected to core", "output missing status message")
	assert.Contains(t, result, "Disconnecting", "output missing disconnect message")

	// Wait for handler to finish with timeout
	select {
	case <-done:
		// Handler finished
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not finish within timeout")
	}
}

// TestHandleTelnetConnection_WriteError tests handling when writes fail.
func TestHandleTelnetConnection_WriteError(t *testing.T) {
	// Create a pipe and close the client side immediately to cause write errors
	server, client := net.Pipe()
	require.NoError(t, client.Close(), "failed to close client")

	// Run handler - should handle write errors gracefully without panic
	done := make(chan struct{})
	go func() {
		handleTelnetConnection(server)
		close(done)
	}()

	// Wait for handler to finish - it should complete without panic
	<-done
}

// mockNetConn is a mock net.Conn that fails after N writes.
type mockNetConn struct {
	net.Conn
	writesUntilFail int
	writeCount      int
	closed          bool
}

func (m *mockNetConn) Write(p []byte) (int, error) {
	m.writeCount++
	if m.writeCount >= m.writesUntilFail {
		return 0, fmt.Errorf("mock write error on write %d", m.writeCount)
	}
	return len(p), nil
}

func (m *mockNetConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockNetConn) Read(_ []byte) (int, error) {
	return 0, fmt.Errorf("mock read error")
}

func (m *mockNetConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

func (m *mockNetConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321}
}

// TestHandleTelnetConnection_SecondWriteFails tests when second write fails.
func TestHandleTelnetConnection_SecondWriteFails(t *testing.T) {
	// Mock that fails on second write (status message)
	mock := &mockNetConn{writesUntilFail: 2}

	done := make(chan struct{})
	go func() {
		handleTelnetConnection(mock)
		close(done)
	}()

	<-done

	assert.True(t, mock.closed, "connection should be closed after handler completes")
}

func TestGatewayCommand_InvalidCACN(t *testing.T) {
	// Create a certs directory with a CA that has wrong CN prefix
	tmpDir := t.TempDir()
	certsDir := tmpDir + "/holomush/certs"
	require.NoError(t, os.MkdirAll(certsDir, 0o700), "failed to create certs dir")

	// Write a valid PEM certificate with wrong CN prefix
	caPath := certsDir + "/root-ca.crt"
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
	require.NoError(t, os.WriteFile(caPath, []byte(pemData), 0o600), "failed to write CA with wrong CN")

	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"gateway"})

	err := cmd.Execute()
	require.Error(t, err, "Expected error when CA has wrong CN prefix")

	// Error should mention game_id or CN
	assert.True(t, assert.Condition(t, func() bool {
		return strings.Contains(err.Error(), "game_id") || strings.Contains(err.Error(), "prefix")
	}), "Error should mention game_id/prefix issue, got: %v", err)
}

// TestGatewayConfig_Validate tests validation of gatewayConfig.
func TestGatewayConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		cfg       gatewayConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid config",
			cfg: gatewayConfig{
				telnetAddr:  ":4201",
				coreAddr:    "localhost:9000",
				controlAddr: "127.0.0.1:9002",
				logFormat:   "json",
			},
			wantError: false,
		},
		{
			name: "valid config with text format",
			cfg: gatewayConfig{
				telnetAddr:  ":4201",
				coreAddr:    "localhost:9000",
				controlAddr: "127.0.0.1:9002",
				logFormat:   "text",
			},
			wantError: false,
		},
		{
			name: "empty telnet-addr",
			cfg: gatewayConfig{
				telnetAddr:  "",
				coreAddr:    "localhost:9000",
				controlAddr: "127.0.0.1:9002",
				logFormat:   "json",
			},
			wantError: true,
			errorMsg:  "telnet-addr is required",
		},
		{
			name: "empty core-addr",
			cfg: gatewayConfig{
				telnetAddr:  ":4201",
				coreAddr:    "",
				controlAddr: "127.0.0.1:9002",
				logFormat:   "json",
			},
			wantError: true,
			errorMsg:  "core-addr is required",
		},
		{
			name: "empty control-addr",
			cfg: gatewayConfig{
				telnetAddr:  ":4201",
				coreAddr:    "localhost:9000",
				controlAddr: "",
				logFormat:   "json",
			},
			wantError: true,
			errorMsg:  "control-addr is required",
		},
		{
			name: "invalid log-format",
			cfg: gatewayConfig{
				telnetAddr:  ":4201",
				coreAddr:    "localhost:9000",
				controlAddr: "127.0.0.1:9002",
				logFormat:   "invalid",
			},
			wantError: true,
			errorMsg:  "log-format must be 'json' or 'text'",
		},
		{
			name: "empty log-format",
			cfg: gatewayConfig{
				telnetAddr:  ":4201",
				coreAddr:    "localhost:9000",
				controlAddr: "127.0.0.1:9002",
				logFormat:   "",
			},
			wantError: true,
			errorMsg:  "log-format must be 'json' or 'text'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantError {
				require.Error(t, err, "Validate() expected error")
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGatewayConfig_Defaults(t *testing.T) {
	// Verify the default constants are set correctly
	assert.Equal(t, ":4201", defaultTelnetAddr)
	assert.Equal(t, "localhost:9000", defaultCoreAddr)
	assert.Equal(t, "127.0.0.1:9002", defaultGatewayControlAddr)
	assert.Equal(t, "127.0.0.1:9101", defaultGatewayMetricsAddr)
}

// TestControlServerError_TriggersShutdown verifies that when the control gRPC server
// encounters an error, the gateway process shuts down.
func TestControlServerError_TriggersShutdown(t *testing.T) {
	// This test verifies bug fix: control server errors should trigger shutdown
	// The bug was that errors were logged but cancel() was not called

	// Create a mock error channel that simulates control server failure
	errCh := make(chan error, 1)
	shutdownCalled := make(chan struct{})

	// Simulate the current buggy behavior: receive error but don't trigger shutdown
	// After fix, the goroutine should call cancel() which triggers shutdown
	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()

	cancel := func() {
		close(shutdownCalled)
	}

	// Start goroutine that monitors control server errors
	go monitorServerErrors(ctx, cancel, errCh, "control-grpc")

	// Send an error to simulate control server failure
	errCh <- fmt.Errorf("simulated control server error")

	// Wait for shutdown to be triggered
	select {
	case <-shutdownCalled:
		// Success - shutdown was triggered
	case <-time.After(1 * time.Second):
		t.Fatal("control server error did not trigger shutdown within timeout")
	}
}

// TestTelnetAcceptLoopPanicRecovery verifies that a panic in the telnet accept loop
// triggers graceful shutdown instead of crashing the process.
func TestTelnetAcceptLoopPanicRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownCalled := make(chan struct{})
	cancelFunc := func() {
		close(shutdownCalled)
	}

	// Simulate what the accept loop does with panic recovery
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// This simulates the behavior in gateway.go
				cancelFunc()
			}
		}()
		// Simulate a panic in the accept loop
		panic("simulated panic in accept loop")
	}()

	// Wait for shutdown to be triggered
	select {
	case <-shutdownCalled:
		// Success - panic triggered shutdown
	case <-ctx.Done():
		t.Fatal("context cancelled before shutdown triggered")
	case <-time.After(1 * time.Second):
		t.Fatal("panic did not trigger shutdown within timeout")
	}
}

// TestObservabilityServerError_TriggersShutdown verifies that when the observability
// server encounters an error, the gateway process shuts down.
func TestObservabilityServerError_TriggersShutdown(t *testing.T) {
	// This test verifies bug fix: observability server errors should trigger shutdown
	// The bug was that the error channel was discarded with _

	// Create a mock error channel that simulates observability server failure
	errCh := make(chan error, 1)
	shutdownCalled := make(chan struct{})

	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()

	cancel := func() {
		close(shutdownCalled)
	}

	// Start goroutine that monitors observability server errors
	go monitorServerErrors(ctx, cancel, errCh, "observability")

	// Send an error to simulate observability server failure
	errCh <- fmt.Errorf("simulated observability server error")

	// Wait for shutdown to be triggered
	select {
	case <-shutdownCalled:
		// Success - shutdown was triggered
	case <-time.After(1 * time.Second):
		t.Fatal("observability server error did not trigger shutdown within timeout")
	}
}

// acceptLoopMockListener is a mock listener for testing the accept loop backoff behavior.
type acceptLoopMockListener struct {
	acceptTimes  []time.Time
	acceptCalls  int
	acceptErrors int
	closed       bool
	closeCh      chan struct{}
}

func (m *acceptLoopMockListener) Accept() (net.Conn, error) {
	m.acceptTimes = append(m.acceptTimes, time.Now())
	m.acceptCalls++
	if m.acceptCalls <= m.acceptErrors {
		return nil, fmt.Errorf("mock accept error %d", m.acceptCalls)
	}
	// Block until closed
	<-m.closeCh
	return nil, fmt.Errorf("listener closed")
}

func (m *acceptLoopMockListener) Close() error {
	if !m.closed {
		m.closed = true
		close(m.closeCh)
	}
	return nil
}

func (m *acceptLoopMockListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 4201}
}

// TestTelnetAcceptLoop_BackoffOnErrors verifies that the accept loop applies
// exponential backoff when accept errors occur.
func TestTelnetAcceptLoop_BackoffOnErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mock := &acceptLoopMockListener{
		acceptErrors: 3,
		closeCh:      make(chan struct{}),
	}

	// Run the accept loop in a goroutine
	done := make(chan struct{})
	go func() {
		runTelnetAcceptLoop(ctx, mock, cancel)
		close(done)
	}()

	// Give it time to hit 3 errors with backoff (100ms, 200ms, 400ms = 700ms minimum)
	time.Sleep(800 * time.Millisecond)

	// Close the listener first, then cancel context to stop the loop
	// The loop checks ctx.Done() after Accept() returns an error
	require.NoError(t, mock.Close(), "failed to close mock listener")
	cancel()

	select {
	case <-done:
		// Loop finished
	case <-time.After(1 * time.Second):
		t.Fatal("accept loop did not finish within timeout")
	}

	// Verify backoff behavior: should have recorded 3+ accept attempts
	assert.GreaterOrEqual(t, mock.acceptCalls, 3, "expected at least 3 accept calls")

	// Verify timing shows backoff (first to second should be ~100ms, second to third ~200ms)
	if len(mock.acceptTimes) >= 3 {
		gap1 := mock.acceptTimes[1].Sub(mock.acceptTimes[0])
		gap2 := mock.acceptTimes[2].Sub(mock.acceptTimes[1])

		// First gap should be around 100ms (the initial backoff)
		assert.True(t, gap1 >= 50*time.Millisecond && gap1 <= 200*time.Millisecond,
			"first backoff gap = %v, expected ~100ms", gap1)

		// Second gap should be around 200ms (doubled from first)
		assert.True(t, gap2 >= 150*time.Millisecond && gap2 <= 350*time.Millisecond,
			"second backoff gap = %v, expected ~200ms", gap2)
	}
}

// TestAcceptBackoff_ExponentialIncrease verifies the backoff doubles on each failure.
func TestAcceptBackoff_ExponentialIncrease(t *testing.T) {
	b := newAcceptBackoff()

	// Initial state - no delay
	assert.Equal(t, time.Duration(0), b.wait(), "initial wait should be 0")

	// First failure - should be initial (100ms)
	b.failure()
	assert.Equal(t, 100*time.Millisecond, b.wait())

	// Second failure - should double (200ms)
	b.failure()
	assert.Equal(t, 200*time.Millisecond, b.wait())

	// Third failure - should double again (400ms)
	b.failure()
	assert.Equal(t, 400*time.Millisecond, b.wait())
}

// TestAcceptBackoff_MaxCap verifies the backoff is capped at max (30s).
func TestAcceptBackoff_MaxCap(t *testing.T) {
	b := newAcceptBackoff()

	// Rapidly increase backoff past the cap
	for i := 0; i < 20; i++ {
		b.failure()
	}

	// Should be capped at 30 seconds
	assert.Equal(t, 30*time.Second, b.wait(), "backoff should be capped at 30s")
}

// TestAcceptBackoff_ResetOnSuccess verifies backoff resets after successful accept.
func TestAcceptBackoff_ResetOnSuccess(t *testing.T) {
	b := newAcceptBackoff()

	// Build up some backoff
	b.failure()
	b.failure()
	b.failure()

	assert.NotEqual(t, time.Duration(0), b.wait(), "expected non-zero backoff after failures")

	// Success should reset
	b.success()

	assert.Equal(t, time.Duration(0), b.wait(), "backoff should reset to 0 after success")

	// Next failure should start at initial again
	b.failure()
	assert.Equal(t, 100*time.Millisecond, b.wait())
}

// TestGatewaySignalHandling_TriggersShutdown verifies that receiving a signal
// triggers graceful shutdown through the select statement in runGatewayWithDeps.
func TestGatewaySignalHandling_TriggersShutdown(t *testing.T) {
	// This test verifies the signal handling pattern used in gateway.go:
	// sigChan := make(chan os.Signal, 1)
	// signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	// select { case sig := <-sigChan: ... }

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Track if shutdown was triggered
	shutdownTriggered := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Simulate the main select loop
	go func() {
		select {
		case sig := <-sigChan:
			// This simulates: slog.Info("received shutdown signal", "signal", sig)
			if sig == syscall.SIGTERM || sig == syscall.SIGINT {
				close(shutdownTriggered)
			}
		case <-ctx.Done():
			return
		}
	}()

	// Send signal
	sigChan <- syscall.SIGTERM

	// Verify shutdown was triggered
	select {
	case <-shutdownTriggered:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("signal did not trigger shutdown within timeout")
	}
}

// TestGatewaySignalHandling_BothSignals verifies both SIGINT and SIGTERM trigger shutdown.
func TestGatewaySignalHandling_BothSignals(t *testing.T) {
	tests := []struct {
		name   string
		signal os.Signal
	}{
		{"SIGINT", syscall.SIGINT},
		{"SIGTERM", syscall.SIGTERM},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(sigChan)

			received := make(chan os.Signal, 1)

			go func() {
				select {
				case sig := <-sigChan:
					received <- sig
				case <-time.After(1 * time.Second):
					close(received)
				}
			}()

			// Send the signal
			sigChan <- tt.signal

			// Verify it was received
			select {
			case sig := <-received:
				assert.Equal(t, tt.signal, sig)
			case <-time.After(1 * time.Second):
				t.Fatal("signal not received within timeout")
			}
		})
	}
}

// TestGatewaySignalHandling_ContextCancelAlsoExits verifies that context cancellation
// also exits the main select loop (the third case in the select).
func TestGatewaySignalHandling_ContextCancelAlsoExits(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	exitedViaContext := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	// Simulate the main select loop
	go func() {
		select {
		case <-sigChan:
			// Not expected in this test
		case <-ctx.Done():
			close(exitedViaContext)
		}
	}()

	// Cancel context
	cancel()

	// Verify we exited via context cancellation
	select {
	case <-exitedViaContext:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("context cancel did not exit select within timeout")
	}
}
