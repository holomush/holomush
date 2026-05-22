// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// stubMsg is the minimal jetstream.Msg surface persist() reads:
// Headers, Subject, Data, Metadata. Ack/Nak/InProgress/etc. are not
// used on the validation-error path so they're no-ops.
type stubMsg struct {
	headers nats.Header
	subject string
	data    []byte
	meta    *jetstream.MsgMetadata
	metaErr error
}

func (s *stubMsg) Headers() nats.Header { return s.headers }
func (s *stubMsg) Subject() string      { return s.subject }
func (s *stubMsg) Data() []byte         { return s.data }
func (s *stubMsg) Reply() string        { return "" }
func (s *stubMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return s.meta, s.metaErr
}
func (s *stubMsg) Ack() error                         { return nil }
func (s *stubMsg) AckSync() error                     { return nil }
func (s *stubMsg) DoubleAck(_ context.Context) error  { return nil }
func (s *stubMsg) Nak() error                         { return nil }
func (s *stubMsg) NakWithDelay(_ time.Duration) error { return nil }
func (s *stubMsg) InProgress() error                  { return nil }
func (s *stubMsg) Term() error                        { return nil }
func (s *stubMsg) TermWithReason(_ string) error      { return nil }

// validHeaders returns a minimally-valid header set for persist(); tests
// mutate one field at a time to exercise each validation branch.
func validHeaders(t *testing.T) nats.Header {
	t.Helper()
	h := nats.Header{}
	h.Set(headerMsgID, ulid.Make().String())
	h.Set(headerCodec, "identity")
	h.Set(headerEventType, "test.unit")
	h.Set(headerSchemaVersion, "1")
	h.Set(headerActorKind, defaultActorKind)
	h.Set(headerRendering,
		`{"category":"system","format":"narrative",`+
			`"display_target":"EVENT_CHANNEL_TERMINAL","source_plugin":"builtin",`+
			`"source_plugin_version":"host-test","label":""}`)
	return h
}

// newTestProjection constructs a projection with no pool/consumer. Safe
// only for tests that exit persist() before reaching pool.Exec (i.e.,
// any of the validation error branches).
func newTestProjection() *projection {
	return &projection{cfg: Config{}.Defaults()}
}

func TestPersistRejectsMissingRequiredHeaders(t *testing.T) {
	cases := []struct {
		name     string
		stripKey string
		wantCode string
	}{
		{"missing Nats-Msg-Id", headerMsgID, "AUDIT_MISSING_HEADER"},
		{"missing App-Codec", headerCodec, "AUDIT_MISSING_HEADER"},
		{"missing App-Event-Type", headerEventType, "AUDIT_MISSING_HEADER"},
		{"missing App-Schema-Version", headerSchemaVersion, "AUDIT_MISSING_HEADER"},
	}
	p := newTestProjection()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := validHeaders(t)
			h.Del(tc.stripKey)
			msg := &stubMsg{headers: h, subject: "events.main.test"}
			err := p.persist(msg)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, tc.wantCode)
		})
	}
}

func TestPersistRejectsMissingAppRenderingHeader(t *testing.T) {
	p := newTestProjection()
	h := validHeaders(t)
	// validHeaders now sets the rendering header; remove it for the negative case.
	h.Del(headerRendering)
	msg := &stubMsg{
		headers: h,
		subject: "events.main.character.01ABC",
		meta:    &jetstream.MsgMetadata{Sequence: jetstream.SequencePair{Stream: 1}},
	}
	err := p.persist(msg)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_MISSING_HEADER")
	errutil.AssertErrorContext(t, err, "header", headerRendering)
}

func TestPersistRejectsMalformedSchemaVersion(t *testing.T) {
	p := newTestProjection()
	h := validHeaders(t)
	h.Set(headerSchemaVersion, "not-a-number")
	msg := &stubMsg{headers: h, subject: "events.main.test"}
	err := p.persist(msg)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_BAD_SCHEMA_VERSION")
}

func TestPersistRejectsMalformedMsgID(t *testing.T) {
	p := newTestProjection()
	h := validHeaders(t)
	h.Set(headerMsgID, "not-a-ulid")
	// Metadata must parse BEFORE MsgID decode per current code order —
	// but persist decodes MsgID last, so a stub that returns valid
	// metadata is required. Provide a minimal one.
	msg := &stubMsg{
		headers: h,
		subject: "events.main.test",
		meta:    &jetstream.MsgMetadata{Sequence: jetstream.SequencePair{Stream: 1}},
	}
	err := p.persist(msg)
	require.Error(t, err)
	// persist() wraps decodeULIDString's AUDIT_BAD_ULID in AUDIT_BAD_MSG_ID;
	// errutil.AssertErrorCode surfaces the innermost code.
	errutil.AssertErrorCode(t, err, "AUDIT_BAD_ULID")
}

func TestPersistRejectsMalformedActorID(t *testing.T) {
	p := newTestProjection()
	h := validHeaders(t)
	h.Set(headerActorID, "not-a-ulid")
	msg := &stubMsg{
		headers: h,
		subject: "events.main.test",
		meta:    &jetstream.MsgMetadata{Sequence: jetstream.SequencePair{Stream: 1}},
	}
	err := p.persist(msg)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_BAD_ACTOR_ID")
}

func TestPersistPropagatesMetadataError(t *testing.T) {
	p := newTestProjection()
	h := validHeaders(t)
	sentinel := errors.New("metadata broken")
	msg := &stubMsg{
		headers: h,
		subject: "events.main.test",
		metaErr: sentinel,
	}
	err := p.persist(msg)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_METADATA_FAILED")
	require.ErrorIs(t, err, sentinel)
}

func TestDecodeULIDStringAcceptsValid(t *testing.T) {
	id := ulid.Make()
	bytes, err := decodeULIDString(id.String())
	require.NoError(t, err)
	require.Len(t, bytes, 16)
	require.Equal(t, id[:], bytes)
}

func TestDecodeULIDStringRejectsInvalid(t *testing.T) {
	_, err := decodeULIDString("not-a-ulid")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_BAD_ULID")
}

// withShortBackoffs swaps consumerCreateBackoffs for the duration of t
// so retry tests don't pay the production schedule's wall time. The
// schedule shape (number of retries) is preserved so attempt counts
// match the production path; only the per-step sleep shrinks.
func withShortBackoffs(t *testing.T) {
	t.Helper()
	orig := consumerCreateBackoffs
	consumerCreateBackoffs = []time.Duration{1 * time.Millisecond, 2 * time.Millisecond}
	t.Cleanup(func() { consumerCreateBackoffs = orig })
}

func TestCreateConsumerWithRetrySucceedsOnFirstAttempt(t *testing.T) {
	withShortBackoffs(t)
	var attempts int
	cons, err := createConsumerWithRetry(context.Background(), func(_ context.Context) (jetstream.Consumer, error) {
		attempts++
		return nil, nil //nolint:nilnil // test stub
	})
	require.NoError(t, err)
	require.Nil(t, cons, "test stub returns nil consumer; production returns a real one")
	require.Equal(t, 1, attempts, "no retries when first attempt succeeds")
}

func TestCreateConsumerWithRetrySucceedsAfterTransientFailures(t *testing.T) {
	withShortBackoffs(t)
	transient := errors.New("nats: no responders")
	var attempts int
	cons, err := createConsumerWithRetry(context.Background(), func(_ context.Context) (jetstream.Consumer, error) {
		attempts++
		if attempts <= 2 {
			return nil, transient
		}
		return nil, nil //nolint:nilnil // test stub: success after 2 transient errors
	})
	require.NoError(t, err)
	require.Nil(t, cons)
	require.Equal(t, 3, attempts, "should retry through the two-step backoff schedule")
}

func TestCreateConsumerWithRetryGivesUpAfterBudgetExhausted(t *testing.T) {
	withShortBackoffs(t)
	permanent := errors.New("nats: stream not found")
	var attempts int
	_, err := createConsumerWithRetry(context.Background(), func(_ context.Context) (jetstream.Consumer, error) {
		attempts++
		return nil, permanent
	})
	require.ErrorIs(t, err, permanent, "final error MUST be the underlying NATS error so the caller can surface it")
	require.Equal(t, 1+len(consumerCreateBackoffs), attempts, "should attempt initial call + one per backoff entry")
}

func TestCreateConsumerWithRetryRespectsCancelledContext(t *testing.T) {
	withShortBackoffs(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	transient := errors.New("transient")
	var attempts int
	_, err := createConsumerWithRetry(ctx, func(_ context.Context) (jetstream.Consumer, error) {
		attempts++
		return nil, transient
	})
	require.ErrorIs(t, err, transient)
	require.Equal(t, 1, attempts,
		"ctx.Err() check between attempts MUST short-circuit further retries; only the initial attempt should run")
}

func TestCreateConsumerWithRetryHonorsCtxDeadlineDuringBackoff(t *testing.T) {
	// Production backoffs (100ms, 250ms) outlast a 1ms ctx deadline, so
	// the retry loop should exit via the ctx.Done() select arm rather
	// than waiting out the full backoff.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	transient := errors.New("transient")
	start := time.Now()
	_, err := createConsumerWithRetry(ctx, func(_ context.Context) (jetstream.Consumer, error) {
		return nil, transient
	})
	elapsed := time.Since(start)
	require.ErrorIs(t, err, transient)
	require.Less(t, elapsed, 50*time.Millisecond,
		"ctx deadline MUST short-circuit the backoff sleep; production 100ms backoff would otherwise dominate")
}

// TestNewProjectionWrapSurfacesUnderlyingNATSError pins the observability
// contract: when CreateOrUpdateConsumer ultimately fails, the wrapped
// error MUST surface the underlying NATS error message via a structured
// `nats_err` field, not just the AUDIT_CONSUMER_CREATE_FAILED code.
// Without this, Ginkgo's Succeed() matcher and oops.Code() consumers
// see only the code and cannot diagnose the root cause (the original
// holomush-l015 failure observation lost the underlying error here).
func TestNewProjectionWrapSurfacesUnderlyingNATSError(t *testing.T) {
	withShortBackoffs(t)
	// Use the retry helper directly with a permanent error to exercise
	// the wrap path. newProjection itself can't be reached without a
	// real jetstream.JetStream impl; the wrap shape is the only thing
	// under test here.
	permanent := errors.New("nats: no stream matches subject")
	_, retryErr := createConsumerWithRetry(context.Background(), func(_ context.Context) (jetstream.Consumer, error) {
		return nil, permanent
	})
	require.Error(t, retryErr)

	// Apply the same wrap newProjection applies.
	wrapped := wrapConsumerCreateError(retryErr, "EVENTS", "host_audit_projection")

	errutil.AssertErrorCode(t, wrapped, "AUDIT_CONSUMER_CREATE_FAILED")
	require.ErrorContains(t, wrapped, "no stream matches subject",
		"wrapped error chain MUST contain the underlying NATS message")
	errutil.AssertErrorContext(t, wrapped, "nats_err", permanent.Error())
}
