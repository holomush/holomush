// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package auth_test

import (
	"context"

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
