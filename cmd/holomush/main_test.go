package main

import (
	"bytes"
	"testing"
)

func TestRootCommand_HasExpectedSubcommands(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	subcommands := []string{"gateway", "core", "migrate", "status"}
	for _, sub := range subcommands {
		if !bytes.Contains([]byte(output), []byte(sub)) {
			t.Errorf("Help missing %q command", sub)
		}
	}
}

func TestRootCommand_VersionFlag(t *testing.T) {
	cmd := NewRootCmd()
	cmd.Version = "test-version"
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("test-version")) {
		t.Errorf("Version output missing version info: %s", output)
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
	if !bytes.Contains([]byte(output), []byte("--config")) {
		t.Error("Gateway missing --config flag")
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
	if !bytes.Contains([]byte(output), []byte("--config")) {
		t.Error("Core missing --config flag")
	}
}

func TestMigrateCommand_Help(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"migrate", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("--config")) {
		t.Error("Migrate missing --config flag")
	}
}

func TestStatusCommand_Help(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"status", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("--config")) {
		t.Error("Status missing --config flag")
	}
}
