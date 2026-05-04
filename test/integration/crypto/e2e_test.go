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
// Cold-tier wiring note: history.NewReader does not yet expose AuthGuard /
// DEKManager options on its Reader-level constructor (the cold-tier-internal
// options live in cold_postgres.go::WithColdHistoryAuthGuard but are not
// plumbed through NewReader). Until that wiring lands, the cold-read tests
// here use a local ColdTier shim (e2eColdTier) that delegates to the same
// dispatcher contract via the public codec / AAD / authguard packages. The
// shim's correctness w.r.t. the production dispatcher is locked by the
// unit-level tests in internal/eventbus/history/cold_postgres_test.go
// (TestColdPostgresUnmarshalsEnvelope) and dispatcher_test.go.
package crypto_test

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/authguard"
	guardaudit "github.com/holomush/holomush/internal/eventbus/authguard/audit"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/eventbus/history"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	"github.com/holomush/holomush/test/testutil"
)

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
	dekMgr, err := dek.NewManager(provider, dekStore, dekCache, dekPartCache)
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
	env.cleanup = append(env.cleanup,
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
		ctxID, ok := contextIDFromLegacySubject(subject)
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
	actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
		return core.Actor{Kind: core.ActorPlugin, ID: pluginName}, nil
	}
	emitter := plugins.NewPluginEventEmitter(env.publisher, manifestLookup, actorResolver,
		plugins.WithCryptoEnabled(true),
	)
	intent := pluginsdk.EmitIntent{
		Subject:   subject,
		Type:      pluginsdk.EventType("test-plugin:whisper"),
		Payload:   plaintext,
		Sensitive: true,
	}
	require.NoError(t, emitter.Emit(ctx, pluginName, intent))
}

// publishSensitiveWithLegacyActor publishes a sensitive event directly
// through env.publisher with Actor.kind=PLUGIN and Actor.LegacyID set to
// the given plugin name. Used by the Decision-5 regression-lock test —
// the plugin emit path does not propagate Actor.LegacyID through the
// core.Actor → eventbus.Actor bridge today, so the regression target
// (LegacyID-bearing AAD round-trip) requires direct publish.
func publishSensitiveWithLegacyActor(
	ctx context.Context, t *testing.T,
	env *e2eEnv,
	subject, eventType, plaintext, pluginLegacyID string,
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
			Kind:     eventbus.ActorKindPlugin,
			LegacyID: pluginLegacyID,
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

// contextIDFromLegacySubject parses "scene:<id>" forms used by the plugin
// emit path. Mirrors publisher.contextIDFromSubject's behavior for the
// pre-translation form (the publisher receives the post-translation
// "events.main.scene.<id>" form).
func contextIDFromLegacySubject(subject string) (dek.ContextID, bool) {
	if len(subject) > 6 && subject[:6] == "scene:" {
		return dek.ContextID{Type: "scene", ID: subject[6:]}, true
	}
	return dek.ContextID{}, false
}

// e2eColdTier reads from events_audit and runs each row through the
// production codec / AAD / authguard chain — equivalent to what the cold
// reader's dispatcher does, but accessible from a test that wires its
// own dependencies (history.NewReader's cold-crypto options aren't yet
// exposed as Reader-level options).
type e2eColdTier struct {
	pool    *pgxpool.Pool
	guard   eventbus.SessionAuthGuard
	dekMgr  eventbus.SessionDEKManager
	auditEm eventbus.SessionAuditEmitter
}

func (c *e2eColdTier) Read(ctx context.Context, q eventbus.HistoryQuery, _ time.Time, pageSize int, _ *history.StreamStateSnapshot) ([]eventbus.Event, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT id, envelope, js_seq, codec, dek_ref, dek_version
		FROM events_audit
		WHERE subject = $1
		ORDER BY js_seq ASC
		LIMIT $2`, string(q.Subject), pageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []eventbus.Event
	for rows.Next() {
		var (
			idBytes       []byte
			envelopeBytes []byte
			seq           int64
			codecStr      string
			dekRef        sql.NullInt64
			dekVersion    sql.NullInt32
		)
		if scanErr := rows.Scan(&idBytes, &envelopeBytes, &seq, &codecStr, &dekRef, &dekVersion); scanErr != nil {
			return nil, scanErr
		}
		ev, metaOnly, dispatchErr := dispatchColdRow(ctx, envelopeBytes, codecStr, dekRef, dekVersion, q.Identity, c.guard, c.dekMgr)
		if dispatchErr != nil {
			return nil, dispatchErr
		}
		var id ulid.ULID
		copy(id[:], idBytes)
		ev.ID = id
		ev.Seq = uint64(seq) //nolint:gosec
		ev.MetadataOnly = metaOnly
		out = append(out, ev)
	}
	return out, rows.Err()
}

// dispatchColdRow mirrors history.decodeAuthorizeAndDispatch for the cold
// path. The duplication is exact w.r.t. the production codec + AAD + decode
// chain because we import the same aad.Build and codec.Resolve.
func dispatchColdRow(
	ctx context.Context,
	envelopeBytes []byte,
	codecStr string,
	dekRefCol sql.NullInt64,
	dekVerCol sql.NullInt32,
	identity eventbus.SessionIdentity,
	guard eventbus.SessionAuthGuard,
	dekMgr eventbus.SessionDEKManager,
) (eventbus.Event, bool, error) {
	var pbEnvelope eventbusv1.Event
	if err := proto.Unmarshal(envelopeBytes, &pbEnvelope); err != nil {
		return eventbus.Event{}, false, err
	}
	codecName := codec.Name(codecStr)
	if codecName == codec.NameIdentity {
		return eventFromEnvelope(&pbEnvelope, pbEnvelope.GetPayload()), false, nil
	}
	var keyID codec.KeyID
	var keyVer uint32
	if dekRefCol.Valid {
		keyID = codec.KeyID(dekRefCol.Int64) //nolint:gosec
	}
	if dekVerCol.Valid {
		keyVer = uint32(dekVerCol.Int32) //nolint:gosec
	}
	var eventID ulid.ULID
	if raw := pbEnvelope.GetId(); len(raw) == 16 {
		copy(eventID[:], raw)
	}
	req := eventbus.SessionCheckRequest{
		Identity:   identity,
		KeyID:      keyID,
		KeyVersion: keyVer,
		EventType:  pbEnvelope.GetType(),
		EventID:    eventID,
	}
	decision, err := guard.Check(ctx, req)
	if err != nil {
		return eventbus.Event{}, false, err
	}
	if !decision.Permit {
		return eventFromEnvelope(&pbEnvelope, nil), true, nil
	}
	key, err := dekMgr.Resolve(ctx, keyID, keyVer)
	if err != nil {
		return eventbus.Event{}, false, err
	}
	aadBytes, err := aad.Build(&pbEnvelope, codecStr, uint64(keyID), keyVer)
	if err != nil {
		return eventbus.Event{}, false, err
	}
	cdc, err := codec.Resolve(codecName)
	if err != nil {
		return eventbus.Event{}, false, err
	}
	plaintext, err := cdc.Decode(ctx, pbEnvelope.GetPayload(), key, aadBytes)
	if err != nil {
		return eventbus.Event{}, false, err
	}
	return eventFromEnvelope(&pbEnvelope, plaintext), false, nil
}

// eventFromEnvelope mirrors history.buildHistoryEventFromEnvelope.
func eventFromEnvelope(env *eventbusv1.Event, payload []byte) eventbus.Event {
	var id ulid.ULID
	if raw := env.GetId(); len(raw) == 16 {
		copy(id[:], raw)
	}
	var actorID ulid.ULID
	if raw := env.GetActor().GetId(); len(raw) == 16 {
		copy(actorID[:], raw)
	}
	return eventbus.Event{
		ID:        id,
		Subject:   eventbus.Subject(env.GetSubject()),
		Type:      eventbus.Type(env.GetType()),
		Timestamp: env.GetTimestamp().AsTime(),
		Actor: eventbus.Actor{
			Kind:     protoActorKindToEventbus(env.GetActor().GetKind()),
			ID:       actorID,
			LegacyID: env.GetActor().GetLegacyId(),
		},
		Payload: payload,
	}
}

func protoActorKindToEventbus(k eventbusv1.ActorKind) eventbus.ActorKind {
	switch k {
	case eventbusv1.ActorKind_ACTOR_KIND_CHARACTER:
		return eventbus.ActorKindCharacter
	case eventbusv1.ActorKind_ACTOR_KIND_PLAYER:
		return eventbus.ActorKindPlayer
	case eventbusv1.ActorKind_ACTOR_KIND_SYSTEM:
		return eventbus.ActorKindSystem
	case eventbusv1.ActorKind_ACTOR_KIND_PLUGIN:
		return eventbus.ActorKindPlugin
	default:
		return eventbus.ActorKindUnknown
	}
}

// buildColdReader wires a Reader with a custom cold tier whose dispatcher
// chain has the AuthGuard + DEKManager + AuditEmitter populated. The hot
// tier is left as the default (it sees the same JetStream the publisher
// just wrote to), but our queries set the clock to "far future" so every
// event is read from cold.
func buildColdReader(env *e2eEnv) *history.Reader {
	coldTier := &e2eColdTier{
		pool:    env.pool,
		guard:   env.guard,
		dekMgr:  env.dekMgr,
		auditEm: env.auditEm,
	}
	farFuture := time.Now().UTC().Add(100 * 365 * 24 * time.Hour)
	return history.NewReader(env.bus.JS, env.pool,
		eventbus.Config{}.Defaults().StreamMaxAge,
		func() time.Time { return farFuture },
		history.WithColdTier(coldTier),
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
			emitSensitivePluginEvent(ctx, suiteT, env,
				"scene:"+sceneID,
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

			stream, err := env.subscriber.OpenSession(ctx, "hot-part-"+sceneID, participantID, []eventbus.Subject{"events.>"})
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
			emitSensitivePluginEvent(ctx, suiteT, env,
				"scene:"+sceneID,
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
			stream, err := env.subscriber.OpenSession(ctx, sessID, nonParticipantID, []eventbus.Subject{"events.>"})
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
			emitSensitivePluginEvent(ctx, suiteT, env,
				"scene:"+sceneID, plaintext,
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
			emitSensitivePluginEvent(ctx, suiteT, env,
				"scene:"+sceneID, `{"text":"cold secret"}`,
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

	Describe("plugin-authored sensitive event (Decision 5 regression lock)", func() {
		// AAD includes Actor.LegacyID — if the cold path's AAD reconstruction
		// loses LegacyID, AEAD authentication fails with a decode error. This
		// is the regression lock for Decision 5.
		It("round-trips through cold tier with Actor.legacy_id preserved", func() {
			sceneID := "01HEEPLUGIN0000000000000"
			plaintext := `{"text":"plugin-authored secret"}`
			participantID := eventbus.SessionIdentity{
				Kind:        eventbus.IdentityKindCharacter,
				PlayerID:    "01PLAYERGG000000000000000",
				CharacterID: "01CHARGG0000000000000000",
				BindingID:   "01BINDGG0000000000000000",
			}
			translated := "events.main.scene." + sceneID
			publishSensitiveWithLegacyActor(ctx, suiteT, env,
				translated,
				"test-plugin:whisper",
				plaintext,
				"core-scenes",
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
			Expect(ev.Actor.LegacyID).To(Equal("core-scenes"),
				"Decision 5: Actor.legacy_id must round-trip via envelope unmarshal")
			Expect(ev.MetadataOnly).To(BeFalse())
			Expect(string(ev.Payload)).To(Equal(plaintext))
		})
	})
})
