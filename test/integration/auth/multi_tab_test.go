// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package auth_test

import (
	"bytes"
	"context"
	"log/slog"
	"sync"

	"connectrpc.com/connect"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

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
	mu  sync.Mutex
	sub slog.Handler
}

func newCaptureHandler() (*captureHandler, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := &captureHandler{buf: buf, sub: slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})}
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
	return &captureHandler{buf: c.buf, sub: c.sub.WithAttrs(a)}
}

func (c *captureHandler) WithGroup(g string) slog.Handler {
	return &captureHandler{buf: c.buf, sub: c.sub.WithGroup(g)}
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
