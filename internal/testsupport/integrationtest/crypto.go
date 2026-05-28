// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/eventbus/subjectxlate"
	"github.com/holomush/holomush/internal/plugin/cryptowiring"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
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

	sel := cryptowiring.KeySelector()
	raw := bus.Bus.Publisher(eventbus.WithDEKManager(dekMgr), eventbus.WithCodecSelector(sel))
	return &pluginCrypto{
		dekMgr:    dekMgr,
		selector:  sel,
		publisher: eventbus.NewRenderingPublisher(raw, verbReg),
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
	legacySubject := dp.Manifest.Emits[0] + ":" + eventType

	err := s.pluginSub.Manager().EmitPluginEvent(ctx, plugin, pluginsdk.EmitEvent{
		Stream:    legacySubject,
		Type:      pluginsdk.EventType(eventType),
		Payload:   payloadJSON,
		Sensitive: sensitive,
	})
	require.NoError(s.t, err, "integrationtest.Server.EmitPluginEvent: Manager.EmitPluginEvent")

	natsSubject, err := subjectxlate.Legacy(legacySubject, s.bus.Bus.GameID())
	require.NoError(s.t, err, "integrationtest.Server.EmitPluginEvent: subjectxlate.Legacy")

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
