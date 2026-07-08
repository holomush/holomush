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
	channelv1 "github.com/holomush/holomush/pkg/proto/holomush/channel/v1"
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
//   - pluginsdk.Handler                    (HandleEvent)
//   - pluginsdk.ServiceProvider            (RegisterServices, Init)
//   - pluginsdk.AttributeResolverProvider  (RegisterAttributeResolver)
//   - pluginsdk.HostEvaluatorAware         (SetHostEvaluator)
//
// service and resolver are pre-allocated in main() so the gRPC server
// registration in RegisterServices/RegisterAttributeResolver (which runs before
// Init) has valid receivers; Init wires the store (and the create rate limiter)
// into both after NewChannelStore returns. The host evaluator is injected via
// SetHostEvaluator before Init (SDK lifecycle).
type channelPlugin struct {
	cfg       channelConfig
	store     *channelStore
	service   *channelService
	resolver  *ChannelResolver
	evaluator pluginsdk.HostEvaluator
}

// HandleEvent is a no-op for this foundation plan. The channel plugin does not
// subscribe to event streams until the live-delivery plan wires HandleEvent.
func (p *channelPlugin) HandleEvent(_ context.Context, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return nil, nil
}

// RegisterServices registers the ChannelServiceServer on the go-plugin gRPC
// transport so the host can proxy channel RPCs to this plugin. It embeds
// UnimplementedChannelServiceServer, so the not-yet-filled RPCs (added in
// 01-05b) return Unimplemented and nothing calls them until then.
// PluginAuditService (01-06) is registered by a later plan.
func (p *channelPlugin) RegisterServices(registrar grpc.ServiceRegistrar) {
	channelv1.RegisterChannelServiceServer(registrar, p.service)
}

// SetHostEvaluator is called by the SDK adapter during Init when the plugin
// declares HostEvaluatorAware (manifest `requires: capability: eval`). The
// evaluator drives the per-RPC ABAC self-enforcement in the channel service
// (and admin-gated commands in 01-07). Wired before Init; nil until then so all
// gated RPCs fail closed.
func (p *channelPlugin) SetHostEvaluator(ev pluginsdk.HostEvaluator) {
	p.evaluator = ev
	if p.service != nil {
		p.service.SetHostEvaluator(ev)
	}
}

// RegisterAttributeResolver registers the ChannelResolver on the go-plugin gRPC
// transport so the host's ABAC engine can resolve channel attributes during
// policy evaluation. The host auto-registers
// holomush.plugin.v1.AttributeResolverService via this AttributeResolverProvider
// method — it MUST NOT appear in the manifest `provides` (that causes
// SERVICE_ALREADY_REGISTERED). The paired manifest `resource_types: [channel]`
// declaration is what drives the host to call GetSchema at load; the two land
// together so the plugin stays loadable (01-03 deferred resource_types to here).
func (p *channelPlugin) RegisterAttributeResolver(registrar grpc.ServiceRegistrar) {
	pluginv1.RegisterAttributeResolverServiceServer(registrar, p.resolver)
}

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
// Postgres schema/role have been provisioned. It decodes and validates config,
// opens the store (running the embedded migrations), then seeds the default
// channel set idempotently. A seed failure fails Init loud so there is no silent
// partial seed. The connection string from ServiceConfig has
// search_path=plugin_core_channels pre-set, so queries target the plugin schema.
func (p *channelPlugin) Init(ctx context.Context, config *pluginv1.ServiceConfig) error {
	connStr := config.GetConnectionString()
	if connStr == "" {
		return oops.Code("CHANNEL_INIT_FAILED").Errorf("connection_string is required")
	}
	if err := p.applyConfig(config); err != nil {
		return err
	}

	store, err := NewChannelStore(ctx, connStr)
	if err != nil {
		return oops.Code("CHANNEL_INIT_FAILED").Wrap(err)
	}
	p.store = store
	p.resolver.store = store
	// Wire the service's store + create rate limiter now that config is decoded.
	// The evaluator was already injected via SetHostEvaluator before Init.
	p.service.store = store
	p.service.limiter = newCreateRateLimiter(p.cfg.CreateRateLimit, createRateWindow, time.Now)

	// Seed the default channel set (incl. Public) idempotently. Safe to re-run
	// on every Init — ON CONFLICT DO NOTHING on lower(name) (D-01, T-01-13).
	if err := store.SeedDefaultChannels(ctx, defaultChannels); err != nil {
		return oops.Code("CHANNEL_INIT_FAILED").Wrap(err)
	}

	slog.InfoContext(
		ctx, "core-channels plugin initialised",
		"storage", "postgres",
		"default_channels", len(defaultChannels),
	)
	return nil
}

func main() {
	plugin := &channelPlugin{
		service:  &channelService{},
		resolver: &ChannelResolver{},
	}
	pluginsdk.ServeWithServices(
		&pluginsdk.ServeConfig{Handler: plugin},
		plugin,
	)
}
