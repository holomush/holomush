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
  created_at: Date;
}

export async function getPlayerByUsername(username: string): Promise<DbPlayer | null> {
  const { rows } = await getPool().query<DbPlayer>(
    'SELECT id, username, created_at FROM players WHERE username = $1',
    [username],
  );
  return rows[0] ?? null;
}

// ── Player token queries ────────────────────────────────────────

export interface DbPlayerToken {
  token: string;
  player_id: string;
  expires_at: Date;
}

export async function getPlayerTokens(playerId: string): Promise<DbPlayerToken[]> {
  const { rows } = await getPool().query<DbPlayerToken>(
    'SELECT token, player_id, expires_at FROM player_tokens WHERE player_id = $1 AND expires_at > now()',
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

// ── Zero ULID check ─────────────────────────────────────────────

const ZERO_ULID = '00000000000000000000000000';

export function isValidLocationId(locationId: string): boolean {
  return !!locationId && locationId !== ZERO_ULID && locationId.length === 26;
}
