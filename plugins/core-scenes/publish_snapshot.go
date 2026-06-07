// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/types/known/timestamppb"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// snapshotDecryptBatch is the per-call row cap the snapshot chunks its decrypt
// into. The host's read-back primitive REJECTS an over-cap batch
// (DECRYPT_BATCH_TOO_LARGE at maxDecryptBatch=500 — it does NOT silently clamp,
// read-back design §3.2), so the snapshot MUST chunk to support scenes of any
// length. Kept at the host cap so each call is maximally efficient while still
// bounding per-call memory + blast radius.
const snapshotDecryptBatch = 500

// snapshotEventKinds maps the three publishable IC event types to their
// PublishedSceneEntry kind. scene_ooc and all notice/ops events are excluded by
// ReadSceneLogForSnapshot's type filter, so any other type here is unreachable.
var snapshotEventKinds = map[string]EntryKind{
	"core-scenes:scene_pose": EntryKindPose,
	"core-scenes:scene_say":  EntryKindSay,
	"core-scenes:scene_emit": EntryKindEmit,
}

// snapshotDecryptor is the narrow host-mediated read-back decrypt seam the
// snapshot consumes. The production wiring (SetSnapshotDecryptor) supplies a
// pluginsdk.SnapshotDecryptor; tests substitute a real-stack ReadbackDecryptor
// adapter so the full crypto path is exercised without standing up the gRPC
// plugin host. The plugin NEVER holds a DEK — it submits ciphertext rows and
// receives per-row plaintext or a typed refusal (INV-CRYPTO-26, INV-CRYPTO-37).
type snapshotDecryptor interface {
	DecryptOwnAuditRows(ctx context.Context, rows []*pluginv1.AuditRow) ([]*pluginv1.RowResult, error)
}

// runSnapshot drives the COOLOFF→PUBLISHED snapshot pipeline for one attempt
// (read-back design §3.3, §6; scenes-phase-6 §11). It is a callable invoked by
// the scheduler ticker (E5, out of scope here).
//
// Ordering (atomicity, INV-CRYPTO-33): the IC rows are read, decrypted, and rendered
// BEFORE the write-tx opens — the write-tx (SELECT FOR UPDATE on published_scenes
// + re-validate COOLOFF + all-yes) is the serialization point, and there is no
// observable intermediate state where a publication is PUBLISHED without content.
//
// Failure mapping (INV-CRYPTO-35, §11.4):
//   - ANY per-row decrypt refusal/error → ATTEMPT_FAILED / SNAPSHOT_DECRYPT_FAILED
//   - render/decode error               → ATTEMPT_FAILED / SNAPSHOT_RENDER_FAILED
//   - all-yes broken at re-validate      → ATTEMPT_FAILED / COOLOFF_INVARIANT_BROKEN
//   - status no longer COOLOFF           → idempotent no-op (vote-flip race)
//
// The fullICSubject is events.<game_id>.scene.<scene_id>.ic — the snapshot's
// authoritative subject for the in-tx SQL read and the AAD subject (the store
// has no game_id of its own, so the caller passes the full subject).
func (s *SceneServiceImpl) runSnapshot(ctx context.Context, attemptID, sceneID, fullICSubject string) error {
	ctx, span := startSpan(ctx, "scene.publish.snapshot",
		attribute.String("attempt_id", attemptID),
		attribute.String("scene_id", sceneID))
	defer span.End()

	start := time.Now()
	outcome := "published"
	defer func() {
		metricScenePublishSnapshotDuration(outcome, time.Since(start).Seconds())
	}()

	if s.decryptor == nil {
		outcome = "error"
		return oops.Code("SCENE_PUBLISH_SNAPSHOT_NO_DECRYPTOR").
			With("attempt_id", attemptID).
			Errorf("snapshot decryptor not configured")
	}

	// Phase 1 (no write lock): read the full IC row set in one read-tx — a
	// consistent snapshot of scene_log — then decrypt + render entirely outside
	// any transaction (a pure transform). All of this completes before the
	// write-tx opens (read-back design §6).
	logRows, err := s.readSceneLogTx(ctx, fullICSubject)
	if err != nil {
		outcome = "error"
		return err
	}

	entries, failReason := s.decryptAndRender(ctx, attemptID, fullICSubject, logRows)

	// Phase 2 (write-tx): lock, re-validate, and atomically finalize. A content
	// failure detected in phase 1 transitions to ATTEMPT_FAILED here (under the
	// lock) rather than mid-read, so the failure write is serialized too.
	pool := s.store.SnapshotPool()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		outcome = "error"
		return oops.Code("SCENE_PUBLISH_SNAPSHOT_TX_BEGIN_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	pub, err := s.store.LockForSnapshot(ctx, tx, attemptID)
	if err != nil {
		if oe, ok := oops.AsOops(err); ok {
			if code, isStr := oe.Code().(string); isStr && code == "SCENE_PUBLISH_INVALID_STATE" {
				// Status flipped out of COOLOFF before we acquired the lock — a
				// vote-flip race. Idempotent no-op; commit nothing.
				outcome = "noop"
				slog.InfoContext(ctx, "snapshot no-op: attempt not in COOLOFF at lock",
					"attempt_id", attemptID, "scene_id", sceneID)
				return nil
			}
		}
		outcome = "error"
		return err //nolint:wrapcheck // store oops code (SCENE_PUBLISH_LOCK_FAILED/NOT_FOUND) passes through to the ticker logger
	}

	// Re-validate the all-yes invariant under the lock (INV-CRYPTO-33, §11.3). A
	// vote-flip that committed before our lock is observed here consistently.
	tally, err := s.store.TallyVotesTx(ctx, tx, pub.ID)
	if err != nil {
		outcome = "error"
		return err //nolint:wrapcheck // store oops code (SCENE_PUBLISH_TALLY_FAILED) passes through to the ticker logger
	}
	if tally.Yes == 0 || tally.No > 0 || tally.Pending > 0 {
		return s.failSnapshotTx(ctx, tx, attemptID, FailureCoolOffInvariantBroken, &outcome)
	}

	// A content-level failure from phase 1 becomes ATTEMPT_FAILED under the lock.
	if failReason != "" {
		return s.failSnapshotTx(ctx, tx, attemptID, failReason, &outcome)
	}

	// Read frozen scene metadata in-tx (title + owner/member character IDs). A
	// scene deleted mid-publication yields empty meta — publication still
	// finalizes (FK soft-no-op, ADR holomush-jrefa).
	meta, err := s.store.ReadSceneMetaForSnapshot(ctx, tx, sceneID)
	if err != nil {
		outcome = "error"
		return err //nolint:wrapcheck // store oops code (SCENE_PUBLISH_META_*) passes through to the ticker logger
	}

	if mpErr := s.store.MarkPublished(ctx, tx, attemptID, MarkPublishedInput{
		ContentEntries:       entries,
		TitleSnapshot:        meta.Title,
		ParticipantsSnapshot: meta.Participants,
		PublishedAt:          time.Now(),
	}); mpErr != nil {
		outcome = "error"
		return mpErr //nolint:wrapcheck // store oops code (SCENE_PUBLISH_MARK_PUBLISHED_FAILED/INVALID_TRANSITION) passes through to the ticker logger
	}

	// Archive the parent scene. FK soft-no-op: published_scenes has no FK to
	// scenes(id), so the scene may have been deleted between COOLOFF entry and
	// snapshot fire. A 0-row UPDATE is NOT a failure — publication still
	// finalizes; the archive intentionally outlives its source (ADR
	// holomush-jrefa). Log a warning and continue.
	archived, err := s.store.ArchiveSceneStateForPublish(ctx, tx, sceneID)
	if err != nil {
		outcome = "error"
		return err //nolint:wrapcheck // store oops code (SCENE_PUBLISH_ARCHIVE_FAILED) passes through to the ticker logger
	}
	if !archived {
		slog.WarnContext(ctx,
			"scene deleted before publication snapshot — publication completes; scene state UPDATE no-op",
			"attempt_id", attemptID,
			"scene_id", sceneID,
			"code", "SCENE_PUBLISH_PARENT_SCENE_GONE")
	}

	if err := tx.Commit(ctx); err != nil {
		outcome = "error"
		return oops.Code("SCENE_PUBLISH_SNAPSHOT_COMMIT_FAILED").Wrap(err)
	}

	slog.InfoContext(ctx, "scene publication snapshot completed",
		"attempt_id", attemptID, "scene_id", sceneID,
		"entry_count", len(entries), "scene_archived", archived)

	// Post-tx: emit scene_publish_resolved{outcome=PUBLISHED}. Best-effort —
	// the DB commit above is the source of truth (mirrors applyTrigger).
	if emitErr := s.events.emitResolved(ctx, attemptID, StatusPublished, nil, tally); emitErr != nil {
		slog.WarnContext(ctx, "publish-resolved emit failed",
			"err", emitErr.Error(), "attempt_id", attemptID)
	}
	return nil
}

// readSceneLogTx opens a short read-tx and reads the full filtered IC row set
// (a consistent snapshot of scene_log). The tx is closed before decrypt/render,
// so no lock is held during the host round-trips.
func (s *SceneServiceImpl) readSceneLogTx(ctx context.Context, fullICSubject string) ([]LogRow, error) {
	pool := s.store.SnapshotPool()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, oops.Code("SCENE_PUBLISH_SNAPSHOT_READ_TX_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only tx; rollback releases the snapshot

	rows, err := s.store.ReadSceneLogForSnapshot(ctx, tx, fullICSubject)
	if err != nil {
		return nil, err //nolint:wrapcheck // store oops code (SCENE_PUBLISH_LOG_*) passes through; logged by the caller
	}
	return rows, nil
}

// decryptAndRender chunks the IC rows through the host read-back decrypt entry
// (≤snapshotDecryptBatch per call, INV-CRYPTO-37 order-preserving), then decodes
// each plaintext {actor_id,text} payload into a PublishedSceneEntry.
//
// Return contract — (entries, failReason):
//   - (entries, "")                         — success
//   - (nil, FailureSnapshotDecryptFailed)   — any per-row refusal/host error
//     (INV-CRYPTO-35, INV-CRYPTO-37 "treat any refusal as a publish failure")
//   - (nil, FailureSnapshotRenderFailed)    — a plaintext payload could not be
//     decoded into an entry
//
// There is no infrastructure-error return: a host decrypt error is mapped to
// SNAPSHOT_DECRYPT_FAILED (the content cannot be recovered, so the attempt
// fails closed rather than retrying forever). The (failReason) shape lets the
// caller perform the ATTEMPT_FAILED transition inside the write-tx, under the
// published_scenes lock.
func (s *SceneServiceImpl) decryptAndRender(ctx context.Context, attemptID, fullICSubject string, logRows []LogRow) ([]PublishedSceneEntry, PublishFailureReason) {
	entries := make([]PublishedSceneEntry, 0, len(logRows))

	for start := 0; start < len(logRows); start += snapshotDecryptBatch {
		end := start + snapshotDecryptBatch
		if end > len(logRows) {
			end = len(logRows)
		}
		chunk := logRows[start:end]

		auditRows := make([]*pluginv1.AuditRow, len(chunk))
		for i := range chunk {
			auditRows[i] = logRowToAuditRow(fullICSubject, chunk[i])
		}

		results, err := s.decryptor.DecryptOwnAuditRows(ctx, auditRows)
		if err != nil {
			// A host-level decrypt error (e.g. DEK destroyed mid-flight, batch
			// rejection) is a snapshot decrypt failure (INV-CRYPTO-35): the content
			// cannot be recovered.
			slog.ErrorContext(ctx, "snapshot decrypt batch failed",
				"err", err.Error(), "attempt_id", attemptID,
				"chunk_start", start, "chunk_len", len(chunk),
				"code", "SNAPSHOT_DECRYPT_FAILED")
			return nil, FailureSnapshotDecryptFailed
		}
		if len(results) != len(chunk) {
			// The primitive contract is 1:1 in input order (INV-CRYPTO-37). A
			// length mismatch is a contract violation — fail the snapshot
			// closed rather than render a partial scene.
			slog.ErrorContext(ctx, "snapshot decrypt returned wrong result count",
				"attempt_id", attemptID, "want", len(chunk), "got", len(results),
				"code", "SNAPSHOT_DECRYPT_FAILED")
			return nil, FailureSnapshotDecryptFailed
		}

		for i := range results {
			plaintext, refused := decryptedPlaintext(results[i])
			if refused != "" {
				// ANY row refusal → the whole publish fails (INV-CRYPTO-37). Do NOT
				// render a partial scene.
				slog.ErrorContext(ctx, "snapshot row decrypt refused",
					"attempt_id", attemptID, "reason", refused,
					"code", "SNAPSHOT_DECRYPT_FAILED")
				return nil, FailureSnapshotDecryptFailed
			}
			entry, ok, decodeErr := decodeSnapshotEntry(chunk[i].Type, plaintext)
			if decodeErr != nil {
				slog.ErrorContext(ctx, "snapshot entry decode failed",
					"err", decodeErr.Error(), "attempt_id", attemptID,
					"event_type", chunk[i].Type, "code", "SNAPSHOT_RENDER_FAILED")
				return nil, FailureSnapshotRenderFailed
			}
			if !ok {
				// Unknown event type slipped past the SQL filter — should be
				// unreachable, but never silently drop content.
				return nil, FailureSnapshotRenderFailed
			}
			entries = append(entries, entry)
		}
	}
	return entries, ""
}

// failSnapshotTx applies the ATTEMPT_FAILED transition with the given reason
// inside the write-tx and commits, then records the outcome metric label. A
// failed transition (e.g. status flipped) bubbles up as an error.
func (s *SceneServiceImpl) failSnapshotTx(ctx context.Context, tx pgx.Tx, attemptID string, reason PublishFailureReason, outcome *string) error {
	if err := s.store.FailAttemptTx(ctx, tx, attemptID, reason); err != nil {
		*outcome = "error"
		return err //nolint:wrapcheck // store oops code (SCENE_PUBLISH_TRANSITION_FAILED/INVALID_TRANSITION) passes through to the ticker logger
	}
	if err := tx.Commit(ctx); err != nil {
		*outcome = "error"
		return oops.Code("SCENE_PUBLISH_SNAPSHOT_COMMIT_FAILED").Wrap(err)
	}
	*outcome = "failed"
	slog.InfoContext(ctx, "scene publication snapshot failed",
		"attempt_id", attemptID, "failure_reason", string(reason))

	// Post-tx best-effort resolved emit (outcome=ATTEMPT_FAILED).
	frCopy := reason
	if emitErr := s.events.emitResolved(ctx, attemptID, StatusAttemptFailed, &frCopy, nil); emitErr != nil {
		slog.WarnContext(ctx, "publish-resolved emit failed",
			"err", emitErr.Error(), "attempt_id", attemptID)
	}
	return nil
}

// logRowToAuditRow converts a scene_log LogRow into the *pluginv1.AuditRow the
// host read-back primitive consumes. The conversion mirrors the QueryHistory
// proto build (audit.go) field-for-field so the AAD the host rebuilds via
// AuditRowToEvent + aad.Build is byte-equal to the encrypt-side AAD
// (INV-CRYPTO-29 / INV-STORE-5). Subject is the snapshot's authoritative IC subject
// (the store passes the same value to the WHERE clause). DEK ref/version are
// widened to the proto's unsigned optional fields only when present (identity
// rows leave them nil).
func logRowToAuditRow(subject string, r LogRow) *pluginv1.AuditRow {
	out := &pluginv1.AuditRow{
		Id:        r.ID,
		Subject:   subject,
		Type:      r.Type,
		Timestamp: timestamppb.New(r.Timestamp.Time()),
		Actor:     actorProtoFromRow(r.ActorKind, r.ActorID),
		Codec:     r.Codec,
		Payload:   r.Payload,
		SchemaVer: int32(r.SchemaVer),
	}
	if r.DEKRef != nil {
		v := uint64(*r.DEKRef) //nolint:gosec // scene_log.dek_ref is BIGINT from crypto_keys.id (>= 0); int64→uint64 widening is safe
		out.DekRef = &v
	}
	if r.DEKVersion != nil {
		v := uint32(*r.DEKVersion) //nolint:gosec // scene_log.dek_version is a 1-based counter (>= 0); int32→uint32 widening is safe
		out.DekVersion = &v
	}
	return out
}

// decryptedPlaintext extracts the plaintext OR the refusal reason from a
// per-row RowResult. Exactly one arm is set (the proto oneof guarantees it):
// a non-empty refusal string means the row was NOT decrypted.
func decryptedPlaintext(res *pluginv1.RowResult) (plaintext []byte, refusal string) {
	if reason := res.GetNoPlaintextReason(); reason != "" {
		return nil, reason
	}
	return res.GetPlaintext(), ""
}

// decodeSnapshotEntry decodes a decrypted IC payload ({actor_id,text} JSON, the
// shape emitted by handleEmit / consumed by decodeReplayEntries) into a
// PublishedSceneEntry. Returns ok=false for an event type with no entry-kind
// mapping (unreachable past the SQL filter; never silently dropped).
func decodeSnapshotEntry(eventType string, plaintext []byte) (PublishedSceneEntry, bool, error) {
	kind, ok := snapshotEventKinds[eventType]
	if !ok {
		return PublishedSceneEntry{}, false, nil
	}
	var pl struct {
		ActorID string `json:"actor_id"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal(plaintext, &pl); err != nil {
		return PublishedSceneEntry{}, false, oops.Code("SCENE_PUBLISH_ENTRY_DECODE_FAILED").
			With("event_type", eventType).Wrap(err)
	}
	return PublishedSceneEntry{Speaker: pl.ActorID, Kind: kind, Content: pl.Text}, true, nil
}

// compile-time assurance that the SDK's SnapshotDecryptor satisfies the narrow
// snapshotDecryptor seam the service consumes.
var _ snapshotDecryptor = pluginsdk.SnapshotDecryptor(nil)
