// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package crypto_test — end-to-end proof that with a KEK present:
//
//   - A scene participant receives a sensitive event DECRYPTED
//     (MetadataOnly()==false, payload == original plaintext).
//   - A comms recipient (page on a character.<id> subject) likewise
//     receives plaintext — proving the behaviour is domain-generic,
//     not scene-specific.
//   - A non-participant receives metadata-only (MetadataOnly()==true,
//     payload empty).
//
// Asserts the publish/subscribe asymmetry is gone end-to-end under
// live crypto (real KEK + real DEK + real AEAD encryption/decryption).
//
// Pre-covered by metadata_only_test.go (referenced here to avoid duplication):
//   - TestSubscribeWithNonParticipantIdentityDeliversMetadataOnly: scene type,
//     non-participant MetadataOnly=true, empty payload (INV-CRYPTO-15, INV-CRYPTO-6).
//   - TestSubscribeWithParticipantIdentityDeliversPlaintext: scene type,
//     participant MetadataOnly=false, payload == plaintext (INV-CRYPTO-15).
//
// New legs added in this file:
//   - Comms (character subject / page event type): recipient participant
//     receives plaintext (MetadataOnly=false).
//   - Comms non-recipient receives metadata-only (MetadataOnly=true).
//   - Combined three-party scene test: participant + non-participant in one
//     describe block for clarity, confirming asymmetry in a single harness run.
package crypto_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/plugintest"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	"github.com/holomush/holomush/test/testutil"
)

// buildCommsHarness is buildSubscribeHarness plus the test-plugin:page verb
// registration, so emitSensitivePage (which flows through
// RenderingPublisher.Lookup) resolves the page verb instead of failing
// EMIT_UNKNOWN_VERB.
func buildCommsHarness(t *testing.T) *subscribeHarness {
	t.Helper()
	return buildSubscribeHarness(t, core.VerbRegistration{
		Type:          "test-plugin:page",
		Category:      "communication",
		Format:        "speech",
		Label:         "pages",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        "test-plugin",
	})
}

// emitSensitivePage emits a sensitive page event to the character subject
// events.main.character.<recipientCharID> using a freshly-wired emitter.
// The verb type is test-plugin:page (parallel to test-plugin:whisper for
// scene events). The emit flows through PluginEventEmitter into the
// harness's RenderingPublisher, so the rendering lookup gate fires — which
// is why buildCommsHarness registers the page verb (an unregistered verb
// would hard-fail EMIT_UNKNOWN_VERB). Same path as binary-plugin gRPC
// EmitEvent against the production publisher.
//
// Asserts the comms domain uses a character-typed ContextID, not scene.
func emitSensitivePage(ctx context.Context, h *subscribeHarness, recipientCharID, plaintext string) {
	manifest := &plugins.Manifest{
		Name:                "test-plugin",
		Emits:               []string{"character"},
		ActorKindsClaimable: []string{"plugin"},
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "test-plugin:page", Sensitivity: plugins.SensitivityMay},
			},
		},
	}
	manifestLookup := func(name string) *plugins.Manifest {
		if name == "test-plugin" {
			return manifest
		}
		return nil
	}
	actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
		return core.Actor{
			Kind: core.ActorPlugin,
			ID:   plugintest.PluginULIDFromName("test-plugin").String(),
		}, nil
	}
	emitter := plugins.NewPluginEventEmitter(h.publisher, manifestLookup, actorResolver)

	intent := pluginsdk.EmitIntent{
		Subject:   "character." + recipientCharID,
		Type:      pluginsdk.EventType("test-plugin:page"),
		Payload:   plaintext,
		Sensitive: true,
	}
	err := emitter.Emit(ctx, "test-plugin", intent)
	Expect(err).NotTo(HaveOccurred(), "emitSensitivePage: emit must succeed")
}

// Asserts participant of a scene event receives plaintext AND a non-participant
// in the same harness run receives metadata-only — combined three-party proof.
// (The individual legs are also covered separately in metadata_only_test.go;
// this Describe shows both in a single harness invocation for clarity.)
var _ = Describe("Decrypt-to-participant: scene combined three-party proof", func() {
	It("participant receives plaintext and non-participant receives metadata-only in the same run", func() {
		ctx := context.Background()
		h := buildCommsHarness(suiteT)

		const (
			participantPlayerID    = "01DTPSCENEPLAYER000000001"
			participantCharacterID = "01DTPSCENECHARACT00000001"
			participantBindingID   = "01DTPSCENEBINDING0000001"

			nonPartPlayerID    = "01DTPSCENENONPLAYER000001"
			nonPartCharacterID = "01DTPSCENENONCHARACT00001"
			nonPartBindingID   = "01DTPSCENENONBINDING00001"
		)

		sceneID := "01DTPSCENETESTSCENE000001"
		ctxID := dek.ContextID{Type: "scene", ID: sceneID}

		// Pre-create DEK with participant only.
		_, err := h.dekMgr.GetOrCreate(ctx, ctxID, []dek.Participant{
			{
				PlayerID:    participantPlayerID,
				CharacterID: participantCharacterID,
				BindingID:   participantBindingID,
				JoinedAt:    time.Now().UTC(),
				AddedVia:    "test_setup",
			},
		})
		Expect(err).NotTo(HaveOccurred(), "pre-create DEK with scene participant")

		// Emit via PluginEventEmitter (same manifest/emitter setup as metadata_only_test.go).
		sceneManifest := &plugins.Manifest{
			Name:                "test-plugin",
			Emits:               []string{"scene"},
			ActorKindsClaimable: []string{"plugin"},
			Crypto: &plugins.CryptoSection{
				Emits: []plugins.CryptoEmit{
					{EventType: "test-plugin:whisper", Sensitivity: plugins.SensitivityMay},
				},
			},
		}
		sceneLookup := func(name string) *plugins.Manifest {
			if name == "test-plugin" {
				return sceneManifest
			}
			return nil
		}
		actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
			return core.Actor{
				Kind: core.ActorPlugin,
				ID:   plugintest.PluginULIDFromName("test-plugin").String(),
			}, nil
		}
		emitter := plugins.NewPluginEventEmitter(h.publisher, sceneLookup, actorResolver)

		const plaintext = `{"text":"scene secret for participant only"}`
		intent := pluginsdk.EmitIntent{
			Subject:   "scene." + sceneID,
			Type:      pluginsdk.EventType("test-plugin:whisper"),
			Payload:   plaintext,
			Sensitive: true,
		}

		// Open sessions BEFORE emitting so both consumers are subscribed
		// when the message lands on JetStream.
		participantID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    participantPlayerID,
			CharacterID: participantCharacterID,
			BindingID:   participantBindingID,
		}
		partStream, err := h.subscriber.OpenSession(ctx, "dtp-scene-part-session", participantID,
			[]eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = partStream.Close() })

		nonPartID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    nonPartPlayerID,
			CharacterID: nonPartCharacterID,
			BindingID:   nonPartBindingID,
		}
		nonPartStream, err := h.subscriber.OpenSession(ctx, "dtp-scene-nonpart-session", nonPartID,
			[]eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = nonPartStream.Close() })

		// Emit after both sessions are open.
		Expect(emitter.Emit(ctx, "test-plugin", intent)).NotTo(HaveOccurred())

		recvCtx, cancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer cancel()

		// Participant delivery: plaintext, MetadataOnly=false.
		partDelivery, err := partStream.Next(recvCtx)
		Expect(err).NotTo(HaveOccurred(), "participant session must receive the event")
		Expect(partDelivery.Ack()).NotTo(HaveOccurred())

		// Asserts participant of a live-crypto sensitive scene event receives decrypted
		// plaintext with MetadataOnly==false (end-to-end KEK+DEK+AEAD round-trip).
		Expect(partDelivery.MetadataOnly()).To(BeFalse(),
			"participant must receive MetadataOnly=false (decrypt-to-participant)")
		Expect(partDelivery.Event().Payload).To(Equal([]byte(plaintext)),
			"participant payload must equal original plaintext after AEAD decryption")

		// Non-participant delivery: metadata-only, empty payload.
		recvCtx2, cancel2 := context.WithTimeout(ctx, testutil.DefaultWait)
		defer cancel2()

		nonPartDelivery, err := nonPartStream.Next(recvCtx2)
		Expect(err).NotTo(HaveOccurred(), "non-participant session must receive the event")
		Expect(nonPartDelivery.Ack()).NotTo(HaveOccurred())

		// Asserts non-participant receives metadata-only with empty payload under live crypto.
		Expect(nonPartDelivery.MetadataOnly()).To(BeTrue(),
			"non-participant must receive MetadataOnly=true (no plaintext)")
		Expect(nonPartDelivery.Event().Payload).To(BeEmpty(),
			"non-participant payload must be empty — no plaintext leaks")
		Expect(nonPartDelivery.Event().Type).To(Equal(eventbus.Type("test-plugin:whisper")),
			"non-participant metadata: event type must still be visible")
	})
})

// Asserts comms recipient (page to character subject) receives decrypted plaintext
// under live crypto, and a non-recipient receives metadata-only.
// This proves the decrypt-to-participant behaviour is domain-generic
// (character context, not just scene context).
var _ = Describe("Decrypt-to-participant: comms (character subject) domain proof", func() {
	It("comms recipient receives plaintext and non-recipient receives metadata-only", func() {
		ctx := context.Background()
		// buildCommsHarness registers both test-plugin:whisper and test-plugin:page
		// in the RenderingPublisher's verb registry so emitSensitivePage succeeds.
		h := buildCommsHarness(suiteT)

		const (
			recipientPlayerID  = "01DTPCOMMRECIPIENTPLAYER0"
			recipientCharID    = "01DTPCOMMRECIPIENTCHAR000"
			recipientBindingID = "01DTPCOMMRECIPIENTBIND000"

			nonRecipientPlayerID  = "01DTPCOMMNONRECIPPLAYER00"
			nonRecipientCharID    = "01DTPCOMMNONRECIPCHAR0000"
			nonRecipientBindingID = "01DTPCOMMNONRECIPBIND0000"
		)

		// For character context, the subject is "character.<recipientCharID>".
		// ContextID type becomes "character", derived by contextIDFromSubject.
		// Pre-create the DEK for this character context with the recipient as participant.
		ctxID := dek.ContextID{Type: "character", ID: recipientCharID}
		_, err := h.dekMgr.GetOrCreate(ctx, ctxID, []dek.Participant{
			{
				PlayerID:    recipientPlayerID,
				CharacterID: recipientCharID,
				BindingID:   recipientBindingID,
				JoinedAt:    time.Now().UTC(),
				AddedVia:    "test_setup",
			},
		})
		Expect(err).NotTo(HaveOccurred(), "pre-create DEK for character context with recipient")

		// Open sessions before emitting.
		recipientID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    recipientPlayerID,
			CharacterID: recipientCharID,
			BindingID:   recipientBindingID,
		}
		recipientStream, err := h.subscriber.OpenSession(ctx, "dtp-comms-recipient-session", recipientID,
			[]eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = recipientStream.Close() })

		nonRecipientID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    nonRecipientPlayerID,
			CharacterID: nonRecipientCharID,
			BindingID:   nonRecipientBindingID,
		}
		nonRecipientStream, err := h.subscriber.OpenSession(ctx, "dtp-comms-nonrecipient-session", nonRecipientID,
			[]eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = nonRecipientStream.Close() })

		// Emit sensitive page event to character subject.
		const plaintext = `{"text":"private page to recipient only"}`
		emitSensitivePage(ctx, h, recipientCharID, plaintext)

		recvCtx, cancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer cancel()

		// Recipient delivery: plaintext, MetadataOnly=false.
		recipientDelivery, err := recipientStream.Next(recvCtx)
		Expect(err).NotTo(HaveOccurred(), "recipient session must receive the page event")
		Expect(recipientDelivery.Ack()).NotTo(HaveOccurred())

		// Asserts comms recipient (page on character subject) receives decrypted
		// plaintext under live crypto — proves domain-generic decrypt-to-participant.
		Expect(recipientDelivery.MetadataOnly()).To(BeFalse(),
			"comms recipient must receive MetadataOnly=false (decrypt-to-participant)")
		Expect(recipientDelivery.Event().Payload).To(Equal([]byte(plaintext)),
			"comms recipient payload must equal original plaintext after AEAD decryption")

		// Non-recipient delivery: metadata-only, empty payload.
		recvCtx2, cancel2 := context.WithTimeout(ctx, testutil.DefaultWait)
		defer cancel2()

		nonRecipientDelivery, err := nonRecipientStream.Next(recvCtx2)
		Expect(err).NotTo(HaveOccurred(), "non-recipient session must receive the event (metadata-only)")
		Expect(nonRecipientDelivery.Ack()).NotTo(HaveOccurred())

		// Asserts non-recipient of a comms page event receives metadata-only under live crypto.
		Expect(nonRecipientDelivery.MetadataOnly()).To(BeTrue(),
			"non-recipient must receive MetadataOnly=true (no plaintext leaks)")
		Expect(nonRecipientDelivery.Event().Payload).To(BeEmpty(),
			"non-recipient payload must be empty — no comms plaintext visible")
		Expect(nonRecipientDelivery.Event().Type).To(Equal(eventbus.Type("test-plugin:page")),
			"non-recipient metadata: event type must still be visible")
	})
})
