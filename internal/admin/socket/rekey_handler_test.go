// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket_test

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/admin/socket"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// --- fakes ---

// fakeRekeySessionStore implements socket.RekeySessionStore for tests.
type fakeRekeySessionStore struct {
	token    string
	identity socket.OperatorSession
	err      error
}

func (s *fakeRekeySessionStore) GetOperatorSession(token string) (socket.OperatorSession, error) {
	if s.err != nil {
		return socket.OperatorSession{}, s.err
	}
	if token != s.token {
		return socket.OperatorSession{}, oops.Code("DENY_SESSION_INVALID").Errorf("session token not found")
	}
	return s.identity, nil
}

var _ socket.RekeySessionStore = (*fakeRekeySessionStore)(nil)

// fakeRekeyResolver implements access.SubjectResolver.
type fakeRekeyResolver struct {
	grants []string
	err    error
}

func (r *fakeRekeyResolver) ResolveSubjectAttributes(_ context.Context, _ string, _ string) (*types.AttributeBags, error) {
	if r.err != nil {
		return nil, r.err
	}
	bags := types.NewAttributeBags()
	if len(r.grants) > 0 {
		bags.Subject[access.PlayerGrantsAttribute] = r.grants
	}
	return bags, nil
}

var _ access.SubjectResolver = (*fakeRekeyResolver)(nil)

// fakeRekeyRoleChecker implements socket.OperatorRoleChecker.
type fakeRekeyRoleChecker struct {
	roles map[string][]string
}

func (f *fakeRekeyRoleChecker) PlayerHasRole(_ context.Context, playerID, role string) (bool, error) {
	for _, r := range f.roles[playerID] {
		if r == role {
			return true, nil
		}
	}
	return false, nil
}

var _ socket.OperatorRoleChecker = (*fakeRekeyRoleChecker)(nil)

// fakeOrchRunner implements socket.OrchestratorRunner using socket-layer types.
type fakeOrchRunner struct {
	outcome socket.RekeyRunOutcome
	err     error
}

func (f *fakeOrchRunner) Run(_ context.Context, _ socket.RekeyRunRequest) (socket.RekeyRunOutcome, error) {
	return f.outcome, f.err
}

var _ socket.OrchestratorRunner = (*fakeOrchRunner)(nil)

// capturingOrchRunner records the last RekeyRunRequest passed to Run.
type capturingOrchRunner struct {
	lastReq socket.RekeyRunRequest
}

func (c *capturingOrchRunner) Run(_ context.Context, req socket.RekeyRunRequest) (socket.RekeyRunOutcome, error) {
	c.lastReq = req
	return socket.RekeyRunOutcome{}, nil
}

var _ socket.OrchestratorRunner = (*capturingOrchRunner)(nil)

// fakeRekeyStream collects sent progress messages.
type fakeRekeyStream struct {
	sent []*adminv1.RekeyProgress
}

func (s *fakeRekeyStream) Send(p *adminv1.RekeyProgress) error {
	s.sent = append(s.sent, p)
	return nil
}

var _ socket.RekeyStreamSender = (*fakeRekeyStream)(nil)

// --- constants ---

const (
	rekeyTestPlayerID = "01HZAVGE83MGFEXQQH5SP9NXKF"
	rekeyTestToken    = "rekey-test-session-token"
)

// --- builder helpers ---

// newHandlerWithOperator creates a RekeyHandler backed by fakes where the
// registered token maps to a player with crypto.operator + RoleAdmin.
// The abort runner is set to a no-op fake (tests that verify abort behavior
// should use newAbortHandlerWithOperator instead).
func newHandlerWithOperator(t *testing.T, orch socket.OrchestratorRunner) *socket.RekeyHandler {
	t.Helper()
	sessions := &fakeRekeySessionStore{
		token:    rekeyTestToken,
		identity: socket.OperatorSession{PlayerID: rekeyTestPlayerID, TOTPVerified: true},
	}
	grants := &fakeRekeyResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRekeyRoleChecker{roles: map[string][]string{rekeyTestPlayerID: {access.RoleAdmin}}}
	return socket.NewRekeyHandler(sessions, grants, roles, orch, &fakeAbortRunner{})
}

// newHandlerNoOp creates a RekeyHandler where the session resolves but
// the player holds no crypto.operator grant.
func newHandlerNoOp(t *testing.T) *socket.RekeyHandler {
	t.Helper()
	sessions := &fakeRekeySessionStore{
		token:    rekeyTestToken,
		identity: socket.OperatorSession{PlayerID: rekeyTestPlayerID, TOTPVerified: true},
	}
	grants := &fakeRekeyResolver{grants: nil} // no capabilities
	roles := &fakeRekeyRoleChecker{roles: map[string][]string{rekeyTestPlayerID: {access.RoleAdmin}}}
	orch := &fakeOrchRunner{}
	return socket.NewRekeyHandler(sessions, grants, roles, orch, &fakeAbortRunner{})
}

// --- tests ---

// TestRekeyHandler_Rejects_NoSession verifies that an empty session token
// causes DENY_SESSION_INVALID before any capability check or orchestration.
func TestRekeyHandler_Rejects_NoSession(t *testing.T) {
	orch := &fakeOrchRunner{}
	h := newHandlerWithOperator(t, orch)

	stream := &fakeRekeyStream{}
	err := h.Rekey(context.Background(), &adminv1.RekeyRequest{SessionToken: ""}, stream)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T: %v", err, err)
	require.Equal(t, "DENY_SESSION_INVALID", oopsErr.Code())
}

// TestRekeyHandler_Rejects_NoCryptoOperatorCap verifies that a valid session
// without the crypto.operator grant returns DENY_NOT_OPERATOR.
func TestRekeyHandler_Rejects_NoCryptoOperatorCap(t *testing.T) {
	h := newHandlerNoOp(t)

	stream := &fakeRekeyStream{}
	err := h.Rekey(context.Background(), &adminv1.RekeyRequest{
		SessionToken:  rekeyTestToken,
		ContextType:   "scene",
		ContextId:     "01ABC",
		Justification: "test",
	}, stream)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T: %v", err, err)
	require.Equal(t, "DENY_NOT_OPERATOR", oopsErr.Code())
}

// TestRekeyHandler_Streams_Progress verifies the happy path: a valid session
// with crypto.operator + RoleAdmin drives OrchestratorRunner.Run and emits a
// RekeyCompleted event on the stream.
func TestRekeyHandler_Streams_Progress(t *testing.T) {
	rid := [16]byte{0x01, 0x93, 0xAB, 0xCD, 0xEF, 0x01, 0x02, 0x03,
		0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B}
	orch := &fakeOrchRunner{
		outcome: socket.RekeyRunOutcome{
			RequestID:        rid,
			Phase3RowCount:   100,
			Phase5Attempts:   1,
			ForceDestroyUsed: false,
			Resumed:          false,
			DurationMs:       12345,
		},
	}
	h := newHandlerWithOperator(t, orch)

	stream := &fakeRekeyStream{}
	err := h.Rekey(context.Background(), &adminv1.RekeyRequest{
		SessionToken:  rekeyTestToken,
		ContextType:   "scene",
		ContextId:     "01ABC",
		Justification: "test rekey",
	}, stream)
	require.NoError(t, err)
	require.NotEmpty(t, stream.sent)
	final := stream.sent[len(stream.sent)-1]
	require.NotNil(t, final.GetCompleted(), "final event must be RekeyCompleted")
	require.Equal(t, int32(1), final.GetCompleted().Phase5Attempts)
	require.Equal(t, int64(100), final.GetCompleted().Phase3RowsRewritten)
	require.Equal(t, int64(12345), final.GetCompleted().DurationMs)
	require.False(t, final.GetCompleted().ForceDestroyUsed)
}

// TestRekeyHandler_OrchestratorError_StreamsRekeyError verifies that when
// OrchestratorRunner.Run returns an error, a RekeyError progress event is
// sent on the stream (errors are terminal progress events, not transport errors).
func TestRekeyHandler_OrchestratorError_StreamsRekeyError(t *testing.T) {
	orch := &fakeOrchRunner{
		err: oops.Code("DEK_REKEY_ARGS_CONFLICT").Errorf("conflict in progress"),
	}
	h := newHandlerWithOperator(t, orch)

	stream := &fakeRekeyStream{}
	err := h.Rekey(context.Background(), &adminv1.RekeyRequest{
		SessionToken:  rekeyTestToken,
		ContextType:   "scene",
		ContextId:     "01ABC",
		Justification: "test",
	}, stream)
	require.NoError(t, err, "orchestrator errors stream as RekeyError, not handler errors")
	require.NotEmpty(t, stream.sent)
	final := stream.sent[len(stream.sent)-1]
	require.NotNil(t, final.GetError(), "terminal event must be RekeyError on orchestrator failure")
	require.Equal(t, "DEK_REKEY_ARGS_CONFLICT", final.GetError().Code)
}

// TestRekeyResumeHandler_Rejects_NoSession verifies that an empty session
// token is rejected by the RekeyResume handler.
func TestRekeyResumeHandler_Rejects_NoSession(t *testing.T) {
	orch := &fakeOrchRunner{}
	h := newHandlerWithOperator(t, orch)

	stream := &fakeRekeyStream{}
	err := h.RekeyResume(context.Background(), &adminv1.RekeyResumeRequest{
		SessionToken: "",
	}, stream)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T: %v", err, err)
	require.Equal(t, "DENY_SESSION_INVALID", oopsErr.Code())
}

// TestRekeyResumeHandler_Rejects_NoCryptoOperatorCap verifies that a session
// without crypto.operator is denied on the resume path.
func TestRekeyResumeHandler_Rejects_NoCryptoOperatorCap(t *testing.T) {
	h := newHandlerNoOp(t)

	rid := [16]byte{0x01, 0x02}
	stream := &fakeRekeyStream{}
	err := h.RekeyResume(context.Background(), &adminv1.RekeyResumeRequest{
		SessionToken: rekeyTestToken,
		RequestId:    rid[:],
		ForceDestroy: false,
	}, stream)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T: %v", err, err)
	require.Equal(t, "DENY_NOT_OPERATOR", oopsErr.Code())
}

// TestRekeyResumeHandler_ForceDestroy_PassThrough verifies that
// ForceDestroy=true from the proto request is forwarded to OrchestratorRunner.Run
// (INV-E11 force-destroy escape hatch pass-through).
func TestRekeyResumeHandler_ForceDestroy_PassThrough(t *testing.T) {
	capturer := &capturingOrchRunner{}
	h := newHandlerWithOperator(t, capturer)

	rid := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
	stream := &fakeRekeyStream{}
	_ = h.RekeyResume(context.Background(), &adminv1.RekeyResumeRequest{
		SessionToken: rekeyTestToken,
		RequestId:    rid[:],
		ForceDestroy: true,
	}, stream)
	require.True(t, capturer.lastReq.ForceDestroy,
		"ForceDestroy=true must be forwarded to OrchestratorRunner.Run (INV-E11)")
}

// TestRekeyResumeHandler_Streams_Completed verifies the happy path for
// RekeyResume: valid session + crypto.operator + successful run emits
// RekeyCompleted with Resumed=true.
func TestRekeyResumeHandler_Streams_Completed(t *testing.T) {
	rid := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
	orch := &fakeOrchRunner{
		outcome: socket.RekeyRunOutcome{
			RequestID:        rid,
			Resumed:          true,
			ForceDestroyUsed: true,
			Phase3RowCount:   50,
			Phase5Attempts:   2,
			DurationMs:       9999,
		},
	}
	h := newHandlerWithOperator(t, orch)

	stream := &fakeRekeyStream{}
	err := h.RekeyResume(context.Background(), &adminv1.RekeyResumeRequest{
		SessionToken: rekeyTestToken,
		RequestId:    rid[:],
		ForceDestroy: true,
	}, stream)
	require.NoError(t, err)
	require.NotEmpty(t, stream.sent)
	final := stream.sent[len(stream.sent)-1]
	require.NotNil(t, final.GetCompleted())
	require.True(t, final.GetCompleted().Resumed,
		"RekeyCompleted.Resumed must be true on resume path")
	require.True(t, final.GetCompleted().ForceDestroyUsed)
}

// TestRekeyResumeHandler_Rejects_EmptyRequestID verifies that a nil/empty
// RequestId is rejected before the orchestrator is invoked.
func TestRekeyResumeHandler_Rejects_EmptyRequestID(t *testing.T) {
	orch := &fakeOrchRunner{}
	h := newHandlerWithOperator(t, orch)

	stream := &fakeRekeyStream{}
	err := h.RekeyResume(context.Background(), &adminv1.RekeyResumeRequest{
		SessionToken: rekeyTestToken,
		RequestId:    nil,
		ForceDestroy: false,
	}, stream)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T: %v", err, err)
	require.Equal(t, "REKEY_INVALID_REQUEST_ID", oopsErr.Code())
}

// --- RekeyAbort fakes and tests ---

// fakeAbortRunner implements socket.RekeyAbortRunner for tests.
type fakeAbortRunner struct {
	outcome socket.RekeyAbortOutcome
	err     error
}

func (f *fakeAbortRunner) RunAbort(_ context.Context, _ socket.RekeyAbortRequest) (socket.RekeyAbortOutcome, error) {
	return f.outcome, f.err
}

var _ socket.RekeyAbortRunner = (*fakeAbortRunner)(nil)

// newAbortHandlerWithOperator creates a RekeyHandler backed by fakes where
// the registered token maps to an operator with crypto.operator + RoleAdmin,
// and with the provided abort runner.
func newAbortHandlerWithOperator(t *testing.T, abort socket.RekeyAbortRunner) *socket.RekeyHandler {
	t.Helper()
	sessions := &fakeRekeySessionStore{
		token:    rekeyTestToken,
		identity: socket.OperatorSession{PlayerID: rekeyTestPlayerID, TOTPVerified: true},
	}
	grants := &fakeRekeyResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRekeyRoleChecker{roles: map[string][]string{rekeyTestPlayerID: {access.RoleAdmin}}}
	orch := &fakeOrchRunner{}
	return socket.NewRekeyHandler(sessions, grants, roles, orch, abort)
}

// TestRekeyHandler_Abort_SingleControl_Allowed verifies INV-E17: a session with
// only crypto.operator capability (no dual-control approval) can abort an
// in-flight checkpoint even when site policy mandates dual-control for rekey.
func TestRekeyHandler_Abort_SingleControl_Allowed(t *testing.T) {
	abortedAt := timestamppb.Now()
	eventID := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
	abort := &fakeAbortRunner{
		outcome: socket.RekeyAbortOutcome{
			AbortedAt:    abortedAt.AsTime(),
			AuditEventID: eventID,
		},
	}
	h := newAbortHandlerWithOperator(t, abort)
	rid := [16]byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20}

	res, err := h.RekeyAbort(context.Background(), &adminv1.RekeyAbortRequest{
		SessionToken: rekeyTestToken,
		RequestId:    rid[:],
	})
	require.NoError(t, err, "INV-E17: Abort accepts single-control even when site mandates dual for rekey")
	require.NotNil(t, res)
	require.NotNil(t, res.AbortedAt, "AbortedAt must be populated")
	require.Equal(t, eventID[:], res.AuditEventId, "AuditEventId must be the runner's returned event ID")
}

// TestRekeyHandler_Abort_Rejects_NoSession verifies that an empty session token
// causes DENY_SESSION_INVALID before any abort logic.
func TestRekeyHandler_Abort_Rejects_NoSession(t *testing.T) {
	h := newAbortHandlerWithOperator(t, &fakeAbortRunner{})
	rid := [16]byte{0x01}

	_, err := h.RekeyAbort(context.Background(), &adminv1.RekeyAbortRequest{
		SessionToken: "",
		RequestId:    rid[:],
	})
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T: %v", err, err)
	require.Equal(t, "DENY_SESSION_INVALID", oopsErr.Code())
}

// TestRekeyHandler_Abort_Rejects_NoCryptoOperatorCap verifies that a session
// without crypto.operator is denied on the abort path.
func TestRekeyHandler_Abort_Rejects_NoCryptoOperatorCap(t *testing.T) {
	// newHandlerNoOp has no grants; we also need an abort runner
	sessions := &fakeRekeySessionStore{
		token:    rekeyTestToken,
		identity: socket.OperatorSession{PlayerID: rekeyTestPlayerID, TOTPVerified: true},
	}
	grants := &fakeRekeyResolver{grants: nil} // no capabilities
	roles := &fakeRekeyRoleChecker{roles: map[string][]string{rekeyTestPlayerID: {access.RoleAdmin}}}
	orch := &fakeOrchRunner{}
	h := socket.NewRekeyHandler(sessions, grants, roles, orch, &fakeAbortRunner{})

	rid := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
	_, err := h.RekeyAbort(context.Background(), &adminv1.RekeyAbortRequest{
		SessionToken: rekeyTestToken,
		RequestId:    rid[:],
	})
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T: %v", err, err)
	require.Equal(t, "DENY_NOT_OPERATOR", oopsErr.Code())
}

// TestRekeyHandler_Abort_Terminal verifies that when the abort runner returns
// DEK_REKEY_CHECKPOINT_TERMINAL the handler surfaces it to the caller.
func TestRekeyHandler_Abort_Terminal(t *testing.T) {
	abort := &fakeAbortRunner{
		err: oops.Code("DEK_REKEY_CHECKPOINT_TERMINAL").Errorf("checkpoint already terminal"),
	}
	h := newAbortHandlerWithOperator(t, abort)
	rid := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}

	_, err := h.RekeyAbort(context.Background(), &adminv1.RekeyAbortRequest{
		SessionToken: rekeyTestToken,
		RequestId:    rid[:],
	})
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T: %v", err, err)
	require.Equal(t, "DEK_REKEY_CHECKPOINT_TERMINAL", oopsErr.Code())
}

// TestRekeyHandler_Abort_NotFound verifies that DEK_REKEY_CHECKPOINT_NOT_FOUND
// from the runner is surfaced to the caller unchanged.
func TestRekeyHandler_Abort_NotFound(t *testing.T) {
	abort := &fakeAbortRunner{
		err: oops.Code("DEK_REKEY_CHECKPOINT_NOT_FOUND").Errorf("checkpoint not found"),
	}
	h := newAbortHandlerWithOperator(t, abort)
	rid := [16]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x01, 0x02,
		0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A}

	_, err := h.RekeyAbort(context.Background(), &adminv1.RekeyAbortRequest{
		SessionToken: rekeyTestToken,
		RequestId:    rid[:],
	})
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T: %v", err, err)
	require.Equal(t, "DEK_REKEY_CHECKPOINT_NOT_FOUND", oopsErr.Code())
}

// TestRekeyHandler_Abort_Rejects_EmptyRequestID verifies that a nil/empty
// RequestId is rejected before the abort runner is invoked.
func TestRekeyHandler_Abort_Rejects_EmptyRequestID(t *testing.T) {
	h := newAbortHandlerWithOperator(t, &fakeAbortRunner{})

	_, err := h.RekeyAbort(context.Background(), &adminv1.RekeyAbortRequest{
		SessionToken: rekeyTestToken,
		RequestId:    nil,
	})
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T: %v", err, err)
	require.Equal(t, "REKEY_INVALID_REQUEST_ID", oopsErr.Code())
}
