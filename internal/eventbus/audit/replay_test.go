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
	replayOne(context.Background(), nil, msg, ulid.Make().String(), &result)

	assert.Equal(t, 1, result.Skipped, "non-matching message must be counted skipped")
	assert.Equal(t, 0, result.Replayed)
	assert.Equal(t, 0, result.Failed)
	assert.Equal(t, 1, msg.ackCalls, "skipped message must be acked to advance the cursor")
}

func TestReplayOneCountsPoisonAsFailedAndRetains(t *testing.T) {
	t.Parallel()

	// No Nats-Msg-Id header ⇒ writeAuditRow returns AUDIT_MISSING_HEADER
	// before touching the (nil) pool — a genuinely-poison dead letter.
	msg := &ackCountingMsg{stubMsg: stubMsg{headers: nats.Header{}, subject: "events.main.poison"}}

	var result ReplayResult
	replayOne(context.Background(), nil, msg, "", &result)

	assert.Equal(t, 1, result.Failed, "poison message must be counted failed")
	assert.Equal(t, 0, result.Replayed)
	assert.Equal(t, 0, result.Skipped)
	assert.Equal(t, 1, msg.ackCalls,
		"failed message is acked to advance the cursor; LimitsPolicy retention keeps it in the DLQ")
}

func TestReplayResultZeroValueIsEmpty(t *testing.T) {
	t.Parallel()
	var r ReplayResult
	assert.Equal(t, ReplayResult{}, r)
	assert.Zero(t, r.Scanned)
	assert.Zero(t, r.Replayed)
	assert.Zero(t, r.Skipped)
	assert.Zero(t, r.Failed)
}
