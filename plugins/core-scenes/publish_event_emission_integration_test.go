// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// D4: Event emission integration tests — holomush-5rh.20.30.
//
// These specs verify that each Phase-6 publish lifecycle transition emits its
// event on the scene IC stream with the correct payload fields, exercised
// against a REAL eventbus via the integrationtest harness
// (WithInTreePlugins + WithPluginCrypto).
//
// # Read mechanism: QueryPluginAuditRows
//
// Every publish event lands on subject:
//
//	events.<game_id>.scene.<sceneID>.ic
//
// where <sceneID> is the bare scene ULID (holomush-y5inx — scenes mint bare
// ULIDs, no "scene-" prefix).
//
// These specs verify event EMISSION and PAYLOAD FIELDS directly via
// QueryPluginAuditRows (ts.QueryPluginAuditRows(ctx, "core-scenes", subject)),
// which reads plugin_core_scenes.scene_log by raw subject string — the durable
// audit row, independent of the CoreServer.QueryStreamHistory read/floor path
// (that path is covered end-to-end by the real-scene history-readable and
// publish-history-scope-floor specs in test/integration/scenes/). All six
// Phase 6 publish events are sensitivity:never (codec=identity), so their
// payloads land cleartext in scene_log and are directly decodable with
// protojson.Unmarshal.
//
// The audit rows arrive asynchronously (JetStream → PluginConsumerManager →
// AuditEvent RPC → scene_log INSERT), so each assertion wraps
// QueryPluginAuditRows in Eventually to wait for the rows to appear.
//
// # scene_publish_started
//
// Emitted synchronously by StartScenePublish before returning. Row appears in
// scene_log after the JetStream delivery completes.
//
// # scene_publish_vote_cast
//
// Emitted once per CastPublishSceneVote call. The test drives:
//   - alice casts yes (IsChange=false) → first vote_cast row
//   - alice changes to no (IsChange=true) → second vote_cast row
//   - alice changes back to yes (IsChange=true) → third vote_cast row
//
// # scene_publish_cooloff_started
//
// Emitted synchronously when the all-yes resolution transitions the attempt
// to COOLOFF (bob casts yes → all-yes → COOLOFF). Row appears after JetStream.
//
// # scene_publish_resolved
//
// Emitted asynchronously by the scheduler after the cool-off window elapses.
// The scheduler interval is 20ms; the cooloff_window is 1ms.
//
// # scene_publish_withdrawn
//
// Emitted synchronously by WithdrawScenePublish before applyTrigger.
//
// # scene_publish_vote_attempts_extended
//
// Emitted synchronously by ExtendScenePublishVoteAttempts.
//
// Ginkgo dot-import collision note (package main + Ginkgo/Gomega dot-imports):
// use domain-qualified names (PublishedSceneEntry, not bare Entry; BeforeEach,
// not conflicting method names).
package main

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// findAuditRowByType returns the first row whose Type equals eventType, or nil.
func findAuditRowByType(rows []integrationtest.PluginAuditRow, eventType string) *integrationtest.PluginAuditRow {
	GinkgoHelper()
	for i := range rows {
		if rows[i].Type == eventType {
			return &rows[i]
		}
	}
	return nil
}

// findAllAuditRowsByType returns all rows whose Type equals eventType.
func findAllAuditRowsByType(rows []integrationtest.PluginAuditRow, eventType string) []integrationtest.PluginAuditRow {
	GinkgoHelper()
	var out []integrationtest.PluginAuditRow
	for _, r := range rows {
		if r.Type == eventType {
			out = append(out, r)
		}
	}
	return out
}

// pollAuditRows polls QueryPluginAuditRows until at least minCount rows of the
// given type appear on the subject, then returns all rows for that subject.
// Wraps the poll in an outer Eventually; fails the spec if the count is not
// reached within the timeout.
func pollAuditRows(
	ctx context.Context,
	ts *integrationtest.Server,
	plugin, subject, eventType string,
	minCount int,
) []integrationtest.PluginAuditRow {
	GinkgoHelper()
	var rows []integrationtest.PluginAuditRow
	Eventually(func(g Gomega) {
		rows = ts.QueryPluginAuditRows(ctx, plugin, subject)
		matched := findAllAuditRowsByType(rows, eventType)
		g.Expect(matched).To(HaveLen(minCount),
			"waiting for %d row(s) of type %q on subject %q; got %d so far",
			minCount, eventType, subject, len(matched))
	}).WithTimeout(10 * time.Second).WithPolling(25 * time.Millisecond).Should(Succeed())
	return rows
}

// pollAtLeastAuditRows polls until at LEAST minCount rows of the given type
// are present, then returns all rows for the subject. Use when the exact
// count may exceed minCount (e.g. multiple vote_cast rows accumulate).
func pollAtLeastAuditRows(
	ctx context.Context,
	ts *integrationtest.Server,
	plugin, subject, eventType string,
	minCount int,
) []integrationtest.PluginAuditRow {
	GinkgoHelper()
	var rows []integrationtest.PluginAuditRow
	Eventually(func(g Gomega) {
		rows = ts.QueryPluginAuditRows(ctx, plugin, subject)
		matched := findAllAuditRowsByType(rows, eventType)
		g.Expect(len(matched)).To(BeNumerically(">=", minCount),
			"waiting for at least %d row(s) of type %q on subject %q; got %d so far",
			minCount, eventType, subject, len(matched))
	}).WithTimeout(10 * time.Second).WithPolling(25 * time.Millisecond).Should(Succeed())
	return rows
}

// D4: happy-path publish lifecycle — started, vote_cast (IsChange), cooloff_started, resolved.
var _ = Describe("D4: Phase-6 publish lifecycle events", func() {
	var (
		ts    *integrationtest.Server
		ctx   context.Context
		alice *integrationtest.Session
		bob   *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 120*time.Second)
		DeferCleanup(cancel)
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
			integrationtest.WithPluginConfigOverrides(map[string]map[string]string{
				"core-scenes": {"cooloff_window": "1ms", "scheduler_interval": "20ms"},
			}),
		)
		alice = ts.ConnectAuthed(ctx, "Alice")
		bob = ts.ConnectAuthed(ctx, "Bob")
	})

	AfterEach(func() {
		if bob != nil {
			bob.Logout(ctx)
		}
		if alice != nil {
			alice.Logout(ctx)
		}
		ts.Stop()
	})

	// -------------------------------------------------------------------------
	// Happy path: started → vote_cast (first=no IsChange, second=IsChange) →
	// all-yes → cooloff_started → PUBLISHED resolved
	// -------------------------------------------------------------------------
	It("emits started, vote_cast (IsChange=false then IsChange=true), cooloff_started, and resolved(PUBLISHED) with correct payload fields", func() {
		loc := ts.NewLocation(ctx)

		// CreateScene returns the bare ULID — the stored id (holomush-y5inx).
		sceneID := alice.CreateScene(ctx, loc)
		sceneRef := sceneID.String()

		// Subject all six publish events land on.
		sceneICSubject := "events." + ts.GameID() + ".scene." + sceneRef + ".ic"

		// Bob must JOIN (owner=alice is already a DB participant; Bob needs the
		// DB membership row so the vote roster seeds him — INV-SCENE-28). Post
		// holomush-y5inx the scene id is a bare ULID, so JoinFocus parses it and
		// the focus subscription succeeds cleanly; the DB membership row (seeded
		// from scene_participants WHERE role IN ('owner','member')) is what the
		// CastPublishSceneVote roster path requires.
		Expect(alice.SendCommand(ctx, "scene invite "+sceneRef+" "+bob.CharacterID.String())).To(Succeed())
		// Bob joins (mirrors the sibling publish_e2e_test.go happy-path): the DB
		// membership row seeds the vote roster (IsParticipant), which is all the
		// CastPublishSceneVote RPC path requires.
		Expect(bob.SendCommand(ctx, "scene join "+sceneRef)).To(Succeed())

		// End the scene (required before publish attempts can start).
		Expect(alice.SendCommand(ctx, "scene end "+sceneRef)).To(Succeed())

		// ── scene_publish_started ────────────────────────────────────────────
		startResp, err := ts.SceneServiceClient().StartScenePublish(ctx,
			&scenev1.StartScenePublishRequest{
				CallerCharacterId: alice.CharacterID.String(),
				SceneId:           sceneRef,
			})
		Expect(err).NotTo(HaveOccurred(), "StartScenePublish must succeed")
		attemptID := startResp.GetPublishedSceneId()
		Expect(attemptID).NotTo(BeEmpty(), "D4/started: AttemptId must be non-empty")

		// Wait for the scene_publish_started row to appear in scene_log.
		// Payload fields: AttemptId, AttemptNumber, InitiatedBy,
		//   VoteWindowSeconds, CooloffWindowSeconds, RosterCharacterIds.
		rows := pollAuditRows(ctx, ts, "core-scenes", sceneICSubject, "core-scenes:scene_publish_started", 1)
		startRow := findAuditRowByType(rows, "core-scenes:scene_publish_started")
		Expect(startRow).NotTo(BeNil(), "D4/started: scene_log must have a scene_publish_started row")

		var startedEv scenev1.ScenePublishStartedEvent
		Expect(protojson.Unmarshal(startRow.Payload, &startedEv)).To(Succeed(),
			"D4/started: payload must decode as ScenePublishStartedEvent")
		Expect(startedEv.GetAttemptId()).To(Equal(attemptID),
			"D4/started: AttemptId must match the attempt returned by StartScenePublish")
		Expect(startedEv.GetAttemptNumber()).To(BeEquivalentTo(1),
			"D4/started: first attempt must have AttemptNumber=1")
		Expect(startedEv.GetInitiatedBy()).To(Equal(alice.CharacterID.String()),
			"D4/started: InitiatedBy must be alice's character ID")
		Expect(startedEv.GetRosterCharacterIds()).NotTo(BeEmpty(),
			"D4/started: RosterCharacterIds must be non-empty (alice + bob seeded)")
		Expect(startedEv.GetRosterCharacterIds()).To(ContainElement(alice.CharacterID.String()),
			"D4/started: RosterCharacterIds must contain alice")
		Expect(startedEv.GetRosterCharacterIds()).To(ContainElement(bob.CharacterID.String()),
			"D4/started: RosterCharacterIds must contain bob")
		// VoteWindow is a multi-day default → positive whole seconds. (The
		// CooloffWindowSeconds field truncates to 0 here because the test
		// overrides cooloff_window to 1ms, so it is not asserted.)
		Expect(startedEv.GetVoteWindowSeconds()).To(BeNumerically(">", int64(0)),
			"D4/started: VoteWindowSeconds must be the positive configured vote window")

		// ── scene_publish_vote_cast (alice, yes, IsChange=false) ─────────────
		_, err = ts.SceneServiceClient().CastPublishSceneVote(ctx,
			&scenev1.CastPublishSceneVoteRequest{
				CallerCharacterId: alice.CharacterID.String(),
				PublishedSceneId:  attemptID,
				Vote:              true, // yes
			})
		Expect(err).NotTo(HaveOccurred(), "CastPublishSceneVote (alice, yes) must succeed")

		// Wait for the first vote_cast row. IsChange=false for a first cast.
		rows = pollAtLeastAuditRows(ctx, ts, "core-scenes", sceneICSubject, "core-scenes:scene_publish_vote_cast", 1)
		voteCastRows := findAllAuditRowsByType(rows, "core-scenes:scene_publish_vote_cast")

		// Find the first alice-yes row (IsChange=false).
		var firstAliceYes *scenev1.ScenePublishVoteCastEvent
		for _, r := range voteCastRows {
			var ev scenev1.ScenePublishVoteCastEvent
			Expect(protojson.Unmarshal(r.Payload, &ev)).To(Succeed(),
				"D4/vote_cast: payload must decode as ScenePublishVoteCastEvent")
			if ev.GetCharacterId() == alice.CharacterID.String() && ev.GetVote() && !ev.GetIsChange() {
				firstAliceYes = &ev
				break
			}
		}
		Expect(firstAliceYes).NotTo(BeNil(),
			"D4/vote_cast: must find a vote_cast row for alice, vote=yes, IsChange=false")
		Expect(firstAliceYes.GetAttemptId()).To(Equal(attemptID),
			"D4/vote_cast: AttemptId must match the active attempt")

		// ── scene_publish_vote_cast (alice, no, IsChange=true) ───────────────
		_, err = ts.SceneServiceClient().CastPublishSceneVote(ctx,
			&scenev1.CastPublishSceneVoteRequest{
				CallerCharacterId: alice.CharacterID.String(),
				PublishedSceneId:  attemptID,
				Vote:              false, // no — vote change
			})
		Expect(err).NotTo(HaveOccurred(), "CastPublishSceneVote (alice, no) must succeed")

		// Wait for a second vote_cast row where alice changes to no.
		rows = pollAtLeastAuditRows(ctx, ts, "core-scenes", sceneICSubject, "core-scenes:scene_publish_vote_cast", 2)
		voteCastRows = findAllAuditRowsByType(rows, "core-scenes:scene_publish_vote_cast")

		var aliceChangeToNo *scenev1.ScenePublishVoteCastEvent
		for _, r := range voteCastRows {
			var ev scenev1.ScenePublishVoteCastEvent
			Expect(protojson.Unmarshal(r.Payload, &ev)).To(Succeed())
			if ev.GetCharacterId() == alice.CharacterID.String() && !ev.GetVote() && ev.GetIsChange() {
				aliceChangeToNo = &ev
				break
			}
		}
		Expect(aliceChangeToNo).NotTo(BeNil(),
			"D4/vote_cast: must find a vote_cast row for alice, vote=no, IsChange=true")
		Expect(aliceChangeToNo.GetAttemptId()).To(Equal(attemptID),
			"D4/vote_cast: IsChange=true row AttemptId must match the active attempt")

		// Alice changes back to yes (another IsChange=true row).
		_, err = ts.SceneServiceClient().CastPublishSceneVote(ctx,
			&scenev1.CastPublishSceneVoteRequest{
				CallerCharacterId: alice.CharacterID.String(),
				PublishedSceneId:  attemptID,
				Vote:              true, // back to yes
			})
		Expect(err).NotTo(HaveOccurred(), "CastPublishSceneVote (alice, yes again) must succeed")

		// ── scene_publish_cooloff_started (bob votes yes → all-yes → COOLOFF) ─
		// Bob casts yes — his first vote (IsChange=false). With all roster members
		// having voted yes, the resolution check transitions to COOLOFF and
		// emits scene_publish_cooloff_started synchronously before completing.
		_, err = ts.SceneServiceClient().CastPublishSceneVote(ctx,
			&scenev1.CastPublishSceneVoteRequest{
				CallerCharacterId: bob.CharacterID.String(),
				PublishedSceneId:  attemptID,
				Vote:              true, // yes — triggers all-yes → COOLOFF
			})
		Expect(err).NotTo(HaveOccurred(), "CastPublishSceneVote (bob, yes) must succeed")

		// Wait for the bob-yes vote_cast row AND the cooloff_started row.
		// Bob's vote is his first, so IsChange=false.
		rows = pollAtLeastAuditRows(ctx, ts, "core-scenes", sceneICSubject, "core-scenes:scene_publish_vote_cast", 4)
		voteCastRows = findAllAuditRowsByType(rows, "core-scenes:scene_publish_vote_cast")
		var bobFirstYes *scenev1.ScenePublishVoteCastEvent
		for _, r := range voteCastRows {
			var ev scenev1.ScenePublishVoteCastEvent
			Expect(protojson.Unmarshal(r.Payload, &ev)).To(Succeed())
			if ev.GetCharacterId() == bob.CharacterID.String() && ev.GetVote() && !ev.GetIsChange() {
				bobFirstYes = &ev
				break
			}
		}
		Expect(bobFirstYes).NotTo(BeNil(),
			"D4/vote_cast: must find a vote_cast row for bob, vote=yes, IsChange=false")
		Expect(bobFirstYes.GetAttemptId()).To(Equal(attemptID))

		// Now wait for scene_publish_cooloff_started.
		rows = pollAuditRows(ctx, ts, "core-scenes", sceneICSubject, "core-scenes:scene_publish_cooloff_started", 1)
		cooloffRow := findAuditRowByType(rows, "core-scenes:scene_publish_cooloff_started")
		Expect(cooloffRow).NotTo(BeNil(), "D4/cooloff_started: scene_log must have a scene_publish_cooloff_started row")

		var cooloffEv scenev1.ScenePublishCoolOffStartedEvent
		Expect(protojson.Unmarshal(cooloffRow.Payload, &cooloffEv)).To(Succeed(),
			"D4/cooloff_started: payload must decode as ScenePublishCoolOffStartedEvent")
		Expect(cooloffEv.GetAttemptId()).To(Equal(attemptID),
			"D4/cooloff_started: AttemptId must match the active attempt")
		Expect(cooloffEv.GetCooloffEndsAtUnixNs()).To(BeNumerically(">", int64(0)),
			"D4/cooloff_started: CooloffEndsAtUnixNs must be a positive epoch-ns value")

		// ── scene_publish_resolved (PUBLISHED) ───────────────────────────────
		// The scheduler sweeps every ~20ms with a ~1ms cooloff_window. Poll for
		// the scene_publish_resolved row to appear in scene_log (async after
		// the scheduler fires applyTrigger → emitResolved).
		rows = pollAuditRows(ctx, ts, "core-scenes", sceneICSubject, "core-scenes:scene_publish_resolved", 1)
		resolvedRow := findAuditRowByType(rows, "core-scenes:scene_publish_resolved")
		Expect(resolvedRow).NotTo(BeNil(), "D4/resolved: scene_log must have a scene_publish_resolved row")

		var resolvedEv scenev1.ScenePublishResolvedEvent
		Expect(protojson.Unmarshal(resolvedRow.Payload, &resolvedEv)).To(Succeed(),
			"D4/resolved: payload must decode as ScenePublishResolvedEvent")
		Expect(resolvedEv.GetAttemptId()).To(Equal(attemptID),
			"D4/resolved: AttemptId must match the active attempt")
		Expect(resolvedEv.GetOutcome()).To(Equal("PUBLISHED"),
			"D4/resolved: Outcome must be PUBLISHED (all-yes → cooloff → resolved)")
		Expect(resolvedEv.GetFailureReason()).To(BeEmpty(),
			"D4/resolved: FailureReason must be empty for a PUBLISHED outcome")
		// Tally: alice + bob both voted yes → TallyYes=2, No=0, Pending=0.
		Expect(resolvedEv.GetTallyYes()).To(BeEquivalentTo(2),
			"D4/resolved: TallyYes must equal 2 (alice + bob voted yes)")
		Expect(resolvedEv.GetTallyNo()).To(BeEquivalentTo(0),
			"D4/resolved: TallyNo must equal 0")
		Expect(resolvedEv.GetTallyPending()).To(BeEquivalentTo(0),
			"D4/resolved: TallyPending must equal 0")
	})

	// -------------------------------------------------------------------------
	// Withdrawn: scene_publish_withdrawn → scene_log row with correct payload
	// -------------------------------------------------------------------------
	It("emits scene_publish_withdrawn with correct AttemptId and WithdrawnBy fields", func() {
		loc := ts.NewLocation(ctx)
		sceneID := alice.CreateScene(ctx, loc)
		sceneRef := sceneID.String()
		sceneICSubject := "events." + ts.GameID() + ".scene." + sceneRef + ".ic"

		// End the scene before publishing.
		Expect(alice.SendCommand(ctx, "scene end "+sceneRef)).To(Succeed())

		// Start a publish attempt (alice is the sole roster member as owner).
		startResp, err := ts.SceneServiceClient().StartScenePublish(ctx,
			&scenev1.StartScenePublishRequest{
				CallerCharacterId: alice.CharacterID.String(),
				SceneId:           sceneRef,
			})
		Expect(err).NotTo(HaveOccurred(), "StartScenePublish must succeed")
		attemptID := startResp.GetPublishedSceneId()
		Expect(attemptID).NotTo(BeEmpty())

		// Wait for the started row so we know the attempt is live.
		pollAuditRows(ctx, ts, "core-scenes", sceneICSubject, "core-scenes:scene_publish_started", 1)

		// ── scene_publish_withdrawn ──────────────────────────────────────────
		// WithdrawScenePublish emits scene_publish_withdrawn synchronously
		// (publish_service.go:714) before applyTrigger transitions the attempt.
		_, err = ts.SceneServiceClient().WithdrawScenePublish(ctx,
			&scenev1.WithdrawScenePublishRequest{
				CallerCharacterId: alice.CharacterID.String(),
				PublishedSceneId:  attemptID,
			})
		Expect(err).NotTo(HaveOccurred(), "WithdrawScenePublish must succeed")

		// Wait for the scene_publish_withdrawn row.
		rows := pollAuditRows(ctx, ts, "core-scenes", sceneICSubject, "core-scenes:scene_publish_withdrawn", 1)
		withdrawnRow := findAuditRowByType(rows, "core-scenes:scene_publish_withdrawn")
		Expect(withdrawnRow).NotTo(BeNil(), "D4/withdrawn: scene_log must have a scene_publish_withdrawn row")

		var withdrawnEv scenev1.ScenePublishWithdrawnEvent
		Expect(protojson.Unmarshal(withdrawnRow.Payload, &withdrawnEv)).To(Succeed(),
			"D4/withdrawn: payload must decode as ScenePublishWithdrawnEvent")
		Expect(withdrawnEv.GetAttemptId()).To(Equal(attemptID),
			"D4/withdrawn: AttemptId must match the withdrawn attempt")
		Expect(withdrawnEv.GetWithdrawnBy()).To(Equal(alice.CharacterID.String()),
			"D4/withdrawn: WithdrawnBy must be alice's character ID (the owner who withdrew)")

		// WithdrawScenePublish also fires applyTrigger which emits
		// scene_publish_resolved (outcome=ATTEMPT_FAILED, reason=WITHDRAWN).
		rows = pollAuditRows(ctx, ts, "core-scenes", sceneICSubject, "core-scenes:scene_publish_resolved", 1)
		resolvedRow := findAuditRowByType(rows, "core-scenes:scene_publish_resolved")
		Expect(resolvedRow).NotTo(BeNil(), "D4/withdrawn: scene_log must also have a scene_publish_resolved row")

		var resolvedEv scenev1.ScenePublishResolvedEvent
		Expect(protojson.Unmarshal(resolvedRow.Payload, &resolvedEv)).To(Succeed(),
			"D4/withdrawn/resolved: payload must decode as ScenePublishResolvedEvent")
		Expect(resolvedEv.GetAttemptId()).To(Equal(attemptID),
			"D4/withdrawn/resolved: AttemptId must match the withdrawn attempt")
		Expect(resolvedEv.GetOutcome()).To(Equal("ATTEMPT_FAILED"),
			"D4/withdrawn/resolved: Outcome must be ATTEMPT_FAILED for a withdrawal")
		Expect(resolvedEv.GetFailureReason()).To(Equal(string(FailureWithdrawn)),
			"D4/withdrawn/resolved: FailureReason must be WITHDRAWN")
	})

	// -------------------------------------------------------------------------
	// Attempts extended: scene_publish_vote_attempts_extended → scene_log row
	// -------------------------------------------------------------------------
	It("emits scene_publish_vote_attempts_extended with correct SceneId, Additional, and NewMax fields", func() {
		loc := ts.NewLocation(ctx)
		sceneID := alice.CreateScene(ctx, loc)
		sceneRef := sceneID.String()
		sceneICSubject := "events." + ts.GameID() + ".scene." + sceneRef + ".ic"

		Expect(alice.SendCommand(ctx, "scene end "+sceneRef)).To(Succeed())

		// Start a publish attempt to ensure the scene has an active budget record.
		startResp, err := ts.SceneServiceClient().StartScenePublish(ctx,
			&scenev1.StartScenePublishRequest{
				CallerCharacterId: alice.CharacterID.String(),
				SceneId:           sceneRef,
			})
		Expect(err).NotTo(HaveOccurred(), "StartScenePublish must succeed")
		Expect(startResp.GetPublishedSceneId()).NotTo(BeEmpty())

		// Wait for started row so the scene row exists in published_scenes.
		pollAuditRows(ctx, ts, "core-scenes", sceneICSubject, "core-scenes:scene_publish_started", 1)

		// ── scene_publish_vote_attempts_extended ──────────────────────────────
		// ExtendScenePublishVoteAttempts emits scene_publish_vote_attempts_extended
		// synchronously (publish_service.go:506) after the budget bump.
		// The default max_publish_attempts is 3. Extending by 2 → new_max = 5.
		// The allow-all ABAC engine passes the admin gate in handleVoteExtend.
		const additional = 2
		extResp, err := ts.SceneServiceClient().ExtendScenePublishVoteAttempts(ctx,
			&scenev1.ExtendScenePublishVoteAttemptsRequest{
				CallerCharacterId: alice.CharacterID.String(),
				SceneId:           sceneRef,
				Additional:        additional,
			})
		Expect(err).NotTo(HaveOccurred(), "ExtendScenePublishVoteAttempts must succeed")
		expectedNewMax := extResp.GetNewMax() // use the RPC-returned value as ground truth for the event assertion

		// Wait for the scene_publish_vote_attempts_extended row.
		rows := pollAuditRows(ctx, ts, "core-scenes", sceneICSubject, "core-scenes:scene_publish_vote_attempts_extended", 1)
		extRow := findAuditRowByType(rows, "core-scenes:scene_publish_vote_attempts_extended")
		Expect(extRow).NotTo(BeNil(), "D4/extended: scene_log must have a scene_publish_vote_attempts_extended row")

		var extEv scenev1.ScenePublishVoteAttemptsExtendedEvent
		Expect(protojson.Unmarshal(extRow.Payload, &extEv)).To(Succeed(),
			"D4/extended: payload must decode as ScenePublishVoteAttemptsExtendedEvent")
		Expect(extEv.GetSceneId()).To(Equal(sceneRef),
			"D4/extended: SceneId must be the bare stored scene ULID (holomush-y5inx)")
		Expect(extEv.GetAdminId()).To(Equal(alice.CharacterID.String()),
			"D4/extended: AdminId must be alice's character ID")
		Expect(extEv.GetAdditional()).To(BeEquivalentTo(additional),
			"D4/extended: Additional must equal the requested extension count")
		Expect(extEv.GetNewMax()).To(BeEquivalentTo(expectedNewMax),
			"D4/extended: NewMax must match the value returned by the RPC")
		Expect(extEv.GetNewMax()).To(BeNumerically(">", int32(additional)),
			"D4/extended: NewMax must be greater than the added count (budget was already > 0)")
	})
})

// D4: scene IC stream subject form — bare-ULID verification (holomush-y5inx).
//
// Post holomush-y5inx, scenes mint bare ULIDs, so publish events land on
// "events.<game>.scene.<bareULID>.ic" — part[3] is the bare ULID with NO
// "scene-" prefix. That bare form is exactly what lets streamToFocusKey's
// ulid.Parse(parts[3]) succeed, so the stream is readable AND temporally
// floored via CoreServer.QueryStreamHistory (covered end-to-end by the
// real-scene history-readable + publish-history-scope-floor specs in
// test/integration/scenes/). This guards against reintroducing a type-tag
// prefix into the subject builder.
//
//	dotStyleSceneSubjectIC(gameID, sceneID) = "events."+gameID+".scene."+sceneID+".ic"
var _ = Describe("D4: scene IC stream subject form for publish events", func() {
	It("subject returned by dotStyleSceneSubjectIC embeds a bare scene ULID in part[3]", func() {
		const gameID = "main"
		// A valid 26-char Crockford-base32 ULID (bare, no prefix).
		const bareSceneID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
		subject := dotStyleSceneSubjectIC(gameID, bareSceneID)
		Expect(subject).To(Equal("events.main.scene.01ARZ3NDEKTSV4RRFFQ69G5FAV.ic"),
			"D4: subject must embed the bare scene ULID in the scene-id segment")
		parts := strings.Split(subject, ".")
		Expect(parts).To(HaveLen(5),
			"D4: subject must have exactly 5 dot-separated segments")
		Expect(parts[3]).NotTo(HavePrefix("scene-"),
			"INV-Y5INX: part[3] MUST be a bare ULID, not 'scene-'-prefixed")
		Expect(parts[3]).To(Equal(bareSceneID),
			"INV-Y5INX: part[3] MUST be exactly the bare scene ULID so "+
				"streamToFocusKey/streamScopeFloor can read + floor via QueryStreamHistory")
	})
})
