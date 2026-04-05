// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

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

// scenePlugin implements Handler, CommandHandler, and ServiceProvider for the
// core-scenes binary plugin. It is created uninitialised in main(); the host
// calls Init via gRPC to supply the database connection string before any
// game traffic arrives.
type scenePlugin struct {
	store   *SceneStore
	service *SceneServiceImpl
}

// HandleEvent is a no-op for Phase 3 — the scene plugin does not subscribe to
// any event streams yet.
func (p *scenePlugin) HandleEvent(_ context.Context, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return nil, nil
}

// HandleCommand is a placeholder — scene commands will be routed through the
// gRPC SceneService rather than the command handler path. Returns OK with a
// help message for now.
func (p *scenePlugin) HandleCommand(_ context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: "Scene commands are not yet implemented. Use the scene service RPCs.",
	}, nil
}

// RegisterServices registers the SceneServiceServer on the go-plugin gRPC
// transport so the host can proxy scene RPCs to this plugin.
func (p *scenePlugin) RegisterServices(registrar grpc.ServiceRegistrar) {
	scenev1.RegisterSceneServiceServer(registrar, p.service)
}

// Init is called by the host after the go-plugin connection is established.
// It extracts the connection string from ServiceConfig, creates the SceneStore
// (which runs migrations), and wires up the SceneServiceImpl.
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
	p.service = NewSceneServiceImpl(store)

	slog.Info("core-scenes plugin initialised",
		"storage", "postgres",
	)
	return nil
}

func main() {
	plugin := &scenePlugin{}

	pluginsdk.ServeWithServices(
		&pluginsdk.ServeConfig{Handler: plugin},
		plugin,
	)
}
