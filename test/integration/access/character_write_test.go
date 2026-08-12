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
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/holomush/holomush/internal/access/profilevis"
	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/core"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/testsupport/chartest"
	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// The owner's two edit surfaces, end to end.
//
// # Why the boundary cases live HERE rather than at the unit tier
//
// The description's cap, its UTF-8 rule and its control-character rule are
// enforced by the DOMAIN (world.Service.UpdateCharacterDescription ->
// Character.SetDescription -> ValidateDescription), which is a gap this plan
// closed: at HEAD the command assigned the field directly with no validator
// anywhere between the authorization check and the SQL, so an over-cap
// description reached the column (issue #4954). A unit test against a domain
// DOUBLE would prove only that the facade converts an error the double was told
// to return. These specs run against the REAL world.Service over the REAL
// repositories, so the over-cap assertion below — that the `characters` row is
// UNCHANGED — is the one that would have caught #4954.
//
// # Why the character is placed in a location
//
// The placement is fixture realism, NOT a correctness requirement of the
// readback. An earlier draft of this comment claimed a location-less character
// could not read back its own `public` rows because `seed:property-public-read`
// is colocation-gated. That claim is FALSE against the shipped corpus and is
// corrected here so nobody widens a policy to fix a defect that does not exist:
// `seed:profile-public-read-property` (internal/access/policy/seed.go:888-893)
// permits `read` on any row with `visibility == "public" && parent_type ==
// "character"` with NO location clause at all, so the colocation-gated permit is
// not the only one in play. seed_policies_test.go's S3 asserts exactly the
// off-location case ALLOW, and Phase 2's own D-10/D-11 note calls the colocation
// restriction "the anomaly" that the PROFILE-11 widening deliberately reversed.
//
// These assertions are therefore non-vacuous regardless of placement; the
// location is kept because a character on the grid is the realistic shape.
var _ = Describe("IDENT-02/IDENT-02a: the owner edits prose profile fields and the in-world description", func() {
	const ownerToken = "character-write-owner-session-token"

	var (
		ownerID  ulid.ULID
		charID   ulid.ULID
		charName string
		locID    ulid.ULID
		srv      *holoGRPC.CharacterAccessServer
	)

	// newWriteServer builds the facade over a WRITE-CAPABLE world.Service. The
	// suite's shared env.worldSvc is read-only (no Transactor, no OutboxWriter),
	// so a mutation through it would fail with a configuration error rather than
	// exercising the same-transaction outbox seam these specs are here to prove.
	newWriteServer := func() *holoGRPC.CharacterAccessServer {
		worldSvc := world.NewService(world.ServiceConfig{
			CharacterRepo: env.charRepo,
			PropertyRepo:  env.propRepo,
			Engine:        env.engine,
			Transactor:    worldpg.NewTransactor(env.pool),
			OutboxWriter:  worldpg.NewOutboxStore(env.pool),
		})
		return holoGRPC.NewCharacterAccessServer(
			worldSvc,
			worldSvc,
			&profilevis.Evaluator{Engine: env.engine},
			env.engine,
			setup.NewCharRepoAdapter(env.pool, env.charRepo),
			store.NewPostgresPlayerSessionStore(env.pool),
			authpg.NewPlayerRepository(env.pool),
			setup.NewCharRepoAdapter(env.pool, env.charRepo),
			// The two edit surfaces below seat nothing, so the create pipeline
			// fails the spec if it is ever reached.
			failOnCallCreator{},
		)
	}

	storedDescription := func() string {
		var got string
		Expect(env.pool.QueryRow(context.Background(),
			`SELECT description FROM characters WHERE id = $1`, charID.String()).Scan(&got)).To(Succeed())
		return got
	}

	storedVersion := func() int32 {
		var got int32
		Expect(env.pool.QueryRow(context.Background(),
			`SELECT version FROM characters WHERE id = $1`, charID.String()).Scan(&got)).To(Succeed())
		return got
	}

	outboxCount := func() int {
		var got int
		Expect(env.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM outbox WHERE aggregate_id = $1`, charID.String()).Scan(&got)).To(Succeed())
		return got
	}

	storedProperty := func(name string) (string, bool) {
		var got string
		err := env.pool.QueryRow(context.Background(),
			`SELECT coalesce(value, '') FROM entity_properties
			 WHERE parent_type = 'character' AND parent_id = $1 AND name = $2`,
			charID.String(), name).Scan(&got)
		if err != nil {
			return "", false
		}
		return got, true
	}

	describe := func(description string, version int32) (*characteraccessv1.UpdateCharacterDescriptionResponse, error) {
		return srv.UpdateCharacterDescription(context.Background(), &characteraccessv1.UpdateCharacterDescriptionRequest{
			CharacterId:        charID.String(),
			PlayerSessionToken: ownerToken,
			ExpectedVersion:    version,
			Description:        description,
		})
	}

	BeforeEach(func() {
		ctx := context.Background()

		// Cleanup order is seed_policies_test.go's, for its documented reasons.
		// `outbox` leads because these specs assert exact envelope counts and a
		// row left by a previous spec would inflate them; it references nothing.
		for _, table := range []string{
			"outbox",
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

		playerRep := authpg.NewPlayerRepository(env.pool)
		psRepo := store.NewPostgresPlayerSessionStore(env.pool)

		locID = core.NewULID()
		_, err := env.pool.Exec(ctx, `
			INSERT INTO locations (id, name, description, type, replay_policy)
			VALUES ($1, 'Edit Hall', 'A hall.', 'persistent', 'last:0')`,
			locID.String())
		Expect(err).NotTo(HaveOccurred())

		ownerID = core.NewULID()
		Expect(playerRep.Create(ctx, &auth.Player{
			ID:           ownerID,
			Username:     "owner_" + ownerID.String()[20:],
			PasswordHash: "hash",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		})).To(Succeed())

		sess, err := auth.NewPlayerSession(ownerID, auth.HashSessionToken(ownerToken), "", "", auth.PlayerSessionTTL)
		Expect(err).NotTo(HaveOccurred())
		Expect(psRepo.Create(ctx, sess)).To(Succeed())

		charID = core.NewULID()
		charName = uniqueCharFixtureName("Editor", charID)
		_, err = env.pool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, description, location_id, normalized_name, name_skeleton, name_skeleton_unicode_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			append([]any{charID.String(), ownerID.String(), charName, "The original description.", locID.String()},
				chartest.Columns(charName)...)...)
		Expect(err).NotTo(HaveOccurred())

		srv = newWriteServer()
		env.auditWriter.Reset()
	})

	Describe("the in-world description edit (IDENT-02a)", func() {
		It("W1: an owner's edit changes the characters row and commits exactly one envelope", func() {
			before := storedVersion()

			resp, err := describe("A tall figure wrapped in a travelling cloak.", before)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.GetCharacter().GetDescription()).To(Equal("A tall figure wrapped in a travelling cloak."))

			Expect(storedDescription()).To(Equal("A tall figure wrapped in a travelling cloak."))
			Expect(storedVersion()).To(BeNumerically(">", before),
				"the guarded write bumps the aggregate's concurrency token")
			Expect(outboxCount()).To(Equal(1),
				"the state change and its world-change envelope commit in the SAME transaction")
		})

		It("W2: a description at exactly the cap is accepted and the row changes", func() {
			atCap := strings.Repeat("a", world.MaxDescriptionLength)

			_, err := describe(atCap, storedVersion())
			Expect(err).NotTo(HaveOccurred())
			Expect(storedDescription()).To(Equal(atCap))
		})

		// This is the assertion that would have caught issue #4954. Before this
		// plan the domain command wrote the value verbatim, so the row DID change.
		It("W3: a description one byte past the cap is refused and the row is UNCHANGED", func() {
			original := storedDescription()
			overCap := strings.Repeat("a", world.MaxDescriptionLength+1)

			_, err := describe(overCap, storedVersion())
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))

			Expect(storedDescription()).To(Equal(original),
				"an over-cap description MUST NOT reach the column")
			Expect(outboxCount()).To(Equal(0), "a refused write emits no envelope")
		})

		It("W4: invalid UTF-8 and a control character other than newline/CR/tab are refused, and the row is unchanged", func() {
			original := storedDescription()

			for _, bad := range []string{
				string([]byte{0xff, 0xfe, 0x41}),
				// NOT "\r": hasControlCharsExceptWhitespace permits carriage
				// return alongside newline and tab, so a "\r" fixture would be
				// permanently RED against correct code.
				"\x1b[31mdanger",
				"ring\x07ring",
			} {
				_, err := describe(bad, storedVersion())
				Expect(err).To(HaveOccurred(), "value %q must be refused", bad)
				Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
				Expect(storedDescription()).To(Equal(original))
			}
			Expect(outboxCount()).To(Equal(0))
		})

		It("W5: newline, carriage return and tab are legal prose and reach the column", func() {
			multiline := "First paragraph.\r\n\r\nSecond paragraph.\tIndented."

			_, err := describe(multiline, storedVersion())
			Expect(err).NotTo(HaveOccurred())
			Expect(storedDescription()).To(Equal(multiline))
		})

		It("W6: an empty description is accepted, and the public profile then OMITS the field rather than emitting an empty one", func() {
			_, err := describe("", storedVersion())
			Expect(err).NotTo(HaveOccurred())
			Expect(storedDescription()).To(BeEmpty())

			public, readErr := srv.GetCharacterProfile(context.Background(),
				&characteraccessv1.GetCharacterProfileRequest{CharacterId: charID.String()})
			Expect(readErr).NotTo(HaveOccurred())
			Expect(public.GetCharacter().GetDescription()).To(BeEmpty(),
				"§7.5: a blank field and a withheld field are indistinguishable")
			Expect(public.GetCharacter().GetName()).To(Equal(charName),
				"and the rest of the profile still resolves — the omission is the description alone")
		})

		It("W7: a stale expected_version is refused with Aborted and the row is unchanged", func() {
			original := storedDescription()

			_, err := describe("a rewrite from a stale form", storedVersion()+99)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.Aborted))
			Expect(storedDescription()).To(Equal(original))
		})

		It("W8: an absent or zero expected_version is refused with InvalidArgument and the row is unchanged", func() {
			original := storedDescription()

			_, err := describe("an unguarded rewrite", 0)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(storedDescription()).To(Equal(original))
		})
	})

	Describe("the prose profile edit (IDENT-02)", func() {
		edit := func(version int32, mutate func(*characteraccessv1.UpdateCharacterProfileRequest), paths ...string) (*characteraccessv1.UpdateCharacterProfileResponse, error) {
			req := &characteraccessv1.UpdateCharacterProfileRequest{
				CharacterId:        charID.String(),
				PlayerSessionToken: ownerToken,
				ExpectedVersion:    version,
				UpdateMask:         &fieldmaskpb.FieldMask{Paths: paths},
			}
			mutate(req)
			return srv.UpdateCharacterProfile(context.Background(), req)
		}

		It("W9: a two-path edit writes both rows and commits exactly one envelope", func() {
			resp, err := edit(storedVersion(), func(r *characteraccessv1.UpdateCharacterProfileRequest) {
				r.Pronouns = "they/them"
				r.Biography = "Raised in the archive stacks."
				// Outside the mask: immaterial, and MUST NOT be written.
				r.Species = "this must never be stored"
			}, "profile.pronouns", "profile.biography")
			Expect(err).NotTo(HaveOccurred())

			pronouns, ok := storedProperty("profile.pronouns")
			Expect(ok).To(BeTrue())
			Expect(pronouns).To(Equal("they/them"))

			bio, ok := storedProperty("profile.biography")
			Expect(ok).To(BeTrue())
			Expect(bio).To(Equal("Raised in the archive stacks."))

			_, speciesPresent := storedProperty("profile.species")
			Expect(speciesPresent).To(BeFalse(), "a field outside the mask is never written")

			Expect(outboxCount()).To(Equal(1),
				"the rows and their one character_profile_update envelope commit together")

			// The colocated owner reads its own public rows straight back.
			Expect(resp.GetCharacter().GetProfile()).To(HaveKeyWithValue("profile.pronouns", "they/them"))
		})

		It("W10: an over-cap prose value is refused server-side and no row is written", func() {
			_, err := edit(storedVersion(), func(r *characteraccessv1.UpdateCharacterProfileRequest) {
				r.Pronouns = strings.Repeat("a", world.MaxNameLength+1)
			}, "profile.pronouns")
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))

			_, present := storedProperty("profile.pronouns")
			Expect(present).To(BeFalse())
			Expect(outboxCount()).To(Equal(0))
		})

		It("W11: an update_mask path outside the allowlist is refused and no row is written", func() {
			_, err := edit(storedVersion(), func(r *characteraccessv1.UpdateCharacterProfileRequest) {
				r.Pronouns = "they/them"
			}, "profile")
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))

			_, present := storedProperty("profile.pronouns")
			Expect(present).To(BeFalse(), "`profile` MUST NOT expand into its members (§9.5 rule 2)")
		})

		It("W12: the empty string CLEARS a row rather than storing an empty value", func() {
			_, err := edit(storedVersion(), func(r *characteraccessv1.UpdateCharacterProfileRequest) {
				r.Pronouns = "they/them"
			}, "profile.pronouns")
			Expect(err).NotTo(HaveOccurred())
			_, present := storedProperty("profile.pronouns")
			Expect(present).To(BeTrue())

			_, err = edit(storedVersion(), func(r *characteraccessv1.UpdateCharacterProfileRequest) {
				r.Pronouns = ""
			}, "profile.pronouns")
			Expect(err).NotTo(HaveOccurred())

			_, stillPresent := storedProperty("profile.pronouns")
			Expect(stillPresent).To(BeFalse(), "an erasure removes the row; it does not blank it")
		})

		// The PROFILE path's guard is a genuine CAS: the caller's version is
		// threaded into the domain command's version-predicated UPDATE. There is
		// deliberately NO equivalent spec for the description RPC — its guard is a
		// TOCTOU narrowing (issue #4956) and this property is FALSE there, so
		// asserting it would either flake or pass for the wrong reason.
		//
		// The two writers are issued SEQUENTIALLY and both carry the SAME
		// expected_version, which is exactly the shape the CAS decides: the first
		// commit moves the row's version, so the second's predicate no longer
		// matches. Sequencing makes the outcome deterministic rather than racing
		// two goroutines for the same guarantee.
		It("W13: of two writers carrying the same expected_version, exactly one succeeds and exactly one is Aborted", func() {
			shared := storedVersion()

			_, firstErr := edit(shared, func(r *characteraccessv1.UpdateCharacterProfileRequest) {
				r.Concept = "the first writer's concept"
			}, "profile.concept")

			_, secondErr := edit(shared, func(r *characteraccessv1.UpdateCharacterProfileRequest) {
				r.Concept = "the second writer's concept"
			}, "profile.concept")

			Expect(firstErr).NotTo(HaveOccurred(), "exactly one writer succeeds")
			Expect(secondErr).To(HaveOccurred(), "and exactly one loses")
			Expect(status.Code(secondErr)).To(Equal(codes.Aborted),
				"the loser is SURFACED as a lost update, not silently resolved and not retried")

			concept, ok := storedProperty("profile.concept")
			Expect(ok).To(BeTrue())
			Expect(concept).To(Equal("the first writer's concept"),
				"the loser's value never reached the row — this is a lost-update REFUSAL, not last-write-wins")
			Expect(outboxCount()).To(Equal(1), "the refused write emitted no envelope")
		})
	})
})

// The default-character write, end to end (IDENT-05).
//
// # Why these specs run against real Postgres rather than a repository double
//
// The property under test is that ONE column moved and no other did. A double
// can only report the call it was told to expect, so it proves the handler
// reached a method — not that the SQL behind it is narrow. The trap this guards
// (RESEARCH pitfall 3) is the obvious alternative implementation, GetByID +
// PlayerRepository.Update, which issues a full-row UPDATE rewriting
// password_hash, email, failed_attempts and locked_until from a struct read
// moments earlier, with no version guard. Against a mock that implementation
// passes; against these specs the before/after column comparison fails.
var _ = Describe("IDENT-05: the player sets which of their characters is the default", func() {
	const (
		ownerToken = "set-default-owner-session-token"
		guestToken = "set-default-guest-session-token"
	)

	var (
		ownerID   ulid.ULID
		activeID  ulid.ULID
		secondID  ulid.ULID
		retiredID ulid.ULID
		locID     ulid.ULID
		srv       *holoGRPC.CharacterAccessServer
	)

	newServer := func() *holoGRPC.CharacterAccessServer {
		worldSvc := world.NewService(world.ServiceConfig{
			CharacterRepo: env.charRepo,
			PropertyRepo:  env.propRepo,
			Engine:        env.engine,
			Transactor:    worldpg.NewTransactor(env.pool),
			OutboxWriter:  worldpg.NewOutboxStore(env.pool),
		})
		return holoGRPC.NewCharacterAccessServer(
			worldSvc,
			worldSvc,
			&profilevis.Evaluator{Engine: env.engine},
			env.engine,
			setup.NewCharRepoAdapter(env.pool, env.charRepo),
			store.NewPostgresPlayerSessionStore(env.pool),
			authpg.NewPlayerRepository(env.pool),
			setup.NewCharRepoAdapter(env.pool, env.charRepo),
			// The two edit surfaces below seat nothing, so the create pipeline
			// fails the spec if it is ever reached.
			failOnCallCreator{},
		)
	}

	// storedDefault reads the column under test directly. A read through
	// PlayerRepository.GetByID would be answered by the same code path the write
	// went through, so a shared misinterpretation of the column would agree with
	// itself; raw SQL cannot.
	storedDefault := func() string {
		var got *string
		Expect(env.pool.QueryRow(context.Background(),
			`SELECT default_character_id FROM players WHERE id = $1`, ownerID.String()).Scan(&got)).To(Succeed())
		if got == nil {
			return ""
		}
		return *got
	}

	// playerCredentialColumns is every column the narrow UPDATE must leave alone.
	// `locked_until` is nullable, hence the coalesce into a comparable scalar.
	type credentials struct {
		passwordHash   string
		email          *string
		failedAttempts int
		lockedUntil    int64
	}
	storedCredentials := func() credentials {
		var got credentials
		Expect(env.pool.QueryRow(context.Background(),
			`SELECT password_hash, email, failed_attempts, coalesce(locked_until, 0)
			 FROM players WHERE id = $1`, ownerID.String()).
			Scan(&got.passwordHash, &got.email, &got.failedAttempts, &got.lockedUntil)).To(Succeed())
		return got
	}

	setDefault := func(token string, characterID ulid.ULID) (*characteraccessv1.SetDefaultCharacterResponse, error) {
		return srv.SetDefaultCharacter(context.Background(), &characteraccessv1.SetDefaultCharacterRequest{
			CharacterId:        characterID.String(),
			PlayerSessionToken: token,
		})
	}

	insertCharacter := func(ctx context.Context, id ulid.ULID, playerID ulid.ULID, base string, status string) {
		name := uniqueCharFixtureName(base, id)
		_, err := env.pool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, description, location_id, status, normalized_name, name_skeleton, name_skeleton_unicode_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			append([]any{id.String(), playerID.String(), name, "A character.", locID.String(), status},
				chartest.Columns(name)...)...)
		Expect(err).NotTo(HaveOccurred())
	}

	BeforeEach(func() {
		ctx := context.Background()

		for _, table := range []string{
			"outbox",
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

		playerRep := authpg.NewPlayerRepository(env.pool)
		psRepo := store.NewPostgresPlayerSessionStore(env.pool)

		locID = core.NewULID()
		_, err := env.pool.Exec(ctx, `
			INSERT INTO locations (id, name, description, type, replay_policy)
			VALUES ($1, 'Default Hall', 'A hall.', 'persistent', 'last:0')`,
			locID.String())
		Expect(err).NotTo(HaveOccurred())

		ownerID = core.NewULID()
		email := "owner_" + ownerID.String()[20:] + "@example.test"
		Expect(playerRep.Create(ctx, &auth.Player{
			ID:           ownerID,
			Username:     "owner_" + ownerID.String()[20:],
			PasswordHash: "the-owners-original-password-hash",
			Email:        &email,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		})).To(Succeed())

		sess, err := auth.NewPlayerSession(ownerID, auth.HashSessionToken(ownerToken), "", "", auth.PlayerSessionTTL)
		Expect(err).NotTo(HaveOccurred())
		Expect(psRepo.Create(ctx, sess)).To(Succeed())

		guestID := core.NewULID()
		Expect(playerRep.Create(ctx, &auth.Player{
			ID:           guestID,
			Username:     "guest_" + guestID.String()[20:],
			PasswordHash: "hash",
			IsGuest:      true,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		})).To(Succeed())
		guestSess, err := auth.NewPlayerSession(guestID, auth.HashSessionToken(guestToken), "", "", auth.PlayerSessionTTL)
		Expect(err).NotTo(HaveOccurred())
		Expect(psRepo.Create(ctx, guestSess)).To(Succeed())

		activeID = core.NewULID()
		secondID = core.NewULID()
		retiredID = core.NewULID()
		insertCharacter(ctx, activeID, ownerID, "Primary", string(world.StatusActive))
		insertCharacter(ctx, secondID, ownerID, "Secondary", string(world.StatusActive))
		insertCharacter(ctx, retiredID, ownerID, "Withdrawn", string(world.StatusRetired))

		srv = newServer()
		env.auditWriter.Reset()
	})

	It("D1: an owner's request moves players.default_character_id and answers with the whole roster", func() {
		Expect(storedDefault()).To(BeEmpty(), "the fixture starts with no default, which is the state IDENT-05 exists to leave")

		resp, err := setDefault(ownerToken, activeID)
		Expect(err).NotTo(HaveOccurred())

		Expect(storedDefault()).To(Equal(activeID.String()),
			"the column holds the character's ULID — this is the write nothing in the tree performed before")

		ids := make([]string, 0, len(resp.GetCharacters()))
		for _, c := range resp.GetCharacters() {
			ids = append(ids, c.GetId())
		}
		Expect(ids).To(ConsistOf(activeID.String(), secondID.String(), retiredID.String()),
			"the response is the caller's WHOLE roster, retired entries included (D-90)")
	})

	It("D2: setting the character that is already the default succeeds and leaves the column byte-identical", func() {
		_, err := setDefault(ownerToken, activeID)
		Expect(err).NotTo(HaveOccurred())
		first := storedDefault()

		resp, err := setDefault(ownerToken, activeID)
		Expect(err).NotTo(HaveOccurred(), "a repeat is idempotent, not a conflict")
		Expect(storedDefault()).To(Equal(first))
		Expect(resp.GetCharacters()).To(HaveLen(3), "and the full roster still comes back")
	})

	// The paired positive control (PORTAL-10 rule 2) for D1: the write is not a
	// constant that happens to agree with the first fixture's id.
	It("D3: the owner's OTHER character can also be made the default", func() {
		_, err := setDefault(ownerToken, activeID)
		Expect(err).NotTo(HaveOccurred())
		Expect(storedDefault()).To(Equal(activeID.String()))

		_, err = setDefault(ownerToken, secondID)
		Expect(err).NotTo(HaveOccurred())
		Expect(storedDefault()).To(Equal(secondID.String()),
			"the second request moved the pointer — a hardcoded write would still read the first id")
	})

	It("D4: the write leaves password_hash, email, failed_attempts and locked_until untouched", func() {
		before := storedCredentials()

		_, err := setDefault(ownerToken, activeID)
		Expect(err).NotTo(HaveOccurred())

		after := storedCredentials()
		Expect(after.passwordHash).To(Equal(before.passwordHash),
			"a full-row UPDATE would rewrite the hash from a stale read (RESEARCH pitfall 3)")
		Expect(after.email).To(Equal(before.email))
		Expect(after.failedAttempts).To(Equal(before.failedAttempts))
		Expect(after.lockedUntil).To(Equal(before.lockedUntil))
	})

	It("D5: a RETIRED owned character is refused with FailedPrecondition and the column does not move", func() {
		_, err := setDefault(ownerToken, activeID)
		Expect(err).NotTo(HaveOccurred())
		before := storedDefault()

		_, err = setDefault(ownerToken, retiredID)
		Expect(err).To(HaveOccurred())
		Expect(status.Code(err)).To(Equal(codes.FailedPrecondition),
			"the retire path clears this column in the same transaction as the status write (Q4); permitting the set would reconstruct exactly the state that code prevents")
		Expect(storedDefault()).To(Equal(before))
	})

	It("D6: a guest is denied, and the same fixture's non-guest owner is permitted", func() {
		_, err := setDefault(guestToken, activeID)
		Expect(err).To(HaveOccurred())
		Expect(status.Code(err)).To(Equal(codes.PermissionDenied))
		Expect(status.Convert(err).Message()).To(Equal("guests do not have characters of their own"),
			"the character facade's OWN guest message, not the scene facade's")

		_, err = setDefault(ownerToken, activeID)
		Expect(err).NotTo(HaveOccurred(), "the paired positive control: the same fixture permits a non-guest owner")
		Expect(storedDefault()).To(Equal(activeID.String()))
	})
})
