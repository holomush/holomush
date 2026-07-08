// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package main implements the core-channels binary plugin: the host-loaded
// process that owns the channel domain (persistent, named, location-independent
// communication channels — CHAN-01). See plugin.yaml for the manifest declaring
// its resource type, event domain, audit table, and runtime config.
//
// This foundation skeleton owns the plugin-owned Postgres schema (migrations),
// the domain type/state model (types.go), the store (store.go), and idempotent
// default-channel seeding at Init. The resolver, service, commands, audit RPC,
// and prune scheduler are wired by later plans.
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/samber/oops"
	"google.golang.org/grpc"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// channelConfig is the mapstructure target for the plugin_config block declared
// in plugin.yaml. Keys match the manifest config: section; mapstructure tags map
// snake_case config keys to typed Go fields. DecodeConfig applies the
// string->typed coercion; applyConfig does the plugin-owned semantic validation.
type channelConfig struct {
	RetentionWindow time.Duration `mapstructure:"retention_window"`
	ReplayCount     int           `mapstructure:"replay_count"`
	PruneInterval   time.Duration `mapstructure:"prune_interval"`
	CreateRateLimit int           `mapstructure:"create_rate_limit"`
	ScrollbackCap   int           `mapstructure:"scrollback_cap"`
}

// channelPlugin is the binary plugin entry struct. It implements:
//   - pluginsdk.Handler         (HandleEvent)
//   - pluginsdk.ServiceProvider (RegisterServices, Init)
type channelPlugin struct {
	cfg channelConfig
}

// HandleEvent is a no-op for this foundation plan. The channel plugin does not
// subscribe to event streams until the live-delivery plan wires HandleEvent.
func (p *channelPlugin) HandleEvent(_ context.Context, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return nil, nil
}

// RegisterServices is a no-op for this foundation plan. ChannelService (01-05)
// and PluginAuditService (01-06) are registered by later plans; the manifest
// declares no `provides` yet, so nothing is registered here.
func (p *channelPlugin) RegisterServices(_ grpc.ServiceRegistrar) {}

// applyConfig decodes the host-delivered plugin_config into p.cfg and performs
// plugin-owned semantic validation. The host validates the generic duration/int
// types but not their meaning; a non-positive interval/count would break the
// prune scheduler, replay, or rate limiter, so reject it fail-loud at Init.
func (p *channelPlugin) applyConfig(config *pluginv1.ServiceConfig) error {
	decoded, err := pluginsdk.DecodeConfig[channelConfig](config)
	if err != nil {
		return oops.Code("CHANNEL_INIT_FAILED").Wrap(err)
	}
	if decoded.RetentionWindow <= 0 {
		return oops.Code("CHANNEL_INIT_FAILED").
			With("retention_window", decoded.RetentionWindow.String()).
			Errorf("retention_window must be positive")
	}
	if decoded.PruneInterval <= 0 {
		return oops.Code("CHANNEL_INIT_FAILED").
			With("prune_interval", decoded.PruneInterval.String()).
			Errorf("prune_interval must be positive")
	}
	if decoded.ReplayCount <= 0 {
		return oops.Code("CHANNEL_INIT_FAILED").
			With("replay_count", decoded.ReplayCount).
			Errorf("replay_count must be positive")
	}
	if decoded.CreateRateLimit <= 0 {
		return oops.Code("CHANNEL_INIT_FAILED").
			With("create_rate_limit", decoded.CreateRateLimit).
			Errorf("create_rate_limit must be positive")
	}
	if decoded.ScrollbackCap <= 0 {
		return oops.Code("CHANNEL_INIT_FAILED").
			With("scrollback_cap", decoded.ScrollbackCap).
			Errorf("scrollback_cap must be positive")
	}
	p.cfg = decoded
	return nil
}

// Init is called by the host after the gRPC connection is established and the
// Postgres schema/role have been provisioned. This foundation plan decodes and
// validates config; store open + default-channel seeding are wired by the
// seeding task. The connection string from ServiceConfig has
// search_path=plugin_core_channels pre-set.
func (p *channelPlugin) Init(ctx context.Context, config *pluginv1.ServiceConfig) error {
	if config.GetConnectionString() == "" {
		return oops.Code("CHANNEL_INIT_FAILED").Errorf("connection_string is required")
	}
	if err := p.applyConfig(config); err != nil {
		return err
	}

	slog.InfoContext(
		ctx, "core-channels plugin initialised",
		"storage", "postgres",
	)
	return nil
}

func main() {
	plugin := &channelPlugin{}
	pluginsdk.ServeWithServices(
		&pluginsdk.ServeConfig{Handler: plugin},
		plugin,
	)
}
