-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

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

-- ── Step 2: copy partitioned-parent rows back (idempotent) ───────────────────
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

-- ── Step 3: copy surviving legacy rows back (idempotent) ─────────────────────
-- events_audit_unpartitioned is present when 06-02's backfill has not yet
-- re-homed + dropped it.
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

-- ── Step 4: drop the partitioned parent (+ children) FIRST to free names ─────
DO $$
BEGIN
  IF to_regclass('public.events_audit') IS NOT NULL
     AND (SELECT relkind FROM pg_class WHERE oid = to_regclass('public.events_audit')) = 'p'
  THEN
    DROP TABLE public.events_audit CASCADE;
  END IF;
END $$;

-- ── Step 5: drop the legacy un-partitioned table if still present ────────────
DROP TABLE IF EXISTS public.events_audit_unpartitioned CASCADE;

-- ── Step 6: rename the temp restored table into place ────────────────────────
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

-- ── Step 7: recreate the original secondary indexes on the restored table ────
-- Guarded on the restored table existing so a down run against a DB that never
-- had events_audit is a no-op rather than an error. The parent's same-named
-- partitioned indexes were dropped with it in Step 4.
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
