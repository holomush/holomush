// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// C7 snapshot pipeline (COOLOFF→PUBLISHED) integration tests — holomush-5rh.20.26.
//
// These tests exercise the CONSUMER side of the read-back design
// (docs/superpowers/specs/2026-05-25-plugin-readback-decrypt-design.md): the
// snapshot pipeline's atomicity (INV-CRYPTO-33), failure mapping (INV-CRYPTO-35), and the
// FK soft-no-op (ADR holomush-jrefa) — invariants explicitly OUT OF SCOPE for
// the primitive-side tests in test/integration/crypto/readback_test.go.
//
// Two decryptor seams are used:
//   - Happy path / chunking: a REAL history.ReadbackDecryptor (full DEK manager
//   - xchacha20poly1305 codec + OwnerMap + AuthGuard) proves end-to-end
//     ciphertext → plaintext through the production primitive (INV-CRYPTO-31
//     consumer-side, INV-CRYPTO-33). scene_log is seeded with REAL ciphertext.
//   - Failure modes / soft-no-op / idempotency: a fault-injecting fake
//     snapshotDecryptor so the pipeline's failure mapping is asserted precisely
//     without coupling to crypto-fault injection.
//
// Ginkgo dot-import collision note (epic lesson): this is package main with the
// Ginkgo/Gomega dot-imports, so PublishedSceneEntry (NOT a bare Entry) is used.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

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
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"github.com/holomush/holomush/test/testutil"
)

// ---------------------------------------------------------------------------
// Fault-injecting fake decryptor (failure-mode arms)
// ---------------------------------------------------------------------------

// fakeSnapshotDecryptor returns a configurable per-row outcome. Used by the
// failure-mode arms (decrypt error, refusal, render) where real ciphertext is
// unnecessary — the pipeline's failure mapping is the unit under test.
type fakeSnapshotDecryptor struct {
	// err, when non-nil, is returned for every batch (host-level decrypt error).
	err error
	// refuseReason, when non-empty, refuses every row with this reason.
	refuseReason string
	// plaintextFor maps a row's event type to the plaintext to return. When set,
	// each row decrypts to plaintextFor[type]. Used for the render-failure arm
	// (return malformed JSON) and to count chunk calls.
	plaintextFor map[string][]byte
	// calls records the per-call batch sizes so chunking can be asserted.
	calls []int
	// rowTypes records, in order, the event type each returned row corresponds
	// to (looked up from the chunk by index); needed so plaintextFor resolves.
	chunkTypes [][]string
}

func (f *fakeSnapshotDecryptor) DecryptOwnAuditRows(_ context.Context, rows []*pluginv1.AuditRow) ([]*pluginv1.RowResult, error) {
	f.calls = append(f.calls, len(rows))
	if f.err != nil {
		return nil, f.err
	}
	out := make([]*pluginv1.RowResult, len(rows))
	for i, r := range rows {
		if f.refuseReason != "" {
			out[i] = &pluginv1.RowResult{Id: r.GetId(), Outcome: &pluginv1.RowResult_NoPlaintextReason{NoPlaintextReason: f.refuseReason}}
			continue
		}
		pt := f.plaintextFor[r.GetType()]
		out[i] = &pluginv1.RowResult{Id: r.GetId(), Outcome: &pluginv1.RowResult_Plaintext{Plaintext: pt}}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Real-stack snapshot fixture (happy path / chunking)
// ---------------------------------------------------------------------------

// snapFixedJS / snapFixedPool / snapNoopBackpressure / snapDEKBindingStub /
// snapAlwaysTrueCryptoKeysLookup / snapManifestLookup mirror the helper types
// used by test/integration/crypto/readback_test.go; named distinctly to avoid
// collision within package main.
type snapFixedJS struct{ js jetstream.JetStream }

func (f snapFixedJS) JS() jetstream.JetStream { return f.js }

type snapFixedPool struct{ pool *pgxpool.Pool }

func (f snapFixedPool) Pool() *pgxpool.Pool { return f.pool }

type snapNoopBackpressure struct{}

func (snapNoopBackpressure) ShouldThrottle(_ string) bool { return false }

type snapDEKBindingStub struct{ bindingID string }

func (s *snapDEKBindingStub) Current(_ context.Context, _ string) (string, error) {
	return s.bindingID, nil
}

type snapAlwaysTrueCryptoKeysLookup struct{}

func (snapAlwaysTrueCryptoKeysLookup) Exists(_ context.Context, _ uint64) (bool, error) {
	return true, nil
}

type snapManifestLookup struct {
	pluginName string
	eventTypes map[string]struct{}
}

func (m *snapManifestLookup) PluginRequestsDecryption(_, _ string) bool { return false }
func (m *snapManifestLookup) PluginCanReadBack(name, eventType string) bool {
	if name != m.pluginName {
		return false
	}
	_, ok := m.eventTypes[eventType]
	return ok
}

// realDecryptorAdapter adapts a *history.ReadbackDecryptor to the snapshot's
// snapshotDecryptor seam — exactly the shape the production goplugin
// host_service.DecryptOwnAuditRows RPC handler wraps (the plugin name is fixed
// to the owning plugin; instance ID is informational).
type realDecryptorAdapter struct {
	dec        *history.ReadbackDecryptor
	pluginName string
}

func (a *realDecryptorAdapter) DecryptOwnAuditRows(ctx context.Context, rows []*pluginv1.AuditRow) ([]*pluginv1.RowResult, error) {
	res, err := a.dec.DecryptOwnRows(ctx, a.pluginName, "", rows)
	if err != nil {
		return nil, err //nolint:wrapcheck // test adapter mirrors the RPC handler's pass-through
	}
	return res, nil
}

// snapshotRealEnv bundles the real-stack crypto components plus a plugin store
// whose scene_log lives in a coexisting plugin_core_scenes schema in the same
// FreshDatabase (which carries the host crypto_keys / events_audit tables).
type snapshotRealEnv struct {
	store      *SceneStore
	svc        *SceneServiceImpl
	cryptoPool *pgxpool.Pool // public schema: crypto_keys, events_audit
	dekMgr     dek.Manager
	pluginPub  eventbus.Publisher
	hostSub    *audit.Subsystem
	gameID     string
	pluginName string
	cleanup    []func()
}

func (e *snapshotRealEnv) teardown() {
	for i := len(e.cleanup) - 1; i >= 0; i-- {
		e.cleanup[i]()
	}
}

// buildSnapshotRealEnv stands up a FreshDatabase (host crypto schema in public),
// a plugin SceneStore in a plugin_core_scenes schema in the SAME database, and a
// real history.ReadbackDecryptor wired into the service as the snapshot
// decryptor seam.
func buildSnapshotRealEnv(ctx context.Context, pluginName string) *snapshotRealEnv {
	GinkgoHelper()

	shared := testutil.SharedPostgres(suiteT)
	connStr := testutil.FreshDatabase(suiteT, shared) // host migrations → crypto_keys, events_audit

	// crypto pool (public schema) for DEK manager + KEK provider.
	cryptoPool, err := pgxpool.New(ctx, connStr)
	Expect(err).NotTo(HaveOccurred(), "buildSnapshotRealEnv: crypto pool")

	// Create the plugin schema and run plugin migrations into it via a
	// search_path-scoped connStr so the plugin's scene_log coexists with the
	// host crypto tables (which stay in public).
	_, err = cryptoPool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS plugin_core_scenes`)
	Expect(err).NotTo(HaveOccurred(), "buildSnapshotRealEnv: create plugin schema")
	pluginConnStr := appendSearchPath(connStr, "plugin_core_scenes")
	store, err := NewSceneStore(ctx, pluginConnStr)
	Expect(err).NotTo(HaveOccurred(), "buildSnapshotRealEnv: NewSceneStore")

	bus := testutil.StartEmbeddedJetStream(suiteT)
	_, err = bus.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "AUDIT",
		Subjects: []string{"audit.>"},
		Storage:  jetstream.MemoryStorage,
	})
	Expect(err).NotTo(HaveOccurred())

	kekHex := testutil.RandomKEKHex(suiteT)
	suiteT.Setenv("HOLOMUSH_C7_SNAP_KEK", kekHex)
	kekSrc := kek.NewEnvSource("HOLOMUSH_C7_SNAP_KEK", false)
	provider, err := kek.NewLocalAEADProvider(ctx, kekSrc, cryptoPool)
	Expect(err).NotTo(HaveOccurred())

	dekStore := dek.NewStore(cryptoPool)
	dekCache := dek.NewCache(dek.CacheConfig{Capacity: 64})
	dekPartCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64})
	dekMgr, err := dek.NewManager(provider, dekStore, dekCache, dekPartCache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&snapDEKBindingStub{bindingID: "bind-c7-snap"})
	Expect(err).NotTo(HaveOccurred())

	// Host audit subsystem populates events_audit (our ciphertext source).
	hostSub := audit.NewSubsystem(snapFixedJS{js: bus.JS}, snapFixedPool{pool: cryptoPool}, audit.Config{})
	Expect(hostSub.Start(ctx)).To(Succeed())

	// Production scene IC events are emitted with QUALIFIED wire types
	// (core-scenes:scene_pose / :scene_say / :scene_emit — commands.go:516-520,
	// holomush-r0kup), so the AAD binds the qualified type and scene_log stores
	// it qualified. The fixture mirrors that exactly: verb registry, manifest,
	// alwaysSensitive, and the manifest lookup all key on the qualified types,
	// and the emit uses the qualified type.
	registry, err := core.BootstrapVerbRegistry("test")
	Expect(err).NotTo(HaveOccurred())
	for _, et := range []string{"core-scenes:scene_pose", "core-scenes:scene_say", "core-scenes:scene_emit"} {
		Expect(registry.RegisterWithSource(core.VerbRegistration{
			Type:          et,
			Category:      "communication",
			Format:        "speech",
			Label:         "poses",
			DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
			Source:        pluginName,
		}, "1.0.0")).To(Succeed())
	}

	rawPub := eventbus.NewJetStreamPublisher(bus.JS, eventbus.Config{}.Defaults(), eventbus.WithDEKManager(dekMgr))
	hostPub := eventbus.NewRenderingPublisher(rawPub, registry)

	manifestLookup := &snapManifestLookup{
		pluginName: pluginName,
		eventTypes: map[string]struct{}{
			"core-scenes:scene_pose": {},
			"core-scenes:scene_say":  {},
			"core-scenes:scene_emit": {},
		},
	}
	guardCore, err := authguard.New(
		authguard.NewDEKParticipantLookup(dekMgr),
		manifestLookup,
		policytest.AllowAllEngine(),
		snapNoopBackpressure{},
	)
	Expect(err).NotTo(HaveOccurred())
	sessionGuard := authguard.NewSessionBridgeGuard(guardCore)

	// INV-CRYPTO-28 audit emitter. The ReadbackDecryptor requires a non-nil
	// SessionAuditEmitter for plugin principals (fail-closed). The queued
	// emitter's drain goroutine silently drops audit.> publish errors against
	// the EVENTS stream, so a plain rawPub is sufficient here — this test does
	// NOT assert on the audit record (INV-CRYPTO-28 is covered by
	// test/integration/crypto/readback_test.go).
	auditEmitter, err := guardaudit.NewQueuedEmitter(rawPub)
	Expect(err).NotTo(HaveOccurred())
	sessionAuditEmitter, err := guardaudit.NewSessionBridgeEmitter(auditEmitter)
	Expect(err).NotTo(HaveOccurred())

	ownerMap, err := audit.NewOwnerMap([]audit.SubjectOwner{
		{PluginName: pluginName, Pattern: "events.*.scene.>"},
	})
	Expect(err).NotTo(HaveOccurred())
	alwaysSensitive := map[string]struct{}{
		"core-scenes:scene_pose": {},
		"core-scenes:scene_say":  {},
		"core-scenes:scene_emit": {},
	}

	decryptor := history.NewReadbackDecryptor(
		ownerMap, alwaysSensitive,
		snapAlwaysTrueCryptoKeysLookup{},
		sessionGuard, dekMgr, sessionAuditEmitter,
	)

	svc := newTestService(GinkgoT(), store)
	svc.gameID = "main"
	svc.SetSnapshotDecryptor(&realDecryptorAdapter{dec: decryptor, pluginName: pluginName})

	return &snapshotRealEnv{
		store:      store,
		svc:        svc,
		cryptoPool: cryptoPool,
		dekMgr:     dekMgr,
		pluginPub:  hostPub,
		hostSub:    hostSub,
		gameID:     "main",
		pluginName: pluginName,
		cleanup: []func(){
			func() {
				shutCtx, c := context.WithTimeout(context.Background(), 2*time.Second)
				defer c()
				_ = auditEmitter.Shutdown(shutCtx) //nolint:errcheck // test cleanup
			},
			func() { _ = hostSub.Stop(context.Background()) }, //nolint:errcheck // test cleanup
			func() { store.Close() },
			func() { cryptoPool.Close() },
		},
	}
}

// emitAndSeed emits a sensitive IC event (populating events_audit with real
// ciphertext) then copies that ciphertext row into the plugin's scene_log so
// ReadSceneLogForSnapshot finds it. Returns the EXACT subject the event was
// stored under — the AAD binds this subject, so scene_log and the runSnapshot
// call MUST use the identical string or decrypt fails the AEAD tag-check.
func (e *snapshotRealEnv) emitAndSeed(ctx context.Context, sceneID, eventType, plaintext string, participants []dek.Participant) string {
	GinkgoHelper()

	ctxID := dek.ContextID{Type: "scene", ID: sceneID}
	_, err := e.dekMgr.GetOrCreate(ctx, ctxID, participants)
	Expect(err).NotTo(HaveOccurred())

	manifest := &plugins.Manifest{
		Name:                e.pluginName,
		Emits:               []string{"scene"},
		ActorKindsClaimable: []string{"plugin"},
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: eventType, Sensitivity: plugins.SensitivityAlways, Readback: true},
			},
		},
	}
	manifestFn := func(name string) *plugins.Manifest {
		if name == e.pluginName {
			return manifest
		}
		return nil
	}
	pluginActorID := plugintest.PluginULIDFromName(e.pluginName).String()
	actorFn := func(_ context.Context, _ string) (core.Actor, error) {
		return core.Actor{Kind: core.ActorPlugin, ID: pluginActorID}, nil
	}
	emitter := plugins.NewPluginEventEmitter(e.pluginPub, manifestFn, actorFn)
	intent := pluginsdk.EmitIntent{
		Subject:   "scene." + sceneID,
		Type:      pluginsdk.EventType(eventType), // bare type — mirrors production (commands.go:516)
		Payload:   plaintext,
		Sensitive: true,
	}
	Expect(emitter.Emit(ctx, e.pluginName, intent)).To(Succeed())
	e.hostSub.AwaitDrained(GinkgoT(), 10*time.Second)

	// Copy the encrypted row from events_audit into the plugin's scene_log
	// verbatim (same id/subject/type/timestamp/actor/codec/dek so the AAD the
	// host rebuilds matches the encrypt-time AAD exactly).
	subject := "events." + e.gameID + ".scene." + sceneID
	var (
		idB        []byte
		codecStr   string
		envelopeB  []byte
		schemaVer  int32
		dekRef     sql.NullInt64
		dekVersion sql.NullInt32
	)
	err = e.cryptoPool.QueryRow(
		ctx, `
		SELECT id, codec, envelope, schema_ver, dek_ref, dek_version
		FROM events_audit WHERE subject = $1 AND type = $2 ORDER BY id DESC LIMIT 1`,
		subject, eventType,
	).Scan(&idB, &codecStr, &envelopeB, &schemaVer, &dekRef, &dekVersion)
	Expect(err).NotTo(HaveOccurred(), "emitAndSeed: read events_audit ciphertext")

	var env eventbusv1.Event
	Expect(proto.Unmarshal(envelopeB, &env)).To(Succeed())

	var actorKind string
	var actorID []byte
	if a := env.GetActor(); a != nil {
		actorKind = a.GetKind().String()
		actorID = a.GetId()
	}
	var dekRefP *int64
	if dekRef.Valid {
		dekRefP = &dekRef.Int64
	}
	var dekVerP *int32
	if dekVersion.Valid {
		v := dekVersion.Int32
		dekVerP = &v
	}

	_, err = e.store.Pool().Exec(
		ctx, `
		INSERT INTO scene_log (id, subject, type, timestamp, actor_kind, actor_id, payload, schema_ver, codec, dek_ref, dek_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		idB, subject, eventType,
		env.GetTimestamp().AsTime().UnixNano(),
		actorKind, actorID, env.GetPayload(), schemaVer, codecStr, dekRefP, dekVerP,
	)
	Expect(err).NotTo(HaveOccurred(), "emitAndSeed: INSERT scene_log ciphertext")
	return subject
}

// ---------------------------------------------------------------------------
// Plain store fixture (failure-mode arms — fake decryptor, identity scene_log)
// ---------------------------------------------------------------------------

// newSnapshotPlainEnv returns a plugin-only store (RawDatabase) plus a service
// wired with the given fake decryptor. Used for failure-mode / soft-no-op /
// idempotency arms where no real crypto is needed.
func newSnapshotPlainEnv(dec snapshotDecryptor) (*SceneStore, *SceneServiceImpl) {
	GinkgoHelper()
	store := newTestStore()
	svc := newTestService(GinkgoT(), store)
	svc.gameID = "main"
	svc.SetSnapshotDecryptor(dec)
	return store, svc
}

// seedCoolOffAttempt creates a scene + an all-yes COOLOFF attempt directly so
// runSnapshot has a lockable COOLOFF row. Returns the attempt ID.
func seedCoolOffAttempt(ctx context.Context, store *SceneStore, sceneID, ownerID string) string {
	GinkgoHelper()

	row := &SceneRow{
		ID: sceneID, Title: "Snapshot Scene", OwnerID: ownerID,
		State: string(SceneStateEnded), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen), ContentWarnings: []string{}, Tags: []string{},
	}
	Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

	pub, err := store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
		SceneID: sceneID, AttemptNumber: 1, InitiatedBy: ownerID,
		VoteWindow: time.Hour, CoolOffWindow: time.Minute, MaxAttempts: 3,
	})
	Expect(err).NotTo(HaveOccurred())

	// All roster members vote yes, then move COLLECTING→COOLOFF.
	voters, err := store.ListPublishVoters(ctx, pub.ID)
	Expect(err).NotTo(HaveOccurred())
	for _, v := range voters {
		_, err := store.CastVote(ctx, pub.ID, v.CharacterID, true)
		Expect(err).NotTo(HaveOccurred())
	}
	now := time.Now()
	Expect(store.TransitionStatus(ctx, pub.ID, TransitionInput{
		To: StatusCoolOff, SetCoolOffAt: &now,
	})).To(Succeed())
	return pub.ID
}

// seedScenePoseLog inserts an identity-codec scene_log row with the given
// {actor_id,text} payload so the fake-decryptor pipeline has rows to read.
func seedScenePoseLog(ctx context.Context, store *SceneStore, sceneID, actorID, text string) {
	GinkgoHelper()
	subject := "events.main.scene." + sceneID + ".ic"
	payload, err := json.Marshal(map[string]string{"actor_id": actorID, "text": text})
	Expect(err).NotTo(HaveOccurred())
	id := newPoseULID()
	_, err = store.Pool().Exec(
		ctx, `
		INSERT INTO scene_log (id, subject, type, timestamp, actor_kind, actor_id, payload, schema_ver, codec)
		VALUES ($1, $2, 'core-scenes:scene_pose', $3, 'character', $4, $5, 1, 'identity')`,
		id, subject, time.Now().UnixNano(), []byte(actorID), payload,
	)
	Expect(err).NotTo(HaveOccurred())
}

func icSubjectFor(sceneID string) string { return "events.main.scene." + sceneID + ".ic" }

// appendSearchPath adds (or replaces) the search_path query parameter on a
// URL-form Postgres connection string so the plugin's tables land in the named
// schema while the host crypto tables stay in public.
func appendSearchPath(connStr, schema string) string {
	GinkgoHelper()
	u, err := url.Parse(connStr)
	Expect(err).NotTo(HaveOccurred(), "appendSearchPath: parse connStr")
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// ---------------------------------------------------------------------------
// Specs
// ---------------------------------------------------------------------------

var _ = Describe("C7 snapshot pipeline (COOLOFF → PUBLISHED)", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
	})
	AfterEach(func() { cancel() })

	// INV-CRYPTO-33 / INV-CRYPTO-31 consumer-side: real ciphertext in scene_log decrypts
	// end-to-end through the production primitive; the attempt transitions to
	// PUBLISHED with rendered PLAINTEXT content_entries and the scene archives.
	It("publishes with decrypted content_entries and archives the scene (INV-CRYPTO-33)", func() {
		const (
			pluginName = "core-scenes"
			sceneID    = "01C7SNAPHAPPY00000000000A"
			ownerID    = "01C7SNAPHAPPYOWNER000000A"
			text       = "Alice waves warmly."
		)
		env := buildSnapshotRealEnv(ctx, pluginName)
		defer env.teardown()

		// Seed a real encrypted pose into scene_log; use the EXACT subject it
		// was stored under (the AAD binds it).
		seededSubject := env.emitAndSeed(ctx, sceneID, "core-scenes:scene_pose", `{"actor_id":"`+ownerID+`","text":"`+text+`"}`,
			[]dek.Participant{{
				PlayerID: "01C7SNAPHAPPYPLAYER00000A", CharacterID: ownerID,
				BindingID: "01C7SNAPHAPPYBIND00000A", JoinedAt: time.Now().UTC(), AddedVia: "c7_test",
			}})

		attemptID := seedCoolOffAttempt(ctx, env.store, sceneID, ownerID)

		Expect(env.svc.runSnapshot(ctx, attemptID, sceneID, seededSubject)).To(Succeed())

		// PUBLISHED with content_entries holding the rendered PLAINTEXT.
		pub, err := env.store.GetPublishedSceneHeader(ctx, attemptID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pub.Status).To(Equal(StatusPublished), "INV-CRYPTO-33: attempt must be PUBLISHED")
		Expect(pub.PublishedAt).NotTo(BeNil())

		entries, err := env.store.GetPublishedSceneContent(ctx, attemptID)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(1), "INV-CRYPTO-33: content_entries must hold the decrypted pose")
		Expect(entries[0].Kind).To(Equal(EntryKindPose))
		Expect(entries[0].Content).To(Equal(text),
			"INV-CRYPTO-33: content_entries must hold rendered PLAINTEXT, not ciphertext")

		// Scene archived.
		scene, err := env.store.Get(ctx, sceneID)
		Expect(err).NotTo(HaveOccurred())
		Expect(scene.State).To(Equal(string(SceneStateArchived)),
			"INV-CRYPTO-33 / INV-SCENE-31: parent scene must transition to archived ONLY on PUBLISHED")
	})

	// INV-CRYPTO-35 / INV-CRYPTO-37: a host decrypt error fails the publish closed with
	// SNAPSHOT_DECRYPT_FAILED and writes NO content.
	It("transitions to ATTEMPT_FAILED with SNAPSHOT_DECRYPT_FAILED on decrypt error (INV-CRYPTO-35)", func() {
		const sceneID = "01C7SNAPDECFAIL000000000A"
		const ownerID = "01C7SNAPDECFAILOWNER0000A"
		dec := &fakeSnapshotDecryptor{err: oops.Code("DEK_DESTROYED").Errorf("dek gone")}
		store, svc := newSnapshotPlainEnv(dec)

		seedScenePoseLog(ctx, store, sceneID, ownerID, "doomed pose")
		attemptID := seedCoolOffAttempt(ctx, store, sceneID, ownerID)

		Expect(svc.runSnapshot(ctx, attemptID, sceneID, icSubjectFor(sceneID))).To(Succeed())

		pub, err := store.GetPublishedSceneHeader(ctx, attemptID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pub.Status).To(Equal(StatusAttemptFailed), "INV-CRYPTO-35: decrypt error → ATTEMPT_FAILED")
		Expect(pub.FailureReason).NotTo(BeNil())
		Expect(*pub.FailureReason).To(Equal(FailureSnapshotDecryptFailed),
			"INV-CRYPTO-35: failure_reason must be SNAPSHOT_DECRYPT_FAILED")
		entries, err := store.GetPublishedSceneContent(ctx, attemptID)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty(), "INV-CRYPTO-35: no content written on decrypt failure")
	})

	// INV-CRYPTO-37: ANY per-row refusal → publish fails closed.
	It("transitions to ATTEMPT_FAILED with SNAPSHOT_DECRYPT_FAILED on a per-row refusal (INV-CRYPTO-37)", func() {
		const sceneID = "01C7SNAPREFUSE0000000000A"
		const ownerID = "01C7SNAPREFUSEOWNER00000A"
		dec := &fakeSnapshotDecryptor{refuseReason: "not_owner"}
		store, svc := newSnapshotPlainEnv(dec)

		seedScenePoseLog(ctx, store, sceneID, ownerID, "refused pose")
		attemptID := seedCoolOffAttempt(ctx, store, sceneID, ownerID)

		Expect(svc.runSnapshot(ctx, attemptID, sceneID, icSubjectFor(sceneID))).To(Succeed())

		pub, err := store.GetPublishedSceneHeader(ctx, attemptID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pub.Status).To(Equal(StatusAttemptFailed))
		Expect(*pub.FailureReason).To(Equal(FailureSnapshotDecryptFailed),
			"INV-CRYPTO-37: any per-row refusal is a publish failure")
	})

	// SNAPSHOT_RENDER_FAILED: a decrypted payload that is not valid {actor_id,text}
	// JSON fails the render step.
	It("transitions to ATTEMPT_FAILED with SNAPSHOT_RENDER_FAILED on a malformed payload", func() {
		const sceneID = "01C7SNAPRENDER0000000000A"
		const ownerID = "01C7SNAPRENDEROWNER00000A"
		dec := &fakeSnapshotDecryptor{plaintextFor: map[string][]byte{
			"core-scenes:scene_pose": []byte("this is not json"),
		}}
		store, svc := newSnapshotPlainEnv(dec)

		seedScenePoseLog(ctx, store, sceneID, ownerID, "ignored — decryptor overrides")
		attemptID := seedCoolOffAttempt(ctx, store, sceneID, ownerID)

		Expect(svc.runSnapshot(ctx, attemptID, sceneID, icSubjectFor(sceneID))).To(Succeed())

		pub, err := store.GetPublishedSceneHeader(ctx, attemptID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pub.Status).To(Equal(StatusAttemptFailed))
		Expect(*pub.FailureReason).To(Equal(FailureSnapshotRenderFailed),
			"render failure → SNAPSHOT_RENDER_FAILED")
	})

	// INV-SCENE-37 idempotency / vote-flip: a second fire after PUBLISHED is a no-op
	// (the lock re-check finds non-COOLOFF status).
	It("is idempotent — a re-fire after PUBLISHED is a no-op (INV-SCENE-37)", func() {
		const sceneID = "01C7SNAPIDEMPOTENT00000A0"
		const ownerID = "01C7SNAPIDEMPOTENTOWN000A"
		dec := &fakeSnapshotDecryptor{plaintextFor: map[string][]byte{
			"core-scenes:scene_pose": []byte(`{"actor_id":"` + ownerID + `","text":"once"}`),
		}}
		store, svc := newSnapshotPlainEnv(dec)

		seedScenePoseLog(ctx, store, sceneID, ownerID, "once")
		attemptID := seedCoolOffAttempt(ctx, store, sceneID, ownerID)

		Expect(svc.runSnapshot(ctx, attemptID, sceneID, icSubjectFor(sceneID))).To(Succeed())
		pub1, err := store.GetPublishedSceneHeader(ctx, attemptID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pub1.Status).To(Equal(StatusPublished))

		// Re-fire: must be a clean no-op, leaving the PUBLISHED row untouched.
		Expect(svc.runSnapshot(ctx, attemptID, sceneID, icSubjectFor(sceneID))).To(Succeed())
		pub2, err := store.GetPublishedSceneHeader(ctx, attemptID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pub2.Status).To(Equal(StatusPublished), "INV-SCENE-37: re-fire must not change a PUBLISHED row")
		Expect(pub2.PublishedAt.Time()).To(Equal(pub1.PublishedAt.Time()),
			"INV-SCENE-37: published_at must be stable across re-fire (no re-publish)")
	})

	// COOLOFF_INVARIANT_BROKEN: a no-vote that landed before the lock fails the
	// all-yes re-validation under the lock.
	It("transitions to ATTEMPT_FAILED with COOLOFF_INVARIANT_BROKEN when a no slipped in", func() {
		const sceneID = "01C7SNAPINVARIANT00000A00"
		const ownerID = "01C7SNAPINVARIANTOWNER0A0"
		dec := &fakeSnapshotDecryptor{plaintextFor: map[string][]byte{
			"core-scenes:scene_pose": []byte(`{"actor_id":"` + ownerID + `","text":"x"}`),
		}}
		store, svc := newSnapshotPlainEnv(dec)

		seedScenePoseLog(ctx, store, sceneID, ownerID, "x")
		attemptID := seedCoolOffAttempt(ctx, store, sceneID, ownerID)

		// Flip the owner's vote to NO while the attempt is still in COOLOFF
		// (simulating a vote-flip that committed before the snapshot lock). We
		// keep the status COOLOFF so the lock succeeds but the all-yes re-check
		// fails.
		_, err := store.CastVote(ctx, attemptID, ownerID, false)
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.runSnapshot(ctx, attemptID, sceneID, icSubjectFor(sceneID))).To(Succeed())

		pub, err := store.GetPublishedSceneHeader(ctx, attemptID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pub.Status).To(Equal(StatusAttemptFailed))
		Expect(*pub.FailureReason).To(Equal(FailureCoolOffInvariantBroken),
			"all-yes broken at re-validate → COOLOFF_INVARIANT_BROKEN")
	})

	// FK soft-no-op (bead requirement 3 / ADR holomush-jrefa): the scenes row is
	// DELETED between attempt creation and snapshot fire. Publication STILL
	// completes (PUBLISHED + content_entries) and the scene-state UPDATE no-ops
	// without erroring.
	It("completes publication even when the parent scene was deleted (FK soft-no-op)", func() {
		const sceneID = "01C7SNAPFKGONE00000000A00"
		const ownerID = "01C7SNAPFKGONEOWNER0000A0"
		dec := &fakeSnapshotDecryptor{plaintextFor: map[string][]byte{
			"core-scenes:scene_pose": []byte(`{"actor_id":"` + ownerID + `","text":"survives deletion"}`),
		}}
		store, svc := newSnapshotPlainEnv(dec)

		seedScenePoseLog(ctx, store, sceneID, ownerID, "survives deletion")
		attemptID := seedCoolOffAttempt(ctx, store, sceneID, ownerID)

		// Delete the parent scene AFTER the attempt exists (published_scenes has
		// no FK to scenes(id), so this is allowed). scene_log + published_scenes
		// rows survive.
		_, err := store.Pool().Exec(ctx, `DELETE FROM scene_participants WHERE scene_id = $1`, sceneID)
		Expect(err).NotTo(HaveOccurred())
		_, err = store.Pool().Exec(ctx, `DELETE FROM scenes WHERE id = $1`, sceneID)
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.runSnapshot(ctx, attemptID, sceneID, icSubjectFor(sceneID))).To(Succeed(),
			"FK soft-no-op: publication completes; no error on missing scene")

		pub, err := store.GetPublishedSceneHeader(ctx, attemptID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pub.Status).To(Equal(StatusPublished),
			"FK soft-no-op: publication finalizes to PUBLISHED even with the scene gone")
		entries, err := store.GetPublishedSceneContent(ctx, attemptID)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(1), "FK soft-no-op: content_entries still populated")
	})

	// Chunking: a row set larger than snapshotDecryptBatch is decrypted across
	// multiple ≤500-row calls (read-back design §3.2 / §6).
	It("chunks the decrypt into ≤500-row calls for a large scene", func() {
		const sceneID = "01C7SNAPCHUNK00000000A000"
		const ownerID = "01C7SNAPCHUNKOWNER0000A00"
		dec := &fakeSnapshotDecryptor{plaintextFor: map[string][]byte{
			"core-scenes:scene_pose": []byte(`{"actor_id":"` + ownerID + `","text":"chunk"}`),
		}}
		store, svc := newSnapshotPlainEnv(dec)

		const rowCount = snapshotDecryptBatch + 50 // 550 → two chunks (500 + 50)
		for i := 0; i < rowCount; i++ {
			seedScenePoseLog(ctx, store, sceneID, ownerID, fmt.Sprintf("pose-%d", i))
		}
		attemptID := seedCoolOffAttempt(ctx, store, sceneID, ownerID)

		Expect(svc.runSnapshot(ctx, attemptID, sceneID, icSubjectFor(sceneID))).To(Succeed())

		Expect(dec.calls).To(Equal([]int{snapshotDecryptBatch, 50}),
			"decrypt must chunk into ≤500-row calls (read-back design §3.2)")

		pub, err := store.GetPublishedSceneHeader(ctx, attemptID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pub.Status).To(Equal(StatusPublished))
		entries, err := store.GetPublishedSceneContent(ctx, attemptID)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(rowCount), "all chunked rows must be rendered")
	})
})
