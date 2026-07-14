// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// phase3TestSetup is the canonical Phase 3 test harness. It wires a real
// pgx pool, a real KEK provider, a real DEK Manager (which satisfies both
// dek.Minter and dek.MaterialResolver), seeds a fresh DEK for the rekey
// context, and exposes helpers for inserting encrypted events_audit rows
// + driving the orchestrator past Phase 2.
//
// The harness deliberately bypasses RunPhase1Fresh + RunPhase2 (which
// require a configured policy_set chain handler and KEK setup we already
// have in seedPolicySetHeadForOrch / newTestProvider) by opening the
// checkpoint manually at phase2_mint_dek with the new DEK already minted.
// This isolates the Phase 3 contract under test from upstream phases.
type phase3TestSetup struct {
	pool      *pgxpool.Pool
	provider  kek.Provider
	manager   dek.Manager
	orch      *dek.Orchestrator
	repo      *dek.CheckpointRepo
	requestID dek.RequestID
	oldDEKID  int64
	oldDEKVer uint32
	oldKey    codec.Key
	newDEKID  int64
	newDEKVer uint32
	newKey    codec.Key
	codecName codec.Name
	// eventIDs records the ULIDs of every seeded event, in insertion order.
	// Tests use this to look up rows post-rewrite for INV-CRYPTO-95 assertions.
	eventIDs []ulid.ULID
	// plaintexts mirrors eventIDs and records the cleartext payload each
	// row was encrypted from, so the round-trip assertion can compare bytes
	// without re-deriving anything from the post-rewrite ciphertext.
	plaintexts [][]byte
}

// newPhase3TestSetup builds a harness ready to drive RunPhase3. The
// caller seeds events with InsertEncryptedRows(n) before invoking
// orch.RunPhase3.
func newPhase3TestSetup() *phase3TestSetup {
	pool := testIntegrationPool(suiteT)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	provider := newTestProvider(suiteT)
	store := dek.NewStore(pool)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})
	pcache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})

	mgr, err := dek.NewManager(
		provider, store, cache, pcache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&stubBindingResolver{},
	)
	Expect(err).NotTo(HaveOccurred())

	// Seed an active DEK row via GetOrCreate so the wrapped_dek column
	// carries a real wrapped key (not the '\xdeadbeef' placeholder used
	// by seedActiveDEKWithParticipants). Resolve under that keyID gives
	// us the codec.Key for encrypting test events with the OLD DEK.
	ctxID := dek.ContextID{Type: "scene", ID: "01PH3"}
	oldKey, err := mgr.GetOrCreate(context.Background(), ctxID, nil)
	Expect(err).NotTo(HaveOccurred(), "seed old DEK via GetOrCreate")
	Expect(oldKey.ID).NotTo(BeZero())

	// Mint the new DEK now (mirrors Phase 2's MintNewDEKForRekey) so the
	// harness can open the checkpoint pre-populated with new_dek_id.
	// store.selectByPK lets us derive oldDEKID's PK from the active row.
	oldRecord, err := mgr.ActiveDEKRow(context.Background(), ctxID)
	Expect(err).NotTo(HaveOccurred())
	newDEKID, err := mgr.MintNewDEKForRekey(context.Background(), oldRecord.ID)
	Expect(err).NotTo(HaveOccurred())

	// Resolve the new DEK material so encrypted-row seeding can verify
	// round-trip later. We don't actually need newKey at setup, but it's
	// load-bearing for INV-CRYPTO-95 assertions in the AADRebindOnRewrite spec.
	const newDEKVer uint32 = 2                                                         // GetOrCreate seeded at v1; MintNewDEKForRekey produces v2
	newKey, err := mgr.Resolve(context.Background(), codec.KeyID(newDEKID), newDEKVer) //nolint:gosec // G115: newDEKID is a BIGSERIAL PK
	Expect(err).NotTo(HaveOccurred())

	// Open the checkpoint at status=phase2_mint_dek with new_dek_id set.
	// We bypass NewCheckpointOpenRequest/Open because that would land at
	// 'pending'; we want to start at 'phase2_mint_dek' so RunPhase3 enters
	// via its fresh-entry path (which CAS-advances to phase3_reencrypt_cold).
	rid := dek.RequestID(idgen.New())
	_, err = pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id)
        VALUES ($1, $2, $3, $4, $5, $6, 'phase2_mint_dek', $7, $8)
    `, rid[:], ctxID.Type, ctxID.ID, make([]byte, 32), make([]byte, 32),
		"01PLAYER", oldRecord.ID, newDEKID)
	Expect(err).NotTo(HaveOccurred())

	repo := dek.NewCheckpointRepo(pool)
	orch := dek.NewOrchestrator(store, repo, &nilPolicyHashSource{}, mgr)
	// Phase 3's additive seam: wire the material resolver. *manager
	// satisfies dek.MaterialResolver structurally via Resolve +
	// VersionForDEKID.
	orch.SetMaterialResolver(mgr.(dek.MaterialResolver))

	setup := &phase3TestSetup{
		pool:      pool,
		provider:  provider,
		manager:   mgr,
		orch:      orch,
		repo:      repo,
		oldDEKID:  oldRecord.ID,
		oldDEKVer: oldRecord.Version,
		oldKey:    oldKey,
		newDEKID:  newDEKID,
		newDEKVer: newDEKVer,
		newKey:    newKey,
		codecName: codec.NameXChaCha20v1,
	}
	// Store rid for the test to retrieve; encoded in a context the test
	// can pull via setup.RequestID.
	setup.requestID = rid
	return setup
}

// nilPolicyHashSource is a placeholder PolicyHashSource for the Phase 3
// harness; Phase 3 never calls into it (policy hash was captured at
// Phase 1, which we bypass), so any implementation is fine.
type nilPolicyHashSource struct{}

func (nilPolicyHashSource) CurrentPolicyHash(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func (s *phase3TestSetup) RequestID() dek.RequestID { return s.requestID }

// compareBytes returns -1/0/+1 for byte-slice ordering. Used by tests
// to find max-id across ULID slices without pulling in sort.Slice.
func compareBytes(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

// InsertEncryptedRow seeds one events_audit row encrypted under the
// harness's OLD DEK. Plaintext is unique per row so a post-rewrite
// round-trip can prove byte-equality. Returns the ULID stamped into
// the row, which the test can use to load the row afterwards.
func (s *phase3TestSetup) InsertEncryptedRow(plaintext []byte) ulid.ULID {
	id := idgen.New()

	// Build the envelope proto with subject/type/timestamp/actor and a
	// PLAIN payload, then encode the payload to ciphertext, then write
	// the ciphertext back into Payload before marshaling. AAD construction
	// MUST happen against the same proto whose payload-bytes the rewrite
	// path will later see (after Phase 3 swaps in re-encrypted ciphertext).
	envelope := &eventbusv1.Event{
		Id:        id[:],
		Subject:   "events.g1.system.scene.01PH3",
		Type:      "test.event",
		Timestamp: nil, // zero TS is fine — aad.Build handles it
		Actor: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM,
		},
		Payload: nil,
	}

	aadBytes, err := aad.Build(envelope, string(s.codecName), uint64(s.oldDEKID), s.oldDEKVer) //nolint:gosec // G115: oldDEKID is a BIGSERIAL PK
	Expect(err).NotTo(HaveOccurred())

	codecImpl, err := codec.Resolve(s.codecName)
	Expect(err).NotTo(HaveOccurred())

	ciphertext, err := codecImpl.Encode(context.Background(), plaintext, s.oldKey, aadBytes)
	Expect(err).NotTo(HaveOccurred())

	envelope.Payload = ciphertext
	envelopeBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(envelope)
	Expect(err).NotTo(HaveOccurred())

	// event_ms (000052 partition key) is derived from the now-based ULID id
	// (idgen.New) — lands in the current partition. Not an AAD input.
	eventMS := int64(id.Time()) * int64(time.Millisecond)
	_, err = s.pool.Exec(context.Background(), `
        INSERT INTO events_audit
          (id, subject, type, timestamp, actor_kind, envelope, schema_ver,
           codec, js_seq, rendering, dek_ref, dek_version, event_ms)
        VALUES ($1, 'events.g1.system.scene.01PH3', 'test.event', $2,
                'system', $3, 1, $4, $5, '{}'::jsonb, $6, $7, $8)
    `, id[:], pgnanos.From(time.Now()), envelopeBytes, string(s.codecName),
		int64(time.Now().UnixNano()),   // js_seq monotonic placeholder
		s.oldDEKID, int32(s.oldDEKVer), //nolint:gosec // G115: oldDEKVer is uint32 < 2^31
		eventMS)
	Expect(err).NotTo(HaveOccurred())

	s.eventIDs = append(s.eventIDs, id)
	s.plaintexts = append(s.plaintexts, append([]byte(nil), plaintext...))
	return id
}

// InsertEncryptedRows seeds n rows with predictable per-row plaintext.
func (s *phase3TestSetup) InsertEncryptedRows(n int) {
	for i := 0; i < n; i++ {
		pt := []byte("plaintext-row-")
		pt = append(pt, byte(i>>24), byte(i>>16), byte(i>>8), byte(i))
		s.InsertEncryptedRow(pt)
	}
}

// AssertAllRowsReferenceDEK queries events_audit and fails the test if
// any seeded row's dek_ref column is not equal to dekID.
func (s *phase3TestSetup) AssertAllRowsReferenceDEK(dekID int64) {
	for _, id := range s.eventIDs {
		var actual int64
		err := s.pool.QueryRow(context.Background(),
			`SELECT dek_ref FROM events_audit WHERE id = $1`, id[:]).Scan(&actual)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(dekID), "row %s must reference DEK %d after rewrite", id.String(), dekID)
	}
}

// LoadEventsAuditRow returns the post-rewrite raw fields of a seeded row.
type phase3RewrittenRow struct {
	Envelope   []byte
	Codec      codec.Name
	DEKRef     int64
	DEKVersion uint32
}

func (s *phase3TestSetup) LoadEventsAuditRow(id ulid.ULID) phase3RewrittenRow {
	var r phase3RewrittenRow
	var codecStr string
	var dekVerInt32 int32
	err := s.pool.QueryRow(context.Background(), `
        SELECT envelope, codec, dek_ref, dek_version
          FROM events_audit
         WHERE id = $1
    `, id[:]).Scan(&r.Envelope, &codecStr, &r.DEKRef, &dekVerInt32)
	Expect(err).NotTo(HaveOccurred())
	r.Codec = codec.Name(codecStr)
	r.DEKVersion = uint32(dekVerInt32) //nolint:gosec // G115: column constraint enforces non-negative
	return r
}

// stubBindingResolver / noopInvalidator are shared with manager_integration_test.go
// (package dek_test). Re-declared here is unnecessary.

var _ = Describe("Phase3_RewriteAllRowsAtomically (INV-CRYPTO-94)", func() {
	It("rewrites all seeded rows atomically, advances checkpoint to phase3_reencrypt_cold, and sets cursor to largest-id row", func() {
		setup := newPhase3TestSetup()
		const eventCount = 100
		setup.InsertEncryptedRows(eventCount)

		rid := setup.RequestID()
		rowsRewritten, err := setup.orch.RunPhase3(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(rowsRewritten).To(Equal(eventCount),
			"INV-CRYPTO-94: every seeded row must be rewritten in one invocation")

		ckpt, err := setup.repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.Status).To(Equal(dek.CheckpointStatusPhase3ReencryptCold),
			"Phase 3 leaves status at phase3_reencrypt_cold; phase5_invalidate transition is .22's job")
		Expect(ckpt.NewDEKID).NotTo(BeNil())
		setup.AssertAllRowsReferenceDEK(*ckpt.NewDEKID)

		// Cursor points at the row with the largest id processed. idgen.New
		// is not strictly monotonic across rapid calls (entropy randomness),
		// so the test computes the expected cursor by taking the max id from
		// the seeded set rather than assuming insertion order matches sort
		// order. This still tests the contract: AdvanceCursor sees the last
		// row of the final batch, which (under the ORDER BY id ASC SELECT)
		// is the largest-id row.
		finalID, ok := ckpt.LastProcessedEventID()
		Expect(ok).To(BeTrue(), "cursor must be populated after a successful rewrite")
		var maxSeed [16]byte
		for _, id := range setup.eventIDs {
			if compareBytes(id[:], maxSeed[:]) > 0 {
				copy(maxSeed[:], id[:])
			}
		}
		Expect(finalID).To(Equal(maxSeed),
			"INV-CRYPTO-94: cursor advances to the largest-id row processed")
	})
})

var _ = Describe("Phase3_CrashResumeIdempotent (INV-CRYPTO-94)", func() {
	It("resumes from crash cursor and produces state identical to a non-crashed run", func() {
		setup := newPhase3TestSetup()
		const eventCount = 2500
		setup.InsertEncryptedRows(eventCount)

		rid := setup.RequestID()

		// Crash simulation: cancel the parent ctx after the first batch
		// commits (rowsRewritten >= 1000 on the inner counter).
		var crashTriggered atomic.Bool
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		setup.orch.SetBatchHookForTest(func(rowsSoFar int) {
			if rowsSoFar >= 1000 && crashTriggered.CompareAndSwap(false, true) {
				cancel()
			}
		})

		firstAttemptCount, firstErr := setup.orch.RunPhase3(ctx, rid)
		Expect(firstErr).To(HaveOccurred(), "INV-CRYPTO-94: context cancel must surface an error")
		Expect(firstAttemptCount).To(BeNumerically(">=", 1000),
			"first attempt processed at least one batch before crash")
		Expect(firstAttemptCount).To(BeNumerically("<", eventCount),
			"first attempt crashed before completing all rows")

		// Verify the cursor advanced ONLY to the last committed batch
		// boundary. The crashed batch's rows + cursor advance MUST have been
		// rolled back together (INV-CRYPTO-94).
		ckptMid, err := setup.repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckptMid.Status).To(Equal(dek.CheckpointStatusPhase3ReencryptCold),
			"crash leaves status at phase3_reencrypt_cold for the resume path")
		_, cursorSet := ckptMid.LastProcessedEventID()
		Expect(cursorSet).To(BeTrue(), "at least one batch committed before crash")

		// Resume from the cursor with a fresh ctx.
		setup.orch.SetBatchHookForTest(nil) // no crash this time
		resumeCount, err := setup.orch.RunPhase3(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(resumeCount).To(Equal(eventCount-firstAttemptCount),
			"resume completes exactly the rows the first attempt did not")

		ckptFinal, err := setup.repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckptFinal.Status).To(Equal(dek.CheckpointStatusPhase3ReencryptCold))
		Expect(ckptFinal.NewDEKID).NotTo(BeNil())
		setup.AssertAllRowsReferenceDEK(*ckptFinal.NewDEKID)

		// holomush-jxo8.7.54 — Phase 3 row count must be CUMULATIVE across
		// the crash + resume cycle. IncrementPhase3Count runs inside each
		// batch's transaction, so committed batches contribute to the
		// count and rolled-back batches don't. The final count MUST equal
		// the total row count, NOT just the resume invocation's count.
		// Without the in-tx increment fix, this would report only the
		// resume-invocation's contribution (eventCount-firstAttemptCount)
		// and silently drop the firstAttemptCount rows from the audit trail.
		Expect(ckptFinal.Phase3RowsRewritten).To(Equal(eventCount),
			"INV-CRYPTO-94 + jxo8.7.54: Phase3RowsRewritten on checkpoint must be cumulative across crash-resume cycles")

		// Plaintext round-trip: decrypt each rewritten row under the NEW
		// DEK + new AAD; bytes MUST equal the seeded plaintext for that
		// row. This is the strongest form of INV-CRYPTO-94 (final state is
		// observationally identical to a non-crashed run).
		codecImpl, err := codec.Resolve(setup.codecName)
		Expect(err).NotTo(HaveOccurred())
		for i, id := range setup.eventIDs {
			row := setup.LoadEventsAuditRow(id)
			var envelope eventbusv1.Event
			Expect(proto.Unmarshal(row.Envelope, &envelope)).To(Succeed())
			Expect(row.DEKRef).To(Equal(*ckptFinal.NewDEKID))
			Expect(row.DEKVersion).To(Equal(setup.newDEKVer))

			aadBytes, err := aad.Build(&envelope, string(row.Codec), uint64(row.DEKRef), row.DEKVersion) //nolint:gosec // G115: row.DEKRef is BIGSERIAL PK
			Expect(err).NotTo(HaveOccurred())
			decoded, err := codecImpl.Decode(context.Background(), envelope.GetPayload(), setup.newKey, aadBytes)
			Expect(err).NotTo(HaveOccurred(), "row %d (%s) must decrypt under new DEK + new AAD after rewrite", i, id.String())
			Expect(decoded).To(Equal(setup.plaintexts[i]),
				"INV-CRYPTO-94: plaintext byte-equal between pre-rewrite and post-rewrite for row %d", i)
		}
	})
})

var _ = Describe("Phase3_AADRebindOnRewrite (INV-CRYPTO-95)", func() {
	It("old AAD fails AEAD tag check and new AAD succeeds after rewrite", func() {
		setup := newPhase3TestSetup()
		setup.InsertEncryptedRows(1)

		rid := setup.RequestID()
		_, err := setup.orch.RunPhase3(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())

		ckpt, err := setup.repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.NewDEKID).NotTo(BeNil())

		rewritten := setup.LoadEventsAuditRow(setup.eventIDs[0])
		Expect(rewritten.DEKRef).To(Equal(*ckpt.NewDEKID))

		var envelope eventbusv1.Event
		Expect(proto.Unmarshal(rewritten.Envelope, &envelope)).To(Succeed())

		codecImpl, err := codec.Resolve(rewritten.Codec)
		Expect(err).NotTo(HaveOccurred())

		// New AAD: must succeed.
		newAAD, err := aad.Build(&envelope, string(rewritten.Codec), uint64(rewritten.DEKRef), rewritten.DEKVersion) //nolint:gosec // G115: rewritten.DEKRef is BIGSERIAL PK
		Expect(err).NotTo(HaveOccurred())
		_, err = codecImpl.Decode(context.Background(), envelope.GetPayload(), setup.newKey, newAAD)
		Expect(err).NotTo(HaveOccurred(), "INV-CRYPTO-95: new AAD MUST decode the rewritten ciphertext")

		// Old AAD: must fail. Two distinct mutations probe the rebind
		// surface: (1) old dek_ref (= setup.oldDEKID) with new version,
		// (2) new dek_ref with old version. Either mutation breaks the AEAD
		// tag — both demonstrate the AAD bytes differ post-rewrite.
		oldRefAAD, err := aad.Build(&envelope, string(rewritten.Codec), uint64(setup.oldDEKID), rewritten.DEKVersion) //nolint:gosec // G115
		Expect(err).NotTo(HaveOccurred())
		_, err = codecImpl.Decode(context.Background(), envelope.GetPayload(), setup.newKey, oldRefAAD)
		Expect(err).To(HaveOccurred(), "INV-CRYPTO-95: old dek_ref in AAD MUST fail AEAD tag check")

		oldVerAAD, err := aad.Build(&envelope, string(rewritten.Codec), uint64(rewritten.DEKRef), setup.oldDEKVer) //nolint:gosec // G115
		Expect(err).NotTo(HaveOccurred())
		_, err = codecImpl.Decode(context.Background(), envelope.GetPayload(), setup.newKey, oldVerAAD)
		Expect(err).To(HaveOccurred(), "INV-CRYPTO-95: old dek_version in AAD MUST fail AEAD tag check")
	})
})

var _ = Describe("Phase3_HeartbeatAdvancesDuringLongRun (INV-CRYPTO-106)", func() {
	It("last_heartbeat_at advances past its pre-run value after RunPhase3 with multiple batches", func() {
		setup := newPhase3TestSetup()
		setup.InsertEncryptedRows(2500) // > 2 batches → multiple AdvanceCursor calls

		rid := setup.RequestID()
		ckptBefore, err := setup.repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		beforeHB := ckptBefore.LastHeartbeatAt

		// Sleep 5ms so even fast batch commits produce a strictly-greater
		// last_heartbeat_at (Postgres now() resolution is microsecond, but
		// the test's pre-read can race the first AdvanceCursor on hot CI
		// runners with clock skew).
		time.Sleep(5 * time.Millisecond)

		_, err = setup.orch.RunPhase3(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())

		ckptAfter, err := setup.repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckptAfter.LastHeartbeatAt.After(beforeHB)).To(BeTrue(),
			"INV-CRYPTO-106: heartbeat MUST advance during the Phase 3 loop (before=%s after=%s)",
			beforeHB, ckptAfter.LastHeartbeatAt)
	})
})

var _ = Describe("Phase3_RequiresPrecondition", func() {
	It("refuses to run from status outside {phase2_mint_dek, phase3_reencrypt_cold}", func() {
		setup := newPhase3TestSetup()
		rid := setup.RequestID()

		// Force the checkpoint into phase1_auth (illegal precondition).
		_, err := setup.pool.Exec(context.Background(),
			`UPDATE crypto_rekey_checkpoints SET status = 'phase1_auth' WHERE request_id = $1`, rid[:])
		Expect(err).NotTo(HaveOccurred())

		_, err = setup.orch.RunPhase3(context.Background(), rid)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "DEK_REKEY_PHASE_PRECONDITION_FAILED")
	})
})

var _ = Describe("Phase3_NoResolver_FailsClosed", func() {
	It("refuses with DEK_REKEY_MATERIAL_RESOLVER_NIL when SetMaterialResolver was not called", func() {
		setup := newPhase3TestSetup()
		// Reset the resolver to nil to simulate misconfigured wiring.
		setup.orch.SetMaterialResolver(nil)

		_, err := setup.orch.RunPhase3(context.Background(), setup.RequestID())
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "DEK_REKEY_MATERIAL_RESOLVER_NIL")
	})
})
