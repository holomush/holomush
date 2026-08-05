// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"hash/fnv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/charname"
)

// This file is D-30 part 2: the confusable guarantee is ENFORCED by
// serialization, not left advisory.
//
// # Why the gate's own check is not enough
//
// charname.Gate.Check reads the skeleton corpus and then the write happens
// later. The skeleton index is DELIBERATELY non-unique (D-21, reaffirmed by
// D-30 part 1: the confusables table shifts between Unicode versions, and a
// unique constraint would block a legitimate post-upgrade recompute that
// collapses two live rows), so nothing at the storage layer serializes that
// read against the insert.
//
// Two concurrent creates whose skeletons MATCH but whose normalized names
// DIFFER therefore both read "not exists" and both land — and migration
// 000056's normalized_name unique index structurally CANNOT catch them, because
// differing normalized names is precisely what makes a pair confusable. Without
// this guard, ROADMAP success criterion 1's "rejected server-side" holds only
// for a sequential single writer.
//
// Gate.Check's skeleton read stays exactly what it is: a friendly pre-check, in
// the same sense §6.1.3 assigns to the existence pre-check. It produces a good
// error most of the time and it does not close the race. This guard closes it.

// skeletonLockKey derives the advisory-lock key for a skeleton.
//
// It is computed in GO with FNV-1a rather than by calling Postgres's hashtext,
// so every replica computes the same key for the same skeleton and the key does
// not depend on an undocumented, version-unstable server function.
//
// The uint64 -> int64 reinterpretation is deliberate: pg_advisory_xact_lock's
// single-argument form takes a signed bigint, and a wraparound reinterpretation
// is a bijection, so distinct hashes stay distinct keys.
func skeletonLockKey(skeleton string) int64 {
	h := fnv.New64a()
	// hash.Hash.Write never returns an error (documented on the interface).
	_, _ = h.Write([]byte(skeleton))
	return int64(h.Sum64()) //nolint:gosec // G115: a wraparound reinterpretation is intended; the key space is the full int64.
}

// guardSkeleton serializes the confusable check with the write that follows it.
//
// It MUST be called INSIDE the caller's write transaction, before the identity
// write is issued, by every method that writes characters.name. Census rule E
// (test/meta/character_name_admission_test.go) asserts that structurally.
//
// Three steps, in order:
//
//  1. Take a TRANSACTION-SCOPED advisory lock keyed on the admitted skeleton.
//     Transaction-scoped is load-bearing: it releases on commit OR rollback,
//     so there is no unlock path an error return can skip.
//  2. Re-run the skeleton query inside the same transaction, applying the SAME
//     two rules charname.Gate applies — exclude the row named by excluding
//     (Rename passes its own id; Create passes nil), and treat any
//     name_skeleton IS NULL row as making the corpus unverifiable.
//  3. Refuse before any write: NAME_CONFUSABLE on a hit,
//     NAME_SKELETON_UNVERIFIABLE when the corpus cannot answer.
//
// The fail-closed rule (D-30) is not this phase's backfill gap alone: it
// reopens after every future Unicode-version recompute that is interrupted. A
// re-check that reads a NULL skeleton and concludes "no collision" admits a
// confusable of an existing row — the same hazard as T-02-06, one layer down.
func guardSkeleton(ctx context.Context, tx pgx.Tx, admitted charname.Admitted, excluding *ulid.ULID) error {
	skeleton := admitted.Skeleton()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, skeletonLockKey(skeleton)); err != nil {
		return oops.Code("NAME_SKELETON_LOCK_FAILED").
			With("operation", "acquire skeleton advisory lock").
			Wrap(err)
	}

	var excludingArg *string
	if excluding != nil {
		s := excluding.String()
		excludingArg = &s
	}

	var exists, unverifiable bool
	err := tx.QueryRow(ctx, `
		SELECT
			EXISTS(
				SELECT 1 FROM characters
				WHERE name_skeleton = $1
				  AND ($2::text IS NULL OR id::text <> $2)
			),
			EXISTS(SELECT 1 FROM characters WHERE name_skeleton IS NULL)
	`, skeleton, excludingArg).Scan(&exists, &unverifiable)
	if err != nil {
		return oops.Code("NAME_SKELETON_LOOKUP_FAILED").
			With("operation", "in-transaction skeleton re-check").
			Wrap(err)
	}

	if unverifiable {
		// Fail closed. The corpus cannot answer the question, so admitting
		// would be a guess — and the guess that costs something is the one that
		// seats a confusable.
		return oops.Code("NAME_SKELETON_UNVERIFIABLE").
			Errorf("character name cannot be checked for confusability right now; try again shortly")
	}
	if exists {
		// Names NO colliding character, for the same reason Gate.Check does not:
		// naming it would turn this into a name-enumeration oracle.
		return oops.Code("NAME_CONFUSABLE").
			Errorf("that character name is too similar to an existing one")
	}

	return nil
}

// SkeletonLookup answers charname.Gate's confusable question against the stored
// corpus. It is the PRODUCTION charname.SkeletonLookup.
//
// It is a READ, so it is outside the raw-world-SQL fence's INSERT/UPDATE/DELETE
// scope; it lives here because it must ask the corpus EXACTLY the two questions
// guardSkeleton asks inside the write transaction. Two implementations that
// drift would mean the gate and the guard disagree about what a collision is,
// and the divergence would show up only as a create that passes the friendly
// check and is then refused by the writer.
type SkeletonLookup struct {
	pool *pgxpool.Pool
}

// NewSkeletonLookup constructs the production skeleton lookup.
func NewSkeletonLookup(pool *pgxpool.Pool) *SkeletonLookup {
	return &SkeletonLookup{pool: pool}
}

// SkeletonExists reports whether any OTHER character holds this skeleton, and
// whether the corpus can answer at all.
//
// unverifiable is set when any characters row still has name_skeleton IS NULL:
// a lookup that read a NULL skeleton and reported "no collision" would admit a
// confusable of an existing row (D-30).
func (l *SkeletonLookup) SkeletonExists(ctx context.Context, skeleton string, excluding *ulid.ULID) (exists, unverifiable bool, err error) {
	var excludingArg *string
	if excluding != nil {
		s := excluding.String()
		excludingArg = &s
	}

	err = l.pool.QueryRow(ctx, `
		SELECT
			EXISTS(
				SELECT 1 FROM characters
				WHERE name_skeleton = $1
				  AND ($2::text IS NULL OR id::text <> $2)
			),
			EXISTS(SELECT 1 FROM characters WHERE name_skeleton IS NULL)
	`, skeleton, excludingArg).Scan(&exists, &unverifiable)
	if err != nil {
		return false, false, oops.Code("NAME_SKELETON_LOOKUP_FAILED").
			With("operation", "corpus skeleton lookup").
			Wrap(err)
	}
	return exists, unverifiable, nil
}

// Compile-time interface check.
var _ charname.SkeletonLookup = (*SkeletonLookup)(nil)
