// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// holomush-shcyu (Task 4 validation): proves the parameterized crypto
// round-trip — encrypted IC content seeded into a CreateScene-created scene
// reads back on the wire as XChaCha20v1 — BEFORE the full publish E2E. The
// command emit path can't set Sensitive (INV-7 fence, spec §3.4), so the
// harness seeds encrypted content via EmitSceneICContent for an arbitrary
// scene + actor.
var _ = Describe("encrypted IC content seeded into a CreateScene'd scene", func() {
	var (
		ts    *integrationtest.Server
		ctx   context.Context
		alice *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
		DeferCleanup(cancel)
		ts = integrationtest.Start(suiteT, integrationtest.WithInTreePlugins(), integrationtest.WithPluginCrypto())
		alice = ts.ConnectAuthed(ctx, "Alice")
	})

	AfterEach(func() {
		if alice != nil {
			alice.Logout(ctx)
		}
		ts.Stop()
	})

	It("encrypted scene_pose seeded into a CreateScene'd scene reads back", func() {
		loc := ts.NewLocation(ctx)
		sceneID := alice.CreateScene(ctx, loc)

		emitted := ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			alice.CharacterID, "scene_pose", `{"text":"a secret pose"}`)

		Expect(emitted.SubjectStr).To(ContainSubstring(sceneID.String()))
		Eventually(func() codec.Name { return ts.WireCodecFor(ctx, emitted.SubjectStr) }).
			Should(Equal(codec.NameXChaCha20v1)) // encrypted on the wire
	})
})
