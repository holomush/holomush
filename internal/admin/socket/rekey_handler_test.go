// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket_test

import (
	"context"
	"testing"
	"time"

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
	return socket.NewRekeyHandler(sessions, grants, roles, orch, &fakeAbortRunner{}, nil)
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
	return socket.NewRekeyHandler(sessions, grants, roles, orch, &fakeAbortRunner{}, nil)
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
	rid := [16]byte{
		0x01, 0x93, 0xAB, 0xCD, 0xEF, 0x01, 0x02, 0x03,
		0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B,
	}
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

// TestRekeyResumeHandler_RequestID_PassThrough verifies that the request_id
// bytes from the proto are forwarded verbatim as RekeyRunRequest.RequestID
// to the OrchestratorRunner. This is the core fix for the code-reviewer
// finding: the previous implementation discarded ridFixed with _ = ridFixed.
func TestRekeyResumeHandler_RequestID_PassThrough(t *testing.T) {
	capturer := &capturingOrchRunner{}
	h := newHandlerWithOperator(t, capturer)

	rid := [16]byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
	}
	stream := &fakeRekeyStream{}
	_ = h.RekeyResume(context.Background(), &adminv1.RekeyResumeRequest{
		SessionToken: rekeyTestToken,
		RequestId:    rid[:],
		ForceDestroy: false,
	}, stream)
	require.Equal(t, rid, capturer.lastReq.RequestID,
		"RequestID must be forwarded verbatim to OrchestratorRunner.Run — "+
			"the adapter needs it to look up the checkpoint via RunByRequestID")
}

// TestRekeyResumeHandler_ForceDestroy_PassThrough verifies that
// ForceDestroy=true from the proto request is forwarded to OrchestratorRunner.Run
// (INV-CRYPTO-98 force-destroy escape hatch pass-through).
func TestRekeyResumeHandler_ForceDestroy_PassThrough(t *testing.T) {
	capturer := &capturingOrchRunner{}
	h := newHandlerWithOperator(t, capturer)

	rid := [16]byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
	}
	stream := &fakeRekeyStream{}
	_ = h.RekeyResume(context.Background(), &adminv1.RekeyResumeRequest{
		SessionToken: rekeyTestToken,
		RequestId:    rid[:],
		ForceDestroy: true,
	}, stream)
	require.True(t, capturer.lastReq.ForceDestroy,
		"ForceDestroy=true must be forwarded to OrchestratorRunner.Run (INV-CRYPTO-98)")
}

// TestRekeyResumeHandler_Streams_Completed verifies the happy path for
// RekeyResume: valid session + crypto.operator + successful run emits
// RekeyCompleted with Resumed=true.
func TestRekeyResumeHandler_Streams_Completed(t *testing.T) {
	rid := [16]byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
	}
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
	return socket.NewRekeyHandler(sessions, grants, roles, orch, abort, nil)
}

// TestRekeyHandler_Abort_SingleControl_Allowed verifies INV-CRYPTO-104: a session with
// only crypto.operator capability (no dual-control approval) can abort an
// in-flight checkpoint even when site policy mandates dual-control for rekey.
func TestRekeyHandler_Abort_SingleControl_Allowed(t *testing.T) {
	abortedAt := timestamppb.Now()
	eventID := [16]byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
	}
	abort := &fakeAbortRunner{
		outcome: socket.RekeyAbortOutcome{
			AbortedAt:    abortedAt.AsTime(),
			AuditEventID: eventID,
		},
	}
	h := newAbortHandlerWithOperator(t, abort)
	rid := [16]byte{
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20,
	}

	res, err := h.RekeyAbort(context.Background(), &adminv1.RekeyAbortRequest{
		SessionToken: rekeyTestToken,
		RequestId:    rid[:],
	})
	require.NoError(t, err, "INV-CRYPTO-104: Abort accepts single-control even when site mandates dual for rekey")
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
	h := socket.NewRekeyHandler(sessions, grants, roles, orch, &fakeAbortRunner{}, nil)

	rid := [16]byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
	}
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
	rid := [16]byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
	}

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
	rid := [16]byte{
		0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x01, 0x02,
		0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A,
	}

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

// --- RekeyStatus + RekeyList fakes and tests ---

// fakeCheckpointRepo implements socket.CheckpointStatusReader for tests.
type fakeCheckpointRepo struct {
	byID   map[[16]byte]socket.CheckpointView
	rows   []socket.CheckpointView
	getErr error
	lstErr error
}

func (f *fakeCheckpointRepo) GetCheckpoint(_ context.Context, rid [16]byte) (socket.CheckpointView, error) {
	if f.getErr != nil {
		return socket.CheckpointView{}, f.getErr
	}
	c, ok := f.byID[rid]
	if !ok {
		return socket.CheckpointView{}, oops.Code("DEK_REKEY_CHECKPOINT_NOT_FOUND").Errorf("not found")
	}
	return c, nil
}

func (f *fakeCheckpointRepo) ListCheckpoints(_ context.Context, filter socket.CheckpointListFilter) ([]socket.CheckpointView, error) {
	if f.lstErr != nil {
		return nil, f.lstErr
	}
	var out []socket.CheckpointView
	for _, c := range f.rows {
		if !filter.IncludeTerminal && (c.Status == "complete" || c.Status == "aborted") {
			continue
		}
		if filter.ContextPattern != nil && c.ContextID != *filter.ContextPattern {
			continue
		}
		out = append(out, c)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

var _ socket.CheckpointStatusReader = (*fakeCheckpointRepo)(nil)

// fakeStatusStream collects sent RekeyStatusResponse messages.
type fakeStatusStream struct {
	sent []*adminv1.RekeyStatusResponse
}

func (s *fakeStatusStream) Send(r *adminv1.RekeyStatusResponse) error {
	s.sent = append(s.sent, r)
	return nil
}

var _ socket.RekeyListStream = (*fakeStatusStream)(nil)

// newStatusHandlerWithOperator creates a RekeyHandler with a CheckpointStatusReader
// and an operator session with crypto.operator + RoleAdmin.
func newStatusHandlerWithOperator(t *testing.T, repo socket.CheckpointStatusReader) *socket.RekeyHandler {
	t.Helper()
	sessions := &fakeRekeySessionStore{
		token:    rekeyTestToken,
		identity: socket.OperatorSession{PlayerID: rekeyTestPlayerID, TOTPVerified: true},
	}
	grants := &fakeRekeyResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRekeyRoleChecker{roles: map[string][]string{rekeyTestPlayerID: {access.RoleAdmin}}}
	return socket.NewRekeyHandler(sessions, grants, roles, &fakeOrchRunner{}, &fakeAbortRunner{}, repo)
}

var rekeyTestRID = [16]byte{
	0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
	0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
}

// seedView constructs a minimal CheckpointView for test seeding.
func seedView(rid [16]byte, status, ctxID string) socket.CheckpointView {
	return socket.CheckpointView{
		RequestID:       rid,
		ContextType:     "scene",
		ContextID:       ctxID,
		Status:          status,
		PrimaryPlayerID: rekeyTestPlayerID,
		StartedAt:       time.Now(),
		LastHeartbeatAt: time.Now(),
	}
}

// TestRekeyHandler_Status_ReturnsAllFields verifies that RekeyStatus returns
// the full checkpoint state with matching request_id and status.
func TestRekeyHandler_Status_ReturnsAllFields(t *testing.T) {
	view := seedView(rekeyTestRID, "phase1_complete", "01ABC")
	repo := &fakeCheckpointRepo{
		byID: map[[16]byte]socket.CheckpointView{rekeyTestRID: view},
	}
	h := newStatusHandlerWithOperator(t, repo)

	res, err := h.RekeyStatus(context.Background(), &adminv1.RekeyStatusRequest{
		SessionToken: rekeyTestToken,
		RequestId:    rekeyTestRID[:],
	})
	require.NoError(t, err)
	require.Equal(t, rekeyTestRID[:], res.RequestId)
	require.Equal(t, "phase1_complete", res.Status)
	require.Equal(t, rekeyTestPlayerID, res.PrimaryPlayerId)
	require.NotNil(t, res.StartedAt)
}

// TestRekeyHandler_Status_NotFound verifies that a missing checkpoint
// surfaces DEK_REKEY_CHECKPOINT_NOT_FOUND to the caller.
func TestRekeyHandler_Status_NotFound(t *testing.T) {
	repo := &fakeCheckpointRepo{byID: map[[16]byte]socket.CheckpointView{}}
	h := newStatusHandlerWithOperator(t, repo)

	_, err := h.RekeyStatus(context.Background(), &adminv1.RekeyStatusRequest{
		SessionToken: rekeyTestToken,
		RequestId:    rekeyTestRID[:],
	})
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T: %v", err, err)
	require.Equal(t, "DEK_REKEY_CHECKPOINT_NOT_FOUND", oopsErr.Code())
}

// TestRekeyHandler_Status_Rejects_NoCryptoOperatorCap verifies that a session
// without crypto.operator is denied on the status path.
func TestRekeyHandler_Status_Rejects_NoCryptoOperatorCap(t *testing.T) {
	sessions := &fakeRekeySessionStore{
		token:    rekeyTestToken,
		identity: socket.OperatorSession{PlayerID: rekeyTestPlayerID, TOTPVerified: true},
	}
	grants := &fakeRekeyResolver{grants: nil} // no capabilities
	roles := &fakeRekeyRoleChecker{roles: map[string][]string{rekeyTestPlayerID: {access.RoleAdmin}}}
	repo := &fakeCheckpointRepo{}
	h := socket.NewRekeyHandler(sessions, grants, roles, &fakeOrchRunner{}, &fakeAbortRunner{}, repo)

	_, err := h.RekeyStatus(context.Background(), &adminv1.RekeyStatusRequest{
		SessionToken: rekeyTestToken,
		RequestId:    rekeyTestRID[:],
	})
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T: %v", err, err)
	require.Equal(t, "DENY_NOT_OPERATOR", oopsErr.Code())
}

// TestRekeyHandler_List_NonTerminalOnly_ByDefault verifies that RekeyList
// excludes terminal checkpoints (complete, aborted) unless IncludeTerminal=true.
func TestRekeyHandler_List_NonTerminalOnly_ByDefault(t *testing.T) {
	activeRID := [16]byte{0x01, 0x02}
	doneRID := [16]byte{0x03, 0x04}
	repo := &fakeCheckpointRepo{
		rows: []socket.CheckpointView{
			seedView(activeRID, "phase1_complete", "01ACTIVE"),
			seedView(doneRID, "complete", "01DONE"),
		},
	}
	h := newStatusHandlerWithOperator(t, repo)

	// Default: non-terminal only.
	stream1 := &fakeStatusStream{}
	err := h.RekeyList(context.Background(), &adminv1.RekeyListRequest{
		SessionToken: rekeyTestToken,
	}, stream1)
	require.NoError(t, err)
	require.Len(t, stream1.sent, 1, "default excludes terminal")

	// IncludeTerminal=true: both rows.
	stream2 := &fakeStatusStream{}
	err = h.RekeyList(context.Background(), &adminv1.RekeyListRequest{
		SessionToken:    rekeyTestToken,
		IncludeTerminal: true,
	}, stream2)
	require.NoError(t, err)
	require.Len(t, stream2.sent, 2)
}

// TestRekeyHandler_List_Rejects_NoCryptoOperatorCap verifies that a session
// without crypto.operator is denied on the list path.
func TestRekeyHandler_List_Rejects_NoCryptoOperatorCap(t *testing.T) {
	sessions := &fakeRekeySessionStore{
		token:    rekeyTestToken,
		identity: socket.OperatorSession{PlayerID: rekeyTestPlayerID, TOTPVerified: true},
	}
	grants := &fakeRekeyResolver{grants: nil} // no capabilities
	roles := &fakeRekeyRoleChecker{roles: map[string][]string{rekeyTestPlayerID: {access.RoleAdmin}}}
	repo := &fakeCheckpointRepo{}
	h := socket.NewRekeyHandler(sessions, grants, roles, &fakeOrchRunner{}, &fakeAbortRunner{}, repo)

	stream := &fakeStatusStream{}
	err := h.RekeyList(context.Background(), &adminv1.RekeyListRequest{
		SessionToken: rekeyTestToken,
	}, stream)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T: %v", err, err)
	require.Equal(t, "DENY_NOT_OPERATOR", oopsErr.Code())
}

// TestRekeyHandler_List_EmptyResult verifies that List with no matching rows
// sends zero stream messages and returns no error.
func TestRekeyHandler_List_EmptyResult(t *testing.T) {
	repo := &fakeCheckpointRepo{rows: nil}
	h := newStatusHandlerWithOperator(t, repo)

	stream := &fakeStatusStream{}
	err := h.RekeyList(context.Background(), &adminv1.RekeyListRequest{
		SessionToken: rekeyTestToken,
	}, stream)
	require.NoError(t, err)
	require.Empty(t, stream.sent)
}
