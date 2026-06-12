// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"

	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// pluginHostServiceServer is a TEST-ONLY aggregate over the per-capability
// host.v1 servers. The production monolithic god-service was deleted in
// holomush-eykuh.1 (Task 12) and its authoritative handler bodies now live on
// the per-capability servers (host_capability_servers.go). This shim lets the
// existing behavior tests — which were written against the single server — keep
// driving every RPC through one struct without re-deriving the wiring per test.
// It carries NO logic of its own: each method delegates to the real capability
// server, so the tests exercise the production code paths unchanged.
type pluginHostServiceServer struct {
	host       *Host
	pluginName string
}

func (s *pluginHostServiceServer) base() hostCapabilityBase {
	return hostCapabilityBase{host: s.host, pluginName: s.pluginName}
}

func (s *pluginHostServiceServer) focus() *focusServer {
	return &focusServer{hostCapabilityBase: s.base()}
}

func (s *pluginHostServiceServer) emit() *emitServer {
	return &emitServer{hostCapabilityBase: s.base()}
}

func (s *pluginHostServiceServer) eval() *evalServer {
	return &evalServer{hostCapabilityBase: s.base()}
}

func (s *pluginHostServiceServer) settings() *settingsServer {
	return &settingsServer{hostCapabilityBase: s.base()}
}

func (s *pluginHostServiceServer) streamHistory() *streamHistoryServer {
	return &streamHistoryServer{hostCapabilityBase: s.base()}
}

func (s *pluginHostServiceServer) audit() *auditServer {
	return &auditServer{hostCapabilityBase: s.base()}
}

func (s *pluginHostServiceServer) commands() *commandRegistryServer {
	return &commandRegistryServer{hostCapabilityBase: s.base()}
}

// --- FocusService delegations ---

func (s *pluginHostServiceServer) JoinFocus(ctx context.Context, req *hostv1.JoinFocusRequest) (*hostv1.JoinFocusResponse, error) {
	return s.focus().JoinFocus(ctx, req)
}

func (s *pluginHostServiceServer) LeaveFocus(ctx context.Context, req *hostv1.LeaveFocusRequest) (*hostv1.LeaveFocusResponse, error) {
	return s.focus().LeaveFocus(ctx, req)
}

func (s *pluginHostServiceServer) LeaveFocusByTarget(ctx context.Context, req *hostv1.LeaveFocusByTargetRequest) (*hostv1.LeaveFocusByTargetResponse, error) {
	return s.focus().LeaveFocusByTarget(ctx, req)
}

func (s *pluginHostServiceServer) PresentFocus(ctx context.Context, req *hostv1.PresentFocusRequest) (*hostv1.PresentFocusResponse, error) {
	return s.focus().PresentFocus(ctx, req)
}

func (s *pluginHostServiceServer) SetConnectionFocus(ctx context.Context, req *hostv1.SetConnectionFocusRequest) (*hostv1.SetConnectionFocusResponse, error) {
	return s.focus().SetConnectionFocus(ctx, req)
}

func (s *pluginHostServiceServer) GetConnectionFocus(ctx context.Context, req *hostv1.GetConnectionFocusRequest) (*hostv1.GetConnectionFocusResponse, error) {
	return s.focus().GetConnectionFocus(ctx, req)
}

func (s *pluginHostServiceServer) AutoFocusOnJoin(ctx context.Context, req *hostv1.AutoFocusOnJoinRequest) (*hostv1.AutoFocusOnJoinResponse, error) {
	return s.focus().AutoFocusOnJoin(ctx, req)
}

func (s *pluginHostServiceServer) IsAnyConnFocused(ctx context.Context, req *hostv1.IsAnyConnFocusedRequest) (*hostv1.IsAnyConnFocusedResponse, error) {
	return s.focus().IsAnyConnFocused(ctx, req)
}

// --- EmitService delegations ---

func (s *pluginHostServiceServer) EmitEvent(ctx context.Context, req *hostv1.EmitEventRequest) (*hostv1.EmitEventResponse, error) {
	return s.emit().EmitEvent(ctx, req)
}

func (s *pluginHostServiceServer) RequestEmitToken(ctx context.Context, req *hostv1.RequestEmitTokenRequest) (*hostv1.RequestEmitTokenResponse, error) {
	return s.emit().RequestEmitToken(ctx, req)
}

// --- EvalService delegations ---

func (s *pluginHostServiceServer) Evaluate(ctx context.Context, req *hostv1.EvaluateRequest) (*hostv1.EvaluateResponse, error) {
	return s.eval().Evaluate(ctx, req)
}

// --- SettingsService delegations ---

func (s *pluginHostServiceServer) GetSetting(ctx context.Context, req *hostv1.GetSettingRequest) (*hostv1.GetSettingResponse, error) {
	return s.settings().GetSetting(ctx, req)
}

func (s *pluginHostServiceServer) SetSetting(ctx context.Context, req *hostv1.SetSettingRequest) (*hostv1.SetSettingResponse, error) {
	return s.settings().SetSetting(ctx, req)
}

// --- StreamHistoryService delegations ---

func (s *pluginHostServiceServer) QueryStreamHistory(ctx context.Context, req *hostv1.QueryStreamHistoryRequest) (*hostv1.QueryStreamHistoryResponse, error) {
	return s.streamHistory().QueryStreamHistory(ctx, req)
}

// --- AuditService delegations ---

func (s *pluginHostServiceServer) DecryptOwnAuditRows(ctx context.Context, req *hostv1.DecryptOwnAuditRowsRequest) (*hostv1.DecryptOwnAuditRowsResponse, error) {
	return s.audit().DecryptOwnAuditRows(ctx, req)
}

// --- CommandRegistryService delegations ---

func (s *pluginHostServiceServer) ListCommands(ctx context.Context, req *hostv1.ListCommandsRequest) (*hostv1.ListCommandsResponse, error) {
	return s.commands().ListCommands(ctx, req)
}

func (s *pluginHostServiceServer) GetCommandHelp(ctx context.Context, req *hostv1.GetCommandHelpRequest) (*hostv1.GetCommandHelpResponse, error) {
	return s.commands().GetCommandHelp(ctx, req)
}
