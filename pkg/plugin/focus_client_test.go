// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
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

func TestFocusClient_InterfaceShape(_ *testing.T) {
	var _ FocusClient = (*pluginHostFocusClient)(nil)
}

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
	wantEvt := &pluginv1.Event{Id: "01EVT", Stream: "scene:1:ic", Type: "say", Payload: `{"m":"hi"}`}
	srv := &focusTestServer{historyResp: &pluginv1.PluginHostServiceQueryStreamHistoryResponse{Events: []*pluginv1.Event{wantEvt}}}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostFocusClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	events, err := client.QueryStreamHistory(context.Background(), QueryStreamHistoryRequest{
		Stream: "scene:1:ic",
		Count:  10,
	})
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "01EVT", events[0].ID)
	assert.Equal(t, "scene:1:ic", events[0].Stream)
	assert.Equal(t, EventType("say"), events[0].Type)
	assert.Equal(t, `{"m":"hi"}`, events[0].Payload)
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
	mu          sync.Mutex
	joinReqs    []*pluginv1.PluginHostServiceJoinFocusRequest
	leaveReqs   []*pluginv1.PluginHostServiceLeaveFocusRequest
	presentReqs []*pluginv1.PluginHostServicePresentFocusRequest
	historyReqs []*pluginv1.PluginHostServiceQueryStreamHistoryRequest

	joinErr     error
	leaveErr    error
	presentErr  error
	historyResp *pluginv1.PluginHostServiceQueryStreamHistoryResponse
	historyErr  error
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
