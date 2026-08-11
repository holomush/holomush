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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/access/policy"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
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

// directoryFloorPolicyName is 01-SPEC §9.2's tier floor on the directory
// resource — the viewer twin this plan seeds. The gate spec below removes it
// from a control corpus, which is the only way to stand a viewer below a floor
// that clears all three rungs as shipped.
const directoryFloorPolicyName = "seed:viewer-directory-list-characters"

var _ = Describe("PROFILE-03/PROFILE-04: the public character directory", func() {
	const guestToken = "guest-directory-list-token"

	var (
		adaID     ulid.ULID
		adaName   string
		bramID    ulid.ULID
		bramName  string
		locID     ulid.ULID
		psRepo    auth.PlayerSessionRepository
		playerRep auth.PlayerRepository
		srv       *holoGRPC.CharacterAccessServer
	)

	// newServer builds the facade over an arbitrary policy corpus, with the
	// harness's real repositories underneath. Every spec drives the SAME
	// constructor, so a control differs from the subject only in its corpus —
	// and the SAME engine value is both the §9.2 gate's evaluator and the
	// profilevis evaluator's, exactly as cmd/holomush wires it.
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
			engine,
			setup.NewCharRepoAdapter(env.pool, env.charRepo),
			psRepo,
			playerRep,
			setup.NewCharRepoAdapter(env.pool, env.charRepo),
		)
	}

	// newCorpusEngine builds a second engine over the SAME real provider stack
	// but a DIFFERENT corpus, and pins that the exclusion actually matched — a
	// control whose policy name stopped matching anything is a copy of the full
	// corpus that still LOOKS like a control.
	newCorpusEngine := func(ctx context.Context, excluded []string, appended ...*policystore.StoredPolicy) *policy.Engine {
		exSet := make(map[string]bool, len(excluded))
		for _, name := range excluded {
			exSet[name] = true
		}
		corpus := &profileCorpusStore{PolicyStore: env.pStore, excluded: exSet, appended: appended}
		cache := policy.NewCache(corpus, env.compiler)
		Expect(cache.Reload(ctx)).To(Succeed())
		Expect(corpus.removed).To(Equal(len(excluded)),
			"the control corpus must exclude exactly %d policies (%v) — a 0 here means the name no longer matches anything seeded and the control is disarmed",
			len(excluded), excluded)
		return policy.NewEngine(env.resolver, cache, &noopSessionResolver{}, env.auditLogger)
	}

	list := func(server *holoGRPC.CharacterAccessServer, token string) (*characteraccessv1.ListCharacterDirectoryResponse, error) {
		return server.ListCharacterDirectory(context.Background(), &characteraccessv1.ListCharacterDirectoryRequest{
			PlayerSessionToken: token,
		})
	}

	names := func(resp *characteraccessv1.ListCharacterDirectoryResponse) []string {
		out := make([]string, 0, len(resp.GetCharacters()))
		for _, row := range resp.GetCharacters() {
			out = append(out, row.GetName())
		}
		return out
	}

	insertCharacter := func(ctx context.Context, id ulid.ULID, ownerID ulid.ULID, name, description string) {
		_, err := env.pool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, description, location_id, normalized_name, name_skeleton, name_skeleton_unicode_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			append([]any{id.String(), ownerID.String(), name, description, locID.String()},
				chartest.Columns(name)...)...)
		Expect(err).NotTo(HaveOccurred())
	}

	BeforeEach(func() {
		ctx := context.Background()

		// Cleanup order is seed_policies_test.go's, for its documented reasons.
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
			VALUES ($1, 'Directory Hall', 'A hall.', 'persistent', 'last:0')`,
			locID.String())
		Expect(err).NotTo(HaveOccurred())

		// The characters are PLACED IN A LOCATION deliberately. Nothing in the
		// directory path reads a property row, so the colocation gate on
		// seed:property-public-read cannot bite here — but the fixture matches
		// the shape every other character spec in this suite uses, so a spec
		// added later that DOES read a profile does not inherit 04-05's trap.
		ownerID := core.NewULID()
		Expect(playerRep.Create(ctx, &auth.Player{
			ID:           ownerID,
			Username:     "owner_" + ownerID.String()[20:],
			PasswordHash: "hash",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		})).To(Succeed())

		adaID = core.NewULID()
		adaName = uniqueCharFixtureName("Ada", adaID)
		insertCharacter(ctx, adaID, ownerID, adaName, "A tinkerer with ink-stained cuffs.")

		bramID = core.NewULID()
		bramName = uniqueCharFixtureName("Bram", bramID)
		insertCharacter(ctx, bramID, ownerID, bramName, "A quiet figure by the door.")

		guestID := core.NewULID()
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

	Describe("a logged-out visitor enumerates the directory against the shipped corpus", func() {
		It("D1: lists every reachable character as id and name", func() {
			resp, err := list(srv, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(names(resp)).To(ConsistOf(adaName, bramName))

			byID := map[string]string{}
			for _, row := range resp.GetCharacters() {
				byID[row.GetId()] = row.GetName()
			}
			Expect(byID).To(HaveKeyWithValue(adaID.String(), adaName))
			Expect(byID).To(HaveKeyWithValue(bramID.String(), bramName))
		})

		It("D2: publishes IDENTITY ONLY — the in-world description never reaches the marshaled bytes", func() {
			resp, err := list(srv, "")
			Expect(err).NotTo(HaveOccurred())

			raw, err := proto.Marshal(resp)
			Expect(err).NotTo(HaveOccurred())

			// The paired positive control comes first: without it, an absence
			// assertion over an empty response is vacuously true.
			Expect(string(raw)).To(ContainSubstring(adaName),
				"the control: the NAME is present, so the bytes under test are a populated listing")
			Expect(string(raw)).NotTo(ContainSubstring("ink-stained"),
				"the summary carries no description — the field does not exist on the message")
			Expect(string(raw)).NotTo(ContainSubstring("Directory Hall"),
				"the summary carries no location — presence telemetry has nowhere to land")
		})

		It("D3: is deterministic — two identical calls marshal to identical bytes, ordered by id", func() {
			first, err := list(srv, "")
			Expect(err).NotTo(HaveOccurred())
			second, err := list(srv, "")
			Expect(err).NotTo(HaveOccurred())

			a, err := proto.Marshal(first)
			Expect(err).NotTo(HaveOccurred())
			b, err := proto.Marshal(second)
			Expect(err).NotTo(HaveOccurred())
			Expect(a).To(Equal(b))

			Expect(first.GetCharacters()).To(HaveLen(2))
			Expect(first.GetCharacters()[0].GetId() < first.GetCharacters()[1].GetId()).To(BeTrue(),
				"the listing is sorted by id rather than forwarded in repository order")
		})

		It("D4: returns an empty list and a success for an empty directory, never a not-found", func() {
			_, err := env.pool.Exec(context.Background(), "DELETE FROM characters")
			Expect(err).NotTo(HaveOccurred())

			resp, err := list(srv, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.GetCharacters()).To(BeEmpty())
		})
	})

	Describe("the §8.7 membership rule filters by profile reachability", func() {
		It("D5: an unreachable character is absent, and the response equals one for a corpus without it", func() {
			ctx := context.Background()

			// Raise the reachability floor to guest+player so the anonymous rung
			// genuinely falls below it. Nothing else about the corpus changes,
			// and in particular the DIRECTORY floor still clears every rung — so
			// the anonymous caller passes the gate and is filtered by the
			// membership rule alone.
			raised := newCorpusEngine(ctx, []string{reachabilityPolicyName}, &policystore.StoredPolicy{
				Name:    reachabilityPolicyName,
				DSLText: `permit(principal is viewer, action in ["read"], resource is profile) when { principal.viewer.tier in ["guest", "player"] };`,
				Enabled: true,
			})
			server := newServer(raised)

			withheld, err := list(server, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(withheld.GetCharacters()).To(BeEmpty(),
				"a character whose profile the viewer cannot reach is simply absent")

			// The absence is indistinguishable from nonexistence: the SAME
			// response the same viewer gets when no character row exists at all.
			_, err = env.pool.Exec(ctx, "DELETE FROM characters")
			Expect(err).NotTo(HaveOccurred())
			absent, err := list(server, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(proto.Equal(withheld, absent)).To(BeTrue(),
				"a withheld character and a nonexistent one MUST produce the same response")
		})

		It("D6: the SAME characters are listed for a viewer at a clearing rung", func() {
			ctx := context.Background()

			raised := newCorpusEngine(ctx, []string{reachabilityPolicyName}, &policystore.StoredPolicy{
				Name:    reachabilityPolicyName,
				DSLText: `permit(principal is viewer, action in ["read"], resource is profile) when { principal.viewer.tier in ["guest", "player"] };`,
				Enabled: true,
			})
			server := newServer(raised)

			resp, err := list(server, guestToken)
			Expect(err).NotTo(HaveOccurred())
			Expect(names(resp)).To(ConsistOf(adaName, bramName),
				"the previous spec's emptiness is the FLOOR, not an empty fixture")
		})
	})

	Describe("the §9.2 gate is one decision on the directory resource", func() {
		It("D7: a viewer below the directory floor is denied the whole call", func() {
			ctx := context.Background()

			// Remove ONLY the directory floor. seed:profile-reachable is
			// untouched, so every character remains reachable — which is exactly
			// what makes this a test of the gate rather than of the filter.
			closed := newCorpusEngine(ctx, []string{directoryFloorPolicyName})
			server := newServer(closed)

			resp, err := list(server, "")
			Expect(err).To(HaveOccurred())
			Expect(resp).To(BeNil())
			Expect(status.Code(err)).To(Equal(codes.PermissionDenied))
		})

		It("D8: the same viewer with the floor restored lists everything — the two decisions are separable", func() {
			// The paired positive control for D7, and the separability claim in
			// one spec: with the directory floor in place and reachability
			// unchanged, the identical call succeeds. So the gate, not
			// reachability, is what closed the directory above.
			resp, err := list(srv, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(names(resp)).To(ConsistOf(adaName, bramName))
		})
	})
})
