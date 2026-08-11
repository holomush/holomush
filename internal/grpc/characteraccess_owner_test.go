// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/profilevis"
	"github.com/holomush/holomush/internal/auth"
	authmocks "github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/world"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// This file drives the OWNER audience. It is a separate audience from the
// public read next door, and the separation is the point (D-69): the owner
// reaches their own profile rows through a path that constructs NO `viewer:`
// principal, so the multi-alt asymmetry in the derived owner peer
// (internal/access/policy/attribute/property.go, D-27) never governs a surface
// anyone depends on.
//
// Every spec below therefore asserts the SUBJECT the enumeration was driven
// with, not merely that the call succeeded — the subject string is the only
// externally observable evidence of which principal the facade built.

const ownerTestToken = "owner-audience-session-token"

// ownerWorldReader returns a fixed row slice and description, and RECORDS every
// world.Caller it was handed.
//
// Recording is load-bearing. world.Caller's fields are unexported, but the type
// is comparable by value, so a recorded caller can be asserted equal to the one
// the owner path is required to build — and unequal to any viewer-flavoured
// one. A spec that only asserted "the read succeeded" would pass just as well
// if the owner path resolved a viewer rung.
type ownerWorldReader struct {
	rows    []*world.EntityProperty
	listErr error
	desc    world.CharacterDescription
	callers []world.Caller
}

func (r *ownerWorldReader) ListPropertiesByParent(_ context.Context, caller world.Caller, _ string, _ ulid.ULID) ([]*world.EntityProperty, error) {
	r.callers = append(r.callers, caller)
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.rows, nil
}

func (r *ownerWorldReader) GetCharacterDescription(_ context.Context, caller world.Caller, _ ulid.ULID) (world.CharacterDescription, error) {
	r.callers = append(r.callers, caller)
	return r.desc, nil
}

// failOnCallProfileVisibility fails the test if EITHER method is reached. It
// deliberately does not return a usable zero value: behavior 6 is that the
// owner audience never consults the per-attribute visibility evaluator at all,
// and a double that quietly returned (false, nil) would let a regression that
// DOES consult it pass as an empty profile.
type failOnCallProfileVisibility struct{ t *testing.T }

func (f *failOnCallProfileVisibility) Reachable(context.Context, string, string) (bool, error) {
	f.t.Helper()
	f.t.Fatal("the owner audience MUST NOT consult the profile-visibility evaluator: Reachable was called")
	return false, nil
}

func (f *failOnCallProfileVisibility) VisibleAttributes(context.Context, string, string, []profilevis.Property) (map[string]profilevis.Property, error) {
	f.t.Helper()
	f.t.Fatal("the owner audience MUST NOT consult the profile-visibility evaluator: VisibleAttributes was called")
	return nil, nil
}

// ownerHarness is one wired facade plus the fixture identifiers a spec drives
// it with.
type ownerHarness struct {
	srv      *CharacterAccessServer
	reader   *ownerWorldReader
	owned    []*world.Character
	playerID ulid.ULID
	token    string
}

// ownerFixture describes the caller the harness wires.
type ownerFixture struct {
	// isGuest makes the resolved player a guest, so resolveAndGate denies.
	isGuest bool
	// owned is the roster charRepo.ListByPlayer returns.
	owned []*world.Character
	// rows is what the property enumeration returns for any character.
	rows []*world.EntityProperty
	// listErr fails the roster lookup.
	listErr error
}

// newOwnerHarness wires a facade whose profile-visibility dependency FAILS the
// test if it is reached, so every spec in this file is simultaneously a proof
// of behavior 6.
func newOwnerHarness(t *testing.T, f ownerFixture) *ownerHarness {
	t.Helper()

	playerID := idgen.New()
	ps := &auth.PlayerSession{ID: idgen.New(), PlayerID: playerID, TokenHash: auth.HashSessionToken(ownerTestToken)}

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, ps.TokenHash).Return(ps, nil).Maybe()
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, mock.Anything).
		Return(nil, oops.Code("PLAYER_SESSION_NOT_FOUND").Errorf("no such session")).Maybe()
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, ps.ID, auth.PlayerSessionTTL).Return(nil).Maybe()

	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).
		Return(&auth.Player{ID: playerID, IsGuest: f.isGuest}, nil).Maybe()

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return(f.owned, f.listErr).Maybe()

	reader := &ownerWorldReader{
		rows: f.rows,
		desc: world.CharacterDescription{Name: profileTestCharacterName, Description: profileTestDescription},
	}

	return &ownerHarness{
		srv:      NewCharacterAccessServer(reader, &recordingWorldMutator{t: t, failOnCall: true}, &failOnCallProfileVisibility{t: t}, &failOnCallDirectoryGate{t: t}, &failOnCallDirectoryReader{t: t}, sessionRepo, playerRepo, charRepo),
		reader:   reader,
		owned:    f.owned,
		playerID: playerID,
		token:    ownerTestToken,
	}
}

// ownedCharacterFixture builds a full-entity character row the way a
// ListByPlayer read returns one.
func ownedCharacterFixture(playerID ulid.ULID, name string, st world.Status) *world.Character {
	return &world.Character{
		ID:          idgen.New(),
		PlayerID:    playerID,
		Name:        name,
		Description: profileTestDescription,
		Status:      st,
		Version:     7,
	}
}

// TestListMyCharactersReturnsTheCallersOwnRosterIncludingRetiredCharacters is
// behavior 1. The roster MUST carry retired characters (01-SPEC §4.5 property
// 1): a roster that hid them would put un-retire out of reach of the only
// surface that can perform it.
func TestListMyCharactersReturnsTheCallersOwnRosterIncludingRetiredCharacters(t *testing.T) {
	t.Parallel()

	playerID := idgen.New()
	active := ownedCharacterFixture(playerID, "Ada", world.StatusActive)
	retired := ownedCharacterFixture(playerID, "Bex", world.StatusRetired)
	idle := ownedCharacterFixture(playerID, "Cyd", world.StatusIdle)

	h := newOwnerHarness(t, ownerFixture{owned: []*world.Character{active, retired, idle}})

	resp, err := h.srv.ListMyCharacters(context.Background(), &characteraccessv1.ListMyCharactersRequest{
		PlayerSessionToken: h.token,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetCharacters(), 3, "every owned character is on the roster, whatever its lifecycle status")

	byID := make(map[string]*characteraccessv1.OwnCharacter, len(resp.GetCharacters()))
	for _, c := range resp.GetCharacters() {
		byID[c.GetId()] = c
	}

	require.Contains(t, byID, retired.ID.String(), "a retired character MUST appear on its owner's roster")
	assert.Equal(t, "retired", byID[retired.ID.String()].GetStatus(),
		"the lifecycle status travels with the row — it is what the un-retire affordance branches on")
	assert.Equal(t, "active", byID[active.ID.String()].GetStatus())
	assert.Equal(t, "idle", byID[idle.ID.String()].GetStatus())
	assert.Equal(t, "Bex", byID[retired.ID.String()].GetName())
	assert.Equal(t, int32(7), byID[active.ID.String()].GetVersion(),
		"the optimistic-concurrency token the client sends back as expected_version")
}

// TestListMyCharactersDeniesAGuestWithTheCharacterFacadesOwnDenialMessage is
// behavior 2. The message MUST NOT be the scene facade's: a character surface
// that told a guest "guests cannot access scenes" would be naming a subsystem
// the caller never touched (01-SPEC §9.6 wire opacity).
func TestListMyCharactersDeniesAGuestWithTheCharacterFacadesOwnDenialMessage(t *testing.T) {
	t.Parallel()

	h := newOwnerHarness(t, ownerFixture{isGuest: true})

	resp, err := h.srv.ListMyCharacters(context.Background(), &characteraccessv1.ListMyCharactersRequest{
		PlayerSessionToken: h.token,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, characterGuestDenialMessage, status.Convert(err).Message())
	assert.NotEqual(t, sceneGuestDenialMessage, status.Convert(err).Message(),
		"the character facade denies in its own words; it names no other subsystem")
}

// TestListMyCharactersIsUnauthenticatedWithoutAResolvableSession is behavior 3.
//
// This is the one place the owner audience differs sharply from the public
// read: an unresolvable token there DEGRADES to the anonymous rung, because a
// logged-out visitor must still reach a public page. Here there is no rung to
// degrade to — "my characters" has no meaning without a resolved player — so it
// is an authentication failure.
func TestListMyCharactersIsUnauthenticatedWithoutAResolvableSession(t *testing.T) {
	t.Parallel()

	h := newOwnerHarness(t, ownerFixture{})

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"no token at all", ""},
		{"a token naming no session", "not-a-real-session-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, err := h.srv.ListMyCharacters(context.Background(), &characteraccessv1.ListMyCharactersRequest{
				PlayerSessionToken: tc.token,
			})
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Equal(t, codes.Unauthenticated, status.Code(err))
		})
	}
}

// TestGetMyCharacterCarriesTheOwnersFullPropertySliceAndDrivesNoViewerPrincipal
// is behavior 4.
//
// The seeded row is `profile.biography`, which the shipped corpus withholds at
// the ANONYMOUS rung — asserted directly next door by
// TestGetCharacterProfileWithholdsABelowFloorFieldFromTheMarshaledBytes, and by
// the paired public leg below, which drives the SAME row through the real
// seeded engine and the real profilevis.Evaluator and gets nothing.
func TestGetMyCharacterCarriesTheOwnersFullPropertySliceAndDrivesNoViewerPrincipal(t *testing.T) {
	t.Parallel()

	playerID := idgen.New()
	char := ownedCharacterFixture(playerID, "Ada", world.StatusActive)

	const belowAnonFloor = "Born in the salt flats."
	seeds := []profileSeed{
		publicSeed("profile.pronouns", "they/them"),
		publicSeed("profile.biography", belowAnonFloor),
	}
	rows := make([]*world.EntityProperty, 0, len(seeds))
	for _, s := range seeds {
		rows = append(rows, s.entity(char.ID))
	}

	h := newOwnerHarness(t, ownerFixture{owned: []*world.Character{char}, rows: rows})

	resp, err := h.srv.GetMyCharacter(context.Background(), &characteraccessv1.GetMyCharacterRequest{
		CharacterId:        char.ID.String(),
		PlayerSessionToken: h.token,
	})
	require.NoError(t, err)
	require.NotNil(t, resp.GetCharacter())

	assert.Equal(t, belowAnonFloor, resp.GetCharacter().GetProfile()["profile.biography"],
		"the owner reads the VALUE of a row a public viewer would not see at all")
	assert.Equal(t, "they/them", resp.GetCharacter().GetProfile()["profile.pronouns"])
	assert.Equal(t, char.ID.String(), resp.GetCharacter().GetId())
	assert.Equal(t, "active", resp.GetCharacter().GetStatus())
	assert.Equal(t, int32(7), resp.GetCharacter().GetVersion())

	// The subject assertion is the D-69 pin: the enumeration is driven with the
	// owning CHARACTER's subject, never a viewer-flavoured one, so the derived
	// player-keyed peer never governs this read.
	wantCaller := world.HumanCaller(access.CharacterSubject(char.ID.String()))
	require.NotEmpty(t, h.reader.callers)
	for _, got := range h.reader.callers {
		assert.Equal(t, wantCaller, got,
			"the owner audience constructs a character subject; a viewer principal on this path is D-69's regression")
		assert.NotEqual(t, world.HumanCaller(access.ViewerSubject(access.ViewerTierPlayer, playerID.String())), got)
		assert.NotEqual(t, world.HumanCaller(access.ViewerSubject(access.ViewerTierAnonymous, "")), got)
	}

	t.Run("the same row is withheld from an anonymous public read of the same character", func(t *testing.T) {
		t.Parallel()
		// The positive control for the claim above: the SAME seeds, driven
		// through the real seeded corpus at the anonymous rung, publish
		// pronouns and withhold biography. Without this leg the owner
		// assertion cannot distinguish "the owner sees more" from "every rung
		// sees everything".
		ph := newProfileHarness(t, access.ViewerTierAnonymous, seeds)
		public, err := ph.read(t)
		require.NoError(t, err)
		assert.Equal(t, "they/them", public.GetCharacter().GetProfile()["profile.pronouns"])
		assert.NotContains(t, public.GetCharacter().GetProfile(), "profile.biography",
			"the shipped corpus floors biography above the anonymous rung")
	})
}

// TestGetMyCharacterReturnsAnIdenticalNotFoundForANonOwnedAndAnUnknownCharacter
// is behavior 5, and the assertion is an EQUALITY between the two outcomes
// rather than two comparisons against a literal.
//
// A literal comparison would still pass if one branch later gained a distinct
// message while the literal was updated in both places. The equality cannot: it
// fails the moment the two diverge, which is the property §8.7 actually wants —
// a caller MUST NOT be able to learn that a character exists by being refused
// it differently.
func TestGetMyCharacterReturnsAnIdenticalNotFoundForANonOwnedAndAnUnknownCharacter(t *testing.T) {
	t.Parallel()

	playerID := idgen.New()
	mine := ownedCharacterFixture(playerID, "Ada", world.StatusActive)
	someoneElses := idgen.New()

	h := newOwnerHarness(t, ownerFixture{owned: []*world.Character{mine}})

	get := func(id string) error {
		_, err := h.srv.GetMyCharacter(context.Background(), &characteraccessv1.GetMyCharacterRequest{
			CharacterId:        id,
			PlayerSessionToken: h.token,
		})
		return err
	}

	notOwned := get(someoneElses.String())
	unknown := get(idgen.New().String())
	require.Error(t, notOwned)
	require.Error(t, unknown)

	assert.Equal(t, status.Code(unknown), status.Code(notOwned))
	assert.Equal(t, status.Convert(unknown).Message(), status.Convert(notOwned).Message(),
		"a character the caller does not own and a character that does not exist MUST be indistinguishable")
	assert.Equal(t, codes.NotFound, status.Code(notOwned))
}

// TestGetMyCharacterReportsARosterLookupFailureAsInternal pins that an OUTAGE
// is not rendered as the not-found-equivalent. Collapsing the two would make an
// unreachable repository look like a caller who owns nothing — a working
// feature with no rows (01-SPEC §8.10).
func TestGetMyCharacterReportsARosterLookupFailureAsInternal(t *testing.T) {
	t.Parallel()

	h := newOwnerHarness(t, ownerFixture{listErr: oops.Code("DB_DOWN").Errorf("connection refused")})

	_, err := h.srv.GetMyCharacter(context.Background(), &characteraccessv1.GetMyCharacterRequest{
		CharacterId:        idgen.New().String(),
		PlayerSessionToken: h.token,
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))

	_, listErr := h.srv.ListMyCharacters(context.Background(), &characteraccessv1.ListMyCharactersRequest{
		PlayerSessionToken: h.token,
	})
	require.Error(t, listErr)
	assert.Equal(t, codes.Internal, status.Code(listErr))
}

// TestOwnerAudienceNeverReachesTheVisibilityEvaluator is behavior 6 stated
// explicitly. Every spec in this file already wires
// failOnCallProfileVisibility, so the property is enforced continuously; this
// spec exists so the guarantee has a name a reader can grep for, and so a
// future harness change that quietly softened the double is caught here.
func TestOwnerAudienceNeverReachesTheVisibilityEvaluator(t *testing.T) {
	t.Parallel()

	playerID := idgen.New()
	char := ownedCharacterFixture(playerID, "Ada", world.StatusActive)
	h := newOwnerHarness(t, ownerFixture{owned: []*world.Character{char}})

	_, ok := h.srv.profileVis.(*failOnCallProfileVisibility)
	require.True(t, ok, "the owner harness MUST wire the fail-on-call evaluator, not a permissive stub")

	_, err := h.srv.ListMyCharacters(context.Background(), &characteraccessv1.ListMyCharactersRequest{
		PlayerSessionToken: h.token,
	})
	require.NoError(t, err)

	_, err = h.srv.GetMyCharacter(context.Background(), &characteraccessv1.GetMyCharacterRequest{
		CharacterId:        char.ID.String(),
		PlayerSessionToken: h.token,
	})
	require.NoError(t, err)
}

// TestOwnerAudienceOmitsAnEmptyValuedRowFromTheProfileMap pins that
// projectOwner omits rather than emits-empty, the same rule projectPublic
// follows (§7.5). The owner has no redaction to hide, but a present-and-empty
// key is still a shape the edit surface would write back as a blank row.
func TestOwnerAudienceOmitsAnEmptyValuedRowFromTheProfileMap(t *testing.T) {
	t.Parallel()

	playerID := idgen.New()
	char := ownedCharacterFixture(playerID, "Ada", world.StatusActive)

	empty := publicSeed("profile.concept", "")
	nilled := profileSeed{id: idgen.New(), name: "profile.currently", nilValue: true, visibility: "public"}
	present := publicSeed("profile.pronouns", "they/them")

	rows := []*world.EntityProperty{
		empty.entity(char.ID),
		nilled.entity(char.ID),
		present.entity(char.ID),
	}
	h := newOwnerHarness(t, ownerFixture{owned: []*world.Character{char}, rows: rows})

	resp, err := h.srv.GetMyCharacter(context.Background(), &characteraccessv1.GetMyCharacterRequest{
		CharacterId:        char.ID.String(),
		PlayerSessionToken: h.token,
	})
	require.NoError(t, err)

	profile := resp.GetCharacter().GetProfile()
	assert.Contains(t, profile, "profile.pronouns")
	assert.NotContains(t, profile, "profile.concept", "an empty-string value is OMITTED, never present-and-empty")
	assert.NotContains(t, profile, "profile.currently", "a NULL value column is OMITTED")
}
