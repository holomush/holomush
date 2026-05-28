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
	s.requirePluginCrypto("EmitPluginEvent")
	s.t.Helper()

	// Legacy colon-style subject: namespace token MUST be a namespace the
	// plugin declares in its manifest `emits:` (the emitter rejects otherwise).
	// core-scenes declares `emits: [scene]`, so the namespace is "scene".
	dp, ok := s.pluginSub.Manager().GetLoadedPlugin(plugin)
	require.Truef(s.t, ok, "integrationtest.Server.EmitPluginEvent: plugin %q not loaded", plugin)
	require.NotEmptyf(s.t, dp.Manifest.Emits,
		"integrationtest.Server.EmitPluginEvent: plugin %q declares no emit namespaces", plugin)

	// Build a well-formed scene IC subject: events.<game_id>.scene.<sceneID>.ic.
	// core-scenes' AuditEvent handler routes scene_pose through InsertScenePose,
	// which calls parseSceneSubject (requires the 5-token <id>.<channel> shape,
	// audit.go:629) and UPDATEs scenes WHERE id=<sceneID> … RETURNING (requires
	// the seeded scene row). A bare "scene:scene_pose" subject would fail both.
	natsSubject := "events." + s.bus.Bus.GameID() + ".scene." + s.pluginCrypto.sceneID.String() + ".ic"

	// Stamp the host event actor the emitter requires (event_emitter.go::Emit →
	// actorFromContext). Production stamps it at the command-dispatch / host-RPC
	// boundary; this helper bypasses dispatch, so it stamps directly. A character
	// actor is in core-scenes' actor_kinds_claimable list ([plugin, character])
	// and is what scene_pose ingest requires. The actor's (kind, id) is persisted
	// to scene_log and rebuilt verbatim into the read-back AAD, so it MUST be
	// stable per emit — s.pluginCrypto.actorID is allocated once per Server.
	ctx = core.WithActor(ctx, core.Actor{Kind: core.ActorCharacter, ID: s.pluginCrypto.actorID.String()})

	err := s.pluginSub.Manager().EmitPluginEvent(ctx, plugin, pluginsdk.EmitEvent{
		Stream:    natsSubject, // already a dot-style events.<gid>.scene.<id>.ic subject; passed through unchanged
		Type:      pluginsdk.EventType(eventType),
		Payload:   payloadJSON,
		Sensitive: sensitive,
	})
	require.NoError(s.t, err, "integrationtest.Server.EmitPluginEvent: Manager.EmitPluginEvent")

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

// configureReadback wires the read-back decryptor (link 4): the REAL
// SessionBridgeGuard (DEK-participant + plugin-manifest lookups + ABAC engine +
// backpressure) plus the audit-chain emitter, threaded into the Manager so the
// DecryptOwnAuditRows host RPC (and the harness's ReadBackOwnRows helper)
// recover plaintext from a plugin's own encrypted audit rows and emit the INV-19
// read-back audit record. Mirrors cmd/holomush/sub_grpc.go:347-366,478-485.
// Pure construction — takes no context (all New* calls below are synchronous).
func (s *Server) configureReadback(pc *pluginCrypto) {
	mgr := s.pluginSub.Manager()

	auditEm, err := authguardaudit.NewQueuedEmitter(pc.publisher, authguardaudit.WithGameID(s.bus.Bus.GameID()))
	require.NoError(s.t, err, "integrationtest.configureReadback: NewQueuedEmitter")
	s.readbackAuditEm = auditEm
	sessionBridgeEm, err := authguardaudit.NewSessionBridgeEmitter(auditEm)
	require.NoError(s.t, err, "integrationtest.configureReadback: NewSessionBridgeEmitter")
	// Wrap so ReadBackAuditCount observes the emission synchronously (see
	// countingAuditEmitter). The guard's BackpressureChecker still uses the raw
	// auditEm (throttle state lives there); only the read-back audit-emit seam
	// fed to the decryptor is wrapped.
	countingEm := countingAuditEmitter{inner: sessionBridgeEm, n: &s.readbackAuditCount}

	guard, err := authguard.New(
		authguard.NewDEKParticipantLookup(pc.dekMgr),
		authguard.NewPluginManifestLookup(mgr),
		s.accessEngine, // ABACEngine — never invoked on the plugin-readback path
		auditEm,        // BackpressureChecker — fresh QueuedEmitter ⇒ no throttle
	)
	require.NoError(s.t, err, "integrationtest.configureReadback: authguard.New")

	src := managerSourceForHarness{mgr: mgr}
	decryptor := history.NewReadbackDecryptor(
		cryptowiring.OwnerMapFromManager(src),
		cryptowiring.AlwaysSensitiveSet(src),
		cryptowiring.CryptoKeysLookup(s.pool),
		authguard.NewSessionBridgeGuard(guard),
		pc.dekMgr,
		countingEm,
	)
	// Wire into the Manager so the binary plugin's DecryptOwnAuditRows host RPC
	// uses it (production parity), AND retain the concrete decryptor so the
	// harness's ReadBackOwnRows helper can drive it directly without the RPC.
	mgr.ConfigureReadbackDecryptor(decryptor)
	s.readbackDecryptor = decryptor
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
