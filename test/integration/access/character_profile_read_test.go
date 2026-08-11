// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package access_test

import (
	"context"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/access/policy"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/access/profilevis"
	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/core"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/testsupport/chartest"
	"github.com/holomush/holomush/internal/world"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// descriptionPolicyName is the seed entry this file's paired positive control
// proves is load-bearing: the D-76 viewer twin that lets a `viewer:` principal
// read characters.description. Before plan 04-01 seeded it, NO shipped policy
// granted a viewer any read on that column — both tier-floor policies target
// `resource is property` — so the control below is the tree exactly as it stood
// one commit earlier.
const descriptionPolicyName = "seed:viewer-character-description-read"

// reachabilityPolicyName is the §8.4.2 reachability permit. The guest-rung spec
// swaps it for a guest-and-player-only variant so that the SAME request with and
// without a session token lands on two different rungs — which is what proves the
// facade resolved the rung from the token rather than collapsing every caller to
// `anonymous`.
const reachabilityPolicyName = "seed:profile-reachable"

// integrationBiographySentinel is the criterion-2 marker for the full-stack
// absence assertion. It is long, hyphenated and uppercase so a match in a
// marshaled body is the seeded value rather than framing, a field name or
// another fixture — and it is seeded NON-EMPTY, because a withholding spec
// written against an empty row passes while the property is false.
const integrationBiographySentinel = "SENTINEL-INTEGRATION-BIOGRAPHY-BELOW-ANON-FLOOR-9b41ce07"

// profileCorpusStore is the paired-positive-control corpus for this package: the
// real seeded corpus with named policies removed and/or extra ones appended.
//
// It generalizes profile_public_read_test.go's excludingPolicyStore in the one
// direction that file did not need. Cache.Reload reads the corpus through
// ListEnabled and compiles each row's DSLText and nothing else (cache.go:142-158),
// so overriding that one method is the whole mechanism and an appended row needs
// only a name and a DSL body.
//
// The exclusion is BY NAME, and `removed` counts what ListEnabled actually
// dropped, so a control whose name stops matching fails loudly instead of
// quietly becoming a copy of the full corpus while still LOOKING like a control.
type profileCorpusStore struct {
	policystore.PolicyStore

	excluded map[string]bool
	appended []*policystore.StoredPolicy
	removed  int
}

func (s *profileCorpusStore) ListEnabled(ctx context.Context) ([]*policystore.StoredPolicy, error) {
	all, err := s.PolicyStore.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}

	kept := make([]*policystore.StoredPolicy, 0, len(all)+len(s.appended))
	s.removed = 0
	for _, p := range all {
		if s.excluded[p.Name] {
			s.removed++
			continue
		}
		kept = append(kept, p)
	}
	return append(kept, s.appended...), nil
}

// newCorpusEngine builds a second engine over the SAME real provider stack as
// env.engine but a DIFFERENT policy corpus, and pins that the corpus really does
// differ from the seeded one.
//
// It is package-level rather than a per-file closure DELIBERATELY. Two
// byte-similar copies existed — one here, one in character_directory_test.go —
// and only this one carried the differs-in-at-least-one-direction refusal. That
// file is safe today only because all three of its call sites happen to pass a
// non-empty `excluded`; an append-only control added there later would have
// reached exactly the disarmed shape the refusal exists to reject. One
// definition means one place for the refusal to live.
//
// What the two guards prove, and what they do not: the refusal proves the corpus
// differs in SHAPE, and `removed` proves each named exclusion actually matched.
// Neither proves the difference took EFFECT — an appended `forbid` naming a
// resource that matches nothing still compiles and still passes Cache.Reload.
// Effect is each spec's own paired positive control to establish.
func newCorpusEngine(ctx context.Context, excluded []string, appended ...*policystore.StoredPolicy) *policy.Engine {
	exSet := make(map[string]bool, len(excluded))
	for _, name := range excluded {
		exSet[name] = true
	}

	// A corpus that neither excludes nor appends is byte-identical to the seeded
	// one, so a "control" built from it differs from the subject in NOTHING and
	// can only ever agree with it. Refuse that shape here rather than let a
	// caller reach it by passing a nil exclusion list and forgetting the append.
	Expect(len(excluded)+len(appended)).To(BeNumerically(">", 0),
		"a control corpus must differ from the seeded one in at least one direction — exclude a policy, append a policy, or both")

	corpus := &profileCorpusStore{PolicyStore: env.pStore, excluded: exSet, appended: appended}
	cache := policy.NewCache(corpus, env.compiler)
	Expect(cache.Reload(ctx)).To(Succeed())
	Expect(corpus.removed).To(Equal(len(excluded)),
		"the control corpus must exclude exactly %d policies (%v) — a 0 here means the name no longer matches anything seeded and the control is disarmed",
		len(excluded), excluded)
	return policy.NewEngine(env.resolver, cache, &noopSessionResolver{}, env.auditLogger)
}

var _ = Describe("PROFILE-04/PROFILE-05/EXT-06: the anonymous public profile read", func() {
	const (
		anonToken  = ""
		guestToken = "guest-profile-read-token"
	)

	var (
		charID   ulid.ULID // the character whose profile is read
		charName string
		charDesc string
		// otherCharID is a SECOND reachable character, named by no spec's
		// targeted policy. It exists so a control can assert the engine under
		// test still PERMITS something — an engine that denied every profile
		// would satisfy an all-negative differential just as well.
		otherCharID   ulid.ULID
		otherCharName string
		guestID       ulid.ULID // the guest PLAYER whose session token drives the guest rung
		locID         ulid.ULID
		srv           *holoGRPC.CharacterAccessServer
		psRepo        auth.PlayerSessionRepository
		playerRep     auth.PlayerRepository
	)

	// newServer builds a CharacterAccessServer over an arbitrary policy engine,
	// with the harness's real world repositories underneath. Every spec in this
	// file drives the SAME constructor, so a control differs from the subject
	// only in its policy corpus.
	newServer := func(engine *policy.Engine) *holoGRPC.CharacterAccessServer {
		worldSvc := world.NewService(world.ServiceConfig{
			CharacterRepo: env.charRepo,
			PropertyRepo:  env.propRepo,
			Engine:        engine,
		})
		return holoGRPC.NewCharacterAccessServer(
			worldSvc,
			worldSvc,
			&profilevis.Evaluator{Engine: engine},
			// The §9.2 directory gate's evaluator and the directory enumeration.
			// No spec in this file lists the directory, but both are wired to the
			// same values cmd/holomush passes rather than to nil, so a spec added
			// later exercises the production shape.
			engine,
			setup.NewCharRepoAdapter(env.pool, env.charRepo),
			psRepo,
			playerRep,
			// The owner audience's repository. No spec in this file reaches it
			// — every spec here drives the PUBLIC read — but the production
			// adapter is wired anyway rather than nil, so a spec added later
			// exercises the same shape cmd/holomush does.
			setup.NewCharRepoAdapter(env.pool, env.charRepo),
		)
	}

	read := func(id, token string) (*characteraccessv1.GetCharacterProfileResponse, error) {
		return srv.GetCharacterProfile(context.Background(), &characteraccessv1.GetCharacterProfileRequest{
			CharacterId:        id,
			PlayerSessionToken: token,
		})
	}

	// insertProperty writes one real entity_properties row, so the enumeration
	// under test is world.Service's own ABAC-filtered read over the real
	// repository rather than a fixture slice.
	insertProperty := func(ctx context.Context, parentID ulid.ULID, name, value, visibility string) {
		_, err := env.pool.Exec(ctx, `
			INSERT INTO entity_properties (id, parent_type, parent_id, name, value, visibility)
			VALUES ($1, 'character', $2, $3, $4, $5)`,
			core.NewULID().String(), parentID.String(), name, value, visibility)
		Expect(err).NotTo(HaveOccurred())
	}

	BeforeEach(func() {
		ctx := context.Background()

		// Cleanup order is seed_policies_test.go's, for its documented reasons:
		// bindings first; objects before characters/locations because their
		// containment FKs are ON DELETE SET NULL; entity_properties before
		// characters and locations because it references them by parent_id at the
		// APPLICATION layer with no FK. player_sessions precedes players because
		// it carries a player FK.
		for _, table := range []string{
			"player_character_bindings",
			"objects",
			"entity_properties",
			"characters",
			"player_sessions",
			"players",
			"locations",
		} {
			_, err := env.pool.Exec(ctx, "DELETE FROM "+table)
			Expect(err).NotTo(HaveOccurred())
		}

		psRepo = store.NewPostgresPlayerSessionStore(env.pool)
		playerRep = authpg.NewPlayerRepository(env.pool)

		locID = core.NewULID()
		_, err := env.pool.Exec(ctx, `
			INSERT INTO locations (id, name, description, type, replay_policy)
			VALUES ($1, 'Profile Hall', 'A hall.', 'persistent', 'last:0')`,
			locID.String())
		Expect(err).NotTo(HaveOccurred())

		ownerID := core.NewULID()
		Expect(playerRep.Create(ctx, &auth.Player{
			ID:           ownerID,
			Username:     "owner_" + ownerID.String()[20:],
			PasswordHash: "hash",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		})).To(Succeed())

		charID = core.NewULID()
		charName = uniqueCharFixtureName("Subject", charID)
		charDesc = "A tall figure wrapped in a travelling cloak."
		_, err = env.pool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, description, location_id, normalized_name, name_skeleton, name_skeleton_unicode_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			append([]any{charID.String(), ownerID.String(), charName, charDesc, locID.String()},
				chartest.Columns(charName)...)...)
		Expect(err).NotTo(HaveOccurred())

		// The second, unremarkable character. It carries no property rows, so it
		// changes nothing for the specs that enumerate charID's profile; its only
		// job is to be reachable while a targeted policy names charID.
		otherCharID = core.NewULID()
		otherCharName = uniqueCharFixtureName("Bystander", otherCharID)
		_, err = env.pool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, description, location_id, normalized_name, name_skeleton, name_skeleton_unicode_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			append([]any{otherCharID.String(), ownerID.String(), otherCharName, "A bystander.", locID.String()},
				chartest.Columns(otherCharName)...)...)
		Expect(err).NotTo(HaveOccurred())

		// The guest player and its live session. The token is what moves the
		// reading principal off the `anonymous` rung.
		guestID = core.NewULID()
		Expect(playerRep.Create(ctx, &auth.Player{
			ID:           guestID,
			Username:     "guest_" + guestID.String()[20:],
			PasswordHash: "hash",
			IsGuest:      true,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		})).To(Succeed())
		sess, err := auth.NewPlayerSession(guestID, auth.HashSessionToken(guestToken), "", "", auth.PlayerSessionTTL)
		Expect(err).NotTo(HaveOccurred())
		Expect(psRepo.Create(ctx, sess)).To(Succeed())

		srv = newServer(env.engine)
		env.auditWriter.Reset()
	})

	Describe("a logged-out visitor standing nowhere reads name and in-world description", func() {
		It("P1: returns the character's name and description with no session and no location", func() {
			resp, err := read(charID.String(), anonToken)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.GetCharacter().GetId()).To(Equal(charID.String()))
			Expect(resp.GetCharacter().GetName()).To(Equal(charName))
			Expect(resp.GetCharacter().GetDescription()).To(Equal(charDesc))
		})

		It("P1b: the name is the stored bytes verbatim — no normalization, casefolding or re-encoding", func() {
			resp, err := read(charID.String(), anonToken)
			Expect(err).NotTo(HaveOccurred())

			var stored string
			Expect(env.pool.QueryRow(context.Background(),
				`SELECT name FROM characters WHERE id = $1`, charID.String()).Scan(&stored)).To(Succeed())
			Expect([]byte(resp.GetCharacter().GetName())).To(Equal([]byte(stored)),
				"the read path must emit characters.name byte-for-byte")
		})

		It("P2: WITHOUT the new viewer description permit the same read does not carry the description (paired positive control)", func() {
			ctx := context.Background()

			// The tree exactly as it stood before this plan seeded the twin. No
			// shipped policy grants a `viewer:` principal a read_description on a
			// character, so world.Service default-denies and the facade renders
			// the uniform opaque outcome. The point of the control is that the
			// description is UNREACHABLE without this one named policy.
			control := newServer(newCorpusEngine(ctx, []string{descriptionPolicyName}))

			resp, err := control.GetCharacterProfile(ctx, &characteraccessv1.GetCharacterProfileRequest{
				CharacterId: charID.String(),
			})
			Expect(err).To(HaveOccurred(), "without %s the viewer must not reach the description", descriptionPolicyName)
			Expect(resp.GetCharacter().GetDescription()).To(BeEmpty())
			Expect(status.Code(err)).To(Equal(codes.NotFound))

			// And the subject engine, on the identical fixture, does carry it —
			// so the denial above is specific to the removed policy rather than an
			// artifact of a broken control engine.
			ok, okErr := read(charID.String(), anonToken)
			Expect(okErr).NotTo(HaveOccurred())
			Expect(ok.GetCharacter().GetDescription()).To(Equal(charDesc))
		})
	})

	// Verifies: INV-PRIVACY-9
	Describe("an unreachable profile is indistinguishable from a character that does not exist (§8.7, §9.6.1)", func() {
		It("P3: the below-floor read and the no-such-row read are identical across status, message and marshaled body", func() {
			ctx := context.Background()

			// A targeted forbid on THIS character's profile resource only. The
			// engine is deny-overrides (combineDecisions), so it beats the seeded
			// reachability permit for exactly one id and leaves every other
			// profile reachable — which is what makes the second leg of the
			// differential (a reachable-but-absent id) meaningful rather than
			// trivially equal.
			belowFloor := newServer(newCorpusEngine(ctx, nil, &policystore.StoredPolicy{
				ID:      core.NewULID().String(),
				Name:    "test:forbid-one-profile",
				Effect:  types.PolicyEffectForbid,
				Source:  "admin",
				Enabled: true,
				DSLText: `forbid(principal is viewer, action in ["read"], resource == "profile:` + charID.String() + `");`,
			}))

			absentID := core.NewULID().String()

			unreachableResp, unreachableErr := belowFloor.GetCharacterProfile(ctx,
				&characteraccessv1.GetCharacterProfileRequest{CharacterId: charID.String()})
			absentResp, absentErr := belowFloor.GetCharacterProfile(ctx,
				&characteraccessv1.GetCharacterProfileRequest{CharacterId: absentID})

			Expect(unreachableErr).To(HaveOccurred())
			Expect(absentErr).To(HaveOccurred())

			Expect(status.Code(unreachableErr)).To(Equal(status.Code(absentErr)))
			Expect(status.Code(unreachableErr)).To(Equal(codes.NotFound))
			Expect(status.Convert(unreachableErr).Message()).To(Equal(status.Convert(absentErr).Message()))

			// The marshaled bodies, not the Go values: §9.6.1 mandates the
			// assertion be made over the wire.
			unreachableBody, mErr := proto.Marshal(unreachableResp)
			Expect(mErr).NotTo(HaveOccurred())
			absentBody, mErr := proto.Marshal(absentResp)
			Expect(mErr).NotTo(HaveOccurred())
			Expect(unreachableBody).To(Equal(absentBody))

			// No leak: the internal code never reaches the wire message.
			Expect(strings.Contains(status.Convert(unreachableErr).Message(), "CHARACTER_PROFILE_NOT_FOUND")).
				To(BeFalse(), "the wire message must not carry the internal code string")

			// Non-vacuity: a DIFFERENT, reachable, existing character on the SAME
			// engine must SUCCEED. Without this the byte equality above is
			// satisfiable by an engine that denies every profile — both legs would
			// then be NotFound with the identical shared literal, both responses
			// nil, and proto.Marshal(nil) empty for both, so the spec would go
			// green with the property under test never exercised.
			okResp, okErr := belowFloor.GetCharacterProfile(ctx,
				&characteraccessv1.GetCharacterProfileRequest{CharacterId: otherCharID.String()})
			Expect(okErr).NotTo(HaveOccurred(),
				"the appended forbid names ONE profile; every other profile must stay reachable on this same engine")
			Expect(okResp.GetCharacter().GetName()).To(Equal(otherCharName))
		})
	})

	Describe("the empty profile still resolves (§7.5) and v0.13 emits zero images (§9.7)", func() {
		It("P4: a character with ZERO profile rows returns its name rather than a not-found", func() {
			ctx := context.Background()

			// Non-vacuity: prove the fixture is genuinely degenerate. A character
			// that happened to own rows would satisfy the assertion just as well.
			var propCount int
			Expect(env.pool.QueryRow(ctx,
				`SELECT count(*) FROM entity_properties WHERE parent_id = $1`, charID.String()).
				Scan(&propCount)).To(Succeed())
			Expect(propCount).To(Equal(0), "the empty-profile fixture must own no property rows")

			resp, err := read(charID.String(), anonToken)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.GetCharacter().GetName()).To(Equal(charName))
		})

		It("P5: primary_image is absent and gallery is empty on every response", func() {
			resp, err := read(charID.String(), anonToken)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.GetCharacter().GetPrimaryImage()).To(BeNil())
			Expect(resp.GetCharacter().GetGallery()).To(BeEmpty())
		})
	})

	// The full-stack half of criterion 2. The unit tier
	// (internal/grpc/characteraccess_profile_test.go) drives the same sentinels
	// through the real seeded corpus with a doubled world reader; these specs
	// drive them through the real world.Service, the real property repository and
	// real rows in Postgres, so the guarantee is asserted against the enumeration
	// the production path actually makes.
	Describe("a field below the viewer's floor is absent from the marshaled bytes (§8.9, D-80)", func() {
		It("P7: the biography sentinel is absent for an anonymous viewer and present for a guest", func() {
			ctx := context.Background()
			insertProperty(ctx, charID, "profile.biography", integrationBiographySentinel, "public")

			anonResp, err := read(charID.String(), anonToken)
			Expect(err).NotTo(HaveOccurred(),
				"a below-floor field withholds the FIELD, never the whole profile")
			anonBody, mErr := proto.Marshal(anonResp)
			Expect(mErr).NotTo(HaveOccurred())
			Expect(strings.Contains(string(anonBody), integrationBiographySentinel)).To(BeFalse(),
				"profile.biography sits at the GUEST floor; its bytes must not appear anywhere in an anonymous response")

			// Paired positive control on the identical row through the identical
			// path: without it the assertion above cannot tell "the floor worked"
			// from "the INSERT never landed".
			guestResp, guestErr := read(charID.String(), guestToken)
			Expect(guestErr).NotTo(HaveOccurred())
			guestBody, mErr := proto.Marshal(guestResp)
			Expect(mErr).NotTo(HaveOccurred())
			Expect(strings.Contains(string(guestBody), integrationBiographySentinel)).To(BeTrue(),
				"the guest rung clears the biography floor, so the seeded sentinel must reach the wire")
		})

		It("P8: a system-visibility row is denied at every rung, while the same name at public publishes", func() {
			ctx := context.Background()

			// `system` is the fifth value in entity_properties' visibility CHECK
			// vocabulary and appears in no §8.6 row. No term-B policy matches it,
			// so the default-deny engine withholds it without anything having to
			// enumerate it.
			insertProperty(ctx, charID, "profile.pronouns", "they/them", "system")

			for _, token := range []string{anonToken, guestToken} {
				resp, err := read(charID.String(), token)
				Expect(err).NotTo(HaveOccurred())
				_, present := resp.GetCharacter().GetProfile()["profile.pronouns"]
				Expect(present).To(BeFalse(),
					"visibility=system matches no term-B permit at any rung")
			}

			// Paired positive control: the SAME name and value, differing only in
			// the visibility column.
			_, err := env.pool.Exec(ctx,
				`UPDATE entity_properties SET visibility = 'public' WHERE parent_id = $1 AND name = $2`,
				charID.String(), "profile.pronouns")
			Expect(err).NotTo(HaveOccurred())

			resp, err := read(charID.String(), anonToken)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.GetCharacter().GetProfile()["profile.pronouns"]).To(Equal("they/them"),
				"only the visibility column changed, so the denial above was that column and nothing else")
		})
	})

	Describe("the public path resolves its own viewer rung from the session token (D-83)", func() {
		It("P6: a guest token reaches a profile the anonymous rung cannot — the rung resolveAndGate would have refused outright", func() {
			ctx := context.Background()

			// Reachability narrowed to guest and player. The anonymous rung is
			// now BELOW the floor, so the two legs below differ only in whether
			// the facade turned the token into a guest principal.
			guestOnly := newServer(newCorpusEngine(ctx, []string{reachabilityPolicyName},
				&policystore.StoredPolicy{
					ID:      core.NewULID().String(),
					Name:    "test:profile-reachable-guest-and-player",
					Effect:  types.PolicyEffectPermit,
					Source:  "admin",
					Enabled: true,
					DSLText: `permit(principal is viewer, action in ["read"], resource is profile) when { principal.viewer.tier in ["guest", "player"] };`,
				}))

			_, anonErr := guestOnly.GetCharacterProfile(ctx,
				&characteraccessv1.GetCharacterProfileRequest{CharacterId: charID.String()})
			Expect(anonErr).To(HaveOccurred(), "the anonymous rung is below this floor")
			Expect(status.Code(anonErr)).To(Equal(codes.NotFound))

			resp, guestErr := guestOnly.GetCharacterProfile(ctx,
				&characteraccessv1.GetCharacterProfileRequest{
					CharacterId:        charID.String(),
					PlayerSessionToken: guestToken,
				})
			Expect(guestErr).NotTo(HaveOccurred(),
				"a GUEST player's session token must resolve the guest rung — playerGate.resolveAndGate would have returned PermissionDenied here (INV-SCENE-64)")
			Expect(resp.GetCharacter().GetName()).To(Equal(charName))
		})

		It("P9: a viewer exactly at the reachability floor receives name AND pronouns; one rung below receives the not-found-equivalent", func() {
			ctx := context.Background()
			insertProperty(ctx, charID, "profile.pronouns", "they/them", "public")

			// The floor is RAISED to guest so a below-floor rung exists at all.
			// In the shipped corpus the floor is the bottom rung, so there is no
			// viewer below it — narrowing the corpus is the only way to drive the
			// boundary, and it is also what a game raising its floor would do.
			guestFloor := newServer(newCorpusEngine(ctx, []string{reachabilityPolicyName},
				&policystore.StoredPolicy{
					ID:      core.NewULID().String(),
					Name:    "test:profile-reachable-floor-at-guest",
					Effect:  types.PolicyEffectPermit,
					Source:  "admin",
					Enabled: true,
					DSLText: `permit(principal is viewer, action in ["read"], resource is profile) when { principal.viewer.tier in ["guest", "player"] };`,
				}))

			atFloor, err := guestFloor.GetCharacterProfile(ctx,
				&characteraccessv1.GetCharacterProfileRequest{
					CharacterId:        charID.String(),
					PlayerSessionToken: guestToken,
				})
			Expect(err).NotTo(HaveOccurred())
			Expect(atFloor.GetCharacter().GetName()).To(Equal(charName),
				"§8.8's minimum public identity is name…")
			Expect(atFloor.GetCharacter().GetProfile()["profile.pronouns"]).To(Equal("they/them"),
				"…and pronouns, both delivered to a viewer standing exactly at the floor")

			belowResp, belowErr := guestFloor.GetCharacterProfile(ctx,
				&characteraccessv1.GetCharacterProfileRequest{CharacterId: charID.String()})
			Expect(belowErr).To(HaveOccurred())
			Expect(belowResp).To(BeNil())
			Expect(status.Code(belowErr)).To(Equal(codes.NotFound))

			// The not-found-EQUIVALENCE is asserted as a differential against a
			// character id naming no row, rather than against a literal copied
			// out of the facade. A literal would still pass if both branches
			// drifted together; the differential is the property §8.7 states.
			_, absentErr := guestFloor.GetCharacterProfile(ctx,
				&characteraccessv1.GetCharacterProfileRequest{CharacterId: core.NewULID().String()})
			Expect(absentErr).To(HaveOccurred())
			Expect(status.Convert(belowErr).Message()).To(Equal(status.Convert(absentErr).Message()),
				"one rung below the floor is the UNIFORM not-found-equivalent, not a distinguishable denial")
		})
	})
})
