// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// Plugin crash resilience specs — exercises spec §8 "Plugin process crash
// mid-deliver -> Restart drains; PK ON CONFLICT prevents dups". A plugin
// audit client is scripted to return an error on its first AuditEvent call,
// then succeed on subsequent calls. The per-plugin JetStream consumer MUST
// redeliver after AckWait expires; the plugin's idempotent INSERT (ON CONFLICT
// DO NOTHING on the id PK) MUST then absorb the redelivered message without
// producing a duplicate row.
//
// This uses a fake PluginAuditClient that simulates a crash via error return
// rather than actually killing a plugin subprocess — that's what the
// ack/redelivery contract tests, and what the plugin's PK asserts it handles.
var _ = Describe("Plugin crash resilience", func() {
	It("does not duplicate rows under redelivery after a failed first dispatch", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		bus := freshBus()
		pool := freshPool()

		// scene_log schema. Matches plugins/core-scenes/migrations/000004 +
		// 000005 (Phase 7 dek_ref + dek_version columns).
		ensurePluginSchema(ctx, suiteT, pool, "plugin_core_scenes", `
			CREATE TABLE IF NOT EXISTS plugin_core_scenes.scene_log (
				id          BYTEA PRIMARY KEY,
				subject     TEXT NOT NULL,
				type        TEXT NOT NULL,
				timestamp   TIMESTAMPTZ NOT NULL,
				actor_kind  TEXT NOT NULL,
				actor_id    BYTEA,
				payload     BYTEA NOT NULL,
				schema_ver  SMALLINT NOT NULL,
				codec       TEXT NOT NULL,
				js_seq      BIGINT,
				dek_ref     BIGINT,
				dek_version INTEGER,
				inserted_at TIMESTAMPTZ NOT NULL DEFAULT now()
			);
		`)

		owners, err := audit.NewOwnerMap([]audit.SubjectOwner{
			{PluginName: "core-scenes", Pattern: "events.*.scene.>"},
		})
		Expect(err).NotTo(HaveOccurred())

		// The failFirstClient returns an error on the first N calls so the
		// JetStream consumer is forced to redeliver. After that it delegates
		// to the real INSERT client.
		inner := &pgSceneLogClient{pool: pool}
		unstable := &failFirstClient{inner: inner, failN: 1}

		pluginMgr := audit.NewPluginConsumerManager(bus.JS)
		Expect(pluginMgr.Add(ctx, audit.PluginConsumerConfig{
			PluginName:    "core-scenes",
			Subjects:      []string{"events.*.scene.>"},
			Client:        unstable,
			AckWait:       100 * time.Millisecond, // aggressive so redelivery fires fast
			MaxAckPending: 32,
			MaxDeliver:    5, // plenty of room to redeliver a couple of times
		})).To(Succeed())

		hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{
			Owners: owners,
		})
		hostSub.SetLateInitProvider(func() (*audit.OwnerMap, *audit.PluginConsumerManager) {
			return owners, pluginMgr
		})
		Expect(hostSub.Prepare(ctx)).To(Succeed())
		Expect(hostSub.Activate(ctx)).To(Succeed())
		DeferCleanup(func() { _ = hostSub.Stop(context.Background()) })

		// Publish one plugin-owned event.
		pub := bus.Bus.Publisher()
		evt := mintEvent("events.main.scene.01ABC.ic", "scene.pose", `{"n":1}`)
		Expect(pub.Publish(ctx, evt)).To(Succeed())

		// Wait until scene_log has the row (implies redelivery succeeded).
		Eventually(func() bool {
			return countRows(ctx, pool, "plugin_core_scenes.scene_log",
				"id = '\\x"+bytesToHex(evt.ID.Bytes())+"'") == 1
		}, 5*time.Second, 20*time.Millisecond).Should(BeTrue(),
			"plugin must persist the redelivered event exactly once")

		// There must be exactly one row even though the first dispatch failed.
		Expect(countRows(ctx, pool, "plugin_core_scenes.scene_log", "")).To(Equal(1),
			"ON CONFLICT DO NOTHING must prevent duplicate rows under redelivery")

		// And the client saw at least 2 calls (initial failure + successful
		// redelivery).
		Expect(unstable.calls()).To(BeNumerically(">=", int32(2)),
			"expected at least one failing call plus a successful retry")
	})
})

// failFirstClient is a PluginAuditClient that returns an error for its
// first failN calls, then delegates to inner. Counts attempts so the test
// can assert that redelivery actually fired.
type failFirstClient struct {
	mu       sync.Mutex
	inner    *pgSceneLogClient
	failN    int32
	attempts atomic.Int32
}

func (f *failFirstClient) AuditEvent(ctx context.Context, req *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error) {
	n := f.attempts.Add(1)
	if n <= f.failN {
		//nolint:wrapcheck // test dispatcher; error goes to JS redelivery
		return nil, errPluginEnvelope("simulated plugin crash")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	//nolint:wrapcheck // passthrough
	return f.inner.AuditEvent(ctx, req)
}

func (f *failFirstClient) calls() int32 { return f.attempts.Load() }

// bytesToHex renders the BYTEA-escape hex literal PostgreSQL expects for
// an id comparison in a WHERE clause. Pulled out for readability.
func bytesToHex(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, v := range b {
		out = append(out, hex[v>>4], hex[v&0x0f])
	}
	return string(out)
}

var _ eventbus.Event // keep eventbus import in scope even if helpers move
