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
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/admin/policy"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/store"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	"github.com/holomush/holomush/test/testutil"
)

// testPool is the shared database pool for the policy integration suite.
// Initialized by TestMain (below) before any classic-style Test* function
// or Ginkgo spec runs. Read-only after initialization.
var testPool *pgxpool.Pool

// TestMain spins up a testcontainer-backed Postgres, applies migrations,
// and seeds testPool. Lives in this file (rather than the suite-entry
// file) so the existing classic-style tests in emitter_test.go and
// elsewhere continue to share the same pool.
//
// We do NOT lift this setup into Ginkgo BeforeSuite/AfterSuite: doing so
// would force every classic-style Test* function in the same package
// (emitter_test.go::TestEmitCurrentSnapshot* etc.) to also become a
// Ginkgo node. CodeRabbit #8/#9/#10 only ask for the integration *_test
// files in this package to use Ginkgo/Gomega; the package-wide TestMain
// continues to provide testPool to both surfaces.
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
//
// Classic-style helper retained for the testify tests in emitter_test.go.
// Ginkgo specs use insertChainRowGinkgo (DeferCleanup) instead.
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

	idBytes := chainRowIDBytes(jsSeq)

	_, err = testPool.Exec(context.Background(),
		`INSERT INTO events_audit
		   (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering)
		 VALUES ($1, $2, $3, $6, 'system', $4, 1, 'identity', $5, '{}'::jsonb)`,
		idBytes, subject, "crypto.policy_set", envelope, jsSeq, pgnanos.From(time.Now()))
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM events_audit WHERE js_seq = $1 AND subject = $2`, jsSeq, subject)
	})
}

// insertChainRowGinkgo is the Ginkgo equivalent of insertChainRow:
// proto-marshals the envelope, INSERTs, and registers DeferCleanup.
// MUST be called from inside a Ginkgo spec / setup node.
func insertChainRowGinkgo(subject string, jsSeq int64, p policy.PolicySetPayload) {
	GinkgoHelper()
	body, err := json.Marshal(&p)
	Expect(err).NotTo(HaveOccurred())

	envelope, err := proto.Marshal(&eventbusv1.Event{
		Subject: subject,
		Type:    "crypto.policy_set",
		Payload: body,
	})
	Expect(err).NotTo(HaveOccurred())

	idBytes := chainRowIDBytes(jsSeq)

	_, err = testPool.Exec(context.Background(),
		`INSERT INTO events_audit
		   (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering)
		 VALUES ($1, $2, $3, $6, 'system', $4, 1, 'identity', $5, '{}'::jsonb)`,
		idBytes, subject, "crypto.policy_set", envelope, jsSeq, pgnanos.From(time.Now()))
	Expect(err).NotTo(HaveOccurred())

	DeferCleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM events_audit WHERE js_seq = $1 AND subject = $2`, jsSeq, subject)
	})
}

// chainRowIDBytes constructs a unique 16-byte id from jsSeq (sufficient
// for test isolation). Shared between insertChainRow and the Ginkgo helper.
func chainRowIDBytes(jsSeq int64) []byte {
	idBytes := make([]byte, 16)
	idBytes[0] = byte(jsSeq >> 8)
	idBytes[1] = byte(jsSeq)
	idBytes[15] = byte(jsSeq ^ 0xAB)
	return idBytes
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

// VerifyChain integration specs covering CodeRabbit #9 + the chain-init
// signal added in CodeRabbit #11. Migrated from testify to Ginkgo/Gomega.
var _ = Describe("VerifyChain (integration)", func() {
	Context("with a clean 3-row chain in events_audit", func() {
		It("verifies cleanly, then surfaces a chain integrity error after envelope tamper", func() {
			// Subject suffix MUST agree with payload.policy_name post Phase 5
			// sub-epic E (INV-CRYPTO-114 — chain.Verifier enforces). D's pre-E verifier
			// queried by subject directly and only cross-checked at the typed
			// struct level, tolerating fixture mismatches like the prior
			// "crypto_operators_int" subject paired with "crypto.operators"
			// payload.policy_name.
			subject := "events.testgame.system.crypto_policy.crypto.operators"

			// Build genesis row.
			gen := buildPayload("crypto.operators", nil, 1700000000)
			genHash, err := policy.ComputePolicyHash(&gen)
			Expect(err).NotTo(HaveOccurred())
			gen.PolicyHash = genHash

			// Build mid row linked to genesis.
			mid := buildPayload("crypto.operators", genHash, 1700000060)
			midHash, err := policy.ComputePolicyHash(&mid)
			Expect(err).NotTo(HaveOccurred())
			mid.PolicyHash = midHash

			// Build tip row linked to mid.
			tip := buildPayload("crypto.operators", midHash, 1700000120)
			tipHash, err := policy.ComputePolicyHash(&tip)
			Expect(err).NotTo(HaveOccurred())
			tip.PolicyHash = tipHash

			insertChainRowGinkgo(subject, 100, gen)
			insertChainRowGinkgo(subject, 101, mid)
			insertChainRowGinkgo(subject, 102, tip)

			// Clean chain must verify without error.
			Expect(policy.VerifyChain(context.Background(), testPool, subject, "crypto.operators")).
				To(Succeed())

			// Corrupt the mid row: tamper the policy_snapshot but keep the stored
			// policy_hash (as it was before the tamper) → the recomputed hash will
			// not match the stored hash → POLICY_CHAIN_HASH_MISMATCH.
			corrupt := mid
			corrupt.PolicySnapshot = map[string]any{"members": []any{"tampered"}}
			// policy_hash is kept as midHash (the original, now-stale hash).
			corruptBody, err := json.Marshal(&corrupt)
			Expect(err).NotTo(HaveOccurred())
			corruptEnv, err := proto.Marshal(&eventbusv1.Event{
				Subject: subject,
				Type:    "crypto.policy_set",
				Payload: corruptBody,
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = testPool.Exec(context.Background(),
				`UPDATE events_audit SET envelope = $1 WHERE js_seq = 101 AND subject = $2`,
				corruptEnv, subject)
			Expect(err).NotTo(HaveOccurred())

			err = policy.VerifyChain(context.Background(), testPool, subject, "crypto.operators")
			Expect(err).To(HaveOccurred())

			o, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue(), "expected oops error; got %T: %v", err, err)
			// Mid row's stored policy_hash no longer matches the recomputed hash over
			// the tampered payload → AUDIT_CHAIN_HASH_MISMATCH (post-Phase-5-sub-epic-E
			// generalization of POLICY_CHAIN_HASH_MISMATCH). If the verifier reaches the
			// tip row first via prev_hash comparison, AUDIT_CHAIN_BROKEN_LINK is also
			// acceptable (both indicate chain integrity failure).
			Expect([]string{"AUDIT_CHAIN_HASH_MISMATCH", "AUDIT_CHAIN_BROKEN_LINK"}).
				To(ContainElement(o.Code()),
					"expected AUDIT_CHAIN_HASH_MISMATCH or AUDIT_CHAIN_BROKEN_LINK; got %s", o.Code())
		})
	})

	Context("with an empty audit row-set and no chain-init signal (first boot)", func() {
		It("returns nil (CodeRabbit #11 first-boot path)", func() {
			policyName := "first_boot_policy"
			subject := "events.testgame.system.crypto_policy." + policyName
			// Defensive: ensure no leftover state. Post-E migration 000030
			// the bootstrap_metadata schema is (chain_name, scope_key);
			// chainStateCleanupGinkgo handles that purge.
			_, _ = testPool.Exec(context.Background(),
				`DELETE FROM events_audit WHERE subject = $1`, subject)
			chainStateCleanupGinkgo("testgame", policyName)

			Expect(policy.VerifyChain(context.Background(), testPool, subject, policyName)).
				To(Succeed(), "first-boot empty chain MUST verify cleanly")
		})
	})

	Context("with an empty audit row-set but the chain-init signal recorded (truncation)", func() {
		It("MUST surface AUDIT_CHAIN_TRUNCATED (CodeRabbit #11 audit-integrity)", func() {
			policyName := "truncated_policy"
			subject := "events.testgame.system.crypto_policy." + policyName
			chainStateCleanupGinkgo("testgame", policyName)
			DeferCleanup(func() {
				_, _ = testPool.Exec(context.Background(),
					`DELETE FROM events_audit WHERE subject = $1`, subject)
			})

			// Mark the chain as initialized (simulating a prior successful emit).
			// Post-E migration 000030: bootstrap_metadata keys on (chain_name, scope_key);
			// chain_name = PolicySetChainFor(gameID).SubjectPrefix.
			chainName := policy.PolicySetChainFor("testgame").SubjectPrefix
			_, err := testPool.Exec(context.Background(),
				`INSERT INTO bootstrap_metadata (chain_name, scope_key) VALUES ($1, $2)
				 ON CONFLICT (chain_name, scope_key) DO NOTHING`,
				chainName, policyName)
			Expect(err).NotTo(HaveOccurred())

			// Audit row-set is empty (no chain rows seeded). Truncation case.
			err = policy.VerifyChain(context.Background(), testPool, subject, policyName)
			Expect(err).To(HaveOccurred())
			o, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			// Post-E refactor: the generalized verifier owns the typed-error
			// surface (AUDIT_CHAIN_TRUNCATED). policy.VerifyChain wraps with
			// POLICY_CHAIN_VERIFY_FAILED; oops.AsOops surfaces the deepest code.
			Expect(o.Code()).To(Equal("AUDIT_CHAIN_TRUNCATED"),
				"empty chain + initialized signal MUST surface AUDIT_CHAIN_TRUNCATED")
		})
	})
})

// EmitCurrentSnapshot integration specs verifying the chain-init signal
// is written after a successful Publish (CodeRabbit #11 emitter side).
var _ = Describe("EmitCurrentSnapshot (chain-init signal)", func() {
	It("records the bootstrap_metadata signal after a successful emit", func() {
		const policyName = "dual_control_required"
		const gameID = "init-signal-game"
		subject := "events." + gameID + ".system.crypto_policy." + policyName
		cleanupSubjectGinkgo(subject)
		chainStateCleanupGinkgo(gameID, policyName)

		// Build emitter deps with a fakePublisher (the host does the audit
		// projection in production; this unit-style test only verifies the
		// emitter's own state-mark side effect).
		pub := &fakePublisher{}
		deps := policy.EmitDeps{
			GameID:          gameID,
			ServerStartULID: "01HZSTART0000000000000000",
			ServerIdentity:  "holomush@host",
			Pool:            testPool,
			Publisher:       pub,
			Clock:           fixedClock{t: time.Unix(1700000000, 0).UTC()},
			Config:          policy.CryptoEffectiveConfig{DualControlRequired: []string{"rekey"}},
		}
		Expect(policy.EmitCurrentSnapshot(context.Background(), deps, policyName)).To(Succeed())
		Expect(pub.Events()).To(HaveLen(1), "emit should publish exactly one event")

		// Bootstrap signal MUST be present. Post-E migration 000030
		// the schema is (chain_name, scope_key); chain_name equals the
		// auditchain SubjectPrefix from PolicySetChainFor(gameID).
		expectedChainName := policy.PolicySetChainFor(gameID).SubjectPrefix
		var exists bool
		err := testPool.QueryRow(
			context.Background(),
			`SELECT EXISTS(
				SELECT 1 FROM bootstrap_metadata
				 WHERE chain_name = $1 AND scope_key = $2
			)`,
			expectedChainName, policyName,
		).Scan(&exists)
		Expect(err).NotTo(HaveOccurred(),
			"chain-init signal MUST be recorded after successful emit")
		Expect(exists).To(BeTrue(),
			"bootstrap_metadata row MUST exist for (chain_name=%q, scope_key=%q)",
			expectedChainName, policyName)
	})
})
