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

	MonitorPort        int  `koanf:"monitor_port"`
	PrometheusExporter bool `koanf:"prometheus_exporter"`

	// Cluster-mode only (unused in Phase A).
	ClusterURL      string `koanf:"cluster_url"`
	CredentialsFile string `koanf:"credentials_file"`
}

// Defaults applies the documented defaults to any zero-value field.
// StoreDir is intentionally left blank — the subsystem resolves it via
// xdg.DataDir() + "/jetstream" at Start time.
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
