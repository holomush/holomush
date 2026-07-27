// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package session_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// Session-lifecycle matrix — the idle-timeout transitions: dropping every
// connection, the reaper sweep at expiry, and logging in again afterwards.
//
// Each spec carries a `// matrix-row: <id>` marker naming the cell of
// test/session-matrix.yaml it satisfies. A marker with no row, or a row with
// no marker, fails the bijection meta-test.
//
// # Why these three transitions belong together
//
// They are one chain, and the security content is in the chain rather than in
// any single link. A detach must START a time-to-live rather than end the
// session, and must leave the history floor alone, or a returning player
// silently gains or loses history. Expiry must actually remove the row, or the
// floor survives into a re-login that was never entitled to it. So the detach
// specs assert the arrival timestamp is UNCHANGED and the re-login specs assert
// it MOVED — the same column, asserted in opposite directions at the two ends
// of the chain.
//
// # Driving the real reaper — and the helper that must NOT be used
//
// Every sweep below runs the production session.Reaper against the harness's
// session store, as production runs it. Nothing here calls the reaper's
// internal sweep directly and nothing asserts a deletion the spec performed
// itself.
//
// The row is made eligible with Server.DetachAndExpireSession, which writes the
// state ListExpired's predicate selects (status='detached' AND expires_at <
// now, internal/store/session_store.go:445-452). Server.ExpireSession — the
// older, similarly named helper — forces the TERMINAL 'expired' status, which
// that predicate can NEVER match: a spec seeded through it would wait forever
// for a sweep that cannot see the row. reapSessionAndExpectRowGone therefore
// asserts the row is genuinely in ListExpired's result set BEFORE starting the
// reaper, so the sweep assertion cannot pass against a row the reaper never
// had a chance to look at.
//
// # No sleeping
//
// Expiry order comes from parameters, not from waiting: DetachAndExpireSession
// takes the expiry instant, EmitDirectEventAt takes the event instant, and the
// reaper's tick interval is injected. No spec in this file sleeps, and none
// shortens a production time-to-live constant to finish sooner.
//
// # TELNET SCOPE
//
// The telnet cells assert SESSION STATE for a session whose connection row
// carries client_type='telnet'. No telnet gateway is in the loop and
// internal/telnet is not exercised. Nothing below is a claim about telnet
// PROTOCOL behaviour.
//
// # PRIVACY SPEC IDENTIFIERS
//
// The arrival-timestamp assertions are the by-identifier form of INV-PRIVACY-1
// (LocationArrivedAt is the per-session floor every location history query is
// filtered against, internal/grpc/scope_floor.go:34-38) and of I-PRIV-3 /
// INV-PRIVACY-3 for the detach half of the detach→reattach cycle. They are
// cited in prose and deliberately carry NO `// Verifies:` binding annotation:
// each invariant also covers the durable consumer's DeliverPolicy / OptStartTime
// which these specs do not touch, so annotating them would claim a whole
// invariant on a partial assertion.

const (
	// reaperTick is how often the driven reaper sweeps. Small enough that the
	// specs finish quickly, and injected rather than waited on.
	reaperTick = 25 * time.Millisecond
	// reaperRunBudget bounds the reaper goroutine's lifetime; reaperAssertBudget
	// bounds the assertion. The assertion budget is deliberately the SHORTER of
	// the two, so a spec that is going to fail fails on its own assertion while
	// the reaper is still running, rather than on a reaper that quietly stopped.
	reaperRunBudget    = 15 * time.Second
	reaperAssertBudget = 10 * time.Second
)

// reapSessionAndExpectRowGone runs the production session reaper against the
// harness's session store until sessionID's row is gone, then stops it.
//
// Two properties make the sweep assertion falsifiable rather than decorative:
//
//   - The row is asserted to be in ListExpired's result set FIRST. That is the
//     reaper's own selection predicate, so a seam that wrote a state the reaper
//     cannot see (Server.ExpireSession's terminal 'expired' status is exactly
//     such a state) fails here, loudly, instead of timing out eight seconds
//     later on an assertion that was never reachable.
//   - Absence is checked by a KEYED lookup on this session's identifier, never
//     by counting rows. The suite shares one harness, so a concurrently created
//     session would otherwise be able to mask the result.
//
// LeaseTTL is deliberately left zero, which disables the lease sweep
// (reapLapsedConnections). The lease sweep ranges over every connection in the
// database, so enabling it here would let one spec reap another spec's live
// connections.
func reapSessionAndExpectRowGone(ctx context.Context, ts *integrationtest.Server, sessionID string) {
	GinkgoHelper()

	expired, err := ts.SessionStore().ListExpired(ctx)
	Expect(err).NotTo(HaveOccurred(), "listing expired sessions MUST succeed")
	expiredIDs := make([]string, 0, len(expired))
	for _, info := range expired {
		expiredIDs = append(expiredIDs, info.ID)
	}
	Expect(expiredIDs).To(ContainElement(sessionID),
		"precondition: session %s MUST match the reaper's OWN selection predicate "+
			"(status='detached' AND expires_at < now) before the sweep is driven — a row the "+
			"reaper cannot select would make the deletion assertion below unreachable",
		sessionID)

	reaper := session.NewReaper(ts.SessionStore(), session.ReaperConfig{
		Interval: reaperTick,
	})

	reaperCtx, cancel := context.WithTimeout(ctx, reaperRunBudget)
	done := make(chan struct{})
	go func() {
		defer close(done)
		reaper.Run(reaperCtx)
	}()
	// Deterministic teardown even on failure: Ginkgo unwinds a failed assertion
	// with runtime.Goexit, which runs defers but skips any inline cancel placed
	// after the failure point. Cancelling and then waiting for the goroutine
	// keeps a reaper from outliving its spec and sweeping a later one's rows.
	defer func() {
		cancel()
		<-done
	}()

	Eventually(func(g Gomega) {
		_, getErr := ts.SessionStore().Get(ctx, sessionID)
		g.Expect(getErr).To(HaveOccurred(),
			"the reaper MUST delete session %s once its expiry has passed", sessionID)
		o, ok := oops.AsOops(getErr)
		g.Expect(ok).To(BeTrue(), "a missing session MUST surface as a coded error, got %v", getErr)
		g.Expect(o.Code()).To(Equal("SESSION_NOT_FOUND"),
			"the row MUST be absent, not merely unreadable for some other reason")
	}, reaperAssertBudget, 50*time.Millisecond).Should(Succeed())
}

// detachAndExpectDetached drops every connection of sess through the production
// Disconnect RPC and returns the persisted row, having asserted it reached the
// detached state. Shared by the reaper and re-login specs so they start from the
// state the detach specs below prove, rather than re-deriving it.
func detachAndExpectDetached(
	ctx context.Context,
	ts *integrationtest.Server,
	sess *integrationtest.Session,
) *session.Info {
	GinkgoHelper()
	sess.DetachTransport(ctx)

	info, err := ts.SessionStore().Get(ctx, sess.SessionID)
	Expect(err).NotTo(HaveOccurred(),
		"dropping every connection MUST NOT delete session %s", sess.SessionID)
	Expect(info.Status).To(Equal(session.StatusDetached),
		"dropping every connection MUST move the session to detached")
	return info
}

var _ = Describe("Dropping every connection detaches the session and starts its time-to-live", func() {
	var (
		ctx context.Context
		ts  *integrationtest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		ts = lifecycleHarness()
	})

	// matrix-row: detach-all.web-guest
	It("leaves a guest session detached with a future expiry and an unmoved arrival timestamp", func() {
		sess := ts.ConnectGuest(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		originalArrival := sess.LocationArrivedAt
		Expect(originalArrival).NotTo(BeZero(),
			"precondition: the guest session carries an arrival floor to compare against")

		info := detachAndExpectDetached(ctx, ts, sess)

		Expect(info.DetachedAt).NotTo(BeNil(),
			"detaching MUST record when the session detached")
		Expect(info.ExpiresAt).NotTo(BeNil(),
			"detaching MUST give the session an expiry — a detached session with no expiry "+
				"would never be swept")
		Expect(*info.ExpiresAt).To(BeTemporally(">", time.Now()),
			"the expiry MUST be in the FUTURE: dropping every connection STARTS a time-to-live "+
				"during which a reattach can still resume the session, it does not end it")
		Expect(info.LocationArrivedAt).To(BeTemporally("==", originalArrival),
			"INV-PRIVACY-1 / I-PRIV-3: the detach half of the detach-reattach cycle MUST leave "+
				"the location arrival timestamp untouched — a detach that moved the floor would "+
				"silently change which history the player sees when they come back")
	})

	// matrix-row: detach-all.web-char
	It("leaves a registered player's session detached and still bound to the same character", func() {
		player := ts.AuthedPlayer(ctx, "DetachDelia")
		sess := player.OpenWebSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		originalArrival := sess.LocationArrivedAt

		info := detachAndExpectDetached(ctx, ts, sess)

		Expect(info.CharacterID).To(Equal(player.CharacterID),
			"the detached session MUST still be bound to the character that was selected — "+
				"the row is what a reattach within the time-to-live resumes")
		Expect(info.DetachedAt).NotTo(BeNil(),
			"detaching MUST record when the session detached")
		Expect(info.ExpiresAt).NotTo(BeNil(),
			"detaching MUST give the session an expiry")
		Expect(*info.ExpiresAt).To(BeTemporally(">", time.Now()),
			"the expiry MUST be in the future — the detach starts a time-to-live")
		Expect(info.LocationArrivedAt).To(BeTemporally("==", originalArrival),
			"INV-PRIVACY-1 / I-PRIV-3: detaching MUST leave the location arrival timestamp "+
				"unchanged")
	})

	// matrix-row: detach-all.telnet
	It("removes the telnet connection row while the detached session and its arrival timestamp survive", func() {
		player := ts.AuthedPlayer(ctx, "DetachTobias")
		sess := player.OpenTelnetSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		originalArrival := sess.LocationArrivedAt

		// Precondition read through the column production writes, not through
		// the argument this spec passed to OpenTelnetSession.
		Expect(connectionClientTypes(ctx, ts, sess.SessionID)).To(HaveEach("telnet"),
			"precondition: the session's connections MUST be telnet-typed before the drop")

		info := detachAndExpectDetached(ctx, ts, sess)

		// This is the "drop ALL connections" half of the transition, observed
		// where production observes it. The grid-presence roster counts rows in
		// session_connections by client_type, so a telnet row that survived the
		// drop would keep a disconnected character visible on the grid.
		Expect(connectionClientTypes(ctx, ts, sess.SessionID)).To(BeEmpty(),
			"every connection row MUST be gone once the transport drops; a surviving telnet "+
				"row would keep the character on the grid roster after they disconnected")

		Expect(info.ExpiresAt).NotTo(BeNil(),
			"detaching MUST give the telnet session an expiry")
		Expect(*info.ExpiresAt).To(BeTemporally(">", time.Now()),
			"the expiry MUST be in the future — the detach starts a time-to-live")
		Expect(info.LocationArrivedAt).To(BeTemporally("==", originalArrival),
			"INV-PRIVACY-1 / I-PRIV-3: detaching MUST leave the location arrival timestamp "+
				"unchanged regardless of transport")
	})
})

var _ = Describe("The reaper sweep at time-to-live expiry removes the session row", func() {
	var (
		ctx context.Context
		ts  *integrationtest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		ts = lifecycleHarness()
	})

	// matrix-row: reaper-sweep.web-guest
	It("deletes a detached guest session whose expiry has passed", func() {
		sess := ts.ConnectGuest(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		detachAndExpectDetached(ctx, ts, sess)

		// Move the expiry into the past rather than waiting for the real
		// thirty-minute time-to-live. detached_at is preserved, so the row keeps
		// the detach moment the production Disconnect RPC recorded.
		ts.DetachAndExpireSession(ctx, sess.SessionID, time.Now().Add(-time.Minute))

		reapSessionAndExpectRowGone(ctx, ts, sess.SessionID)
	})

	// matrix-row: reaper-sweep.web-char
	It("deletes a detached registered player's session whose expiry has passed", func() {
		player := ts.AuthedPlayer(ctx, "SweepSeren")
		sess := player.OpenWebSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		detachAndExpectDetached(ctx, ts, sess)
		ts.DetachAndExpireSession(ctx, sess.SessionID, time.Now().Add(-time.Minute))

		reapSessionAndExpectRowGone(ctx, ts, sess.SessionID)
	})

	// matrix-row: reaper-sweep.telnet
	It("deletes a detached telnet session whose expiry has passed", func() {
		player := ts.AuthedPlayer(ctx, "SweepSorrel")
		sess := player.OpenTelnetSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		Expect(connectionClientTypes(ctx, ts, sess.SessionID)).To(HaveEach("telnet"),
			"precondition: the reaped session MUST be the telnet-typed one")

		detachAndExpectDetached(ctx, ts, sess)
		ts.DetachAndExpireSession(ctx, sess.SessionID, time.Now().Add(-time.Minute))

		reapSessionAndExpectRowGone(ctx, ts, sess.SessionID)
	})
})

// frameIDs projects the event identifiers out of a history read, so a spec can
// assert a SPECIFIC event is present or absent rather than counting frames. The
// suite is shared, so a count would be sensitive to any other spec that happened
// to publish into the same stream.
func frameIDs(frames []*corev1.EventFrame) []string {
	out := make([]string, 0, len(frames))
	for _, f := range frames {
		out = append(out, f.GetId())
	}
	return out
}

// relogPayload is the event body the post-expiry specs publish. Its shape mirrors
// the pose payload the privacy suite uses, so the event is a realistic history
// row rather than a synthetic one.
func relogPayload(name string) []byte {
	return []byte(`{"character_name":"` + name + `","action":"speaks while the first session is live."}`)
}

var _ = Describe("Logging in again after expiry produces a genuinely fresh session", func() {
	var (
		ctx context.Context
		ts  *integrationtest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		ts = lifecycleHarness()
	})

	// matrix-row: post-ttl-relogin.web-guest
	//
	// The guest arm of the same INV-PRIVACY-1 question, and the cell that was
	// blocked until Server.GuestPlayer existed. The block mattered: with only
	// Server.ConnectGuest available, the sole stand-in was a SECOND guest, which
	// would satisfy "different session identifier" and "later arrival timestamp"
	// with no expiry involved and nothing for the reaper to have done — a spec
	// that passes whether or not the transition works. This one takes the SAME
	// guest character through detach, expiry and a real reaper sweep, then logs
	// it in again on its own bearer token.
	//
	// Guest sessions carry a second floor, the INV-PRIVACY-2 guest identity
	// overlay, which streamScopeFloor applies only when it is LATER than the
	// arrival floor (internal/grpc/scope_floor.go:61-64). The guest character is
	// created before either session, so the arrival floor governs both reads —
	// and asserting the guest floor is UNCHANGED across the re-login while the
	// arrival floor MOVED is what shows this is the same guest coming back
	// rather than a new one.
	It("mints a new guest session whose history floor excludes an event the expired session could read", func() {
		guest := ts.GuestPlayer(ctx)

		first := guest.OpenWebSession(ctx)
		firstID := first.SessionID
		firstArrival := first.LocationArrivedAt
		locStream := "location." + first.LocationID.String()

		firstInfo, err := ts.SessionStore().Get(ctx, firstID)
		Expect(err).NotTo(HaveOccurred(), "the guest session row MUST be readable")
		guestFloor := firstInfo.GuestCharacterCreatedAt
		// Unset round-trips as the UNIX EPOCH, not Go's zero time, so
		// NotTo(BeZero()) would pass on an unset floor.
		Expect(guestFloor).To(BeTemporally("~", firstInfo.CreatedAt, time.Minute),
			"precondition: INV-PRIVACY-2 — this MUST genuinely be a guest session")
		Expect(guestFloor).To(BeTemporally("<", firstArrival),
			"precondition: the guest identity floor MUST sit below the arrival floor, so the "+
				"arrival floor is what governs the reads below")

		plantedAt := firstArrival.Add(time.Millisecond)
		plantedID, err := first.EmitDirectEventAt(ctx, locStream,
			"core-communication:pose", relogPayload(first.CharacterName), plantedAt)
		Expect(err).NotTo(HaveOccurred(), "the timestamped emit MUST publish")

		notAfterMs := time.Now().Add(time.Hour).UnixMilli()

		liveFrames, err := first.QueryStreamHistoryBounded(ctx, locStream, notAfterMs)
		Expect(err).NotTo(HaveOccurred(), "the first guest session MUST read its own location history")
		Expect(frameIDs(liveFrames)).To(ContainElement(plantedID),
			"precondition: the planted event MUST be readable by the session that was present for it")

		detachAndExpectDetached(ctx, ts, first)
		ts.DetachAndExpireSession(ctx, firstID, time.Now().Add(-time.Minute))
		reapSessionAndExpectRowGone(ctx, ts, firstID)

		second := guest.OpenWebSession(ctx)
		DeferCleanup(func() { second.Logout(ctx) })

		Expect(second.Reattached).To(BeFalse(),
			"with the row swept there is nothing left to reattach to")
		Expect(second.SessionID).NotTo(Equal(firstID),
			"a guest re-login after expiry MUST mint a NEW session, not resurrect the reaped one")
		Expect(second.CharacterID).To(Equal(guest.CharacterID),
			"the returning guest MUST be the SAME character — this is the assertion a "+
				"second-guest stand-in could not satisfy")
		Expect(second.LocationArrivedAt).To(BeTemporally(">", firstArrival),
			"INV-PRIVACY-1: the new session MUST carry a LATER arrival floor")

		secondInfo, err := ts.SessionStore().Get(ctx, second.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the fresh guest session row MUST be readable")
		Expect(secondInfo.GuestCharacterCreatedAt).To(BeTemporally("==", guestFloor),
			"INV-PRIVACY-2: the guest identity floor is the CHARACTER's creation time, so it MUST "+
				"be identical across the re-login; only the arrival floor moves")

		Expect(plantedAt).To(BeTemporally(">", firstArrival),
			"the planted event MUST sit above the expired session's floor")
		Expect(plantedAt).To(BeTemporally("<", second.LocationArrivedAt),
			"the planted event MUST sit below the new session's floor")

		// Positive control on the fresh read — see the web-char spec for why a
		// bare absence assertion would be satisfied by an empty read.
		afterAt := second.LocationArrivedAt.Add(time.Millisecond)
		afterID, err := second.EmitDirectEventAt(ctx, locStream,
			"core-communication:pose", relogPayload(second.CharacterName), afterAt)
		Expect(err).NotTo(HaveOccurred(), "the post-relogin emit MUST publish")

		freshFrames, err := second.QueryStreamHistoryBounded(ctx, locStream, notAfterMs)
		Expect(err).NotTo(HaveOccurred(), "the new guest session MUST query its location history")
		freshIDs := frameIDs(freshFrames)

		Expect(freshIDs).To(ContainElement(afterID),
			"positive control: the new session's read MUST return the event emitted at %s, above "+
				"its own floor of %s",
			afterAt, second.LocationArrivedAt)
		Expect(freshIDs).NotTo(ContainElement(plantedID),
			"INV-PRIVACY-1: the event emitted at %s, before the new session arrived at %s, MUST NOT "+
				"be readable by it — its presence means the re-login inherited the expired "+
				"session's history floor",
			plantedAt, second.LocationArrivedAt)
	})

	// matrix-row: post-ttl-relogin.web-char
	//
	// INV-PRIVACY-1 is the invariant with the security content here.
	// LocationArrivedAt is the per-session floor every location history query is
	// filtered against (internal/grpc/scope_floor.go:34-38), so if a re-login
	// after expiry inherited the expired session's floor, the returning player
	// would read history from a period they were not present for. This spec
	// decides that question rather than merely observing the new session's
	// bookkeeping: it plants an event STRICTLY BETWEEN the two arrival
	// timestamps, so the event is readable under the old floor and unreadable
	// under a fresh one, and the absence assertion at the end therefore fails if
	// the floor were inherited.
	It("mints a new session whose history floor excludes an event the expired session could read", func() {
		player := ts.AuthedPlayer(ctx, "RejoinRosalind")

		first := player.OpenWebSession(ctx)
		firstID := first.SessionID
		firstArrival := first.LocationArrivedAt
		locStream := "location." + first.LocationID.String()

		// Placed just after the first session's floor with the timestamped emit
		// helper, so its position relative to both floors is exact and no sleep
		// is needed to order it.
		plantedAt := firstArrival.Add(time.Millisecond)
		plantedID, err := first.EmitDirectEventAt(ctx, locStream,
			"core-communication:pose", relogPayload(first.CharacterName), plantedAt)
		Expect(err).NotTo(HaveOccurred(), "the timestamped emit MUST publish")

		// A deliberately NON-excluding upper bound: it sits far in the future, so
		// the server-side scope floor is the only thing that can remove an event
		// from either read below. A tight bound would confound the two.
		notAfterMs := time.Now().Add(time.Hour).UnixMilli()

		// Positive control. Without it, the absence assertion at the end could be
		// satisfied by an event that was never queryable in the first place —
		// proving nothing about the floor.
		liveFrames, err := first.QueryStreamHistoryBounded(ctx, locStream, notAfterMs)
		Expect(err).NotTo(HaveOccurred(), "the first session MUST be able to read its own location history")
		Expect(frameIDs(liveFrames)).To(ContainElement(plantedID),
			"precondition: the planted event MUST be readable by the session that was present "+
				"for it, or its later absence would prove nothing about the history floor")

		// Take the session all the way through expiry: detached, past its expiry,
		// then genuinely swept by the real reaper — not merely marked.
		detachAndExpectDetached(ctx, ts, first)
		ts.DetachAndExpireSession(ctx, firstID, time.Now().Add(-time.Minute))
		reapSessionAndExpectRowGone(ctx, ts, firstID)

		// Log in again as the SAME character.
		second := player.OpenWebSession(ctx)
		DeferCleanup(func() { second.Logout(ctx) })

		Expect(second.Reattached).To(BeFalse(),
			"with the row swept there is nothing left to reattach to — SelectCharacter MUST take "+
				"its create branch")
		Expect(second.SessionID).NotTo(Equal(firstID),
			"a re-login after expiry MUST mint a NEW session, not resurrect the reaped one")
		Expect(second.LocationArrivedAt).To(BeTemporally(">", firstArrival),
			"INV-PRIVACY-1: the new session MUST carry a LATER arrival floor")

		// The planted event sits strictly between the two floors. Asserted rather
		// than assumed: if wall-clock ordering ever collapsed these instants
		// together, the exclusion below would hold for the wrong reason, and this
		// spec would pass while proving nothing. It fails here instead.
		Expect(plantedAt).To(BeTemporally(">", firstArrival),
			"the planted event MUST sit above the expired session's floor")
		Expect(plantedAt).To(BeTemporally("<", second.LocationArrivedAt),
			"the planted event MUST sit below the new session's floor, so that inheriting the "+
				"old floor is the only way it could still be readable")

		// A second event, this one ABOVE the new session's floor. It is what makes
		// the exclusion below attributable: a read that returned nothing at all —
		// because it was broken, denied, or drained empty — would satisfy a bare
		// "does not contain the planted event" assertion while proving nothing.
		// Both events are read back in ONE query, so the floor is demonstrably
		// what separates them.
		afterAt := second.LocationArrivedAt.Add(time.Millisecond)
		afterID, err := second.EmitDirectEventAt(ctx, locStream,
			"core-communication:pose", relogPayload(second.CharacterName), afterAt)
		Expect(err).NotTo(HaveOccurred(), "the post-relogin emit MUST publish")

		freshFrames, err := second.QueryStreamHistoryBounded(ctx, locStream, notAfterMs)
		Expect(err).NotTo(HaveOccurred(), "the new session MUST be able to query its location history")
		freshIDs := frameIDs(freshFrames)

		Expect(freshIDs).To(ContainElement(afterID),
			"positive control: the new session's read MUST return the event emitted at %s, above "+
				"its own floor of %s — without this, the exclusion asserted next could be "+
				"satisfied by a read that returns nothing at all",
			afterAt, second.LocationArrivedAt)
		Expect(freshIDs).NotTo(ContainElement(plantedID),
			"INV-PRIVACY-1: the event emitted at %s, before the new session arrived at %s, MUST NOT "+
				"be readable by it — its presence means the re-login inherited the expired "+
				"session's history floor and leaked history the returning player was not present for",
			plantedAt, second.LocationArrivedAt)
	})

	// matrix-row: post-ttl-relogin.telnet
	//
	// The same INV-PRIVACY-1 question for a telnet-typed session. The transport
	// identity is asserted through the column production writes
	// (session_connections.client_type), never through the argument handed to
	// OpenTelnetSession; see this file's TELNET SCOPE note for what that does and
	// does not cover.
	It("mints a new telnet session whose history floor excludes an event the expired session could read", func() {
		player := ts.AuthedPlayer(ctx, "RejoinTorvald")

		first := player.OpenTelnetSession(ctx)
		firstID := first.SessionID
		firstArrival := first.LocationArrivedAt
		locStream := "location." + first.LocationID.String()

		Expect(connectionClientTypes(ctx, ts, firstID)).To(HaveEach("telnet"),
			"precondition: the session that expires MUST be the telnet-typed one")

		plantedAt := firstArrival.Add(time.Millisecond)
		plantedID, err := first.EmitDirectEventAt(ctx, locStream,
			"core-communication:pose", relogPayload(first.CharacterName), plantedAt)
		Expect(err).NotTo(HaveOccurred(), "the timestamped emit MUST publish")

		notAfterMs := time.Now().Add(time.Hour).UnixMilli()

		liveFrames, err := first.QueryStreamHistoryBounded(ctx, locStream, notAfterMs)
		Expect(err).NotTo(HaveOccurred(), "the first telnet session MUST read its own location history")
		Expect(frameIDs(liveFrames)).To(ContainElement(plantedID),
			"precondition: the planted event MUST be readable by the session that was present for it")

		detachAndExpectDetached(ctx, ts, first)
		ts.DetachAndExpireSession(ctx, firstID, time.Now().Add(-time.Minute))
		reapSessionAndExpectRowGone(ctx, ts, firstID)

		second := player.OpenTelnetSession(ctx)
		DeferCleanup(func() { second.Logout(ctx) })

		Expect(second.Reattached).To(BeFalse(),
			"with the row swept there is nothing left to reattach to")
		Expect(second.SessionID).NotTo(Equal(firstID),
			"a telnet re-login after expiry MUST mint a NEW session")
		Expect(second.LocationArrivedAt).To(BeTemporally(">", firstArrival),
			"INV-PRIVACY-1: the new session MUST carry a LATER arrival floor")
		Expect(connectionClientTypes(ctx, ts, second.SessionID)).To(HaveEach("telnet"),
			"the fresh session's connection MUST be telnet-typed; the roster counts by "+
				"client_type, so a silent change of transport identity changes visibility")

		Expect(plantedAt).To(BeTemporally(">", firstArrival),
			"the planted event MUST sit above the expired session's floor")
		Expect(plantedAt).To(BeTemporally("<", second.LocationArrivedAt),
			"the planted event MUST sit below the new session's floor")

		// Positive control on the fresh read — see the web-char spec for why a
		// bare absence assertion would be satisfied by an empty read.
		afterAt := second.LocationArrivedAt.Add(time.Millisecond)
		afterID, err := second.EmitDirectEventAt(ctx, locStream,
			"core-communication:pose", relogPayload(second.CharacterName), afterAt)
		Expect(err).NotTo(HaveOccurred(), "the post-relogin emit MUST publish")

		freshFrames, err := second.QueryStreamHistoryBounded(ctx, locStream, notAfterMs)
		Expect(err).NotTo(HaveOccurred(), "the new telnet session MUST query its location history")
		freshIDs := frameIDs(freshFrames)

		Expect(freshIDs).To(ContainElement(afterID),
			"positive control: the new session's read MUST return the event emitted at %s, above "+
				"its own floor of %s",
			afterAt, second.LocationArrivedAt)
		Expect(freshIDs).NotTo(ContainElement(plantedID),
			"INV-PRIVACY-1: the event emitted at %s, before the new session arrived at %s, MUST NOT "+
				"be readable by it — its presence means the re-login inherited the expired "+
				"session's history floor",
			plantedAt, second.LocationArrivedAt)
	})
})
