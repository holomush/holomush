// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package access_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/access/policy"
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

// mediaPrimaryName is 01-SPEC §7.3's single primary-image row name, transcribed
// as EXACT BYTES from internal/grpc/characteraccess_projection.go:14. There is
// no normalization step anywhere in the read path, so the literal here and the
// literal there are the whole contract.
const mediaPrimaryName = "profile.image.primary"

// mediaGallerySlotNames is §7.3's ten gallery slot names, transcribed verbatim
// from profileGallerySlotNames (characteraccess_projection.go:27-38) IN THE
// ORDER THEY ARE EMITTED. Two-digit zero padding is load-bearing: a
// single-digit spelling is a different row that no policy enumerates.
var mediaGallerySlotNames = []string{
	"profile.image.gallery.00",
	"profile.image.gallery.01",
	"profile.image.gallery.02",
	"profile.image.gallery.03",
	"profile.image.gallery.04",
	"profile.image.gallery.05",
	"profile.image.gallery.06",
	"profile.image.gallery.07",
	"profile.image.gallery.08",
	"profile.image.gallery.09",
}

// mediaUnenumeratedSlotName is the eleventh gallery index. It is a perfectly
// legal row — the namespace is open (§7.1) and an owner may write it — and it is
// named by NO §8.6 tier-floor list, so §8.6's totality rule DENIES it rather than
// defaulting it to the rung its ten siblings sit at.
const mediaUnenumeratedSlotName = "profile.image.gallery.10"

const (
	mediaPrimarySentinel      = "SENTINEL-MEDIA-PRIMARY-6b0f41ae"
	mediaUnenumeratedSentinel = "SENTINEL-MEDIA-UNENUMERATED-6b0f41ae"
)

// gallerySentinel gives each slot a DISTINCT handle, so a positional mix-up in
// the emitted gallery shows up as a readable diff rather than hiding behind ten
// identical strings.
func gallerySentinel(slot int) string {
	return fmt.Sprintf("SENTINEL-MEDIA-GALLERY-%02d-6b0f41ae", slot)
}

// EXT-05 / ROADMAP criterion 5. One primary plus ten gallery rows insert through
// the REAL entity_properties schema and read back through the REAL viewer-filtered
// read path, so the claim that no migration is owed when upload arrives is
// demonstrated rather than asserted.
//
// TWO CLAUSES ARE CITED HERE AND DELIBERATELY NOT REPROVED (rule 7zy1161fh1):
//
//   - "exactly one primary" is the database's UNIQUE(parent_type, parent_id, name)
//     constraint, already proven by TestPropertyRepository_ParentNameUniqueness
//     (internal/world/postgres/property_repo_test.go:430). This spec inserts ONE
//     primary and does not attempt a second: a duplicate-insert here would be a
//     second gate over coverage that exists, which is the anti-pattern 01-SPEC
//     §2.6 names.
//   - "the policy enumerates exactly eleven media names and not a twelfth" is
//     already proven by TestTheElevenMediaNamesAreEnumeratedAndTheTwelfthIsNot
//     (internal/access/policy/seed_profile_visibility_test.go:393). This spec
//     asserts the RUNTIME consequence of that enumeration — an unenumerated row
//     that EXISTS is absent from the response — which the seed test cannot reach.
//
// It is kept SEPARATE from character_readtime_floor_test.go on purpose: a
// media-schema regression and a read-time-evaluation regression must not collapse
// into one red test with two possible causes.
var _ = Describe("EXT-05: the media naming scheme survives the viewer-filtered read path", func() {
	const (
		anonToken  = ""
		guestToken = "guest-media-schema-token"
	)

	var (
		charID    ulid.ULID
		locID     ulid.ULID
		psRepo    auth.PlayerSessionRepository
		playerRep auth.PlayerRepository
		srv       *holoGRPC.CharacterAccessServer
	)

	// newServer is the same constructor every character spec in this package
	// drives, wired to the same production adapters cmd/holomush passes.
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
			// No spec here seats a character, so the create pipeline fails the
			// spec rather than answering with a zero value.
			failOnCallCreator{},
		)
	}

	read := func(token string) *characteraccessv1.GetCharacterProfileResponse {
		resp, err := srv.GetCharacterProfile(context.Background(), &characteraccessv1.GetCharacterProfileRequest{
			CharacterId:        charID.String(),
			PlayerSessionToken: token,
		})
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	// body is the assertion surface, per PORTAL-10 rule 3: a populated Go struct
	// would satisfy a presence check against an implementation that never
	// serialized the field, and an absence check against one that emitted it.
	body := func(resp *characteraccessv1.GetCharacterProfileResponse) string {
		raw, err := proto.Marshal(resp)
		Expect(err).NotTo(HaveOccurred())
		return string(raw)
	}

	galleryHandles := func(resp *characteraccessv1.GetCharacterProfileResponse) []string {
		out := make([]string, 0, len(resp.GetCharacter().GetGallery()))
		for _, img := range resp.GetCharacter().GetGallery() {
			out = append(out, img.GetMediaId())
		}
		return out
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
			VALUES ($1, 'Media Hall', 'A hall.', 'persistent', 'last:0')`,
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
		charName := uniqueCharFixtureName("Portrait", charID)
		_, err = env.pool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, description, location_id, normalized_name, name_skeleton, name_skeleton_unicode_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			append([]any{charID.String(), ownerID.String(), charName, "A figure in a framed portrait.", locID.String()},
				chartest.Columns(charName)...)...)
		Expect(err).NotTo(HaveOccurred())

		// The guest PLAYER and its live session. The token is what moves the
		// reading principal off the `anonymous` rung — without it the guest read
		// below silently collapses into a second anonymous read and the paired
		// control compares one rung against itself.
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

		// The gallery rows are inserted in a SCRAMBLED order on purpose. The
		// emitted order must come from the projection's slot-name slice, not from
		// insertion order or from Go's map iteration, and an in-order fixture
		// could not tell the two apart.
		for _, slot := range []int{7, 0, 3, 9, 1, 5, 2, 8, 4, 6} {
			insertProperty("character", charID, mediaGallerySlotNames[slot], gallerySentinel(slot), "public", nil, nil, nil)
		}

		// Exactly ONE primary. The "at most one" half is the database's UNIQUE
		// constraint and is cited, not reproved — see this file's header.
		insertProperty("character", charID, mediaPrimaryName, mediaPrimarySentinel, "public", nil, nil, nil)

		// The unenumerated eleventh index, stored exactly as its ten siblings are.
		insertProperty("character", charID, mediaUnenumeratedSlotName, mediaUnenumeratedSentinel, "public", nil, nil, nil)

		srv = newServer(env.engine)
		env.auditWriter.Reset()
	})

	Describe("eleven real rows through the real schema reach a guest-rung viewer as one primary and ten ordered gallery entries", func() {
		It("M1: the primary and the ten slots read back positionally, and the unenumerated eleventh does not", func() {
			// Non-vacuity: the fixture really did land twelve rows, so an absence
			// asserted below is the read path withholding rather than an INSERT
			// that never happened.
			var stored int
			Expect(env.pool.QueryRow(context.Background(),
				`SELECT count(*) FROM entity_properties WHERE parent_id = $1 AND name LIKE 'profile.image.%'`,
				charID.String()).Scan(&stored)).To(Succeed())
			Expect(stored).To(Equal(12),
				"one primary, ten enumerated slots and the unenumerated eleventh must all be in the table")

			resp := read(guestToken)
			raw := body(resp)

			Expect(resp.GetCharacter().GetPrimaryImage().GetMediaId()).To(Equal(mediaPrimarySentinel))
			Expect(strings.Contains(raw, mediaPrimarySentinel)).To(BeTrue(),
				"the primary handle must reach the wire, not merely the Go struct")

			want := make([]string, len(mediaGallerySlotNames))
			for i := range mediaGallerySlotNames {
				want[i] = gallerySentinel(i)
			}
			// Positional, never set-wise: a re-sorted gallery is a different
			// answer, because element order on a repeated field is carried on
			// the wire.
			Expect(galleryHandles(resp)).To(Equal(want))
			for _, handle := range want {
				Expect(strings.Contains(raw, handle)).To(BeTrue(),
					"gallery handle %s must reach the wire", handle)
			}

			// The media rows are routed OUT of the text map rather than
			// duplicated into it: a media handle in the prose map is rendered as
			// prose by any client walking the map.
			profile := resp.GetCharacter().GetProfile()
			Expect(profile).NotTo(HaveKey(mediaPrimaryName))
			for _, name := range mediaGallerySlotNames {
				Expect(profile).NotTo(HaveKey(name))
			}

			// §8.6's totality rule: an unenumerated name is DENIED, not defaulted
			// to the rung its siblings sit at. The row exists, is `public`, and is
			// spelled exactly as its siblings are — and it is still absent.
			Expect(profile).NotTo(HaveKey(mediaUnenumeratedSlotName))
			Expect(strings.Contains(raw, mediaUnenumeratedSentinel)).To(BeFalse(),
				"an unenumerated media name is denied by §8.6's totality rule, never defaulted")
		})

		It("M2: an anonymous viewer receives neither the primary nor any gallery entry for the same character", func() {
			resp := read(anonToken)
			raw := body(resp)

			// The profile still RESOLVES — reachability sits at the anonymous
			// rung — so this is field-level withholding, not a not-found.
			Expect(resp.GetCharacter().GetId()).To(Equal(charID.String()))

			Expect(resp.GetCharacter().GetPrimaryImage()).To(BeNil())
			Expect(resp.GetCharacter().GetGallery()).To(BeEmpty())

			Expect(strings.Contains(raw, mediaPrimarySentinel)).To(BeFalse(),
				"the primary handle sits at the guest floor; its bytes must not appear in an anonymous response")
			for i := range mediaGallerySlotNames {
				Expect(strings.Contains(raw, gallerySentinel(i))).To(BeFalse(),
					"gallery slot %d sits at the guest floor; its bytes must not appear in an anonymous response", i)
			}
		})
	})
})
