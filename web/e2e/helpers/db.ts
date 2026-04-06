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
  expires_at: Date;
}

export async function getPlayerSessions(playerId: string): Promise<DbPlayerSession[]> {
  const { rows } = await getPool().query<DbPlayerSession>(
    'SELECT id, player_id, token_hash, expires_at FROM player_sessions WHERE player_id = $1 AND expires_at > now()',
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
  // The bootstrap metadata table stores the starting location ID
  const { rows } = await getPool().query<{ value: string }>(
    "SELECT value FROM bootstrap_metadata WHERE key = 'starting_location_id'",
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

// ── Zero ULID check ─────────────────────────────────────────────

const ZERO_ULID = '00000000000000000000000000';

export function isValidLocationId(locationId: string): boolean {
  return !!locationId && locationId !== ZERO_ULID && locationId.length === 26;
}

// ── Channel queries (plugin_core_channels schema) ──────────────

const CHANNEL_SCHEMA = 'plugin_core_channels';

export interface DbChannel {
  id: string;
  name: string;
  type: string;
  description: string;
  owner_id: string;
  created_at: Date;
  archived_at: Date | null;
}

export async function getChannelByName(name: string): Promise<DbChannel | null> {
  const { rows } = await getPool().query<DbChannel>(
    `SELECT id, name, type, description, owner_id, created_at, archived_at
     FROM ${CHANNEL_SCHEMA}.channels WHERE lower(name) = lower($1)`,
    [name],
  );
  return rows[0] ?? null;
}

export interface DbChannelMembership {
  channel_id: string;
  player_id: string;
  role: string;
  joined_at: Date;
}

export async function getChannelMemberships(channelId: string): Promise<DbChannelMembership[]> {
  const { rows } = await getPool().query<DbChannelMembership>(
    `SELECT channel_id, player_id, role, joined_at
     FROM ${CHANNEL_SCHEMA}.channel_memberships WHERE channel_id = $1 ORDER BY joined_at`,
    [channelId],
  );
  return rows;
}

export async function getChannelMembership(
  channelId: string,
  playerId: string,
): Promise<DbChannelMembership | null> {
  const { rows } = await getPool().query<DbChannelMembership>(
    `SELECT channel_id, player_id, role, joined_at
     FROM ${CHANNEL_SCHEMA}.channel_memberships WHERE channel_id = $1 AND player_id = $2`,
    [channelId, playerId],
  );
  return rows[0] ?? null;
}

export interface DbChannelMessage {
  id: string;
  channel_id: string;
  author_id: string;
  author_name: string;
  message: string;
  event_type: string;
  source: string;
  created_at: Date;
}

export async function getChannelMessages(channelId: string): Promise<DbChannelMessage[]> {
  const { rows } = await getPool().query<DbChannelMessage>(
    `SELECT id, channel_id, author_id, author_name, message, event_type, source, created_at
     FROM ${CHANNEL_SCHEMA}.channel_messages WHERE channel_id = $1 ORDER BY created_at`,
    [channelId],
  );
  return rows;
}
