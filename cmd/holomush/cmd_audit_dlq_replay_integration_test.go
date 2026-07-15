// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

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

	"github.com/holomush/holomush/internal/bootstrap"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/testsupport/natstest"
)

// provisionReplayDLQStream creates the bounded EVENTS_AUDIT_DLQ stream on a
// REAL broker with the game-scoped subject filter, mirroring dlq.go
// EnsureStream (LimitsPolicy so reads/acks never delete a dead letter).
func provisionReplayDLQStream(t *testing.T, js jetstream.JetStream, dlqPrefix string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      audit.DefaultDLQStreamName,
		Subjects:  []string{dlqPrefix + ".>"},
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.MemoryStorage,
		Replicas:  1,
		MaxBytes:  -1,
	})
	require.NoError(t, err)
}

// seedReplayDeadLetter publishes a well-formed captured audit message to the
// DLQ exactly as dlq.go Capture would: the original event subject encoded as
// the DLQ subject suffix, full audit headers preserved. Returns the ULID used
// as Nats-Msg-Id and the original event subject.
func seedReplayDeadLetter(t *testing.T, js jetstream.JetStream, dlqPrefix string) (ulid.ULID, string) {
	t.Helper()
	id := ulid.Make()
	const origSubject = "events.thing.arrive"
	msg := &nats.Msg{
		Subject: dlqPrefix + "." + origSubject,
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

// auditRowCount returns the number of rows in events_audit.
func auditRowCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM events_audit`).Scan(&n))
	return n
}

// TestAuditDLQReplayResolvesGameIDForExternalNATS proves the F3 fix end-to-end
// against a REAL external NATS broker: the CLI's game_id resolver
// (resolveGameID → dlqConfigForGame → audit.ReplayDLQ) is driven with a game_id
// that DIVERGES from the seeded, server-persisted ULID game.
//
// Unlike the old same-"main"-on-both-sides embedded-NATS tautology, this
// exercises the ACTUAL unexported resolver seam (reachable only because the
// test is package main) with a natstest container — the external-mode shape the
// shared in-process eventbustest connection cannot express (.claude/rules/testing.md):
//
//   - FAILURE GUARD: a WRONG --game-id override yields a DLQ subject prefix that
//     mismatches the seeded dead letter → fail-loud (Failed>0), ZERO events_audit
//     rows (no cross-tenant mis-write — T-06-5-01).
//   - RECOVERY: override empty AND core.game_id empty → resolveGameID reads the
//     seeded holomush_system_info ULID from the DB (its real DB leg) → the prefix
//     matches → Replayed==1 and events_audit.subject == the original event subject.
func TestAuditDLQReplayResolvesGameIDForExternalNATS(t *testing.T) {
	ctx := context.Background()

	// --- Postgres with the composite-PK events_audit (000052, via 06-01) ---
	connStr, cleanup := startPostgresContainer(t)
	t.Cleanup(cleanup)
	require.NoError(t, runAutoMigration(connStr, func(url string) (bootstrap.AutoMigrator, error) {
		return store.NewMigrator(url)
	}))
	es, err := store.NewPostgresEventStore(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(es.Close)
	pool := es.Pool()
	require.NotNil(t, pool)

	// Seed a SERVER-STYLE ULID game_id so the resolver's DB leg is real, not a
	// magic string. The server persists a ULID; the CLI must recover the same.
	seededGame := ulid.Make().String()
	require.NoError(t, es.SetSystemInfo(ctx, "game_id", seededGame))

	// --- Real single-node NATS JetStream container (external-mode harness) ---
	env, err := natstest.StartNATS(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = env.Terminate(context.Background()) })
	js, err := jetstream.New(env.Conn(t))
	require.NoError(t, err)

	dlqPrefix := "internal." + seededGame + ".audit.dlq"
	provisionReplayDLQStream(t, js, dlqPrefix)
	id, origSubject := seedReplayDeadLetter(t, js, dlqPrefix)

	require.Equal(t, 0, auditRowCount(t, pool), "seeded dead letter must not be in events_audit before replay")

	// --- FAILURE GUARD: a divergent game_id must fail loud, never mis-write ---
	wrongGame := ulid.Make().String()
	require.NotEqual(t, seededGame, wrongGame)
	resolvedWrong, err := resolveGameID(ctx, es.GetSystemInfo, wrongGame, "")
	require.NoError(t, err)
	require.Equal(t, wrongGame, resolvedWrong, "a non-empty override must win over the DB value")

	resFail, err := audit.ReplayDLQ(ctx, js, pool, dlqConfigForGame(resolvedWrong), audit.ReplayOptions{})
	require.NoError(t, err)
	assert.Positive(t, resFail.Failed, "a divergent game_id prefix must fail-loud (Failed>0), not mis-write")
	assert.Equal(t, 0, resFail.Replayed, "nothing may be replayed under a mismatched prefix")
	assert.Equal(t, 0, auditRowCount(t, pool), "a game_id mismatch must write ZERO events_audit rows")

	// --- RECOVERY: resolver reads the seeded DB ULID (override+core empty) ---
	resolvedOK, err := resolveGameID(ctx, es.GetSystemInfo, "", "")
	require.NoError(t, err)
	require.Equal(t, seededGame, resolvedOK, "resolveGameID must recover the persisted DB game_id")

	resOK, err := audit.ReplayDLQ(ctx, js, pool, dlqConfigForGame(resolvedOK), audit.ReplayOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, resOK.Replayed, "the resolved-from-DB game_id must recover the dead letter")
	assert.Equal(t, 0, resOK.Failed, "a well-formed dead letter under the correct prefix must not fail")

	require.Equal(t, 1, auditRowCount(t, pool), "recovery must write exactly one events_audit row")
	var subject string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT subject FROM events_audit WHERE id = $1`, id.Bytes()).Scan(&subject))
	assert.Equal(t, origSubject, subject,
		"replay must store the ORIGINAL event subject, not the DLQ-wrapped subject")
}
