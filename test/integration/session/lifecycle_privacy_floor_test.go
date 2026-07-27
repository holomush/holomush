// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package session_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// Session-lifecycle matrix — the last two transitions: a location change while
// the session stays attached, and a transport-level blip with no logout.
//
// Each spec carries a `// matrix-row: <id>` marker naming the cell of
// test/session-matrix.yaml it satisfies. A marker with no row, or a row with no
// marker, fails the bijection meta-test.
//
// # What these transitions have in common
//
// Both are decided by ONE column: sessions.location_arrived_at, the per-session
// floor every location history query is filtered against
// (internal/grpc/scope_floor.go:34-38). A location change must ADVANCE it, so the
// arriving character does not read what happened before they got there; a blip
// must LEAVE IT ALONE, so a returning player does not lose the conversation they
// dropped out of. Getting either wrong leaks history or hides it.
//
// So no spec below asserts the transition by its bookkeeping. Each one plants
// events at chosen instants either side of the floor and asserts, by event
// IDENTIFIER, which of them a real history read returns. A count would pass
// under an off-by-one window and under substitution by an unrelated event — and
// this suite shares one harness and one start location, so unrelated events
// genuinely do land in the same stream.
//
// # WHAT THE MOVE SPECS DO AND DO NOT COVER — read before citing them
//
// The move specs drive Session.MoveTo, which writes sessions.location_id and
// sessions.location_arrived_at directly and, by its own doc comment
// (internal/testsupport/integrationtest/session.go:312-339), deliberately does
// NOT invoke world.Service.MoveCharacter and never touches
// characters.location_id. So these specs prove that THE PRIVACY FLOOR IS APPLIED
// CORRECTLY TO A SESSION WHOSE LOCATION CHANGED. They do NOT prove that the
// production movement pipeline advances that floor, because that pipeline never
// runs here.
//
// That is not a shortcut taken for convenience — the production pipeline is not
// reachable from this harness at all, and is currently unreachable from a player
// too:
//
//   - world.Service.MoveCharacter propagates the new location to the session
//     store through a MovementHook (internal/world/movement_hook.go:13-33). The
//     ONLY implementation, sessionStoreMovementHook, is wired exclusively in
//     cmd/holomush/sub_grpc.go:331. The integration harness builds its world
//     service without it (internal/testsupport/integrationtest/plugins.go:267),
//     so MoveCharacter there runs against NoopMovementHook and leaves
//     location_arrived_at untouched. Driving the pipeline would prove the
//     OPPOSITE of what this row asserts.
//   - Issue #4788 records that MoveCharacter has ZERO production callers and is
//     not integration-tested, that no plugin registers a movement command, and
//     that its acceptance includes "a command→move integration test". The
//     production movement-lifecycle claim is therefore uncovered and tracked
//     THERE, not implicitly satisfied here. session.Store.UpdateLocationOnMove —
//     the tail of that pipeline, and the statement that actually writes the new
//     floor — has no test reference anywhere in the tree.
//
// The registry rows say the same thing in their notes. The hard-gate arm of a
// move (a query against the PRIOR location is denied) is covered separately by
// test/integration/privacy/privacy_test.go, which runs a deny-all engine so the
// gate can fire; this suite's harness runs the permissive default, under which
// staffOverride bypasses the gate for every session.
//
// # TELNET SCOPE
//
// The telnet cells assert SESSION STATE for a session whose connection row
// carries client_type='telnet'. No telnet gateway is in the loop and
// internal/telnet is not exercised. Nothing below is a claim about telnet
// PROTOCOL behaviour.
//
// # No sleeping
//
// Every instant is chosen through Session.EmitDirectEventAt. Nothing here waits
// for the wall clock to separate two events.

// # The two named privacy-floor tests
//
// The two containers at the foot of this file are the specifications named
// verbatim in docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md
// §8 — TestPrivacy_ReattachWithinTTLPreservesFloor and
// TestPrivacy_TTLExpiryEndsSessionFreshFloor. They are the two halves of one
// question: a session that comes BACK keeps its floor, and a session that
// EXPIRED does not hand its floor to the one that replaces it.
//
// They carry NO matrix-row marker. The registry has no row for them — they are
// acceptance items of the source work item, not cells of the 12x4 matrix — so a
// marker would fail the bijection meta-test.
//
// WHY THEY ARE GINKGO CONTAINERS RATHER THAN `func TestPrivacy_...(t *testing.T)`.
// The names read as Go test functions, but this package is a Ginkgo suite whose
// Gomega fail handler is registered inside RunSpecs; a plain Test function in the
// same package would run outside that registration and, if it ran first, would
// panic instead of failing. Carrying the identifier verbatim in the container
// description keeps the repo's Ginkgo requirement for full-stack integration
// tests, keeps the shared suite harness, and leaves the token greppable — which
// is what any consumer looking these up by name needs. Nothing in the tree binds
// these names today: the identifiers appear only in the spec document above
// (verified: they occur in no file under test/meta/).

// historyCeiling returns a NotAfter bound far enough in the future that it can
// never exclude anything these specs publish. It is passed to every bounded read
// deliberately: with a non-excluding ceiling, the server-side scope floor is the
// ONLY thing that can remove an event from a result, so an absence assertion is
// attributable to the floor rather than to the window.
func historyCeiling() int64 {
	return time.Now().Add(time.Hour).UnixMilli()
}

// plantEvent publishes one pose into stream at exactly `at` and returns its
// identifier, so callers assert on a specific event instead of a frame count.
func plantEvent(
	ctx context.Context,
	sess *integrationtest.Session,
	stream string,
	at time.Time,
) string {
	GinkgoHelper()
	id, err := sess.EmitDirectEventAt(ctx, stream, "core-communication:pose",
		[]byte(`{"character_name":"`+sess.CharacterName+`","action":"speaks at a chosen instant."}`), at)
	Expect(err).NotTo(HaveOccurred(), "the timestamped emit at %s MUST publish", at)
	Expect(id).NotTo(BeEmpty(),
		"the timestamped emit MUST return the published event's identifier, or the "+
			"assertions built on it would be vacuous")
	return id
}

// logoutSharedPlayerSession tears down two game sessions that belong to ONE
// player session.
//
// The suite's usual per-session cleanup (DeferCleanup(sess.Logout)) cannot be
// registered twice here. Session.Logout calls the production Logout RPC with the
// player-session bearer token, which invalidates that token and cascades away
// EVERY game session it owns — so the second cleanup would find its session
// already gone and fail in teardown, because auth.Service.Logout resolves the
// token before deleting it and returns SESSION_NOT_FOUND when it is already
// invalid (internal/auth/auth_service.go:274-285).
//
// The sibling's transport is dropped first, through the production Disconnect
// RPC, so no Subscribe goroutine outlives the spec; the single Logout then
// removes both rows. Both calls are idempotent with respect to a spec that
// failed partway through and already detached one of them.
func logoutSharedPlayerSession(ctx context.Context, primary, sibling *integrationtest.Session) {
	GinkgoHelper()
	sibling.DetachTransport(ctx)
	primary.Logout(ctx)
}

// moveAndAssertFloorConsequence is the shared body of every move spec.
//
// It plants an event in the DESTINATION location at an instant that sits ABOVE
// the mover's current floor and BELOW the floor the move installs, reads it back
// BEFORE the move as a positive control, moves, and then reads the destination
// again. The same event, the same query, the same stream — the only thing that
// changed is the session's arrival floor, so the post-move absence is
// attributable to the floor and to nothing else.
//
// The pre-move read succeeds because this suite's harness runs the permissive
// default access engine, under which staffOverride (internal/grpc/scope_floor.go:94-109)
// returns true for every session and the INV-PRIVACY-1 location hard-gate is
// bypassed. That is INV-PRIVACY-6's own subject matter: the override bypasses the
// location match, NOT the temporal floor — which is exactly why the pre-move read
// returns the planted event rather than being denied outright.
func moveAndAssertFloorConsequence(
	ctx context.Context,
	ts *integrationtest.Server,
	mover *integrationtest.Session,
) {
	GinkgoHelper()

	originalArrival := mover.LocationArrivedAt
	destID := ts.NewLocation(ctx)
	destStream := "location." + destID.String()
	ceiling := historyCeiling()

	// Above the mover's CURRENT floor, so it is readable now.
	preAt := originalArrival.Add(time.Millisecond)
	preID := plantEvent(ctx, mover, destStream, preAt)

	beforeFrames, err := mover.QueryStreamHistoryBounded(ctx, destStream, ceiling)
	Expect(err).NotTo(HaveOccurred(),
		"the pre-move read of the destination MUST succeed — the permissive default engine "+
			"grants read_unrestricted_history, so the location hard-gate is bypassed")
	Expect(frameIDs(beforeFrames)).To(ContainElement(preID),
		"precondition: the event planted at %s MUST be readable BEFORE the move, or its "+
			"later absence would prove nothing about the floor", preAt)

	mover.MoveTo(ctx, destID)

	// Read the floor back from the row production filters against, not from the
	// harness struct the helper also updated.
	moved, err := ts.SessionStore().Get(ctx, mover.SessionID)
	Expect(err).NotTo(HaveOccurred(), "the moved session row MUST still be readable")
	Expect(moved.LocationID).To(Equal(destID),
		"the session row MUST now record the destination location")
	newArrival := moved.LocationArrivedAt
	Expect(newArrival).To(BeTemporally(">", originalArrival),
		"INV-PRIVACY-1: a location change MUST advance the arrival floor")

	// Asserted rather than assumed. If these instants ever collapsed together the
	// exclusion below would hold for the wrong reason and this spec would pass
	// while proving nothing; it fails here instead.
	Expect(preAt).To(BeTemporally("<", newArrival),
		"the pre-move event MUST sit strictly below the new floor, so inheriting the old "+
			"floor is the only way it could still be readable")

	// A second event, this one ABOVE the new floor. Both are read back in ONE
	// query, so the floor is demonstrably what separates them: a read that
	// returned nothing at all — broken, denied, or drained empty — would satisfy a
	// bare "does not contain the pre-move event" assertion while proving nothing.
	postAt := newArrival.Add(time.Millisecond)
	postID := plantEvent(ctx, mover, destStream, postAt)

	afterFrames, err := mover.QueryStreamHistoryBounded(ctx, destStream, ceiling)
	Expect(err).NotTo(HaveOccurred(),
		"the post-move read of the destination MUST succeed — the session is now located there")
	afterIDs := frameIDs(afterFrames)

	Expect(afterIDs).To(ContainElement(postID),
		"positive control: the event emitted at %s, above the new floor of %s, MUST be readable",
		postAt, newArrival)
	Expect(afterIDs).NotTo(ContainElement(preID),
		"INV-PRIVACY-1: the event emitted at %s, before the character arrived at %s, MUST NOT "+
			"be readable after the move — its presence means the floor did not advance and the "+
			"arriving character reads history from before they were there",
		preAt, newArrival)
}

var _ = Describe("Moving while attached advances the history floor of the session that moved", func() {
	var (
		ctx context.Context
		ts  *integrationtest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		ts = lifecycleHarness()
	})

	// matrix-row: move-arrival.web-char
	It("stops returning a destination event that predates arrival once the session's location changes", func() {
		player := ts.AuthedPlayer(ctx, "MoverMagnus")
		sess := player.OpenWebSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		moveAndAssertFloorConsequence(ctx, ts, sess)
	})

	// matrix-row: move-arrival.telnet
	It("applies the same advanced floor to a telnet-typed session and leaves its transport identity intact", func() {
		player := ts.AuthedPlayer(ctx, "MoverTiberius")
		sess := player.OpenTelnetSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		Expect(connectionClientTypes(ctx, ts, sess.SessionID)).To(HaveEach("telnet"),
			"precondition: the session that moves MUST be the telnet-typed one, read from the "+
				"column the production Subscribe handler writes")

		moveAndAssertFloorConsequence(ctx, ts, sess)

		// A location change is not a transport event: the connection survives it.
		// The grid-presence roster counts connections by client_type, so a move
		// that dropped or retyped the connection would change who can see the
		// character at the destination.
		Expect(connectionClientTypes(ctx, ts, sess.SessionID)).To(HaveEach("telnet"),
			"the moved session's connection MUST still be telnet-typed — a location change "+
				"must not disturb the transport")
	})

	// matrix-row: move-arrival.multi-session
	//
	// Two concurrent game sessions under ONE player session — the shape
	// test/integration/auth/multi_tab_test.go:282 builds for the other
	// multi-session cells, here from AuthedPlayer.AdditionalCharacter.
	//
	// HONEST SCOPE. The production half of this cell is that each session derives
	// its history view from its OWN row: streamScopeFloor is handed one
	// session.Info (internal/grpc/scope_floor.go:34), so the stayer's read is
	// floored by the stayer's arrival regardless of what the mover's row says. The
	// stayer's row being untouched in the first place is a property of
	// Session.MoveTo's WHERE clause — the test's own setup, not production's
	// behaviour — and this spec does not pretend otherwise. Production's real
	// write, session.Store.UpdateLocationOnMove, is keyed by CHARACTER and updates
	// every active session of that character; it is untested and unreachable from
	// here (see this file's header and issue #4788).
	It("leaves a sibling session's floor and history view untouched when the other session's character moves", func() {
		player := ts.AuthedPlayer(ctx, "MoverMarisol")
		siblingPlayer := player.AdditionalCharacter(ctx, "StayerSorcha")

		mover := player.OpenWebSession(ctx)
		stayer := siblingPlayer.OpenWebSession(ctx)
		DeferCleanup(func() { logoutSharedPlayerSession(ctx, mover, stayer) })

		moverInfo, err := ts.SessionStore().Get(ctx, mover.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the moving session's row MUST be readable")
		stayerInfo, err := ts.SessionStore().Get(ctx, stayer.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the sibling session's row MUST be readable")

		// The multi-session precondition, read from the rows rather than assumed
		// from how the two handles were built.
		Expect(stayer.SessionID).NotTo(Equal(mover.SessionID),
			"two characters of one player MUST hold two distinct game sessions")
		Expect(stayerInfo.PlayerSessionID).To(Equal(moverInfo.PlayerSessionID),
			"both game sessions MUST belong to the SAME player session — otherwise this is two "+
				"unrelated players and the concurrency under test is not present")

		startStream := "location." + stayer.LocationID.String()
		ceiling := historyCeiling()

		// Above BOTH floors: the stayer opened second, so its arrival is the later
		// of the two.
		Expect(stayerInfo.LocationArrivedAt).To(BeTemporally(">=", moverInfo.LocationArrivedAt),
			"precondition: the session opened second MUST carry the later arrival floor")
		sharedAt := stayerInfo.LocationArrivedAt.Add(time.Millisecond)
		sharedID := plantEvent(ctx, stayer, startStream, sharedAt)

		sharedFrames, err := stayer.QueryStreamHistoryBounded(ctx, startStream, ceiling)
		Expect(err).NotTo(HaveOccurred(), "the sibling session MUST read its own location history")
		Expect(frameIDs(sharedFrames)).To(ContainElement(sharedID),
			"precondition: the event planted at %s MUST be readable by the sibling before the "+
				"other session moves", sharedAt)

		moveAndAssertFloorConsequence(ctx, ts, mover)

		afterMove, err := ts.SessionStore().Get(ctx, stayer.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the sibling session MUST survive the other session's move")
		Expect(afterMove.LocationID).To(Equal(stayerInfo.LocationID),
			"the sibling session MUST still be located where it was")
		Expect(afterMove.LocationArrivedAt).To(BeTemporally("==", stayerInfo.LocationArrivedAt),
			"INV-PRIVACY-1: one session's move MUST NOT shift a concurrent session's arrival floor")

		stillReadable, err := stayer.QueryStreamHistoryBounded(ctx, startStream, ceiling)
		Expect(err).NotTo(HaveOccurred(), "the sibling session MUST still read its location history")
		Expect(frameIDs(stillReadable)).To(ContainElement(sharedID),
			"the event planted at %s MUST still be readable by the sibling: each session's view "+
				"is floored by its OWN row, so a concurrent move must not remove history the "+
				"sibling was present for", sharedAt)
	})
})

// blipAndAssertContinuity is the shared body of every transport-blip spec.
//
// The drop is genuine, not notional: the connection rows are asserted GONE while
// the transport is away, so a DetachTransport that silently no-opped would fail
// here rather than sail through the continuity assertions that follow.
//
// The gap event's chosen instant sits just above the session's ORIGINAL floor,
// which is what makes the final assertion falsifiable. A blip that reset the
// floor to the reattach moment — seconds later on the wall clock — would swallow
// exactly this event, so its presence is evidence the floor was preserved rather
// than a restatement of the setup.
func blipAndAssertContinuity(
	ctx context.Context,
	ts *integrationtest.Server,
	sess *integrationtest.Session,
) {
	GinkgoHelper()

	sessionID := sess.SessionID
	locStream := "location." + sess.LocationID.String()
	ceiling := historyCeiling()

	before, err := ts.SessionStore().Get(ctx, sessionID)
	Expect(err).NotTo(HaveOccurred(), "precondition: the session row MUST exist before the blip")
	Expect(before.Status).To(Equal(session.StatusActive),
		"precondition: the session MUST be active before its transport drops")
	arrival := before.LocationArrivedAt
	createdAt := before.CreatedAt

	// The drop. No logout: the player never left.
	sess.DetachTransport(ctx)

	Expect(connectionClientTypes(ctx, ts, sessionID)).To(BeEmpty(),
		"the transport MUST really be gone during the gap; a surviving connection row would "+
			"mean nothing dropped and every continuity assertion below would be decorative")

	duringGap, err := ts.SessionStore().Get(ctx, sessionID)
	Expect(err).NotTo(HaveOccurred(),
		"a transport-level drop MUST NOT delete the session row — that is what distinguishes "+
			"a blip from a logout")
	Expect(duringGap.Status).To(Equal(session.StatusDetached),
		"dropping the transport MUST detach the session rather than end it")

	gapAt := arrival.Add(2 * time.Millisecond)
	gapID := plantEvent(ctx, sess, locStream, gapAt)

	// The transport returns. Subscribe's ReattachCAS flips the row back to active.
	sess.ReattachTransport(ctx)

	after, err := ts.SessionStore().Get(ctx, sessionID)
	Expect(err).NotTo(HaveOccurred(),
		"the SAME session row MUST still be there after the transport returns — a keyed lookup "+
			"on the original identifier, so a freshly minted session cannot stand in for it")
	Expect(after.ID).To(Equal(sessionID),
		"the reattached session MUST be the same one, by identifier")
	Expect(after.CreatedAt).To(BeTemporally("==", createdAt),
		"the row MUST be the original, not a recreation that happened to reuse the identifier")
	Expect(after.Status).To(Equal(session.StatusActive),
		"a reattach within the time-to-live MUST return the session to active")
	Expect(after.LocationArrivedAt).To(BeTemporally("==", arrival),
		"INV-PRIVACY-3: a transport blip MUST leave the arrival floor untouched")
	Expect(connectionClientTypes(ctx, ts, sessionID)).NotTo(BeEmpty(),
		"the returning transport MUST register a connection row")

	frames, err := sess.QueryStreamHistoryBounded(ctx, locStream, ceiling)
	Expect(err).NotTo(HaveOccurred(), "the reattached session MUST read its location history")
	Expect(frameIDs(frames)).To(ContainElement(gapID),
		"the event emitted at %s, while the transport was away, MUST still be readable: the "+
			"floor stayed at %s, so a blip does not cost the player the conversation they "+
			"dropped out of. Its absence would mean the reattach reset the floor",
		gapAt, arrival)
}

var _ = Describe("A transport-level blip preserves the session and its history floor", func() {
	var (
		ctx context.Context
		ts  *integrationtest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		ts = lifecycleHarness()
	})

	// matrix-row: wifi-blip.web-guest
	It("returns a guest session to active on the same row with an event from the gap still readable", func() {
		guest := ts.GuestPlayer(ctx)
		sess := guest.OpenWebSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		// A guest session carries a SECOND floor, the INV-PRIVACY-2 guest identity
		// overlay, which streamScopeFloor applies only when it is LATER than the
		// arrival floor (internal/grpc/scope_floor.go:62-64). The guest character is
		// created before the session, so the arrival floor governs the read below —
		// asserted rather than assumed, because if the overlay were the higher of
		// the two it, and not the transition under test, would decide the result.
		info, err := ts.SessionStore().Get(ctx, sess.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the guest session row MUST be readable")
		Expect(info.GuestCharacterCreatedAt).To(BeTemporally("~", info.CreatedAt, time.Minute),
			"precondition: INV-PRIVACY-2 — this MUST genuinely be a guest session")
		Expect(info.GuestCharacterCreatedAt).To(BeTemporally("<", info.LocationArrivedAt),
			"precondition: the guest identity floor MUST sit below the arrival floor, so the "+
				"arrival floor is what governs the read")

		blipAndAssertContinuity(ctx, ts, sess)
	})

	// matrix-row: wifi-blip.web-char
	It("returns a registered player's session to active on the same row with an event from the gap still readable", func() {
		player := ts.AuthedPlayer(ctx, "BlipBenedict")
		sess := player.OpenWebSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		blipAndAssertContinuity(ctx, ts, sess)

		Expect(sess.CharacterID).To(Equal(player.CharacterID),
			"the reattached session MUST still be bound to the same character")
	})

	// matrix-row: wifi-blip.telnet
	It("returns a telnet session to active with a new connection carrying the same transport identity", func() {
		player := ts.AuthedPlayer(ctx, "BlipTorquil")
		sess := player.OpenTelnetSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		Expect(connectionClientTypes(ctx, ts, sess.SessionID)).To(HaveEach("telnet"),
			"precondition: the session that blips MUST be the telnet-typed one")

		blipAndAssertContinuity(ctx, ts, sess)

		// The connection row is new — blipAndAssertContinuity asserted the old one
		// was gone — so this reads the transport identity of the RETURNING
		// connection. The roster counts by client_type, so a reconnect that
		// silently retyped the transport would change who can see the character.
		Expect(connectionClientTypes(ctx, ts, sess.SessionID)).To(HaveEach("telnet"),
			"the returning connection MUST carry the telnet client type")
	})

	// matrix-row: wifi-blip.multi-session
	//
	// The multi-session content here is genuinely production's, not the harness's.
	// Session.DetachTransport calls the production Disconnect RPC, which takes a
	// session id AND the caller's player-session token and validates the pairing
	// through auth.ValidateSessionOwnership
	// (internal/grpc/lifecycle_handler.go:106-123). Both sessions below share ONE
	// token, so an implementation that keyed the teardown on the token rather than
	// the session id would take the steady session down with the blipped one. That
	// is what these assertions would catch, and it is why the two sessions must
	// belong to one player rather than to two.
	It("drops and restores one session while a concurrent session under the same player session stays live", func() {
		player := ts.AuthedPlayer(ctx, "BlipBeatrix")
		siblingPlayer := player.AdditionalCharacter(ctx, "SteadySeverin")

		blipped := player.OpenWebSession(ctx)
		steady := siblingPlayer.OpenWebSession(ctx)
		DeferCleanup(func() { logoutSharedPlayerSession(ctx, blipped, steady) })

		blippedInfo, err := ts.SessionStore().Get(ctx, blipped.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the blipping session's row MUST be readable")
		steadyBefore, err := ts.SessionStore().Get(ctx, steady.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the steady session's row MUST be readable")

		Expect(steady.SessionID).NotTo(Equal(blipped.SessionID),
			"two characters of one player MUST hold two distinct game sessions")
		Expect(steadyBefore.PlayerSessionID).To(Equal(blippedInfo.PlayerSessionID),
			"both game sessions MUST belong to the SAME player session — the shared token is "+
				"what makes a token-keyed teardown detectable")

		steadyStream := "location." + steady.LocationID.String()
		ceiling := historyCeiling()

		// Both sessions read the SAME stream but are floored by their OWN rows,
		// and the session opened second carries the later floor. Every instant
		// planted below is therefore derived from the LATER of the two, so both
		// sessions can see both events and any absence is attributable to the
		// transition rather than to an event that was under one session's floor
		// all along. Asserted rather than assumed.
		Expect(steadyBefore.LocationArrivedAt).To(BeTemporally(">=", blippedInfo.LocationArrivedAt),
			"precondition: the session opened second MUST carry the later arrival floor")
		steadyAt := steadyBefore.LocationArrivedAt.Add(time.Millisecond)
		steadyID := plantEvent(ctx, steady, steadyStream, steadyAt)

		sess := blipped
		sessionID := sess.SessionID
		arrival := blippedInfo.LocationArrivedAt

		sess.DetachTransport(ctx)

		// Mid-gap: the blipped session is detached with no transport, and the
		// steady one is untouched. Read while the gap is OPEN, because a
		// token-keyed teardown that took the steady session down would be invisible
		// once the blipped one has already come back.
		Expect(connectionClientTypes(ctx, ts, sessionID)).To(BeEmpty(),
			"the blipped session's transport MUST really be gone during the gap")
		duringGap, err := ts.SessionStore().Get(ctx, sessionID)
		Expect(err).NotTo(HaveOccurred(), "the blipped session row MUST survive the drop")
		Expect(duringGap.Status).To(Equal(session.StatusDetached),
			"the blipped session MUST be detached during the gap")

		steadyDuring, err := ts.SessionStore().Get(ctx, steady.SessionID)
		Expect(err).NotTo(HaveOccurred(),
			"the concurrent session MUST NOT be deleted by the other session's transport drop")
		Expect(steadyDuring.Status).To(Equal(session.StatusActive),
			"the concurrent session MUST stay ACTIVE while its sibling is detached — a "+
				"Disconnect keyed on the shared player-session token instead of the session id "+
				"would have detached this one too")
		Expect(connectionClientTypes(ctx, ts, steady.SessionID)).NotTo(BeEmpty(),
			"the concurrent session's transport MUST still be attached")
		Expect(steadyDuring.LocationArrivedAt).To(BeTemporally("==", steadyBefore.LocationArrivedAt),
			"INV-PRIVACY-1: the concurrent session's arrival floor MUST be untouched")

		// Above BOTH floors (see the note above), and still far below the wall
		// clock at which the reattach happens — which is what makes the blipped
		// session's read falsifiable against a floor that was reset on reattach.
		gapAt := steadyBefore.LocationArrivedAt.Add(2 * time.Millisecond)
		gapID := plantEvent(ctx, sess, steadyStream, gapAt)

		sess.ReattachTransport(ctx)

		after, err := ts.SessionStore().Get(ctx, sessionID)
		Expect(err).NotTo(HaveOccurred(), "the blipped session MUST be resumable by identifier")
		Expect(after.ID).To(Equal(sessionID),
			"the reattached session MUST be the same one, by identifier")
		Expect(after.Status).To(Equal(session.StatusActive),
			"the reattached session MUST return to active")
		Expect(after.LocationArrivedAt).To(BeTemporally("==", arrival),
			"INV-PRIVACY-3: the blip MUST leave the arrival floor untouched")

		blippedFrames, err := sess.QueryStreamHistoryBounded(ctx, steadyStream, ceiling)
		Expect(err).NotTo(HaveOccurred(), "the reattached session MUST read its location history")
		Expect(frameIDs(blippedFrames)).To(ContainElement(gapID),
			"the event emitted at %s while the transport was away MUST still be readable to the "+
				"session that dropped", gapAt)

		steadyFrames, err := steady.QueryStreamHistoryBounded(ctx, steadyStream, ceiling)
		Expect(err).NotTo(HaveOccurred(), "the concurrent session MUST still read its location history")
		steadyIDs := frameIDs(steadyFrames)
		Expect(steadyIDs).To(ContainElement(steadyID),
			"the concurrent session MUST still see the event it published at %s before the "+
				"other session's transport dropped", steadyAt)
		Expect(steadyIDs).To(ContainElement(gapID),
			"and MUST see the event published during the gap: it never left, so nothing about "+
				"its sibling's blip may narrow what it reads")
	})
})

// TestPrivacy_ReattachWithinTTLPreservesFloor, from the history-scope privacy
// specification §8, reproduced there as:
//
//	character A connects at T0 in location L (LocationArrivedAt=T0), a third
//	party emits events at T1, A's transport drops at T2 (status=Detached), the
//	third party emits events at T3, A's Subscribe.ReattachCAS at T4 (within TTL);
//	A's subsequent history query for location L returns events with timestamps in
//	[T0, T4] — INCLUDING the T1 and T3 events. The session row stayed alive
//	through the disconnect; floor preserved at T0.
//
// Every instant is placed explicitly with the timestamped emit rather than by
// letting the wall clock separate the events, and both third-party events are
// asserted by IDENTIFIER — a count of returned frames would be satisfied by an
// unrelated event landing in this shared start-location stream.
//
// The upper bound of the read is T4 ITSELF: the attach moment the production
// Subscribe handler stamps on the REPLAY_COMPLETE control frame, which is the
// value a real client passes as not_after_ms on its reconnect backfill. Using it
// rather than an arbitrary future bound is what makes the read genuinely the
// spec's [T0, T4] window.
var _ = Describe("TestPrivacy_ReattachWithinTTLPreservesFloor: a reattach within the time-to-live returns the events emitted while the player was detached", func() {
	var (
		ctx context.Context
		ts  *integrationtest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		ts = lifecycleHarness()
	})

	It("returns both the pre-detach and the during-detach third-party events after the transport returns", func() {
		alice := ts.AuthedPlayer(ctx, "FloorAlice")
		sess := alice.OpenWebSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		// A different character in the same location. The third party is the one
		// that emits, so the events under test are not the reader's own.
		third := ts.AuthedPlayer(ctx, "FloorBartholomew").OpenWebSession(ctx)
		DeferCleanup(func() { third.Logout(ctx) })
		Expect(third.LocationID).To(Equal(sess.LocationID),
			"precondition: both characters MUST share a location so the third party's emits land "+
				"in the stream under test")

		atT0, err := ts.SessionStore().Get(ctx, sess.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the session row MUST be readable at T0")
		t0 := atT0.LocationArrivedAt
		locStream := "location." + sess.LocationID.String()

		// T1 — before the drop, above the floor.
		t1 := t0.Add(time.Millisecond)
		t1ID := plantEvent(ctx, third, locStream, t1)

		// T2 — the transport drops. No logout.
		sess.DetachTransport(ctx)
		atT2, err := ts.SessionStore().Get(ctx, sess.SessionID)
		Expect(err).NotTo(HaveOccurred(),
			"the session row MUST stay alive through the disconnect — that is the premise of "+
				"the whole scenario")
		Expect(atT2.Status).To(Equal(session.StatusDetached),
			"the drop MUST leave the session detached, not ended")
		Expect(atT2.ExpiresAt).NotTo(BeNil(),
			"the detach MUST start a time-to-live for the reattach below to be within")

		// T3 — emitted while A is away.
		t3 := t0.Add(2 * time.Millisecond)
		t3ID := plantEvent(ctx, third, locStream, t3)

		// T4 — the production Subscribe path, whose ReattachCAS flips the row back.
		sess.ReattachTransport(ctx)

		atT4, err := ts.SessionStore().Get(ctx, sess.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the same session row MUST still be there at T4")
		Expect(atT4.Status).To(Equal(session.StatusActive),
			"Subscribe's ReattachCAS MUST return the session to active")
		Expect(atT4.LocationArrivedAt).To(BeTemporally("==", t0),
			"floor preserved at T0: the reattach MUST NOT move the arrival timestamp")

		t4Ms := sess.AttachMomentMs()
		Expect(t4Ms).To(BeNumerically(">", int64(0)),
			"the production Subscribe handler MUST have stamped an attach moment on "+
				"REPLAY_COMPLETE; without it the bound below would silently become unbounded and "+
				"the [T0, T4] window would not be the window actually read")
		t4 := time.UnixMilli(t4Ms).UTC()
		Expect(t1).To(BeTemporally("<=", t4), "T1 MUST fall inside the [T0, T4] window")
		Expect(t3).To(BeTemporally("<=", t4), "T3 MUST fall inside the [T0, T4] window")

		frames, err := sess.QueryStreamHistoryBounded(ctx, locStream, t4Ms)
		Expect(err).NotTo(HaveOccurred(),
			"the reattached session MUST be able to query its own location history")
		ids := frameIDs(frames)

		Expect(ids).To(ContainElement(t1ID),
			"the third party's event at T1 (%s) MUST be returned: it is above the preserved "+
				"floor of %s", t1, t0)
		Expect(ids).To(ContainElement(t3ID),
			"the third party's event at T3 (%s), emitted while A was detached, MUST be returned "+
				"too — its absence would mean the reattach reset the floor and cost the player "+
				"the conversation they dropped out of", t3)
	})
})

// TestPrivacy_TTLExpiryEndsSessionFreshFloor, from the history-scope privacy
// specification §8, reproduced there as:
//
//	character A connects at T0, A's transport drops at T1, no reattach for TTL+1,
//	the reaper deletes the row at T2; A logs in again at T3 -> a fresh
//	SelectCharacter creates a new session with LocationArrivedAt=T3; events with
//	timestamps in [T0, T3) are NOT visible to the new session.
//
// The expired state is reached the way production reaches it, because the
// specification's own wording is that the REAPER deleted the session: detach
// through the production Disconnect RPC, backdate the expiry with
// Server.DetachAndExpireSession, and drive the real session.Reaper.
//
// Server.ExpireSession — the older helper with the confusingly similar name —
// MUST NOT be used here. It forces the terminal 'expired' status, which
// ListExpired's predicate (status='detached' AND expires_at < now,
// internal/store/session_store.go:445-452) can never match, so a spec seeded
// through it would be asserting against a state production never produces.
// reapSessionAndExpectRowGone additionally asserts the row is genuinely in
// ListExpired's result set BEFORE the sweep, so that mistake fails loudly rather
// than timing out on an unreachable assertion.
var _ = Describe("TestPrivacy_TTLExpiryEndsSessionFreshFloor: a session that expired hands no history floor to the login that replaces it", func() {
	var (
		ctx context.Context
		ts  *integrationtest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		ts = lifecycleHarness()
	})

	It("hides an event from before the new arrival timestamp after the reaper deleted the expired session", func() {
		alice := ts.AuthedPlayer(ctx, "ExpiryAlice")

		// T0 — A connects.
		first := alice.OpenWebSession(ctx)
		firstID := first.SessionID
		locStream := "location." + first.LocationID.String()

		atT0, err := ts.SessionStore().Get(ctx, firstID)
		Expect(err).NotTo(HaveOccurred(), "the first session row MUST be readable at T0")
		t0 := atT0.LocationArrivedAt
		ceiling := historyCeiling()

		// An event inside [T0, T3). Planted by a third party so the assertion is
		// about visibility rather than about the reader's own output.
		third := ts.AuthedPlayer(ctx, "ExpiryBartholomew").OpenWebSession(ctx)
		DeferCleanup(func() { third.Logout(ctx) })
		Expect(third.LocationID).To(Equal(first.LocationID),
			"precondition: both characters MUST share a location")

		earlyAt := t0.Add(time.Millisecond)
		earlyID := plantEvent(ctx, third, locStream, earlyAt)

		// Positive control: readable by the session that was present for it. Without
		// it the absence asserted at the end could be satisfied by an event that was
		// never queryable in the first place.
		liveFrames, err := first.QueryStreamHistoryBounded(ctx, locStream, ceiling)
		Expect(err).NotTo(HaveOccurred(), "the first session MUST read its own location history")
		Expect(frameIDs(liveFrames)).To(ContainElement(earlyID),
			"precondition: the event at %s MUST be readable by the session that was present for it",
			earlyAt)

		// T1 — the transport drops. T2 — the real reaper deletes the row.
		detachAndExpectDetached(ctx, ts, first)
		ts.DetachAndExpireSession(ctx, firstID, time.Now().Add(-time.Minute))
		reapSessionAndExpectRowGone(ctx, ts, firstID)

		// T3 — A logs in again. With the row swept there is nothing to reattach to,
		// so SelectCharacter must take its create branch.
		second := alice.OpenWebSession(ctx)
		DeferCleanup(func() { second.Logout(ctx) })

		Expect(second.Reattached).To(BeFalse(),
			"the reaper deleted the row, so the re-login MUST take SelectCharacter's create branch")
		Expect(second.SessionID).NotTo(Equal(firstID),
			"the re-login MUST mint a NEW session, not resurrect the reaped one")

		atT3, err := ts.SessionStore().Get(ctx, second.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the fresh session row MUST be readable at T3")
		t3 := atT3.LocationArrivedAt
		Expect(t3).To(BeTemporally(">", t0),
			"the fresh session MUST carry a LATER arrival floor than the expired one")
		Expect(earlyAt).To(BeTemporally("<", t3),
			"the planted event MUST sit strictly inside [T0, T3), so inheriting the expired "+
				"session's floor is the only way it could still be readable")

		// A second event, above the NEW floor, read back in the SAME query. A read
		// that returned nothing at all — broken, denied, or drained empty — would
		// satisfy the absence assertion below while proving nothing.
		lateAt := t3.Add(time.Millisecond)
		lateID := plantEvent(ctx, third, locStream, lateAt)

		freshFrames, err := second.QueryStreamHistoryBounded(ctx, locStream, ceiling)
		Expect(err).NotTo(HaveOccurred(), "the fresh session MUST query its location history")
		freshIDs := frameIDs(freshFrames)

		Expect(freshIDs).To(ContainElement(lateID),
			"positive control: the event at %s, above the new floor of %s, MUST be readable",
			lateAt, t3)
		Expect(freshIDs).NotTo(ContainElement(earlyID),
			"the event at %s falls in [T0, T3) and MUST NOT be visible to the session created "+
				"at %s — its presence means the re-login inherited the expired session's floor",
			earlyAt, t3)
	})
})
