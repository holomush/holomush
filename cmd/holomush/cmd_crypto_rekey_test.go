// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"context"
	"io"
	"testing"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
)

// --- fakeRekeyStreamReader implements RekeyStreamReader for tests ---

// fakeRekeyStreamReader is a test double for *connect.ServerStreamForClient.
// It yields pre-loaded messages then terminates: if termErr is nil the stream
// ends cleanly; otherwise Err() returns termErr.
type fakeRekeyStreamReader struct {
	msgs    []*adminv1.RekeyProgress
	pos     int
	termErr error // error returned by Err() after stream exhaustion
	current *adminv1.RekeyProgress
}

func newFakeRekeyStream(msgs ...*adminv1.RekeyProgress) *fakeRekeyStreamReader {
	return &fakeRekeyStreamReader{msgs: msgs}
}

// Receive implements RekeyStreamReader.
func (f *fakeRekeyStreamReader) Receive() bool {
	if f.pos < len(f.msgs) {
		f.current = f.msgs[f.pos]
		f.pos++
		return true
	}
	return false
}

// Msg implements RekeyStreamReader.
func (f *fakeRekeyStreamReader) Msg() *adminv1.RekeyProgress {
	return f.current
}

// Err implements RekeyStreamReader.
func (f *fakeRekeyStreamReader) Err() error {
	return f.termErr
}

// --- fakeAdminHandlerWithRekey implements adminv1connect.AdminServiceHandler ---

// fakeAdminHandlerWithRekey extends the unimplemented handler with Authenticate,
// Rekey, and RekeyResume streaming so unit tests can exercise the full
// happy/error paths of runRekeyFresh, runRekeyResume, and streamProgress
// without a live server.
type fakeAdminHandlerWithRekey struct {
	adminv1connect.UnimplementedAdminServiceHandler
	onAuthenticate func(context.Context, *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error)
	onRekey        func(context.Context, *connect.Request[adminv1.RekeyRequest], *connect.ServerStream[adminv1.RekeyProgress]) error
	onRekeyResume  func(context.Context, *connect.Request[adminv1.RekeyResumeRequest], *connect.ServerStream[adminv1.RekeyProgress]) error
}

func (f *fakeAdminHandlerWithRekey) Authenticate(
	ctx context.Context,
	req *connect.Request[adminv1.AuthenticateRequest],
) (*connect.Response[adminv1.AuthenticateResponse], error) {
	if f.onAuthenticate != nil {
		return f.onAuthenticate(ctx, req)
	}
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

func (f *fakeAdminHandlerWithRekey) Rekey(
	ctx context.Context,
	req *connect.Request[adminv1.RekeyRequest],
	stream *connect.ServerStream[adminv1.RekeyProgress],
) error {
	if f.onRekey != nil {
		return f.onRekey(ctx, req, stream)
	}
	return connect.NewError(connect.CodeUnimplemented, nil)
}

func (f *fakeAdminHandlerWithRekey) RekeyResume(
	ctx context.Context,
	req *connect.Request[adminv1.RekeyResumeRequest],
	stream *connect.ServerStream[adminv1.RekeyProgress],
) error {
	if f.onRekeyResume != nil {
		return f.onRekeyResume(ctx, req, stream)
	}
	return connect.NewError(connect.CodeUnimplemented, nil)
}

// newFakeAdminServerWithRekey starts an httptest.Server backed by h and returns
// a ConnectRPC client and cleanup func.
func newFakeAdminServerWithRekey(t *testing.T, h adminv1connect.AdminServiceHandler) (adminv1connect.AdminServiceClient, func()) {
	t.Helper()
	return newFakeAdminServer(t, h)
}

// --- Tests for runRekeyFresh ---

// TestCmd_CryptoRekey_RequiresJustification is TDD acceptance criterion #1:
// omitting --justification must produce an error containing
// "--justification is required".
func TestCmd_CryptoRekey_RequiresJustification(t *testing.T) {
	cmd, _ := newTestCmdWithIO("")
	cmd.SetContext(t.Context())
	cmd.Flags().String("justification", "", "")
	cmd.Flags().Bool("dual-control", false, "")
	cmd.Flags().Bool("no-progress", false, "")
	// Do NOT set --justification.

	factory := func() (adminv1connect.AdminServiceClient, error) { return nil, nil }
	err := runRekeyFresh(cmd, factory, "scene:01ABC")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--justification is required")
}

// TestCmd_CryptoRekey_PrintsProgress is TDD acceptance criterion #2: the
// happy path authenticates, calls Rekey, renders phases and "Rekey complete".
func TestCmd_CryptoRekey_PrintsProgress(t *testing.T) {
	reqID := [16]byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
	}

	h := &fakeAdminHandlerWithRekey{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return connect.NewResponse(&adminv1.AuthenticateResponse{SessionToken: "tok-rekey"}), nil
		},
		onRekey: func(_ context.Context, req *connect.Request[adminv1.RekeyRequest], stream *connect.ServerStream[adminv1.RekeyProgress]) error {
			assert.Equal(t, "tok-rekey", req.Msg.GetSessionToken())
			assert.Equal(t, "scene", req.Msg.GetContextType())
			assert.Equal(t, "01ABC", req.Msg.GetContextId())
			assert.Equal(t, "test reason", req.Msg.GetJustification())

			if err := stream.Send(&adminv1.RekeyProgress{
				Event: &adminv1.RekeyProgress_PhaseStarted{
					PhaseStarted: &adminv1.PhaseStarted{Phase: "1"},
				},
			}); err != nil {
				return err
			}
			return stream.Send(&adminv1.RekeyProgress{
				Event: &adminv1.RekeyProgress_Completed{
					Completed: &adminv1.RekeyCompleted{
						RequestId:  reqID[:],
						DurationMs: 1234,
					},
				},
			})
		},
	}
	client, cleanup := newFakeAdminServerWithRekey(t, h)
	defer cleanup()

	cmd, out := newTestCmdWithIO("operator\nsecret\n123456\n")
	cmd.SetContext(t.Context())
	cmd.Flags().String("justification", "", "")
	cmd.Flags().Bool("dual-control", false, "")
	cmd.Flags().Bool("no-progress", false, "")
	require.NoError(t, cmd.Flags().Set("justification", "test reason"))

	factory := func() (adminv1connect.AdminServiceClient, error) { return client, nil }
	err := runRekeyFresh(cmd, factory, "scene:01ABC")
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Rekey complete")
}

// TestCmd_CryptoRekey_AuthFailure verifies that an authentication failure
// returns a non-nil error.
func TestCmd_CryptoRekey_AuthFailure(t *testing.T) {
	h := &fakeAdminHandlerWithRekey{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		},
	}
	client, cleanup := newFakeAdminServerWithRekey(t, h)
	defer cleanup()

	cmd, _ := newTestCmdWithIO("operator\nbadpass\n000000\n")
	cmd.SetContext(t.Context())
	cmd.Flags().String("justification", "", "")
	cmd.Flags().Bool("dual-control", false, "")
	cmd.Flags().Bool("no-progress", false, "")
	require.NoError(t, cmd.Flags().Set("justification", "test reason"))

	factory := func() (adminv1connect.AdminServiceClient, error) { return client, nil }
	err := runRekeyFresh(cmd, factory, "scene:01ABC")
	require.Error(t, err)
}

// TestCmd_CryptoRekey_OrchestratorError verifies that a RekeyError progress
// event is surfaced as a non-nil error containing the code.
func TestCmd_CryptoRekey_OrchestratorError(t *testing.T) {
	h := &fakeAdminHandlerWithRekey{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return connect.NewResponse(&adminv1.AuthenticateResponse{SessionToken: "tok-ok"}), nil
		},
		onRekey: func(_ context.Context, _ *connect.Request[adminv1.RekeyRequest], stream *connect.ServerStream[adminv1.RekeyProgress]) error {
			return stream.Send(&adminv1.RekeyProgress{
				Event: &adminv1.RekeyProgress_Error{
					Error: &adminv1.RekeyError{
						Code:    "DEK_REKEY_ALREADY_IN_PROGRESS",
						Message: "a rekey is already in progress for this context",
					},
				},
			})
		},
	}
	client, cleanup := newFakeAdminServerWithRekey(t, h)
	defer cleanup()

	cmd, _ := newTestCmdWithIO("operator\nsecret\n123456\n")
	cmd.SetContext(t.Context())
	cmd.Flags().String("justification", "", "")
	cmd.Flags().Bool("dual-control", false, "")
	cmd.Flags().Bool("no-progress", false, "")
	require.NoError(t, cmd.Flags().Set("justification", "test reason"))

	factory := func() (adminv1connect.AdminServiceClient, error) { return client, nil }
	err := runRekeyFresh(cmd, factory, "scene:01ABC")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DEK_REKEY_ALREADY_IN_PROGRESS")
}

// TestCmd_CryptoRekey_MissingArgs verifies that a malformed context reference
// (no colon separator) returns an error containing "context must be".
func TestCmd_CryptoRekey_MissingArgs(t *testing.T) {
	cmd, _ := newTestCmdWithIO("")
	cmd.SetContext(t.Context())
	cmd.Flags().String("justification", "", "")
	cmd.Flags().Bool("dual-control", false, "")
	cmd.Flags().Bool("no-progress", false, "")
	require.NoError(t, cmd.Flags().Set("justification", "test reason"))

	factory := func() (adminv1connect.AdminServiceClient, error) { return nil, nil }
	err := runRekeyFresh(cmd, factory, "badref")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context must be")
}

// --- Tests for streamProgress (unit, using fakeRekeyStreamReader) ---

// TestStreamProgress_CompletedMessage verifies the happy path renders
// "Rekey complete" and returns nil.
func TestStreamProgress_CompletedMessage(t *testing.T) {
	reqID := []byte{0x01, 0x02}
	stream := newFakeRekeyStream(
		&adminv1.RekeyProgress{
			Event: &adminv1.RekeyProgress_PhaseStarted{
				PhaseStarted: &adminv1.PhaseStarted{Phase: "1"},
			},
		},
		&adminv1.RekeyProgress{
			Event: &adminv1.RekeyProgress_Completed{
				Completed: &adminv1.RekeyCompleted{
					RequestId:  reqID,
					DurationMs: 500,
				},
			},
		},
	)
	var buf bytes.Buffer
	err := streamProgress(stream, false, &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Rekey complete")
}

// TestStreamProgress_ErrorEvent verifies that a RekeyError event surfaces as
// a non-nil error containing the code.
func TestStreamProgress_ErrorEvent(t *testing.T) {
	stream := newFakeRekeyStream(
		&adminv1.RekeyProgress{
			Event: &adminv1.RekeyProgress_Error{
				Error: &adminv1.RekeyError{
					Code:    "DEK_REKEY_PHASE7_AUDIT_FAILED",
					Message: "audit emit failed",
				},
			},
		},
	)
	err := streamProgress(stream, false, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DEK_REKEY_PHASE7_AUDIT_FAILED")
}

// TestStreamProgress_NoProgress verifies that noProgress=true suppresses phase
// output but returns nil on Completed.
func TestStreamProgress_NoProgress(t *testing.T) {
	reqID := []byte{0xab, 0xcd}
	stream := newFakeRekeyStream(
		&adminv1.RekeyProgress{
			Event: &adminv1.RekeyProgress_PhaseStarted{
				PhaseStarted: &adminv1.PhaseStarted{Phase: "1"},
			},
		},
		&adminv1.RekeyProgress{
			Event: &adminv1.RekeyProgress_Completed{
				Completed: &adminv1.RekeyCompleted{RequestId: reqID},
			},
		},
	)
	var buf bytes.Buffer
	err := streamProgress(stream, true, &buf)
	require.NoError(t, err)
	// noProgress=true: phase lines suppressed; only "Rekey complete" should appear.
	assert.NotContains(t, buf.String(), "Phase 1 started")
	assert.Contains(t, buf.String(), "Rekey complete")
}

// TestStreamProgress_Phase3Progress verifies Phase3Progress events are consumed
// without error before a Completed event.
func TestStreamProgress_Phase3Progress(t *testing.T) {
	reqID := []byte{0x01}
	stream := newFakeRekeyStream(
		&adminv1.RekeyProgress{
			Event: &adminv1.RekeyProgress_Phase3Progress{
				Phase3Progress: &adminv1.Phase3Progress{RowsRewritten: 500},
			},
		},
		&adminv1.RekeyProgress{
			Event: &adminv1.RekeyProgress_Completed{
				Completed: &adminv1.RekeyCompleted{RequestId: reqID},
			},
		},
	)
	var buf bytes.Buffer
	err := streamProgress(stream, false, &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "500 rows rewritten")
}

// TestStreamProgress_TransportError verifies that a transport error (termErr
// set on the fake stream) surfaces as a non-nil error.
func TestStreamProgress_TransportError(t *testing.T) {
	stream := &fakeRekeyStreamReader{
		termErr: connect.NewError(connect.CodeUnavailable, nil),
	}
	err := streamProgress(stream, false, io.Discard)
	require.Error(t, err)
}

// --- Tests for cobra command tree ---

// TestNewCryptoRekeyCmd_SubcommandsRegistered verifies that newCryptoRekeyCmd
// registers the four expected sub-subcommands.
func TestNewCryptoRekeyCmd_SubcommandsRegistered(t *testing.T) {
	factory := func() (adminv1connect.AdminServiceClient, error) { return nil, nil }
	cmd := newCryptoRekeyCmd(factory)
	names := make(map[string]struct{})
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = struct{}{}
	}
	for _, want := range []string{"resume", "abort", "status", "list"} {
		assert.Contains(t, names, want, "missing sub-subcommand %q", want)
	}
}

// TestNewCryptoCmdRegisteredInRoot verifies that the crypto parent command is
// reachable from the root command tree.
func TestNewCryptoCmdRegisteredInRoot(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"crypto"})
	assert.NoError(t, err)
	assert.Equal(t, "crypto", cmd.Name())
}

// TestNewCryptoCmdRekeySubcmdRegistered verifies the rekey subcommand is
// reachable from `holomush crypto rekey`.
func TestNewCryptoCmdRekeySubcmdRegistered(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"crypto", "rekey"})
	assert.NoError(t, err)
	assert.Equal(t, "rekey", cmd.Name())
}

// --- Tests for mapToExitCodeErr (INV-E23) ---

// TestMapToExitCodeErr_TEMPFAIL verifies DEK_REKEY_PHASE5_TIMEOUT → exitCode 75.
func TestMapToExitCodeErr_TEMPFAIL(t *testing.T) {
	input := &rekeyProgressError{code: "DEK_REKEY_PHASE5_TIMEOUT", msg: "timeout"}
	err := mapToExitCodeErr(input)
	var exitErr *exitCodeError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 75, exitErr.exitCode)
}

// TestMapToExitCodeErr_CANTCREAT verifies conflict codes → exitCode 73.
func TestMapToExitCodeErr_CANTCREAT(t *testing.T) {
	for _, code := range []string{"DEK_REKEY_ALREADY_IN_PROGRESS", "DEK_REKEY_ARGS_CONFLICT"} {
		input := &rekeyProgressError{code: code, msg: "conflict"}
		err := mapToExitCodeErr(input)
		var exitErr *exitCodeError
		require.ErrorAs(t, err, &exitErr, "code=%s", code)
		assert.Equal(t, 73, exitErr.exitCode, "code=%s", code)
	}
}

// TestMapToExitCodeErr_SOFTWARE verifies audit failure → exitCode 70.
func TestMapToExitCodeErr_SOFTWARE(t *testing.T) {
	input := &rekeyProgressError{code: "DEK_REKEY_PHASE7_AUDIT_FAILED", msg: "audit fail"}
	err := mapToExitCodeErr(input)
	var exitErr *exitCodeError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 70, exitErr.exitCode)
}

// TestMapToExitCodeErr_NOPERM verifies auth denial codes → exitCode 77.
func TestMapToExitCodeErr_NOPERM(t *testing.T) {
	for _, code := range []string{"DENY_SESSION_INVALID", "DENY_SESSION_EXPIRED", "DENY_CAPABILITY"} {
		input := &rekeyProgressError{code: code, msg: "denied"}
		err := mapToExitCodeErr(input)
		var exitErr *exitCodeError
		require.ErrorAs(t, err, &exitErr, "code=%s", code)
		assert.Equal(t, 77, exitErr.exitCode, "code=%s", code)
	}
}

// TestMapToExitCodeErr_Unknown verifies unknown codes pass through unchanged
// (not wrapped as exitCodeError).
func TestMapToExitCodeErr_Unknown(t *testing.T) {
	input := &rekeyProgressError{code: "SOME_OTHER_CODE", msg: "other"}
	err := mapToExitCodeErr(input)
	require.Error(t, err)
	var exitErr *exitCodeError
	assert.False(t, assert.ObjectsAreEqual(exitErr, err))
	assert.ErrorIs(t, err, input)
}

// --- Tests for runRekeyResume (bead holomush-jxo8.7.32) ---

// setupRekeyResumeServerWithCompleted builds a fake server that authenticates
// and responds to RekeyResume with a Completed event.  The onRekeyResume
// callback receives the request so callers can assert on ForceDestroy.
func setupRekeyResumeServerWithCompleted(
	t *testing.T,
	onRekeyResume func(*testing.T, *connect.Request[adminv1.RekeyResumeRequest], *connect.ServerStream[adminv1.RekeyProgress]) error,
) (adminv1connect.AdminServiceClient, func()) {
	t.Helper()
	reqID := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	h := &fakeAdminHandlerWithRekey{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return connect.NewResponse(&adminv1.AuthenticateResponse{SessionToken: "tok-resume"}), nil
		},
		onRekeyResume: func(_ context.Context, req *connect.Request[adminv1.RekeyResumeRequest], stream *connect.ServerStream[adminv1.RekeyProgress]) error {
			if onRekeyResume != nil {
				return onRekeyResume(t, req, stream)
			}
			return stream.Send(&adminv1.RekeyProgress{
				Event: &adminv1.RekeyProgress_Completed{
					Completed: &adminv1.RekeyCompleted{
						RequestId:  reqID,
						DurationMs: 200,
						Resumed:    true,
					},
				},
			})
		},
	}
	return newFakeAdminServerWithRekey(t, h)
}

// TestCmd_CryptoRekey_Resume_ForceDestroy_RequiresConfirmation is TDD
// acceptance criterion: in non-TTY mode (stdin is strings.NewReader, not
// *os.File), --force-destroy without --confirm must return an error with
// exit code 64 (EX_USAGE) and contain "--confirm required".
func TestCmd_CryptoRekey_Resume_ForceDestroy_RequiresConfirmation(t *testing.T) {
	client, cleanup := setupRekeyResumeServerWithCompleted(t, nil)
	defer cleanup()

	factory := func() (adminv1connect.AdminServiceClient, error) { return client, nil }

	// stdin is strings.NewReader — not *os.File — so isTTY=false path fires.
	cmd, _ := newTestCmdWithIO("operator\nsecret\n123456\n")
	cmd.SetContext(t.Context())
	cmd.Flags().Bool("force-destroy", false, "")
	cmd.Flags().String("confirm", "", "")
	require.NoError(t, cmd.Flags().Set("force-destroy", "true"))
	// No --confirm flag set.

	err := runRekeyResume(cmd, factory, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	require.Error(t, err)
	var exitErr *exitCodeError
	require.ErrorAs(t, err, &exitErr, "expected exitCodeError")
	assert.Equal(t, 64, exitErr.exitCode, "must exit 64 EX_USAGE")
	assert.Contains(t, exitErr.Error(), "--confirm required")
}

// TestCmd_CryptoRekey_Resume_ForceDestroy_WithConfirm is TDD acceptance
// criterion: --force-destroy with matching --confirm passes ForceDestroy=true
// to the RPC and exits 0 with "Rekey complete".
func TestCmd_CryptoRekey_Resume_ForceDestroy_WithConfirm(t *testing.T) {
	var capturedForceDestroy bool
	reqID := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}

	client, cleanup := setupRekeyResumeServerWithCompleted(t,
		func(_ *testing.T, req *connect.Request[adminv1.RekeyResumeRequest], stream *connect.ServerStream[adminv1.RekeyProgress]) error {
			capturedForceDestroy = req.Msg.GetForceDestroy()
			return stream.Send(&adminv1.RekeyProgress{
				Event: &adminv1.RekeyProgress_Completed{
					Completed: &adminv1.RekeyCompleted{
						RequestId:        reqID,
						DurationMs:       300,
						Resumed:          true,
						ForceDestroyUsed: true,
					},
				},
			})
		})
	defer cleanup()

	factory := func() (adminv1connect.AdminServiceClient, error) { return client, nil }

	cmd, out := newTestCmdWithIO("operator\nsecret\n123456\n")
	cmd.SetContext(t.Context())
	cmd.Flags().Bool("force-destroy", false, "")
	cmd.Flags().String("confirm", "", "")
	require.NoError(t, cmd.Flags().Set("force-destroy", "true"))
	require.NoError(t, cmd.Flags().Set("confirm", "scene:01ABC"))

	err := runRekeyResume(cmd, factory, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Rekey complete")
	assert.True(t, capturedForceDestroy, "ForceDestroy must be true in RPC request")
}

// TestCmd_CryptoRekey_Resume_HappyPath verifies that a plain resume (no
// force-destroy) authenticates, calls RekeyResume with ForceDestroy=false,
// and renders "Rekey complete".
func TestCmd_CryptoRekey_Resume_HappyPath(t *testing.T) {
	var capturedForceDestroy bool
	reqID := []byte{0xab, 0xcd}

	client, cleanup := setupRekeyResumeServerWithCompleted(t,
		func(_ *testing.T, req *connect.Request[adminv1.RekeyResumeRequest], stream *connect.ServerStream[adminv1.RekeyProgress]) error {
			capturedForceDestroy = req.Msg.GetForceDestroy()
			return stream.Send(&adminv1.RekeyProgress{
				Event: &adminv1.RekeyProgress_Completed{
					Completed: &adminv1.RekeyCompleted{RequestId: reqID, Resumed: true},
				},
			})
		})
	defer cleanup()

	factory := func() (adminv1connect.AdminServiceClient, error) { return client, nil }

	cmd, out := newTestCmdWithIO("operator\nsecret\n123456\n")
	cmd.SetContext(t.Context())
	cmd.Flags().Bool("force-destroy", false, "")
	cmd.Flags().String("confirm", "", "")
	// force-destroy is false (default)

	err := runRekeyResume(cmd, factory, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Rekey complete")
	assert.False(t, capturedForceDestroy, "ForceDestroy must be false in plain resume")
}

// TestCmd_CryptoRekey_Resume_AlreadyComplete verifies that a RekeyResume RPC
// returning a Completed event is idempotent from the CLI's perspective (exit 0).
func TestCmd_CryptoRekey_Resume_AlreadyComplete(t *testing.T) {
	reqID := []byte{0xde, 0xad, 0xbe, 0xef}
	h := &fakeAdminHandlerWithRekey{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return connect.NewResponse(&adminv1.AuthenticateResponse{SessionToken: "tok-resume"}), nil
		},
		onRekeyResume: func(_ context.Context, _ *connect.Request[adminv1.RekeyResumeRequest], stream *connect.ServerStream[adminv1.RekeyProgress]) error {
			// Handler returns completed immediately — checkpoint was already done.
			return stream.Send(&adminv1.RekeyProgress{
				Event: &adminv1.RekeyProgress_Completed{
					Completed: &adminv1.RekeyCompleted{RequestId: reqID, Resumed: true},
				},
			})
		},
	}
	client, cleanup := newFakeAdminServerWithRekey(t, h)
	defer cleanup()

	factory := func() (adminv1connect.AdminServiceClient, error) { return client, nil }
	cmd, out := newTestCmdWithIO("operator\nsecret\n123456\n")
	cmd.SetContext(t.Context())
	cmd.Flags().Bool("force-destroy", false, "")
	cmd.Flags().String("confirm", "", "")

	err := runRekeyResume(cmd, factory, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Rekey complete")
}

// TestCmd_CryptoRekey_Resume_NonExistentID verifies that a server-returned
// DEK_REKEY_CHECKPOINT_NOT_FOUND error surfaces from runRekeyResume.
func TestCmd_CryptoRekey_Resume_NonExistentID(t *testing.T) {
	h := &fakeAdminHandlerWithRekey{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return connect.NewResponse(&adminv1.AuthenticateResponse{SessionToken: "tok-resume"}), nil
		},
		onRekeyResume: func(_ context.Context, _ *connect.Request[adminv1.RekeyResumeRequest], stream *connect.ServerStream[adminv1.RekeyProgress]) error {
			return stream.Send(&adminv1.RekeyProgress{
				Event: &adminv1.RekeyProgress_Error{
					Error: &adminv1.RekeyError{
						Code:    "DEK_REKEY_CHECKPOINT_NOT_FOUND",
						Message: "no checkpoint found for request_id",
					},
				},
			})
		},
	}
	client, cleanup := newFakeAdminServerWithRekey(t, h)
	defer cleanup()

	factory := func() (adminv1connect.AdminServiceClient, error) { return client, nil }
	cmd, _ := newTestCmdWithIO("operator\nsecret\n123456\n")
	cmd.SetContext(t.Context())
	cmd.Flags().Bool("force-destroy", false, "")
	cmd.Flags().String("confirm", "", "")

	err := runRekeyResume(cmd, factory, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DEK_REKEY_CHECKPOINT_NOT_FOUND")
}

// TestCmd_CryptoRekey_Resume_AuthFailure verifies that authentication failure
// surfaces as a non-nil error from runRekeyResume.
func TestCmd_CryptoRekey_Resume_AuthFailure(t *testing.T) {
	h := &fakeAdminHandlerWithRekey{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		},
	}
	client, cleanup := newFakeAdminServerWithRekey(t, h)
	defer cleanup()

	factory := func() (adminv1connect.AdminServiceClient, error) { return client, nil }
	cmd, _ := newTestCmdWithIO("operator\nbadpass\n000000\n")
	cmd.SetContext(t.Context())
	cmd.Flags().Bool("force-destroy", false, "")
	cmd.Flags().String("confirm", "", "")

	err := runRekeyResume(cmd, factory, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	require.Error(t, err)
}

// TestCmd_CryptoRekey_Resume_ForceDestroy_PassThroughToRPC verifies that
// ForceDestroy=true is passed through to RekeyResumeRequest when --confirm
// is provided (non-TTY path).  The server asserts the field directly.
func TestCmd_CryptoRekey_Resume_ForceDestroy_PassThroughToRPC(t *testing.T) {
	var gotForceDestroy bool
	reqID := []byte{0x01}
	h := &fakeAdminHandlerWithRekey{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return connect.NewResponse(&adminv1.AuthenticateResponse{SessionToken: "tok"}), nil
		},
		onRekeyResume: func(_ context.Context, req *connect.Request[adminv1.RekeyResumeRequest], stream *connect.ServerStream[adminv1.RekeyProgress]) error {
			gotForceDestroy = req.Msg.GetForceDestroy()
			return stream.Send(&adminv1.RekeyProgress{
				Event: &adminv1.RekeyProgress_Completed{
					Completed: &adminv1.RekeyCompleted{RequestId: reqID},
				},
			})
		},
	}
	client, cleanup := newFakeAdminServerWithRekey(t, h)
	defer cleanup()

	factory := func() (adminv1connect.AdminServiceClient, error) { return client, nil }
	cmd, _ := newTestCmdWithIO("operator\nsecret\n123456\n")
	cmd.SetContext(t.Context())
	cmd.Flags().Bool("force-destroy", false, "")
	cmd.Flags().String("confirm", "", "")
	require.NoError(t, cmd.Flags().Set("force-destroy", "true"))
	require.NoError(t, cmd.Flags().Set("confirm", "scene:01ABC"))

	err := runRekeyResume(cmd, factory, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	require.NoError(t, err)
	assert.True(t, gotForceDestroy, "ForceDestroy must be true in RekeyResumeRequest")
}

// TestCmd_CryptoRekey_Resume_InvalidRequestID verifies that a malformed
// request_id (not ULID, not 32-char hex) returns a parse error before
// hitting the server.
func TestCmd_CryptoRekey_Resume_InvalidRequestID(t *testing.T) {
	factory := func() (adminv1connect.AdminServiceClient, error) {
		return nil, nil // should never be called
	}
	cmd, _ := newTestCmdWithIO("")
	cmd.SetContext(t.Context())
	cmd.Flags().Bool("force-destroy", false, "")
	cmd.Flags().String("confirm", "", "")

	err := runRekeyResume(cmd, factory, "not-a-valid-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request_id")
}

// TestCmd_CryptoRekey_Resume_Registered verifies that the `resume` sub-subcommand
// appears in the rekey command tree with its expected flags.
func TestCmd_CryptoRekey_Resume_Registered(t *testing.T) {
	factory := func() (adminv1connect.AdminServiceClient, error) { return nil, nil }
	rekeyCmd := newCryptoRekeyCmd(factory)

	var resumeCmd *cobra.Command
	for _, sub := range rekeyCmd.Commands() {
		if sub.Name() == "resume" {
			resumeCmd = sub
			break
		}
	}
	require.NotNil(t, resumeCmd, "resume sub-subcommand must be registered")
	assert.NotNil(t, resumeCmd.Flags().Lookup("force-destroy"), "must have --force-destroy flag")
	assert.NotNil(t, resumeCmd.Flags().Lookup("confirm"), "must have --confirm flag")
}

// TestCmd_CryptoRekey_Resume_ForceDestroy_IgnoredWhenStatusNotTimeout verifies
// that DEK_REKEY_FORCE_DESTROY_FORBIDDEN from the server surfaces as an error
// (INV-E10: server-side guard).  The CLI passes the flag through; the server
// rejects it.
func TestCmd_CryptoRekey_Resume_ForceDestroy_RejectedByServer(t *testing.T) {
	h := &fakeAdminHandlerWithRekey{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return connect.NewResponse(&adminv1.AuthenticateResponse{SessionToken: "tok"}), nil
		},
		onRekeyResume: func(_ context.Context, _ *connect.Request[adminv1.RekeyResumeRequest], stream *connect.ServerStream[adminv1.RekeyProgress]) error {
			return stream.Send(&adminv1.RekeyProgress{
				Event: &adminv1.RekeyProgress_Error{
					Error: &adminv1.RekeyError{
						Code:    "DEK_REKEY_FORCE_DESTROY_FORBIDDEN",
						Message: "force_destroy requires status=phase5_timeout",
					},
				},
			})
		},
	}
	client, cleanup := newFakeAdminServerWithRekey(t, h)
	defer cleanup()

	factory := func() (adminv1connect.AdminServiceClient, error) { return client, nil }
	cmd, _ := newTestCmdWithIO("operator\nsecret\n123456\n")
	cmd.SetContext(t.Context())
	cmd.Flags().Bool("force-destroy", false, "")
	cmd.Flags().String("confirm", "", "")
	require.NoError(t, cmd.Flags().Set("force-destroy", "true"))
	require.NoError(t, cmd.Flags().Set("confirm", "scene:01ABC"))

	err := runRekeyResume(cmd, factory, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DEK_REKEY_FORCE_DESTROY_FORBIDDEN")
}

