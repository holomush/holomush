//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname_test

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/charname/blocklist"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/settings"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

// The block list is proven at the GATE, not through CharacterService.Create.
// Plan 02-06 owns every production composition root and carries the end-to-end
// create-path criterion; splitting composition across 02-05 and 02-06 is the
// ownership defect the cross-AI review found, and this is the side of the
// split that gives it up.
var _ = Describe("Character-name block list against real Postgres", func() {
	const blockedName = "Overlord"

	var (
		ctx        context.Context
		cancel     context.CancelFunc
		pool       *pgxpool.Pool
		eventStore *store.PostgresEventStore
		host       settings.Writable
		cache      *blocklist.Cache
		gate       *charname.Gate
	)

	// backfill populates the identity columns for every pre-existing row.
	// migration 000001_baseline.sql seeds a bootstrap character with no
	// skeleton, and the D-30 fail-closed rule refuses to adjudicate against an
	// unverifiable corpus — which would mask the block-list verdicts below.
	backfill := func() {
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
			Expect(nerr).NotTo(HaveOccurred())
			_, uerr := pool.Exec(ctx, `
				UPDATE characters
				   SET normalized_name = $2, name_skeleton = $3, name_skeleton_unicode_version = $4
				 WHERE id = $1`,
				r.id, normalized.Key, charname.Skeleton(normalized.Key), charname.UnicodeVersion)
			Expect(uerr).NotTo(HaveOccurred())
		}
	}

	// setListBySupportedPath writes through settings.SetStringSlice — the
	// ordinary path, which DOES bump updated_at.
	setListBySupportedPath := func(patterns []string) {
		GinkgoHelper()
		Expect(host.SetStringSlice(ctx, blocklist.DefaultKey, patterns)).To(Succeed())
	}

	// setListByDirectSQL writes the raw value WITHOUT touching updated_at —
	// the only edit path v0.13 actually has (01-SPEC §8.12 ships no editing
	// surface, so operators reach for psql). This is the shape a bare
	// updated_at poll indicator cannot see.
	setListByDirectSQL := func(rawValue string) {
		GinkgoHelper()
		tag, err := pool.Exec(ctx,
			`UPDATE holomush_system_info SET value = $2 WHERE key = $1`,
			blocklist.DefaultKey, rawValue)
		Expect(err).NotTo(HaveOccurred())
		Expect(tag.RowsAffected()).To(Equal(int64(1)),
			"migration 000054 seeds the key, so the direct UPDATE must hit exactly one row")
	}

	updatedAtOf := func() int64 {
		GinkgoHelper()
		var ts int64
		Expect(pool.QueryRow(ctx,
			`SELECT COALESCE(updated_at, 0) FROM holomush_system_info WHERE key = $1`,
			blocklist.DefaultKey).Scan(&ts)).To(Succeed())
		return ts
	}

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)

		shared := testutil.SharedPostgres(suiteT)
		connStr := testutil.FreshDatabase(suiteT, shared)

		var err error
		pool, err = pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())

		eventStore, err = store.NewPostgresEventStore(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		host = settings.NewGameSettings(eventStore).Host()

		cache = blocklist.NewCache(
			func() blocklist.RawGetter { return eventStore }, blocklist.DefaultKey,
		)
		Expect(cache.Reload(ctx)).To(Succeed(), "the seeded `[]` value compiles to an empty list")

		gate = &charname.Gate{Skeletons: poolSkeletonLookup{pool: pool}, BlockList: cache}
		backfill()
	})

	AfterEach(func() {
		if eventStore != nil {
			eventStore.Close()
		}
		if pool != nil {
			pool.Close()
		}
		cancel()
	})

	Describe("A configured pattern refuses the name it matches", func() {
		BeforeEach(func() {
			setListBySupportedPath([]string{"^overlord$"})
			Expect(cache.Reload(ctx)).To(Succeed())
		})

		It("rejects the matching name with NAME_BLOCKED", func() {
			_, _, err := gate.Check(ctx, blockedName)

			Expect(err).To(HaveOccurred())
			Expect(codeOf(err)).To(Equal("NAME_BLOCKED"))
		})

		It("admits a non-matching name on the same fixture", func() {
			// Paired positive control (PORTAL-10 rule 2). Without it the
			// refusal above cannot distinguish "blocked by the list" from
			// "the gate rejects everything".
			normalized, _, err := gate.Check(ctx, "Brenna")

			Expect(err).NotTo(HaveOccurred())
			Expect(normalized.Display).To(Equal("Brenna"))
		})

		It("blocks a mixed-case submission from a lowercase pattern, because the KEY is what is evaluated", func() {
			// `^overlord$` is anchored and lowercase; the submission is not.
			// This passes only if the gate hands the block list the case-folded
			// key rather than the display form or the raw submission.
			_, _, err := gate.Check(ctx, "OvErLoRd")

			Expect(err).To(HaveOccurred())
			Expect(codeOf(err)).To(Equal("NAME_BLOCKED"))
		})

		It("admits the same name again once the list is emptied", func() {
			setListBySupportedPath([]string{})
			Expect(cache.Reload(ctx)).To(Succeed())

			_, _, err := gate.Check(ctx, blockedName)

			Expect(err).NotTo(HaveOccurred(), "an empty list rejects nothing")
		})
	})

	Describe("A malformed edit leaves the last valid list enforcing", func() {
		It("keeps blocking the previously blocked name and does NOT silently disable the list", func() {
			setListBySupportedPath([]string{"^overlord$"})
			Expect(cache.Reload(ctx)).To(Succeed())

			_, _, err := gate.Check(ctx, blockedName)
			Expect(err).To(HaveOccurred())
			Expect(codeOf(err)).To(Equal("NAME_BLOCKED"))

			// The fat-fingered psql edit. Under a settings.StringSliceN-based
			// loader this decodes to (nil,false), compiles to an empty list,
			// and the name below becomes ACCEPTED — the whole fail-open
			// finding, at the integration tier.
			setListByDirectSQL(`{"oops": `)

			reloadErr := cache.Reload(ctx)
			Expect(reloadErr).To(HaveOccurred())
			Expect(codeOf(reloadErr)).To(Equal("BLOCKLIST_VALUE_MALFORMED"))

			_, _, err = gate.Check(ctx, blockedName)
			Expect(err).To(HaveOccurred(), "the last valid list is still in force")
			Expect(codeOf(err)).To(Equal("NAME_BLOCKED"))

			// Paired control on the same post-failure state: a non-matching
			// name is still admitted, so "still blocked" is not "everything is
			// blocked now".
			_, _, err = gate.Check(ctx, "Brenna")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("A direct-SQL edit that never touches updated_at", func() {
		It("is observed within one poll interval by a gate constructed BEFORE the edit", func() {
			// RESEARCH § Concerns 3 and the live-Match finding, closed together
			// against real Postgres: the gate below is built once, the edit
			// moves only the value, and the SAME gate instance must start
			// refusing.
			tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{})
			poller, err := blocklist.NewPoller(blocklist.PollerConfig{
				Querier:  eventStore,
				Reloader: cache,
				Tracker:  tracker,
				Key:      blocklist.DefaultKey,
				Interval: 50 * time.Millisecond,
			})
			Expect(err).NotTo(HaveOccurred())

			pollCtx, stopPoller := context.WithCancel(ctx)
			done := make(chan struct{})
			go func() {
				defer close(done)
				poller.Run(pollCtx)
			}()
			defer func() {
				stopPoller()
				<-done
			}()

			_, _, err = gate.Check(ctx, blockedName)
			Expect(err).NotTo(HaveOccurred(), "admitted under the seeded empty list")

			before := updatedAtOf()
			setListByDirectSQL(`["^overlord$"]`)
			Expect(updatedAtOf()).To(Equal(before),
				"the fixture must reproduce the real edit path: updated_at MUST NOT move, "+
					"or this spec silently degrades into a bare-updated_at test")

			Eventually(func() string {
				_, _, checkErr := gate.Check(ctx, blockedName)
				return codeOf(checkErr)
			}, 10*time.Second, 50*time.Millisecond).Should(Equal("NAME_BLOCKED"))

			// Paired control after the poll took effect.
			_, _, err = gate.Check(ctx, "Brenna")
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
