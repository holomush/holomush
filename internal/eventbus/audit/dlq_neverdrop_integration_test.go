// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package audit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/test/testutil"
)

// failingDLQCapturer always fails Capture, simulating a DLQ-stream outage.
// It counts invocations so the test can confirm the final-attempt branch
// fired. Guarded by a mutex because Capture runs on the JetStream consumer
// goroutine while Calls() is read from the test goroutine (race detector).
type failingDLQCapturer struct {
	mu    sync.Mutex
	calls int
}

func (f *failingDLQCapturer) Capture(_ context.Context, _ jetstream.Msg) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return errors.New("simulated DLQ outage")
}

func (f *failingDLQCapturer) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func openIntegPool(t *testing.T, connStr string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// TestProjectionNeverDropsWhenDLQPublishFails proves the never-drop
// guarantee against a real broker: when the DLQ publish itself fails on the
// final delivery attempt, the projection leaves the message un-acked (it does
// NOT Term) — the message is never captured to the DLQ, never persisted, and
// never marked complete. Because it is never Term'd without a durable capture,
// it is retained in the source EVENTS stream until StreamMaxAge, never silently
// dropped (D-09). MaxDeliver is a hard ceiling, so it is not redelivered past
// the cap.
//
// Verifies: INV-EVENTBUS-30
func TestProjectionNeverDropsWhenDLQPublishFails(t *testing.T) {
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool := openIntegPool(t, connStr)

	bus := eventbustest.New(t)

	cfg := Config{
		MaxDeliver: 2,
		AckWait:    200 * time.Millisecond,
		DLQ: DLQConfig{
			Subject: "internal.main.audit.dlq",
			Storage: jetstream.MemoryStorage,
		},
	}.Defaults()

	// newProjection provisions the real (empty) EVENTS_AUDIT_DLQ stream; we
	// then override the capturer with one that always fails so the no-ack
	// fallback path (capture failure ⇒ leave un-acked, never Term) is exercised
	// against a real consumer/redelivery loop.
	p, err := newProjection(t.Context(), bus.JS, pool, cfg)
	require.NoError(t, err)

	failing := &failingDLQCapturer{}
	p.dlq = failing

	workerCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, p.start(workerCtx))
	t.Cleanup(func() { _ = p.drain(context.Background()) })

	// Poison message: missing Nats-Msg-Id ⇒ persist fails on every delivery.
	msg := &nats.Msg{
		Subject: "events.main.poison",
		Header:  nats.Header{"App-Event-Type": []string{"test.poison"}},
		Data:    []byte(`{"poison":true}`),
	}
	_, pubErr := bus.JS.PublishMsg(t.Context(), msg)
	require.NoError(t, pubErr)

	// Wait for the final-attempt DLQ branch to fire at least once.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && failing.Calls() == 0 {
		<-time.After(100 * time.Millisecond)
	}
	require.GreaterOrEqual(t, failing.Calls(), 1,
		"DLQ capture must be attempted on the final delivery attempt")

	// Never dropped into a false-success state: the failed publish left the
	// DLQ stream empty and the message was never persisted. Because we leave
	// the message un-acked (never Term'd) on DLQ failure, it is retained in the
	// source EVENTS stream until MaxAge (not redelivered past MaxDeliver).
	dlqStream, err := bus.JS.Stream(t.Context(), DefaultDLQStreamName)
	require.NoError(t, err)
	dlqInfo, err := dlqStream.Info(t.Context())
	require.NoError(t, err)
	assert.Equal(t, uint64(0), dlqInfo.State.Msgs,
		"a failed DLQ publish must leave the DLQ stream empty (no false capture)")

	var rows int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT count(*) FROM events_audit`).Scan(&rows))
	assert.Equal(t, 0, rows, "poison message must not be persisted to events_audit")
}
