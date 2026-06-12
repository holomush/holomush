// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"

	"github.com/samber/oops"

	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// This file implements the capability-scoped host.v1 gRPC services as thin
// translation shims over the legacy pluginHostServiceServer (host_service.go).
//
// Decomposition rationale (holomush-eykuh.1, Task 3): the broker serves ALL of
// these services on the one *grpc.Server alongside the still-present
// PluginHostService. Each per-capability server shares *Host + pluginName.
//
// host.v1 is a SELF-CONTAINED package (Task 2): it carries its OWN copies of
// the shared wire types — FocusKey / FocusKind / FocusFailureReason (focus),
// SettingScope (settings) — as hostv1.* Go types DISTINCT from the pluginv1.*
// types the *Host internals actually speak. So these shims are NOT byte-verbatim
// moves of the legacy bodies: each translates hostv1.<T> <-> pluginv1.<T> at the
// request/response boundary, then delegates the authenticated business logic to
// the legacy handler (single source of truth until Task 12 deletes it). The
// pluginv1<->hostv1 enum correspondence is pinned by
// TestPluginV1HostV1SharedTypeRoundTrip so a one-sided proto edit fails loudly.

// hostCapabilityBase carries the host handle, the mTLS-bound plugin name, and a
// legacy delegate. Every per-capability server embeds it so the shared *Host
// wiring and the delegation target are declared once (DRY). The delegate is the
// legacy server holding the verified, token-authenticated logic; the new servers
// only translate proto shapes around it.
type hostCapabilityBase struct {
	host       *Host
	pluginName string
}

// legacy returns a legacy server bound to the same host + plugin name. The
// legacy handler is the single implementation of the authenticated business
// logic; the capability servers delegate to it after translating proto shapes.
func (b hostCapabilityBase) legacy() *pluginHostServiceServer {
	return &pluginHostServiceServer{host: b.host, pluginName: b.pluginName}
}

// --- focusServer (FocusService: 8 RPCs) -------------------------------------

type focusServer struct {
	hostv1.UnimplementedFocusServiceServer
	hostCapabilityBase
}

func (s *focusServer) JoinFocus(ctx context.Context, req *hostv1.JoinFocusRequest) (*hostv1.JoinFocusResponse, error) {
	if _, err := s.legacy().JoinFocus(ctx, &pluginv1.PluginHostServiceJoinFocusRequest{
		SessionId: req.GetSessionId(),
		Target:    focusKeyToPluginV1(req.GetTarget()),
	}); err != nil {
		return nil, err
	}
	return &hostv1.JoinFocusResponse{}, nil
}

func (s *focusServer) LeaveFocus(ctx context.Context, req *hostv1.LeaveFocusRequest) (*hostv1.LeaveFocusResponse, error) {
	if _, err := s.legacy().LeaveFocus(ctx, &pluginv1.PluginHostServiceLeaveFocusRequest{
		SessionId: req.GetSessionId(),
		Target:    focusKeyToPluginV1(req.GetTarget()),
	}); err != nil {
		return nil, err
	}
	return &hostv1.LeaveFocusResponse{}, nil
}

func (s *focusServer) LeaveFocusByTarget(ctx context.Context, req *hostv1.LeaveFocusByTargetRequest) (*hostv1.LeaveFocusByTargetResponse, error) {
	resp, err := s.legacy().LeaveFocusByTarget(ctx, &pluginv1.PluginHostServiceLeaveFocusByTargetRequest{
		Target: focusKeyToPluginV1(req.GetTarget()),
	})
	if err != nil {
		return nil, err
	}
	return &hostv1.LeaveFocusByTargetResponse{
		Succeeded:        resp.GetSucceeded(),
		FailedSessionIds: resp.GetFailedSessionIds(),
		TotalScanned:     resp.GetTotalScanned(),
	}, nil
}

func (s *focusServer) PresentFocus(ctx context.Context, req *hostv1.PresentFocusRequest) (*hostv1.PresentFocusResponse, error) {
	if _, err := s.legacy().PresentFocus(ctx, &pluginv1.PluginHostServicePresentFocusRequest{
		SessionId: req.GetSessionId(),
		Target:    focusKeyToPluginV1(req.GetTarget()),
	}); err != nil {
		return nil, err
	}
	return &hostv1.PresentFocusResponse{}, nil
}

func (s *focusServer) SetConnectionFocus(ctx context.Context, req *hostv1.SetConnectionFocusRequest) (*hostv1.SetConnectionFocusResponse, error) {
	resp, err := s.legacy().SetConnectionFocus(ctx, &pluginv1.PluginHostServiceSetConnectionFocusRequest{
		ConnectionId: req.GetConnectionId(),
		FocusKey:     focusKeyToPluginV1(req.GetFocusKey()),
		IsSceneGrid:  req.GetIsSceneGrid(),
	})
	if err != nil {
		return nil, err
	}
	return &hostv1.SetConnectionFocusResponse{
		FocusKey: focusKeyToHostV1(resp.GetFocusKey()),
	}, nil
}

func (s *focusServer) GetConnectionFocus(ctx context.Context, req *hostv1.GetConnectionFocusRequest) (*hostv1.GetConnectionFocusResponse, error) {
	resp, err := s.legacy().GetConnectionFocus(ctx, &pluginv1.PluginHostServiceGetConnectionFocusRequest{
		ConnectionId: req.GetConnectionId(),
	})
	if err != nil {
		return nil, err
	}
	return &hostv1.GetConnectionFocusResponse{
		FocusKey: focusKeyToHostV1(resp.GetFocusKey()),
	}, nil
}

func (s *focusServer) AutoFocusOnJoin(ctx context.Context, req *hostv1.AutoFocusOnJoinRequest) (*hostv1.AutoFocusOnJoinResponse, error) {
	resp, err := s.legacy().AutoFocusOnJoin(ctx, &pluginv1.PluginHostServiceAutoFocusOnJoinRequest{
		CharacterId: req.GetCharacterId(),
		SceneId:     req.GetSceneId(),
	})
	if err != nil {
		return nil, err
	}
	out := &hostv1.AutoFocusOnJoinResponse{
		FocusedConnectionIds: resp.GetFocusedConnectionIds(),
		SkippedConnectionIds: resp.GetSkippedConnectionIds(),
		TotalConnectionCount: resp.GetTotalConnectionCount(),
	}
	if failed := resp.GetFailedConnectionIds(); len(failed) > 0 {
		out.FailedConnectionIds = make([]*hostv1.FocusFailure, len(failed))
		for i, f := range failed {
			out.FailedConnectionIds[i] = &hostv1.FocusFailure{
				ConnectionId: f.GetConnectionId(),
				Reason:       focusFailureReasonToHostV1(f.GetReason()),
			}
		}
	}
	return out, nil
}

func (s *focusServer) IsAnyConnFocused(ctx context.Context, req *hostv1.IsAnyConnFocusedRequest) (*hostv1.IsAnyConnFocusedResponse, error) {
	resp, err := s.legacy().IsAnyConnFocused(ctx, &pluginv1.PluginHostServiceIsAnyConnFocusedRequest{
		CharacterId: req.GetCharacterId(),
		SceneId:     req.GetSceneId(),
	})
	if err != nil {
		return nil, err
	}
	return &hostv1.IsAnyConnFocusedResponse{Focused: resp.GetFocused()}, nil
}

// --- emitServer (EmitService: EmitEvent, RequestEmitToken, RegisterEmitType) -

type emitServer struct {
	hostv1.UnimplementedEmitServiceServer
	hostCapabilityBase
}

func (s *emitServer) EmitEvent(ctx context.Context, req *hostv1.EmitEventRequest) (*hostv1.EmitEventResponse, error) {
	if _, err := s.legacy().EmitEvent(ctx, &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    req.GetStream(),
		EventType: req.GetEventType(),
		Payload:   req.GetPayload(),
		Sensitive: req.GetSensitive(),
	}); err != nil {
		return nil, err
	}
	return &hostv1.EmitEventResponse{}, nil
}

func (s *emitServer) RequestEmitToken(ctx context.Context, _ *hostv1.RequestEmitTokenRequest) (*hostv1.RequestEmitTokenResponse, error) {
	resp, err := s.legacy().RequestEmitToken(ctx, &pluginv1.PluginHostServiceRequestEmitTokenRequest{})
	if err != nil {
		return nil, err
	}
	return &hostv1.RequestEmitTokenResponse{Token: resp.GetToken()}, nil
}

// RegisterEmitType promotes the Lua holomush.register_emit_type(type) host
// function to the binary surface (INV-PLUGIN-32): the caller declares one bare
// plugin-owned event type it intends to emit. Binary plugins currently declare
// their emit-type set through InitResponse.RegisteredEmitTypes at Load time
// (captured on the plugin record; see host.go RegisteredEmitTypes), which the
// substrate validator checks against the manifest's crypto.emits. This RPC
// exposes the same single-string registration channel both runtimes share. The
// fail-closed nil-host guard mirrors every other host RPC. No host-side mutation
// is performed here in this sub-spec — the binary SDK client wiring lands in a
// later phase; serving the endpoint keeps both runtimes addressable through one
// channel without a behavior change to the existing Load-time path.
func (s *emitServer) RegisterEmitType(_ context.Context, req *hostv1.RegisterEmitTypeRequest) (*hostv1.RegisterEmitTypeResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	if req.GetEventType() == "" {
		return nil, oops.Code("INVALID_ARGUMENT").
			With("plugin", s.pluginName).
			Errorf("event_type is required")
	}
	return &hostv1.RegisterEmitTypeResponse{}, nil
}

// --- evalServer (EvalService: Evaluate) -------------------------------------

type evalServer struct {
	hostv1.UnimplementedEvalServiceServer
	hostCapabilityBase
}

func (s *evalServer) Evaluate(ctx context.Context, req *hostv1.EvaluateRequest) (*hostv1.EvaluateResponse, error) {
	resp, err := s.legacy().Evaluate(ctx, &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   req.GetAction(),
		Resource: req.GetResource(),
	})
	if err != nil {
		return nil, err
	}
	return &hostv1.EvaluateResponse{
		Allowed:       resp.GetAllowed(),
		Reason:        resp.GetReason(),
		MatchedPolicy: resp.GetMatchedPolicy(),
	}, nil
}

// --- settingsServer (SettingsService: GetSetting, SetSetting) ---------------

type settingsServer struct {
	hostv1.UnimplementedSettingsServiceServer
	hostCapabilityBase
}

func (s *settingsServer) GetSetting(ctx context.Context, req *hostv1.GetSettingRequest) (*hostv1.GetSettingResponse, error) {
	resp, err := s.legacy().GetSetting(ctx, &pluginv1.PluginHostServiceGetSettingRequest{
		Scope:       settingScopeToPluginV1(req.GetScope()),
		PrincipalId: req.GetPrincipalId(),
		Key:         req.GetKey(),
	})
	if err != nil {
		return nil, err
	}
	return &hostv1.GetSettingResponse{
		Found:       resp.GetFound(),
		StringValue: resp.GetStringValue(),
		StringList:  resp.GetStringList(),
	}, nil
}

func (s *settingsServer) SetSetting(ctx context.Context, req *hostv1.SetSettingRequest) (*hostv1.SetSettingResponse, error) {
	if _, err := s.legacy().SetSetting(ctx, &pluginv1.PluginHostServiceSetSettingRequest{
		Scope:       settingScopeToPluginV1(req.GetScope()),
		PrincipalId: req.GetPrincipalId(),
		Key:         req.GetKey(),
		StringList:  req.GetStringList(),
	}); err != nil {
		return nil, err
	}
	return &hostv1.SetSettingResponse{}, nil
}

// --- streamHistoryServer (StreamHistoryService: QueryStreamHistory) ---------

type streamHistoryServer struct {
	hostv1.UnimplementedStreamHistoryServiceServer
	hostCapabilityBase
}

func (s *streamHistoryServer) QueryStreamHistory(ctx context.Context, req *hostv1.QueryStreamHistoryRequest) (*hostv1.QueryStreamHistoryResponse, error) {
	resp, err := s.legacy().QueryStreamHistory(ctx, &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream:      req.GetStream(),
		Count:       req.GetCount(),
		NotBeforeMs: req.GetNotBeforeMs(),
		Cursor:      req.GetCursor(),
	})
	if err != nil {
		return nil, err
	}
	out := &hostv1.QueryStreamHistoryResponse{
		NextCursor: resp.GetNextCursor(),
	}
	if events := resp.GetEvents(); len(events) > 0 {
		out.Events = make([]*hostv1.Event, len(events))
		for i, e := range events {
			out.Events[i] = eventToHostV1(e)
		}
	}
	return out, nil
}

// --- streamSubscriptionServer (StreamSubscriptionService: Unimplemented) ----
//
// No implementation in this sub-spec (holomush-l6std): session-stream add/remove
// is deferred. Registering the Unimplemented base keeps the service addressable
// on the broker so a later sub-spec can fill it in without re-touching the broker
// registration.

type streamSubscriptionServer struct {
	hostv1.UnimplementedStreamSubscriptionServiceServer
	hostCapabilityBase
}

// --- auditServer (AuditService: DecryptOwnAuditRows) ------------------------
//
// DecryptOwnAuditRows reuses the pluginv1 crypto row types (hostv1's audit
// messages embed pluginv1.AuditRow / pluginv1.RowResult directly), so no
// per-row translation is needed — only the request/response envelope changes.

type auditServer struct {
	hostv1.UnimplementedAuditServiceServer
	hostCapabilityBase
}

func (s *auditServer) DecryptOwnAuditRows(ctx context.Context, req *hostv1.DecryptOwnAuditRowsRequest) (*hostv1.DecryptOwnAuditRowsResponse, error) {
	resp, err := s.legacy().DecryptOwnAuditRows(ctx, &pluginv1.DecryptOwnAuditRowsRequest{
		Rows: req.GetRows(),
	})
	if err != nil {
		return nil, err
	}
	return &hostv1.DecryptOwnAuditRowsResponse{Results: resp.GetResults()}, nil
}

// --- commandRegistryServer (CommandRegistryService: ListCommands, GetCommandHelp)

type commandRegistryServer struct {
	hostv1.UnimplementedCommandRegistryServiceServer
	hostCapabilityBase
}

func (s *commandRegistryServer) ListCommands(ctx context.Context, req *hostv1.ListCommandsRequest) (*hostv1.ListCommandsResponse, error) {
	resp, err := s.legacy().ListCommands(ctx, &pluginv1.PluginHostServiceListCommandsRequest{
		CharacterId: req.GetCharacterId(),
	})
	if err != nil {
		return nil, err
	}
	out := &hostv1.ListCommandsResponse{Incomplete: resp.GetIncomplete()}
	if cmds := resp.GetCommands(); len(cmds) > 0 {
		out.Commands = make([]*hostv1.CommandInfo, len(cmds))
		for i, c := range cmds {
			out.Commands[i] = &hostv1.CommandInfo{
				Name:   c.GetName(),
				Help:   c.GetHelp(),
				Usage:  c.GetUsage(),
				Source: c.GetSource(),
			}
		}
	}
	return out, nil
}

func (s *commandRegistryServer) GetCommandHelp(ctx context.Context, req *hostv1.GetCommandHelpRequest) (*hostv1.GetCommandHelpResponse, error) {
	resp, err := s.legacy().GetCommandHelp(ctx, &pluginv1.PluginHostServiceGetCommandHelpRequest{
		Name:        req.GetName(),
		CharacterId: req.GetCharacterId(),
	})
	if err != nil {
		return nil, err
	}
	return &hostv1.GetCommandHelpResponse{
		Name:     resp.GetName(),
		Help:     resp.GetHelp(),
		Usage:    resp.GetUsage(),
		HelpText: resp.GetHelpText(),
		Source:   resp.GetSource(),
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

// --- shared-type translation (pluginv1 <-> hostv1) --------------------------
//
// host.v1 carries its own copies of these shared wire types (Task 2). The *Host
// internals speak pluginv1; the capability servers above translate at the
// boundary. TestPluginV1HostV1SharedTypeRoundTrip pins the enum correspondence.

// focusKeyToPluginV1 converts a hostv1.FocusKey to its pluginv1 counterpart.
// A nil input yields nil (preserving the "grid pivot" / absent-key semantics).
func focusKeyToPluginV1(fk *hostv1.FocusKey) *pluginv1.FocusKey {
	if fk == nil {
		return nil
	}
	return &pluginv1.FocusKey{
		Kind:     focusKindToPluginV1(fk.GetKind()),
		TargetId: fk.GetTargetId(),
	}
}

// focusKeyToHostV1 converts a pluginv1.FocusKey to its hostv1 counterpart.
// A nil input yields nil.
func focusKeyToHostV1(fk *pluginv1.FocusKey) *hostv1.FocusKey {
	if fk == nil {
		return nil
	}
	return &hostv1.FocusKey{
		Kind:     focusKindToHostV1(fk.GetKind()),
		TargetId: fk.GetTargetId(),
	}
}

// focusKindToPluginV1 maps a hostv1.FocusKind to the pluginv1 enum. Unknown
// values collapse to UNSPECIFIED (fail-closed: an unrecognized kind is rejected
// downstream by protoToFocusKind in the legacy handler).
func focusKindToPluginV1(k hostv1.FocusKind) pluginv1.FocusKind {
	switch k {
	case hostv1.FocusKind_FOCUS_KIND_SCENE:
		return pluginv1.FocusKind_FOCUS_KIND_SCENE
	default:
		return pluginv1.FocusKind_FOCUS_KIND_UNSPECIFIED
	}
}

// focusKindToHostV1 maps a pluginv1.FocusKind to the hostv1 enum.
func focusKindToHostV1(k pluginv1.FocusKind) hostv1.FocusKind {
	switch k {
	case pluginv1.FocusKind_FOCUS_KIND_SCENE:
		return hostv1.FocusKind_FOCUS_KIND_SCENE
	default:
		return hostv1.FocusKind_FOCUS_KIND_UNSPECIFIED
	}
}

// focusKindFromHostV1 is the round-trip inverse used by the parity test.
func focusKindFromHostV1(k hostv1.FocusKind) pluginv1.FocusKind {
	return focusKindToPluginV1(k)
}

// focusFailureReasonToHostV1 maps a pluginv1.FocusFailureReason to the hostv1
// enum (response path of AutoFocusOnJoin).
func focusFailureReasonToHostV1(r pluginv1.FocusFailureReason) hostv1.FocusFailureReason {
	switch r {
	case pluginv1.FocusFailureReason_FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT:
		return hostv1.FocusFailureReason_FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT
	case pluginv1.FocusFailureReason_FOCUS_FAILURE_REASON_CONNECTION_NOT_FOUND:
		return hostv1.FocusFailureReason_FOCUS_FAILURE_REASON_CONNECTION_NOT_FOUND
	default:
		return hostv1.FocusFailureReason_FOCUS_FAILURE_REASON_UNSPECIFIED
	}
}

// autoFocusFailureReasonToHostV1ByReason is the test-facing alias used to pin
// the FocusFailureReason correspondence in TestPluginV1HostV1SharedTypeRoundTrip.
func autoFocusFailureReasonToHostV1ByReason(r pluginv1.FocusFailureReason) hostv1.FocusFailureReason {
	return focusFailureReasonToHostV1(r)
}

// settingScopeToPluginV1 maps a hostv1.SettingScope to the pluginv1 enum. The
// legacy resolveSettingScope fails closed on UNSPECIFIED, so an unknown value
// collapsing to UNSPECIFIED is the correct fail-closed behavior.
func settingScopeToPluginV1(scope hostv1.SettingScope) pluginv1.SettingScope {
	switch scope {
	case hostv1.SettingScope_SETTING_SCOPE_GAME:
		return pluginv1.SettingScope_SETTING_SCOPE_GAME
	case hostv1.SettingScope_SETTING_SCOPE_PLAYER:
		return pluginv1.SettingScope_SETTING_SCOPE_PLAYER
	case hostv1.SettingScope_SETTING_SCOPE_CHARACTER:
		return pluginv1.SettingScope_SETTING_SCOPE_CHARACTER
	default:
		return pluginv1.SettingScope_SETTING_SCOPE_UNSPECIFIED
	}
}

// eventToHostV1 converts a pluginv1.Event (QueryStreamHistory result) to the
// hostv1.Event copy. Field shapes are identical; only the Go type differs.
func eventToHostV1(e *pluginv1.Event) *hostv1.Event {
	if e == nil {
		return nil
	}
	return &hostv1.Event{
		Id:        e.GetId(),
		Stream:    e.GetStream(),
		Type:      e.GetType(),
		Timestamp: e.GetTimestamp(),
		ActorKind: e.GetActorKind(),
		ActorId:   e.GetActorId(),
		Payload:   e.GetPayload(),
		Cursor:    e.GetCursor(),
	}
}
