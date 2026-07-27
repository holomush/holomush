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
