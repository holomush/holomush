// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"time"

	"github.com/samber/oops"
)

// Mode selects between embedded and external NATS deployments.
type Mode string

const (
	// ModeEmbedded runs NATS JetStream in-process via DontListen + InProcessServer.
	ModeEmbedded Mode = "embedded"
	// ModeExternal connects to an external NATS cluster addressed by Config.URL.
	// Embedded remains the zero-config default; external requires deliberate
	// opt-in and a non-empty URL (enforced by Validate).
	ModeExternal Mode = "external"
)

// Default values for Config.Defaults.
const (
	defaultStreamMaxAge = 30 * 24 * time.Hour
	defaultDupeWindow   = 30 * time.Minute
	defaultGameID       = "main"
	// defaultDLQMaxAge bounds the audit dead-letter stream's retention so a
	// poison flood cannot eat the disk (D-12). ~30 days; operators may override
	// via event_bus.dlq.max_age.
	defaultDLQMaxAge = 30 * 24 * time.Hour
)

// Config controls the EventBus subsystem.
//
// StoreDir left blank defers path resolution to Start-time via
// internal/xdg. This keeps Defaults() pure (no filesystem side effects)
// and lets test helpers swap in a t.TempDir().
type Config struct {
	Mode     Mode   `koanf:"mode"`
	GameID   string `koanf:"game_id"`
	StoreDir string `koanf:"store_dir"`

	StreamMaxAge time.Duration `koanf:"stream_max_age"`
	DupeWindow   time.Duration `koanf:"dupe_window"`

	// MonitorPort is the HTTP port the embedded NATS server binds for its
	// monitoring endpoint (/varz, /jsz, etc). 0 disables. -1 selects a
	// random port — the subsystem reads the actual bound port back via
	// server.MonitorAddr() before starting the Prometheus exporter.
	MonitorPort int `koanf:"monitor_port"`

	// PrometheusExporter toggles the in-process prometheus-nats-exporter.
	// Requires MonitorPort to be set (> 0 or -1).
	PrometheusExporter bool `koanf:"prometheus_exporter"`

	// ExporterPort is the TCP port the Prometheus exporter's scrape
	// endpoint listens on (loopback only). 0 selects an ephemeral port.
	ExporterPort int `koanf:"exporter_port"`

	// External-mode connection settings (Mode == ModeExternal).
	//
	// URL is the NATS server/cluster URL (e.g. nats://host:4222). Required in
	// external mode — Validate() fails closed when it is empty (D-01/D-02).
	// Credentials is a path to a NATS .creds file (JWT/NKey decentralized
	// auth, D-04); empty means no creds-file auth (dev clusters may carry
	// user:pass in the URL). TLS carries an optional mTLS/private-CA block.
	URL         string    `koanf:"url"`
	Credentials string    `koanf:"credentials"`
	TLS         TLSConfig `koanf:"tls"`

	// Provision selects whether the server idempotently creates/updates
	// JetStream streams (D-03). Nil resolves to true via IsProvision; set a
	// non-nil false for locked-down clusters where the server account lacks
	// $JS.API stream-admin permissions — then the server verifies existence
	// and fails closed on mismatch instead of creating. Provision is a *bool
	// so an explicit false survives Defaults() (mirrors CryptoConfig.Enabled).
	Provision *bool `koanf:"provision"`

	// DLQ bounds the audit dead-letter stream (D-12).
	DLQ DLQConfig `koanf:"dlq"`

	// Crypto gates the Phase 3a sensitivity-aware crypto path.
	// See spec §11.1 phase 3.
	Crypto CryptoConfig `koanf:"crypto"`
}

// TLSConfig carries an optional TLS block for external-mode connections
// (mTLS / private CAs, D-04). All fields are filesystem paths; empty means
// the corresponding material is not supplied.
type TLSConfig struct {
	CA   string `koanf:"ca"`
	Cert string `koanf:"cert"`
	Key  string `koanf:"key"`
}

// DLQConfig bounds the audit dead-letter stream's retention (D-12) so a
// poison flood cannot exhaust storage.
type DLQConfig struct {
	// MaxAge caps how long dead letters are retained. Zero resolves to
	// defaultDLQMaxAge via Defaults().
	MaxAge time.Duration `koanf:"max_age"`
	// MaxBytes caps the DLQ stream size in bytes. Zero means unbounded by
	// size (age-capped only).
	MaxBytes int64 `koanf:"max_bytes"`
}

// CryptoConfig gates the Phase 3a sensitivity-aware crypto path.
// As of Phase 3d the effective default is Enabled=true (see IsEnabled).
// Operators MAY explicitly set `crypto.enabled: false` to disable the
// path; that explicit value MUST survive Defaults() (Enabled is a
// pointer to disambiguate "unset" from "explicitly false" — Go bool
// zero-value can't carry that distinction). See spec §11.1 phase 3.
type CryptoConfig struct {
	// Enabled is nil when the operator did not set the field; nil
	// resolves to true via IsEnabled. Set to a non-nil false to
	// explicitly disable.
	Enabled *bool `koanf:"enabled"`
}

// IsEnabled returns the effective crypto-enabled flag, applying
// Phase 3d's default-true when the operator did not set the field.
// Use this helper everywhere — never dereference Enabled directly.
func (c CryptoConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// IsProvision returns the effective provision flag (D-03), applying the
// default-true when the operator did not set the field. Use this helper
// everywhere — never dereference Provision directly.
func (c Config) IsProvision() bool {
	if c.Provision == nil {
		return true
	}
	return *c.Provision
}

// Defaults applies the documented defaults to any zero-value field.
// StoreDir is intentionally left blank — the subsystem resolves it via
// xdg.DataDir() + "/jetstream" at Start time.
//
// Crypto.Enabled and Provision are intentionally NOT touched here — they
// are *bool so that an explicit false survives Defaults(). The default-true
// behavior is applied lazily by CryptoConfig.IsEnabled() / Config.IsProvision().
func (c Config) Defaults() Config {
	if c.Mode == "" {
		c.Mode = ModeEmbedded
	}
	if c.GameID == "" {
		c.GameID = defaultGameID
	}
	if c.StreamMaxAge == 0 {
		c.StreamMaxAge = defaultStreamMaxAge
	}
	if c.DupeWindow == 0 {
		c.DupeWindow = defaultDupeWindow
	}
	if c.DLQ.MaxAge == 0 {
		c.DLQ.MaxAge = defaultDLQMaxAge
	}
	return c
}

// Validate checks the config for fail-closed correctness at config-validation
// time, before any dial or filesystem access (D-01/D-02). External mode with
// no URL is rejected so the server refuses to boot rather than silently
// falling back to embedded. Validate is pure: it performs no I/O.
func (c Config) Validate() error {
	if c.Mode == ModeExternal && c.URL == "" {
		return oops.Code("EVENTBUS_CONFIG_INVALID").
			With("mode", string(c.Mode)).
			Errorf("event_bus mode is external but url is empty")
	}
	return nil
}
