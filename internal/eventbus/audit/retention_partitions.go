// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	retaudit "github.com/holomush/holomush/internal/audit"
)

// EventsAuditPartitionManager is the production
// internal/audit.PartitionManager for the RANGE-partitioned events_audit
// table (partitioned on the deterministic event_ms BIGINT key from 06-01's
// migration 000052). It maintains the retention-window-backward + forward
// partition coverage, prunes old partitions via a DETACH-rename-then-drop-by-
// name-age cycle, and re-homes the legacy events_audit_unpartitioned history
// (the one-time Backfill method).
//
// It lives in package audit (internal/eventbus/audit) while the
// PartitionManager interface / RetentionWorker live in a DIFFERENT package
// ALSO named audit (internal/audit), so that import is aliased retaudit
// (round-5 M5). A bare audit.PartitionManager here resolves to THIS package
// and does not compile.
type EventsAuditPartitionManager struct {
	pool         *pgxpool.Pool
	retainWindow time.Duration
	logger       *slog.Logger
}

// Compile-time assertion that the manager satisfies the retention interface
// via the aliased import (round-5 M5).
var _ retaudit.PartitionManager = (*EventsAuditPartitionManager)(nil)

const (
	// eventsAuditTable is the partitioned parent this manager maintains.
	// Never events_audit_unpartitioned or any non-child table.
	eventsAuditTable = "events_audit"

	// schemaName is the target schema for every catalog query and DDL
	// statement (schema-qualified discovery — round-4 F9 / round-3 MEDIUM).
	schemaName = "public"

	// partitionProvenanceMarker is the durable COMMENT ON TABLE marker
	// stamped on every genuine events_audit partition at creation (round-4
	// F9). It is the ONLY durable proof that a table now absent from
	// pg_inherits was formerly OUR child (it survives DETACH, which strips
	// the parent link, and the _detached_<unix> rename, which keeps the same
	// relation OID). Reconcile and drop REQUIRE it; the stamp itself is gated
	// on genuine child-ness (round-5 H2) so it is never granted to a
	// same-named non-child.
	partitionProvenanceMarker = "holomush:events_audit_partition"

	// detachedInfix separates the canonical partition name from the unix
	// detach epoch in the events_audit_<YYYY_MM>_detached_<unix> rename. The
	// suffix is the durable grace clock (DETACH removes partition-bound
	// catalog metadata, so the name carries the detach time).
	detachedInfix = "_detached_"
)

// NewEventsAuditPartitionManager constructs a manager over the given pool with
// the operator-configured retention window. The window drives EnsurePartitions'
// backward month-span (independent of the worker's forward months arg).
func NewEventsAuditPartitionManager(pool *pgxpool.Pool, retainWindow time.Duration) *EventsAuditPartitionManager {
	return &EventsAuditPartitionManager{
		pool:         pool,
		retainWindow: retainWindow,
		logger:       slog.Default(),
	}
}

// EnsurePartitions creates monthly event_ms partitions covering the retention
// window BACKWARD (derived from the manager's configured retainWindow, so an
// in-window historical DLQ replay lands in a real prunable partition) AND
// `months` FORWARD from now. Re-running is a no-op (CREATE ... IF NOT EXISTS).
//
// After each CREATE, the durable provenance marker is stamped ONLY behind a
// schema-qualified pg_inherits child-ness gate (round-5 H2): a pre-existing
// SAME-NAMED NON-CHILD that CREATE ... IF NOT EXISTS silently skipped is NEVER
// stamped — EnsurePartitions FAILS CLOSED with a structured oops instead.
func (m *EventsAuditPartitionManager) EnsurePartitions(ctx context.Context, months int) error {
	now := time.Now().UTC()
	start := monthStart(now.Add(-m.retainWindow))
	end := monthStart(now.AddDate(0, months, 0))
	for ms := start; !ms.After(end); ms = ms.AddDate(0, 1, 0) {
		if err := m.ensureMonthPartition(ctx, ms); err != nil {
			return err
		}
	}
	return nil
}

// ensureMonthPartition creates (idempotently) the partition covering the month
// starting at ms and stamps the durable provenance marker behind the
// child-ness gate.
func (m *EventsAuditPartitionManager) ensureMonthPartition(ctx context.Context, ms time.Time) error {
	name := partitionNameForMonth(ms)
	fromNS, toNS := monthBoundsNS(ms)

	create := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM (%d) TO (%d)",
		pgx.Identifier{schemaName, name}.Sanitize(),
		pgx.Identifier{schemaName, eventsAuditTable}.Sanitize(),
		fromNS, toNS,
	)
	if _, err := m.pool.Exec(ctx, create); err != nil {
		return oops.Code("AUDIT_PARTITION_CREATE_FAILED").
			With("partition", name).
			With("range_start_ns", fromNS).
			With("range_end_ns", toNS).
			Wrap(err)
	}

	// STAMP-TIME child-ness gate (round-5 H2): PG's IF NOT EXISTS does not
	// guarantee an existing relation matches the requested partition, so a
	// pre-existing same-named non-child would be silently skipped by CREATE.
	// Verify genuine current child-ness before stamping the trusted marker.
	isChild, err := m.isCurrentChild(ctx, name)
	if err != nil {
		return oops.Code("AUDIT_PARTITION_PROBE_FAILED").With("partition", name).Wrap(err)
	}
	if !isChild {
		return oops.Code("AUDIT_PARTITION_NAME_OCCUPIED").
			With("partition", name).
			With("parent", eventsAuditTable).
			Errorf("relation %q exists but is not a current child of %s (same-named non-child); refusing to stamp provenance marker", name, eventsAuditTable)
	}

	// Idempotent on a genuine existing child (COMMENT set to the same value).
	stamp := fmt.Sprintf(
		"COMMENT ON TABLE %s IS %s",
		pgx.Identifier{schemaName, name}.Sanitize(),
		quoteLiteral(partitionProvenanceMarker),
	)
	if _, err := m.pool.Exec(ctx, stamp); err != nil {
		return oops.Code("AUDIT_PARTITION_STAMP_FAILED").With("partition", name).Wrap(err)
	}
	return nil
}

// isCurrentChild reports whether `child` is a genuine current partition of the
// schema-qualified events_audit parent.
func (m *EventsAuditPartitionManager) isCurrentChild(ctx context.Context, child string) (bool, error) {
	var one int
	err := m.pool.QueryRow(ctx, `
		SELECT 1 FROM pg_inherits i
		  JOIN pg_class c      ON c.oid = i.inhrelid
		  JOIN pg_namespace n  ON n.oid = c.relnamespace
		  JOIN pg_class p      ON p.oid = i.inhparent
		  JOIN pg_namespace pn ON pn.oid = p.relnamespace
		 WHERE pn.nspname = $1 AND p.relname = $2
		   AND n.nspname  = $1 AND c.relname = $3`,
		schemaName, eventsAuditTable, child).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, oops.Code("AUDIT_CHILD_PROBE_FAILED").Wrap(err)
	}
	return true, nil
}

// DetachExpiredPartitions runs two recovery passes FIRST, then the normal
// detach:
//
//	0-FINALIZE  finalizes any child left pg_inherits.inhdetachpending=true by
//	            an interrupted DETACH ... CONCURRENTLY (round-3 finding 2),
//	            then renames it to the _detached_<unix> form.
//	0-RECONCILE re-homes canonical-named tables no longer children of
//	            events_audit that carry the provenance marker (a partition
//	            detached by a prior cycle that crashed before its rename —
//	            round-2 finding 7 / round-4 F9) into the _detached_<unix> form.
//	DETACH      detaches (CONCURRENTLY) each current child whose entire
//	            event_ms range is older than olderThan and renames it.
//
// Returns the new _detached_<unix> names. A recent partition is not detached.
func (m *EventsAuditPartitionManager) DetachExpiredPartitions(ctx context.Context, olderThan time.Time) ([]string, error) {
	// Passes run in order (finalize → reconcile → detach); all three are
	// attempted even if an earlier one errors, and errors are combined.
	fin, finErr := m.finalizePendingDetaches(ctx)
	rec, recErr := m.reconcileCrashOrphans(ctx)
	det, detErr := m.detachOlderThan(ctx, olderThan.UnixNano())

	renamed := make([]string, 0, len(fin)+len(rec)+len(det))
	renamed = append(renamed, fin...)
	renamed = append(renamed, rec...)
	renamed = append(renamed, det...)

	if err := errors.Join(finErr, recErr, detErr); err != nil {
		return renamed, oops.Code("AUDIT_DETACH_CYCLE_FAILED").Wrap(err)
	}
	return renamed, nil
}

// finalizePendingDetaches completes any interrupted DETACH ... CONCURRENTLY
// (child left inhdetachpending=true) via DETACH ... FINALIZE, then renames the
// finalized child to the _detached_<unix> form.
func (m *EventsAuditPartitionManager) finalizePendingDetaches(ctx context.Context) ([]string, error) {
	names, err := m.queryNames(ctx, `
		SELECT c.relname FROM pg_inherits i
		  JOIN pg_class c      ON c.oid = i.inhrelid
		  JOIN pg_namespace n  ON n.oid = c.relnamespace
		  JOIN pg_class p      ON p.oid = i.inhparent
		  JOIN pg_namespace pn ON pn.oid = p.relnamespace
		 WHERE pn.nspname = $1 AND p.relname = $2 AND n.nspname = $1
		   AND i.inhdetachpending = true`,
		schemaName, eventsAuditTable)
	if err != nil {
		return nil, oops.Code("AUDIT_FINALIZE_DISCOVERY_FAILED").Wrap(err)
	}
	var renamed []string
	for _, name := range names {
		fin := fmt.Sprintf("ALTER TABLE %s DETACH PARTITION %s FINALIZE",
			pgx.Identifier{schemaName, eventsAuditTable}.Sanitize(), pgx.Identifier{schemaName, name}.Sanitize())
		if _, err := m.pool.Exec(ctx, fin); err != nil {
			return renamed, oops.Code("AUDIT_DETACH_FINALIZE_FAILED").With("partition", name).Wrap(err)
		}
		newName, err := m.renameDetached(ctx, name)
		if err != nil {
			return renamed, err
		}
		m.logger.InfoContext(ctx, "finalized interrupted concurrent detach", "partition", name, "renamed_to", newName)
		renamed = append(renamed, newName)
	}
	return renamed, nil
}

// reconcileCrashOrphans re-homes canonical-named events_audit_YYYY_MM tables
// that are no longer children of events_audit AND carry the provenance marker
// (a prior-cycle DETACH that crashed before its rename) into the
// _detached_<unix> form so DropDetachedPartitions can eventually reclaim them.
// The marker requirement (round-4 F9) is what proves the orphan was formerly
// ours; a coincidentally-named non-child lacking the marker is never touched.
func (m *EventsAuditPartitionManager) reconcileCrashOrphans(ctx context.Context) ([]string, error) {
	names, err := m.queryNames(ctx, `
		SELECT c.relname FROM pg_class c
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = $1
		   AND c.relname ~ '^events_audit_[0-9]{4}_[0-9]{2}$'
		   AND obj_description(c.oid, 'pg_class') = $2
		   AND NOT EXISTS (
		     SELECT 1 FROM pg_inherits i
		       JOIN pg_class p      ON p.oid = i.inhparent
		       JOIN pg_namespace pn ON pn.oid = p.relnamespace
		      WHERE i.inhrelid = c.oid AND pn.nspname = $1 AND p.relname = $3)`,
		schemaName, partitionProvenanceMarker, eventsAuditTable)
	if err != nil {
		return nil, oops.Code("AUDIT_RECONCILE_DISCOVERY_FAILED").Wrap(err)
	}
	var renamed []string
	for _, name := range names {
		newName, err := m.renameDetached(ctx, name)
		if err != nil {
			return renamed, err
		}
		m.logger.WarnContext(ctx, "reconciled crash-orphaned detached partition", "partition", name, "renamed_to", newName)
		renamed = append(renamed, newName)
	}
	return renamed, nil
}

// detachOlderThan detaches (CONCURRENTLY, outside an explicit transaction) each
// current child whose entire event_ms range (derived from the canonical
// YYYY_MM name) ends at or before olderThanNS, then records the detach epoch in
// a _detached_<unix> rename.
func (m *EventsAuditPartitionManager) detachOlderThan(ctx context.Context, olderThanNS int64) ([]string, error) {
	names, err := m.queryNames(ctx, `
		SELECT c.relname FROM pg_inherits i
		  JOIN pg_class c      ON c.oid = i.inhrelid
		  JOIN pg_namespace n  ON n.oid = c.relnamespace
		  JOIN pg_class p      ON p.oid = i.inhparent
		  JOIN pg_namespace pn ON pn.oid = p.relnamespace
		 WHERE pn.nspname = $1 AND p.relname = $2 AND n.nspname = $1
		   AND i.inhdetachpending = false`,
		schemaName, eventsAuditTable)
	if err != nil {
		return nil, oops.Code("AUDIT_DETACH_DISCOVERY_FAILED").Wrap(err)
	}
	var renamed []string
	for _, name := range names {
		ms, ok := parseMonthFromPartitionName(name)
		if !ok {
			continue // not a canonical monthly partition; never touch
		}
		_, upperNS := monthBoundsNS(ms)
		if upperNS > olderThanNS {
			continue // range not entirely older than the cutoff; retain
		}
		// DETACH CONCURRENTLY MUST run outside an explicit tx; pool.Exec
		// autocommits (no Begin/Commit), one detach at a time.
		detach := fmt.Sprintf("ALTER TABLE %s DETACH PARTITION %s CONCURRENTLY",
			pgx.Identifier{schemaName, eventsAuditTable}.Sanitize(), pgx.Identifier{schemaName, name}.Sanitize())
		if _, err := m.pool.Exec(ctx, detach); err != nil {
			return renamed, oops.Code("AUDIT_DETACH_FAILED").With("partition", name).Wrap(err)
		}
		newName, err := m.renameDetached(ctx, name)
		if err != nil {
			return renamed, err
		}
		m.logger.InfoContext(ctx, "detached expired partition", "partition", name, "renamed_to", newName)
		renamed = append(renamed, newName)
	}
	return renamed, nil
}

// renameDetached renames a (now non-child) canonical-named partition to
// events_audit_<YYYY_MM>_detached_<now-unix>, stamping the detach epoch.
func (m *EventsAuditPartitionManager) renameDetached(ctx context.Context, canonical string) (string, error) {
	newName := fmt.Sprintf("%s%s%d", canonical, detachedInfix, time.Now().UTC().Unix())
	// The source is schema-qualified to public (matching the public-pinned
	// discovery queries); RENAME TO takes a BARE new name — a schema-qualified
	// target is a syntax error and cannot move a relation across schemas.
	rename := fmt.Sprintf("ALTER TABLE %s RENAME TO %s",
		pgx.Identifier{schemaName, canonical}.Sanitize(), pgx.Identifier{newName}.Sanitize())
	if _, err := m.pool.Exec(ctx, rename); err != nil {
		return "", oops.Code("AUDIT_DETACH_RENAME_FAILED").With("from", canonical).With("to", newName).Wrap(err)
	}
	return newName, nil
}

// DropDetachedPartitions discovers detached tables (schema-qualified) by the
// events_audit_%_detached_% name pattern AND the durable provenance marker
// (round-4 F9 — never drop a same-named table lacking the marker), parses the
// trailing unix epoch, and DROPs those whose age exceeds grace. A just-detached
// partition within grace is not dropped.
func (m *EventsAuditPartitionManager) DropDetachedPartitions(ctx context.Context, grace time.Duration) ([]string, error) {
	names, err := m.queryNames(ctx, `
		SELECT c.relname FROM pg_class c
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = $1
		   AND c.relname LIKE 'events_audit_%_detached_%'
		   AND obj_description(c.oid, 'pg_class') = $2`,
		schemaName, partitionProvenanceMarker)
	if err != nil {
		return nil, oops.Code("AUDIT_DROP_DISCOVERY_FAILED").Wrap(err)
	}
	nowUnix := time.Now().UTC().Unix()
	graceSecs := int64(grace.Seconds())
	var dropped []string
	for _, name := range names {
		epoch, ok := parseDetachedEpoch(name)
		if !ok {
			continue
		}
		if nowUnix-epoch <= graceSecs {
			continue // still within grace
		}
		drop := fmt.Sprintf("DROP TABLE IF EXISTS %s", pgx.Identifier{schemaName, name}.Sanitize())
		if _, err := m.pool.Exec(ctx, drop); err != nil {
			return dropped, oops.Code("AUDIT_DROP_FAILED").With("partition", name).Wrap(err)
		}
		m.logger.InfoContext(ctx, "dropped detached partition past grace", "partition", name, "age_seconds", nowUnix-epoch)
		dropped = append(dropped, name)
	}
	return dropped, nil
}

// PurgeExpiredAllows is a no-op: events_audit has no allow/deny split (unlike
// the ABAC access_audit_log). Returns (0, nil).
func (m *EventsAuditPartitionManager) PurgeExpiredAllows(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

// HealthCheck returns nil when events_audit is reachable (cheap probe).
func (m *EventsAuditPartitionManager) HealthCheck(ctx context.Context) error {
	if _, err := m.pool.Exec(ctx, "SELECT 1 FROM public.events_audit LIMIT 0"); err != nil {
		return oops.Code("AUDIT_PARTITION_HEALTHCHECK_FAILED").Wrap(err)
	}
	return nil
}

// backfillChunk bounds how many legacy rows are read per keyset page.
const backfillChunk = 1000

// legacyAuditRow is one row of the pre-000052 events_audit table (now
// events_audit_unpartitioned), which has NO event_ms column.
type legacyAuditRow struct {
	id        []byte
	subject   string
	typ       string
	timestamp int64
	actorKind string
	actorID   []byte
	envelope  []byte
	schemaVer int16
	codec     string
	jsSeq     int64
	rendering []byte
	dekRef    *int64
	dekVer    *int32
}

// Backfill is the one-time re-homing of the legacy events_audit_unpartitioned
// rows into the partitioned events_audit, so no audit history is lost after
// 06-01's migration 000052 renamed the legacy table out of the way. It is the
// ONLY path that restores cold-history visibility after 000052.
//
// For each legacy row it derives event_ms via 06-01's shared eventMsFromULID
// helper (IDENTICAL to writeAuditRow), ensures a covering partition, and
// inserts ON CONFLICT (id, event_ms) DO NOTHING — so a legacy row and a later
// DLQ replay of the same event dedup to exactly one row. Rows are processed in
// bounded keyset chunks (ORDER BY id). After all rows are copied the legacy
// table is renamed to events_audit_legacy_migrated (retained briefly for ops).
//
// Idempotent: if events_audit_unpartitioned is absent, it returns nil
// immediately; a partial prior run re-copies harmlessly under ON CONFLICT.
func (m *EventsAuditPartitionManager) Backfill(ctx context.Context) error {
	var present *string
	if err := m.pool.QueryRow(ctx,
		`SELECT to_regclass('public.events_audit_unpartitioned')::text`).Scan(&present); err != nil {
		return oops.Code("AUDIT_BACKFILL_PROBE_FAILED").Wrap(err)
	}
	if present == nil {
		return nil // already re-homed (or never existed) — no-op
	}

	ensured := make(map[string]struct{})
	var copied int
	lastID := []byte{} // empty bytea sorts before any 16-byte ULID
	for {
		rows, err := m.readLegacyChunk(ctx, lastID)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for i := range rows {
			r := &rows[i]
			if len(r.id) != 16 {
				return oops.Code("AUDIT_BACKFILL_BAD_ID").
					With("id_len", len(r.id)).
					Errorf("legacy events_audit id is not a 16-byte ULID")
			}
			var u ulid.ULID
			copy(u[:], r.id)
			eventMS := eventMsFromULID(u)

			// Ensure the covering historical partition exists exactly once per
			// distinct month (memoized), reusing the stamp-gated ensure logic.
			ms := monthStart(time.Unix(0, eventMS).UTC())
			name := partitionNameForMonth(ms)
			if _, ok := ensured[name]; !ok {
				if err := m.ensureMonthPartition(ctx, ms); err != nil {
					return err
				}
				ensured[name] = struct{}{}
			}

			if _, err := m.pool.Exec(
				ctx, `
				INSERT INTO public.events_audit
				  (id, subject, type, timestamp, actor_kind, actor_id, envelope,
				   schema_ver, codec, js_seq, rendering, dek_ref, dek_version, event_ms)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
				ON CONFLICT (id, event_ms) DO NOTHING`,
				r.id, r.subject, r.typ, r.timestamp, r.actorKind, r.actorID, r.envelope,
				r.schemaVer, r.codec, r.jsSeq, r.rendering, r.dekRef, r.dekVer, eventMS,
			); err != nil {
				return oops.Code("AUDIT_BACKFILL_INSERT_FAILED").Wrap(err)
			}
			copied++
			lastID = r.id
		}
		if len(rows) < backfillChunk {
			break
		}
	}

	// Rename the drained legacy table so a re-run is a no-op. The SOURCE is
	// schema-qualified to public (matching the rest of this file's hardening);
	// RENAME TO takes a BARE new name — a schema-qualified target is a syntax
	// error (mirrors renameDetached).
	rename := fmt.Sprintf("ALTER TABLE %s RENAME TO %s",
		pgx.Identifier{schemaName, "events_audit_unpartitioned"}.Sanitize(),
		pgx.Identifier{"events_audit_legacy_migrated"}.Sanitize())
	if _, err := m.pool.Exec(ctx, rename); err != nil {
		return oops.Code("AUDIT_BACKFILL_RENAME_FAILED").Wrap(err)
	}
	m.logger.InfoContext(ctx, "backfilled legacy events_audit rows into partitioned table",
		"rows", copied, "partitions_ensured", len(ensured))
	return nil
}

// readLegacyChunk keyset-paginates the legacy table by id.
func (m *EventsAuditPartitionManager) readLegacyChunk(ctx context.Context, afterID []byte) ([]legacyAuditRow, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT id, subject, type, timestamp, actor_kind, actor_id, envelope,
		       schema_ver, codec, js_seq, rendering, dek_ref, dek_version
		  FROM events_audit_unpartitioned
		 WHERE id > $1
		 ORDER BY id
		 LIMIT $2`, afterID, backfillChunk)
	if err != nil {
		return nil, oops.Code("AUDIT_BACKFILL_READ_FAILED").Wrap(err)
	}
	defer rows.Close()
	var out []legacyAuditRow
	for rows.Next() {
		var r legacyAuditRow
		if err := rows.Scan(&r.id, &r.subject, &r.typ, &r.timestamp, &r.actorKind, &r.actorID,
			&r.envelope, &r.schemaVer, &r.codec, &r.jsSeq, &r.rendering, &r.dekRef, &r.dekVer); err != nil {
			return nil, oops.Code("AUDIT_BACKFILL_SCAN_FAILED").Wrap(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("AUDIT_BACKFILL_READ_FAILED").Wrap(err)
	}
	return out, nil
}

// queryNames runs a single-column relname query and returns the names. It
// fully drains and closes the rows before returning so the pooled connection
// is free for the DDL that follows (the callers issue ALTER/DROP per name).
func (m *EventsAuditPartitionManager) queryNames(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := m.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, oops.Code("AUDIT_CATALOG_QUERY_FAILED").Wrap(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, oops.Code("AUDIT_CATALOG_SCAN_FAILED").Wrap(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("AUDIT_CATALOG_QUERY_FAILED").Wrap(err)
	}
	return names, nil
}

// --- naming helpers -------------------------------------------------------

// monthStart returns midnight UTC on the first day of t's month.
func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// partitionNameForMonth renders the canonical events_audit_YYYY_MM name.
func partitionNameForMonth(ms time.Time) string {
	return fmt.Sprintf("%s_%04d_%02d", eventsAuditTable, ms.Year(), int(ms.Month()))
}

// monthBoundsNS returns the inclusive-from / exclusive-to event_ms bounds (int64
// UnixNano) for the month starting at ms.
func monthBoundsNS(ms time.Time) (fromNS, toNS int64) {
	start := monthStart(ms)
	end := start.AddDate(0, 1, 0)
	return start.UnixNano(), end.UnixNano()
}

// parseMonthFromPartitionName extracts the month start from a canonical
// events_audit_YYYY_MM name (no _detached_ suffix). ok is false for any other
// shape.
func parseMonthFromPartitionName(name string) (time.Time, bool) {
	rest, ok := strings.CutPrefix(name, eventsAuditTable+"_")
	if !ok {
		return time.Time{}, false
	}
	parts := strings.Split(rest, "_")
	if len(parts) != 2 {
		return time.Time{}, false
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil || len(parts[0]) != 4 {
		return time.Time{}, false
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil || len(parts[1]) != 2 || month < 1 || month > 12 {
		return time.Time{}, false
	}
	return time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC), true
}

// parseDetachedEpoch extracts the trailing unix epoch from a
// events_audit_<YYYY_MM>_detached_<unix> name.
func parseDetachedEpoch(name string) (int64, bool) {
	idx := strings.LastIndex(name, detachedInfix)
	if idx < 0 {
		return 0, false
	}
	tail := name[idx+len(detachedInfix):]
	epoch, err := strconv.ParseInt(tail, 10, 64)
	if err != nil {
		return 0, false
	}
	return epoch, true
}

// quoteLiteral renders a single-quoted SQL string literal for a utility
// statement (COMMENT ON) where bind parameters are not permitted. The marker
// carries no quotes; the doubling keeps the helper safe for any input.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
