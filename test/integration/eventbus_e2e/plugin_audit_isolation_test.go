// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// Plugin audit isolation specs — exercises spec §8 "Plugin audit isolation":
//
//   - Plugin-owned subjects (events.*.scene.>) MUST NOT appear in the host
//     events_audit table.
//   - They MUST flow through the per-plugin consumer into the plugin's
//     audit schema (plugin_core_scenes.scene_log).
//
// The spec stands up the real host projection (via audit.NewSubsystem)
// wired to an OwnerMap that routes scene subjects to a fake plugin audit
// client. That client persists into plugin_core_scenes.scene_log using
// the same schema the real plugin ships (plugins/core-scenes/migrations/
// 000004_create_scene_log.up.sql).
var _ = Describe("Plugin audit isolation", func() {
	It("routes plugin-owned subjects to plugin schema and not host events_audit", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		bus := freshBus()
		pool := freshPool()

		// Create the plugin's audit schema + scene_log table by hand. In
		// production the schema provisioner runs the plugin's migrations; for
		// this test we inline the DDL so there's no dependency on the plugin
		// loader.
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

		// Build the ownership map — scene subjects belong to core-scenes.
		owners, err := audit.NewOwnerMap([]audit.SubjectOwner{
			{PluginName: "core-scenes", Pattern: "events.*.scene.>"},
		})
		Expect(err).NotTo(HaveOccurred())

		// Host projection: ack-and-skips plugin-owned subjects.
		hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{
			Owners: owners,
		})

		// Per-plugin manager: routes events.*.scene.> into our fake client.
		pluginMgr := audit.NewPluginConsumerManager(bus.JS)
		client := &pgSceneLogClient{pool: pool}
		Expect(pluginMgr.Add(ctx, audit.PluginConsumerConfig{
			PluginName: "core-scenes",
			Subjects:   []string{"events.*.scene.>"},
			Client:     client,
		})).To(Succeed())

		// Wire the manager into the host subsystem so Start/Stop are a unit.
		hostSub.SetLateInitProvider(func() (*audit.OwnerMap, *audit.PluginConsumerManager) {
			return owners, pluginMgr
		})
		Expect(hostSub.Start(ctx)).To(Succeed())
		DeferCleanup(func() { _ = hostSub.Stop(context.Background()) })

		rawPub := bus.Bus.Publisher()
		Expect(rawPub).NotTo(BeNil())

		// RenderingPublisher stamps the App-Rendering header required by the
		// audit projection (events_audit.rendering NOT NULL after migration 000012).
		// location_state is registered as a builtin, so the lookup succeeds.
		registry, err := core.BootstrapVerbRegistry("test")
		Expect(err).NotTo(HaveOccurred())
		hostPub := eventbus.NewRenderingPublisher(rawPub, registry)

		// Publish 3 scene (plugin-owned) events and 2 host-owned events.
		// Scene events use the raw publisher — the OwnerMap routes them to the
		// plugin consumer (ack-and-skip in host projection, no persist() call).
		// Host events use the wrapped publisher so App-Rendering is stamped.
		sceneEvents := []eventbus.Event{
			mintEvent("events.main.scene.01ABC.ic", "scene.pose", `{"n":1}`),
			mintEvent("events.main.scene.01ABC.ic", "scene.pose", `{"n":2}`),
			mintEvent("events.main.scene.01DEF.ic", "scene.pose", `{"n":3}`),
		}
		hostEvents := []eventbus.Event{
			// Use location_state (underscore) — the canonical builtin type registered
			// in BootstrapVerbRegistry, valid per typeRe (no separator needed).
			mintEvent("events.main.loc.01HOST.out", "location_state", `{"h":1}`),
			mintEvent("events.main.loc.01HOST.out", "location_state", `{"h":2}`),
		}
		for _, e := range sceneEvents {
			Expect(rawPub.Publish(ctx, e)).To(Succeed())
		}
		for _, e := range hostEvents {
			Expect(hostPub.Publish(ctx, e)).To(Succeed())
		}

		// Wait for the host projection to drain (host rows + ack-skipped
		// plugin rows both advance its cursor). Plugin consumer drains in
		// parallel; we separately poll scene_log to assert arrival.
		hostSub.AwaitDrained(suiteT, 10*time.Second)
		Eventually(func() bool {
			return countRows(ctx, pool, "plugin_core_scenes.scene_log", "") == len(sceneEvents)
		}, 10*time.Second, 20*time.Millisecond).Should(BeTrue(),
			"plugin scene_log did not see all plugin-owned events")

		// Host audit table MUST NOT contain plugin-owned rows.
		Expect(countRows(ctx, pool, "events_audit", "subject LIKE 'events.main.scene.%'")).To(BeZero(),
			"events_audit must be empty for plugin-owned subjects")

		// Host audit table MUST contain the host-owned rows.
		Expect(countRows(ctx, pool, "events_audit", "subject LIKE 'events.main.loc.%'")).To(Equal(len(hostEvents)),
			"events_audit must hold host-owned rows")

		// Plugin scene_log MUST contain exactly the plugin-owned rows.
		Expect(countRows(ctx, pool, "plugin_core_scenes.scene_log", "")).To(Equal(len(sceneEvents)),
			"plugin scene_log must hold exactly the plugin-owned rows")
	})
})

// countRows counts rows in `table` with optional WHERE clause (pass "" for none).
// Takes the spec's context so the query respects the It's deadline (vs the
// previous context.Background() which sidestepped the spec timeout) and uses a
// Gomega assertion instead of panic so failures surface as proper spec failures.
func countRows(ctx context.Context, pool *pgxpool.Pool, table, where string) int {
	q := "SELECT count(*) FROM " + table
	if where != "" {
		q += " WHERE " + where
	}
	var n int
	Expect(pool.QueryRow(ctx, q).Scan(&n)).To(Succeed())
	return n
}

// fixedJS / fixedPool satisfy the audit.Subsystem provider interfaces
// from already-started resources.
type fixedJS struct{ js jetstream.JetStream }

func (f fixedJS) JS() jetstream.JetStream { return f.js }

type fixedPool struct{ pool *pgxpool.Pool }

func (f fixedPool) Pool() *pgxpool.Pool { return f.pool }

// pgSceneLogClient is a minimal PluginAuditClient that INSERTs into
// plugin_core_scenes.scene_log. It mirrors the real plugin's audit.go
// behavior without pulling the plugin binary into the test harness.
type pgSceneLogClient struct {
	pool *pgxpool.Pool
}

func (c *pgSceneLogClient) AuditEvent(ctx context.Context, req *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error) {
	// Phase 7 (INV-P7-1, INV-EVENTBUS-25): the host dispatcher's buildAuditRow
	// populates Row.{Codec,SchemaVer,DekRef,DekVersion} from the JS
	// headers; the test stub mirrors the real plugin Insert by carrying
	// those fields verbatim into the plugin's scene_log table.
	row := req.GetRow()
	if row == nil {
		return nil, errPluginEnvelope("nil row")
	}
	ts := row.GetTimestamp().AsTime()
	var actorID []byte
	if a := row.GetActor(); a != nil && len(a.GetId()) == 16 {
		actorID = a.GetId()
	}
	schemaVer := int16(1)
	if v := row.GetSchemaVer(); v > 0 && v <= 32767 {
		schemaVer = int16(v) //nolint:gosec // bounded above by the >0 && <=32767 guard
	}
	codecName := row.GetCodec()
	if codecName == "" {
		codecName = "identity"
	}
	var dekRef *int64
	if row.DekRef != nil {
		v := int64(*row.DekRef) //nolint:gosec // dek_ref column is BIGINT (signed) — uint64→int64 widening matches column shape
		dekRef = &v
	}
	var dekVersion *int32
	if row.DekVersion != nil {
		v := int32(*row.DekVersion) //nolint:gosec // dek_version column is INTEGER (signed) — uint32→int32 matches column shape
		dekVersion = &v
	}
	_, err := c.pool.Exec(
		ctx, `
		INSERT INTO plugin_core_scenes.scene_log (
			id, subject, type, timestamp, actor_kind, actor_id,
			payload, schema_ver, codec, js_seq, dek_ref, dek_version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO NOTHING`,
		row.GetId(),
		row.GetSubject(),
		row.GetType(),
		ts,
		actorKindStringFromRow(row.GetActor()),
		actorID,
		row.GetPayload(),
		schemaVer,
		codecName,
		// Plugin dispatch path does not carry the JS seq explicitly; the
		// plugin can use it for its own ordering by spying on headers in
		// a future proto revision. For this test, 0 is acceptable
		// (scene_log.js_seq is nullable).
		int64(0),
		dekRef,
		dekVersion,
	)
	if err != nil {
		//nolint:wrapcheck // test dispatcher — surface raw DB error
		return nil, err
	}
	return &pluginv1.AuditEventResponse{}, nil
}

func actorKindStringFromRow(a *eventbusv1.Actor) string {
	if a == nil {
		return "system"
	}
	switch a.GetKind() {
	case eventbusv1.ActorKind_ACTOR_KIND_CHARACTER:
		return "character"
	case eventbusv1.ActorKind_ACTOR_KIND_PLAYER:
		return "player"
	case eventbusv1.ActorKind_ACTOR_KIND_PLUGIN:
		return "plugin"
	case eventbusv1.ActorKind_ACTOR_KIND_SYSTEM:
		return "system"
	default:
		return "system"
	}
}

type errPluginEnvelope string

func (e errPluginEnvelope) Error() string { return string(e) }
