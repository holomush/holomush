// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package session_test

import (
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/idgen"
)

// Verifies: INV-PRESENCE-1
// Postgres S-1/S-2/S-3: PostgresSessionStore.ListActiveByLocation enforces
// the same filtering semantics as the original session store — Active-only, LocationID-strict,
// empty result for empty location. This locks the SQL predicate at
// internal/store/session_store.go (WHERE location_id = $1 AND status = 'active').
//
// Sessions are seeded via raw SQL to bypass the full FK chain (players,
// characters, player_sessions). The sessions table has no FK on character_id,
// so direct INSERT is valid for store-layer isolation tests.
var _ = Describe("PostgresSessionStore.ListActiveByLocation (INV-PRESENCE-1)", func() {
	BeforeEach(func() {
		cleanupTestData(env.ctx, env.pool)
	})

	// seedSession inserts a minimal session row directly into the sessions
	// table. Only the columns that have no DEFAULT or are required for the
	// test assertions are supplied; the rest rely on column DEFAULTs.
	// grid_present is derived in production from a live connection lease
	// (recomputeSessionLiveness, holomush-rsoe6.4). ListActiveByLocation
	// filters on it (status='active' AND grid_present=true — the presence
	// roster, holomush-rsoe6.12). Raw-seeded rows bypass the lease path, so
	// the seed sets grid_present explicitly: true for active sessions
	// (simulating a live connection), false otherwise.
	seedSession := func(id string, charID ulid.ULID, locID ulid.ULID, status string) {
		_, err := env.pool.Exec(
			env.ctx,
			`INSERT INTO sessions (id, character_id, character_name, location_id, status, grid_present)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			id, charID.String(), "TestChar-"+id, locID.String(), status, status == "active",
		)
		Expect(err).NotTo(HaveOccurred(), "seed session %q", id)
	}

	// Verifies: INV-PRESENCE-1
	// S-1: Only status='active' sessions are returned; detached and expired
	// rows at the same location are excluded.
	It("returns only active sessions — detached and expired are excluded", func() {
		loc := idgen.New()

		seedSession("s-active-1", idgen.New(), loc, "active")
		seedSession("s-active-2", idgen.New(), loc, "active")
		seedSession("s-detached", idgen.New(), loc, "detached")
		seedSession("s-expired", idgen.New(), loc, "expired")

		got, err := env.sessionStore.ListActiveByLocation(env.ctx, loc)
		Expect(err).NotTo(HaveOccurred())

		ids := make([]string, 0, len(got))
		for _, s := range got {
			ids = append(ids, s.ID)
		}
		Expect(ids).To(ConsistOf("s-active-1", "s-active-2"),
			"detached and expired sessions must not appear in ListActiveByLocation")
	})

	// Verifies: INV-PRESENCE-1
	// S-2: Query against a location with no sessions returns an empty slice
	// (not an error).
	It("returns empty slice for a location with no active sessions", func() {
		emptyLoc := idgen.New()

		got, err := env.sessionStore.ListActiveByLocation(env.ctx, emptyLoc)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty(),
			"empty location must return empty slice, not an error")
	})

	// Verifies: INV-PRESENCE-1
	// S-3: Sessions at other locations are excluded — LocationID filter is strict.
	It("excludes sessions at other locations", func() {
		loc1 := idgen.New()
		loc2 := idgen.New()

		seedSession("s-loc1", idgen.New(), loc1, "active")
		seedSession("s-loc2", idgen.New(), loc2, "active")

		got, err := env.sessionStore.ListActiveByLocation(env.ctx, loc1)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveLen(1))
		Expect(got[0].ID).To(Equal("s-loc1"),
			"only sessions at the queried location must be returned")
	})

	// Verifies: INV-PRESENCE-1
	// Schema-level dedup guard: the unique partial index
	//   idx_sessions_active_character ON sessions(character_id)
	//     WHERE status IN ('active','detached')
	// prevents two active sessions for the same character from existing.
	// This test verifies that inserting a duplicate character_id+active row
	// is rejected at the database level — the index that enforces the
	// at-most-one-active-session invariant is live.
	It("rejects a second active session for the same character (unique partial index)", func() {
		loc := idgen.New()
		charID := idgen.New()
		futureExpiry := time.Now().Add(time.Hour).UnixNano()

		_, err := env.pool.Exec(
			env.ctx,
			`INSERT INTO sessions (id, character_id, character_name, location_id, status, expires_at)
			 VALUES ($1, $2, $3, $4, 'active', $5)`,
			"dup-1", charID.String(), "DupChar", loc.String(), futureExpiry,
		)
		Expect(err).NotTo(HaveOccurred(), "first insert must succeed")

		_, err = env.pool.Exec(
			env.ctx,
			`INSERT INTO sessions (id, character_id, character_name, location_id, status, expires_at)
			 VALUES ($1, $2, $3, $4, 'active', $5)`,
			"dup-2", charID.String(), "DupChar", loc.String(), futureExpiry,
		)
		Expect(err).To(HaveOccurred(),
			"duplicate active session for the same character must be rejected by the unique partial index")
	})
})
