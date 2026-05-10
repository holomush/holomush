// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package policy_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/admin/policy"
	"github.com/holomush/holomush/internal/store"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	"github.com/holomush/holomush/test/testutil"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	if err != nil {
		panic("StartPostgres: " + err.Error())
	}

	migrator, err := store.NewMigrator(pgEnv.ConnStr)
	if err != nil {
		_ = pgEnv.Terminate(ctx)
		panic("NewMigrator: " + err.Error())
	}
	if err := migrator.Up(); err != nil {
		_ = migrator.Close()
		_ = pgEnv.Terminate(ctx)
		panic("Up: " + err.Error())
	}
	_ = migrator.Close()

	pool, err := pgxpool.New(ctx, pgEnv.ConnStr)
	if err != nil {
		_ = pgEnv.Terminate(ctx)
		panic("pgxpool.New: " + err.Error())
	}
	testPool = pool

	code := m.Run()

	pool.Close()
	_ = pgEnv.Terminate(ctx)
	os.Exit(code)
}

// insertChainRow JSON-marshals the payload, wraps it in an eventbusv1.Event
// envelope, proto-marshals it, and INSERTs into events_audit at the given
// subject + js_seq. Registers a t.Cleanup to delete the row after the test.
func insertChainRow(t *testing.T, subject string, jsSeq int64, p policy.PolicySetPayload) {
	t.Helper()
	body, err := json.Marshal(&p)
	require.NoError(t, err)

	envelope, err := proto.Marshal(&eventbusv1.Event{
		Subject: subject,
		Type:    "crypto.policy_set",
		Payload: body,
	})
	require.NoError(t, err)

	// Construct a unique 16-byte id from jsSeq (sufficient for test isolation).
	idBytes := make([]byte, 16)
	idBytes[0] = byte(jsSeq >> 8)
	idBytes[1] = byte(jsSeq)
	idBytes[15] = byte(jsSeq ^ 0xAB)

	_, err = testPool.Exec(context.Background(),
		`INSERT INTO events_audit
		   (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering)
		 VALUES ($1, $2, $3, now(), 'system', $4, 1, 'identity', $5, '{}'::jsonb)`,
		idBytes, subject, "crypto.policy_set", envelope, jsSeq)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM events_audit WHERE js_seq = $1 AND subject = $2`, jsSeq, subject)
	})
}

// buildPayload constructs a deterministic PolicySetPayload for integration tests.
func buildPayload(name string, prev []byte, ts int64) policy.PolicySetPayload {
	return policy.PolicySetPayload{
		PolicyName:      name,
		PolicySnapshot:  map[string]any{"members": []any{}},
		PrevHash:        prev,
		ServerStartULID: "01HZSTART0000000000000000",
		ServerIdentity:  "holomush@host",
		Timestamp:       time.Unix(ts, 0).UTC(),
	}
}

// chainStateCleanup deletes the bootstrap_metadata row for a policyName
// after the test, so subsequent runs (or other tests on the same DB)
// see an uninitialized chain by default.
func chainStateCleanup(t *testing.T, policyName string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM bootstrap_metadata WHERE key = $1`,
			"crypto.policy_chain_initialized."+policyName)
	})
}

// TestVerifyChainAgainstRealEventsAudit inserts 3 valid chain rows into the
// events_audit table, verifies the clean chain passes, then corrupts the
// middle row's envelope (keeping the stored policy_hash unchanged) and
// asserts the verifier returns POLICY_CHAIN_HASH_MISMATCH or
// POLICY_CHAIN_BROKEN_LINK.
func TestVerifyChainAgainstRealEventsAudit(t *testing.T) {
	subject := "events.testgame.system.crypto_policy.crypto_operators_int"

	// Build genesis row.
	gen := buildPayload("crypto.operators", nil, 1700000000)
	genHash, err := policy.ComputePolicyHash(&gen)
	require.NoError(t, err)
	gen.PolicyHash = genHash

	// Build mid row linked to genesis.
	mid := buildPayload("crypto.operators", genHash, 1700000060)
	midHash, err := policy.ComputePolicyHash(&mid)
	require.NoError(t, err)
	mid.PolicyHash = midHash

	// Build tip row linked to mid.
	tip := buildPayload("crypto.operators", midHash, 1700000120)
	tipHash, err := policy.ComputePolicyHash(&tip)
	require.NoError(t, err)
	tip.PolicyHash = tipHash

	insertChainRow(t, subject, 100, gen)
	insertChainRow(t, subject, 101, mid)
	insertChainRow(t, subject, 102, tip)

	// Clean chain must verify without error.
	require.NoError(t, policy.VerifyChain(context.Background(), testPool, subject, "crypto.operators"))

	// Corrupt the mid row: tamper the policy_snapshot but keep the stored
	// policy_hash (as it was before the tamper) → the recomputed hash will
	// not match the stored hash → POLICY_CHAIN_HASH_MISMATCH.
	corrupt := mid
	corrupt.PolicySnapshot = map[string]any{"members": []any{"tampered"}}
	// policy_hash is kept as midHash (the original, now-stale hash).
	corruptBody, err := json.Marshal(&corrupt)
	require.NoError(t, err)
	corruptEnv, err := proto.Marshal(&eventbusv1.Event{
		Subject: subject,
		Type:    "crypto.policy_set",
		Payload: corruptBody,
	})
	require.NoError(t, err)

	_, err = testPool.Exec(context.Background(),
		`UPDATE events_audit SET envelope = $1 WHERE js_seq = 101 AND subject = $2`,
		corruptEnv, subject)
	require.NoError(t, err)

	err = policy.VerifyChain(context.Background(), testPool, subject, "crypto.operators")
	require.Error(t, err)

	o, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error; got %T: %v", err, err)
	// Mid row's stored policy_hash no longer matches the recomputed hash over
	// the tampered payload → POLICY_CHAIN_HASH_MISMATCH. If the verifier
	// reaches the tip row first via prev_hash comparison, BROKEN_LINK is also
	// acceptable (both indicate chain integrity failure).
	assert.Contains(t,
		[]string{"POLICY_CHAIN_HASH_MISMATCH", "POLICY_CHAIN_BROKEN_LINK"},
		o.Code(),
		"expected HASH_MISMATCH or BROKEN_LINK; got %s", o.Code())
}

// TestVerifyChainAcceptsEmptyOnFirstBoot covers the chain-init signal
// added in CodeRabbit #11: an empty audit row-set with NO bootstrap
// signal is the first-boot path and VerifyChain returns nil so the
// emitter can write genesis.
func TestVerifyChainAcceptsEmptyOnFirstBoot(t *testing.T) {
	policyName := "first_boot_policy"
	subject := "events.testgame.system.crypto_policy." + policyName
	// Defensive: ensure no leftover state.
	_, _ = testPool.Exec(context.Background(),
		`DELETE FROM events_audit WHERE subject = $1`, subject)
	chainStateCleanup(t, policyName)
	_, _ = testPool.Exec(context.Background(),
		`DELETE FROM bootstrap_metadata WHERE key = $1`,
		"crypto.policy_chain_initialized."+policyName)

	require.NoError(t, policy.VerifyChain(context.Background(), testPool, subject, policyName),
		"first-boot empty chain MUST verify cleanly")
}

// TestVerifyChainRejectsTruncatedChain locks in the security-critical
// branch of CodeRabbit #11. Initialize the chain (write the bootstrap
// signal), then DELETE every events_audit row for the subject —
// simulating full-chain truncation. The next VerifyChain MUST fail with
// POLICY_CHAIN_TRUNCATED rather than silently treating an empty
// row-set as fresh-DB.
func TestVerifyChainRejectsTruncatedChain(t *testing.T) {
	policyName := "truncated_policy"
	subject := "events.testgame.system.crypto_policy." + policyName
	chainStateCleanup(t, policyName)
	defer testPool.Exec(context.Background(),
		`DELETE FROM events_audit WHERE subject = $1`, subject) //nolint:errcheck

	// Mark the chain as initialized (simulating a prior successful emit).
	_, err := testPool.Exec(context.Background(),
		`INSERT INTO bootstrap_metadata (key, value) VALUES ($1, 'true')
		 ON CONFLICT (key) DO NOTHING`,
		"crypto.policy_chain_initialized."+policyName)
	require.NoError(t, err)

	// Audit row-set is empty (no chain rows seeded). Truncation case.
	err = policy.VerifyChain(context.Background(), testPool, subject, policyName)
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "POLICY_CHAIN_TRUNCATED", o.Code(),
		"empty chain + initialized signal MUST surface POLICY_CHAIN_TRUNCATED")
}

// TestEmitCurrentSnapshotMarksChainInitialized verifies the emitter
// writes the chain-init signal after a successful Publish, so
// subsequent VerifyChain calls can detect truncation.
func TestEmitCurrentSnapshotMarksChainInitialized(t *testing.T) {
	const policyName = "init-signal-game"
	const gameID = "init-signal-game"
	subject := "events." + gameID + ".system.crypto_policy.dual_control_required"
	cleanupSubject(t, subject)
	chainStateCleanup(t, "dual_control_required")

	// Build emitter deps with a fakePublisher (the host does the audit
	// projection in production; this unit-style test only verifies the
	// emitter's own state-mark side effect).
	pub := &fakePublisher{}
	deps := emitDeps(t, gameID, pub, policy.CryptoEffectiveConfig{DualControlRequired: []string{"rekey"}})
	require.NoError(t, policy.EmitCurrentSnapshot(context.Background(), deps, "dual_control_required"))
	require.Len(t, pub.Events(), 1, "emit should publish exactly one event")

	// Bootstrap signal MUST be present.
	var value string
	err := testPool.QueryRow(context.Background(),
		`SELECT value FROM bootstrap_metadata WHERE key = $1`,
		"crypto.policy_chain_initialized.dual_control_required",
	).Scan(&value)
	require.NoError(t, err, "chain-init signal MUST be recorded after successful emit")
	assert.Equal(t, "true", value)
}
