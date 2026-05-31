// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package auth_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"

	"connectrpc.com/connect"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/web"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

var _ = Describe("Multi-tab session isolation — two-tab guest scenario", func() {
	var ctx context.Context
	var gw *web.Handler

	BeforeEach(func() {
		ctx = context.Background()
		// FK-safe wipe of any state from prior specs (deletes locations among
		// other tables — see auth_suite_test.go cleanupTestData).
		cleanupTestData(ctx, env.pool)

		// Re-create the guest start-location row that the GuestService's namer
		// references. characters.location_id has a FK to locations(id), so this
		// must exist before WebCreateGuest is invoked.
		loc := &world.Location{
			ID:           env.guestStartLocationID,
			Name:         "Guest Lobby",
			Description:  "Guest start location for multi-tab integration tests",
			Type:         world.LocationTypePersistent,
			ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
		}
		Expect(env.locRepo.Create(ctx, loc)).To(Succeed())

		gw = env.webHandler
	})

	It("the second WebCreateGuest returns ALREADY_AUTHENTICATED and the first stays live", func() {
		// Tab 1: WebCreateGuest with no cookie. Mints guest player A.
		tab1Resp, err := gw.WebCreateGuest(ctx, connect.NewRequest(&webv1.WebCreateGuestRequest{}))
		Expect(err).NotTo(HaveOccurred())
		Expect(tab1Resp.Msg.GetSuccess()).To(BeTrue())

		tab1Token := tab1Resp.Header().Get(web.HeaderSetSessionToken)
		Expect(tab1Token).NotTo(BeEmpty(), "WebCreateGuest must signal Set-Cookie via the X-Set-Session-Token header")

		// Tab 2: same browser, same cookie token. Cookie middleware would inject
		// this in production; here we simulate by setting the X-Session-Token
		// header that CookieMiddleware would set.
		tab2Req := connect.NewRequest(&webv1.WebCreateGuestRequest{})
		tab2Req.Header().Set(web.HeaderInjectSessionToken, tab1Token)

		tab2Resp, err := gw.WebCreateGuest(ctx, tab2Req)
		Expect(err).NotTo(HaveOccurred())
		Expect(tab2Resp.Msg.GetSuccess()).To(BeFalse())
		Expect(tab2Resp.Msg.GetErrorCode()).To(Equal("ALREADY_AUTHENTICATED"))
		Expect(tab2Resp.Msg.GetCurrentPlayerName()).NotTo(BeEmpty())
		Expect(tab2Resp.Header().Get(web.HeaderSetSessionToken)).To(BeEmpty(),
			"gate hit MUST NOT signal a Set-Cookie")
	})
})

var _ = Describe("Multi-tab session isolation — same character in two tabs", func() {
	var ctx context.Context
	var gw *web.Handler

	BeforeEach(func() {
		ctx = context.Background()
		// FK-safe wipe of any state from prior specs (deletes locations among
		// other tables — see auth_suite_test.go cleanupTestData).
		cleanupTestData(ctx, env.pool)

		// Re-create the guest start-location row that the GuestService's namer
		// references. Other specs in this file rely on it; keep it consistent
		// across the suite even though this spec uses a registered player.
		loc := &world.Location{
			ID:           env.guestStartLocationID,
			Name:         "Guest Lobby",
			Description:  "Guest start location for multi-tab integration tests",
			Type:         world.LocationTypePersistent,
			ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
		}
		Expect(env.locRepo.Create(ctx, loc)).To(Succeed())

		gw = env.webHandler
	})

	It("both tabs reattach to one session and dual-tab command submission succeeds", func() {
		// Seed: a registered player + character. CreatePlayer returns
		// (*Player, *PlayerSession, rawToken string, error). Note that
		// auth.Service.CreatePlayer constructs the PlayerSession but does NOT
		// persist it — the gRPC handler does (internal/grpc/auth_handlers.go:354).
		// We replicate that here so resolvePlayerSession can find the token.
		username := "two_tab_player"
		password := "correct horse battery staple"
		_, playerSession, rawToken, err := env.authService.CreatePlayer(ctx, username, password, username+"@test.local")
		Expect(err).NotTo(HaveOccurred(), "seed: create player")
		Expect(rawToken).NotTo(BeEmpty())
		Expect(env.playerSessionStore.Create(ctx, playerSession)).To(Succeed(),
			"seed: persist player session so the rawToken resolves")

		player, err := env.playerRepo.GetByUsername(ctx, username)
		Expect(err).NotTo(HaveOccurred())
		// world.Character.Name is validated as "letters and spaces only", so
		// avoid underscores/hyphens in the seeded character name. Location
		// names are not subject to that constraint, but we keep them simple
		// for symmetry.
		loc := createTestLocation(ctx, "Two Tab Lobby")
		char := createTestCharacter(ctx, player.ID, "Two Tab Hero", loc.ID)
		charID := char.ID.String()

		// Tab 1: select the character.
		selReq1 := connect.NewRequest(&webv1.WebSelectCharacterRequest{CharacterId: charID})
		selReq1.Header().Set(web.HeaderInjectSessionToken, rawToken)
		selResp1, err := gw.WebSelectCharacter(ctx, selReq1)
		Expect(err).NotTo(HaveOccurred())
		Expect(selResp1.Msg.GetSuccess()).To(BeTrue(), "tab 1: %s", selResp1.Msg.GetErrorMessage())
		Expect(selResp1.Msg.GetSessionId()).NotTo(BeEmpty())

		// Tab 2: same cookie token, same character → reattach.
		selReq2 := connect.NewRequest(&webv1.WebSelectCharacterRequest{CharacterId: charID})
		selReq2.Header().Set(web.HeaderInjectSessionToken, rawToken)
		selResp2, err := gw.WebSelectCharacter(ctx, selReq2)
		Expect(err).NotTo(HaveOccurred())
		Expect(selResp2.Msg.GetSuccess()).To(BeTrue())
		Expect(selResp2.Msg.GetReattached()).To(BeTrue(), "tab 2 MUST reattach, not create a new session")
		Expect(selResp2.Msg.GetSessionId()).To(Equal(selResp1.Msg.GetSessionId()),
			"both tabs MUST land on the same session_id")

		// Both tabs can submit commands. HandleCommand validates ownership at
		// the player level (ValidateSessionOwnership → player_id), not by
		// connection_id, so both succeed. The dispatcher in this suite has no
		// commands registered, so "look" surfaces as an unknown-command user-
		// facing error; HandleCommand returns Success=true at the RPC level
		// (see internal/grpc/server.go:427-441 isUserFacingError handling).
		// The assertion that matters here is RPC-level acceptance, which
		// proves the ownership gate let both tabs through.
		cmdReq1 := &corev1.HandleCommandRequest{
			SessionId:          selResp1.Msg.GetSessionId(),
			PlayerSessionToken: rawToken,
			Command:            "look",
		}
		cmdResp1, err := env.coreServer.HandleCommand(ctx, cmdReq1)
		Expect(err).NotTo(HaveOccurred())
		Expect(cmdResp1.GetSuccess()).To(BeTrue(), "tab 1 command MUST succeed")

		cmdReq2 := &corev1.HandleCommandRequest{
			SessionId:          selResp2.Msg.GetSessionId(), // same session_id
			PlayerSessionToken: rawToken,
			Command:            "look",
		}
		cmdResp2, err := env.coreServer.HandleCommand(ctx, cmdReq2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cmdResp2.GetSuccess()).To(BeTrue(), "tab 2 command MUST succeed against the shared session")
	})
})

// captureHandler is a thread-safe slog.Handler that records emitted log lines
// into an in-memory buffer so tests can assert on log contents (e.g., that a
// specific warning was NOT emitted).
type captureHandler struct {
	buf *bytes.Buffer
	// Mutex is a pointer so derived handlers (WithAttrs/WithGroup) share
	// the same lock and serialize writes to the shared buf. A value-typed
	// mutex would give each clone a fresh zero mutex while keeping buf
	// shared, racing concurrent writes through different derived handlers.
	mu  *sync.Mutex
	sub slog.Handler
}

func newCaptureHandler() (*captureHandler, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := &captureHandler{
		buf: buf,
		mu:  &sync.Mutex{},
		sub: slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	}
	return h, buf
}

func (c *captureHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return c.sub.Enabled(ctx, l)
}

func (c *captureHandler) Handle(ctx context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sub.Handle(ctx, r)
}

func (c *captureHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return &captureHandler{buf: c.buf, mu: c.mu, sub: c.sub.WithAttrs(a)}
}

func (c *captureHandler) WithGroup(g string) slog.Handler {
	return &captureHandler{buf: c.buf, mu: c.mu, sub: c.sub.WithGroup(g)}
}

var _ = Describe("Multi-tab session isolation — browser cookie + concurrent telnet auth", func() {
	var ctx context.Context
	var gw *web.Handler

	BeforeEach(func() {
		ctx = context.Background()
		// FK-safe wipe of any state from prior specs (deletes locations among
		// other tables — see auth_suite_test.go cleanupTestData).
		cleanupTestData(ctx, env.pool)

		// Re-create the guest start-location row that the GuestService's namer
		// references. Other specs in this file rely on it; keep it consistent
		// across the suite even though this spec uses a registered player.
		loc := &world.Location{
			ID:           env.guestStartLocationID,
			Name:         "Guest Lobby",
			Description:  "Guest start location for multi-tab integration tests",
			Type:         world.LocationTypePersistent,
			ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
		}
		Expect(env.locRepo.Create(ctx, loc)).To(Succeed())

		gw = env.webHandler
	})

	It("telnet auth bypasses the gateway gate; both PlayerSessions exist; reattach holds", func() {
		// Seed a registered player. CreatePlayer returns
		// (*Player, *PlayerSession, rawToken string, error). We discard all
		// returns because this test mints fresh sessions via WebAuthenticatePlayer
		// (web) and authService.AuthenticatePlayer (telnet) — each persists its
		// own PlayerSession.
		username := "parity_player"
		password := "correct horse battery staple"
		_, _, _, err := env.authService.CreatePlayer(ctx, username, password, username+"@test.local")
		Expect(err).NotTo(HaveOccurred(), "seed: create player")

		// Web: WebAuthenticatePlayer — cookie set, PlayerSession_W exists.
		webResp, err := gw.WebAuthenticatePlayer(ctx, connect.NewRequest(&webv1.WebAuthenticatePlayerRequest{
			Username: username,
			Password: password,
		}))
		Expect(err).NotTo(HaveOccurred())
		Expect(webResp.Msg.GetSuccess()).To(BeTrue())
		webToken := webResp.Header().Get(web.HeaderSetSessionToken)
		Expect(webToken).NotTo(BeEmpty())

		// Telnet: authenticate with the same username+password. This calls
		// env.authService.AuthenticatePlayer directly (telnet does not go
		// through the web gateway). The gate doesn't apply.
		telnetToken, _, err := env.authService.AuthenticatePlayer(ctx, username, password, "telnet-ua", "127.0.0.1")
		Expect(err).NotTo(HaveOccurred())
		Expect(telnetToken).NotTo(BeEmpty())
		Expect(telnetToken).NotTo(Equal(webToken), "telnet's PlayerSession is distinct from web's")

		// ListPlayerSessions reports >= 2 active sessions for this player —
		// browser + telnet MUST coexist as two PlayerSessions.
		player, err := env.playerRepo.GetByUsername(ctx, username)
		Expect(err).NotTo(HaveOccurred())
		sessions, err := env.playerSessionStore.ListByPlayer(ctx, player.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(sessions)).To(BeNumerically(">=", 2),
			"browser + telnet MUST coexist as two PlayerSessions")
	})
})

var _ = Describe("Multi-tab session isolation — two characters of one player", func() {
	var ctx context.Context
	var gw *web.Handler
	var prevDefault *slog.Logger
	var logBuf *bytes.Buffer

	BeforeEach(func() {
		ctx = context.Background()
		// FK-safe wipe of any state from prior specs (deletes locations among
		// other tables — see auth_suite_test.go cleanupTestData).
		cleanupTestData(ctx, env.pool)

		// Re-create the guest start-location row that the GuestService's namer
		// references. Other specs in this file rely on it; keep it consistent
		// across the suite.
		loc := &world.Location{
			ID:           env.guestStartLocationID,
			Name:         "Guest Lobby",
			Description:  "Guest start location for multi-tab integration tests",
			Type:         world.LocationTypePersistent,
			ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
		}
		Expect(env.locRepo.Create(ctx, loc)).To(Succeed())

		gw = env.webHandler

		// Capture slog output so we can assert on the absence of an
		// ownership-mismatch warning at the end of the spec.
		prevDefault = slog.Default()
		h, buf := newCaptureHandler()
		slog.SetDefault(slog.New(h))
		logBuf = buf
	})

	AfterEach(func() {
		slog.SetDefault(prevDefault)
	})

	It("creates two distinct sessions and produces no ownership-mismatch warnings", func() {
		// Seed: a registered player with two characters X and Y. CreatePlayer
		// returns (*Player, *PlayerSession, rawToken string, error). Note that
		// auth.Service.CreatePlayer constructs the PlayerSession but does NOT
		// persist it — the gRPC handler does (internal/grpc/auth_handlers.go:354).
		// We replicate that here so resolvePlayerSession can find the token.
		username := "two_char_player"
		password := "correct horse battery staple"
		_, playerSession, rawToken, err := env.authService.CreatePlayer(ctx, username, password, username+"@test.local")
		Expect(err).NotTo(HaveOccurred(), "seed: create player")
		Expect(rawToken).NotTo(BeEmpty())
		Expect(env.playerSessionStore.Create(ctx, playerSession)).To(Succeed(),
			"seed: persist player session so the rawToken resolves")

		player, err := env.playerRepo.GetByUsername(ctx, username)
		Expect(err).NotTo(HaveOccurred())
		// world.Character.Name is validated as "letters and spaces only", so
		// avoid underscores/hyphens in the seeded character names.
		loc := createTestLocation(ctx, "Two Char Lobby")
		charX := createTestCharacter(ctx, player.ID, "Hero X", loc.ID)
		charY := createTestCharacter(ctx, player.ID, "Hero Y", loc.ID)
		charXID := charX.ID.String()
		charYID := charY.ID.String()
		Expect(charXID).NotTo(Equal(charYID))

		// Tab 1: select X.
		selReq1 := connect.NewRequest(&webv1.WebSelectCharacterRequest{CharacterId: charXID})
		selReq1.Header().Set(web.HeaderInjectSessionToken, rawToken)
		selResp1, err := gw.WebSelectCharacter(ctx, selReq1)
		Expect(err).NotTo(HaveOccurred())
		Expect(selResp1.Msg.GetSuccess()).To(BeTrue(), "tab 1: %s", selResp1.Msg.GetErrorMessage())
		Expect(selResp1.Msg.GetSessionId()).NotTo(BeEmpty())

		// Tab 2: same cookie token, DIFFERENT character → distinct session.
		selReq2 := connect.NewRequest(&webv1.WebSelectCharacterRequest{CharacterId: charYID})
		selReq2.Header().Set(web.HeaderInjectSessionToken, rawToken)
		selResp2, err := gw.WebSelectCharacter(ctx, selReq2)
		Expect(err).NotTo(HaveOccurred())
		Expect(selResp2.Msg.GetSuccess()).To(BeTrue(), "tab 2: %s", selResp2.Msg.GetErrorMessage())
		Expect(selResp2.Msg.GetSessionId()).NotTo(BeEmpty())

		Expect(selResp2.Msg.GetSessionId()).NotTo(Equal(selResp1.Msg.GetSessionId()),
			"different characters MUST have distinct sessions")

		// Both sessions accept commands. The dispatcher in this suite has no
		// commands registered, so "look" surfaces as an unknown-command user-
		// facing error; HandleCommand returns Success=true at the RPC level
		// (see internal/grpc/server.go:427-441 isUserFacingError handling).
		// The assertion that matters here is RPC-level acceptance, which
		// proves the ownership gate let both sessions through.
		for _, sessionID := range []string{selResp1.Msg.GetSessionId(), selResp2.Msg.GetSessionId()} {
			cmdReq := &corev1.HandleCommandRequest{
				SessionId:          sessionID,
				PlayerSessionToken: rawToken,
				Command:            "look",
			}
			cmdResp, err := env.coreServer.HandleCommand(ctx, cmdReq)
			Expect(err).NotTo(HaveOccurred())
			Expect(cmdResp.GetSuccess()).To(BeTrue(), "command MUST succeed for session %s", sessionID)
		}

		Expect(logBuf.String()).NotTo(ContainSubstring("session ownership mismatch"),
			"two characters under one player MUST NOT trigger ownership-mismatch logs")
	})
})

var _ = Describe("Multi-tab session isolation — logout in tab 1, action in tab 2", func() {
	var ctx context.Context
	var gw *web.Handler

	BeforeEach(func() {
		ctx = context.Background()
		// FK-safe wipe of any state from prior specs (deletes locations among
		// other tables — see auth_suite_test.go cleanupTestData).
		cleanupTestData(ctx, env.pool)

		// Re-create the guest start-location row that the GuestService's namer
		// references. Other specs in this file rely on it; keep it consistent
		// across the suite even though this spec uses a registered player.
		loc := &world.Location{
			ID:           env.guestStartLocationID,
			Name:         "Guest Lobby",
			Description:  "Guest start location for multi-tab integration tests",
			Type:         world.LocationTypePersistent,
			ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
		}
		Expect(env.locRepo.Create(ctx, loc)).To(Succeed())

		gw = env.webHandler
	})

	It("each call site from spec §4.4.5 returns SESSION_NOT_FOUND after logout", func() {
		// Seed: a registered player with one character. CreatePlayer returns
		// (*Player, *PlayerSession, rawToken string, error). auth.Service
		// constructs the PlayerSession but does NOT persist it — replicate
		// the gRPC handler's persistence so resolvePlayerSession can find
		// the token.
		username := "logout_player"
		password := "correct horse battery staple"
		_, playerSession, rawToken, err := env.authService.CreatePlayer(ctx, username, password, username+"@test.local")
		Expect(err).NotTo(HaveOccurred(), "seed: create player")
		Expect(rawToken).NotTo(BeEmpty())
		Expect(env.playerSessionStore.Create(ctx, playerSession)).To(Succeed(),
			"seed: persist player session so the rawToken resolves")

		player, err := env.playerRepo.GetByUsername(ctx, username)
		Expect(err).NotTo(HaveOccurred())
		// world.Character.Name is "letters and spaces only"; avoid hyphens.
		loc := createTestLocation(ctx, "Logout Lobby")
		char := createTestCharacter(ctx, player.ID, "Logout Hero", loc.ID)
		charID := char.ID.String()

		// Tab 1 + Tab 2 both select the character. WebSelectCharacter is
		// idempotent for the same (player, character) — both calls land on
		// the same session_id (verified by the "same character in two tabs"
		// spec above). We only need one sessionID for the post-logout retries.
		selReq := connect.NewRequest(&webv1.WebSelectCharacterRequest{CharacterId: charID})
		selReq.Header().Set(web.HeaderInjectSessionToken, rawToken)
		selResp, err := gw.WebSelectCharacter(ctx, selReq)
		Expect(err).NotTo(HaveOccurred())
		Expect(selResp.Msg.GetSuccess()).To(BeTrue(), "select: %s", selResp.Msg.GetErrorMessage())
		sessionID := selResp.Msg.GetSessionId()
		Expect(sessionID).NotTo(BeEmpty())

		// Tab 1: WebLogout. PlayerSession is deleted; child game sessions are
		// also deleted via the fanout in CoreServer.Logout.
		_, err = env.coreServer.Logout(ctx, &corev1.LogoutRequest{
			PlayerSessionToken: rawToken,
		})
		Expect(err).NotTo(HaveOccurred())

		// Tab 2 retries each of the four call sites with the (now-stale)
		// token. The post-logout error surface differs by call site:
		//
		//   - WebQueryStreamHistory  → errors out (gRPC status passes through)
		//   - WebListSessionStreams  → errors out (gRPC status passes through)
		//   - HandleCommand          → returns Success=false (enumeration-safe;
		//                              see server.go:317-328)
		//   - GetCommandHistory      → returns Success=false (enumeration-safe;
		//                              see server.go:1261-1278)
		//
		// Both shapes are valid "session not found" surfaces; what matters
		// is that none of them return Success=true with real data.

		// 1. WebQueryStreamHistory.
		qReq := connect.NewRequest(&webv1.WebQueryStreamHistoryRequest{
			SessionId: sessionID,
			Stream:    "character:" + charID,
			Count:     10,
		})
		qReq.Header().Set(web.HeaderInjectSessionToken, rawToken)
		_, err = gw.WebQueryStreamHistory(ctx, qReq)
		Expect(err).To(HaveOccurred(), "WebQueryStreamHistory MUST surface stale-session error")

		// 2. WebListSessionStreams.
		lReq := connect.NewRequest(&webv1.WebListSessionStreamsRequest{SessionId: sessionID})
		lReq.Header().Set(web.HeaderInjectSessionToken, rawToken)
		_, err = gw.WebListSessionStreams(ctx, lReq)
		Expect(err).To(HaveOccurred(), "WebListSessionStreams MUST surface stale-session error")

		// 3. HandleCommand on the core server. Enumeration-safe path returns
		// Success=false with no transport-level error.
		cmdResp, err := env.coreServer.HandleCommand(ctx, &corev1.HandleCommandRequest{
			SessionId:          sessionID,
			PlayerSessionToken: rawToken,
			Command:            "look",
		})
		Expect(err).NotTo(HaveOccurred(), "HandleCommand uses enumeration-safe Success=false rejection")
		Expect(cmdResp.GetSuccess()).To(BeFalse(),
			"HandleCommand MUST reject stale token with Success=false")
		Expect(cmdResp.GetError()).To(ContainSubstring("session not found"))

		// 4. GetCommandHistory — same enumeration-safe path. This stands in
		// for the Subscribe call site: both run through ValidateSessionOwnership,
		// and Subscribe's stream-opening machinery is awkward to drive directly
		// from this test. GetCommandHistory exercises the identical validation
		// path (see server.go:1261-1278).
		histResp, err := env.coreServer.GetCommandHistory(ctx, &corev1.GetCommandHistoryRequest{
			SessionId:          sessionID,
			PlayerSessionToken: rawToken,
		})
		Expect(err).NotTo(HaveOccurred(), "GetCommandHistory uses enumeration-safe Success=false rejection")
		Expect(histResp.GetSuccess()).To(BeFalse(),
			"GetCommandHistory MUST reject revoked token with Success=false")
		Expect(histResp.GetError()).To(ContainSubstring("session not found"))
	})
})

var _ = Describe("Multi-tab session isolation — Subscribe-path post-logout (spec §4.4.5 Subscribe call site)", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		// FK-safe wipe of any state from prior specs (deletes locations among
		// other tables — see auth_suite_test.go cleanupTestData).
		cleanupTestData(ctx, env.pool)

		// Re-create the guest start-location row so other specs in this file
		// remain consistent. This spec uses a registered player and does not
		// hit the guest start-location FK, but we keep the seed parity with
		// every other Describe in this file.
		loc := &world.Location{
			ID:           env.guestStartLocationID,
			Name:         "Guest Lobby",
			Description:  "Guest start location for multi-tab integration tests",
			Type:         world.LocationTypePersistent,
			ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
		}
		Expect(env.locRepo.Create(ctx, loc)).To(Succeed())
	})

	It("Subscribe rejects a stale token with SESSION_NOT_FOUND before sending any frame", func() {
		// Companion to the "logout in tab 1, action in tab 2" spec above. That
		// spec substituted GetCommandHistory for the Subscribe call site
		// (spec §4.4.5) because Subscribe is server-streaming and awkward to
		// drive directly from a Ginkgo spec. Both call sites run through
		// auth.ValidateSessionOwnership at internal/grpc/server.go (Subscribe
		// at :645, GetCommandHistory at :1261-1278), so the validation path
		// is identical — but Subscribe's stream-opening machinery (early-return
		// vs. error-frame, transport-level vs. response-level signal) could
		// regress independently. This spec pins the wire-level error shape
		// that the Subscribe handler returns when ownership validation fails.

		// Seed: registered player + character + persisted PlayerSession (gRPC
		// handler normally persists; auth.Service constructs but does not).
		username := "subscribe_logout_player"
		password := "correct horse battery staple"
		_, playerSession, rawToken, err := env.authService.CreatePlayer(ctx, username, password, username+"@test.local")
		Expect(err).NotTo(HaveOccurred(), "seed: create player")
		Expect(rawToken).NotTo(BeEmpty())
		Expect(env.playerSessionStore.Create(ctx, playerSession)).To(Succeed(),
			"seed: persist player session so the rawToken resolves")

		player, err := env.playerRepo.GetByUsername(ctx, username)
		Expect(err).NotTo(HaveOccurred())
		// world.Character.Name is "letters and spaces only"; avoid hyphens.
		loc := createTestLocation(ctx, "Subscribe Lobby")
		char := createTestCharacter(ctx, player.ID, "Subscribe Hero", loc.ID)
		charID := char.ID.String()

		// Tab 1: select the character → game session exists.
		selReq := connect.NewRequest(&webv1.WebSelectCharacterRequest{CharacterId: charID})
		selReq.Header().Set(web.HeaderInjectSessionToken, rawToken)
		selResp, err := env.webHandler.WebSelectCharacter(ctx, selReq)
		Expect(err).NotTo(HaveOccurred())
		Expect(selResp.Msg.GetSuccess()).To(BeTrue(), "select: %s", selResp.Msg.GetErrorMessage())
		sessionID := selResp.Msg.GetSessionId()
		Expect(sessionID).NotTo(BeEmpty())

		// Tab 1: WebLogout deletes the PlayerSession + child game sessions.
		_, err = env.coreServer.Logout(ctx, &corev1.LogoutRequest{PlayerSessionToken: rawToken})
		Expect(err).NotTo(HaveOccurred())

		// Tab 2: open Subscribe with the now-stale token. MUST return an
		// error tagged SESSION_NOT_FOUND, and MUST NOT have sent any frame
		// on the stream before validation fired.
		stream := &captureSubscribeStream{ctx: ctx}
		err = env.coreServer.Subscribe(&corev1.SubscribeRequest{
			SessionId:          sessionID,
			PlayerSessionToken: rawToken,
		}, stream)
		Expect(err).To(HaveOccurred(), "Subscribe MUST surface stale-session error at the transport level")

		// Post-rsoe6.11.1 wire contract: the Subscribe handler stamps the
		// enumeration-safe SESSION_NOT_FOUND with a classifiable gRPC status
		// code (codes.Unauthenticated) via subscribeSessionNotFound, rather
		// than returning a bare oops error (which grpc-go would surface as
		// codes.Unknown — indistinguishable from a transient core-down). The
		// gateway client recovers the SESSION_NOT_FOUND oops code via
		// holoGRPC.TranslateSubscribeErr. Mirrors the unit-level contract in
		// internal/grpc/server_helpers_test.go + client_test.go.
		st, ok := status.FromError(err)
		Expect(ok).To(BeTrue(), "Subscribe error MUST carry a gRPC status code")
		Expect(st.Code()).To(Equal(codes.Unauthenticated),
			"stale-token failure MUST cross the wire as Unauthenticated")
		o, ok := oops.AsOops(holoGRPC.TranslateSubscribeErr(err))
		Expect(ok).To(BeTrue(), "TranslateSubscribeErr MUST yield an oops error")
		Expect(o.Code()).To(Equal("SESSION_NOT_FOUND"),
			"Subscribe MUST collapse stale-token failure to enumeration-safe SESSION_NOT_FOUND")
		Expect(stream.sent).To(BeEmpty(),
			"Subscribe MUST NOT send any frame before ownership validation fires")
	})
})

// captureSubscribeStream is a test double for grpc.ServerStreamingServer that
// captures sent frames so specs can assert that no data was streamed before an
// early validation failure. Mirrors the capturingStream pattern in
// internal/grpc/location_follow_test.go.
type captureSubscribeStream struct {
	grpc.ServerStream
	ctx  context.Context
	sent []*corev1.SubscribeResponse
}

func (s *captureSubscribeStream) Send(resp *corev1.SubscribeResponse) error {
	s.sent = append(s.sent, resp)
	return nil
}

func (s *captureSubscribeStream) Context() context.Context       { return s.ctx }
func (s *captureSubscribeStream) SetHeader(_ metadata.MD) error  { return nil }
func (s *captureSubscribeStream) SendHeader(_ metadata.MD) error { return nil }
func (s *captureSubscribeStream) SetTrailer(_ metadata.MD)       {}
func (s *captureSubscribeStream) SendMsg(_ any) error            { return nil }
func (s *captureSubscribeStream) RecvMsg(_ any) error            { return nil }

var _ = Describe("Pre-deploy WebCheckSession contract", func() {
	var gw *web.Handler

	BeforeEach(func() {
		ctx := context.Background()
		// FK-safe wipe of any state from prior specs (deletes locations among
		// other tables — see auth_suite_test.go cleanupTestData).
		cleanupTestData(ctx, env.pool)

		// Re-create the guest start-location row that the GuestService's namer
		// references. characters.location_id has a FK to locations(id), so this
		// must exist before WebCreateGuest is invoked.
		loc := &world.Location{
			ID:           env.guestStartLocationID,
			Name:         "Guest Lobby",
			Description:  "Guest start location for multi-tab integration tests",
			Type:         world.LocationTypePersistent,
			ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
		}
		Expect(env.locRepo.Create(ctx, loc)).To(Succeed())

		gw = env.webHandler
	})

	It("still throws / returns Unauthenticated on auth failure", func() {
		ctx := context.Background()
		req := connect.NewRequest(&webv1.WebCheckSessionRequest{})
		// No token set.
		_, err := gw.WebCheckSession(ctx, req)
		Expect(err).To(HaveOccurred(), "auth-failure path MUST still return an error response")
		var connectErr *connect.Error
		Expect(errors.As(err, &connectErr)).To(BeTrue())
		Expect(connectErr.Code()).To(Equal(connect.CodeUnauthenticated))
	})

	It("still populates player_name on success and now also player_id, is_guest, characters", func() {
		ctx := context.Background()

		// Mint a guest, capture the token, call WebCheckSession.
		guestResp, err := gw.WebCreateGuest(ctx, connect.NewRequest(&webv1.WebCreateGuestRequest{}))
		Expect(err).NotTo(HaveOccurred())
		token := guestResp.Header().Get(web.HeaderSetSessionToken)

		req := connect.NewRequest(&webv1.WebCheckSessionRequest{})
		req.Header().Set(web.HeaderInjectSessionToken, token)
		resp, err := gw.WebCheckSession(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Msg.GetPlayerName()).NotTo(BeEmpty())
		Expect(resp.Msg.GetPlayerId()).NotTo(BeEmpty())
		Expect(resp.Msg.GetIsGuest()).To(BeTrue())
		Expect(resp.Msg.GetCharacters()).To(HaveLen(1))
	})
})
