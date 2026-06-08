// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/pgnanos"
)

// poseMeta holds the per-participant pose-order metadata captured from
// scene_participants for comparison across tamper + rebuild phases.
type poseMeta struct {
	lastPoseAt  *pgnanos.Time
	lastPoseSeq *int32
}

// readTotalPoseCount fetches scenes.total_pose_count for sceneID.
func readTotalPoseCount(ctx context.Context, pool *pgxpool.Pool, sceneID string) int {
	GinkgoHelper()
	var count int
	Expect(pool.QueryRow(
		ctx,
		`SELECT total_pose_count FROM scenes WHERE id = $1`, sceneID,
	).Scan(&count)).NotTo(HaveOccurred())
	return count
}

// readParticipantMeta fetches last_pose_at + last_pose_seq for one participant.
func readParticipantMeta(ctx context.Context, pool *pgxpool.Pool, sceneID, characterID string) poseMeta {
	GinkgoHelper()
	var m poseMeta
	Expect(pool.QueryRow(
		ctx,
		`SELECT last_pose_at, last_pose_seq FROM scene_participants
		 WHERE scene_id = $1 AND character_id = $2`,
		sceneID, characterID,
	).Scan(&m.lastPoseAt, &m.lastPoseSeq)).NotTo(HaveOccurred())
	return m
}

// rebuildMetadataFromSceneLog re-derives pose-order metadata purely from
// scene_log scene_pose rows. This is the INV-SCENE-8 recovery path: if
// maintained metadata (total_pose_count, last_pose_at, last_pose_seq)
// drifts due to operator intervention or a future bug, reading scene_log
// and recomputing in Go produces identical state.
//
// Algorithm (mirrors spec §6 / InsertScenePose contract):
//  1. Walk scene_log rows for this scene in chronological order (ORDER BY id).
//  2. Assign monotonic sequence numbers starting at 1 (mirroring total_pose_count
//     RETURNING the incremented value per row).
//  3. For each actor: track the last seq and timestamp seen.
//  4. UPDATE scenes.total_pose_count = total rows seen.
//  5. For each participant: UPDATE last_pose_seq + last_pose_at.
//
// Actor IDs are stored as ULID bytes in scene_log.actor_id. The same
// ULID.String() conversion the AuditEvent dispatcher applies gives the
// character_id stored in scene_participants.
func rebuildMetadataFromSceneLog(ctx context.Context, pool *pgxpool.Pool, sceneID string) {
	GinkgoHelper()

	// Read scene_log scene_pose rows for this scene in chronological order.
	// id is a ULID (time-ordered), so ORDER BY id ASC is chronological.
	// subject matches events.<gameID>.scene.<sceneID>.ic (the .ic channel
	// is where scene_pose events are published).
	rows, err := pool.Query(ctx, `
		SELECT actor_id, timestamp
		FROM scene_log
		WHERE subject LIKE 'events.%.scene.' || $1 || '.ic'
		  AND type = 'core-scenes:scene_pose'
		ORDER BY id ASC
	`, sceneID)
	Expect(err).NotTo(HaveOccurred())
	defer rows.Close()

	type actorLast struct {
		seq int32
		ts  pgnanos.Time
	}
	totalCount := int32(0)
	lastByActor := map[string]actorLast{}

	for rows.Next() {
		totalCount++
		var actorIDBytes []byte
		var ts pgnanos.Time
		Expect(rows.Scan(&actorIDBytes, &ts)).NotTo(HaveOccurred())

		// Convert ULID bytes (16 bytes) to the string form stored in
		// scene_participants.character_id — matching AuditEvent's conversion:
		//   copy(posedCharULID[:], actorID); posedCharID = posedCharULID.String()
		var ulidVal ulid.ULID
		copy(ulidVal[:], actorIDBytes)
		charID := ulidVal.String()

		// Keep last occurrence per actor (highest seq wins).
		lastByActor[charID] = actorLast{seq: totalCount, ts: ts}
	}
	Expect(rows.Err()).NotTo(HaveOccurred())

	// Update scenes.total_pose_count.
	_, err = pool.Exec(
		ctx,
		`UPDATE scenes SET total_pose_count = $1 WHERE id = $2`,
		totalCount, sceneID,
	)
	Expect(err).NotTo(HaveOccurred())

	// Update scene_participants for each actor observed in scene_log.
	for charID, last := range lastByActor {
		_, err = pool.Exec(
			ctx,
			`UPDATE scene_participants
			 SET last_pose_seq = $1, last_pose_at = $2
			 WHERE scene_id = $3 AND character_id = $4`,
			last.seq, last.ts, sceneID, charID,
		)
		Expect(err).NotTo(HaveOccurred())
	}
}

var _ = Describe("INV-SCENE-8: pose-order metadata is a function of scene_log", func() {
	// INV-SCENE-8: scenes.total_pose_count + scene_participants.last_pose_at /
	// last_pose_seq are a deterministic function of scene_log scene_pose rows.
	// After tampering with the maintained metadata (simulating operator drift),
	// a rebuild that re-reads scene_log MUST produce identical state to what
	// InsertScenePose computed transactionally.
	//
	// This pins the defense-in-depth guarantee: if maintained metadata drifts
	// (operator intervention, future bug), the canonical truth is scene_log
	// and a rebuild produces identical state. Supports INV-SCENE-10 (transactional
	// consistency is not the only guard; the event log is the source of truth).

	It("rebuilds maintained metadata identically after tamper (INV-SCENE-8)", func() {
		store := newTestStore()
		audit := NewSceneAuditStore(store.Pool())
		ctx := context.Background()

		// Use ULID-keyed character IDs so actor_id bytes in scene_log
		// round-trip correctly through ULID.String() in rebuildMetadataFromSceneLog,
		// matching the AuditEvent dispatcher's copy(posedCharULID[:], actorID) pattern.
		char1ULID := ulid.Make()
		char2ULID := ulid.Make()
		char1ID := char1ULID.String()
		char2ID := char2ULID.String()
		char1Bytes := char1ULID.Bytes()
		char2Bytes := char2ULID.Bytes()

		sceneID := "scene-inv-p4-8"

		// Setup: create scene with char1 as owner; add char2 as member.
		sceneRow := &SceneRow{
			ID: sceneID, Title: "INV-SCENE-8 Rebuild Test", OwnerID: char1ID,
			State:           string(SceneStateActive),
			PoseOrder:       string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, sceneRow)).NotTo(HaveOccurred())
		_, _, err := store.AddParticipant(ctx, sceneID, char2ID)
		Expect(err).NotTo(HaveOccurred())

		subject := "events.main.scene." + sceneID + ".ic"

		// Phase 1: emit 3 scene_pose events via InsertScenePose.
		// Chronological order: pose 1 → char1, pose 2 → char2, pose 3 → char1.
		// Expected maintained state:
		//   total_pose_count = 3
		//   char1: last_pose_seq = 3, last_pose_at = poseTs[2]
		//   char2: last_pose_seq = 2, last_pose_at = poseTs[1]
		now := time.Now().UTC()
		poseTs := []time.Time{
			now.Add(-30 * time.Second).Truncate(time.Millisecond),
			now.Add(-20 * time.Second).Truncate(time.Millisecond),
			now.Add(-10 * time.Second).Truncate(time.Millisecond),
		}
		poses := []struct {
			charID string
			bytes  []byte
		}{
			{char1ID, char1Bytes[:]},
			{char2ID, char2Bytes[:]},
			{char1ID, char1Bytes[:]},
		}

		for i, p := range poses {
			eventID := newPoseULID()
			Expect(audit.InsertScenePose(
				ctx,
				eventID,
				subject, "core-scenes:scene_pose",
				timestamppb.New(poseTs[i]),
				"character", p.bytes,
				[]byte(`{}`), 1, "identity", nil, nil,
				sceneID, p.charID,
			)).NotTo(HaveOccurred())
		}

		// Capture Phase 1 maintained metadata (ground truth).
		wantTotal := readTotalPoseCount(ctx, store.Pool(), sceneID)
		wantChar1 := readParticipantMeta(ctx, store.Pool(), sceneID, char1ID)
		wantChar2 := readParticipantMeta(ctx, store.Pool(), sceneID, char2ID)

		// Verify Phase 1 invariants before proceeding.
		Expect(wantTotal).To(Equal(3), "Phase 1: total_pose_count must be 3")
		Expect(wantChar1.lastPoseSeq).NotTo(BeNil())
		Expect(*wantChar1.lastPoseSeq).To(Equal(int32(3)),
			"Phase 1: char1.last_pose_seq must be 3 (posed at seq 1 and 3)")
		Expect(wantChar2.lastPoseSeq).NotTo(BeNil())
		Expect(*wantChar2.lastPoseSeq).To(Equal(int32(2)),
			"Phase 1: char2.last_pose_seq must be 2 (posed at seq 2)")
		Expect(wantChar1.lastPoseAt).NotTo(BeNil())
		Expect(wantChar2.lastPoseAt).NotTo(BeNil())

		// Phase 2: tamper — reset all metadata to simulate operator drift
		// (e.g. a failed migration, manual SQL, or a future bug).
		_, err = store.Pool().Exec(ctx,
			`UPDATE scenes SET total_pose_count = 0 WHERE id = $1`, sceneID)
		Expect(err).NotTo(HaveOccurred())
		_, err = store.Pool().Exec(ctx,
			`UPDATE scene_participants
			 SET last_pose_at = NULL, last_pose_seq = NULL
			 WHERE scene_id = $1`, sceneID)
		Expect(err).NotTo(HaveOccurred())

		// Sanity-check: confirm the tamper took effect.
		Expect(readTotalPoseCount(ctx, store.Pool(), sceneID)).To(Equal(0),
			"tamper sanity: total_pose_count must be 0 after reset")
		tamperedChar1 := readParticipantMeta(ctx, store.Pool(), sceneID, char1ID)
		Expect(tamperedChar1.lastPoseSeq).To(BeNil(),
			"tamper sanity: char1.last_pose_seq must be NULL after reset")

		// Phase 3: rebuild from scene_log — no InsertScenePose, pure re-derivation.
		rebuildMetadataFromSceneLog(ctx, store.Pool(), sceneID)

		// Phase 4: assert rebuilt state matches Phase 1 capture exactly.
		// INV-SCENE-8: metadata is a deterministic function of scene_log.
		gotTotal := readTotalPoseCount(ctx, store.Pool(), sceneID)
		gotChar1 := readParticipantMeta(ctx, store.Pool(), sceneID, char1ID)
		gotChar2 := readParticipantMeta(ctx, store.Pool(), sceneID, char2ID)

		Expect(gotTotal).To(Equal(wantTotal),
			"INV-SCENE-8: rebuilt total_pose_count MUST equal maintained value")

		Expect(gotChar1.lastPoseSeq).NotTo(BeNil(),
			"INV-SCENE-8: rebuilt char1.last_pose_seq MUST be set")
		Expect(*gotChar1.lastPoseSeq).To(Equal(*wantChar1.lastPoseSeq),
			"INV-SCENE-8: rebuilt char1.last_pose_seq MUST equal maintained value")

		Expect(gotChar2.lastPoseSeq).NotTo(BeNil(),
			"INV-SCENE-8: rebuilt char2.last_pose_seq MUST be set")
		Expect(*gotChar2.lastPoseSeq).To(Equal(*wantChar2.lastPoseSeq),
			"INV-SCENE-8: rebuilt char2.last_pose_seq MUST equal maintained value")

		// last_pose_at is BIGINT-ns; round-trip is bit-exact (INV-STORE-1, INV-STORE-2).
		Expect(gotChar1.lastPoseAt).NotTo(BeNil(),
			"INV-SCENE-8: rebuilt char1.last_pose_at MUST be set")
		Expect(gotChar2.lastPoseAt).NotTo(BeNil(),
			"INV-SCENE-8: rebuilt char2.last_pose_at MUST be set")
		Expect(gotChar1.lastPoseAt.Time().Equal(wantChar1.lastPoseAt.Time())).To(BeTrue(),
			"INV-SCENE-8: rebuilt char1.last_pose_at MUST equal maintained value at ns precision")
		Expect(gotChar2.lastPoseAt.Time().Equal(wantChar2.lastPoseAt.Time())).To(BeTrue(),
			"INV-SCENE-8: rebuilt char2.last_pose_at MUST equal maintained value at ns precision")
	})
})

// Verifies: INV-SCENE-61
var _ = Describe("holomush-5rh.8.4: observer excluded from the pose-order roster", func() {
	// Pins the DB-level structural exclusion: ListParticipantsWithPoseMeta's
	// SQL filters `p.role IN ('owner', 'member')` (store.go), so a REAL
	// role='observer' row never reaches poseorder.Compute. This is the
	// authoritative pin for the pose-order clause of the observer exclusions;
	// the unit test in poseorder_test.go only documents that Compute itself
	// has no role awareness.
	It("omits a role='observer' row from ListParticipantsWithPoseMeta", func() {
		store := newTestStore()
		ctx := context.Background()

		ownerID := ulid.Make().String()
		memberID := ulid.Make().String()
		observerID := ulid.Make().String()

		sceneID := "scene-poseorder-observer-excl"
		Expect(store.CreateWithOwner(ctx, &SceneRow{
			ID: sceneID, Title: "Observer Pose-Order Exclusion", OwnerID: ownerID,
			State:           string(SceneStateActive),
			PoseOrder:       string(PoseOrderModeStrict),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{}, Tags: []string{},
		})).NotTo(HaveOccurred())
		mustAddParticipant(store, sceneID, memberID, "member")
		mustAddParticipant(store, sceneID, observerID, "observer")

		meta, err := store.ListParticipantsWithPoseMeta(ctx, sceneID)
		Expect(err).NotTo(HaveOccurred())

		ids := make([]string, 0, len(meta.Participants))
		for _, p := range meta.Participants {
			ids = append(ids, p.CharacterID)
		}
		Expect(ids).To(ConsistOf(ownerID, memberID),
			"pose-order roster MUST contain exactly the owner and member")
		Expect(ids).NotTo(ContainElement(observerID),
			"role='observer' row MUST be excluded by the role IN ('owner','member') filter")
	})
})
