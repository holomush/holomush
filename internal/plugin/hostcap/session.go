// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"

	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/pkg/errutil"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// sessionServer implements holomush.plugin.host.v1.SessionService. It reads
// and updates sessions through the runtime-neutral HostCapabilities port,
// mirroring the existing Lua session.find_by_name / session.list_active /
// session.set_last_whispered host functions so both runtimes share identical
// semantics (INV-PLUGIN-49).
type sessionServer struct {
	hostv1.UnimplementedSessionServiceServer
	hostCapabilityBase
}

// NewSessionServer builds the SessionService capability server bound to base.
// Returned as the narrow service interface so callers cannot reach into the struct.
func NewSessionServer(base hostCapabilityBase) hostv1.SessionServiceServer {
	return &sessionServer{hostCapabilityBase: base}
}

// FindByName returns the active session for the named character, mirroring the
// Lua session.find_by_name host function. The session field is absent in the
// response when no active session matches the name. Inner errors never leak to
// the caller (grpc-errors.md).
func (s *sessionServer) FindByName(ctx context.Context, req *hostv1.FindByNameRequest) (*hostv1.FindByNameResponse, error) {
	access := s.host.SessionAccess()
	if access == nil {
		return nil, status.Errorf(codes.Unimplemented, "session service not supported")
	}
	info, err := access.FindByCharacterName(ctx, req.GetName())
	if err != nil {
		errutil.LogErrorContext(ctx, "session.find_by_name failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	if info == nil {
		// Not found → empty response (absent session field), mirroring the Lua
		// nil-return semantics from cap_session.go findByNameFn.
		return &hostv1.FindByNameResponse{}, nil
	}
	return &hostv1.FindByNameResponse{Session: sessionInfoToProto(info)}, nil
}

// ListActive returns every currently active session, mirroring the Lua
// session.list_active host function. Inner errors never leak to the caller.
func (s *sessionServer) ListActive(ctx context.Context, _ *hostv1.ListActiveRequest) (*hostv1.ListActiveResponse, error) {
	access := s.host.SessionAccess()
	if access == nil {
		return nil, status.Errorf(codes.Unimplemented, "session service not supported")
	}
	infos, err := access.ListActive(ctx)
	if err != nil {
		errutil.LogErrorContext(ctx, "session.list_active failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	out := make([]*hostv1.SessionInfo, 0, len(infos))
	for _, i := range infos {
		out = append(out, sessionInfoToProto(i))
	}
	return &hostv1.ListActiveResponse{Sessions: out}, nil
}

// SetLastWhispered records the last whisper target on a session, mirroring the
// Lua session.set_last_whispered host function. Inner errors never leak to the
// caller.
func (s *sessionServer) SetLastWhispered(ctx context.Context, req *hostv1.SetLastWhisperedRequest) (*hostv1.SetLastWhisperedResponse, error) {
	access := s.host.SessionAccess()
	if access == nil {
		return nil, status.Errorf(codes.Unimplemented, "session service not supported")
	}
	if err := access.UpdateLastWhispered(ctx, req.GetSessionId(), req.GetName()); err != nil {
		errutil.LogErrorContext(ctx, "session.set_last_whispered failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	return &hostv1.SetLastWhisperedResponse{}, nil
}

// sessionAdminServer implements holomush.plugin.host.v1.SessionAdminService.
// It delegates to the SessionAdmin port (broadcast/disconnect), mirroring the
// existing Lua session.broadcast / session.disconnect host functions so both
// runtimes share identical semantics (INV-PLUGIN-49). The binary adapter has
// no consumer for this service; it is registered only in the Lua capability set.
type sessionAdminServer struct {
	hostv1.UnimplementedSessionAdminServiceServer
	hostCapabilityBase
}

// NewSessionAdminServer builds the SessionAdminService capability server bound
// to base. Returned as the narrow service interface so callers cannot reach
// into the struct.
func NewSessionAdminServer(base hostCapabilityBase) hostv1.SessionAdminServiceServer {
	return &sessionAdminServer{hostCapabilityBase: base}
}

// Broadcast sends a system message to all active sessions, mirroring the Lua
// session.broadcast host function. Inner errors never leak to the caller.
//
// SessionAdmin() may be nil: the binary adapter has no consumer and returns nil,
// and the Lua adapter returns nil until a broadcast/disconnect backing is wired
// (holomush-eykuh.4.2). A nil port fails closed with Unimplemented rather than
// nil-dereferencing — protecting BOTH runtimes (mirrors settingsServer's
// fail-closed-on-nil pattern in servers.go).
func (s *sessionAdminServer) Broadcast(ctx context.Context, req *hostv1.BroadcastRequest) (*hostv1.BroadcastResponse, error) {
	admin := s.host.SessionAdmin()
	if admin == nil {
		return nil, status.Errorf(codes.Unimplemented, "session admin not supported")
	}
	if err := admin.BroadcastSystemMessage(ctx, req.GetMessage()); err != nil {
		errutil.LogErrorContext(ctx, "session.broadcast failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	return &hostv1.BroadcastResponse{}, nil
}

// Disconnect forcibly disconnects a session with a reason, mirroring the Lua
// session.disconnect host function. Inner errors never leak to the caller.
//
// As with Broadcast, a nil SessionAdmin() port fails closed with Unimplemented
// (protects both runtimes; see Broadcast for the nil-port rationale).
func (s *sessionAdminServer) Disconnect(ctx context.Context, req *hostv1.DisconnectRequest) (*hostv1.DisconnectResponse, error) {
	admin := s.host.SessionAdmin()
	if admin == nil {
		return nil, status.Errorf(codes.Unimplemented, "session admin not supported")
	}
	if err := admin.DisconnectSession(ctx, req.GetSessionId(), req.GetReason()); err != nil {
		errutil.LogErrorContext(ctx, "session.disconnect failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	return &hostv1.DisconnectResponse{}, nil
}

// sessionInfoToProto maps the host session.Info to the wire SessionInfo.
// LocationID is a ulid.ULID value type; the zero value means "no location",
// which maps to an empty string on the wire (proto3 default), matching the Lua
// shim's hostfunc.SessionInfo.LocationID empty-string convention.
func sessionInfoToProto(i *session.Info) *hostv1.SessionInfo {
	return &hostv1.SessionInfo{
		Id:            i.ID,
		CharacterId:   i.CharacterID.String(),
		CharacterName: i.CharacterName,
		LocationId:    locationIDString(i),
		GridPresent:   i.GridPresent,
		LastWhispered: i.LastWhispered,
	}
}

// locationIDString returns the session's LocationID as a string, or an empty
// string when the LocationID is the zero ULID (no location assigned). This
// matches the Lua hostfunc.SessionInfo.LocationID empty-string convention.
func locationIDString(i *session.Info) string {
	if i.LocationID == (ulid.ULID{}) {
		return ""
	}
	return i.LocationID.String()
}
