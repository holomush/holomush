// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// E5 publishScheduler integration tests — holomush-5rh.20.38.
//
// Tests cover:
//  1. vote_window timeout: COLLECTING attempt past vote_window → ATTEMPT_FAILED/TIMEOUT
//  2. cool-off expiry → PUBLISHED with real-crypto content_entries under the production .ic
//     subject (closes C7 fidelity gap: both C7 reviewers flagged wrong subject = AEAD failure)
//  3. COLLECTING attempt NOT yet past the window is untouched
//  4. sweep with no expired attempts is a clean no-op
//  5. per-attempt failure WARN-logs and sweep continues to the next attempt
//
// The scheduler's `now` clock is injected so tests drive deterministic time
// without waiting real ticker intervals. sweep(ctx) is called directly.
//
// Ginkgo dot-import collision note: this is package main with the Ginkgo/Gomega
// dot-imports, so PublishedSceneEntry (NOT a bare Entry) is used.
package main

import (
	"context"
	"database/sql"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/plugintest"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// emitAndSeedIC is like snapshotRealEnv.emitAndSeed but seeds scene_log under
// the production .ic subject (events.<game>.scene.<sceneID>.ic). This is the
// correct AAD subject for production pose events emitted via
// dotStyleSceneSubjectIC(). The E5 scheduler calls runSnapshot with
// dotStyleSceneSubjectIC(...) as fullICSubject; seeding under a different subject
// would cause AEAD tag-check failures (the AAD binds the subject at encrypt time).
//
// The existing emitAndSeed seeds under the base scene subject (without .ic) because
// the colon-style "scene:sceneID" intent translates to "events.<game>.scene.<id>".
// This helper bypasses the colon-style path and emits directly with the dot-style
// .ic subject so the AAD round-trips correctly.
func emitAndSeedIC(ctx context.Context, env *snapshotRealEnv, sceneID, eventType, plaintext string, participants []dek.Participant) {
	GinkgoHelper()

	icSubject := dotStyleSceneSubjectIC(env.gameID, sceneID)
	ctxID := dek.ContextID{Type: "scene", ID: sceneID}
	_, err := env.dekMgr.GetOrCreate(ctx, ctxID, participants)
	Expect(err).NotTo(HaveOccurred())

	manifest := &plugins.Manifest{
		Name:                env.pluginName,
		Emits:               []string{"scene"},
		ActorKindsClaimable: []string{"plugin"},
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: eventType, Sensitivity: plugins.SensitivityAlways, Readback: true},
			},
		},
	}
	manifestFn := func(name string) *plugins.Manifest {
		if name == env.pluginName {
			return manifest
		}
		return nil
	}
	pluginActorID := plugintest.PluginULIDFromName(env.pluginName).String()
	actorFn := func(_ context.Context, _ string) (core.Actor, error) {
		return core.Actor{Kind: core.ActorPlugin, ID: pluginActorID}, nil
	}
	emitter := plugins.NewPluginEventEmitter(env.pluginPub, manifestFn, actorFn)
	intent := pluginsdk.EmitIntent{
		Subject:   icSubject, // DOT-STYLE .ic subject — AAD binds this exact string
		Type:      pluginsdk.EventType(eventType),
		Payload:   plaintext,
		Sensitive: true,
	}
	Expect(emitter.Emit(ctx, env.pluginName, intent)).To(Succeed())
	env.hostSub.AwaitDrained(GinkgoT(), 10*time.Second)

	// Read back the ciphertext from events_audit (keyed on the .ic subject).
	var (
		idB        []byte
		codecStr   string
		envelopeB  []byte
		schemaVer  int32
		dekRef     sql.NullInt64
		dekVersion sql.NullInt32
	)
	err = env.cryptoPool.QueryRow(
		ctx, `
		SELECT id, codec, envelope, schema_ver, dek_ref, dek_version
		FROM events_audit WHERE subject = $1 AND type = $2 ORDER BY id DESC LIMIT 1`,
		icSubject, eventType,
	).Scan(&idB, &codecStr, &envelopeB, &schemaVer, &dekRef, &dekVersion)
	Expect(err).NotTo(HaveOccurred(), "emitAndSeedIC: read events_audit ciphertext under .ic subject")

	var ev eventbusv1.Event
	Expect(proto.Unmarshal(envelopeB, &ev)).To(Succeed())

	var actorKind string
	var actorIDBytes []byte
	if a := ev.GetActor(); a != nil {
		actorKind = a.GetKind().String()
		actorIDBytes = a.GetId()
	}
	var dekRefP *int64
	if dekRef.Valid {
		dekRefP = &dekRef.Int64
	}
	var dekVerP *int32
	if dekVersion.Valid {
		v := dekVersion.Int32
		dekVerP = &v
	}

	_, err = env.store.Pool().Exec(
		ctx, `
		INSERT INTO scene_log (id, subject, type, timestamp, actor_kind, actor_id, payload, schema_ver, codec, dek_ref, dek_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		idB, icSubject, eventType,
		ev.GetTimestamp().AsTime().UnixNano(),
		actorKind, actorIDBytes, ev.GetPayload(), schemaVer, codecStr, dekRefP, dekVerP,
	)
	Expect(err).NotTo(HaveOccurred(), "emitAndSeedIC: INSERT scene_log ciphertext under .ic subject")
}

var _ = Describe("E5 publishScheduler", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
	})
	AfterEach(func() { cancel() })

	// -------------------------------------------------------------------------
	// 1. vote_window timeout: COLLECTING attempt past window → ATTEMPT_FAILED/TIMEOUT
	// -------------------------------------------------------------------------
	It("transitions a COLLECTING attempt past vote_window to ATTEMPT_FAILED with TIMEOUT (deterministic clock)", func() {
		const (
			sceneID = "01E5SCHED_TIMEOUT00000000A"
			ownerID = "01E5SCHED_TIMEOUTOWNER000A"
		)
		store := newTestStore()
		svc := newTestService(GinkgoT(), store)
		svc.gameID = "main"

		// Create a scene + attempt with a 1-second vote window.
		row := &SceneRow{
			ID: sceneID, Title: "Timeout Scene", OwnerID: ownerID,
			State: string(SceneStateEnded), PoseOrder: string(PoseOrderModeFree),
			Visibility: string(SceneVisibilityOpen), ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

		voteWindow := time.Second
		pub, err := store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
			SceneID:       sceneID,
			AttemptNumber: 1,
			InitiatedBy:   ownerID,
			VoteWindow:    voteWindow,
			CoolOffWindow: time.Minute,
			MaxAttempts:   3,
		})
		Expect(err).NotTo(HaveOccurred())

		// Advance now beyond the vote_window deadline.
		initiatedAt := pub.InitiatedAt.Time()
		pastDeadline := initiatedAt.Add(voteWindow + time.Millisecond)

		sched := &publishScheduler{
			svc:      svc,
			store:    store,
			interval: time.Minute, // not used in direct sweep calls
			now:      func() time.Time { return pastDeadline },
		}

		Expect(sched.sweep(ctx)).To(Succeed())

		// Attempt must be ATTEMPT_FAILED with TIMEOUT.
		got, err := store.GetPublishedSceneHeader(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(StatusAttemptFailed),
			"E5: COLLECTING attempt past vote_window MUST transition to ATTEMPT_FAILED")
		Expect(got.FailureReason).NotTo(BeNil())
		Expect(*got.FailureReason).To(Equal(FailureTimeout),
			"E5: failure_reason MUST be TIMEOUT for vote_window expiry")
	})

	// -------------------------------------------------------------------------
	// 2. cool-off expiry → PUBLISHED with real-crypto content_entries
	//    Seeded under the PRODUCTION .ic subject to close the C7 fidelity gap.
	//    (Wrong subject = AEAD tag-check failure = every decrypt fails silently.)
	// -------------------------------------------------------------------------
	It("fires runSnapshot with dotStyleSceneSubjectIC and publishes with real-crypto content_entries (C7 fidelity, .ic subject)", func() {
		const (
			pluginName = "core-scenes"
			sceneID    = "01E5SCHED_COOLOFF0000000A"
			ownerID    = "01E5SCHED_COOLOFFOWNER0A0"
			text       = "The scheduler fires and the scene is published."
		)

		// Use the real-crypto env so encryption/decryption is production-faithful.
		env := buildSnapshotRealEnv(ctx, pluginName)
		defer env.teardown()

		// Seed a real encrypted pose under the PRODUCTION .ic subject.
		// emitAndSeedIC inserts into scene_log with subject=dotStyleSceneSubjectIC(gameID, sceneID).
		emitAndSeedIC(ctx, env, sceneID, "core-scenes:scene_pose",
			`{"actor_id":"`+ownerID+`","text":"`+text+`"}`,
			[]dek.Participant{{
				PlayerID:    "01E5SCHED_COOLOFFPLAYER0A",
				CharacterID: ownerID,
				BindingID:   "01E5SCHED_COOLOFFBIND00A",
				JoinedAt:    time.Now().UTC(),
				AddedVia:    "e5_test",
			}})

		// Create scene + COOLOFF attempt.
		row := &SceneRow{
			ID: sceneID, Title: "Real Crypto Scene", OwnerID: ownerID,
			State: string(SceneStateEnded), PoseOrder: string(PoseOrderModeFree),
			Visibility: string(SceneVisibilityOpen), ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(env.store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

		coolOffWindow := time.Second
		pub, err := env.store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
			SceneID: sceneID, AttemptNumber: 1, InitiatedBy: ownerID,
			VoteWindow: time.Minute, CoolOffWindow: coolOffWindow, MaxAttempts: 3,
		})
		Expect(err).NotTo(HaveOccurred())

		// Vote all-yes then transition to COOLOFF.
		voters, err := env.store.ListPublishVoters(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		for _, v := range voters {
			_, err := env.store.CastVote(ctx, pub.ID, v.CharacterID, true)
			Expect(err).NotTo(HaveOccurred())
		}
		coolOffStarted := time.Now()
		Expect(env.store.TransitionStatus(ctx, pub.ID, TransitionInput{
			To: StatusCoolOff, SetCoolOffAt: &coolOffStarted,
		})).To(Succeed())

		// Advance now past the cooloff_window deadline.
		pastCoolOff := coolOffStarted.Add(coolOffWindow + time.Millisecond)

		sched := &publishScheduler{
			svc:      env.svc,
			store:    env.store,
			interval: time.Minute,
			now:      func() time.Time { return pastCoolOff },
		}

		Expect(sched.sweep(ctx)).To(Succeed())

		// Attempt must be PUBLISHED with populated content_entries holding the real plaintext.
		got, err := env.store.GetPublishedSceneHeader(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(StatusPublished),
			"E5: COOLOFF attempt past cooloff_window MUST transition to PUBLISHED")
		Expect(got.PublishedAt).NotTo(BeNil())

		entries, err := env.store.GetPublishedSceneContent(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(1),
			"E5: content_entries must hold the decrypted pose from the .ic stream")
		Expect(entries[0].Kind).To(Equal(EntryKindPose))
		Expect(entries[0].Content).To(Equal(text),
			"E5: content_entries must hold rendered PLAINTEXT (real AEAD decrypt via .ic subject)")
	})

	// -------------------------------------------------------------------------
	// 3. COLLECTING attempt NOT yet past the window is untouched
	// -------------------------------------------------------------------------
	It("leaves a COLLECTING attempt whose vote_window has not expired untouched", func() {
		const (
			sceneID = "01E5SCHED_NOTYET00000000A"
			ownerID = "01E5SCHED_NOTYETOWNER000A"
		)
		store := newTestStore()
		svc := newTestService(GinkgoT(), store)
		svc.gameID = "main"

		row := &SceneRow{
			ID: sceneID, Title: "Not Yet Scene", OwnerID: ownerID,
			State: string(SceneStateEnded), PoseOrder: string(PoseOrderModeFree),
			Visibility: string(SceneVisibilityOpen), ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

		pub, err := store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
			SceneID:       sceneID,
			AttemptNumber: 1,
			InitiatedBy:   ownerID,
			VoteWindow:    7 * 24 * time.Hour, // week-long window
			CoolOffWindow: time.Minute,
			MaxAttempts:   3,
		})
		Expect(err).NotTo(HaveOccurred())

		// now is BEFORE the window deadline.
		beforeDeadline := pub.InitiatedAt.Time().Add(time.Hour) // 1h into a 7d window

		sched := &publishScheduler{
			svc:      svc,
			store:    store,
			interval: time.Minute,
			now:      func() time.Time { return beforeDeadline },
		}

		Expect(sched.sweep(ctx)).To(Succeed())

		// Must remain COLLECTING.
		got, err := store.GetPublishedSceneHeader(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(StatusCollecting),
			"E5: attempt not past vote_window MUST remain COLLECTING")
	})

	// -------------------------------------------------------------------------
	// 4. No expired attempts — sweep is a clean no-op
	// -------------------------------------------------------------------------
	It("completes without error when no expired attempts exist", func() {
		store := newTestStore()
		svc := newTestService(GinkgoT(), store)
		svc.gameID = "main"

		sched := &publishScheduler{
			svc:      svc,
			store:    store,
			interval: time.Minute,
			now:      time.Now,
		}

		Expect(sched.sweep(ctx)).To(Succeed(), "E5: sweep with no attempts must be a clean no-op")
	})

	// -------------------------------------------------------------------------
	// 5. Per-attempt error WARN-logs and sweep continues to the next attempt
	//
	// Strategy: one COLLECTING attempt (will succeed → ATTEMPT_FAILED/TIMEOUT)
	// and one COOLOFF attempt with NO decryptor (runSnapshot errors, stays COOLOFF).
	// Both phases run in a single sweep. The COOLOFF error is WARN-logged; the
	// COLLECTING transition still succeeds. The sweep returns nil.
	// -------------------------------------------------------------------------
	It("processes remaining attempts when one phase fails (per-attempt WARN, sweep does not abort)", func() {
		const (
			sceneIDCol  = "01E5SCHED_CONTCOL000000A"
			sceneIDCool = "01E5SCHED_CONTCOOL00000A"
			ownerID     = "01E5SCHED_CONTOWNER0000A"
		)
		store := newTestStore()
		// Service with NO decryptor → runSnapshot errors for COOLOFF attempt.
		svc := newTestService(GinkgoT(), store)
		svc.gameID = "main"
		// Deliberately do NOT call svc.SetSnapshotDecryptor(...)

		// Create COLLECTING attempt (short vote_window, expired).
		rowCol := &SceneRow{
			ID: sceneIDCol, Title: "Col Scene", OwnerID: ownerID,
			State: string(SceneStateEnded), PoseOrder: string(PoseOrderModeFree),
			Visibility: string(SceneVisibilityOpen), ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, rowCol)).NotTo(HaveOccurred())
		pubCol, err := store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
			SceneID: sceneIDCol, AttemptNumber: 1, InitiatedBy: ownerID,
			VoteWindow: time.Second, CoolOffWindow: time.Minute, MaxAttempts: 3,
		})
		Expect(err).NotTo(HaveOccurred())

		// Create COOLOFF attempt (short cooloff_window, expired).
		rowCool := &SceneRow{
			ID: sceneIDCool, Title: "Cool Scene", OwnerID: ownerID,
			State: string(SceneStateEnded), PoseOrder: string(PoseOrderModeFree),
			Visibility: string(SceneVisibilityOpen), ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, rowCool)).NotTo(HaveOccurred())
		pubCool, err := store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
			SceneID: sceneIDCool, AttemptNumber: 1, InitiatedBy: ownerID,
			VoteWindow: time.Minute, CoolOffWindow: time.Second, MaxAttempts: 3,
		})
		Expect(err).NotTo(HaveOccurred())
		// Transition COOLOFF attempt to COOLOFF status.
		voters, err := store.ListPublishVoters(ctx, pubCool.ID)
		Expect(err).NotTo(HaveOccurred())
		for _, v := range voters {
			_, err := store.CastVote(ctx, pubCool.ID, v.CharacterID, true)
			Expect(err).NotTo(HaveOccurred())
		}
		now := time.Now()
		Expect(store.TransitionStatus(ctx, pubCool.ID, TransitionInput{
			To: StatusCoolOff, SetCoolOffAt: &now,
		})).To(Succeed())

		// Set now to be past both windows.
		pastBoth := time.Now().Add(time.Hour)

		sched := &publishScheduler{
			svc:      svc,
			store:    store,
			interval: time.Minute,
			now:      func() time.Time { return pastBoth },
		}

		// sweep must return nil even though runSnapshot errored for the COOLOFF attempt.
		Expect(sched.sweep(ctx)).To(Succeed(),
			"E5: per-attempt runSnapshot error MUST NOT abort the sweep")

		// COLLECTING attempt must be ATTEMPT_FAILED/TIMEOUT (phase 1 succeeded).
		gotCol, err := store.GetPublishedSceneHeader(ctx, pubCol.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(gotCol.Status).To(Equal(StatusAttemptFailed),
			"E5: COLLECTING attempt must be transitioned despite COOLOFF phase error")
		Expect(*gotCol.FailureReason).To(Equal(FailureTimeout))

		// COOLOFF attempt must still be in COOLOFF (runSnapshot errored, no transition).
		gotCool, err := store.GetPublishedSceneHeader(ctx, pubCool.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(gotCool.Status).To(Equal(StatusCoolOff),
			"E5: COOLOFF attempt stays COOLOFF when runSnapshot errors (nil decryptor)")
	})
})
