// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"

	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// TestWebHandlerLogsRPCErrorsWithStructuredOopsFields verifies that the core
// BFF handlers in handler.go log RPC failures via errutil.LogErrorContext (not
// a bare slog.ErrorContext), surfacing the oops Code() as a TOP-LEVEL attr
// while preserving the existing "session_id" structured attr.
//
// The discriminator is the same as the auth-handler suite: errutil emits a flat
// "code" attr, whereas slog.ErrorContext(ctx, msg, "session_id", id, "error",
// err) renders the oops LogValuer as a nested group under "error", leaving
// "code" absent at top level. session_id must survive the migration.
func TestWebHandlerLogsRPCErrorsWithStructuredOopsFields(t *testing.T) {
	const (
		wantCode = "WEB_CORE_RPC_ERR"
		sessID   = "sess-err"
	)

	tests := []struct {
		name    string
		wantMsg string
		setErr  func(m *mockCoreClient, err error)
		invoke  func(h *Handler)
	}{
		{
			name:    "SendCommand",
			wantMsg: "web: handle command RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.cmdErr = err },
			invoke: func(h *Handler) {
				_, _ = h.SendCommand(context.Background(),
					connect.NewRequest(&webv1.SendCommandRequest{SessionId: sessID, Text: "look"}))
			},
		},
		{
			name:    "Disconnect",
			wantMsg: "web: disconnect RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.discErr = err },
			invoke: func(h *Handler) {
				_, _ = h.Disconnect(context.Background(),
					connect.NewRequest(&webv1.DisconnectRequest{SessionId: sessID}))
			},
		},
		{
			name:    "GetCommandHistory",
			wantMsg: "web: get command history RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.cmdHistoryRPCErr = err },
			invoke: func(h *Handler) {
				_, _ = h.GetCommandHistory(context.Background(),
					connect.NewRequest(&webv1.GetCommandHistoryRequest{SessionId: sessID}))
			},
		},
		{
			name:    "WebQueryStreamHistory",
			wantMsg: "web: query stream history RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.queryStreamHistoryErr = err },
			invoke: func(h *Handler) {
				_, _ = h.WebQueryStreamHistory(context.Background(),
					connect.NewRequest(&webv1.WebQueryStreamHistoryRequest{SessionId: sessID}))
			},
		},
		{
			name:    "WebListSessionStreams",
			wantMsg: "web: list session streams RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.listSessionStreamsErr = err },
			invoke: func(h *Handler) {
				_, _ = h.WebListSessionStreams(context.Background(),
					connect.NewRequest(&webv1.WebListSessionStreamsRequest{SessionId: sessID}))
			},
		},
		{
			name:    "WebListFocusPresence",
			wantMsg: "web: list focus presence RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.listFocusPresenceErr = err },
			invoke: func(h *Handler) {
				_, _ = h.WebListFocusPresence(context.Background(),
					connect.NewRequest(&webv1.WebListFocusPresenceRequest{SessionId: sessID}))
			},
		},
		{
			name:    "WebListCommands",
			wantMsg: "web: list available commands RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.listAvailableCommandsErr = err },
			invoke: func(h *Handler) {
				_, _ = h.WebListCommands(context.Background(),
					connect.NewRequest(&webv1.WebListCommandsRequest{SessionId: sessID}))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" logs the core error with a top-level oops code", func(t *testing.T) {
			client := &mockCoreClient{}
			tt.setErr(client, oops.Code(wantCode).Errorf("core unreachable"))
			h := NewHandler(client)

			entry := captureSingleHandlerLog(t, func() { tt.invoke(h) })

			assert.Equal(t, "ERROR", entry["level"])
			assert.Equal(t, tt.wantMsg, entry["msg"])
			assert.Equal(t, wantCode, entry["code"],
				"oops code must be a top-level attr (errutil.LogErrorContext), not nested under error")
			assert.Equal(t, sessID, entry["session_id"],
				"handler's structured attrs must survive the migration")
		})
	}
}
