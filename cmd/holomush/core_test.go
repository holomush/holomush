package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestCoreCommand_Flags(t *testing.T) {
	cmd := NewCoreCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

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
		if !strings.Contains(output, flag) {
			t.Errorf("Help missing %q flag", flag)
		}
	}
}

func TestCoreCommand_DefaultValues(t *testing.T) {
	cmd := NewCoreCmd()

	// Check default grpc-addr
	grpcAddr, err := cmd.Flags().GetString("grpc-addr")
	if err != nil {
		t.Fatalf("Failed to get grpc-addr flag: %v", err)
	}
	if grpcAddr != "localhost:9000" {
		t.Errorf("grpc-addr default = %q, want %q", grpcAddr, "localhost:9000")
	}

	// Check default control-addr
	controlAddr, err := cmd.Flags().GetString("control-addr")
	if err != nil {
		t.Fatalf("Failed to get control-addr flag: %v", err)
	}
	if controlAddr != "127.0.0.1:9001" {
		t.Errorf("control-addr default = %q, want %q", controlAddr, "127.0.0.1:9001")
	}

	// Check default metrics-addr
	metricsAddr, err := cmd.Flags().GetString("metrics-addr")
	if err != nil {
		t.Fatalf("Failed to get metrics-addr flag: %v", err)
	}
	if metricsAddr != "127.0.0.1:9100" {
		t.Errorf("metrics-addr default = %q, want %q", metricsAddr, "127.0.0.1:9100")
	}

	// Check default log-format
	logFormat, err := cmd.Flags().GetString("log-format")
	if err != nil {
		t.Fatalf("Failed to get log-format flag: %v", err)
	}
	if logFormat != "json" {
		t.Errorf("log-format default = %q, want %q", logFormat, "json")
	}

	// Check other flags have empty defaults
	dataDir, err := cmd.Flags().GetString("data-dir")
	if err != nil {
		t.Fatalf("Failed to get data-dir flag: %v", err)
	}
	if dataDir != "" {
		t.Errorf("data-dir default = %q, want empty string", dataDir)
	}

	gameID, err := cmd.Flags().GetString("game-id")
	if err != nil {
		t.Fatalf("Failed to get game-id flag: %v", err)
	}
	if gameID != "" {
		t.Errorf("game-id default = %q, want empty string", gameID)
	}
}

func TestCoreCommand_Properties(t *testing.T) {
	cmd := NewCoreCmd()

	if cmd.Use != "core" {
		t.Errorf("Use = %q, want %q", cmd.Use, "core")
	}

	if !strings.Contains(cmd.Short, "core") {
		t.Error("Short description should mention core")
	}

	if !strings.Contains(cmd.Long, "game engine") {
		t.Error("Long description should mention game engine")
	}
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
	if err == nil {
		t.Fatal("Expected error when DATABASE_URL is not set")
	}

	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("Error should mention DATABASE_URL, got: %v", err)
	}
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
	if err == nil {
		t.Fatal("Expected error with invalid DATABASE_URL")
	}

	if !strings.Contains(err.Error(), "connect") && !strings.Contains(err.Error(), "database") {
		t.Errorf("Error should mention connection/database issue, got: %v", err)
	}
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

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			addr, _ := cmd.Flags().GetString("grpc-addr")
			if addr != tt.wantAddr {
				t.Errorf("grpc-addr = %q, want %q", addr, tt.wantAddr)
			}

			fmtVal, _ := cmd.Flags().GetString("log-format")
			if fmtVal != tt.wantFmt {
				t.Errorf("log-format = %q, want %q", fmtVal, tt.wantFmt)
			}
		})
	}
}

func TestSetupLogging(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		wantError bool
	}{
		{
			name:      "json format",
			format:    "json",
			wantError: false,
		},
		{
			name:      "text format",
			format:    "text",
			wantError: false,
		},
		{
			name:      "invalid format",
			format:    "invalid",
			wantError: true,
		},
		{
			name:      "empty format",
			format:    "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := setupLogging(tt.format)
			if (err != nil) != tt.wantError {
				t.Errorf("setupLogging(%q) error = %v, wantError = %v", tt.format, err, tt.wantError)
			}
		})
	}
}

func TestEnsureTLSCerts(t *testing.T) {
	// Create a temp directory for certs
	tmpDir, err := os.MkdirTemp("", "holomush-test-certs-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

	gameID := "test-game-id"

	// First call should generate new certs
	config1, err := ensureTLSCerts(tmpDir, gameID)
	if err != nil {
		t.Fatalf("ensureTLSCerts() error = %v", err)
	}
	if config1 == nil {
		t.Fatal("ensureTLSCerts() returned nil config")
	}

	// Verify certificates were created
	expectedFiles := []string{
		"root-ca.crt",
		"root-ca.key",
		"core.crt",
		"core.key",
		"gateway.crt",
		"gateway.key",
	}
	for _, file := range expectedFiles {
		path := tmpDir + "/" + file
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Expected file %s was not created", file)
		}
	}

	// Second call should load existing certs
	config2, err := ensureTLSCerts(tmpDir, gameID)
	if err != nil {
		t.Fatalf("ensureTLSCerts() second call error = %v", err)
	}
	if config2 == nil {
		t.Fatal("ensureTLSCerts() second call returned nil config")
	}
}

func TestCoreCommand_Help(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"core", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

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
		if !strings.Contains(output, phrase) {
			t.Errorf("Help missing phrase %q", phrase)
		}
	}
}
