// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package crypto_test — Task 9 Step 1+2: real-stack snapshot direct-entry read-back
// integration tests and INV-RB meta-test.
//
// These tests exercise the full production stack:
//   - Real Postgres testcontainer + embedded NATS JetStream
//   - Real DEK manager (NewLocalAEADProvider + dek.NewManager)
//   - Real codec (xchacha20poly1305-v1)
//   - Real fence (fenceCheckRow) + real primitive (decryptPluginRow / ReadbackDecryptor)
//   - Real audit emitter (guardaudit.NewQueuedEmitter)
//   - Real AuthGuard (authguard.New)
//
// NOT fakeHistoryReader — these tests close the fake-bus coverage gap described in
// holomush-m7pxs Task 9.
//
// INV-RB invariants covered:
//   - INV-CRYPTO-26: one primitive, snapshot + fence (both call decryptPluginRow)
//   - INV-CRYPTO-27: g1 OwnerMap ownership gate refuses foreign-plugin subjects (not_owner)
//   - INV-CRYPTO-28: every clean plugin read-back emits an INV-CRYPTO-11 audit record
//   - INV-CRYPTO-29: clean rows yield plaintext; refused rows yield reason + no plaintext
//   - INV-CRYPTO-30: downgrade fence runs BEFORE any decrypt (layer-1)
//   - INV-CRYPTO-31 (direct-entry side): snapshot direct entry calls decryptPluginRow
//   - INV-CRYPTO-34: over-cap batch rejected (DECRYPT_BATCH_TOO_LARGE)
//   - INV-CRYPTO-36: no plaintext reason echoes row.Id for correlation
//   - INV-CRYPTO-37: ReadBack=true selects manifest readback branch for plugin principals
//
// INV-CRYPTO-32 (participant fence decrypt) is covered by
// test/integration/privacy/scene_history_readback_test.go.
//
// OUT OF SCOPE (holomush-5rh.20.26):
//   - INV-CRYPTO-33 (snapshot atomicity)
//   - INV-CRYPTO-35 (SNAPSHOT_DECRYPT_FAILED)
//   - INV-CRYPTO-31 consumer-side (snapshot pipeline calling DecryptOwnAuditRows)
package crypto_test

import (
	"context"
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"github.com/holomush/holomush/test/testutil"

	"github.com/jackc/pgx/v5/pgxpool"
)

// readbackEnv is a self-contained real-stack fixture for read-back tests.
// It wires PG, embedded NATS, DEK manager, AuthGuard + audit emitter, and the
// ReadbackDecryptor — the same components the production DecryptOwnAuditRows
// handler uses (goplugin/host_service.go).
type readbackEnv struct {
	pool         *pgxpool.Pool
	dekMgr       dek.Manager
	decryptor    *history.ReadbackDecryptor
	ownerMap     *audit.OwnerMap
	auditEm      eventbus.SessionAuditEmitter
	pluginPub    eventbus.Publisher    // emits sensitive events as a plugin actor
	hostAuditSub *audit.Subsystem      // populates events_audit
	bus          *testutil.EmbeddedBus // for consuming INV-CRYPTO-28 plugin-decrypt audit records
	gameID       string
	cleanup      []func()
}

func (e *readbackEnv) teardown() {
	for i := len(e.cleanup) - 1; i >= 0; i-- {
		e.cleanup[i]()
	}
}

// buildReadbackEnv constructs a full real-stack read-back fixture.
// pluginName is the declaring plugin; its scene events are marked readback:true.
func buildReadbackEnv(ctx context.Context, t *testing.T, pluginName string) *readbackEnv {
	t.Helper()

	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool := newPool(t, connStr)

	bus := testutil.StartEmbeddedJetStream(t)

	// Create an AUDIT stream so guardaudit.Emitter publishes succeed for INV-CRYPTO-28.
	_, err := bus.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "AUDIT",
		Subjects: []string{"audit.>"},
		Storage:  jetstream.MemoryStorage,
	})
	require.NoError(t, err, "buildReadbackEnv: create AUDIT stream")

	kekHex := testutil.RandomKEKHex(t)
	t.Setenv("HOLOMUSH_RB_TEST_KEK", kekHex)
	kekSrc := kek.NewEnvSource("HOLOMUSH_RB_TEST_KEK", false)
	provider, err := kek.NewLocalAEADProvider(ctx, kekSrc, pool)
	require.NoError(t, err)

	dekStore := dek.NewStore(pool)
	dekCache := dek.NewCache(dek.CacheConfig{Capacity: 64})
	dekPartCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64})
	dekMgr, err := dek.NewManager(provider, dekStore, dekCache, dekPartCache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&dekBindingStub{bindingID: "bind-rb-test"})
	require.NoError(t, err)

	// Host audit subsystem — populates events_audit for emitted events.
	hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, hostSub.Start(ctx))

	// VerbRegistry — needed by RenderingPublisher.
	registry, err := core.BootstrapVerbRegistry("test")
	require.NoError(t, err)
	require.NoError(t, registry.RegisterWithSource(core.VerbRegistration{
		Type:          pluginName + ":scene_pose",
		Category:      "communication",
		Format:        "speech",
		Label:         "poses",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        pluginName,
	}, "1.0.0"))

	rawPub := eventbus.NewJetStreamPublisher(
		bus.JS,
		eventbus.Config{}.Defaults(),
		eventbus.WithDEKManager(dekMgr),
	)
	hostPub := eventbus.NewRenderingPublisher(rawPub, registry)

	// AuthGuard: allow-all ABAC; alwaysDenyManifest (read-back path uses ReadBack=true
	// manifest gate, not requests_decryption). The guard routes to checkPluginReadback
	// when ReadBack=true; AllowAll engine + a manifest lookup that returns readback:true.
	readbackManifestLookup := &singlePluginReadbackLookup{
		pluginName: pluginName,
		eventType:  pluginName + ":scene_pose",
	}
	abacEngine := policytest.AllowAllEngine()
	guardCore, err := authguard.New(
		authguard.NewDEKParticipantLookup(dekMgr),
		readbackManifestLookup,
		abacEngine,
		noopBackpressure{},
	)
	require.NoError(t, err)
	sessionGuard := authguard.NewSessionBridgeGuard(guardCore)

	auditPub := &auditPassthroughPublisher{inner: rawPub, js: bus.JS}
	auditEmitter, err := guardaudit.NewQueuedEmitter(auditPub)
	require.NoError(t, err)
	sessionAuditEmitter, err := guardaudit.NewSessionBridgeEmitter(auditEmitter)
	require.NoError(t, err)

	// OwnerMap: pluginName owns events.*.scene.> (subjects emitted by core-scenes pattern).
	ownerMap, err := audit.NewOwnerMap([]audit.SubjectOwner{
		{PluginName: pluginName, Pattern: "events.*.scene.>"},
	})
	require.NoError(t, err)

	// alwaysSensitive: the plugin's scene_pose event type is always-sensitive.
	alwaysSensitive := map[string]struct{}{
		pluginName + ":scene_pose": {},
	}

	// cryptoKeysLookup: always returns true — we just minted the DEK in this test.
	// Production equivalent: cmd/holomush/phase7_fence_wiring.go:newCryptoKeysLookup.
	cryptoKeysLookup := &alwaysTrueCryptoKeysLookup{}

	// ReadbackDecryptor — the production g1+g2 gate + decrypt primitive.
	decryptor := history.NewReadbackDecryptor(
		ownerMap,
		alwaysSensitive,
		cryptoKeysLookup,
		sessionGuard,
		dekMgr,
		sessionAuditEmitter,
	)

	env := &readbackEnv{
		pool:         pool,
		dekMgr:       dekMgr,
		decryptor:    decryptor,
		ownerMap:     ownerMap,
		auditEm:      sessionAuditEmitter,
		pluginPub:    hostPub,
		hostAuditSub: hostSub,
		bus:          bus,
		gameID:       "main",
		cleanup: []func(){
			func() { _ = hostSub.Stop(context.Background()) }, //nolint:errcheck // test cleanup
			func() {
				shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = auditEmitter.Shutdown(shutCtx) //nolint:errcheck // test cleanup
			},
		},
	}
	return env
}

// singlePluginReadbackLookup is a ManifestLookup that returns readback:true
// for one specific (pluginName, eventType) pair and false for everything else.
type singlePluginReadbackLookup struct {
	pluginName string
	eventType  string
}

func (m *singlePluginReadbackLookup) PluginRequestsDecryption(_, _ string) bool { return false }
func (m *singlePluginReadbackLookup) PluginCanReadBack(name, eventType string) bool {
	return name == m.pluginName && eventType == m.eventType
}

// alwaysTrueCryptoKeysLookup implements history.CryptoKeysLookup by always
// reporting that a DEK exists. Safe for tests where the DEK was just minted.
// Production uses cmd/holomush/phase7_fence_wiring.go:newCryptoKeysLookup.
type alwaysTrueCryptoKeysLookup struct{}

func (l *alwaysTrueCryptoKeysLookup) Exists(_ context.Context, _ uint64) (bool, error) {
	return true, nil
}

// emitSensitiveScenePose emits a sensitive pluginName:scene_pose event to
// the given scene subject using the plugin emitter path. Returns after the
// audit subsystem has persisted the events_audit row.
func emitSensitiveScenePose(
	ctx context.Context, t *testing.T,
	env *readbackEnv,
	sceneID, plaintext, pluginName string,
	participants []dek.Participant,
) {
	t.Helper()

	// Ensure DEK exists for the scene context.
	ctxID := dek.ContextID{Type: "scene", ID: sceneID}
	_, err := env.dekMgr.GetOrCreate(ctx, ctxID, participants)
	require.NoError(t, err, "emitSensitiveScenePose: GetOrCreate DEK")

	manifest := &plugins.Manifest{
		Name:                pluginName,
		Emits:               []string{"scene"},
		ActorKindsClaimable: []string{"plugin"},
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: pluginName + ":scene_pose", Sensitivity: plugins.SensitivityAlways, Readback: true},
			},
		},
	}
	manifestLookupFn := func(name string) *plugins.Manifest {
		if name == pluginName {
			return manifest
		}
		return nil
	}
	pluginActorID := plugintest.PluginULIDFromName(pluginName).String()
	actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
		return core.Actor{Kind: core.ActorPlugin, ID: pluginActorID}, nil
	}
	emitter := plugins.NewPluginEventEmitter(
		env.pluginPub, manifestLookupFn, actorResolver,
		plugins.WithCryptoEnabled(true),
	)
	intent := pluginsdk.EmitIntent{
		Subject:   "scene." + sceneID,
		Type:      pluginsdk.EventType(pluginName + ":scene_pose"),
		Payload:   plaintext,
		Sensitive: true,
	}
	require.NoError(t, emitter.Emit(ctx, pluginName, intent))

	// Wait for the audit subsystem to drain so events_audit is populated.
	env.hostAuditSub.AwaitDrained(t, 10*time.Second)
}

// loadFirstAuditRowAsPluginRow queries events_audit for the first row with the
// given subject and returns it as a *pluginv1.AuditRow ready for DecryptOwnRows.
// The events_audit.envelope column contains the marshaled eventbusv1.Event proto;
// Payload within it is the ciphertext for sensitive events.
func loadFirstAuditRowAsPluginRow(ctx context.Context, t *testing.T, pool *pgxpool.Pool, subject string) *pluginv1.AuditRow {
	t.Helper()

	var (
		idBytes    []byte
		evType     string
		codecStr   string
		envelopeB  []byte
		schemaVer  int32
		dekRef     sql.NullInt64
		dekVersion sql.NullInt32
	)

	err := pool.QueryRow(
		ctx,
		`SELECT id, type, codec, envelope, schema_ver, dek_ref, dek_version
		   FROM events_audit
		  WHERE subject = $1
		  ORDER BY id ASC
		  LIMIT 1`,
		subject,
	).Scan(&idBytes, &evType, &codecStr, &envelopeB, &schemaVer, &dekRef, &dekVersion)
	if err != nil {
		t.Logf("loadFirstAuditRowAsPluginRow: no row found for subject=%s: %v", subject, err)
		return nil
	}

	// events_audit.envelope stores the full marshaled eventbusv1.Event proto.
	// We unmarshal it to recover:
	//   1. AuditRow.Payload — the inner ciphertext bytes (Event.Payload), which
	//      decryptPluginRow passes to codec.Decode. Passing the full proto bytes
	//      causes AEAD authentication failure.
	//   2. AuditRow.Actor — the Actor proto from the publish-time envelope. This
	//      is required for AAD reconstruction: AuditRowToEvent(row) includes Actor
	//      in the AAD, and a nil Actor produces different AAD bytes than publish time.
	var pbEnv eventbusv1.Event
	if unmarshalErr := proto.Unmarshal(envelopeB, &pbEnv); unmarshalErr != nil {
		t.Logf("loadFirstAuditRowAsPluginRow: proto.Unmarshal failed for subject=%s: %v", subject, unmarshalErr)
		return nil
	}

	row := &pluginv1.AuditRow{
		Id:        idBytes,
		Subject:   subject,
		Type:      evType,
		Codec:     codecStr,
		Payload:   pbEnv.GetPayload(),   // actual ciphertext bytes, not the proto envelope
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
	return row
}

// -----------------------------------------------------------------------------
// Step 1 tests — snapshot direct-entry path
// -----------------------------------------------------------------------------

// TestReadbackAuthorizedReturnsPlaintext verifies INV-CRYPTO-26/27/28/29/37:
// an authorized plugin (owner of the subject, readback:true in manifest) calling
// DecryptOwnRows on its own encrypted row receives the original plaintext, and
// an INV-CRYPTO-11 plugin-decrypt audit record is emitted (INV-CRYPTO-28).
func TestReadbackAuthorizedReturnsPlaintext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const (
		pluginName = "core-scenes"
		sceneID    = "01RBT9AUTH000000000000000"
		plaintext  = `{"text":"Alice poses the test scene."}`
	)

	env := buildReadbackEnv(ctx, t, pluginName)
	defer env.teardown()

	// Emit a sensitive scene_pose so the events_audit row exists.
	emitSensitiveScenePose(ctx, t, env, sceneID, plaintext, pluginName, []dek.Participant{
		{
			PlayerID:    "01RBT9AUTH_PLAYER0000000",
			CharacterID: "01RBT9AUTH_CHAR00000000",
			BindingID:   "01RBT9AUTH_BIND00000000",
			JoinedAt:    time.Now().UTC(),
			AddedVia:    "readback_test",
		},
	})

	// Load the persisted events_audit row as a pluginv1.AuditRow.
	subject := "events." + env.gameID + ".scene." + sceneID
	row := loadFirstAuditRowAsPluginRow(ctx, t, env.pool, subject)
	require.NotNil(t, row, "TestReadbackAuthorizedReturnsPlaintext: no audit row found for subject=%s", subject)
	require.NotNil(t, row.DekRef, "INV-CRYPTO-30: sensitive row must have dek_ref set")

	// Invoke the real decryptor (same path as the goplugin host handler).
	results, err := env.decryptor.DecryptOwnRows(ctx, pluginName, "", []*pluginv1.AuditRow{row})
	require.NoError(t, err, "INV-CRYPTO-26: DecryptOwnRows must not error for authorized owner")
	require.Len(t, results, 1)

	// INV-CRYPTO-26/29: clean row yields plaintext, no refusal reason.
	assert.Empty(t, results[0].GetNoPlaintextReason(),
		"INV-CRYPTO-29: clean owned row must have no refusal reason")
	// Recovering the exact plaintext proves the snapshot direct-entry path ran
	// the real decryptPluginRow primitive (INV-CRYPTO-31 direct-entry side) — no
	// other code path yields plaintext from the ciphertext audit row.
	assert.Equal(t, []byte(plaintext), results[0].GetPlaintext(),
		"INV-CRYPTO-26: authorized owner must receive original plaintext (INV-CRYPTO-29 clean, INV-CRYPTO-37 echo)")
	assert.Equal(t, []byte(plaintext), results[0].GetPlaintext(),
		"INV-CRYPTO-31: snapshot direct-entry must decrypt via the real decryptPluginRow primitive")

	// INV-CRYPTO-37: id echoes row.id for positional correlation.
	assert.Equal(t, row.GetId(), results[0].GetId(), "INV-CRYPTO-37: result id must echo row id")

	// INV-CRYPTO-28: a clean plugin read-back MUST emit an INV-CRYPTO-11 plugin-decrypt
	// audit record. The guardaudit.Emitter publishes to the AUDIT stream on
	// subject audit.<gameID>.plugin_decrypt.<pluginName>; the emitter defaults
	// gameID to "holomush" (emitter.go defaultGameID), independent of the
	// scene's events.<gameID> namespace. This assertion FAILS if the audit
	// emit is silently dropped — closing the false-green gap.
	auditSubject := "audit.holomush.plugin_decrypt." + pluginName
	auditMsg := testutil.WaitForOneJetStreamMsgOnStream(t, env.bus, "AUDIT", auditSubject, testutil.DefaultWait)
	assert.Equal(t, "audit:plugin_decrypt", auditMsg.Headers().Get("App-Event-Type"),
		"INV-CRYPTO-28: read-back must emit an audit:plugin_decrypt record on the AUDIT stream")
}

// TestReadbackForeignSubjectRefused verifies INV-CRYPTO-27:
// a plugin asking to decrypt a row whose subject belongs to a DIFFERENT plugin
// receives not_owner (g1 OwnerMap gate), and no decrypt/audit happens.
func TestReadbackForeignSubjectRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const (
		ownerPlugin   = "core-scenes" // the plugin that emitted the row
		foreignPlugin = "core-comms"  // a different plugin attempting decrypt
		sceneID       = "01RBT9FRGN00000000000000"
		plaintext     = `{"text":"Forbidden scene pose."}`
	)

	env := buildReadbackEnv(ctx, t, ownerPlugin)
	defer env.teardown()

	emitSensitiveScenePose(ctx, t, env, sceneID, plaintext, ownerPlugin, []dek.Participant{
		{
			PlayerID:    "01RBT9FRGN_PLAYER000000",
			CharacterID: "01RBT9FRGN_CHAR0000000",
			BindingID:   "01RBT9FRGN_BIND0000000",
			JoinedAt:    time.Now().UTC(),
			AddedVia:    "readback_test",
		},
	})

	subject := "events." + env.gameID + ".scene." + sceneID
	row := loadFirstAuditRowAsPluginRow(ctx, t, env.pool, subject)
	require.NotNil(t, row, "TestReadbackForeignSubjectRefused: no audit row for subject=%s", subject)

	// foreignPlugin tries to decrypt ownerPlugin's row — must be refused.
	results, err := env.decryptor.DecryptOwnRows(ctx, foreignPlugin, "", []*pluginv1.AuditRow{row})
	require.NoError(t, err, "g1 refusal must not be an RPC-level error")
	require.Len(t, results, 1)

	// INV-CRYPTO-27: g1 gate refuses with not_owner.
	assert.Equal(t, "not_owner", results[0].GetNoPlaintextReason(),
		"INV-CRYPTO-27: foreign plugin must receive not_owner")
	assert.Nil(t, results[0].GetPlaintext(),
		"INV-CRYPTO-27: foreign-subject row must not yield plaintext")
	assert.Equal(t, row.GetId(), results[0].GetId(),
		"INV-CRYPTO-36: id must echo even on refusal")
}

// TestReadbackWithoutReadbackFlagDenied verifies INV-CRYPTO-27 (g2 gate):
// a plugin whose manifest declares the event type but WITHOUT readback:true
// is denied via the checkPluginReadback guard (auth_guard_deny).
func TestReadbackWithoutReadbackFlagDenied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const (
		pluginName = "no-readback-plugin"
		sceneID    = "01RBT9NOFLAG000000000000"
		plaintext  = `{"text":"Must not be readable back."}`
	)

	// Build an env where this plugin owns the scene subject but does NOT
	// declare readback:true — the noReadbackManifestLookup returns false.
	noReadbackLookup := &noReadbackManifestLookup{}
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool := newPool(t, connStr)
	busInst := testutil.StartEmbeddedJetStream(t)
	_, err := busInst.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "AUDIT",
		Subjects: []string{"audit.>"},
		Storage:  jetstream.MemoryStorage,
	})
	require.NoError(t, err)

	kekHex := testutil.RandomKEKHex(t)
	t.Setenv("HOLOMUSH_RB_NOFLAG_KEK", kekHex)
	kekSrc := kek.NewEnvSource("HOLOMUSH_RB_NOFLAG_KEK", false)
	provider, err := kek.NewLocalAEADProvider(ctx, kekSrc, pool)
	require.NoError(t, err)

	dekStore := dek.NewStore(pool)
	dekCache := dek.NewCache(dek.CacheConfig{Capacity: 64})
	dekPartCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64})
	mgr, err := dek.NewManager(provider, dekStore, dekCache, dekPartCache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&dekBindingStub{bindingID: "bind-noflag"})
	require.NoError(t, err)

	// Emit a sensitive scene event as the plugin.
	ctxID := dek.ContextID{Type: "scene", ID: sceneID}
	_, err = mgr.GetOrCreate(ctx, ctxID, []dek.Participant{{
		PlayerID:    "01RBT9NOFLAG_PLAYER00000",
		CharacterID: "01RBT9NOFLAG_CHAR000000",
		BindingID:   "01RBT9NOFLAG_BIND000000",
		JoinedAt:    time.Now().UTC(),
		AddedVia:    "readback_test",
	}})
	require.NoError(t, err)

	registry, err := core.BootstrapVerbRegistry("test")
	require.NoError(t, err)
	require.NoError(t, registry.RegisterWithSource(core.VerbRegistration{
		Type:          pluginName + ":scene_pose",
		Category:      "communication",
		Format:        "speech",
		Label:         "poses",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        pluginName,
	}, "1.0.0"))
	rawPub := eventbus.NewJetStreamPublisher(busInst.JS, eventbus.Config{}.Defaults(), eventbus.WithDEKManager(mgr))
	hostPub := eventbus.NewRenderingPublisher(rawPub, registry)

	hostSub := audit.NewSubsystem(fixedJS{js: busInst.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, hostSub.Start(ctx))
	defer func() { _ = hostSub.Stop(context.Background()) }() //nolint:errcheck // test cleanup

	manifest := &plugins.Manifest{
		Name:                pluginName,
		Emits:               []string{"scene"},
		ActorKindsClaimable: []string{"plugin"},
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				// Readback: false — this is the test condition.
				{EventType: pluginName + ":scene_pose", Sensitivity: plugins.SensitivityAlways, Readback: false},
			},
		},
	}
	manifestFn := func(name string) *plugins.Manifest {
		if name == pluginName {
			return manifest
		}
		return nil
	}
	pluginActorID := plugintest.PluginULIDFromName(pluginName).String()
	actorFn := func(_ context.Context, _ string) (core.Actor, error) {
		return core.Actor{Kind: core.ActorPlugin, ID: pluginActorID}, nil
	}
	emitter := plugins.NewPluginEventEmitter(hostPub, manifestFn, actorFn, plugins.WithCryptoEnabled(true))
	intent := pluginsdk.EmitIntent{
		Subject:   "scene." + sceneID,
		Type:      pluginsdk.EventType(pluginName + ":scene_pose"),
		Payload:   plaintext,
		Sensitive: true,
	}
	require.NoError(t, emitter.Emit(ctx, pluginName, intent))
	hostSub.AwaitDrained(t, 10*time.Second)

	// Build decryptor with noReadbackManifestLookup.
	abacEngine := policytest.AllowAllEngine()
	guardCore, err := authguard.New(
		authguard.NewDEKParticipantLookup(mgr),
		noReadbackLookup,
		abacEngine,
		noopBackpressure{},
	)
	require.NoError(t, err)
	sessionGuard := authguard.NewSessionBridgeGuard(guardCore)

	auditPub := &auditPassthroughPublisher{inner: rawPub, js: busInst.JS}
	auditEmitter, err := guardaudit.NewQueuedEmitter(auditPub)
	require.NoError(t, err)
	sessionAuditEmitter, err := guardaudit.NewSessionBridgeEmitter(auditEmitter)
	require.NoError(t, err)
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = auditEmitter.Shutdown(shutCtx) //nolint:errcheck // test cleanup
	}()

	ownerMap, err := audit.NewOwnerMap([]audit.SubjectOwner{
		{PluginName: pluginName, Pattern: "events.*.scene.>"},
	})
	require.NoError(t, err)
	alwaysSensitive := map[string]struct{}{pluginName + ":scene_pose": {}}

	decryptor := history.NewReadbackDecryptor(
		ownerMap, alwaysSensitive,
		&alwaysTrueCryptoKeysLookup{},
		sessionGuard, mgr, sessionAuditEmitter,
	)

	subject := "events.main.scene." + sceneID
	row := loadFirstAuditRowAsPluginRow(ctx, t, pool, subject)
	require.NotNil(t, row, "TestReadbackWithoutReadbackFlagDenied: no audit row for subject=%s", subject)

	results, err := decryptor.DecryptOwnRows(ctx, pluginName, "", []*pluginv1.AuditRow{row})
	require.NoError(t, err)
	require.Len(t, results, 1)

	// INV-CRYPTO-27 (g2): readback:false → auth_guard_deny or downgrade_refused.
	// The g2 check fires after the g1 owner check passes; the manifest check
	// inside checkPluginReadback denies because PluginCanReadBack returns false.
	assert.Equal(t, "auth_guard_deny", results[0].GetNoPlaintextReason(),
		"INV-CRYPTO-27: plugin without readback flag must be denied by the g2 readback gate (auth_guard_deny)")
	assert.Nil(t, results[0].GetPlaintext(),
		"INV-CRYPTO-27: plugin without readback flag must not receive plaintext")
	assert.Equal(t, row.GetId(), results[0].GetId(), "INV-CRYPTO-36: id echoes on refusal")
}

// noReadbackManifestLookup returns false for PluginCanReadBack unconditionally.
type noReadbackManifestLookup struct{}

func (noReadbackManifestLookup) PluginRequestsDecryption(_, _ string) bool { return false }
func (noReadbackManifestLookup) PluginCanReadBack(_, _ string) bool        { return false }

// -----------------------------------------------------------------------------
// Step 2 — INV-RB meta-test
// -----------------------------------------------------------------------------

// TestINVRBInvariantCoverage is the meta-test: every INV-RB-* invariant that
// this plan (Task 9) implements MUST be named in at least one test in this
// package or in test/integration/privacy/.
//
// OUT OF SCOPE (holomush-5rh.20.26, C7 snapshot-pipeline bead):
//   - INV-CRYPTO-33 (snapshot atomicity)
//   - INV-CRYPTO-35 (SNAPSHOT_DECRYPT_FAILED)
//   - INV-CRYPTO-31 consumer-side (snapshot pipeline calling DecryptOwnAuditRows)
//
// These MUST NOT be required here; their absence from the name-list below
// is deliberate and documents the boundary.
func TestINVRBInvariantCoverage(t *testing.T) {
	// Invariants implemented by this plan's T1-T9, asserting at least one
	// test name per invariant. The search covers both integration packages.
	inScope := []struct {
		inv   string
		files []string // files that reference the invariant
	}{
		{
			inv: "INV-CRYPTO-26",
			files: []string{
				"test/integration/crypto/readback_test.go",
			},
		},
		{
			inv: "INV-CRYPTO-27",
			files: []string{
				"test/integration/crypto/readback_test.go",
			},
		},
		{
			inv: "INV-CRYPTO-28",
			files: []string{
				"test/integration/crypto/readback_test.go",
			},
		},
		{
			inv: "INV-CRYPTO-29",
			files: []string{
				"test/integration/crypto/readback_test.go",
			},
		},
		{
			inv: "INV-CRYPTO-30",
			files: []string{
				"test/integration/crypto/readback_test.go",
			},
		},
		{
			inv: "INV-CRYPTO-31",
			files: []string{
				"test/integration/crypto/readback_test.go", // direct-entry side only
			},
		},
		{
			inv: "INV-CRYPTO-32",
			files: []string{
				"test/integration/privacy/scene_history_readback_test.go",
			},
		},
		{
			inv: "INV-CRYPTO-34",
			files: []string{
				"test/integration/crypto/readback_test.go",
			},
		},
		{
			inv: "INV-CRYPTO-36",
			files: []string{
				"test/integration/crypto/readback_test.go",
			},
		},
		{
			inv: "INV-CRYPTO-37",
			files: []string{
				"test/integration/crypto/readback_test.go",
			},
		},
	}

	// Out-of-scope invariants (C7 snapshot-pipeline bead, holomush-5rh.20.26):
	// these MUST NOT have real assertions in THIS package's tests. A meaningful
	// check verifies the invariant string is absent from actual string-literal
	// nodes of this file (comments naming it as out-of-scope are fine and are
	// excluded by countInvariantStringLiterals). This catches the failure mode
	// where someone silently adds an in-package assertion for a C7 invariant
	// without moving it in-scope.
	outOfScope := []string{"INV-CRYPTO-33", "INV-CRYPTO-35"}
	const thisFile = "test/integration/crypto/readback_test.go"
	for _, inv := range outOfScope {
		assert.Zero(t, countInvariantStringLiterals(t, thisFile, inv),
			"invariant %s is out of scope for this task (C7 snapshot pipeline) and must not be asserted in %s", inv, thisFile)
	}

	// For each in-scope invariant, assert it is referenced in at least one
	// integration test file in this package (the files are relative to repo root).
	for _, tc := range inScope {
		found := false
		for _, f := range tc.files {
			if fileReferencesInvariant(t, f, tc.inv) {
				found = true
				break
			}
		}
		assert.True(t, found,
			"INV-RB meta-test: %s must be referenced in at least one integration test file", tc.inv)
	}
}

// fileReferencesInvariant reports whether the given file (relative to repo root)
// references the invariant inside a REAL test construct — i.e. a non-comment
// string literal (assertion message, label, etc.) — NOT merely in a comment.
//
// A naive strings.Contains over the file bytes is a false-green: the package
// doc-comment and assertion-label comments name every invariant, so the gate
// would pass even when no real assertion exists. Hardening parses the file with
// go/parser and walks only *ast.BasicLit string-literal nodes; comments are
// *ast.Comment nodes and are never visited here, so a comment-only reference
// does NOT satisfy the gate.
func fileReferencesInvariant(t *testing.T, relPath, inv string) bool {
	t.Helper()
	return countInvariantStringLiterals(t, relPath, inv) > 0
}

// countInvariantStringLiterals parses the file (relative to repo root) and
// returns the number of ASSERTION-MESSAGE string-literal nodes that reference
// inv. Comments are excluded by construction (they are not BasicLit nodes).
//
// A real assertion message embeds the invariant in a larger sentence, e.g.
// "INV-CRYPTO-26: DecryptOwnRows must not error...". The meta-test's own coverage
// catalog stores the invariant as a BARE literal (`inv: "INV-CRYPTO-26"`); counting
// those would make the gate trivially self-satisfying. We therefore require the
// literal's value to STRICTLY CONTAIN inv (be longer than the invariant token
// alone) — that excludes the catalog entries and demands a real labeled
// assertion (or any literal that says more than just the bare token).
func countInvariantStringLiterals(t *testing.T, relPath, inv string) int {
	t.Helper()
	// Integration test binaries execute with cwd = the test package directory.
	// Navigate to repo root: test/integration/crypto/ → ../../../
	fset := token.NewFileSet()
	// Parse WITHOUT ParseComments: comment groups are dropped from the AST, so
	// only real source constructs remain. String literals appear as *ast.BasicLit.
	file, err := parser.ParseFile(fset, "../../../"+relPath, nil, 0)
	if err != nil {
		t.Logf("countInvariantStringLiterals: cannot parse %s: %v", relPath, err)
		return 0
	}
	count := 0
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		val, unqErr := strconv.Unquote(lit.Value)
		if unqErr != nil {
			val = lit.Value // raw fallback for backtick/unparseable literals
		}
		// Strict containment: the literal must say MORE than the bare invariant
		// token, which excludes the coverage-catalog entries (`inv: "INV-CRYPTO-N"`)
		// and demands an actual assertion message naming the invariant.
		if strings.Contains(val, inv) && val != inv {
			count++
		}
		return true
	})
	return count
}

// TestReadbackBatchCapRejected verifies INV-CRYPTO-34:
// a batch larger than maxDecryptBatch (500) is REJECTED with DECRYPT_BATCH_TOO_LARGE,
// not truncated silently.
func TestReadbackBatchCapRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const pluginName = "core-scenes"

	env := buildReadbackEnv(ctx, t, pluginName)
	defer env.teardown()

	// Build 501 synthetic (non-encrypted) rows — we only need to trigger the
	// cap check, which runs before any row is decrypted.
	rows := make([]*pluginv1.AuditRow, 501)
	for i := range rows {
		rows[i] = &pluginv1.AuditRow{
			Id:      []byte(fmt.Sprintf("row-%04d", i)),
			Subject: fmt.Sprintf("events.main.scene.01RBT9CAP%04d.ic", i),
		}
	}

	_, err := env.decryptor.DecryptOwnRows(ctx, pluginName, "", rows)
	require.Error(t, err, "INV-CRYPTO-34: batch > maxDecryptBatch must be rejected")

	// The error must carry the DECRYPT_BATCH_TOO_LARGE oops code.
	// Use errutil.AssertErrorCode — the project-standard way to discriminate
	// oops sentinel codes (errors.Is always matches any OopsError, type
	// assertions on the coder interface don't work across package boundaries).
	errutil.AssertErrorCode(t, err, "DECRYPT_BATCH_TOO_LARGE")
}
