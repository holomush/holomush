// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package audit_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	holotestutil "github.com/holomush/holomush/test/testutil"
)

// replayDLQSubject is the game-scoped DLQ subject prefix used by these
// specs (mirrors core.go's internal.<game_id>.audit.dlq for game "main").
const replayDLQSubject = "internal.main.audit.dlq"

// provisionDLQStream creates the bounded EVENTS_AUDIT_DLQ stream directly
// (MemoryStorage) so a spec can seed it without standing up a projection.
// Mirrors dlq.go EnsureStream: LimitsPolicy retention, subject <prefix>.>.
func provisionDLQStream(t *testing.T, js jetstream.JetStream) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      audit.DefaultDLQStreamName,
		Subjects:  []string{replayDLQSubject + ".>"},
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.MemoryStorage,
		Replicas:  1,
		MaxBytes:  -1,
	})
	require.NoError(t, err)
}

// seedDLQMessage publishes a well-formed captured message to the DLQ,
// exactly as dlq.go Capture would: original subject encoded as the DLQ
// subject suffix, full audit headers preserved (including Nats-Msg-Id).
// Returns the ULID used as Nats-Msg-Id and the original event subject.
func seedDLQMessage(t *testing.T, js jetstream.JetStream) (ulid.ULID, string) {
	t.Helper()
	id := ulid.Make()
	const origSubject = "events.main.thing"
	msg := &nats.Msg{
		Subject: replayDLQSubject + "." + origSubject,
		Header: nats.Header{
			"Nats-Msg-Id":        []string{id.String()},
			"App-Codec":          []string{"identity"},
			"App-Event-Type":     []string{"test.replay"},
			"App-Schema-Version": []string{"1"},
			"App-Actor-Kind":     []string{"system"},
			"App-Rendering":      []string{`{}`},
		},
		Data: []byte(`{"recovered":true}`),
	}
	_, err := js.PublishMsg(t.Context(), msg)
	require.NoError(t, err)
	return id, origSubject
}

// auditRowSubject returns the subject column for the events_audit row with
// the given id, or ("", false) if no such row exists.
func auditRowSubject(t *testing.T, pool *pgxpool.Pool, id ulid.ULID) (string, bool) {
	t.Helper()
	var subject string
	err := pool.QueryRow(t.Context(),
		`SELECT subject FROM events_audit WHERE id = $1`, id.Bytes()).Scan(&subject)
	if err != nil {
		return "", false
	}
	return subject, true
}

// TestReplayDLQRestoresDeadLetterToAuditTable proves the CLUSTER-04
// recovery half end-to-end against a real broker: a well-formed dead
// letter captured to EVENTS_AUDIT_DLQ (because Postgres was down) is
// re-driven back into events_audit once the DB is healthy — the original
// event subject is recovered, and a second replay does not duplicate the
// row (idempotent).
//
// NOTE: this asserts the CLUSTER-04 *recovery* half (idempotent replay
// restores a dead letter). INV-EVENTBUS-30 covers the *capture* half
// (never-drop Term/Nak) and is bound by the capture/never-drop specs; a
// recovery invariant is a candidate for the phase's registry
// consolidation and is deliberately NOT bound here to avoid a false-green.
func TestReplayDLQRestoresDeadLetterToAuditTable(t *testing.T) {
	shared := holotestutil.SharedPostgres(t)
	connStr := holotestutil.FreshDatabase(t, shared)
	pool := openPool(t, connStr)

	bus := eventbustest.New(t)

	provisionDLQStream(t, bus.JS)
	id, origSubject := seedDLQMessage(t, bus.JS)

	cfg := audit.DLQConfig{Subject: replayDLQSubject, Storage: jetstream.MemoryStorage}

	// Before replay: the dead letter is not in events_audit.
	require.Equal(t, 0, countAuditRows(t, pool),
		"seeded dead letter must not be in events_audit before replay")

	res, err := audit.ReplayDLQ(t.Context(), bus.JS, pool, cfg, audit.ReplayOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Replayed, "the seeded dead letter must be replayed")
	assert.Equal(t, 0, res.Failed, "a well-formed dead letter must not fail")

	// After replay: exactly one row, keyed on the preserved Nats-Msg-Id,
	// carrying the ORIGINAL event subject (recovered from the DLQ suffix).
	require.Equal(t, 1, countAuditRows(t, pool), "dead letter must be recovered into events_audit")
	subject, found := auditRowSubject(t, pool, id)
	require.True(t, found, "the recovered row must be keyed on the original Nats-Msg-Id")
	assert.Equal(t, origSubject, subject,
		"replay must store the original event subject, not the DLQ-wrapped subject")

	// Second replay is idempotent: ON CONFLICT (id) DO NOTHING keeps the
	// row count at one.
	res2, err := audit.ReplayDLQ(t.Context(), bus.JS, pool, cfg, audit.ReplayOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, res2.Replayed, "second pass re-reads the same dead letter")
	assert.Equal(t, 1, countAuditRows(t, pool),
		"a second replay must not duplicate the recovered row")
}
