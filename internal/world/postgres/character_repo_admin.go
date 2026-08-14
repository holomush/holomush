// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
)

// adminCharacterProjection is the SELECT list of [CharacterRepository.Get]
// verbatim, plus the joined players.username.
//
// It is defined BY REFERENCE to the full-entity read rather than as a hand
// copy of a column list, because the one thing that must not happen here is a
// drift into ListAll's id+name-only projection — whose Status and LastActiveAt
// are zero BY OMISSION, so a lifecycle decision made from such a row violates
// INV-WORLD-5 while looking entirely well-formed.
//
// normalized_name is deliberately NOT in this list. world.Character has no such
// field (internal/world/character.go:15-39) and Get does not select it; the
// column is written by the ORDER BY and by the search predicate as SQL text and
// is never carried on a row.
const adminCharacterProjection = `
	c.id, c.player_id, c.name, c.description, c.location_id, c.created_at,
	c.version, c.status, c.last_active_at, p.username`

// adminCharacterFrom is the one join both admin reads share. players.id is a
// TEXT PRIMARY KEY and characters.player_id is NOT NULL, so the join is
// many-to-one and cannot multiply a character row.
const adminCharacterFrom = ` FROM characters c JOIN players p ON p.id = c.player_id`

// AdminReadTxOptions is the transaction shape both admin reads open.
//
// AccessMode: ReadOnly alone would NOT be enough, and the difference is the
// whole point of exporting this: PostgreSQL's default isolation is
// read-committed, under which EVERY STATEMENT takes its own snapshot even
// inside a single transaction. A concurrent insert landing between the scalar
// count and the page query would then make TotalCount disagree with the rows
// returned — the exact inconsistency the transaction was added to prevent,
// claimed but not delivered. RepeatableRead is what makes the claim true.
//
// It costs nothing here: this is a two-statement read of one small bounded
// page, holding no locks and blocking no writer.
//
// It is exported so the integration suite can reproduce the EXACT transaction
// shape the repository opens rather than assert the isolation level by reading
// the source.
func AdminReadTxOptions() pgx.TxOptions {
	return pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}
}

// AdminListCharacters returns one page of the cross-player admin character list
// plus the total over the same filtered set.
//
// The caller is authorized UPSTREAM, by the admin-portal interceptor's
// `admin_section:characters` decision (D-99). This method evaluates no policy;
// it is a bounded projection whose field list is 01-SPEC §11.3's.
func (r *CharacterRepository) AdminListCharacters(
	ctx context.Context,
	opts world.AdminCharacterListOptions,
) (world.AdminCharacterPage, error) {
	return r.adminCharacterPage(ctx, "", opts)
}

// AdminSearchCharacters returns one page of characters whose STORED
// normalized_name or whose joined players.username contains normalizedTerm as a
// substring, plus the total over that same filtered set.
//
// # The term arrives ALREADY NORMALIZED, and that is the contract
//
// normalizedTerm has been through the same charname pipeline that produced the
// stored characters.normalized_name. This method does not normalize and does
// not lower-case: there is exactly ONE normalizer, service-side, and a second
// one here would be a second definition of name equality that could drift from
// the column it is matched against. An EMPTY term bypasses the predicate
// entirely and returns the unfiltered page.
//
// # Why players.username is admissible and nothing else is (D-106)
//
// username is an OOC IDENTITY column the `admin` audience already sees on every
// row of this very list — it is not profile content, which is why widening the
// search to it does not touch 01-SPEC §11.3's prose-search prohibition. Widening
// it any further WOULD: characters.description and the twelve `profile.*`
// property names are privacy-bearing prose, and a substring predicate over them
// is a content search over player-authored text wearing the name of a lookup.
// §11.1 forbids it, `06-CONTEXT.md`'s D-106 defers it explicitly, and
// TestAdminSearchPredicatesNameOnlySearchableColumns in test/meta fences it by
// parsing the predicates in this package rather than by trusting this comment.
func (r *CharacterRepository) AdminSearchCharacters(
	ctx context.Context,
	normalizedTerm string,
	opts world.AdminCharacterListOptions,
) (world.AdminCharacterPage, error) {
	return r.adminCharacterPage(ctx, normalizedTerm, opts)
}

// adminCharacterPage runs the count and the page in ONE snapshot.
func (r *CharacterRepository) adminCharacterPage(
	ctx context.Context,
	normalizedTerm string,
	opts world.AdminCharacterListOptions,
) (world.AdminCharacterPage, error) {
	orderBy, err := adminOrderByClause(opts)
	if err != nil {
		return world.AdminCharacterPage{}, err
	}

	where, args := adminWhereClause(normalizedTerm, opts)

	var page world.AdminCharacterPage
	tx, err := r.pool.BeginTx(ctx, AdminReadTxOptions())
	if err != nil {
		return world.AdminCharacterPage{}, oops.Code("CHARACTER_ADMIN_LIST_FAILED").Wrap(err)
	}
	// A read-only transaction has nothing to commit; the rollback is the release.
	// Its error is deliberately dropped: it fires only when the transaction is
	// already finished, and surfacing it would replace a successful read's result
	// with a cleanup artefact.
	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			errutil.LogErrorContext(ctx, "admin characters: read transaction rollback failed", rbErr)
		}
	}()

	// The total comes from its OWN scalar count over the same filtered set, NOT
	// from a COUNT(*) OVER () window column: once OFFSET has removed every row
	// there is no row left to carry a window value, so a page beyond the end
	// would report 0 instead of the true total.
	if countErr := tx.QueryRow(
		ctx,
		`SELECT count(*)`+adminCharacterFrom+where, args...,
	).Scan(&page.TotalCount); countErr != nil {
		return world.AdminCharacterPage{}, oops.Code("CHARACTER_ADMIN_COUNT_FAILED").Wrap(countErr)
	}

	pageArgs := append(append([]any{}, args...), opts.Limit, opts.Offset)
	query := `SELECT` + adminCharacterProjection + adminCharacterFrom + where + orderBy +
		` LIMIT $` + itoa(len(pageArgs)-1) + ` OFFSET $` + itoa(len(pageArgs))

	rows, err := tx.Query(ctx, query, pageArgs...)
	if err != nil {
		return world.AdminCharacterPage{}, oops.Code("CHARACTER_ADMIN_LIST_FAILED").Wrap(err)
	}
	defer rows.Close()

	page.Rows = make([]world.AdminCharacterRow, 0)
	for rows.Next() {
		var char world.Character
		var f characterScanFields
		var username string
		if err := rows.Scan(
			&f.idStr, &f.playerIDStr, &char.Name, &char.Description,
			&f.locationIDStr, &f.createdAt, &char.Version,
			&f.statusStr, &char.LastActiveAt, &username,
		); err != nil {
			return world.AdminCharacterPage{}, oops.Code("CHARACTER_ADMIN_LIST_SCAN_FAILED").Wrap(err)
		}
		if err := parseCharacterFromFields(&f, &char); err != nil {
			return world.AdminCharacterPage{}, err
		}
		page.Rows = append(page.Rows, world.AdminCharacterRow{Character: &char, PlayerUsername: username})
	}
	if err := rows.Err(); err != nil {
		return world.AdminCharacterPage{}, oops.Code("CHARACTER_ADMIN_LIST_ITERATE_FAILED").Wrap(err)
	}

	return page, nil
}

// adminWhereClause builds the filter and its bound parameters.
//
// EVERY value is a bound $n parameter. Nothing a caller supplies is
// concatenated into SQL text; the only text this function chooses is the
// column names, which are literals here.
func adminWhereClause(normalizedTerm string, opts world.AdminCharacterListOptions) (where string, args []any) {
	clauses := make([]string, 0, 3)
	args = make([]any, 0, 3)

	if normalizedTerm != "" {
		args = append(args, "%"+escapeLikeWildcards(normalizedTerm)+"%")
		n := itoa(len(args))
		// ESCAPE rides on BOTH arms. Without it a typed `a_b` silently matches
		// `axb` and `100%` matches every row containing `100`.
		clauses = append(clauses,
			`(c.normalized_name ILIKE $`+n+` ESCAPE '\' OR p.username ILIKE $`+n+` ESCAPE '\')`)
	}
	if opts.StatusFilter != nil {
		args = append(args, string(*opts.StatusFilter))
		clauses = append(clauses, `c.status = $`+itoa(len(args)))
	}
	if opts.PlayerID != nil {
		args = append(args, opts.PlayerID.String())
		clauses = append(clauses, `c.player_id = $`+itoa(len(args)))
	}

	if len(clauses) == 0 {
		return "", args
	}
	return ` WHERE ` + strings.Join(clauses, ` AND `), args
}

// escapeLikeWildcards makes a normalized search term match LITERALLY.
//
// charname.Normalize folds case and strips format runes but passes `%`, `_` and
// `\` through untouched — none of them is in category Cf — so an operator who
// typed `a_b` would otherwise get every three-character `a?b`. Parameter
// binding already closes injection (T-06-21); this is the CORRECTNESS half.
//
// The backslash is replaced FIRST. Doing it after would re-escape the
// backslashes this function itself introduced, turning `a_b` into `a\\_b`,
// which matches a literal backslash followed by any character.
func escapeLikeWildcards(term string) string {
	term = strings.ReplaceAll(term, `\`, `\\`)
	term = strings.ReplaceAll(term, `%`, `\%`)
	term = strings.ReplaceAll(term, `_`, `\_`)
	return term
}

// adminOrderByClause emits the ordering from a CLOSED switch over the sort
// enum. No caller-supplied text reaches it, which is what keeps T-06-21's
// ORDER-BY half closed by construction rather than by escaping.
//
// There are FIVE arms and a DENYING default. There is no player_id arm: §11.3
// marks it Sort=No and §14 row 12 re-asserts that an ordering on it would leak
// creation sequence.
//
// Each clause is load-bearing:
//
//   - `(c.last_active_at = 0)` FIRST is what puts the never-active sentinel last
//     in BOTH directions. Descending gets it free as the column minimum;
//     ascending does not, so a one-direction test cannot observe this clause.
//   - the primary key in the requested direction.
//   - `c.normalized_name ASC` LAST is the tiebreak that makes the order TOTAL.
//     Without it, rows tying on the primary key fall back to whatever secondary
//     key the plan happens to carry, and the ordering is untestable.
func adminOrderByClause(opts world.AdminCharacterListOptions) (string, error) {
	dir := ` ASC`
	if opts.Descending {
		dir = ` DESC`
	}

	switch opts.SortField {
	case world.AdminSortLastActiveAt:
		return ` ORDER BY (c.last_active_at = 0), c.last_active_at` + dir + `, c.normalized_name ASC`, nil
	case world.AdminSortName:
		// §11.3's name row orders on the STORED normal form of §6.1.3, not on
		// the display name; the two orderings differ observably under case and
		// NFKC folding.
		//
		// This arm carries NO trailing tiebreak, and its absence is deliberate:
		// migration 000056 makes normalized_name UNIQUE, so this key is already
		// a total order and `ORDER BY c.normalized_name DESC, c.normalized_name
		// ASC` would be a clause that can never be reached — a tiebreak on
		// itself. Writing it for symmetry would make the shape uniform and the
		// meaning false, and a test asserting the tiebreak here would be
		// vacuous by construction.
		return ` ORDER BY c.normalized_name` + dir, nil
	case world.AdminSortCreatedAt:
		return ` ORDER BY c.created_at` + dir + `, c.normalized_name ASC`, nil
	case world.AdminSortStatus:
		return ` ORDER BY c.status` + dir + `, c.normalized_name ASC`, nil
	case world.AdminSortPlayerUsername:
		return ` ORDER BY p.username` + dir + `, c.normalized_name ASC`, nil
	default:
		// REJECT, never default. A silently-defaulted ordering is
		// indistinguishable from an honoured one at the call site, and §11.3's
		// closed field list would then be advisory rather than enforced.
		return "", oops.Code("CHARACTER_ADMIN_SORT_FIELD_UNSUPPORTED").
			With("sort_field", string(opts.SortField)).
			Errorf("sort field %q is outside the closed admin ordering vocabulary", string(opts.SortField))
	}
}

// itoa renders a bound-parameter ordinal. It exists so the $n numbering reads
// as one token at every use site rather than as a strconv call embedded in a
// SQL concatenation.
func itoa(n int) string { return strconv.Itoa(n) }

// AdminGetCharacterRow reads ONE character through the SAME joined projection
// the two page reads use.
//
// It exists rather than reusing Get because the admin detail message embeds the
// §11.3 list projection, and players.username is one of its fields — Get does
// not join players, so a detail composed from it would carry a silently-empty
// username on the one read the admin edit sheet renders from.
//
// An absent row wraps world.ErrNotFound under the same CHARACTER_NOT_FOUND code
// Get uses, so callers already matching on either keep working.
func (r *CharacterRepository) AdminGetCharacterRow(
	ctx context.Context,
	id ulid.ULID,
) (world.AdminCharacterRow, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT`+adminCharacterProjection+adminCharacterFrom+` WHERE c.id = $1`, id.String())

	var char world.Character
	var f characterScanFields
	var username string
	err := row.Scan(
		&f.idStr, &f.playerIDStr, &char.Name, &char.Description,
		&f.locationIDStr, &f.createdAt, &char.Version,
		&f.statusStr, &char.LastActiveAt, &username,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return world.AdminCharacterRow{},
			oops.Code("CHARACTER_NOT_FOUND").With("id", id.String()).Wrap(world.ErrNotFound)
	}
	if err != nil {
		return world.AdminCharacterRow{},
			oops.Code("CHARACTER_ADMIN_GET_FAILED").With("id", id.String()).Wrap(err)
	}
	if err := parseCharacterFromFields(&f, &char); err != nil {
		return world.AdminCharacterRow{}, err
	}
	return world.AdminCharacterRow{Character: &char, PlayerUsername: username}, nil
}
