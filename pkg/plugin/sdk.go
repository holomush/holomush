// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"errors"

	hashiplug "github.com/hashicorp/go-plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"github.com/samber/oops"
	"google.golang.org/grpc"
)

// Handler is the interface that binary plugins must implement.
type Handler interface {
	// HandleEvent processes an incoming event and returns any events to emit.
	HandleEvent(ctx context.Context, event Event) ([]EmitEvent, error)
}

// CommandHandler is implemented by binary plugins that handle commands.
// Plugins that only handle events need not implement this interface.
type CommandHandler interface {
	// HandleCommand processes a command and returns the result.
	HandleCommand(ctx context.Context, req CommandRequest) (*CommandResponse, error)
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
//
// Example usage:
//
//	package main
//
//	import (
//		"context"
//		pluginsdk "github.com/holomush/holomush/pkg/plugin"
//	)
//
//	type EchoPlugin struct{}
//
//	func (p *EchoPlugin) HandleEvent(ctx context.Context, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
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
func Serve(config *ServeConfig) {
	if config == nil {
		panic("plugin: config cannot be nil")
	}
	if config.Handler == nil {
		panic("plugin: config.Handler cannot be nil")
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
		return errors.New("plugin: handler is nil")
	}
	adapter := &pluginServerAdapter{handler: p.handler}
	if ch, ok := p.handler.(CommandHandler); ok {
		adapter.cmdHandler = ch
	}
	pluginv1.RegisterPluginServiceServer(s, adapter)
	return nil
}

// GRPCClient is required by go-plugin's GRPCPlugin interface but is never
// called on the plugin side. The host has its own GRPCClient implementation.
func (p *grpcPlugin) GRPCClient(_ context.Context, _ *hashiplug.GRPCBroker, _ *grpc.ClientConn) (interface{}, error) {
	return nil, errors.New("plugin: GRPCClient not implemented on plugin side")
}

// pluginServerAdapter adapts Handler (and optionally CommandHandler) to pluginv1.PluginServiceServer.
type pluginServerAdapter struct {
	pluginv1.UnimplementedPluginServiceServer
	handler         Handler
	cmdHandler      CommandHandler  // nil if handler does not implement CommandHandler
	serviceProvider ServiceProvider // nil if plugin does not provide services
}

// Init implements pluginv1.PluginServiceServer. When a ServiceProvider is set,
// it delegates to the provider's Init; otherwise it returns an empty response.
func (a *pluginServerAdapter) Init(ctx context.Context, req *pluginv1.InitRequest) (*pluginv1.InitResponse, error) {
	if a.serviceProvider == nil {
		return &pluginv1.InitResponse{}, nil
	}
	if err := a.serviceProvider.Init(ctx, req.GetConfig()); err != nil {
		return nil, oops.With("phase", "init").Wrap(err)
	}
	return &pluginv1.InitResponse{}, nil
}

// HandleEvent implements pluginv1.PluginServiceServer.
func (a *pluginServerAdapter) HandleEvent(ctx context.Context, req *pluginv1.HandleEventRequest) (*pluginv1.HandleEventResponse, error) {
	// protoEvent may be nil; proto getters return zero values for nil receivers,
	// making this safe without explicit nil checks.
	protoEvent := req.GetEvent()

	// Convert proto Event to SDK Event
	event := Event{
		ID:        protoEvent.GetId(),
		Stream:    protoEvent.GetStream(),
		Type:      EventType(protoEvent.GetType()),
		Timestamp: protoEvent.GetTimestamp(),
		ActorKind: protoActorKindToActorKind(protoEvent.GetActorKind()),
		ActorID:   protoEvent.GetActorId(),
		Payload:   protoEvent.GetPayload(),
	}

	// Call the user's handler
	emits, err := a.handler.HandleEvent(ctx, event)
	if err != nil {
		return nil, oops.With("event_id", event.ID).Wrap(err)
	}

	// Convert SDK EmitEvent to proto EmitEvent
	protoEmits := make([]*pluginv1.EmitEvent, len(emits))
	for i, e := range emits {
		protoEmits[i] = &pluginv1.EmitEvent{
			Stream:  e.Stream,
			Type:    string(e.Type),
			Payload: e.Payload,
		}
	}

	return &pluginv1.HandleEventResponse{EmitEvents: protoEmits}, nil
}

// HandleCommand implements pluginv1.PluginServiceServer.
func (a *pluginServerAdapter) HandleCommand(ctx context.Context, req *pluginv1.HandleCommandRequest) (*pluginv1.HandleCommandResponse, error) {
	if a.cmdHandler == nil {
		return &pluginv1.HandleCommandResponse{Response: &pluginv1.CommandResponse{}}, nil
	}

	protoCmd := req.GetCommand()
	cmd := CommandRequest{
		Command:       protoCmd.GetCommand(),
		Args:          protoCmd.GetArgs(),
		CharacterID:   protoCmd.GetCharacterId(),
		CharacterName: protoCmd.GetCharacterName(),
		LocationID:    protoCmd.GetLocationId(),
		SessionID:     protoCmd.GetSessionId(),
		PlayerID:      protoCmd.GetPlayerId(),
		InvokedAs:     protoCmd.GetRawInput(),
	}

	resp, err := a.cmdHandler.HandleCommand(ctx, cmd)
	if err != nil {
		return nil, oops.With("command", cmd.Command).Wrap(err)
	}

	if resp == nil {
		return &pluginv1.HandleCommandResponse{Response: &pluginv1.CommandResponse{}}, nil
	}

	protoEvents := make([]*pluginv1.EmitEvent, len(resp.Events))
	for i, e := range resp.Events {
		protoEvents[i] = &pluginv1.EmitEvent{
			Stream:  e.Stream,
			Type:    string(e.Type),
			Payload: e.Payload,
		}
	}

	return &pluginv1.HandleCommandResponse{
		Response: &pluginv1.CommandResponse{
			Status: sdkCommandStatusToProto(resp.Status),
			Output: resp.Output,
			Events: protoEvents,
		},
	}, nil
}

// sdkCommandStatusToProto converts an SDK CommandStatus to a proto CommandStatus.
func sdkCommandStatusToProto(s CommandStatus) pluginv1.CommandStatus {
	switch s {
	case CommandOK:
		return pluginv1.CommandStatus_COMMAND_STATUS_OK
	case CommandError:
		return pluginv1.CommandStatus_COMMAND_STATUS_ERROR
	case CommandFailure:
		return pluginv1.CommandStatus_COMMAND_STATUS_FAILURE
	case CommandFatal:
		return pluginv1.CommandStatus_COMMAND_STATUS_FATAL
	default:
		return pluginv1.CommandStatus_COMMAND_STATUS_OK
	}
}

// protoActorKindToActorKind converts proto ActorKind to pkg/plugin ActorKind.
func protoActorKindToActorKind(kind string) ActorKind {
	switch kind {
	case "character":
		return ActorCharacter
	case "system":
		return ActorSystem
	case "plugin":
		return ActorPlugin
	default:
		return ActorCharacter
	}
}
