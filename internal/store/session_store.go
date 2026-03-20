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
	return &PostgresSessionStore{pool: pool}
}

// compile-time check
var _ session.Store = (*PostgresSessionStore)(nil)

const sessionSelectColumns = `id, character_id, character_name, location_id,
	is_guest, status, grid_present, event_cursors,
	command_history, ttl_seconds, max_history,
	detached_at, expires_at, created_at, updated_at`

// parseSessionRow parses the scalar fields scanned from a session row into a
// session.Info. Both scanSession and scanSessions call Scan with the same
// variable set and then delegate here to avoid duplicating the parse logic.
func parseSessionRow(info *session.Info, charIDStr, locIDStr, statusStr string, cursorsJSON []byte) error {
	charID, err := ulid.Parse(charIDStr)
	if err != nil {
		return oops.With("operation", "parse character_id").With("raw_id", charIDStr).Wrap(err)
	}
	info.CharacterID = charID

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

	if len(cursorsJSON) > 0 {
		cursors := make(map[string]ulid.ULID)
		if err := json.Unmarshal(cursorsJSON, &cursors); err != nil {
			return oops.With("operation", "unmarshal event_cursors").Wrap(err)
		}
		info.EventCursors = cursors
	}

	return nil
}

// scanSession scans a pgx.Row into a session.Info.
func scanSession(row pgx.Row) (*session.Info, error) {
	var info session.Info
	var charIDStr, locIDStr, statusStr string
	var cursorsJSON []byte

	err := row.Scan(
		&info.ID,
		&charIDStr,
		&info.CharacterName,
		&locIDStr,
		&info.IsGuest,
		&statusStr,
		&info.GridPresent,
		&cursorsJSON,
		&info.CommandHistory,
		&info.TTLSeconds,
		&info.MaxHistory,
		&info.DetachedAt,
		&info.ExpiresAt,
		&info.CreatedAt,
		&info.UpdatedAt,
	)
	if err != nil {
		return nil, oops.With("operation", "scan session row").Wrap(err)
	}

	if err := parseSessionRow(&info, charIDStr, locIDStr, statusStr, cursorsJSON); err != nil {
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
		var charIDStr, locIDStr, statusStr string
		var cursorsJSON []byte

		err := rows.Scan(
			&info.ID,
			&charIDStr,
			&info.CharacterName,
			&locIDStr,
			&info.IsGuest,
			&statusStr,
			&info.GridPresent,
			&cursorsJSON,
			&info.CommandHistory,
			&info.TTLSeconds,
			&info.MaxHistory,
			&info.DetachedAt,
			&info.ExpiresAt,
			&info.CreatedAt,
			&info.UpdatedAt,
		)
		if err != nil {
			return nil, oops.With("operation", "scan session row").Wrap(err)
		}

		if err := parseSessionRow(&info, charIDStr, locIDStr, statusStr, cursorsJSON); err != nil {
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
	cursorsJSON, err := json.Marshal(info.EventCursors)
	if err != nil {
		return oops.With("operation", "marshal event_cursors").With("session_id", id).Wrap(err)
	}

	// Coerce nil slice to empty — pgx sends nil Go slices as SQL NULL,
	// which violates the NOT NULL constraint on command_history.
	cmdHistory := info.CommandHistory
	if cmdHistory == nil {
		cmdHistory = []string{}
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO sessions (id, character_id, character_name, location_id,
			is_guest, status, grid_present, event_cursors,
			command_history, ttl_seconds, max_history,
			detached_at, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12, $13, $14)
		 ON CONFLICT (id) DO UPDATE SET
			character_id = EXCLUDED.character_id,
			character_name = EXCLUDED.character_name,
			location_id = EXCLUDED.location_id,
			is_guest = EXCLUDED.is_guest,
			status = EXCLUDED.status,
			grid_present = EXCLUDED.grid_present,
			event_cursors = EXCLUDED.event_cursors,
			command_history = EXCLUDED.command_history,
			ttl_seconds = EXCLUDED.ttl_seconds,
			max_history = EXCLUDED.max_history,
			detached_at = EXCLUDED.detached_at,
			expires_at = EXCLUDED.expires_at,
			updated_at = now()`,
		id,
		info.CharacterID.String(),
		info.CharacterName,
		info.LocationID.String(),
		info.IsGuest,
		string(info.Status),
		info.GridPresent,
		cursorsJSON,
		cmdHistory,
		info.TTLSeconds,
		info.MaxHistory,
		info.DetachedAt,
		info.ExpiresAt,
		info.CreatedAt,
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
// TODO: filter by playerID when player-character relationship table exists.
// Note: player_id is not stored in the sessions table yet (will be added in
// chunk 5). For now, returns all non-expired sessions.
func (s *PostgresSessionStore) ListByPlayer(ctx context.Context, _ ulid.ULID) ([]*session.Info, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+sessionSelectColumns+` FROM sessions WHERE status != 'expired'`)
	if err != nil {
		return nil, oops.With("operation", "list sessions by player").Wrap(err)
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

// UpdateCursors updates the event cursors for a session via jsonb merge.
func (s *PostgresSessionStore) UpdateCursors(ctx context.Context, id string, cursors map[string]ulid.ULID) error {
	cursorsJSON, err := json.Marshal(cursors)
	if err != nil {
		return oops.With("operation", "marshal cursors").With("session_id", id).Wrap(err)
	}

	_, err = s.pool.Exec(ctx,
		`UPDATE sessions SET event_cursors = event_cursors || $1::jsonb, updated_at = now() WHERE id = $2`,
		cursorsJSON, id)
	if err != nil {
		return oops.With("operation", "update cursors").With("session_id", id).Wrap(err)
	}
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
