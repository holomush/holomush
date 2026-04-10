// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"

	"github.com/samber/oops"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/core"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

type pluginHostServiceServer struct {
	pluginv1.UnimplementedPluginHostServiceServer
	host       *Host
	pluginName string
}

func newPluginHostServiceServer(host *Host, pluginName string) func([]grpc.ServerOption) *grpc.Server {
	return func(opts []grpc.ServerOption) *grpc.Server {
		server := grpc.NewServer(opts...)
		pluginv1.RegisterPluginHostServiceServer(server, &pluginHostServiceServer{
			host:       host,
			pluginName: pluginName,
		})
		return server
	}
}

func (s *pluginHostServiceServer) EmitEvent(ctx context.Context, req *pluginv1.PluginHostServiceEmitEventRequest) (*pluginv1.PluginHostServiceEmitEventResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}

	s.host.mu.RLock()
	emitter := s.host.eventEmitter
	s.host.mu.RUnlock()
	if emitter == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin event emitter is not configured")
	}

	emitCtx := ctx
	if kind, id, ok := pluginsdk.ActorMetadataFromIncomingContext(ctx); ok {
		emitCtx = core.WithActor(ctx, core.Actor{
			Kind: sdkActorKindToCore(kind),
			ID:   id,
		})
	} else {
		emitCtx = core.WithActor(emitCtx, core.Actor{
			Kind: core.ActorPlugin,
			ID:   s.pluginName,
		})
	}
	if err := emitter.Emit(emitCtx, s.pluginName, pluginsdk.EmitIntent{
		Stream:  req.GetStream(),
		Type:    pluginsdk.EventType(req.GetEventType()),
		Payload: string(req.GetPayload()),
	}); err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	return &pluginv1.PluginHostServiceEmitEventResponse{}, nil
}

func sdkActorKindToCore(kind pluginsdk.ActorKind) core.ActorKind {
	switch kind {
	case pluginsdk.ActorCharacter:
		return core.ActorCharacter
	case pluginsdk.ActorSystem:
		return core.ActorSystem
	default:
		return core.ActorPlugin
	}
}
