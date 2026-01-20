// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package pluginsdk provides the SDK for building HoloMUSH binary plugins.
//
// Binary plugins communicate with the HoloMUSH host via gRPC using the
// HashiCorp go-plugin framework. This package provides helpers to simplify
// plugin development.
//
// Example usage:
//
//	package main
//
//	import (
//		"context"
//		"github.com/holomush/holomush/pkg/pluginsdk"
//	)
//
//	type EchoPlugin struct{}
//
//	func (p *EchoPlugin) HandleEvent(ctx context.Context, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
//		// Echo the event back to the same stream
//		return []pluginsdk.EmitEvent{
//			{
//				Stream:  event.Stream,
//				Type:    event.Type,
//				Payload: event.Payload,
//			},
//		}, nil
//	}
//
//	func main() {
//		pluginsdk.Serve(&pluginsdk.ServeConfig{
//			Handler: &EchoPlugin{},
//		})
//	}
package pluginsdk

import (
	"context"
	"errors"
	"fmt"

	hashiplug "github.com/hashicorp/go-plugin"
	pluginv1 "github.com/holomush/holomush/internal/proto/holomush/plugin/v1"
	"google.golang.org/grpc"
)

// Event represents a game event delivered to plugins.
type Event struct {
	// ID is the unique event identifier (ULID string).
	ID string
	// Stream the event belongs to (e.g., "room:room_abc123").
	Stream string
	// Type is the event type (e.g., "say", "pose", "arrive", "leave", "system").
	Type string
	// Timestamp in Unix milliseconds.
	Timestamp int64
	// ActorKind identifies the actor type ("character", "system", "plugin").
	ActorKind string
	// ActorID is the actor identifier.
	ActorID string
	// Payload is the JSON-encoded event data.
	Payload string
}

// EmitEvent represents an event that a plugin wants to emit.
type EmitEvent struct {
	// Stream is the target stream for the event.
	Stream string
	// Type is the event type.
	Type string
	// Payload is the JSON-encoded event data.
	Payload string
}

// Handler is the interface that binary plugins must implement.
type Handler interface {
	// HandleEvent processes an incoming event and returns any events to emit.
	HandleEvent(ctx context.Context, event Event) ([]EmitEvent, error)
}

// HandshakeConfig is the go-plugin handshake configuration.
// Both host and plugins must use the same values.
var HandshakeConfig = hashiplug.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "HOLOMUSH_PLUGIN",
	MagicCookieValue: "holomush-v1",
}

// ServeConfig configures the plugin server.
type ServeConfig struct {
	// Handler is the event handler implementation.
	// Required; Serve will panic if nil.
	Handler Handler
}

// Serve starts the plugin server. This should be called from main().
// It blocks and never returns under normal operation.
func Serve(config *ServeConfig) {
	if config == nil {
		panic("pluginsdk: config cannot be nil")
	}
	if config.Handler == nil {
		panic("pluginsdk: config.Handler cannot be nil")
	}
	hashiplug.Serve(&hashiplug.ServeConfig{
		HandshakeConfig: HandshakeConfig,
		Plugins: map[string]hashiplug.Plugin{
			"plugin": &grpcPlugin{handler: config.Handler},
		},
		GRPCServer: hashiplug.DefaultGRPCServer,
	})
}

// grpcPlugin implements go-plugin's Plugin interface for gRPC.
type grpcPlugin struct {
	hashiplug.NetRPCUnsupportedPlugin
	handler Handler
}

// GRPCServer registers the plugin server (called by plugin process).
func (p *grpcPlugin) GRPCServer(_ *hashiplug.GRPCBroker, s *grpc.Server) error {
	if p.handler == nil {
		return errors.New("pluginsdk: handler is nil")
	}
	pluginv1.RegisterPluginServer(s, &pluginServerAdapter{handler: p.handler})
	return nil
}

// GRPCClient returns a plugin client (called by host process).
func (p *grpcPlugin) GRPCClient(_ context.Context, _ *hashiplug.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return pluginv1.NewPluginClient(c), nil
}

// pluginServerAdapter adapts Handler to pluginv1.PluginServer.
type pluginServerAdapter struct {
	pluginv1.UnimplementedPluginServer
	handler Handler
}

// HandleEvent implements pluginv1.PluginServer.
func (a *pluginServerAdapter) HandleEvent(ctx context.Context, req *pluginv1.HandleEventRequest) (*pluginv1.HandleEventResponse, error) {
	protoEvent := req.GetEvent()

	// Convert proto Event to SDK Event
	event := Event{
		ID:        protoEvent.GetId(),
		Stream:    protoEvent.GetStream(),
		Type:      protoEvent.GetType(),
		Timestamp: protoEvent.GetTimestamp(),
		ActorKind: protoEvent.GetActorKind(),
		ActorID:   protoEvent.GetActorId(),
		Payload:   protoEvent.GetPayload(),
	}

	// Call the user's handler
	emits, err := a.handler.HandleEvent(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("handler error: %w", err)
	}

	// Convert SDK EmitEvent to proto EmitEvent
	protoEmits := make([]*pluginv1.EmitEvent, len(emits))
	for i, e := range emits {
		protoEmits[i] = &pluginv1.EmitEvent{
			Stream:  e.Stream,
			Type:    e.Type,
			Payload: e.Payload,
		}
	}

	return &pluginv1.HandleEventResponse{EmitEvents: protoEmits}, nil
}
