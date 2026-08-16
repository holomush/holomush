// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/pkg/errutil"
)

// The whole package shares ONE database, so every assertion here scopes itself
// to a freshly-created player through AdminCharacterListOptions.PlayerID —
// §11.3's Filter=Yes half of `player_id`. That is what makes a TotalCount
// assertion meaningful at all: "the unfiltered first page" means "unfiltered by
// the SEARCH TERM", and the player scope is what turns the seeded row count
// into a number a test may assert.

// adminSeed creates one character with an explicit last_active_at, under the
// given player, and returns it.
func adminSeed(ctx context.Context, t *testing.T, playerID ulid.ULID, name string, lastActiveAt int64) *world.Character {
	t.Helper()
	repo := postgres.NewCharacterRepository(testPool)
	char := &world.Character{
		ID:          ulid.Make(),
		PlayerID:    playerID,
		Name:        name,
		Description: "seeded for the admin read",
		CreatedAt:   time.Now().UTC(),
	}
	require.NoError(t, delErr(repo.Create(ctx, char, admit(ctx, t, char.Name))))
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, char.ID.String()) })

	if lastActiveAt != 0 {
		_, err := testPool.Exec(ctx,
			`UPDATE characters SET last_active_at = $2 WHERE id = $1`, char.ID.String(), lastActiveAt)
		require.NoError(t, err)
		char.LastActiveAt = lastActiveAt
	}
	return char
}

// adminPlayerWithUsername creates a player carrying an exact username, so a
// username-arm search has something deterministic to match.
func adminPlayerWithUsername(ctx context.Context, t *testing.T, username string) ulid.ULID {
	t.Helper()
	playerID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at, updated_at)
		VALUES ($1, $2, 'testhash', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
	`, playerID.String(), username)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String()) })
	return playerID
}

// normalizedNameOf reads characters.normalized_name verbatim — the column the
// ordering and the search predicate are written against, and which
// world.Character deliberately does not carry.
func normalizedNameOf(ctx context.Context, t *testing.T, id ulid.ULID) string {
	t.Helper()
	var got string
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT normalized_name FROM characters WHERE id = $1`, id.String()).Scan(&got))
	return got
}

func adminRowIDs(page world.AdminCharacterPage) []string {
	out := make([]string, 0, len(page.Rows))
	for _, r := range page.Rows {
		out = append(out, r.ID.String())
	}
	return out
}

// TestAdminListCharactersSortsNeverActiveLastInBothDirections is ONE
// table-driven function whose case table carries BOTH directions.
//
// A test exercising only DESC passes under the bug: descending gets the 0
// sentinel last for free, as the column minimum. Only the ASC case can observe
// the leading `(c.last_active_at = 0)` clause, and only a tiebreak assertion
// among the two sentinel rows can observe the trailing `c.normalized_name ASC`.
func TestAdminListCharactersSortsNeverActiveLastInBothDirections(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)
	playerID := createTestPlayer(ctx, t)

	// Two never-active rows whose normalized names order deterministically, plus
	// two active rows. The seeded names are randomized for corpus uniqueness, so
	// the tiebreak expectation is derived from the STORED normal form rather
	// than assumed from the literal.
	// INSERTION ORDER IS THE REVERSE OF NAME ORDER, DELIBERATELY. Without the
	// trailing `c.normalized_name ASC` tiebreak the order among rows tying on
	// the primary key is UNSPECIFIED — which in practice means physical/heap
	// order, i.e. insertion order. Seeding "zzz" first and "aaa" second makes
	// those two orders DISAGREE, so deleting the tiebreak turns this test red
	// instead of leaving it green by luck. Seeded the other way round the two
	// orders coincide and the clause is unobservable, which is coverage in
	// appearance only.
	neverB := adminSeed(ctx, t, playerID, charFixtureName("zzz never"), 0)
	neverA := adminSeed(ctx, t, playerID, charFixtureName("aaa never"), 0)
	low := adminSeed(ctx, t, playerID, charFixtureName("low active"), 100)
	high := adminSeed(ctx, t, playerID, charFixtureName("high active"), 5000)

	first, second := neverA, neverB
	if normalizedNameOf(ctx, t, neverB.ID) < normalizedNameOf(ctx, t, neverA.ID) {
		first, second = neverB, neverA
	}

	// ONE case table carrying BOTH directions. `direction` is the SQL keyword the
	// case exercises; it is carried on the case rather than derived at the
	// assertion so the table itself states which orderings this one run covers.
	tests := []struct {
		name       string
		direction  string
		descending bool
		wantOrder  []string
	}{
		{
			name:       "ascending puts the never-active sentinel last, after the largest real value",
			direction:  "ASC",
			descending: false,
			wantOrder:  []string{low.ID.String(), high.ID.String(), first.ID.String(), second.ID.String()},
		},
		{
			name:       "descending puts the never-active sentinel last, after the smallest real value",
			direction:  "DESC",
			descending: true,
			wantOrder:  []string{high.ID.String(), low.ID.String(), first.ID.String(), second.ID.String()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := repo.AdminListCharacters(ctx, world.AdminCharacterListOptions{
				SortField:  world.AdminSortLastActiveAt,
				Descending: tt.descending,
				PlayerID:   &playerID,
				Limit:      50,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantOrder, adminRowIDs(page),
				"the ordering is (last_active_at = 0), last_active_at %s, normalized_name ASC", tt.direction)
		})
	}
}

// TestAdminSearchCharactersMatchesEitherPredicateArmIndependently asserts each
// arm CONTRIBUTES. A bare "one row came back" assertion proves nothing: an OR
// inside a WHERE cannot duplicate a row and the join is many-to-one onto
// players.id, a TEXT PRIMARY KEY, so such an assertion passes identically with
// and without any conceivable bug in the predicate.
func TestAdminSearchCharactersMatchesEitherPredicateArmIndependently(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	marker := randomFixtureLetters(10)
	// The player's username carries the marker; the character's name does not.
	usernameOnlyPlayer := adminPlayerWithUsername(ctx, t, "u"+marker+"name")
	usernameOnly := adminSeed(ctx, t, usernameOnlyPlayer, charFixtureName("plainname"), 0)

	// The character's name carries the marker; its player's username does not.
	nameOnlyPlayer := adminPlayerWithUsername(ctx, t, "plain"+randomFixtureLetters(8))
	nameOnly := adminSeed(ctx, t, nameOnlyPlayer, "n"+marker+" one", 0)

	// Both carry it.
	bothPlayer := adminPlayerWithUsername(ctx, t, "b"+marker+"name")
	both := adminSeed(ctx, t, bothPlayer, "b"+marker+" two", 0)

	search := func(t *testing.T, term string, playerID ulid.ULID) world.AdminCharacterPage {
		t.Helper()
		normalized, err := charname.Normalize(term)
		require.NoError(t, err)
		page, err := repo.AdminSearchCharacters(ctx, normalized.Key, world.AdminCharacterListOptions{
			SortField: world.AdminSortName,
			PlayerID:  &playerID,
			Limit:     50,
		})
		require.NoError(t, err)
		return page
	}

	t.Run("a term matching ONLY the stored normalized name returns that row", func(t *testing.T) {
		page := search(t, marker, nameOnlyPlayer)
		assert.Equal(t, []string{nameOnly.ID.String()}, adminRowIDs(page))
	})

	t.Run("a term matching ONLY the joined player username returns that character", func(t *testing.T) {
		page := search(t, marker, usernameOnlyPlayer)
		assert.Equal(t, []string{usernameOnly.ID.String()}, adminRowIDs(page))
	})

	t.Run("a term matching BOTH returns the row exactly once", func(t *testing.T) {
		page := search(t, marker, bothPlayer)
		assert.Equal(t, []string{both.ID.String()}, adminRowIDs(page))
		assert.Equal(t, int64(1), page.TotalCount)
	})
}

// TestAdminSearchCharactersWithAnEmptyTermReturnsTheUnfilteredPage pins the
// bypass at the repository. The handler's own blank-term branch is asserted at
// the wire, where the bug actually lives.
func TestAdminSearchCharactersWithAnEmptyTermReturnsTheUnfilteredPage(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)
	playerID := createTestPlayer(ctx, t)
	adminSeed(ctx, t, playerID, charFixtureName("empty one"), 0)
	adminSeed(ctx, t, playerID, charFixtureName("empty two"), 0)
	adminSeed(ctx, t, playerID, charFixtureName("empty three"), 0)

	page, err := repo.AdminSearchCharacters(ctx, "", world.AdminCharacterListOptions{
		SortField: world.AdminSortName,
		PlayerID:  &playerID,
		Limit:     50,
	})
	require.NoError(t, err)
	assert.Len(t, page.Rows, 3)
	assert.Equal(t, int64(3), page.TotalCount)
}

func TestAdminSearchCharactersWithNoMatchReturnsAnEmptyPageAndNoError(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)
	playerID := createTestPlayer(ctx, t)
	adminSeed(ctx, t, playerID, charFixtureName("nomatch"), 0)

	page, err := repo.AdminSearchCharacters(ctx, "zzz"+randomFixtureLetters(12), world.AdminCharacterListOptions{
		SortField: world.AdminSortName,
		PlayerID:  &playerID,
		Limit:     50,
	})
	require.NoError(t, err)
	assert.Empty(t, page.Rows)
	assert.Equal(t, int64(0), page.TotalCount)
}

// TestAdminListCharactersTotalsAPageBeyondTheEnd is the criterion a
// COUNT(*) OVER () implementation cannot satisfy: OFFSET removes every row, so
// there is no row left to carry the window value and the total reports 0.
func TestAdminListCharactersTotalsAPageBeyondTheEnd(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)
	playerID := createTestPlayer(ctx, t)
	adminSeed(ctx, t, playerID, charFixtureName("beyond one"), 0)
	adminSeed(ctx, t, playerID, charFixtureName("beyond two"), 0)
	adminSeed(ctx, t, playerID, charFixtureName("beyond three"), 0)

	page, err := repo.AdminListCharacters(ctx, world.AdminCharacterListOptions{
		SortField: world.AdminSortName,
		PlayerID:  &playerID,
		Limit:     2,
		Offset:    10,
	})
	require.NoError(t, err)
	assert.Empty(t, page.Rows, "a page beyond the end carries no rows")
	assert.Equal(t, int64(3), page.TotalCount, "the total is over the filtered SET, not over the returned page")
}

// TestAdminSearchCharactersMatchesTheStoredNormalFormNotTheDisplayName
// normalizes the term through charname.Normalize BEFORE the repository call.
// The repository does NOT normalize; stating where normalization happens is the
// point of this test, because the whole design rests on there being exactly one
// normalizer, service-side.
func TestAdminSearchCharactersMatchesTheStoredNormalFormNotTheDisplayName(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)
	playerID := createTestPlayer(ctx, t)
	// The fixture name is lowercase ASCII; the searched term differs from it by
	// case and by an NFKC-foldable fullwidth codepoint.
	char := adminSeed(ctx, t, playerID, "kafka "+randomFixtureLetters(8), 0)

	normalized, err := charname.Normalize("ＫＡＦＫＡ")
	require.NoError(t, err)
	require.Equal(t, "kafka", normalized.Key, "NFKC folding plus case folding is charname's job, not the repository's")

	page, err := repo.AdminSearchCharacters(ctx, normalized.Key, world.AdminCharacterListOptions{
		SortField: world.AdminSortName,
		PlayerID:  &playerID,
		Limit:     50,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{char.ID.String()}, adminRowIDs(page))
}

// TestAdminSearchCharactersTreatsLikeMetacharactersLiterally is the correctness
// half of T-06-21. Parameter binding already closes injection; charname.Normalize
// folds case and strips format runes but passes %, _ and \ through untouched —
// none of them is in category Cf — so without an escape step a typed `a_b`
// silently matches `axb`.
//
// # The fixtures are USERNAMES, not character names, and that is forced
//
// charname's admission gate refuses a character name containing anything but
// letters and spaces (NAME_INVALID_SYNTAX, gate.go:155), so no metacharacter can
// ever reach characters.normalized_name. players.username is under no such gate:
// it is inserted verbatim, so it is the arm where a stored metacharacter is
// reachable — and the TERM can carry one on either arm regardless, because the
// operator types it and Normalize passes it through. Each case therefore seeds a
// matching username and a DECOY username that only an unescaped pattern would
// also match.
//
// Scoping is by the random suffix rather than by PlayerID, because each fixture
// needs its own player to carry its own username, and the decoy must be able to
// appear in the result set for the assertion to discriminate.
func TestAdminSearchCharactersTreatsLikeMetacharactersLiterally(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	suffix := randomFixtureLetters(10)

	seedUnder := func(t *testing.T, username string) *world.Character {
		t.Helper()
		p := adminPlayerWithUsername(ctx, t, username)
		return adminSeed(ctx, t, p, charFixtureName("meta"), 0)
	}

	underscore := seedUnder(t, "a_b"+suffix)
	underscoreDecoy := seedUnder(t, "axb"+suffix)
	percent := seedUnder(t, "100%"+suffix)
	percentDecoy := seedUnder(t, "1000"+suffix)
	backslash := seedUnder(t, `c\d`+suffix)
	backslashDecoy := seedUnder(t, "cd"+suffix)

	search := func(t *testing.T, term string) []string {
		t.Helper()
		normalized, err := charname.Normalize(term)
		require.NoError(t, err, "Normalize does not reject a metacharacter; only the admission gate does")
		page, err := repo.AdminSearchCharacters(ctx, normalized.Key, world.AdminCharacterListOptions{
			SortField: world.AdminSortPlayerUsername,
			Limit:     50,
		})
		require.NoError(t, err)
		return adminRowIDs(page)
	}

	// Each decoy is asserted reachable first: a decoy the predicate could never
	// return under ANY escaping would make its case pass vacuously.
	t.Run("every decoy is reachable by a plain substring of the shared suffix", func(t *testing.T) {
		got := search(t, suffix)
		assert.ElementsMatch(t, []string{
			underscore.ID.String(), underscoreDecoy.ID.String(),
			percent.ID.String(), percentDecoy.ID.String(),
			backslash.ID.String(), backslashDecoy.ID.String(),
		}, got)
	})

	t.Run("an underscore matches literally and does not match any single character", func(t *testing.T) {
		assert.Equal(t, []string{underscore.ID.String()}, search(t, "a_b"+suffix))
	})

	t.Run("a percent matches literally and does not match every row", func(t *testing.T) {
		assert.Equal(t, []string{percent.ID.String()}, search(t, "100%"+suffix))
	})

	t.Run("a backslash matches literally", func(t *testing.T) {
		assert.Equal(t, []string{backslash.ID.String()}, search(t, `c\d`+suffix))
	})
}

// TestAdminListCharactersReadsTheFullProjectionNotListAlls proves the row
// carries the lifecycle columns. ListAll's id+name-only projection zero-values
// Status and LastActiveAt BY OMISSION, and a lifecycle decision from such a
// result violates INV-WORLD-5.
func TestAdminListCharactersReadsTheFullProjectionNotListAlls(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)
	username := "full" + randomFixtureLetters(10)
	playerID := adminPlayerWithUsername(ctx, t, username)
	char := adminSeed(ctx, t, playerID, charFixtureName("full projection"), 4242)

	page, err := repo.AdminListCharacters(ctx, world.AdminCharacterListOptions{
		SortField: world.AdminSortName,
		PlayerID:  &playerID,
		Limit:     50,
	})
	require.NoError(t, err)
	require.Len(t, page.Rows, 1)

	row := page.Rows[0]
	assert.Equal(t, char.ID, row.ID)
	assert.Equal(t, world.StatusActive, row.Status, "Status is READ, not left zero by omission")
	assert.Equal(t, int64(4242), row.LastActiveAt)
	assert.Equal(t, playerID, row.PlayerID)
	assert.Positive(t, row.Version)
	assert.False(t, row.CreatedAt.IsZero())
	assert.Equal(t, username, row.PlayerUsername, "the joined players.username rides on the row")
}

// TestAdminListCharactersRejectsASortFieldOutsideTheClosedSet — reject, never
// default. A silently-defaulted ordering is indistinguishable from an honoured
// one at the call site.
func TestAdminListCharactersRejectsASortFieldOutsideTheClosedSet(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)
	playerID := createTestPlayer(ctx, t)

	t.Run("an unrecognised key is refused with the typed code", func(t *testing.T) {
		_, err := repo.AdminListCharacters(ctx, world.AdminCharacterListOptions{
			SortField: world.AdminCharacterSortField("player_id"),
			PlayerID:  &playerID,
			Limit:     50,
		})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_ADMIN_SORT_FIELD_UNSUPPORTED")
	})

	t.Run("the zero value is refused too, rather than defaulting", func(t *testing.T) {
		_, err := repo.AdminListCharacters(ctx, world.AdminCharacterListOptions{
			PlayerID: &playerID,
			Limit:    50,
		})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_ADMIN_SORT_FIELD_UNSUPPORTED")
	})
}

// TestAdminListCharactersCountAndPageShareOneSnapshot mutates from a SECOND
// connection between the two statements.
//
// pgx.TxOptions{AccessMode: pgx.ReadOnly} alone does NOT deliver this: under
// PostgreSQL's default read-committed isolation every statement takes its own
// snapshot even inside one transaction, so the insert would land between the
// count and the page and the two would disagree. Only IsoLevel:
// pgx.RepeatableRead makes the claim true.
//
// The interleaving is driven by re-running the repository's own two statements
// against an explicitly-begun transaction: the assertion is over the ISOLATION
// LEVEL the repository declares, verified by reproducing the exact transaction
// shape it opens.
func TestAdminListCharactersCountAndPageShareOneSnapshot(t *testing.T) {
	ctx := context.Background()
	playerID := createTestPlayer(ctx, t)
	adminSeed(ctx, t, playerID, charFixtureName("snapshot one"), 0)
	adminSeed(ctx, t, playerID, charFixtureName("snapshot two"), 0)

	tx, err := testPool.BeginTx(ctx, postgres.AdminReadTxOptions())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	countOf := func(tx pgx.Tx) int64 {
		var n int64
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT count(*) FROM characters c JOIN players p ON p.id = c.player_id WHERE c.player_id = $1`,
			playerID.String()).Scan(&n))
		return n
	}

	before := countOf(tx)
	require.Equal(t, int64(2), before)

	// A SECOND connection inserts a matching row and commits.
	adminSeed(ctx, t, playerID, charFixtureName("snapshot three"), 0)

	after := countOf(tx)
	assert.Equal(t, before, after,
		"the count and the page share ONE snapshot; a concurrent insert must not be visible mid-transaction")
}
