// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus/cursor"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/settings"
	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// This file implements the capability-scoped holomush.plugin.host.v1 gRPC
// services. After the holomush-eykuh.1 decomposition (Task 12) these are the
// SINGLE source of the authenticated host-callback logic: the former monolithic
// pluginHostServiceServer is gone and each authoritative handler body lives on
// the per-capability server for its domain (one capability per service). After
// the holomush-eykuh.2 relocation these bodies live in the runtime-neutral
// hostcap package so both the binary (goplugin) and Lua runtimes consume the
// SAME server implementations through the HostCapabilities port (INV-PLUGIN-49).
//
// hostCapabilityBase carries the host handle and the mTLS-bound plugin name; every
// per-capability server embeds it so the shared host wiring is declared once.
//
// host is the runtime-neutral hostcap.HostCapabilities port (holomush-eykuh.2),
// not a concrete *Host: the same server bodies serve both the binary runtime
// (where *Host satisfies the port, reading its fields under h.mu) and the Lua
// runtime (where a hostfunc-backed adapter satisfies it). The port never exposes
// the host mutex — every mutable-state read is a port method that locks
// internally.
type hostCapabilityBase struct {
	host       HostCapabilities
	pluginName string
}

// NewBase builds a hostCapabilityBase binding the given HostCapabilities port
// adapter and the mTLS-bound (binary) or wiring-time-established (Lua) plugin
// name. The base is embedded into every per-capability server. hostCapabilityBase
// stays unexported — NewBase is the only construction surface — so callers wire
// servers through NewBase + RegisterCapabilities (or the per-server constructors)
// rather than reaching into the struct.
// unexported by design (the public construction surface is NewBase +
// RegisterCapabilities). Callers only ever pass the returned value straight back
// into RegisterCapabilities / the New*Server constructors, never reaching into
// the struct — so the opaque return type is the intended ergonomics, not a wart.
//
//nolint:revive // unexported-return is intentional: hostCapabilityBase stays
func NewBase(host HostCapabilities, pluginName string) hostCapabilityBase {
	return hostCapabilityBase{host: host, pluginName: pluginName}
}

// --- focusServer (FocusService: 8 RPCs) -------------------------------------

type focusServer struct {
	hostv1.UnimplementedFocusServiceServer
	hostCapabilityBase
}

func (s *focusServer) JoinFocus(ctx context.Context, req *hostv1.JoinFocusRequest) (*hostv1.JoinFocusResponse, error) {
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
	return &hostv1.JoinFocusResponse{}, nil
}

func (s *focusServer) LeaveFocus(ctx context.Context, req *hostv1.LeaveFocusRequest) (*hostv1.LeaveFocusResponse, error) {
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
	return &hostv1.LeaveFocusResponse{}, nil
}

func (s *focusServer) LeaveFocusByTarget(ctx context.Context, req *hostv1.LeaveFocusByTargetRequest) (*hostv1.LeaveFocusByTargetResponse, error) {
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

func (s *focusServer) PresentFocus(ctx context.Context, req *hostv1.PresentFocusRequest) (*hostv1.PresentFocusResponse, error) {
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
	return &hostv1.PresentFocusResponse{}, nil
}

func (s *focusServer) SetConnectionFocus(ctx context.Context, req *hostv1.SetConnectionFocusRequest) (*hostv1.SetConnectionFocusResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	fc := s.host.FocusCoordinator()
	if fc == nil {
		return nil, oops.With("plugin", s.pluginName).New("focus coordinator not configured")
	}

	// Decode connection_id bytes → ULID (16-byte wire format, INV-SCENE-22).
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
	resp := &hostv1.SetConnectionFocusResponse{}
	if focusKey != nil {
		resp.FocusKey = focusKeyToProto(*focusKey)
	}
	return resp, nil
}

func (s *focusServer) GetConnectionFocus(ctx context.Context, req *hostv1.GetConnectionFocusRequest) (*hostv1.GetConnectionFocusResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	fc := s.host.FocusCoordinator()
	if fc == nil {
		return nil, oops.With("plugin", s.pluginName).New("focus coordinator not configured")
	}

	connID, err := bytesToULID(req.GetConnectionId())
	if err != nil {
		return nil, oops.Code("INVALID_ULID").
			With("plugin", s.pluginName).
			With("field", "connection_id").
			Wrap(err)
	}

	fk, err := fc.GetConnectionFocus(ctx, connID)
	if err != nil {
		slog.ErrorContext(
			ctx, "plugin host service GetConnectionFocus failed",
			"plugin", s.pluginName,
			"connection_id", connID.String(),
			"error", err,
		)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	resp := &hostv1.GetConnectionFocusResponse{}
	if fk != nil {
		resp.FocusKey = focusKeyToProto(*fk)
	}
	return resp, nil
}

func (s *focusServer) AutoFocusOnJoin(ctx context.Context, req *hostv1.AutoFocusOnJoinRequest) (*hostv1.AutoFocusOnJoinResponse, error) {
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

	resp := &hostv1.AutoFocusOnJoinResponse{
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
		resp.FailedConnectionIds = make([]*hostv1.FocusFailure, len(r.FailedConnectionIDs))
		for i, f := range r.FailedConnectionIDs {
			resp.FailedConnectionIds[i] = &hostv1.FocusFailure{
				ConnectionId: f.ConnectionID.Bytes(),
				Reason:       autoFocusFailureReasonToProto(f.Reason),
			}
		}
	}
	return resp, nil
}

func (s *focusServer) IsAnyConnFocused(ctx context.Context, req *hostv1.IsAnyConnFocusedRequest) (*hostv1.IsAnyConnFocusedResponse, error) {
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
	return &hostv1.IsAnyConnFocusedResponse{Focused: focused}, nil
}

// --- emitServer (EmitService: EmitEvent, RequestEmitToken, RegisterEmitType) -

type emitServer struct {
	hostv1.UnimplementedEmitServiceServer
	hostCapabilityBase
}

func (s *emitServer) EmitEvent(ctx context.Context, req *hostv1.EmitEventRequest) (*hostv1.EmitEventResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}

	emitter := s.host.EventEmitter()
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
	// actor headers cannot escape the token's stored actor. The port's
	// LookupActor performs the token read + store lookup (binary adapter);
	// EmitEvent does not need the owning player — actor identity is sufficient.
	storedActor, _, err := s.host.LookupActor(ctx, s.pluginName)
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	emitCtx := core.WithActor(ctx, storedActor)
	intent := pluginsdk.EmitIntent{
		Subject:   req.GetStream(),
		Type:      pluginsdk.EventType(req.GetEventType()),
		Payload:   string(req.GetPayload()),
		Sensitive: req.GetSensitive(),
	}
	if err := emitter.Emit(emitCtx, s.pluginName, intent); err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	return &hostv1.EmitEventResponse{}, nil
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
func (s *emitServer) RequestEmitToken(ctx context.Context, _ *hostv1.RequestEmitTokenRequest) (*hostv1.RequestEmitTokenResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}

	// HARDCODED actor: ActorPlugin + the mTLS-bound plugin name resolved
	// to a ULID via the IdentityRegistry. We deliberately ignore any
	// caller-supplied identity (the request has none) so this RPC cannot
	// be used as an actor-escalation vector. Post-w9ml the strict gate at
	// event_emitter.go::Emit rejects non-ULID actor IDs with
	// ACTOR_ID_NOT_ULID, so we MUST resolve a ULID here — using the plain
	// plugin name as ActorID would break every plugin self-token emit.
	// Stamp the self-token actor only when an identity registry exists. A nil
	// registry means the runtime has no emit-token forgery surface at all (the
	// Lua adapter returns nil): synthesizing a PLUGIN_UNREGISTERED_INVOKE here
	// would mask the adapter's intended UNSUPPORTED_OPERATION error and produce
	// observable cross-runtime contract drift. Leave the zero actor and let
	// IssueEmitToken give the runtime-appropriate answer. Binary always has a
	// registry, so a genuine missing-name PLUGIN_UNREGISTERED_INVOKE is preserved.
	var storedActor core.Actor
	if reg := s.host.IdentityRegistrySnapshot(); reg != nil {
		var stampErr error
		storedActor, stampErr = stampPluginActor(reg, s.pluginName)
		if stampErr != nil {
			return nil, oops.Code("EMIT_TOKEN_ISSUE_FAILED").
				With("plugin", s.pluginName).
				Wrap(stampErr)
		}
	}

	// IssueEmitToken mints the self-token via the port (binary adapter reads the
	// token store). Self-tokens carry no player context (no DeliverCommand
	// dispatch), so the owning player is "" — PLAYER-scope settings from a
	// self-token path fail closed, consistent with DeliverEvent.
	token, err := s.host.IssueEmitToken(ctx, s.pluginName, storedActor)
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	return &hostv1.RequestEmitTokenResponse{Token: token}, nil
}

// RegisterEmitType is the binary-surface counterpart of the Lua
// holomush.register_emit_type(type) host function (INV-PLUGIN-32). No host-side
// mutation is wired to this RPC: binary plugins declare their emit-type set at
// Load time through InitResponse.RegisteredEmitTypes (captured on the plugin
// record; see host.go RegisteredEmitTypes), and the Lua runtime registers through
// the in-process holomush.register_emit_type hostfunc. The dedicated binary SDK
// client wiring lands in a later phase. Until then the endpoint returns
// codes.Unimplemented rather than a misleading silent-success, giving either
// runtime's generated bridge a clear "not wired yet" contract. The handler is
// runtime-neutral, so the contract is identical across binary and Lua
// (plugin-runtime-symmetry).
func (s *emitServer) RegisterEmitType(_ context.Context, _ *hostv1.RegisterEmitTypeRequest) (*hostv1.RegisterEmitTypeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "RegisterEmitType is not implemented") //nolint:wrapcheck // gRPC status errors pass through as-is
}

// --- evalServer (EvalService: Evaluate) -------------------------------------

type evalServer struct {
	hostv1.UnimplementedEvalServiceServer
	hostCapabilityBase
}

// Evaluate runs the host ABAC engine for one action against one resource
// instance owned by the calling plugin.
//
// Security invariant: the acting subject is ALWAYS derived from the
// host-issued dispatch token, never from plugin-supplied fields. This
// mirrors EmitEvent's token→actor recovery exactly (spec §3.3.5 / §5.4).
//
// Fail-closed on: nil host, nil engine, nil tokenStore, missing token,
// rejected token, empty actor subject, and foreign resource types.
func (s *evalServer) Evaluate(ctx context.Context, req *hostv1.EvaluateRequest) (*hostv1.EvaluateResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}

	// Snapshot engine and auditor via the port (each accessor locks internally).
	eng := s.host.AccessEngine()
	auditor := s.host.Auditor()

	if eng == nil {
		return nil, oops.Code("EVALUATE_ENGINE_UNCONFIGURED").
			With("plugin", s.pluginName).
			Errorf("access policy engine is not configured")
	}

	// Token-based authentication — mirrors EmitEvent §3.3.5 exactly:
	// read the host-issued dispatch token from the incoming metadata header;
	// look up the stored actor; never trust plugin-supplied identity claims.
	// The port's LookupActor performs the read + lookup; Evaluate does not need
	// the owning player — actor identity is sufficient.
	storedActor, _, err := s.host.LookupActor(ctx, s.pluginName)
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	subject := pluginauthz.ActorSubject(storedActor)
	ownedTypes := s.host.OwnedResourceTypes(s.pluginName)

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

	return &hostv1.EvaluateResponse{
		Allowed:       dec.Allowed,
		Reason:        dec.Reason,
		MatchedPolicy: dec.MatchedPolicy,
	}, nil
}

// --- settingsServer (SettingsService: GetSetting, SetSetting) ---------------

type settingsServer struct {
	hostv1.UnimplementedSettingsServiceServer
	hostCapabilityBase
}

// GetSetting reads one plugin-partitioned setting for the calling plugin.
//
// Security invariants (holomush-iokti.7):
//   - The plugin partition is bound host-side from s.pluginName (stamped at
//     construction, never from the request) via base.Plugin(s.pluginName). Two
//     plugins with different names address disjoint partitions (INV-PLUGIN-28).
//   - Scope must be specified; SETTING_SCOPE_UNSPECIFIED fails closed
//     (InvalidArgument).
//   - CHARACTER: req.principal_id must equal the acting character's ID (correct
//     and functional; the dispatch-token actor is always an ActorCharacter).
//   - PLAYER: req.principal_id must equal the host-vouched owning player of the
//     acting character (spec §3.3, INV-6 — settings shared across a player's
//     characters). FUNCTIONAL as of holomush-iokti.19: the owning player is
//     carried on the dispatch token (stamped by the command dispatcher via
//     core.WithOwningPlayer), recovered by actorFromToken, and compared by
//     requirePrincipalOwnership. A PLAYER request whose dispatch had no player
//     context (e.g. from a pure event handler) fails closed (PermissionDenied).
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
func (s *settingsServer) GetSetting(ctx context.Context, req *hostv1.GetSettingRequest) (*hostv1.GetSettingResponse, error) {
	// resolveSettingScope is the shared authorization gate (see its doc and the
	// method-level invariants above): it fails closed on a bad token/subject and
	// enforces principal ownership for PLAYER / CHARACTER. GAME reads are open to
	// any plugin (no engine check); base.Plugin(s.pluginName) below confines the
	// read to the caller's own keyspace (INV-PLUGIN-28).
	base, _, err := s.resolveSettingScope(ctx, req.GetScope(), req.GetPrincipalId())
	if err != nil {
		return nil, err
	}

	// Plugin partition is bound from s.pluginName host-side — NEVER from the request.
	part := base.Plugin(s.pluginName)

	values, found := part.StringSliceN(ctx, req.GetKey())
	return &hostv1.GetSettingResponse{
		Found:      found,
		StringList: values,
		// StringValue intentionally empty: Phase 8 settings are list-valued and
		// returned via StringList. Scalar reads are deferred (holomush-iokti.7).
	}, nil
}

// SetSetting writes one plugin-partitioned setting for the calling plugin.
//
// Security invariants (holomush-iokti.7):
//   - Plugin partition bound host-side from s.pluginName (same as GetSetting).
//   - SETTING_SCOPE_UNSPECIFIED → InvalidArgument (fail closed).
//   - CHARACTER: req.principal_id must equal the acting character's ID (correct
//     and functional).
//   - PLAYER: req.principal_id must equal the host-vouched owning player of the
//     acting character — FUNCTIONAL as of holomush-iokti.19. See GetSetting
//     invariants for the full rationale; fails closed when no player context.
//   - GAME writes require an operator authorization decision: the recovered
//     subject must be permitted to "write" the per-plugin resource
//     pluginauthz.SettingsGameWriteResource(s.pluginName) via the ABAC engine. A
//     non-operator subject is denied (PermissionDenied). This is host-enforced,
//     never trusted from the wire.
//
// Inner errors from the engine or the store are logged and replaced with a
// generic Internal status (grpc-errors.md).
func (s *settingsServer) SetSetting(ctx context.Context, req *hostv1.SetSettingRequest) (*hostv1.SetSettingResponse, error) {
	base, subject, err := s.resolveSettingScope(ctx, req.GetScope(), req.GetPrincipalId())
	if err != nil {
		return nil, err
	}

	// GAME-scope writes require an operator authorization decision via the engine.
	if req.GetScope() == hostv1.SettingScope_SETTING_SCOPE_GAME {
		if authErr := s.authorizeGameWrite(ctx, subject); authErr != nil {
			return nil, authErr
		}
	}

	// Plugin partition is bound from s.pluginName host-side — NEVER from the request.
	part := base.Plugin(s.pluginName)

	if setErr := part.SetStringSlice(ctx, req.GetKey(), req.GetStringList()); setErr != nil {
		slog.ErrorContext(ctx, "set setting failed",
			"plugin", s.pluginName, "scope", req.GetScope().String(), "err", setErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}
	return &hostv1.SetSettingResponse{}, nil
}

// resolveSettingScope validates the scope, recovers the acting subject from the
// dispatch token (exactly as Evaluate does — never from plugin-supplied data),
// enforces principal ownership for PLAYER / CHARACTER scopes, and returns the
// base Scoped handle plus the recovered ABAC subject string.
//
// The returned Scoped is NEVER nil on a nil error.
func (b *hostCapabilityBase) resolveSettingScope(
	ctx context.Context, scope hostv1.SettingScope, principalID string,
) (settings.Scoped, string, error) {
	if b.host == nil {
		return nil, "", oops.With("plugin", b.pluginName).New("plugin host service is not configured")
	}

	// Fail closed on an unspecified scope.
	if scope == hostv1.SettingScope_SETTING_SCOPE_UNSPECIFIED {
		return nil, "", status.Error(codes.InvalidArgument, "scope required") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}

	// Recover the acting actor AND the host-vouched owning player from the
	// dispatch token — mirrors Evaluate / EmitEvent token recovery. Plugin-supplied
	// identity is never trusted.
	actor, ownerPlayer, err := b.actorFromToken(ctx)
	if err != nil {
		return nil, "", err
	}
	subject := pluginauthz.ActorSubject(actor)
	if subject == "" {
		return nil, "", status.Error(codes.PermissionDenied, "permission denied") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}

	switch scope {
	case hostv1.SettingScope_SETTING_SCOPE_GAME:
		game := b.host.GameSettings()
		// Fail closed on an unwired deployment: a nil store would nil-deref
		// in Owner/StringSliceN below. Unimplemented signals "settings not
		// configured" to the plugin without leaking host internals.
		if game == nil {
			return nil, "", status.Error(codes.Unimplemented, "settings not configured") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
		}
		return game, subject, nil

	case hostv1.SettingScope_SETTING_SCOPE_PLAYER:
		// PLAYER ownership compares principal_id against the host-vouched owning
		// player of the acting character (token-carried). The resulting pid IS the
		// player ULID, so store.For keys the player partition correctly.
		return b.principalScopedStore(ctx, b.host.PlayerSettings(), principalID, ownerPlayer, subject)

	case hostv1.SettingScope_SETTING_SCOPE_CHARACTER:
		// CHARACTER ownership compares principal_id against the acting character's
		// ID (the dispatch-token actor is always an ActorCharacter).
		return b.principalScopedStore(ctx, b.host.CharacterSettings(), principalID, actor.ID, subject)

	default:
		return nil, "", status.Error(codes.InvalidArgument, "unsupported scope") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}
}

// principalScopedFor is the narrow For-factory shape shared by the player and
// character settings stores. Both PlayerSettingsStore and CharacterSettingsStore
// satisfy it, letting principalScopedStore handle the structurally-identical
// nil-store guard, ownership check, and For() call once for both scopes.
type principalScopedFor interface {
	For(ctx context.Context, principalID ulid.ULID) settings.Scoped
}

// principalScopedStore is the shared PLAYER / CHARACTER resolution path: it fails
// closed when the store is unwired, enforces requirePrincipalOwnership against
// the caller-supplied expectedOwnerID, and returns the principal's Scoped handle.
//
// The per-scope security distinction is preserved at the call site: PLAYER passes
// the host-vouched owning player, CHARACTER passes the acting character's ID. This
// helper never chooses the expected owner — it only deduplicates the surrounding
// guard/lookup boilerplate. A nil store interface value (an unset store) fails
// closed with Unimplemented.
func (b *hostCapabilityBase) principalScopedStore(
	ctx context.Context, store principalScopedFor, principalID, expectedOwnerID, subject string,
) (settings.Scoped, string, error) {
	// An unset store is a true nil interface: Host().PlayerSettings() /
	// CharacterSettings() return the interface-typed field unchanged, and a nil
	// PlayerSettingsStore / CharacterSettingsStore converts to a nil
	// principalScopedFor — so this guard catches the unwired case as before.
	if store == nil {
		return nil, "", status.Error(codes.Unimplemented, "settings not configured") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}
	pid, ownErr := b.requirePrincipalOwnership(principalID, expectedOwnerID)
	if ownErr != nil {
		return nil, "", ownErr
	}
	return store.For(ctx, pid), subject, nil
}

// requirePrincipalOwnership enforces that the request's principal_id equals the
// host-vouched expectedOwnerID by delegating to the runtime-neutral shared gate
// pluginauthz.CheckPrincipalOwnership — the SAME helper the Lua
// get_setting/set_setting hostfuncs use, so the binary and Lua ownership trust
// checks cannot diverge (plugin-runtime-symmetry, INV-PLUGIN-27). The oops codes the
// helper returns are mapped to the gRPC statuses this RPC has always returned:
// INVALID_PRINCIPAL_ID → InvalidArgument ("invalid principal_id"),
// PRINCIPAL_NOT_OWNED → PermissionDenied ("permission denied").
//
// expectedOwnerID is supplied by resolveSettingScope per scope:
//   - CHARACTER: the acting character's ID (dispatch-token actor is always an
//     ActorCharacter).
//   - PLAYER: the host-vouched owning player ULID of the acting character,
//     recovered from the dispatch token (holomush-iokti.19). PLAYER scope is now
//     FUNCTIONAL — a principal_id matching the owning player succeeds. When the
//     dispatch carried no player context the owning player is "" and the shared
//     gate fails closed (PRINCIPAL_NOT_OWNED → PermissionDenied).
func (b *hostCapabilityBase) requirePrincipalOwnership(principalID, expectedOwnerID string) (ulid.ULID, error) {
	pid, err := pluginauthz.CheckPrincipalOwnership(principalID, expectedOwnerID)
	if err == nil {
		return pid, nil
	}
	oopsErr, _ := oops.AsOops(err)
	if oopsErr.Code() == "PRINCIPAL_NOT_OWNED" {
		return ulid.ULID{}, status.Error(codes.PermissionDenied, "permission denied") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}
	// INVALID_PRINCIPAL_ID and any unexpected code fail closed as InvalidArgument.
	return ulid.ULID{}, status.Error(codes.InvalidArgument, "invalid principal_id") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
}

// authorizeGameWrite evaluates the operator authorization decision required for
// a GAME-scope write. The subject (token-recovered) must be permitted to "write"
// the per-plugin resource pluginauthz.SettingsGameWriteResource(s.pluginName).
// Using the per-plugin resource lets operator policies scope GAME-write per
// plugin (plugin-runtime-symmetry, INV-PLUGIN-27; holomush-iokti.15 Item 2).
// A deny → PermissionDenied; an engine/build failure is logged and surfaced as
// a generic Internal (no inner-error leak).
func (b *hostCapabilityBase) authorizeGameWrite(ctx context.Context, subject string) error {
	eng := b.host.AccessEngine()
	// Fail closed on a nil engine: a GAME write cannot be authorized without
	// the ABAC engine, so deny rather than nil-deref on eng.Evaluate below.
	// Unimplemented mirrors the nil-store guard in resolveSettingScope.
	if eng == nil {
		return status.Error(codes.Unimplemented, "settings not configured") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}

	areq, err := types.NewAccessRequest(subject, types.ActionWrite, pluginauthz.SettingsGameWriteResource(b.pluginName), nil)
	if err != nil {
		slog.ErrorContext(ctx, "build game-write access request failed",
			"plugin", b.pluginName, "err", err)
		return status.Error(codes.Internal, "internal error") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}

	dec, err := eng.Evaluate(ctx, areq)
	if err != nil {
		slog.ErrorContext(ctx, "game-write authorization failed",
			"plugin", b.pluginName, "err", err)
		return status.Error(codes.Internal, "internal error") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}
	if !dec.IsAllowed() {
		return status.Error(codes.PermissionDenied, "permission denied") //nolint:wrapcheck // status errors are gRPC-native, not wrapped per grpc-errors.md
	}
	return nil
}

// actorFromToken recovers the host-issued dispatch-token actor AND the
// host-vouched owning player ULID from the incoming metadata, mirroring
// Evaluate / EmitEvent token recovery. Plugin-supplied identity claims are never
// trusted. The owning player is the binary runtime's recovery of the value the
// dispatcher stamped via core.WithOwningPlayer (Lua recovers the same value
// in-process from the ctx); PLAYER-scope settings ownership compares it against
// the request's principal_id. It is "" when the dispatch had no player context.
// Fails closed on missing token, unconfigured store, or a token not valid for
// this plugin.
func (b *hostCapabilityBase) actorFromToken(ctx context.Context) (core.Actor, string, error) {
	// Delegate to the runtime-neutral port: the binary adapter reads the
	// host-issued token from ctx metadata and looks up the stored actor; the
	// Lua adapter recovers the connection-scoped actor from the context. The
	// fail-closed codes (EMIT_TOKEN_MISSING / _STORE_UNCONFIGURED / _REJECTED)
	// originate inside LookupActor.
	actor, ownerPlayer, err := b.host.LookupActor(ctx, b.pluginName)
	if err != nil {
		return core.Actor{}, "", oops.With("plugin", b.pluginName).Wrap(err)
	}
	return actor, ownerPlayer, nil
}

// --- streamHistoryServer (StreamHistoryService: QueryStreamHistory) ---------

type streamHistoryServer struct {
	hostv1.UnimplementedStreamHistoryServiceServer
	hostCapabilityBase
}

func (s *streamHistoryServer) QueryStreamHistory(ctx context.Context, req *hostv1.QueryStreamHistoryRequest) (*hostv1.QueryStreamHistoryResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	hr := s.host.HistoryReader()
	if hr == nil {
		return nil, oops.With("plugin", s.pluginName).New("history reader not configured")
	}

	// Instance-level ABAC on the concrete stream. The capability interceptor
	// authorizes stream.history only at the type level (resource "stream:*"), where
	// the system-namespace / audit / crypto forbids (keyed on the QUALIFIED
	// resource.stream.name) cannot match. AuthorizeStreamRead qualifies the
	// domain-relative stream and runs the default-deny capability decision on the
	// qualified resource — the single shared gate also used by the ambient Lua
	// holomush.query_stream_history hostfunc, so both runtimes enforce identically
	// (plugin-runtime-symmetry; holomush-xakba, INV-PLUGIN-50).
	dec, decErr := pluginauthz.AuthorizeStreamRead(ctx, pluginauthz.StreamReadInput{
		Engine:     s.host.AccessEngine(),
		Auditor:    s.host.Auditor(),
		PluginName: s.pluginName,
		Subject:    access.PluginSubject(s.pluginName),
		GameID:     s.host.GameID(),
		Stream:     req.GetStream(),
	})
	if decErr != nil {
		// Fail closed: log internally, return a generic error (do not leak inner
		// detail past the gRPC trust boundary — grpc-errors rule).
		errutil.LogErrorContext(ctx, "plugin stream.history capability check failed", decErr,
			"plugin", s.pluginName, "stream", req.GetStream())
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	if !dec.Allowed {
		return nil, status.Errorf(codes.PermissionDenied, "not authorized to read stream")
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

	protoEvents := make([]*hostv1.Event, 0, len(events))
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

	return &hostv1.QueryStreamHistoryResponse{
		Events:     protoEvents,
		NextCursor: nextCursor,
	}, nil
}

// --- streamSubscriptionServer (StreamSubscriptionService) -------------------
//
// Serves the stream.subscription capability (holomush-l6std): a plugin mutates
// an active session's stream subscriptions mid-session. Both RPCs run the
// instance-level concrete-stream authorization guard (AuthorizeStreamSubscribe,
// HIGH-3) BEFORE touching the host StreamRegistry — the same shared
// AuthorizePluginStreamContribution fence the session-establishment merge uses
// (R3-A), fencing a plugin to its own declared emit domains and rejecting
// system/audit/crypto namespaces IN-HANDLER (R2-B, not via the read-only seed
// forbids). The relative req.GetStream() is forwarded to the registry as-is (the
// ctrl path applyFilterCtrl qualifies it, R2-A), exactly as stream.history
// forwards req.GetStream() to ReplayTail.

type streamSubscriptionServer struct {
	hostv1.UnimplementedStreamSubscriptionServiceServer
	hostCapabilityBase
}

// authorizeSubscribe runs the shared concrete-stream guard for both RPCs. It
// maps guard errors to gRPC codes (relative/wildcard → InvalidArgument;
// forbidden/not-owned/deny → PermissionDenied; infra → Internal) without leaking
// inner error text past the boundary (.claude/rules/grpc-errors.md).
func (s *streamSubscriptionServer) authorizeSubscribe(ctx context.Context, stream string) error {
	if s.host == nil {
		return status.Errorf(codes.Internal, "internal error")
	}
	dec, decErr := pluginauthz.AuthorizeStreamSubscribe(ctx, pluginauthz.StreamSubscribeInput{
		Engine:           s.host.AccessEngine(),
		Auditor:          s.host.Auditor(),
		PluginName:       s.pluginName,
		Subject:          access.PluginSubject(s.pluginName),
		GameID:           s.host.GameID(),
		Stream:           stream,
		OwnedEmitDomains: s.host.OwnedEmitDomains(s.pluginName),
	})
	if decErr != nil {
		var code string
		if oopsErr, ok := oops.AsOops(decErr); ok {
			code, _ = oopsErr.Code().(string)
		}
		switch code {
		case "STREAM_NOT_RELATIVE", "STREAM_WILDCARD_FORBIDDEN":
			return status.Errorf(codes.InvalidArgument, "invalid stream reference")
		case "STREAM_FORBIDDEN_NAMESPACE", "STREAM_NAMESPACE_NOT_OWNED":
			return status.Errorf(codes.PermissionDenied, "not authorized to subscribe stream")
		default:
			errutil.LogErrorContext(ctx, "plugin stream.subscription capability check failed", decErr,
				"plugin", s.pluginName, "stream", stream)
			return status.Errorf(codes.Internal, "internal error")
		}
	}
	if !dec.Allowed {
		return status.Errorf(codes.PermissionDenied, "not authorized to subscribe stream")
	}
	return nil
}

// AddSessionStream subscribes an active session to a stream mid-session.
func (s *streamSubscriptionServer) AddSessionStream(ctx context.Context, req *hostv1.AddSessionStreamRequest) (*hostv1.AddSessionStreamResponse, error) {
	if err := s.authorizeSubscribe(ctx, req.GetStream()); err != nil {
		return nil, err
	}
	reg := s.host.StreamRegistry()
	if reg == nil {
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	mode := protoReplayModeToSession(req.GetReplayMode())
	if err := reg.AddStreamWithMode(ctx, req.GetSessionId(), req.GetStream(), mode); err != nil {
		errutil.LogErrorContext(ctx, "plugin AddSessionStream failed", err,
			"plugin", s.pluginName, "session_id", req.GetSessionId(), "stream", req.GetStream())
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	return &hostv1.AddSessionStreamResponse{}, nil
}

// RemoveSessionStream unsubscribes an active session from a stream. Idempotent.
func (s *streamSubscriptionServer) RemoveSessionStream(ctx context.Context, req *hostv1.RemoveSessionStreamRequest) (*hostv1.RemoveSessionStreamResponse, error) {
	if err := s.authorizeSubscribe(ctx, req.GetStream()); err != nil {
		return nil, err
	}
	reg := s.host.StreamRegistry()
	if reg == nil {
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	if err := reg.RemoveStream(ctx, req.GetSessionId(), req.GetStream()); err != nil {
		errutil.LogErrorContext(ctx, "plugin RemoveSessionStream failed", err,
			"plugin", s.pluginName, "session_id", req.GetSessionId(), "stream", req.GetStream())
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	return &hostv1.RemoveSessionStreamResponse{}, nil
}

// protoReplayModeToSession maps the proto StreamReplayMode to session.ReplayMode.
// UNSPECIFIED defaults to FROM_CURSOR (backward compatibility per the proto);
// LIVE_ONLY maps to ReplayModeLiveOnly (channels' mid-session join, HIGH-2).
func protoReplayModeToSession(m hostv1.StreamReplayMode) session.ReplayMode {
	switch m {
	case hostv1.StreamReplayMode_STREAM_REPLAY_MODE_LIVE_ONLY:
		return session.ReplayModeLiveOnly
	default:
		return session.ReplayModeFromCursor
	}
}

// --- auditServer (AuditService: DecryptOwnAuditRows) ------------------------

type auditServer struct {
	hostv1.UnimplementedAuditServiceServer
	hostCapabilityBase
}

// DecryptOwnAuditRows decrypts a batch of the calling plugin's OWN audit rows
// host-side (the plugin never holds a DEK). Each row is gated by the OwnerMap
// g1 ownership check inside the ReadbackDecryptor; rows owned by a different
// plugin are refused with no_plaintext_reason="not_owner" before any decrypt
// (INV-CRYPTO-27). Results are returned 1:1 in request order (INV-CRYPTO-37). The
// per-call batch cap (DECRYPT_BATCH_TOO_LARGE on an over-cap batch) is enforced
// inside the common ReadbackDecryptor.DecryptOwnRows path — the SAME bound the
// Lua hostfunc adapter inherits, so neither runtime gets an unbounded batch the
// other is denied (plugin-runtime-symmetry invariant). Request / response
// envelopes reuse the pluginv1 crypto row types (hostv1's audit messages embed
// holomush.plugin.v1.AuditRow / RowResult directly), so no per-row translation
// is needed.
func (s *auditServer) DecryptOwnAuditRows(ctx context.Context, req *hostv1.DecryptOwnAuditRowsRequest) (*hostv1.DecryptOwnAuditRowsResponse, error) {
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
	return &hostv1.DecryptOwnAuditRowsResponse{Results: results}, nil
}

// --- commandRegistryServer (CommandRegistryService: ListCommands, GetCommandHelp)

type commandRegistryServer struct {
	hostv1.UnimplementedCommandRegistryServiceServer
	hostCapabilityBase
}

// ListCommands enumerates the commands the acting character may execute,
// ABAC-filtered by the host. The ABAC subject is the HOST-VOUCHED actor recovered
// via the port's LookupActor (INV-PLUGIN-51) — runtime-neutral and uniform with
// evalServer.Evaluate: the binary adapter reads the host-issued dispatch token
// from the incoming gRPC metadata; the Lua adapter reads the connection-scoped
// actor from core.ActorFromContext. The wire-supplied character_id is NOT trusted
// for authorization (it would let any plugin enumerate command visibility for an
// arbitrary character) — the proto field is structurally ignored. Fails closed
// (NO_DISPATCH_SUBJECT) when no host-vouched actor is present; also on nil host /
// nil querier.
func (s *commandRegistryServer) ListCommands(ctx context.Context, _ *hostv1.ListCommandsRequest) (*hostv1.ListCommandsResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	q := s.host.CommandQuerier()
	if q == nil {
		return nil, oops.Code("COMMAND_QUERIER_UNCONFIGURED").With("plugin", s.pluginName).Errorf("command querier is not configured")
	}
	storedActor, _, err := s.host.LookupActor(ctx, s.pluginName)
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}
	subject := pluginauthz.ActorSubject(storedActor)
	if subject == "" {
		return nil, oops.Code("NO_DISPATCH_SUBJECT").With("plugin", s.pluginName).
			Errorf("command-registry call without a host-vouched actor")
	}
	res, err := q.Available(ctx, subject)
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}
	out := make([]*hostv1.CommandInfo, 0, len(res.Commands))
	for i := range res.Commands {
		out = append(out, &hostv1.CommandInfo{
			Name:   res.Commands[i].Name,
			Help:   res.Commands[i].Help,
			Usage:  res.Commands[i].Usage,
			Source: res.Commands[i].Source,
		})
	}
	return &hostv1.ListCommandsResponse{Commands: out, Incomplete: res.Incomplete}, nil
}

// GetCommandHelp returns full help detail for one command after an access check
// for the acting character. The ABAC subject is the HOST-VOUCHED actor recovered
// via the port's LookupActor (INV-PLUGIN-51) — runtime-neutral and uniform with
// evalServer.Evaluate: the binary adapter reads the host-issued dispatch token
// from the incoming gRPC metadata; the Lua adapter reads the connection-scoped
// actor from core.ActorFromContext. The wire-supplied character_id is NOT trusted
// for authorization — the proto field is structurally ignored. Fails closed
// (NO_DISPATCH_SUBJECT) when no host-vouched actor is present; also on nil host /
// nil querier.
func (s *commandRegistryServer) GetCommandHelp(ctx context.Context, req *hostv1.GetCommandHelpRequest) (*hostv1.GetCommandHelpResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	q := s.host.CommandQuerier()
	if q == nil {
		return nil, oops.Code("COMMAND_QUERIER_UNCONFIGURED").With("plugin", s.pluginName).Errorf("command querier is not configured")
	}
	storedActor, _, err := s.host.LookupActor(ctx, s.pluginName)
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}
	subject := pluginauthz.ActorSubject(storedActor)
	if subject == "" {
		return nil, oops.Code("NO_DISPATCH_SUBJECT").With("plugin", s.pluginName).
			Errorf("command-registry call without a host-vouched actor")
	}
	d, err := q.Help(ctx, subject, req.GetName())
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}
	return &hostv1.GetCommandHelpResponse{
		Name:     d.Name,
		Help:     d.Help,
		Usage:    d.Usage,
		HelpText: d.HelpText,
		Source:   d.Source,
	}, nil
}

// --- kvServer (KVService: Unimplemented) ------------------------------------
//
// No implementation in this sub-spec (holomush-l6std): plugin KV storage is
// deferred to sub-spec 3. The Unimplemented base is registered so the service is
// addressable on the broker without a behavior change.

type kvServer struct {
	hostv1.UnimplementedKVServiceServer
	hostCapabilityBase
}

// --- converters & helpers ---------------------------------------------------
//
// These conversion helpers moved with the server bodies (holomush-eykuh.2): they
// are the proto↔domain glue the relocated servers call and have no consumer
// outside this package.

// leaveByTargetResultToProto converts the host-side sweep result to the
// wire format. Callers reconstruct partial-success state from
// succeeded + len(failed_session_ids) == total_scanned.
func leaveByTargetResultToProto(r session.LeaveByTargetResult) *hostv1.LeaveFocusByTargetResponse {
	resp := &hostv1.LeaveFocusByTargetResponse{
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

// focusKeyToProto converts a session.FocusKey to the host.v1 FocusKey type.
func focusKeyToProto(fk session.FocusKey) *hostv1.FocusKey {
	return &hostv1.FocusKey{
		Kind:     focusKindToProto(fk.Kind),
		TargetId: fk.TargetID.String(),
	}
}

// focusKindToProto maps session.FocusKind to host.v1 FocusKind.
func focusKindToProto(k session.FocusKind) hostv1.FocusKind {
	switch k {
	case session.FocusKindScene:
		return hostv1.FocusKind_FOCUS_KIND_SCENE
	default:
		return hostv1.FocusKind_FOCUS_KIND_UNSPECIFIED
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
// to the host.v1 FocusFailureReason enum.
func autoFocusFailureReasonToProto(reason string) hostv1.FocusFailureReason {
	switch reason {
	case "membership_absent":
		return hostv1.FocusFailureReason_FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT
	case "connection_not_found":
		return hostv1.FocusFailureReason_FOCUS_FAILURE_REASON_CONNECTION_NOT_FOUND
	default:
		return hostv1.FocusFailureReason_FOCUS_FAILURE_REASON_UNSPECIFIED
	}
}

// protoToFocusKey converts a host.v1 FocusKey to the session.FocusKey domain type.
func protoToFocusKey(pk *hostv1.FocusKey) (session.FocusKey, error) {
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

// protoToFocusKind maps host.v1 FocusKind to session.FocusKind.
func protoToFocusKind(pk hostv1.FocusKind) (session.FocusKind, error) {
	switch pk {
	case hostv1.FocusKind_FOCUS_KIND_SCENE:
		return session.FocusKindScene, nil
	default:
		return "", oops.Code("FOCUS_KIND_UNREGISTERED").
			With("kind", pk.String()).
			Errorf("unsupported focus kind: %s", pk.String())
	}
}

const maxQueryStreamHistoryCount = 500

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

// coreEventToProto converts a core.Event to the host.v1 Event.
func coreEventToProto(e core.Event) *hostv1.Event {
	return &hostv1.Event{
		Id:        e.ID.String(),
		Stream:    e.Stream,
		Type:      string(e.Type),
		Timestamp: e.Timestamp.UnixMilli(),
		ActorKind: e.Actor.Kind.String(),
		ActorId:   e.Actor.ID,
		Payload:   string(e.Payload),
	}
}

// stampPluginActor resolves a plugin name to a core.Actor with a ULID-string
// ID via the IdentityRegistry. Returns PLUGIN_UNREGISTERED_INVOKE if the
// plugin is not active in the registry, or if the registry is nil (which is
// operationally equivalent: "no registry" and "registry doesn't have plugin"
// both mean the ULID cannot be resolved). This defensive nil-check keeps
// existing test fixtures that construct a host directly without registering it
// safe — they'll receive a clean error rather than a nil-pointer panic.
//
// A goplugin-private twin (goplugin.stampPluginActor) backs the binary host's
// own self-token paths; this copy is the runtime-neutral one the relocated
// emitServer.RequestEmitToken calls through the port snapshot. Both operate
// purely on the plugins.IdentityRegistry port, so they cannot drift in policy.
func stampPluginActor(reg plugins.IdentityRegistry, name string) (core.Actor, error) {
	if reg == nil {
		return core.Actor{}, oops.Code("PLUGIN_UNREGISTERED_INVOKE").
			With("plugin", name).
			Errorf("IdentityRegistry not configured on Host")
	}
	id, ok := reg.IDByName(name)
	if !ok {
		return core.Actor{}, oops.Code("PLUGIN_UNREGISTERED_INVOKE").
			With("plugin", name).
			Errorf("plugin not registered in IdentityRegistry")
	}
	return core.Actor{Kind: core.ActorPlugin, ID: id.String()}, nil
}
