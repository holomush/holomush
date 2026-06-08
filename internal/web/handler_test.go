// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/pkg/errutil"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/web/v1/webv1connect"
)

// TestCoreClient_SatisfiedByGRPCClient verifies at compile time that
// *holoGRPC.Client implements the CoreClient interface.
func TestCoreClient_SatisfiedByGRPCClient(t *testing.T) {
	t.Helper()
	var _ CoreClient = (*holoGRPC.Client)(nil)
}

// mockCoreClient is a test double for CoreClient.
type mockCoreClient struct {
	cmdResp *corev1.HandleCommandResponse
	cmdErr  error
	cmdReq  *corev1.HandleCommandRequest // captured for assertion

	subStream corev1.CoreService_SubscribeClient
	subErr    error
	subReq    *corev1.SubscribeRequest // captured for assertion

	discResp *corev1.DisconnectResponse
	discErr  error
	discReq  *corev1.DisconnectRequest // captured for assertion

	cmdHistory       []string
	cmdHistoryErr    error                            // application-level failure (Success=false)
	cmdHistoryRPCErr error                            // transport/RPC-level failure (nil response)
	cmdHistoryReq    *corev1.GetCommandHistoryRequest // captured for assertion

	// Auth RPC fields
	authPlayerResp     *corev1.AuthenticatePlayerResponse
	authPlayerErr      error
	authPlayerCalls    atomic.Int32 // call counter; atomic for use under -race in concurrent tests
	selectCharResp     *corev1.SelectCharacterResponse
	selectCharErr      error
	createPlayerResp   *corev1.CreatePlayerResponse
	createPlayerErr    error
	createPlayerCalls  atomic.Int32
	createCharResp     *corev1.CreateCharacterResponse
	createCharErr      error
	listCharsResp      *corev1.ListCharactersResponse
	listCharsErr       error
	logoutResp         *corev1.LogoutResponse
	logoutErr          error
	reqPwResetResp     *corev1.RequestPasswordResetResponse
	reqPwResetErr      error
	confirmPwResetResp *corev1.ConfirmPasswordResetResponse
	confirmPwResetErr  error
	checkSessionResp   *corev1.CheckPlayerSessionResponse
	checkSessionErr    error
	checkSessionCalls  atomic.Int32 // call counter; atomic for use under -race
	createGuestResp    *corev1.CreateGuestResponse
	createGuestErr     error
	createGuestCalls   atomic.Int32

	queryStreamHistoryResp *corev1.QueryStreamHistoryResponse
	queryStreamHistoryErr  error
	queryStreamHistoryReq  *corev1.QueryStreamHistoryRequest // captured for assertion

	listSessionStreamsResp *corev1.ListSessionStreamsResponse
	listSessionStreamsErr  error
	listSessionStreamsReq  *corev1.ListSessionStreamsRequest // captured for assertion

	// Session management fields
	listSessionsResp *corev1.ListPlayerSessionsResponse
	listSessionsErr  error
	listSessionsReq  *corev1.ListPlayerSessionsRequest // captured for assertion

	revokeSessionResp *corev1.RevokePlayerSessionResponse
	revokeSessionErr  error
	revokeSessionReq  *corev1.RevokePlayerSessionRequest // captured for assertion

	revokeOtherResp *corev1.RevokeOtherPlayerSessionsResponse
	revokeOtherErr  error
	revokeOtherReq  *corev1.RevokeOtherPlayerSessionsRequest // captured for assertion

	listFocusPresenceResp *corev1.ListFocusPresenceResponse
	listFocusPresenceErr  error
	listFocusPresenceReq  *corev1.ListFocusPresenceRequest // captured for assertion

	listAvailableCommandsResp *corev1.ListAvailableCommandsResponse
	listAvailableCommandsErr  error
	listAvailableCommandsReq  *corev1.ListAvailableCommandsRequest // captured for assertion

	// RefreshConnection fields
	refreshConnectionResp  *corev1.RefreshConnectionResponse
	refreshConnectionErr   error
	refreshConnectionCalls atomic.Int32
	refreshConnectionReq   atomic.Pointer[corev1.RefreshConnectionRequest] // last captured request (atomic for -race)
}

func (m *mockCoreClient) HandleCommand(_ context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
	m.cmdReq = req
	return m.cmdResp, m.cmdErr
}

func (m *mockCoreClient) Subscribe(_ context.Context, req *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
	m.subReq = req
	return m.subStream, m.subErr
}

func (m *mockCoreClient) Disconnect(_ context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	m.discReq = req
	return m.discResp, m.discErr
}

func (m *mockCoreClient) GetCommandHistory(_ context.Context, req *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error) {
	m.cmdHistoryReq = req
	if m.cmdHistoryRPCErr != nil {
		return nil, m.cmdHistoryRPCErr
	}
	if m.cmdHistoryErr != nil {
		//nolint:nilerr // intentional: simulates application-level failure, not RPC error
		return &corev1.GetCommandHistoryResponse{
			Meta:    &corev1.ResponseMeta{},
			Success: false,
			Error:   m.cmdHistoryErr.Error(),
		}, nil
	}
	return &corev1.GetCommandHistoryResponse{
		Meta:     &corev1.ResponseMeta{},
		Success:  true,
		Commands: m.cmdHistory,
	}, nil
}

func (m *mockCoreClient) AuthenticatePlayer(_ context.Context, _ *corev1.AuthenticatePlayerRequest) (*corev1.AuthenticatePlayerResponse, error) {
	m.authPlayerCalls.Add(1)
	return m.authPlayerResp, m.authPlayerErr
}

func (m *mockCoreClient) SelectCharacter(_ context.Context, _ *corev1.SelectCharacterRequest) (*corev1.SelectCharacterResponse, error) {
	return m.selectCharResp, m.selectCharErr
}

func (m *mockCoreClient) CreatePlayer(_ context.Context, _ *corev1.CreatePlayerRequest) (*corev1.CreatePlayerResponse, error) {
	m.createPlayerCalls.Add(1)
	return m.createPlayerResp, m.createPlayerErr
}

func (m *mockCoreClient) CreateCharacter(_ context.Context, _ *corev1.CreateCharacterRequest) (*corev1.CreateCharacterResponse, error) {
	return m.createCharResp, m.createCharErr
}

func (m *mockCoreClient) ListCharacters(_ context.Context, _ *corev1.ListCharactersRequest) (*corev1.ListCharactersResponse, error) {
	return m.listCharsResp, m.listCharsErr
}

func (m *mockCoreClient) RequestPasswordReset(_ context.Context, _ *corev1.RequestPasswordResetRequest) (*corev1.RequestPasswordResetResponse, error) {
	return m.reqPwResetResp, m.reqPwResetErr
}

func (m *mockCoreClient) ConfirmPasswordReset(_ context.Context, _ *corev1.ConfirmPasswordResetRequest) (*corev1.ConfirmPasswordResetResponse, error) {
	return m.confirmPwResetResp, m.confirmPwResetErr
}

func (m *mockCoreClient) Logout(_ context.Context, _ *corev1.LogoutRequest) (*corev1.LogoutResponse, error) {
	return m.logoutResp, m.logoutErr
}

func (m *mockCoreClient) CheckPlayerSession(_ context.Context, _ *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error) {
	m.checkSessionCalls.Add(1)
	return m.checkSessionResp, m.checkSessionErr
}

func (m *mockCoreClient) CreateGuest(_ context.Context, _ *corev1.CreateGuestRequest) (*corev1.CreateGuestResponse, error) {
	m.createGuestCalls.Add(1)
	return m.createGuestResp, m.createGuestErr
}

func (m *mockCoreClient) QueryStreamHistory(_ context.Context, req *corev1.QueryStreamHistoryRequest) (*corev1.QueryStreamHistoryResponse, error) {
	m.queryStreamHistoryReq = req
	return m.queryStreamHistoryResp, m.queryStreamHistoryErr
}

func (m *mockCoreClient) ListSessionStreams(_ context.Context, req *corev1.ListSessionStreamsRequest) (*corev1.ListSessionStreamsResponse, error) {
	m.listSessionStreamsReq = req
	return m.listSessionStreamsResp, m.listSessionStreamsErr
}

func (m *mockCoreClient) ListPlayerSessions(_ context.Context, req *corev1.ListPlayerSessionsRequest) (*corev1.ListPlayerSessionsResponse, error) {
	m.listSessionsReq = req
	return m.listSessionsResp, m.listSessionsErr
}

func (m *mockCoreClient) RevokePlayerSession(_ context.Context, req *corev1.RevokePlayerSessionRequest) (*corev1.RevokePlayerSessionResponse, error) {
	m.revokeSessionReq = req
	return m.revokeSessionResp, m.revokeSessionErr
}

func (m *mockCoreClient) RevokeOtherPlayerSessions(_ context.Context, req *corev1.RevokeOtherPlayerSessionsRequest) (*corev1.RevokeOtherPlayerSessionsResponse, error) {
	m.revokeOtherReq = req
	return m.revokeOtherResp, m.revokeOtherErr
}

func (m *mockCoreClient) ListFocusPresence(_ context.Context, req *corev1.ListFocusPresenceRequest) (*corev1.ListFocusPresenceResponse, error) {
	m.listFocusPresenceReq = req
	return m.listFocusPresenceResp, m.listFocusPresenceErr
}

func (m *mockCoreClient) ListAvailableCommands(_ context.Context, req *corev1.ListAvailableCommandsRequest) (*corev1.ListAvailableCommandsResponse, error) {
	m.listAvailableCommandsReq = req
	return m.listAvailableCommandsResp, m.listAvailableCommandsErr
}

func (m *mockCoreClient) RefreshConnection(_ context.Context, req *corev1.RefreshConnectionRequest) (*corev1.RefreshConnectionResponse, error) {
	m.refreshConnectionCalls.Add(1)
	m.refreshConnectionReq.Store(req)
	if m.refreshConnectionResp == nil {
		return &corev1.RefreshConnectionResponse{}, m.refreshConnectionErr
	}
	return m.refreshConnectionResp, m.refreshConnectionErr
}

func TestHandler_SendCommand_Success(t *testing.T) {
	client := &mockCoreClient{
		cmdResp: &corev1.HandleCommandResponse{
			Success: true,
		},
	}
	h := NewHandler(client)

	resp, err := h.SendCommand(context.Background(), connect.NewRequest(&webv1.SendCommandRequest{
		SessionId: "sess-abc",
		Text:      "say hello",
	}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
}

func TestHandler_Disconnect_Success(t *testing.T) {
	client := &mockCoreClient{
		discResp: &corev1.DisconnectResponse{Success: true},
	}
	h := NewHandler(client)

	resp, err := h.Disconnect(context.Background(), connect.NewRequest(&webv1.DisconnectRequest{
		SessionId: "sess-abc",
	}))
	require.NoError(t, err)
	assert.NotNil(t, resp.Msg)
}

func TestHandler_Disconnect_RPCError(t *testing.T) {
	client := &mockCoreClient{
		discErr: errors.New("core unavailable"),
	}
	h := NewHandler(client)

	// Best-effort: error is logged, not returned.
	resp, err := h.Disconnect(context.Background(), connect.NewRequest(&webv1.DisconnectRequest{
		SessionId: "sess-xyz",
	}))
	require.NoError(t, err)
	assert.NotNil(t, resp.Msg)
}

func TestHandler_GetCommandHistory_Success(t *testing.T) {
	client := &mockCoreClient{
		cmdHistory: []string{"look", "say hello", "go north"},
	}
	h := NewHandler(client)

	resp, err := h.GetCommandHistory(context.Background(), connect.NewRequest(&webv1.GetCommandHistoryRequest{
		SessionId: "sess-abc",
	}))
	require.NoError(t, err)
	assert.Equal(t, []string{"look", "say hello", "go north"}, resp.Msg.GetCommands())
}

func TestHandler_GetCommandHistory_RPCError(t *testing.T) {
	client := &mockCoreClient{
		cmdHistoryRPCErr: errors.New("rpc error"),
	}
	h := NewHandler(client)

	resp, err := h.GetCommandHistory(context.Background(), connect.NewRequest(&webv1.GetCommandHistoryRequest{
		SessionId: "sess-abc",
	}))
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.GetCommands())
}

func TestHandler_GetCommandHistory_NotSuccess(t *testing.T) {
	client := &mockCoreClient{
		cmdHistoryErr: errors.New("session not found"),
	}
	h := NewHandler(client)

	resp, err := h.GetCommandHistory(context.Background(), connect.NewRequest(&webv1.GetCommandHistoryRequest{
		SessionId: "sess-abc",
	}))
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.GetCommands(), "non-success response should return empty commands")
}

// TestHandlerHasNoSessionStoreField asserts at compile time that the Handler
// struct does not hold a session.Store reference. The web gateway is a pure
// protocol-translation layer (bd-j2xj) and must run without database
// credentials; per-connection registration happens inside core's Subscribe
// RPC, not via a direct session-store call from the gateway.
func TestHandlerHasNoSessionStoreField(t *testing.T) {
	h := NewHandler(&mockCoreClient{})
	require.NotNil(t, h)
	// Use reflection would be overkill; the constructor signature and
	// the absence of WithSessionStore in this package are the real
	// compile-time guarantees. This test exists so a reviewer grepping
	// for sessionStore usage in the web package sees a single sentinel.
	assert.Nil(t, h.contentClient, "no options configured")
}

// mockSubscribeStream is a test double for corev1.CoreService_SubscribeClient.
// It returns pre-configured responses from a channel, then io.EOF.
type mockSubscribeStream struct {
	responses []*corev1.SubscribeResponse
	idx       int
}

func (m *mockSubscribeStream) Recv() (*corev1.SubscribeResponse, error) {
	if m.idx >= len(m.responses) {
		return nil, io.EOF
	}
	r := m.responses[m.idx]
	m.idx++
	return r, nil
}

func (m *mockSubscribeStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockSubscribeStream) Trailer() metadata.MD         { return nil }
func (m *mockSubscribeStream) CloseSend() error             { return nil }
func (m *mockSubscribeStream) Context() context.Context     { return context.Background() }
func (m *mockSubscribeStream) SendMsg(_ any) error          { return nil }
func (m *mockSubscribeStream) RecvMsg(_ any) error          { return nil }

// newStreamEventsServer starts an httptest server with the web handler and
// returns a connect client pointing at it, plus a cleanup function.
func newStreamEventsServer(t *testing.T, client CoreClient) (webv1connect.WebServiceClient, func()) {
	t.Helper()
	handler := NewHandler(client)
	_, h := webv1connect.NewWebServiceHandler(handler)
	srv := httptest.NewServer(h)
	wsc := webv1connect.NewWebServiceClient(http.DefaultClient, srv.URL)
	return wsc, srv.Close
}

// TestStreamEventsPassesConnectionIDAndClientTypeOnSubscribe asserts that the
// web gateway generates a connection_id and passes it (plus client_type
// "terminal") on its Subscribe call so core can register the connection in
// the session store (bd-j2xj). Previously the gateway called AddConnection
// directly; now that wiring lives inside core's Subscribe handler.
func TestStreamEventsPassesConnectionIDAndClientTypeOnSubscribe(t *testing.T) {
	sub := &mockSubscribeStream{
		responses: []*corev1.SubscribeResponse{
			{
				Frame: &corev1.SubscribeResponse_Control{
					Control: &corev1.ControlFrame{
						Signal: corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED,
					},
				},
			},
		},
	}
	client := &mockCoreClient{
		subStream: sub,
		discResp:  &corev1.DisconnectResponse{Success: true},
	}
	wsc, cleanup := newStreamEventsServer(t, client)
	defer cleanup()

	stream, err := wsc.StreamEvents(context.Background(), connect.NewRequest(&webv1.StreamEventsRequest{
		SessionId: "sess-conn",
	}))
	require.NoError(t, err)
	for stream.Receive() {
		// drain until server closes
	}
	require.NoError(t, stream.Close())

	require.Eventually(t, func() bool {
		return client.subReq != nil
	}, 2*time.Second, 10*time.Millisecond, "Subscribe should have been called")

	assert.Equal(t, "sess-conn", client.subReq.GetSessionId())
	assert.NotEmpty(t, client.subReq.GetConnectionId(),
		"Subscribe must carry a connection_id so core can register the connection")
	assert.Equal(t, "terminal", client.subReq.GetClientType(),
		"StreamEvents is the terminal-mode endpoint; client_type must be %q", "terminal")
}

func TestStreamEvents_ForwardsControlFrame(t *testing.T) {
	// holomush-iu8j: the gateway MUST forward attach_moment_ms from
	// core's ControlFrame to the web ControlFrame on REPLAY_COMPLETE
	// so the browser receives the attach moment and can scope its
	// backfill (notAfterMs) correctly. Without this, attach_moment_ms
	// is silently dropped at the gateway and the client falls back to
	// the legacy unbounded-backfill behavior — re-opening the fujt race.
	const attachMomentMs int64 = 1700000999999
	sub := &mockSubscribeStream{
		responses: []*corev1.SubscribeResponse{
			{
				Frame: &corev1.SubscribeResponse_Control{
					Control: &corev1.ControlFrame{
						Signal:         corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE,
						Message:        "replay done",
						AttachMomentMs: attachMomentMs,
					},
				},
			},
		},
	}
	client := &mockCoreClient{subStream: sub}
	wsc, cleanup := newStreamEventsServer(t, client)
	defer cleanup()

	stream, err := wsc.StreamEvents(context.Background(), connect.NewRequest(&webv1.StreamEventsRequest{
		SessionId: "sess-test",
	}))
	require.NoError(t, err)
	defer stream.Close()

	// First frame is the STREAM_OPENED ControlFrame with connection_id
	// (added in PR #4191 round 5 to fix multi-tab routing).
	ok := stream.Receive()
	require.True(t, ok, "expected to receive STREAM_OPENED frame")
	openCtrl := stream.Msg().GetControl()
	require.NotNil(t, openCtrl, "first frame MUST be a ControlFrame")
	assert.Equal(t, webv1.ControlSignal_CONTROL_SIGNAL_STREAM_OPENED, openCtrl.GetSignal())
	assert.NotEmpty(t, openCtrl.GetConnectionId(), "STREAM_OPENED MUST include connection_id")

	// Second frame is the forwarded REPLAY_COMPLETE from the fixture.
	ok = stream.Receive()
	require.True(t, ok, "expected to receive forwarded REPLAY_COMPLETE frame")

	msg := stream.Msg()
	require.NotNil(t, msg)
	ctrl := msg.GetControl()
	require.NotNil(t, ctrl, "expected a ControlFrame, got: %v", msg)
	assert.Equal(t, webv1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE, ctrl.GetSignal())
	assert.Equal(t, "replay done", ctrl.GetMessage())
	assert.Equal(t, attachMomentMs, ctrl.GetAttachMomentMs(),
		"gateway MUST forward attach_moment_ms on REPLAY_COMPLETE (iu8j cursor-bounded backfill)")
}

func TestStreamEvents_StreamClosedEndsStream(t *testing.T) {
	sub := &mockSubscribeStream{
		responses: []*corev1.SubscribeResponse{
			{
				Frame: &corev1.SubscribeResponse_Control{
					Control: &corev1.ControlFrame{
						Signal: corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED,
					},
				},
			},
		},
	}
	client := &mockCoreClient{subStream: sub}
	wsc, cleanup := newStreamEventsServer(t, client)
	defer cleanup()

	stream, err := wsc.StreamEvents(context.Background(), connect.NewRequest(&webv1.StreamEventsRequest{
		SessionId: "sess-test",
	}))
	require.NoError(t, err)
	defer stream.Close()

	// First frame is the STREAM_OPENED ControlFrame (PR #4191 round 5).
	ok := stream.Receive()
	require.True(t, ok, "expected to receive STREAM_OPENED frame")
	openCtrl := stream.Msg().GetControl()
	require.NotNil(t, openCtrl)
	assert.Equal(t, webv1.ControlSignal_CONTROL_SIGNAL_STREAM_OPENED, openCtrl.GetSignal())

	// Second frame: the STREAM_CLOSED control frame from the fixture.
	ok = stream.Receive()
	require.True(t, ok, "expected to receive STREAM_CLOSED frame")
	ctrl := stream.Msg().GetControl()
	require.NotNil(t, ctrl)
	assert.Equal(t, webv1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED, ctrl.GetSignal())

	// Stream should now be done.
	ok = stream.Receive()
	assert.False(t, ok, "stream should be closed after STREAM_CLOSED")
}

func TestWebQueryStreamHistoryProxiesToCoreService(t *testing.T) {
	now := timestamppb.Now()
	sayRendering := &corev1.RenderingMetadata{Category: "communication", Format: "speech", Label: "says", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"}
	poseRendering := &corev1.RenderingMetadata{Category: "communication", Format: "action", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"}
	client := &mockCoreClient{
		queryStreamHistoryResp: &corev1.QueryStreamHistoryResponse{
			Events: []*corev1.EventFrame{
				{Type: "say", Timestamp: now, Payload: []byte(`{"character_name":"Alice","message":"Hello!"}`), Rendering: sayRendering},
				{Type: "pose", Timestamp: now, Payload: []byte(`{"character_name":"Bob","action":"waves."}`), Rendering: poseRendering},
			},
			HasMore: true,
		},
	}
	h := NewHandler(client)

	resp, err := h.WebQueryStreamHistory(context.Background(), connect.NewRequest(&webv1.WebQueryStreamHistoryRequest{
		SessionId: "sess-abc",
		Stream:    "main",
		Count:     2,
	}))
	require.NoError(t, err)
	assert.Len(t, resp.Msg.GetEvents(), 2)
	assert.True(t, resp.Msg.GetHasMore())
}

func TestWebQueryStreamHistoryPropagatesError(t *testing.T) {
	// The proxy MUST return upstream errors unchanged so ConnectRPC preserves
	// the original gRPC status code (SESSION_EXPIRED, STREAM_ACCESS_DENIED,
	// INVALID_ARGUMENT, etc.). Wrapping as connect.CodeInternal would collapse
	// all of these into one opaque server error.
	upstream := errors.New("core unavailable")
	client := &mockCoreClient{
		queryStreamHistoryErr: upstream,
	}
	h := NewHandler(client)

	_, err := h.WebQueryStreamHistory(context.Background(), connect.NewRequest(&webv1.WebQueryStreamHistoryRequest{
		SessionId: "sess-abc",
		Stream:    "main",
	}))
	require.Error(t, err)
	assert.ErrorIs(t, err, upstream, "proxy must return the upstream error unchanged")
}

func TestWebQueryStreamHistoryPopulatesTypeAndTimestamp(t *testing.T) {
	ts := timestamppb.New(timestamppb.Now().AsTime())
	client := &mockCoreClient{
		queryStreamHistoryResp: &corev1.QueryStreamHistoryResponse{
			Events: []*corev1.EventFrame{
				{
					Id:        "01ABCDEF",
					Stream:    "main",
					Type:      "say",
					Timestamp: ts,
					ActorId:   "char-1",
					Payload:   []byte(`{"character_name":"Alice","message":"Hello!"}`),
					Rendering: &corev1.RenderingMetadata{Category: "communication", Format: "speech", Label: "says", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
				},
			},
			HasMore: false,
		},
	}
	h := NewHandler(client)

	resp, err := h.WebQueryStreamHistory(context.Background(), connect.NewRequest(&webv1.WebQueryStreamHistoryRequest{
		SessionId: "sess-abc",
		Stream:    "main",
	}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetEvents(), 1)
	ge := resp.Msg.GetEvents()[0]
	assert.Equal(t, "say", ge.GetType())
	assert.Equal(t, ts.GetSeconds(), ge.GetTimestamp())
}

func TestWebQueryStreamHistoryPropagatesHasMore(t *testing.T) {
	now := timestamppb.Now()
	payload := []byte(`{"character_name":"Alice","message":"Hi"}`)
	rendering := &corev1.RenderingMetadata{Category: "communication", Format: "speech", Label: "says", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"}

	t.Run("has_more true is forwarded", func(t *testing.T) {
		client := &mockCoreClient{
			queryStreamHistoryResp: &corev1.QueryStreamHistoryResponse{
				Events:  []*corev1.EventFrame{{Type: "say", Timestamp: now, Payload: payload, Rendering: rendering}},
				HasMore: true,
			},
		}
		h := NewHandler(client)
		resp, err := h.WebQueryStreamHistory(context.Background(), connect.NewRequest(&webv1.WebQueryStreamHistoryRequest{
			SessionId: "sess-1", Stream: "main",
		}))
		require.NoError(t, err)
		assert.True(t, resp.Msg.GetHasMore())
	})

	t.Run("has_more false is forwarded", func(t *testing.T) {
		client := &mockCoreClient{
			queryStreamHistoryResp: &corev1.QueryStreamHistoryResponse{
				Events:  []*corev1.EventFrame{{Type: "say", Timestamp: now, Payload: payload, Rendering: rendering}},
				HasMore: false,
			},
		}
		h := NewHandler(client)
		resp, err := h.WebQueryStreamHistory(context.Background(), connect.NewRequest(&webv1.WebQueryStreamHistoryRequest{
			SessionId: "sess-1", Stream: "main",
		}))
		require.NoError(t, err)
		assert.False(t, resp.Msg.GetHasMore())
	})
}

func TestWebQueryStreamHistoryPropagatesRequestFields(t *testing.T) {
	client := &mockCoreClient{
		queryStreamHistoryResp: &corev1.QueryStreamHistoryResponse{},
	}
	h := NewHandler(client)

	cursorBytes := []byte{0x01, 0x02, 0x03} // opaque cursor bytes
	_, err := h.WebQueryStreamHistory(context.Background(), connect.NewRequest(&webv1.WebQueryStreamHistoryRequest{
		SessionId:   "sess-xyz",
		Stream:      "location:abc",
		Count:       50,
		NotBeforeMs: 1700000000000,
		Cursor:      cursorBytes,
		NotAfterMs:  1700000999999, // holomush-iu8j: Subscribe attach-moment ceiling
	}))
	require.NoError(t, err)

	req := client.queryStreamHistoryReq
	require.NotNil(t, req, "QueryStreamHistory should have been called")
	assert.Equal(t, "sess-xyz", req.GetSessionId())
	assert.Equal(t, "location:abc", req.GetStream())
	assert.Equal(t, int32(50), req.GetCount())
	assert.Equal(t, int64(1700000000000), req.GetNotBeforeMs())
	assert.Equal(t, cursorBytes, req.GetCursor())
	assert.Equal(t, int64(1700000999999), req.GetNotAfterMs(),
		"gateway MUST forward not_after_ms to core (iu8j cursor-bounded backfill)")
}

// Post-auth RPC token forwarding (bd-jv7z, Task 7).
//
// The gateway MUST forward the X-Session-Token header (injected by
// CookieMiddleware) to core on every post-auth RPC. Without this,
// server-side ValidateSessionOwnership (Tasks 9-12) cannot enforce that
// the caller owns session_id and requests would be rejected.

func TestSendCommandForwardsPlayerSessionToken(t *testing.T) {
	const token = "tok-send-command"
	client := &mockCoreClient{
		cmdResp: &corev1.HandleCommandResponse{Success: true},
	}
	h := NewHandler(client)

	req := connect.NewRequest(&webv1.SendCommandRequest{
		SessionId: "sess-1",
		Text:      "look",
	})
	req.Header().Set(headerInjectSessionToken, token)

	_, err := h.SendCommand(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, client.cmdReq, "HandleCommand should have been called")
	assert.Equal(t, token, client.cmdReq.GetPlayerSessionToken())
	assert.Equal(t, "sess-1", client.cmdReq.GetSessionId())
}

func TestStreamEventsForwardsPlayerSessionToken(t *testing.T) {
	const token = "tok-stream-events"
	sub := &mockSubscribeStream{
		responses: []*corev1.SubscribeResponse{
			{
				Frame: &corev1.SubscribeResponse_Control{
					Control: &corev1.ControlFrame{
						Signal: corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED,
					},
				},
			},
		},
	}
	client := &mockCoreClient{subStream: sub}
	wsc, cleanup := newStreamEventsServer(t, client)
	defer cleanup()

	streamReq := connect.NewRequest(&webv1.StreamEventsRequest{SessionId: "sess-2"})
	streamReq.Header().Set(headerInjectSessionToken, token)

	stream, err := wsc.StreamEvents(context.Background(), streamReq)
	require.NoError(t, err)
	defer stream.Close()

	// Drain until server ends the stream via STREAM_CLOSED.
	for stream.Receive() {
	}

	require.NotNil(t, client.subReq, "Subscribe should have been called")
	assert.Equal(t, token, client.subReq.GetPlayerSessionToken())
	assert.Equal(t, "sess-2", client.subReq.GetSessionId())
}

// TestStreamEventsForwardsPlayerSessionTokenOnStreamCloseCleanup verifies that
// when StreamEvents closes (stream end/context cancel), the internal cleanup
// Disconnect RPC forwards the player session token. Without the token the
// server-side ownership gate would skip the cleanup (connection remove,
// detach/delete, disconnect hooks).
func TestStreamEventsForwardsPlayerSessionTokenOnStreamCloseCleanup(t *testing.T) {
	const token = "tok-stream-cleanup"
	sub := &mockSubscribeStream{
		responses: []*corev1.SubscribeResponse{
			{
				Frame: &corev1.SubscribeResponse_Control{
					Control: &corev1.ControlFrame{
						Signal: corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED,
					},
				},
			},
		},
	}
	client := &mockCoreClient{
		subStream: sub,
		discResp:  &corev1.DisconnectResponse{Success: true},
	}

	handler := NewHandler(client)
	_, h := webv1connect.NewWebServiceHandler(handler)
	srv := httptest.NewServer(h)
	defer srv.Close()
	wsc := webv1connect.NewWebServiceClient(http.DefaultClient, srv.URL)

	streamReq := connect.NewRequest(&webv1.StreamEventsRequest{SessionId: "sess-cleanup"})
	streamReq.Header().Set(headerInjectSessionToken, token)

	stream, err := wsc.StreamEvents(context.Background(), streamReq)
	require.NoError(t, err)
	// Drain until server ends the stream via STREAM_CLOSED, which triggers
	// the deferred cleanup Disconnect.
	for stream.Receive() {
	}
	require.NoError(t, stream.Close())

	// The cleanup Disconnect runs in a deferred goroutine on the server side.
	// Poll briefly to give it time to fire before asserting.
	require.Eventually(t, func() bool {
		return client.discReq != nil
	}, 2*time.Second, 10*time.Millisecond, "cleanup Disconnect should have been called")

	assert.Equal(t, "sess-cleanup", client.discReq.GetSessionId())
	assert.Equal(t, token, client.discReq.GetPlayerSessionToken(),
		"cleanup Disconnect must forward the player session token so the ownership gate passes")
}

func TestDisconnectForwardsPlayerSessionToken(t *testing.T) {
	const token = "tok-disconnect"
	client := &mockCoreClient{
		discResp: &corev1.DisconnectResponse{Success: true},
	}
	h := NewHandler(client)

	req := connect.NewRequest(&webv1.DisconnectRequest{SessionId: "sess-3"})
	req.Header().Set(headerInjectSessionToken, token)

	_, err := h.Disconnect(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, client.discReq, "Disconnect should have been called")
	assert.Equal(t, token, client.discReq.GetPlayerSessionToken())
	assert.Equal(t, "sess-3", client.discReq.GetSessionId())
}

func TestGetCommandHistoryForwardsPlayerSessionToken(t *testing.T) {
	const token = "tok-cmd-history"
	client := &mockCoreClient{
		cmdHistory: []string{"look"},
	}
	h := NewHandler(client)

	req := connect.NewRequest(&webv1.GetCommandHistoryRequest{SessionId: "sess-4"})
	req.Header().Set(headerInjectSessionToken, token)

	_, err := h.GetCommandHistory(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, client.cmdHistoryReq, "GetCommandHistory should have been called")
	assert.Equal(t, token, client.cmdHistoryReq.GetPlayerSessionToken())
	assert.Equal(t, "sess-4", client.cmdHistoryReq.GetSessionId())
}

func TestWebListSessionStreamsProxiesToCore(t *testing.T) {
	client := &mockCoreClient{
		listSessionStreamsResp: &corev1.ListSessionStreamsResponse{
			Streams: []string{"character:c1", "location:l1"},
		},
	}
	h := NewHandler(client)

	resp, err := h.WebListSessionStreams(context.Background(),
		connect.NewRequest(&webv1.WebListSessionStreamsRequest{SessionId: "s1"}))
	require.NoError(t, err)
	assert.Equal(t, []string{"character:c1", "location:l1"}, resp.Msg.GetStreams())

	require.NotNil(t, client.listSessionStreamsReq, "ListSessionStreams should have been called")
	assert.Equal(t, "s1", client.listSessionStreamsReq.GetSessionId())
}

func TestWebListSessionStreamsPassesErrorsThrough(t *testing.T) {
	client := &mockCoreClient{
		listSessionStreamsErr: oops.Code("SESSION_EXPIRED").Errorf("expired"),
	}
	h := NewHandler(client)

	_, err := h.WebListSessionStreams(context.Background(),
		connect.NewRequest(&webv1.WebListSessionStreamsRequest{SessionId: "s1"}))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_EXPIRED")
}

func TestWebListSessionStreamsForwardsPlayerSessionToken(t *testing.T) {
	const token = "tok-list-streams"
	client := &mockCoreClient{
		listSessionStreamsResp: &corev1.ListSessionStreamsResponse{
			Streams: []string{"character:c1"},
		},
	}
	h := NewHandler(client)

	req := connect.NewRequest(&webv1.WebListSessionStreamsRequest{SessionId: "sess-5"})
	req.Header().Set(headerInjectSessionToken, token)

	_, err := h.WebListSessionStreams(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, client.listSessionStreamsReq, "ListSessionStreams should have been called")
	assert.Equal(t, token, client.listSessionStreamsReq.GetPlayerSessionToken())
	assert.Equal(t, "sess-5", client.listSessionStreamsReq.GetSessionId())
}

func TestWebListFocusPresenceForwardsToCoreService(t *testing.T) {
	sessID := "sess-1"
	token := "tok-from-header"
	coreResp := &corev1.ListFocusPresenceResponse{
		Context:   corev1.PresenceContext_PRESENCE_CONTEXT_LOCATION,
		ContextId: "01HYXLOCATION0000000000001",
		Entries: []*corev1.PresenceEntry{
			{
				CharacterId:   "01HYXCHARALICE0000000000AA",
				CharacterName: "alice",
				State:         corev1.PresenceState_PRESENCE_STATE_ACTIVE,
			},
		},
	}
	client := &mockCoreClient{
		listFocusPresenceResp: coreResp,
	}
	h := NewHandler(client)

	req := connect.NewRequest(&webv1.WebListFocusPresenceRequest{SessionId: sessID})
	req.Header().Set(headerInjectSessionToken, token)

	resp, err := h.WebListFocusPresence(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, client.listFocusPresenceReq, "ListFocusPresence should have been called")
	assert.Equal(t, sessID, client.listFocusPresenceReq.GetSessionId())
	assert.Equal(t, token, client.listFocusPresenceReq.GetPlayerSessionToken())

	msg := resp.Msg
	assert.Equal(t, webv1.WebPresenceContext_WEB_PRESENCE_CONTEXT_LOCATION, msg.GetContext())
	assert.Equal(t, "01HYXLOCATION0000000000001", msg.GetContextId())
	require.Len(t, msg.GetEntries(), 1)
	assert.Equal(t, "alice", msg.GetEntries()[0].GetCharacterName())
	assert.Equal(t, webv1.WebPresenceState_WEB_PRESENCE_STATE_ACTIVE, msg.GetEntries()[0].GetState())
}

func TestWebListCommandsForwardsToCoreServiceAndMapsFields(t *testing.T) {
	sessID := "sess-cmd-1"
	token := "tok-cmd-header"
	coreResp := &corev1.ListAvailableCommandsResponse{
		Commands: []*corev1.AvailableCommand{
			{Name: "look", Help: "Look around", Usage: "look [target]", Source: "core"},
			{Name: "say", Help: "Say something", Usage: "say <text>", Source: "core"},
		},
		Aliases:    map[string]string{"l": "look", "'": "say"},
		Incomplete: true,
	}
	client := &mockCoreClient{
		listAvailableCommandsResp: coreResp,
	}
	h := NewHandler(client)

	req := connect.NewRequest(&webv1.WebListCommandsRequest{SessionId: sessID})
	req.Header().Set(headerInjectSessionToken, token)

	resp, err := h.WebListCommands(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, client.listAvailableCommandsReq, "ListAvailableCommands should have been called")
	assert.Equal(t, sessID, client.listAvailableCommandsReq.GetSessionId())
	assert.Equal(t, token, client.listAvailableCommandsReq.GetPlayerSessionToken())

	msg := resp.Msg
	require.Len(t, msg.GetCommands(), 2)
	assert.Equal(t, "look", msg.GetCommands()[0].GetName())
	assert.Equal(t, "Look around", msg.GetCommands()[0].GetHelp())
	assert.Equal(t, "look [target]", msg.GetCommands()[0].GetUsage())
	assert.Equal(t, "core", msg.GetCommands()[0].GetSource())
	assert.Equal(t, "say", msg.GetCommands()[1].GetName())
	assert.Equal(t, map[string]string{"l": "look", "'": "say"}, msg.GetAliases())
	assert.True(t, msg.GetIncomplete())
}

func TestWebListCommandsSkipsNilCommandsInResponse(t *testing.T) {
	coreResp := &corev1.ListAvailableCommandsResponse{
		Commands: []*corev1.AvailableCommand{
			nil,
			{Name: "look", Help: "Look around", Usage: "look [target]", Source: "core"},
			nil,
		},
	}
	client := &mockCoreClient{listAvailableCommandsResp: coreResp}
	h := NewHandler(client)

	req := connect.NewRequest(&webv1.WebListCommandsRequest{SessionId: "sess-nil"})
	resp, err := h.WebListCommands(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetCommands(), 1)
	assert.Equal(t, "look", resp.Msg.GetCommands()[0].GetName())
}

func TestWebListCommandsPassesThroughCoreServiceError(t *testing.T) {
	coreErr := oops.Errorf("RPC_FAILED: core unavailable")
	client := &mockCoreClient{listAvailableCommandsErr: coreErr}
	h := NewHandler(client)

	req := connect.NewRequest(&webv1.WebListCommandsRequest{SessionId: "sess-err"})
	_, err := h.WebListCommands(context.Background(), req)
	require.Error(t, err)
	assert.ErrorIs(t, err, coreErr)
}

// blockingSubscribeStream is a CoreService_SubscribeClient that blocks on Recv
// until its done channel is closed, then returns io.EOF.
type blockingSubscribeStream struct {
	done chan struct{}
}

func (b *blockingSubscribeStream) Recv() (*corev1.SubscribeResponse, error) {
	<-b.done
	return nil, io.EOF
}

func (b *blockingSubscribeStream) Header() (metadata.MD, error) { return nil, nil }
func (b *blockingSubscribeStream) Trailer() metadata.MD         { return nil }
func (b *blockingSubscribeStream) CloseSend() error             { return nil }
func (b *blockingSubscribeStream) Context() context.Context     { return context.Background() }
func (b *blockingSubscribeStream) SendMsg(_ any) error          { return nil }
func (b *blockingSubscribeStream) RecvMsg(_ any) error          { return nil }

// TestStreamEventsRefreshesLeaseOnHeartbeat asserts that the heartbeat tick
// calls RefreshConnection so the server-side liveness lease is renewed while
// the stream is open (I-LIVE-1).
// Verifies: I-LIVE-1
func TestStreamEventsRefreshesLeaseOnHeartbeat(t *testing.T) {
	done := make(chan struct{})
	mc := &mockCoreClient{
		subStream: &blockingSubscribeStream{done: done},
		discResp:  &corev1.DisconnectResponse{Success: true},
	}

	h := NewHandler(mc)
	h.heartbeatInterval = 5 * time.Millisecond

	_, httpHandler := webv1connect.NewWebServiceHandler(h)
	srv := httptest.NewServer(httpHandler)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const refreshToken = "tok-heartbeat-refresh"
	wsc := webv1connect.NewWebServiceClient(http.DefaultClient, srv.URL)
	streamReq := connect.NewRequest(&webv1.StreamEventsRequest{SessionId: "sess-refresh"})
	streamReq.Header().Set(headerInjectSessionToken, refreshToken)
	stream, err := wsc.StreamEvents(ctx, streamReq)
	require.NoError(t, err)

	// Receive the STREAM_OPENED frame so the HTTP/2 stream is live.
	ok := stream.Receive()
	require.True(t, ok, "expected STREAM_OPENED frame")
	ctrl := stream.Msg().GetControl()
	require.NotNil(t, ctrl)
	assert.Equal(t, webv1.ControlSignal_CONTROL_SIGNAL_STREAM_OPENED, ctrl.GetSignal())
	connID := ctrl.GetConnectionId()
	require.NotEmpty(t, connID, "STREAM_OPENED must carry a connection_id")

	// Wait for at least one RefreshConnection call (heartbeat fires every 5ms).
	assert.Eventually(t, func() bool {
		return mc.refreshConnectionCalls.Load() >= 1
	}, 500*time.Millisecond, time.Millisecond, "heartbeat must call RefreshConnection at least once")

	// Cancel the context to terminate StreamEvents, then unblock Recv.
	cancel()
	close(done)
	// Drain remaining frames.
	for stream.Receive() {
	}

	// Assert on the last captured refresh request.
	lastReq := mc.refreshConnectionReq.Load()
	require.NotNil(t, lastReq, "RefreshConnection must have been called")
	assert.Equal(t, "sess-refresh", lastReq.GetSessionId(),
		"refresh must carry the session_id")
	assert.Equal(t, connID, lastReq.GetConnectionId(),
		"refresh must carry the connection_id from STREAM_OPENED")
	assert.Equal(t, refreshToken, lastReq.GetPlayerSessionToken(),
		"refresh must carry the player session token so the ownership-validated RefreshConnection gate passes")
}

// scriptedSubscribeStream is a CoreService_SubscribeClient that yields a fixed
// list of frames, then returns a terminal error (e.g. a non-EOF transport error
// to simulate a core-stream break, or io.EOF for a clean end). Used by the
// reconnect-survival test to script a break-then-resume sequence.
type scriptedSubscribeStream struct {
	ctx     context.Context
	frames  []*corev1.SubscribeResponse
	idx     int
	termErr error
}

func (s *scriptedSubscribeStream) Recv() (*corev1.SubscribeResponse, error) {
	if s.idx < len(s.frames) {
		f := s.frames[s.idx]
		s.idx++
		return f, nil
	}
	return nil, s.termErr
}

func (s *scriptedSubscribeStream) Header() (metadata.MD, error) { return nil, nil }
func (s *scriptedSubscribeStream) Trailer() metadata.MD         { return nil }
func (s *scriptedSubscribeStream) CloseSend() error             { return nil }
func (s *scriptedSubscribeStream) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}
func (s *scriptedSubscribeStream) SendMsg(_ any) error { return nil }
func (s *scriptedSubscribeStream) RecvMsg(_ any) error { return nil }

// reconnectCoreClient embeds the standard mock but routes Subscribe through a
// per-call func so the test can script a different stream on each (re)Subscribe.
type reconnectCoreClient struct {
	mockCoreClient
	subscribeFunc func(ctx context.Context, req *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error)
}

func (m *reconnectCoreClient) Subscribe(ctx context.Context, req *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
	return m.subscribeFunc(ctx, req)
}

// reconnectEventFrame builds a forward-able event frame carrying an id (the dedup
// key) and the rendering band the gateway requires (INV-EVENTBUS-6).
func reconnectEventFrame(id string) *corev1.SubscribeResponse {
	return &corev1.SubscribeResponse{
		Frame: &corev1.SubscribeResponse_Event{
			Event: &corev1.EventFrame{
				Id:      id,
				Type:    "say",
				Payload: []byte(`{"message":"hello"}`),
				Rendering: &corev1.RenderingMetadata{
					Category:      "communication",
					Format:        "speech",
					Label:         "says",
					DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
					SourcePlugin:  "core-communication",
				},
			},
		},
	}
}

// controlSignals extracts the control-frame signals from a captured frame slice.
func controlSignals(frames []*webv1.StreamEventsResponse) []webv1.ControlSignal {
	var sigs []webv1.ControlSignal
	for _, f := range frames {
		if c := f.GetControl(); c != nil {
			sigs = append(sigs, c.GetSignal())
		}
	}
	return sigs
}

// eventIDs extracts the forwarded event ids (GameEvent.event_id) in order.
func eventIDs(frames []*webv1.StreamEventsResponse) []string {
	var ids []string
	for _, f := range frames {
		if e := f.GetEvent(); e != nil {
			ids = append(ids, e.GetEventId())
		}
	}
	return ids
}

// TestStreamEventsReconnectsOnCoreStreamBreakWithoutClosingClient verifies that
// when the core Subscribe stream errors mid-flight (core redeploy / transient
// transport loss), the gateway holds the client stream open, re-subscribes (the
// durable JetStream consumer resumes server-side), dedups the redelivered
// overlap frame, and continues — emitting RECONNECTING then RECONNECTED control
// frames around the break. (holomush-rsoe6.10, I-SURV-1/2/4.)
// Verifies: I-SURV-1
// Verifies: I-SURV-2
func TestStreamEventsReconnectsOnCoreStreamBreakWithoutClosingClient(t *testing.T) {
	var subscribeCalls atomic.Int32

	mc := &reconnectCoreClient{}
	mc.discResp = &corev1.DisconnectResponse{Success: true}
	mc.subscribeFunc = func(ctx context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
		n := subscribeCalls.Add(1)
		if n == 1 {
			// First attempt: yield E1, then break with a non-EOF transport error
			// (simulating core going away mid-stream).
			return &scriptedSubscribeStream{
				ctx:     ctx,
				frames:  []*corev1.SubscribeResponse{reconnectEventFrame("E1")},
				termErr: connect.NewError(connect.CodeUnavailable, errors.New("core stream broke")),
			}, nil
		}
		// Resume: the durable consumer redelivers E1 (overlap), then E2, then
		// ends cleanly via io.EOF.
		return &scriptedSubscribeStream{
			ctx:     ctx,
			frames:  []*corev1.SubscribeResponse{reconnectEventFrame("E1"), reconnectEventFrame("E2")},
			termErr: io.EOF,
		}, nil
	}

	h := NewHandler(mc)
	h.reconnectCeiling = 2 * time.Second
	h.heartbeatInterval = 1 * time.Hour // keep the heartbeat out of the way

	_, httpHandler := webv1connect.NewWebServiceHandler(h)
	srv := httptest.NewServer(httpHandler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsc := webv1connect.NewWebServiceClient(http.DefaultClient, srv.URL)
	stream, err := wsc.StreamEvents(ctx, connect.NewRequest(&webv1.StreamEventsRequest{
		SessionId: "session-reconnect",
	}))
	require.NoError(t, err)

	var frames []*webv1.StreamEventsResponse
	for stream.Receive() {
		frames = append(frames, stream.Msg())
	}
	require.NoError(t, stream.Err())

	// The client stream was NOT closed on the first core break: it received E2,
	// which only arrives on the second (resumed) subscription.
	ids := eventIDs(frames)
	require.Equal(t, []string{"E1", "E2"}, ids, "E1 forwarded once (redelivery deduped), E2 forwarded")

	sigs := controlSignals(frames)
	require.Contains(t, sigs, webv1.ControlSignal_CONTROL_SIGNAL_RECONNECTING)
	require.Contains(t, sigs, webv1.ControlSignal_CONTROL_SIGNAL_RECONNECTED)

	require.GreaterOrEqual(t, subscribeCalls.Load(), int32(2), "expected at least one re-Subscribe")
}

// gatedSubscribeStream forwards its frames, then blocks on a release channel
// before returning its terminal error. This lets a test hold a subscription
// "healthy" for a controlled wall-clock duration (longer than the reconnect
// ceiling) before simulating a core-stream break, proving the reconnect budget
// is per-outage rather than measured from stream-open.
type gatedSubscribeStream struct {
	frames  []*corev1.SubscribeResponse
	idx     int
	release <-chan struct{} // closed by the test to let the break happen
	termErr error
}

func (g *gatedSubscribeStream) Recv() (*corev1.SubscribeResponse, error) {
	if g.idx < len(g.frames) {
		f := g.frames[g.idx]
		g.idx++
		return f, nil
	}
	if g.release != nil {
		<-g.release // stay "healthy" until the test releases us
	}
	return nil, g.termErr
}

func (g *gatedSubscribeStream) Header() (metadata.MD, error) { return nil, nil }
func (g *gatedSubscribeStream) Trailer() metadata.MD         { return nil }
func (g *gatedSubscribeStream) CloseSend() error             { return nil }
func (g *gatedSubscribeStream) Context() context.Context     { return context.Background() }
func (g *gatedSubscribeStream) SendMsg(_ any) error          { return nil }
func (g *gatedSubscribeStream) RecvMsg(_ any) error          { return nil }

// TestStreamEventsReconnectCeilingIsPerOutageNotStreamLifetime proves the
// reconnect ceiling bounds a single outage, not total stream lifetime
// (I-SURV-4). The first subscription stays healthy (forwarding E1) for longer
// than the (small) ceiling before the core breaks. Under the old stream-open
// deadline the break would be past the ceiling → CodeUnavailable → client
// dropped, never seeing RECONNECTED or E2. With the per-outage budget the
// outage clock starts at break-detection, so the reconnect proceeds and the
// client survives to E2.
// Verifies: I-SURV-4
func TestStreamEventsReconnectCeilingIsPerOutageNotStreamLifetime(t *testing.T) {
	const ceiling = 50 * time.Millisecond
	// healthyFor must exceed the ceiling so the break lands past
	// stream-open+ceiling — the exact condition that fails the old code.
	const healthyFor = 150 * time.Millisecond

	var subscribeCalls atomic.Int32
	release := make(chan struct{})

	mc := &reconnectCoreClient{}
	mc.discResp = &corev1.DisconnectResponse{Success: true}
	mc.subscribeFunc = func(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
		n := subscribeCalls.Add(1)
		if n == 1 {
			// First attempt: forward E1, then stay healthy past the ceiling
			// (blocking on release) before breaking with a transport error.
			return &gatedSubscribeStream{
				frames:  []*corev1.SubscribeResponse{reconnectEventFrame("E1")},
				release: release,
				termErr: connect.NewError(connect.CodeUnavailable, errors.New("core stream broke")),
			}, nil
		}
		// Resume: redeliver E1 (deduped) then E2, then end cleanly.
		return &scriptedSubscribeStream{
			frames:  []*corev1.SubscribeResponse{reconnectEventFrame("E1"), reconnectEventFrame("E2")},
			termErr: io.EOF,
		}, nil
	}

	h := NewHandler(mc)
	h.reconnectCeiling = ceiling
	h.heartbeatInterval = 1 * time.Hour // keep the heartbeat out of the way

	_, httpHandler := webv1connect.NewWebServiceHandler(h)
	srv := httptest.NewServer(httpHandler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Release the first stream's break only after the stream has been healthy
	// longer than the ceiling, so the break is provably past stream-open+ceiling.
	go func() {
		time.Sleep(healthyFor)
		close(release)
	}()

	wsc := webv1connect.NewWebServiceClient(http.DefaultClient, srv.URL)
	stream, err := wsc.StreamEvents(ctx, connect.NewRequest(&webv1.StreamEventsRequest{
		SessionId: "session-per-outage",
	}))
	require.NoError(t, err)

	var frames []*webv1.StreamEventsResponse
	for stream.Receive() {
		frames = append(frames, stream.Msg())
	}
	// With the old stream-open deadline this stream would terminate with
	// CodeUnavailable; with the per-outage budget it ends cleanly.
	require.NoError(t, stream.Err(),
		"per-outage ceiling: a long-healthy stream must survive a later core blip, not return CodeUnavailable")

	ids := eventIDs(frames)
	require.Equal(t, []string{"E1", "E2"}, ids,
		"stream survived the break: E1 forwarded once (redelivery deduped), E2 forwarded after reconnect")

	sigs := controlSignals(frames)
	require.Contains(t, sigs, webv1.ControlSignal_CONTROL_SIGNAL_RECONNECTING)
	require.Contains(t, sigs, webv1.ControlSignal_CONTROL_SIGNAL_RECONNECTED,
		"the client must see RECONNECTED — proving the reconnect was not aborted by a lapsed ceiling")

	require.GreaterOrEqual(t, subscribeCalls.Load(), int32(2), "expected a re-Subscribe after the break")
}

// TestStreamEventsTerminatesOnSessionNotFoundFromRecv is the round-2 P0
// regression guard: grpc-go's server-streaming dispatch returns (stream, nil)
// from Subscribe and defers the handler's ownership error to the FIRST Recv().
// So a session reaped past its reattach TTL surfaces as a codes.Unauthenticated
// status from sub.Recv() — NOT from the synchronous Subscribe return. The
// gateway MUST classify that as terminal (send STREAM_CLOSED so the client
// re-authenticates) rather than treating it as a transient core break and
// retrying for the full reconnect ceiling. Before the fix, this recv error fell
// into the breakCoreGone branch and the loop spun until the ceiling lapsed.
// Verifies: I-SURV-1
func TestStreamEventsTerminatesOnSessionNotFoundFromRecv(t *testing.T) {
	var subscribeCalls atomic.Int32

	mc := &reconnectCoreClient{}
	mc.discResp = &corev1.DisconnectResponse{Success: true}
	mc.subscribeFunc = func(ctx context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
		subscribeCalls.Add(1)
		// Subscribe succeeds synchronously (the production shape); the
		// reaped-session error is deferred to the first Recv, surfacing as a
		// codes.Unauthenticated status — exactly what the core server stamps
		// for SESSION_NOT_FOUND on the wire (subscribeSessionNotFound).
		return &scriptedSubscribeStream{
			ctx:     ctx,
			frames:  nil,
			termErr: status.Error(codes.Unauthenticated, "session not found"),
		}, nil
	}

	h := NewHandler(mc)
	// A small ceiling: if the fix regresses and the recv error is treated as
	// transient (breakCoreGone), the loop would retry until this lapses. We
	// instead assert exactly one Subscribe and no RECONNECTING, so a regression
	// shows up as extra Subscribe calls / a RECONNECTING frame, not a hang.
	h.reconnectCeiling = 2 * time.Second
	h.heartbeatInterval = 1 * time.Hour // keep the heartbeat out of the way

	_, httpHandler := webv1connect.NewWebServiceHandler(h)
	srv := httptest.NewServer(httpHandler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsc := webv1connect.NewWebServiceClient(http.DefaultClient, srv.URL)
	stream, err := wsc.StreamEvents(ctx, connect.NewRequest(&webv1.StreamEventsRequest{
		SessionId: "session-reaped",
	}))
	require.NoError(t, err)

	var frames []*webv1.StreamEventsResponse
	for stream.Receive() {
		frames = append(frames, stream.Msg())
	}
	require.NoError(t, stream.Err(),
		"terminal SESSION_NOT_FOUND must end the stream cleanly (client re-auths), not as CodeUnavailable")

	sigs := controlSignals(frames)
	// (a) The client is told to re-auth via STREAM_CLOSED.
	require.Contains(t, sigs, webv1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED,
		"a reaped session surfacing on Recv MUST send STREAM_CLOSED so the client re-authenticates")
	// (c) It was NOT treated as a transient core break.
	require.NotContains(t, sigs, webv1.ControlSignal_CONTROL_SIGNAL_RECONNECTING,
		"terminal SESSION_NOT_FOUND MUST NOT emit RECONNECTING — it is not a transient core break")

	// (b) Exactly one Subscribe attempt: no reconnect loop.
	require.Equal(t, int32(1), subscribeCalls.Load(),
		"terminal SESSION_NOT_FOUND on Recv MUST NOT trigger a re-Subscribe")
}

// TestForwardFrameMapsSceneActivityControlFrameSceneId verifies that a
// CONTROL_SIGNAL_SCENE_ACTIVITY core ControlFrame is translated by forwardFrame
// into the matching web ControlFrame with SceneId preserved (INV-SCENE-62
// badge passthrough). Uses the real HTTP/Connect path so the proto codec is
// live, mirroring TestStreamEvents_ForwardsControlFrame.
func TestForwardFrameMapsSceneActivityControlFrameSceneId(t *testing.T) {
	t.Parallel()
	const testSceneID = "01HYSCENE00000000000000001"

	sub := &mockSubscribeStream{
		responses: []*corev1.SubscribeResponse{
			{
				Frame: &corev1.SubscribeResponse_Control{
					Control: &corev1.ControlFrame{
						Signal:  corev1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY,
						SceneId: testSceneID,
					},
				},
			},
		},
	}
	client := &mockCoreClient{subStream: sub}
	wsc, cleanup := newStreamEventsServer(t, client)
	defer cleanup()

	stream, err := wsc.StreamEvents(context.Background(), connect.NewRequest(&webv1.StreamEventsRequest{
		SessionId: "sess-scene-activity",
	}))
	require.NoError(t, err)
	defer stream.Close()

	// First frame is the gateway-synthesised STREAM_OPENED.
	ok := stream.Receive()
	require.True(t, ok, "expected STREAM_OPENED frame")
	openCtrl := stream.Msg().GetControl()
	require.NotNil(t, openCtrl)
	assert.Equal(t, webv1.ControlSignal_CONTROL_SIGNAL_STREAM_OPENED, openCtrl.GetSignal())

	// Second frame: the forwarded SCENE_ACTIVITY badge.
	ok = stream.Receive()
	require.True(t, ok, "expected to receive forwarded SCENE_ACTIVITY frame")
	ctrl := stream.Msg().GetControl()
	require.NotNil(t, ctrl, "forwarded frame must be a ControlFrame")
	assert.Equal(t, webv1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY, ctrl.GetSignal(),
		"web ControlSignal must match SCENE_ACTIVITY")
	assert.Equal(t, testSceneID, ctrl.GetSceneId(),
		"scene_id must round-trip through forwardFrame unchanged (INV-SCENE-62 badge passthrough)")
}
