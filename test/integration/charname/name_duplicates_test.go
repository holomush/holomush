//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname_test

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/charname/blocklist"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// Duplicate detection and resolution (D-19, D-22, D-30 part 3).
//
// A job that has only ever run against clean data is a job nobody has watched
// work. Real data will very likely contain ZERO collisions, so these specs seed
// deliberately colliding rows — two kinds — and drive the whole loop: detect,
// halt, resolve, apply.
//
// # This spec does NOT exercise the operator CLI, and must not claim to
//
// It lives in package charname_test, which CANNOT import cmd/holomush — that is
// package main. Resolution here goes through CharacterRepository.Rename with a
// token minted from a real gate, which is the same write the CLI performs but
// not the same SURFACE. The `holomush character name set` command itself — its
// registration on the root, its flag parsing, its argument validation and its
// exit-code mapping — is exercised by
// cmd/holomush/cmd_character_name_integration_test.go, from inside package main.
// Do not add a CLI shortcut here; it would pass while the command is broken.

// duplicatesEnv is a database staged at 000055 — backfilled, not yet
// constrained — which is the only state in which a colliding pair can exist.
type duplicatesEnv struct {
	pool *pgxpool.Pool
	db   *sql.DB
	gate *charname.Gate
	repo *worldpg.CharacterRepository
}

// newDuplicatesEnv stages the chain to 000055 and wires the detection and the
// gated writer against it.
func newDuplicatesEnv(ctx context.Context) *duplicatesEnv {
	GinkgoHelper()
	connStr := stageSchemaWithoutUniqueIndex(ctx, suiteT)

	pool, err := pgxpool.New(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(pool.Close)

	// BackfillCharacterIdentity takes a database/sql executor because its
	// production caller is a goose Go migration running on a *sql.Tx.
	db, err := sql.Open("pgx", connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = db.Close() }) //nolint:errcheck // best-effort cleanup

	snapshot, err := blocklist.Compile(nil)
	Expect(err).NotTo(HaveOccurred())

	return &duplicatesEnv{
		pool: pool,
		db:   db,
		gate: &charname.Gate{Skeletons: worldpg.NewSkeletonLookup(pool), BlockList: snapshot},
		repo: worldpg.NewCharacterRepository(pool),
	}
}

// seedCollider inserts a character with the identity columns supplied
// EXPLICITLY, so a pair that could never arise through the gated create path
// can be staged.
func (e *duplicatesEnv) seedCollider(ctx context.Context, display, key, skeleton string) ulid.ULID {
	GinkgoHelper()
	playerID := ulid.Make()
	_, err := e.pool.Exec(ctx,
		`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'hash')`,
		playerID.String(), "dup_"+playerID.String())
	Expect(err).NotTo(HaveOccurred())

	id := ulid.Make()
	_, err = e.pool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, normalized_name, name_skeleton, name_skeleton_unicode_version, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)`,
		id.String(), playerID.String(), display, key, skeleton, charname.UnicodeVersion)
	Expect(err).NotTo(HaveOccurred())
	return id
}

// setOfKind returns the first reported collision set of the given kind whose
// members include every id named, or nil.
func setOfKind(
	sets []worldpg.IdentityCollisionSet, kind worldpg.IdentityCollisionKind, ids ...ulid.ULID,
) *worldpg.IdentityCollisionSet {
	for i := range sets {
		if sets[i].Kind != kind {
			continue
		}
		found := 0
		for _, want := range ids {
			for _, m := range sets[i].Members {
				if m.ID == want.String() {
					found++
					break
				}
			}
		}
		if found == len(ids) {
			return &sets[i]
		}
	}
	return nil
}

// resolveThroughTheGate renames a character to replacement, minting the token
// through a real gate — the same admission the operator CLI performs.
func (e *duplicatesEnv) resolveThroughTheGate(ctx context.Context, id ulid.ULID, replacement string) error {
	GinkgoHelper()
	admitted, err := e.gate.Admit(ctx, replacement, charname.ExcludingCharacter(id))
	if err != nil {
		return err
	}
	_, err = e.repo.Rename(ctx, id, admitted, 0, wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		GameID:        "main",
		Kind:          "character_updated",
		SchemaVersion: 1,
		Actor:         "operator",
		AggregateType: wmodel.AggregateCharacter,
		AggregateID:   id,
		Payload:       []byte(`{"renamed":true}`),
	}))
	return err
}

var _ = Describe("Pre-existing character-name collisions are detected and resolvable (D-19)", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		env    *duplicatesEnv
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
		DeferCleanup(cancel)
		env = newDuplicatesEnv(ctx)
	})

	Describe("An NFKC-only pair — one the live LOWER(name) check could never have caught", func() {
		It("is reported as a normalized-name collision, and 000056 applies once it is resolved", func() {
			// Fullwidth vs ASCII: two DIFFERENT display strings that NFKC folds
			// to the same uniqueness key. A case-folding LOWER(name) comparison
			// sees two unrelated names; §6.1.1 normalization sees one.
			const key = "brenna"
			skeleton := charname.Skeleton(key)
			ascii := env.seedCollider(ctx, "Brenna", key, skeleton)
			fullwidth := env.seedCollider(ctx, "Ｂｒｅｎｎａ", key, skeleton)

			sets, err := worldpg.BackfillCharacterIdentity(ctx, env.db)
			Expect(err).NotTo(HaveOccurred())

			found := setOfKind(sets, worldpg.CollisionNormalizedName, ascii, fullwidth)
			Expect(found).NotTo(BeNil(),
				"a fullwidth/ASCII pair shares one normalized key and must be reported")
			Expect(found.Members).To(HaveLen(2))
			for _, m := range found.Members {
				Expect(m.Name).NotTo(BeEmpty())
				Expect(m.PlayerID).NotTo(BeEmpty())
				Expect(m.CreatedAt).NotTo(BeZero())
			}

			// The migration HALTS on this — no auto-resolution (D-17/D-22).
			notNullErr, indexErr := applyUniqueIndexMigration(ctx, env.pool)
			Expect(notNullErr).NotTo(HaveOccurred())
			Expect(indexErr).To(HaveOccurred(), "000056 cannot apply over a duplicate key")
			Expect(uniqueIndexExists(ctx, env.pool)).To(BeFalse(),
				"the schema is left at the prior version")

			// The operator supplies the replacement; it passes the full
			// pipeline before it is written.
			Expect(env.resolveThroughTheGate(ctx, fullwidth, "Cordelia")).To(Succeed())

			_, indexErr = applyUniqueIndexMigration(ctx, env.pool)
			Expect(indexErr).NotTo(HaveOccurred(), "000056 applies once the set is resolved")
			Expect(uniqueIndexExists(ctx, env.pool)).To(BeTrue())
		})
	})

	Describe("A SKELETON-only pair — the case 000056's index structurally cannot see (D-30 part 3)", func() {
		It("is reported as a skeleton collision, tagged distinctly from a normalized-name one", func() {
			// A Latin name and its whole-script Cyrillic homoglyph. NFKC
			// deliberately does NOT collapse cross-script confusables, so the
			// two normalized names DIFFER — which is precisely what makes the
			// pair confusable, and precisely why a normalized_name-only scan
			// can never find it.
			const latinKey = "cocoa"
			cyrillicKey := "сосоа" // сосоа
			Expect(cyrillicKey).NotTo(Equal(latinKey),
				"fixture precondition: the pair's normalized names must DIFFER")
			skeleton := charname.Skeleton(latinKey)
			Expect(charname.Skeleton(cyrillicKey)).To(Equal(skeleton),
				"fixture precondition: the pair's UTS #39 skeletons must be EQUAL")

			latin := env.seedCollider(ctx, "Cocoa", latinKey, skeleton)
			cyrillic := env.seedCollider(ctx, "Сосоа", cyrillicKey, skeleton)

			sets, err := worldpg.BackfillCharacterIdentity(ctx, env.db)
			Expect(err).NotTo(HaveOccurred())

			found := setOfKind(sets, worldpg.CollisionSkeleton, latin, cyrillic)
			Expect(found).NotTo(BeNil(),
				"a whole-script homoglyph pair must be reported as a SKELETON collision")
			Expect(found.Kind).To(Equal(worldpg.CollisionSkeleton))

			// The paired negative: the same rows are NOT a normalized-name
			// collision. That is the whole reason the second scan exists.
			Expect(setOfKind(sets, worldpg.CollisionNormalizedName, latin, cyrillic)).To(BeNil(),
				"the pair's normalized names differ, so a normalized_name-only scan cannot see it")
		})

		It("would be ACCEPTED by 000056 alone — the negative control proving the scan is load-bearing", func() {
			// This is the criterion's point. With the skeleton pair present and
			// UNRESOLVED, 000056 applies successfully: its unique index is over
			// normalized_name and the two rows differ there. The index is not a
			// backstop for confusables and never could be — differing normalized
			// names is exactly what makes a pair confusable — so 000055's
			// skeleton scan is the only mechanism in the phase that reaches
			// pre-existing confusable pairs.
			const latinKey = "cocoa"
			cyrillicKey := "сосоа"
			skeleton := charname.Skeleton(latinKey)
			env.seedCollider(ctx, "Cocoa", latinKey, skeleton)
			env.seedCollider(ctx, "Сосоа", cyrillicKey, skeleton)

			notNullErr, indexErr := applyUniqueIndexMigration(ctx, env.pool)
			Expect(notNullErr).NotTo(HaveOccurred())
			Expect(indexErr).NotTo(HaveOccurred(),
				"000056 accepts a confusable pair: the index sees only normalized_name")
			Expect(uniqueIndexExists(ctx, env.pool)).To(BeTrue())

			// Both impersonating rows are still there, coexisting under the
			// constraint that supposedly protects the namespace.
			var n int
			Expect(env.pool.QueryRow(ctx,
				`SELECT count(*) FROM characters WHERE name_skeleton = $1`, skeleton).Scan(&n)).To(Succeed())
			Expect(n).To(Equal(2))
		})
	})

	Describe("A clean corpus", func() {
		It("reports nothing and lets 000056 apply — the paired control for both cases above", func() {
			env.seedCollider(ctx, "Brenna", "brenna", charname.Skeleton("brenna"))
			env.seedCollider(ctx, "Cordelia", "cordelia", charname.Skeleton("cordelia"))

			sets, err := worldpg.BackfillCharacterIdentity(ctx, env.db)
			Expect(err).NotTo(HaveOccurred())
			Expect(sets).To(BeEmpty())

			notNullErr, indexErr := applyUniqueIndexMigration(ctx, env.pool)
			Expect(notNullErr).NotTo(HaveOccurred())
			Expect(indexErr).NotTo(HaveOccurred())
		})
	})
})
