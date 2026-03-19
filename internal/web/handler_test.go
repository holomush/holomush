// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	holoGRPC "github.com/holomush/holomush/internal/grpc"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// TestCoreClient_SatisfiedByGRPCClient verifies at compile time that
// *holoGRPC.Client implements the CoreClient interface.
func TestCoreClient_SatisfiedByGRPCClient(t *testing.T) {
	t.Helper()
	var _ CoreClient = (*holoGRPC.Client)(nil)
}

// mockCoreClient is a test double for CoreClient.
type mockCoreClient struct {
	authResp *corev1.AuthResponse
	authErr  error

	cmdResp *corev1.CommandResponse
	cmdErr  error

	subStream corev1.Core_SubscribeClient
	subErr    error

	discResp *corev1.DisconnectResponse
	discErr  error
}

func (m *mockCoreClient) Authenticate(_ context.Context, _ *corev1.AuthRequest) (*corev1.AuthResponse, error) {
	return m.authResp, m.authErr
}

func (m *mockCoreClient) HandleCommand(_ context.Context, _ *corev1.CommandRequest) (*corev1.CommandResponse, error) {
	return m.cmdResp, m.cmdErr
}

func (m *mockCoreClient) Subscribe(_ context.Context, _ *corev1.SubscribeRequest) (corev1.Core_SubscribeClient, error) {
	return m.subStream, m.subErr
}

func (m *mockCoreClient) Disconnect(_ context.Context, _ *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	return m.discResp, m.discErr
}

func TestHandler_Login_Success(t *testing.T) {
	client := &mockCoreClient{
		authResp: &corev1.AuthResponse{
			Success:       true,
			SessionId:     "sess-abc",
			CharacterName: "Guest-1",
		},
	}
	h := NewHandler(client)

	resp, err := h.Login(context.Background(), connect.NewRequest(&webv1.LoginRequest{
		Username: "guest",
		Password: "",
	}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
	assert.Equal(t, "sess-abc", resp.Msg.GetSessionId())
	assert.Equal(t, "Guest-1", resp.Msg.GetCharacterName())
	assert.Empty(t, resp.Msg.GetErrorMessage())
}

func TestHandler_Login_Failure(t *testing.T) {
	client := &mockCoreClient{
		authResp: &corev1.AuthResponse{
			Success: false,
			Error:   "invalid credentials",
		},
	}
	h := NewHandler(client)

	resp, err := h.Login(context.Background(), connect.NewRequest(&webv1.LoginRequest{
		Username: "user",
		Password: "wrong",
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.NotEmpty(t, resp.Msg.GetErrorMessage())
	assert.Empty(t, resp.Msg.GetSessionId())
}

func TestHandler_Login_RPCError(t *testing.T) {
	client := &mockCoreClient{
		authErr: errors.New("connection refused"),
	}
	h := NewHandler(client)

	resp, err := h.Login(context.Background(), connect.NewRequest(&webv1.LoginRequest{
		Username: "guest",
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.NotEmpty(t, resp.Msg.GetErrorMessage())
}

func TestHandler_SendCommand_Success(t *testing.T) {
	client := &mockCoreClient{
		cmdResp: &corev1.CommandResponse{
			Success: true,
			Output:  "You say, \"hello\"",
		},
	}
	h := NewHandler(client)

	resp, err := h.SendCommand(context.Background(), connect.NewRequest(&webv1.SendCommandRequest{
		SessionId: "sess-abc",
		Text:      "say hello",
	}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
	assert.Equal(t, "You say, \"hello\"", resp.Msg.GetOutput())
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
