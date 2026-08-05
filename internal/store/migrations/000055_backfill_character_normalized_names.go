// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/pressly/goose/v3"
	"github.com/samber/oops"

	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
)

// 000055 — character identity backfill and duplicate detection (D-21 step B).
//
// This is the SECOND of three migrations in one release. 000054 (A) added the
// nullable identity columns; this step populates them for every pre-existing
// row and reports every collision it finds; 000056 (C) then does SET NOT NULL
// followed by CREATE UNIQUE INDEX. See 000056's header for the deployment
// precondition that governs the window between this migration and that one.
//
// # This file deliberately holds no SQL of its own
//
// Every characters mutation lives behind the writer boundary in
// internal/world/postgres. test/meta/world_sql_fence_test.go's Go scan walks
// the whole tree, exempts exactly one directory (internal/world/postgres) plus
// _test.go and generated files, and does NOT skip internal/store. Its
// world-sql-fence:allow escape hatch exists only in the MIGRATION scan, which
// globs *.sql — there is no marker path for .go files. A characters mutation
// written here would therefore be an unexemptable violation of a bound
// invariant. So this migration is a thin CALLER of
// worldpostgres.BackfillCharacterIdentity, which owns the statements.
//
// .claude/rules/database-migrations.md forbids long-running data backfills in
// migrations for the same reason a whole-table UPDATE is expensive; this one is
// bounded by the pre-existing character corpus, runs once per database, and is
// a hard precondition of 000056, so it belongs in the chain rather than in a
// separate one-shot job an operator could forget.
//
// # Transactional, and that is load-bearing
//
// goose.AddMigrationContext is transactional by definition. That is what makes
// D-22's halt-and-report posture real: returning an error on collision rolls
// the whole step back and leaves the schema at the prior version, so the
// operator resolves duplicates against a database in a known state rather than
// a half-migrated one.
func init() {
	goose.AddMigrationContext(upBackfillCharacterNormalizedNames, downBackfillCharacterNormalizedNames)
}

// collisionCode is the oops code a pre-existing duplicate halts the migration
// with. It is the operator-facing signal for D-22: startup aborts, and the
// resolution path is `holomush character name duplicates` followed by
// `holomush character name set`.
const collisionCode = "CHARACTER_NAME_COLLISION"

// backfillRevertCode is the oops code the down step wraps a failure with.
//
// The down is a REAL revert rather than the error-returning stub an
// irreversible migration owes its operator (.claude/rules/database-migrations.md).
// Nothing this step wrote is unrecoverable: characters.name is never touched and
// the three columns are pure functions of it, so clearing them restores exactly
// the version-54 shape and a later Up recomputes byte-identical values.
//
// Refusing would also have been operationally wrong. goose rolls down in
// version order, so a Down that errors makes every version below 55
// unreachable — including 000054's own Down, which drops these very columns —
// and wedges any spec that stages an older schema to exercise an earlier
// migration. A rollback an operator cannot perform is not a safety property.
const backfillRevertCode = "CHARACTER_IDENTITY_REVERT_FAILED"

// upBackfillCharacterNormalizedNames computes the three derived identity
// columns for every unpopulated characters row and halts on any collision.
//
// It resolves nothing (D-17): a machine-chosen replacement name would be seated
// onto a player's character with no human judgement. The operator supplies
// every replacement, and that replacement passes the full §6.1.1 pipeline and
// the block list through charname.Gate.Admit before it is written.
func upBackfillCharacterNormalizedNames(ctx context.Context, tx *sql.Tx) error {
	sets, err := worldpostgres.BackfillCharacterIdentity(ctx, tx)
	if err != nil {
		return oops.Code("CHARACTER_IDENTITY_BACKFILL_FAILED").Wrap(err)
	}
	if len(sets) == 0 {
		return nil
	}
	return oops.
		Code(collisionCode).
		With("collision_sets", len(sets)).
		Errorf("%s", formatCollisionReport(sets))
}

// downBackfillCharacterNormalizedNames clears the three derived identity
// columns, restoring the all-NULL shape the schema has at version 54.
//
// Like the up, it carries no SQL of its own — the statement lives behind the
// writer boundary as worldpostgres.ClearCharacterIdentity.
func downBackfillCharacterNormalizedNames(ctx context.Context, tx *sql.Tx) error {
	if err := worldpostgres.ClearCharacterIdentity(ctx, tx); err != nil {
		return oops.Code(backfillRevertCode).Wrap(err)
	}
	return nil
}

// formatCollisionReport renders every collision set with each member's id,
// name, owner and created_at — D-17's "enough context to decide".
//
// The two kinds are labelled distinctly on purpose. A normalized-name set is
// two rows that would violate 000056's UNIQUE index. A skeleton set is two rows
// that are visual impersonations of each other and that 000056 will happily let
// coexist: NFKC does not collapse cross-script confusables, so the pair has
// DIFFERENT normalized names by construction — which is precisely what makes it
// confusable, and precisely why the index over normalized_name structurally
// cannot see it (D-30 part 3). Reporting the two undifferentiated makes the
// second look like a false positive and invites an operator to dismiss it.
func formatCollisionReport(sets []worldpostgres.IdentityCollisionSet) string {
	var b strings.Builder
	fmt.Fprintf(
		&b,
		"migration 55 halted: %d pre-existing character-name collision set(s) must be resolved before migration 56 can create the UNIQUE index.\n"+
			"Resolve each with: holomush character name duplicates, then holomush character name set <character-id> <new-name>.\n",
		len(sets),
	)
	for _, set := range sets {
		fmt.Fprintf(&b, "\n%s collision on %q (%d characters):\n",
			describeCollisionKind(set.Kind), set.Key, len(set.Members))
		for _, m := range set.Members {
			fmt.Fprintf(&b, "  id=%s name=%q player_id=%s created_at=%d\n",
				m.ID, m.Name, m.PlayerID, m.CreatedAt)
		}
	}
	return b.String()
}

// describeCollisionKind turns a collision kind into the operator-facing phrase
// that says what the set actually means.
func describeCollisionKind(kind worldpostgres.IdentityCollisionKind) string {
	switch kind {
	case worldpostgres.CollisionNormalizedName:
		return "NORMALIZED-NAME (same §6.1.1 uniqueness key; migration 56's UNIQUE index would reject these)"
	case worldpostgres.CollisionSkeleton:
		return "SKELETON (UTS #39 confusables — these names LOOK alike but have different normalized keys, " +
			"so migration 56's index would NOT catch them; this scan is the only thing that sees them)"
	default:
		return "UNKNOWN-KIND"
	}
}
