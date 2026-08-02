-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up

-- 000052 — convert events_audit from a single un-partitioned table into a
-- RANGE-partitioned table keyed on a NEW deterministic column event_ms.
--
-- Design (06-01, redesigned after cross-AI review — 06-REVIEWS.md consensus):
--   * event_ms BIGINT NOT NULL is a SEPARATE partition key derived purely from
--     the immutable event ULID (ulid.Time(id.Time()).UnixNano() in Go), so the
--     dedup key is identical on the live-projection and DLQ-replay paths. The
--     existing `timestamp` column is left UNTOUCHED (still JetStream store-time)
--     so cold-history window filtering keeps its exact semantics.
--   * NO DEFAULT partition — a DEFAULT would forbid DETACH ... CONCURRENTLY in
--     06-02 and never prune. An out-of-window INSERT fails loud (correct).
--   * The legacy rows are NOT copied/ATTACHed here — they stay in
--     events_audit_unpartitioned and are re-homed by 06-02's one-time Go
--     Backfill (migration rule: no long-running backfill inside a migration).
--   * 000052 co-deploys with 06-02 as a SINGLE ship unit (it renames all
--     history off events_audit).
--
-- Idempotent (regclass / catalog-guarded DO-blocks — the 000017/000038 idiom;
-- anonymous DO-blocks are NOT persisted triggers/functions). Every timestamp-
-- class column stays BIGINT epoch-ns (INV-STORE-1 / lint:no-timestamptz).

-- ── Step 1: rename the legacy table out of the way ────────────────────────────
-- Only when events_audit exists, is NOT already partitioned, and the target
-- name is free — so a rerun is a no-op and the partitioned parent is never
-- clobbered.
-- +goose StatementBegin
DO $$
BEGIN
  IF to_regclass('public.events_audit') IS NOT NULL
     AND (SELECT relkind FROM pg_class WHERE oid = 'public.events_audit'::regclass) <> 'p'
     AND to_regclass('public.events_audit_unpartitioned') IS NULL
  THEN
    ALTER TABLE public.events_audit RENAME TO events_audit_unpartitioned;
  END IF;
END $$;
-- +goose StatementEnd

-- ── Step 2: free the legacy PK + index NAMES ─────────────────────────────────
-- Rename the legacy PK constraint and every events_audit_* index to a _legacy
-- name so Step 3's new parent owns freshly-created relations under the original
-- names. Each rename is guarded on the relation still belonging to
-- events_audit_unpartitioned (indrelid / conrelid), so a rerun after the parent
-- exists never re-renames the NEW parent's PK/indexes.
-- +goose StatementBegin
DO $$
BEGIN
  IF to_regclass('public.events_audit_unpartitioned') IS NULL THEN
    RETURN;
  END IF;

  IF EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'events_audit_pkey'
      AND conrelid = 'public.events_audit_unpartitioned'::regclass
  ) THEN
    ALTER TABLE public.events_audit_unpartitioned
      RENAME CONSTRAINT events_audit_pkey TO events_audit_pkey_legacy;
  END IF;

  IF EXISTS (
    SELECT 1 FROM pg_index i JOIN pg_class ic ON ic.oid = i.indexrelid
    WHERE ic.relname = 'events_audit_subject_id'
      AND i.indrelid = 'public.events_audit_unpartitioned'::regclass
  ) THEN
    ALTER INDEX public.events_audit_subject_id RENAME TO events_audit_subject_id_legacy;
  END IF;

  IF EXISTS (
    SELECT 1 FROM pg_index i JOIN pg_class ic ON ic.oid = i.indexrelid
    WHERE ic.relname = 'events_audit_subject_ts'
      AND i.indrelid = 'public.events_audit_unpartitioned'::regclass
  ) THEN
    ALTER INDEX public.events_audit_subject_ts RENAME TO events_audit_subject_ts_legacy;
  END IF;

  IF EXISTS (
    SELECT 1 FROM pg_index i JOIN pg_class ic ON ic.oid = i.indexrelid
    WHERE ic.relname = 'events_audit_subject_pat'
      AND i.indrelid = 'public.events_audit_unpartitioned'::regclass
  ) THEN
    ALTER INDEX public.events_audit_subject_pat RENAME TO events_audit_subject_pat_legacy;
  END IF;

  IF EXISTS (
    SELECT 1 FROM pg_index i JOIN pg_class ic ON ic.oid = i.indexrelid
    WHERE ic.relname = 'events_audit_subject_js_seq'
      AND i.indrelid = 'public.events_audit_unpartitioned'::regclass
  ) THEN
    ALTER INDEX public.events_audit_subject_js_seq RENAME TO events_audit_subject_js_seq_legacy;
  END IF;

  IF EXISTS (
    SELECT 1 FROM pg_index i JOIN pg_class ic ON ic.oid = i.indexrelid
    WHERE ic.relname = 'events_audit_dek_ref'
      AND i.indrelid = 'public.events_audit_unpartitioned'::regclass
  ) THEN
    ALTER INDEX public.events_audit_dek_ref RENAME TO events_audit_dek_ref_legacy;
  END IF;
END $$;
-- +goose StatementEnd

-- ── Step 3: create the partitioned parent ────────────────────────────────────
-- Reproduce the CURRENT (post-000038) column set — BIGINT epoch-ns timestamps,
-- envelope (000017 rename), rendering (000012), dek_ref/dek_version (000014) —
-- plus the NEW event_ms partition key. Composite PK includes the partition
-- column (Postgres requirement). Recreate the original-named indexes on the
-- parent (names freed by Step 2) + a BRIN index on event_ms for cheap pruning.
CREATE TABLE IF NOT EXISTS public.events_audit (
    id           BYTEA       NOT NULL,
    subject      TEXT        NOT NULL,
    type         TEXT        NOT NULL,
    timestamp    BIGINT      NOT NULL,
    actor_kind   TEXT        NOT NULL,
    actor_id     BYTEA,
    envelope     BYTEA       NOT NULL,
    schema_ver   SMALLINT    NOT NULL,
    codec        TEXT        NOT NULL,
    js_seq       BIGINT      NOT NULL,
    inserted_at  BIGINT      NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    rendering    JSONB       NOT NULL,
    dek_ref      BIGINT,
    dek_version  INTEGER,
    event_ms     BIGINT      NOT NULL,
    PRIMARY KEY (id, event_ms)
) PARTITION BY RANGE (event_ms);

CREATE INDEX IF NOT EXISTS events_audit_subject_id      ON public.events_audit (subject, id);
CREATE INDEX IF NOT EXISTS events_audit_subject_ts      ON public.events_audit (subject, timestamp);
CREATE INDEX IF NOT EXISTS events_audit_subject_pat     ON public.events_audit (subject text_pattern_ops);
CREATE INDEX IF NOT EXISTS events_audit_subject_js_seq  ON public.events_audit (subject, js_seq);
CREATE INDEX IF NOT EXISTS events_audit_dek_ref         ON public.events_audit (dek_ref) WHERE dek_ref IS NOT NULL;
CREATE INDEX IF NOT EXISTS events_audit_event_ms_brin   ON public.events_audit USING BRIN (event_ms);

-- ── Step 4: create the current + next-2 monthly event_ms partitions ──────────
-- Mirror the Go worker naming (events_audit_%04d_%02d) and int64-ns UnixNano
-- bounds so live writes land immediately after deploy. Month boundaries are
-- whole seconds, so EXTRACT(EPOCH ...)::bigint * 1e9 is exact (no double
-- precision loss). NO DEFAULT partition. 06-02's EnsurePartitions keeps the
-- forward + retention-window coverage thereafter.
-- Locals are BIGINT/TEXT only (no date/time-typed vars) so lint:no-timestamptz
-- stays green; month-start is an inline expression via make_interval.
-- +goose StatementBegin
DO $$
DECLARE
  i       INT;
  from_ns BIGINT;
  to_ns   BIGINT;
  pname   TEXT;
BEGIN
  FOR i IN 0..2 LOOP
    from_ns := (EXTRACT(EPOCH FROM ((date_trunc('month', now() AT TIME ZONE 'UTC') + make_interval(months => i))     AT TIME ZONE 'UTC'))::bigint) * 1000000000;
    to_ns   := (EXTRACT(EPOCH FROM ((date_trunc('month', now() AT TIME ZONE 'UTC') + make_interval(months => i + 1)) AT TIME ZONE 'UTC'))::bigint) * 1000000000;
    pname   := 'events_audit_' || to_char(date_trunc('month', now() AT TIME ZONE 'UTC') + make_interval(months => i), 'YYYY_MM');
    EXECUTE format(
      'CREATE TABLE IF NOT EXISTS public.%I PARTITION OF public.events_audit FOR VALUES FROM (%s) TO (%s)',
      pname, from_ns, to_ns);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down

-- 000052 DOWN — reverse the partition swap, DATA-PRESERVING and idempotently
-- resumable.
--
-- Rows written to the partitioned parent AFTER the up survive the rollback:
-- they are copied back into a restored un-partitioned events_audit BEFORE the
-- partitioned parent is dropped. Both copies (parent→temp and legacy→temp) use
-- ON CONFLICT (id) DO NOTHING so a resumed partial down never hits a
-- duplicate-key error (review MEDIUM). The partitioned parent is dropped FIRST
-- to free the original PK/index names, then the surviving legacy table, then
-- the temp restored table is renamed into place under the original names.
--
-- event_ms is DROPPED (it did not exist pre-000052). Anonymous DO-blocks only
-- (no persisted triggers/functions).
--
-- Every object reference is schema-qualified to public and every existence
-- check uses to_regclass() — never a bare ::regclass cast, which RAISES when the
-- relation is absent on a resumed or already-completed rollback — so a
-- non-default session search_path can neither roll back the wrong schema nor
-- error out. RENAME ... TO targets stay bare (a schema-qualified rename target
-- is a Postgres syntax error). A re-run after a completed rollback is a true
-- no-op: Step 1 only builds the temp table when a rollback SOURCE is still
-- present, so no empty events_audit_restore_tmp is left behind.

-- ── Step 1: build the restored un-partitioned table under a temp name ────────
-- Guarded on a rollback SOURCE existing (the partitioned parent, the surviving
-- legacy table, or an in-progress temp) so a re-run after a completed rollback
-- creates nothing.
-- +goose StatementBegin
DO $$
BEGIN
  IF (to_regclass('public.events_audit') IS NOT NULL
      AND (SELECT relkind FROM pg_class WHERE oid = to_regclass('public.events_audit')) = 'p')
     OR to_regclass('public.events_audit_unpartitioned') IS NOT NULL
     OR to_regclass('public.events_audit_restore_tmp') IS NOT NULL
  THEN
    CREATE TABLE IF NOT EXISTS public.events_audit_restore_tmp (
        id           BYTEA       NOT NULL,
        subject      TEXT        NOT NULL,
        type         TEXT        NOT NULL,
        timestamp    BIGINT      NOT NULL,
        actor_kind   TEXT        NOT NULL,
        actor_id     BYTEA,
        envelope     BYTEA       NOT NULL,
        schema_ver   SMALLINT    NOT NULL,
        codec        TEXT        NOT NULL,
        js_seq       BIGINT      NOT NULL,
        inserted_at  BIGINT      NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
        rendering    JSONB       NOT NULL,
        dek_ref      BIGINT,
        dek_version  INTEGER,
        CONSTRAINT events_audit_restore_tmp_pkey PRIMARY KEY (id)
    );
  END IF;
END $$;
-- +goose StatementEnd

-- ── Step 2: copy partitioned-parent rows back (idempotent) ───────────────────
-- +goose StatementBegin
DO $$
BEGIN
  IF to_regclass('public.events_audit') IS NOT NULL
     AND (SELECT relkind FROM pg_class WHERE oid = to_regclass('public.events_audit')) = 'p'
  THEN
    INSERT INTO public.events_audit_restore_tmp (
        id, subject, type, timestamp, actor_kind, actor_id,
        envelope, schema_ver, codec, js_seq, inserted_at, rendering,
        dek_ref, dek_version)
    SELECT id, subject, type, timestamp, actor_kind, actor_id,
           envelope, schema_ver, codec, js_seq, inserted_at, rendering,
           dek_ref, dek_version
    FROM public.events_audit
    ON CONFLICT (id) DO NOTHING;
  END IF;
END $$;
-- +goose StatementEnd

-- ── Step 3: copy surviving legacy rows back (idempotent) ─────────────────────
-- events_audit_unpartitioned is present when 06-02's backfill has not yet
-- re-homed + dropped it.
-- +goose StatementBegin
DO $$
BEGIN
  IF to_regclass('public.events_audit_unpartitioned') IS NOT NULL THEN
    INSERT INTO public.events_audit_restore_tmp (
        id, subject, type, timestamp, actor_kind, actor_id,
        envelope, schema_ver, codec, js_seq, inserted_at, rendering,
        dek_ref, dek_version)
    SELECT id, subject, type, timestamp, actor_kind, actor_id,
           envelope, schema_ver, codec, js_seq, inserted_at, rendering,
           dek_ref, dek_version
    FROM public.events_audit_unpartitioned
    ON CONFLICT (id) DO NOTHING;
  END IF;
END $$;
-- +goose StatementEnd

-- ── Step 4: drop the partitioned parent (+ children) FIRST to free names ─────
-- +goose StatementBegin
DO $$
BEGIN
  IF to_regclass('public.events_audit') IS NOT NULL
     AND (SELECT relkind FROM pg_class WHERE oid = to_regclass('public.events_audit')) = 'p'
  THEN
    DROP TABLE public.events_audit CASCADE;
  END IF;
END $$;
-- +goose StatementEnd

-- ── Step 5: drop the legacy un-partitioned table if still present ────────────
DROP TABLE IF EXISTS public.events_audit_unpartitioned CASCADE;

-- ── Step 6: rename the temp restored table into place ────────────────────────
-- +goose StatementBegin
DO $$
BEGIN
  IF to_regclass('public.events_audit_restore_tmp') IS NOT NULL
     AND to_regclass('public.events_audit') IS NULL
  THEN
    ALTER TABLE public.events_audit_restore_tmp RENAME TO events_audit;
    ALTER TABLE public.events_audit
      RENAME CONSTRAINT events_audit_restore_tmp_pkey TO events_audit_pkey;
  END IF;
END $$;
-- +goose StatementEnd

-- ── Step 7: recreate the original secondary indexes on the restored table ────
-- Guarded on the restored table existing so a down run against a DB that never
-- had events_audit is a no-op rather than an error. The parent's same-named
-- partitioned indexes were dropped with it in Step 4.
-- +goose StatementBegin
DO $$
BEGIN
  IF to_regclass('public.events_audit') IS NOT NULL THEN
    CREATE INDEX IF NOT EXISTS events_audit_subject_id      ON public.events_audit (subject, id);
    CREATE INDEX IF NOT EXISTS events_audit_subject_ts      ON public.events_audit (subject, timestamp);
    CREATE INDEX IF NOT EXISTS events_audit_subject_pat     ON public.events_audit (subject text_pattern_ops);
    CREATE INDEX IF NOT EXISTS events_audit_subject_js_seq  ON public.events_audit (subject, js_seq);
    CREATE INDEX IF NOT EXISTS events_audit_dek_ref         ON public.events_audit (dek_ref) WHERE dek_ref IS NOT NULL;
  END IF;
END $$;
-- +goose StatementEnd
