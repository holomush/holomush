// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"github.com/samber/oops"
	"google.golang.org/grpc"
)

// EventSink is the SDK-facing facade binary plugin service code uses to ask
// the host to emit an event on the plugin's behalf.
type EventSink interface {
	Emit(ctx context.Context, intent EmitIntent) error
}

// EventSinkAware is an optional interface for service providers that want the
// SDK to inject an EventSink during Init.
type EventSinkAware interface {
	SetEventSink(EventSink)
}

type brokerDialer interface {
	DialWithOptions(id uint32, opts ...grpc.DialOption) (*grpc.ClientConn, error)
}

type pluginHostEventSink struct {
	client pluginv1.PluginHostServiceClient
}

func (s *pluginHostEventSink) Emit(ctx context.Context, intent EmitIntent) error {
	if s.client == nil {
		return oops.New("plugin host event sink client is not configured")
	}
	callCtx := ctx
	if kind, id, ok := actorMetadataFromContext(ctx); ok {
		callCtx = WithOutgoingActorMetadata(ctx, kind, id)
	}
	_, err := s.client.EmitEvent(callCtx, &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    intent.Stream,
		EventType: string(intent.Type),
		Payload:   []byte(intent.Payload),
	})
	if err != nil {
		return oops.With("stream", intent.Stream).Wrap(err)
	}
	return nil
}

func newEventSinkFromBroker(broker brokerDialer, services map[string]string) (EventSink, error) {
	if broker == nil {
		return nil, oops.New("plugin host broker is not configured")
	}
	conn, err := dialPluginHost(broker, services)
	if err != nil {
		return nil, err
	}
	return &pluginHostEventSink{
		client: pluginv1.NewPluginHostServiceClient(conn),
	}, nil
}
