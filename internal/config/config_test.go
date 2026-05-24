// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// testConfig mirrors the structure of a subcommand config for testing.
type testConfig struct {
	Addr      string `koanf:"addr"`
	LogFormat string `koanf:"log_format"`
	Verbose   bool   `koanf:"verbose"`
}

func newTestCmd(cfg *testConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use: "test",
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.Addr, "addr", "localhost:8080", "listen address")
	cmd.Flags().StringVar(&cfg.LogFormat, "log-format", "json", "log format")
	cmd.Flags().BoolVar(&cfg.Verbose, "verbose", false, "verbose output")
	return cmd
}

func TestLoadParsesYAMLConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte("server:\n  addr: \"0.0.0.0:9000\"\n  log_format: \"text\"\n  verbose: true\n"), 0o600)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err = Load(cfgFile, cmd, cfg, "server")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:9000", cfg.Addr)
	assert.Equal(t, "text", cfg.LogFormat)
	assert.True(t, cfg.Verbose)
}

func TestLoadCLIFlagsOverrideConfigFileValues(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte("server:\n  addr: \"0.0.0.0:9000\"\n  log_format: \"text\"\n"), 0o600)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)
	require.NoError(t, cmd.ParseFlags([]string{"--addr", "127.0.0.1:3000"}))

	err = Load(cfgFile, cmd, cfg, "server")
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:3000", cfg.Addr, "CLI flag should override config file")
	assert.Equal(t, "text", cfg.LogFormat, "config file value should remain when flag not set")
}

func TestLoadDefaultFlagValuesDoNotOverrideConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte("server:\n  addr: \"0.0.0.0:9000\"\n  log_format: \"text\"\n"), 0o600)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err = Load(cfgFile, cmd, cfg, "server")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:9000", cfg.Addr, "config file should win over flag default")
	assert.Equal(t, "text", cfg.LogFormat, "config file should win over flag default")
}

func TestLoadExplicitPathMissingReturnsError(t *testing.T) {
	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err := Load("/nonexistent/config.yaml", cmd, cfg, "server")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/nonexistent/config.yaml")
}

func TestLoadDefaultPathMissingReturnsNoError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err := Load("", cmd, cfg, "server")
	require.NoError(t, err)
	assert.Equal(t, "localhost:8080", cfg.Addr)
}

func TestLoadMalformedYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte("{not: valid: yaml: ["), 0o600)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err = Load(cfgFile, cmd, cfg, "server")
	require.Error(t, err)
}

func TestLoadUnknownYAMLKeysAreIgnored(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte("server:\n  addr: \"0.0.0.0:9000\"\n  unknown_key: \"should be ignored\"\n  another_unknown: 42\n"), 0o600)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err = Load(cfgFile, cmd, cfg, "server")
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0:9000", cfg.Addr)
}

func TestLoadEmptyConfigFileUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(""), 0o600)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err = Load(cfgFile, cmd, cfg, "server")
	require.NoError(t, err)
	assert.Equal(t, "localhost:8080", cfg.Addr)
}

func TestLoadParsesGameConfigSection(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte("game:\n  guest_start_location: \"01JMHZ5H3ZSBVTGARX4MSS1MBH\"\n"), 0o600)
	require.NoError(t, err)

	cfg := &GameConfig{}
	cmd := &cobra.Command{Use: "test"}

	err = Load(cfgFile, cmd, cfg, "game")
	require.NoError(t, err)
	assert.Equal(t, "01JMHZ5H3ZSBVTGARX4MSS1MBH", cfg.GuestStartLocation)
}

func TestDefaultAuthConfigMaxPlayerSessionsIsTen(t *testing.T) {
	cfg := DefaultAuthConfig()
	assert.Equal(t, 10, cfg.MaxPlayerSessionsPerPlayer)
	assert.Equal(t, DefaultMaxPlayerSessionsPerPlayer, cfg.MaxPlayerSessionsPerPlayer)
}

func TestLoadParsesAuthConfigMaxPlayerSessionsPerPlayer(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte("auth:\n  max_player_sessions_per_player: 3\n"), 0o600)
	require.NoError(t, err)

	cfg := DefaultAuthConfig()
	cmd := &cobra.Command{Use: "test"}

	err = Load(cfgFile, cmd, &cfg, "auth")
	require.NoError(t, err)
	assert.Equal(t, 3, cfg.MaxPlayerSessionsPerPlayer)
}

func TestLoadParsesPluginTrustAllowlist(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte("game:\n  plugin_trust_allowlist:\n    - exotic-plugin\n    - another-plugin\n"), 0o600)
	require.NoError(t, err)

	cfg := &GameConfig{}
	cmd := &cobra.Command{Use: "test"}

	err = Load(cfgFile, cmd, cfg, "game")
	require.NoError(t, err)
	assert.Equal(t, []string{"exotic-plugin", "another-plugin"}, cfg.PluginTrustAllowlist)
}

func TestLoadExplicitPathPermissionDeniedReturnsError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permission test skipped when running as root")
	}

	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte("server:\n  addr: \"0.0.0.0:9000\"\n"), 0o600)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(cfgFile, 0o000))
	t.Cleanup(func() { _ = os.Chmod(cfgFile, 0o600) })

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err = Load(cfgFile, cmd, cfg, "server")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "not found", "permission error should not be reported as 'not found'")
}

func TestLoadHyphenFlagNamesMatchUnderscoreYAMLKeys(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte("server:\n  log_format: \"text\"\n"), 0o600)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)
	require.NoError(t, cmd.ParseFlags([]string{"--log-format", "json"}))

	err = Load(cfgFile, cmd, cfg, "server")
	require.NoError(t, err)
	assert.Equal(t, "json", cfg.LogFormat, "hyphenated flag should override underscored YAML key")
}

func TestDefaultCryptoConfigIsEmpty(t *testing.T) {
	cfg := DefaultCryptoConfig()
	assert.Empty(t, cfg.Operators)
}

func TestCryptoConfig_Defaults_PopulatesZeroFields(t *testing.T) {
	// A zero-value CryptoConfig must get all default durations applied.
	cfg := CryptoConfig{}.Defaults()
	assert.Equal(t, DefaultRekeyCheckpointTTL, cfg.RekeyCheckpointTTL)
	assert.Equal(t, DefaultRekeyCheckpointSweepInterval, cfg.RekeyCheckpointSweepInterval)
	assert.Equal(t, DefaultOperatorReadDefaultWindow, cfg.OperatorReadDefaultWindow)
	assert.Equal(t, DefaultOperatorReadMaxWindow, cfg.OperatorReadMaxWindow)
	assert.Equal(t, DefaultOperatorReadWriteDeadline, cfg.OperatorReadWriteDeadline)
	assert.Equal(t, DefaultOperatorReadApprovalTTL, cfg.OperatorReadApprovalTTL)
	assert.NotNil(t, cfg.Operators)
}

func TestCryptoConfig_Defaults_DoesNotOverrideExplicitValues(t *testing.T) {
	// Non-zero values must be preserved (Defaults is idempotent, not clobber).
	cfg := CryptoConfig{
		Operators:                    []string{"op1"},
		RekeyCheckpointTTL:           48 * 60 * 60 * 1000000000, // 48h
		RekeyCheckpointSweepInterval: 2 * 60 * 60 * 1000000000,  // 2h
		OperatorReadDefaultWindow:    2 * 60 * 60 * 1000000000,  // 2h
		OperatorReadMaxWindow:        60 * 24 * 60 * 60 * 1000000000,
		OperatorReadWriteDeadline:    60 * 1000000000,
		OperatorReadApprovalTTL:      10 * 60 * 1000000000,
	}.Defaults()
	assert.Equal(t, []string{"op1"}, cfg.Operators)
	// All durations were non-zero so they must survive unchanged.
	assert.NotEqual(t, DefaultRekeyCheckpointTTL, cfg.RekeyCheckpointTTL)
}

func TestLoadParsesCryptoOperators(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`crypto:
  operators:
    - "01HZAVGE83MGFEXQQH5SP9NXKF"
    - "01HZAVGE83MGFEXQQH5SP9NXKG"
`), 0o600)
	require.NoError(t, err)

	cfg := DefaultCryptoConfig()
	cmd := &cobra.Command{Use: "test"}
	require.NoError(t, Load(cfgFile, cmd, &cfg, "crypto"))

	assert.Equal(t, []string{
		"01HZAVGE83MGFEXQQH5SP9NXKF",
		"01HZAVGE83MGFEXQQH5SP9NXKG",
	}, cfg.Operators)
}

func TestLoadCryptoMissingSectionIsEmpty(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`other:
  setting: value
`), 0o600)
	require.NoError(t, err)

	cfg := DefaultCryptoConfig()
	cmd := &cobra.Command{Use: "test"}
	require.NoError(t, Load(cfgFile, cmd, &cfg, "crypto"))
	assert.Empty(t, cfg.Operators)
}

func TestLoadCryptoOperatorsEmptyListIsEmpty(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`crypto:
  operators: []
`), 0o600)
	require.NoError(t, err)

	cfg := DefaultCryptoConfig()
	cmd := &cobra.Command{Use: "test"}
	require.NoError(t, Load(cfgFile, cmd, &cfg, "crypto"))
	assert.Empty(t, cfg.Operators)
}

func TestLoadCryptoOperatorsMalformedFails(t *testing.T) {
	// Operators must be a list of strings; nesting a non-string element
	// (here, a map) under operators is unambiguously malformed and must
	// fail with CONFIG_UNMARSHAL_FAILED rather than silently coerce.
	// (Note: koanf permissively coerces a bare scalar into a single-element
	// list, so we test a structurally-incompatible element type instead.)
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`crypto:
  operators:
    - {nested: map}
`), 0o600)
	require.NoError(t, err)

	cfg := DefaultCryptoConfig()
	cmd := &cobra.Command{Use: "test"}
	err = Load(cfgFile, cmd, &cfg, "crypto")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CONFIG_UNMARSHAL_FAILED")
}

func TestLoadUsesXDGConfigPathWhenNoPathFlagSet(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	holoDir := filepath.Join(dir, "holomush")
	require.NoError(t, os.MkdirAll(holoDir, 0o700))
	err := os.WriteFile(filepath.Join(holoDir, "config.yaml"), []byte("server:\n  addr: \"from-xdg:9000\"\n"), 0o600)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err = Load("", cmd, cfg, "server")
	require.NoError(t, err)
	assert.Equal(t, "from-xdg:9000", cfg.Addr)
}
