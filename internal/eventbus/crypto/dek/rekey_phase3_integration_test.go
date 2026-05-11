// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/idgen"
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
	t         *testing.T
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
	// Tests use this to look up rows post-rewrite for INV-E8 assertions.
	eventIDs []ulid.ULID
	// plaintexts mirrors eventIDs and records the cleartext payload each
	// row was encrypted from, so the round-trip assertion can compare bytes
	// without re-deriving anything from the post-rewrite ciphertext.
	plaintexts [][]byte
}

// newPhase3TestSetup builds a harness ready to drive RunPhase3. The
// caller seeds events with InsertEncryptedRows(n) before invoking
// orch.RunPhase3.
func newPhase3TestSetup(t *testing.T) *phase3TestSetup {
	t.Helper()
	pool := testIntegrationPool(t)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	provider := newTestProvider(t)
	store := dek.NewStore(pool)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})
	pcache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})

	mgr, err := dek.NewManager(
		provider, store, cache, pcache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&stubBindingResolver{},
	)
	require.NoError(t, err)

	// Seed an active DEK row via GetOrCreate so the wrapped_dek column
	// carries a real wrapped key (not the '\xdeadbeef' placeholder used
	// by seedActiveDEKWithParticipants). Resolve under that keyID gives
	// us the codec.Key for encrypting test events with the OLD DEK.
	ctxID := dek.ContextID{Type: "scene", ID: "01PH3"}
	oldKey, err := mgr.GetOrCreate(context.Background(), ctxID, nil)
	require.NoError(t, err, "seed old DEK via GetOrCreate")
	require.NotZero(t, oldKey.ID)

	// Mint the new DEK now (mirrors Phase 2's MintNewDEKForRekey) so the
	// harness can open the checkpoint pre-populated with new_dek_id.
	// store.selectByPK lets us derive oldDEKID's PK from the active row.
	oldRecord, err := mgr.ActiveDEKRow(context.Background(), ctxID)
	require.NoError(t, err)
	newDEKID, err := mgr.MintNewDEKForRekey(context.Background(), oldRecord.ID)
	require.NoError(t, err)

	// Resolve the new DEK material so encrypted-row seeding can verify
	// round-trip later. We don't actually need newKey at setup, but it's
	// load-bearing for INV-E8 assertions in TestPhase3_AADRebindOnRewrite.
	const newDEKVer uint32 = 2                                                         // GetOrCreate seeded at v1; MintNewDEKForRekey produces v2
	newKey, err := mgr.Resolve(context.Background(), codec.KeyID(newDEKID), newDEKVer) //nolint:gosec // G115: newDEKID is a BIGSERIAL PK
	require.NoError(t, err)

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
	require.NoError(t, err)

	repo := dek.NewCheckpointRepo(pool)
	orch := dek.NewOrchestrator(store, repo, &nilPolicyHashSource{}, mgr)
	// Phase 3's additive seam: wire the material resolver. *manager
	// satisfies dek.MaterialResolver structurally via Resolve +
	// VersionForDEKID.
	orch.SetMaterialResolver(mgr.(dek.MaterialResolver))

	setup := &phase3TestSetup{
		t:         t,
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
	s.t.Helper()
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
	require.NoError(s.t, err)

	codecImpl, err := codec.Resolve(s.codecName)
	require.NoError(s.t, err)

	ciphertext, err := codecImpl.Encode(context.Background(), plaintext, s.oldKey, aadBytes)
	require.NoError(s.t, err)

	envelope.Payload = ciphertext
	envelopeBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(envelope)
	require.NoError(s.t, err)

	_, err = s.pool.Exec(context.Background(), `
        INSERT INTO events_audit
          (id, subject, type, timestamp, actor_kind, envelope, schema_ver,
           codec, js_seq, rendering, dek_ref, dek_version)
        VALUES ($1, 'events.g1.system.scene.01PH3', 'test.event', now(),
                'system', $2, 1, $3, $4, '{}'::jsonb, $5, $6)
    `, id[:], envelopeBytes, string(s.codecName),
		int64(time.Now().UnixNano()),   // js_seq monotonic placeholder
		s.oldDEKID, int32(s.oldDEKVer)) //nolint:gosec // G115: oldDEKVer is uint32 < 2^31
	require.NoError(s.t, err)

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
	s.t.Helper()
	for _, id := range s.eventIDs {
		var actual int64
		err := s.pool.QueryRow(context.Background(),
			`SELECT dek_ref FROM events_audit WHERE id = $1`, id[:]).Scan(&actual)
		require.NoError(s.t, err)
		require.Equal(s.t, dekID, actual, "row %s must reference DEK %d after rewrite", id.String(), dekID)
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
	s.t.Helper()
	var r phase3RewrittenRow
	var codecStr string
	var dekVerInt32 int32
	err := s.pool.QueryRow(context.Background(), `
        SELECT envelope, codec, dek_ref, dek_version
          FROM events_audit
         WHERE id = $1
    `, id[:]).Scan(&r.Envelope, &codecStr, &r.DEKRef, &dekVerInt32)
	require.NoError(s.t, err)
	r.Codec = codec.Name(codecStr)
	r.DEKVersion = uint32(dekVerInt32) //nolint:gosec // G115: column constraint enforces non-negative
	return r
}

// stubBindingResolver / noopInvalidator are shared with manager_integration_test.go
// (package dek_test). Re-declared here is unnecessary.

// TestPhase3_RewriteAllRowsAtomically — INV-E7 happy path.
// Seeds 100 events under old DEK, runs Phase 3, asserts:
//   - rowsRewritten == 100
//   - checkpoint status advanced to phase3_reencrypt_cold
//   - every events_audit row now references new_dek_id
//   - checkpoint cursor points at the last (highest-id) row
func TestPhase3_RewriteAllRowsAtomically(t *testing.T) {
	setup := newPhase3TestSetup(t)
	const eventCount = 100
	setup.InsertEncryptedRows(eventCount)

	rid := setup.RequestID()
	rowsRewritten, err := setup.orch.RunPhase3(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, eventCount, rowsRewritten,
		"INV-E7: every seeded row must be rewritten in one invocation")

	ckpt, err := setup.repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusPhase3ReencryptCold, ckpt.Status,
		"Phase 3 leaves status at phase3_reencrypt_cold; phase5_invalidate transition is .22's job")
	require.NotNil(t, ckpt.NewDEKID)
	setup.AssertAllRowsReferenceDEK(*ckpt.NewDEKID)

	// Cursor points at the row with the largest id processed. idgen.New
	// is not strictly monotonic across rapid calls (entropy randomness),
	// so the test computes the expected cursor by taking the max id from
	// the seeded set rather than assuming insertion order matches sort
	// order. This still tests the contract: AdvanceCursor sees the last
	// row of the final batch, which (under the ORDER BY id ASC SELECT)
	// is the largest-id row.
	finalID, ok := ckpt.LastProcessedEventID()
	require.True(t, ok, "cursor must be populated after a successful rewrite")
	var maxSeed [16]byte
	for _, id := range setup.eventIDs {
		if compareBytes(id[:], maxSeed[:]) > 0 {
			copy(maxSeed[:], id[:])
		}
	}
	require.Equal(t, maxSeed, finalID,
		"INV-E7: cursor advances to the largest-id row processed")
}

// TestPhase3_CrashResumeIdempotent — INV-E7-COLD-RESUME-CURSOR.
// Seeds 2500 rows (>2× batch size) so the loop crosses multiple batch
// boundaries. Cancels the ctx after ~1000 rows via the test hook,
// resumes with a fresh ctx, asserts the rewrite completes and the
// cumulative state is identical to a non-crashed run.
func TestPhase3_CrashResumeIdempotent(t *testing.T) {
	setup := newPhase3TestSetup(t)
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
	require.Error(t, firstErr, "INV-E7: context cancel must surface an error")
	require.GreaterOrEqual(t, firstAttemptCount, 1000,
		"first attempt processed at least one batch before crash")
	require.Less(t, firstAttemptCount, eventCount,
		"first attempt crashed before completing all rows")

	// Verify the cursor advanced ONLY to the last committed batch
	// boundary. The crashed batch's rows + cursor advance MUST have been
	// rolled back together (INV-E7).
	ckptMid, err := setup.repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusPhase3ReencryptCold, ckptMid.Status,
		"crash leaves status at phase3_reencrypt_cold for the resume path")
	_, cursorSet := ckptMid.LastProcessedEventID()
	require.True(t, cursorSet, "at least one batch committed before crash")

	// Resume from the cursor with a fresh ctx.
	setup.orch.SetBatchHookForTest(nil) // no crash this time
	resumeCount, err := setup.orch.RunPhase3(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, eventCount-firstAttemptCount, resumeCount,
		"resume completes exactly the rows the first attempt did not")

	ckptFinal, err := setup.repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusPhase3ReencryptCold, ckptFinal.Status)
	require.NotNil(t, ckptFinal.NewDEKID)
	setup.AssertAllRowsReferenceDEK(*ckptFinal.NewDEKID)

	// Plaintext round-trip: decrypt each rewritten row under the NEW
	// DEK + new AAD; bytes MUST equal the seeded plaintext for that
	// row. This is the strongest form of INV-E7 (final state is
	// observationally identical to a non-crashed run).
	codecImpl, err := codec.Resolve(setup.codecName)
	require.NoError(t, err)
	for i, id := range setup.eventIDs {
		row := setup.LoadEventsAuditRow(id)
		var envelope eventbusv1.Event
		require.NoError(t, proto.Unmarshal(row.Envelope, &envelope))
		require.Equal(t, *ckptFinal.NewDEKID, row.DEKRef)
		require.Equal(t, setup.newDEKVer, row.DEKVersion)

		aadBytes, err := aad.Build(&envelope, string(row.Codec), uint64(row.DEKRef), row.DEKVersion) //nolint:gosec // G115: row.DEKRef is BIGSERIAL PK
		require.NoError(t, err)
		decoded, err := codecImpl.Decode(context.Background(), envelope.GetPayload(), setup.newKey, aadBytes)
		require.NoError(t, err, "row %d (%s) must decrypt under new DEK + new AAD after rewrite", i, id.String())
		require.Equal(t, setup.plaintexts[i], decoded,
			"INV-E7: plaintext byte-equal between pre-rewrite and post-rewrite for row %d", i)
	}
}

// TestPhase3_AADRebindOnRewrite — INV-E8.
// Seeds 1 row, runs Phase 3, asserts that attempting to decode the
// rewritten ciphertext under the OLD AAD (built with old dek_version)
// fails with AEAD tag mismatch. The new AAD MUST decode correctly.
func TestPhase3_AADRebindOnRewrite(t *testing.T) {
	setup := newPhase3TestSetup(t)
	setup.InsertEncryptedRows(1)

	rid := setup.RequestID()
	_, err := setup.orch.RunPhase3(context.Background(), rid)
	require.NoError(t, err)

	ckpt, err := setup.repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.NotNil(t, ckpt.NewDEKID)

	rewritten := setup.LoadEventsAuditRow(setup.eventIDs[0])
	require.Equal(t, *ckpt.NewDEKID, rewritten.DEKRef)

	var envelope eventbusv1.Event
	require.NoError(t, proto.Unmarshal(rewritten.Envelope, &envelope))

	codecImpl, err := codec.Resolve(rewritten.Codec)
	require.NoError(t, err)

	// New AAD: must succeed.
	newAAD, err := aad.Build(&envelope, string(rewritten.Codec), uint64(rewritten.DEKRef), rewritten.DEKVersion) //nolint:gosec // G115: rewritten.DEKRef is BIGSERIAL PK
	require.NoError(t, err)
	_, err = codecImpl.Decode(context.Background(), envelope.GetPayload(), setup.newKey, newAAD)
	require.NoError(t, err, "INV-E8: new AAD MUST decode the rewritten ciphertext")

	// Old AAD: must fail. Two distinct mutations probe the rebind
	// surface: (1) old dek_ref (= setup.oldDEKID) with new version,
	// (2) new dek_ref with old version. Either mutation breaks the AEAD
	// tag — both demonstrate the AAD bytes differ post-rewrite.
	oldRefAAD, err := aad.Build(&envelope, string(rewritten.Codec), uint64(setup.oldDEKID), rewritten.DEKVersion) //nolint:gosec // G115
	require.NoError(t, err)
	_, err = codecImpl.Decode(context.Background(), envelope.GetPayload(), setup.newKey, oldRefAAD)
	require.Error(t, err, "INV-E8: old dek_ref in AAD MUST fail AEAD tag check")

	oldVerAAD, err := aad.Build(&envelope, string(rewritten.Codec), uint64(rewritten.DEKRef), setup.oldDEKVer) //nolint:gosec // G115
	require.NoError(t, err)
	_, err = codecImpl.Decode(context.Background(), envelope.GetPayload(), setup.newKey, oldVerAAD)
	require.Error(t, err, "INV-E8: old dek_version in AAD MUST fail AEAD tag check")
}

// TestPhase3_HeartbeatAdvancesDuringLongRun — INV-E19.
// Seeds enough rows that the run takes >30s to ensure at least one
// heartbeat fires would be ideal, but at 1000-rows-per-batch + fast
// crypto the wall-clock won't reach 30s in CI. The contract under test
// is weaker but still meaningful: AdvanceCursor inside the batch tx
// bumps last_heartbeat_at as a side-effect (see CheckpointRepo.AdvanceCursor
// SQL), so every batch advances the heartbeat. We assert
// post-run last_heartbeat_at strictly exceeds the pre-run value, which
// proves the loop is making forward progress against the sweep-TTL clock.
func TestPhase3_HeartbeatAdvancesDuringLongRun(t *testing.T) {
	setup := newPhase3TestSetup(t)
	setup.InsertEncryptedRows(2500) // > 2 batches → multiple AdvanceCursor calls

	rid := setup.RequestID()
	ckptBefore, err := setup.repo.Get(context.Background(), rid)
	require.NoError(t, err)
	beforeHB := ckptBefore.LastHeartbeatAt

	// Sleep 5ms so even fast batch commits produce a strictly-greater
	// last_heartbeat_at (Postgres now() resolution is microsecond, but
	// the test's pre-read can race the first AdvanceCursor on hot CI
	// runners with clock skew).
	time.Sleep(5 * time.Millisecond)

	_, err = setup.orch.RunPhase3(context.Background(), rid)
	require.NoError(t, err)

	ckptAfter, err := setup.repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.True(t, ckptAfter.LastHeartbeatAt.After(beforeHB),
		"INV-E19: heartbeat MUST advance during the Phase 3 loop (before=%s after=%s)",
		beforeHB, ckptAfter.LastHeartbeatAt)
}

// TestPhase3_RequiresPrecondition — Phase 3 MUST refuse to run from any
// status outside {phase2_mint_dek, phase3_reencrypt_cold}.
func TestPhase3_RequiresPrecondition(t *testing.T) {
	setup := newPhase3TestSetup(t)
	rid := setup.RequestID()

	// Force the checkpoint into phase1_auth (illegal precondition).
	_, err := setup.pool.Exec(context.Background(),
		`UPDATE crypto_rekey_checkpoints SET status = 'phase1_auth' WHERE request_id = $1`, rid[:])
	require.NoError(t, err)

	_, err = setup.orch.RunPhase3(context.Background(), rid)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_PHASE_PRECONDITION_FAILED")
}

// TestPhase3_NoResolver_FailsClosed — calling RunPhase3 without
// SetMaterialResolver MUST refuse rather than NPE.
func TestPhase3_NoResolver_FailsClosed(t *testing.T) {
	setup := newPhase3TestSetup(t)
	// Reset the resolver to nil to simulate misconfigured wiring.
	setup.orch.SetMaterialResolver(nil)

	_, err := setup.orch.RunPhase3(context.Background(), setup.RequestID())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_MATERIAL_RESOLVER_NIL")
}
