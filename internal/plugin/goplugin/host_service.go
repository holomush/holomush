// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"math"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus/cursor"
	"github.com/holomush/holomush/internal/session"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// newPluginHostServiceServer builds the single broker *grpc.Server a binary
// plugin dials back into. Every host-brokered capability is served as a
// capability-scoped holomush.plugin.host.v1 service on this one server, reached
// through the single broker handshake (pkg/plugin.PluginHostServiceName names
// the broker channel, not a gRPC service). The former monolithic
// holomush.plugin.v1.PluginHostService is gone (holomush-eykuh.1, Task 12): its
// authoritative handler bodies now live on the per-capability servers in
// host_capability_servers.go, one capability domain per service.
//
// World/Property/Session have no binary consumer in this sub-spec and are
// deliberately NOT registered here.
func newPluginHostServiceServer(host *Host, pluginName string) func([]grpc.ServerOption) *grpc.Server {
	return func(opts []grpc.ServerOption) *grpc.Server {
		server := grpc.NewServer(opts...)
		base := hostCapabilityBase{host: host, pluginName: pluginName}
		hostv1.RegisterFocusServiceServer(server, &focusServer{hostCapabilityBase: base})
		hostv1.RegisterEmitServiceServer(server, &emitServer{hostCapabilityBase: base})
		hostv1.RegisterEvalServiceServer(server, &evalServer{hostCapabilityBase: base})
		hostv1.RegisterSettingsServiceServer(server, &settingsServer{hostCapabilityBase: base})
		hostv1.RegisterStreamHistoryServiceServer(server, &streamHistoryServer{hostCapabilityBase: base})
		hostv1.RegisterStreamSubscriptionServiceServer(server, &streamSubscriptionServer{hostCapabilityBase: base})
		hostv1.RegisterAuditServiceServer(server, &auditServer{hostCapabilityBase: base})
		hostv1.RegisterCommandRegistryServiceServer(server, &commandRegistryServer{hostCapabilityBase: base})
		hostv1.RegisterKVServiceServer(server, &kvServer{hostCapabilityBase: base})
		return server
	}
}

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
