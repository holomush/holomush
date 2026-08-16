// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package access_test

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/testsupport/chartest"
	"github.com/samber/oops"
)

var _ = Describe("ABAC Full Evaluation Path (Canary)", func() {
	It("exercises the complete evaluation path with seed policies", func() {
		ctx := context.Background()

		_, err := env.pool.Exec(ctx, "DELETE FROM player_character_bindings")
		Expect(err).NotTo(HaveOccurred())
		// Must precede characters/locations: held_by_character_id and
		// location_id FKs are ON DELETE SET NULL, which would clear all
		// three containment fields on any leftover object and violate
		// chk_exactly_one_containment. Per holomush-k3ud regression test.
		_, err = env.pool.Exec(ctx, "DELETE FROM objects")
		Expect(err).NotTo(HaveOccurred())
		_, err = env.pool.Exec(ctx, "DELETE FROM characters")
		Expect(err).NotTo(HaveOccurred())
		_, err = env.pool.Exec(ctx, "DELETE FROM players")
		Expect(err).NotTo(HaveOccurred())
		_, err = env.pool.Exec(ctx, "DELETE FROM locations")
		Expect(err).NotTo(HaveOccurred())

		locID := core.NewULID()
		_, err = env.pool.Exec(ctx, `
			INSERT INTO locations (id, name, description, type, replay_policy)
			VALUES ($1, 'Canary Location', 'Test.', 'persistent', 'last:0')`,
			locID.String())
		Expect(err).NotTo(HaveOccurred())

		playerID := core.NewULID()
		_, err = env.pool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash)
			VALUES ($1, $2, 'hash')`,
			playerID.String(), "canary_"+time.Now().Format("150405.000"))
		Expect(err).NotTo(HaveOccurred())

		charID := core.NewULID()
		_, err = env.pool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, location_id, normalized_name, name_skeleton, name_skeleton_unicode_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			append([]any{charID.String(), playerID.String(), uniqueCharFixtureName("CanaryChar", charID), locID.String()},
				chartest.Columns(uniqueCharFixtureName("CanaryChar", charID))...)...)
		Expect(err).NotTo(HaveOccurred())

		env.auditWriter.Reset()

		decision := evalAccess("character:"+charID.String(), "read", "character:"+charID.String())
		Expect(decision.Effect()).To(Equal(types.EffectAllow))
		// Allow entries are written asynchronously via a buffered channel;
		// poll until the async consumer has flushed the entry to the writer.
		Eventually(func() []audit.Event {
			return env.auditWriter.Entries()
		}).WithTimeout(2 * time.Second).WithPolling(10 * time.Millisecond).ShouldNot(BeEmpty())

		decision = evalAccess("character:"+charID.String(), "destroy", "location:"+locID.String())
		Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
	})
})

var _ = Describe("System Bypass", func() {
	It("allows system subject with system context", func() {
		ctx := access.WithSystemSubject(context.Background())
		req := types.AccessRequest{
			Subject:  "system",
			Action:   "read",
			Resource: "location:any-id",
		}
		decision, err := env.engine.Evaluate(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(decision.Effect()).To(Equal(types.EffectSystemBypass))
	})

	It("rejects system subject without system context with SYSTEM_SUBJECT_REJECTED", func() {
		req := types.AccessRequest{
			Subject:  "system",
			Action:   "read",
			Resource: "location:any-id",
		}
		_, err := env.engine.Evaluate(context.Background(), req)
		Expect(err).To(HaveOccurred())
		oopsErr, ok := oops.AsOops(err)
		Expect(ok).To(BeTrue())
		code, isStr := oopsErr.Code().(string)
		Expect(isStr).To(BeTrue())
		Expect(code).To(Equal("SYSTEM_SUBJECT_REJECTED"))
	})
})

// Character retirement is ADMIN-ONLY in v0.13 (IDENT-04, user decision
// 2026-08-07, superseding D-40's player-retires half). D-40's action SPLIT
// survives that decision — `retire` and `unretire` stay distinct so a later
// policy may grant one without the other — but in v0.13 both are reachable
// only through seed:admin-full-access's bare-action permit, and no
// player-facing seed ships.
//
// D-39 is why these specs are the control rather than a nicety: the world
// commands carry NO ownership predicate in Go. Policy IS the control, so the
// only thing standing between a player and retiring someone else's character
// is the absence of a seed that matches — and an absence is exactly what a
// test suite forgets to assert. Each DENY below is therefore PAIRED with an
// admin positive control on the SAME action and resource (PORTAL-10 rule 2):
// without the pair, a typo in the action string would produce a green DENY for
// the wrong reason and prove nothing.
var _ = Describe("Character retirement is admin-only (IDENT-04)", func() {
	var (
		adminChar  ulid.ULID
		playerChar ulid.ULID
		targetChar ulid.ULID
	)

	BeforeEach(func() {
		ctx := context.Background()
		locID := core.NewULID()
		_, err := env.pool.Exec(ctx, `
			INSERT INTO locations (id, name, description, type, replay_policy)
			VALUES ($1, $2, 'Retirement fixtures.', 'persistent', 'last:0')`,
			locID.String(), "Retirement Location "+locID.String()[20:])
		Expect(err).NotTo(HaveOccurred())

		insertChar := func(label string) ulid.ULID {
			playerID := core.NewULID()
			_, insErr := env.pool.Exec(ctx,
				`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'h')`,
				playerID.String(), label+"_"+playerID.String())
			Expect(insErr).NotTo(HaveOccurred())
			charID := core.NewULID()
			name := uniqueCharFixtureName(label, charID)
			_, insErr = env.pool.Exec(ctx, `
				INSERT INTO characters (id, player_id, name, location_id, normalized_name, name_skeleton, name_skeleton_unicode_version)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				append([]any{charID.String(), playerID.String(), name, locID.String()},
					chartest.Columns(name)...)...)
			Expect(insErr).NotTo(HaveOccurred())
			return charID
		}

		adminChar = insertChar("RetAdmin")
		playerChar = insertChar("RetPlayer")
		targetChar = insertChar("RetTarget")

		env.roleResolver.roles[access.CharacterSubject(adminChar.String())] = []string{"admin"}
	})

	AfterEach(func() {
		delete(env.roleResolver.roles, access.CharacterSubject(adminChar.String()))
	})

	It("permits an admin to retire an arbitrary character", func() {
		decision := evalAccess("character:"+adminChar.String(), "retire", "character:"+targetChar.String())
		Expect(decision.Effect()).To(Equal(types.EffectAllow),
			"seed:admin-full-access's bare-action permit is the whole v0.13 human retire surface")
	})

	It("permits an admin to unretire an arbitrary character", func() {
		decision := evalAccess("character:"+adminChar.String(), "unretire", "character:"+targetChar.String())
		Expect(decision.Effect()).To(Equal(types.EffectAllow),
			"D-40's admin-only-unretire half stands unchanged")
	})

	// The load-bearing spec. A player retiring their OWN character is the most
	// sympathetic case for a self-grant, and the one D-40 originally shipped —
	// so proving it DENIES is what proves player self-retire genuinely did not
	// ship, rather than merely being unrouted in the command layer.
	It("denies a non-admin character retiring their OWN character", func() {
		decision := evalAccess("character:"+playerChar.String(), "retire", "character:"+playerChar.String())
		Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny),
			"v0.13 ships NO player self-retire seed; default-deny is the whole mechanism")
	})

	It("denies a non-admin character retiring someone else's character", func() {
		decision := evalAccess("character:"+playerChar.String(), "retire", "character:"+targetChar.String())
		Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
	})

	It("denies a non-admin character unretiring their own character", func() {
		decision := evalAccess("character:"+playerChar.String(), "unretire", "character:"+playerChar.String())
		Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny),
			"unretire is the reversal of an admin action and was never player-facing")
	})

	// seed:player-self-access permits `read`/`write` on the character's own
	// row. This spec is the reason the two DENYs above are not vacuous: the
	// subject, the resource and the resolver all work, and it is specifically
	// the retire/unretire ACTIONS that find no matching permit.
	It("still permits a non-admin character to read their own character", func() {
		decision := evalAccess("character:"+playerChar.String(), "read", "character:"+playerChar.String())
		Expect(decision.Effect()).To(Equal(types.EffectAllow),
			"if this denied too, the retire DENYs above would prove only that the fixture is broken")
	})

	// The retirement reactor's job grant is instance-scoped (D-54): it may
	// touch exactly the character its triggering character_retired event names.
	// These specs exercise the seed's TARGET clause only — the `when` conjuncts
	// read action.job.* attributes that world.JobCaller supplies per call,
	// which evalAccess does not carry. So they pin the FAIL-CLOSED FLOOR
	// beneath the whole job model: with no provenance and no liveness, a job
	// subject has no authority at all. The positive instance-scope path (and
	// its wrong-aggregate DENY twin) needs a live registry plus a real caller
	// and is plan 03-06's.
	Describe("the retirement job has no unattributed authority (D-54)", func() {
		It("denies a job:retirement character write carrying no provenance", func() {
			decision := evalAccess("job:retirement", "write", "character:"+targetChar.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny),
				"without action.job.* attributes the instance fence cannot match, and a missing "+
					"attribute is false for every operator (ADR holomush-iv43)")
		})

		It("denies a job:retirement character read carrying no provenance", func() {
			decision := evalAccess("job:retirement", "read", "character:"+targetChar.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
		})

		It("denies job:retirement the retire action outright", func() {
			decision := evalAccess("job:retirement", "retire", "character:"+targetChar.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny),
				"the reactor REACTS to a retirement; it must never be able to perform one")
		})
	})
})
