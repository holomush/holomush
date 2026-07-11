// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
)

// ackCountingMsg augments stubMsg with an Ack counter so replay tests can
// assert the ephemeral consumer cursor is advanced on every branch.
type ackCountingMsg struct {
	stubMsg
	ackCalls int
}

func (m *ackCountingMsg) Ack() error { m.ackCalls++; return nil }

func TestReplayOneSkipsOnMsgIDMismatch(t *testing.T) {
	t.Parallel()

	h := nats.Header{}
	h.Set(headerMsgID, ulid.Make().String())
	msg := &ackCountingMsg{stubMsg: stubMsg{headers: h, subject: "events.main.thing"}}

	var result ReplayResult
	// pool is nil: the mismatch path must return before any DB access.
	replayOne(context.Background(), nil, msg, ulid.Make().String(), "internal.main.audit.dlq", &result)

	assert.Equal(t, 1, result.Skipped, "non-matching message must be counted skipped")
	assert.Equal(t, 0, result.Replayed)
	assert.Equal(t, 0, result.Failed)
	assert.Equal(t, 1, msg.ackCalls, "skipped message must be acked to advance the cursor")
}

func TestReplayOneCountsPoisonAsFailedAndRetains(t *testing.T) {
	t.Parallel()

	// Subject carries the expected DLQ prefix (so it passes the prefix check),
	// but has no Nats-Msg-Id header ⇒ writeAuditRow returns AUDIT_MISSING_HEADER
	// before touching the (nil) pool — a genuinely-poison dead letter.
	msg := &ackCountingMsg{stubMsg: stubMsg{headers: nats.Header{}, subject: "internal.main.audit.dlq.events.main.poison"}}

	var result ReplayResult
	replayOne(context.Background(), nil, msg, "", "internal.main.audit.dlq", &result)

	assert.Equal(t, 1, result.Failed, "poison message must be counted failed")
	assert.Equal(t, 0, result.Replayed)
	assert.Equal(t, 0, result.Skipped)
	assert.Equal(t, 1, msg.ackCalls,
		"failed message is acked to advance the cursor; LimitsPolicy retention keeps it in the DLQ")
}

func TestReplayOneCountsPrefixMismatchAsFailedWithoutPersisting(t *testing.T) {
	t.Parallel()

	// Subject does NOT carry the expected prefix (e.g. replay run with a
	// game_id that differs from capture time). replayOne must fail loud —
	// count Failed and ack (retain), never persist a corrupted subject. The
	// nil pool proves no DB write is attempted on this path.
	h := nats.Header{}
	h.Set(headerMsgID, ulid.Make().String())
	msg := &ackCountingMsg{stubMsg: stubMsg{headers: h, subject: "events.main.thing"}}

	var result ReplayResult
	replayOne(context.Background(), nil, msg, "", "internal.other.audit.dlq", &result)

	assert.Equal(t, 1, result.Failed, "prefix-mismatched message must be counted failed")
	assert.Equal(t, 0, result.Replayed, "a corrupted subject must never be persisted")
	assert.Equal(t, 0, result.Skipped)
	assert.Equal(t, 1, msg.ackCalls,
		"mismatched message is acked to advance the cursor; it stays in the DLQ for a corrected re-run")
}

func TestOriginalSubjectStripsDLQPrefix(t *testing.T) {
	t.Parallel()
	got, ok := originalSubject("internal.main.audit.dlq.events.main.thing", "internal.main.audit.dlq")
	assert.True(t, ok, "a clean prefix match reports ok")
	assert.Equal(t, "events.main.thing", got)
}

func TestOriginalSubjectReportsMismatchOnWrongPrefix(t *testing.T) {
	t.Parallel()
	// A DLQ subject that does not carry the expected prefix must report NOT-ok
	// so the caller fails loud instead of persisting a corrupted subject.
	_, ok := originalSubject("events.main.thing", "internal.main.audit.dlq")
	assert.False(t, ok, "a prefix mismatch must report not-ok, never silently pass through")
}

func TestOriginalSubjectReportsMismatchOnEmptyPrefix(t *testing.T) {
	t.Parallel()
	// An empty prefix cannot be validated against, so it is treated as a
	// mismatch (fail-loud) rather than passing the subject through unchanged.
	_, ok := originalSubject("internal.main.audit.dlq.events.main.thing", "")
	assert.False(t, ok, "an empty prefix must report not-ok")
}
