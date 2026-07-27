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

// Session-lifecycle matrix — the connect and reconnect transitions.
//
// Each spec below carries a `// matrix-row: <id>` marker naming the cell of
// test/session-matrix.yaml it satisfies. The registry is the division of
// labour: rows this file does not claim belong to a later plan, and a marker
// with no row (or a row with no marker) fails the bijection meta-test.
//
// TELNET SCOPE. The telnet cells assert SESSION STATE for a session whose
// connection row carries client_type='telnet'. There is no telnet gateway in
// the loop and internal/telnet is not exercised here — the session is opened
// through the same SelectCharacter RPC as a web session, and the transport
// identity is observable only as the column the production Subscribe handler
// stamps on session_connections. Nothing below is a claim about telnet
// PROTOCOL behaviour.
//
// PRIVACY SPEC IDENTIFIERS. The arrival-timestamp assertions in the reattach
// specs are the by-identifier form of I-PRIV-3 / INV-PRIVACY-3 ("Subscribe
// .ReattachCAS and SelectCharacter reattach leave LocationArrivedAt
// UNCHANGED"). They are cited in prose and deliberately carry NO binding
// annotation: that invariant also covers the durable consumer's
// DeliverPolicy / OptStartTime, which these specs do not touch, so annotating
// them would claim a whole invariant on a partial assertion. It is already
// bound by test/integration/privacy/privacy_test.go, which does assert both
// halves.

// connectionClientTypes reads the client_type column of every
// session_connections row belonging to sessionID.
//
// This is the observation point for every telnet cell: production writes the
// column, so reading it back is what distinguishes a genuinely telnet-typed
// session from one that is telnet only in the test's own intent. A spec
// phrased against the argument handed to OpenTelnetSession would prove
// nothing. Keyed by session id, never a table-wide read, because the suite is
// shared and a concurrently created session would otherwise pollute the result.
func connectionClientTypes(ctx context.Context, ts *integrationtest.Server, sessionID string) []string {
	GinkgoHelper()
	rows, err := ts.Pool().Query(ctx,
		`SELECT client_type FROM session_connections WHERE session_id = $1`, sessionID)
	Expect(err).NotTo(HaveOccurred(), "query session_connections for session %s", sessionID)
	defer rows.Close()

	var out []string
	for rows.Next() {
		var clientType string
		Expect(rows.Scan(&clientType)).To(Succeed(), "scan client_type for session %s", sessionID)
		out = append(out, clientType)
	}
	Expect(rows.Err()).NotTo(HaveOccurred(), "iterate session_connections for session %s", sessionID)
	return out
}

var _ = Describe("Fresh character selection to an active session", func() {
	var (
		ctx context.Context
		ts  *integrationtest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		ts = lifecycleHarness()
	})

	// matrix-row: fresh-select.web-guest
	It("creates an active session for a guest character carrying a location arrival timestamp", func() {
		sess := ts.ConnectGuest(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		info, err := ts.SessionStore().Get(ctx, sess.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the freshly created guest session row MUST be readable")

		Expect(info.ID).To(Equal(sess.SessionID),
			"the persisted row MUST be the session SelectCharacter returned")
		Expect(info.Status).To(Equal(session.StatusActive),
			"a fresh selection MUST leave the session active")
		// The guest signal on a game session is GuestCharacterCreatedAt, NOT
		// Info.IsGuest. SelectCharacter deliberately leaves IsGuest false
		// (internal/grpc/auth_handlers.go:291-296): Disconnect reads that flag
		// to delete the session immediately, which would break page-reload
		// reattach. The non-zero guest floor is what streamScopeFloor uses.
		//
		// Asserted against the session's own creation time rather than with
		// BeZero: the column round-trips as the UNIX EPOCH when unset, not as
		// Go's zero time, so `NotTo(BeZero())` would pass on an unset floor and
		// prove nothing. Comparing to CreatedAt pins a real floor.
		Expect(info.GuestCharacterCreatedAt).To(BeTemporally("~", info.CreatedAt, time.Minute),
			"INV-PRIVACY-2: a guest session MUST carry a real guest identity floor, set to "+
				"the guest character's creation time")
		Expect(info.CharacterID).To(Equal(sess.CharacterID),
			"the session MUST be bound to the character that was selected")
		Expect(info.LocationArrivedAt).NotTo(BeZero(),
			"INV-PRIVACY-1: a fresh session MUST carry a location arrival timestamp, "+
				"which is the floor every later history query is filtered against")
	})

	// matrix-row: fresh-select.web-char
	It("creates an active session for a registered player's character without reattaching", func() {
		player := ts.AuthedPlayer(ctx, "FreshFinn")
		sess := player.OpenWebSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		Expect(sess.Reattached).To(BeFalse(),
			"the first selection MUST take SelectCharacter's create branch, not its reattach branch")

		info, err := ts.SessionStore().Get(ctx, sess.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the freshly created session row MUST be readable")

		Expect(info.Status).To(Equal(session.StatusActive),
			"a fresh selection MUST leave the session active")
		// The counterpart of the guest spec's assertion: a registered player's
		// session carries no guest identity floor, so streamScopeFloor applies
		// no guest overlay to it.
		// Unset is the UNIX EPOCH here, not Go's zero time — see the guest spec.
		Expect(info.GuestCharacterCreatedAt.Unix()).To(BeZero(),
			"INV-PRIVACY-2: a registered player's session MUST NOT carry a guest identity floor")
		Expect(info.CharacterID).To(Equal(player.CharacterID),
			"the session MUST be bound to the character that was selected")
		Expect(info.LocationArrivedAt).NotTo(BeZero(),
			"INV-PRIVACY-1: a fresh session MUST carry a location arrival timestamp")
	})

	// matrix-row: fresh-select.telnet
	It("creates an active session whose connection row records the telnet client type", func() {
		player := ts.AuthedPlayer(ctx, "FreshTamsin")
		sess := player.OpenTelnetSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		info, err := ts.SessionStore().Get(ctx, sess.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the freshly created telnet session row MUST be readable")
		Expect(info.Status).To(Equal(session.StatusActive),
			"a fresh selection MUST leave the session active regardless of transport")
		Expect(info.LocationArrivedAt).NotTo(BeZero(),
			"INV-PRIVACY-1: a fresh session MUST carry a location arrival timestamp")

		// Read the transport identity back out of Postgres. The grid-presence
		// roster counts connections BY client_type, so this column decides who
		// is visible to whom.
		types := connectionClientTypes(ctx, ts, sess.SessionID)
		Expect(types).NotTo(BeEmpty(),
			"the attach MUST have registered a connection row for this session")
		Expect(types).To(HaveEach("telnet"),
			"every connection of a telnet session MUST carry client_type=telnet as written by "+
				"the production Subscribe handler")
	})
})

var _ = Describe("Reattach within TTL through the character-selection path", func() {
	var (
		ctx context.Context
		ts  *integrationtest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		ts = lifecycleHarness()
	})

	// matrix-row: reattach-select.web-guest
	//
	// The cell this spec covers was blocked until Server.GuestPlayer existed,
	// and the block was real rather than fussy: Server.ConnectGuest mints a NEW
	// guest player and character on every call, so the only stand-in available
	// was "connect a second guest" — which satisfies every assertion about
	// identifiers and timestamps trivially, without any reattach having
	// happened. GuestPlayer re-enters the production SelectCharacter path with
	// the SAME guest's bearer token instead, so the reattach branch is genuinely
	// taken. The character-identity assertion below is what pins that: a second
	// guest would fail it.
	It("returns the same guest session with its arrival and guest identity floors unchanged", func() {
		guest := ts.GuestPlayer(ctx)

		first := guest.OpenWebSession(ctx)
		originalID := first.SessionID
		originalArrival := first.LocationArrivedAt
		Expect(first.Reattached).To(BeFalse(), "precondition: the first selection creates the session")

		firstInfo, err := ts.SessionStore().Get(ctx, originalID)
		Expect(err).NotTo(HaveOccurred(), "the guest session row MUST be readable")
		// Unset round-trips as the UNIX EPOCH here, not Go's zero time, so
		// NotTo(BeZero()) would pass on an unset floor. Comparing to the
		// session's own creation instant pins a real one.
		Expect(firstInfo.GuestCharacterCreatedAt).To(BeTemporally("~", firstInfo.CreatedAt, time.Minute),
			"precondition: INV-PRIVACY-2 — this MUST genuinely be a guest session, or the guest "+
				"column is covered by a registered player under a guest-sounding name")

		first.DetachTransport(ctx)
		detached, err := ts.SessionStore().Get(ctx, originalID)
		Expect(err).NotTo(HaveOccurred(), "the detached guest session row MUST survive its TTL window")
		Expect(detached.Status).To(Equal(session.StatusDetached),
			"precondition: dropping the transport MUST detach rather than delete the session")

		second := guest.OpenWebSession(ctx)
		DeferCleanup(func() { second.Logout(ctx) })

		Expect(second.Reattached).To(BeTrue(),
			"the second selection MUST take SelectCharacter's reattach branch")
		Expect(second.SessionID).To(Equal(originalID),
			"a guest reattach within the TTL MUST resume the SAME session, never mint a second one")
		Expect(second.CharacterID).To(Equal(guest.CharacterID),
			"the returning guest MUST resume its OWN character — this is the assertion a "+
				"second-guest stand-in could not satisfy, and the reason this cell waited for a "+
				"seam that re-selects the same guest")

		info, err := ts.SessionStore().Get(ctx, originalID)
		Expect(err).NotTo(HaveOccurred(), "the reattached guest session row MUST be readable")
		Expect(info.Status).To(Equal(session.StatusActive),
			"reattaching MUST return the session to active")
		Expect(info.LocationArrivedAt).To(BeTemporally("==", originalArrival),
			"I-PRIV-3 / INV-PRIVACY-3: reattach MUST leave the location arrival timestamp unchanged")
		Expect(info.GuestCharacterCreatedAt).To(BeTemporally("==", firstInfo.GuestCharacterCreatedAt),
			"INV-PRIVACY-2: the guest identity floor MUST survive the reattach unchanged — a "+
				"moved floor would mean the session was rebound to a different guest identity")
	})

	// matrix-row: reattach-select.web-char
	It("returns the same session identifier with the location arrival timestamp unchanged", func() {
		player := ts.AuthedPlayer(ctx, "ReselectRhea")

		first := player.OpenWebSession(ctx)
		originalID := first.SessionID
		originalArrival := first.LocationArrivedAt
		Expect(first.Reattached).To(BeFalse(), "precondition: the first selection creates the session")

		// Drop the transport so the session is detached and holding its TTL
		// window open — the state this row's "within TTL" is measured against.
		first.DetachTransport(ctx)
		detached, err := ts.SessionStore().Get(ctx, originalID)
		Expect(err).NotTo(HaveOccurred(), "the detached session row MUST survive its TTL window")
		Expect(detached.Status).To(Equal(session.StatusDetached),
			"precondition: dropping the transport MUST detach rather than delete the session")

		// Re-select the same character within the TTL: SelectCharacter's
		// reattach branch matches the detached row rather than creating a new one.
		second := player.OpenWebSession(ctx)
		DeferCleanup(func() { second.Logout(ctx) })

		Expect(second.Reattached).To(BeTrue(),
			"the second selection MUST take SelectCharacter's reattach branch")
		Expect(second.SessionID).To(Equal(originalID),
			"a reattach within the TTL MUST resume the SAME session, never mint a second one")

		info, err := ts.SessionStore().Get(ctx, originalID)
		Expect(err).NotTo(HaveOccurred(), "the reattached session row MUST be readable")
		Expect(info.Status).To(Equal(session.StatusActive),
			"reattaching MUST return the session to active")
		Expect(info.LocationArrivedAt).To(BeTemporally("==", originalArrival),
			"I-PRIV-3 / INV-PRIVACY-3: reattach MUST leave the location arrival timestamp "+
				"unchanged, or a returning player would silently gain or lose history")
	})

	// matrix-row: reattach-select.telnet
	It("returns the same session for a telnet client and records a telnet type on the new connection", func() {
		player := ts.AuthedPlayer(ctx, "ReselectTansy")

		first := player.OpenTelnetSession(ctx)
		originalID := first.SessionID
		originalArrival := first.LocationArrivedAt

		first.DetachTransport(ctx)
		detached, err := ts.SessionStore().Get(ctx, originalID)
		Expect(err).NotTo(HaveOccurred(), "the detached telnet session row MUST survive its TTL window")
		Expect(detached.Status).To(Equal(session.StatusDetached),
			"precondition: dropping the transport MUST detach rather than delete the session")

		second := player.OpenTelnetSession(ctx)
		DeferCleanup(func() { second.Logout(ctx) })

		Expect(second.Reattached).To(BeTrue(),
			"the second selection MUST take SelectCharacter's reattach branch")
		Expect(second.SessionID).To(Equal(originalID),
			"a telnet reattach within the TTL MUST resume the SAME session")

		info, err := ts.SessionStore().Get(ctx, originalID)
		Expect(err).NotTo(HaveOccurred(), "the reattached telnet session row MUST be readable")
		Expect(info.Status).To(Equal(session.StatusActive),
			"reattaching MUST return the session to active")
		Expect(info.LocationArrivedAt).To(BeTemporally("==", originalArrival),
			"I-PRIV-3 / INV-PRIVACY-3: reattach MUST leave the location arrival timestamp unchanged")

		types := connectionClientTypes(ctx, ts, originalID)
		Expect(types).NotTo(BeEmpty(), "the reattach MUST have registered a connection row")
		Expect(types).To(HaveEach("telnet"),
			"the transport identity MUST survive the reattach — a session that came back as "+
				"'terminal' would silently change who can see it on the grid roster")
	})
})

var _ = Describe("Reattach within TTL through the subscribe compare-and-swap path", func() {
	var (
		ctx context.Context
		ts  *integrationtest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		ts = lifecycleHarness()
	})

	// matrix-row: reattach-cas.web-guest
	//
	// test/integration/presence/reattach_presence_test.go drives the same
	// sequence for a guest, but its subject is grid_present (I-LIVE-3) and it
	// asserts neither session identity nor arrival-timestamp preservation.
	// This spec asserts those, which is why the registry does not treat that
	// one as covering this cell.
	It("returns a detached guest session to active without moving its location arrival timestamp", func() {
		sess := ts.ConnectGuest(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		originalID := sess.SessionID
		originalArrival := sess.LocationArrivedAt
		Expect(originalArrival).NotTo(BeZero(), "precondition: the guest session has an arrival floor")

		sess.DetachTransport(ctx)
		detached, err := ts.SessionStore().Get(ctx, originalID)
		Expect(err).NotTo(HaveOccurred(), "the detached guest session row MUST survive its TTL window")
		Expect(detached.Status).To(Equal(session.StatusDetached),
			"precondition: dropping the transport MUST detach rather than delete the session")

		// Production's Subscribe handler runs ReattachCAS when it finds the
		// session detached; attach blocks until REPLAY_COMPLETE, so the
		// compare-and-swap has already run by the time this returns.
		sess.ReattachTransport(ctx)

		info, err := ts.SessionStore().Get(ctx, originalID)
		Expect(err).NotTo(HaveOccurred(), "the reattached guest session row MUST be readable")
		Expect(info.ID).To(Equal(originalID),
			"the compare-and-swap MUST resume the same session row, not create a second one")
		Expect(info.Status).To(Equal(session.StatusActive),
			"ReattachCAS MUST flip the session from detached back to active")
		Expect(info.LocationArrivedAt).To(BeTemporally("==", originalArrival),
			"I-PRIV-3 / INV-PRIVACY-3: the subscribe reattach path MUST leave the location "+
				"arrival timestamp unchanged")
	})

	// matrix-row: reattach-cas.web-char
	It("returns a detached registered character's session to active on the same session row", func() {
		player := ts.AuthedPlayer(ctx, "CasCallum")
		sess := player.OpenWebSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		originalID := sess.SessionID
		originalArrival := sess.LocationArrivedAt

		sess.DetachTransport(ctx)
		detached, err := ts.SessionStore().Get(ctx, originalID)
		Expect(err).NotTo(HaveOccurred(), "the detached session row MUST survive its TTL window")
		Expect(detached.Status).To(Equal(session.StatusDetached),
			"precondition: dropping the transport MUST detach rather than delete the session")

		sess.ReattachTransport(ctx)

		info, err := ts.SessionStore().Get(ctx, originalID)
		Expect(err).NotTo(HaveOccurred(), "the reattached session row MUST be readable")
		Expect(info.ID).To(Equal(originalID),
			"the compare-and-swap MUST resume the same session row")
		Expect(info.CharacterID).To(Equal(player.CharacterID),
			"the resumed session MUST still be bound to the same character — a reattach that "+
				"rebound the connection to another character is the spoofing case this row guards")
		Expect(info.Status).To(Equal(session.StatusActive),
			"ReattachCAS MUST flip the session from detached back to active")
		Expect(info.LocationArrivedAt).To(BeTemporally("==", originalArrival),
			"I-PRIV-3 / INV-PRIVACY-3: the subscribe reattach path MUST leave the location "+
				"arrival timestamp unchanged")
	})

	// matrix-row: reattach-cas.telnet
	It("returns a detached telnet session to active with a fresh telnet-typed connection row", func() {
		player := ts.AuthedPlayer(ctx, "CasTeagan")
		sess := player.OpenTelnetSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		originalID := sess.SessionID
		originalArrival := sess.LocationArrivedAt

		sess.DetachTransport(ctx)
		detached, err := ts.SessionStore().Get(ctx, originalID)
		Expect(err).NotTo(HaveOccurred(), "the detached telnet session row MUST survive its TTL window")
		Expect(detached.Status).To(Equal(session.StatusDetached),
			"precondition: dropping the transport MUST detach rather than delete the session")

		sess.ReattachTransport(ctx)

		info, err := ts.SessionStore().Get(ctx, originalID)
		Expect(err).NotTo(HaveOccurred(), "the reattached telnet session row MUST be readable")
		Expect(info.ID).To(Equal(originalID),
			"the compare-and-swap MUST resume the same session row")
		Expect(info.Status).To(Equal(session.StatusActive),
			"ReattachCAS MUST flip the session from detached back to active")
		Expect(info.LocationArrivedAt).To(BeTemporally("==", originalArrival),
			"I-PRIV-3 / INV-PRIVACY-3: the subscribe reattach path MUST leave the location "+
				"arrival timestamp unchanged")

		types := connectionClientTypes(ctx, ts, originalID)
		Expect(types).NotTo(BeEmpty(), "the reattach MUST have registered a connection row")
		Expect(types).To(HaveEach("telnet"),
			"the reattached connection MUST still be telnet-typed; the roster counts by "+
				"client_type, so a silent change of transport identity changes visibility")
	})
})
