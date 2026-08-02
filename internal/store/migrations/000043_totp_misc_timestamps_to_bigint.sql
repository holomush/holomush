-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up

-- Convert totp, admin_approvals, plugins, content_items, aliases, access
-- policy, and access_audit_log timestamp columns from TIMESTAMPTZ to BIGINT
-- (epoch nanoseconds, UTC). INV-STORE-1.
--
-- Idempotent: each ALTER COLUMN ... TYPE step is wrapped in a DO block that
-- guards on information_schema.columns.data_type, so re-running this migration
-- (recovery replays, partial-apply retries) is safe. Pattern mirrors
-- 000038_eventbus_crypto_timestamps_to_bigint.up.sql.
--
-- Overflow-safe (INV-STORE-9): each TYPE USING clause converts in numeric and
-- clamps with GREATEST/LEAST to the int64-ns range, so pre-existing values
-- beyond ~[1678, 2262] or ±infinity saturate to the int64 bounds instead of
-- raising "bigint out of range" (SQLSTATE 22003). NULL is guarded explicitly
-- (LEAST/GREATEST ignore NULL inputs). SET DEFAULT keeps now()*1e9 — now()
-- cannot overflow. Backfills the gap that wedged the sandbox deploy
-- (holomush-0b3ec).

-- +goose StatementBegin
DO $$
BEGIN
  -- ═══ player_totp ═══
  -- enrolled_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_totp'
               AND column_name = 'enrolled_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE player_totp ALTER COLUMN enrolled_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE player_totp ALTER COLUMN enrolled_at TYPE BIGINT USING CASE WHEN enrolled_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM enrolled_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE player_totp ALTER COLUMN enrolled_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- player_totp.last_verified_at → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_totp'
               AND column_name = 'last_verified_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE player_totp ALTER COLUMN last_verified_at TYPE BIGINT USING CASE WHEN last_verified_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM last_verified_at) * 1000000000))::bigint END';
  END IF;

  -- player_totp.locked_until → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_totp'
               AND column_name = 'locked_until' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE player_totp ALTER COLUMN locked_until TYPE BIGINT USING CASE WHEN locked_until IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM locked_until) * 1000000000))::bigint END';
  END IF;

  -- ═══ player_totp_recovery_codes ═══
  -- created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_totp_recovery_codes'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE player_totp_recovery_codes ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE player_totp_recovery_codes ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE player_totp_recovery_codes ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- player_totp_recovery_codes.consumed_at → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_totp_recovery_codes'
               AND column_name = 'consumed_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE player_totp_recovery_codes ALTER COLUMN consumed_at TYPE BIGINT USING CASE WHEN consumed_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM consumed_at) * 1000000000))::bigint END';
  END IF;

  -- ═══ crypto_bootstrap_state ═══
  -- consumed_at: NOT NULL, no DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'crypto_bootstrap_state'
               AND column_name = 'consumed_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE crypto_bootstrap_state ALTER COLUMN consumed_at TYPE BIGINT USING CASE WHEN consumed_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM consumed_at) * 1000000000))::bigint END';
  END IF;

  -- ═══ admin_approvals ═══
  -- expires_at: NOT NULL, no DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'admin_approvals'
               AND column_name = 'expires_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE admin_approvals ALTER COLUMN expires_at TYPE BIGINT USING CASE WHEN expires_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM expires_at) * 1000000000))::bigint END';
  END IF;

  -- admin_approvals.approved_at → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'admin_approvals'
               AND column_name = 'approved_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE admin_approvals ALTER COLUMN approved_at TYPE BIGINT USING CASE WHEN approved_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM approved_at) * 1000000000))::bigint END';
  END IF;

  -- admin_approvals.created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'admin_approvals'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE admin_approvals ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE admin_approvals ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE admin_approvals ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- ═══ plugins ═══
  -- first_seen_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'plugins'
               AND column_name = 'first_seen_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN first_seen_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN first_seen_at TYPE BIGINT USING CASE WHEN first_seen_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM first_seen_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN first_seen_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- plugins.last_seen_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'plugins'
               AND column_name = 'last_seen_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN last_seen_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN last_seen_at TYPE BIGINT USING CASE WHEN last_seen_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM last_seen_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN last_seen_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- plugins.gc_at → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'plugins'
               AND column_name = 'gc_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN gc_at TYPE BIGINT USING CASE WHEN gc_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM gc_at) * 1000000000))::bigint END';
  END IF;

  -- ═══ content_items ═══
  -- updated_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'content_items'
               AND column_name = 'updated_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE content_items ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE content_items ALTER COLUMN updated_at TYPE BIGINT USING CASE WHEN updated_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM updated_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE content_items ALTER COLUMN updated_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- ═══ system_aliases ═══
  -- created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'system_aliases'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE system_aliases ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE system_aliases ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE system_aliases ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- ═══ player_aliases ═══
  -- created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_aliases'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE player_aliases ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE player_aliases ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE player_aliases ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- ═══ access_policies ═══
  -- created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'access_policies'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE access_policies ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE access_policies ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE access_policies ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- access_policies.updated_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'access_policies'
               AND column_name = 'updated_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE access_policies ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE access_policies ALTER COLUMN updated_at TYPE BIGINT USING CASE WHEN updated_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM updated_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE access_policies ALTER COLUMN updated_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- ═══ access_policy_versions ═══
  -- changed_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'access_policy_versions'
               AND column_name = 'changed_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE access_policy_versions ALTER COLUMN changed_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE access_policy_versions ALTER COLUMN changed_at TYPE BIGINT USING CASE WHEN changed_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM changed_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE access_policy_versions ALTER COLUMN changed_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;
END $$;
-- +goose StatementEnd

-- ═══ access_audit_log ═══
--
-- timestamp is the partition key (CREATE TABLE ... PARTITION BY RANGE (timestamp)
-- in 000001_baseline.up.sql:302). PostgreSQL forbids ALTER COLUMN TYPE on a
-- partition-key column (SQLSTATE 42P16 "cannot alter column ... because it is
-- part of the partition key"). The only mechanism is DROP + CREATE.
--
-- For TEST/E2E (fresh DB, no audit data yet): clean drop-and-recreate.
-- For PRODUCTION: operators MUST first copy access_audit_log data to a side
-- table BEFORE running this migration, then reload after. The data-loss
-- caveat is documented in the bead close note and PR description; the
-- partition_creator.go change (BIGINT bounds) ensures EnsurePartitions
-- can recreate child partitions on the new BIGINT parent.
--
-- DROP TABLE IF EXISTS + CREATE TABLE are both inherently idempotent — re-running
-- this section is safe (existing table is dropped and recreated identically).

DROP TABLE IF EXISTS access_audit_log CASCADE;

-- Schema mirrors the post-000005 shape (event_id/event_name renamed from
-- policy_id/policy_name; source/component/message added). Keep in sync with
-- 000005_audit_source_component.up.sql when either evolves.
CREATE TABLE access_audit_log (
    id               TEXT NOT NULL,
    timestamp        BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    subject          TEXT NOT NULL,
    original_subject TEXT,
    action           TEXT NOT NULL,
    resource         TEXT NOT NULL,
    effect           TEXT NOT NULL CHECK (effect IN ('allow', 'deny', 'default_deny', 'system_bypass')),
    event_id         TEXT,
    event_name       TEXT,
    attributes       JSONB,
    error_message    TEXT,
    provider_errors  JSONB,
    duration_us      INTEGER,
    source           TEXT NOT NULL,
    component        TEXT NOT NULL,
    message          TEXT NOT NULL,
    PRIMARY KEY (id, timestamp)
) PARTITION BY RANGE (timestamp);

CREATE INDEX idx_audit_log_timestamp ON access_audit_log USING BRIN (timestamp)
    WITH (pages_per_range = 128);
CREATE INDEX idx_audit_log_subject ON access_audit_log(subject, timestamp DESC);
CREATE INDEX idx_audit_log_denied ON access_audit_log(effect, timestamp DESC)
    WHERE effect IN ('deny', 'default_deny');
CREATE INDEX idx_audit_log_source_component
    ON access_audit_log(source, component, timestamp DESC);

-- +goose Down

-- Revert totp, admin_approvals, plugins, content_items, aliases, access
-- policy, and access_audit_log timestamp columns from BIGINT (epoch
-- nanoseconds) back to TIMESTAMPTZ. PRECISION LOSS: sub-microsecond
-- nanoseconds are discarded by to_timestamp(col::double precision / 1e9).
--
-- Idempotent: guarded on data_type = 'bigint' so re-running is safe.

-- ═══ access_audit_log ═══
--
-- Same partition-key constraint as the up: PG forbids ALTER COLUMN TYPE on a
-- partition-key column. DROP + CREATE matches the up's recreation pattern.
-- For PRODUCTION rollback, operators must preserve audit data via a side
-- table copy BEFORE running this down migration.
-- DROP TABLE IF EXISTS + CREATE TABLE are inherently idempotent.

DROP TABLE IF EXISTS access_audit_log CASCADE;

-- Mirror the post-000005 shape (matches the up's recreation pattern).
CREATE TABLE access_audit_log (
    id               TEXT NOT NULL,
    timestamp        TIMESTAMPTZ NOT NULL DEFAULT now(),
    subject          TEXT NOT NULL,
    original_subject TEXT,
    action           TEXT NOT NULL,
    resource         TEXT NOT NULL,
    effect           TEXT NOT NULL CHECK (effect IN ('allow', 'deny', 'default_deny', 'system_bypass')),
    event_id         TEXT,
    event_name       TEXT,
    attributes       JSONB,
    error_message    TEXT,
    provider_errors  JSONB,
    duration_us      INTEGER,
    source           TEXT NOT NULL,
    component        TEXT NOT NULL,
    message          TEXT NOT NULL,
    PRIMARY KEY (id, timestamp)
) PARTITION BY RANGE (timestamp);

CREATE INDEX idx_audit_log_timestamp ON access_audit_log USING BRIN (timestamp)
    WITH (pages_per_range = 128);
CREATE INDEX idx_audit_log_subject ON access_audit_log(subject, timestamp DESC);
CREATE INDEX idx_audit_log_denied ON access_audit_log(effect, timestamp DESC)
    WHERE effect IN ('deny', 'default_deny');
CREATE INDEX idx_audit_log_source_component
    ON access_audit_log(source, component, timestamp DESC);

-- +goose StatementBegin
DO $$
BEGIN
  -- ═══ access_policy_versions ═══
  -- changed_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'access_policy_versions'
               AND column_name = 'changed_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE access_policy_versions ALTER COLUMN changed_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE access_policy_versions ALTER COLUMN changed_at TYPE TIMESTAMPTZ USING to_timestamp(changed_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE access_policy_versions ALTER COLUMN changed_at SET DEFAULT now()';
  END IF;

  -- ═══ access_policies ═══
  -- updated_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'access_policies'
               AND column_name = 'updated_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE access_policies ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE access_policies ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING to_timestamp(updated_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE access_policies ALTER COLUMN updated_at SET DEFAULT now()';
  END IF;

  -- created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'access_policies'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE access_policies ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE access_policies ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE access_policies ALTER COLUMN created_at SET DEFAULT now()';
  END IF;

  -- ═══ player_aliases ═══
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_aliases'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE player_aliases ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE player_aliases ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE player_aliases ALTER COLUMN created_at SET DEFAULT NOW()';
  END IF;

  -- ═══ system_aliases ═══
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'system_aliases'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE system_aliases ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE system_aliases ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE system_aliases ALTER COLUMN created_at SET DEFAULT NOW()';
  END IF;

  -- ═══ content_items ═══
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'content_items'
               AND column_name = 'updated_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE content_items ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE content_items ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING to_timestamp(updated_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE content_items ALTER COLUMN updated_at SET DEFAULT NOW()';
  END IF;

  -- ═══ plugins ═══
  -- gc_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'plugins'
               AND column_name = 'gc_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN gc_at TYPE TIMESTAMPTZ USING to_timestamp(gc_at::double precision / 1e9)';
  END IF;

  -- last_seen_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'plugins'
               AND column_name = 'last_seen_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN last_seen_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN last_seen_at TYPE TIMESTAMPTZ USING to_timestamp(last_seen_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN last_seen_at SET DEFAULT now()';
  END IF;

  -- first_seen_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'plugins'
               AND column_name = 'first_seen_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN first_seen_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN first_seen_at TYPE TIMESTAMPTZ USING to_timestamp(first_seen_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE plugins ALTER COLUMN first_seen_at SET DEFAULT now()';
  END IF;

  -- ═══ admin_approvals ═══
  -- created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'admin_approvals'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE admin_approvals ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE admin_approvals ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE admin_approvals ALTER COLUMN created_at SET DEFAULT now()';
  END IF;

  -- approved_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'admin_approvals'
               AND column_name = 'approved_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE admin_approvals ALTER COLUMN approved_at TYPE TIMESTAMPTZ USING to_timestamp(approved_at::double precision / 1e9)';
  END IF;

  -- expires_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'admin_approvals'
               AND column_name = 'expires_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE admin_approvals ALTER COLUMN expires_at TYPE TIMESTAMPTZ USING to_timestamp(expires_at::double precision / 1e9)';
  END IF;

  -- ═══ crypto_bootstrap_state ═══
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'crypto_bootstrap_state'
               AND column_name = 'consumed_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE crypto_bootstrap_state ALTER COLUMN consumed_at TYPE TIMESTAMPTZ USING to_timestamp(consumed_at::double precision / 1e9)';
  END IF;

  -- ═══ player_totp_recovery_codes ═══
  -- consumed_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_totp_recovery_codes'
               AND column_name = 'consumed_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE player_totp_recovery_codes ALTER COLUMN consumed_at TYPE TIMESTAMPTZ USING to_timestamp(consumed_at::double precision / 1e9)';
  END IF;

  -- created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_totp_recovery_codes'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE player_totp_recovery_codes ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE player_totp_recovery_codes ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE player_totp_recovery_codes ALTER COLUMN created_at SET DEFAULT NOW()';
  END IF;

  -- ═══ player_totp ═══
  -- locked_until
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_totp'
               AND column_name = 'locked_until' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE player_totp ALTER COLUMN locked_until TYPE TIMESTAMPTZ USING to_timestamp(locked_until::double precision / 1e9)';
  END IF;

  -- last_verified_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_totp'
               AND column_name = 'last_verified_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE player_totp ALTER COLUMN last_verified_at TYPE TIMESTAMPTZ USING to_timestamp(last_verified_at::double precision / 1e9)';
  END IF;

  -- enrolled_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_totp'
               AND column_name = 'enrolled_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE player_totp ALTER COLUMN enrolled_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE player_totp ALTER COLUMN enrolled_at TYPE TIMESTAMPTZ USING to_timestamp(enrolled_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE player_totp ALTER COLUMN enrolled_at SET DEFAULT NOW()';
  END IF;
END $$;
-- +goose StatementEnd
