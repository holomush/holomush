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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus/cursor"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/settings"
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
	if tokenStore == nil {
		return nil, oops.Code("EMIT_TOKEN_STORE_UNCONFIGURED").
			With("plugin", s.pluginName).
			Errorf("plugin token store is not configured")
	}

	storedActor, ok := tokenStore.Lookup(s.pluginName, tokens[0])
	if !ok {
		slog.WarnContext(
			ctx, "emitEvent rejected: token not valid for this plugin",
			"plugin", s.pluginName,
			"code", "EMIT_TOKEN_REJECTED",
		)
		return nil, oops.Code("EMIT_TOKEN_REJECTED").
			With("plugin", s.pluginName).
			Errorf("dispatch token is not valid for this plugin")
	}

	emitCtx := core.WithActor(ctx, storedActor)
	if err := emitter.Emit(emitCtx, s.pluginName, pluginsdk.EmitIntentFromEmitRequest(req)); err != nil {
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

func (s *pluginHostServiceServer) SetConnectionFocus(ctx context.Context, req *pluginv1.PluginHostServiceSetConnectionFocusRequest) (*pluginv1.PluginHostServiceSetConnectionFocusResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	fc := s.host.FocusCoordinator()
	if fc == nil {
		return nil, oops.With("plugin", s.pluginName).New("focus coordinator not configured")
	}

	// Decode connection_id bytes → ULID (16-byte wire format, INV-P5-9).
	connID, err := bytesToULID(req.GetConnectionId())
	if err != nil {
		return nil, oops.Code("INVALID_ULID").
			With("plugin", s.pluginName).
			With("field", "connection_id").
			Wrap(err)
	}

	// Decode optional focus_key (nil = grid pivot).
	var focusKey *session.FocusKey
	if pk := req.GetFocusKey(); pk != nil {
		fk, parseErr := protoToFocusKey(pk)
		if parseErr != nil {
			return nil, oops.With("plugin", s.pluginName).Wrap(parseErr)
		}
		focusKey = &fk
	}

	// Reject contradictory input: is_scene_grid=true means "pivot to grid"
	// which requires focus_key to be nil. Without this gate the substrate
	// could persist focus_key while the live connection's stream-delta
	// computation below also routes scene streams — divergent state.
	// (CodeRabbit PR #4191)
	if focusKey != nil && req.GetIsSceneGrid() {
		return nil, oops.Code("INVALID_ARGUMENT").
			With("plugin", s.pluginName).
			Errorf("is_scene_grid=true is incompatible with a non-nil focus_key; supply one or the other")
	}

	_, err = fc.SetConnectionFocus(ctx, connID, focusKey, req.GetIsSceneGrid())
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).
			With("connection_id", connID.String()).
			Wrap(err)
	}

	// Echo back the new FocusKey (nil = grid).
	resp := &pluginv1.PluginHostServiceSetConnectionFocusResponse{}
	if focusKey != nil {
		resp.FocusKey = focusKeyToProto(*focusKey)
	}
	return resp, nil
}

func (s *pluginHostServiceServer) AutoFocusOnJoin(ctx context.Context, req *pluginv1.PluginHostServiceAutoFocusOnJoinRequest) (*pluginv1.PluginHostServiceAutoFocusOnJoinResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	fc := s.host.FocusCoordinator()
	if fc == nil {
		return nil, oops.With("plugin", s.pluginName).New("focus coordinator not configured")
	}

	charID, err := bytesToULID(req.GetCharacterId())
	if err != nil {
		return nil, oops.Code("INVALID_ULID").
			With("plugin", s.pluginName).
			With("field", "character_id").
			Wrap(err)
	}
	sceneID, err := bytesToULID(req.GetSceneId())
	if err != nil {
		return nil, oops.Code("INVALID_ULID").
			With("plugin", s.pluginName).
			With("field", "scene_id").
			Wrap(err)
	}

	r, err := fc.AutoFocusOnJoin(ctx, charID, sceneID)
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).
			With("character_id", charID.String()).
			With("scene_id", sceneID.String()).
			Wrap(err)
	}

	resp := &pluginv1.PluginHostServiceAutoFocusOnJoinResponse{
		TotalConnectionCount: r.TotalConnectionCount,
	}
	if len(r.FocusedConnectionIDs) > 0 {
		resp.FocusedConnectionIds = make([][]byte, len(r.FocusedConnectionIDs))
		for i, id := range r.FocusedConnectionIDs {
			resp.FocusedConnectionIds[i] = id.Bytes()
		}
	}
	if len(r.SkippedConnectionIDs) > 0 {
		resp.SkippedConnectionIds = make([][]byte, len(r.SkippedConnectionIDs))
		for i, id := range r.SkippedConnectionIDs {
			resp.SkippedConnectionIds[i] = id.Bytes()
		}
	}
	if len(r.FailedConnectionIDs) > 0 {
		resp.FailedConnectionIds = make([]*pluginv1.FocusFailure, len(r.FailedConnectionIDs))
		for i, f := range r.FailedConnectionIDs {
			resp.FailedConnectionIds[i] = &pluginv1.FocusFailure{
				ConnectionId: f.ConnectionID.Bytes(),
				Reason:       autoFocusFailureReasonToProto(f.Reason),
			}
		}
	}
	return resp, nil
}

func (s *pluginHostServiceServer) IsAnyConnFocused(ctx context.Context, req *pluginv1.PluginHostServiceIsAnyConnFocusedRequest) (*pluginv1.PluginHostServiceIsAnyConnFocusedResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	fc := s.host.FocusCoordinator()
	if fc == nil {
		return nil, oops.With("plugin", s.pluginName).New("focus coordinator not configured")
	}

	charID, err := bytesToULID(req.GetCharacterId())
	if err != nil {
		return nil, oops.Code("INVALID_ULID").
			With("plugin", s.pluginName).
			With("field", "character_id").
			Wrap(err)
	}
	sceneID, err := bytesToULID(req.GetSceneId())
	if err != nil {
		return nil, oops.Code("INVALID_ULID").
			With("plugin", s.pluginName).
			With("field", "scene_id").
			Wrap(err)
	}

	focused, err := fc.IsAnyConnFocused(ctx, charID, sceneID)
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).
			With("character_id", charID.String()).
			With("scene_id", sceneID.String()).
			Wrap(err)
	}
	return &pluginv1.PluginHostServiceIsAnyConnFocusedResponse{Focused: focused}, nil
}

// focusKeyToProto converts a session.FocusKey to the proto FocusKey type.
func focusKeyToProto(fk session.FocusKey) *pluginv1.FocusKey {
	return &pluginv1.FocusKey{
		Kind:     focusKindToProto(fk.Kind),
		TargetId: fk.TargetID.String(),
	}
}

// focusKindToProto maps session.FocusKind to proto FocusKind.
func focusKindToProto(k session.FocusKind) pluginv1.FocusKind {
	switch k {
	case session.FocusKindScene:
		return pluginv1.FocusKind_FOCUS_KIND_SCENE
	default:
		return pluginv1.FocusKind_FOCUS_KIND_UNSPECIFIED
	}
}

// bytesToULID converts a 16-byte proto bytes field to ulid.ULID.
// Returns INVALID_ULID error on wrong length (proto3 bytes ULID fields
// carry the 16-byte binary form, not the 26-char string encoding).
func bytesToULID(b []byte) (ulid.ULID, error) {
	if len(b) != 16 {
		return ulid.ULID{}, oops.Code("INVALID_ULID").
			Errorf("expected 16-byte ULID, got %d bytes", len(b))
	}
	var id ulid.ULID
	copy(id[:], b)
	return id, nil
}

// autoFocusFailureReasonToProto maps the string reason from AutoFocusOnJoin
// to the proto FocusFailureReason enum.
func autoFocusFailureReasonToProto(reason string) pluginv1.FocusFailureReason {
	switch reason {
	case "membership_absent":
		return pluginv1.FocusFailureReason_FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT
	case "connection_not_found":
		return pluginv1.FocusFailureReason_FOCUS_FAILURE_REASON_CONNECTION_NOT_FOUND
	default:
		return pluginv1.FocusFailureReason_FOCUS_FAILURE_REASON_UNSPECIFIED
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

// Evaluate implements PluginHostService.Evaluate for binary plugins.
//
// Security invariant: the acting subject is ALWAYS derived from the
// host-issued dispatch token, never from plugin-supplied fields. This
// mirrors EmitEvent's token→actor recovery exactly (spec §3.3.5 / §5.4).
//
// Fail-closed on: nil host, nil engine, nil tokenStore, missing token,
// rejected token, empty actor subject, and foreign resource types.
func (s *pluginHostServiceServer) Evaluate(ctx context.Context, req *pluginv1.PluginHostServiceEvaluateRequest) (*pluginv1.PluginHostServiceEvaluateResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}

	// Snapshot engine, auditor, and tokenStore under a single RLock, then release
	// before delegating — avoids holding the lock across the engine call.
	s.host.mu.RLock()
	eng := s.host.engine
	auditor := s.host.auditor
	tokenStore := s.host.tokenStore
	s.host.mu.RUnlock()

	if eng == nil {
		return nil, oops.Code("EVALUATE_ENGINE_UNCONFIGURED").
			With("plugin", s.pluginName).
			Errorf("access policy engine is not configured")
	}

	// Token-based authentication — mirrors EmitEvent §3.3.5 exactly:
	// read the host-issued dispatch token from the incoming metadata header;
	// look up the stored actor; never trust plugin-supplied identity claims.
	md, _ := metadata.FromIncomingContext(ctx)
	tokens := md.Get("x-holomush-emit-token")
	if len(tokens) == 0 || tokens[0] == "" {
		return nil, oops.Code("EMIT_TOKEN_MISSING").
			With("plugin", s.pluginName).
			Errorf("plugin evaluated without a host-issued dispatch token")
	}

	if tokenStore == nil {
		return nil, oops.Code("EMIT_TOKEN_STORE_UNCONFIGURED").
			With("plugin", s.pluginName).
			Errorf("plugin token store is not configured")
	}

	storedActor, ok := tokenStore.Lookup(s.pluginName, tokens[0])
	if !ok {
		slog.WarnContext(
			ctx, "evaluate rejected: token not valid for this plugin",
			"plugin", s.pluginName,
			"code", "EMIT_TOKEN_REJECTED",
		)
		return nil, oops.Code("EMIT_TOKEN_REJECTED").
			With("plugin", s.pluginName).
			Errorf("dispatch token is not valid for this plugin")
	}

	subject := pluginauthz.ActorSubject(storedActor)
	ownedTypes := s.host.ownedResourceTypes(s.pluginName)

	dec, err := pluginauthz.Evaluate(ctx, pluginauthz.Input{
		Engine:     eng,
		Auditor:    auditor,
		PluginName: s.pluginName,
		OwnedTypes: ownedTypes,
		Subject:    subject,
		Action:     req.GetAction(),
		Resource:   req.GetResource(),
	})
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	return &pluginv1.PluginHostServiceEvaluateResponse{
		Allowed:       dec.Allowed,
		Reason:        dec.Reason,
		MatchedPolicy: dec.MatchedPolicy,
	}, nil
}

// settingsGameWriteResource is the ABAC resource the host evaluates for a
// GAME-scope SetSetting. The subject (recovered from the dispatch token) must be
// permitted to "write" it; in practice only operator subjects are granted this,
// so a non-operator plugin/character is denied (PermissionDenied).
const settingsGameWriteResource = "setting:game"

// GetSetting reads one owner-partitioned setting for the calling plugin.
//
// Security invariants (holomush-iokti.7):
//   - The owner partition is bound host-side from s.pluginName (stamped at
//     construction, never from the request) via base.Owner(s.pluginName). Two
//     plugins with different names address disjoint partitions (INV-11).
//   - Scope must be specified; SETTING_SCOPE_UNSPECIFIED fails closed
//     (InvalidArgument).
//   - CHARACTER: req.principal_id must equal the acting character's ID (correct
//     and functional; the dispatch-token actor is always an ActorCharacter).
//   - PLAYER: intended semantics are "the owning player of the acting character"
//     (spec §3.3, INV-6 — settings shared across a player's characters). The
//     host-side char→player resolver is DEFERRED (holomush-iokti.19). Until it
//     lands, the gate is fail-closed: requirePrincipalOwnership compares
//     principal_id against the character actor's ID, and a player's ULID
//     differs from the acting character's ULID (distinct entities), so a real
//     player-principal PLAYER request is denied. Decision in holomush-iokti.16.
//   - GAME reads are server-wide readable by any plugin (no engine call): game
//     settings are not principal-scoped, and the owner prefix already isolates
//     the plugin's keyspace.
//
// Phase 8 settings are list-valued: the response sets found + string_list from
// StringSliceN. string_value (scalar reads) is deferred — left empty in Phase 8.
//
// Inner errors are never leaked past the gRPC boundary (grpc-errors.md): the
// settings stores follow the reads-never-error contract, so the only error paths
// here are argument validation and authorization.
func (s *pluginHostServiceServer) GetSetting(ctx context.Context, req *pluginv1.PluginHostServiceGetSettingRequest) (*pluginv1.PluginHostServiceGetSettingResponse, error) {
	// resolveSettingScope is the single authorization gate shared with
	// SetSetting: it recovers the acting subject from the dispatch token,
	// fails closed on a missing/invalid token or empty subject, and — for
	// PLAYER / CHARACTER scope — enforces requirePrincipalOwnership so a
	// plugin can only READ the settings of the principal it is currently
	// acting on behalf of (own settings only). A foreign principal_id ⇒
	// PermissionDenied before any read happens.
	//
	// GAME-read decision (plan Task 7 authz matrix): game settings are
	// server-wide readable by ANY plugin, so the GAME path deliberately
	// performs NO engine check. There is no cross-plugin leak because the
	// owner partition below (base.Owner(s.pluginName)) confines the read to
	// the calling plugin's own keyspace (INV-11). GAME *writes* still require
	// an operator engine decision in SetSetting; only reads are open.
	base, _, err := s.resolveSettingScope(ctx, req.GetScope(), req.GetPrincipalId())
	if err != nil {
		return nil, err
	}

	// Owner is bound from s.pluginName host-side — NEVER from the request.
	part := base.Owner(s.pluginName)

	values, found := part.StringSliceN(ctx, req.GetKey())
	return &pluginv1.PluginHostServiceGetSettingResponse{
		Found:      found,
		StringList: values,
		// StringValue intentionally empty: Phase 8 settings are list-valued and
		// returned via StringList. Scalar reads are deferred (holomush-iokti.7).
	}, nil
}

// SetSetting writes one owner-partitioned setting for the calling plugin.
//
// Security invariants (holomush-iokti.7):
//   - Owner partition bound host-side from s.pluginName (same as GetSetting).
//   - SETTING_SCOPE_UNSPECIFIED → InvalidArgument (fail closed).
//   - CHARACTER: req.principal_id must equal the acting character's ID (correct
//     and functional).
//   - PLAYER: fail-closed pending char→player resolution (holomush-iokti.19) —
//     see GetSetting invariants for the full rationale (holomush-iokti.16).
//   - GAME writes require an operator authorization decision: the recovered
//     subject must be permitted to "write" settingsGameWriteResource via the
//     ABAC engine. A non-operator subject is denied (PermissionDenied). This is
//     host-enforced, never trusted from the wire.
//
// Inner errors from the engine or the store are logged and replaced with a
// generic Internal status (grpc-errors.md).
func (s *pluginHostServiceServer) SetSetting(ctx context.Context, req *pluginv1.PluginHostServiceSetSettingRequest) (*pluginv1.PluginHostServiceSetSettingResponse, error) {
	base, subject, err := s.resolveSettingScope(ctx, req.GetScope(), req.GetPrincipalId())
	if err != nil {
		return nil, err
	}

	// GAME-scope writes require an operator authorization decision via the engine.
	if req.GetScope() == pluginv1.SettingScope_SETTING_SCOPE_GAME {
		if authErr := s.authorizeGameWrite(ctx, subject); authErr != nil {
			return nil, authErr
		}
	}

	// Owner is bound from s.pluginName host-side — NEVER from the request.
	part := base.Owner(s.pluginName)

	if setErr := part.SetStringSlice(ctx, req.GetKey(), req.GetStringList()); setErr != nil {
		slog.ErrorContext(ctx, "set setting failed",
			"plugin", s.pluginName, "scope", req.GetScope().String(), "err", setErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}
	return &pluginv1.PluginHostServiceSetSettingResponse{}, nil
}

// resolveSettingScope validates the scope, recovers the acting subject from the
// dispatch token (exactly as Evaluate does — never from plugin-supplied data),
// enforces principal ownership for PLAYER / CHARACTER scopes, and returns the
// base Scoped handle plus the recovered ABAC subject string.
//
// The returned Scoped is NEVER nil on a nil error.
func (s *pluginHostServiceServer) resolveSettingScope(
	ctx context.Context, scope pluginv1.SettingScope, principalID string,
) (settings.Scoped, string, error) {
	if s.host == nil {
		return nil, "", oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}

	// Fail closed on an unspecified scope.
	if scope == pluginv1.SettingScope_SETTING_SCOPE_UNSPECIFIED {
		return nil, "", status.Error(codes.InvalidArgument, "scope required") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}

	// Recover the acting actor from the dispatch token — mirrors Evaluate /
	// EmitEvent token→actor recovery. Plugin-supplied identity is never trusted.
	actor, err := s.actorFromToken(ctx)
	if err != nil {
		return nil, "", err
	}
	subject := pluginauthz.ActorSubject(actor)
	if subject == "" {
		return nil, "", status.Error(codes.PermissionDenied, "permission denied") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}

	switch scope {
	case pluginv1.SettingScope_SETTING_SCOPE_GAME:
		game := s.host.GameSettings()
		// Fail closed on an unwired deployment: a nil store would nil-deref
		// in Owner/StringSliceN below. Unimplemented signals "settings not
		// configured" to the plugin without leaking host internals.
		if game == nil {
			return nil, "", status.Error(codes.Unimplemented, "settings not configured") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
		}
		return game, subject, nil

	case pluginv1.SettingScope_SETTING_SCOPE_PLAYER:
		store := s.host.PlayerSettings()
		if store == nil {
			return nil, "", status.Error(codes.Unimplemented, "settings not configured") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
		}
		pid, ownErr := s.requirePrincipalOwnership(principalID, actor)
		if ownErr != nil {
			return nil, "", ownErr
		}
		return store.For(ctx, pid), subject, nil

	case pluginv1.SettingScope_SETTING_SCOPE_CHARACTER:
		store := s.host.CharacterSettings()
		if store == nil {
			return nil, "", status.Error(codes.Unimplemented, "settings not configured") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
		}
		pid, ownErr := s.requirePrincipalOwnership(principalID, actor)
		if ownErr != nil {
			return nil, "", ownErr
		}
		return store.For(ctx, pid), subject, nil

	default:
		return nil, "", status.Error(codes.InvalidArgument, "unsupported scope") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}
}

// requirePrincipalOwnership parses principalID as a ULID and enforces that the
// acting subject owns it (principal_id == actor.ID). Returns InvalidArgument on
// an unparseable/empty principal, PermissionDenied on mismatch.
//
// For CHARACTER scope this is correct and functional: the dispatch-token actor
// is always an ActorCharacter, so principal_id == character ID is the expected
// comparison (holomush-iokti.16).
//
// For PLAYER scope the INTENDED semantics are "the owning player of the acting
// character" (spec §3.3; holomush-iokti.16 decision). The char→player resolver
// is deferred to holomush-iokti.19. Until it lands the gate is fail-closed: a
// player's ULID differs from the acting character's ULID (distinct entities),
// so any real player-principal PLAYER request is denied. This is a deliberate
// interim contract, NOT a bug.
func (s *pluginHostServiceServer) requirePrincipalOwnership(principalID string, actor core.Actor) (ulid.ULID, error) {
	pid, err := ulid.Parse(principalID)
	if err != nil {
		return ulid.ULID{}, status.Error(codes.InvalidArgument, "invalid principal_id") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}
	// Compare against the token-recovered actor ID, never a request field.
	if principalID != actor.ID {
		return ulid.ULID{}, status.Error(codes.PermissionDenied, "permission denied") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}
	return pid, nil
}

// authorizeGameWrite evaluates the operator authorization decision required for
// a GAME-scope write. The subject (token-recovered) must be permitted to "write"
// settingsGameWriteResource. A deny → PermissionDenied; an engine/build failure
// is logged and surfaced as a generic Internal (no inner-error leak).
func (s *pluginHostServiceServer) authorizeGameWrite(ctx context.Context, subject string) error {
	s.host.mu.RLock()
	eng := s.host.engine
	s.host.mu.RUnlock()
	// Fail closed on a nil engine: a GAME write cannot be authorized without
	// the ABAC engine, so deny rather than nil-deref on eng.Evaluate below.
	// Unimplemented mirrors the nil-store guard in resolveSettingScope.
	if eng == nil {
		return status.Error(codes.Unimplemented, "settings not configured") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}

	areq, err := types.NewAccessRequest(subject, types.ActionWrite, settingsGameWriteResource, nil)
	if err != nil {
		slog.ErrorContext(ctx, "build game-write access request failed",
			"plugin", s.pluginName, "err", err)
		return status.Error(codes.Internal, "internal error") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}

	dec, err := eng.Evaluate(ctx, areq)
	if err != nil {
		slog.ErrorContext(ctx, "game-write authorization failed",
			"plugin", s.pluginName, "err", err)
		return status.Error(codes.Internal, "internal error") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}
	if !dec.IsAllowed() {
		return status.Error(codes.PermissionDenied, "permission denied") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}
	return nil
}

// actorFromToken recovers the host-issued dispatch-token actor from the incoming
// metadata, mirroring Evaluate / EmitEvent exactly. Plugin-supplied identity
// claims are never trusted. Fails closed on missing token, unconfigured store,
// or a token not valid for this plugin.
func (s *pluginHostServiceServer) actorFromToken(ctx context.Context) (core.Actor, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	tokens := md.Get("x-holomush-emit-token")
	if len(tokens) == 0 || tokens[0] == "" {
		return core.Actor{}, oops.Code("EMIT_TOKEN_MISSING").
			With("plugin", s.pluginName).
			Errorf("plugin called settings without a host-issued dispatch token")
	}

	s.host.mu.RLock()
	tokenStore := s.host.tokenStore
	s.host.mu.RUnlock()
	if tokenStore == nil {
		return core.Actor{}, oops.Code("EMIT_TOKEN_STORE_UNCONFIGURED").
			With("plugin", s.pluginName).
			Errorf("plugin token store is not configured")
	}

	storedActor, ok := tokenStore.Lookup(s.pluginName, tokens[0])
	if !ok {
		slog.WarnContext(ctx, "setting rejected: token not valid for this plugin",
			"plugin", s.pluginName, "code", "EMIT_TOKEN_REJECTED")
		return core.Actor{}, oops.Code("EMIT_TOKEN_REJECTED").
			With("plugin", s.pluginName).
			Errorf("dispatch token is not valid for this plugin")
	}
	return storedActor, nil
}

// DecryptOwnAuditRows decrypts a batch of the calling plugin's OWN audit rows
// host-side (the plugin never holds a DEK). Each row is gated by the OwnerMap
// g1 ownership check inside the ReadbackDecryptor; rows owned by a different
// plugin are refused with no_plaintext_reason="not_owner" before any decrypt
// (INV-RB-2). Results are returned 1:1 in request order (INV-RB-12). The
// per-call batch cap (DECRYPT_BATCH_TOO_LARGE on an over-cap batch) is enforced
// inside the common ReadbackDecryptor.DecryptOwnRows path — the SAME bound the
// Lua hostfunc adapter inherits, so neither runtime gets an unbounded batch the
// other is denied (plugin-runtime-symmetry invariant).
func (s *pluginHostServiceServer) DecryptOwnAuditRows(ctx context.Context, req *pluginv1.DecryptOwnAuditRowsRequest) (*pluginv1.DecryptOwnAuditRowsResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	dec := s.host.ReadbackDecryptor()
	if dec == nil {
		return nil, oops.With("plugin", s.pluginName).New("read-back decryptor not configured")
	}

	// The instance ID is not bound to the host-service struct; the plugin
	// name is the identity that matters for g1 ownership and the ReadBack
	// AuthGuard branch. Empty instance is informational only on the audit
	// record.
	const instanceID = ""

	results, err := dec.DecryptOwnRows(ctx, s.pluginName, instanceID, req.GetRows())
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}
	return &pluginv1.DecryptOwnAuditRowsResponse{Results: results}, nil
}

// RequestEmitToken issues a self-token bound to {ActorPlugin, pluginName}.
//
// Self-tokens cover the gap left by dispatch-token authentication when a
// plugin emits from a path that DID NOT originate at DeliverEvent or
// DeliverCommand — typically a plugin-served gRPC handler such as
// SceneService.CreateScene. Without a self-token, every such emit would
// fail with EMIT_TOKEN_MISSING after Task 9 landed.
//
// G1 (forgery resistance) preservation:
//   - The request carries no identity fields.
//   - The actor is hardcoded to {ActorPlugin, s.pluginName}; s.pluginName
//     is set at server construction (mTLS-bound) and the plugin cannot
//     forge it.
//   - The plugin's outgoing actor-claim metadata is still discarded at
//     EmitEvent — the host uses the tokenStore-bound actor.
//   - Manifest gate (actor_kinds_claimable must include "plugin") still
//     fires inside EmitEvent's emit path.
//   - Cross-plugin defense unchanged: tokenStore keys on (pluginName, token).
//   - Character-actor cascading still requires a real DeliverEvent /
//     DeliverCommand dispatch, where the host issues a character-bound
//     dispatch token; this self-token cannot grant that elevation.
//
// (Spec §3.3.5 / §5.4 — two-token pattern.)
func (s *pluginHostServiceServer) RequestEmitToken(_ context.Context, _ *pluginv1.PluginHostServiceRequestEmitTokenRequest) (*pluginv1.PluginHostServiceRequestEmitTokenResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}

	s.host.mu.RLock()
	tokenStore := s.host.tokenStore
	s.host.mu.RUnlock()
	if tokenStore == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin token store is not configured")
	}

	// HARDCODED actor: ActorPlugin + the mTLS-bound plugin name resolved
	// to a ULID via the IdentityRegistry. We deliberately ignore any
	// caller-supplied identity (the request has none) so this RPC cannot
	// be used as an actor-escalation vector. Post-w9ml the strict gate at
	// event_emitter.go::Emit rejects non-ULID actor IDs with
	// ACTOR_ID_NOT_ULID, so we MUST resolve a ULID here — using the plain
	// plugin name as ActorID would break every plugin self-token emit.
	storedActor, stampErr := stampPluginActor(s.host.identityRegistrySnapshot(), s.pluginName)
	if stampErr != nil {
		return nil, oops.Code("EMIT_TOKEN_ISSUE_FAILED").
			With("plugin", s.pluginName).
			Wrap(stampErr)
	}

	token, err := tokenStore.Issue(s.pluginName, storedActor)
	if err != nil {
		return nil, oops.Code("EMIT_TOKEN_ISSUE_FAILED").
			With("plugin", s.pluginName).
			Wrap(err)
	}

	return &pluginv1.PluginHostServiceRequestEmitTokenResponse{Token: token}, nil
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

// ListCommands implements PluginHostService.ListCommands for binary plugins.
// Unlike Evaluate/EmitEvent, this is read-only metadata keyed by the request's
// character_id (parity with the Lua list_commands host function), so it does NOT
// require a dispatch token. Fail-closed on nil host / nil querier.
func (s *pluginHostServiceServer) ListCommands(ctx context.Context, req *pluginv1.PluginHostServiceListCommandsRequest) (*pluginv1.PluginHostServiceListCommandsResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	s.host.mu.RLock()
	q := s.host.commandQuerier
	s.host.mu.RUnlock()
	if q == nil {
		return nil, oops.Code("COMMAND_QUERIER_UNCONFIGURED").With("plugin", s.pluginName).Errorf("command querier is not configured")
	}
	charID, err := ulid.Parse(req.GetCharacterId())
	if err != nil {
		return nil, oops.Code("INVALID_ARGUMENT").With("plugin", s.pluginName).Errorf("invalid character_id")
	}
	res, err := q.Available(ctx, access.CharacterSubject(charID.String()))
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}
	out := make([]*pluginv1.PluginHostServiceCommandInfo, 0, len(res.Commands))
	for i := range res.Commands {
		out = append(out, &pluginv1.PluginHostServiceCommandInfo{
			Name:   res.Commands[i].Name,
			Help:   res.Commands[i].Help,
			Usage:  res.Commands[i].Usage,
			Source: res.Commands[i].Source,
		})
	}
	return &pluginv1.PluginHostServiceListCommandsResponse{Commands: out, Incomplete: res.Incomplete}, nil
}

// GetCommandHelp implements PluginHostService.GetCommandHelp for binary plugins.
// Read-only; no dispatch token required (parity with Lua get_command_help host function).
func (s *pluginHostServiceServer) GetCommandHelp(ctx context.Context, req *pluginv1.PluginHostServiceGetCommandHelpRequest) (*pluginv1.PluginHostServiceGetCommandHelpResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	s.host.mu.RLock()
	q := s.host.commandQuerier
	s.host.mu.RUnlock()
	if q == nil {
		return nil, oops.Code("COMMAND_QUERIER_UNCONFIGURED").With("plugin", s.pluginName).Errorf("command querier is not configured")
	}
	charID, err := ulid.Parse(req.GetCharacterId())
	if err != nil {
		return nil, oops.Code("INVALID_ARGUMENT").With("plugin", s.pluginName).Errorf("invalid character_id")
	}
	d, err := q.Help(ctx, access.CharacterSubject(charID.String()), req.GetName())
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}
	return &pluginv1.PluginHostServiceGetCommandHelpResponse{
		Name:     d.Name,
		Help:     d.Help,
		Usage:    d.Usage,
		HelpText: d.HelpText,
		Source:   d.Source,
	}, nil
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
