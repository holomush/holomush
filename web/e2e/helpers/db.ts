// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import pg from 'pg';

// Set via E2E_DATABASE_URL in compose.e2e.yaml playwright service environment.
const DATABASE_URL = process.env.E2E_DATABASE_URL;
if (!DATABASE_URL) {
  throw new Error('E2E_DATABASE_URL environment variable is required');
}

let pool: pg.Pool | null = null;

function getPool(): pg.Pool {
  if (!pool) {
    pool = new pg.Pool({ connectionString: DATABASE_URL, max: 3 });
  }
  return pool;
}

export async function closePool(): Promise<void> {
  if (pool) {
    await pool.end();
    pool = null;
  }
}

// ── Player queries ──────────────────────────────────────────────

export interface DbPlayer {
  id: string;
  username: string;
  is_guest: boolean;
  created_at: Date;
}

export async function getPlayerByUsername(username: string): Promise<DbPlayer | null> {
  const { rows } = await getPool().query<DbPlayer>(
    'SELECT id, username, is_guest, created_at FROM players WHERE username = $1',
    [username],
  );
  return rows[0] ?? null;
}

export async function getPlayerByCharacterId(characterId: string): Promise<DbPlayer | null> {
  const { rows } = await getPool().query<DbPlayer>(
    `SELECT p.id, p.username, p.is_guest, p.created_at
     FROM players p JOIN characters c ON c.player_id = p.id
     WHERE c.id = $1`,
    [characterId],
  );
  return rows[0] ?? null;
}

// ── Player session queries ──────────────────────────────────────

export interface DbPlayerSession {
  id: string;
  player_id: string;
  token_hash: string;
  // BIGINT epoch-ns column (post-gfo6.16 Phase 2 migration). pg's node driver
  // returns BIGINT as string by default; tests that compare must parse.
  expires_at: string;
}

export async function getPlayerSessions(playerId: string): Promise<DbPlayerSession[]> {
  const { rows } = await getPool().query<DbPlayerSession>(
    // expires_at is BIGINT epoch-ns; compare against SQL-side ns-now to stay in the same type domain.
    `SELECT id, player_id, token_hash, expires_at FROM player_sessions
     WHERE player_id = $1 AND expires_at > (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT`,
    [playerId],
  );
  return rows;
}

// ── Character queries ───────────────────────────────────────────

export interface DbCharacter {
  id: string;
  player_id: string;
  name: string;
  location_id: string;
}

export async function getCharacterByName(name: string): Promise<DbCharacter | null> {
  const { rows } = await getPool().query<DbCharacter>(
    'SELECT id, player_id, name, location_id FROM characters WHERE name = $1',
    [name],
  );
  return rows[0] ?? null;
}

export async function getCharactersByPlayerId(playerId: string): Promise<DbCharacter[]> {
  const { rows } = await getPool().query<DbCharacter>(
    'SELECT id, player_id, name, location_id FROM characters WHERE player_id = $1',
    [playerId],
  );
  return rows;
}

// ── Session queries ─────────────────────────────────────────────

export interface DbSession {
  id: string;
  character_id: string;
  character_name: string;
  location_id: string;
  is_guest: boolean;
  status: string;
  grid_present: boolean;
}

export async function getSessionByCharacterId(characterId: string): Promise<DbSession | null> {
  const { rows } = await getPool().query<DbSession>(
    `SELECT id, character_id, character_name, location_id, is_guest, status, grid_present
     FROM sessions WHERE character_id = $1 AND status IN ('active', 'detached')
     ORDER BY created_at DESC LIMIT 1`,
    [characterId],
  );
  return rows[0] ?? null;
}

export async function getSessionById(sessionId: string): Promise<DbSession | null> {
  const { rows } = await getPool().query<DbSession>(
    `SELECT id, character_id, character_name, location_id, is_guest, status, grid_present
     FROM sessions WHERE id = $1`,
    [sessionId],
  );
  return rows[0] ?? null;
}

export async function getActiveSessionByCharacterName(
  characterName: string,
): Promise<DbSession | null> {
  const { rows } = await getPool().query<DbSession>(
    `SELECT id, character_id, character_name, location_id, is_guest, status, grid_present
     FROM sessions WHERE character_name = $1 AND status IN ('active', 'detached')
     ORDER BY created_at DESC LIMIT 1`,
    [characterName],
  );
  return rows[0] ?? null;
}

// ── Location queries ────────────────────────────────────────────

export interface DbLocation {
  id: string;
  name: string;
}

export async function getStartingLocation(): Promise<DbLocation | null> {
  // The setting_bootstrap_state table stores the starting location ID.
  // Migration 30 split bootstrap_metadata into auditchain-owned (chain_name, scope_key)
  // and migration 32 introduced setting_bootstrap_state as the (key, value) shape for
  // content/setting bootstrap state — including starting_location_id.
  const { rows } = await getPool().query<{ value: string }>(
    "SELECT value FROM setting_bootstrap_state WHERE key = 'starting_location_id'",
  );
  if (!rows[0]) return null;
  const locId = rows[0].value;
  const loc = await getPool().query<DbLocation>(
    'SELECT id, name FROM locations WHERE id = $1',
    [locId],
  );
  return loc.rows[0] ?? null;
}

// ── Event queries ──────────────────────────────────────────────

export interface DbEvent {
  id: string;
  stream: string;
  type: string;
  actor_id: string;
  payload: Record<string, unknown>;
  created_at: Date;
}

export async function getEventsByStream(stream: string): Promise<DbEvent[]> {
  const { rows } = await getPool().query<DbEvent>(
    'SELECT id, stream, type, actor_id, payload, created_at FROM events WHERE stream = $1 ORDER BY id',
    [stream],
  );
  return rows;
}

// DbAuditEvent mirrors the events_audit schema (internal/store/migrations/
// 000009_create_events_audit.up.sql + 000017_events_audit_envelope_rename).
// Post-F1 all published events land here via the host JetStream audit
// projection; the envelope column carries the marshaled Event proto
// envelope bytes (renamed from payload in Phase 3d migration 000017 to
// clarify pre-existing semantics — the column has always stored the full
// envelope, of which Event.payload is one nested field).
export interface DbAuditEvent {
  id: Buffer;
  subject: string;
  type: string;
  actor_kind: string;
  actor_id: Buffer | null;
  envelope: Buffer;
  timestamp: Date;
  codec: string;
}

// getAuditEventsBySubjectSuffix returns audit rows whose subject ends with
// the given suffix. F5 pre-cutover, say events are published to subjects
// like `events.<game>.location.<id>`; callers pass the legacy stream name
// (e.g. `location:<id>`) and this helper translates it to the JetStream
// dot-subject suffix. Equivalent to the old getEventsByStream but reads
// the audit tier rather than the now-empty events table.
export async function getAuditEventsBySubjectSuffix(
  legacyStream: string,
): Promise<DbAuditEvent[]> {
  // legacy "location:<id>" → dot-subject suffix ".location.<id>"
  const suffix = '.' + legacyStream.replace(/:/g, '.');
  const { rows } = await getPool().query<DbAuditEvent>(
    `SELECT id, subject, type, actor_kind, actor_id, envelope, timestamp, codec
       FROM events_audit
      WHERE subject LIKE $1
      ORDER BY id`,
    [`%${suffix}`],
  );
  return rows;
}

// ── Command history queries ────────────────────────────────────

export async function getCommandHistory(sessionId: string): Promise<string[]> {
  const { rows } = await getPool().query<{ command_history: string[] }>(
    'SELECT command_history FROM sessions WHERE id = $1',
    [sessionId],
  );
  return rows[0]?.command_history ?? [];
}

// ── Content item queries ───────────────────────────────────────

export interface DbContentItem {
  key: string;
  content_type: string;
  body: string;
  metadata: Record<string, unknown>;
}

export async function getContentItemsByPrefix(prefix: string): Promise<DbContentItem[]> {
  const { rows } = await getPool().query<DbContentItem>(
    `SELECT key, content_type, convert_from(body, 'UTF8') AS body, metadata
     FROM content_items WHERE key LIKE $1 ORDER BY key`,
    [`${prefix}%`],
  );
  return rows;
}

export async function getContentItem(key: string): Promise<DbContentItem | null> {
  const { rows } = await getPool().query<DbContentItem>(
    `SELECT key, content_type, convert_from(body, 'UTF8') AS body, metadata
     FROM content_items WHERE key = $1`,
    [key],
  );
  return rows[0] ?? null;
}

// ── Password hash queries ──────────────────────────────────────

export async function getPlayerPasswordHash(playerId: string): Promise<string | null> {
  const { rows } = await getPool().query<{ password_hash: string }>(
    'SELECT password_hash FROM players WHERE id = $1',
    [playerId],
  );
  return rows[0]?.password_hash ?? null;
}

// ── Role queries ───────────────────────────────────────────────

export async function grantAdminRole(characterId: string): Promise<void> {
  await getPool().query(
    `INSERT INTO character_roles (character_id, role) VALUES ($1, 'admin') ON CONFLICT DO NOTHING`,
    [characterId],
  );
}

// ── Scene queries ───────────────────────────────────────────────

export interface DbScene {
  id: string;
  title: string;
  description: string;
  owner_id: string;
  state: string;
  visibility: string;
  pose_order: string;
  location_id: string | null;
  created_at: Date;
  ended_at: Date | null;
  archived_at: Date | null;
}

/**
 * Fetch a scene row directly from the core-scenes plugin's Postgres schema.
 *
 * The plugin owns its own schema (plugin_core_scenes); cross-schema reads
 * from a privileged test role like the e2e test runner are permitted by
 * the underlying Postgres role. The plugin's restricted role only protects
 * writes from cross-plugin contamination, not reads from privileged callers.
 */
export async function getSceneById(sceneId: string): Promise<DbScene | null> {
  const { rows } = await getPool().query<DbScene>(
    `SELECT id, title, description, owner_id, state, visibility, pose_order,
            location_id, created_at, ended_at, archived_at
     FROM plugin_core_scenes.scenes
     WHERE id = $1`,
    [sceneId],
  );
  return rows[0] ?? null;
}

export async function getSceneByTitle(title: string): Promise<DbScene | null> {
  const { rows } = await getPool().query<DbScene>(
    `SELECT id, title, description, owner_id, state, visibility, pose_order,
            location_id, created_at, ended_at, archived_at
     FROM plugin_core_scenes.scenes
     WHERE title = $1
     ORDER BY created_at DESC
     LIMIT 1`,
    [title],
  );
  return rows[0] ?? null;
}

// ── Scene participant queries ───────────────────────────────────

export interface DbSceneParticipant {
  character_id: string;
  role: string;
}

/**
 * Returns all participant rows for a scene from the plugin_core_scenes schema.
 * Includes every role (owner, member, observer, invited) so callers can assert
 * membership state directly against the database without going through GetScene,
 * which only surfaces owner/member rows in its proto response.
 */
export async function getParticipantsBySceneId(sceneId: string): Promise<DbSceneParticipant[]> {
  const { rows } = await getPool().query<DbSceneParticipant>(
    `SELECT character_id, role
     FROM plugin_core_scenes.scene_participants
     WHERE scene_id = $1`,
    [sceneId],
  );
  return rows;
}

// ── Zero ULID check ─────────────────────────────────────────────

const ZERO_ULID = '00000000000000000000000000';

export function isValidLocationId(locationId: string): boolean {
  return !!locationId && locationId !== ZERO_ULID && locationId.length === 26;
}
