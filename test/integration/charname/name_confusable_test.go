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

		seededName = "Cocoa"
		seededID = seedCharacter(seededName)
	})

	AfterEach(func() {
		if pool != nil {
			pool.Close()
		}
		cancel()
	})

	Describe("Migration 000055 leaves a stock database verifiable (D-30 sequencing constraint)", func() {
		It("carries no NULL skeleton on a freshly migrated database, and therefore admits an ordinary name", func() {
			// The D-30 sequencing constraint, observed against REAL data rather
			// than argued — and this spec's verdict INVERTED when migration
			// 000055 landed.
			//
			// Migration 000001_baseline.sql seeds a bootstrap character
			// ('TestChar') with no skeleton, so before 000055 a freshly migrated
			// database ALWAYS carried a NULL-skeleton row and charname.Gate
			// correctly refused to adjudicate against it (the fixture stand-in
			// backfillSkeletons existed for exactly that reason). 000055 now
			// backfills that row as part of the chain, so the corpus a stock
			// database hands the gate is whole.
			//
			// The fail-closed behaviour this spec used to demonstrate is NOT
			// lost: it moved to "Fail-closed against a newly inserted
			// unbackfilled row" below, which is the durable hazard — an
			// interrupted post-Unicode-upgrade recompute — rather than the
			// transient repo-state artifact this one pinned.
			var unbackfilled int
			Expect(pool.QueryRow(ctx,
				`SELECT count(*) FROM characters WHERE name_skeleton IS NULL`).
				Scan(&unbackfilled)).To(Succeed())
			Expect(unbackfilled).To(Equal(0),
				"migration 000055 backfills every pre-existing row, bootstrap character included")

			_, _, err := gate.Check(ctx, "Brenna")
			Expect(err).NotTo(HaveOccurred(),
				"a stock migrated corpus is verifiable, so an ordinary name is admitted with no fixture repair")

			// Paired control on the same fixture: the gate is genuinely
			// adjudicating rather than passing everything, so the admission
			// above cannot be vacuous.
			_, _, err = gate.Check(ctx, seededName)
			Expect(err).To(HaveOccurred(), "the seeded name still collides with itself")
		})
	})

	Describe("Whole-script homoglyph of an existing character name", func() {
		BeforeEach(func() { backfillSkeletons() })

		It("rejects a Cyrillic homoglyph of the seeded Latin name with NAME_CONFUSABLE", func() {
			// EVERY letter is Cyrillic — U+0421, U+043E, U+0441, U+043E, U+0430 —
			// so the name is single-script and §6.1.2 Mechanism A permits it.
			// Catching it is Mechanism B's job, which is what this spec asserts.
			// A Latin+Cyrillic splice would be refused by Mechanism A before any
			// database round trip and would prove nothing about skeletons.
			_, _, err := gate.Check(ctx, "\u0421\u043E\u0441\u043E\u0430")

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
			_, _, err := gate.Check(ctx, "\u0421\u043E\u0441\u043E\u0430")

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
			normalized, _, err := gate.Check(ctx, "COCOA", charname.ExcludingCharacter(seededID))
			Expect(err).NotTo(HaveOccurred())
			Expect(normalized.Display).To(Equal("COCOA"), "the player's own capitalization survives")
			Expect(normalized.Key).To(Equal("cocoa"))

			_, _, err = gate.Check(ctx, "COCOA")
			Expect(err).To(HaveOccurred())
			Expect(codeOf(err)).To(Equal("NAME_CONFUSABLE"))

			_, _, err = gate.Check(ctx, "COCOA", charname.ExcludingCharacter(idgen.New()))
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

			// normalized_name is supplied because migration 000056 makes it NOT
			// NULL; name_skeleton is deliberately left NULL, which is the
			// interrupted-recompute shape — a row that HAS a uniqueness key but
			// no confusable skeleton. name_skeleton stays nullable precisely so
			// a post-Unicode-upgrade recompute can be expressed.
			unbackfilledID := idgen.New()
			_, err = pool.Exec(ctx,
				`INSERT INTO characters (id, name, normalized_name) VALUES ($1, $2, $3)`,
				unbackfilledID.String(), "Cordelia", "cordelia")
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
