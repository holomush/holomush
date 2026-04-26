// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus/cursor"
	"github.com/holomush/holomush/internal/session"
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

	// Token-based authentication (spec §3.3.5, §5.4): the host issues a
	// per-dispatch token in DeliverEvent / DeliverCommand and stores the
	// vouched-for actor (Kind + ID) keyed by (pluginName, token). The
	// plugin presents the token in the x-holomush-emit-token header on
	// EmitEvent. The plugin's x-holomush-actor-kind / -actor-id metadata
	// values are NOT trusted as identity claims at this boundary — the
	// host uses the actor it stored at issue time. This closes the
	// forgery surface (G1): a malicious plugin that substitutes the
	// actor headers cannot escape the token's stored actor.
	md, _ := metadata.FromIncomingContext(ctx)
	tokens := md.Get("x-holomush-emit-token")
	if len(tokens) == 0 || tokens[0] == "" {
		return nil, oops.Code("EMIT_TOKEN_MISSING").
			With("plugin", s.pluginName).
			Errorf("plugin emitted without a host-issued dispatch token")
	}

	s.host.mu.RLock()
	tokenStore := s.host.tokenStore
	s.host.mu.RUnlock()

	storedActor, ok := tokenStore.Lookup(s.pluginName, tokens[0])
	if !ok {
		slog.WarnContext(ctx, "EmitEvent rejected: token not valid for this plugin",
			"plugin", s.pluginName,
			"code", "EMIT_TOKEN_REJECTED",
		)
		return nil, oops.Code("EMIT_TOKEN_REJECTED").
			With("plugin", s.pluginName).
			Errorf("dispatch token is not valid for this plugin")
	}

	emitCtx := core.WithActor(ctx, storedActor)
	if err := emitter.Emit(emitCtx, s.pluginName, pluginsdk.EmitIntent{
		// TODO(F5): proto request field renames to Subject; keep Stream on
		// the wire until the proto regeneration task runs.
		Subject: req.GetStream(),
		Type:    pluginsdk.EventType(req.GetEventType()),
		Payload: string(req.GetPayload()),
	}); err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	return &pluginv1.PluginHostServiceEmitEventResponse{}, nil
}

func (s *pluginHostServiceServer) JoinFocus(ctx context.Context, req *pluginv1.PluginHostServiceJoinFocusRequest) (*pluginv1.PluginHostServiceJoinFocusResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	fc := s.host.FocusCoordinator()
	if fc == nil {
		return nil, oops.With("plugin", s.pluginName).New("focus coordinator not configured")
	}

	key, err := protoToFocusKey(req.GetTarget())
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	if err := fc.JoinFocus(ctx, req.GetSessionId(), key); err != nil {
		return nil, oops.With("plugin", s.pluginName).With("session_id", req.GetSessionId()).Wrap(err)
	}
	return &pluginv1.PluginHostServiceJoinFocusResponse{}, nil
}

func (s *pluginHostServiceServer) LeaveFocus(ctx context.Context, req *pluginv1.PluginHostServiceLeaveFocusRequest) (*pluginv1.PluginHostServiceLeaveFocusResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	fc := s.host.FocusCoordinator()
	if fc == nil {
		return nil, oops.With("plugin", s.pluginName).New("focus coordinator not configured")
	}

	key, err := protoToFocusKey(req.GetTarget())
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	if err := fc.LeaveFocus(ctx, req.GetSessionId(), key); err != nil {
		return nil, oops.With("plugin", s.pluginName).With("session_id", req.GetSessionId()).Wrap(err)
	}
	return &pluginv1.PluginHostServiceLeaveFocusResponse{}, nil
}

func (s *pluginHostServiceServer) LeaveFocusByTarget(ctx context.Context, req *pluginv1.PluginHostServiceLeaveFocusByTargetRequest) (*pluginv1.PluginHostServiceLeaveFocusByTargetResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	fc := s.host.FocusCoordinator()
	if fc == nil {
		return nil, oops.With("plugin", s.pluginName).New("focus coordinator not configured")
	}

	key, err := protoToFocusKey(req.GetTarget())
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	// Enumeration failure is the only path that returns an RPC-level error.
	// Partial per-session failures are carried on the response via
	// failed_session_ids; callers inspect the result to distinguish
	// full / partial / empty outcomes without parsing error strings.
	result, err := fc.LeaveFocusByTarget(ctx, key)
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).
			With("focus_kind", string(key.Kind)).
			With("target_id", key.TargetID.String()).Wrap(err)
	}
	return leaveByTargetResultToProto(result), nil
}

// leaveByTargetResultToProto converts the host-side sweep result to the
// wire format. Callers reconstruct partial-success state from
// succeeded + len(failed_session_ids) == total_scanned.
func leaveByTargetResultToProto(r session.LeaveByTargetResult) *pluginv1.PluginHostServiceLeaveFocusByTargetResponse {
	resp := &pluginv1.PluginHostServiceLeaveFocusByTargetResponse{
		Succeeded:    clampCountToInt32(r.Succeeded),
		TotalScanned: clampCountToInt32(r.TotalScanned),
	}
	if len(r.Failed) > 0 {
		resp.FailedSessionIds = make([]string, 0, len(r.Failed))
		for _, f := range r.Failed {
			resp.FailedSessionIds = append(resp.FailedSessionIds, f.SessionID)
		}
	}
	return resp
}

// clampCountToInt32 narrows a Go int to proto int32 safely. The session count
// is bounded by live-session capacity (far below math.MaxInt32 in any realistic
// deployment), but explicit bounds keep gosec quiet and guard against future
// 64-bit-only callers.
func clampCountToInt32(n int) int32 {
	switch {
	case n < 0:
		return 0
	case n > math.MaxInt32:
		return math.MaxInt32
	default:
		return int32(n)
	}
}

func (s *pluginHostServiceServer) PresentFocus(ctx context.Context, req *pluginv1.PluginHostServicePresentFocusRequest) (*pluginv1.PluginHostServicePresentFocusResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	fc := s.host.FocusCoordinator()
	if fc == nil {
		return nil, oops.With("plugin", s.pluginName).New("focus coordinator not configured")
	}

	key, err := protoToFocusKey(req.GetTarget())
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	if err := fc.PresentFocus(ctx, req.GetSessionId(), key); err != nil {
		return nil, oops.With("plugin", s.pluginName).With("session_id", req.GetSessionId()).Wrap(err)
	}
	return &pluginv1.PluginHostServicePresentFocusResponse{}, nil
}

// protoToFocusKey converts a proto FocusKey to the session.FocusKey domain type.
func protoToFocusKey(pk *pluginv1.FocusKey) (session.FocusKey, error) {
	if pk == nil {
		return session.FocusKey{}, oops.Code("INVALID_ARGUMENT").
			Errorf("focus key is required")
	}

	targetID, err := ulid.Parse(pk.GetTargetId())
	if err != nil {
		return session.FocusKey{}, oops.Code("INVALID_ARGUMENT").
			With("target_id", pk.GetTargetId()).
			Wrap(err)
	}

	kind, err := protoToFocusKind(pk.GetKind())
	if err != nil {
		return session.FocusKey{}, err
	}

	return session.FocusKey{Kind: kind, TargetID: targetID}, nil
}

// protoToFocusKind maps proto FocusKind to session.FocusKind.
func protoToFocusKind(pk pluginv1.FocusKind) (session.FocusKind, error) {
	switch pk {
	case pluginv1.FocusKind_FOCUS_KIND_SCENE:
		return session.FocusKindScene, nil
	default:
		return "", oops.Code("FOCUS_KIND_UNREGISTERED").
			With("kind", pk.String()).
			Errorf("unsupported focus kind: %s", pk.String())
	}
}

const maxQueryStreamHistoryCount = 500

func (s *pluginHostServiceServer) QueryStreamHistory(ctx context.Context, req *pluginv1.PluginHostServiceQueryStreamHistoryRequest) (*pluginv1.PluginHostServiceQueryStreamHistoryResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	hr := s.host.HistoryReader()
	if hr == nil {
		return nil, oops.With("plugin", s.pluginName).New("history reader not configured")
	}

	count := int(req.GetCount())
	if count < 0 {
		return nil, oops.Code("INVALID_ARGUMENT").
			With("count", req.GetCount()).
			Errorf("count must be non-negative")
	}
	if count > maxQueryStreamHistoryCount {
		count = maxQueryStreamHistoryCount
	}

	var notBefore time.Time
	if req.GetNotBeforeMs() > 0 {
		notBefore = time.UnixMilli(req.GetNotBeforeMs()).UTC()
	}

	// Decode the opaque cursor (if any) to extract the beforeID for
	// ReplayTail. The cursor bytes are a host-format OwnerHost token
	// (Seq + ULID) produced by encodeHostEventCursor below. On first
	// page the cursor is empty and we pass the zero ULID.
	var beforeID ulid.ULID
	if len(req.GetCursor()) > 0 {
		c, decodeErr := cursor.Decode(req.GetCursor())
		if decodeErr != nil {
			return nil, oops.Code("INVALID_ARGUMENT").
				With("plugin", s.pluginName).
				Wrap(decodeErr)
		}
		if c.Host != nil {
			beforeID = c.Host.ID
		}
	}

	events, err := hr.ReplayTail(ctx, req.GetStream(), count, notBefore, beforeID)
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).With("stream", req.GetStream()).Wrap(err)
	}

	protoEvents := make([]*pluginv1.Event, 0, len(events))
	for _, e := range events {
		pe := coreEventToProto(e)
		pe.Cursor = encodeHostEventCursor(e.ID)
		protoEvents = append(protoEvents, pe)
	}

	// Populate next_cursor from the oldest (first) event in the page, which
	// is the pagination anchor for the next backward read. ReplayTail returns
	// events in ascending order (oldest→newest), so index 0 is the boundary.
	var nextCursor []byte
	if len(protoEvents) == count && len(protoEvents) > 0 {
		nextCursor = protoEvents[0].GetCursor()
	}

	return &pluginv1.PluginHostServiceQueryStreamHistoryResponse{
		Events:     protoEvents,
		NextCursor: nextCursor,
	}, nil
}

// encodeHostEventCursor encodes an event ULID into an opaque host cursor
// token for the plugin → host boundary. Seq is not available here (the
// plugins.HistoryReader.ReplayTail interface returns core.Event without Seq),
// so Seq=0 is used. The cold tier handles Seq=0 as "ID-only" fallback.
// Returns nil on encoding failure (non-fatal; client cannot paginate from
// this event but the page result is still valid).
func encodeHostEventCursor(id ulid.ULID) []byte {
	b, err := cursor.Encode(cursor.Cursor{
		Version: cursor.CurrentVersion,
		Epoch:   cursor.CurrentEpoch(),
		Owner:   cursor.Owner{Kind: cursor.OwnerHost},
		Host:    &cursor.HostCursor{Seq: 0, ID: id},
	})
	if err != nil {
		return nil
	}
	return b
}

// coreEventToProto converts a core.Event to the plugin proto Event.
func coreEventToProto(e core.Event) *pluginv1.Event {
	return &pluginv1.Event{
		Id:        e.ID.String(),
		Stream:    e.Stream,
		Type:      string(e.Type),
		Timestamp: e.Timestamp.UnixMilli(),
		ActorKind: e.Actor.Kind.String(),
		ActorId:   e.Actor.ID,
		Payload:   string(e.Payload),
	}
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
