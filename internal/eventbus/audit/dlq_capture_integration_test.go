// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package audit_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	holotestutil "github.com/holomush/holomush/test/testutil"
)

// dlqStreamName is the dead-letter stream the projection provisions.
const dlqStreamName = "EVENTS_AUDIT_DLQ"

// publishPoison publishes a message that fails persist() deterministically
// (missing Nats-Msg-Id) so it exhausts MaxDeliver and reaches the DLQ path.
func publishPoison(t *testing.T, js jetstream.JetStream, subject string) {
	t.Helper()
	msg := &nats.Msg{
		Subject: subject,
		Header: nats.Header{
			// Deliberately NO Nats-Msg-Id: persist() rejects it immediately
			// with AUDIT_MISSING_HEADER, so every delivery fails.
			"App-Event-Type": []string{"test.poison"},
		},
		Data: []byte(`{"poison":true}`),
	}
	_, err := js.PublishMsg(t.Context(), msg)
	require.NoError(t, err)
}

// dlqStreamMsgs returns the message count of the DLQ stream, or -1 if the
// stream cannot be read yet.
func dlqStreamMsgs(t *testing.T, js jetstream.JetStream) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	stream, err := js.Stream(ctx, dlqStreamName)
	if err != nil {
		return -1
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return -1
	}
	return int64(info.State.Msgs)
}

// TestProjectionCapturesPoisonToDLQAfterMaxDeliver proves the CLUSTER-04
// capture half end-to-end against a real broker: a poison message that
// exhausts MaxDeliver lands in EVENTS_AUDIT_DLQ and the Prometheus counter
// increments.
//
// Verifies: INV-EVENTBUS-30
func TestProjectionCapturesPoisonToDLQAfterMaxDeliver(t *testing.T) {
	shared := holotestutil.SharedPostgres(t)
	connStr := holotestutil.FreshDatabase(t, shared)
	pool := openPool(t, connStr)

	bus := eventbustest.New(t)

	before := testutil.ToFloat64(audit.DLQMessagesTotal)

	sub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{
		MaxDeliver: 2,
		AckWait:    200 * time.Millisecond,
		DLQ: audit.DLQConfig{
			Subject: "internal.main.audit.dlq",
			Storage: jetstream.MemoryStorage,
		},
	})
	require.NoError(t, sub.Start(t.Context()))
	t.Cleanup(func() { _ = sub.Stop(context.Background()) })

	publishPoison(t, bus.JS, "events.main.poison")

	// After MaxDeliver (2) delivery attempts × ~200ms AckWait, the final
	// attempt captures the message to the DLQ. Poll the DLQ stream until the
	// message lands (or fail the test on timeout).
	deadline := time.Now().Add(10 * time.Second)
	var msgs int64
	for time.Now().Before(deadline) {
		if msgs = dlqStreamMsgs(t, bus.JS); msgs >= 1 {
			break
		}
		<-time.After(100 * time.Millisecond)
	}
	require.GreaterOrEqual(t, msgs, int64(1),
		"poison message should have been captured to EVENTS_AUDIT_DLQ after exhausting MaxDeliver")

	after := testutil.ToFloat64(audit.DLQMessagesTotal)
	assert.InDelta(t, 1.0, after-before, 0.5,
		"holomush_audit_dlq_messages_total should increment once for the captured message")

	// The poison message must NOT have been persisted to events_audit
	// (persist failed on every attempt) — it lives only in the DLQ now.
	assert.Equal(t, 0, countAuditRows(t, pool),
		"poison message must not appear in events_audit")
}
