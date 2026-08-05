// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"database/sql"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/charname"
)

// This file lives in internal/world/postgres, and not in
// internal/store/migrations where its only caller will be, for ONE reason:
// test/meta/world_sql_fence_test.go's Go scan exempts exactly one directory
// (internal/world/postgres) and its world-sql-fence:allow escape hatch exists
// only in the MIGRATION scan, which globs *.sql. There is NO marker path for
// .go files. So a characters mutation planted in a Go migration would be an
// unexemptable fence violation, and the migration must be a thin caller with no
// SQL literal of its own.

// IdentityCollisionKind names which uniqueness dimension a collision set shares.
type IdentityCollisionKind string

const (
	// CollisionNormalizedName groups rows sharing a §6.1.1 normalized key. These
	// are the rows migration 000056's UNIQUE index would reject.
	CollisionNormalizedName IdentityCollisionKind = "normalized_name"

	// CollisionSkeleton groups rows sharing a UTS #39 confusable skeleton.
	//
	// This dimension is NOT redundant with the one above — it is the case a
	// normalized-name scan can NEVER find. NFKC deliberately does not collapse
	// cross-script confusables (that is exactly why the skeleton mechanism
	// exists separately), so a pre-existing confusable pair has DIFFERENT
	// normalized names by construction. Without this scan two pre-existing rows
	// that are confusables of each other are detected by nothing in this phase,
	// 000056's unique index passes over them, and IDENT-06 stays unmet for the
	// existing corpus (D-30 part 3).
	CollisionSkeleton IdentityCollisionKind = "name_skeleton"
)

// IdentityCollisionMember is one row in a collision set, carrying the fields an
// operator needs to decide which character keeps the name.
type IdentityCollisionMember struct {
	ID        string
	Name      string
	PlayerID  string
	CreatedAt int64
}

// IdentityCollisionSet is a group of two or more characters sharing a
// uniqueness key.
type IdentityCollisionSet struct {
	// Kind says which dimension the set shares — the two are handled
	// identically by D-22's halt-and-report path, but an operator needs to know
	// which one they are looking at.
	Kind IdentityCollisionKind
	// Key is the shared normalized name or skeleton.
	Key string
	// Members are the colliding rows, oldest first.
	Members []IdentityCollisionMember
}

// BackfillExecutor is the minimal executor BackfillCharacterIdentity needs,
// declared CONSUMER-SIDE in this package.
//
// It is two methods rather than an imported concrete type on purpose: goose Go
// migrations run on a database/sql *sql.Tx while this package is pgx, and
// importing goose (or internal/store) from the writer boundary would close a
// real import cycle — internal/store blank-imports internal/store/migrations,
// so internal/world/postgres -> internal/store would make plan 02-12's
// migration unbuildable (T-02-77).
type BackfillExecutor interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// BackfillCharacterIdentity computes normalized_name, name_skeleton and
// name_skeleton_unicode_version for every characters row whose identity columns
// are unpopulated, and reports every collision it finds.
//
// # It writes no name and it resolves nothing
//
// The UPDATE assigns the three DERIVED columns only; characters.name is never
// written, which is why census rule A does not fire on it and why the rule is
// deliberately one-directional.
//
// It also MUST NOT run charname.Gate, MUST NOT consult the block list, and MUST
// NOT reject anything. These are names already admitted under the old rules; per
// D-17 this function DETECTS and REPORTS, and the operator resolves. Keys come
// from charname.Normalize and charname.Skeleton directly.
//
// Running it twice over the same rows produces identical column values the
// second time: the pipeline is deterministic and the row filter is
// "identity columns unpopulated", so a second pass is a no-op.
func BackfillCharacterIdentity(ctx context.Context, db BackfillExecutor) ([]IdentityCollisionSet, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name FROM characters
		WHERE normalized_name IS NULL
		   OR name_skeleton IS NULL
		   OR name_skeleton_unicode_version IS NULL
	`)
	if err != nil {
		return nil, oops.Code("CHARACTER_IDENTITY_BACKFILL_SCAN_FAILED").Wrap(err)
	}

	type pending struct {
		id       string
		key      string
		skeleton string
	}
	var todo []pending
	for rows.Next() {
		var id, name string
		if scanErr := rows.Scan(&id, &name); scanErr != nil {
			_ = rows.Close() //nolint:errcheck // the scan already failed; the close error would mask it
			return nil, oops.Code("CHARACTER_IDENTITY_BACKFILL_SCAN_FAILED").With("id", id).Wrap(scanErr)
		}
		normalized, nErr := charname.Normalize(name)
		if nErr != nil {
			_ = rows.Close() //nolint:errcheck // the scan already failed; the close error would mask it
			return nil, oops.Code("CHARACTER_IDENTITY_BACKFILL_NORMALIZE_FAILED").
				With("id", id).
				Wrap(nErr)
		}
		todo = append(todo, pending{
			id:       id,
			key:      normalized.Key,
			skeleton: charname.Skeleton(normalized.Key),
		})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		_ = rows.Close() //nolint:errcheck // the iteration already failed; the close error would mask it
		return nil, oops.Code("CHARACTER_IDENTITY_BACKFILL_SCAN_FAILED").Wrap(rowsErr)
	}
	if closeErr := rows.Close(); closeErr != nil {
		return nil, oops.Code("CHARACTER_IDENTITY_BACKFILL_SCAN_FAILED").Wrap(closeErr)
	}

	for _, p := range todo {
		if _, execErr := db.ExecContext(ctx, `
			UPDATE characters
			SET normalized_name = $2,
			    name_skeleton = $3,
			    name_skeleton_unicode_version = $4
			WHERE id = $1
		`, p.id, p.key, p.skeleton, charname.UnicodeVersion); execErr != nil {
			return nil, oops.Code("CHARACTER_IDENTITY_BACKFILL_WRITE_FAILED").
				With("id", p.id).
				Wrap(execErr)
		}
	}

	normalizedSets, err := collectIdentityCollisions(ctx, db, CollisionNormalizedName)
	if err != nil {
		return nil, err
	}
	skeletonSets, err := collectIdentityCollisions(ctx, db, CollisionSkeleton)
	if err != nil {
		return nil, err
	}
	return append(normalizedSets, skeletonSets...), nil
}

// collectIdentityCollisions groups characters by one identity column and
// returns every group of two or more.
func collectIdentityCollisions(ctx context.Context, db BackfillExecutor, kind IdentityCollisionKind) ([]IdentityCollisionSet, error) {
	// The column name is interpolated from a CLOSED constant vocabulary, never
	// from caller input: the only two values are the package constants above.
	var column string
	switch kind {
	case CollisionNormalizedName:
		column = "normalized_name"
	case CollisionSkeleton:
		column = "name_skeleton"
	default:
		return nil, oops.Code("CHARACTER_IDENTITY_COLLISION_KIND_UNKNOWN").
			With("kind", string(kind)).
			Errorf("unknown identity collision kind")
	}

	rows, err := db.QueryContext(ctx, `
		SELECT c.`+column+`, c.id, c.name, c.player_id, c.created_at
		FROM characters c
		JOIN (
			SELECT `+column+` AS k
			FROM characters
			WHERE `+column+` IS NOT NULL
			GROUP BY `+column+`
			HAVING count(*) > 1
		) d ON d.k = c.`+column+`
		ORDER BY c.`+column+`, c.created_at, c.id
	`)
	if err != nil {
		return nil, oops.Code("CHARACTER_IDENTITY_COLLISION_SCAN_FAILED").
			With("kind", string(kind)).
			Wrap(err)
	}
	defer rows.Close() //nolint:errcheck // the deferred close is best-effort; rows.Err() below is the real signal

	var (
		sets    []IdentityCollisionSet
		current *IdentityCollisionSet
	)
	for rows.Next() {
		var key string
		var m IdentityCollisionMember
		if scanErr := rows.Scan(&key, &m.ID, &m.Name, &m.PlayerID, &m.CreatedAt); scanErr != nil {
			return nil, oops.Code("CHARACTER_IDENTITY_COLLISION_SCAN_FAILED").
				With("kind", string(kind)).
				Wrap(scanErr)
		}
		if current == nil || current.Key != key {
			sets = append(sets, IdentityCollisionSet{Kind: kind, Key: key})
			current = &sets[len(sets)-1]
		}
		current.Members = append(current.Members, m)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, oops.Code("CHARACTER_IDENTITY_COLLISION_SCAN_FAILED").
			With("kind", string(kind)).
			Wrap(rowsErr)
	}
	return sets, nil
}
