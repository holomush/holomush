package main

import (
	"bytes"
	"strings"
	"testing"
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
