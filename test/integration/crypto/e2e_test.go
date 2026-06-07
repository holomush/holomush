// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package crypto_test — Phase 3d Task 10: end-to-end happy-path BDD coverage.
//
// This file lifts the existing emit_test.go + metadata_only_test.go fixture
// into a Ginkgo Describe and adds the cold-tier round-trip cases for both
// character-binding (the participant scenario) and plugin-actor (the
// Decision 5 regression lock).
//
// Cold-tier wiring: as of holomush-ojw1.7 (2026-05-06), history.NewReader
// exposes WithHistoryAuth(g, m, em) which wires both hot and cold tiers
// symmetrically. The cold-read tests use buildColdReader which calls the
// real production postgres cold tier via WithHistoryAuth. Bypassing the
// hot tier deterministically is achieved by setting the clock to "far
// future" so every query routes to cold.
package crypto_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

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
	"github.com/holomush/holomush/internal/eventbus/history"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/plugintest"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"

	"github.com/holomush/holomush/test/testutil"
)

// dekBindingStub implements dek.BindingResolver for single-process E2E tests
// where no real wizard binding store exists.
type dekBindingStub struct {
	bindingID string
}

func (s *dekBindingStub) Current(_ context.Context, _ string) (string, error) {
	return s.bindingID, nil
}

// e2eEnv groups the bus + crypto + audit + reader fixture used by the
// happy-path BDD spec. Built per BeforeEach to keep PG/JS isolation.
type e2eEnv struct {
	pool       *pgxpool.Pool
	bus        *testutil.EmbeddedBus
	publisher  eventbus.Publisher
	subscriber *eventbus.JetStreamSubscriber
	dekMgr     dek.Manager
	hostSub    *audit.Subsystem
	guard      eventbus.SessionAuthGuard
	auditEm    eventbus.SessionAuditEmitter
	cleanup    []func()
}

func (e *e2eEnv) Teardown() {
	for i := len(e.cleanup) - 1; i >= 0; i-- {
		e.cleanup[i]()
	}
}

// setupE2EEnv builds the full Phase-3d sensitive flow stack: PG, embedded
// JetStream, DEK manager, audit subsystem, AuthGuard, publisher, subscriber.
func setupE2EEnv(ctx context.Context, t *testing.T) *e2eEnv {
	t.Helper()

	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)

	poolCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(poolCtx, connStr)
	require.NoError(t, err)

	bus := testutil.StartEmbeddedJetStream(t)

	kekHex := testutil.RandomKEKHex(t)
	t.Setenv("HOLOMUSH_TEST_E2E_KEK", kekHex)
	kekSource := kek.NewEnvSource("HOLOMUSH_TEST_E2E_KEK", false)
	provider, err := kek.NewLocalAEADProvider(ctx, kekSource, pool)
	require.NoError(t, err)

	dekStore := dek.NewStore(pool)
	dekCache := dek.NewCache(dek.CacheConfig{Capacity: 64})
	dekPartCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64})
	dekMgr, err := dek.NewManager(provider, dekStore, dekCache, dekPartCache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&dekBindingStub{bindingID: "bind-e2e"})
	require.NoError(t, err)

	hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, hostSub.Start(ctx))

	registry, err := core.BootstrapVerbRegistry("test")
	require.NoError(t, err)
	require.NoError(t, registry.RegisterWithSource(core.VerbRegistration{
		Type:          "test-plugin:whisper",
		Category:      "communication",
		Format:        "speech",
		Label:         "whispers",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        "test-plugin",
	}, "1.0.0"))

	rawPub := eventbus.NewJetStreamPublisher(
		bus.JS,
		eventbus.Config{}.Defaults(),
		eventbus.WithDEKManager(dekMgr),
	)
	hostPub := eventbus.NewRenderingPublisher(rawPub, registry)

	participantLookup := authguard.NewDEKParticipantLookup(dekMgr)
	abacEngine := policytest.AllowAllEngine()
	guardCore, err := authguard.New(participantLookup, alwaysDenyManifest{}, abacEngine, noopBackpressure{})
	require.NoError(t, err)
	sessionGuard := authguard.NewSessionBridgeGuard(guardCore)

	auditEmitter, err := guardaudit.NewQueuedEmitter(rawPub)
	require.NoError(t, err)
	sessionAuditEmitter, err := guardaudit.NewSessionBridgeEmitter(auditEmitter)
	require.NoError(t, err)

	sub := eventbus.NewJetStreamSubscriber(
		bus.JS,
		eventbus.WithSubscriberAuthGuard(sessionGuard),
		eventbus.WithSubscriberDEKManager(dekMgr),
		eventbus.WithSubscriberDecryptAuditEmitter(sessionAuditEmitter),
		eventbus.WithSessionAckWait(5*time.Second),
		eventbus.WithSessionInactiveThreshold(30*time.Second),
	)

	env := &e2eEnv{
		pool:       pool,
		bus:        bus,
		publisher:  hostPub,
		subscriber: sub,
		dekMgr:     dekMgr,
		hostSub:    hostSub,
		guard:      sessionGuard,
		auditEm:    sessionAuditEmitter,
	}
	env.cleanup = append(
		env.cleanup,
		func() { pool.Close() },
		func() { _ = hostSub.Stop(context.Background()) },
		func() {
			shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = auditEmitter.Shutdown(shutCtx)
		},
	)
	return env
}

// emitSensitivePluginEvent pre-creates the DEK with the given participants
// and emits a sensitive event via the plugin emitter (Actor.kind=PLUGIN,
// Actor.legacy_id=pluginName).
func emitSensitivePluginEvent(
	ctx context.Context, t *testing.T,
	env *e2eEnv,
	subject, plaintext string,
	participants []dek.Participant,
	pluginName string,
) {
	t.Helper()
	if len(participants) > 0 {
		ctxID, ok := contextIDFromRelativeSubject(subject)
		require.True(t, ok, "subject must yield a ContextID")
		_, err := env.dekMgr.GetOrCreate(ctx, ctxID, participants)
		require.NoError(t, err)
	}
	manifest := &plugins.Manifest{
		Name:                pluginName,
		Emits:               []string{"scene"},
		ActorKindsClaimable: []string{"plugin"},
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "test-plugin:whisper", Sensitivity: plugins.SensitivityMay},
			},
		},
	}
	manifestLookup := func(name string) *plugins.Manifest {
		if name == pluginName {
			return manifest
		}
		return nil
	}
	// Post-w9ml: Actor.ID MUST be a ULID string (the strict-gate
	// coreActorToEventbusActor rejects non-ULID actor IDs). Use the
	// deterministic-by-name fixture helper so test assertions remain
	// stable across runs.
	pluginActorID := plugintest.PluginULIDFromName(pluginName).String()
	actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
		return core.Actor{Kind: core.ActorPlugin, ID: pluginActorID}, nil
	}
	emitter := plugins.NewPluginEventEmitter(
		env.publisher, manifestLookup, actorResolver,
	)
	intent := pluginsdk.EmitIntent{
		Subject:   subject,
		Type:      pluginsdk.EventType("test-plugin:whisper"),
		Payload:   plaintext,
		Sensitive: true,
	}
	require.NoError(t, emitter.Emit(ctx, pluginName, intent))
}

// publishSensitiveWithPluginActor publishes a sensitive event directly
// through env.publisher with Actor.kind=PLUGIN and Actor.ID set to the
// given plugin ULID. Used by the regression-lock test for AAD round-trip
// of plugin-authored sensitive events.
//
// Post-w9ml retarget (formerly publishSensitiveWithLegacyActor): the
// LegacyID-bearing AAD round-trip is replaced by ULID-based plugin
// identity. Test intent unchanged: a sensitive plugin-authored event
// MUST round-trip its actor identity through publisher → AAD → cold
// reader → AEAD verify.
func publishSensitiveWithPluginActor(
	ctx context.Context, t *testing.T,
	env *e2eEnv,
	subject, eventType, plaintext string,
	pluginActorID ulid.ULID,
	participants []dek.Participant,
) {
	t.Helper()
	if len(participants) > 0 {
		// Subject here is post-translation form: events.<game>.scene.<id>.
		ctxID, ok := contextIDFromTranslatedSubject(subject)
		require.True(t, ok, "subject must yield a ContextID")
		_, err := env.dekMgr.GetOrCreate(ctx, ctxID, participants)
		require.NoError(t, err)
	}
	id := ulid.MustNew(ulid.Timestamp(time.Now()), nil)
	ev := eventbus.Event{
		ID:        id,
		Subject:   eventbus.Subject(subject),
		Type:      eventbus.Type(eventType),
		Timestamp: time.Now().UTC(),
		Actor: eventbus.Actor{
			Kind: eventbus.ActorKindPlugin,
			ID:   pluginActorID,
		},
		Payload:   []byte(plaintext),
		Sensitive: true,
	}
	require.NoError(t, env.publisher.Publish(ctx, ev))
}

// contextIDFromTranslatedSubject parses "events.<game>.scene.<id>" form.
func contextIDFromTranslatedSubject(subject string) (dek.ContextID, bool) {
	parts := []string{}
	cur := ""
	for _, r := range subject {
		if r == '.' {
			parts = append(parts, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	parts = append(parts, cur)
	if len(parts) >= 4 && parts[0] == "events" && parts[2] == "scene" {
		return dek.ContextID{Type: "scene", ID: parts[3]}, true
	}
	return dek.ContextID{}, false
}

// contextIDFromRelativeSubject parses "scene.<id>" dot-relative forms used by
// the plugin emit path. Mirrors publisher.contextIDFromSubject's behavior for
// the pre-qualification form (the publisher receives the post-qualification
// "events.main.scene.<id>" form).
func contextIDFromRelativeSubject(subject string) (dek.ContextID, bool) {
	if len(subject) > 6 && subject[:6] == "scene." {
		return dek.ContextID{Type: "scene", ID: subject[6:]}, true
	}
	return dek.ContextID{}, false
}

// buildColdReader wires a Reader using WithHistoryAuth so the real
// production cold tier (postgresColdTier) handles decodeAuthAndDispatch
// with the AuthGuard + DEKManager + AuditEmitter populated. The hot tier
// is left as the default (it sees the same JetStream the publisher
// just wrote to), but our queries set the clock to "far future" so every
// event is read from cold.
func buildColdReader(env *e2eEnv) *history.Reader {
	farFuture := time.Now().UTC().Add(100 * 365 * 24 * time.Hour)
	return history.NewReader(
		env.bus.JS, env.pool,
		eventbus.Config{}.Defaults().StreamMaxAge,
		func() time.Time { return farFuture },
		history.WithHistoryAuth(env.guard, env.dekMgr, env.auditEm),
	)
}

// ---------------------------------------------------------------------------
// Ginkgo BDD specs
// ---------------------------------------------------------------------------

var _ = Describe("Sensitive event end-to-end", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		env    *e2eEnv
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		env = setupE2EEnv(ctx, suiteT)
	})

	AfterEach(func() {
		env.Teardown()
		cancel()
	})

	Describe("character-binding sensitive event (plugin emit on character's behalf)", func() {
		// In Phase-3d's API surface the only sensitive emit path is the
		// plugin emitter (Actor.kind=PLUGIN). A "character-authored"
		// sensitive event is shaped via DEK participants carrying the
		// character's binding. We exercise both hot- and cold-tier
		// participant / non-participant scenarios on this path.

		It("delivers plaintext to the participant via hot tier", func() {
			sceneID := "01HEEHOTPART000000000000"
			participantID := eventbus.SessionIdentity{
				Kind:        eventbus.IdentityKindCharacter,
				PlayerID:    "01PLAYERAA000000000000000",
				CharacterID: "01CHARAA0000000000000000",
				BindingID:   "01BINDAA0000000000000000",
			}
			emitSensitivePluginEvent(
				ctx, suiteT, env,
				"scene."+sceneID,
				`{"text":"hello hot participant"}`,
				[]dek.Participant{{
					PlayerID:    participantID.PlayerID,
					CharacterID: participantID.CharacterID,
					BindingID:   participantID.BindingID,
					JoinedAt:    time.Now().UTC(),
					AddedVia:    "test_setup",
				}},
				"test-plugin",
			)

			stream, err := env.subscriber.OpenSession(ctx, "hot-part-"+sceneID, participantID, []eventbus.Subject{"events.>"}, time.Time{})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = stream.Close() })

			recvCtx, recvCancel := context.WithTimeout(ctx, testutil.DefaultWait)
			defer recvCancel()
			delivery, err := stream.Next(recvCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(delivery.Ack()).To(Succeed())

			Expect(delivery.MetadataOnly()).To(BeFalse())
			Expect(string(delivery.Event().Payload)).To(Equal(`{"text":"hello hot participant"}`))
		})

		It("delivers metadata-only to a non-participant via hot tier", func() {
			sceneID := "01HEEHOTNON0000000000000"
			emitSensitivePluginEvent(
				ctx, suiteT, env,
				"scene."+sceneID,
				`{"text":"hot secret"}`,
				[]dek.Participant{{
					PlayerID:    "01PLAYERBB000000000000000",
					CharacterID: "01CHARBB0000000000000000",
					BindingID:   "01BINDBB0000000000000000",
					JoinedAt:    time.Now().UTC(),
					AddedVia:    "test_setup",
				}},
				"test-plugin",
			)

			nonParticipantID := eventbus.SessionIdentity{
				Kind:        eventbus.IdentityKindCharacter,
				PlayerID:    "01PLAYERCC000000000000000",
				CharacterID: "01CHARCC0000000000000000",
				BindingID:   "01BINDCC0000000000000000",
			}
			sessID := "hot-nonpart-" + strconv.Itoa(int(time.Now().UnixNano()))
			stream, err := env.subscriber.OpenSession(ctx, sessID, nonParticipantID, []eventbus.Subject{"events.>"}, time.Time{})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = stream.Close() })

			recvCtx, recvCancel := context.WithTimeout(ctx, testutil.DefaultWait)
			defer recvCancel()
			delivery, err := stream.Next(recvCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(delivery.Ack()).To(Succeed())

			Expect(delivery.MetadataOnly()).To(BeTrue())
			Expect(delivery.Event().Payload).To(BeEmpty())
			Expect(string(delivery.Event().Type)).To(Equal("test-plugin:whisper"))
		})

		It("delivers plaintext to the participant via cold tier", func() {
			sceneID := "01HEECOLDPART00000000000"
			participantID := eventbus.SessionIdentity{
				Kind:        eventbus.IdentityKindCharacter,
				PlayerID:    "01PLAYERDD000000000000000",
				CharacterID: "01CHARDD0000000000000000",
				BindingID:   "01BINDDD0000000000000000",
			}
			plaintext := `{"text":"cold participant secret"}`
			emitSensitivePluginEvent(
				ctx, suiteT, env,
				"scene."+sceneID, plaintext,
				[]dek.Participant{{
					PlayerID:    participantID.PlayerID,
					CharacterID: participantID.CharacterID,
					BindingID:   participantID.BindingID,
					JoinedAt:    time.Now().UTC(),
					AddedVia:    "test_setup",
				}},
				"test-plugin",
			)
			env.hostSub.AwaitDrained(suiteT, 10*time.Second)

			translated := "events.main.scene." + sceneID
			reader := buildColdReader(env)
			stream, err := reader.QueryHistory(ctx, eventbus.HistoryQuery{
				Subject:   eventbus.Subject(translated),
				Direction: eventbus.DirectionForward,
				PageSize:  10,
				Identity:  participantID,
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = stream.Close() })

			recvCtx, recvCancel := context.WithTimeout(ctx, testutil.DefaultWait)
			defer recvCancel()
			ev, err := stream.Next(recvCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(ev.MetadataOnly).To(BeFalse())
			Expect(string(ev.Payload)).To(Equal(plaintext))
		})

		It("delivers metadata-only to a non-participant via cold tier", func() {
			sceneID := "01HEECOLDNON000000000000"
			emitSensitivePluginEvent(
				ctx, suiteT, env,
				"scene."+sceneID, `{"text":"cold secret"}`,
				[]dek.Participant{{
					PlayerID:    "01PLAYEREE000000000000000",
					CharacterID: "01CHAREE0000000000000000",
					BindingID:   "01BINDEE0000000000000000",
					JoinedAt:    time.Now().UTC(),
					AddedVia:    "test_setup",
				}},
				"test-plugin",
			)
			env.hostSub.AwaitDrained(suiteT, 10*time.Second)

			nonParticipantID := eventbus.SessionIdentity{
				Kind:        eventbus.IdentityKindCharacter,
				PlayerID:    "01PLAYERFF000000000000000",
				CharacterID: "01CHARFF0000000000000000",
				BindingID:   "01BINDFF0000000000000000",
			}
			translated := "events.main.scene." + sceneID
			reader := buildColdReader(env)
			stream, err := reader.QueryHistory(ctx, eventbus.HistoryQuery{
				Subject:   eventbus.Subject(translated),
				Direction: eventbus.DirectionForward,
				PageSize:  10,
				Identity:  nonParticipantID,
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = stream.Close() })

			recvCtx, recvCancel := context.WithTimeout(ctx, testutil.DefaultWait)
			defer recvCancel()
			ev, err := stream.Next(recvCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(ev.MetadataOnly).To(BeTrue())
			Expect(ev.Payload).To(BeEmpty())
			Expect(string(ev.Type)).To(Equal("test-plugin:whisper"))
		})
	})

	Describe("plugin-authored sensitive event (regression lock)", func() {
		// AAD includes Actor.ID — if the cold path's AAD reconstruction
		// loses the actor ULID, AEAD authentication fails with a decode
		// error. Post-w9ml retarget: same regression lock, now ULID-based.
		It("round-trips through cold tier with Actor.ID preserved", func() {
			sceneID := "01HEEPLUGIN0000000000000"
			plaintext := `{"text":"plugin-authored secret"}`
			participantID := eventbus.SessionIdentity{
				Kind:        eventbus.IdentityKindCharacter,
				PlayerID:    "01PLAYERGG000000000000000",
				CharacterID: "01CHARGG0000000000000000",
				BindingID:   "01BINDGG0000000000000000",
			}
			translated := "events.main.scene." + sceneID
			pluginActorID := ulid.MustNew(ulid.Timestamp(time.Now()), nil)
			publishSensitiveWithPluginActor(
				ctx, suiteT, env,
				translated,
				"test-plugin:whisper",
				plaintext,
				pluginActorID,
				[]dek.Participant{{
					PlayerID:    participantID.PlayerID,
					CharacterID: participantID.CharacterID,
					BindingID:   participantID.BindingID,
					JoinedAt:    time.Now().UTC(),
					AddedVia:    "test_setup",
				}},
			)
			env.hostSub.AwaitDrained(suiteT, 10*time.Second)

			reader := buildColdReader(env)
			stream, err := reader.QueryHistory(ctx, eventbus.HistoryQuery{
				Subject:   eventbus.Subject(translated),
				Direction: eventbus.DirectionForward,
				PageSize:  10,
				Identity:  participantID,
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = stream.Close() })

			recvCtx, recvCancel := context.WithTimeout(ctx, testutil.DefaultWait)
			defer recvCancel()
			ev, err := stream.Next(recvCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(ev.Actor.ID).To(Equal(pluginActorID),
				"Actor.ID must round-trip via envelope unmarshal")
			Expect(ev.MetadataOnly).To(BeFalse())
			Expect(string(ev.Payload)).To(Equal(plaintext))
		})
	})
})
