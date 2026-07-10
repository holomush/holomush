// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
)

func TestConfigDefaultsUnsetModeResolvesToEmbedded(t *testing.T) {
	cfg := eventbus.Config{}.Defaults()
	assert.Equal(t, eventbus.ModeEmbedded, cfg.Mode,
		"embedded is the zero-config default")
}

func TestConfigDefaultsExplicitExternalModeSurvives(t *testing.T) {
	cfg := eventbus.Config{Mode: eventbus.ModeExternal, URL: "nats://x:4222"}.Defaults()
	assert.Equal(t, eventbus.ModeExternal, cfg.Mode,
		"an explicitly requested external mode must survive Defaults()")
}

func TestConfigDefaultsProvisionDefaultsTrueWhenUnset(t *testing.T) {
	cfg := eventbus.Config{}.Defaults()
	assert.True(t, cfg.IsProvision(),
		"provision defaults to true when the operator did not set it (D-03)")
}

func TestConfigProvisionExplicitFalseSurvivesDefaults(t *testing.T) {
	falseV := false
	cfg := eventbus.Config{Provision: &falseV}.Defaults()
	assert.False(t, cfg.IsProvision(),
		"an explicit provision=false MUST survive Defaults() (D-03 opt-out seam)")
}

func TestConfigDefaultsDLQMaxAgeFilledWhenZero(t *testing.T) {
	cfg := eventbus.Config{}.Defaults()
	assert.Equal(t, 30*24*time.Hour, cfg.DLQ.MaxAge,
		"DLQ.MaxAge defaults to ~30d retention (D-12)")
}

func TestConfigDefaultsDLQMaxAgePreservesExplicitValue(t *testing.T) {
	cfg := eventbus.Config{DLQ: eventbus.DLQConfig{MaxAge: 1 * time.Hour}}.Defaults()
	assert.Equal(t, 1*time.Hour, cfg.DLQ.MaxAge,
		"an explicit DLQ.MaxAge must survive Defaults()")
}

// TestConfigDecodesExternalModeKoanfKeys locks the external-mode koanf
// vocabulary (D-01/D-04/D-12): mode, url, credentials, tls.{ca,cert,key},
// provision, dlq.{max_age,max_bytes} decode into the reconciled fields.
func TestConfigDecodesExternalModeKoanfKeys(t *testing.T) {
	const raw = `
mode: external
url: nats://cluster:4222
credentials: /etc/holomush/server.creds
tls:
  ca: /etc/holomush/ca.pem
  cert: /etc/holomush/client.pem
  key: /etc/holomush/client-key.pem
provision: false
dlq:
  max_age: 168h
  max_bytes: 1048576
`
	path := filepath.Join(t.TempDir(), "event_bus.yaml")
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))

	k := koanf.New(".")
	require.NoError(t, k.Load(file.Provider(path), yaml.Parser()))

	var cfg eventbus.Config
	require.NoError(t, k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{Tag: "koanf"}))

	assert.Equal(t, eventbus.ModeExternal, cfg.Mode)
	assert.Equal(t, "nats://cluster:4222", cfg.URL)
	assert.Equal(t, "/etc/holomush/server.creds", cfg.Credentials)
	assert.Equal(t, "/etc/holomush/ca.pem", cfg.TLS.CA)
	assert.Equal(t, "/etc/holomush/client.pem", cfg.TLS.Cert)
	assert.Equal(t, "/etc/holomush/client-key.pem", cfg.TLS.Key)
	require.NotNil(t, cfg.Provision)
	assert.False(t, *cfg.Provision, "provision: false decodes to an explicit false")
	assert.Equal(t, 168*time.Hour, cfg.DLQ.MaxAge)
	assert.Equal(t, int64(1048576), cfg.DLQ.MaxBytes)
}

func TestCryptoEnabledDefaultsToTrue(t *testing.T) {
	cfg := eventbus.Config{}.Defaults()
	assert.True(t, cfg.Crypto.IsEnabled(), "Phase 3d ships live (default-true when unset)")
}

// TestCryptoEnabledExplicitFalseSurvivesDefaults locks the security
// invariant flagged during Phase 3d code review: an operator who sets
// crypto.enabled=false in config MUST get a disabled crypto path.
// Defaults() MUST NOT clobber an explicit false.
func TestCryptoEnabledExplicitFalseSurvivesDefaults(t *testing.T) {
	falseV := false
	cfg := eventbus.Config{
		Crypto: eventbus.CryptoConfig{Enabled: &falseV},
	}.Defaults()
	assert.False(t, cfg.Crypto.IsEnabled(),
		"explicit operator-set false MUST survive Defaults()")
}

// TestCryptoEnabledExplicitTrueSurvivesDefaults symmetric — explicit
// true survives identically to explicit false.
func TestCryptoEnabledExplicitTrueSurvivesDefaults(t *testing.T) {
	trueV := true
	cfg := eventbus.Config{
		Crypto: eventbus.CryptoConfig{Enabled: &trueV},
	}.Defaults()
	assert.True(t, cfg.Crypto.IsEnabled(),
		"explicit operator-set true MUST survive Defaults()")
}
