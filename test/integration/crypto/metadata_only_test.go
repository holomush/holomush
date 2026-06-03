// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package crypto_test — Phase 3b integration tests for the decrypt-on-fanout
// subscriber path. This file covers INV-CRYPTO-15 (delivery contract: non-participant
// receives MetadataOnly, participant receives plaintext) and by extension
// INV-CRYPTO-6 (no plaintext to non-participant).
package crypto_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/authguard"
	guardaudit "github.com/holomush/holomush/internal/eventbus/authguard/audit"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/plugintest"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	"github.com/holomush/holomush/test/testutil"
)

// noopBackpressure is a BackpressureChecker that never throttles.
type noopBackpressure struct{}

func (noopBackpressure) ShouldThrottle(_ string) bool { return false }

// alwaysDenyManifest is a ManifestLookup that denies all plugin decrypt requests.
// The character identity path does not consult ManifestLookup; this stub
// prevents a nil-pointer panic in Guard if the plugin path were ever triggered.
type alwaysDenyManifest struct{}

func (alwaysDenyManifest) PluginRequestsDecryption(_, _ string) bool { return false }
func (alwaysDenyManifest) PluginCanReadBack(_, _ string) bool        { return false }

// subscribeHarness holds all components needed for the subscriber-side tests.
type subscribeHarness struct {
	publisher  eventbus.Publisher
	subscriber *eventbus.JetStreamSubscriber
	dekMgr     dek.Manager
}

// buildSubscribeHarness constructs the full crypto stack — PostgreSQL, embedded
// JetStream, DEK manager, AuthGuard, audit emitter — and returns the harness.
// The harness is self-contained: the publisher can emit sensitive events and the
// subscriber can open sessions and receive deliveries.
func buildSubscribeHarness(t *testing.T) *subscribeHarness {
	t.Helper()
	ctx := context.Background()

	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool := newPool(t, connStr)

	bus := testutil.StartEmbeddedJetStream(t)

	kekHex := testutil.RandomKEKHex(t)
	t.Setenv("HOLOMUSH_TEST_SUBSCRIBER_KEK", kekHex)
	kekSource := kek.NewEnvSource("HOLOMUSH_TEST_SUBSCRIBER_KEK", false)

	provider, err := kek.NewLocalAEADProvider(ctx, kekSource, pool)
	if err != nil {
		t.Fatalf("buildSubscribeHarness: NewLocalAEADProvider: %v", err)
	}

	dekStore := dek.NewStore(pool)
	dekCache := dek.NewCache(dek.CacheConfig{Capacity: 64})
	dekPartCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64})
	dekMgr, err := dek.NewManager(provider, dekStore, dekCache, dekPartCache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&dekBindingStub{bindingID: "bind-metadata"})
	if err != nil {
		t.Fatalf("buildSubscribeHarness: NewManager: %v", err)
	}

	// Audit subsystem: needed so events_audit is populated (same pattern as
	// emit_test.go). Not strictly required for subscribe assertions but keeps
	// the test environment representative.
	hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	if err := hostSub.Start(ctx); err != nil {
		t.Fatalf("buildSubscribeHarness: hostSub.Start: %v", err)
	}
	t.Cleanup(func() { _ = hostSub.Stop(context.Background()) })

	// Verb registry: the RenderingPublisher requires test-plugin:whisper to be
	// registered so the App-Rendering header is stamped correctly.
	registry, err := core.BootstrapVerbRegistry("test")
	if err != nil {
		t.Fatalf("buildSubscribeHarness: BootstrapVerbRegistry: %v", err)
	}
	if err := registry.RegisterWithSource(core.VerbRegistration{
		Type:          "test-plugin:whisper",
		Category:      "communication",
		Format:        "speech",
		Label:         "whispers",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        "test-plugin",
	}, "1.0.0"); err != nil {
		t.Fatalf("buildSubscribeHarness: RegisterWithSource: %v", err)
	}

	rawPub := eventbus.NewJetStreamPublisher(
		bus.JS,
		eventbus.Config{}.Defaults(),
		eventbus.WithDEKManager(dekMgr),
	)
	hostPub := eventbus.NewRenderingPublisher(rawPub, registry)

	// AuthGuard wiring.
	//   - ParticipantLookup: backed by the real DEK manager (reads crypto_keys).
	//   - ManifestLookup: stub that denies all plugin decrypt (character path skips this).
	//   - ABACEngine: AllowAllEngine (character path skips ABAC; required non-nil).
	//   - BackpressureChecker: never throttles.
	participantLookup := authguard.NewDEKParticipantLookup(dekMgr)
	abacEngine := policytest.AllowAllEngine()
	guard, err := authguard.New(participantLookup, alwaysDenyManifest{}, abacEngine, noopBackpressure{})
	if err != nil {
		t.Fatalf("buildSubscribeHarness: authguard.New: %v", err)
	}

	sessionGuard := authguard.NewSessionBridgeGuard(guard)

	// Audit emitter for plugin decrypt records. nil is safe for character
	// sessions; pass it anyway so WithSubscriberDecryptAuditEmitter is wired.
	auditEmitter, err := guardaudit.NewQueuedEmitter(rawPub)
	if err != nil {
		t.Fatalf("buildSubscribeHarness: NewQueuedEmitter: %v", err)
	}
	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = auditEmitter.Shutdown(shutCtx)
	})
	sessionAuditEmitter, err := guardaudit.NewSessionBridgeEmitter(auditEmitter)
	if err != nil {
		t.Fatalf("buildSubscribeHarness: NewSessionBridgeEmitter: %v", err)
	}

	sub := eventbus.NewJetStreamSubscriber(
		bus.JS,
		eventbus.WithSubscriberAuthGuard(sessionGuard),
		eventbus.WithSubscriberDEKManager(dekMgr),
		eventbus.WithSubscriberDecryptAuditEmitter(sessionAuditEmitter),
		// Short AckWait for test teardown speed.
		eventbus.WithSessionAckWait(5*time.Second),
		eventbus.WithSessionInactiveThreshold(30*time.Second),
	)

	return &subscribeHarness{
		publisher:  hostPub,
		subscriber: sub,
		dekMgr:     dekMgr,
	}
}

// TestSubscribeWithNonParticipantIdentityDeliversMetadataOnly covers INV-CRYPTO-15
// (delivery contract) and INV-CRYPTO-6 (no plaintext to non-participant):
//
//   - Publishes a sensitive event whose DEK has exactly one participant.
//   - Opens a session as a different identity (non-participant binding_id).
//   - Asserts: MetadataOnly()==true, Event().Payload is empty.
var _ = Describe("Subscribe with non-participant identity delivers MetadataOnly (INV-CRYPTO-15, INV-CRYPTO-6)", func() {
	It("non-participant receives MetadataOnly=true with empty payload", func() {
		ctx := context.Background()
		h := buildSubscribeHarness(suiteT)

		const (
			participantPlayerID    = "01PARTICIPANTPLAYER00000"
			participantCharacterID = "01PARTICIPANTCHARACT0000"
			participantBindingID   = "01PARTICIPANTBINDING0000"

			nonParticipantPlayerID    = "01NONPARTICIPANTPLAYER00"
			nonParticipantCharacterID = "01NONPARTICIPANTCHARACT0"
			nonParticipantBindingID   = "01NONPARTICIPANTBINDING0"
		)

		sceneID := "01HXXXTESTSCENE000000001"

		// Pre-create the DEK with the participant set BEFORE the publisher calls
		// GetOrCreate. Because GetOrCreate is idempotent (INSERT or SELECT), the
		// publisher's subsequent GetOrCreate(ctx, ctxID, nil) call will return the
		// existing DEK row — with participants already stored.
		ctxID := dek.ContextID{Type: "scene", ID: sceneID}
		_, err := h.dekMgr.GetOrCreate(ctx, ctxID, []dek.Participant{
			{
				PlayerID:    participantPlayerID,
				CharacterID: participantCharacterID,
				BindingID:   participantBindingID,
				JoinedAt:    time.Now().UTC(),
				AddedVia:    "test_setup",
			},
		})
		Expect(err).NotTo(HaveOccurred(), "pre-create DEK with participant")

		// Emit the sensitive event. The publisher resolves the ContextID from the
		// translated subject (events.main.scene.<sceneID>) and calls GetOrCreate
		// which returns the existing DEK (participants already set).
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
		// Post-w9ml: Actor.ID MUST be a ULID string (strict-gate
		// coreActorToEventbusActor rejects non-ULID IDs).
		testPluginActorID := plugintest.PluginULIDFromName("test-plugin").String()
		actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: testPluginActorID}, nil
		}
		emitter := plugins.NewPluginEventEmitter(
			h.publisher, manifestLookup, actorResolver,
			plugins.WithCryptoEnabled(true),
		)

		const plaintext = `{"text":"secret message for participant only"}`
		intent := pluginsdk.EmitIntent{
			Subject:   "scene." + sceneID,
			Type:      pluginsdk.EventType("test-plugin:whisper"),
			Payload:   plaintext,
			Sensitive: true,
		}
		Expect(emitter.Emit(ctx, "test-plugin", intent)).NotTo(HaveOccurred())

		// Open a session as the NON-participant identity.
		nonParticipantID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    nonParticipantPlayerID,
			CharacterID: nonParticipantCharacterID,
			BindingID:   nonParticipantBindingID,
		}
		sessionID := "nonparticipant-session-" + sceneID

		stream, err := h.subscriber.OpenSession(ctx, sessionID, nonParticipantID, []eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		receiveCtx, cancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer cancel()

		delivery, err := stream.Next(receiveCtx)
		Expect(err).NotTo(HaveOccurred(), "expected delivery from stream")
		Expect(delivery.Ack()).NotTo(HaveOccurred())

		// INV-CRYPTO-15: non-participant must receive metadata-only delivery (no plaintext).
		Expect(delivery.MetadataOnly()).To(BeTrue(), "INV-CRYPTO-15: non-participant must receive MetadataOnly=true")
		// INV-CRYPTO-6: the delivery payload must be empty — no plaintext visible to non-participant.
		Expect(delivery.Event().Payload).To(BeEmpty(), "INV-CRYPTO-6: non-participant payload must be empty bytes")

		// Metadata fields must still be present.
		Expect(delivery.Event().Type).To(Equal(eventbus.Type("test-plugin:whisper")),
			"metadata: event type must be visible to non-participant")
		Expect(delivery.Event().Timestamp).NotTo(BeZero(), "metadata: timestamp must be visible")
	})
})

// TestSubscribeWithParticipantIdentityDeliversPlaintext covers INV-CRYPTO-15
// (delivery contract — permit path):
//
//   - Same setup as the non-participant test above, but opens the session
//     as the participant identity.
//   - Asserts: MetadataOnly()==false, Event().Payload equals the original JSON.
var _ = Describe("Subscribe with participant identity delivers plaintext (INV-CRYPTO-15)", func() {
	It("participant receives MetadataOnly=false with original plaintext", func() {
		ctx := context.Background()
		h := buildSubscribeHarness(suiteT)

		const (
			participantPlayerID    = "01PARTICIPANTPLAYER00001"
			participantCharacterID = "01PARTICIPANTCHARACT0001"
			participantBindingID   = "01PARTICIPANTBINDING0001"
		)

		sceneID := "01HXXXTESTSCENE000000002"

		// Pre-create DEK with participant.
		ctxID := dek.ContextID{Type: "scene", ID: sceneID}
		_, err := h.dekMgr.GetOrCreate(ctx, ctxID, []dek.Participant{
			{
				PlayerID:    participantPlayerID,
				CharacterID: participantCharacterID,
				BindingID:   participantBindingID,
				JoinedAt:    time.Now().UTC(),
				AddedVia:    "test_setup",
			},
		})
		Expect(err).NotTo(HaveOccurred(), "pre-create DEK with participant")

		// Emit the sensitive event.
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
		// Post-w9ml: Actor.ID MUST be a ULID string (strict-gate
		// coreActorToEventbusActor rejects non-ULID IDs).
		testPluginActorID := plugintest.PluginULIDFromName("test-plugin").String()
		actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: testPluginActorID}, nil
		}
		emitter := plugins.NewPluginEventEmitter(
			h.publisher, manifestLookup, actorResolver,
			plugins.WithCryptoEnabled(true),
		)

		const plaintext = `{"text":"hello participant"}`
		intent := pluginsdk.EmitIntent{
			Subject:   "scene." + sceneID,
			Type:      pluginsdk.EventType("test-plugin:whisper"),
			Payload:   plaintext,
			Sensitive: true,
		}
		Expect(emitter.Emit(ctx, "test-plugin", intent)).NotTo(HaveOccurred())

		// Open a session as the PARTICIPANT identity.
		participantID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    participantPlayerID,
			CharacterID: participantCharacterID,
			BindingID:   participantBindingID,
		}
		sessionID := "participant-session-" + sceneID

		stream, err := h.subscriber.OpenSession(ctx, sessionID, participantID, []eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		receiveCtx, cancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer cancel()

		delivery, err := stream.Next(receiveCtx)
		Expect(err).NotTo(HaveOccurred(), "expected delivery from stream")
		Expect(delivery.Ack()).NotTo(HaveOccurred())

		// INV-CRYPTO-15: participant must receive full plaintext (MetadataOnly=false).
		Expect(delivery.MetadataOnly()).To(BeFalse(), "INV-CRYPTO-15: participant must receive MetadataOnly=false")

		// INV-CRYPTO-6 (permit path): payload must be the original plaintext.
		// The publisher encrypts event.Payload (the JSON bytes) and the subscriber
		// decrypts to recover the original bytes.
		Expect(delivery.Event().Payload).To(Equal([]byte(plaintext)),
			"INV-CRYPTO-15: participant payload must equal original plaintext")

		// Metadata fields also present.
		Expect(delivery.Event().Type).To(Equal(eventbus.Type("test-plugin:whisper")),
			"metadata: event type must be visible to participant")
		Expect(delivery.Event().Timestamp).NotTo(BeZero(), "metadata: timestamp must be visible")
	})
})
