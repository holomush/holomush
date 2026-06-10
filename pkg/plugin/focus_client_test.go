// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// --- compile-time interface checks ---

var _ FocusClient = (*pluginHostFocusClient)(nil)

// --- error mapping ---

func TestPluginHostFocusClient_JoinFocusMapsSessionNotFound(t *testing.T) {
	srv := &focusTestServer{joinErr: status.Error(codes.NotFound, "SESSION_NOT_FOUND: missing")}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	err := client.JoinFocus(context.Background(), "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.Error(t, err)
	var oe oops.OopsError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, "SESSION_NOT_FOUND", oe.Code())
}

func TestPluginHostFocusClient_JoinFocusMapsAlreadyMember(t *testing.T) {
	srv := &focusTestServer{joinErr: status.Error(codes.AlreadyExists, "FOCUS_ALREADY_MEMBER: duplicate")}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	err := client.JoinFocus(context.Background(), "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.Error(t, err)
	var oe oops.OopsError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, "FOCUS_ALREADY_MEMBER", oe.Code())
}

func TestPluginHostFocusClient_PresentFocusMapsNotMember(t *testing.T) {
	srv := &focusTestServer{presentErr: status.Error(codes.NotFound, "FOCUS_NOT_MEMBER: not joined")}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	err := client.PresentFocus(context.Background(), "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.Error(t, err)
	var oe oops.OopsError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, "FOCUS_NOT_MEMBER", oe.Code())
}

func TestPluginHostFocusClient_JoinFocusHappyPath(t *testing.T) {
	srv := &focusTestServer{}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	err := client.JoinFocus(context.Background(), "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.NoError(t, err)
	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Len(t, srv.joinReqs, 1)
	assert.Equal(t, "sess-1", srv.joinReqs[0].GetSessionId())
	assert.Equal(t, "scene-1", srv.joinReqs[0].GetTarget().GetTargetId())
	assert.Equal(t, pluginv1.FocusKind_FOCUS_KIND_SCENE, srv.joinReqs[0].GetTarget().GetKind())
}

func TestPluginHostFocusClient_LeaveFocusHappyPath(t *testing.T) {
	srv := &focusTestServer{}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	err := client.LeaveFocus(context.Background(), "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.NoError(t, err)
	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Len(t, srv.leaveReqs, 1)
	assert.Equal(t, "sess-1", srv.leaveReqs[0].GetSessionId())
}

func TestPluginHostFocusClient_LeaveFocusByTargetMapsResponseToResult(t *testing.T) {
	srv := &focusTestServer{leaveByTargetResp: &pluginv1.PluginHostServiceLeaveFocusByTargetResponse{
		Succeeded:        4,
		TotalScanned:     5,
		FailedSessionIds: []string{"sess-bad"},
	}}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	result, err := client.LeaveFocusByTarget(context.Background(), FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.NoError(t, err)
	assert.Equal(t, 4, result.Succeeded)
	assert.Equal(t, 5, result.TotalScanned)
	require.Len(t, result.Failed, 1)
	assert.Equal(t, "sess-bad", result.Failed[0].SessionID)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Len(t, srv.leaveByTargetReqs, 1)
	assert.Equal(t, "scene-1", srv.leaveByTargetReqs[0].GetTarget().GetTargetId())
	assert.Equal(t, pluginv1.FocusKind_FOCUS_KIND_SCENE, srv.leaveByTargetReqs[0].GetTarget().GetKind())
}

func TestPluginHostFocusClient_LeaveFocusByTargetReturnsZeroResultOnEnumerationError(t *testing.T) {
	srv := &focusTestServer{leaveByTargetErr: status.Error(codes.Internal, "FOCUS_SWEEP_LIST_FAILED: store down")}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	result, err := client.LeaveFocusByTarget(context.Background(), FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.Error(t, err)
	assert.Equal(t, LeaveByTargetResult{}, result, "zero result on enumeration error preserves Go err!=nil contract")
}

func TestPluginHostFocusClient_LeaveFocusByTargetNilClientReturnsError(t *testing.T) {
	client := &pluginHostFocusClient{}
	result, err := client.LeaveFocusByTarget(context.Background(), FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.Error(t, err)
	assert.Equal(t, LeaveByTargetResult{}, result)
	assert.Contains(t, err.Error(), "not configured")
}

func TestPluginHostFocusClient_PresentFocusHappyPath(t *testing.T) {
	srv := &focusTestServer{}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	err := client.PresentFocus(context.Background(), "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.NoError(t, err)
	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Len(t, srv.presentReqs, 1)
}

func TestPluginHostFocusClient_QueryStreamHistoryHappyPath(t *testing.T) {
	wantCursor := []byte("plugin-evt-cursor")
	wantNextCursor := []byte("plugin-next-cursor")
	wantEvt := &pluginv1.Event{Id: "01EVT", Stream: "scene:1:ic", Type: "say", Payload: `{"m":"hi"}`, Cursor: wantCursor}
	srv := &focusTestServer{historyResp: &pluginv1.PluginHostServiceQueryStreamHistoryResponse{
		Events:     []*pluginv1.Event{wantEvt},
		NextCursor: wantNextCursor,
	}}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	resp, err := client.QueryStreamHistory(context.Background(), QueryStreamHistoryRequest{
		Stream: "scene:1:ic",
		Count:  10,
	})
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	assert.Equal(t, "01EVT", resp.Events[0].ID)
	assert.Equal(t, "scene:1:ic", resp.Events[0].Stream)
	assert.Equal(t, EventType("say"), resp.Events[0].Type)
	assert.Equal(t, `{"m":"hi"}`, resp.Events[0].Payload)
	assert.Equal(t, wantCursor, resp.Events[0].Cursor, "per-event cursor must be propagated from proto response")
	assert.Equal(t, wantNextCursor, resp.NextCursor, "next_cursor must be propagated from proto response")
}

func TestPluginHostFocusClient_NilClientReturnsError(t *testing.T) {
	client := &pluginHostFocusClient{}
	err := client.JoinFocus(context.Background(), "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestNewFocusClientFromBroker_MissingBroker(t *testing.T) {
	c, err := newFocusClientFromBroker(nil, map[string]string{PluginHostServiceName: "broker:7"})
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "broker is not configured")
}

func TestNewFocusClientFromBroker_MissingHostService(t *testing.T) {
	c, err := newFocusClientFromBroker(&testBrokerDialer{}, map[string]string{})
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), PluginHostServiceName)
}

// --- Group A: nil-client guards for LeaveFocus, PresentFocus, QueryStreamHistory ---

func TestPluginHostFocusClient_LeaveFocusNilClientReturnsError(t *testing.T) {
	client := &pluginHostFocusClient{}
	err := client.LeaveFocus(context.Background(), "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestPluginHostFocusClient_PresentFocusNilClientReturnsError(t *testing.T) {
	client := &pluginHostFocusClient{}
	err := client.PresentFocus(context.Background(), "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestPluginHostFocusClient_QueryStreamHistoryNilClientReturnsError(t *testing.T) {
	client := &pluginHostFocusClient{}
	resp, err := client.QueryStreamHistory(context.Background(), QueryStreamHistoryRequest{Stream: "scene:1:ic", Count: 5})
	require.Error(t, err)
	assert.Empty(t, resp.Events)
	assert.Contains(t, err.Error(), "not configured")
}

// --- Group A: QueryStreamHistory count clamping ---

func TestPluginHostFocusClient_QueryStreamHistoryClampsNegativeCount(t *testing.T) {
	srv := &focusTestServer{}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	_, err := client.QueryStreamHistory(context.Background(), QueryStreamHistoryRequest{
		Stream: "scene:1:ic",
		Count:  -5,
	})
	require.NoError(t, err)
	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Len(t, srv.historyReqs, 1)
	assert.Equal(t, int32(0), srv.historyReqs[0].GetCount())
}

func TestPluginHostFocusClient_QueryStreamHistoryClampsOverflow(t *testing.T) {
	srv := &focusTestServer{}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	_, err := client.QueryStreamHistory(context.Background(), QueryStreamHistoryRequest{
		Stream: "scene:1:ic",
		Count:  math.MaxInt,
	})
	require.NoError(t, err)
	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Len(t, srv.historyReqs, 1)
	assert.Equal(t, int32(1<<30), srv.historyReqs[0].GetCount())
}

func TestPluginHostFocusClient_QueryStreamHistoryPropagatesHostError(t *testing.T) {
	srv := &focusTestServer{historyErr: status.Error(codes.Internal, "storage failure")}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	resp, err := client.QueryStreamHistory(context.Background(), QueryStreamHistoryRequest{
		Stream: "scene:1:ic",
		Count:  5,
	})
	require.Error(t, err)
	assert.Empty(t, resp.Events)
	// The stream name is attached as oops context, not embedded in the message string.
	var oe oops.OopsError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, "scene:1:ic", oe.Context()["stream"])
}

// --- Group A: wrapFocusError unit tests ---

func TestWrapFocusErrorNilErrorReturnsNil(t *testing.T) {
	err := wrapFocusError(nil, "JoinFocus", "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	assert.NoError(t, err)
}

func TestWrapFocusErrorNonStatusErrorPassesThrough(t *testing.T) {
	plain := errors.New("boom")
	err := wrapFocusError(plain, "JoinFocus", "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.Error(t, err)
	var oe oops.OopsError
	require.ErrorAs(t, err, &oe)
	// Non-status errors get context wrapping but no oops code (Code() returns nil).
	assert.Nil(t, oe.Code())
}

// --- Group A: codeFromStatus gRPC-code fallbacks ---

func TestCodeFromStatus_AllGRPCCodeFallbacks(t *testing.T) {
	tests := []struct {
		name         string
		grpcCode     codes.Code
		message      string
		expectedCode string
	}{
		{
			name:         "NotFound without prefix falls back to SESSION_NOT_FOUND",
			grpcCode:     codes.NotFound,
			message:      "generic not found",
			expectedCode: "SESSION_NOT_FOUND",
		},
		{
			name:         "AlreadyExists without prefix falls back to FOCUS_ALREADY_MEMBER",
			grpcCode:     codes.AlreadyExists,
			message:      "generic already exists",
			expectedCode: "FOCUS_ALREADY_MEMBER",
		},
		{
			name:         "FailedPrecondition without prefix falls back to SESSION_EXPIRED",
			grpcCode:     codes.FailedPrecondition,
			message:      "generic precondition",
			expectedCode: "SESSION_EXPIRED",
		},
		{
			name:         "InvalidArgument without prefix falls back to FOCUS_KIND_UNREGISTERED",
			grpcCode:     codes.InvalidArgument,
			message:      "generic invalid",
			expectedCode: "FOCUS_KIND_UNREGISTERED",
		},
		{
			name:         "Internal without prefix falls back to FOCUS_POLICY_FAILED",
			grpcCode:     codes.Internal,
			message:      "generic internal",
			expectedCode: "FOCUS_POLICY_FAILED",
		},
		{
			name:         "Unavailable without prefix returns empty code",
			grpcCode:     codes.Unavailable,
			message:      "generic unavailable",
			expectedCode: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := status.New(tt.grpcCode, tt.message)
			got := codeFromStatus(st)
			assert.Equal(t, tt.expectedCode, got)
		})
	}
}

// --- Group A: JoinFocus gRPC-code mappings (FailedPrecondition, InvalidArgument, Internal) ---

func TestPluginHostFocusClient_JoinFocusMapsSessionExpired(t *testing.T) {
	srv := &focusTestServer{joinErr: status.Error(codes.FailedPrecondition, "SESSION_EXPIRED: ttl elapsed")}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	err := client.JoinFocus(context.Background(), "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.Error(t, err)
	var oe oops.OopsError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, "SESSION_EXPIRED", oe.Code())
}

func TestPluginHostFocusClient_JoinFocusMapsFocusKindUnregistered(t *testing.T) {
	srv := &focusTestServer{joinErr: status.Error(codes.InvalidArgument, "FOCUS_KIND_UNREGISTERED: no policy")}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	err := client.JoinFocus(context.Background(), "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.Error(t, err)
	var oe oops.OopsError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, "FOCUS_KIND_UNREGISTERED", oe.Code())
}

func TestPluginHostFocusClient_JoinFocusMapsFocusPolicyFailed(t *testing.T) {
	srv := &focusTestServer{joinErr: status.Error(codes.Internal, "FOCUS_POLICY_FAILED: rejected")}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	err := client.JoinFocus(context.Background(), "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
	require.Error(t, err)
	var oe oops.OopsError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, "FOCUS_POLICY_FAILED", oe.Code())
}

// --- Group A: toProtoFocusKind unknown kind ---

func TestToProtoFocusKindUnknownReturnsUnspecified(t *testing.T) {
	// Exercise toProtoFocusKind indirectly: pass an unknown FocusKind to JoinFocus
	// and verify the server received FOCUS_KIND_UNSPECIFIED.
	srv := &focusTestServer{}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	_ = client.JoinFocus(context.Background(), "sess-1", FocusKey{Kind: FocusKind("nonexistent"), TargetID: "target-1"})

	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Len(t, srv.joinReqs, 1)
	assert.Equal(t, pluginv1.FocusKind_FOCUS_KIND_UNSPECIFIED, srv.joinReqs[0].GetTarget().GetKind())
}

// --- Group B: newFocusClientFromBroker dial failure ---

func TestNewFocusClientFromBroker_DialFailureWrapsError(t *testing.T) {
	// testBrokerDialer has no entry for the requested broker ID; dial will fail.
	c, err := newFocusClientFromBroker(&testBrokerDialer{}, map[string]string{
		PluginHostServiceName: "broker:99",
	})
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "unknown broker id")
}

func TestQueryStreamHistoryRequestNotBeforeIsPassedThrough(t *testing.T) {
	srv := &focusTestServer{}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	notBefore := time.UnixMilli(1_700_000_000_000)
	_, err := client.QueryStreamHistory(context.Background(), QueryStreamHistoryRequest{
		Stream:    "scene:1:ic",
		Count:     5,
		NotBefore: notBefore,
	})
	require.NoError(t, err)
	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Len(t, srv.historyReqs, 1)
	assert.Equal(t, int64(1_700_000_000_000), srv.historyReqs[0].GetNotBeforeMs())
}

// --- test double: PluginHostService with per-RPC hooks ---

type focusTestServer struct {
	pluginv1.UnimplementedPluginHostServiceServer
	mu                sync.Mutex
	joinReqs          []*pluginv1.PluginHostServiceJoinFocusRequest
	leaveReqs         []*pluginv1.PluginHostServiceLeaveFocusRequest
	leaveByTargetReqs []*pluginv1.PluginHostServiceLeaveFocusByTargetRequest
	presentReqs       []*pluginv1.PluginHostServicePresentFocusRequest
	historyReqs       []*pluginv1.PluginHostServiceQueryStreamHistoryRequest
	setConnFocusReqs  []*pluginv1.PluginHostServiceSetConnectionFocusRequest

	joinErr           error
	leaveErr          error
	leaveByTargetErr  error
	leaveByTargetResp *pluginv1.PluginHostServiceLeaveFocusByTargetResponse
	presentErr        error
	historyResp       *pluginv1.PluginHostServiceQueryStreamHistoryResponse
	historyErr        error
	setConnFocusErr   error
}

func (s *focusTestServer) JoinFocus(_ context.Context, req *pluginv1.PluginHostServiceJoinFocusRequest) (*pluginv1.PluginHostServiceJoinFocusResponse, error) {
	s.mu.Lock()
	s.joinReqs = append(s.joinReqs, req)
	s.mu.Unlock()
	if s.joinErr != nil {
		return nil, s.joinErr
	}
	return &pluginv1.PluginHostServiceJoinFocusResponse{}, nil
}

func (s *focusTestServer) LeaveFocus(_ context.Context, req *pluginv1.PluginHostServiceLeaveFocusRequest) (*pluginv1.PluginHostServiceLeaveFocusResponse, error) {
	s.mu.Lock()
	s.leaveReqs = append(s.leaveReqs, req)
	s.mu.Unlock()
	if s.leaveErr != nil {
		return nil, s.leaveErr
	}
	return &pluginv1.PluginHostServiceLeaveFocusResponse{}, nil
}

func (s *focusTestServer) LeaveFocusByTarget(_ context.Context, req *pluginv1.PluginHostServiceLeaveFocusByTargetRequest) (*pluginv1.PluginHostServiceLeaveFocusByTargetResponse, error) {
	s.mu.Lock()
	s.leaveByTargetReqs = append(s.leaveByTargetReqs, req)
	s.mu.Unlock()
	if s.leaveByTargetErr != nil {
		return nil, s.leaveByTargetErr
	}
	if s.leaveByTargetResp != nil {
		return s.leaveByTargetResp, nil
	}
	return &pluginv1.PluginHostServiceLeaveFocusByTargetResponse{}, nil
}

func (s *focusTestServer) PresentFocus(_ context.Context, req *pluginv1.PluginHostServicePresentFocusRequest) (*pluginv1.PluginHostServicePresentFocusResponse, error) {
	s.mu.Lock()
	s.presentReqs = append(s.presentReqs, req)
	s.mu.Unlock()
	if s.presentErr != nil {
		return nil, s.presentErr
	}
	return &pluginv1.PluginHostServicePresentFocusResponse{}, nil
}

func (s *focusTestServer) QueryStreamHistory(_ context.Context, req *pluginv1.PluginHostServiceQueryStreamHistoryRequest) (*pluginv1.PluginHostServiceQueryStreamHistoryResponse, error) {
	s.mu.Lock()
	s.historyReqs = append(s.historyReqs, req)
	s.mu.Unlock()
	if s.historyErr != nil {
		return nil, s.historyErr
	}
	if s.historyResp != nil {
		return s.historyResp, nil
	}
	return &pluginv1.PluginHostServiceQueryStreamHistoryResponse{}, nil
}

func (s *focusTestServer) SetConnectionFocus(_ context.Context, req *pluginv1.PluginHostServiceSetConnectionFocusRequest) (*pluginv1.PluginHostServiceSetConnectionFocusResponse, error) {
	s.mu.Lock()
	s.setConnFocusReqs = append(s.setConnFocusReqs, req)
	s.mu.Unlock()
	if s.setConnFocusErr != nil {
		return nil, s.setConnFocusErr
	}
	return &pluginv1.PluginHostServiceSetConnectionFocusResponse{}, nil
}

// --- SetConnectionFocus wire-level error mapping tests ---

// TestSetConnectionFocus_PreservesFocusWithoutMembershipCode asserts that when
// the gRPC server returns a status.Error whose message starts with
// "FOCUS_WITHOUT_MEMBERSHIP" (the oops code that crossed the wire), the client
// re-emits an OopsError with that exact code — so the plugin consumer's
// `oe.Code() == "FOCUS_WITHOUT_MEMBERSHIP"` branch is reachable in production.
func TestSetConnectionFocus_PreservesFocusWithoutMembershipCode(t *testing.T) {
	srv := &focusTestServer{
		setConnFocusErr: status.Errorf(codes.Unknown, "FOCUS_WITHOUT_MEMBERSHIP: focus target not in session FocusMemberships"),
	}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	// Use a valid 16-byte ULID so the client doesn't fail on ULID parse before hitting gRPC.
	validConnID := "01HNSMF4QK8XP2000000000000"
	fk := FocusKey{Kind: FocusKindScene, TargetID: "scene-1"}
	err := client.SetConnectionFocus(context.Background(), validConnID, &fk, false /* isSceneGrid */)

	require.Error(t, err)
	var oe oops.OopsError
	require.ErrorAs(t, err, &oe, "error must be an OopsError")
	assert.Equal(t, "FOCUS_WITHOUT_MEMBERSHIP", oe.Code(),
		"FOCUS_WITHOUT_MEMBERSHIP must survive the gRPC wire round-trip so handleSceneFocus can branch on it")
}

// TestSetConnectionFocus_NonGridUsesSceneFocusErrCode asserts that a generic
// error on a non-grid (isSceneGrid=false) SetConnectionFocus call is wrapped
// with SCENE_FOCUS_SET_FAILED, not SCENE_GRID_SET_FAILED — telemetry accuracy.
func TestSetConnectionFocus_NonGridUsesSceneFocusErrCode(t *testing.T) {
	srv := &focusTestServer{
		setConnFocusErr: status.Errorf(codes.Internal, "storage failure"),
	}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	validConnID := "01HNSMF4QK8XP2000000000000"
	err := client.SetConnectionFocus(context.Background(), validConnID, nil, false /* isSceneGrid */)

	require.Error(t, err)
	var oe oops.OopsError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, "SCENE_FOCUS_SET_FAILED", oe.Code(),
		"non-grid errors must use SCENE_FOCUS_SET_FAILED, not SCENE_GRID_SET_FAILED")
}

// TestSetConnectionFocus_GridUsesSceneGridErrCode asserts that a generic error
// on a grid (isSceneGrid=true) SetConnectionFocus call is wrapped with
// SCENE_GRID_SET_FAILED — preserving the original error code for grid paths.
func TestSetConnectionFocus_GridUsesSceneGridErrCode(t *testing.T) {
	srv := &focusTestServer{
		setConnFocusErr: status.Errorf(codes.Internal, "storage failure"),
	}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	validConnID := "01HNSMF4QK8XP2000000000000"
	err := client.SetConnectionFocus(context.Background(), validConnID, nil, true /* isSceneGrid */)

	require.Error(t, err)
	var oe oops.OopsError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, "SCENE_GRID_SET_FAILED", oe.Code(),
		"grid errors must use SCENE_GRID_SET_FAILED")
}
