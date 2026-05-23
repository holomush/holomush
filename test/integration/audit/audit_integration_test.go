// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package audit_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	// Register pgx stdlib driver for database/sql under the "pgx" name.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/command"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/holomush/holomush/test/testutil"
)

var suiteT *testing.T

func TestAuditIntegration(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Audit Subsystem Integration")
}

var _ = Describe("Plugin-emitted audit events reaching access_audit_log", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		db     *sql.DB
		logger *audit.Logger
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)

		shared := testutil.SharedPostgres(suiteT)
		connStr := testutil.FreshDatabase(suiteT, shared)

		var err error
		db, err = sql.Open("pgx", connStr)
		Expect(err).NotTo(HaveOccurred())

		// access_audit_log is RANGE-partitioned by timestamp; production
		// creates monthly partitions at bootstrap. The integration test
		// must create the current month's partition explicitly so writes
		// from PostgresWriter find a valid child table.
		Expect(ensureAuditPartitionForMonth(ctx, db, time.Now().UTC())).To(Succeed())

		// Real PostgresWriter + Logger — no in-memory capture. The whole
		// point of this suite is to verify the column round-trip works.
		writer := audit.NewPostgresWriter(db)
		logger = audit.NewLogger(audit.ModeAll, writer, "")
	})

	AfterEach(func() {
		// Logger.Close also closes the writer (which closes its async
		// goroutine but does NOT close the underlying *sql.DB).
		if logger != nil {
			_ = logger.Close()
			logger = nil
		}
		if db != nil {
			_ = db.Close()
			db = nil
		}
		if cancel != nil {
			cancel()
		}
	})

	Describe("binary plugin handler emits a deny hint during HandleCommand", func() {
		It("writes a row to access_audit_log with all host-stamped fields populated", func() {
			deliverer := &scriptedDeliverer{
				response: &pluginsdk.CommandResponse{
					Status: pluginsdk.CommandError,
					Output: "denied",
					AuditHints: []pluginsdk.AuditHint{
						{
							ID:              "not_member",
							Name:            "channels: not a member",
							Message:         "player not in channel members",
							Effect:          pluginsdk.AuditEffectDeny,
							ActionQualifier: "speak",
							Resource:        "channel:01XYZ",
							Attributes:      map[string]string{"channel.type": "public"},
						},
					},
				},
			}

			charID := ulid.Make()
			dispatcher := newIntegrationTestDispatcher(deliverer, logger)
			exec := newIntegrationCommandExecution(charID)

			dispatchErr := dispatcher.Dispatch(ctx, "channel hello", exec)
			Expect(dispatchErr).NotTo(HaveOccurred(),
				"CommandError is a user denial, not a dispatch error")

			// Deny entries are sync-written in ModeAll, so the row should
			// be present immediately after Dispatch returns.
			rows := queryAuditRows(ctx, db, "plugin")
			Expect(rows).To(HaveLen(1),
				"expected exactly one plugin-sourced row in access_audit_log")

			row := rows[0]
			// Host-stamped fields — these are the anti-spoofing invariants.
			Expect(row.source).To(Equal("plugin"),
				"Source must be host-stamped as 'plugin'")
			Expect(row.component).To(Equal("test-plugin"),
				"Component must be host-stamped from the plugin name")
			Expect(row.subject).To(Equal(access.CharacterSubject(charID.String())),
				"Subject must be host-stamped from the dispatch context")
			Expect(row.action).To(Equal("channel:speak"),
				"Action must be composed as base:qualifier")
			Expect(row.effect).To(Equal("deny"),
				"Effect must round-trip as 'deny'")

			// Plugin-provided fields — verified round-trip through the DB.
			Expect(row.eventID).To(Equal("not_member"))
			Expect(row.eventName).To(Equal("channels: not a member"))
			Expect(row.message).To(Equal("player not in channel members"))
			Expect(row.resource).To(Equal("channel:01XYZ"))

			// Attributes — verify the plugin-provided key round-trips and
			// the host overlay key was merged in.
			Expect(row.attributes).To(ContainSubstring(`"channel.type"`))
			Expect(row.attributes).To(ContainSubstring(`"public"`))
			Expect(row.attributes).To(ContainSubstring(`"command.invoked_as"`))
		})
	})

	Describe("Lua plugin calls audit.deny during command handler", func() {
		It("writes a row to access_audit_log via the hostfunc capability path", func() {
			// The Lua emit path is verified end-to-end by Task 12's unit
			// tests for the audit hostfunc capability module. Wiring a
			// full Lua plugin into the dispatcher in an integration test
			// requires the plugin manager + manifest loader, which is
			// substantially more scaffolding than the dispatch path
			// itself. Once the Lua plugin harness lands as a reusable
			// helper, this test should mirror the binary path above
			// starting from a Lua script that calls
			//   audit.deny("not_member", "...", {channel_type = "public"}).
			Skip("implement once a reusable Lua plugin integration harness lands; " +
				"the Lua audit emit path is already verified by unit tests for " +
				"the hostfunc.AuditCapability module (Task 12)")
		})
	})

	Describe("binary plugin emits multiple hints during a successful command", func() {
		It("flushes every hint to access_audit_log and the user command still returns normally", func() {
			deliverer := &scriptedDeliverer{
				response: &pluginsdk.CommandResponse{
					Status: pluginsdk.CommandOK,
					AuditHints: []pluginsdk.AuditHint{
						{ID: "e1", Effect: pluginsdk.AuditEffectDeny, Message: "first"},
						{ID: "e2", Effect: pluginsdk.AuditEffectDeny, Message: "second"},
						{ID: "e3", Effect: pluginsdk.AuditEffectDeny, Message: "third"},
					},
				},
			}

			charID := ulid.Make()
			dispatcher := newIntegrationTestDispatcher(deliverer, logger)
			exec := newIntegrationCommandExecution(charID)

			dispatchErr := dispatcher.Dispatch(ctx, "channel hello", exec)
			Expect(dispatchErr).NotTo(HaveOccurred(),
				"audit flush failures must never propagate to the user command")

			// All three deny events should have landed in the DB. Deny
			// effects are sync-written before Dispatch returns.
			rows := queryAuditRows(ctx, db, "plugin")
			Expect(rows).To(HaveLen(3),
				"all three plugin-emitted hints should be flushed to access_audit_log")

			// Sanity-check that the messages are the expected three.
			var messages []string
			for _, r := range rows {
				messages = append(messages, r.message)
			}
			Expect(messages).To(ConsistOf("first", "second", "third"))
		})
	})
})

// auditRow holds the relevant columns from one access_audit_log row.
type auditRow struct {
	source     string
	component  string
	subject    string
	action     string
	resource   string
	effect     string
	eventID    string
	eventName  string
	message    string
	attributes string // JSON as text — sufficient for substring assertions
}

// queryAuditRows returns all rows matching the given source, ordered by
// timestamp ascending.
func queryAuditRows(ctx context.Context, db *sql.DB, source string) []auditRow {
	const query = `
		SELECT source, component, subject, action, resource, effect,
		       event_id, event_name, message, attributes::text
		FROM access_audit_log
		WHERE source = $1
		ORDER BY timestamp ASC
	`
	rows, err := db.QueryContext(ctx, query, source)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = rows.Close() }()

	var results []auditRow
	for rows.Next() {
		var r auditRow
		Expect(rows.Scan(
			&r.source, &r.component, &r.subject, &r.action, &r.resource,
			&r.effect, &r.eventID, &r.eventName, &r.message, &r.attributes,
		)).To(Succeed())
		results = append(results, r)
	}
	Expect(rows.Err()).NotTo(HaveOccurred())
	return results
}

// ensureAuditPartitionForMonth creates the monthly partition that covers t,
// using the same naming convention as audit.PostgresPartitionCreator. The
// statement is idempotent.
//
// All interpolated values are derived from time.Time formatting (year, month,
// day) — none come from external input — so the SQL injection rule does not
// apply here. PostgreSQL's CREATE TABLE syntax does not accept partition
// boundaries via parameter binding, so string interpolation is unavoidable.
func ensureAuditPartitionForMonth(ctx context.Context, db *sql.DB, t time.Time) error {
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	name := fmt.Sprintf("access_audit_log_%04d_%02d", t.Year(), t.Month())
	// access_audit_log.timestamp is BIGINT epoch-ns (post-gfo6 Phase 4).
	// Partition bounds are int64 ns; range_end is exclusive.
	// nosemgrep: go.lang.security.audit.database.string-formatted-query.string-formatted-query -- name and int values are derived from time.Time, never from external input
	stmt := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF access_audit_log FOR VALUES FROM (%d) TO (%d)`,
		name, start.UnixNano(), end.UnixNano(),
	)
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("create audit partition %s: %w", name, err)
	}
	return nil
}

// scriptedDeliverer is a command.PluginCommandDeliverer that returns a
// pre-canned CommandResponse on every Deliver call. Used to drive the
// dispatcher's audit-hint extraction path with deterministic input.
type scriptedDeliverer struct {
	response *pluginsdk.CommandResponse
}

func (d *scriptedDeliverer) DeliverCommand(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return d.response, nil
}

func (d *scriptedDeliverer) EmitPluginEvent(_ context.Context, _ string, _ pluginsdk.EmitEvent) error {
	return nil
}

// newIntegrationTestDispatcher constructs a minimal dispatcher with the given
// deliverer and audit logger wired in. The registry contains a single
// plugin-backed command named "channel" attributed to "test-plugin", which
// the dispatcher will stamp as the audit Component on extracted hints.
func newIntegrationTestDispatcher(deliverer command.PluginCommandDeliverer, logger *audit.Logger) *command.Dispatcher {
	engine := policytest.AllowAllEngine()

	registry := command.NewRegistry()
	entry := command.NewTestEntry(command.CommandEntryConfig{
		Name:       "channel",
		PluginName: "test-plugin",
		Source:     "test-plugin",
	})
	Expect(registry.Register(entry)).To(Succeed())

	dispatcher, err := command.NewDispatcher(
		registry,
		engine,
		command.WithPluginDeliverer(deliverer),
		command.WithAuditLogger(logger),
	)
	Expect(err).NotTo(HaveOccurred())
	return dispatcher
}

// newIntegrationCommandExecution returns a CommandExecution carrying the
// given character ID and a discardable output buffer. SessionID is left
// zero so the dispatcher's success path skips the activity update (which
// would require a real session service).
func newIntegrationCommandExecution(charID ulid.ULID) *command.CommandExecution {
	return command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: charID,
		Output:      &bytes.Buffer{},
		Services:    command.NewTestServices(command.ServicesConfig{}),
	})
}
