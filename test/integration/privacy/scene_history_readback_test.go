// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package privacy_test — Task 9 Step 3: INV-CRYPTO-32 participant fence decrypt E2E.
//
// This file exercises the PluginDowngradeFence's clean-row decrypt path for
// routed participants (INV-CRYPTO-32). The test uses a REAL stack (DEK manager,
// codec, fence, AuthGuard) rather than fakeHistoryReader, closing the
// fake-bus coverage gap described in holomush-m7pxs Task 9.
//
// Architecture:
//  1. Build a self-contained fixture: PG + embedded NATS + DEK manager + audit
//     subsystem + AuthGuard.
//  2. Emit a sensitive plugin:scene_pose event with a known participant list
//     so events_audit is populated with a real encrypted envelope.
//  3. Build a pgPluginHistoryRouter that queries events_audit directly (no
//     binary plugin needed) and stamps AuditRows onto Events via StampAuditRow.
//  4. Wire history.NewReader with WithOwners + WithPluginRouter +
//     WithPluginDowngradeFenceReadback — exactly the production shape.
//  5. Query with a CHARACTER identity that is a DEK participant → fence's
//     decryptClean succeeds → ev.MetadataOnly=false, ev.Payload=plaintext.
//  6. Query with a different CHARACTER identity (non-member) → fence's
//     checkCharacter denies → ev.MetadataOnly=true, ev.Payload=nil.
//
// INV-RB invariants covered: INV-CRYPTO-32.
//
// The audit emitter passed to WithFenceReadbackCrypto is nil because the
// audit emitter is only consulted for IdentityKindPlugin principals
// (see dispatcher.go:349). CHARACTER callers skip the INV-CRYPTO-11 audit record,
// so nil is safe here — verified by TestDecodeJetStreamMessagePluginIdentityWithNilAuditEmitterFailsClosed
// which covers the fail-closed behaviour for plugin principals.
package privacy_test

import (
	"context"
	"database/sql"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/authguard"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/eventbus/history"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/plugintest"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"github.com/holomush/holomush/test/testutil"
	"github.com/samber/oops"
)

// ---------------------------------------------------------------------------
// Local stubs — package-local equivalents of types in crypto_test
// ---------------------------------------------------------------------------

// rbPrivacyFixedJS satisfies audit.JSProvider.
type rbPrivacyFixedJS struct{ js jetstream.JetStream }

func (f rbPrivacyFixedJS) JS() jetstream.JetStream { return f.js }

// rbPrivacyFixedPool satisfies audit.PoolProvider.
type rbPrivacyFixedPool struct{ pool *pgxpool.Pool }

func (f rbPrivacyFixedPool) Pool() *pgxpool.Pool { return f.pool }

// rbPrivacyNoopBackpressure is a BackpressureChecker that never throttles.
type rbPrivacyNoopBackpressure struct{}

func (rbPrivacyNoopBackpressure) ShouldThrottle(_ string) bool { return false }

// rbPrivacyDEKBindingStub implements dek.BindingResolver with a fixed binding ID.
type rbPrivacyDEKBindingStub struct{ bindingID string }

func (s *rbPrivacyDEKBindingStub) CurrentWithPlayer(_ context.Context, _ string) (bindingID, playerID string, err error) {
	return s.bindingID, "", nil
}

// rbPrivacyAlwaysTrueCryptoKeysLookup always reports that a DEK exists.
// Safe for tests where the DEK was just minted.
type rbPrivacyAlwaysTrueCryptoKeysLookup struct{}

func (l *rbPrivacyAlwaysTrueCryptoKeysLookup) Exists(_ context.Context, _ uint64) (bool, error) {
	return true, nil
}

// rbPrivacyManifestLookup returns readback:true only for the named
// (pluginName, eventType) pair. Required by authguard.New but not
// exercised on the CHARACTER path (checkCharacter does not consult it).
type rbPrivacyManifestLookup struct {
	pluginName string
	eventType  string
}

func (m *rbPrivacyManifestLookup) PluginRequestsDecryption(_, _ string) bool { return false }
func (m *rbPrivacyManifestLookup) PluginCanReadBack(name, evType string) bool {
	return name == m.pluginName && evType == m.eventType
}

// ---------------------------------------------------------------------------
// pgPluginHistoryRouter — queries events_audit directly, stamps AuditRows
// ---------------------------------------------------------------------------

// pgPluginHistoryRouter serves the history.PluginHistoryRouter contract by
// reading from events_audit. In production, plugin history goes through the
// plugin's gRPC PluginAuditService. For integration tests without a running
// binary plugin, this satisfies the interface by reading the host's events_audit
// table and stamping plugin-source-of-truth AuditRows onto Events via
// eventbus.StampAuditRow, which is the same stamping the production
// audit.PluginHistoryRouter performs on real gRPC responses.
//
// The fence wraps this router via WithPluginDowngradeFenceReadback, so the
// fence's clean-row path (INV-CRYPTO-32) runs on top of rows we serve here.
type pgPluginHistoryRouter struct {
	pool *pgxpool.Pool
}

// pgHistoryStream is a simple slice-backed HistoryStream for one-shot queries.
type pgHistoryStream struct {
	events []eventbus.Event
	pos    int
}

func (s *pgHistoryStream) Next(_ context.Context) (eventbus.Event, error) {
	if s.pos >= len(s.events) {
		return eventbus.Event{}, io.EOF
	}
	ev := s.events[s.pos]
	s.pos++
	return ev, nil
}

func (s *pgHistoryStream) Close() error { return nil }

// QueryHistory implements history.PluginHistoryRouter. It fetches rows from
// events_audit matching q.Subject, converts them to pluginv1.AuditRow, stamps
// them onto Events using eventbus.StampAuditRow, and returns the stream.
func (r *pgPluginHistoryRouter) QueryHistory(
	ctx context.Context,
	_ string, // pluginName — unused; we query by subject
	q eventbus.HistoryQuery,
) (eventbus.HistoryStream, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT id, subject, type, codec, envelope, schema_ver, dek_ref, dek_version
		   FROM events_audit
		  WHERE subject = $1
		  ORDER BY id ASC`,
		string(q.Subject),
	)
	if err != nil {
		return nil, oops.Code("PG_PLUGIN_HISTORY_QUERY_FAILED").Wrap(err)
	}
	defer rows.Close()

	var events []eventbus.Event
	for rows.Next() {
		var (
			idBytes    []byte
			subject    string
			evType     string
			codecStr   string
			envelopeB  []byte
			schemaVer  int32
			dekRef     sql.NullInt64
			dekVersion sql.NullInt32
		)
		if scanErr := rows.Scan(
			&idBytes, &subject, &evType, &codecStr, &envelopeB, &schemaVer, &dekRef, &dekVersion,
		); scanErr != nil {
			return nil, oops.Code("PG_PLUGIN_HISTORY_SCAN_FAILED").Wrap(scanErr)
		}

		// events_audit.envelope stores the full marshaled eventbusv1.Event proto.
		// We unmarshal it to recover:
		//   1. AuditRow.Payload — the inner ciphertext bytes (Event.Payload), which
		//      decryptPluginRow passes to codec.Decode. Passing the full proto bytes
		//      causes AEAD authentication failure.
		//   2. AuditRow.Actor — the Actor proto from the publish-time envelope. This
		//      is required for AAD reconstruction: AuditRowToEvent(row) includes Actor
		//      in the AAD, and a nil Actor produces different AAD bytes than publish time.
		//   3. AuditRow.Timestamp — must match publish-time proto exactly. Reconstructing
		//      from the BIGINT nanosecond column introduces precision differences that break
		//      AEAD authentication.
		var pbEnv eventbusv1.Event
		if unmarshalErr := proto.Unmarshal(envelopeB, &pbEnv); unmarshalErr != nil {
			return nil, oops.Code("PG_PLUGIN_HISTORY_UNMARSHAL_FAILED").Wrap(unmarshalErr)
		}
		ciphertext := pbEnv.GetPayload()

		row := &pluginv1.AuditRow{
			Id:        idBytes,
			Subject:   subject,
			Type:      evType,
			Codec:     codecStr,
			Payload:   ciphertext,           // actual ciphertext bytes, not the proto envelope
			Actor:     pbEnv.GetActor(),     // AAD-canonical Actor from publish-time envelope
			Timestamp: pbEnv.GetTimestamp(), // AAD-canonical Timestamp from publish-time envelope
			SchemaVer: schemaVer,
		}
		if dekRef.Valid {
			v := uint64(dekRef.Int64) //nolint:gosec // dek_ref is BIGSERIAL; safe widening
			row.DekRef = &v
		}
		if dekVersion.Valid {
			v := uint32(dekVersion.Int32) //nolint:gosec // dek_version fits in uint32
			row.DekVersion = &v
		}

		ts := pbEnv.GetTimestamp().AsTime().UTC()
		ev := eventbus.Event{
			Subject:   eventbus.Subject(subject),
			Type:      eventbus.Type(evType),
			Timestamp: ts,
			Payload:   ciphertext, // mirror AuditRow.Payload for the fence's hot-path check
		}
		// Stamp the plugin source-of-truth row so the fence can inspect
		// codec, dek_ref, dek_version (INV-CRYPTO-30 / INV-CRYPTO-42 / INV-CRYPTO-50).
		// Unique-pointer contract: a fresh row is allocated per iteration.
		eventbus.StampAuditRow(&ev, row)

		events = append(events, ev)
	}
	if rows.Err() != nil {
		return nil, oops.Code("PG_PLUGIN_HISTORY_ROWS_FAILED").Wrap(rows.Err())
	}

	return &pgHistoryStream{events: events}, nil
}

// ---------------------------------------------------------------------------
// fencedReaderEnv — full real-stack fixture
// ---------------------------------------------------------------------------

// fencedReaderEnv groups the real-stack components needed for INV-CRYPTO-32 tests.
type fencedReaderEnv struct {
	pool         *pgxpool.Pool
	bus          *testutil.EmbeddedBus
	dekMgr       dek.Manager
	hostAuditSub *audit.Subsystem
	pluginPub    eventbus.Publisher
	reader       *history.Reader
	gameID       string
	pluginName   string
	cleanup      []func()
}

func (e *fencedReaderEnv) teardown() {
	for i := len(e.cleanup) - 1; i >= 0; i-- {
		e.cleanup[i]()
	}
}

// buildFencedReaderEnv constructs the full INV-CRYPTO-32 fixture. The resulting
// reader uses WithPluginDowngradeFenceReadback so that:
//   - CHARACTER callers that are DEK participants get plaintext (INV-CRYPTO-32 positive)
//   - CHARACTER callers not in the DEK participant list get MetadataOnly=true (INV-CRYPTO-32 negative)
func buildFencedReaderEnv(ctx context.Context, pluginName string) *fencedReaderEnv {
	GinkgoHelper()

	shared := testutil.SharedPostgres(suiteT)
	connStr := testutil.FreshDatabase(suiteT, shared)

	poolCtx, poolCancel := context.WithTimeout(ctx, 10*time.Second)
	defer poolCancel()
	pool, err := pgxpool.New(poolCtx, connStr)
	Expect(err).NotTo(HaveOccurred(), "buildFencedReaderEnv: pgxpool.New")

	bus := testutil.StartEmbeddedJetStream(suiteT)

	kekHex := testutil.RandomKEKHex(suiteT)
	suiteT.Setenv("HOLOMUSH_RB7_PRIV_TEST_KEK", kekHex)
	kekSrc := kek.NewEnvSource("HOLOMUSH_RB7_PRIV_TEST_KEK", false)
	provider, err := kek.NewLocalAEADProvider(ctx, kekSrc, pool)
	Expect(err).NotTo(HaveOccurred())

	dekStore := dek.NewStore(pool)
	dekCache := dek.NewCache(dek.CacheConfig{Capacity: 64})
	dekPartCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64})
	dekMgr, err := dek.NewManager(provider, dekStore, dekCache, dekPartCache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&rbPrivacyDEKBindingStub{bindingID: "bind-rb7-priv"})
	Expect(err).NotTo(HaveOccurred())

	// Host audit subsystem — populates events_audit for emitted events.
	hostSub := audit.NewSubsystem(rbPrivacyFixedJS{js: bus.JS}, rbPrivacyFixedPool{pool: pool}, audit.Config{})
	Expect(hostSub.Start(ctx)).To(Succeed())

	// VerbRegistry — needed by RenderingPublisher.
	registry, err := core.BootstrapVerbRegistry("test")
	Expect(err).NotTo(HaveOccurred())
	Expect(registry.RegisterWithSource(core.VerbRegistration{
		Type:          pluginName + ":scene_pose",
		Category:      "communication",
		Format:        "speech",
		Label:         "poses",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        pluginName,
	}, "1.0.0")).To(Succeed())

	rawPub := eventbus.NewJetStreamPublisher(
		bus.JS,
		eventbus.Config{}.Defaults(),
		eventbus.WithDEKManager(dekMgr),
	)
	hostPub := eventbus.NewRenderingPublisher(rawPub, registry)

	// AuthGuard for the fence's decryptClean path. Uses allow-all ABAC.
	// The BackpressureChecker must be non-nil (enforced by authguard.New).
	manifestLookup := &rbPrivacyManifestLookup{
		pluginName: pluginName,
		eventType:  pluginName + ":scene_pose",
	}
	guardCore, err := authguard.New(
		authguard.NewDEKParticipantLookup(dekMgr),
		manifestLookup,
		policytest.AllowAllEngine(),
		rbPrivacyNoopBackpressure{},
	)
	Expect(err).NotTo(HaveOccurred())
	sessionGuard := authguard.NewSessionBridgeGuard(guardCore)

	// OwnerMap: pluginName owns events.*.scene.>
	ownerMap, err := audit.NewOwnerMap([]audit.SubjectOwner{
		{PluginName: pluginName, Pattern: "events.*.scene.>"},
	})
	Expect(err).NotTo(HaveOccurred())

	alwaysSensitive := map[string]struct{}{
		pluginName + ":scene_pose": {},
	}

	// pgPluginHistoryRouter: satisfies PluginHistoryRouter from events_audit.
	pgRouter := &pgPluginHistoryRouter{pool: pool}

	// history.Reader wired with the full INV-CRYPTO-32 fence.
	// Clock set to far future so all queries route cold (events_audit).
	farFuture := time.Now().UTC().Add(100 * 365 * 24 * time.Hour)
	reader := history.NewReader(
		bus.JS, pool,
		eventbus.Config{}.Defaults().StreamMaxAge,
		func() time.Time { return farFuture },
		history.WithOwners(ownerMap),
		history.WithPluginRouter(pgRouter),
		history.WithPluginDowngradeFenceReadback(
			alwaysSensitive,
			&rbPrivacyAlwaysTrueCryptoKeysLookup{},
			nil, // violationEmitter: nil is safe — downgrade refusals still surface as metadata_only
			sessionGuard,
			dekMgr,
			nil, // auditEm: nil is safe — CHARACTER callers skip the INV-CRYPTO-11 audit record (dispatcher.go:349)
		),
	)

	env := &fencedReaderEnv{
		pool:         pool,
		bus:          bus,
		dekMgr:       dekMgr,
		hostAuditSub: hostSub,
		pluginPub:    hostPub,
		reader:       reader,
		gameID:       "main",
		pluginName:   pluginName,
		cleanup: []func(){
			func() { _ = hostSub.Stop(context.Background()) }, //nolint:errcheck // test cleanup
			func() { pool.Close() },
		},
	}
	return env
}

// emitSensitiveScenePoseForPrivacy emits a sensitive pluginName:scene_pose event
// for the given scene and waits for the audit subsystem to drain before returning.
func emitSensitiveScenePoseForPrivacy(
	ctx context.Context,
	env *fencedReaderEnv,
	sceneID, plaintext string,
	participants []dek.Participant,
) {
	GinkgoHelper()

	ctxID := dek.ContextID{Type: "scene", ID: sceneID}
	_, err := env.dekMgr.GetOrCreate(ctx, ctxID, participants)
	Expect(err).NotTo(HaveOccurred(), "emitSensitiveScenePoseForPrivacy: GetOrCreate DEK")

	manifest := &plugins.Manifest{
		Name:                env.pluginName,
		Emits:               []string{"scene"},
		ActorKindsClaimable: []string{"plugin"},
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{
					EventType:   env.pluginName + ":scene_pose",
					Sensitivity: plugins.SensitivityAlways,
					Readback:    true,
				},
			},
		},
	}
	manifestLookupFn := func(name string) *plugins.Manifest {
		if name == env.pluginName {
			return manifest
		}
		return nil
	}
	pluginActorID := plugintest.PluginULIDFromName(env.pluginName).String()
	actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
		return core.Actor{Kind: core.ActorPlugin, ID: pluginActorID}, nil
	}
	emitter := plugins.NewPluginEventEmitter(
		env.pluginPub, manifestLookupFn, actorResolver,
	)
	intent := pluginsdk.EmitIntent{
		Subject:   "scene." + sceneID,
		Type:      pluginsdk.EventType(env.pluginName + ":scene_pose"),
		Payload:   plaintext,
		Sensitive: true,
	}
	Expect(emitter.Emit(ctx, env.pluginName, intent)).To(Succeed())

	// Wait for the audit subsystem to drain so events_audit is populated.
	// GinkgoT() satisfies audit.AwaitT (has Helper() and Fatalf()).
	env.hostAuditSub.AwaitDrained(GinkgoT(), 10*time.Second)
}

// drainHistory consumes a HistoryStream to completion and returns all events.
func drainHistory(ctx context.Context, stream eventbus.HistoryStream) []eventbus.Event {
	GinkgoHelper()
	defer func() { _ = stream.Close() }() //nolint:errcheck // test cleanup
	var events []eventbus.Event
	for {
		ev, err := stream.Next(ctx)
		if err == io.EOF {
			break
		}
		Expect(err).NotTo(HaveOccurred(), "drainHistory: unexpected stream error")
		events = append(events, ev)
	}
	return events
}

// ---------------------------------------------------------------------------
// INV-CRYPTO-32 Ginkgo spec
// ---------------------------------------------------------------------------

var _ = Describe("INV-CRYPTO-32: participant fence decrypt via PluginDowngradeFence", func() {
	const (
		pluginNameRB7 = "core-scenes-rb7"
		plaintextRB7  = `{"text":"A participant's sensitive pose."}`
	)

	var (
		ctx    context.Context
		cancel context.CancelFunc
		env    *fencedReaderEnv
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
		env = buildFencedReaderEnv(ctx, pluginNameRB7)
	})

	AfterEach(func() {
		env.teardown()
		cancel()
	})

	// INV-CRYPTO-32 positive arm: a scene participant reads through the fence and
	// receives the decrypted plaintext. The fence's decryptClean routes to
	// the CHARACTER DEK-membership branch (ReadBack=false) and succeeds
	// because the participant's BindingID is in the DEK's participant list.
	Describe("participant read — fence decrypt positive arm", func() {
		It("delivers plaintext to a scene participant via fence decrypt (INV-CRYPTO-32)", func() {
			const sceneID = "01RB7PART0000000000000000"

			participantIdentity := eventbus.SessionIdentity{
				Kind:        eventbus.IdentityKindCharacter,
				PlayerID:    "01RB7PART_PLAYER000000000",
				CharacterID: "01RB7PART_CHAR0000000000",
				BindingID:   "01RB7PART_BIND0000000000",
			}

			// Emit a sensitive scene pose with participantIdentity as the sole
			// participant so the DEK's participant list contains their BindingID.
			emitSensitiveScenePoseForPrivacy(ctx, env, sceneID, plaintextRB7, []dek.Participant{
				{
					PlayerID:    participantIdentity.PlayerID,
					CharacterID: participantIdentity.CharacterID,
					BindingID:   participantIdentity.BindingID,
					JoinedAt:    time.Now().UTC(),
					AddedVia:    "readback_test",
				},
			})

			// Query via the fenced reader as the participant.
			subject := eventbus.Subject("events." + env.gameID + ".scene." + sceneID)
			stream, err := env.reader.QueryHistory(ctx, eventbus.HistoryQuery{
				Subject:  subject,
				Identity: participantIdentity,
				PageSize: 10,
			})
			Expect(err).NotTo(HaveOccurred(),
				"INV-CRYPTO-32: participant QueryHistory must not error")

			events := drainHistory(ctx, stream)
			Expect(events).NotTo(BeEmpty(),
				"INV-CRYPTO-32: at least one event must be returned for the scene subject")

			ev := events[0]
			// INV-CRYPTO-32 positive: MetadataOnly must be false and Payload must equal plaintext.
			Expect(ev.MetadataOnly).To(BeFalse(),
				"INV-CRYPTO-32: scene participant must receive non-metadata-only event via fence decrypt")
			Expect(string(ev.Payload)).To(Equal(plaintextRB7),
				"INV-CRYPTO-32: fence decrypt must restore the original plaintext for the participant")
		})
	})

	// INV-CRYPTO-32 negative arm: a non-participant CHARACTER reads through the fence
	// and receives a metadata-only event. The fence's decryptClean routes to the
	// checkCharacter DEK-membership branch (ReadBack=false) and denies because
	// the non-participant's BindingID is NOT in the DEK's participant list.
	Describe("non-participant read — fence decrypt negative arm", func() {
		It("delivers metadata-only to a non-participant reading a plugin-owned scene (INV-CRYPTO-32)", func() {
			const sceneID = "01RB7NONP0000000000000000"

			participantIdentity := eventbus.SessionIdentity{
				Kind:        eventbus.IdentityKindCharacter,
				PlayerID:    "01RB7NONP_PLAYER000000000",
				CharacterID: "01RB7NONP_CHAR0000000000",
				BindingID:   "01RB7NONP_BIND0000000000",
			}
			nonParticipantIdentity := eventbus.SessionIdentity{
				Kind:        eventbus.IdentityKindCharacter,
				PlayerID:    "01RB7NONP_PLAYER2_0000000",
				CharacterID: "01RB7NONP_CHAR20000000000",
				BindingID:   "01RB7NONP_BIND20000000000",
			}

			// Emit with only participantIdentity as participant.
			// nonParticipantIdentity is deliberately excluded.
			emitSensitiveScenePoseForPrivacy(ctx, env, sceneID, plaintextRB7, []dek.Participant{
				{
					PlayerID:    participantIdentity.PlayerID,
					CharacterID: participantIdentity.CharacterID,
					BindingID:   participantIdentity.BindingID,
					JoinedAt:    time.Now().UTC(),
					AddedVia:    "readback_test",
				},
			})

			// Query as the non-participant.
			subject := eventbus.Subject("events." + env.gameID + ".scene." + sceneID)
			stream, err := env.reader.QueryHistory(ctx, eventbus.HistoryQuery{
				Subject:  subject,
				Identity: nonParticipantIdentity,
				PageSize: 10,
			})
			Expect(err).NotTo(HaveOccurred(),
				"INV-CRYPTO-32: non-participant QueryHistory must not error (refusal is per-row, not stream-fatal)")

			events := drainHistory(ctx, stream)
			Expect(events).NotTo(BeEmpty(),
				"INV-CRYPTO-32: at least one event must be returned even for non-participants (metadata-only row)")

			ev := events[0]
			// INV-CRYPTO-32 negative: MetadataOnly must be true and Payload must be nil.
			Expect(ev.MetadataOnly).To(BeTrue(),
				"INV-CRYPTO-32: non-participant must receive metadata-only event via fence decrypt refusal")
			Expect(ev.Payload).To(BeNil(),
				"INV-CRYPTO-32: non-participant must not receive plaintext — Payload must be nil")
		})
	})
})
