// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integration

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/admin/policy"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/test/testutil"
)

// Plan reference: docs/superpowers/plans/2026-05-09-phase5-sub-epic-d.md §Task 27.
// Bead:           holomush-jxo8.6.25.
//
// Spec invariants validated:
//   - INV-CRYPTO-77: genesis row of a chain has prev_hash == nil.
//   - INV-CRYPTO-78: every chain extension's prev_hash equals the predecessor's
//     recomputed policy_hash.
//   - INV-CRYPTO-79: every row's stored policy_hash equals the recomputed hash
//     over its own canonicalized payload.
//
// Strategy deviation from plan §Task 27 step text ("boot the full server"):
//
// Strategy Z — direct subsystem invocation across simulated reboots —
// rather than Strategy X (subprocess full-boot) or running runCoreWithDeps
// twice in-process. The plan was written before the prometheus.DefaultRegisterer
// process-wide collector singleton was discovered (T25 commit log) to make
// running runCoreWithDeps twice in the same Go test process panic with
// "duplicate metrics collector registration attempted". Strategy Z avoids
// that constraint while still validating the precise contract T27 cares
// about: the chain emitter (CryptoPolicySubsystem) and chain verifier
// (auditchain.VerifierSubsystem with PolicySetHandlerFor), publishing through
// the production RenderingPublisher into a real embedded JetStream and
// projecting through the real audit.Subsystem into the real events_audit table.
// The full-boot subsystem-ordering wiring (verifier-before-emitter) is
// separately validated by T22's wiring tests (cmd/holomush/core_subsystems_test.go).
// CryptoChainVerifierSubsystem was replaced by auditchain.VerifierSubsystem
// in holomush-jxo8.7.8.
//
// Each "boot" is modeled as a fresh subsystem instantiation against the
// same persistent pool + JetStream — exactly what production reboot
// semantics produce: a fresh process re-reads the persisted chain.
var _ = Describe("admin policy_chain integrity (E2E, INV-CRYPTO-77/INV-CRYPTO-78/INV-CRYPTO-79)", func() {
	const (
		gameID     = "main"
		policyName = "dual_control_required"
	)

	var (
		ctx     context.Context
		cancel  context.CancelFunc
		pool    *pgxpool.Pool
		bus     *eventbustest.Embedded
		hostSub *audit.Subsystem
		pub     eventbus.Publisher
		subject = "events." + gameID + ".system.crypto_policy." + policyName
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)

		// Fresh template-cloned PG database per spec (no shared state across
		// scenarios — each It runs against a clean events_audit).
		shared := testutil.SharedPostgres(suiteT)
		connStr := testutil.FreshDatabase(suiteT, shared)
		var err error
		pool, err = pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred(), "pgxpool.New")
		DeferCleanup(func() { pool.Close() })

		bus = eventbustest.New(suiteT)

		// Real audit projection. The chain emitter Publishes via
		// RenderingPublisher → JetStream; this subsystem drains JetStream
		// into events_audit.
		hostSub = audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
		Expect(hostSub.Start(ctx)).To(Succeed(), "audit.Subsystem.Start")
		DeferCleanup(func() { _ = hostSub.Stop(context.Background()) })

		registry, err := core.BootstrapVerbRegistry("test-0.1")
		Expect(err).NotTo(HaveOccurred(), "BootstrapVerbRegistry")
		pub = eventbus.NewRenderingPublisher(bus.Bus.Publisher(), registry)

		DeferCleanup(cancel)
	})

	// startEmitterEpoch instantiates a fresh CryptoPolicySubsystem against the
	// shared pool + JetStream and runs Start once. Models a server boot at the
	// emitter level: a new process reads the persisted chain tip from
	// events_audit and either no-ops (idempotent on no-change) or appends a new
	// extension row.
	startEmitterEpoch := func(cfg policy.CryptoEffectiveConfig) {
		emitter := policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{
			EmitDeps: policy.EmitDeps{
				GameID:          gameID,
				ServerStartULID: ulid.Make().String(),
				ServerIdentity:  "holomush@e2e-test",
				Pool:            pool,
				Publisher:       pub,
				Clock:           realClock{},
				Config:          cfg,
			},
			PolicyNames: []string{policyName},
		})
		Expect(emitter.Start(ctx)).To(Succeed(), "CryptoPolicySubsystem.Start")
	}

	// awaitChainLength blocks until events_audit has exactly the expected
	// number of rows for the chain subject. Uses Eventually because audit
	// projection is asynchronous (JetStream → projection → INSERT).
	awaitChainLength := func(want int) {
		Eventually(func() int {
			var n int
			err := pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM events_audit WHERE subject = $1`, subject).
				Scan(&n)
			if err != nil {
				return -1
			}
			return n
		}, "10s", "100ms").Should(Equal(want),
			"events_audit row count for subject %s", subject)
	}

	// loadChain returns the chain entries via SQL, in js_seq order.
	loadChain := func() []policy.PolicySetPayload {
		entries := loadChainEntriesViaSQL(ctx, pool, subject)
		return entries
	}

	It("Spec 1 (INV-CRYPTO-77): genesis on fresh DB writes a single policy_set row with prev_hash IS NULL", func() {
		startEmitterEpoch(policy.CryptoEffectiveConfig{DualControlRequired: []string{}})
		hostSub.AwaitDrained(suiteT, 10*time.Second)
		awaitChainLength(1)

		entries := loadChain()
		Expect(entries).To(HaveLen(1), "exactly one chain row at genesis")
		Expect(entries[0].PrevHash).To(BeNil(),
			"INV-CRYPTO-77: genesis row prev_hash MUST be nil")
		Expect(entries[0].PolicyName).To(Equal(policyName))

		// INV-CRYPTO-79: genesis row's stored policy_hash matches recomputed hash.
		recomputed, err := policy.ComputePolicyHash(&entries[0])
		Expect(err).NotTo(HaveOccurred(), "ComputePolicyHash on genesis")
		Expect(entries[0].PolicyHash).To(Equal(recomputed),
			"INV-CRYPTO-79: stored policy_hash MUST equal recomputed hash")

		// Production verifier path (loads from events_audit, two-step decode,
		// walks chain): clean genesis chain MUST verify.
		verifier := chain.NewVerifierSubsystem(chain.VerifierSubsystemConfig{
			Repo:             chain.NewPostgresRepo(pool),
			HandlersProvider: func() []chain.Handler { return []chain.Handler{policy.PolicySetHandlerFor(gameID)} },
			Logger:           slog.Default(),
		})
		Expect(verifier.Start(ctx)).To(Succeed(),
			"genesis chain MUST pass verifier")
	})

	It("Spec 2 (INV-CRYPTO-78): chain-extends across simulated reboot with config change; second row's prev_hash matches first row's recomputed hash", func() {
		// Epoch 1: genesis with empty config.
		startEmitterEpoch(policy.CryptoEffectiveConfig{DualControlRequired: []string{}})
		hostSub.AwaitDrained(suiteT, 10*time.Second)
		awaitChainLength(1)

		first := loadChain()[0]
		Expect(first.PrevHash).To(BeNil(), "first epoch must be genesis")

		// Epoch 2: simulate reboot — fresh subsystem instance — with a
		// changed effective config. The emitter MUST read the persisted
		// genesis row, compute its hash, and emit an extension whose
		// prev_hash equals that recomputed value.
		startEmitterEpoch(policy.CryptoEffectiveConfig{DualControlRequired: []string{"rekey"}})
		hostSub.AwaitDrained(suiteT, 10*time.Second)
		awaitChainLength(2)

		entries := loadChain()
		Expect(entries).To(HaveLen(2))

		firstRecomputed, err := policy.ComputePolicyHash(&entries[0])
		Expect(err).NotTo(HaveOccurred(), "ComputePolicyHash on first")
		Expect(entries[1].PrevHash).To(Equal(firstRecomputed),
			"INV-CRYPTO-78: extension prev_hash MUST equal predecessor's recomputed policy_hash")

		// INV-CRYPTO-79: extension row's stored policy_hash matches its recomputed hash.
		extRecomputed, err := policy.ComputePolicyHash(&entries[1])
		Expect(err).NotTo(HaveOccurred(), "ComputePolicyHash on extension")
		Expect(entries[1].PolicyHash).To(Equal(extRecomputed),
			"INV-CRYPTO-79: stored policy_hash MUST equal recomputed hash on extension row")

		// The two rows must reflect the configuration change (snapshot
		// content differed across epochs — otherwise the emitter's
		// idempotency check would have suppressed the extension).
		Expect(entries[0].PolicySnapshot).NotTo(Equal(entries[1].PolicySnapshot),
			"epoch 2 snapshot must differ from epoch 1 (else idempotency skips)")

		// Production verifier path on the clean two-row chain MUST succeed.
		verifier := chain.NewVerifierSubsystem(chain.VerifierSubsystemConfig{
			Repo:             chain.NewPostgresRepo(pool),
			HandlersProvider: func() []chain.Handler { return []chain.Handler{policy.PolicySetHandlerFor(gameID)} },
			Logger:           slog.Default(),
		})
		Expect(verifier.Start(ctx)).To(Succeed(),
			"two-row chain MUST pass verifier on second-boot path")
	})

	It("Spec 3 (INV-CRYPTO-78/INV-CRYPTO-79): tampered events_audit row causes verifier subsystem to fail-closed at next boot with POLICY_CHAIN_HASH_MISMATCH or POLICY_CHAIN_BROKEN_LINK", func() {
		// Build a clean two-row chain across two epochs.
		startEmitterEpoch(policy.CryptoEffectiveConfig{DualControlRequired: []string{}})
		hostSub.AwaitDrained(suiteT, 10*time.Second)
		awaitChainLength(1)

		startEmitterEpoch(policy.CryptoEffectiveConfig{DualControlRequired: []string{"rekey"}})
		hostSub.AwaitDrained(suiteT, 10*time.Second)
		awaitChainLength(2)

		// Sanity: clean chain verifies.
		cleanVerifier := chain.NewVerifierSubsystem(chain.VerifierSubsystemConfig{
			Repo:             chain.NewPostgresRepo(pool),
			HandlersProvider: func() []chain.Handler { return []chain.Handler{policy.PolicySetHandlerFor(gameID)} },
			Logger:           slog.Default(),
		})
		Expect(cleanVerifier.Start(ctx)).To(Succeed(),
			"sanity: clean chain MUST verify before tamper")

		// Tamper: corrupt the second row's envelope by overwriting its
		// envelope bytes with a hand-crafted invalid policy_set payload.
		// Two construction options:
		//   (a) Corrupt the stored policy_hash → POLICY_CHAIN_HASH_MISMATCH
		//       (recomputed != stored on the tampered row).
		//   (b) Corrupt the stored prev_hash → POLICY_CHAIN_BROKEN_LINK
		//       (extension's prev_hash != predecessor's recomputed hash).
		// Either signal is acceptable per the bead's acceptance criteria.
		// We use option (a) by mutating the snapshot inside the envelope
		// while keeping the stored policy_hash unchanged — exactly the
		// pattern verifier_integration_test.go uses for the same invariant.
		tamperSecondRowEnvelope(ctx, pool, subject)

		// "Next boot": fresh chain.VerifierSubsystem.Start. The
		// orchestrator-level fail-closed contract is that Start returns a
		// non-nil error; in production this propagates up through
		// lifecycle.Run and the server refuses to boot.
		tamperedVerifier := chain.NewVerifierSubsystem(chain.VerifierSubsystemConfig{
			Repo:             chain.NewPostgresRepo(pool),
			HandlersProvider: func() []chain.Handler { return []chain.Handler{policy.PolicySetHandlerFor(gameID)} },
			Logger:           slog.Default(),
		})
		err := tamperedVerifier.Start(ctx)
		Expect(err).To(HaveOccurred(),
			"verifier MUST fail-closed on tampered chain (INV-CRYPTO-78/INV-CRYPTO-79)")

		o, ok := oops.AsOops(err)
		Expect(ok).To(BeTrue(), "expected oops error; got %T: %v", err, err)
		// oops.AsOops surfaces the deepest Code in the chain. The outer
		// wraps are CRYPTO_CHAIN_VERIFY_FAILED → POLICY_CHAIN_VERIFY_FAILED;
		// the inner verifier code is AUDIT_CHAIN_HASH_MISMATCH (option (a))
		// or AUDIT_CHAIN_BROKEN_LINK (option (b)) after the Phase 5 sub-epic E
		// refactor onto the generalized auditchain primitive (was
		// POLICY_CHAIN_HASH_MISMATCH / POLICY_CHAIN_BROKEN_LINK pre-E).
		Expect([]string{"AUDIT_CHAIN_HASH_MISMATCH", "AUDIT_CHAIN_BROKEN_LINK"}).
			To(ContainElement(o.Code()),
				"expected AUDIT_CHAIN_HASH_MISMATCH or AUDIT_CHAIN_BROKEN_LINK; got %s",
				o.Code())
	})
})

// realClock implements policy.Clock by delegating to time.Now. The emitter
// stamps Timestamp + ServerStartULID into the payload, so two different
// epochs will have measurably different timestamps; this matters only
// indirectly (the canonicalized payload differs across epochs, so the
// idempotency check fires only when DualControlRequired actually changed).
type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// fixedJS / fixedPool satisfy the audit.Subsystem provider interfaces.
// Mirrors the pattern in test/integration/eventbus_e2e/plugin_audit_isolation_test.go.
type fixedJS struct{ js jetstream.JetStream }

func (f fixedJS) JS() jetstream.JetStream { return f.js }

type fixedPool struct{ pool *pgxpool.Pool }

func (f fixedPool) Pool() *pgxpool.Pool { return f.pool }
