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
// core-scenes binary plugin. The service field is pre-allocated in main() so
// that RegisterServices (called during gRPC server setup, before Init) has a
// valid receiver. Init wires the store into both scenePlugin and the service.
type scenePlugin struct {
	store   *SceneStore
	service *SceneServiceImpl
}

// HandleEvent is a no-op for Phase 3 — the scene plugin does not subscribe to
// any event streams yet.
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
	p.service.store = store

	slog.Info("core-scenes plugin initialised",
		"storage", "postgres",
	)
	return nil
}

func main() {
	plugin := &scenePlugin{
		service: &SceneServiceImpl{},
	}

	pluginsdk.ServeWithServices(
		&pluginsdk.ServeConfig{Handler: plugin},
		plugin,
	)
}
