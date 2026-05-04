// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import "time"

// Mode selects between embedded and clustered NATS deployments.
type Mode string

const (
	// ModeEmbedded runs NATS JetStream in-process via DontListen + InProcessServer.
	ModeEmbedded Mode = "embedded"
	// ModeCluster connects to an external NATS cluster. Reserved for a future
	// phase; not implemented in Phase A.
	ModeCluster Mode = "cluster"
)

// Default values for Config.Defaults.
const (
	defaultStreamMaxAge = 30 * 24 * time.Hour
	defaultDupeWindow   = 30 * time.Minute
	defaultGameID       = "main"
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

	// Cluster-mode only (unused in Phase A).
	ClusterURL      string `koanf:"cluster_url"`
	CredentialsFile string `koanf:"credentials_file"`

	// Crypto gates the Phase 3a sensitivity-aware crypto path.
	// See spec §11.1 phase 3.
	Crypto CryptoConfig `koanf:"crypto"`
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

// Defaults applies the documented defaults to any zero-value field.
// StoreDir is intentionally left blank — the subsystem resolves it via
// xdg.DataDir() + "/jetstream" at Start time.
//
// Crypto.Enabled is intentionally NOT touched here — it is a *bool so
// that an explicit false survives Defaults(). The default-true behavior
// is applied lazily by CryptoConfig.IsEnabled().
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
	return c
}
