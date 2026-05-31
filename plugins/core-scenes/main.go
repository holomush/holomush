// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package main implements the core-scenes binary plugin: the host-loaded
// process that owns the scene domain (membership, lifecycle, ops events,
// resolver, ABAC attribute resolution). See plugin.yaml for the manifest
// declaring its services, policies, and command commands.
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/samber/oops"
	"google.golang.org/grpc"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// scenePlugin is the binary plugin entry struct. It implements:
//   - pluginsdk.Handler          (HandleEvent, HandleCommand)
//   - pluginsdk.ServiceProvider  (RegisterServices, Init)
//   - pluginsdk.AttributeResolverProvider (RegisterAttributeResolver)
//
// The service and resolver fields are pre-allocated in main() so the gRPC
// server registration in RegisterServices/RegisterAttributeResolver (which
// runs before Init) has valid receivers. Init wires the store into both
// after NewSceneStore returns.
type scenePlugin struct {
	store         *SceneStore
	service       *SceneServiceImpl
	resolver      *SceneResolver
	auditSrv      *SceneAuditServer
	focusClient   pluginsdk.FocusClient
	evaluator     pluginsdk.HostEvaluator
	emitRegistry  *pluginsdk.EmitRegistry
	schedInterval time.Duration // decoded from manifest scheduler_interval
}

// sceneConfig is the mapstructure target for the plugin_config block declared
// in plugin.yaml. Keys match the manifest config: section; mapstructure tags
// map snake_case config keys to typed Go fields.
type sceneConfig struct {
	VoteWindow        time.Duration `mapstructure:"vote_window"`
	CoolOffWindow     time.Duration `mapstructure:"cooloff_window"`
	SchedulerInterval time.Duration `mapstructure:"scheduler_interval"`
}

// applyConfig decodes the host-delivered plugin_config into service.cfg and
// schedInterval. Called from Init after the connection string check so both
// production and tests drive config from the manifest. Errors are wrapped with
// SCENE_INIT_FAILED so the host surfaces a clear reason for plugin load failure.
func (p *scenePlugin) applyConfig(config *pluginv1.ServiceConfig) error {
	decoded, err := pluginsdk.DecodeConfig[sceneConfig](config)
	if err != nil {
		return oops.Code("SCENE_INIT_FAILED").Wrap(err)
	}
	// Plugin-owned semantic validation: the host validates the generic duration
	// type but not its meaning. A non-positive scheduler_interval would panic
	// time.NewTicker at scheduler start, so reject it fail-loud at Init.
	if decoded.SchedulerInterval <= 0 {
		return oops.Code("SCENE_INIT_FAILED").
			With("scheduler_interval", decoded.SchedulerInterval.String()).
			Errorf("scheduler_interval must be positive")
	}
	p.service.cfg = SceneServiceConfig{
		DefaultVoteWindow:    decoded.VoteWindow,
		DefaultCoolOffWindow: decoded.CoolOffWindow,
	}
	p.schedInterval = decoded.SchedulerInterval
	return nil
}

// HandleEvent is a no-op for Phase 1. The scene plugin does not subscribe
// to event streams until Phase 4 (event streams + pose order).
func (p *scenePlugin) HandleEvent(_ context.Context, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return nil, nil
}

// HandleCommand routes scene commands to the appropriate subcommand handler.
// "scene" dispatches to the per-character subcommand router; "scenes" dispatches
// to the public open-scene board browser. The dispatcher lives in commands.go to
// keep main.go focused on plugin lifecycle.
func (p *scenePlugin) HandleCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	switch req.Command {
	case "scene":
		return p.dispatchCommand(ctx, req)
	case "scenes":
		return p.handleScenesBoard(ctx, req)
	default:
		return pluginsdk.Errorf("core-scenes does not handle command %q", req.Command), nil
	}
}

// RegisterServices registers the SceneServiceServer on the go-plugin gRPC
// transport so the host can proxy scene RPCs to this plugin. Also
// registers the PluginAuditService that serves the plugin-owned audit
// subject prefix (events.*.scene.>) per the manifest's audit block.
func (p *scenePlugin) RegisterServices(registrar grpc.ServiceRegistrar) {
	scenev1.RegisterSceneServiceServer(registrar, p.service)
	pluginv1.RegisterPluginAuditServiceServer(registrar, p.auditSrv)
}

// RegisterAttributeResolver registers the SceneResolver on the go-plugin
// gRPC transport so the host's ABAC engine can resolve scene attributes
// during policy evaluation.
func (p *scenePlugin) RegisterAttributeResolver(registrar grpc.ServiceRegistrar) {
	pluginv1.RegisterAttributeResolverServiceServer(registrar, p.resolver)
}

// SetFocusClient is called by the SDK adapter during Init when the plugin
// declares FocusClientAware. The client is used by command handlers to
// drive session focus state via PluginHostService.{JoinFocus,LeaveFocus,
// PresentFocus}.
func (p *scenePlugin) SetFocusClient(client pluginsdk.FocusClient) {
	p.focusClient = client
}

// SetHostEvaluator is called by the SDK adapter during Init when the plugin
// declares HostEvaluatorAware. The evaluator is used by admin-gated command
// handlers (e.g., handleVoteExtend) to perform host ABAC checks.
func (p *scenePlugin) SetHostEvaluator(ev pluginsdk.HostEvaluator) {
	p.evaluator = ev
}

// SetEventSink forwards the SDK-injected event sink to the scene service so
// service-owned RPC handlers can emit host-owned core events.
func (p *scenePlugin) SetEventSink(sink pluginsdk.EventSink) {
	if p.service != nil {
		p.service.SetEventSink(sink)
	}
}

// SetSnapshotDecryptor forwards the SDK-injected host-mediated read-back
// decryptor to the scene service, where the COOLOFF→PUBLISHED snapshot pipeline
// (C7) uses it to decrypt its own IC content (the plugin holds no DEK —
// INV-RB-1). Declares scenePlugin as pluginsdk.SnapshotDecryptorAware so the SDK
// adapter wires it before Init.
func (p *scenePlugin) SetSnapshotDecryptor(d pluginsdk.SnapshotDecryptor) {
	if p.service != nil {
		p.service.SetSnapshotDecryptor(d)
	}
}

// SetSettingsClient forwards the SDK-injected host settings client to the scene
// service so service-owned RPC handlers can read game-scope settings (e.g. the
// content-warning taxonomy override). Declares scenePlugin as
// pluginsdk.SettingsClientAware so the SDK adapter wires it before Init.
func (p *scenePlugin) SetSettingsClient(c pluginsdk.SettingsClient) {
	if p.service != nil {
		p.service.SetSettingsClient(c)
	}
}

// EmitRegistry implements pluginsdk.EmitTypeRegistrar. The substrate
// INV-S5 validator reads this set via the binary-plugin Init RPC and
// validates set-equality against manifest crypto.emits.
func (p *scenePlugin) EmitRegistry() *pluginsdk.EmitRegistry {
	return p.emitRegistry
}

// phase4EmitTypes returns the 8 plugin-owned scene event types declared
// in crypto.emits. Exposed at package level so the manifest-vs-registry
// test in main_test.go can build the same set without duplicating the
// list.
func phase4EmitTypes() []string {
	return []string{
		"scene_pose",
		"scene_say",
		"scene_emit",
		"scene_ooc",
		"scene_join_ic",
		"scene_leave_ic",
		"scene_pose_order_changed_ic",
		"scene_idle_nudge",
	}
}

// phase6EmitTypes returns the 6 Phase 6 publication notice event types
// declared in crypto.emits (all sensitivity:never). These MUST be
// registered alongside phase4EmitTypes so the EmitTypeRegistrar set equals
// the manifest crypto.emits set (INV-S5 / INV-P4-2); the host fails plugin
// load otherwise. The matching emitter wiring lands in Phase D2.
func phase6EmitTypes() []string {
	return []string{
		"scene_publish_started",
		"scene_publish_vote_cast",
		"scene_publish_cooloff_started",
		"scene_publish_resolved",
		"scene_publish_withdrawn",
		"scene_publish_vote_attempts_extended",
	}
}

// Init is called by the host after the gRPC connection is established and
// the Postgres schema/role have been provisioned. It opens the connection
// pool, runs the embedded migrations, and wires the resulting store into
// both the service and the resolver.
//
// The connection string from ServiceConfig has search_path=plugin_core_scenes
// pre-set, so all queries automatically target the plugin's schema.
func (p *scenePlugin) Init(ctx context.Context, config *pluginv1.ServiceConfig) error {
	connStr := config.GetConnectionString()
	if connStr == "" {
		return oops.Code("SCENE_INIT_FAILED").Errorf("connection_string is required")
	}

	if err := p.applyConfig(config); err != nil {
		return err
	}

	store, err := NewSceneStore(ctx, connStr)
	if err != nil {
		return oops.Code("SCENE_INIT_FAILED").Wrap(err)
	}

	p.store = store
	p.service.store = store
	p.resolver.store = store
	p.auditSrv.store = NewSceneAuditStore(store.Pool())
	p.auditSrv.memberLookup = store // *SceneStore satisfies sceneMembershipLookup

	// Set the game ID for NATS dot-style emit subjects. Substrate uses
	// "main" as the default game_id when unset (see internal/grpc/server.go:181).
	// ServiceConfig does not currently carry a game_id field; this hardcode
	// is the documented expedient until multi-tenant deployment is real
	// (tracked as a post-Phase-4 follow-up).
	p.service.gameID = "main"

	// Wire the real publish eventer now that sink, store, and gameID are all
	// set. SetEventSink runs before Init in the SDK lifecycle, so
	// p.service.eventSink is already populated by the time we reach here.
	// Guard against a nil sink (e.g. in test harnesses that call Init directly
	// without going through the full SDK lifecycle).
	if p.service.eventSink != nil {
		p.service.SetPublishEventer(newPublishEventEmitter(p.service.eventSink, p.service.store, p.service.gameID))
	} else {
		slog.WarnContext(ctx, "core-scenes: event sink nil at Init; publish eventer left as noop")
	}

	// Start the publish scheduler in a goroutine tied to an independently
	// cancellable context so it survives the Init RPC context (which is
	// request-scoped and will cancel when the gRPC call returns). The
	// goroutine terminates on plugin shutdown via the store pool's close
	// propagation or SIGTERM — the process exits cleanly regardless.
	schedCtx, schedCancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel intentionally not called; goroutine is daemon-lifetime, process exit is the signal
	_ = schedCancel
	sched := &publishScheduler{
		svc:      p.service,
		store:    store,
		interval: p.schedInterval, // decoded from manifest scheduler_interval
		now:      time.Now,
	}
	go sched.Run(schedCtx)

	slog.InfoContext(
		ctx, "core-scenes plugin initialised",
		"storage", "postgres",
	)
	return nil
}

func main() {
	reg := pluginsdk.NewEmitRegistry()
	reg.RegisterEmitTypes(phase4EmitTypes())
	reg.RegisterEmitTypes(phase6EmitTypes())

	plugin := &scenePlugin{
		service:      &SceneServiceImpl{},
		resolver:     &SceneResolver{},
		auditSrv:     &SceneAuditServer{},
		emitRegistry: reg,
	}

	pluginsdk.ServeWithServices(
		&pluginsdk.ServeConfig{Handler: plugin},
		plugin,
	)
}
