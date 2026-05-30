// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package auth_test

import (
	"context"
	"errors"

	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/web"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// coreClientShim adapts an in-process *grpc.CoreServer to the web.CoreClient
// interface so integration tests can stand up the full gateway+core stack
// without HTTP transport. Each method delegates directly to the corresponding
// CoreServer method; gRPC framing is bypassed entirely.
//
// Add new methods here whenever web.CoreClient gains an RPC.
type coreClientShim struct {
	s *holoGRPC.CoreServer
}

func (c *coreClientShim) HandleCommand(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
	return c.s.HandleCommand(ctx, req)
}

// Subscribe is the streaming RPC. Auth/multi-tab tests do not exercise
// streaming; return an error so any accidental call surfaces immediately.
func (c *coreClientShim) Subscribe(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
	return nil, errors.New("subscribe not implemented in test shim")
}

func (c *coreClientShim) Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	return c.s.Disconnect(ctx, req)
}

func (c *coreClientShim) GetCommandHistory(ctx context.Context, req *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error) {
	return c.s.GetCommandHistory(ctx, req)
}

func (c *coreClientShim) AuthenticatePlayer(ctx context.Context, req *corev1.AuthenticatePlayerRequest) (*corev1.AuthenticatePlayerResponse, error) {
	return c.s.AuthenticatePlayer(ctx, req)
}

func (c *coreClientShim) SelectCharacter(ctx context.Context, req *corev1.SelectCharacterRequest) (*corev1.SelectCharacterResponse, error) {
	return c.s.SelectCharacter(ctx, req)
}

func (c *coreClientShim) CreatePlayer(ctx context.Context, req *corev1.CreatePlayerRequest) (*corev1.CreatePlayerResponse, error) {
	return c.s.CreatePlayer(ctx, req)
}

func (c *coreClientShim) CreateCharacter(ctx context.Context, req *corev1.CreateCharacterRequest) (*corev1.CreateCharacterResponse, error) {
	return c.s.CreateCharacter(ctx, req)
}

func (c *coreClientShim) ListCharacters(ctx context.Context, req *corev1.ListCharactersRequest) (*corev1.ListCharactersResponse, error) {
	return c.s.ListCharacters(ctx, req)
}

func (c *coreClientShim) RequestPasswordReset(ctx context.Context, req *corev1.RequestPasswordResetRequest) (*corev1.RequestPasswordResetResponse, error) {
	return c.s.RequestPasswordReset(ctx, req)
}

func (c *coreClientShim) ConfirmPasswordReset(ctx context.Context, req *corev1.ConfirmPasswordResetRequest) (*corev1.ConfirmPasswordResetResponse, error) {
	return c.s.ConfirmPasswordReset(ctx, req)
}

func (c *coreClientShim) Logout(ctx context.Context, req *corev1.LogoutRequest) (*corev1.LogoutResponse, error) {
	return c.s.Logout(ctx, req)
}

func (c *coreClientShim) CheckPlayerSession(ctx context.Context, req *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error) {
	return c.s.CheckPlayerSession(ctx, req)
}

func (c *coreClientShim) CreateGuest(ctx context.Context, req *corev1.CreateGuestRequest) (*corev1.CreateGuestResponse, error) {
	return c.s.CreateGuest(ctx, req)
}

func (c *coreClientShim) QueryStreamHistory(ctx context.Context, req *corev1.QueryStreamHistoryRequest) (*corev1.QueryStreamHistoryResponse, error) {
	return c.s.QueryStreamHistory(ctx, req)
}

func (c *coreClientShim) ListSessionStreams(ctx context.Context, req *corev1.ListSessionStreamsRequest) (*corev1.ListSessionStreamsResponse, error) {
	return c.s.ListSessionStreams(ctx, req)
}

func (c *coreClientShim) ListPlayerSessions(ctx context.Context, req *corev1.ListPlayerSessionsRequest) (*corev1.ListPlayerSessionsResponse, error) {
	return c.s.ListPlayerSessions(ctx, req)
}

func (c *coreClientShim) RevokePlayerSession(ctx context.Context, req *corev1.RevokePlayerSessionRequest) (*corev1.RevokePlayerSessionResponse, error) {
	return c.s.RevokePlayerSession(ctx, req)
}

func (c *coreClientShim) RevokeOtherPlayerSessions(ctx context.Context, req *corev1.RevokeOtherPlayerSessionsRequest) (*corev1.RevokeOtherPlayerSessionsResponse, error) {
	return c.s.RevokeOtherPlayerSessions(ctx, req)
}

func (c *coreClientShim) ListFocusPresence(ctx context.Context, req *corev1.ListFocusPresenceRequest) (*corev1.ListFocusPresenceResponse, error) {
	return c.s.ListFocusPresence(ctx, req)
}

func (c *coreClientShim) ListAvailableCommands(ctx context.Context, req *corev1.ListAvailableCommandsRequest) (*corev1.ListAvailableCommandsResponse, error) {
	return c.s.ListAvailableCommands(ctx, req)
}

// Compile-time check that coreClientShim satisfies the interface. If
// web.CoreClient gains a method, this fails to build until the shim is updated.
var _ web.CoreClient = (*coreClientShim)(nil)
