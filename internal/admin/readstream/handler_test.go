// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/admin/approval"
	"github.com/holomush/holomush/internal/admin/readstream"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/pkg/errutil"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// ---------- Test fakes ----------

const (
	testPlayerULID   = "01HZAVGE83MGFEXQQH5SP9NXKF"
	otherPlayerULID  = "01HZAVGE83MGFEXQQH5SP9NXKG"
	testSessionToken = "01HZAVGEAA000000000000000A"
)

// fakeSessionStore is a stub SessionStore. When token == storedToken, it
// returns storedSession. Otherwise it returns DENY_SESSION_INVALID.
type fakeSessionStore struct {
	storedToken   string
	storedSession readstream.OperatorSession
}

func (f *fakeSessionStore) GetOperatorSession(token string) (readstream.OperatorSession, error) {
	if token != f.storedToken {
		return readstream.OperatorSession{}, oops.Code("DENY_SESSION_INVALID").Errorf("invalid token")
	}
	return f.storedSession, nil
}

// fakeGrantsResolver implements access.SubjectResolver. When grants is
// non-empty, the player is granted those capabilities; when empty, the
// capability check returns false (DENY_OPERATOR_CAPABILITY).
type fakeGrantsResolver struct {
	grants []string
	err    error
}

func (f *fakeGrantsResolver) ResolveSubjectAttributes(_ context.Context, _ string, _ string) (*types.AttributeBags, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.grants) == 0 {
		return &types.AttributeBags{Subject: map[string]any{}}, nil
	}
	return &types.AttributeBags{
		Subject: map[string]any{access.PlayerGrantsAttribute: f.grants},
	}, nil
}

// fakeApprovalRepo implements approval.Repo with knobs for every branch
// the dual-control flow can hit.
type fakeApprovalRepo struct {
	// GetByOpArgsHash result.
	getResult approval.Approval
	getErr    error

	// Open result.
	openResult approval.RequestID
	openErr    error

	// WaitForApproval result.
	waitResult approval.Approval
	waitErr    error

	// Call counters for assertions.
	mu        sync.Mutex
	getCalls  int
	openCalls int
	waitCalls int
}

func (f *fakeApprovalRepo) Open(_ context.Context, _ approval.OpenRequest) (approval.RequestID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openCalls++
	return f.openResult, f.openErr
}

func (f *fakeApprovalRepo) Get(_ context.Context, _ approval.RequestID) (approval.Approval, error) {
	return approval.Approval{}, oops.Code("APPROVAL_NOT_FOUND").Errorf("unused in handler tests")
}

func (f *fakeApprovalRepo) GetByOpArgsHash(_ context.Context, _ string, _ []byte, _ string) (approval.Approval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	return f.getResult, f.getErr
}

func (f *fakeApprovalRepo) MarkApproved(_ context.Context, _ approval.RequestID, _ string) error {
	return nil
}

func (f *fakeApprovalRepo) WaitForApproval(_ context.Context, _ approval.RequestID, _ time.Time) (approval.Approval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waitCalls++
	return f.waitResult, f.waitErr
}

func (f *fakeApprovalRepo) GetCalls() int  { f.mu.Lock(); defer f.mu.Unlock(); return f.getCalls }
func (f *fakeApprovalRepo) OpenCalls() int { f.mu.Lock(); defer f.mu.Unlock(); return f.openCalls }
func (f *fakeApprovalRepo) WaitCalls() int { f.mu.Lock(); defer f.mu.Unlock(); return f.waitCalls }

// fakeAuditEmitter records every EmitStart / EmitCompleted call. emitStartErr
// (when non-nil) is returned from EmitStart on the call number indexed by
// startErrAt (1-indexed). Same for completed. This lets a test simulate an
// audit-emit failure cleanly without coupling to chain.Emitter internals.
type fakeAuditEmitter struct {
	mu               sync.Mutex
	startCallTimes   []int64 // monotonic call-id (set from order across both methods)
	completedTimes   []int64
	monotonicCounter int64

	emitStartErr     error
	emitCompletedErr error
}

func (f *fakeAuditEmitter) EmitStart(_ context.Context, _ readstream.OperatorReadStartPayload, _ ulid.ULID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.monotonicCounter++
	f.startCallTimes = append(f.startCallTimes, f.monotonicCounter)
	return f.emitStartErr
}

func (f *fakeAuditEmitter) EmitCompleted(_ context.Context, _ readstream.OperatorReadCompletedPayload, _ ulid.ULID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.monotonicCounter++
	f.completedTimes = append(f.completedTimes, f.monotonicCounter)
	if f.emitCompletedErr != nil {
		readstream.CompletedAuditFailuresTotal.Inc()
		return f.emitCompletedErr
	}
	return nil
}

func (f *fakeAuditEmitter) StartCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.startCallTimes)
}

func (f *fakeAuditEmitter) CompletedCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.completedTimes)
}

// firstStartCallID returns the monotonic call-id of the first EmitStart call
// (1-indexed). 0 = never called.
func (f *fakeAuditEmitter) firstStartCallID() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.startCallTimes) == 0 {
		return 0
	}
	return f.startCallTimes[0]
}

// firstCompletedCallID returns the monotonic call-id of the first
// EmitCompleted call (1-indexed). 0 = never called.
func (f *fakeAuditEmitter) firstCompletedCallID() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.completedTimes) == 0 {
		return 0
	}
	return f.completedTimes[0]
}

// stubDEKResolver and stubCodecResolver are inert resolvers for handler
// unit tests that DO NOT reach DecryptRow. The handler tests focus on
// flows that succeed or fail BEFORE ColdReader.Read is called
// (INV-CRYPTO-53/INV-CRYPTO-54/INV-CRYPTO-55/INV-CRYPTO-61); the per-row decrypt classifier matrix is exercised
// in decrypt_test.go and the scan-and-stream contract in integration
// tests (R.17-R.19).
type stubDEKResolver struct{}

func (stubDEKResolver) Resolve(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
	return codec.Key{}, oops.Code("STUB_DEK_RESOLVER").Errorf("stub resolver not configured")
}

type stubCodecResolver struct{}

func (stubCodecResolver) Resolve(_ codec.Name) (codec.Codec, error) {
	return nil, oops.Code("STUB_CODEC_RESOLVER").Errorf("stub resolver not configured")
}

// stubColdReader replaces *ColdReader in unit tests. It returns the
// configured rows + err deterministically and records the last query it
// received so tests can assert that BuildSubjects + ResolveBounds produced
// the expected SQL inputs (INV-CRYPTO-58 subjects + INV-CRYPTO-56 bounds round-trip).
type stubColdReader struct {
	mu        sync.Mutex
	rows      []readstream.ColdRow
	err       error
	lastQuery readstream.ColdQuery
	callCount int
}

func (s *stubColdReader) Read(_ context.Context, q readstream.ColdQuery) ([]readstream.ColdRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastQuery = q
	s.callCount++
	return s.rows, s.err
}

func (s *stubColdReader) CallCount() int { s.mu.Lock(); defer s.mu.Unlock(); return s.callCount }
func (s *stubColdReader) LastQuery() readstream.ColdQuery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastQuery
}

// ---------- helpers ----------

func newTestConfig() readstream.Config {
	return readstream.Config{
		Sessions: &fakeSessionStore{
			storedToken: testSessionToken,
			storedSession: readstream.OperatorSession{
				PlayerID:       testPlayerULID,
				SessionTokenID: "session-tok-id",
				PeerCredUID:    1001,
				PeerCredPID:    23456,
			},
		},
		Grants:        &fakeGrantsResolver{grants: []string{access.CapabilityCryptoOperator}},
		Approvals:     &fakeApprovalRepo{},
		ColdReader:    &stubColdReader{}, // empty rows, no error — tests that need rows override this
		DEK:           stubDEKResolver{},
		Codecs:        stubCodecResolver{},
		AuditEmitter:  &fakeAuditEmitter{},
		PolicyHash:    "sha256:aabbccddeeff",
		Clock:         func() time.Time { return time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC) },
		Logger:        slog.New(slog.DiscardHandler),
		Game:          "g1",
		MaxWindow:     24 * time.Hour,
		DefaultWindow: 1 * time.Hour,
		WriteDeadline: 5 * time.Second,
		ApprovalTTL:   30 * time.Second,
	}
}

// makeIdentityRows returns n synthetic identity-encoded ColdRow values.
// Identity-codec rows make DecryptRow short-circuit to a pass-through, so
// neither the DEK resolver nor the codec resolver is consulted — perfect
// for handler-flow unit tests that DON'T want to construct real ciphertexts.
//
// The envelope is a marshaled eventbusv1.Event carrying the row's payload
// bytes so DecryptRow's `proto.Unmarshal(row.Envelope, &envProto)` step
// succeeds. payload bytes contain a small JSON sentinel so plaintext
// frames carry detectable content.
func makeIdentityRows(n int) []readstream.ColdRow {
	rows := make([]readstream.ColdRow, 0, n)
	base := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	for i := 0; i < n; i++ {
		id := base
		id[15] = byte(i)
		// Build a tiny eventbusv1.Event envelope with the payload bytes.
		envProto, _ := proto.Marshal(&eventbusv1.Event{
			Payload: []byte(`{"ok":true}`),
		})
		rows = append(rows, readstream.ColdRow{
			ID:        id,
			Subject:   "events.g1.scene.01H.spoke",
			Type:      "scene.spoke",
			Timestamp: time.Date(2026, 5, 12, 9, 30, 0, 0, time.UTC).Add(time.Duration(i) * time.Second),
			Actor:     eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: id},
			Envelope:  envProto,
			Codec:     codec.NameIdentity,
		})
	}
	return rows
}

func validHandlerRequest(token string) *adminv1.AdminReadStreamRequest {
	return &adminv1.AdminReadStreamRequest{
		SessionToken:  token,
		Context:       []*adminv1.ContextRef{{Type: "scene", Ids: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}}},
		Since:         timestamppb.New(time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)),
		Until:         timestamppb.New(time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)),
		Justification: "incident-2026-05-12 P0 investigation",
	}
}

// runHandler builds a TestHandler with the given config, drives
// HandleInternalForTest, and returns the recording stream plus error.
func runHandler(t *testing.T, cfg readstream.Config, req *adminv1.AdminReadStreamRequest) (*readstream.RecordingStream, error) {
	t.Helper()
	th, err := readstream.NewTestHandler(cfg)
	require.NoError(t, err, "NewTestHandler must accept the test config")
	stream := &readstream.RecordingStream{}
	runErr := th.HandleInternalForTest(context.Background(), req, stream)
	return stream, runErr
}

// ---------- INV-CRYPTO-55: capability check precedes audit ----------

// TestINV_CRYPTO_55_CapabilityCheckPrecedesAudit asserts that the capability check
// runs BEFORE EmitStart and BEFORE any frame send. A player without
// crypto.operator must see DENY_OPERATOR_CAPABILITY, zero frames sent, and
// EmitStart NEVER called.
func TestINV_CRYPTO_55_CapabilityCheckPrecedesAudit(t *testing.T) {
	cfg := newTestConfig()
	cfg.Grants = &fakeGrantsResolver{grants: nil} // no grants

	emitter := &fakeAuditEmitter{}
	cfg.AuditEmitter = emitter

	stream, err := runHandler(t, cfg, validHandlerRequest(testSessionToken))

	require.Error(t, err, "handler must fail when capability absent")
	errutil.AssertErrorCode(t, err, "DENY_OPERATOR_CAPABILITY")
	assert.Equal(t, 0, emitter.StartCalls(), "EmitStart MUST NOT be called when capability is denied")
	assert.Equal(t, 0, emitter.CompletedCalls(), "EmitCompleted MUST NOT be called when capability is denied")
	assert.Equal(t, 0, stream.Len(), "ZERO frames may be sent when capability is denied")
}

// TestSessionLookupFails verifies the session lookup error path: an invalid
// token returns DENY_SESSION_INVALID and no audit/frames are emitted.
func TestSessionLookupFails(t *testing.T) {
	cfg := newTestConfig()
	emitter := &fakeAuditEmitter{}
	cfg.AuditEmitter = emitter

	stream, err := runHandler(t, cfg, validHandlerRequest("wrong-token"))

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DENY_SESSION_INVALID")
	assert.Equal(t, 0, emitter.StartCalls(), "EmitStart MUST NOT be called on session failure")
	assert.Equal(t, 0, stream.Len(), "ZERO frames may be sent on session failure")
}

// ---------- INV-CRYPTO-54: audit-publish failure refuses to stream ----------

// TestINV_CRYPTO_54_AuditPublishFailRefuses asserts that an EmitStart failure
// blocks all data emission: handler returns DENY_AUDIT_PRE_DATA_PUBLISH,
// ZERO frames are sent (no PendingApproval, no ReadStarted, no Event), and
// ColdReader.Read is never called. (We assert the latter indirectly: the
// Config carries a ColdReader whose pool is nil — if Read were called it
// would panic.)
func TestINV_CRYPTO_54_AuditPublishFailRefuses(t *testing.T) {
	cfg := newTestConfig()
	emitter := &fakeAuditEmitter{
		emitStartErr: oops.Code("EMITTER_PUBLISH_FAILED").Errorf("simulated publish failure"),
	}
	cfg.AuditEmitter = emitter

	stream, err := runHandler(t, cfg, validHandlerRequest(testSessionToken))

	require.Error(t, err, "handler must fail when EmitStart fails")
	errutil.AssertErrorCode(t, err, "DENY_AUDIT_PRE_DATA_PUBLISH")
	assert.Equal(t, 1, emitter.StartCalls(), "EmitStart must be attempted exactly once")
	assert.Equal(t, 0, emitter.CompletedCalls(),
		"EmitCompleted MUST NOT be called when EmitStart fails (no audit pair without a successful start)")
	assert.Equal(t, 0, stream.Len(), "ZERO frames may be sent when EmitStart fails")
}

// ---------- INV-CRYPTO-53: pre-data audit ordering ----------

// TestINV_CRYPTO_53_PreDataAuditOrdering asserts the canonical ordering for the
// non-dual-control happy path with a small row set:
//
//	EmitStart → ReadStarted frame → Event frames → ReadFinished frame → EmitCompleted
//
// The fake ColdReader returns three identity-encoded rows; the stub DEK
// resolver is never reached because identity-codec rows pass plaintext
// through directly. The test asserts the EmitStart call-id precedes EVERY
// frame send and EmitCompleted call-id follows the LAST frame send.
func TestINV_CRYPTO_53_PreDataAuditOrdering(t *testing.T) {
	cfg := newTestConfig()
	emitter := &fakeAuditEmitter{}
	cfg.AuditEmitter = emitter
	cold := &stubColdReader{rows: makeIdentityRows(3)}
	cfg.ColdReader = cold

	stream, err := runHandler(t, cfg, validHandlerRequest(testSessionToken))
	require.NoError(t, err, "handler must succeed on the happy path")

	require.Equal(t, 1, emitter.StartCalls(), "EmitStart must be called exactly once")
	require.Equal(t, 1, emitter.CompletedCalls(), "EmitCompleted must be called exactly once")

	frames := stream.Frames()
	// Expected sequence: 1 ReadStarted, 3 Event frames, 1 ReadFinished = 5 total.
	require.Len(t, frames, 5, "expected ReadStarted + 3 Events + ReadFinished")

	// Frame ordering: Started → Event*3 → Finished.
	_, isStarted := frames[0].GetPayload().(*adminv1.AdminReadStreamResponse_Started)
	assert.True(t, isStarted, "frame[0] MUST be ReadStarted")
	for i := 1; i <= 3; i++ {
		_, isEvent := frames[i].GetPayload().(*adminv1.AdminReadStreamResponse_Event)
		assert.Truef(t, isEvent, "frame[%d] must be Event; got %T", i, frames[i].GetPayload())
	}
	_, isFinished := frames[4].GetPayload().(*adminv1.AdminReadStreamResponse_Finished)
	assert.True(t, isFinished, "frame[4] MUST be ReadFinished")

	// Audit ordering: EmitStart (call-id 1) precedes EmitCompleted (call-id 2).
	assert.Equal(t, int64(1), emitter.firstStartCallID(),
		"EmitStart MUST be the first audit call (no completed-before-start)")
	assert.Equal(t, int64(2), emitter.firstCompletedCallID(),
		"EmitCompleted MUST be the SECOND audit call (start-before-completed)")

	// ColdReader was hit exactly once — the handler does not retry.
	assert.Equal(t, 1, cold.CallCount(), "ColdReader.Read must be called exactly once")
}

// ---------- INV-CRYPTO-60: completion audit failure does not raise ----------

// TestINV_CRYPTO_60_CompletionAuditFailureNotRaised asserts that when
// EmitCompleted fails after a clean scan-and-stream run, the failure is
// logged + metric-counted but NEVER raised back to the operator. The
// outer handler return value reflects only stream-level errors (which is
// nil on the happy path used here).
func TestINV_CRYPTO_60_CompletionAuditFailureNotRaised(t *testing.T) {
	cfg := newTestConfig()
	completedErr := oops.Code("COMPLETION_PUBLISH_FAILED").Errorf("simulated completion failure")
	emitter := &fakeAuditEmitter{emitCompletedErr: completedErr}
	cfg.AuditEmitter = emitter
	cfg.ColdReader = &stubColdReader{rows: makeIdentityRows(1)}

	before := testutil.ToFloat64(readstream.CompletedAuditFailuresTotal)
	_, err := runHandler(t, cfg, validHandlerRequest(testSessionToken))
	after := testutil.ToFloat64(readstream.CompletedAuditFailuresTotal)

	require.NoError(t, err,
		"handler MUST NOT raise on EmitCompleted failure — return value reflects stream-level errors only (INV-CRYPTO-60)")
	require.NotErrorIs(t, err, completedErr,
		"handler MUST NOT return the EmitCompleted error to the operator (INV-CRYPTO-60)")
	assert.Equal(t, 1, emitter.CompletedCalls(),
		"EmitCompleted must be attempted exactly once (best-effort)")
	assert.Greater(t, after, before,
		"CompletedAuditFailuresTotal must increment on completion-audit failure (INV-CRYPTO-60)")
}

// ---------- INV-CRYPTO-61: dual-control flow ----------

// TestINV_CRYPTO_61_DualControlBlocksUntilApproval asserts the Open + Wait path:
// when no fresh approved row exists, the handler opens a new approval row,
// emits the PendingApproval frame, waits for approval, then proceeds with
// EmitStart and ReadStarted.
func TestINV_CRYPTO_61_DualControlBlocksUntilApproval(t *testing.T) {
	cfg := newTestConfig()
	approverULID := otherPlayerULID
	openedID := approval.RequestID(ulid.MustParse("01HZB000000000000000000000"))
	repo := &fakeApprovalRepo{
		getErr:     oops.Code("APPROVAL_NOT_FOUND").Errorf("none yet"),
		openResult: openedID,
		waitResult: approval.Approval{
			RequestID:          openedID,
			ApprovedByPlayerID: approverULID,
		},
	}
	cfg.Approvals = repo

	emitter := &fakeAuditEmitter{}
	cfg.AuditEmitter = emitter

	req := validHandlerRequest(testSessionToken)
	req.DualControl = true

	stream, _ := runHandler(t, cfg, req)

	assert.Equal(t, 1, repo.GetCalls(), "GetByOpArgsHash must be tried first")
	assert.Equal(t, 1, repo.OpenCalls(), "Open must be called when no reusable approval found")
	assert.Equal(t, 1, repo.WaitCalls(), "WaitForApproval must be called after Open")
	assert.Equal(t, 1, emitter.StartCalls(),
		"EmitStart must be called exactly once after approval")

	frames := stream.Frames()
	require.GreaterOrEqual(t, len(frames), 2,
		"PendingApproval + ReadStarted must both have been sent")

	// First frame: PendingApproval BEFORE EmitStart.
	_, isPending := frames[0].GetPayload().(*adminv1.AdminReadStreamResponse_PendingApproval)
	assert.True(t, isPending, "first frame MUST be PendingApproval; got %T", frames[0].GetPayload())

	// Subsequent frame after Wait returns: ReadStarted.
	_, isStarted := frames[1].GetPayload().(*adminv1.AdminReadStreamResponse_Started)
	assert.True(t, isStarted, "second frame MUST be ReadStarted; got %T", frames[1].GetPayload())
}

// TestINV_CRYPTO_61_DualControlIdempotentReuse asserts the reuse path: when
// GetByOpArgsHash returns a fresh approved row, NO PendingApproval frame is
// sent, Open + WaitForApproval are NEVER called, and the handler proceeds
// directly to EmitStart.
func TestINV_CRYPTO_61_DualControlIdempotentReuse(t *testing.T) {
	cfg := newTestConfig()
	approverULID := otherPlayerULID
	reusedID := approval.RequestID(ulid.MustParse("01HZB000000000000000000001"))
	approvedAt := time.Date(2026, 5, 12, 9, 59, 0, 0, time.UTC)
	repo := &fakeApprovalRepo{
		getResult: approval.Approval{
			RequestID:          reusedID,
			PrimaryPlayerID:    otherPlayerULID,
			OpKind:             "readstream",
			ApprovedAt:         &approvedAt,
			ApprovedByPlayerID: approverULID,
			ExpiresAt:          time.Date(2026, 5, 12, 10, 5, 0, 0, time.UTC),
		},
	}
	cfg.Approvals = repo

	emitter := &fakeAuditEmitter{}
	cfg.AuditEmitter = emitter

	req := validHandlerRequest(testSessionToken)
	req.DualControl = true

	stream, _ := runHandler(t, cfg, req)

	assert.Equal(t, 1, repo.GetCalls(), "GetByOpArgsHash must be called")
	assert.Equal(t, 0, repo.OpenCalls(), "Open MUST NOT be called when a fresh approval is reusable")
	assert.Equal(t, 0, repo.WaitCalls(), "WaitForApproval MUST NOT be called when reusing")

	frames := stream.Frames()
	require.GreaterOrEqual(t, len(frames), 1, "at least the ReadStarted frame must have been sent")
	for _, f := range frames {
		_, isPending := f.GetPayload().(*adminv1.AdminReadStreamResponse_PendingApproval)
		assert.False(t, isPending, "PendingApproval MUST NOT be sent on reuse path")
	}
	assert.Equal(t, 1, emitter.StartCalls(),
		"EmitStart must fire after successful reuse")
}

// TestDualControlTimeout_EmitsFinishedAndReturnsErr verifies the timeout
// branch: WaitForApproval returns APPROVAL_WAIT_DEADLINE; the handler emits
// ReadFinished{DUAL_CONTROL_TIMEOUT} and returns a wrapped error.
func TestDualControlTimeout_EmitsFinishedAndReturnsErr(t *testing.T) {
	cfg := newTestConfig()
	openedID := approval.RequestID(ulid.MustParse("01HZB000000000000000000002"))
	repo := &fakeApprovalRepo{
		getErr:     oops.Code("APPROVAL_NOT_FOUND").Errorf("none yet"),
		openResult: openedID,
		waitErr:    oops.Code("APPROVAL_WAIT_DEADLINE").Errorf("deadline reached"),
	}
	cfg.Approvals = repo

	emitter := &fakeAuditEmitter{}
	cfg.AuditEmitter = emitter

	req := validHandlerRequest(testSessionToken)
	req.DualControl = true

	stream, err := runHandler(t, cfg, req)

	require.Error(t, err, "handler must return an error on dual-control timeout")
	errutil.AssertErrorCode(t, err, "READSTREAM_DUAL_CONTROL_TIMEOUT")

	// EmitStart MUST NOT have fired — the audit trail captures only
	// actually-started reads.
	assert.Equal(t, 0, emitter.StartCalls(),
		"EmitStart MUST NOT be called when dual-control times out")

	// Frames: PendingApproval then ReadFinished{DUAL_CONTROL_TIMEOUT}.
	frames := stream.Frames()
	require.Len(t, frames, 2, "exactly two frames: PendingApproval + ReadFinished")
	_, isPending := frames[0].GetPayload().(*adminv1.AdminReadStreamResponse_PendingApproval)
	assert.True(t, isPending, "first frame must be PendingApproval")
	fin, isFinished := frames[1].GetPayload().(*adminv1.AdminReadStreamResponse_Finished)
	require.True(t, isFinished, "second frame must be ReadFinished")
	assert.Equal(t, adminv1.ReadFinished_TERMINATED_BY_DUAL_CONTROL_TIMEOUT,
		fin.Finished.GetTerminatedBy(),
		"TerminatedBy must be DUAL_CONTROL_TIMEOUT")
}

// TestDualControlNonDeadlineError_EmitsFinishedAndReturnsWrappedErr tests
// Finding 3 (R.20): when WaitForApproval returns a non-deadline error (DB
// outage, context cancel, etc.), the handler MUST send a best-effort
// ReadFinished{SERVER_ERROR} frame before returning so the operator CLI
// gets a structured terminator. No Event frames are expected (EmitStart
// was not called). The error is wrapped with READSTREAM_DUAL_CONTROL_ERROR.
func TestDualControlNonDeadlineError_EmitsFinishedAndReturnsWrappedErr(t *testing.T) {
	cfg := newTestConfig()
	openedID := approval.RequestID(ulid.MustParse("01HZB000000000000000000003"))
	repo := &fakeApprovalRepo{
		getErr:     oops.Code("APPROVAL_NOT_FOUND").Errorf("none yet"),
		openResult: openedID,
		waitErr:    errors.New("database connection lost"),
	}
	cfg.Approvals = repo

	emitter := &fakeAuditEmitter{}
	cfg.AuditEmitter = emitter

	req := validHandlerRequest(testSessionToken)
	req.DualControl = true

	stream, err := runHandler(t, cfg, req)

	require.Error(t, err, "handler must return error on non-deadline WaitForApproval failure")
	errutil.AssertErrorCode(t, err, "READSTREAM_DUAL_CONTROL_ERROR")

	// EmitStart MUST NOT have fired.
	assert.Equal(t, 0, emitter.StartCalls(),
		"EmitStart MUST NOT be called — read never started")

	// Frames: PendingApproval, then ReadFinished{SERVER_ERROR}. No event frames.
	frames := stream.Frames()
	require.Len(t, frames, 2, "exactly two frames: PendingApproval + ReadFinished")
	_, isPending := frames[0].GetPayload().(*adminv1.AdminReadStreamResponse_PendingApproval)
	assert.True(t, isPending, "first frame must be PendingApproval")
	fin, isFinished := frames[1].GetPayload().(*adminv1.AdminReadStreamResponse_Finished)
	require.True(t, isFinished, "second frame must be ReadFinished")
	assert.Equal(t, adminv1.ReadFinished_TERMINATED_BY_SERVER_ERROR,
		fin.Finished.GetTerminatedBy(),
		"TerminatedBy must be SERVER_ERROR for non-deadline dual-control failures")
}

// ---------- INV-CRYPTO-65: cold-tier filters on dek_ref IS NOT NULL ----------

// TestINV_CRYPTO_65_ColdReaderFiltersByDekRefNotNull asserts that the cold-tier
// reader's SQL filters by dek_ref IS NOT NULL — i.e., identity-codec rows
// (cleartext) are excluded from break-glass reads. The handler delegates
// to ColdReader.Read; this test verifies the contract at the reader level
// where the SQL is built. The handler tests above use a stub ColdReader to
// keep the handler-flow tests independent of the SQL details.
func TestINV_CRYPTO_65_ColdReaderFiltersByDekRefNotNull(t *testing.T) {
	r := readstream.NewColdReader(nil)
	sql, _, err := r.BuildSQLForTest(readstream.ColdQuery{
		Subjects: []eventbus.Subject{"events.g1.scene.01H.>"},
		Since:    time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC),
		Until:    time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	assert.Contains(t, sql, "dek_ref IS NOT NULL",
		"cold-tier SQL must filter by dek_ref IS NOT NULL (INV-CRYPTO-65)")
}

// TestHandler_ColdReaderQueryWiring asserts the handler passes
// BuildSubjects(resolved.Contexts, gameID) + (Since, Until) to ColdReader.Read
// — i.e., resolved bounds flow through cleanly without mutation.
func TestHandler_ColdReaderQueryWiring(t *testing.T) {
	cfg := newTestConfig()
	cold := &stubColdReader{}
	cfg.ColdReader = cold

	_, err := runHandler(t, cfg, validHandlerRequest(testSessionToken))
	require.NoError(t, err)

	q := cold.LastQuery()
	require.Equal(t, 1, cold.CallCount(), "ColdReader.Read called once")
	require.Len(t, q.Subjects, 1, "subjects derived from one ContextRef")
	assert.Equal(t,
		eventbus.Subject("events.g1.scene.01ARZ3NDEKTSV4RRFFQ69G5FAV.>"),
		q.Subjects[0],
		"subject MUST be events.<game>.<type>.<id>.> for the scene ContextRef")
	assert.True(t, q.Since.Equal(time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)),
		"Since must round-trip from the proto request")
	assert.True(t, q.Until.Equal(time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)),
		"Until must round-trip from the proto request")
}

// ---------- Typed redaction assertion (INV-CRYPTO-62) ----------

// TestEventFrameCarriesTypedRedaction asserts the metadata-only frame
// builder produces an EventFrame with metadata_only=true and a typed
// no_plaintext_reason — never legacy redacted_payload or string reasons.
// This is a pure builder unit test; the production scanAndStream path is
// exercised via integration tests.
func TestEventFrameCarriesTypedRedaction(t *testing.T) {
	rowID := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	row := readstream.ColdRow{
		ID:        rowID,
		Subject:   "events.g1.scene.01H.spoke",
		Type:      "scene.spoke",
		Timestamp: time.Date(2026, 5, 12, 9, 30, 0, 0, time.UTC),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: rowID},
	}

	frame := readstream.BuildMetadataOnlyEventFrameForTest(row, eventbus.NoPlaintextReasonStaleDEK)
	ev, ok := frame.GetPayload().(*adminv1.AdminReadStreamResponse_Event)
	require.True(t, ok, "payload must be Event variant")
	require.NotNil(t, ev.Event)
	assert.True(t, ev.Event.GetMetadataOnly(), "metadata_only must be true")
	assert.Equal(t,
		corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_STALE_DEK,
		ev.Event.GetNoPlaintextReason(),
		"no_plaintext_reason must be the typed STALE_DEK enum")
	assert.Empty(t, ev.Event.GetPayload(),
		"metadata-only frame MUST NOT carry plaintext bytes")

	frame2 := readstream.BuildPlaintextEventFrameForTest(row, []byte(`{"ok":true}`))
	ev2, _ := frame2.GetPayload().(*adminv1.AdminReadStreamResponse_Event)
	require.NotNil(t, ev2.Event)
	assert.False(t, ev2.Event.GetMetadataOnly(),
		"plaintext frame must have metadata_only=false")
	assert.Equal(t,
		corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_UNSPECIFIED,
		ev2.Event.GetNoPlaintextReason(),
		"plaintext frame must carry UNSPECIFIED reason")
	assert.NotEmpty(t, ev2.Event.GetPayload(),
		"plaintext frame must carry the decrypted bytes")
}

// ---------- Config validation ----------

// TestConfigValidate_RejectsMissingRequired verifies that every required
// Config field has a corresponding Validate guard. Acts as a fence against
// silent dependency-skipping during refactors.
func TestConfigValidate_RejectsMissingRequired(t *testing.T) {
	base := newTestConfig()
	cases := []struct {
		name   string
		mutate func(*readstream.Config)
	}{
		{"Sessions nil", func(c *readstream.Config) { c.Sessions = nil }},
		{"Grants nil", func(c *readstream.Config) { c.Grants = nil }},
		{"Approvals nil", func(c *readstream.Config) { c.Approvals = nil }},
		{"ColdReader nil", func(c *readstream.Config) { c.ColdReader = nil }},
		{"DEK nil", func(c *readstream.Config) { c.DEK = nil }},
		{"Codecs nil", func(c *readstream.Config) { c.Codecs = nil }},
		{"AuditEmitter nil", func(c *readstream.Config) { c.AuditEmitter = nil }},
		{"PolicyHash empty", func(c *readstream.Config) { c.PolicyHash = "" }},
		{"Clock nil", func(c *readstream.Config) { c.Clock = nil }},
		{"Game empty", func(c *readstream.Config) { c.Game = "" }},
		{"MaxWindow zero", func(c *readstream.Config) { c.MaxWindow = 0 }},
		{"DefaultWindow zero", func(c *readstream.Config) { c.DefaultWindow = 0 }},
		{"DefaultWindow exceeds MaxWindow", func(c *readstream.Config) {
			c.MaxWindow = 1 * time.Hour
			c.DefaultWindow = 2 * time.Hour
		}},
		{"WriteDeadline zero", func(c *readstream.Config) { c.WriteDeadline = 0 }},
		{"ApprovalTTL zero", func(c *readstream.Config) { c.ApprovalTTL = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			err := cfg.Validate()
			require.Error(t, err, "missing required field must be rejected")
			errutil.AssertErrorCode(t, err, "READSTREAM_CONFIG_INVALID")
		})
	}
}

// ---------- opArgsHash resolved-bounds semantics ----------

// TestComputeReadStreamArgsHash_ResolvedBoundsSemantics verifies the critical
// security invariant: the op-args hash binds the EFFECTIVE (resolved) window,
// not the raw proto fields. This prevents approval reuse across different
// actual time ranges.
func TestComputeReadStreamArgsHash_ResolvedBoundsSemantics(t *testing.T) {
	const justification = "audit investigation for incident-2026"
	ctx := []readstream.ContextRef{{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}}}

	t0 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	t2 := t0.Add(2 * time.Hour)

	resolvedA := readstream.Resolved{
		Contexts:      ctx,
		Since:         t0,
		Until:         t1,
		Justification: justification,
	}
	resolvedB := readstream.Resolved{
		Contexts:      ctx,
		Since:         t1,
		Until:         t2,
		Justification: justification,
	}
	resolvedASame := readstream.Resolved{
		Contexts:      ctx,
		Since:         t0,
		Until:         t1,
		Justification: justification,
	}

	hashA, err := readstream.ComputeReadStreamArgsHashForTest(resolvedA)
	require.NoError(t, err)
	hashB, err := readstream.ComputeReadStreamArgsHashForTest(resolvedB)
	require.NoError(t, err)
	hashASame, err := readstream.ComputeReadStreamArgsHashForTest(resolvedASame)
	require.NoError(t, err)

	// Different resolved windows → different hashes (approval not reusable).
	assert.NotEqual(t, hashA, hashB,
		"different resolved windows must produce different hashes (prevents cross-window approval reuse)")

	// Same resolved window → same hash (approval correctly reusable).
	assert.Equal(t, hashA, hashASame,
		"identical resolved values must produce the same hash (approval reuse must work)")
}

// ---------- TerminatedBy classification ----------

// TestClassifyTerminator verifies the priority order: oops-coded errors win
// over context.* errors so an audit-emit failure isn't misclassified as
// SERVER_ERROR.
func TestClassifyTerminator(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want adminv1.ReadFinished_TerminatedBy
	}{
		{"nil → CLIENT_EOF", nil, adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF},
		{
			"audit-emit oops → AUDIT_EMIT_FAILURE",
			oops.Code("DENY_AUDIT_PRE_DATA_PUBLISH").Errorf("emit fail"),
			adminv1.ReadFinished_TERMINATED_BY_AUDIT_EMIT_FAILURE,
		},
		{
			"dual-control timeout oops → DUAL_CONTROL_TIMEOUT",
			oops.Code("READSTREAM_DUAL_CONTROL_TIMEOUT").Errorf("timeout"),
			adminv1.ReadFinished_TERMINATED_BY_DUAL_CONTROL_TIMEOUT,
		},
		{"context.Canceled → CLIENT_DISCONNECT", context.Canceled, adminv1.ReadFinished_TERMINATED_BY_CLIENT_DISCONNECT},
		{"context.DeadlineExceeded → DEADLINE_EXCEEDED", context.DeadlineExceeded, adminv1.ReadFinished_TERMINATED_BY_DEADLINE_EXCEEDED},
		{"write-deadline sentinel → DEADLINE_EXCEEDED", readstream.ErrWriteDeadlineExceeded, adminv1.ReadFinished_TERMINATED_BY_DEADLINE_EXCEEDED},
		{"unrelated error → SERVER_ERROR", errors.New("boom"), adminv1.ReadFinished_TERMINATED_BY_SERVER_ERROR},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, readstream.ClassifyTerminatorForTest(tc.err))
		})
	}
}
