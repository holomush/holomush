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

// F6: event_cursors column dropped (migration 000010). The session.Info.EventCursors
// field still exists in Go (removed by F7) but is no longer persisted to the database.
const sessionSelectColumns = `id, character_id, player_id,
	COALESCE(player_session_id, '') AS player_session_id,
	character_name, location_id,
	is_guest, status, grid_present,
	command_history, ttl_seconds, max_history,
	detached_at, expires_at, created_at, updated_at,
	last_paged, last_whispered,
	focus_memberships, presenting_focus`

// parseSessionRow parses the scalar fields scanned from a session row into a
// session.Info. Both scanSession and scanSessions call Scan with the same
// variable set and then delegate here to avoid duplicating the parse logic.
//
// F6: cursorsJSON parameter removed — event_cursors column was dropped by
// migration 000010. session.Info.EventCursors is always empty after this point
// (F7 removes the field entirely).
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

	err := row.Scan(
		&info.ID,
		&charIDStr,
		&playerIDStr,
		&playerSessionIDStr,
		&info.CharacterName,
		&locIDStr,
		&info.IsGuest,
		&statusStr,
		&info.GridPresent,
		&info.CommandHistory,
		&info.TTLSeconds,
		&info.MaxHistory,
		&info.DetachedAt,
		&info.ExpiresAt,
		&info.CreatedAt,
		&info.UpdatedAt,
		&info.LastPaged,
		&info.LastWhispered,
		&focusMembershipsJSON,
		&presentingFocusJSON,
	)
	if err != nil {
		return nil, oops.With("operation", "scan session row").Wrap(err)
	}

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

		err := rows.Scan(
			&info.ID,
			&charIDStr,
			&playerIDStr,
			&playerSessionIDStr,
			&info.CharacterName,
			&locIDStr,
			&info.IsGuest,
			&statusStr,
			&info.GridPresent,
			&info.CommandHistory,
			&info.TTLSeconds,
			&info.MaxHistory,
			&info.DetachedAt,
			&info.ExpiresAt,
			&info.CreatedAt,
			&info.UpdatedAt,
			&info.LastPaged,
			&info.LastWhispered,
			&focusMembershipsJSON,
			&presentingFocusJSON,
		)
		if err != nil {
			return nil, oops.With("operation", "scan session row").Wrap(err)
		}

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

	_, err = s.pool.Exec(ctx,
		`INSERT INTO sessions (id, character_id, player_id, player_session_id,
			character_name, location_id,
			is_guest, status, grid_present,
			command_history, ttl_seconds, max_history,
			detached_at, expires_at, created_at,
			last_paged, last_whispered,
			focus_memberships, presenting_focus)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18::jsonb, $19::jsonb)
		 ON CONFLICT (id) DO UPDATE SET
			character_id = EXCLUDED.character_id,
			player_id = EXCLUDED.player_id,
			player_session_id = EXCLUDED.player_session_id,
			character_name = EXCLUDED.character_name,
			location_id = EXCLUDED.location_id,
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
			updated_at = now()`,
		id,
		info.CharacterID.String(),
		info.PlayerID.String(),
		playerSessionIDArg,
		info.CharacterName,
		info.LocationID.String(),
		info.IsGuest,
		string(info.Status),
		info.GridPresent,
		cmdHistory,
		info.TTLSeconds,
		info.MaxHistory,
		info.DetachedAt,
		info.ExpiresAt,
		info.CreatedAt,
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
		`SELECT `+sessionSelectColumns+` FROM sessions WHERE status = 'detached' AND expires_at < now()`)
	if err != nil {
		return nil, oops.With("operation", "list expired sessions").Wrap(err)
	}
	return scanSessions(rows)
}

// UpdateStatus transitions a session's status.
func (s *PostgresSessionStore) UpdateStatus(ctx context.Context, id string, status session.Status,
	detachedAt *time.Time, expiresAt *time.Time,
) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET status = $1, detached_at = $2, expires_at = $3, updated_at = now() WHERE id = $4`,
		string(status), detachedAt, expiresAt, id)
	if err != nil {
		return oops.With("operation", "update session status").With("session_id", id).With("status", string(status)).Wrap(err)
	}
	return nil
}

// ReattachCAS atomically transitions a detached session to active.
// Returns true if the row was updated, false if another client won the race.
func (s *PostgresSessionStore) ReattachCAS(ctx context.Context, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE sessions SET status = 'active', detached_at = NULL, expires_at = NULL, updated_at = now() WHERE id = $1 AND status = 'detached'`,
		id)
	if err != nil {
		return false, oops.With("operation", "reattach session").With("session_id", id).Wrap(err)
	}
	return tag.RowsAffected() == 1, nil
}

// UpdateCursors is a no-op after F6 dropped the event_cursors column
// (migration 000010). Cursors are no longer persisted to the database.
// The session.Access interface method remains for backward compatibility;
// F7 removes it along with the EventCursors field on session.Info.
func (s *PostgresSessionStore) UpdateCursors(_ context.Context, _ string, _ map[string]ulid.ULID) error {
	return nil
}

// AppendCommand adds a command to the session's history, enforcing the cap.
func (s *PostgresSessionStore) AppendCommand(ctx context.Context, id, command string, maxHistory int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET command_history = (command_history || ARRAY[$1::text])[
			GREATEST(1, array_length(command_history || ARRAY[$1::text], 1) - $2 + 1) :
		], updated_at = now() WHERE id = $3`,
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
			Errorf("invalid client_type %q: must be one of terminal, comms_hub, telnet", conn.ClientType)
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO session_connections (id, session_id, client_type, streams, connected_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		conn.ID.String(), conn.SessionID, conn.ClientType, conn.Streams, conn.ConnectedAt)
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
		`UPDATE sessions SET grid_present = $2, updated_at = NOW() WHERE id = $1`,
		id, present)
	if err != nil {
		return oops.With("operation", "update grid present").
			With("session_id", id).Wrap(err)
	}
	return nil
}

// ListActiveByLocation returns active sessions whose location_id matches.
func (s *PostgresSessionStore) ListActiveByLocation(ctx context.Context, locationID ulid.ULID) ([]*session.Info, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+sessionSelectColumns+` FROM sessions WHERE location_id = $1 AND status = 'active'`,
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
		`UPDATE sessions SET updated_at = now() WHERE id = $1`, id)
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
		`UPDATE sessions SET last_paged = $1, updated_at = now() WHERE id = $2`, name, sessionID)
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
		`UPDATE sessions SET last_whispered = $1, updated_at = now() WHERE id = $2`, name, sessionID)
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
	err = tx.QueryRow(ctx,
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
		`UPDATE sessions SET focus_memberships = $1::jsonb, presenting_focus = $2::jsonb, updated_at = now() WHERE id = $3`,
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
