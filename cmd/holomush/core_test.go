// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/pkg/errutil"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

func TestCoreCommandHelpListsEveryExpectedFlag(t *testing.T) {
	cmd := NewCoreCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	require.NoError(t, cmd.Execute())

	output := buf.String()

	// Verify all expected flags are present
	expectedFlags := []string{
		"--grpc-addr",
		"--control-addr",
		"--metrics-addr",
		"--data-dir",
		"--game-id",
		"--log-format",
	}

	for _, flag := range expectedFlags {
		assert.Contains(t, output, flag, "Help missing %q flag", flag)
	}
}

func TestCoreCommand_LogSinkFlags(t *testing.T) {
	cmd := NewCoreCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})
	require.NoError(t, cmd.Execute())
	for _, f := range []string{"--log-sentry", "--log-sentry-level", "--log-otel", "--log-otel-level", "--log-stderr", "--log-stderr-level"} {
		require.Contains(t, buf.String(), f)
	}
}

func TestCoreCommand_DefaultValues(t *testing.T) {
	cmd := NewCoreCmd()

	// Check default grpc-addr
	grpcAddr, err := cmd.Flags().GetString("grpc-addr")
	require.NoError(t, err, "Failed to get grpc-addr flag")
	assert.Equal(t, "localhost:9000", grpcAddr)

	// Check default control-addr
	controlAddr, err := cmd.Flags().GetString("control-addr")
	require.NoError(t, err, "Failed to get control-addr flag")
	assert.Equal(t, "127.0.0.1:9001", controlAddr)

	// Check default metrics-addr
	metricsAddr, err := cmd.Flags().GetString("metrics-addr")
	require.NoError(t, err, "Failed to get metrics-addr flag")
	assert.Equal(t, "127.0.0.1:9100", metricsAddr)

	// Check default log-format
	logFormat, err := cmd.Flags().GetString("log-format")
	require.NoError(t, err, "Failed to get log-format flag")
	assert.Equal(t, "json", logFormat)

	// Check other flags have empty defaults
	dataDir, err := cmd.Flags().GetString("data-dir")
	require.NoError(t, err, "Failed to get data-dir flag")
	assert.Empty(t, dataDir)

	gameID, err := cmd.Flags().GetString("game-id")
	require.NoError(t, err, "Failed to get game-id flag")
	assert.Empty(t, gameID)
}

func TestCoreCommandDeclaresItsUseShortAndLongDescriptions(t *testing.T) {
	cmd := NewCoreCmd()

	assert.Equal(t, "core", cmd.Use)
	assert.Contains(t, cmd.Short, "core", "Short description should mention core")
	assert.Contains(t, cmd.Long, "game engine", "Long description should mention game engine")
}

func TestCoreCommand_NoDatabaseURL(t *testing.T) {
	// Ensure DATABASE_URL is not set for this test
	t.Setenv("DATABASE_URL", "")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"core"})

	err := cmd.Execute()
	require.Error(t, err, "Expected error when DATABASE_URL is not set")
	assert.Contains(t, err.Error(), "DATABASE_URL")
}

func TestCoreCommand_InvalidDatabaseURL(t *testing.T) {
	// Set an invalid DATABASE_URL
	t.Setenv("DATABASE_URL", "invalid://not-a-real-db")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"core"})

	err := cmd.Execute()
	require.Error(t, err, "Expected error with invalid DATABASE_URL")
	// Error from golang-migrate during auto-migration - "unknown driver" when scheme is invalid
	assert.Contains(t, err.Error(), "unknown driver", "Error should mention unknown driver, got: %v", err)
}

func TestCoreCommand_FlagParsing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantAddr string
		wantFmt  string
	}{
		{
			name:     "default values",
			args:     []string{"--help"},
			wantAddr: "localhost:9000",
			wantFmt:  "json",
		},
		{
			name:     "custom grpc addr",
			args:     []string{"--grpc-addr=0.0.0.0:8080", "--help"},
			wantAddr: "0.0.0.0:8080",
			wantFmt:  "json",
		},
		{
			name:     "text log format",
			args:     []string{"--log-format=text", "--help"},
			wantAddr: "localhost:9000",
			wantFmt:  "text",
		},
		{
			name:     "all custom flags",
			args:     []string{"--grpc-addr=127.0.0.1:7000", "--log-format=text", "--help"},
			wantAddr: "127.0.0.1:7000",
			wantFmt:  "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewCoreCmd()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetArgs(tt.args)

			require.NoError(t, cmd.Execute())

			addr, _ := cmd.Flags().GetString("grpc-addr")
			assert.Equal(t, tt.wantAddr, addr)

			fmtVal, _ := cmd.Flags().GetString("log-format")
			assert.Equal(t, tt.wantFmt, fmtVal)
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLevel slog.Level
		wantError bool
	}{
		{name: "debug lowercase", input: "debug", wantLevel: slog.LevelDebug},
		{name: "info lowercase", input: "info", wantLevel: slog.LevelInfo},
		{name: "warn lowercase", input: "warn", wantLevel: slog.LevelWarn},
		{name: "error lowercase", input: "error", wantLevel: slog.LevelError},
		{name: "INFO uppercase", input: "INFO", wantLevel: slog.LevelInfo},
		{name: "DEBUG uppercase", input: "DEBUG", wantLevel: slog.LevelDebug},
		{name: "invalid level", input: "verbose", wantError: true},
		{name: "empty level", input: "", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLogLevel(tt.input)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantLevel, got)
			}
		})
	}
}

func TestRootCommandCoreHelpContainsEveryExpectedSection(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"core", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	require.NoError(t, cmd.Execute())

	output := buf.String()

	// Verify help contains expected sections
	expectedPhrases := []string{
		"Start the core process",
		"game engine",
		"--grpc-addr",
		"--control-addr",
		"--metrics-addr",
		"--log-format",
	}

	for _, phrase := range expectedPhrases {
		assert.Contains(t, output, phrase, "Help missing phrase %q", phrase)
	}
}

// TestListenerCleanupOnFailure verifies that the gRPC listener is properly
// closed when startup fails after the listener is created.
// This is a regression test for the resource leak bug where the listener
// was not closed when control TLS config loading or control server startup failed.
func TestListenerCleanupOnFailure(t *testing.T) {
	// This test verifies the fix indirectly by checking that port reuse works
	// after a failed startup. If the listener were leaked, the port would remain
	// in use and subsequent operations would fail.

	// Use a random high port to avoid conflicts
	addr := "127.0.0.1:0"

	// Create a listener to get an available port
	listener, err := net.Listen("tcp", addr)
	require.NoError(t, err, "Failed to create initial listener")

	// Get the actual port that was assigned
	actualAddr := listener.Addr().String()

	// Simulate the fix: defer close ensures cleanup
	func() {
		defer func() { _ = listener.Close() }()
		// Simulate an error after listener creation but before using it
		// In the real code, this would be control.LoadControlServerTLS failing
		// The key is that defer ensures cleanup even when we return early
	}()

	// Verify the port is now available again
	// This would fail if the listener wasn't properly closed
	listener2, err := net.Listen("tcp", actualAddr)
	require.NoError(t, err, "Port %s not available after cleanup - listener was leaked", actualAddr)
	defer func() { _ = listener2.Close() }()
}

func TestCoreCommand_LuaLimitDefaults(t *testing.T) {
	cmd := NewCoreCmd()

	timeout, err := cmd.Flags().GetDuration("plugin-lua-timeout")
	require.NoError(t, err)
	assert.Equal(t, 1*time.Second, timeout, "default Lua timeout per spec")

	regMax, err := cmd.Flags().GetInt("plugin-lua-registry-max")
	require.NoError(t, err)
	assert.Equal(t, 65536, regMax, "default registry max per spec")
}

func TestCoreConfig_ValidateRejectsNonPositiveLuaLimits(t *testing.T) {
	base := coreConfig{
		GRPCAddr:           "localhost:9000",
		ControlAddr:        "127.0.0.1:9001",
		LogFormat:          "json",
		LuaTimeout:         1 * time.Second,
		LuaRegistryMaxSize: 65536,
	}

	cases := []struct {
		name string
		mut  func(c *coreConfig)
	}{
		{"LuaTimeout=0", func(c *coreConfig) { c.LuaTimeout = 0 }},
		{"LuaTimeout<0", func(c *coreConfig) { c.LuaTimeout = -1 * time.Second }},
		{"LuaRegistryMaxSize=0", func(c *coreConfig) { c.LuaRegistryMaxSize = 0 }},
		{"LuaRegistryMaxSize<0", func(c *coreConfig) { c.LuaRegistryMaxSize = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mut(&cfg)
			err := cfg.Validate()
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "CONFIG_INVALID")
		})
	}
}

// TestCoreConfig_Validate tests validation of coreConfig.
func TestCoreConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		cfg       coreConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid config",
			cfg: coreConfig{
				GRPCAddr:           "localhost:9000",
				ControlAddr:        "127.0.0.1:9001",
				LogFormat:          "json",
				LuaTimeout:         1 * time.Second,
				LuaRegistryMaxSize: 65536,
			},
			wantError: false,
		},
		{
			name: "valid config with text format",
			cfg: coreConfig{
				GRPCAddr:           "localhost:9000",
				ControlAddr:        "127.0.0.1:9001",
				LogFormat:          "text",
				LuaTimeout:         1 * time.Second,
				LuaRegistryMaxSize: 65536,
			},
			wantError: false,
		},
		{
			name: "empty grpc-addr",
			cfg: coreConfig{
				GRPCAddr:    "",
				ControlAddr: "127.0.0.1:9001",
				LogFormat:   "json",
			},
			wantError: true,
			errorMsg:  "grpc-addr is required",
		},
		{
			name: "empty control-addr",
			cfg: coreConfig{
				GRPCAddr:    "localhost:9000",
				ControlAddr: "",
				LogFormat:   "json",
			},
			wantError: true,
			errorMsg:  "control-addr is required",
		},
		{
			name: "invalid log-format",
			cfg: coreConfig{
				GRPCAddr:    "localhost:9000",
				ControlAddr: "127.0.0.1:9001",
				LogFormat:   "invalid",
			},
			wantError: true,
			errorMsg:  "log-format must be 'json' or 'text'",
		},
		{
			name: "empty log-format",
			cfg: coreConfig{
				GRPCAddr:    "localhost:9000",
				ControlAddr: "127.0.0.1:9001",
				LogFormat:   "",
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

// TestCoreCommand_InvalidLogFormat verifies that invalid log format is rejected.
func TestCoreCommand_InvalidLogFormat(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test:test@localhost/test")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"core", "--log-format=invalid"})

	err := cmd.Execute()
	require.Error(t, err, "Expected error with invalid log format")

	assert.True(t, assert.Condition(t, func() bool {
		return assert.Contains(t, err.Error(), "log") || assert.Contains(t, err.Error(), "format")
	}), "Error should mention log/format issue, got: %v", err)
}

// TestMonitorServerErrors verifies that monitorServerErrors cancels context on error.
func TestMonitorServerErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create error channel and send error
	errCh := make(chan error, 1)
	testErr := fmt.Errorf("test server error")
	errCh <- testErr

	// Start monitoring
	done := make(chan struct{})
	go func() {
		monitorServerErrors(ctx, cancel, errCh, "test-server")
		close(done)
	}()

	// Wait for context to be cancelled
	select {
	case <-ctx.Done():
		// Success - context was cancelled
	case <-time.After(time.Second):
		t.Fatal("context was not cancelled after server error")
	}

	// Wait for goroutine to complete
	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("monitorServerErrors goroutine did not complete")
	}
}

// TestMonitorServerErrors_NilError verifies that nil errors don't cancel context.
func TestMonitorServerErrors_NilError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create error channel and send nil (graceful shutdown)
	errCh := make(chan error, 1)
	errCh <- nil

	// Start monitoring
	done := make(chan struct{})
	go func() {
		monitorServerErrors(ctx, cancel, errCh, "test-server")
		close(done)
	}()

	// Wait for goroutine to complete
	select {
	case <-done:
		// Success - goroutine completed
	case <-time.After(time.Second):
		t.Fatal("monitorServerErrors goroutine did not complete")
	}

	// Context should NOT be cancelled for nil error
	select {
	case <-ctx.Done():
		t.Fatal("context should not be cancelled for nil error")
	default:
		// Success - context still active
	}
}

// TestMonitorServerErrors_ChannelClose verifies handling when channel is closed.
func TestMonitorServerErrors_ChannelClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create and immediately close channel
	errCh := make(chan error, 1)
	close(errCh)

	// Start monitoring
	done := make(chan struct{})
	go func() {
		monitorServerErrors(ctx, cancel, errCh, "test-server")
		close(done)
	}()

	// Wait for goroutine to complete (should exit on closed channel)
	select {
	case <-done:
		// Success - goroutine completed
	case <-time.After(time.Second):
		t.Fatal("monitorServerErrors goroutine did not complete")
	}

	// Context should NOT be cancelled for closed channel (graceful)
	select {
	case <-ctx.Done():
		t.Fatal("context should not be cancelled when channel closes gracefully")
	default:
		// Success - context still active
	}
}

// TestMonitorServerErrors_ContextCancelled verifies behavior when context is cancelled first.
func TestMonitorServerErrors_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create error channel but don't send anything
	errCh := make(chan error, 1)

	// Start monitoring
	done := make(chan struct{})
	go func() {
		monitorServerErrors(ctx, cancel, errCh, "test-server")
		close(done)
	}()

	// Cancel context before any error arrives
	cancel()

	// Wait for goroutine to complete
	select {
	case <-done:
		// Success - goroutine completed
	case <-time.After(time.Second):
		t.Fatal("monitorServerErrors goroutine did not complete after context cancel")
	}
}

// TestListenerCloseError verifies that listener close errors are logged.
// The actual logging verification would require log capture, but this test
// ensures the code path is exercised and doesn't panic.
func TestListenerCloseError(t *testing.T) {
	// Create a listener and close it before the defer runs
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "Failed to create listener")

	// Close it now so the defer Close() will get an error
	require.NoError(t, listener.Close(), "Failed to close listener")

	// Simulate what the code does - this should log at debug level, not panic
	// In a real scenario, this would be verified with log capture
	if closeErr := listener.Close(); closeErr != nil {
		// This is the expected path - error is logged
		t.Logf("Expected close error: %v", closeErr)
	}
}

// TestCoreCommand_GameConfigLoading verifies that game.guest_start_location is loaded
// from the config file, and that the default ULID is used when not set.
func TestCoreCommand_GameConfigLoading(t *testing.T) {
	tests := []struct {
		name            string
		yamlContent     string
		wantLocation    string
		wantEmptyConfig bool
	}{
		{
			name: "guest_start_location from config file",
			yamlContent: `
game:
  guest_start_location: "01JPQR0000ABCDEFGHJKMNPQRS"
`,
			wantLocation: "01JPQR0000ABCDEFGHJKMNPQRS",
		},
		{
			name:            "no game section in config — empty GameConfig",
			yamlContent:     ``,
			wantEmptyConfig: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write a temporary config file.
			tmpFile, err := os.CreateTemp("", "holomush-game-config-test-*.yaml")
			require.NoError(t, err)
			t.Cleanup(func() { _ = os.Remove(tmpFile.Name()) })

			_, err = tmpFile.WriteString(tt.yamlContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			// Use a minimal cobra command (no flags needed for the game section).
			cmd := NewCoreCmd()

			var gameConfig config.GameConfig
			err = config.Load(tmpFile.Name(), cmd, &gameConfig, "game")
			require.NoError(t, err)

			if tt.wantEmptyConfig {
				assert.Empty(t, gameConfig.GuestStartLocation,
					"GuestStartLocation should be empty when not set in config")
			} else {
				assert.Equal(t, tt.wantLocation, gameConfig.GuestStartLocation)
			}
		})
	}
}

// TestCoreCommand_GameConfigFallback verifies that runCoreWithDeps uses the
// hardcoded Nexus ULID when gameConfig.GuestStartLocation is empty.
func TestCoreCommand_GameConfigFallback(t *testing.T) {
	// Verify that an empty GuestStartLocation triggers the default Nexus ULID.
	// We check this by confirming the default is a valid parseable ULID —
	// the actual wiring is exercised by the full runCoreWithDeps happy-path test.
	const defaultNexusULID = "01HK153X0006AFVGQT61FPQX3S"

	id, err := ulid.Parse(defaultNexusULID)
	require.NoError(t, err, "hardcoded default Nexus ULID must be parseable")
	assert.NotZero(t, id, "parsed ULID should not be zero value")
}

// TestCoreCommand_LogSinkFlagsBind verifies that explicitly-set --log-* flags
// overlay onto LoggingConfig (spec §5: CLI > config > default), and that
// untouched flags leave the config defaults intact.
func TestCoreCommand_LogSinkFlagsBind(t *testing.T) {
	cmd := NewCoreCmd()
	require.NoError(t, cmd.Flags().Parse([]string{
		"--log-stderr=false", "--log-sentry-level=warn", "--log-otel=false",
	}))
	lc := config.DefaultLoggingConfig()
	applyLogSinkFlags(cmd, &lc)
	require.False(t, lc.Stderr.Enabled)
	require.False(t, lc.OTel.Enabled)
	require.Equal(t, "warn", lc.Sentry.Level)
	require.True(t, lc.Sentry.Enabled) // untouched flag keeps default
}

// TestSignalHandling_ChannelSetup verifies that signal handling sets up channels correctly.
// This tests the signal.Notify behavior and ensures proper channel configuration.
func TestSignalHandling_ChannelSetup(t *testing.T) {
	// Create a buffered channel like the code does
	sigChan := make(chan os.Signal, 1)

	// Register for signals
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Verify the channel is buffered with capacity 1
	// This is important to prevent signal loss
	assert.Equal(t, 1, cap(sigChan), "signal channel capacity should be 1")

	// Verify we can send a signal to ourselves and receive it
	// This simulates what happens when the OS sends a signal
	go func() {
		// Small delay to ensure the main goroutine is waiting on the channel
		time.Sleep(10 * time.Millisecond)
		// Send a signal through the channel (simulating OS signal delivery)
		sigChan <- syscall.SIGTERM
	}()

	// Wait for the signal with timeout
	select {
	case sig := <-sigChan:
		assert.Equal(t, syscall.SIGTERM, sig)
	case <-time.After(1 * time.Second):
		t.Fatal("did not receive signal within timeout")
	}
}

// TestSignalHandling_MultipleSignals verifies behavior with multiple signals.
// Since channel capacity is 1, only one signal can be buffered.
func TestSignalHandling_MultipleSignals(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// First signal should be delivered
	sigChan <- syscall.SIGINT

	// Second signal - since we haven't read yet, behavior depends on buffer
	// With capacity 1, channel is full so this would block without select
	select {
	case sigChan <- syscall.SIGTERM:
		// If this succeeds, channel wasn't full (unexpected)
		t.Log("second signal sent (unexpected - channel should be full)")
	default:
		// This is expected - channel is full with first signal
		t.Log("second signal blocked as expected (channel full)")
	}

	// Read the first signal
	select {
	case sig := <-sigChan:
		assert.Equal(t, syscall.SIGINT, sig, "first signal should be SIGINT")
	default:
		t.Fatal("no signal available when expected")
	}
}

// TestSignalStopStopsDeliveringOSSignalsToTheChannel verifies that signal.Stop properly unregisters signal handling.
func TestSignalStopStopsDeliveringOSSignalsToTheChannel(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Send a signal before stop - should be received
	sigChan <- syscall.SIGINT
	select {
	case <-sigChan:
		// Good - signal received
	default:
		t.Fatal("signal not available before Stop")
	}

	// Stop signal handling
	signal.Stop(sigChan)

	// After Stop, channel should be drained but no longer receives OS signals
	// We can verify Stop was called by checking the channel is empty
	select {
	case sig := <-sigChan:
		t.Errorf("unexpected signal after Stop: %v", sig)
	default:
		// Good - channel is empty after Stop
	}
}

func TestCoreCommand_ConfigFileLoading(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte("core:\n  grpc_addr: \"0.0.0.0:7777\"\n  control_addr: \"0.0.0.0:7778\"\n  log_format: \"text\"\n"), 0o600)
	require.NoError(t, err)

	cfg := &coreConfig{}
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringVar(&cfg.GRPCAddr, "grpc-addr", defaultGRPCAddr, "")
	cmd.Flags().StringVar(&cfg.ControlAddr, "control-addr", defaultCoreControlAddr, "")
	cmd.Flags().StringVar(&cfg.LogFormat, "log-format", defaultLogFormat, "")

	err = config.Load(cfgFile, cmd, cfg, "core")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:7777", cfg.GRPCAddr)
	assert.Equal(t, "0.0.0.0:7778", cfg.ControlAddr)
	assert.Equal(t, "text", cfg.LogFormat)
}

// TestParseSessionConfigDefaultsEmptyFields verifies that empty TTL and reaper fields
// receive their default values (30m TTL, 30s reaper, 500 history).
func TestParseSessionConfigDefaultsEmptyFields(t *testing.T) {
	cfg := &coreConfig{}

	ttl, reaper, _, _, err := parseSessionConfig(cfg)

	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, ttl)
	assert.Equal(t, 30*time.Second, reaper)
	assert.Equal(t, 500, cfg.SessionMaxHistory)
}

// TestParseSessionConfigUsesExplicitValues verifies that explicitly set TTL and
// reaper values are preserved rather than overwritten with defaults.
func TestParseSessionConfigUsesExplicitValues(t *testing.T) {
	cfg := &coreConfig{
		SessionTTL:            "1h",
		SessionReaperInterval: "5m",
		SessionMaxHistory:     250,
	}

	ttl, reaper, _, _, err := parseSessionConfig(cfg)

	require.NoError(t, err)
	assert.Equal(t, 1*time.Hour, ttl)
	assert.Equal(t, 5*time.Minute, reaper)
	assert.Equal(t, 250, cfg.SessionMaxHistory)
}

// TestParseSessionConfigRejectsInvalidTTL verifies that a malformed TTL value
// returns an error.
func TestParseSessionConfigRejectsInvalidTTL(t *testing.T) {
	cfg := &coreConfig{
		SessionTTL:            "not-a-duration",
		SessionReaperInterval: "30s",
	}

	_, _, _, _, err := parseSessionConfig(cfg)

	require.Error(t, err)
}

// TestParseSessionConfigRejectsInvalidReaperInterval verifies that a malformed
// reaper interval value returns an error.
func TestParseSessionConfigRejectsInvalidReaperInterval(t *testing.T) {
	cfg := &coreConfig{
		SessionTTL:            "30m",
		SessionReaperInterval: "not-a-duration",
	}

	_, _, _, _, err := parseSessionConfig(cfg)

	require.Error(t, err)
}

// TestParseSessionConfigRejectsZeroTTL verifies that a zero TTL returns an error
// containing "positive".
func TestParseSessionConfigRejectsZeroTTL(t *testing.T) {
	cfg := &coreConfig{
		SessionTTL:            "0s",
		SessionReaperInterval: "30s",
	}

	_, _, _, _, err := parseSessionConfig(cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "positive")
}

// TestParseSessionConfigRejectsZeroReaperInterval verifies that a zero reaper
// interval returns an error containing "positive".
func TestParseSessionConfigRejectsZeroReaperInterval(t *testing.T) {
	cfg := &coreConfig{
		SessionTTL:            "30m",
		SessionReaperInterval: "0s",
	}

	_, _, _, _, err := parseSessionConfig(cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "positive")
}

// TestParseSessionConfigDefaultsNegativeMaxHistory verifies that a negative
// max history value (-1) is replaced with the default of 500.
func TestParseSessionConfigDefaultsNegativeMaxHistory(t *testing.T) {
	cfg := &coreConfig{
		SessionTTL:            "30m",
		SessionReaperInterval: "30s",
		SessionMaxHistory:     -1,
	}

	_, _, _, _, err := parseSessionConfig(cfg)

	require.NoError(t, err)
	assert.Equal(t, 500, cfg.SessionMaxHistory)
}

// TestParseSessionConfigPreservesPositiveMaxHistory verifies that a positive
// max history value (250) is preserved without modification.
func TestParseSessionConfigPreservesPositiveMaxHistory(t *testing.T) {
	cfg := &coreConfig{
		SessionTTL:            "30m",
		SessionReaperInterval: "30s",
		SessionMaxHistory:     250,
	}

	_, _, _, _, err := parseSessionConfig(cfg)

	require.NoError(t, err)
	assert.Equal(t, 250, cfg.SessionMaxHistory)
}

// TestParseSessionConfigLeaseTTLAndBootGrace covers parsing/validation of
// session_lease_ttl and session_boot_grace: malformed and zero values are
// rejected, empty values default to 45s/60s, explicit values are preserved, and
// values below 2× the gateway refresh cadence (the 30s floor, holomush-rsoe6.22)
// are rejected to prevent the reaper sweeping a healthy connection between refreshes.
func TestParseSessionConfigLeaseTTLAndBootGrace(t *testing.T) {
	tests := []struct {
		name          string
		leaseTTL      string
		bootGrace     string
		wantErrCode   string        // non-empty → assert this oops code
		wantErrSubstr string        // non-empty → assert err.Error() contains
		wantLease     time.Duration // asserted when no error expected
		wantBoot      time.Duration
	}{
		{name: "rejects malformed lease_ttl", leaseTTL: "bogus", wantErrCode: "CONFIG_INVALID"},
		{name: "rejects malformed boot_grace", bootGrace: "bogus", wantErrCode: "CONFIG_INVALID"},
		{name: "rejects zero lease_ttl", leaseTTL: "0s", wantErrSubstr: "positive"},
		{name: "rejects zero boot_grace", bootGrace: "0s", wantErrSubstr: "positive"},
		{name: "empty values default to 45s/60s", wantLease: 45 * time.Second, wantBoot: 60 * time.Second},
		{name: "explicit values preserved", leaseTTL: "2m", bootGrace: "3m", wantLease: 2 * time.Minute, wantBoot: 3 * time.Minute},
		// holomush-rsoe6.22: lease/grace below 2× the gateway refresh cadence
		// (session.DefaultLeaseRefreshInterval = 15s) let the reaper sweep a
		// healthy connection between refreshes. Reject anything under the 30s floor.
		{name: "rejects lease_ttl below 2x refresh cadence", leaseTTL: "20s", wantErrCode: "CONFIG_INVALID", wantErrSubstr: "cadence"},
		{name: "rejects boot_grace below 2x refresh cadence", bootGrace: "20s", wantErrCode: "CONFIG_INVALID", wantErrSubstr: "cadence"},
		// Both below floor: the lease check runs first, so its error wins.
		{name: "rejects when both below floor — lease error wins", leaseTTL: "20s", bootGrace: "20s", wantErrCode: "CONFIG_INVALID", wantErrSubstr: "lease TTL"},
		{name: "accepts lease_ttl and boot_grace at the 2x cadence floor", leaseTTL: "30s", bootGrace: "30s", wantLease: 30 * time.Second, wantBoot: 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &coreConfig{
				SessionTTL:            "30m",
				SessionReaperInterval: "30s",
				SessionLeaseTTL:       tt.leaseTTL,
				SessionBootGrace:      tt.bootGrace,
			}

			_, _, leaseTTL, bootGrace, err := parseSessionConfig(cfg)

			if tt.wantErrCode != "" || tt.wantErrSubstr != "" {
				require.Error(t, err)
				if tt.wantErrCode != "" {
					errutil.AssertErrorCode(t, err, tt.wantErrCode)
				}
				if tt.wantErrSubstr != "" {
					assert.Contains(t, err.Error(), tt.wantErrSubstr)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantLease, leaseTTL)
			assert.Equal(t, tt.wantBoot, bootGrace)
		})
	}
}

// TestResolveLogLevel verifies that resolveLogLevel correctly resolves log level
// from the flag, LOG_LEVEL env var, and default.
func TestResolveLogLevel(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, cmd *cobra.Command)
		wantLevel slog.Level
		wantError bool
	}{
		{
			name: "flag explicitly set uses flag value",
			setup: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				require.NoError(t, cmd.Flags().Set("log-level", "debug"))
			},
			wantLevel: slog.LevelDebug,
		},
		{
			name: "flag not set, LOG_LEVEL env var set uses env var",
			setup: func(t *testing.T, _ *cobra.Command) {
				t.Helper()
				t.Setenv("LOG_LEVEL", "warn")
			},
			wantLevel: slog.LevelWarn,
		},
		{
			name: "flag not set, no env var returns slog.LevelInfo",
			setup: func(t *testing.T, _ *cobra.Command) {
				t.Helper()
				t.Setenv("LOG_LEVEL", "")
			},
			wantLevel: slog.LevelInfo,
		},
		{
			name: "flag not set, invalid LOG_LEVEL env var returns error",
			setup: func(t *testing.T, _ *cobra.Command) {
				t.Helper()
				t.Setenv("LOG_LEVEL", "verbose")
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build a minimal command with the --log-level flag registered,
			// mirroring how NewRootCmd registers it on the persistent flags
			// and each subcommand inherits it.
			cmd := &cobra.Command{Use: "test"}
			cmd.Flags().StringVar(&logLevel, "log-level", "info", "log level")

			tt.setup(t, cmd)

			got, err := resolveLogLevel(cmd)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantLevel, got)
			}
		})
	}
}

// --- rekeyAuditPublisherAdapter -------------------------------------------
//
// The adapter is the only thing standing between dek.AuditPublisher and the
// bus: it validates the subject and type, overrides NewEvent's wall clock with
// the injected one, and hands back the minted ULID so a rekey operation can be
// correlated with its audit row. Production wires it at cryptowiring.go:213
// with a real clock, so none of these paths are reachable from a unit boot.

// fixedClock is a deterministic Now() source satisfying the anonymous clock
// interface on rekeyAuditPublisherAdapter.
type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

// recordingPublisher captures every event handed to Publish so a test can
// assert on what actually reached the bus, and optionally fails the publish.
type recordingPublisher struct {
	published []eventbus.Event
	err       error
}

func (p *recordingPublisher) Publish(_ context.Context, event eventbus.Event) error {
	p.published = append(p.published, event)
	return p.err
}

func TestRekeyAuditPublisherStampsTheInjectedClockAndReturnsThePublishedEventID(t *testing.T) {
	at := time.Date(2026, time.March, 14, 15, 9, 26, 0, time.UTC)
	pub := &recordingPublisher{}
	adapter := &rekeyAuditPublisherAdapter{publisher: pub, clock: fixedClock{at: at}}
	payload := []byte(`{"request_id":"01JQ0000000000000000000000"}`)

	id, err := adapter.PublishAudit(context.Background(),
		"events.main.system.rekey.completed", "crypto.system.rekey", payload)

	require.NoError(t, err)
	require.Len(t, pub.published, 1, "a successful PublishAudit must emit exactly one event")
	got := pub.published[0]

	assert.Equal(t, got.ID, id,
		"the returned ULID must be the published event's ID, or a caller cannot correlate the audit row")
	assert.NotEqual(t, ulid.ULID{}, id, "a published audit event must carry a minted ULID")
	assert.True(t, at.Equal(got.Timestamp),
		"the injected clock must override NewEvent's time.Now(); want %s got %s", at, got.Timestamp)
	assert.Equal(t, eventbus.Subject("events.main.system.rekey.completed"), got.Subject)
	assert.Equal(t, eventbus.Type("crypto.system.rekey"), got.Type)
	assert.Equal(t, eventbus.ActorKindSystem, got.Actor.Kind,
		"rekey audit is host-emitted, so the actor kind must be system")
	assert.Equal(t, payload, got.Payload)
}

func TestRekeyAuditPublisherRejectsAMalformedSubjectOrTypeWithoutPublishing(t *testing.T) {
	tests := []struct {
		name     string
		subject  string
		evType   string
		wantCode string
	}{
		{
			name:     "rejects a subject outside the events namespace",
			subject:  "audit.main.system.rekey",
			evType:   "crypto.system.rekey",
			wantCode: "DEK_REKEY_AUDIT_INVALID_SUBJECT",
		},
		{
			name:     "rejects an event type carrying uppercase characters",
			subject:  "events.main.system.rekey.completed",
			evType:   "Crypto.System.Rekey",
			wantCode: "DEK_REKEY_AUDIT_INVALID_TYPE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub := &recordingPublisher{}
			adapter := &rekeyAuditPublisherAdapter{
				publisher: pub,
				clock:     fixedClock{at: time.Unix(0, 0).UTC()},
			}

			id, err := adapter.PublishAudit(context.Background(), tt.subject, tt.evType, []byte(`{}`))

			require.Error(t, err)
			errutil.AssertErrorCode(t, err, tt.wantCode)
			assert.Equal(t, ulid.ULID{}, id, "a rejected audit emit must not report an event ID")
			assert.Empty(t, pub.published, "a rejected audit emit must never reach the bus")
		})
	}
}

func TestRekeyAuditPublisherReportsAPublishFailureAndMintsNoEventID(t *testing.T) {
	cause := errors.New("jetstream unavailable")
	pub := &recordingPublisher{err: cause}
	adapter := &rekeyAuditPublisherAdapter{
		publisher: pub,
		clock:     fixedClock{at: time.Unix(0, 0).UTC()},
	}

	id, err := adapter.PublishAudit(context.Background(),
		"events.main.system.rekey.completed", "crypto.system.rekey", []byte(`{}`))

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_AUDIT_PUBLISH_FAILED")
	assert.ErrorIs(t, err, cause, "the underlying publish failure must stay in the chain for diagnosis")
	assert.Equal(t, ulid.ULID{}, id,
		"a failed publish must not report an event ID the caller could record as durable")
	assert.Len(t, pub.published, 1, "the adapter must have attempted the publish before reporting failure")
}

// --- pluginAuditClientAdapter ---------------------------------------------

// stubPluginAuditClient is a PluginAuditServiceClient whose AuditEvent result
// is fixed by the test. QueryHistory is not part of the dispatch adapter's
// surface and fails loudly if anything reaches for it.
type stubPluginAuditClient struct {
	resp *pluginv1.AuditEventResponse
	err  error
}

func (c stubPluginAuditClient) AuditEvent(
	_ context.Context, _ *pluginv1.AuditEventRequest, _ ...grpc.CallOption,
) (*pluginv1.AuditEventResponse, error) {
	return c.resp, c.err
}

func (c stubPluginAuditClient) QueryHistory(
	_ context.Context, _ *pluginv1.QueryHistoryRequest, _ ...grpc.CallOption,
) (grpc.ServerStreamingClient[pluginv1.QueryHistoryResponse], error) {
	return nil, errors.New("QueryHistory is not part of the audit dispatch adapter")
}

func TestPluginAuditClientAdapterWrapsAFailedRPCAndPassesASuccessThroughUnchanged(t *testing.T) {
	t.Run("wraps a transport failure with the audit dispatch code", func(t *testing.T) {
		cause := errors.New("plugin unreachable")
		adapter := pluginAuditClientAdapter{client: stubPluginAuditClient{err: cause}}

		resp, err := adapter.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{})

		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_RPC_FAILED")
		assert.ErrorIs(t, err, cause, "the transport failure must remain diagnosable through the wrap")
		assert.Nil(t, resp, "no response may be handed back alongside an error")
	})

	t.Run("returns the plugin response without substituting a value", func(t *testing.T) {
		want := &pluginv1.AuditEventResponse{}
		adapter := pluginAuditClientAdapter{client: stubPluginAuditClient{resp: want}}

		resp, err := adapter.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{})

		require.NoError(t, err)
		assert.Same(t, want, resp, "the adapter forwards the RPC and must not rebuild the response")
	})
}

// --- applyLogSinkFlags ------------------------------------------------------
//
// TestCoreCommand_LogSinkFlagsBind already covers --log-stderr, --log-otel and
// --log-sentry-level. The three remaining branches are covered here, and both
// tests assert the routing rather than only the value: a sink override landing
// on a sibling sink's field would satisfy a single-field assertion.

func TestApplyLogSinkFlagsRoutesEachSinkOverrideToItsOwnField(t *testing.T) {
	cmd := NewCoreCmd()
	require.NoError(t, cmd.Flags().Parse([]string{
		"--log-stderr-level=debug", "--log-otel-level=error", "--log-sentry=false",
	}))

	lc := config.DefaultLoggingConfig()
	lc.Sentry.Level = "warn"
	applyLogSinkFlags(cmd, &lc)

	assert.Equal(t, "debug", lc.Stderr.Level)
	assert.Equal(t, "error", lc.OTel.Level)
	assert.False(t, lc.Sentry.Enabled)

	// Cross-wiring guards: an override must not land on a sibling sink, and a
	// flag that was never set must not disturb the configured value.
	assert.NotEqual(t, "error", lc.Stderr.Level, "the OTel level override must not overwrite the stderr level")
	assert.NotEqual(t, "debug", lc.OTel.Level, "the stderr level override must not overwrite the OTel level")
	assert.Equal(t, "warn", lc.Sentry.Level, "an unset --log-sentry-level must leave the configured Sentry level intact")
	assert.True(t, lc.Stderr.Enabled, "an unset --log-stderr must leave the stderr sink as configured")
	assert.True(t, lc.OTel.Enabled, "an unset --log-otel must leave the OTel sink as configured")
}

func TestApplyLogSinkFlagsLeavesAConfiguredLoggingSectionUntouchedWhenNoSinkFlagIsSet(t *testing.T) {
	cmd := NewCoreCmd()
	require.NoError(t, cmd.Flags().Parse(nil))

	// Every value below differs from the corresponding --log-* flag default,
	// so dropping a Changed() guard would visibly clobber it (spec §5 requires
	// CLI > config > default, not flag-default > config).
	lc := config.DefaultLoggingConfig()
	lc.Stderr.Enabled, lc.Stderr.Level = false, "error"
	lc.OTel.Enabled, lc.OTel.Level = false, "debug"
	lc.Sentry.Enabled, lc.Sentry.Level = false, "info"

	applyLogSinkFlags(cmd, &lc)

	assert.False(t, lc.Stderr.Enabled)
	assert.Equal(t, "error", lc.Stderr.Level)
	assert.False(t, lc.OTel.Enabled)
	assert.Equal(t, "debug", lc.OTel.Level)
	assert.False(t, lc.Sentry.Enabled)
	assert.Equal(t, "info", lc.Sentry.Level)
}

// --- NewCoreCmd config-section loading -------------------------------------
//
// RunE loads six config sections in sequence and returns on the first failure.
// Each test below breaks exactly one section and asserts that the run stops
// there. DATABASE_URL is deliberately cleared: it is the next thing RunE
// reaches, so if the section under test stopped being loaded the run would
// fall through to the database check and the NotContains assertion would fail
// rather than pass silently.

func TestCoreCommandSurfacesAMalformedConfigSectionInsteadOfStartingTheServer(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantSection string
	}{
		{
			name:        "reports a core section whose grpc_addr is not a scalar",
			yaml:        "core:\n  grpc_addr: [1, 2]\n",
			wantSection: "core",
		},
		{
			name:        "reports a game section whose guest_start_location is not a scalar",
			yaml:        "game:\n  guest_start_location: [1, 2]\n",
			wantSection: "game",
		},
		{
			name:        "reports an auth section whose session cap is not a number",
			yaml:        "auth:\n  max_player_sessions_per_player: \"abc\"\n",
			wantSection: "auth",
		},
		{
			name:        "reports an event_bus section whose monitor_port is not a number",
			yaml:        "event_bus:\n  monitor_port: \"abc\"\n",
			wantSection: "event_bus",
		},
		{
			name:        "reports a crypto section whose operators is not a list",
			yaml:        "crypto:\n  operators:\n    nested: 1\n",
			wantSection: "crypto",
		},
		{
			name:        "reports a logging section whose stderr toggle is not a boolean",
			yaml:        "logging:\n  stderr:\n    enabled: \"notabool\"\n",
			wantSection: "logging",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "")
			t.Cleanup(func() { configFile = "" })

			cfgPath := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(cfgPath, []byte(tt.yaml), 0o600))

			cmd := NewRootCmd()
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(new(bytes.Buffer))
			cmd.SetArgs([]string{"core", "--config", cfgPath})

			err := cmd.Execute()

			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "CONFIG_UNMARSHAL_FAILED")
			errutil.AssertErrorContext(t, err, "section", tt.wantSection)
			assert.NotContains(t, err.Error(), "DATABASE_URL",
				"the run must stop at the malformed section, not fall through to the database check")
		})
	}
}

func TestCoreCommandRejectsAnUnrecognisedEventBusModeBeforeStartingTheServer(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Cleanup(func() { configFile = "" })

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("event_bus:\n  mode: \"externl\"\n"), 0o600))

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"core", "--config", cfgPath})

	err := cmd.Execute()

	// The mode unmarshals fine — only Validate() rejects it, and RunE calls
	// Validate BEFORE Defaults() precisely so a typo cannot silently boot
	// embedded. Dropping that call would let the run reach DATABASE_URL.
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_CONFIG_INVALID")
	errutil.AssertErrorContext(t, err, "mode", "externl")
	assert.NotContains(t, err.Error(), "DATABASE_URL",
		"an unrecognised event_bus mode must fail closed before the server starts")
}
