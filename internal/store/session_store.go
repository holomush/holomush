// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/session"
)

// PostgresSessionStore implements session.Store using PostgreSQL.
type PostgresSessionStore struct {
	pool poolIface
}

// NewPostgresSessionStore creates a new Postgres-backed session store.
func NewPostgresSessionStore(pool poolIface) *PostgresSessionStore {
	return &PostgresSessionStore{
		pool: pool,
	}
}

// compile-time checks
var (
	_ session.Store  = (*PostgresSessionStore)(nil)
	_ session.Access = (*PostgresSessionStore)(nil)
)

const sessionSelectColumns = `id, character_id, player_id,
	COALESCE(player_session_id, '') AS player_session_id,
	character_name, location_id,
	location_arrived_at,
	guest_character_created_at,
	is_guest, status, grid_present,
	command_history, ttl_seconds, max_history,
	detached_at, expires_at, created_at, updated_at,
	last_paged, last_whispered,
	focus_memberships, presenting_focus`

// parseSessionRow parses the scalar fields scanned from a session row into a
// session.Info. Both scanSession and scanSessions call Scan with the same
// variable set and then delegate here to avoid duplicating the parse logic.
func parseSessionRow(info *session.Info, charIDStr, playerIDStr, playerSessionIDStr, locIDStr, statusStr string, focusMembershipsJSON, presentingFocusJSON []byte) error {
	charID, err := ulid.Parse(charIDStr)
	if err != nil {
		return oops.With("operation", "parse character_id").With("raw_id", charIDStr).Wrap(err)
	}
	info.CharacterID = charID

	// PlayerID may be empty for legacy sessions created before this column existed.
	if playerIDStr != "" {
		playerID, parseErr := ulid.Parse(playerIDStr)
		if parseErr != nil {
			return oops.With("operation", "parse player_id").With("raw_id", playerIDStr).Wrap(parseErr)
		}
		info.PlayerID = playerID
	}

	// PlayerSessionID is NULL for legacy sessions created before the
	// player_session_id column existed. COALESCE in the SELECT maps NULL
	// to the empty string, which we parse as the zero ULID.
	if playerSessionIDStr != "" {
		playerSessionID, parseErr := ulid.Parse(playerSessionIDStr)
		if parseErr != nil {
			return oops.With("operation", "parse player_session_id").With("raw_id", playerSessionIDStr).Wrap(parseErr)
		}
		info.PlayerSessionID = playerSessionID
	}

	locID, err := ulid.Parse(locIDStr)
	if err != nil {
		return oops.With("operation", "parse location_id").With("raw_id", locIDStr).Wrap(err)
	}
	info.LocationID = locID

	status := session.Status(statusStr)
	if !status.IsValid() {
		return oops.With("operation", "validate status").With("status", statusStr).
			Errorf("unknown session status %q", statusStr)
	}
	info.Status = status

	if len(focusMembershipsJSON) > 0 {
		var memberships []session.FocusMembership
		if err := json.Unmarshal(focusMembershipsJSON, &memberships); err != nil {
			return oops.With("operation", "unmarshal focus_memberships").Wrap(err)
		}
		info.FocusMemberships = memberships
	}

	if len(presentingFocusJSON) > 0 {
		var key session.FocusKey
		if err := json.Unmarshal(presentingFocusJSON, &key); err != nil {
			return oops.With("operation", "unmarshal presenting_focus").Wrap(err)
		}
		info.PresentingFocus = &key
	}

	return nil
}

// scanSession scans a pgx.Row into a session.Info.
func scanSession(row pgx.Row) (*session.Info, error) {
	var info session.Info
	var charIDStr, playerIDStr, playerSessionIDStr, locIDStr, statusStr string
	var focusMembershipsJSON, presentingFocusJSON []byte
	var (
		createdAt, updatedAt, arrived, guestCreated pgnanos.Time
		detachedAt, expiresAt                       *pgnanos.Time
	)

	err := row.Scan(
		&info.ID,
		&charIDStr,
		&playerIDStr,
		&playerSessionIDStr,
		&info.CharacterName,
		&locIDStr,
		&arrived,
		&guestCreated,
		&info.IsGuest,
		&statusStr,
		&info.GridPresent,
		&info.CommandHistory,
		&info.TTLSeconds,
		&info.MaxHistory,
		&detachedAt,
		&expiresAt,
		&createdAt,
		&updatedAt,
		&info.LastPaged,
		&info.LastWhispered,
		&focusMembershipsJSON,
		&presentingFocusJSON,
	)
	if err != nil {
		return nil, oops.With("operation", "scan session row").Wrap(err)
	}

	info.CreatedAt = createdAt.Time()
	info.UpdatedAt = updatedAt.Time()
	if detachedAt != nil {
		t := detachedAt.Time()
		info.DetachedAt = &t
	}
	if expiresAt != nil {
		t := expiresAt.Time()
		info.ExpiresAt = &t
	}
	info.LocationArrivedAt = arrived.Time()
	info.GuestCharacterCreatedAt = guestCreated.Time()

	if err := parseSessionRow(&info, charIDStr, playerIDStr, playerSessionIDStr, locIDStr, statusStr, focusMembershipsJSON, presentingFocusJSON); err != nil {
		return nil, err
	}

	return &info, nil
}

// scanSessions scans pgx.Rows into a slice of session.Info.
func scanSessions(rows pgx.Rows) ([]*session.Info, error) {
	defer rows.Close()
	var result []*session.Info
	for rows.Next() {
		var info session.Info
		var charIDStr, playerIDStr, playerSessionIDStr, locIDStr, statusStr string
		var focusMembershipsJSON, presentingFocusJSON []byte
		var (
			createdAt, updatedAt, arrived, guestCreated pgnanos.Time
			detachedAt, expiresAt                       *pgnanos.Time
		)

		err := rows.Scan(
			&info.ID,
			&charIDStr,
			&playerIDStr,
			&playerSessionIDStr,
			&info.CharacterName,
			&locIDStr,
			&arrived,
			&guestCreated,
			&info.IsGuest,
			&statusStr,
			&info.GridPresent,
			&info.CommandHistory,
			&info.TTLSeconds,
			&info.MaxHistory,
			&detachedAt,
			&expiresAt,
			&createdAt,
			&updatedAt,
			&info.LastPaged,
			&info.LastWhispered,
			&focusMembershipsJSON,
			&presentingFocusJSON,
		)
		if err != nil {
			return nil, oops.With("operation", "scan session row").Wrap(err)
		}

		info.CreatedAt = createdAt.Time()
		info.UpdatedAt = updatedAt.Time()
		if detachedAt != nil {
			t := detachedAt.Time()
			info.DetachedAt = &t
		}
		if expiresAt != nil {
			t := expiresAt.Time()
			info.ExpiresAt = &t
		}
		info.LocationArrivedAt = arrived.Time()
		info.GuestCharacterCreatedAt = guestCreated.Time()

		if err := parseSessionRow(&info, charIDStr, playerIDStr, playerSessionIDStr, locIDStr, statusStr, focusMembershipsJSON, presentingFocusJSON); err != nil {
			return nil, err
		}

		result = append(result, &info)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate session rows").Wrap(err)
	}
	return result, nil
}

// Get retrieves a session by ID.
func (s *PostgresSessionStore) Get(ctx context.Context, id string) (*session.Info, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+sessionSelectColumns+` FROM sessions WHERE id = $1`, id)

	info, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("SESSION_NOT_FOUND").With("session_id", id).Wrap(err)
	}
	if err != nil {
		return nil, oops.With("operation", "get session").With("session_id", id).Wrap(err)
	}
	return info, nil
}

// Set creates or updates a session.
func (s *PostgresSessionStore) Set(ctx context.Context, id string, info *session.Info) error {
	// F6: event_cursors column dropped (migration 000010); no longer persisted.

	// Coerce nil slice to empty — pgx sends nil Go slices as SQL NULL,
	// which violates the NOT NULL constraint on command_history.
	cmdHistory := info.CommandHistory
	if cmdHistory == nil {
		cmdHistory = []string{}
	}

	focusMembershipsJSON, err := json.Marshal(info.FocusMemberships)
	if err != nil {
		return oops.With("operation", "marshal focus_memberships").With("session_id", id).Wrap(err)
	}

	var presentingFocusJSON []byte
	if info.PresentingFocus != nil {
		presentingFocusJSON, err = json.Marshal(info.PresentingFocus)
		if err != nil {
			return oops.With("operation", "marshal presenting_focus").With("session_id", id).Wrap(err)
		}
	}

	// player_session_id is nullable so that legacy sessions (created before
	// the FK was added) do not get zero-ULID strings written to the column.
	// Pass NULL when the field is the zero ULID; otherwise pass the string.
	var playerSessionIDArg any
	if info.PlayerSessionID.Compare(ulid.ULID{}) != 0 {
		playerSessionIDArg = info.PlayerSessionID.String()
	}

	// location_arrived_at and guest_character_created_at are NOT NULL
	// (migration 040: DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT and DEFAULT 0).
	// Pass NULL from Go when the in-memory Info carries a zero-time and let SQL handle
	// it in two distinct ways:
	//
	//   - On INSERT: COALESCE(NULL, SQL-default) → migration default fires.
	//   - On ON CONFLICT UPDATE: COALESCE(NULL, sessions.X) → existing
	//     stored value is preserved, so a Set() that does not own the
	//     timestamp (e.g., a pre-iwzt-T3 caller) does NOT clobber it.
	//
	// The ON CONFLICT clause references the raw $7/$8 parameters rather
	// than EXCLUDED.X so it can see "input was zero" (NULL) instead of
	// the COALESCEd VALUES result.
	var locationArrivedAtArg *pgnanos.Time
	if !info.LocationArrivedAt.IsZero() {
		t := pgnanos.From(info.LocationArrivedAt)
		locationArrivedAtArg = &t
	}
	var guestCharacterCreatedAtArg *pgnanos.Time
	if !info.GuestCharacterCreatedAt.IsZero() {
		t := pgnanos.From(info.GuestCharacterCreatedAt)
		guestCharacterCreatedAtArg = &t
	}

	var detachedAtArg *pgnanos.Time
	if info.DetachedAt != nil {
		t := pgnanos.From(*info.DetachedAt)
		detachedAtArg = &t
	}
	var expiresAtArg *pgnanos.Time
	if info.ExpiresAt != nil {
		t := pgnanos.From(*info.ExpiresAt)
		expiresAtArg = &t
	}

	_, err = s.pool.Exec(
		ctx,
		`INSERT INTO sessions (id, character_id, player_id, player_session_id,
			character_name, location_id, location_arrived_at, guest_character_created_at,
			is_guest, status, grid_present,
			command_history, ttl_seconds, max_history,
			detached_at, expires_at, created_at,
			last_paged, last_whispered,
			focus_memberships, presenting_focus)
		 VALUES ($1, $2, $3, $4, $5, $6,
			COALESCE($7::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT),
			COALESCE($8::BIGINT, 0::BIGINT),
			$9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20::jsonb, $21::jsonb)
		 ON CONFLICT (id) DO UPDATE SET
			character_id = EXCLUDED.character_id,
			player_id = EXCLUDED.player_id,
			player_session_id = EXCLUDED.player_session_id,
			character_name = EXCLUDED.character_name,
			location_id = EXCLUDED.location_id,
			location_arrived_at = COALESCE($7::BIGINT, sessions.location_arrived_at),
			guest_character_created_at = COALESCE($8::BIGINT, sessions.guest_character_created_at),
			is_guest = EXCLUDED.is_guest,
			status = EXCLUDED.status,
			grid_present = EXCLUDED.grid_present,
			command_history = EXCLUDED.command_history,
			ttl_seconds = EXCLUDED.ttl_seconds,
			max_history = EXCLUDED.max_history,
			detached_at = EXCLUDED.detached_at,
			expires_at = EXCLUDED.expires_at,
			last_paged = EXCLUDED.last_paged,
			last_whispered = EXCLUDED.last_whispered,
			focus_memberships = EXCLUDED.focus_memberships,
			presenting_focus = EXCLUDED.presenting_focus,
			updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT`,
		id,
		info.CharacterID.String(),
		info.PlayerID.String(),
		playerSessionIDArg,
		info.CharacterName,
		info.LocationID.String(),
		locationArrivedAtArg,
		guestCharacterCreatedAtArg,
		info.IsGuest,
		string(info.Status),
		info.GridPresent,
		cmdHistory,
		info.TTLSeconds,
		info.MaxHistory,
		detachedAtArg,
		expiresAtArg,
		pgnanos.From(info.CreatedAt),
		info.LastPaged,
		info.LastWhispered,
		focusMembershipsJSON,
		presentingFocusJSON,
	)
	if err != nil {
		return oops.With("operation", "set session").With("session_id", id).Wrap(err)
	}
	return nil
}

// Delete removes a session.
func (s *PostgresSessionStore) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	if err != nil {
		return oops.With("operation", "delete session").With("session_id", id).Wrap(err)
	}
	return nil
}

// FindByCharacter returns the active or detached session for a character.
func (s *PostgresSessionStore) FindByCharacter(ctx context.Context, characterID ulid.ULID) (*session.Info, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+sessionSelectColumns+` FROM sessions WHERE character_id = $1 AND status IN ('active', 'detached') LIMIT 1`,
		characterID.String())

	info, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("SESSION_NOT_FOUND").With("character_id", characterID.String()).Wrap(err)
	}
	if err != nil {
		return nil, oops.With("operation", "find session by character").With("character_id", characterID.String()).Wrap(err)
	}
	return info, nil
}

// ListByPlayer returns all non-expired sessions for a player's characters.
func (s *PostgresSessionStore) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*session.Info, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+sessionSelectColumns+` FROM sessions WHERE player_id = $1 AND status != 'expired'`,
		playerID.String())
	if err != nil {
		return nil, oops.With("operation", "list sessions by player").With("player_id", playerID.String()).Wrap(err)
	}
	return scanSessions(rows)
}

// ListByPlayerSession returns all active/detached sessions whose player_session_id
// matches any of the given IDs.
func (s *PostgresSessionStore) ListByPlayerSession(
	ctx context.Context,
	playerSessionIDs []ulid.ULID,
) ([]*session.Info, error) {
	if len(playerSessionIDs) == 0 {
		return nil, nil
	}

	ids := make([]string, len(playerSessionIDs))
	for i, id := range playerSessionIDs {
		ids[i] = id.String()
	}

	rows, err := s.pool.Query(ctx,
		`SELECT `+sessionSelectColumns+`
		 FROM sessions
		 WHERE player_session_id = ANY($1)
		   AND status != 'expired'`,
		ids)
	if err != nil {
		return nil, oops.Code("LIST_BY_PLAYER_SESSION_FAILED").
			With("operation", "list sessions by player session").
			Wrap(err)
	}
	return scanSessions(rows)
}

// ListExpired returns all sessions past their expiry time.
func (s *PostgresSessionStore) ListExpired(ctx context.Context) ([]*session.Info, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+sessionSelectColumns+` FROM sessions WHERE status = 'detached' AND expires_at < (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT`)
	if err != nil {
		return nil, oops.With("operation", "list expired sessions").Wrap(err)
	}
	return scanSessions(rows)
}

// UpdateStatus transitions a session's status.
func (s *PostgresSessionStore) UpdateStatus(ctx context.Context, id string, status session.Status,
	detachedAt *time.Time, expiresAt *time.Time,
) error {
	var detachedNanos, expiresNanos *pgnanos.Time
	if detachedAt != nil {
		t := pgnanos.From(*detachedAt)
		detachedNanos = &t
	}
	if expiresAt != nil {
		t := pgnanos.From(*expiresAt)
		expiresNanos = &t
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET status = $1, detached_at = $2, expires_at = $3, updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE id = $4`,
		string(status), detachedNanos, expiresNanos, id)
	if err != nil {
		return oops.With("operation", "update session status").With("session_id", id).With("status", string(status)).Wrap(err)
	}
	return nil
}

// ReattachCAS atomically transitions a detached session to active.
// Returns true if the row was updated, false if another client won the race.
func (s *PostgresSessionStore) ReattachCAS(ctx context.Context, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE sessions SET status = 'active', detached_at = NULL, expires_at = NULL, updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE id = $1 AND status = 'detached'`,
		id)
	if err != nil {
		return false, oops.With("operation", "reattach session").With("session_id", id).Wrap(err)
	}
	return tag.RowsAffected() == 1, nil
}

// AppendCommand adds a command to the session's history, enforcing the cap.
func (s *PostgresSessionStore) AppendCommand(ctx context.Context, id, command string, maxHistory int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET command_history = (command_history || ARRAY[$1::text])[
			GREATEST(1, array_length(command_history || ARRAY[$1::text], 1) - $2 + 1) :
		], updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE id = $3`,
		command, maxHistory, id)
	if err != nil {
		return oops.With("operation", "append command").With("session_id", id).Wrap(err)
	}
	return nil
}

// GetCommandHistory returns the session's command history.
func (s *PostgresSessionStore) GetCommandHistory(ctx context.Context, id string) ([]string, error) {
	var history []string
	err := s.pool.QueryRow(ctx,
		`SELECT command_history FROM sessions WHERE id = $1`, id).Scan(&history)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("SESSION_NOT_FOUND").With("session_id", id).Wrap(err)
	}
	if err != nil {
		return nil, oops.With("operation", "get command history").With("session_id", id).Wrap(err)
	}
	return history, nil
}

// validClientTypes is the set of allowed client_type values.
var validClientTypes = map[string]bool{
	"terminal":  true,
	"comms_hub": true,
	"telnet":    true,
}

// AddConnection registers a new connection to a session.
func (s *PostgresSessionStore) AddConnection(ctx context.Context, conn *session.Connection) error {
	if !validClientTypes[conn.ClientType] {
		return oops.With("operation", "add connection").
			With("client_type", conn.ClientType).
			Code("INVALID_CLIENT_TYPE").
			Errorf("invalid client_type %q: must be one of terminal, comms_hub, telnet", conn.ClientType)
	}

	// DRIFT FIX (holomush-9mxr Task 9): nil []string and []string{} are
	// semantically equivalent ("no streams subscribed"), but the column is
	// TEXT[] NOT NULL so nil is rejected by Postgres. Coerce nil to an
	// empty slice so callers need not distinguish the two zero values.
	streams := conn.Streams
	if streams == nil {
		streams = []string{}
	}

	// DRIFT FIX (holomush-9mxr Task 9): The former in-memory store's AddConnection stored the
	// full Connection including FocusKey. PostgresSessionStore silently
	// dropped the initial FocusKey, causing tests that seed a connection
	// with a pre-existing FocusKey (e.g. TestSetConnectionFocus_HappyPath_
	// ReturnsOldFocusKey) to see nil instead of the seeded value. Marshal
	// focus_key as JSONB on insert, matching the shape used by
	// UpdateConnectionFocusKey.
	var focusKeyJSON []byte
	if conn.FocusKey != nil {
		var merr error
		focusKeyJSON, merr = json.Marshal(conn.FocusKey)
		if merr != nil {
			return oops.With("operation", "marshal initial focus_key").
				With("connection_id", conn.ID.String()).Wrap(merr)
		}
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO session_connections (id, session_id, client_type, streams, focus_key, connected_at, last_seen_at)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6, $6)`,
		conn.ID.String(), conn.SessionID, conn.ClientType, streams, focusKeyJSON, pgnanos.From(conn.ConnectedAt))
	if err != nil {
		return oops.With("operation", "add connection").
			With("connection_id", conn.ID.String()).
			With("session_id", conn.SessionID).Wrap(err)
	}
	return nil
}

// RemoveConnection removes a connection from a session.
func (s *PostgresSessionStore) RemoveConnection(ctx context.Context, connectionID ulid.ULID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM session_connections WHERE id = $1`, connectionID.String())
	if err != nil {
		return oops.With("operation", "remove connection").With("connection_id", connectionID.String()).Wrap(err)
	}
	return nil
}

// RemoveConnectionAndCount removes a single connection and reports the
// session's remaining connection counts plus whether a row was actually
// deleted, all inside one transaction. See the session.Store interface doc for
// the contract (holomush-cizj). The sessions row is locked FOR UPDATE first
// (D11 / INV-SCENE-27 canonical order), which serializes concurrent disconnects
// on the same session: each caller's post-delete COUNT then reflects every
// earlier-committed removal, so exactly one observes Total==0 and runs cleanup.
// A plain DELETE...RETURNING count would NOT suffice — under READ COMMITTED two
// concurrent deletes are mutually invisible, so both could read Total==1 and
// skip cleanup entirely. The returned bool (RowsAffected > 0) lets the caller
// distinguish the removal that drove the count from a duplicate no-op.
func (s *PostgresSessionStore) RemoveConnectionAndCount(
	ctx context.Context,
	sessionID string,
	connectionID ulid.ULID,
) (session.ConnectionCounts, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return session.ConnectionCounts{}, false, oops.With("operation", "begin tx for remove connection and count").
			With("session_id", sessionID).Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// D11 canonical lock order: sessions row FOR UPDATE first. A missing
	// session is not an error — Disconnect is idempotent and the row may
	// already be gone; with no session there is nothing to remove and no
	// connections remain.
	var ignored string
	err = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE id = $1 FOR UPDATE`, sessionID).Scan(&ignored)
	if errors.Is(err, pgx.ErrNoRows) {
		return session.ConnectionCounts{}, false, nil
	}
	if err != nil {
		return session.ConnectionCounts{}, false, oops.With("operation", "lock session for connection removal").
			With("session_id", sessionID).Wrap(err)
	}

	// Delete the named connection, scoped by session_id so a foreign
	// connection ULID cannot be removed under this session (I-SEC-1). A
	// missing connection affects zero rows — idempotent; the RowsAffected
	// count is the "this call mutated state" signal returned below.
	tag, err := tx.Exec(ctx,
		`DELETE FROM session_connections WHERE id = $1 AND session_id = $2`,
		connectionID.String(), sessionID)
	if err != nil {
		return session.ConnectionCounts{}, false, oops.With("operation", "remove connection").
			With("connection_id", connectionID.String()).
			With("session_id", sessionID).Wrap(err)
	}
	removed := tag.RowsAffected() > 0

	// Post-delete counts within the same locked transaction. Grid =
	// terminal + telnet (comms_hub excluded), matching the grid-presence
	// definition used by Disconnect and recomputeSessionLiveness.
	var counts session.ConnectionCounts
	if err = tx.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE client_type IN ('terminal', 'telnet'))
		FROM session_connections WHERE session_id = $1
	`, sessionID).Scan(&counts.Total, &counts.Grid); err != nil {
		return session.ConnectionCounts{}, false, oops.With("operation", "count connections after removal").
			With("session_id", sessionID).Wrap(err)
	}

	if err = tx.Commit(ctx); err != nil {
		return session.ConnectionCounts{}, false, oops.With("operation", "commit remove connection and count").
			With("session_id", sessionID).Wrap(err)
	}
	return counts, removed, nil
}

// RefreshConnection bumps a connection's lease to now (holomush-rsoe6, I-LIVE-2).
// The UPDATE is scoped by both id AND session_id so a connection can only be
// refreshed by its owning session; pairing a foreign connection ULID with a
// caller-controlled session affects zero rows (I-SEC-1).
func (s *PostgresSessionStore) RefreshConnection(ctx context.Context, connectionID ulid.ULID, sessionID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE session_connections SET last_seen_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE id = $1 AND session_id = $2`,
		connectionID.String(), sessionID)
	if err != nil {
		return oops.With("operation", "refresh connection").
			With("connection_id", connectionID.String()).
			With("session_id", sessionID).Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		// Absent OR owned by another session — indistinguishable on purpose.
		return oops.Code("CONNECTION_NOT_FOUND").
			With("connection_id", connectionID.String()).
			With("session_id", sessionID).
			Errorf("connection not found")
	}
	return nil
}

// ListLapsedConnections returns connections whose lease is older than olderThan
// (i.e. last_seen_at < olderThan). Used by the lease sweep (holomush-rsoe6, I-LIVE-2).
func (s *PostgresSessionStore) ListLapsedConnections(ctx context.Context, olderThan time.Time) ([]session.LapsedConnection, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, session_id, client_type FROM session_connections WHERE last_seen_at < $1`,
		pgnanos.From(olderThan))
	if err != nil {
		return nil, oops.With("operation", "list lapsed connections").Wrap(err)
	}
	defer rows.Close()
	var out []session.LapsedConnection
	for rows.Next() {
		var idStr string
		var lc session.LapsedConnection
		if scanErr := rows.Scan(&idStr, &lc.SessionID, &lc.ClientType); scanErr != nil {
			return nil, oops.With("operation", "scan lapsed connection").Wrap(scanErr)
		}
		id, parseErr := ulid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.With("operation", "parse lapsed connection id").With("id", idStr).Wrap(parseErr)
		}
		lc.ID = id
		out = append(out, lc)
	}
	return out, oops.Wrap(rows.Err())
}

// CountConnections returns the number of active connections for a session.
func (s *PostgresSessionStore) CountConnections(ctx context.Context, sessionID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM session_connections WHERE session_id = $1`, sessionID).Scan(&count)
	if err != nil {
		return 0, oops.With("operation", "count connections").With("session_id", sessionID).Wrap(err)
	}
	return count, nil
}

// CountConnectionsByType returns the number of active connections of a
// specific client type for a session.
func (s *PostgresSessionStore) CountConnectionsByType(ctx context.Context, sessionID, clientType string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM session_connections WHERE session_id = $1 AND client_type = $2`,
		sessionID, clientType).Scan(&count)
	if err != nil {
		return 0, oops.With("operation", "count connections by type").
			With("session_id", sessionID).
			With("client_type", clientType).Wrap(err)
	}
	return count, nil
}

// UpdateGridPresent sets the grid_present flag on a session.
func (s *PostgresSessionStore) UpdateGridPresent(ctx context.Context, id string, present bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET grid_present = $2, updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE id = $1`,
		id, present)
	if err != nil {
		return oops.With("operation", "update grid present").
			With("session_id", id).Wrap(err)
	}
	return nil
}

// ListActiveByLocation returns active sessions whose location_id matches and
// that have at least one live terminal or telnet connection. The grid_present
// flag (belt-and-suspenders: set reactively by the reaper) is retained as a
// fast-path filter; the EXISTS predicate is the authoritative presence gate
// that excludes comms_hub-only sessions even before the reaper runs
// (holomush-5rh.8.9).
func (s *PostgresSessionStore) ListActiveByLocation(ctx context.Context, locationID ulid.ULID) ([]*session.Info, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+sessionSelectColumns+` FROM sessions `+
			`WHERE location_id = $1 AND status = 'active' AND grid_present = true `+
			`AND EXISTS (`+
			`SELECT 1 FROM session_connections c `+
			`WHERE c.session_id = sessions.id `+
			`AND c.client_type IN ('terminal', 'telnet'))`,
		locationID.String())
	if err != nil {
		return nil, oops.With("operation", "list active by location").
			With("location_id", locationID.String()).Wrap(err)
	}
	return scanSessions(rows)
}

// ListByFocus returns all non-expired sessions whose focus_memberships
// JSONB array contains an entry matching the given target. Uses PostgreSQL's
// JSONB containment operator (@>), which does partial-object matching: the
// needle only declares Kind and TargetID; JoinedAt on stored rows is ignored.
//
// The needle is built via json.Marshal so field casing and escaping match
// exactly how session.FocusMembership values are serialized on write.
//
// Performance note: focus_memberships has NO GIN index as of migration
// 000006; this query is a sequential scan on the sessions table. That is
// acceptable at current session cardinality (hundreds to low thousands
// of concurrent sessions); if the table grows significantly, add a GIN
// index on focus_memberships with jsonb_path_ops. Concurrent sweep
// callers will not block each other since the scan is read-only.
func (s *PostgresSessionStore) ListByFocus(ctx context.Context, target session.FocusKey) ([]*session.Info, error) {
	needle, err := json.Marshal([]struct {
		Kind     session.FocusKind `json:"Kind"`
		TargetID string            `json:"TargetID"`
	}{{Kind: target.Kind, TargetID: target.TargetID.String()}})
	if err != nil {
		return nil, oops.With("operation", "marshal focus needle").
			With("focus_kind", string(target.Kind)).
			With("target_id", target.TargetID.String()).Wrap(err)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+sessionSelectColumns+`
		 FROM sessions
		 WHERE status <> 'expired' AND focus_memberships @> $1::jsonb
		 ORDER BY created_at`,
		needle)
	if err != nil {
		return nil, oops.With("operation", "list by focus").
			With("focus_kind", string(target.Kind)).
			With("target_id", target.TargetID.String()).Wrap(err)
	}
	return scanSessions(rows)
}

// ListActive returns all sessions with status=active.
func (s *PostgresSessionStore) ListActive(ctx context.Context) ([]*session.Info, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+sessionSelectColumns+` FROM sessions WHERE status = 'active' ORDER BY created_at`)
	if err != nil {
		return nil, oops.With("operation", "list active sessions").Wrap(err)
	}
	return scanSessions(rows)
}

// DeleteByCharacter atomically deletes the active or detached session for a
// character using a single DELETE ... RETURNING query, eliminating the TOCTOU
// race of FindByCharacter + Delete. Returns (nil, nil) when no matching
// session exists.
func (s *PostgresSessionStore) DeleteByCharacter(ctx context.Context, characterID ulid.ULID) (*session.Info, error) {
	query := `DELETE FROM sessions WHERE character_id = $1 AND status IN ('active', 'detached')
		RETURNING ` + sessionSelectColumns
	row := s.pool.QueryRow(ctx, query, characterID.String())
	info, err := scanSession(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, oops.With("operation", "delete_by_character").
			With("character_id", characterID.String()).Wrap(err)
	}

	return info, nil
}

// UpdateActivity bumps the updated_at timestamp for a session.
func (s *PostgresSessionStore) UpdateActivity(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE id = $1`, id)
	if err != nil {
		return oops.With("operation", "update activity").With("session_id", id).Wrap(err)
	}
	return nil
}

// FindByCharacterName returns the active session for a character by name.
// The lookup is case-insensitive.
func (s *PostgresSessionStore) FindByCharacterName(ctx context.Context, name string) (*session.Info, error) {
	query := `SELECT ` + sessionSelectColumns + ` FROM sessions WHERE LOWER(character_name) = LOWER($1) AND status = 'active'`
	row := s.pool.QueryRow(ctx, query, name)
	info, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, oops.With("operation", "find session by character name").With("character_name", name).Wrap(err)
	}
	return info, nil
}

// UpdateLastPaged records the name of the character most recently paged.
func (s *PostgresSessionStore) UpdateLastPaged(ctx context.Context, sessionID, name string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE sessions SET last_paged = $1, updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE id = $2`, name, sessionID)
	if err != nil {
		return oops.With("operation", "update last paged").With("session_id", sessionID).Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("SESSION_NOT_FOUND").With("session_id", sessionID).Errorf("session not found")
	}
	return nil
}

// UpdateLastWhispered records the name of the character most recently whispered to.
func (s *PostgresSessionStore) UpdateLastWhispered(ctx context.Context, sessionID, name string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE sessions SET last_whispered = $1, updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE id = $2`, name, sessionID)
	if err != nil {
		return oops.With("operation", "update last whispered").With("session_id", sessionID).Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("SESSION_NOT_FOUND").With("session_id", sessionID).Errorf("session not found")
	}
	return nil
}

// UpdateFocusMemberships atomically applies the mutator callback to the
// session's focus memberships and presenting focus. Uses a transaction
// to ensure atomicity: reads current state, calls the mutator, and writes
// the result. On mutator error, the transaction is rolled back.
func (s *PostgresSessionStore) UpdateFocusMemberships(ctx context.Context, sessionID string, m session.FocusMutator) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.With("operation", "begin tx for update focus memberships").Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback on commit is a no-op

	// Read current state under the transaction.
	var statusStr string
	var focusMembershipsJSON, presentingFocusJSON []byte
	err = tx.QueryRow(
		ctx,
		`SELECT status, focus_memberships, presenting_focus FROM sessions WHERE id = $1 FOR UPDATE`,
		sessionID,
	).Scan(&statusStr, &focusMembershipsJSON, &presentingFocusJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", sessionID).
			Errorf("session not found")
	}
	if err != nil {
		return oops.With("operation", "read session for focus mutation").
			With("session_id", sessionID).Wrap(err)
	}

	if statusStr == string(session.StatusExpired) {
		return oops.Code("SESSION_EXPIRED").
			With("session_id", sessionID).
			Errorf("cannot mutate focus on expired session")
	}

	// Unmarshal current state.
	var currentMemberships []session.FocusMembership
	if len(focusMembershipsJSON) > 0 {
		if unmarshalErr := json.Unmarshal(focusMembershipsJSON, &currentMemberships); unmarshalErr != nil {
			return oops.With("operation", "unmarshal focus_memberships").Wrap(unmarshalErr)
		}
	}

	var currentPresenting *session.FocusKey
	if len(presentingFocusJSON) > 0 {
		var key session.FocusKey
		if unmarshalErr := json.Unmarshal(presentingFocusJSON, &key); unmarshalErr != nil {
			return oops.With("operation", "unmarshal presenting_focus").Wrap(unmarshalErr)
		}
		currentPresenting = &key
	}

	// Call the mutator.
	nextMemberships, nextPresenting, mutErr := m.Mutate(currentMemberships, currentPresenting)
	if mutErr != nil {
		return oops.Code("FOCUS_MUTATOR_ERROR").
			With("session_id", sessionID).
			Wrap(mutErr)
	}

	// Marshal next state.
	if nextMemberships == nil {
		nextMemberships = []session.FocusMembership{}
	}
	nextMembershipsJSON, err := json.Marshal(nextMemberships)
	if err != nil {
		return oops.With("operation", "marshal next focus_memberships").Wrap(err)
	}

	var nextPresentingJSON []byte
	if nextPresenting != nil {
		nextPresentingJSON, err = json.Marshal(nextPresenting)
		if err != nil {
			return oops.With("operation", "marshal next presenting_focus").Wrap(err)
		}
	}

	// Write back.
	_, err = tx.Exec(ctx,
		`UPDATE sessions SET focus_memberships = $1::jsonb, presenting_focus = $2::jsonb, updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE id = $3`,
		nextMembershipsJSON, nextPresentingJSON, sessionID)
	if err != nil {
		return oops.With("operation", "write focus memberships").
			With("session_id", sessionID).Wrap(err)
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		return oops.With("operation", "commit focus memberships").
			With("session_id", sessionID).Wrap(commitErr)
	}
	return nil
}

// GetConnection reads one session_connections row by PK. O(1) PK lookup.
// ULID columns are TEXT in the schema (migrations/000001_baseline.up.sql:227);
// mirror parseSessionRow pattern at session_store.go:51-83 — scan TEXT into
// string then ulid.Parse (pgx cannot scan TEXT directly into ulid.ULID).
// player_session_id is omitted from the SELECT — session.Connection has no
// PlayerSessionID field; the column persists via AddConnection's insert
// path (session_store.go:466-483) unchanged.
//
// Returns CONNECTION_NOT_FOUND when the row is absent.
func (s *PostgresSessionStore) GetConnection(ctx context.Context, connectionID ulid.ULID) (*session.Connection, error) {
	var (
		idStr        string
		sessionID    string
		clientType   string
		streams      []string
		focusKeyJSON []byte
		connectedAt  pgnanos.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT id, session_id, client_type, streams, focus_key, connected_at
		FROM session_connections WHERE id = $1
	`, connectionID.String()).Scan(
		&idStr, &sessionID, &clientType, &streams, &focusKeyJSON, &connectedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("CONNECTION_NOT_FOUND").
			With("connection_id", connectionID.String()).
			Errorf("connection not found")
	}
	if err != nil {
		return nil, oops.With("operation", "get connection").Wrap(err)
	}
	id, err := ulid.Parse(idStr)
	if err != nil {
		return nil, oops.With("operation", "parse connection id").Wrap(err)
	}
	var fk *session.FocusKey
	if len(focusKeyJSON) > 0 {
		var k session.FocusKey
		if uerr := json.Unmarshal(focusKeyJSON, &k); uerr != nil {
			return nil, oops.With("operation", "unmarshal focus_key").Wrap(uerr)
		}
		fk = &k
	}
	return &session.Connection{
		ID:          id,
		SessionID:   sessionID,
		ClientType:  clientType,
		Streams:     streams,
		FocusKey:    fk,
		ConnectedAt: connectedAt.Time(),
	}, nil
}

// UpdateSessionConnection runs the mutator under a single transaction.
// Lock-acquisition order is canonical per D11 / INV-SCENE-27: sessions row
// FOR UPDATE FIRST, then session_connections row FOR UPDATE. Two
// concurrent calls on the same session for different connections
// therefore cannot deadlock — both serialize on the shared sessions
// row before contending for their respective connection rows.
//
// The narrow UPDATE writes only `presenting_focus` on sessions and
// `focus_key` on session_connections. By contract (Postgres-parity
// with the former in-memory store's T5 impl) the mutator MUST NOT modify Connection.Streams
// or any other field; mutator changes to other fields are silently
// dropped. Phase 5's only legitimate caller (the coordinator) honors
// this contract.
//
// Returns:
//
//	SESSION_NOT_FOUND     — sessionID missing.
//	SESSION_EXPIRED       — status is "expired".
//	CONNECTION_NOT_FOUND  — connectionID missing under that session.
//	(mutator errors)      — passed through unwrapped.
func (s *PostgresSessionStore) UpdateSessionConnection(
	ctx context.Context,
	sessionID string,
	connectionID ulid.ULID,
	mut session.SessionConnectionMutator,
) error {
	// Nil-Mutate guard before any DB work: a zero-value mutator or a
	// keyed-literal bypass would otherwise panic inside the locked
	// section after consuming a transaction. (CodeRabbit PR #4191)
	if nerr := mut.NilSafe(); nerr != nil {
		return nerr //nolint:wrapcheck // session.ErrNilMutator is a sentinel; callers errors.Is against it directly
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.With("operation", "begin tx for update session connection").Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// D11 canonical lock order: sessions row FIRST. Mirror
	// UpdateFocusMemberships' narrow-column SELECT pattern at :676-691.
	// The mutator only needs FocusMemberships + PresentingFocus to make
	// decisions; we also read character_id + location_id for context.
	// ULIDs are TEXT in the schema (parseSessionRow pattern :51-83) —
	// scan into strings, then ulid.Parse.
	var statusStr string
	var focusMembershipsJSON, presentingFocusJSON []byte
	var characterIDStr, locationIDStr string
	err = tx.QueryRow(ctx, `
		SELECT status, focus_memberships, presenting_focus, character_id, location_id
		FROM sessions WHERE id = $1 FOR UPDATE
	`, sessionID).Scan(&statusStr, &focusMembershipsJSON, &presentingFocusJSON, &characterIDStr, &locationIDStr)
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", sessionID).
			Errorf("session not found")
	}
	if err != nil {
		return oops.With("operation", "read session for connection mutation").
			With("session_id", sessionID).Wrap(err)
	}
	if statusStr == string(session.StatusExpired) {
		return oops.Code("SESSION_EXPIRED").
			With("session_id", sessionID).
			Errorf("cannot mutate connection on expired session")
	}

	// Unmarshal session fields the mutator may inspect.
	var fms []session.FocusMembership
	if len(focusMembershipsJSON) > 0 {
		if uerr := json.Unmarshal(focusMembershipsJSON, &fms); uerr != nil {
			return oops.With("operation", "unmarshal focus_memberships").Wrap(uerr)
		}
	}
	var pf *session.FocusKey
	if len(presentingFocusJSON) > 0 {
		var k session.FocusKey
		if uerr := json.Unmarshal(presentingFocusJSON, &k); uerr != nil {
			return oops.With("operation", "unmarshal presenting_focus").Wrap(uerr)
		}
		pf = &k
	}
	characterID, perr := ulid.Parse(characterIDStr)
	if perr != nil {
		return oops.With("operation", "parse session character_id").Wrap(perr)
	}
	locationID, perr := ulid.Parse(locationIDStr)
	if perr != nil {
		return oops.With("operation", "parse session location_id").Wrap(perr)
	}
	info := session.Info{
		ID:               sessionID,
		Status:           session.Status(statusStr),
		CharacterID:      characterID,
		LocationID:       locationID,
		FocusMemberships: fms,
		PresentingFocus:  pf,
	}

	// Then session_connections row FOR UPDATE (D11 second-lock). ULIDs
	// scanned as TEXT then ulid.Parse (CRIT-A fix from plan-review r2).
	// player_session_id omitted — session.Connection has no PlayerSessionID
	// field (CRIT-B); the column still persists via AddConnection's insert
	// path (session_store.go:466-483) unchanged.
	var (
		cIDStr        string
		cSessionID    string
		cClientType   string
		cStreams      []string
		cFocusKeyJSON []byte
		cConnectedAt  pgnanos.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT id, session_id, client_type, streams, focus_key, connected_at
		FROM session_connections WHERE id = $1 AND session_id = $2 FOR UPDATE
	`, connectionID.String(), sessionID).Scan(
		&cIDStr, &cSessionID, &cClientType, &cStreams, &cFocusKeyJSON, &cConnectedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.Code("CONNECTION_NOT_FOUND").
			With("session_id", sessionID).
			With("connection_id", connectionID.String()).
			Errorf("connection not found in session")
	}
	if err != nil {
		return oops.With("operation", "read connection for mutation").Wrap(err)
	}
	cID, perr := ulid.Parse(cIDStr)
	if perr != nil {
		return oops.With("operation", "parse connection id").Wrap(perr)
	}
	var cFK *session.FocusKey
	if len(cFocusKeyJSON) > 0 {
		var k session.FocusKey
		if uerr := json.Unmarshal(cFocusKeyJSON, &k); uerr != nil {
			return oops.With("operation", "unmarshal connection focus_key").Wrap(uerr)
		}
		cFK = &k
	}
	conn := session.Connection{
		ID: cID, SessionID: cSessionID, ClientType: cClientType,
		Streams: cStreams, FocusKey: cFK, ConnectedAt: cConnectedAt.Time(),
	}

	// Call the mutator with coherent snapshots of both Info and Connection.
	// CONTRACT: the mutator MUST NOT modify Connection.Streams.
	// Streams is owned by SessionStreamRegistry (via subscription_router
	// SendToConnection calls), not by this Store path. The UPDATE below
	// writes only focus_key + presenting_focus; any Streams change in
	// the mutator callback is silently dropped. This is by design —
	// Phase 5's mutator only writes the two focus fields.
	nextInfo, nextConn, merr := mut.Mutate(info, conn)
	if merr != nil {
		return merr //nolint:wrapcheck // mutator error codes pass through
	}

	// Marshal and write back. Per D9/D10 the mutator may or may not
	// change PresentingFocus; write whatever it returned (nil → NULL).
	var nextPresentingJSON []byte
	if nextInfo.PresentingFocus != nil {
		nextPresentingJSON, err = json.Marshal(nextInfo.PresentingFocus)
		if err != nil {
			return oops.With("operation", "marshal next presenting_focus").Wrap(err)
		}
	}
	if _, execErr := tx.Exec(ctx, `
		UPDATE sessions SET presenting_focus = $1::jsonb, updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE id = $2
	`, nextPresentingJSON, sessionID); execErr != nil {
		return oops.With("operation", "write presenting_focus").
			With("session_id", sessionID).Wrap(execErr)
	}

	var nextFocusKeyJSON []byte
	if nextConn.FocusKey != nil {
		nextFocusKeyJSON, err = json.Marshal(nextConn.FocusKey)
		if err != nil {
			return oops.With("operation", "marshal next focus_key").Wrap(err)
		}
	}
	if _, execErr := tx.Exec(ctx, `
		UPDATE session_connections SET focus_key = $1::jsonb WHERE id = $2
	`, nextFocusKeyJSON, connectionID.String()); execErr != nil {
		return oops.With("operation", "write connection focus_key").
			With("connection_id", connectionID.String()).Wrap(execErr)
	}

	if cerr := tx.Commit(ctx); cerr != nil {
		return oops.With("operation", "commit session connection update").
			With("session_id", sessionID).
			With("connection_id", connectionID.String()).Wrap(cerr)
	}
	return nil
}

// UpdateLocationOnMove atomically updates location_id and location_arrived_at
// for all Active sessions belonging to characterID.
// Detached and Expired sessions are not touched.
//
// arrivedAt MUST be non-zero — a zero time.Time would collapse the INV-PRIVACY-1
// per-session location floor to year-1 and silently disable history privacy.
func (s *PostgresSessionStore) UpdateLocationOnMove(ctx context.Context, characterID, newLocationID ulid.ULID, arrivedAt time.Time) error {
	if arrivedAt.IsZero() {
		return oops.Code("INVALID_ARGUMENT").
			With("operation", "update_location_on_move").
			With("character_id", characterID.String()).
			Errorf("arrivedAt must be non-zero")
	}
	query := `UPDATE sessions
	          SET location_id = $1, location_arrived_at = $2, updated_at = $2
	          WHERE character_id = $3 AND status = 'active'`
	_, err := s.pool.Exec(ctx, query, newLocationID.String(), pgnanos.From(arrivedAt), characterID.String())
	if err != nil {
		return oops.With("operation", "update_location_on_move").
			With("character_id", characterID.String()).Wrap(err)
	}
	return nil
}

// BumpLocationArrivedAt updates location_arrived_at for a single session
// regardless of status.
//
// arrivedAt MUST be non-zero — a zero time.Time would collapse the INV-PRIVACY-1
// per-session location floor to year-1 and silently disable history privacy.
//
// Errors:
//
//	INVALID_ARGUMENT — arrivedAt is the zero value.
//	SESSION_NOT_FOUND — sessionID does not match any session.
func (s *PostgresSessionStore) BumpLocationArrivedAt(ctx context.Context, sessionID string, arrivedAt time.Time) error {
	if arrivedAt.IsZero() {
		return oops.Code("INVALID_ARGUMENT").
			With("operation", "bump_location_arrived_at").
			With("session_id", sessionID).
			Errorf("arrivedAt must be non-zero")
	}
	query := `UPDATE sessions
	          SET location_arrived_at = $1, updated_at = $1
	          WHERE id = $2`
	res, err := s.pool.Exec(ctx, query, pgnanos.From(arrivedAt), sessionID)
	if err != nil {
		return oops.With("operation", "bump_location_arrived_at").
			With("session_id", sessionID).Wrap(err)
	}
	n := res.RowsAffected()
	if n == 0 {
		return oops.Code("SESSION_NOT_FOUND").With("session_id", sessionID).Errorf("session not found")
	}
	return nil
}

// ListConnectionsBySession returns a snapshot of all active Connections
// for a session. No lock — callers must tolerate the racy snapshot
// (each per-conn UpdateSessionConnection re-validates atomically).
func (s *PostgresSessionStore) ListConnectionsBySession(ctx context.Context, sessionID string) ([]*session.Connection, error) {
	// Verify session exists first.
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sessions WHERE id = $1)`, sessionID).Scan(&exists); err != nil {
		return nil, oops.Code("SESSION_GET_FAILED").Wrap(err)
	}
	if !exists {
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", sessionID).
			Errorf("session not found")
	}

	// ULIDs scanned as TEXT then ulid.Parse (CRIT-A); player_session_id
	// omitted (CRIT-B — Connection has no PlayerSessionID field).
	rows, err := s.pool.Query(ctx, `
        SELECT id, session_id, client_type, streams, focus_key, connected_at
        FROM session_connections
        WHERE session_id = $1
    `, sessionID)
	if err != nil {
		return nil, oops.Code("CONNECTION_LIST_FAILED").Wrap(err)
	}
	defer rows.Close()

	out := make([]*session.Connection, 0)
	for rows.Next() {
		var (
			idStr       string
			sid         string
			ct          string
			streams     []string
			fkJSON      []byte
			connectedAt pgnanos.Time
		)
		if err := rows.Scan(&idStr, &sid, &ct, &streams, &fkJSON, &connectedAt); err != nil {
			return nil, oops.Code("CONNECTION_SCAN_FAILED").Wrap(err)
		}
		id, perr := ulid.Parse(idStr)
		if perr != nil {
			return nil, oops.With("operation", "parse connection id").Wrap(perr)
		}
		var fk *session.FocusKey
		if len(fkJSON) > 0 {
			var k session.FocusKey
			if uerr := json.Unmarshal(fkJSON, &k); uerr != nil {
				return nil, oops.With("operation", "unmarshal connection focus_key").Wrap(uerr)
			}
			fk = &k
		}
		conn := session.Connection{
			ID: id, SessionID: sid, ClientType: ct,
			Streams: streams, FocusKey: fk, ConnectedAt: connectedAt.Time(),
		}
		out = append(out, &conn)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("CONNECTION_ITER_FAILED").Wrap(err)
	}
	return out, nil
}
