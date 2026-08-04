//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname_test

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/test/testutil"
)

// poolSkeletonLookup is the far end of the tracer: a charname.SkeletonLookup
// reading characters.name_skeleton out of a live Postgres pool.
//
// One query answers both halves the interface returns. The first EXISTS is the
// collision check with the self-exclusion applied; the second is the D-30
// unverifiable flag, true while ANY characters row still has a NULL skeleton,
// because such a corpus cannot answer "is this name confusable?" at all.
type poolSkeletonLookup struct{ pool *pgxpool.Pool }

const skeletonLookupSQL = `
SELECT
  EXISTS(SELECT 1 FROM characters WHERE name_skeleton = $1 AND ($2::text IS NULL OR id::text <> $2)),
  EXISTS(SELECT 1 FROM characters WHERE name_skeleton IS NULL)`

func (l poolSkeletonLookup) SkeletonExists(ctx context.Context, skeleton string, excluding *ulid.ULID) (bool, bool, error) {
	var excludedID *string
	if excluding != nil {
		s := excluding.String()
		excludedID = &s
	}

	var exists, unverifiable bool
	if err := l.pool.QueryRow(ctx, skeletonLookupSQL, skeleton, excludedID).Scan(&exists, &unverifiable); err != nil {
		return false, false, oops.Code("SKELETON_LOOKUP_QUERY_FAILED").Wrap(err)
	}
	return exists, unverifiable, nil
}

var _ = Describe("Character-name confusable rejection against real Postgres", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		pool       *pgxpool.Pool
		gate       *charname.Gate
		seededID   ulid.ULID
		seededName string
	)

	// seedCharacter writes a characters row carrying all four identity columns,
	// computed with the same charname functions the gate uses. Test files sit
	// outside the world-SQL fence's Go scan, so a raw fixture INSERT is
	// legitimate here; the PRODUCTION write path is a later plan's.
	seedCharacter := func(name string) ulid.ULID {
		GinkgoHelper()
		normalized, err := charname.Normalize(name)
		Expect(err).NotTo(HaveOccurred())

		id := idgen.New()
		_, err = pool.Exec(ctx, `
			INSERT INTO characters (id, name, normalized_name, name_skeleton, name_skeleton_unicode_version)
			VALUES ($1, $2, $3, $4, $5)`,
			id.String(), normalized.Display, normalized.Key,
			charname.Skeleton(normalized.Key), charname.UnicodeVersion)
		Expect(err).NotTo(HaveOccurred())
		return id
	}

	// backfillSkeletons populates normalized_name / name_skeleton /
	// name_skeleton_unicode_version for every row still missing them.
	//
	// This is a FIXTURE stand-in for the Go backfill migration (D-21 step B),
	// which is a later plan's. It is required here because a stock database is
	// NOT verifiable: migration 000001_baseline.sql seeds a bootstrap character
	// row ('TestChar'), and that row has no skeleton until the backfill runs.
	// The spec below proves exactly that before calling this.
	backfillSkeletons := func() int {
		GinkgoHelper()
		rows, err := pool.Query(ctx, `SELECT id, name FROM characters WHERE name_skeleton IS NULL`)
		Expect(err).NotTo(HaveOccurred())

		type row struct{ id, name string }
		var pending []row
		for rows.Next() {
			var r row
			Expect(rows.Scan(&r.id, &r.name)).To(Succeed())
			pending = append(pending, r)
		}
		rows.Close()
		Expect(rows.Err()).NotTo(HaveOccurred())

		for _, r := range pending {
			normalized, nerr := charname.Normalize(r.name)
			Expect(nerr).NotTo(HaveOccurred(), "bootstrap row %q must normalize", r.name)
			_, uerr := pool.Exec(ctx, `
				UPDATE characters
				   SET normalized_name = $2, name_skeleton = $3, name_skeleton_unicode_version = $4
				 WHERE id = $1`,
				r.id, normalized.Key, charname.Skeleton(normalized.Key), charname.UnicodeVersion)
			Expect(uerr).NotTo(HaveOccurred())
		}
		return len(pending)
	}

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)

		shared := testutil.SharedPostgres(suiteT)
		connStr := testutil.FreshDatabase(suiteT, shared)

		var err error
		pool, err = pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())

		gate = &charname.Gate{Skeletons: poolSkeletonLookup{pool: pool}}

		seededName = "Alaric"
		seededID = seedCharacter(seededName)
	})

	AfterEach(func() {
		if pool != nil {
			pool.Close()
		}
		cancel()
	})

	Describe("A stock database is not a verifiable corpus (D-30 sequencing constraint)", func() {
		It("refuses every name until the pre-existing rows carry skeletons, then admits them", func() {
			// This is the D-30 sequencing constraint observed against REAL data
			// rather than argued. Migration 000001_baseline.sql seeds a
			// bootstrap character, so a freshly migrated database ALWAYS has a
			// row with a NULL skeleton — the gate cannot adjudicate against it
			// and must not report "no collision".
			var unbackfilled int
			Expect(pool.QueryRow(ctx,
				`SELECT count(*) FROM characters WHERE name_skeleton IS NULL`).
				Scan(&unbackfilled)).To(Succeed())
			Expect(unbackfilled).To(BeNumerically(">", 0),
				"a stock migrated database carries at least the bootstrap character row")

			_, _, err := gate.Check(ctx, "Brenna")
			Expect(err).To(HaveOccurred())
			Expect(codeOf(err)).To(Equal("NAME_SKELETON_UNVERIFIABLE"))

			Expect(backfillSkeletons()).To(Equal(unbackfilled))

			_, _, err = gate.Check(ctx, "Brenna")
			Expect(err).NotTo(HaveOccurred(), "the same name is admitted once the corpus is verifiable")
		})
	})

	Describe("Whole-script homoglyph of an existing character name", func() {
		BeforeEach(func() { backfillSkeletons() })

		It("rejects a Cyrillic homoglyph of the seeded Latin name with NAME_CONFUSABLE", func() {
			// U+0410 CYRILLIC CAPITAL LETTER A in place of the Latin A.
			_, _, err := gate.Check(ctx, "\u0410laric")

			Expect(err).To(HaveOccurred())
			Expect(codeOf(err)).To(Equal("NAME_CONFUSABLE"))
		})

		It("admits a non-confusable name on the same fixture", func() {
			// Paired positive control (PORTAL-10 rule 2). Without it, err != nil
			// above cannot distinguish "rejected as confusable" from "rejected
			// because the seeded row was never written".
			normalized, skeleton, err := gate.Check(ctx, "Brenna")

			Expect(err).NotTo(HaveOccurred())
			Expect(normalized.Display).To(Equal("Brenna"))
			Expect(normalized.Key).To(Equal("brenna"))
			Expect(skeleton).NotTo(BeEmpty())
		})

		It("names neither the colliding character nor its id in the message the caller receives", func() {
			// Wire-level opacity assertion (PORTAL-10 rule 5): asserted over the
			// MESSAGE, not over an inner oops code. Naming the collider would
			// turn this gate into a name-enumeration oracle.
			_, _, err := gate.Check(ctx, "\u0410laric")

			Expect(err).To(HaveOccurred())
			msg := err.Error()
			Expect(msg).NotTo(ContainSubstring(seededName))
			Expect(msg).NotTo(ContainSubstring(strings.ToLower(seededName)))
			Expect(msg).NotTo(ContainSubstring(seededID.String()))
		})
	})

	Describe("Self-exclusion on a case-variant rename (B-18)", func() {
		BeforeEach(func() { backfillSkeletons() })

		It("admits the seeded character's own case variant when its id is excluded and refuses it otherwise", func() {
			// Both directions on ONE fixture, so the success cannot pass because
			// the seeded row was never written.
			normalized, _, err := gate.Check(ctx, "ALARIC", charname.ExcludingCharacter(seededID))
			Expect(err).NotTo(HaveOccurred())
			Expect(normalized.Display).To(Equal("ALARIC"), "the player's own capitalization survives")
			Expect(normalized.Key).To(Equal("alaric"))

			_, _, err = gate.Check(ctx, "ALARIC")
			Expect(err).To(HaveOccurred())
			Expect(codeOf(err)).To(Equal("NAME_CONFUSABLE"))

			_, _, err = gate.Check(ctx, "ALARIC", charname.ExcludingCharacter(idgen.New()))
			Expect(err).To(HaveOccurred(), "excluding a different character does not excuse the collision")
			Expect(codeOf(err)).To(Equal("NAME_CONFUSABLE"))
		})
	})

	Describe("Fail-closed against a newly inserted unbackfilled row (D-30)", func() {
		BeforeEach(func() { backfillSkeletons() })

		It("refuses an otherwise-admissible name while any row has a NULL skeleton, and admits it once backfilled", func() {
			// Both directions on ONE fixture. This is the interrupted-recompute
			// case: a row loses its skeleton after the corpus was once whole.
			_, _, err := gate.Check(ctx, "Brenna")
			Expect(err).NotTo(HaveOccurred(), "a fully populated corpus admits the name")

			unbackfilledID := idgen.New()
			_, err = pool.Exec(ctx, `INSERT INTO characters (id, name) VALUES ($1, $2)`,
				unbackfilledID.String(), "Cordelia")
			Expect(err).NotTo(HaveOccurred())

			_, _, err = gate.Check(ctx, "Brenna")
			Expect(err).To(HaveOccurred(), "the same name is refused once the corpus is unverifiable")
			Expect(codeOf(err)).To(Equal("NAME_SKELETON_UNVERIFIABLE"))

			Expect(backfillSkeletons()).To(Equal(1))

			_, _, err = gate.Check(ctx, "Brenna")
			Expect(err).NotTo(HaveOccurred(), "the backfilled corpus admits the name again")
		})
	})

	Describe("Rows written by this plan's fixture carry the pinned Unicode version", func() {
		It("stores the generated table's UnicodeVersion alongside every skeleton", func() {
			var version string
			err := pool.QueryRow(ctx,
				`SELECT name_skeleton_unicode_version FROM characters WHERE id = $1`,
				seededID.String()).Scan(&version)

			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(charname.UnicodeVersion))
			Expect(version).NotTo(BeEmpty())
		})
	})
})

// codeOf returns the oops code carried by err, or "" when it carries none.
func codeOf(err error) string {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return ""
	}
	code, ok := oopsErr.Code().(string)
	if !ok {
		return ""
	}
	return code
}
