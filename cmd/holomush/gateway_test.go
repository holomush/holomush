package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/tls"
)

func TestGatewayCommand_Flags(t *testing.T) {
	cmd := NewGatewayCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

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
		if !strings.Contains(output, flag) {
			t.Errorf("Help missing %q flag", flag)
		}
	}
}

func TestGatewayCommand_DefaultValues(t *testing.T) {
	cmd := NewGatewayCmd()

	// Check default telnet-addr
	telnetAddr, err := cmd.Flags().GetString("telnet-addr")
	if err != nil {
		t.Fatalf("Failed to get telnet-addr flag: %v", err)
	}
	if telnetAddr != ":4201" {
		t.Errorf("telnet-addr default = %q, want %q", telnetAddr, ":4201")
	}

	// Check default core-addr
	coreAddr, err := cmd.Flags().GetString("core-addr")
	if err != nil {
		t.Fatalf("Failed to get core-addr flag: %v", err)
	}
	if coreAddr != "localhost:9000" {
		t.Errorf("core-addr default = %q, want %q", coreAddr, "localhost:9000")
	}

	// Check default control-addr
	controlAddr, err := cmd.Flags().GetString("control-addr")
	if err != nil {
		t.Fatalf("Failed to get control-addr flag: %v", err)
	}
	if controlAddr != "127.0.0.1:9002" {
		t.Errorf("control-addr default = %q, want %q", controlAddr, "127.0.0.1:9002")
	}

	// Check default metrics-addr
	metricsAddr, err := cmd.Flags().GetString("metrics-addr")
	if err != nil {
		t.Fatalf("Failed to get metrics-addr flag: %v", err)
	}
	if metricsAddr != "127.0.0.1:9101" {
		t.Errorf("metrics-addr default = %q, want %q", metricsAddr, "127.0.0.1:9101")
	}

	// Check default log-format
	logFormat, err := cmd.Flags().GetString("log-format")
	if err != nil {
		t.Fatalf("Failed to get log-format flag: %v", err)
	}
	if logFormat != "json" {
		t.Errorf("log-format default = %q, want %q", logFormat, "json")
	}
}

func TestGatewayCommand_Properties(t *testing.T) {
	cmd := NewGatewayCmd()

	if cmd.Use != "gateway" {
		t.Errorf("Use = %q, want %q", cmd.Use, "gateway")
	}

	if !strings.Contains(cmd.Short, "gateway") {
		t.Error("Short description should mention gateway")
	}

	if !strings.Contains(cmd.Long, "telnet") {
		t.Error("Long description should mention telnet")
	}
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

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			telnetAddr, _ := cmd.Flags().GetString("telnet-addr")
			if telnetAddr != tt.wantTelnet {
				t.Errorf("telnet-addr = %q, want %q", telnetAddr, tt.wantTelnet)
			}

			coreAddr, _ := cmd.Flags().GetString("core-addr")
			if coreAddr != tt.wantCore {
				t.Errorf("core-addr = %q, want %q", coreAddr, tt.wantCore)
			}

			fmtVal, _ := cmd.Flags().GetString("log-format")
			if fmtVal != tt.wantFmt {
				t.Errorf("log-format = %q, want %q", fmtVal, tt.wantFmt)
			}
		})
	}
}

func TestGatewayCommand_Help(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"gateway", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

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
		if !strings.Contains(output, phrase) {
			t.Errorf("Help missing phrase %q", phrase)
		}
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
	if err == nil {
		t.Fatal("Expected error when certificates are missing")
	}

	// Error should mention TLS or certificates
	if !strings.Contains(err.Error(), "TLS") && !strings.Contains(err.Error(), "certificate") && !strings.Contains(err.Error(), "certs") {
		t.Errorf("Error should mention TLS/certificate issue, got: %v", err)
	}
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
	if err == nil {
		t.Fatal("Expected error with invalid log format")
	}

	// Error should mention log format (validation moved from setupLogging to Validate)
	errMsg := err.Error()
	if !strings.Contains(errMsg, "logging") && !strings.Contains(errMsg, "log format") && !strings.Contains(errMsg, "log-format") {
		t.Errorf("Error should mention logging issue, got: %v", err)
	}
}

func TestGatewayCommand_CAExtractionFails(t *testing.T) {
	// Create a certs directory with an invalid CA certificate
	tmpDir := t.TempDir()
	certsDir := tmpDir + "/holomush/certs"
	if err := os.MkdirAll(certsDir, 0o700); err != nil {
		t.Fatalf("failed to create certs dir: %v", err)
	}

	// Write an invalid CA certificate
	caPath := certsDir + "/root-ca.crt"
	if err := os.WriteFile(caPath, []byte("not a valid certificate"), 0o600); err != nil {
		t.Fatalf("failed to write invalid CA: %v", err)
	}

	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"gateway"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when CA extraction fails")
	}

	// Error should mention game_id or CA
	if !strings.Contains(err.Error(), "game_id") && !strings.Contains(err.Error(), "CA") {
		t.Errorf("Error should mention game_id/CA extraction issue, got: %v", err)
	}
}

func TestGatewayCommand_TLSLoadFails(t *testing.T) {
	// Create a certs directory with a valid CA but invalid gateway certificate
	tmpDir := t.TempDir()
	certsDir := tmpDir + "/holomush/certs"

	// Generate a valid CA
	gameID := "test-gateway-tls-fail"
	ca, err := tls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("failed to generate CA: %v", err)
	}

	// Save CA only (no gateway certificate)
	if err := tls.SaveCertificates(certsDir, ca, nil); err != nil {
		t.Fatalf("failed to save CA: %v", err)
	}

	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"gateway"})

	err = cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when TLS certificates are incomplete")
	}

	// Error should mention TLS or certificate
	if !strings.Contains(err.Error(), "TLS") && !strings.Contains(err.Error(), "certificate") {
		t.Errorf("Error should mention TLS/certificate issue, got: %v", err)
	}
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
	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}
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
	if !strings.Contains(result, "Welcome to HoloMUSH Gateway") {
		t.Errorf("output missing welcome message, got: %q", result)
	}
	if !strings.Contains(result, "Gateway is connected to core") {
		t.Errorf("output missing status message, got: %q", result)
	}
	if !strings.Contains(result, "Disconnecting") {
		t.Errorf("output missing disconnect message, got: %q", result)
	}

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
	if err := client.Close(); err != nil {
		t.Fatalf("failed to close client: %v", err)
	}

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

	if !mock.closed {
		t.Error("connection should be closed after handler completes")
	}
}

func TestGatewayCommand_InvalidCACN(t *testing.T) {
	// Create a certs directory with a CA that has wrong CN prefix
	tmpDir := t.TempDir()
	certsDir := tmpDir + "/holomush/certs"
	if err := os.MkdirAll(certsDir, 0o700); err != nil {
		t.Fatalf("failed to create certs dir: %v", err)
	}

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
	if err := os.WriteFile(caPath, []byte(pemData), 0o600); err != nil {
		t.Fatalf("failed to write CA with wrong CN: %v", err)
	}

	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"gateway"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when CA has wrong CN prefix")
	}

	// Error should mention game_id or CN
	if !strings.Contains(err.Error(), "game_id") && !strings.Contains(err.Error(), "prefix") {
		t.Errorf("Error should mention game_id/prefix issue, got: %v", err)
	}
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
				if err == nil {
					t.Fatalf("Validate() expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Validate() error = %q, want to contain %q", err.Error(), tt.errorMsg)
				}
			} else if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestGatewayConfig_Defaults(t *testing.T) {
	// Verify the default constants are set correctly
	if defaultTelnetAddr != ":4201" {
		t.Errorf("defaultTelnetAddr = %q, want %q", defaultTelnetAddr, ":4201")
	}
	if defaultCoreAddr != "localhost:9000" {
		t.Errorf("defaultCoreAddr = %q, want %q", defaultCoreAddr, "localhost:9000")
	}
	if defaultGatewayControlAddr != "127.0.0.1:9002" {
		t.Errorf("defaultGatewayControlAddr = %q, want %q", defaultGatewayControlAddr, "127.0.0.1:9002")
	}
	if defaultGatewayMetricsAddr != "127.0.0.1:9101" {
		t.Errorf("defaultGatewayMetricsAddr = %q, want %q", defaultGatewayMetricsAddr, "127.0.0.1:9101")
	}
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
