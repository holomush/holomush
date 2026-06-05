// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// defaultPhase3BatchSize bounds per-transaction WAL pressure (assuming
// sensitive event payloads averaging 1–4 KiB → ~1–4 MiB per batch txn)
// and resume-replay cost (at most this many rows redone after a crash).
// Spec §4.3 Phase 3 calls for 1000 default, tunable via config.
const defaultPhase3BatchSize = 1000

// phase3HeartbeatInterval is the wall-clock cadence at which Phase 3
// updates last_heartbeat_at to reset the sweep-TTL clock (INV-CRYPTO-106).
const phase3HeartbeatInterval = 30 * time.Second

// MaterialResolver supplies decrypted DEK material for Phase 3 cold-tier
// rewrite. The Orchestrator needs both the OLD DEK (to decrypt existing
// events_audit rows) and the NEW DEK (to re-encrypt under the rekeyed
// key). VersionForDEKID maps a crypto_keys primary key to its version
// column so that AAD construction (INV-CRYPTO-95) sees the correct dek_version.
//
// Production wiring: *dek.manager satisfies this interface via Resolve
// (resolves by keyID + version) and a thin VersionForDEKID adapter that
// loads the version from store.selectByPK.
//
// The interface is separate from Minter to keep Phase 2 (mint) and
// Phase 3 (rewrite) concerns distinct. Both ride the same *manager in
// production.
type MaterialResolver interface {
	// Resolve returns the decrypted DEK material for (keyID, version).
	Resolve(ctx context.Context, keyID codec.KeyID, version uint32) (codec.Key, error)
	// VersionForDEKID returns the version column of the crypto_keys row
	// whose primary key id equals dekID. Used by Phase 3 to discover the
	// new DEK's version (the orchestrator's checkpoint row stores only
	// new_dek_id, not new_dek_version) for AAD construction (INV-CRYPTO-95).
	VersionForDEKID(ctx context.Context, dekID int64) (uint32, error)
}

// SetMaterialResolver installs the Phase 3 DEK material resolver and is
// the additive seam introduced by holomush-jxo8.7.21. NewOrchestrator's
// signature is intentionally unchanged (Phase 1/2 wiring from .19/.20
// must continue compiling); production wiring at cmd/holomush/core.go
// MUST call SetMaterialResolver after NewOrchestrator before invoking
// RunPhase3. Calling RunPhase3 without a resolver wired returns
// DEK_REKEY_MATERIAL_RESOLVER_NIL — fail closed.
func (o *Orchestrator) SetMaterialResolver(r MaterialResolver) {
	o.materialResolver = r
}

// SetBatchHookForTest installs a per-batch callback used by integration
// tests to simulate mid-Phase-3 crashes. fn receives the running total
// of rows rewritten so far in this RunPhase3 invocation; tests typically
// cancel the parent ctx once the total reaches a target threshold to
// exercise the resume path. Not safe for production use.
func (o *Orchestrator) SetBatchHookForTest(fn func(rowsRewrittenSoFar int)) {
	o.batchHookForTest = fn
}

// RunPhase3 reads every events_audit row whose dek_ref points at the
// rekey checkpoint's old DEK, in deterministic id-order batches. For
// each row the loop decrypts the existing ciphertext under the OLD DEK +
// OLD AAD, re-encrypts the plaintext under the NEW DEK with AAD rebuilt
// from (subject, type, new_key_id, new_version, codec) per INV-CRYPTO-95, and
// UPDATEs the events_audit row plus the checkpoint cursor inside one
// pgx.Tx so a crash mid-batch leaves no partially-rewritten state
// (INV-CRYPTO-94).
//
// Pre-condition: checkpoint.Status ∈ {Phase2MintDEK,
// Phase3ReencryptCold}. Phase2MintDEK is the fresh-entry case (Phase 2
// just finished); Phase3ReencryptCold is the resume case (a prior
// RunPhase3 invocation crashed mid-flight and the cursor records how
// far it got). The CAS status transition Phase2MintDEK →
// Phase3ReencryptCold runs once at the top of the fresh-entry path.
//
// Resume semantics: SELECT bounds rows by `id > last_processed_event_id`
// (NULL → '\x00'::bytea sentinel meaning "from the beginning"). Because
// the per-batch transaction commits both the events_audit UPDATEs and
// the cursor advance atomically, a crashed batch leaves both reverted —
// the next attempt re-selects the same rows and re-applies idempotently.
//
// Returns the number of rows rewritten by THIS invocation (not cumulative
// across resumes). A resumed run that completes 1,000 of 2,000 total
// rows reports 1,000 even though the cumulative final state is 2,000;
// callers needing the cumulative count read it from the checkpoint cursor.
func (o *Orchestrator) RunPhase3(ctx context.Context, rid RequestID) (int, error) {
	if o.materialResolver == nil {
		return 0, oops.Code("DEK_REKEY_MATERIAL_RESOLVER_NIL").
			Errorf("Phase 3 requires SetMaterialResolver(...) before RunPhase3")
	}

	ckpt, err := o.repo.Get(ctx, rid)
	if err != nil {
		return 0, err
	}
	if ckpt.Status != CheckpointStatusPhase2MintDEK && ckpt.Status != CheckpointStatusPhase3ReencryptCold {
		return 0, oops.Code("DEK_REKEY_PHASE_PRECONDITION_FAILED").
			With("expected", "phase2_mint_dek or phase3_reencrypt_cold").
			With("actual", string(ckpt.Status)).
			Errorf("Phase 3 requires status=phase2_mint_dek or phase3_reencrypt_cold")
	}
	if ckpt.NewDEKID == nil {
		return 0, oops.Code("DEK_REKEY_NEW_DEK_MISSING").
			Errorf("Phase 3 requires new_dek_id set by Phase 2")
	}

	// Fresh-entry transition: advance to phase3_reencrypt_cold so the
	// AdvanceCursor CAS predicate (status='phase3_reencrypt_cold') holds
	// for every batch. Resume runs skip this — the status is already
	// phase3_reencrypt_cold from the prior invocation.
	if ckpt.Status == CheckpointStatusPhase2MintDEK {
		if advErr := o.repo.UpdateStatus(ctx, rid, CheckpointStatusPhase2MintDEK, CheckpointStatusPhase3ReencryptCold); advErr != nil {
			return 0, oops.Code("DEK_REKEY_PHASE3_STATUS_ADVANCE_FAILED").Wrap(advErr)
		}
		// Re-read to pick up the fresh cursor + status; cheap, and avoids
		// stale-in-memory bugs on a second RunPhase3 invocation.
		var reReadErr error
		ckpt, reReadErr = o.repo.Get(ctx, rid)
		if reReadErr != nil {
			return 0, reReadErr
		}
	}

	// Resolve both DEKs once up front. The old DEK lookup requires the
	// version column from the events_audit rows themselves (each row
	// carries its own dek_version), but we can pre-cache by row's reported
	// version inside processPhase3Batch via the resolver cache.
	newDEKVersion, err := o.materialResolver.VersionForDEKID(ctx, *ckpt.NewDEKID)
	if err != nil {
		return 0, oops.Code("DEK_REKEY_NEW_DEK_VERSION_LOOKUP_FAILED").
			With("new_dek_id", *ckpt.NewDEKID).Wrap(err)
	}
	newKey, err := o.materialResolver.Resolve(ctx, codec.KeyID(*ckpt.NewDEKID), newDEKVersion) //nolint:gosec // G115: new_dek_id is a BIGSERIAL PK; always non-negative
	if err != nil {
		return 0, oops.Code("DEK_REKEY_NEW_KEY_RESOLVE_FAILED").
			With("new_dek_id", *ckpt.NewDEKID).
			With("new_dek_version", newDEKVersion).Wrap(err)
	}

	totalRewritten := 0
	lastHeartbeat := time.Now()
	cursor := append([]byte(nil), ckpt.lastProcessedEventID...) // copy to avoid aliasing

	for {
		// Honour cancellation between batches so a test/operator-driven
		// shutdown stops cleanly at a batch boundary rather than mid-row.
		// Per-row cancellation inside the batch is honoured by pgx via
		// the ctx passed to Exec/Query.
		if err := ctx.Err(); err != nil {
			return totalRewritten, oops.Code("DEK_REKEY_PHASE3_CANCELED").Wrap(err)
		}

		n, lastID, err := o.processPhase3Batch(ctx, ckpt.OldDEKID, *ckpt.NewDEKID,
			newKey, newDEKVersion, cursor, defaultPhase3BatchSize, rid)
		if err != nil {
			return totalRewritten, err
		}
		if n == 0 {
			break
		}
		totalRewritten += n
		cursor = lastID

		if o.batchHookForTest != nil {
			o.batchHookForTest(totalRewritten)
		}

		// Heartbeat is best-effort: failure here MUST NOT roll back the
		// batch (the batch tx is already committed) but is logged via the
		// error code so an operator chasing TTL aborts can correlate.
		if time.Since(lastHeartbeat) > phase3HeartbeatInterval {
			if hbErr := o.repo.Heartbeat(ctx, rid); hbErr != nil {
				o.logger.WarnContext(ctx, "phase3 heartbeat failed",
					"request_id", rid.String(), "err", hbErr.Error())
			}
			lastHeartbeat = time.Now()
		}
	}

	// The Phase 3 row count is now incremented atomically inside each
	// batch transaction via IncrementPhase3Count (see processPhase3Batch).
	// No post-loop persist call is needed: by the time we reach here, the
	// checkpoint row's phase3_rows_rewritten column already reflects every
	// committed batch's contribution. Crash-resume correctness is durable
	// (holomush-jxo8.7.54 — crypto-reviewer correctness fix).

	// Leave status at phase3_reencrypt_cold. Phase 5 (.22) advances to
	// phase5_invalidate. Don't transition here; the FSM only allows
	// phase3_reencrypt_cold → phase5_invalidate, not a "phase3_complete"
	// intermediate, and adding such a state crosses the out-of-scope line
	// for this bead.
	return totalRewritten, nil
}

// processPhase3Batch handles a single transactional batch: SELECT rows,
// decrypt + re-encrypt each, UPDATE events_audit, AdvanceCursor — all
// committed together so a crashed batch leaves the cursor and the
// events_audit rows mutually consistent (INV-CRYPTO-94).
//
// Returns (rowsRewritten, newCursor, error). rowsRewritten == 0 with
// nil cursor + nil error signals "no more rows" to the caller's loop.
func (o *Orchestrator) processPhase3Batch(
	ctx context.Context,
	oldDEKID, newDEKID int64,
	newKey codec.Key,
	newDEKVersion uint32,
	afterID []byte,
	batchSize int,
	rid RequestID,
) (rowsRewritten int, newCursor []byte, err error) {
	tx, err := o.repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, nil, oops.Code("DEK_REKEY_BATCH_TXN_FAILED").Wrap(err)
	}
	committed := false
	defer func() {
		if !committed {
			// Rollback errors are swallowed: the only useful action on
			// rollback failure is logging, which we already do via the
			// returned error. We don't want to mask the original error.
			_ = tx.Rollback(ctx) //nolint:errcheck // rollback error masking would lose the original failure cause
		}
	}()

	// SELECT uses `>= zero sentinel` semantics: when afterID is nil/empty
	// (initial scan, cursor never advanced), match all rows by treating
	// the comparison as a no-op via the COALESCE'd '\x00'::bytea bound.
	// Order by id (ULID) so re-running with the same cursor reproduces
	// the same row sequence on resume — load-bearing for INV-CRYPTO-94.
	rows, err := tx.Query(ctx, `
        SELECT id, subject, type, envelope, codec, dek_version
          FROM events_audit
         WHERE dek_ref = $1 AND id > COALESCE($2, '\x00'::bytea)
         ORDER BY id ASC
         LIMIT $3
    `, oldDEKID, afterID, batchSize)
	if err != nil {
		return 0, nil, oops.Code("DEK_REKEY_BATCH_QUERY_FAILED").Wrap(err)
	}

	type rowData struct {
		eventID    []byte
		subject    string
		evType     string
		envelope   []byte
		codecName  string
		dekVersion uint32
	}
	var batch []rowData
	for rows.Next() {
		var r rowData
		var dekVersionRaw int32
		if scanErr := rows.Scan(&r.eventID, &r.subject, &r.evType, &r.envelope, &r.codecName, &dekVersionRaw); scanErr != nil {
			rows.Close()
			return 0, nil, oops.Code("DEK_REKEY_BATCH_SCAN_FAILED").Wrap(scanErr)
		}
		if dekVersionRaw < 0 {
			rows.Close()
			return 0, nil, oops.Code("DEK_REKEY_BAD_DEK_VERSION").
				With("dek_version", dekVersionRaw).
				With("event_id_hex", hexEncodeBytes(r.eventID)).
				Errorf("events_audit row has negative dek_version")
		}
		r.dekVersion = uint32(dekVersionRaw)
		batch = append(batch, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		rows.Close()
		return 0, nil, oops.Code("DEK_REKEY_BATCH_ROWS_ERR").Wrap(rowsErr)
	}
	rows.Close()

	if len(batch) == 0 {
		// No rows left. Commit the empty tx (cheap) and return — caller
		// loop breaks. Don't AdvanceCursor on an empty batch: the cursor
		// is already at its terminal value.
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return 0, nil, oops.Code("DEK_REKEY_BATCH_COMMIT_FAILED").Wrap(commitErr)
		}
		committed = true
		return 0, nil, nil
	}

	var lastID []byte
	for _, r := range batch {
		// Unmarshal the stored envelope so AAD construction sees the
		// full event metadata (subject/type/timestamp/actor/eventID)
		// exactly as the original encrypt path did — INV-CRYPTO-95 requires the
		// AAD bytes to differ ONLY in dek_ref + dek_version after rewrite.
		var envelope eventbusv1.Event
		if unmarshalErr := proto.Unmarshal(r.envelope, &envelope); unmarshalErr != nil {
			return 0, nil, oops.Code("DEK_REKEY_ENVELOPE_UNMARSHAL_FAILED").
				With("event_id_hex", hexEncodeBytes(r.eventID)).Wrap(unmarshalErr)
		}

		// Resolve the OLD DEK material for this row's specific version.
		// Most rows will share a single version, so the underlying cache
		// inside MaterialResolver hits on subsequent calls.
		//
		// NOTE: oldDEKID is the PK from the checkpoint row; r.dekVersion
		// is the column value scanned from this events_audit row. They
		// must agree (the partial index on dek_ref guarantees the row
		// references oldDEKID; the version column records the version
		// active when the row was emitted).
		oldKey, err := o.materialResolver.Resolve(ctx, codec.KeyID(oldDEKID), r.dekVersion) //nolint:gosec // G115: oldDEKID is a BIGSERIAL PK; always non-negative
		if err != nil {
			return 0, nil, oops.Code("DEK_REKEY_OLD_KEY_RESOLVE_FAILED").
				With("old_dek_id", oldDEKID).
				With("dek_version", r.dekVersion).Wrap(err)
		}

		oldAAD, aadErr := aad.Build(&envelope, r.codecName, uint64(oldDEKID), r.dekVersion) //nolint:gosec // G115: oldDEKID is a BIGSERIAL PK; always non-negative
		if aadErr != nil {
			return 0, nil, oops.Code("DEK_REKEY_OLD_AAD_BUILD_FAILED").
				With("event_id_hex", hexEncodeBytes(r.eventID)).Wrap(aadErr)
		}

		codecImpl, codecErr := codec.Resolve(codec.Name(r.codecName))
		if codecErr != nil {
			return 0, nil, oops.Code("DEK_REKEY_CODEC_UNKNOWN").
				With("codec", r.codecName).Wrap(codecErr)
		}

		plaintext, decErr := codecImpl.Decode(ctx, envelope.GetPayload(), oldKey, oldAAD)
		if decErr != nil {
			return 0, nil, oops.Code("DEK_REKEY_DECODE_FAILED").
				With("event_id_hex", hexEncodeBytes(r.eventID)).
				With("codec", r.codecName).Wrap(decErr)
		}

		// AAD rebind (INV-CRYPTO-95): new AAD is built from the same envelope
		// fields but with the NEW (dek_ref, dek_version). Old AAD MUST
		// fail to decode the rewritten ciphertext — proved by
		// TestPhase3_AADRebindOnRewrite.
		newAAD, aadErr := aad.Build(&envelope, r.codecName, uint64(newDEKID), newDEKVersion) //nolint:gosec // G115: newDEKID is a BIGSERIAL PK; always non-negative
		if aadErr != nil {
			return 0, nil, oops.Code("DEK_REKEY_NEW_AAD_BUILD_FAILED").
				With("event_id_hex", hexEncodeBytes(r.eventID)).Wrap(aadErr)
		}

		reencoded, encErr := codecImpl.Encode(ctx, plaintext, newKey, newAAD)
		if encErr != nil {
			return 0, nil, oops.Code("DEK_REKEY_ENCODE_FAILED").
				With("event_id_hex", hexEncodeBytes(r.eventID)).
				With("codec", r.codecName).Wrap(encErr)
		}

		// Repack the envelope with the new ciphertext payload and
		// re-marshal. All non-payload fields stay byte-identical so the
		// next reader (cold-tier dispatcher) reconstructs AAD with the
		// same subject/type/timestamp/actor.
		envelope.Payload = reencoded
		newEnvelopeBytes, marshalErr := proto.MarshalOptions{Deterministic: true}.Marshal(&envelope)
		if marshalErr != nil {
			return 0, nil, oops.Code("DEK_REKEY_ENVELOPE_MARSHAL_FAILED").
				With("event_id_hex", hexEncodeBytes(r.eventID)).Wrap(marshalErr)
		}

		if _, execErr := tx.Exec(ctx, `
            UPDATE events_audit
               SET envelope = $2, dek_ref = $3, dek_version = $4
             WHERE id = $1
        `, r.eventID, newEnvelopeBytes, newDEKID, int32(newDEKVersion)); execErr != nil { //nolint:gosec // G115: newDEKVersion is uint32; fits in int32 for column storage (versions <2^31)
			return 0, nil, oops.Code("DEK_REKEY_ROW_UPDATE_FAILED").
				With("event_id_hex", hexEncodeBytes(r.eventID)).Wrap(execErr)
		}
		lastID = r.eventID
	}

	// Increment the per-batch row count INSIDE the transaction so the
	// count, the row rewrites, and the cursor advance all commit
	// atomically. A crash mid-batch rolls back all three; a crash AFTER
	// commit leaves them consistent. This is load-bearing for audit-payload
	// truth across crash-resume cycles (holomush-jxo8.7.54).
	if countErr := o.repo.IncrementPhase3Count(ctx, tx, rid, len(batch)); countErr != nil {
		return 0, nil, countErr
	}

	// Advance the cursor as the FINAL action inside the transaction so a
	// rollback reverts both the row UPDATEs and the cursor advance
	// atomically (INV-CRYPTO-94). The CAS predicate inside
	// AdvanceCursor (status='phase3_reencrypt_cold') guards against a
	// concurrent abort racing the commit.
	if cursorErr := o.repo.AdvanceCursor(ctx, tx, rid, lastID); cursorErr != nil {
		return 0, nil, cursorErr
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		return 0, nil, oops.Code("DEK_REKEY_BATCH_COMMIT_FAILED").Wrap(commitErr)
	}
	committed = true

	return len(batch), lastID, nil
}

// hexEncodeBytes hex-encodes b for inclusion in error With() metadata.
// Avoids pulling in fmt.Sprintf %x for hot-path error construction.
func hexEncodeBytes(b []byte) string {
	const hexChars = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hexChars[c>>4]
		out[i*2+1] = hexChars[c&0x0f]
	}
	return string(out)
}
