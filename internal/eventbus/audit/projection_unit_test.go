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
	h.Set("App-Rendering",
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
	// validHeaders now sets App-Rendering; remove it for the negative case.
	h.Del("App-Rendering")
	msg := &stubMsg{
		headers: h,
		subject: "events.main.character.01ABC",
		meta:    &jetstream.MsgMetadata{Sequence: jetstream.SequencePair{Stream: 1}},
	}
	err := p.persist(msg)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_MISSING_HEADER")
	errutil.AssertErrorContext(t, err, "header", "App-Rendering")
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
