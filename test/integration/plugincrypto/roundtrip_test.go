// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugincrypto_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

var _ = Describe("plugin crypto round-trip", func() {
	var ts *integrationtest.Server

	BeforeEach(func() {
		ts = integrationtest.Start(suiteT, integrationtest.WithInTreePlugins(), integrationtest.WithPluginCrypto())
		DeferCleanup(ts.Stop)
	})

	It("encrypts sensitivity:always content on the wire and recovers plaintext via read-back", func() {
		ctx := context.Background()
		// 1. Emit a sensitivity:always core-scenes IC event (claims Sensitive=true).
		emitted := ts.EmitPluginEvent(ctx, "core-scenes", "scene_pose", `{"text":"a secret pose"}`, true)

		// INV-5IA-4: encrypted on the wire — non-identity codec + a DEK row.
		Eventually(func() codec.Name { return ts.WireCodecFor(ctx, emitted.SubjectStr) }).
			ShouldNot(Equal(codec.NameIdentity))
		Expect(ts.DEKRowCount(ctx)).To(BeNumerically(">", 0))

		// 2. Event projected to the plugin audit table (scene_log) as an encrypted row.
		var rows []integrationtest.PluginAuditRow
		Eventually(func() int {
			rows = ts.QueryPluginAuditRows(ctx, "core-scenes", emitted.SubjectStr)
			return len(rows)
		}).Should(BeNumerically(">", 0))

		// 3. Read-back via host decryptor → plaintext recovered (INV-5IA-6).
		results := ts.ReadBackOwnRows(ctx, "core-scenes", rows)
		Expect(results).To(HaveLen(len(rows)))
		Expect(results[0].Plaintext).To(ContainSubstring("a secret pose"))
		// INV-5IA-6: read-back audit fired.
		Expect(ts.ReadBackAuditCount(ctx)).To(BeNumerically(">", 0))
	})

	It("denies read-back for an event type whose manifest declares readback:false, without leaking plaintext", func() {
		// scene_ooc is sensitivity:always but has NO readback declaration — it is
		// encrypted on the wire but the g2 AuthGuard check
		// (checkPluginReadback → PluginCanReadBack → false) refuses it on the
		// read-back path, so plaintext is never returned. This exercises the real
		// denial path: decodeAuthorizeAndDispatch skips identity-codec rows before
		// the AuthGuard, so we need a sensitivity:always event to reach the guard.
		ctx := context.Background()
		emitted := ts.EmitPluginEvent(ctx, "core-scenes", "scene_ooc", `{"ooc":"x"}`, true)
		var rows []integrationtest.PluginAuditRow
		Eventually(func() int { rows = ts.QueryPluginAuditRows(ctx, "core-scenes", emitted.SubjectStr); return len(rows) }).
			Should(BeNumerically(">", 0))
		results := ts.ReadBackOwnRows(ctx, "core-scenes", rows)
		Expect(results[0].Plaintext).To(BeEmpty(), "readback-denied row must not yield plaintext")
		Expect(results[0].Denied).To(BeTrue())
		// INV-5IA-4: a sensitivity:always event without readback is still encrypted
		// on the wire (non-identity codec), confirming the deny is from the AuthGuard
		// gate, not from a missing encryption step.
		Expect(ts.WireCodecFor(ctx, emitted.SubjectStr)).NotTo(Equal(codec.NameIdentity))
	})

	It("leaves a sensitivity:never event identity-coded on the wire (no over-encryption)", func() {
		// INV-5IA-4 negative half: scene_join_ic is sensitivity:never, so the
		// publisher MUST NOT encrypt it — the wire codec stays identity. Gating on
		// the projected audit row first guarantees the JetStream message landed, so
		// the WireCodecFor read sees a real message (not the no-message sentinel).
		ctx := context.Background()
		emitted := ts.EmitPluginEvent(ctx, "core-scenes", "scene_join_ic", `{"who":"x"}`, false)
		Eventually(func() int {
			return len(ts.QueryPluginAuditRows(ctx, "core-scenes", emitted.SubjectStr))
		}).Should(BeNumerically(">", 0))
		Expect(ts.WireCodecFor(ctx, emitted.SubjectStr)).To(Equal(codec.NameIdentity))
	})
})
