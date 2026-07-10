// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/pkg/plugin/storage"
)

//go:embed migrations/*.up.sql
var migrationsFS embed.FS

// pgUniqueViolation is the PostgreSQL SQLSTATE for a unique-constraint
// violation; pgForeignKeyViolation is the FK-constraint SQLSTATE.
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

// channelSelectColumns is the column list shared by every statement that reads
// a full channelRow. The scan order below MUST match it.
const channelSelectColumns = `id, name, type, owner_id, archived, is_default, retention_days, created_at`

// channelRow is the persistence-layer representation of a channel; the shape
// matches the channels table column-for-column. RetentionDays is a pointer
// because the column is nullable (NULL = use the plugin config default, D-07).
type channelRow struct {
	ID            string
	Name          string
	Type          string
	OwnerID       string
	Archived      bool
	IsDefault     bool
	RetentionDays *int
	CreatedAt     pgnanos.Time
}

// scanChannelRow scans a single row into dst. Column order MUST match
// channelSelectColumns.
//
//nolint:wrapcheck // callers wrap with an operation-specific oops code
func scanChannelRow(scanner pgx.Row, dst *channelRow) error {
	return scanner.Scan(
		&dst.ID, &dst.Name, &dst.Type, &dst.OwnerID,
		&dst.Archived, &dst.IsDefault, &dst.RetentionDays, &dst.CreatedAt,
	)
}

// channelStore provides PostgreSQL persistence for the channel domain. It is
// the membership source of truth that the resolver (01-04) and audit
// QueryHistory (01-06) authorize against.
type channelStore struct {
	pool *pgxpool.Pool
}

// NewChannelStore opens a connection pool and runs the embedded migrations.
//
// The connection string is the one provided by the host's SchemaProvisioner in
// ServiceConfig.ConnectionString — it has search_path=plugin_core_channels
// pre-configured, so all queries automatically target the plugin's schema.
func NewChannelStore(ctx context.Context, connString string) (*channelStore, error) {
	pool, err := storage.Connect(ctx, connString)
	if err != nil {
		return nil, oops.Code("CHANNEL_STORE_CONNECT_FAILED").Wrap(err)
	}

	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		pool.Close()
		return nil, oops.Code("CHANNEL_STORE_INIT_FAILED").Wrap(err)
	}
	if err := storage.RunMigrationsFS(ctx, pool, sub); err != nil {
		pool.Close()
		return nil, oops.Code("CHANNEL_STORE_MIGRATIONS_FAILED").Wrap(err)
	}

	return &channelStore{pool: pool}, nil
}

// Close releases the underlying connection pool. Safe to defer in main().
func (s *channelStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// Pool exposes the underlying pgxpool so sibling subsystems inside the plugin
// (e.g. the audit store, 01-06) can share a single connection pool.
func (s *channelStore) Pool() *pgxpool.Pool {
	return s.pool
}

// isPgCode reports whether err carries the given PostgreSQL SQLSTATE.
func isPgCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

// CreateChannel inserts a new channel row, an owner membership row (role
// 'owner'), and a lifecycle.created ops event in a single transaction. The
// channel name is validated at the store boundary (T-01-10) and uniqueness is
// enforced case-insensitively; a same-name collision (any case) returns
// CHANNEL_NAME_TAKEN. If row.ID is empty a fresh crypto/rand ULID is minted.
func (s *channelStore) CreateChannel(ctx context.Context, row *channelRow) error {
	if !validateChannelName(row.Name) {
		return oops.Code("CHANNEL_NAME_INVALID").With("name", row.Name).
			Errorf("channel name does not match the accepted pattern")
	}
	if row.Type == "" {
		row.Type = string(channelTypePublic)
	}
	if !channelType(row.Type).IsValid() {
		return oops.Code("CHANNEL_TYPE_INVALID").With("type", row.Type).
			Errorf("unknown channel type")
	}
	if row.OwnerID == "" {
		return oops.Code("CHANNEL_OWNER_REQUIRED").Errorf("owner_id is required")
	}
	if row.ID == "" {
		row.ID = idgen.New().String()
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.Code("CHANNEL_CREATE_FAILED").With("channel_id", row.ID).Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	_, err = tx.Exec(
		ctx, `
		INSERT INTO channels (id, name, type, owner_id, is_default, retention_days)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		row.ID, row.Name, row.Type, row.OwnerID, row.IsDefault, row.RetentionDays,
	)
	if err != nil {
		if isPgCode(err, pgUniqueViolation) {
			return oops.Code("CHANNEL_NAME_TAKEN").With("name", row.Name).Wrap(err)
		}
		return oops.Code("CHANNEL_CREATE_FAILED").With("channel_id", row.ID).Wrap(err)
	}

	_, err = tx.Exec(
		ctx, `
		INSERT INTO channel_memberships (channel_id, character_id, role)
		VALUES ($1, $2, 'owner')`,
		row.ID, row.OwnerID,
	)
	if err != nil {
		return oops.Code("CHANNEL_CREATE_OWNER_MEMBERSHIP_FAILED").
			With("channel_id", row.ID).With("owner_id", row.OwnerID).Wrap(err)
	}

	if err := recordChannelOpsEventTx(ctx, tx, row.ID, opsKindLifecycleCreated, row.OwnerID, "", map[string]any{"type": row.Type}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.Code("CHANNEL_CREATE_FAILED").With("channel_id", row.ID).Wrap(err)
	}
	return nil
}

// GetByID loads a single channel by id. Returns CHANNEL_NOT_FOUND if absent.
func (s *channelStore) GetByID(ctx context.Context, id string) (*channelRow, error) {
	row := &channelRow{}
	err := scanChannelRow(s.pool.QueryRow(
		ctx, `SELECT `+channelSelectColumns+` FROM channels WHERE id = $1`, id,
	), row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.Code("CHANNEL_NOT_FOUND").With("channel_id", id).Wrap(err)
		}
		return nil, oops.Code("CHANNEL_GET_FAILED").With("channel_id", id).Wrap(err)
	}
	return row, nil
}

// GetByName loads a single channel by case-insensitive name. Returns
// CHANNEL_NOT_FOUND if absent.
func (s *channelStore) GetByName(ctx context.Context, name string) (*channelRow, error) {
	row := &channelRow{}
	err := scanChannelRow(s.pool.QueryRow(
		ctx, `SELECT `+channelSelectColumns+` FROM channels WHERE lower(name) = lower($1)`, name,
	), row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.Code("CHANNEL_NOT_FOUND").With("name", name).Wrap(err)
		}
		return nil, oops.Code("CHANNEL_GET_FAILED").With("name", name).Wrap(err)
	}
	return row, nil
}

// JoinChannel adds characterID to channelID as a member. Idempotent: an
// existing non-banned membership is a no-op (no error, no duplicate ops event).
// A banned character cannot rejoin (CHANNEL_BANNED). Joining a channel that does
// not exist returns CHANNEL_NOT_FOUND.
func (s *channelStore) JoinChannel(ctx context.Context, channelID, characterID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.Code("CHANNEL_JOIN_FAILED").With("channel_id", channelID).Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	var banned bool
	err = tx.QueryRow(
		ctx, `SELECT banned FROM channel_memberships WHERE channel_id = $1 AND character_id = $2`,
		channelID, characterID,
	).Scan(&banned)
	switch {
	case err == nil:
		if banned {
			return oops.Code("CHANNEL_BANNED").
				With("channel_id", channelID).With("character_id", characterID).
				Errorf("character is banned from this channel")
		}
		// Already a member — idempotent no-op.
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return oops.Code("CHANNEL_JOIN_FAILED").With("channel_id", channelID).Wrap(commitErr)
		}
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		// Not a member yet — fall through to insert.
	default:
		return oops.Code("CHANNEL_JOIN_FAILED").With("channel_id", channelID).Wrap(err)
	}

	_, err = tx.Exec(
		ctx, `
		INSERT INTO channel_memberships (channel_id, character_id, role)
		VALUES ($1, $2, 'member')`,
		channelID, characterID,
	)
	if err != nil {
		if isPgCode(err, pgForeignKeyViolation) {
			return oops.Code("CHANNEL_NOT_FOUND").With("channel_id", channelID).Wrap(err)
		}
		return oops.Code("CHANNEL_JOIN_FAILED").
			With("channel_id", channelID).With("character_id", characterID).Wrap(err)
	}

	if err := recordChannelOpsEventTx(ctx, tx, channelID, opsKindMembershipJoin, characterID, characterID, nil); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.Code("CHANNEL_JOIN_FAILED").With("channel_id", channelID).Wrap(err)
	}
	return nil
}

// LeaveChannel removes characterID's membership from channelID and records a
// membership.leave ops event. The owner cannot leave (CHANNEL_OWNER_CANNOT_LEAVE)
// — ownership transfer or archive is the exit path. Returns
// CHANNEL_MEMBERSHIP_NOT_FOUND if the character is not a member.
func (s *channelStore) LeaveChannel(ctx context.Context, channelID, characterID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.Code("CHANNEL_LEAVE_FAILED").With("channel_id", channelID).Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	var role string
	err = tx.QueryRow(
		ctx, `
		DELETE FROM channel_memberships
		WHERE channel_id = $1 AND character_id = $2 AND role <> 'owner'
		RETURNING role`,
		channelID, characterID,
	).Scan(&role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.classifyLeaveMiss(ctx, tx, channelID, characterID, err)
		}
		return oops.Code("CHANNEL_LEAVE_FAILED").With("channel_id", channelID).Wrap(err)
	}

	if err := recordChannelOpsEventTx(ctx, tx, channelID, opsKindMembershipLeave, characterID, characterID, map[string]any{"prior_role": role}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.Code("CHANNEL_LEAVE_FAILED").With("channel_id", channelID).Wrap(err)
	}
	return nil
}

// classifyLeaveMiss distinguishes "owner cannot leave" from "not a member" when
// the leave DELETE matched no row.
func (s *channelStore) classifyLeaveMiss(ctx context.Context, tx pgx.Tx, channelID, characterID string, cause error) error {
	var role string
	err := tx.QueryRow(
		ctx, `SELECT role FROM channel_memberships WHERE channel_id = $1 AND character_id = $2`,
		channelID, characterID,
	).Scan(&role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("CHANNEL_MEMBERSHIP_NOT_FOUND").
				With("channel_id", channelID).With("character_id", characterID).Wrap(cause)
		}
		return oops.Code("CHANNEL_LEAVE_FAILED").With("channel_id", channelID).Wrap(err)
	}
	return oops.Code("CHANNEL_OWNER_CANNOT_LEAVE").
		With("channel_id", channelID).With("character_id", characterID).
		Errorf("channel owners cannot leave; transfer ownership or archive the channel")
}

// ListForCharacter returns the non-archived channels characterID is an active
// (non-banned) member of, ordered by name. Empty (not nil-error) for a
// non-member.
func (s *channelStore) ListForCharacter(ctx context.Context, characterID string) ([]channelRow, error) {
	rows, err := s.pool.Query(
		ctx, `
		SELECT `+prefixedChannelColumns("c")+`
		FROM channels c
		JOIN channel_memberships m ON m.channel_id = c.id
		WHERE m.character_id = $1 AND m.banned = false AND c.archived = false
		ORDER BY c.name`,
		characterID,
	)
	if err != nil {
		return nil, oops.Code("CHANNEL_LIST_FAILED").With("character_id", characterID).Wrap(err)
	}
	defer rows.Close()

	out := make([]channelRow, 0)
	for rows.Next() {
		var r channelRow
		if err := scanChannelRow(rows, &r); err != nil {
			return nil, oops.Code("CHANNEL_LIST_FAILED").With("character_id", characterID).Wrap(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("CHANNEL_LIST_FAILED").With("character_id", characterID).Wrap(err)
	}
	return out, nil
}

// MembershipForHistory reports whether characterID is an active (non-banned)
// owner/member of channelID, and their most-recent joined_at (the D-07 history
// floor). It is the single membership+floor read the audit QueryHistory boundary
// authorizes against (01-06).
//
// A missing channel and a missing/banned membership row both return
// (false, zero, nil) by design: the audit-read boundary MUST NOT distinguish
// "channel doesn't exist" from "you're not a member" — that would leak channel
// existence to non-members (T-01-12). The primary key (channel_id, character_id)
// makes joined_at single-valued; a leave+rejoin writes a fresh row, so joined_at
// always reflects the most-recent join.
func (s *channelStore) MembershipForHistory(ctx context.Context, channelID, characterID string) (bool, time.Time, error) {
	var joinedAt pgnanos.Time
	err := s.pool.QueryRow(
		ctx, `
		SELECT joined_at FROM channel_memberships
		WHERE channel_id = $1 AND character_id = $2 AND banned = false
		  AND role IN ('owner', 'member')
		LIMIT 1`,
		channelID, characterID,
	).Scan(&joinedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, time.Time{}, nil
		}
		return false, time.Time{}, oops.Code("CHANNEL_MEMBERSHIP_LOOKUP_FAILED").
			With("channel_id", channelID).With("character_id", characterID).Wrap(err)
	}
	return true, joinedAt.Time(), nil
}

// GetWithMembership returns the channel row plus its members / banned / muted
// character-id lists in a single round trip. Used by the resolver (01-04) to
// materialise ABAC attributes without separate queries. members contains
// non-banned character ids; banned and muted contain the ids with the
// respective flag set.
func (s *channelStore) GetWithMembership(ctx context.Context, id string) (channel *channelRow, members, banned, muted []string, err error) {
	row := &channelRow{}
	err = s.pool.QueryRow(
		ctx, `
		SELECT `+prefixedChannelColumns("c")+`,
			COALESCE((SELECT array_agg(character_id) FROM channel_memberships
			          WHERE channel_id = c.id AND banned = false), '{}'::TEXT[]) AS members,
			COALESCE((SELECT array_agg(character_id) FROM channel_memberships
			          WHERE channel_id = c.id AND banned = true), '{}'::TEXT[]) AS banned,
			COALESCE((SELECT array_agg(character_id) FROM channel_memberships
			          WHERE channel_id = c.id AND muted = true), '{}'::TEXT[]) AS muted
		FROM channels c
		WHERE c.id = $1`,
		id,
	).Scan(
		&row.ID, &row.Name, &row.Type, &row.OwnerID, &row.Archived,
		&row.IsDefault, &row.RetentionDays, &row.CreatedAt,
		&members, &banned, &muted,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil, nil, oops.Code("CHANNEL_NOT_FOUND").With("channel_id", id).Wrap(err)
		}
		return nil, nil, nil, nil, oops.Code("CHANNEL_GET_FAILED").With("channel_id", id).Wrap(err)
	}
	return row, members, banned, muted, nil
}

// SetMuted sets the muted flag on characterID's membership in channelID and
// records a moderation.mute / moderation.unmute ops event. Returns
// CHANNEL_MEMBERSHIP_NOT_FOUND if the character is not a member.
func (s *channelStore) SetMuted(ctx context.Context, channelID, characterID string, muted bool) error {
	kind := opsKindModerationMute
	if !muted {
		kind = opsKindModerationUnmute
	}
	return s.setMembershipFlag(ctx, channelID, characterID, "muted", muted, kind, "CHANNEL_MUTE_FAILED")
}

// SetBanned sets the banned flag on characterID's membership in channelID and
// records a moderation.ban / moderation.unban ops event. A banned member's row
// is retained (banned = true) so JoinChannel refuses a rejoin. Returns
// CHANNEL_MEMBERSHIP_NOT_FOUND if the character is not a member.
func (s *channelStore) SetBanned(ctx context.Context, channelID, characterID string, banned bool) error {
	kind := opsKindModerationBan
	if !banned {
		kind = opsKindModerationUnban
	}
	return s.setMembershipFlag(ctx, channelID, characterID, "banned", banned, kind, "CHANNEL_BAN_FAILED")
}

// setMembershipFlag is the shared implementation for SetMuted/SetBanned. The
// column name is a hard-coded literal at the two call sites (never user input),
// so interpolating it into the UPDATE is safe; the value is parameterized.
func (s *channelStore) setMembershipFlag(ctx context.Context, channelID, characterID, column string, value bool, kind channelOpsEventKind, failCode string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.Code(failCode).With("channel_id", channelID).Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	var characterOut string
	err = tx.QueryRow(
		ctx,
		`UPDATE channel_memberships SET `+column+` = $3
		 WHERE channel_id = $1 AND character_id = $2
		 RETURNING character_id`,
		channelID, characterID, value,
	).Scan(&characterOut)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("CHANNEL_MEMBERSHIP_NOT_FOUND").
				With("channel_id", channelID).With("character_id", characterID).Wrap(err)
		}
		return oops.Code(failCode).With("channel_id", channelID).Wrap(err)
	}

	if err := recordChannelOpsEventTx(ctx, tx, channelID, kind, characterID, characterID, map[string]any{column: value}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.Code(failCode).With("channel_id", channelID).Wrap(err)
	}
	return nil
}

// DeleteChannel soft-archives a channel: it sets archived = true and NEVER
// hard-deletes the row (spec §specifics). Idempotent — archiving an
// already-archived channel is a no-op success. Records a lifecycle.archived ops
// event. Returns CHANNEL_NOT_FOUND if the channel does not exist.
func (s *channelStore) DeleteChannel(ctx context.Context, id, actorID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.Code("CHANNEL_ARCHIVE_FAILED").With("channel_id", id).Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// RETURNING yields a row only when the UPDATE matched an existing channel;
	// archived is unconditionally true afterward, so it serves only as a
	// not-found sentinel. Archiving an already-archived channel is idempotent.
	var archived bool
	err = tx.QueryRow(
		ctx, `UPDATE channels SET archived = true WHERE id = $1 RETURNING archived`,
		id,
	).Scan(&archived)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("CHANNEL_NOT_FOUND").With("channel_id", id).Wrap(err)
		}
		return oops.Code("CHANNEL_ARCHIVE_FAILED").With("channel_id", id).Wrap(err)
	}

	if err := recordChannelOpsEventTx(ctx, tx, id, opsKindLifecycleArchived, actorID, "", nil); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.Code("CHANNEL_ARCHIVE_FAILED").With("channel_id", id).Wrap(err)
	}
	return nil
}

// channelMemberRow is one row of a channel's roster: the member's character id,
// role ("owner"/"member"), moderation flags, and join time. Returned by
// ListMembers to project the WhoInChannel roster (01-05b).
type channelMemberRow struct {
	CharacterID string
	Role        string
	Muted       bool
	Banned      bool
	JoinedAt    pgnanos.Time
}

// ListMembers returns the active (non-banned) roster of channelID ordered owner
// first then by character id. The membership gate at the service boundary runs
// BEFORE this call, so an absent channel simply yields an empty roster here (the
// gate already returned the uniform not-found). Banned rows are excluded — a
// banned character is no longer on the roster (their row is retained only so
// JoinChannel can refuse a rejoin).
func (s *channelStore) ListMembers(ctx context.Context, channelID string) ([]channelMemberRow, error) {
	rows, err := s.pool.Query(
		ctx, `
		SELECT character_id, role, muted, banned, joined_at
		FROM channel_memberships
		WHERE channel_id = $1 AND banned = false
		ORDER BY (role = 'owner') DESC, character_id`,
		channelID,
	)
	if err != nil {
		return nil, oops.Code("CHANNEL_LIST_MEMBERS_FAILED").With("channel_id", channelID).Wrap(err)
	}
	defer rows.Close()

	out := make([]channelMemberRow, 0)
	for rows.Next() {
		var r channelMemberRow
		if err := rows.Scan(&r.CharacterID, &r.Role, &r.Muted, &r.Banned, &r.JoinedAt); err != nil {
			return nil, oops.Code("CHANNEL_LIST_MEMBERS_FAILED").With("channel_id", channelID).Wrap(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("CHANNEL_LIST_MEMBERS_FAILED").With("channel_id", channelID).Wrap(err)
	}
	return out, nil
}

// IsMuted reports whether characterID's active (non-banned) membership in
// channelID carries the muted flag. A missing/banned row returns (false, nil):
// the PostToChannel membership gate (ABAC emit) already ran, so a non-member is
// never reached here — a false result simply means "not muted".
func (s *channelStore) IsMuted(ctx context.Context, channelID, characterID string) (bool, error) {
	var muted bool
	err := s.pool.QueryRow(
		ctx, `
		SELECT muted FROM channel_memberships
		WHERE channel_id = $1 AND character_id = $2 AND banned = false`,
		channelID, characterID,
	).Scan(&muted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, oops.Code("CHANNEL_MEMBERSHIP_LOOKUP_FAILED").
			With("channel_id", channelID).With("character_id", characterID).Wrap(err)
	}
	return muted, nil
}

// KickMember removes targetID's non-owner membership from channelID and records
// a membership.kick ops event (actorID is the acting owner/admin). The owner
// cannot be kicked: the role<>'owner' filter yields no row for the owner, which
// classifyKickMiss maps to CHANNEL_OWNER_CANNOT_KICK; a target that is not a
// member maps to the uniform CHANNEL_MEMBERSHIP_NOT_FOUND.
func (s *channelStore) KickMember(ctx context.Context, channelID, actorID, targetID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.Code("CHANNEL_KICK_FAILED").With("channel_id", channelID).Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	var role string
	err = tx.QueryRow(
		ctx, `
		DELETE FROM channel_memberships
		WHERE channel_id = $1 AND character_id = $2 AND role <> 'owner'
		RETURNING role`,
		channelID, targetID,
	).Scan(&role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.classifyKickMiss(ctx, tx, channelID, targetID, err)
		}
		return oops.Code("CHANNEL_KICK_FAILED").With("channel_id", channelID).Wrap(err)
	}

	if err := recordChannelOpsEventTx(ctx, tx, channelID, opsKindMembershipKick, actorID, targetID, map[string]any{"prior_role": role}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.Code("CHANNEL_KICK_FAILED").With("channel_id", channelID).Wrap(err)
	}
	return nil
}

// classifyKickMiss distinguishes "owner cannot be kicked" from "target not a
// member" when the kick DELETE matched no row.
func (s *channelStore) classifyKickMiss(ctx context.Context, tx pgx.Tx, channelID, targetID string, cause error) error {
	var role string
	err := tx.QueryRow(
		ctx, `SELECT role FROM channel_memberships WHERE channel_id = $1 AND character_id = $2`,
		channelID, targetID,
	).Scan(&role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("CHANNEL_MEMBERSHIP_NOT_FOUND").
				With("channel_id", channelID).With("character_id", targetID).Wrap(cause)
		}
		return oops.Code("CHANNEL_KICK_FAILED").With("channel_id", channelID).Wrap(err)
	}
	return oops.Code("CHANNEL_OWNER_CANNOT_KICK").
		With("channel_id", channelID).With("character_id", targetID).
		Errorf("the channel owner cannot be kicked; transfer ownership first")
}

// TransferOwnership reassigns ownership of channelID to newOwnerID (who MUST
// already be a non-banned member) and demotes the current owner to member.
// actorID is the acting caller (owner or admin per the service ABAC gate),
// recorded as the ops-event actor. The current owner is read from the channel
// row, so an admin who is not the owner can still transfer. Returns
// CHANNEL_NOT_FOUND for an absent channel and CHANNEL_TRANSFER_TARGET_NOT_MEMBER
// when the target is not an eligible member.
func (s *channelStore) TransferOwnership(ctx context.Context, channelID, actorID, newOwnerID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.Code("CHANNEL_TRANSFER_FAILED").With("channel_id", channelID).Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	var currentOwner string
	err = tx.QueryRow(ctx, `SELECT owner_id FROM channels WHERE id = $1`, channelID).Scan(&currentOwner)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("CHANNEL_NOT_FOUND").With("channel_id", channelID).Wrap(err)
		}
		return oops.Code("CHANNEL_TRANSFER_FAILED").With("channel_id", channelID).Wrap(err)
	}
	if currentOwner == newOwnerID {
		// Already the owner — idempotent no-op.
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return oops.Code("CHANNEL_TRANSFER_FAILED").With("channel_id", channelID).Wrap(commitErr)
		}
		return nil
	}

	// Promote the target: it MUST currently be a non-banned member (not owner).
	var promoted string
	err = tx.QueryRow(
		ctx, `
		UPDATE channel_memberships SET role = 'owner'
		WHERE channel_id = $1 AND character_id = $2 AND role = 'member' AND banned = false
		RETURNING character_id`,
		channelID, newOwnerID,
	).Scan(&promoted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("CHANNEL_TRANSFER_TARGET_NOT_MEMBER").
				With("channel_id", channelID).With("new_owner", newOwnerID).Wrap(err)
		}
		return oops.Code("CHANNEL_TRANSFER_FAILED").With("channel_id", channelID).Wrap(err)
	}

	// Demote the previous owner to member.
	if _, err := tx.Exec(
		ctx, `
		UPDATE channel_memberships SET role = 'member'
		WHERE channel_id = $1 AND character_id = $2 AND role = 'owner'`,
		channelID, currentOwner,
	); err != nil {
		return oops.Code("CHANNEL_TRANSFER_FAILED").With("channel_id", channelID).Wrap(err)
	}

	// Update the denormalised owner_id on the channel row.
	if _, err := tx.Exec(
		ctx, `UPDATE channels SET owner_id = $1 WHERE id = $2`, newOwnerID, channelID,
	); err != nil {
		return oops.Code("CHANNEL_TRANSFER_FAILED").With("channel_id", channelID).Wrap(err)
	}

	if err := recordChannelOpsEventTx(ctx, tx, channelID, opsKindMembershipTransfer, actorID, newOwnerID, map[string]any{"from": currentOwner}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.Code("CHANNEL_TRANSFER_FAILED").With("channel_id", channelID).Wrap(err)
	}
	return nil
}

// prefixedChannelColumns returns channelSelectColumns with each column prefixed
// by the given table alias, for use in JOIN queries.
func prefixedChannelColumns(alias string) string {
	return alias + `.id, ` + alias + `.name, ` + alias + `.type, ` + alias + `.owner_id, ` +
		alias + `.archived, ` + alias + `.is_default, ` + alias + `.retention_days, ` + alias + `.created_at`
}

// systemOwnerID is the sentinel owner_id stamped on seeded default channels.
// Default channels have no human owner and no owner membership row; the
// sentinel documents that ownership is system-held (D-01).
const systemOwnerID = "system-core-channels"

// defaultChannel describes one channel in the seeded default set.
type defaultChannel struct {
	Name string
	Type channelType
}

// defaultChannels is the default channel set seeded idempotently at plugin
// Init (D-01). `Public` MUST be present — it is the channel every guest and
// player receives. The set is intentionally small; further defaults are
// additive and safe (ON CONFLICT DO NOTHING).
var defaultChannels = []defaultChannel{
	{Name: "Public", Type: channelTypePublic},
}

// SeedDefaultChannels idempotently inserts each default channel with
// is_default = true and NO membership row (guest auto-join is served by the
// ListDefaultChannels ∪ memberships union in 01-08, not a membership write).
// Each insert is `ON CONFLICT (lower(name)) DO NOTHING`, so a re-run Init or a
// concurrent plugin start never duplicates a default (T-01-13), and a
// case-insensitive collision with a pre-existing user channel is a silent
// no-op (the existing row wins) rather than an error.
func (s *channelStore) SeedDefaultChannels(ctx context.Context, defaults []defaultChannel) error {
	for _, d := range defaults {
		if !validateChannelName(d.Name) {
			return oops.Code("CHANNEL_NAME_INVALID").With("name", d.Name).
				Errorf("default channel name does not match the accepted pattern")
		}
		if !d.Type.IsValid() {
			return oops.Code("CHANNEL_TYPE_INVALID").With("type", string(d.Type)).
				Errorf("unknown default channel type")
		}
		_, err := s.pool.Exec(
			ctx, `
			INSERT INTO channels (id, name, type, owner_id, is_default)
			VALUES ($1, $2, $3, $4, true)
			ON CONFLICT (lower(name)) DO NOTHING`,
			idgen.New().String(), d.Name, string(d.Type), systemOwnerID,
		)
		if err != nil {
			return oops.Code("CHANNEL_SEED_FAILED").With("name", d.Name).Wrap(err)
		}
	}
	return nil
}

// ListDefaultChannels returns the channels flagged is_default = true, ordered by
// name. This is the seam 01-08 unions into QuerySessionStreams for guest
// auto-join (D-01). Returns an empty (non-nil) slice when none are seeded.
func (s *channelStore) ListDefaultChannels(ctx context.Context) ([]channelRow, error) {
	rows, err := s.pool.Query(
		ctx, `SELECT `+channelSelectColumns+` FROM channels WHERE is_default = true ORDER BY name`,
	)
	if err != nil {
		return nil, oops.Code("CHANNEL_LIST_DEFAULTS_FAILED").Wrap(err)
	}
	defer rows.Close()

	out := make([]channelRow, 0)
	for rows.Next() {
		var r channelRow
		if err := scanChannelRow(rows, &r); err != nil {
			return nil, oops.Code("CHANNEL_LIST_DEFAULTS_FAILED").Wrap(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("CHANNEL_LIST_DEFAULTS_FAILED").Wrap(err)
	}
	return out, nil
}

// IsBannedFrom reports whether characterID has a banned membership row in
// channelID. It is the ban filter QuerySessionStreams (01-08) applies to the
// seeded default channels: a banned character does NOT receive a default's
// stream, while a guest — a character with NO membership row — is not banned and
// receives it. A missing row therefore returns (false, nil): only an explicit
// banned = true membership excludes the default.
func (s *channelStore) IsBannedFrom(ctx context.Context, channelID, characterID string) (bool, error) {
	var banned bool
	err := s.pool.QueryRow(
		ctx, `SELECT banned FROM channel_memberships WHERE channel_id = $1 AND character_id = $2`,
		channelID, characterID,
	).Scan(&banned)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, oops.Code("CHANNEL_MEMBERSHIP_LOOKUP_FAILED").
			With("channel_id", channelID).With("character_id", characterID).Wrap(err)
	}
	return banned, nil
}

// ListChannelsForPrune returns the id/type/retention_days of every channel for
// the retention sweep (D-07). Archived channels are included: their history is
// still subject to retention. Ordered by id for a stable sweep.
func (s *channelStore) ListChannelsForPrune(ctx context.Context) ([]channelPruneInfo, error) {
	rows, err := s.pool.Query(
		ctx, `SELECT id, type, retention_days FROM channels ORDER BY id`,
	)
	if err != nil {
		return nil, oops.Code("CHANNEL_PRUNE_LIST_FAILED").Wrap(err)
	}
	defer rows.Close()

	out := make([]channelPruneInfo, 0)
	for rows.Next() {
		var info channelPruneInfo
		if err := rows.Scan(&info.ID, &info.Type, &info.RetentionDays); err != nil {
			return nil, oops.Code("CHANNEL_PRUNE_LIST_FAILED").Wrap(err)
		}
		out = append(out, info)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("CHANNEL_PRUNE_LIST_FAILED").Wrap(err)
	}
	return out, nil
}

// DeleteChannelLogOlderThan deletes channel_log rows for subject whose event
// timestamp is strictly older than cutoff, returning the number deleted. The
// strict `<` preserves a row exactly at the window edge (D-07 boundary). The
// cutoff is a Go-clock value supplied by the prune sweep and passed as a SQL
// parameter (noremoteclockcompare-compliant).
func (s *channelStore) DeleteChannelLogOlderThan(ctx context.Context, subject string, cutoff time.Time) (int64, error) {
	tag, err := s.pool.Exec(
		ctx, `DELETE FROM channel_log WHERE subject = $1 AND timestamp < $2`,
		subject, pgnanos.From(cutoff),
	)
	if err != nil {
		return 0, oops.Code("CHANNEL_PRUNE_DELETE_FAILED").With("subject", subject).Wrap(err)
	}
	return tag.RowsAffected(), nil
}

// channelOpsEventKind enumerates the recognised ops-event kinds. The dotted
// naming convention is also enforced by the DB CHECK on channel_ops_events.kind.
type channelOpsEventKind string

// Ops event kinds. The full membership/moderation/lifecycle vocabulary is
// declared here; methods emit the subset they implement.
const (
	opsKindLifecycleCreated   channelOpsEventKind = "lifecycle.created"
	opsKindLifecycleArchived  channelOpsEventKind = "lifecycle.archived"
	opsKindMembershipJoin     channelOpsEventKind = "membership.join"
	opsKindMembershipLeave    channelOpsEventKind = "membership.leave"
	opsKindMembershipKick     channelOpsEventKind = "membership.kick"
	opsKindMembershipTransfer channelOpsEventKind = "membership.transfer"
	opsKindModerationBan      channelOpsEventKind = "moderation.ban"
	opsKindModerationUnban    channelOpsEventKind = "moderation.unban"
	opsKindModerationMute     channelOpsEventKind = "moderation.mute"
	opsKindModerationUnmute   channelOpsEventKind = "moderation.unmute"
)

// IsValid reports whether k is one of the declared channelOpsEventKind constants.
func (k channelOpsEventKind) IsValid() bool {
	switch k {
	case opsKindLifecycleCreated, opsKindLifecycleArchived,
		opsKindMembershipJoin, opsKindMembershipLeave, opsKindMembershipKick,
		opsKindMembershipTransfer, opsKindModerationBan, opsKindModerationUnban,
		opsKindModerationMute, opsKindModerationUnmute:
		return true
	}
	return false
}

// recordChannelOpsEventTx inserts a channel_ops_events row inside an existing
// transaction. The kind MUST be a declared channelOpsEventKind — unknown kinds
// are rejected so typos surface as errors instead of writing junk. targetID may
// be empty for whole-channel events (lifecycle.*); pass "" for those. payload is
// marshalled to JSONB; pass nil for none.
func recordChannelOpsEventTx(ctx context.Context, tx pgx.Tx, channelID string, kind channelOpsEventKind, actorID, targetID string, payload map[string]any) error {
	if !kind.IsValid() {
		return oops.Code("CHANNEL_OPS_EVENT_INVALID_KIND").With("kind", string(kind)).
			Errorf("unknown ops event kind")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return oops.Code("CHANNEL_OPS_EVENT_PAYLOAD_MARSHAL_FAILED").With("kind", string(kind)).Wrap(err)
	}

	var targetParam any
	if targetID != "" {
		targetParam = targetID
	}

	_, err = tx.Exec(
		ctx, `
		INSERT INTO channel_ops_events (id, channel_id, kind, actor_id, target_id, payload)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		"coe-"+idgen.New().String(), channelID, string(kind), actorID, targetParam, payloadJSON,
	)
	if err != nil {
		return oops.Code("CHANNEL_OPS_EVENT_INSERT_FAILED").
			With("channel_id", channelID).With("kind", string(kind)).Wrap(err)
	}
	return nil
}
