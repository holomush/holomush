// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// newPoseULID returns a fresh 16-byte ULID for use as scene_log.id in
// audit integration tests. Uses ulid.Make (test-only helper) to avoid
// pulling in core.NewULID's monotonic dependencies.
func newPoseULID() []byte {
	id := ulid.Make()
	b := id.Bytes()
	return b[:]
}

var _ = Describe("SceneAuditStore.InsertScenePose", func() {
	// INV-SCENE-10: the scene_log INSERT, scenes.total_pose_count UPDATE,
	// and scene_participants.last_pose_at/last_pose_seq UPDATE MUST
	// either all commit, or none do. This block exercises all three
	// branches: happy path, rollback on missing scene (forces step-2
	// QueryRow.Scan to return ErrNoRows), and the actor-not-participant
	// edge case (intentional 0-row UPDATE in step 3).

	It("commits scene_log row, total_pose_count bump, and participant pose metadata together", func() {
		store := newTestStore()
		audit := NewSceneAuditStore(store.Pool())
		ctx := context.Background()

		sceneID := "scene-isp-happy"
		owner := "char-isp-owner"
		row := &SceneRow{
			ID: sceneID, Title: "T", OwnerID: owner,
			State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

		// Capture the timestamp we feed in so we can assert it round-trips
		// onto last_pose_at (canonical event ts, not wall-clock).
		eventTime := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Millisecond)
		eventID := newPoseULID()

		err := audit.InsertScenePose(
			ctx,
			eventID,
			"events.main.scene."+sceneID+".ic",
			"scene_pose",
			timestamppb.New(eventTime),
			"character", []byte("char-isp-owner-actor"),
			[]byte(`{"pose":"waves"}`), 1, "identity", nil, nil,
			sceneID, owner,
		)
		Expect(err).NotTo(HaveOccurred())

		// 1. scene_log row landed.
		var logCount int
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT COUNT(*) FROM scene_log WHERE id = $1`, eventID,
		).Scan(&logCount)).NotTo(HaveOccurred())
		Expect(logCount).To(Equal(1), "scene_log INSERT MUST commit on happy path")

		// 2. scenes.total_pose_count = 1.
		var total int
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT total_pose_count FROM scenes WHERE id = $1`, sceneID,
		).Scan(&total)).NotTo(HaveOccurred())
		Expect(total).To(Equal(1), "total_pose_count MUST bump by 1")

		// 3. scene_participants.last_pose_at + last_pose_seq for the owner.
		var (
			lastAt  *pgnanos.Time
			lastSeq *int32
		)
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT last_pose_at, last_pose_seq FROM scene_participants
			 WHERE scene_id = $1 AND character_id = $2`,
			sceneID, owner,
		).Scan(&lastAt, &lastSeq)).NotTo(HaveOccurred())
		Expect(lastAt).NotTo(BeNil(), "last_pose_at MUST be set")
		Expect(lastAt.Time().UTC().Truncate(time.Millisecond)).To(Equal(eventTime),
			"last_pose_at MUST use the canonical event timestamp")
		Expect(lastSeq).NotTo(BeNil(), "last_pose_seq MUST be set")
		Expect(*lastSeq).To(Equal(int32(1)), "last_pose_seq MUST match total_pose_count")
	})

	It("rolls back the scene_log INSERT when the scenes UPDATE fails (INV-SCENE-10)", func() {
		store := newTestStore()
		audit := NewSceneAuditStore(store.Pool())
		ctx := context.Background()

		// No scene row exists for "nonexistent-scene", so step 2's
		// UPDATE … RETURNING returns zero rows ⇒ QueryRow.Scan returns
		// pgx.ErrNoRows ⇒ the transaction rolls back.
		eventID := newPoseULID()
		err := audit.InsertScenePose(
			ctx,
			eventID,
			"events.main.scene.nonexistent-scene.ic", "scene_pose",
			timestamppb.Now(),
			"character", []byte("char-rollback-actor"),
			[]byte("{}"), 1, "identity", nil, nil,
			"nonexistent-scene", "char-rollback-actor",
		)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "SCENE_TOTAL_POSE_COUNT_UPDATE_FAILED")

		// scene_log row MUST NOT exist — the INSERT must have rolled back.
		var logCount int
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT COUNT(*) FROM scene_log WHERE id = $1`, eventID,
		).Scan(&logCount)).NotTo(HaveOccurred())
		Expect(logCount).To(Equal(0),
			"scene_log INSERT MUST roll back when total_pose_count UPDATE finds no scene (INV-SCENE-10)")
	})

	It("commits scene_log + total_pose_count when actor is not a current participant", func() {
		store := newTestStore()
		audit := NewSceneAuditStore(store.Pool())
		ctx := context.Background()

		sceneID := "scene-isp-orphan"
		owner := "char-isp-owner-orphan"
		row := &SceneRow{
			ID: sceneID, Title: "T", OwnerID: owner,
			State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

		// posedCharID isn't in scene_participants. The participant UPDATE
		// in step 3 is a 0-row no-op — but step 1 + step 2 still commit.
		eventID := newPoseULID()
		err := audit.InsertScenePose(
			ctx,
			eventID,
			"events.main.scene."+sceneID+".ic", "scene_pose",
			timestamppb.Now(),
			"character", []byte("char-orphan-actor"),
			[]byte("{}"), 1, "identity", nil, nil,
			sceneID, "char-orphan-never-joined",
		)
		Expect(err).NotTo(HaveOccurred(),
			"actor-not-participant MUST be non-fatal (0-row UPDATE)")

		// scene_log row MUST be committed.
		var logCount int
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT COUNT(*) FROM scene_log WHERE id = $1`, eventID,
		).Scan(&logCount)).NotTo(HaveOccurred())
		Expect(logCount).To(Equal(1), "scene_log INSERT MUST commit even when participant UPDATE is 0-row")

		// total_pose_count MUST be bumped.
		var total int
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT total_pose_count FROM scenes WHERE id = $1`, sceneID,
		).Scan(&total)).NotTo(HaveOccurred())
		Expect(total).To(Equal(1),
			"total_pose_count MUST bump even when actor missing from scene_participants")

		// Owner's metadata MUST remain pristine — no spurious UPDATE.
		var (
			lastAt  *pgnanos.Time
			lastSeq *int32
		)
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT last_pose_at, last_pose_seq FROM scene_participants
			 WHERE scene_id = $1 AND character_id = $2`,
			sceneID, owner,
		).Scan(&lastAt, &lastSeq)).NotTo(HaveOccurred())
		Expect(lastAt).To(BeNil(), "non-actor participant's last_pose_at MUST be untouched")
		Expect(lastSeq).To(BeNil(), "non-actor participant's last_pose_seq MUST be untouched")
	})
})

// makeAuditRow builds a minimal AuditEventRequest for dispatcher tests.
func makeAuditRow(
	id []byte,
	subject, eventType string,
	ts *timestamppb.Timestamp,
	actorID []byte,
) *pluginv1.AuditEventRequest {
	return &pluginv1.AuditEventRequest{
		Row: &pluginv1.AuditRow{
			Id:        id,
			Subject:   subject,
			Type:      eventType,
			Timestamp: ts,
			Actor: &eventbusv1.Actor{
				Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
				Id:   actorID,
			},
			Payload:   []byte(`{}`),
			SchemaVer: 1,
			Codec:     "identity",
		},
	}
}

var _ = Describe("SceneAuditServer.AuditEvent dispatcher", func() {
	// Per spec §9.4: AuditEvent MUST route scene_pose events through
	// InsertScenePose (transactional path) and all other types through
	// Insert (plain path).

	It("routes scene_pose to InsertScenePose: scene_log + total_pose_count + last_pose_at all commit", func() {
		// Setup: create a real scene. The owner's character_id is set to
		// a ULID string so the dispatcher's posedCharID conversion matches
		// and the participant UPDATE is a real hit (not a 0-row no-op).
		store := newTestStore()
		audit := NewSceneAuditStore(store.Pool())
		srv := &SceneAuditServer{store: audit}
		ctx := context.Background()

		// ownerULID is the character ID we'll use as both the scene owner
		// and the actorID bytes in the audit row. AuditEvent does
		// copy(posedCharULID[:], actorID) → posedCharULID.String()
		// which must equal ownerCharID for the participant UPDATE to hit.
		ownerULID := ulid.Make()
		ownerCharID := ownerULID.String()
		ownerBytes := ownerULID.Bytes()

		sceneID := "scene-disp-pose"
		row := &SceneRow{
			ID: sceneID, Title: "T", OwnerID: ownerCharID,
			State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

		eventTime := time.Now().UTC().Add(-3 * time.Minute).Truncate(time.Millisecond)
		eventID := newPoseULID()
		subject := "events.main.scene." + sceneID + ".ic"

		req := makeAuditRow(eventID, subject, "scene_pose", timestamppb.New(eventTime), ownerBytes[:])
		_, err := srv.AuditEvent(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		// 1. scene_log row landed.
		var logCount int
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT COUNT(*) FROM scene_log WHERE id = $1`, eventID,
		).Scan(&logCount)).NotTo(HaveOccurred())
		Expect(logCount).To(Equal(1), "scene_log INSERT MUST commit via dispatcher scene_pose path")

		// 2. total_pose_count bumped to 1.
		var total int
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT total_pose_count FROM scenes WHERE id = $1`, sceneID,
		).Scan(&total)).NotTo(HaveOccurred())
		Expect(total).To(Equal(1), "total_pose_count MUST bump when routed through InsertScenePose")

		// 3. scene_participants.last_pose_at stamped with the canonical event ts.
		var lastAt *pgnanos.Time
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT last_pose_at FROM scene_participants
			 WHERE scene_id = $1 AND character_id = $2`,
			sceneID, ownerCharID,
		).Scan(&lastAt)).NotTo(HaveOccurred())
		Expect(lastAt).NotTo(BeNil(),
			"last_pose_at MUST be set when scene_pose routes through InsertScenePose")
		Expect(lastAt.Time().UTC().Truncate(time.Millisecond)).To(Equal(eventTime),
			"last_pose_at MUST use the canonical event timestamp, not wall clock")
	})

	It("routes non-scene_pose to Insert: only scene_log lands, total_pose_count unchanged", func() {
		store := newTestStore()
		audit := NewSceneAuditStore(store.Pool())
		srv := &SceneAuditServer{store: audit}
		ctx := context.Background()

		ownerULID := ulid.Make()
		ownerCharID := ownerULID.String()

		sceneID := "scene-disp-join"
		row := &SceneRow{
			ID: sceneID, Title: "T", OwnerID: ownerCharID,
			State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

		eventID := newPoseULID()
		subject := "events.main.scene." + sceneID + ".ic"
		actorBytes := newPoseULID() // arbitrary 16-byte actor; type is scene_join_ic not scene_pose

		req := makeAuditRow(eventID, subject, "scene_join_ic", timestamppb.Now(), actorBytes)
		_, err := srv.AuditEvent(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		// 1. scene_log row landed.
		var logCount int
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT COUNT(*) FROM scene_log WHERE id = $1`, eventID,
		).Scan(&logCount)).NotTo(HaveOccurred())
		Expect(logCount).To(Equal(1), "scene_log INSERT MUST commit for non-pose events via Insert path")

		// 2. total_pose_count MUST remain 0 — the Insert path does NOT touch scenes.
		var total int
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT total_pose_count FROM scenes WHERE id = $1`, sceneID,
		).Scan(&total)).NotTo(HaveOccurred())
		Expect(total).To(Equal(0),
			"total_pose_count MUST NOT be bumped by non-scene_pose events (plain Insert path)")
	})

	It("rejects scene_pose with malformed subject as SCENE_AUDIT_SUBJECT_INVALID", func() {
		store := newTestStore()
		audit := NewSceneAuditStore(store.Pool())
		srv := &SceneAuditServer{store: audit}
		ctx := context.Background()

		eventID := newPoseULID()
		// Subject missing the required scene segment — parseSceneSubject rejects it.
		req := makeAuditRow(eventID, "events.main.bad", "scene_pose", timestamppb.Now(), newPoseULID())
		_, err := srv.AuditEvent(ctx, req)
		Expect(err).To(HaveOccurred())
		// parseSceneSubject is the sole oops wrapper on this path; the
		// dispatcher returns the parser's error as-is so the deepest
		// (and only) code is the one we assert.
		errutil.AssertErrorCode(suiteT, err, "SCENE_AUDIT_SUBJECT_INVALID")
	})

	It("rejects scene_pose with a non-character actor as SCENE_AUDIT_INVALID_ACTOR_KIND", func() {
		store := newTestStore()
		audit := NewSceneAuditStore(store.Pool())
		srv := &SceneAuditServer{store: audit}
		ctx := context.Background()

		sceneID := "scene-disp-bad-actor-kind"
		row := &SceneRow{
			ID: sceneID, Title: "T", OwnerID: ulid.Make().String(),
			State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

		eventID := newPoseULID()
		subject := "events.main.scene." + sceneID + ".ic"
		actorID := newPoseULID()

		req := &pluginv1.AuditEventRequest{
			Row: &pluginv1.AuditRow{
				Id:        eventID,
				Subject:   subject,
				Type:      "scene_pose",
				Timestamp: timestamppb.Now(),
				Actor: &eventbusv1.Actor{
					Kind: eventbusv1.ActorKind_ACTOR_KIND_PLUGIN,
					Id:   actorID,
				},
				Payload:   []byte(`{}`),
				SchemaVer: 1,
				Codec:     "identity",
			},
		}
		_, err := srv.AuditEvent(ctx, req)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "SCENE_AUDIT_INVALID_ACTOR_KIND")

		// Defense-in-depth: total_pose_count MUST remain at 0 because
		// the rejected row never reached InsertScenePose.
		var total int
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT total_pose_count FROM scenes WHERE id = $1`, sceneID,
		).Scan(&total)).NotTo(HaveOccurred())
		Expect(total).To(Equal(0),
			"total_pose_count MUST stay 0 when scene_pose is rejected for non-character actor")
	})

	It("rejects scene_pose with a malformed actor ID length as SCENE_AUDIT_INVALID_ACTOR_ID", func() {
		store := newTestStore()
		audit := NewSceneAuditStore(store.Pool())
		srv := &SceneAuditServer{store: audit}
		ctx := context.Background()

		sceneID := "scene-disp-bad-actor-id"
		row := &SceneRow{
			ID: sceneID, Title: "T", OwnerID: ulid.Make().String(),
			State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

		eventID := newPoseULID()
		subject := "events.main.scene." + sceneID + ".ic"
		shortActor := []byte{0x01, 0x02, 0x03} // not 16 bytes

		req := makeAuditRow(eventID, subject, "scene_pose", timestamppb.Now(), shortActor)
		_, err := srv.AuditEvent(ctx, req)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "SCENE_AUDIT_INVALID_ACTOR_ID")

		// Defense-in-depth: total_pose_count MUST remain at 0.
		var total int
		Expect(store.Pool().QueryRow(
			ctx,
			`SELECT total_pose_count FROM scenes WHERE id = $1`, sceneID,
		).Scan(&total)).NotTo(HaveOccurred())
		Expect(total).To(Equal(0),
			"total_pose_count MUST stay 0 when scene_pose is rejected for malformed actor ID")
	})
})
