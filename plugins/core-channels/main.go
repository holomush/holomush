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
	auditSrv  *ChannelAuditServer
	evaluator pluginsdk.HostEvaluator
	// channels resolves a channel name to its row for the command layer (01-07).
	// Wired to the store in Init; the command handlers hold it as a narrow
	// interface so unit tests can inject a fake resolver.
	channels channelNameResolver
	// streamStore backs QuerySessionStreams (session-establishment channel
	// subscription, 01-08). Wired to the store in Init; a narrow interface so
	// unit tests inject a fake. Nil until Init, so QuerySessionStreams fails
	// closed rather than contributing an empty set silently.
	streamStore sessionStreamStore
}

// sessionStreamStore is the narrow persistence dependency of QuerySessionStreams:
// the character's non-banned memberships, the seeded default channel set, and the
// per-default ban filter. *channelStore satisfies it; tests substitute a fake.
type sessionStreamStore interface {
	ListForCharacter(ctx context.Context, characterID string) ([]channelRow, error)
	ListDefaultChannels(ctx context.Context) ([]channelRow, error)
	IsBannedFrom(ctx context.Context, channelID, characterID string) (bool, error)
}

// QuerySessionStreams implements pluginsdk.SessionStreamsHandler: at session
// establishment (connect/reconnect) the host asks core-channels which streams to
// subscribe the entering character to. It returns the domain-RELATIVE
// channel.<id> ref (via relativeChannelStream, R2-A — NEVER a pre-qualified
// events. subject) for the deduplicated UNION of:
//
//   - (a) every channel the character is a non-banned member of (ListForCharacter
//     already excludes banned + archived), and
//   - (b) every seeded default channel (ListDefaultChannels) the character is not
//     banned from — guest auto-join is this default-channel union, NOT a
//     membership-row write at establishment (resource-side, plaintext; D-01/D-04).
//
// Dedup is by channel id (a default the character is also an explicit member of
// appears once). The list is empty (no error) ONLY when there are neither
// memberships nor seeded defaults. Every returned ref is a RELATIVE own-domain
// channel.<id>, so ALL pass 01-02's shared establishment namespace fence
// (Manager.QuerySessionStreams → AuthorizePluginStreamContribution); the host
// Qualifies each to events.<game>.channel.<id> (the emit subject).
func (p *channelPlugin) QuerySessionStreams(ctx context.Context, req pluginsdk.SessionStreamsRequest) ([]string, error) {
	if p.streamStore == nil {
		return nil, oops.Code("CHANNEL_SESSION_STREAMS_FAILED").
			Errorf("session-stream store not configured")
	}

	memberships, err := p.streamStore.ListForCharacter(ctx, req.CharacterID)
	if err != nil {
		return nil, oops.Code("CHANNEL_SESSION_STREAMS_FAILED").
			With("character_id", req.CharacterID).Wrap(err)
	}
	defaults, err := p.streamStore.ListDefaultChannels(ctx)
	if err != nil {
		return nil, oops.Code("CHANNEL_SESSION_STREAMS_FAILED").
			With("character_id", req.CharacterID).Wrap(err)
	}

	seen := make(map[string]struct{}, len(memberships)+len(defaults))
	streams := make([]string, 0, len(memberships)+len(defaults))

	for i := range memberships {
		id := memberships[i].ID
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		streams = append(streams, relativeChannelStream(id))
	}

	for i := range defaults {
		id := defaults[i].ID
		if _, dup := seen[id]; dup {
			continue
		}
		banned, banErr := p.streamStore.IsBannedFrom(ctx, id, req.CharacterID)
		if banErr != nil {
			return nil, oops.Code("CHANNEL_SESSION_STREAMS_FAILED").
				With("character_id", req.CharacterID).With("channel_id", id).Wrap(banErr)
		}
		if banned {
			continue
		}
		seen[id] = struct{}{}
		streams = append(streams, relativeChannelStream(id))
	}

	return streams, nil
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
// It also registers the PluginAuditService that serves the plugin-owned audit
// subject prefix (events.*.channel.>) per the manifest's audit block: the host
// forwards audit deliveries to AuditEvent and routes channel-subject
// QueryHistory here.
func (p *channelPlugin) RegisterServices(registrar grpc.ServiceRegistrar) {
	channelv1.RegisterChannelServiceServer(registrar, p.service)
	pluginv1.RegisterPluginAuditServiceServer(registrar, p.auditSrv)
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

// SetEventSink is called by the SDK adapter during Init when the plugin
// declares EventSinkAware. It forwards the sink to the channel service so
// service-owned RPC handlers can emit live channel content + notice events
// through the shared EventBus (CHAN-03). Emit is fence-self-gated, so no
// manifest capability declaration is required. The concrete emitter is built in
// Init once the game id is known.
func (p *channelPlugin) SetEventSink(sink pluginsdk.EventSink) {
	if p.service != nil {
		p.service.SetEventSink(sink)
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
	// The command layer resolves channel names via the store (name → id).
	p.channels = store
	// QuerySessionStreams reads memberships ∪ default channels from the store
	// (01-08 session-establishment subscription).
	p.streamStore = store
	// Wire the service's store + create rate limiter now that config is decoded.
	// The evaluator was already injected via SetHostEvaluator before Init.
	p.service.store = store
	p.service.limiter = newCreateRateLimiter(p.cfg.CreateRateLimit, createRateWindow, time.Now)

	// Wire the audit server: the log store shares the domain pool, and the
	// membership lookup is the same store the resolver/service authorize against
	// (field injection, fail-closed if unwired). The scrollback cap bounds a
	// history page (D-07).
	p.auditSrv.store = NewChannelAuditStore(store.Pool())
	p.auditSrv.memberLookup = store // *channelStore satisfies channelMembershipAuthLookup
	p.auditSrv.scrollbackCap = p.cfg.ScrollbackCap

	// The service's QueryChannelHistory delegates to the audit server's
	// membership-gated read (HistoryForMember), reusing the SINGLE history fence
	// (01-06) rather than a second unfenced path (01-05b HIGH-4).
	p.service.history = p.auditSrv

	// Set the game id for JetStream dot-style emit subjects. Substrate uses
	// "main" as the default game_id when unset (mirrors core-scenes main.go);
	// ServiceConfig carries no game_id field yet — documented expedient until
	// multi-tenant deployment is real.
	p.service.gameID = "main"

	// Build the live-emit emitter now that the sink + gameID are set.
	// SetEventSink runs before Init in the SDK lifecycle, so eventSink is
	// already populated. Guard against a nil sink (test harnesses that call
	// Init directly bypass the full SDK lifecycle).
	if p.service.eventSink != nil {
		p.service.emitter = newChannelEventEmitter(p.service.eventSink, p.service.gameID)
	} else {
		slog.WarnContext(ctx, "core-channels: event sink nil at Init; emitter left unset")
	}

	// Seed the default channel set (incl. Public) idempotently. Safe to re-run
	// on every Init — ON CONFLICT DO NOTHING on lower(name) (D-01, T-01-13).
	if err := store.SeedDefaultChannels(ctx, defaultChannels); err != nil {
		return oops.Code("CHANNEL_INIT_FAILED").Wrap(err)
	}

	// Start the background retention prune sweep (D-07) in a goroutine tied to an
	// independently cancellable context so it survives the request-scoped Init
	// context. The goroutine is daemon-lifetime — it terminates on plugin
	// shutdown (store pool close / SIGTERM); process exit is the signal. Mirrors
	// core-scenes' publishScheduler start.
	pruneCtx, pruneCancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel intentionally not called; goroutine is daemon-lifetime, process exit is the signal
	_ = pruneCancel
	pruner := &channelPruner{
		store:         store,
		gameID:        p.service.gameID,
		defaultWindow: p.cfg.RetentionWindow,
		interval:      p.cfg.PruneInterval,
		now:           time.Now,
	}
	go pruner.Run(pruneCtx)

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
		auditSrv: &ChannelAuditServer{},
	}
	pluginsdk.ServeWithServices(
		&pluginsdk.ServeConfig{Handler: plugin},
		plugin,
	)
}
