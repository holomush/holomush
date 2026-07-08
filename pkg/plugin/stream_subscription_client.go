// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	"github.com/samber/oops"
	"google.golang.org/grpc"

	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// ReplayMode controls how a mid-session stream subscription initially replays.
// It is the SDK mirror of the host StreamReplayMode enum: FROM_CURSOR replays
// from the session's saved cursor (scene catch-up); LIVE_ONLY skips history and
// delivers only newly-arriving events (channels' mid-session join).
type ReplayMode int

const (
	// ReplayModeFromCursor replays from the session's saved cursor for the stream.
	// The zero value, matching the proto's UNSPECIFIED→FROM_CURSOR default.
	ReplayModeFromCursor ReplayMode = iota
	// ReplayModeLiveOnly skips historical replay and delivers only newly-arriving
	// events.
	ReplayModeLiveOnly
)

// StreamSubscription is the interface exposed to binary plugins for mutating an
// active session's stream subscriptions mid-session (the served host
// stream.subscription capability). The host derives the calling plugin subject
// server-side and fences the request to the plugin's owned emit namespaces;
// stream MUST be a domain-RELATIVE reference in a namespace the plugin owns
// (e.g. "channel.<id>"), never a pre-qualified "events." subject — the host
// rejects a pre-qualified subject.
type StreamSubscription interface {
	// AddStream subscribes sessionID to stream with the given replay mode.
	AddStream(ctx context.Context, sessionID, stream string, mode ReplayMode) error
	// RemoveStream unsubscribes sessionID from stream. Idempotent.
	RemoveStream(ctx context.Context, sessionID, stream string) error
}

// StreamSubscriptionAware is the optional interface service providers implement
// to receive a StreamSubscription client during Init, parallel to
// FocusClientAware / HostEvaluatorAware. A provider implementing it MUST declare
// the "stream.subscription" capability in its manifest requires: — otherwise it
// fails closed at load (validateDeclaredCapabilities, INV-PLUGIN-54).
type StreamSubscriptionAware interface {
	SetStreamSubscription(StreamSubscription)
}

// streamSubscriptionClient is the concrete StreamSubscription used by binary
// plugins. It wraps the generated StreamSubscriptionServiceClient and forwards
// calls, mapping the SDK ReplayMode to the proto enum.
type streamSubscriptionClient struct {
	client hostv1.StreamSubscriptionServiceClient
}

// newPluginHostStreamSubscriptionClient constructs a StreamSubscription from a
// broker gRPC connection. Exposed to the adapter for wiring.
func newPluginHostStreamSubscriptionClient(conn grpc.ClientConnInterface) StreamSubscription {
	return &streamSubscriptionClient{client: hostv1.NewStreamSubscriptionServiceClient(conn)}
}

// AddStream implements StreamSubscription. A nil client fails closed.
func (c *streamSubscriptionClient) AddStream(ctx context.Context, sessionID, stream string, mode ReplayMode) error {
	if c.client == nil {
		return oops.New("host stream subscription client is not configured")
	}
	// Ferry the host-issued per-dispatch token from the incoming command context
	// so the host recovers the acting plugin subject server-side (the same
	// mechanism EmitEvent / Evaluate use; join/leave is command-gated).
	_, err := c.client.AddSessionStream(withDispatchToken(ctx), &hostv1.AddSessionStreamRequest{
		SessionId:  sessionID,
		Stream:     stream,
		ReplayMode: sdkReplayModeToProto(mode),
	})
	if err != nil {
		return oops.With("session_id", sessionID, "stream", stream).Wrap(err)
	}
	return nil
}

// RemoveStream implements StreamSubscription. A nil client fails closed.
func (c *streamSubscriptionClient) RemoveStream(ctx context.Context, sessionID, stream string) error {
	if c.client == nil {
		return oops.New("host stream subscription client is not configured")
	}
	_, err := c.client.RemoveSessionStream(withDispatchToken(ctx), &hostv1.RemoveSessionStreamRequest{
		SessionId: sessionID,
		Stream:    stream,
	})
	if err != nil {
		return oops.With("session_id", sessionID, "stream", stream).Wrap(err)
	}
	return nil
}

// sdkReplayModeToProto maps the SDK ReplayMode to the proto StreamReplayMode.
func sdkReplayModeToProto(m ReplayMode) hostv1.StreamReplayMode {
	switch m {
	case ReplayModeLiveOnly:
		return hostv1.StreamReplayMode_STREAM_REPLAY_MODE_LIVE_ONLY
	default:
		return hostv1.StreamReplayMode_STREAM_REPLAY_MODE_FROM_CURSOR
	}
}
