// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package crypto_test — genesis DEK-seeding asymmetry integration coverage.
//
// This file proves the genesis-seed asymmetry end-to-end (INV-CRYPTO-116;
// legacy provenance recorded in the registry):
//
//   - A character.<id> personal context auto-seeds its recipient at genesis
//     (Task 6 + Task 7) → the recipient decrypts a sensitive page with NO
//     pre-seed, while a third party (different binding) on the same stream
//     receives metadata-only.
//   - A scene.<id> context seeds no one at genesis → an unseeded subscriber
//     receives metadata-only (scene readers are seeded only at SetSceneFocus).
//
// Subscriber decrypt mechanics for pre-seeded participants are covered by
// metadata_only_test.go; this file proves the seeding, not the decode.
package crypto_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/plugintest"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/holomush/holomush/test/testutil"
)

// Verifies: INV-CRYPTO-116
var _ = Describe("Genesis DEK seeding asymmetry (character auto-seeds, scene does not)", func() {
	It("a page recipient decrypts with NO pre-seed; a third party gets metadata-only", func() {
		ctx := context.Background()
		h := buildSubscribeHarness(suiteT)

		recipientCharID := "01HRECIPIENTCHARACTER0000"
		const (
			recipientPlayerID = "01HRECIPIENTPLAYER000000"
			thirdPlayerID     = "01HTHIRDPLAYER0000000000"
			thirdCharacterID  = "01HTHIRDCHARACTER0000000"
		)

		// Emit a sensitive page to character.<recipient> with NO pre-seed. The
		// publisher's genesis (Task 7) derives initial=[recipient]; GetOrCreate
		// (Task 6) resolves the recipient's binding via the harness stub
		// ("bind-metadata"), minting the DEK with the recipient as participant.
		manifest := &plugins.Manifest{
			Name:                "test-plugin",
			Emits:               []string{"character"},
			ActorKindsClaimable: []string{"plugin"},
			Crypto: &plugins.CryptoSection{
				Emits: []plugins.CryptoEmit{
					{EventType: "test-plugin:whisper", Sensitivity: plugins.SensitivityMay},
				},
			},
		}
		manifestLookup := func(name string) *plugins.Manifest {
			if name == "test-plugin" {
				return manifest
			}
			return nil
		}
		actorID := plugintest.PluginULIDFromName("test-plugin").String()
		actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: actorID}, nil
		}
		emitter := plugins.NewPluginEventEmitter(h.publisher, manifestLookup, actorResolver)

		const plaintext = `{"text":"a private page for the recipient only"}`
		Expect(emitter.Emit(ctx, "test-plugin", pluginsdk.EmitIntent{
			Subject:   "character." + recipientCharID,
			Type:      pluginsdk.EventType("test-plugin:whisper"),
			Payload:   plaintext,
			Sensitive: true,
		})).NotTo(HaveOccurred())

		// Recipient identity uses the stub's resolved binding ("bind-metadata").
		recipientID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    recipientPlayerID,
			CharacterID: recipientCharID,
			BindingID:   "bind-metadata",
		}
		recStream, err := h.subscriber.OpenSession(ctx, "recipient-"+recipientCharID, recipientID,
			[]eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = recStream.Close() })

		rcv, cancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer cancel()
		d, err := recStream.Next(rcv)
		Expect(err).NotTo(HaveOccurred(), "expected delivery for recipient")
		Expect(d.Ack()).NotTo(HaveOccurred())
		Expect(d.MetadataOnly()).To(BeFalse(), "genesis auto-seeded the recipient")
		Expect(d.Event().Payload).To(Equal([]byte(plaintext)), "INV-CRYPTO-116: recipient decrypts page")

		// A third party (different binding) on the same stream gets metadata-only.
		thirdID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    thirdPlayerID,
			CharacterID: thirdCharacterID,
			BindingID:   "bind-OTHER",
		}
		thirdStream, err := h.subscriber.OpenSession(ctx, "third-"+recipientCharID, thirdID,
			[]eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = thirdStream.Close() })

		thirdRcv, thirdCancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer thirdCancel()
		td, err := thirdStream.Next(thirdRcv)
		Expect(err).NotTo(HaveOccurred(), "expected delivery for third party")
		Expect(td.Ack()).NotTo(HaveOccurred())
		Expect(td.MetadataOnly()).To(BeTrue(), "non-recipient on the personal stream gets metadata-only")
		Expect(td.Event().Payload).To(BeEmpty())
	})

	It("a scene context seeds no one at genesis (subscriber gets metadata-only with no pre-seed)", func() {
		ctx := context.Background()
		h := buildSubscribeHarness(suiteT)
		sceneID := "01HXXXGENESISSCENE000001"

		manifest := &plugins.Manifest{
			Name:                "test-plugin",
			Emits:               []string{"scene"},
			ActorKindsClaimable: []string{"plugin"},
			Crypto: &plugins.CryptoSection{
				Emits: []plugins.CryptoEmit{
					{EventType: "test-plugin:whisper", Sensitivity: plugins.SensitivityMay},
				},
			},
		}
		manifestLookup := func(name string) *plugins.Manifest {
			if name == "test-plugin" {
				return manifest
			}
			return nil
		}
		actorID := plugintest.PluginULIDFromName("test-plugin").String()
		actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: actorID}, nil
		}
		emitter := plugins.NewPluginEventEmitter(h.publisher, manifestLookup, actorResolver)

		Expect(emitter.Emit(ctx, "test-plugin", pluginsdk.EmitIntent{
			Subject:   "scene." + sceneID,
			Type:      pluginsdk.EventType("test-plugin:whisper"),
			Payload:   `{"text":"unseeded scene pose"}`,
			Sensitive: true,
		})).NotTo(HaveOccurred())

		anyID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    "01HANYPLAYER000000000000",
			CharacterID: "01HANYCHARACTER000000000",
			BindingID:   "bind-metadata",
		}
		stream, err := h.subscriber.OpenSession(ctx, "any-"+sceneID, anyID, []eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		rcv, cancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer cancel()
		d, err := stream.Next(rcv)
		Expect(err).NotTo(HaveOccurred(), "expected delivery for unseeded subscriber")
		Expect(d.Ack()).NotTo(HaveOccurred())
		Expect(d.MetadataOnly()).To(BeTrue(), "scene genesis seeds no one; readers seeded only at SetSceneFocus")
	})
})
