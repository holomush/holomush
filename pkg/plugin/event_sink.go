// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// emitTokenHeader is the gRPC metadata header that carries the
// host-issued per-dispatch emit token. The host attaches it to outgoing
// HandleEvent / HandleCommand metadata; the SDK ferries it back to
// EmitEvent so the host can authenticate the actor input. Plugin
// authors do not interact with this header directly — auto-ferry is the
// transparent path. (Spec §3.3.5 / §5.4.)
//
//nolint:gosec // G101 false positive: this is a metadata header NAME, not credential material.
const emitTokenHeader = "x-holomush-emit-token"

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
	client hostv1.EmitServiceClient
}

func (s *pluginHostEventSink) Emit(ctx context.Context, intent EmitIntent) error {
	if s.client == nil {
		return oops.New("plugin host event sink client is not configured")
	}
	callCtx := ctx
	if kind, id, ok := actorMetadataFromContext(ctx); ok {
		callCtx = WithOutgoingActorMetadata(ctx, kind, id)
	}
	// Auto-ferry the host-issued emit token from incoming HandleEvent /
	// HandleCommand metadata onto the outgoing EmitEvent call. The host
	// authenticates the actor input via this token (spec §3.3.5 / §5.4 /
	// G1); plugin authors don't touch the header directly. If the caller
	// has already set the token on the outgoing metadata (test plugins
	// exercising fabrication / forgery paths), we leave it untouched.
	//
	// Two-token pattern (spec §3.3.5):
	//   1. Dispatch token — present on the incoming ctx during a
	//      DeliverEvent / DeliverCommand call. Ferry it through.
	//   2. Self token — for plugin-served gRPC handlers (e.g.,
	//      SceneService.CreateScene) that have NO dispatch token because
	//      the call did not originate at DeliverEvent / DeliverCommand.
	//      Request one from the host. The host hardcodes the actor to
	//      {ActorPlugin, pluginName} so this path can never escalate.
	hasOutgoingToken := false
	if existing, ok := metadata.FromOutgoingContext(callCtx); ok && len(existing.Get(emitTokenHeader)) > 0 {
		hasOutgoingToken = true
	}
	if !hasOutgoingToken {
		if incoming, ok := metadata.FromIncomingContext(ctx); ok {
			if tokens := incoming.Get(emitTokenHeader); len(tokens) > 0 && tokens[0] != "" {
				callCtx = metadata.AppendToOutgoingContext(callCtx, emitTokenHeader, tokens[0])
				hasOutgoingToken = true
			}
		}
	}
	if !hasOutgoingToken {
		// Self-token fallback. No dispatch token in the incoming ctx
		// means we're on a plugin-served RPC path; ask the host for a
		// self-token (bound to ActorPlugin + the plugin's mTLS-bound
		// name) so the EmitEvent token check passes. The host's
		// hardcoded actor binding means we cannot escalate to character
		// or system actors via this path.
		resp, tokErr := s.client.RequestEmitToken(callCtx, &hostv1.RequestEmitTokenRequest{})
		if tokErr != nil {
			return oops.Code("EMIT_TOKEN_REQUEST_FAILED").
				With("subject", intent.Subject).
				Wrap(tokErr)
		}
		callCtx = metadata.AppendToOutgoingContext(callCtx, emitTokenHeader, resp.GetToken())
	}
	// Single EmitIntent->request mapping site (holomush-av954), guarded by
	// TestEmitIntentEmitRequestRoundTripCarriesEveryField so a field added to
	// EmitIntent (notably Sensitive) cannot be silently dropped on the binary
	// active-emit send path.
	_, err := s.client.EmitEvent(callCtx, EmitIntentToEmitRequest(intent))
	if err != nil {
		return oops.With("subject", intent.Subject).Wrap(err)
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
	return newPluginHostEventSink(conn), nil
}

// newPluginHostEventSink constructs an EventSink from a broker gRPC connection.
// Exposed for wiring in sdk.go; test code may construct a pluginHostEventSink directly.
func newPluginHostEventSink(conn grpc.ClientConnInterface) EventSink {
	return &pluginHostEventSink{
		client: hostv1.NewEmitServiceClient(conn),
	}
}
