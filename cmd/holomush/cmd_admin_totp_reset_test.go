// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
)

// fakeAdminHandler is a test double for AdminServiceHandler.
// Each method field can be set independently; unset methods return CodeUnimplemented.
type fakeAdminHandler struct {
	adminv1connect.UnimplementedAdminServiceHandler
	onAuthenticate func(context.Context, *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error)
	onResetTOTP    func(context.Context, *connect.Request[adminv1.ResetTOTPRequest]) (*connect.Response[adminv1.ResetTOTPResponse], error)
	onApprove      func(context.Context, *connect.Request[adminv1.ApproveRequest]) (*connect.Response[adminv1.ApproveResponse], error)
}

func (f *fakeAdminHandler) Authenticate(ctx context.Context, req *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
	if f.onAuthenticate != nil {
		return f.onAuthenticate(ctx, req)
	}
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

func (f *fakeAdminHandler) ResetTOTP(ctx context.Context, req *connect.Request[adminv1.ResetTOTPRequest]) (*connect.Response[adminv1.ResetTOTPResponse], error) {
	if f.onResetTOTP != nil {
		return f.onResetTOTP(ctx, req)
	}
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

func (f *fakeAdminHandler) Approve(ctx context.Context, req *connect.Request[adminv1.ApproveRequest]) (*connect.Response[adminv1.ApproveResponse], error) {
	if f.onApprove != nil {
		return f.onApprove(ctx, req)
	}
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// newFakeAdminServer starts an httptest.Server backed by h and returns a
// ConnectRPC client and cleanup func.
func newFakeAdminServer(t *testing.T, h adminv1connect.AdminServiceHandler) (adminv1connect.AdminServiceClient, func()) {
	t.Helper()
	mux := http.NewServeMux()
	path, handler := adminv1connect.NewAdminServiceHandler(h)
	mux.Handle(path, handler)
	srv := httptest.NewServer(mux)
	client := adminv1connect.NewAdminServiceClient(srv.Client(), srv.URL)
	return client, srv.Close
}

// newTestCmdWithIO builds a cobra command wired to the given stdin string and
// captures stdout in the returned *bytes.Buffer.
func newTestCmdWithIO(stdinContent string) (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "test"}
	cmd.SetIn(strings.NewReader(stdinContent))
	var out bytes.Buffer
	cmd.SetOut(&out)
	return cmd, &out
}

// --- Tests for runAdminTOTPReset ---

func TestRunAdminTOTPResetClearedTrue(t *testing.T) {
	// Arrange: server that authenticates and reports cleared=true.
	h := &fakeAdminHandler{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return connect.NewResponse(&adminv1.AuthenticateResponse{SessionToken: "tok-abc"}), nil
		},
		onResetTOTP: func(_ context.Context, req *connect.Request[adminv1.ResetTOTPRequest]) (*connect.Response[adminv1.ResetTOTPResponse], error) {
			assert.Equal(t, "tok-abc", req.Msg.GetSessionToken())
			assert.Equal(t, "01JXXXXXXXXXXXXXXXXXXX0001", req.Msg.GetTargetPlayerId())
			return connect.NewResponse(&adminv1.ResetTOTPResponse{Cleared: true}), nil
		},
	}
	client, cleanup := newFakeAdminServer(t, h)
	defer cleanup()

	// stdin: username \n password \n TOTP code \n
	cmd, out := newTestCmdWithIO("operator\nsecret\n123456\n")
	cmd.SetContext(t.Context())

	err := runAdminTOTPReset(cmd, client, "01JXXXXXXXXXXXXXXXXXXX0001")
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Cleared TOTP enrollment for player")
}

func TestRunAdminTOTPResetClearedFalse(t *testing.T) {
	// Arrange: player was not enrolled; server reports cleared=false.
	h := &fakeAdminHandler{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return connect.NewResponse(&adminv1.AuthenticateResponse{SessionToken: "tok-xyz"}), nil
		},
		onResetTOTP: func(_ context.Context, _ *connect.Request[adminv1.ResetTOTPRequest]) (*connect.Response[adminv1.ResetTOTPResponse], error) {
			return connect.NewResponse(&adminv1.ResetTOTPResponse{Cleared: false}), nil
		},
	}
	client, cleanup := newFakeAdminServer(t, h)
	defer cleanup()

	cmd, out := newTestCmdWithIO("operator\nsecret\n654321\n")
	cmd.SetContext(t.Context())

	err := runAdminTOTPReset(cmd, client, "01JXXXXXXXXXXXXXXXXXXX0002")
	require.NoError(t, err)
	assert.Contains(t, out.String(), "was not enrolled")
}

func TestRunAdminTOTPResetAuthFails(t *testing.T) {
	// Arrange: Authenticate returns Unauthenticated.
	h := &fakeAdminHandler{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		},
	}
	client, cleanup := newFakeAdminServer(t, h)
	defer cleanup()

	cmd, _ := newTestCmdWithIO("operator\nbadpass\n000000\n")
	cmd.SetContext(t.Context())

	err := runAdminTOTPReset(cmd, client, "01JXXXXXXXXXXXXXXXXXXX0003")
	require.Error(t, err)
}

func TestRunAdminTOTPResetResetFails(t *testing.T) {
	// Arrange: Authenticate succeeds but ResetTOTP returns PermissionDenied
	// (e.g., operator is not authorised to reset other players' TOTP).
	h := &fakeAdminHandler{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return connect.NewResponse(&adminv1.AuthenticateResponse{SessionToken: "tok-ok"}), nil
		},
		onResetTOTP: func(_ context.Context, _ *connect.Request[adminv1.ResetTOTPRequest]) (*connect.Response[adminv1.ResetTOTPResponse], error) {
			return nil, connect.NewError(connect.CodePermissionDenied, nil)
		},
	}
	client, cleanup := newFakeAdminServer(t, h)
	defer cleanup()

	cmd, _ := newTestCmdWithIO("operator\nok\n123456\n")
	cmd.SetContext(t.Context())

	err := runAdminTOTPReset(cmd, client, "01JXXXXXXXXXXXXXXXXXXX0004")
	require.Error(t, err)
}
