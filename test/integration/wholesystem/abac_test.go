// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package wholesystem_test

import (
	"context"

	policytypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// INV-PLUGIN-19: the whole-system stack — real in-tree plugins (WithInTreePlugins)
// loaded over the real seeded ABAC engine (WithRealABAC) — MUST evaluate a
// plugin-installed manifest policy end-to-end. test-abac-widget ships
// widget-read-normal (permit) and widget-forbid-restricted (forbid)
// (plugins/test-abac-widget/plugin.yaml:31-36); its AttributeResolver, loaded
// via WithInTreePlugins, resolves widget:normal-1 → {type:normal} and
// widget:restricted-1 → {type:restricted}, registered on the same engine
// AccessEngine() returns. The forbid spec is the real-engine sentinel: allow-all
// would *permit* widget:restricted-1, so a passing deny proves the plugin
// resolver and the seeded forbid policy are both live on the real engine.
var _ = Describe("cross-plugin ABAC (INV-PLUGIN-19)", Ordered, func() {
	var (
		srv     *integrationtest.Server
		subject string
	)

	BeforeAll(func() {
		srv = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithRealABAC(),
		)
		// A real connected character is the principal. The widget policies gate
		// on resource.widget.type (resolved by test-abac-widget), not on roles,
		// so a roleless character suffices — using a real one keeps the
		// principal resolving cleanly through the seeded providers.
		sess := srv.ConnectAuthed(context.Background(), "Widgeteer")
		subject = "character:" + sess.CharacterID.String()
	})
	AfterAll(func() {
		if srv != nil {
			srv.Stop()
		}
	})

	It("permits read on a normal widget (plugin-installed widget-read-normal policy)", func() {
		req, err := policytypes.NewAccessRequest(subject, "read", "widget:normal-1", nil)
		Expect(err).NotTo(HaveOccurred())

		decision, err := srv.AccessEngine().Evaluate(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(decision.IsAllowed()).To(BeTrue(),
			"test-abac-widget's widget-read-normal permit MUST allow reading a normal widget through the real engine")
	})

	It("forbids read on a restricted widget (plugin-installed widget-forbid-restricted policy)", func() {
		req, err := policytypes.NewAccessRequest(subject, "read", "widget:restricted-1", nil)
		Expect(err).NotTo(HaveOccurred())

		decision, err := srv.AccessEngine().Evaluate(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(decision.IsAllowed()).To(BeFalse(),
			"test-abac-widget's widget-forbid-restricted MUST deny reading a restricted widget (allow-all would have permitted it)")
		Expect(decision.Effect()).To(Equal(policytypes.EffectDeny),
			"a forbid policy MUST surface EffectDeny, not merely the absence of a permit")
	})
})
