// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"math"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/core"
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
	es := s.host.EventStore()
	if es == nil {
		return nil, oops.With("plugin", s.pluginName).New("event store not configured")
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

	events, err := es.ReplayTail(ctx, req.GetStream(), count, notBefore, ulid.ULID{})
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).With("stream", req.GetStream()).Wrap(err)
	}

	protoEvents := make([]*pluginv1.Event, 0, len(events))
	for _, e := range events {
		protoEvents = append(protoEvents, coreEventToProto(e))
	}
	return &pluginv1.PluginHostServiceQueryStreamHistoryResponse{Events: protoEvents}, nil
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
