// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/plugin/storage"
)

//go:embed migrations/*.up.sql
var migrationsFS embed.FS

type channelStore struct {
	pool *pgxpool.Pool
}

func newChannelStore(ctx context.Context, connString string) (*channelStore, error) {
	pool, err := storage.Connect(ctx, connString)
	if err != nil {
		return nil, oops.Code("CHANNEL_STORE_INIT_FAILED").Wrap(err)
	}

	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		pool.Close()
		return nil, oops.Code("CHANNEL_STORE_INIT_FAILED").Wrap(err)
	}
	if err := storage.RunMigrationsFS(ctx, pool, sub); err != nil {
		pool.Close()
		return nil, err
	}

	return &channelStore{pool: pool}, nil
}

func (s *channelStore) close() { s.pool.Close() }

// --- Channel CRUD ---

func (s *channelStore) createChannel(ctx context.Context, ch *channelRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO channels (id, name, type, description, owner_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		ch.ID, ch.Name, string(ch.Type), ch.Description, ch.OwnerID, ch.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "channels_name_unique") {
			return oops.Code("CHANNEL_DUPLICATE_NAME").With("name", ch.Name).Errorf("channel name %q already in use", ch.Name)
		}
		return oops.Code("CHANNEL_CREATE_FAILED").With("name", ch.Name).Wrap(err)
	}
	return nil
}

func (s *channelStore) getChannel(ctx context.Context, id string) (*channelRow, error) {
	row := &channelRow{}
	var typeStr string
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, type, description, owner_id, created_at, archived_at
		FROM channels WHERE id = $1`, id,
	).Scan(&row.ID, &row.Name, &typeStr, &row.Description, &row.OwnerID, &row.CreatedAt, &row.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("CHANNEL_NOT_FOUND").With("id", id).Errorf("channel not found")
	}
	if err != nil {
		return nil, oops.Code("CHANNEL_GET_FAILED").With("id", id).Wrap(err)
	}
	row.Type = channelType(typeStr)
	return row, nil
}

func (s *channelStore) getChannelByName(ctx context.Context, name string) (*channelRow, error) {
	row := &channelRow{}
	var typeStr string
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, type, description, owner_id, created_at, archived_at
		FROM channels WHERE lower(name) = lower($1)`, name,
	).Scan(&row.ID, &row.Name, &typeStr, &row.Description, &row.OwnerID, &row.CreatedAt, &row.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("CHANNEL_NOT_FOUND").With("name", name).Errorf("channel not found")
	}
	if err != nil {
		return nil, oops.Code("CHANNEL_GET_FAILED").With("name", name).Wrap(err)
	}
	row.Type = channelType(typeStr)
	return row, nil
}

func (s *channelStore) listChannels(ctx context.Context, includeArchived bool) ([]*channelRow, error) {
	query := `SELECT id, name, type, description, owner_id, created_at, archived_at FROM channels`
	if !includeArchived {
		query += ` WHERE archived_at IS NULL`
	}
	query += ` ORDER BY name`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, oops.Code("CHANNEL_LIST_FAILED").Wrap(err)
	}
	defer rows.Close()

	var channels []*channelRow
	for rows.Next() {
		ch := &channelRow{}
		var typeStr string
		if scanErr := rows.Scan(&ch.ID, &ch.Name, &typeStr, &ch.Description, &ch.OwnerID, &ch.CreatedAt, &ch.ArchivedAt); scanErr != nil {
			return nil, oops.Code("CHANNEL_LIST_FAILED").Wrap(scanErr)
		}
		ch.Type = channelType(typeStr)
		channels = append(channels, ch)
	}
	if rows.Err() != nil {
		return nil, oops.Code("CHANNEL_LIST_FAILED").Wrap(rows.Err())
	}
	return channels, nil
}

func (s *channelStore) archiveChannel(ctx context.Context, id string) error {
	now := time.Now().UTC()
	tag, err := s.pool.Exec(ctx, `
		UPDATE channels SET archived_at = $2 WHERE id = $1 AND archived_at IS NULL`,
		id, now)
	if err != nil {
		return oops.Code("CHANNEL_ARCHIVE_FAILED").With("id", id).Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("CHANNEL_NOT_FOUND").With("id", id).Errorf("channel not found")
	}
	return nil
}

// --- Membership ---

func (s *channelStore) addMembership(ctx context.Context, m *membershipRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO channel_memberships (channel_id, player_id, role, joined_at)
		VALUES ($1, $2, $3, $4)`,
		m.ChannelID, m.PlayerID, m.Role, m.JoinedAt)
	if err != nil {
		return oops.Code("MEMBERSHIP_ADD_FAILED").
			With("channel_id", m.ChannelID).With("player_id", m.PlayerID).Wrap(err)
	}
	return nil
}

func (s *channelStore) removeMembership(ctx context.Context, channelID, playerID string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM channel_memberships WHERE channel_id = $1 AND player_id = $2`,
		channelID, playerID)
	if err != nil {
		return oops.Code("MEMBERSHIP_REMOVE_FAILED").Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("MEMBERSHIP_NOT_FOUND").Errorf("not a member of this channel")
	}
	return nil
}

func (s *channelStore) getMembership(ctx context.Context, channelID, playerID string) (*membershipRow, error) {
	m := &membershipRow{}
	err := s.pool.QueryRow(ctx, `
		SELECT channel_id, player_id, role, joined_at, muted_until, banned
		FROM channel_memberships WHERE channel_id = $1 AND player_id = $2`,
		channelID, playerID,
	).Scan(&m.ChannelID, &m.PlayerID, &m.Role, &m.JoinedAt, &m.MutedUntil, &m.Banned)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("MEMBERSHIP_NOT_FOUND").Errorf("not a member of this channel")
	}
	if err != nil {
		return nil, oops.Code("MEMBERSHIP_GET_FAILED").Wrap(err)
	}
	return m, nil
}

func (s *channelStore) listMembersByChannel(ctx context.Context, channelID string) ([]*membershipRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT channel_id, player_id, role, joined_at, muted_until, banned
		FROM channel_memberships WHERE channel_id = $1 ORDER BY joined_at`,
		channelID)
	if err != nil {
		return nil, oops.Code("MEMBERSHIP_LIST_FAILED").Wrap(err)
	}
	defer rows.Close()

	var memberships []*membershipRow
	for rows.Next() {
		m := &membershipRow{}
		if scanErr := rows.Scan(&m.ChannelID, &m.PlayerID, &m.Role, &m.JoinedAt, &m.MutedUntil, &m.Banned); scanErr != nil {
			return nil, oops.Code("MEMBERSHIP_LIST_FAILED").Wrap(scanErr)
		}
		memberships = append(memberships, m)
	}
	if rows.Err() != nil {
		return nil, oops.Code("MEMBERSHIP_LIST_FAILED").Wrap(rows.Err())
	}
	return memberships, nil
}

func (s *channelStore) countMembershipsByPlayer(ctx context.Context, playerID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM channel_memberships WHERE player_id = $1`,
		playerID).Scan(&count)
	if err != nil {
		return 0, oops.Code("MEMBERSHIP_COUNT_FAILED").Wrap(err)
	}
	return count, nil
}

// --- Gags ---

func (s *channelStore) setGag(ctx context.Context, channelID, characterID string, gagged bool) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO channel_gags (channel_id, character_id, gagged)
		VALUES ($1, $2, $3)
		ON CONFLICT (channel_id, character_id) DO UPDATE SET gagged = EXCLUDED.gagged`,
		channelID, characterID, gagged)
	if err != nil {
		return oops.Code("GAG_SET_FAILED").Wrap(err)
	}
	return nil
}

func (s *channelStore) getGag(ctx context.Context, channelID, characterID string) (bool, error) {
	var gagged bool
	err := s.pool.QueryRow(ctx, `
		SELECT gagged FROM channel_gags WHERE channel_id = $1 AND character_id = $2`,
		channelID, characterID).Scan(&gagged)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, oops.Code("GAG_GET_FAILED").Wrap(err)
	}
	return gagged, nil
}

// --- Messages ---

func (s *channelStore) insertMessage(ctx context.Context, msg *messageRow) error {
	if msg.ID == "" {
		msg.ID = ulid.MustNew(ulid.Now(), rand.Reader).String()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO channel_messages (id, channel_id, author_id, author_name, message, event_type, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		msg.ID, msg.ChannelID, msg.AuthorID, msg.AuthorName, msg.Message, msg.EventType, msg.Source, msg.CreatedAt)
	if err != nil {
		return oops.Code("MESSAGE_INSERT_FAILED").With("channel_id", msg.ChannelID).Wrap(err)
	}
	return nil
}

func (s *channelStore) getHistory(ctx context.Context, channelID string, count int, notBefore time.Time) ([]*messageRow, error) {
	var rows pgx.Rows
	var err error

	if notBefore.IsZero() {
		rows, err = s.pool.Query(ctx, `
			SELECT id, channel_id, author_id, author_name, message, event_type, source, created_at
			FROM channel_messages WHERE channel_id = $1
			ORDER BY created_at DESC LIMIT $2`,
			channelID, count)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT id, channel_id, author_id, author_name, message, event_type, source, created_at
			FROM channel_messages WHERE channel_id = $1 AND created_at > $2
			ORDER BY created_at DESC LIMIT $3`,
			channelID, notBefore, count)
	}
	if err != nil {
		return nil, oops.Code("HISTORY_QUERY_FAILED").With("channel_id", channelID).Wrap(err)
	}
	defer rows.Close()

	var messages []*messageRow
	for rows.Next() {
		m := &messageRow{}
		if scanErr := rows.Scan(&m.ID, &m.ChannelID, &m.AuthorID, &m.AuthorName, &m.Message, &m.EventType, &m.Source, &m.CreatedAt); scanErr != nil {
			return nil, oops.Code("HISTORY_SCAN_FAILED").Wrap(scanErr)
		}
		messages = append(messages, m)
	}
	if rows.Err() != nil {
		return nil, oops.Code("HISTORY_QUERY_FAILED").Wrap(rows.Err())
	}

	// Reverse to chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}
