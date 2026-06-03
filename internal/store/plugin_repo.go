// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/pgnanos"
)

// PluginUpsertInput is the row data for Upsert. ContentHash MAY be nil
// (setting plugins have no executable artifact).
type PluginUpsertInput struct {
	Name         string
	DisplayName  string
	Version      string
	ManifestHash []byte
	ContentHash  []byte
}

// PluginRow materializes a row from the plugins table.
type PluginRow struct {
	ID           ulid.ULID
	Name         string
	DisplayName  string
	Version      string
	ManifestHash []byte
	ContentHash  []byte
	FirstSeenAt  time.Time
	LastSeenAt   time.Time
	GcAt         *time.Time
}

// DriftReport is non-nil when Upsert observed manifest_hash, content_hash,
// or version drift on an existing row.
type DriftReport struct {
	OldManifestHash []byte
	NewManifestHash []byte
	OldContentHash  []byte
	NewContentHash  []byte
	VersionBefore   string
	VersionAfter    string
}

// PluginRepo persists per-plugin identity (ULID + name + revision metadata).
type PluginRepo interface {
	Upsert(ctx context.Context, in PluginUpsertInput) (id ulid.ULID, drift *DriftReport, err error)
	ListAll(ctx context.Context) ([]PluginRow, error)
	SweepInactive(ctx context.Context, retentionDays int) ([]PluginRow, error)
}

// PostgresPluginRepo implements PluginRepo against pgxpool.Pool.
type PostgresPluginRepo struct {
	pool *pgxpool.Pool
}

// NewPostgresPluginRepo returns a PostgresPluginRepo backed by pool.
func NewPostgresPluginRepo(pool *pgxpool.Pool) *PostgresPluginRepo {
	return &PostgresPluginRepo{pool: pool}
}

// Upsert inserts a new plugin row (fresh ULID) or updates last_seen_at on an
// existing active row. Returns a non-nil DriftReport when manifest_hash,
// content_hash, or version changed since the previous registration.
func (r *PostgresPluginRepo) Upsert(ctx context.Context, in PluginUpsertInput) (ulid.ULID, *DriftReport, error) {
	// Read existing row by name (active only).
	var (
		idBytes      []byte
		existingName string
		dispName     string
		version      string
		manifestHash []byte
		contentHash  []byte
		firstSeenAt  pgnanos.Time
		lastSeenAt   pgnanos.Time
		gcAt         *pgnanos.Time
	)
	scanErr := r.pool.QueryRow(ctx, `
		SELECT id, name, display_name, version, manifest_hash, content_hash,
		       first_seen_at, last_seen_at, gc_at
		  FROM plugins
		 WHERE name = $1 AND gc_at IS NULL
	`, in.Name).Scan(&idBytes, &existingName, &dispName, &version,
		&manifestHash, &contentHash, &firstSeenAt, &lastSeenAt, &gcAt)
	switch {
	case errors.Is(scanErr, pgx.ErrNoRows):
		// INSERT path
		newID := idgen.New()
		_, insertErr := r.pool.Exec(ctx, `
			INSERT INTO plugins (id, name, display_name, version,
			                    manifest_hash, content_hash)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, newID[:], in.Name, in.DisplayName, in.Version, in.ManifestHash, in.ContentHash)
		if insertErr != nil {
			return ulid.ULID{}, nil, oops.Code("PLUGIN_REPO_INSERT").
				With("name", in.Name).Wrap(insertErr)
		}
		return newID, nil, nil
	case scanErr != nil:
		return ulid.ULID{}, nil, oops.Code("PLUGIN_REPO_SELECT").
			With("name", in.Name).Wrap(scanErr)
	}

	// UPDATE path
	var existingID ulid.ULID
	copy(existingID[:], idBytes)

	manifestChanged := !bytes.Equal(manifestHash, in.ManifestHash)
	contentChanged := !bytes.Equal(contentHash, in.ContentHash)
	versionChanged := version != in.Version

	var drift *DriftReport
	if manifestChanged || contentChanged || versionChanged {
		drift = &DriftReport{
			OldManifestHash: manifestHash,
			NewManifestHash: in.ManifestHash,
			OldContentHash:  contentHash,
			NewContentHash:  in.ContentHash,
			VersionBefore:   version,
			VersionAfter:    in.Version,
		}
	}

	_, updateErr := r.pool.Exec(ctx, `
		UPDATE plugins
		   SET manifest_hash = $1, content_hash = $2, version = $3,
		       last_seen_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
		 WHERE id = $4
	`, in.ManifestHash, in.ContentHash, in.Version, existingID[:])
	if updateErr != nil {
		return ulid.ULID{}, nil, oops.Code("PLUGIN_REPO_UPDATE").
			With("name", in.Name).Wrap(updateErr)
	}
	return existingID, drift, nil
}

// ListAll returns all plugin rows including deactivated ones (gc_at IS NOT NULL).
func (r *PostgresPluginRepo) ListAll(ctx context.Context) ([]PluginRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, display_name, version, manifest_hash, content_hash,
		       first_seen_at, last_seen_at, gc_at
		  FROM plugins
	`)
	if err != nil {
		return nil, oops.Code("PLUGIN_REPO_LIST_ALL").Wrap(err)
	}
	defer rows.Close()

	var out []PluginRow
	for rows.Next() {
		var p PluginRow
		var idBytes []byte
		var firstSeenAt pgnanos.Time
		var lastSeenAt pgnanos.Time
		var gcAt *pgnanos.Time
		if err := rows.Scan(&idBytes, &p.Name, &p.DisplayName, &p.Version,
			&p.ManifestHash, &p.ContentHash,
			&firstSeenAt, &lastSeenAt, &gcAt); err != nil {
			return nil, oops.Code("PLUGIN_REPO_LIST_ALL_SCAN").Wrap(err)
		}
		copy(p.ID[:], idBytes)
		p.FirstSeenAt = firstSeenAt.Time()
		p.LastSeenAt = lastSeenAt.Time()
		if gcAt != nil {
			t := gcAt.Time()
			p.GcAt = &t
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("PLUGIN_REPO_LIST_ALL_ROWS").Wrap(err)
	}
	return out, nil
}

// SweepInactive marks plugins inactive (sets gc_at) whose last_seen_at is
// older than retentionDays. It never DELETEs rows (INV-PLUGIN-17). Returns the
// swept rows so the caller can log or act on them.
//
// retentionDays MUST be non-negative. A negative value would push the cutoff
// into the future and mass-mark active plugins as GC candidates.
func (r *PostgresPluginRepo) SweepInactive(ctx context.Context, retentionDays int) ([]PluginRow, error) {
	if retentionDays < 0 {
		return nil, oops.Code("PLUGIN_REPO_SWEEP_INVALID_RETENTION").
			With("retention_days", retentionDays).
			Errorf("retentionDays must be non-negative")
	}
	rows, err := r.pool.Query(ctx, `
		UPDATE plugins
		   SET gc_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
		 WHERE gc_at IS NULL
		   AND last_seen_at < (EXTRACT(EPOCH FROM now() - make_interval(days => $1)) * 1e9)::BIGINT
		 RETURNING id, name, display_name, version, manifest_hash,
		           content_hash, first_seen_at, last_seen_at, gc_at
	`, retentionDays)
	if err != nil {
		return nil, oops.Code("PLUGIN_REPO_SWEEP").
			With("retention_days", retentionDays).Wrap(err)
	}
	defer rows.Close()

	var out []PluginRow
	for rows.Next() {
		var p PluginRow
		var idBytes []byte
		var firstSeenAt pgnanos.Time
		var lastSeenAt pgnanos.Time
		var gcAt *pgnanos.Time
		if err := rows.Scan(&idBytes, &p.Name, &p.DisplayName, &p.Version,
			&p.ManifestHash, &p.ContentHash,
			&firstSeenAt, &lastSeenAt, &gcAt); err != nil {
			return nil, oops.Code("PLUGIN_REPO_SWEEP_SCAN").Wrap(err)
		}
		copy(p.ID[:], idBytes)
		p.FirstSeenAt = firstSeenAt.Time()
		p.LastSeenAt = lastSeenAt.Time()
		if gcAt != nil {
			t := gcAt.Time()
			p.GcAt = &t
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("PLUGIN_REPO_SWEEP_ROWS").
			With("retention_days", retentionDays).Wrap(err)
	}
	return out, nil
}
