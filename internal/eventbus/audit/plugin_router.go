// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"io"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// PluginHistoryClientProvider resolves a plugin name to its
// PluginAuditServiceClient. Keeping this as an interface (rather than a
// map accessor) lets the provider refresh the client when a plugin is
// hot-reloaded without the router holding a stale connection.
type PluginHistoryClientProvider interface {
	// PluginAuditClient returns the client for the named plugin. The
	// boolean is false when no client is registered (plugin not loaded
	// or the plugin failed to provide the service); callers MUST surface
	// that as an explicit error rather than an empty result.
	PluginAuditClient(pluginName string) (pluginv1.PluginAuditServiceClient, bool)
}

// NewPluginHistoryRouter returns a history.PluginHistoryRouter backed by
// the given provider. The returned router is safe for concurrent use.
func NewPluginHistoryRouter(provider PluginHistoryClientProvider) *PluginHistoryRouter {
	return &PluginHistoryRouter{provider: provider}
}

// PluginHistoryRouter implements history.PluginHistoryRouter (structural).
// Keeping the concrete type in this package keeps the audit-domain imports
// contained; the history package depends on audit for OwnerMap anyway.
type PluginHistoryRouter struct {
	provider PluginHistoryClientProvider
}

// QueryHistory forwards a history query to the plugin's PluginAuditService
// QueryHistory RPC and adapts the streaming response to the
// eventbus.HistoryStream contract the host Reader expects.
func (r *PluginHistoryRouter) QueryHistory(
	ctx context.Context,
	pluginName string,
	q eventbus.HistoryQuery,
) (eventbus.HistoryStream, error) {
	client, ok := r.provider.PluginAuditClient(pluginName)
	if !ok {
		return nil, oops.Code("AUDIT_PLUGIN_HISTORY_CLIENT_MISSING").
			With("plugin", pluginName).
			Errorf("no PluginAuditService client registered for plugin")
	}

	// Clamp PageSize to the proto cap (200) before downcasting to int32.
	// The caller has already passed through the host ClampPageSize; this
	// extra guard keeps the downcast obviously safe for gosec.
	pageSize := q.PageSize
	if pageSize < 0 {
		pageSize = 0
	}
	if pageSize > 200 {
		pageSize = 200
	}
	req := &pluginv1.QueryHistoryRequest{
		Subject:   string(q.Subject),
		PageSize:  int32(pageSize),
		Direction: directionProto(q.Direction),
		Caller:    eventbus.ActorToProto(q.Caller),
	}
	if !q.AfterID.IsZero() {
		b := q.AfterID.Bytes()
		req.After = b
	}
	if !q.BeforeID.IsZero() {
		b := q.BeforeID.Bytes()
		req.Before = b
	}
	if !q.NotBefore.IsZero() {
		req.NotBefore = timestamppb.New(q.NotBefore)
	}
	if !q.NotAfter.IsZero() {
		req.NotAfter = timestamppb.New(q.NotAfter)
	}

	// Bind the streaming RPC to a child context we control so Close can
	// cancel it even when the outer caller hasn't cancelled the parent.
	// Otherwise abandoning a page early leaves the plugin-side RPC running
	// until the outer query ctx ends.
	rpcCtx, cancel := context.WithCancel(ctx)
	stream, err := client.QueryHistory(rpcCtx, req)
	if err != nil {
		cancel()
		// Preserve gRPC status codes from the plugin verbatim. The host's error-
		// translation chain (mapHistoryError + the gRPC server-streaming layer)
		// uses status.FromError to extract the code; wrapping with oops here
		// would shadow it. Only wrap non-status errors with the diagnostic
		// AUDIT_PLUGIN_HISTORY_RPC_FAILED oops code.
		if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
			return nil, err //nolint:wrapcheck // intentional: preserve plugin's gRPC status code for downstream status.FromError-based translation
		}
		return nil, oops.Code("AUDIT_PLUGIN_HISTORY_RPC_FAILED").
			With("plugin", pluginName).
			With("subject", string(q.Subject)).
			Wrap(err)
	}
	return &pluginHistoryStream{
		rpc:        stream,
		cancel:     cancel,
		pluginName: pluginName,
		subject:    string(q.Subject),
	}, nil
}

// pluginHistoryStream adapts the server-streaming RPC to HistoryStream.Next /
// Close. The RPC closes itself when the server returns EOF; Close cancels the
// underlying context so the plugin-side stream is released immediately on
// early abandonment.
type pluginHistoryStream struct {
	rpc        pluginv1.PluginAuditService_QueryHistoryClient
	cancel     context.CancelFunc
	pluginName string
	subject    string
	closed     bool
}

// Next returns the next event or io.EOF when the RPC exhausts the page.
// Honours the passed ctx by cancelling the RPC when ctx is Done, matching
// the HistoryStream contract.
func (s *pluginHistoryStream) Next(ctx context.Context) (eventbus.Event, error) {
	if s.closed {
		return eventbus.Event{}, io.EOF
	}
	if err := ctx.Err(); err != nil {
		return eventbus.Event{}, oops.Code("AUDIT_PLUGIN_HISTORY_CTX").
			With("plugin", s.pluginName).
			With("subject", s.subject).
			Wrap(err)
	}
	// Cancel the underlying RPC if the caller's ctx ends before Recv returns.
	// Recv blocks on the stream, so we spawn a short-lived watchdog goroutine
	// that cancels the rpcCtx when the caller's ctx ends.
	doneCh := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			s.cancel()
		case <-doneCh:
		}
	}()
	resp, err := s.rpc.Recv()
	close(doneCh)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return eventbus.Event{}, io.EOF
		}
		// Preserve gRPC status codes from the plugin verbatim. The host's
		// error-translation chain (mapHistoryError + the gRPC server-streaming
		// layer) uses status.FromError to extract the code; wrapping with oops
		// here would shadow it. Only wrap non-status errors with the diagnostic
		// AUDIT_PLUGIN_HISTORY_RECV_FAILED oops code.
		if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
			return eventbus.Event{}, err //nolint:wrapcheck // preserve plugin's gRPC status code for downstream status.FromError-based translation
		}
		return eventbus.Event{}, oops.Code("AUDIT_PLUGIN_HISTORY_RECV_FAILED").
			With("plugin", s.pluginName).
			With("subject", s.subject).
			Wrap(err)
	}
	ev := resp.GetRow()
	if ev == nil {
		return eventbus.Event{}, oops.Code("AUDIT_PLUGIN_HISTORY_EMPTY_EVENT").
			With("plugin", s.pluginName).
			With("subject", s.subject).
			Errorf("plugin returned empty event")
	}

	id, err := ulid.Parse(string(ev.GetId()))
	if err != nil {
		// Fall back to interpreting the 16 raw bytes directly. Plugins are
		// expected to return the raw ULID bytes (as the proto comment says);
		// the Parse attempt above covers plugins that send the canonical
		// 26-char string instead.
		if len(ev.GetId()) == 16 {
			var raw ulid.ULID
			copy(raw[:], ev.GetId())
			id = raw
		} else {
			return eventbus.Event{}, oops.Code("AUDIT_PLUGIN_HISTORY_BAD_ID").
				With("plugin", s.pluginName).
				Wrap(err)
		}
	}

	actor := eventbus.Actor{}
	if a := ev.GetActor(); a != nil {
		// ActorKind enum is bounded by the proto definition (0..4); the
		// downcast is statically safe but govet-gosec flags it. The
		// explicit switch makes the mapping intent-clear AND makes the
		// narrowing explicit to the linter.
		switch a.GetKind() {
		case eventbusv1.ActorKind_ACTOR_KIND_CHARACTER:
			actor.Kind = eventbus.ActorKindCharacter
		case eventbusv1.ActorKind_ACTOR_KIND_PLAYER:
			actor.Kind = eventbus.ActorKindPlayer
		case eventbusv1.ActorKind_ACTOR_KIND_SYSTEM:
			actor.Kind = eventbus.ActorKindSystem
		case eventbusv1.ActorKind_ACTOR_KIND_PLUGIN:
			actor.Kind = eventbus.ActorKindPlugin
		default:
			actor.Kind = eventbus.ActorKindUnknown
		}
		if len(a.GetId()) == 16 {
			var raw ulid.ULID
			copy(raw[:], a.GetId())
			actor.ID = raw
		}
	}
	out := eventbus.Event{
		ID:        id,
		Subject:   eventbus.Subject(ev.GetSubject()),
		Type:      eventbus.Type(ev.GetType()),
		Timestamp: ev.GetTimestamp().AsTime(),
		Actor:     actor,
		Payload:   ev.GetPayload(),
	}
	// Stamp the plugin-source-of-truth row so the read-side fence
	// (history.PluginDowngradeFence) can recover codec / dek_ref /
	// dek_version verbatim — INV-CRYPTO-42 + INV-CRYPTO-50 (Phase 7).
	eventbus.StampAuditRow(&out, ev)
	return out, nil
}

// Close cancels the underlying RPC. Idempotent.
func (s *pluginHistoryStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	// Cancel the child ctx we bound to the streaming RPC so the plugin-side
	// handler is released immediately instead of waiting for the outer query
	// ctx to end.
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

// directionProto maps the host-side Direction enum to the proto int32
// representation defined by QueryHistoryRequest (spec §audit.proto).
func directionProto(d eventbus.Direction) int32 {
	switch d {
	case eventbus.DirectionBackward:
		return 2
	case eventbus.DirectionForward:
		return 1
	default:
		return 0
	}
}
