-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse of 000001_baseline.up.sql — drops all tables in reverse dependency order.

DROP TABLE IF EXISTS entity_properties CASCADE;
DROP TABLE IF EXISTS player_aliases CASCADE;
DROP TABLE IF EXISTS system_aliases CASCADE;
DROP TABLE IF EXISTS content_items CASCADE;
DROP TABLE IF EXISTS access_audit_log CASCADE;
DROP TABLE IF EXISTS access_policy_versions CASCADE;
DROP TABLE IF EXISTS access_policies CASCADE;
DROP TABLE IF EXISTS events CASCADE;
DROP TABLE IF EXISTS session_connections CASCADE;
DROP TABLE IF EXISTS sessions CASCADE;
DROP TABLE IF EXISTS password_resets CASCADE;
DROP TABLE IF EXISTS player_sessions CASCADE;
DROP TABLE IF EXISTS scene_participants CASCADE;
DROP TABLE IF EXISTS objects CASCADE;
DROP TABLE IF EXISTS exits CASCADE;
DROP TABLE IF EXISTS locations CASCADE;
DROP TABLE IF EXISTS character_roles CASCADE;
DROP TABLE IF EXISTS characters CASCADE;
DROP TABLE IF EXISTS players CASCADE;
DROP TABLE IF EXISTS bootstrap_metadata CASCADE;
DROP TABLE IF EXISTS holomush_system_info CASCADE;

DROP EXTENSION IF EXISTS pg_stat_statements;
DROP EXTENSION IF EXISTS pg_trgm;
