// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// holomush-y5inx.8: the integrationtest harness host history reader is wired
// with the crypto substrate (AuthGuard + DEK manager + codec selector) under
// WithPluginCrypto, so a SENSITIVE scene pose read back through
// Session.QueryStreamHistory is DECRYPTED for an authorized DEK participant and
// downgraded to metadata-only (no error) for a non-participant.
//
// BEFORE the wiring (bare reader at harness.go:367) the participant readback
// errored with EVENTBUS_HISTORY_DECODE_FAILED — the guard-nil default path
// tried to decrypt the XChaCha20v1 payload with a zero key.
//
// The character-identity decrypt path runs through the hot-tier AuthGuard
// (decodeAndAuthorizeHistory → decodeAuthorizeAndDispatch::checkCharacter):
// a binding_id match against the DEK's participant set permits the decrypt;
// a miss denies it and surfaces a metadata-only row (dispatcher.go:310-314).
var _ = Describe("encrypted scene IC content read back through QueryStreamHistory", func() {
	const plaintext = `{"text":"a secret pose"}`

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

	It("decrypts the sensitive pose for a scene participant (DEK member)", func() {
		loc := ts.NewLocation(ctx)
		sceneID := alice.CreateScene(ctx, loc)

		// Alice joins the scene (I-17 membership gate) BEFORE the emit so the
		// scope floor (her JoinedAt) does not filter the event out.
		alice.JoinScene(ctx, sceneID)

		// Mint the scene DEK with Alice as a participant BEFORE the emit. The
		// publisher's GetOrCreate(ctx, ctxID, nil) on the emit path then finds
		// the existing DEK (initial participants are only set on first mint),
		// so Alice's binding_id is in the participant set the AuthGuard checks.
		ts.SeedSceneDEKParticipant(ctx, sceneID, alice)

		emitted := ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			alice.CharacterID, "scene_pose", plaintext)
		Expect(emitted.SubjectStr).To(ContainSubstring(sceneID.String()))

		Eventually(func(g Gomega) {
			got, err := alice.QueryStreamHistory(ctx, emitted.SubjectStr)
			g.Expect(err).NotTo(HaveOccurred(),
				"participant QueryStreamHistory must not error (was EVENTBUS_HISTORY_DECODE_FAILED before wiring)")
			g.Expect(got).NotTo(BeEmpty(), "the encrypted scene_pose must be readable back")
		}).Should(Succeed())

		got, err := alice.QueryStreamHistory(ctx, emitted.SubjectStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeEmpty())
		ev := got[len(got)-1]
		Expect(ev.GetMetadataOnly()).To(BeFalse(),
			"participant must receive a DECRYPTED (non-metadata-only) frame")
		Expect(string(ev.GetPayload())).To(Equal(plaintext),
			"the decrypted payload must equal the original plaintext JSON")
	})

	It("downgrades to metadata-only (no error) for a non-participant scene member", func() {
		loc := ts.NewLocation(ctx)
		sceneID := alice.CreateScene(ctx, loc)
		alice.JoinScene(ctx, sceneID)
		ts.SeedSceneDEKParticipant(ctx, sceneID, alice)

		// Bob is a scene member (passes the I-17 membership gate) and joins
		// BEFORE the emit so the I-PRIV-6 temporal scope floor (his JoinedAt)
		// does not filter the event out — he can SEE the frame. But he is NOT a
		// DEK participant, so the decrypt-layer AuthGuard denies, yielding a
		// metadata-only frame WITHOUT an error (dispatcher.go:310-314).
		bob := ts.ConnectAuthed(ctx, "Bob")
		defer bob.Logout(ctx)
		bob.JoinScene(ctx, sceneID) // binding already created at connect (crypto enabled)

		emitted := ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			alice.CharacterID, "scene_pose", plaintext)

		// Wait until the event is readable by the participant (Alice decrypts it),
		// proving the row landed; then read as the non-participant Bob.
		Eventually(func(g Gomega) {
			got, err := alice.QueryStreamHistory(ctx, emitted.SubjectStr)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).NotTo(BeEmpty())
		}).Should(Succeed())

		got, err := bob.QueryStreamHistory(ctx, emitted.SubjectStr)
		Expect(err).NotTo(HaveOccurred(),
			"non-participant decrypt denial must surface as metadata-only, not an error")
		Expect(got).NotTo(BeEmpty(),
			"a metadata-only frame is still returned to a scene member")
		ev := got[len(got)-1]
		Expect(ev.GetMetadataOnly()).To(BeTrue(),
			"non-participant must receive a metadata-only frame")
		Expect(ev.GetPayload()).To(BeEmpty(),
			"non-participant must not receive plaintext")
	})
})
