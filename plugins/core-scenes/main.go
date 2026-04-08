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
	store    *SceneStore
	service  *SceneServiceImpl
	resolver *SceneResolver
}

// HandleEvent is a no-op for Phase 1. The scene plugin does not subscribe
// to event streams until Phase 4 (event streams + pose order).
func (p *scenePlugin) HandleEvent(_ context.Context, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return nil, nil
}

// HandleCommand routes scene commands to the appropriate subcommand handler.
// The dispatcher lives in commands.go to keep main.go focused on plugin
// lifecycle.
func (p *scenePlugin) HandleCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	if req.Command != "scene" {
		return pluginsdk.Errorf("core-scenes does not handle command %q", req.Command), nil
	}
	return p.dispatchCommand(ctx, req)
}

// RegisterServices registers the SceneServiceServer on the go-plugin gRPC
// transport so the host can proxy scene RPCs to this plugin.
func (p *scenePlugin) RegisterServices(registrar grpc.ServiceRegistrar) {
	scenev1.RegisterSceneServiceServer(registrar, p.service)
}

// RegisterAttributeResolver registers the SceneResolver on the go-plugin
// gRPC transport so the host's ABAC engine can resolve scene attributes
// during policy evaluation.
func (p *scenePlugin) RegisterAttributeResolver(registrar grpc.ServiceRegistrar) {
	pluginv1.RegisterAttributeResolverServiceServer(registrar, p.resolver)
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

	store, err := NewSceneStore(ctx, connStr)
	if err != nil {
		return oops.Code("SCENE_INIT_FAILED").Wrap(err)
	}

	p.store = store
	p.service.store = store
	p.resolver.store = store

	slog.InfoContext(ctx, "core-scenes plugin initialised",
		"storage", "postgres",
	)
	return nil
}

func main() {
	plugin := &scenePlugin{
		service:  &SceneServiceImpl{},
		resolver: &SceneResolver{},
	}

	pluginsdk.ServeWithServices(
		&pluginsdk.ServeConfig{Handler: plugin},
		plugin,
	)
}
