// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
	"context"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/outbox"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// retiredEnvelopeCount counts the committed character_retired outbox rows whose
// primary aggregate is characterID. Counting the ROW rather than inspecting a
// returned Go struct is the point: at this layer the guarantee is about what
// survives the transaction, and a populated in-memory envelope proves nothing
// about what committed.
func retiredEnvelopeCount(ctx context.Context, characterID ulid.ULID) int {
	GinkgoHelper()
	var n int
	Expect(env.pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		WHERE aggregate_id = $1 AND kind = $2`,
		characterID.String(), outbox.KindCharacterRetired).Scan(&n)).
		To(Succeed(), "count character_retired outbox rows for the aggregate")
	return n
}

// characterRow reads status+version straight from the pool — the committed row,
// never a service return value.
func characterRow(ctx context.Context, characterID ulid.ULID) (status string, version int) {
	GinkgoHelper()
	Expect(env.pool.QueryRow(ctx,
		`SELECT status, version FROM characters WHERE id = $1`, characterID.String()).
		Scan(&status, &version)).To(Succeed(), "read the committed character row")
	return status, version
}

// retireIntent builds the same envelope intent world.Service builds for a retire
// (kind character_retired, aggregate character, the lifecycle payload), so the
// forced-failure case below poisons the REAL envelope shape rather than a
// stand-in. Service.buildIntent is package-private, so the fields are spelled
// out here; every one of them mirrors internal/world/service.go:990.
func retireIntent(characterID ulid.ULID, actor string) wmodel.EnvelopeIntent {
	GinkgoHelper()
	payload, err := world.BuildCharacterLifecyclePayload(characterID, world.StatusRetired)
	Expect(err).NotTo(HaveOccurred(), "build character lifecycle payload")
	return wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		Kind:          outbox.KindCharacterRetired,
		SchemaVersion: 1,
		Actor:         actor,
		AggregateType: wmodel.AggregateCharacter,
		AggregateID:   characterID,
		Payload:       payload,
	})
}

// IDENT-10 for the retire path, mirroring the INV-WORLD-1 ATOMIC-FEED proof at
// internal/world/postgres/outbox_store_test.go:134 case-for-case:
//
//	commit                     -> both the status change AND its ONE envelope survive;
//	rejected write             -> NEITHER the status change nor an envelope survives;
//	outbox failure after write -> the status change ROLLS BACK with it.
//
// The reference proof binds INV-WORLD-1 on a location Create. This one is the
// same guarantee for Phase 3's new character-lifecycle write, and it asserts
// against the actual DB rows in every case: an envelope-less committed retire, or
// a committed retire envelope for a status change that never landed, is what the
// transactional outbox exists to make impossible.
//
// This spec deliberately does NOT carry a `// Verifies:` annotation. INV-WORLD-1
// is already bound to the reference proof, and the registry's rule is that a
// binding names a test that proves the invariant — adding a second claimant for
// an already-bound entry buys nothing and dilutes the provenance.
var _ = Describe("IDENT-10: a retire's status change and its one envelope commit or roll back together", func() {
	var (
		ctx          context.Context
		locID        ulid.ULID
		playerID     ulid.ULID
		adminSubject string
	)

	BeforeEach(func() {
		ctx = context.Background()
		cleanupCharacterLifecycle(ctx)
		locID = seedLifecycleLocation(ctx)
		playerID = seedLifecyclePlayer(ctx, false)

		// The retire below is authorized by the REAL engine against the REAL
		// seeded corpus (seed.go's admin permit). The role source is the fixture;
		// the evaluation is not — a canned-decision engine would make "the retire
		// committed" pass with nothing authorizing anything.
		adminPlayerID := seedLifecyclePlayer(ctx, false)
		adminID := insertCharacterWithStatus(ctx, adminPlayerID, locID, "Atomicity Admin", "active")
		adminSubject = access.CharacterSubject(adminID.String())
		env.roleResolver.Grant(adminSubject, "admin")
	})

	It("commits the retired status together with exactly one character_retired envelope", func() {
		charID := insertCharacterWithStatus(ctx, playerID, locID, "Committed Retiree", "active")
		_, startVersion := characterRow(ctx, charID)
		Expect(retiredEnvelopeCount(ctx, charID)).To(Equal(0), "control: no envelope exists before the retire")

		Expect(env.worldService.RetireCharacter(ctx, world.HumanCaller(adminSubject), charID, startVersion)).
			To(Succeed(), "the authorized retire must commit")

		status, version := characterRow(ctx, charID)
		Expect(status).To(Equal(string(world.StatusRetired)), "the status change must survive the commit")
		Expect(version).To(Equal(startVersion+1), "a committed retire bumps the version exactly once")
		Expect(retiredEnvelopeCount(ctx, charID)).To(Equal(1),
			"EXACTLY ONE character_retired envelope must survive alongside it — not zero (a state change nobody can observe) and not two")
	})

	It("leaves neither a status change nor an envelope when the write is rejected", func() {
		charID := insertCharacterWithStatus(ctx, playerID, locID, "Rejected Retiree", "active")
		_, startVersion := characterRow(ctx, charID)

		// A stale caller-held expected version: the guard rejects the whole
		// command, so the transaction that would have carried BOTH writes never
		// commits.
		staleVersion := startVersion + 7
		err := env.worldService.RetireCharacter(ctx, world.HumanCaller(adminSubject), charID, staleVersion)
		Expect(err).To(HaveOccurred(), "a stale expected version must be rejected")
		Expect(err).To(MatchError(world.ErrConcurrentEdit), "the rejection is the typed conflict")
		oopsErr, ok := oops.AsOops(err)
		Expect(ok).To(BeTrue(), "conflict must be an oops error")
		Expect(oopsErr.Code()).To(Equal(world.CodeConcurrentEdit))

		status, version := characterRow(ctx, charID)
		Expect(status).To(Equal(string(world.StatusActive)), "the rejected retire must NOT have changed the status")
		Expect(version).To(Equal(startVersion), "the rejected retire must NOT have bumped the version")
		Expect(retiredEnvelopeCount(ctx, charID)).To(Equal(0),
			"no envelope may survive a retire that never landed — a committed envelope for an uncommitted state change is the failure this guarantee forbids")
	})

	It("rolls the status change back when the envelope write fails after it", func() {
		charID := insertCharacterWithStatus(ctx, playerID, locID, "Poisoned Retiree", "active")
		_, startVersion := characterRow(ctx, charID)

		transactor := worldpg.NewTransactor(env.pool)
		outboxStore := worldpg.NewOutboxStore(env.pool)

		// Drive the same two writes the mutator drives — the guarded SetStatus and
		// the envelope insert — inside ONE transaction, then force the SECOND to
		// fail by reusing the intent (a duplicate event_id violates the outbox PK).
		// This is the direction the service path cannot reach on its own, and it is
		// the one that matters: a state change that outlives its failed envelope.
		poison := retireIntent(charID, adminSubject)
		err := transactor.InTransaction(ctx, func(txCtx context.Context) error {
			if _, serr := env.Characters.SetStatus(txCtx, charID, world.StatusRetired, startVersion); serr != nil {
				return serr
			}
			if _, werr := outboxStore.WriteIntent(txCtx, poison, nil); werr != nil {
				return werr
			}
			_, werr := outboxStore.WriteIntent(txCtx, poison, nil)
			return werr
		})
		Expect(err).To(HaveOccurred(), "a duplicate event_id must fail the outbox insert")

		status, version := characterRow(ctx, charID)
		Expect(status).To(Equal(string(world.StatusActive)),
			"the status write rolled back with the failed envelope — no state change survives without its envelope")
		Expect(version).To(Equal(startVersion), "the rolled-back status write left the version untouched")
		Expect(retiredEnvelopeCount(ctx, charID)).To(Equal(0), "neither envelope insert survives the rollback")
	})
})
