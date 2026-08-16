// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// The three admin CHARACTER WRITES at the WIRE, and the two audit properties
// ROADMAP criterion 3 turns on — read through 01-SPEC §14 row 9, which amends it:
// the envelope commits or rolls back WITH the state change, and the events_audit
// row is PROJECTED from that envelope rather than inserted transactionally.
//
// Each assertion here is made against a COMMITTED DATABASE ROW rather than a
// returned Go value. At this layer the guarantee is about what survives the
// transaction, and a populated in-memory envelope proves nothing about what
// committed.
package access_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/outbox"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

// adminWriteEnv is the stack every spec here drives: the real seed corpus (so
// seed:admin-character-administration is what lets the player-flavoured admin
// caller pass world.Service's checkAccess), the gated listener, and the same
// world.Service the portal was wired with.
type adminWriteEnv struct {
	srv    *integrationtest.Server
	client adminportalv1.AdminPortalServiceClient
	admin  *integrationtest.Session
	target *integrationtest.Session
}

func newAdminWriteEnv(t *testing.T, ctx context.Context) *adminWriteEnv {
	t.Helper()
	srv := integrationtest.Start(
		t,
		integrationtest.WithRealABAC(),
		integrationtest.WithGatedGRPCListener(),
	)
	t.Cleanup(srv.Stop)

	return &adminWriteEnv{
		srv:    srv,
		client: adminportalv1.NewAdminPortalServiceClient(srv.GatedGRPCConn()),
		admin:  srv.ConnectAuthedWithRoles(ctx, "Writeadmin", []string{"admin"}),
		// A DIFFERENT player's character: this is the cross-owner surface, and a
		// target the admin happened to own would let an ownership-based permit
		// pass the test in place of the admin one.
		target: srv.ConnectAuthed(ctx, "Writetarget"),
	}
}

// outboxRow is one committed envelope, read straight from the table.
type outboxRow struct {
	kind    string
	actor   string
	payload []byte
}

// envelopesFor returns every committed outbox row for an aggregate, in feed
// order.
func envelopesFor(t *testing.T, ctx context.Context, srv *integrationtest.Server, aggregateID ulid.ULID) []outboxRow {
	t.Helper()
	rows, err := srv.Pool().Query(ctx, `
		SELECT kind, actor, payload FROM outbox
		WHERE aggregate_id = $1 ORDER BY feed_position`, aggregateID.String())
	require.NoError(t, err)
	defer rows.Close()

	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		require.NoError(t, rows.Scan(&r.kind, &r.actor, &r.payload))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// auditRowCountFor counts events_audit rows naming an aggregate anywhere in the
// stored envelope. It is deliberately a substring match over the envelope column
// rather than a subject predicate: the point is "no row about this character
// exists", and a narrower predicate could report zero for the wrong reason.
func auditRowCountFor(t *testing.T, ctx context.Context, srv *integrationtest.Server, aggregateID ulid.ULID) int {
	t.Helper()
	var n int
	require.NoError(t, srv.Pool().QueryRow(ctx,
		`SELECT count(*) FROM events_audit WHERE position($1 in encode(envelope, 'escape')) > 0`,
		aggregateID.String()).Scan(&n))
	return n
}

func characterRow(t *testing.T, ctx context.Context, srv *integrationtest.Server, id ulid.ULID) (status string, version int) {
	t.Helper()
	require.NoError(t, srv.Pool().QueryRow(ctx,
		`SELECT status, version FROM characters WHERE id = $1`, id.String()).Scan(&status, &version))
	return status, version
}

// TestAdminRetireEmitsOneTransactionalEnvelopeCarryingThePlayerActor is Test 1.
//
// It pins four things one committed row must show at once: exactly ONE envelope,
// the D-104 Actor identity, the D-103 before-status DIFFERING from the new
// status, and the §10.7 admin context. A payload asserting merely that a status
// is PRESENT would pass under the pre-widening shape.
//
// Its acting-character-absent assertion is the alt-linkage half of D-104.
//
// Verifies: INV-PRIVACY-13
func TestAdminRetireEmitsOneTransactionalEnvelopeCarryingThePlayerActor(t *testing.T) {
	ctx := context.Background()
	env := newAdminWriteEnv(t, ctx)

	_, startVersion := characterRow(t, ctx, env.srv, env.target.CharacterID)
	require.Empty(t, envelopesFor(t, ctx, env.srv, env.target.CharacterID),
		"control: no envelope exists for this character before the retire")

	_, err := env.client.AdminRetireCharacter(ctx, &adminportalv1.AdminRetireCharacterRequest{
		PlayerSessionToken: env.admin.PlayerSessionToken(),
		CharacterId:        env.target.CharacterID.String(),
		ExpectedVersion:    int32(startVersion), //nolint:gosec // a freshly-seeded row version
	})
	require.NoError(t, err, "the admin retire must pass BOTH gates: the section interceptor and world.Service.checkAccess")

	status, version := characterRow(t, ctx, env.srv, env.target.CharacterID)
	assert.Equal(t, string(world.StatusRetired), status)
	assert.Equal(t, startVersion+1, version, "a committed retire bumps the version exactly once")

	envs := envelopesFor(t, ctx, env.srv, env.target.CharacterID)
	require.Len(t, envs, 1,
		"EXACTLY ONE envelope — not zero (a state change nobody can observe) and not two")
	assert.Equal(t, outbox.KindCharacterRetired, envs[0].kind)
	assert.Equal(t, "player:"+env.admin.PlayerID.String(), envs[0].actor,
		"D-104: the envelope Actor is player:<id>, carried verbatim from Caller.subject")

	var payload world.CharacterLifecycleChangePayload
	require.NoError(t, json.Unmarshal(envs[0].payload, &payload))
	assert.Equal(t, env.target.CharacterID.String(), payload.CharacterID)
	assert.Equal(t, string(world.StatusActive), payload.BeforeStatus)
	assert.Equal(t, string(world.StatusRetired), payload.Status)
	require.NotEqual(t, payload.BeforeStatus, payload.Status,
		"a real transition's before and after MUST differ; a presence-only assertion is insufficient")
	assert.Equal(t, "characters", payload.Section)
	assert.Equal(t, "write", payload.Action)

	// D-104's other half: the ACTING CHARACTER is recorded nowhere. A durable
	// player-to-alt linkage in a retained table is what that decision removes.
	assert.NotContains(t, string(envs[0].payload), env.admin.CharacterID.String(),
		"the acting character id MUST NOT reach the payload")
}

// TestAdminRetireIsIdempotentThroughTheShippedLifecycleGuard: a second retire is
// refused BEFORE any write, so exactly one envelope can ever exist for one
// retirement — and before/after can never be equal in an emitted payload.
func TestAdminRetireIsIdempotentThroughTheShippedLifecycleGuard(t *testing.T) {
	ctx := context.Background()
	env := newAdminWriteEnv(t, ctx)

	_, startVersion := characterRow(t, ctx, env.srv, env.target.CharacterID)
	req := &adminportalv1.AdminRetireCharacterRequest{
		PlayerSessionToken: env.admin.PlayerSessionToken(),
		CharacterId:        env.target.CharacterID.String(),
		ExpectedVersion:    int32(startVersion), //nolint:gosec // a freshly-seeded row version
	}
	_, err := env.client.AdminRetireCharacter(ctx, req)
	require.NoError(t, err)

	// The SAME request again. Its version is now stale AND the character is
	// already retired; the version precheck runs first, so this is Aborted.
	_, err = env.client.AdminRetireCharacter(ctx, req)
	require.Error(t, err)
	assert.Equal(t, codes.Aborted, status.Code(err))

	// And with a FRESH version, the lifecycle guard is what refuses.
	_, freshVersion := characterRow(t, ctx, env.srv, env.target.CharacterID)
	req.ExpectedVersion = int32(freshVersion) //nolint:gosec // a small row version
	_, err = env.client.AdminRetireCharacter(ctx, req)
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))

	assert.Len(t, envelopesFor(t, ctx, env.srv, env.target.CharacterID), 1,
		"running retire three times produces exactly ONE character_retired envelope")
}

// TestAdminUnretireReturnsTheCharacterThroughTheCanonicalCommand rounds out the
// lifecycle pair, and pins that retire is REVERSIBLE — which is what makes the
// absence of any admin delete a complete story rather than a missing feature.
func TestAdminUnretireReturnsTheCharacterThroughTheCanonicalCommand(t *testing.T) {
	ctx := context.Background()
	env := newAdminWriteEnv(t, ctx)

	_, v0 := characterRow(t, ctx, env.srv, env.target.CharacterID)
	_, err := env.client.AdminRetireCharacter(ctx, &adminportalv1.AdminRetireCharacterRequest{
		PlayerSessionToken: env.admin.PlayerSessionToken(),
		CharacterId:        env.target.CharacterID.String(),
		ExpectedVersion:    int32(v0), //nolint:gosec // a freshly-seeded row version
	})
	require.NoError(t, err)

	_, v1 := characterRow(t, ctx, env.srv, env.target.CharacterID)
	resp, err := env.client.AdminUnretireCharacter(ctx, &adminportalv1.AdminUnretireCharacterRequest{
		PlayerSessionToken: env.admin.PlayerSessionToken(),
		CharacterId:        env.target.CharacterID.String(),
		ExpectedVersion:    int32(v1), //nolint:gosec // a small row version
	})
	require.NoError(t, err)
	assert.Equal(t, string(world.StatusActive), resp.GetCharacter().GetStatus())

	envs := envelopesFor(t, ctx, env.srv, env.target.CharacterID)
	require.Len(t, envs, 2)
	assert.Equal(t, outbox.KindCharacterUnretired, envs[1].kind)

	var payload world.CharacterLifecycleChangePayload
	require.NoError(t, json.Unmarshal(envs[1].payload, &payload))
	assert.Equal(t, string(world.StatusRetired), payload.BeforeStatus)
	assert.Equal(t, string(world.StatusActive), payload.Status)
	assert.Equal(t, "characters", payload.Section)
}

// TestAdminUpdateEmitsOneNamesOnlyEnvelopeAndNoProse is Test 5, and the
// prose-absence half is asserted over the SERIALIZED PAYLOAD BYTES rather than
// over struct fields: a struct assertion would pass under a payload that carried
// the values in a field the test did not name.
//
// It also asserts the D-104 Actor on the UPDATE path. The retire path reaches
// the envelope through a different world method with a different option set, so
// an Actor regression here would otherwise go unobserved.
//
// Verifies: INV-PRIVACY-13
func TestAdminUpdateEmitsOneNamesOnlyEnvelopeAndNoProse(t *testing.T) {
	ctx := context.Background()
	env := newAdminWriteEnv(t, ctx)

	const (
		oldDescription = "an unmistakable OLD in-world description"
		newDescription = "an unmistakable NEW in-world description"
		newBiography   = "an unmistakable NEW biography paragraph"
	)

	// Seed a real OLD description so the byte-absence assertion has both sides
	// to look for. Without it the "old value absent" clause is vacuous.
	_, v0 := characterRow(t, ctx, env.srv, env.target.CharacterID)
	require.NoError(t, env.srv.World().UpdateCharacterDescription(ctx,
		world.SystemCaller(), env.target.CharacterID, oldDescription),
		"seed the pre-edit description through the shipped owner command")

	_, v1 := characterRow(t, ctx, env.srv, env.target.CharacterID)
	before := len(envelopesFor(t, ctx, env.srv, env.target.CharacterID))

	resp, err := env.client.AdminUpdateCharacter(ctx, &adminportalv1.AdminUpdateCharacterRequest{
		PlayerSessionToken: env.admin.PlayerSessionToken(),
		CharacterId:        env.target.CharacterID.String(),
		ExpectedVersion:    int32(v1), //nolint:gosec // a small row version
		UpdateMask:         &fieldmaskpb.FieldMask{Paths: []string{"description", "profile.biography"}},
		Description:        newDescription,
		Biography:          newBiography,
	})
	require.NoError(t, err)

	// ONE version bump for a MIXED mask. Two domain calls would show two.
	assert.Equal(t, int32(v1+1), resp.GetCharacter().GetVersion(), //nolint:gosec // a small row version
		"a mixed description + profile.* mask is ONE write and ONE version bump")
	assert.Greater(t, v1, v0, "control: the seeding write really did bump the version")

	envs := envelopesFor(t, ctx, env.srv, env.target.CharacterID)
	require.Len(t, envs, before+1,
		"a mixed mask emits EXACTLY ONE envelope; two domain calls would emit two")
	emitted := envs[len(envs)-1]

	assert.Equal(t, outbox.KindCharacterProfileUpdate, emitted.kind,
		"never kindCharacterUpdated, whose declared payload carries the description STRING")
	assert.Equal(t, "player:"+env.admin.PlayerID.String(), emitted.actor,
		"D-104 on the UPDATE path too — a different world method with a different option set")

	var payload world.CharacterProfileUpdateChangePayload
	require.NoError(t, json.Unmarshal(emitted.payload, &payload))
	assert.Equal(t, []string{"description", "profile.biography"}, payload.ChangedAttributes,
		"a SORTED two-element list of bare NAMES; description appears as a name, never as a value")
	assert.Equal(t, "characters", payload.Section)
	assert.Equal(t, "write", payload.Action)

	// THE D-103 PROOF, over the bytes a consumer receives.
	raw := string(emitted.payload)
	require.NotContains(t, raw, oldDescription, "the OLD description value MUST NOT reach the payload")
	require.NotContains(t, raw, newDescription, "the NEW description value MUST NOT reach the payload")
	require.NotContains(t, raw, newBiography, "the NEW biography value MUST NOT reach the payload")

	// And the write really landed — otherwise the absence assertions above would
	// be satisfied by a request that did nothing.
	var storedDescription string
	require.NoError(t, env.srv.Pool().QueryRow(ctx,
		`SELECT description FROM characters WHERE id = $1`, env.target.CharacterID.String()).
		Scan(&storedDescription))
	assert.Equal(t, newDescription, storedDescription,
		"the description write is real; the payload's silence about it is the point")
}

// TestAdminUpdateWithAMaskOfOnlyUnchangedValuesIsATrueNoOp is the C3-26 case at
// the wire: a NON-EMPTY mask naming only a byte-identical value must leave no
// trace at all.
//
// Removing world.WithSkipUnchangedProperties() from the handler makes this fail
// with the version incremented and ONE envelope carrying an empty
// changed_attributes — because the domain's equal-valued row rewrite is
// UNCONDITIONAL, which is exactly why a handler precheck could not deliver this.
func TestAdminUpdateWithAMaskOfOnlyUnchangedValuesIsATrueNoOp(t *testing.T) {
	ctx := context.Background()
	env := newAdminWriteEnv(t, ctx)

	const biography = "a biography submitted twice, byte for byte"

	_, v0 := characterRow(t, ctx, env.srv, env.target.CharacterID)
	_, err := env.client.AdminUpdateCharacter(ctx, &adminportalv1.AdminUpdateCharacterRequest{
		PlayerSessionToken: env.admin.PlayerSessionToken(),
		CharacterId:        env.target.CharacterID.String(),
		ExpectedVersion:    int32(v0), //nolint:gosec // a freshly-seeded row version
		UpdateMask:         &fieldmaskpb.FieldMask{Paths: []string{"profile.biography"}},
		Biography:          biography,
	})
	require.NoError(t, err)

	_, v1 := characterRow(t, ctx, env.srv, env.target.CharacterID)
	require.Equal(t, v0+1, v1, "control: the FIRST submission is a real change and does bump the version")
	before := len(envelopesFor(t, ctx, env.srv, env.target.CharacterID))

	// The SAME value again, under a NON-EMPTY mask.
	_, err = env.client.AdminUpdateCharacter(ctx, &adminportalv1.AdminUpdateCharacterRequest{
		PlayerSessionToken: env.admin.PlayerSessionToken(),
		CharacterId:        env.target.CharacterID.String(),
		ExpectedVersion:    int32(v1), //nolint:gosec // a small row version
		UpdateMask:         &fieldmaskpb.FieldMask{Paths: []string{"profile.biography"}},
		Biography:          biography,
	})
	require.NoError(t, err, "a resubmit of unchanged values is a SUCCESS, not a refusal")

	_, v2 := characterRow(t, ctx, env.srv, env.target.CharacterID)
	assert.Equal(t, v1, v2, "no row rewrite, so no version bump")
	assert.Len(t, envelopesFor(t, ctx, env.srv, env.target.CharacterID), before,
		"and NO envelope: an audit record claiming a change nobody made is exactly the noise "+
			"the admin-only skip option exists to prevent")
	assert.Zero(t, auditRowCountFor(t, ctx, env.srv, env.target.CharacterID),
		"and therefore no events_audit row either")
}

// TestAdminUpdateWithAnEmptyMaskIsANoOpSuccessAfterTheGuards is the §9.5 rule 4
// contract at the wire, plus the one clause where the admin path DIVERGES from
// the player facade.
func TestAdminUpdateWithAnEmptyMaskIsANoOpSuccessAfterTheGuards(t *testing.T) {
	ctx := context.Background()
	env := newAdminWriteEnv(t, ctx)

	_, v0 := characterRow(t, ctx, env.srv, env.target.CharacterID)

	resp, err := env.client.AdminUpdateCharacter(ctx, &adminportalv1.AdminUpdateCharacterRequest{
		PlayerSessionToken: env.admin.PlayerSessionToken(),
		CharacterId:        env.target.CharacterID.String(),
		ExpectedVersion:    int32(v0), //nolint:gosec // a freshly-seeded row version
		UpdateMask:         &fieldmaskpb.FieldMask{},
	})
	require.NoError(t, err, "§9.5 rule 4: a request with no paths changes nothing and returns SUCCESS")
	assert.Equal(t, int32(v0), resp.GetCharacter().GetVersion()) //nolint:gosec // a small row version
	assert.Empty(t, envelopesFor(t, ctx, env.srv, env.target.CharacterID))

	// THE DIVERGENCE, stated as one. characteraccess_write.go answers a STALE
	// empty-mask caller with SUCCESS ("A STALE CALLER IS ANSWERED, NOT REFUSED").
	// The admin path refuses, because this caller is editing SOMEONE ELSE'S
	// character on an audited surface and a stale guard means the request was
	// composed against a view that has since changed.
	_, err = env.client.AdminUpdateCharacter(ctx, &adminportalv1.AdminUpdateCharacterRequest{
		PlayerSessionToken: env.admin.PlayerSessionToken(),
		CharacterId:        env.target.CharacterID.String(),
		ExpectedVersion:    int32(v0) + 7, //nolint:gosec // a deliberately stale version
		UpdateMask:         &fieldmaskpb.FieldMask{},
	})
	require.Error(t, err)
	assert.Equal(t, codes.Aborted, status.Code(err))
}

// TestARolledBackAdminMutationLeavesNeitherAnEnvelopeNorAnAuditRow is Test 2.
//
// # The rollback seam, and why it is this one
//
// The precedent is test/integration/world/character_retire_atomicity_test.go,
// which forces the failure by duplicating an outbox intent inside a real
// transaction so the second insert violates the outbox primary key. That spec
// does NOT drive an RPC, so it cannot be reused verbatim.
//
// This spec takes ROUTE (a) of the two the plan offers — extend the precedent's
// shape, needing NO production seam. It PRE-INSERTS a conflicting outbox row
// carrying the exact event_id the admin mutation is about to allocate, so the
// mutation's own insert collides and the whole transaction — the status write AND
// the envelope — rolls back. Route (b), a test-only harness fault injector, was
// not needed: the outbox primary key is reachable from the test without any hook
// on a production type, and adding one would have been a seam that exists only to
// be tested.
//
// The assertion is BOTH tables. A test asserting only events_audit would pass
// under a non-transactional envelope write, which is the failure §14 row 9 exists
// to prevent.
//
// Verifies: INV-WORLD-9
func TestARolledBackAdminMutationLeavesNeitherAnEnvelopeNorAnAuditRow(t *testing.T) {
	ctx := context.Background()
	env := newAdminWriteEnv(t, ctx)

	// One REAL write first. world_feed_counter is upserted lazily by the
	// allocator, so on a fresh database there is no row to read a position from
	// until an envelope has been written at least once — and a test that read a
	// missing row would fail for a reason that has nothing to do with the
	// property under test.
	require.NoError(t, env.srv.World().UpdateCharacterDescription(ctx,
		world.SystemCaller(), env.target.CharacterID, "a description that establishes the feed counter"),
		"prime the per-game feed counter with one committed envelope")

	_, startVersion := characterRow(t, ctx, env.srv, env.target.CharacterID)

	// Occupy the NEXT feed position for this game, so the mutation's own envelope
	// insert violates outbox_game_epoch_position_unique. The allocator reads
	// world_feed_counter FOR UPDATE inside the MUTATION transaction, so the
	// collision happens AFTER the status write has already landed in that same
	// transaction — which is precisely the direction that matters: a state change
	// that would otherwise outlive its failed envelope.
	//
	// This is route (a) of the two the plan offers: it extends the shape
	// test/integration/world/character_retire_atomicity_test.go uses (force a
	// duplicate-key failure on the envelope insert) to a spec that drives the
	// admin RPC end to end. Route (b) — a test-only harness fault injector — was
	// not needed, and a seam that exists only to be tested is worth avoiding.
	var epoch int64
	var nextPosition int64
	require.NoError(t, env.srv.Pool().QueryRow(ctx,
		`SELECT epoch, next_position FROM world_feed_counter LIMIT 1`).Scan(&epoch, &nextPosition),
		"world_feed_counter.next_position is the position the allocator will hand out next")

	poison := ulid.Make()
	_, err := env.srv.Pool().Exec(ctx, `
		INSERT INTO outbox (event_id, game_id, feed_position, epoch, kind, schema_version,
		                    actor, aggregate_id, aggregate_type, affected, payload)
		SELECT $1, game_id, $2, $3, 'character_retired', 1, 'system', $4, 'character', '[]'::jsonb, '{}'::jsonb
		  FROM world_feed_counter LIMIT 1`,
		poison.String(), nextPosition, epoch, env.target.CharacterID.String())
	require.NoError(t, err, "seed the conflicting outbox row at the position the mutation will claim")

	beforeEnvelopes := len(envelopesFor(t, ctx, env.srv, env.target.CharacterID))

	_, retireErr := env.client.AdminRetireCharacter(ctx, &adminportalv1.AdminRetireCharacterRequest{
		PlayerSessionToken: env.admin.PlayerSessionToken(),
		CharacterId:        env.target.CharacterID.String(),
		ExpectedVersion:    int32(startVersion), //nolint:gosec // a freshly-seeded row version
	})
	require.Error(t, retireErr, "the colliding envelope insert must fail the whole mutation")

	status, version := characterRow(t, ctx, env.srv, env.target.CharacterID)
	assert.Equal(t, string(world.StatusActive), status,
		"the status write rolled back with the failed envelope — no state change survives without its envelope")
	assert.Equal(t, startVersion, version, "and the version is untouched")
	assert.Len(t, envelopesFor(t, ctx, env.srv, env.target.CharacterID), beforeEnvelopes,
		"no NEW envelope survives the rollback")
	assert.Zero(t, auditRowCountFor(t, ctx, env.srv, env.target.CharacterID),
		"and no events_audit row exists for a mutation that never committed")
}

// TestAdminWritesAreDeniedToANonAdminAtTheWireWithPairedControls walks all three
// write RPCs through both outcomes, so an RPC added to the service without a
// descriptor entry cannot be covered by omission.
//
// The paired control is what distinguishes "denied for lack of the admin role"
// from "the RPC is broken" or "the writer was never wired" — and this plan wires
// a new writer at two composition roots, so a silently-unwired one is exactly the
// failure a bare denial assertion would hide.
func TestAdminWritesAreDeniedToANonAdminAtTheWireWithPairedControls(t *testing.T) {
	ctx := context.Background()
	env := newAdminWriteEnv(t, ctx)
	nonAdmin := env.srv.ConnectAuthed(ctx, "Writeplain")

	_, v0 := characterRow(t, ctx, env.srv, env.target.CharacterID)

	tests := []struct {
		name string
		call func(token string, version int32) error
	}{
		{
			name: "AdminUpdateCharacter",
			call: func(token string, version int32) error {
				_, err := env.client.AdminUpdateCharacter(ctx, &adminportalv1.AdminUpdateCharacterRequest{
					PlayerSessionToken: token,
					CharacterId:        env.target.CharacterID.String(),
					ExpectedVersion:    version,
					UpdateMask:         &fieldmaskpb.FieldMask{Paths: []string{"profile.concept"}},
					Concept:            "a concept only an admin may write",
				})
				return err
			},
		},
		{
			name: "AdminRetireCharacter",
			call: func(token string, version int32) error {
				_, err := env.client.AdminRetireCharacter(ctx, &adminportalv1.AdminRetireCharacterRequest{
					PlayerSessionToken: token, CharacterId: env.target.CharacterID.String(), ExpectedVersion: version,
				})
				return err
			},
		},
		{
			name: "AdminUnretireCharacter",
			call: func(token string, version int32) error {
				_, err := env.client.AdminUnretireCharacter(ctx, &adminportalv1.AdminUnretireCharacterRequest{
					PlayerSessionToken: token, CharacterId: env.target.CharacterID.String(), ExpectedVersion: version,
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" denies a non-admin", func(t *testing.T) {
			requireOpaqueRefusal(t, tt.call(nonAdmin.PlayerSessionToken(), int32(v0))) //nolint:gosec // a small row version
		})
	}

	// The paired POSITIVE controls, run in an order the lifecycle permits:
	// update, then retire, then unretire, each re-reading the version.
	_, v := characterRow(t, ctx, env.srv, env.target.CharacterID)
	require.NoError(t, tests[0].call(env.admin.PlayerSessionToken(), int32(v))) //nolint:gosec // a small row version
	_, v = characterRow(t, ctx, env.srv, env.target.CharacterID)
	require.NoError(t, tests[1].call(env.admin.PlayerSessionToken(), int32(v))) //nolint:gosec // a small row version
	_, v = characterRow(t, ctx, env.srv, env.target.CharacterID)
	require.NoError(t, tests[2].call(env.admin.PlayerSessionToken(), int32(v))) //nolint:gosec // a small row version
}
