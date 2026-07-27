// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package session_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// Session-lifecycle matrix — the DELIBERATE-TERMINATION transitions.
//
// Each spec carries a `// matrix-row: <id>` marker naming the cell of
// test/session-matrix.yaml it satisfies. A marker with no row, or a row with no
// marker, fails the bijection meta-test.
//
// # What these rows are FOR: termination is not detach
//
// The matrix separates ending a session from merely losing its connections
// because the two look alike from the client and are completely different in
// the store. Dropping every connection leaves the session row in place, marked
// detached, holding its time-to-live open so a returning player resumes it —
// that chain is asserted in lifecycle_ttl_test.go. Quitting and logging out
// must instead REMOVE the row, immediately.
//
// The difference is observable and it matters: the grid-presence roster reads
// live session rows, so a "quit" that only detached would leave the character
// apparently present until the reaper ran. Every spec below therefore asserts a
// KEYED not-found lookup on its own session identifier, taken straight after
// the transition, with NO expiry seam applied and NO reaper driven. A spec that
// asserted only a status change, or that counted rows, would pass against a
// detach-and-expire implementation and prove nothing.
//
// # Two different mechanisms, deliberately not blurred
//
//   - quit is a COMMAND, dispatched through Session.SendCommand into the
//     production HandleCommand path. QuitHandler returns ErrSessionEnded and the
//     server does the teardown: leave event, session_ended (cause quit), row
//     delete, disconnect hooks (internal/grpc/command_handler.go:267-289).
//   - logout is NOT a command. It is the Logout RPC on the player session,
//     wrapped by Session.Logout, which fans out over every game session the
//     player session owns and deletes each one
//     (internal/grpc/auth_handlers.go:673-706).
//
// The rows are distinct because the mechanisms are: one ends a game session
// from inside it, the other revokes the player-session credential above it.
//
// # SendCommand's return value proves NOTHING on its own
//
// An unknown command is a USER-FACING error, so HandleCommand emits a
// command_response and still answers Success=true
// (internal/grpc/command_handler.go:291-302). Session.SendCommand therefore
// returns NO error for a command that was never registered. `Expect(SendCommand
// ...).To(Succeed())` passes against an EMPTY command registry, which is the
// harness default. It is kept below only as an RPC-level guard; the
// recognised-command outcome is the deleted row, which an unregistered quit
// cannot produce.
//
// # TELNET SCOPE
//
// The telnet cells assert SESSION STATE for a session whose connection row
// carries client_type='telnet'. There is no telnet gateway in the loop and
// internal/telnet is NOT exercised. Nothing here is a claim about telnet
// PROTOCOL behaviour.
//
// # The administrator-boot row is NOT here, and the reason is specific
//
// admin-boot.{web-char,telnet,multi-session} carry
// not-implementable-from-harness-defaults in the registry, and this file
// deliberately writes no spec for them. That is NOT because the tree lacks an
// administrator session-boot capability — it has one. `resetpassword --kick`
// (internal/command/handlers/resetpassword.go:197-218) is a real,
// capability-gated (capSessionKick, resetpassword.go:35) admin path that
// deletes the session rows of every character belonging to the target player.
// It falls short on two independent counts: it calls DeleteByCharacter, a raw
// DELETE that emits nothing, so no session_ended reaches subscribers and no
// disconnect hooks fire (issue #4862); and it is unreachable from harness
// defaults because RegisterAdmin panics on five dependencies the harness does
// not wire. An approximating spec — deleting the row directly, or standing
// logout in for a boot — would go green on semantics the system does not
// deliver, which is the exact artifact this matrix exists to expose.

// expectSessionRowGone asserts that sessionID has no row in the session store.
//
// Keyed on the identifier, never a count: this suite shares one harness, so a
// row count could be satisfied (or spoiled) by a concurrently created session.
// The coded error is asserted too, so "the row is absent" cannot be confused
// with "the store failed for some other reason".
func expectSessionRowGone(ctx context.Context, ts *integrationtest.Server, sessionID string) {
	GinkgoHelper()
	_, err := ts.SessionStore().Get(ctx, sessionID)
	Expect(err).To(HaveOccurred(),
		"session %s MUST be gone; a surviving row means the transition detached the session "+
			"rather than ending it, leaving the character on the grid roster until the reaper ran",
		sessionID)
	oopsErr, ok := oops.AsOops(err)
	Expect(ok).To(BeTrue(), "a missing session MUST surface as a coded error, got %T: %v", err, err)
	Expect(oopsErr.Code()).To(Equal("SESSION_NOT_FOUND"),
		"the row MUST be absent, not merely unreadable for some other reason")
}

// expectActiveSessionRow reads sessionID back and asserts it is active. Every
// termination spec runs this FIRST: without it, a not-found assertion after the
// transition could be satisfied by a session that never existed.
func expectActiveSessionRow(ctx context.Context, ts *integrationtest.Server, sessionID string) *session.Info {
	GinkgoHelper()
	info, err := ts.SessionStore().Get(ctx, sessionID)
	Expect(err).NotTo(HaveOccurred(),
		"precondition: session %s MUST exist before the termination under test", sessionID)
	Expect(info.Status).To(Equal(session.StatusActive),
		"precondition: the session MUST be active before it is terminated")
	return info
}

var _ = Describe("The quit command ends the session rather than detaching it", func() {
	var (
		ctx context.Context
		ts  *integrationtest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		ts = lifecycleHarness()
	})

	// matrix-row: quit-command.web-guest
	It("removes a guest session row immediately rather than leaving it detached to expire", func() {
		sess := ts.ConnectGuest(ctx)
		// Safe after quit: quit ends the GAME session only; the player-session
		// credential survives, so Logout still tears the transport down and
		// revokes the token. Its session fan-out simply finds nothing to delete.
		DeferCleanup(func() { sess.Logout(ctx) })

		expectActiveSessionRow(ctx, ts, sess.SessionID)

		// RPC-level guard only — see the file comment: this Succeed() also
		// passes when quit is unregistered.
		Expect(sess.SendCommand(ctx, "quit")).To(Succeed(),
			"dispatching quit MUST NOT fail at the RPC level")

		// The load-bearing assertion, and the recognised-command outcome: an
		// unregistered quit falls through as unknown and leaves the row intact.
		// No expiry seam is applied and no reaper is driven, so this also fails
		// against an implementation that merely detached the session.
		expectSessionRowGone(ctx, ts, sess.SessionID)
	})

	// matrix-row: quit-command.web-char
	It("removes a registered player's session row immediately, leaving nothing for the reaper", func() {
		player := ts.AuthedPlayer(ctx, "QuitQuilla")
		sess := player.OpenWebSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		info := expectActiveSessionRow(ctx, ts, sess.SessionID)
		Expect(info.CharacterID).To(Equal(player.CharacterID),
			"precondition: the session MUST be bound to the character that quits")

		Expect(sess.SendCommand(ctx, "quit")).To(Succeed(),
			"dispatching quit MUST NOT fail at the RPC level")

		expectSessionRowGone(ctx, ts, sess.SessionID)
	})

	// matrix-row: quit-command.telnet
	It("removes a telnet session and its connection rows immediately", func() {
		player := ts.AuthedPlayer(ctx, "QuitTarquin")
		sess := player.OpenTelnetSession(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		expectActiveSessionRow(ctx, ts, sess.SessionID)

		// The transport identity is read back from Postgres BEFORE the
		// termination, because session_connections rows CASCADE away with the
		// session. Without this the spec would be a second web-char spec under
		// a telnet-sounding name.
		Expect(connectionClientTypes(ctx, ts, sess.SessionID)).To(HaveEach("telnet"),
			"precondition: this session's connection rows MUST carry client_type=telnet, "+
				"as stamped by the production Subscribe handler")

		Expect(sess.SendCommand(ctx, "quit")).To(Succeed(),
			"dispatching quit MUST NOT fail at the RPC level")

		expectSessionRowGone(ctx, ts, sess.SessionID)
		// The roster counts connections by client_type, so a connection row
		// outliving its session would keep a quit character visible.
		Expect(connectionClientTypes(ctx, ts, sess.SessionID)).To(BeEmpty(),
			"ending the session MUST take its connection rows with it")
	})
})

var _ = Describe("Explicit logout deletes the game sessions the player session owns", func() {
	var (
		ctx context.Context
		ts  *integrationtest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		ts = lifecycleHarness()
	})

	// logoutOnce runs the production Logout RPC through Session.Logout and
	// returns a cleanup that will NOT repeat it.
	//
	// The repeat matters: auth.Service.Logout resolves the token before
	// deleting it and returns SESSION_NOT_FOUND when it is already gone
	// (internal/auth/auth_service.go:274-285), which Session.Logout turns into
	// a require.NoError failure. So a spec that both logs out and registers the
	// usual DeferCleanup(sess.Logout) would fail in teardown. The guard keeps
	// the cleanup useful when the spec fails BEFORE reaching its logout.
	logoutOnce := func(sess *integrationtest.Session) func() {
		done := false
		DeferCleanup(func() {
			if !done {
				sess.Logout(ctx)
			}
		})
		return func() {
			sess.Logout(ctx)
			done = true
		}
	}

	// matrix-row: explicit-logout.web-guest
	It("removes a guest session row immediately when the player session is logged out", func() {
		sess := ts.ConnectGuest(ctx)
		logout := logoutOnce(sess)

		expectActiveSessionRow(ctx, ts, sess.SessionID)

		logout()

		expectSessionRowGone(ctx, ts, sess.SessionID)
	})

	// matrix-row: explicit-logout.web-char
	It("removes a registered player's session row immediately on logout", func() {
		player := ts.AuthedPlayer(ctx, "LogoutLorcan")
		sess := player.OpenWebSession(ctx)
		logout := logoutOnce(sess)

		info := expectActiveSessionRow(ctx, ts, sess.SessionID)
		Expect(info.CharacterID).To(Equal(player.CharacterID),
			"precondition: the session MUST be bound to the character being logged out")

		logout()

		expectSessionRowGone(ctx, ts, sess.SessionID)
	})

	// matrix-row: explicit-logout.telnet
	It("removes a telnet session and its connection rows immediately on logout", func() {
		player := ts.AuthedPlayer(ctx, "LogoutTamlin")
		sess := player.OpenTelnetSession(ctx)
		logout := logoutOnce(sess)

		expectActiveSessionRow(ctx, ts, sess.SessionID)
		Expect(connectionClientTypes(ctx, ts, sess.SessionID)).To(HaveEach("telnet"),
			"precondition: this session's connection rows MUST carry client_type=telnet")

		logout()

		expectSessionRowGone(ctx, ts, sess.SessionID)
		Expect(connectionClientTypes(ctx, ts, sess.SessionID)).To(BeEmpty(),
			"logging out MUST take the session's connection rows with it")
	})
})

// connectionIDs reads the primary keys of every session_connections row
// belonging to sessionID.
//
// The tmux-style reattach row turns on the connection being genuinely NEW while
// the session is genuinely the SAME. connectionClientTypes answers the transport
// half; this answers the identity half, so "a new connection resumed the
// session" is asserted rather than assumed from having called the opener twice.
func connectionIDs(ctx context.Context, ts *integrationtest.Server, sessionID string) []string {
	GinkgoHelper()
	rows, err := ts.Pool().Query(ctx,
		`SELECT id FROM session_connections WHERE session_id = $1`, sessionID)
	Expect(err).NotTo(HaveOccurred(), "query session_connections ids for session %s", sessionID)
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		Expect(rows.Scan(&id)).To(Succeed(), "scan connection id for session %s", sessionID)
		out = append(out, id)
	}
	Expect(rows.Err()).NotTo(HaveOccurred(), "iterate session_connections for session %s", sessionID)
	return out
}

// countSessionsForPlayerSession counts sessions rows owned by playerSessionID,
// across EVERY status.
//
// Across every status deliberately. The partial unique index
// idx_sessions_active_character (internal/store/migrations/000001_baseline.up.sql:221)
// already forbids two rows for one character while either is active or
// detached, so a count restricted to those statuses could not rise above one
// and the assertion would be decorative. Counting all statuses leaves the
// "old row parked in some other status, new row minted alongside it" shape
// detectable, which is the failure this row cares about.
func countSessionsForPlayerSession(ctx context.Context, ts *integrationtest.Server, playerSessionID string) int {
	GinkgoHelper()
	var n int
	Expect(ts.Pool().QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE player_session_id = $1`, playerSessionID).Scan(&n)).
		To(Succeed(), "count sessions for player session %s", playerSessionID)
	return n
}

// Session-lifecycle matrix — the tmux-style telnet reattach row.
//
// # Why this telnet spec lives in the session suite
//
// test/integration/telnet/ contains exactly one file, telnet_suite_test.go, and
// it is a bare Ginkgo suite bootstrap with no specs and no harness wiring.
// Standing a harness up there for this single spec would duplicate the
// suite-scoped stack this phase already built next door (lifecycleHarness), and
// would leave two places to keep in step. So the cell lives here, and the
// registry row says so too — a future reader looking for telnet lifecycle
// coverage in the telnet directory is pointed at this file from both ends.
//
// # What this row asserts, and what it does NOT
//
// It asserts SESSION STATE: that a second telnet connection arriving under an
// already-authenticated player session RESUMES the existing game session
// instead of minting a second one, and that the resumed session's connection
// row still records the telnet client type. It is NOT a claim about telnet
// PROTOCOL behaviour — no telnet gateway is in the loop and internal/telnet is
// never entered. The transport identity is real all the same: it is the column
// the production Subscribe handler stamps on session_connections
// (internal/grpc/subscribe_handler.go:358-364), validated against the store's
// allowlist (internal/store/session_store.go:519-528), and the grid-presence
// roster counts connections by it — so it decides who is visible to whom.
var _ = Describe("Tmux-style telnet reattach under one player session", func() {
	var (
		ctx context.Context
		ts  *integrationtest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		ts = lifecycleHarness()
	})

	// matrix-row: telnet-tmux-reattach.telnet
	It("resumes the one existing game session when a second telnet connection arrives", func() {
		player := ts.AuthedPlayer(ctx, "TmuxTeagan")

		first := player.OpenTelnetSession(ctx)
		sessionID := first.SessionID
		originalArrival := first.LocationArrivedAt
		Expect(first.Reattached).To(BeFalse(),
			"precondition: the first telnet connection creates the session")

		before := expectActiveSessionRow(ctx, ts, sessionID)
		playerSessionID := before.PlayerSessionID.String()
		Expect(before.PlayerSessionID.IsZero()).To(BeFalse(),
			"precondition: the session MUST record the player session that owns it, or the "+
				"one-session-per-player-session assertion below would range over nothing")
		firstConnIDs := connectionIDs(ctx, ts, sessionID)
		Expect(firstConnIDs).To(HaveLen(1),
			"precondition: the first telnet connection MUST have registered exactly one connection row")

		// The terminal goes away. Production removes the connection row on the
		// Subscribe defer and the Disconnect RPC parks the session in its
		// time-to-live window — the state a tmux client returns to.
		first.DetachTransport(ctx)
		detached, err := ts.SessionStore().Get(ctx, sessionID)
		Expect(err).NotTo(HaveOccurred(), "the detached telnet session row MUST survive the drop")
		Expect(detached.Status).To(Equal(session.StatusDetached),
			"precondition: dropping the connection MUST detach rather than delete the session")
		Expect(connectionIDs(ctx, ts, sessionID)).To(BeEmpty(),
			"precondition: the dropped connection's row MUST be gone, so the connection asserted "+
				"below is demonstrably a new one rather than the original still hanging around")

		// The player reattaches from a new terminal, under the SAME player
		// session — OpenTelnetSession reuses this AuthedPlayer's bearer token.
		second := player.OpenTelnetSession(ctx)
		DeferCleanup(func() { second.Logout(ctx) })

		Expect(second.Reattached).To(BeTrue(),
			"the returning telnet connection MUST take SelectCharacter's reattach branch")
		Expect(second.SessionID).To(Equal(sessionID),
			"a tmux-style reattach MUST resume the SAME game session, never mint a second one")

		info, err := ts.SessionStore().Get(ctx, sessionID)
		Expect(err).NotTo(HaveOccurred(), "the resumed telnet session row MUST be readable")
		Expect(info.Status).To(Equal(session.StatusActive),
			"the returning connection MUST return the session to active")
		Expect(info.LocationArrivedAt).To(BeTemporally("==", originalArrival),
			"I-PRIV-3 / INV-PRIVACY-3: resuming MUST leave the location arrival timestamp "+
				"unchanged, or the returning player silently gains or loses history")

		Expect(countSessionsForPlayerSession(ctx, ts, playerSessionID)).To(Equal(1),
			"the player session MUST own exactly one game session after the reattach; a second "+
				"row means the return minted a session instead of resuming one")

		// The connection is new; the session is not.
		secondConnIDs := connectionIDs(ctx, ts, sessionID)
		Expect(secondConnIDs).To(HaveLen(1),
			"the resumed session MUST carry exactly one connection row")
		Expect(secondConnIDs).NotTo(ContainElement(firstConnIDs[0]),
			"the resumed session MUST be serving a NEW connection — reusing the dropped "+
				"connection's identifier would mean nothing was actually reattached")

		// Read the transport identity back out of Postgres. Without this the
		// spec would prove only that a differently-named opener was called, and
		// a helper that silently fell back to the terminal client type would
		// pass.
		Expect(connectionClientTypes(ctx, ts, sessionID)).To(HaveEach("telnet"),
			"the returning connection MUST be recorded as telnet by the production Subscribe "+
				"handler; the grid roster counts connections by client_type, so a session that "+
				"came back as 'terminal' would silently change who can see it")
	})
})
