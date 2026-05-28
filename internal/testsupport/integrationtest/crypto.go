// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/authguard"
	authguardaudit "github.com/holomush/holomush/internal/eventbus/authguard/audit"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/eventbus/history"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/pgnanos"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/cryptowiring"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// WithPluginCrypto wires the full plugin-crypto round-trip (emit fence → publish
// encrypt → audit projection → read-back) into the harness. REQUIRES
// WithInTreePlugins (the emitter, per-plugin consumer, and read-back decryptor
// all need the loaded Manager). Assumes crypto-CORRECT plugins: WithCryptoEnabled
// is global to the shared emitter, so a loaded plugin that emits
// sensitivity:always content without claiming Sensitive=true would reject (spec
// §6.2). Drive only crypto-correct plugins (e.g. core-scenes) under this option.
func WithPluginCrypto() StartOption {
	return func(c *startConfig) { c.withPluginCrypto = true }
}

// pluginCrypto bundles the test crypto substrate: an ephemeral KEK, a
// pool-backed DEK manager (so DEKs persist to crypto_keys), and a
// crypto-enabled publisher over the embedded bus.
type pluginCrypto struct {
	dekMgr    dek.Manager
	selector  codec.KeySelector
	publisher eventbus.Publisher // crypto-enabled (DEK + identity selector), rendering-wrapped
	// actorID is the stable character actor stamped on emits (EmitPluginEvent).
	// Fixed per Server so the actor persisted to scene_log round-trips byte-equal
	// into the read-back AAD.
	actorID ulid.ULID
	// sceneID is the stable scene the emitted scene_* events belong to. The
	// subject is events.<game_id>.scene.<sceneID>.ic; a row is seeded in
	// plugin_core_scenes.scenes so core-scenes' InsertScenePose FK/UPDATE
	// (scenes.total_pose_count … RETURNING) finds the scene.
	sceneID ulid.ULID
}

// newPluginCrypto builds the test crypto substrate: ephemeral KEK → DEK manager
// (pool-backed Store, so DEKs persist to crypto_keys) → a crypto-enabled
// publisher over the embedded bus. Mirrors holomushtest.newKEKProvider
// (server.go:492-505) + dek.NewManager (server.go:404-409).
//
// The publisher is wired WithDEKManager so that sensitive events
// (event.Sensitive=true) take the encrypt branch in JetStreamPublisher.Publish
// (publisher.go:208-225): that branch ignores the codec selector and uses
// codec.NameXChaCha20v1 with a DEK from GetOrCreate, persisting a crypto_keys
// row and stamping App-Dek-Ref. The identity KeySelector wired via
// WithCodecSelector only governs the non-sensitive (default) path.
func newPluginCrypto(t *testing.T, bus *eventbustest.Embedded, pool *pgxpool.Pool, verbReg *core.VerbRegistry) *pluginCrypto {
	t.Helper()
	ctx := context.Background()

	kekBytes := make([]byte, kek.KEKByteLength)
	_, err := rand.Read(kekBytes)
	require.NoError(t, err, "newPluginCrypto: read KEK bytes")
	const envName = "HOLOMUSH_TEST_KEK_PLUGINCRYPTO"
	t.Setenv(envName, hex.EncodeToString(kekBytes))
	provider, err := kek.NewLocalAEADProviderForUnitTest(ctx, kek.NewEnvSource(envName, false))
	require.NoError(t, err, "newPluginCrypto: KEK provider")

	cacheCfg := dek.CacheConfig{Capacity: 64, TTL: time.Minute}
	dekMgr, err := dek.NewManager(
		provider, dek.NewStore(pool),
		dek.NewCache(cacheCfg), dek.NewParticipantsCache(cacheCfg),
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil }, // noop invalidator
		worldpg.NewBindingRepository(pool),                                                   // satisfies dek.BindingResolver via Current
	)
	require.NoError(t, err, "newPluginCrypto: dek.NewManager")

	// The RenderingPublisher (wrapped below) looks up a verb for every emitted
	// event type. core-scenes registers its scene event types as crypto.emits
	// (not manifest verbs:), so the bootstrap registry has no entry — register
	// them here so plugin emits resolve. Mirrors the reference round-trip test
	// (test/integration/crypto/readback_test.go:141) which registers the scene
	// emit type as a verb before emitting.
	registerSceneEmitVerbs(t, verbReg)

	sel := cryptowiring.KeySelector()
	raw := bus.Bus.Publisher(eventbus.WithDEKManager(dekMgr), eventbus.WithCodecSelector(sel))
	return &pluginCrypto{
		dekMgr:    dekMgr,
		selector:  sel,
		publisher: eventbus.NewRenderingPublisher(raw, verbReg),
		actorID:   idgen.New(),
		sceneID:   idgen.New(),
	}
}

// registerSceneEmitVerbs registers core-scenes' plugin-owned scene event types
// as rendering verbs so the RenderingPublisher resolves them on the plugin emit
// path. The list mirrors core-scenes' phase4EmitTypes (sensitive IC content) +
// phase6EmitTypes (publication notices); the harness can't import the
// `package main` plugin, so the set is duplicated here. RegisterWithSource is
// idempotent-safe for fresh per-test registries.
func registerSceneEmitVerbs(t *testing.T, verbReg *core.VerbRegistry) {
	t.Helper()
	sceneEmitTypes := []string{
		"scene_pose", "scene_say", "scene_emit", "scene_ooc",
		"scene_join_ic", "scene_leave_ic", "scene_pose_order_changed_ic", "scene_idle_nudge",
		"scene_publish_started", "scene_publish_vote_cast", "scene_publish_cooloff_started",
		"scene_publish_resolved", "scene_publish_withdrawn", "scene_publish_vote_attempts_extended",
	}
	for _, et := range sceneEmitTypes {
		require.NoErrorf(t, verbReg.RegisterWithSource(core.VerbRegistration{
			Type:          et,
			Category:      "communication",
			Format:        "speech",
			Label:         et,
			DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
			Source:        "core-scenes",
		}, "1.0.0"), "registerSceneEmitVerbs: register %q", et)
	}
}

// EmittedEvent is the result of EmitPluginEvent, carrying the translated NATS
// subject for wire assertions.
type EmittedEvent struct{ SubjectStr string }

// EmitPluginEvent drives a real plugin emit through the Manager's
// EmitPluginEvent boundary (the same path core-scenes commands use), returning
// the translated NATS subject for wire assertions.
//
// The legacy colon-style subject is derived as "<plugin>:<eventType>" — a
// well-formed legacy subject whose namespace token matches the plugin's
// declared emit namespace. The NATS subject returned mirrors the translation
// the emitter itself performs (subjectxlate.Legacy), so WireCodecFor can read
// the JetStream message by subject. Panics via requirePluginCrypto if the
// substrate was not wired.
func (s *Server) EmitPluginEvent(ctx context.Context, plugin, eventType, payloadJSON string, sensitive bool) EmittedEvent {
	// WithPluginCrypto's fixed scene is keyed by the BARE sceneID ULID (the
	// seedScene row at crypto.go uses pc.sceneID.String() with no prefix), so
	// the subject's scene-id token is that bare form — self-consistent with the
	// seeded row. Preserves 5iaov's callers byte-for-byte.
	return s.emitPluginEventForScene(ctx, plugin,
		s.pluginCrypto.sceneID.String(), s.pluginCrypto.actorID, eventType, payloadJSON, sensitive)
}

// EmitSceneICContent emits an encrypted (Sensitive=true) scene IC event for an
// ARBITRARY scene + actor — used to seed content into a CreateScene-created
// scene (the command emit path can't set Sensitive; INV-7 fence, spec §3.4).
// Requires WithPluginCrypto + WithInTreePlugins. The scene row MUST already
// exist (via CreateScene) so core-scenes' InsertScenePose UPDATE … RETURNING
// resolves.
//
// sceneID is the BARE ULID returned by Session.CreateScene. core-scenes
// persists scene rows under that bare id (newSceneID, service.go:1113,
// holomush-y5inx) and its InsertScenePose keys off it via
// parseSceneSubject(subject)[3] → UPDATE scenes WHERE id=<token>. The subject's
// scene-id token is the bare sceneID — identical to production's own subject
// builder dotStyleSceneSubjectIC, which is fed the stored bare id.
func (s *Server) EmitSceneICContent(ctx context.Context, plugin string, sceneID, actorID ulid.ULID, eventType, payloadJSON string) EmittedEvent {
	return s.emitPluginEventForScene(ctx, plugin,
		sceneID.String(), actorID, eventType, payloadJSON, true)
}

// emitPluginEventForScene is the parameterized core extracted from
// EmitPluginEvent. It drives a real plugin emit through the Manager's
// EmitPluginEvent boundary (the same path core-scenes commands use), returning
// the translated NATS subject for wire assertions.
//
// sceneSubjectID is the scene-id token placed verbatim into the subject's
// <sceneID> segment (events.<game_id>.scene.<sceneSubjectID>.ic). It MUST match
// the id stored in plugin_core_scenes.scenes for that scene, because
// core-scenes' AuditEvent handler routes scene_pose through InsertScenePose,
// which calls parseSceneSubject (requires the 5-token <id>.<channel> shape,
// audit.go:629) and UPDATEs scenes WHERE id=<sceneSubjectID> … RETURNING
// (requires the matching scene row). Callers own forming that token:
// EmitPluginEvent passes the bare WithPluginCrypto sceneID (its self-seeded
// row uses the bare form); EmitSceneICContent passes the bare ULID (matching
// CreateScene's stored id).
//
// Panics via requirePluginCrypto if the substrate was not wired.
func (s *Server) emitPluginEventForScene(ctx context.Context, plugin, sceneSubjectID string, actorID ulid.ULID, eventType, payloadJSON string, sensitive bool) EmittedEvent {
	s.requirePluginCrypto("emitPluginEventForScene")
	s.t.Helper()

	// Namespace token MUST be a namespace the plugin declares in its manifest
	// `emits:` (the emitter rejects otherwise). core-scenes declares
	// `emits: [scene]`, so the namespace is "scene".
	dp, ok := s.pluginSub.Manager().GetLoadedPlugin(plugin)
	require.Truef(s.t, ok, "integrationtest.Server.emitPluginEventForScene: plugin %q not loaded", plugin)
	require.NotEmptyf(s.t, dp.Manifest.Emits,
		"integrationtest.Server.emitPluginEventForScene: plugin %q declares no emit namespaces", plugin)

	// Build a well-formed scene IC subject: events.<game_id>.scene.<sceneSubjectID>.ic.
	natsSubject := "events." + s.bus.Bus.GameID() + ".scene." + sceneSubjectID + ".ic"

	// Stamp the host event actor the emitter requires (event_emitter.go::Emit →
	// actorFromContext). Production stamps it at the command-dispatch / host-RPC
	// boundary; this helper bypasses dispatch, so it stamps directly. A character
	// actor is in core-scenes' actor_kinds_claimable list ([plugin, character])
	// and is what scene_pose ingest requires. The actor's (kind, id) is persisted
	// to scene_log and rebuilt verbatim into the read-back AAD, so it MUST be
	// stable per emit.
	ctx = core.WithActor(ctx, core.Actor{Kind: core.ActorCharacter, ID: actorID.String()})

	err := s.pluginSub.Manager().EmitPluginEvent(ctx, plugin, pluginsdk.EmitEvent{
		Stream:    natsSubject, // already a dot-style events.<gid>.scene.<id>.ic subject; passed through unchanged
		Type:      pluginsdk.EventType(eventType),
		Payload:   payloadJSON,
		Sensitive: sensitive,
	})
	require.NoError(s.t, err, "integrationtest.Server.emitPluginEventForScene: Manager.EmitPluginEvent")

	return EmittedEvent{SubjectStr: natsSubject}
}

// WireCodecFor reads the App-Codec header on the most recent JetStream message
// for the subject. Returns codec.NameIdentity if no message is present yet (so
// the suite's Eventually(...).ShouldNot(Equal(NameIdentity)) polls until the
// encrypted message lands). Panics via requirePluginCrypto if the substrate was
// not wired.
func (s *Server) WireCodecFor(_ context.Context, subject string) codec.Name {
	s.requirePluginCrypto("WireCodecFor")
	s.t.Helper()

	msgs := s.bus.RawMessagesOnSubject(s.t, subject, 1, eventbustest.DefaultAwaitTimeout)
	if len(msgs) == 0 {
		return codec.NameIdentity
	}
	return codec.Name(msgs[len(msgs)-1].Header.Get(eventbus.HeaderCodec))
}

// DEKRowCount counts rows in crypto_keys. Panics via requirePluginCrypto if the
// substrate was not wired.
func (s *Server) DEKRowCount(ctx context.Context) int {
	s.requirePluginCrypto("DEKRowCount")
	s.t.Helper()

	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM crypto_keys`).Scan(&n)
	require.NoError(s.t, err, "integrationtest.Server.DEKRowCount: count crypto_keys")
	return n
}

// requirePluginCrypto panics if WithPluginCrypto was not passed to Start.
func (s *Server) requirePluginCrypto(method string) {
	if s.pluginCrypto == nil {
		panic("integrationtest: " + method + "() requires Start(t, WithInTreePlugins(), WithPluginCrypto())")
	}
}

// --- Task 8: audit projection (link 3) + read-back decryptor (link 4) ---

// pluginAuditClientAdapter bridges the proto-generated
// pluginv1.PluginAuditServiceClient to the narrow audit.PluginAuditClient
// interface the PluginConsumerManager dispatch path consumes. Mirrors the
// production adapter at cmd/holomush/core.go:1165-1180.
type pluginAuditClientAdapter struct {
	client pluginv1.PluginAuditServiceClient
}

func (a pluginAuditClientAdapter) AuditEvent(ctx context.Context, req *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error) {
	resp, err := a.client.AuditEvent(ctx, req)
	if err != nil {
		return nil, oops.Code("AUDIT_PLUGIN_RPC_FAILED").Wrap(err)
	}
	return resp, nil
}

// startPluginConsumers wires the per-plugin audit projection (link 3): a
// PluginConsumerManager whose consumers ack-and-forward each plugin-owned
// subject's deliveries to that plugin's PluginAuditService.AuditEvent RPC,
// which projects the (encrypted) event into the plugin's audit table (e.g.
// plugin_core_scenes.scene_log). Mirrors cmd/holomush/core.go:556-591.
//
// INV-P7-9: the SAME codec.KeySelector instance threaded here MUST also feed
// the read-side history reader; the caller (Start) owns that pointer identity
// by passing pc.selector to both sinks.
func startPluginConsumers(t *testing.T, ctx context.Context, bus *eventbustest.Embedded, mgr *plugins.Manager, sel codec.KeySelector) *audit.PluginConsumerManager {
	t.Helper()
	pcm := audit.NewPluginConsumerManager(bus.JS, audit.WithKeySelector(sel))
	byPlugin := map[string][]string{}
	for _, d := range mgr.AuditSubjects() {
		byPlugin[d.PluginName] = append(byPlugin[d.PluginName], d.Subject)
	}
	for name, subjects := range byPlugin {
		client, ok := mgr.PluginAuditClient(name)
		if !ok {
			continue
		}
		require.NoErrorf(t, pcm.Add(ctx, audit.PluginConsumerConfig{
			PluginName: name,
			Subjects:   subjects,
			Client:     pluginAuditClientAdapter{client: client},
		}), "startPluginConsumers: pcm.Add %s", name)
	}
	// Start begins the Consume loops; without it the registered consumers never
	// dispatch deliveries (plugin_consumer.go:235 gates consumption on started).
	require.NoError(t, pcm.Start(ctx), "startPluginConsumers: pcm.Start")
	return pcm
}

// countingAuditEmitter wraps a SessionAuditEmitter, incrementing an atomic
// counter SYNCHRONOUSLY on every EmitPluginDecrypt call. The read-back path
// (history.decryptPluginRow) calls EmitPluginDecrypt inline during
// DecryptOwnRow, so the count is observable the instant ReadBackOwnRows returns
// — unlike the underlying authguardaudit.Emitter, which enqueues and PUBLISHES
// the audit event on a drain goroutine (async). Counting at the publish seam
// would race the suite's synchronous ReadBackAuditCount read. The audit record
// is ALSO published to audit.<game_id>.plugin_decrypt.<plugin>, which the EVENTS
// stream (subject filter events.>) does not capture, so reading the count off
// the stream is not an option either.
type countingAuditEmitter struct {
	inner eventbus.SessionAuditEmitter
	n     *atomic.Int64
}

func (c countingAuditEmitter) EmitPluginDecrypt(ctx context.Context, rec eventbus.PluginDecryptRecord) error {
	c.n.Add(1)
	return c.inner.EmitPluginDecrypt(ctx, rec)
}

// seedScene inserts the scene row the scene_pose projection path requires.
// MUST run after plugins load (the plugin_core_scenes schema + migrations are
// created during core-scenes Init). created_at is BIGINT epoch-ns (migration
// 000007); owner_id is the emit actor.
func (s *Server) seedScene(ctx context.Context, pc *pluginCrypto) {
	s.t.Helper()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO plugin_core_scenes.scenes (id, title, owner_id, created_at)
		 VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
		pc.sceneID.String(), "plugincrypto round-trip scene", pc.actorID.String(),
		time.Now().UTC().UnixNano())
	require.NoError(s.t, err, "integrationtest.seedScene: insert plugin_core_scenes.scenes")
}

// historyCrypto bundles the crypto substrate shared by the host history reader
// (readerCryptoOptions) and the read-back decryptor (configureReadback).
// Building these once guarantees the reader's hot-tier AuthGuard and the
// decryptor's g2 gate are the SAME guard instance over the SAME DEK-participant
// and plugin-manifest lookups — no divergent guards (holomush-y5inx.8).
type historyCrypto struct {
	// sessionGuard is the eventbus.SessionAuthGuard wrapping the real authguard
	// (DEK-participant + plugin-manifest lookups + ABAC engine + backpressure).
	// Threaded into history.NewReader (hot/cold tier auth) AND the read-back
	// decryptor's g2 gate.
	sessionGuard eventbus.SessionAuthGuard
	// sessionAuditEm is the eventbus.SessionAuditEmitter (INV-19 plugin-decrypt
	// audit). Wrapped by countingAuditEmitter for the decryptor; the reader's
	// hot-tier auth path consumes the same emitter so plugin decrypts on the
	// reader path also audit.
	sessionAuditEm eventbus.SessionAuditEmitter
	// auditEm is the raw QueuedEmitter retained for Shutdown (drain goroutines)
	// and reused as the guard's BackpressureChecker.
	auditEm *authguardaudit.Emitter
}

// buildHistoryCrypto constructs the shared AuthGuard + audit-emitter substrate
// ONCE so both the history reader and the read-back decryptor use the same
// instances. MUST run after startPlugins (the manifest lookup needs the loaded
// Manager) and BEFORE the host history reader is constructed (so
// readerCryptoOptions can thread the guard into history.NewReader). Returns the
// bundle plus the raw QueuedEmitter (retained by the caller for t.Cleanup
// Shutdown). Pure construction — all New* calls below are synchronous.
//
// Free function (not a *Server method) because it runs before the *Server is
// assembled in Start: the reader must be built with the guard threaded in, and
// the reader is constructed before the Server struct literal.
func buildHistoryCrypto(
	t *testing.T,
	pc *pluginCrypto,
	mgr *plugins.Manager,
	accessEngine authguard.ABACEngine,
	gameID string,
) *historyCrypto {
	t.Helper()

	auditEm, err := authguardaudit.NewQueuedEmitter(pc.publisher, authguardaudit.WithGameID(gameID))
	require.NoError(t, err, "integrationtest.buildHistoryCrypto: NewQueuedEmitter")
	sessionBridgeEm, err := authguardaudit.NewSessionBridgeEmitter(auditEm)
	require.NoError(t, err, "integrationtest.buildHistoryCrypto: NewSessionBridgeEmitter")

	guard, err := authguard.New(
		authguard.NewDEKParticipantLookup(pc.dekMgr),
		authguard.NewPluginManifestLookup(mgr),
		accessEngine, // ABACEngine — never invoked on the character/plugin-readback paths
		auditEm,      // BackpressureChecker — fresh QueuedEmitter ⇒ no throttle
	)
	require.NoError(t, err, "integrationtest.buildHistoryCrypto: authguard.New")

	return &historyCrypto{
		sessionGuard:   authguard.NewSessionBridgeGuard(guard),
		sessionAuditEm: sessionBridgeEm,
		auditEm:        auditEm,
	}
}

// readerCryptoOptions returns the history.Reader options that wire the shared
// AuthGuard + DEK manager + audit emitter + codec selector into the host
// history reader, so a SENSITIVE plugin-owned scene event read back through
// Session.QueryStreamHistory decrypts for an authorized DEK participant
// (hot-tier checkCharacter binding_id match) and downgrades to metadata-only
// for a non-participant. Mirrors the production newHistoryReader shape
// (cmd/holomush/sub_grpc.go:834-882): WithCodecSelector + WithHistoryAuth.
//
// The harness reader reads recent events from the hot JetStream tier, so the
// minimum is WithHistoryAuth (no source-resolver / cold-tier fallback needed —
// the readback event is always within JS retention).
func (h *historyCrypto) readerCryptoOptions(pc *pluginCrypto) []history.Option {
	return []history.Option{
		history.WithCodecSelector(pc.selector),
		history.WithHistoryAuth(h.sessionGuard, pc.dekMgr, h.sessionAuditEm),
	}
}

// configureReadback wires the read-back decryptor (link 4) using the SHARED
// SessionBridgeGuard + audit-chain emitter built once by buildHistoryCrypto,
// threaded into the Manager so the DecryptOwnAuditRows host RPC (and the
// harness's ReadBackOwnRows helper) recover plaintext from a plugin's own
// encrypted audit rows and emit the INV-19 read-back audit record. Mirrors
// cmd/holomush/sub_grpc.go:347-366,478-485.
//
// Reuses the guard + audit emitter built by buildHistoryCrypto (same instances
// the history reader uses) so there is exactly one guard in the harness. Pure
// construction — takes no context (all New* calls below are synchronous).
func (s *Server) configureReadback(pc *pluginCrypto) {
	s.t.Helper()
	require.NotNil(s.t, s.histCrypto, "integrationtest.configureReadback: buildHistoryCrypto must run first")
	mgr := s.pluginSub.Manager()

	// Wrap so ReadBackAuditCount observes the emission synchronously (see
	// countingAuditEmitter). The guard's BackpressureChecker still uses the raw
	// auditEm (throttle state lives there); only the read-back audit-emit seam
	// fed to the decryptor is wrapped.
	countingEm := countingAuditEmitter{inner: s.histCrypto.sessionAuditEm, n: &s.readbackAuditCount}

	src := managerSourceForHarness{mgr: mgr}
	decryptor := history.NewReadbackDecryptor(
		cryptowiring.OwnerMapFromManager(src),
		cryptowiring.AlwaysSensitiveSet(src),
		cryptowiring.CryptoKeysLookup(s.pool),
		s.histCrypto.sessionGuard,
		pc.dekMgr,
		countingEm,
	)
	// Wire into the Manager so the binary plugin's DecryptOwnAuditRows host RPC
	// uses it (production parity), AND retain the concrete decryptor so the
	// harness's ReadBackOwnRows helper can drive it directly without the RPC.
	mgr.ConfigureReadbackDecryptor(decryptor)
	s.readbackDecryptor = decryptor
}

// SeedSceneDEKParticipant mints the scene's DEK with sess as the sole
// participant (binding_id keyed) BEFORE the sensitive emit, so the hot-tier
// AuthGuard's checkCharacter branch PERMITS sess on read-back. GetOrCreate only
// applies `initial` participants on FIRST mint; the publisher's emit-path
// GetOrCreate(ctx, ctxID, nil) then finds this existing DEK, so the participant
// set survives. Also establishes sess's player↔character binding so
// QueryStreamHistory's binding lookup resolves to the same binding_id stamped
// here. Requires WithPluginCrypto.
func (s *Server) SeedSceneDEKParticipant(ctx context.Context, sceneID ulid.ULID, sess *Session) {
	s.requirePluginCrypto("SeedSceneDEKParticipant")
	s.t.Helper()
	require.NotZero(s.t, sess.PlayerID, "SeedSceneDEKParticipant: session needs a non-zero PlayerID (use ConnectAuthed*, not ConnectGuest)")
	bindingID := s.SeedCharacterBinding(ctx, sess)
	ctxID := dek.ContextID{Type: "scene", ID: sceneID.String()}
	_, err := s.pluginCrypto.dekMgr.GetOrCreate(ctx, ctxID, []dek.Participant{{
		PlayerID:    sess.PlayerID.String(),
		CharacterID: sess.CharacterID.String(),
		BindingID:   bindingID,
		JoinedAt:    time.Now().UTC(),
		AddedVia:    "integrationtest.SeedSceneDEKParticipant",
	}})
	require.NoError(s.t, err, "integrationtest.SeedSceneDEKParticipant: GetOrCreate DEK")
}

// SeedCharacterBinding ensures sess's character has an active player↔character
// binding row and returns its binding_id — the value QueryStreamHistory's
// binding lookup (BindingRepository.Current) resolves and the AuthGuard's
// checkCharacter compares against the DEK participant set. Idempotent: an
// existing active binding is reused. Requires WithPluginCrypto.
func (s *Server) SeedCharacterBinding(ctx context.Context, sess *Session) string {
	s.requirePluginCrypto("SeedCharacterBinding")
	s.t.Helper()
	require.NotZero(s.t, sess.PlayerID, "SeedCharacterBinding: session needs a non-zero PlayerID (use ConnectAuthed*, not ConnectGuest)")
	repo := worldpg.NewBindingRepository(s.pool)
	bindingID, err := repo.Current(ctx, sess.CharacterID.String())
	if err == nil && bindingID != "" {
		return bindingID
	}
	bindingID, err = repo.Create(ctx, sess.PlayerID.String(), sess.CharacterID.String(),
		"integrationtest.SeedCharacterBinding")
	require.NoError(s.t, err, "integrationtest.SeedCharacterBinding: Create binding")
	return bindingID
}

// managerSourceForHarness adapts *plugins.Manager to
// cryptowiring.ManifestSource — the harness copy of cmd/holomush's
// managerSource (cryptowiring_adapter.go). It supplies the read surface the
// OwnerMap (g1 gate) and always-sensitive fence set derivations need.
type managerSourceForHarness struct{ mgr *plugins.Manager }

func (s managerSourceForHarness) ListPlugins() []string { return s.mgr.ListPlugins() }

func (s managerSourceForHarness) AlwaysSensitiveEmitTypes(name string) []string {
	dp, ok := s.mgr.GetLoadedPlugin(name)
	if !ok || dp.Manifest == nil || dp.Manifest.Crypto == nil {
		return nil
	}
	var out []string
	for _, emit := range dp.Manifest.Crypto.Emits {
		if emit.Sensitivity == plugins.SensitivityAlways {
			out = append(out, emit.EventType)
		}
	}
	return out
}

func (s managerSourceForHarness) AuditSubjects() []cryptowiring.AuditSubjectDecl {
	decls := s.mgr.AuditSubjects()
	out := make([]cryptowiring.AuditSubjectDecl, 0, len(decls))
	for _, d := range decls {
		out = append(out, cryptowiring.AuditSubjectDecl{PluginName: d.PluginName, Subject: d.Subject})
	}
	return out
}

func (s managerSourceForHarness) HasAuditClient(name string) bool {
	_, ok := s.mgr.PluginAuditClient(name)
	return ok
}

var _ cryptowiring.ManifestSource = managerSourceForHarness{}

// PluginAuditRow mirrors the scene_log columns the host read-back decrypt
// primitive needs to rebuild a *pluginv1.AuditRow with byte-equal AAD
// (id/subject/type/timestamp/actor.kind/actor.id/codec/dek_ref/dek_version) —
// see plugins/core-scenes/publish_snapshot.go::logRowToAuditRow.
type PluginAuditRow struct {
	ID         []byte
	Subject    string
	Type       string
	Timestamp  pgnanos.Time
	ActorKind  string
	ActorID    []byte
	Payload    []byte
	SchemaVer  int16
	Codec      string
	DEKRef     *int64
	DEKVersion *int32
}

// ReadBackResult is one decrypted read-back outcome. Exactly one of Plaintext
// (clean decrypt) or Denied (the host decryptor refused the row via the g1
// ownership gate or g2 manifest/AuthGuard gate) is meaningful.
type ReadBackResult struct {
	Plaintext string
	Denied    bool
}

// QueryPluginAuditRows reads the projected rows for (plugin, subject) from the
// plugin's audit table. Only "core-scenes" is wired today (it projects to
// plugin_core_scenes.scene_log; the read mirrors ReadSceneLogForSnapshot's
// column set) — any other plugin name fails the test via require. Panics via
// requirePluginCrypto if the substrate was not wired.
func (s *Server) QueryPluginAuditRows(ctx context.Context, plugin, subject string) []PluginAuditRow {
	s.requirePluginCrypto("QueryPluginAuditRows")
	s.t.Helper()
	require.Equalf(s.t, "core-scenes", plugin,
		"integrationtest.QueryPluginAuditRows: only core-scenes (plugin_core_scenes.scene_log) is wired, got %q", plugin)

	rows, err := s.pool.Query(ctx, `
		SELECT id, subject, type, timestamp, actor_kind, actor_id, payload, schema_ver, codec, dek_ref, dek_version
		FROM plugin_core_scenes.scene_log
		WHERE subject = $1
		ORDER BY id ASC
	`, subject)
	require.NoError(s.t, err, "integrationtest.QueryPluginAuditRows: query scene_log")
	defer rows.Close()

	var out []PluginAuditRow
	for rows.Next() {
		var r PluginAuditRow
		require.NoError(s.t, rows.Scan(
			&r.ID, &r.Subject, &r.Type, &r.Timestamp, &r.ActorKind, &r.ActorID,
			&r.Payload, &r.SchemaVer, &r.Codec, &r.DEKRef, &r.DEKVersion,
		), "integrationtest.QueryPluginAuditRows: scan scene_log row")
		out = append(out, r)
	}
	require.NoError(s.t, rows.Err(), "integrationtest.QueryPluginAuditRows: iterate scene_log rows")
	return out
}

// ReadBackOwnRows decrypts each row as plugin's own via the read-back decryptor
// (link 4): real SessionBridgeGuard authorization + DEK decrypt + INV-19 audit
// emission. Results are 1:1 with rows in request order. A refused row
// (g1 not_owner / g2 manifest-or-AuthGuard deny / fail-closed) maps to
// Denied=true with empty Plaintext. Panics via requirePluginCrypto if the
// substrate was not wired.
func (s *Server) ReadBackOwnRows(ctx context.Context, plugin string, rows []PluginAuditRow) []ReadBackResult {
	s.requirePluginCrypto("ReadBackOwnRows")
	s.t.Helper()
	require.NotNil(s.t, s.readbackDecryptor, "integrationtest.ReadBackOwnRows: read-back decryptor not configured")

	out := make([]ReadBackResult, 0, len(rows))
	for i := range rows {
		// instanceID "" mirrors the realDecryptorAdapter test seam
		// (plugins/core-scenes/publish_snapshot_integration_test.go:156).
		res := s.readbackDecryptor.DecryptOwnRow(ctx, plugin, "", auditRowToProto(&rows[i]))
		if reason := res.GetNoPlaintextReason(); reason != "" {
			out = append(out, ReadBackResult{Denied: true})
			continue
		}
		out = append(out, ReadBackResult{Plaintext: string(res.GetPlaintext())})
	}
	return out
}

// ReadBackAuditCount returns the number of read-back audit emissions the
// read-back path has produced so far. The count is incremented SYNCHRONOUSLY by
// countingAuditEmitter on each EmitPluginDecrypt (called inline during
// DecryptOwnRow), so it is observable the instant ReadBackOwnRows returns — no
// Eventually polling needed. Panics via requirePluginCrypto if the substrate
// was not wired.
func (s *Server) ReadBackAuditCount(_ context.Context) int {
	s.requirePluginCrypto("ReadBackAuditCount")
	s.t.Helper()
	return int(s.readbackAuditCount.Load())
}

// auditRowToProto rebuilds the *pluginv1.AuditRow from a scene_log PluginAuditRow
// field-for-field, mirroring plugins/core-scenes/publish_snapshot.go::
// logRowToAuditRow so the AAD the host rebuilds (AuditRowToEvent + aad.Build) is
// byte-equal to the encrypt-side AAD (INV-RB-4 / INV-TS-5).
func auditRowToProto(r *PluginAuditRow) *pluginv1.AuditRow {
	out := &pluginv1.AuditRow{
		Id:        r.ID,
		Subject:   r.Subject,
		Type:      r.Type,
		Timestamp: timestamppb.New(r.Timestamp.Time()),
		Actor:     actorProtoFromHarnessRow(r.ActorKind, r.ActorID),
		Codec:     r.Codec,
		Payload:   r.Payload,
		SchemaVer: int32(r.SchemaVer),
	}
	if r.DEKRef != nil {
		v := uint64(*r.DEKRef) //nolint:gosec // scene_log.dek_ref is BIGINT from crypto_keys.id (>= 0); int64→uint64 widening is safe
		out.DekRef = &v
	}
	if r.DEKVersion != nil {
		v := uint32(*r.DEKVersion) //nolint:gosec // scene_log.dek_version is a 1-based counter (>= 0); int32→uint32 widening is safe
		out.DekVersion = &v
	}
	return out
}

// actorProtoFromHarnessRow mirrors plugins/core-scenes/audit.go::
// actorProtoFromRow + actorKindFromString so the rebuilt Actor matches the
// projected row's stored actor exactly (AAD round-trip).
func actorProtoFromHarnessRow(kind string, id []byte) *eventbusv1.Actor {
	if kind == "" && len(id) == 0 {
		return nil
	}
	return &eventbusv1.Actor{Kind: harnessActorKindFromString(kind), Id: id}
}

func harnessActorKindFromString(s string) eventbusv1.ActorKind {
	switch s {
	case "ACTOR_KIND_CHARACTER", "character":
		return eventbusv1.ActorKind_ACTOR_KIND_CHARACTER
	case "ACTOR_KIND_SYSTEM", "system":
		return eventbusv1.ActorKind_ACTOR_KIND_SYSTEM
	case "ACTOR_KIND_PLUGIN", "plugin":
		return eventbusv1.ActorKind_ACTOR_KIND_PLUGIN
	case "ACTOR_KIND_PLAYER", "player":
		return eventbusv1.ActorKind_ACTOR_KIND_PLAYER
	default:
		return eventbusv1.ActorKind_ACTOR_KIND_UNSPECIFIED
	}
}
