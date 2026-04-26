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
