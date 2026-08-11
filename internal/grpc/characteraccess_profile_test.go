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
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/access/profilevis"
	"github.com/holomush/holomush/internal/auth"
	authmocks "github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/testsupport/abactest"
	"github.com/holomush/holomush/internal/world"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// This file drives the REAL seeded policy corpus through abactest.NewSeedEngine
// and the REAL profilevis.Evaluator. Only the world reader is a double, and it
// is a double solely so the enumeration's contents are a fixture rather than a
// database. Every visibility verdict below is the shipped engine answering.
//
// A hand-built policy set was rejected: the property under test is that the
// facade composes term A and term B AS SEEDED, so a corpus written by the test
// would prove the composition against policies that do not ship.

const (
	profileTestCharacterName = "Ada"
	profileTestDescription   = "A tall figure wrapped in a travelling cloak."
	profileTestToken         = "profile-read-session-token"
)

// profileSeed is one entity_properties row expressed once and consumed twice:
// as the domain row the enumeration returns, and as the attribute bag the real
// seeded policies evaluate against. Expressing it once is what keeps the two
// halves from drifting into a test that permits a row the enumeration never
// supplied.
type profileSeed struct {
	id         ulid.ULID
	name       string
	value      string
	nilValue   bool
	visibility string
}

// publicSeed is the ordinary case: a `public` row carrying a value. Term B
// (seed:viewer-property-public-read) permits it for every rung, so whether it
// reaches the response is decided by term A alone — which is what makes these
// fixtures a clean probe of the per-attribute floor.
func publicSeed(name, value string) profileSeed {
	return profileSeed{id: idgen.New(), name: name, value: value, visibility: "public"}
}

func (s profileSeed) entity(parentID ulid.ULID) *world.EntityProperty {
	row := &world.EntityProperty{
		ID:         s.id,
		ParentType: characterParentType,
		ParentID:   parentID,
		Name:       s.name,
		Visibility: s.visibility,
	}
	if !s.nilValue {
		value := s.value
		row.Value = &value
	}
	return row
}

// rowKeyedPropertyProvider resolves a DIFFERENT attribute bag per property:<id>.
//
// abactest.PropertyProvider cannot be used directly here: it is a static
// provider that returns the same bag for every resource id, so a fixture with
// two rows would evaluate both against one row's name and visibility and every
// per-row assertion in this file would be meaningless. The per-row bags are
// still BUILT by abactest.PropertyProvider, so the derived player-keyed peers
// stay the shipped derivation rather than a second one written here.
type rowKeyedPropertyProvider struct {
	byResource map[string]map[string]any
}

func (p *rowKeyedPropertyProvider) Namespace() string { return "property" }

func (p *rowKeyedPropertyProvider) ResolveSubject(context.Context, string) (map[string]any, error) {
	return nil, nil
}

func (p *rowKeyedPropertyProvider) ResolveResource(_ context.Context, resourceID string) (map[string]any, error) {
	return p.byResource[resourceID], nil
}

func (p *rowKeyedPropertyProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{Attributes: abactest.PropertySchemaKeys}
}

func newRowKeyedPropertyProvider(t *testing.T, seeds []profileSeed, parentID ulid.ULID) attribute.AttributeProvider {
	t.Helper()

	byResource := make(map[string]map[string]any, len(seeds))
	for _, seed := range seeds {
		resource := access.PropertyResource(seed.id.String())
		fixture := abactest.PropertyFixture{
			ID:         seed.id.String(),
			Name:       seed.name,
			ParentType: characterParentType,
			ParentID:   parentID.String(),
			Visibility: seed.visibility,
		}
		if !seed.nilValue {
			value := seed.value
			fixture.Value = &value
		}
		attrs, err := abactest.PropertyProvider(fixture).ResolveResource(context.Background(), resource)
		require.NoError(t, err)
		byResource[resource] = attrs
	}
	return &rowKeyedPropertyProvider{byResource: byResource}
}

// fakeProfileWorldReader stands in for world.Service. It returns a fixed row
// slice and a fixed description, and it COUNTS the enumeration calls, because
// "exactly one enumeration" is a property of the composition rather than of the
// source text alone.
type fakeProfileWorldReader struct {
	rows      []*world.EntityProperty
	listErr   error
	desc      world.CharacterDescription
	descErr   error
	listCalls int
}

func (f *fakeProfileWorldReader) ListPropertiesByParent(_ context.Context, _ world.Caller, _ string, _ ulid.ULID) ([]*world.EntityProperty, error) {
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.rows, nil
}

func (f *fakeProfileWorldReader) GetCharacterDescription(_ context.Context, _ world.Caller, _ ulid.ULID) (world.CharacterDescription, error) {
	if f.descErr != nil {
		return world.CharacterDescription{}, f.descErr
	}
	return f.desc, nil
}

// profileHarness is one wired facade plus the fixture identifiers a spec needs
// to drive it.
type profileHarness struct {
	srv    *CharacterAccessServer
	reader *fakeProfileWorldReader
	charID ulid.ULID
	token  string
}

func (h *profileHarness) read(t *testing.T) (*characteraccessv1.GetCharacterProfileResponse, error) {
	t.Helper()
	return h.srv.GetCharacterProfile(context.Background(), &characteraccessv1.GetCharacterProfileRequest{
		CharacterId:        h.charID.String(),
		PlayerSessionToken: h.token,
	})
}

// newProfileHarness wires a facade whose viewer resolves to `tier` and whose
// enumeration returns exactly `seeds`.
//
// The rung is driven END TO END rather than injected: for a non-anonymous tier
// the harness seeds a session and a player so the facade's own
// resolveViewerIdentity produces the rung (D-83, and the wave-1 seam plan 04-01
// left behind). playerGate.resolveAndGate is deliberately not involved — it
// denies guests outright (INV-SCENE-64), which would refuse the very caller a
// public profile read exists to serve.
func newProfileHarness(t *testing.T, tier string, seeds []profileSeed) *profileHarness {
	t.Helper()

	charID := idgen.New()
	playerID := idgen.New()

	rows := make([]*world.EntityProperty, 0, len(seeds))
	for _, seed := range seeds {
		rows = append(rows, seed.entity(charID))
	}

	viewerPlayerID := ""
	token := ""
	if tier != access.ViewerTierAnonymous {
		viewerPlayerID = playerID.String()
		token = profileTestToken
	}

	engine := abactest.NewSeedEngine(
		t,
		abactest.ViewerProvider(abactest.Viewer{Tier: tier, PlayerID: viewerPlayerID}),
		newRowKeyedPropertyProvider(t, seeds, charID),
	)

	reader := &fakeProfileWorldReader{
		rows: rows,
		desc: world.CharacterDescription{Name: profileTestCharacterName, Description: profileTestDescription},
	}

	sessionRepo, playerRepo := profileAuthRepos(t, playerID, tier)
	return &profileHarness{
		srv:    NewCharacterAccessServer(reader, &profilevis.Evaluator{Engine: engine}, sessionRepo, playerRepo),
		reader: reader,
		charID: charID,
		token:  token,
	}
}

func profileAuthRepos(t *testing.T, playerID ulid.ULID, tier string) (auth.PlayerSessionRepository, auth.PlayerRepository) {
	t.Helper()

	ps := &auth.PlayerSession{ID: idgen.New(), PlayerID: playerID, TokenHash: auth.HashSessionToken(profileTestToken)}
	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, ps.TokenHash).Return(ps, nil).Maybe()
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, ps.ID, auth.PlayerSessionTTL).Return(nil).Maybe()

	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).
		Return(&auth.Player{ID: playerID, IsGuest: tier == access.ViewerTierGuest}, nil).Maybe()

	return sessionRepo, playerRepo
}

// TestGetCharacterProfileCarriesTheStoredValueOfAPermittedAttribute is the
// regression the admissibility-set/value-source join exists to prevent
// (review finding HIGH-5, threat T-04-17).
//
// profilevis.Property is {ID, Name} and NOTHING else, so a projection built
// from the visible map alone produces a response carrying the right KEYS with
// EMPTY VALUES. That response satisfies any assertion phrased as "the map
// contains profile.pronouns". This test therefore asserts the VALUE, and it is
// the reason the facade keeps a row-id index over the same enumerated slice.
func TestGetCharacterProfileCarriesTheStoredValueOfAPermittedAttribute(t *testing.T) {
	t.Parallel()

	h := newProfileHarness(t, access.ViewerTierAnonymous, []profileSeed{
		publicSeed("profile.pronouns", "they/them"),
	})

	resp, err := h.read(t)
	require.NoError(t, err)
	require.NotNil(t, resp.GetCharacter())
	assert.Equal(t, "they/them", resp.GetCharacter().GetProfile()["profile.pronouns"],
		"the emitted value MUST be the stored row value — a present key with an empty value is the HIGH-5 regression")
	assert.Equal(t, 1, h.reader.listCalls,
		"the value index and the evaluator input are two views of ONE enumeration")
}

// divergentProfileVisibility returns a visible map naming a property id the
// enumeration never supplied. It is the only way to reach the join's divergence
// branch, because the two sets are built from one slice in production.
type divergentProfileVisibility struct {
	visible map[string]profilevis.Property
}

func (d *divergentProfileVisibility) Reachable(context.Context, string, string) (bool, error) {
	return true, nil
}

func (d *divergentProfileVisibility) VisibleAttributes(context.Context, string, string, []profilevis.Property) (map[string]profilevis.Property, error) {
	return d.visible, nil
}

// TestGetCharacterProfileReportsAVisibleRowMissingFromTheEnumerationAsInternal
// pins that a divergence is an ERROR rather than a silently skipped field.
//
// Both sets are derived from one slice, so an id in the visible map with no row
// in the index means the evaluator returned something the enumeration never
// supplied. Emitting the field with a zero value would be the HIGH-5 regression
// wearing a different hat; dropping it silently would render a broken
// evaluator as a legitimately sparse profile, which §8.10 forbids.
func TestGetCharacterProfileReportsAVisibleRowMissingFromTheEnumerationAsInternal(t *testing.T) {
	t.Parallel()

	charID := idgen.New()
	reader := &fakeProfileWorldReader{
		desc: world.CharacterDescription{Name: profileTestCharacterName, Description: profileTestDescription},
	}
	vis := &divergentProfileVisibility{visible: map[string]profilevis.Property{
		"profile.pronouns": {ID: idgen.New().String(), Name: "profile.pronouns"},
	}}
	sessionRepo, playerRepo := profileAuthRepos(t, idgen.New(), access.ViewerTierAnonymous)
	srv := NewCharacterAccessServer(reader, vis, sessionRepo, playerRepo)

	resp, err := srv.GetCharacterProfile(context.Background(), &characteraccessv1.GetCharacterProfileRequest{
		CharacterId: charID.String(),
	})
	require.Error(t, err)
	assert.Nil(t, resp, "a divergence MUST NOT return a partial success missing one field")
	assert.Equal(t, codes.Internal, status.Code(err))
}

// TestGetCharacterProfileWithholdsAnAttributeAboveTheViewersFloor is the
// per-attribute floor, with its paired positive control in the same table.
//
// Without the guest leg the anonymous denial is indistinguishable from an empty
// fixture — the vacuity PORTAL-10 names.
func TestGetCharacterProfileWithholdsAnAttributeAboveTheViewersFloor(t *testing.T) {
	t.Parallel()

	seeds := []profileSeed{
		publicSeed("profile.pronouns", "they/them"),
		publicSeed("profile.biography", "Born in the salt flats."),
	}

	t.Run("an anonymous viewer receives pronouns and not the guest-floored biography", func(t *testing.T) {
		t.Parallel()
		h := newProfileHarness(t, access.ViewerTierAnonymous, seeds)

		resp, err := h.read(t)
		require.NoError(t, err)
		profile := resp.GetCharacter().GetProfile()
		assert.Equal(t, "they/them", profile["profile.pronouns"])

		_, present := profile["profile.biography"]
		assert.False(t, present,
			"profile.biography sits at the GUEST floor; the anonymous rung must not see it at all")
	})

	t.Run("a guest viewer receives both — the anonymous denial is the floor, not an empty fixture", func(t *testing.T) {
		t.Parallel()
		h := newProfileHarness(t, access.ViewerTierGuest, seeds)

		resp, err := h.read(t)
		require.NoError(t, err)
		profile := resp.GetCharacter().GetProfile()
		assert.Equal(t, "they/them", profile["profile.pronouns"])
		assert.Equal(t, "Born in the salt flats.", profile["profile.biography"])
	})
}

// TestGetCharacterProfileDeniesAPropertyNameInNoTierFloorList pins §8.6's
// totality rule: an unenumerated name is DENIED, not defaulted. The seeded
// tier-floor policies carry literal name lists with no glob, prefix or
// catch-all, so a name nobody has considered cannot publish itself.
func TestGetCharacterProfileDeniesAPropertyNameInNoTierFloorList(t *testing.T) {
	t.Parallel()

	seeds := []profileSeed{
		publicSeed("profile.pronouns", "they/them"),
		publicSeed("profile.not_in_the_table", "SHOULD NEVER PUBLISH"),
	}

	for _, tier := range []string{access.ViewerTierAnonymous, access.ViewerTierGuest, access.ViewerTierPlayer} {
		t.Run("the "+tier+" rung denies a name in no section 8.6 row", func(t *testing.T) {
			t.Parallel()
			h := newProfileHarness(t, tier, seeds)

			resp, err := h.read(t)
			require.NoError(t, err)
			profile := resp.GetCharacter().GetProfile()

			_, present := profile["profile.not_in_the_table"]
			assert.False(t, present, "an unenumerated name MUST default-deny rather than default-publish")
			assert.Equal(t, "they/them", profile["profile.pronouns"],
				"paired positive control: the same rung, the same path, an enumerated name")
		})
	}
}

// TestGetCharacterProfileMatchesGallerySlotNamesAsExactWholeStrings pins §7.3's
// exact-bytes comparison. profile.image.gallery.0 and .00 are two different
// rows that coexist happily in storage, and no normalization step anywhere in
// the read path collapses them — so the single-digit form must be denied while
// the zero-padded one publishes.
func TestGetCharacterProfileMatchesGallerySlotNamesAsExactWholeStrings(t *testing.T) {
	t.Parallel()

	h := newProfileHarness(t, access.ViewerTierGuest, []profileSeed{
		publicSeed("profile.image.gallery.0", "media-single-digit"),
		publicSeed("profile.image.gallery.00", "media-zero-padded"),
	})

	resp, err := h.read(t)
	require.NoError(t, err)

	_, present := resp.GetCharacter().GetProfile()["profile.image.gallery.0"]
	assert.False(t, present, "the single-digit slot name is in no section 8.6 row and is denied")

	require.Len(t, resp.GetCharacter().GetGallery(), 1,
		"paired positive control: the zero-padded slot name IS enumerated and publishes")
	assert.Equal(t, "media-zero-padded", resp.GetCharacter().GetGallery()[0].GetMediaId())
}

// TestGetCharacterProfileOmitsAnEmptyValuedRowFromTheProfileMap is §7.5's
// absence-not-emptiness rule on the blank-field side: a field the character
// left blank and a field the viewer may not see MUST look identical on the
// wire.
func TestGetCharacterProfileOmitsAnEmptyValuedRowFromTheProfileMap(t *testing.T) {
	t.Parallel()

	h := newProfileHarness(t, access.ViewerTierGuest, []profileSeed{
		publicSeed("profile.pronouns", "they/them"),
		publicSeed("profile.concept", ""),
		{id: idgen.New(), name: "profile.currently", nilValue: true, visibility: "public"},
	})

	resp, err := h.read(t)
	require.NoError(t, err)
	profile := resp.GetCharacter().GetProfile()

	_, emptyPresent := profile["profile.concept"]
	assert.False(t, emptyPresent, "an empty value is OMITTED, never emitted as a present-and-empty string")

	_, nilPresent := profile["profile.currently"]
	assert.False(t, nilPresent, "a NULL value column is omitted the same way")

	assert.Equal(t, "they/them", profile["profile.pronouns"],
		"paired positive control: a populated row on the same call still publishes")
}

// TestGetCharacterProfileReturnsASuccessfulEmptyProfileForACharacterWithNoRows
// pins §7.5: an empty profile is not a not-found.
func TestGetCharacterProfileReturnsASuccessfulEmptyProfileForACharacterWithNoRows(t *testing.T) {
	t.Parallel()

	h := newProfileHarness(t, access.ViewerTierAnonymous, nil)

	resp, err := h.read(t)
	require.NoError(t, err)
	assert.Equal(t, profileTestCharacterName, resp.GetCharacter().GetName())
	assert.Empty(t, resp.GetCharacter().GetProfile())
	assert.Nil(t, resp.GetCharacter().GetPrimaryImage())
	assert.Empty(t, resp.GetCharacter().GetGallery())
}

// TestGetCharacterProfileReportsAnEnumerationFailureAsInternal pins §8.10 on
// the enumeration leg: an infrastructure failure MUST NOT be rendered as a
// legitimately sparse profile. A successful response with an empty map here
// would look exactly like a character who filled nothing in.
func TestGetCharacterProfileReportsAnEnumerationFailureAsInternal(t *testing.T) {
	t.Parallel()

	h := newProfileHarness(t, access.ViewerTierAnonymous, []profileSeed{
		publicSeed("profile.pronouns", "they/them"),
	})
	h.reader.listErr = oops.Code("PROPERTY_QUERY_FAILED").
		Wrap(world.ErrAccessEvaluationFailed)

	resp, err := h.read(t)
	require.Error(t, err)
	assert.Nil(t, resp, "an enumeration outage MUST NOT return a successful sparse profile")
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.NotContains(t, status.Convert(err).Message(), "PROPERTY_QUERY_FAILED",
		"the inner error MUST NOT be interpolated into the wire message")
}

// TestGetCharacterProfileIsDeterministicAcrossIdenticalCalls pins that no Go
// map-iteration order reaches the projection.
//
// The REAL claim is proto.Equal over the two responses. The byte comparison is
// made under proto.MarshalOptions{Deterministic: true} on purpose:
// google.golang.org/protobuf documents Deterministic as the OPT-IN that carries
// "repeated serialization of the same message will return the same bytes"
// (proto/encode.go), and plain proto.Marshal promises nothing about map field
// ordering — so a plain byte comparison over a map<string, string> field can
// agree by luck and prove nothing.
func TestGetCharacterProfileIsDeterministicAcrossIdenticalCalls(t *testing.T) {
	t.Parallel()

	h := newProfileHarness(t, access.ViewerTierGuest, []profileSeed{
		publicSeed("profile.pronouns", "they/them"),
		publicSeed("profile.concept", "A cartographer of drowned roads."),
		publicSeed("profile.species", "human"),
		publicSeed("profile.faction", "The Salt Guild"),
		publicSeed("profile.timezone", "UTC+2"),
	})

	first, err := h.read(t)
	require.NoError(t, err)
	second, err := h.read(t)
	require.NoError(t, err)

	assert.True(t, proto.Equal(first, second),
		"two identical calls over unchanged data MUST produce proto-equal responses")

	opts := proto.MarshalOptions{Deterministic: true}
	firstBytes, err := opts.Marshal(first)
	require.NoError(t, err)
	secondBytes, err := opts.Marshal(second)
	require.NoError(t, err)
	require.Equal(t, firstBytes, secondBytes)
}

// TestGetCharacterProfileEmitsGalleryEntriesInAscendingSlotOrder asserts on the
// ELEMENT SEQUENCE rather than on bytes. Gallery is a repeated field, whose
// element order genuinely is carried on the wire, so this one is a real
// ordering property independent of any marshaler option — and asserting it
// through byte equality would prove something weaker by a longer route.
func TestGetCharacterProfileEmitsGalleryEntriesInAscendingSlotOrder(t *testing.T) {
	t.Parallel()

	// Deliberately seeded out of order.
	h := newProfileHarness(t, access.ViewerTierGuest, []profileSeed{
		publicSeed("profile.image.gallery.03", "media-03"),
		publicSeed("profile.image.gallery.00", "media-00"),
		publicSeed("profile.image.primary", "media-primary"),
		publicSeed("profile.image.gallery.09", "media-09"),
		publicSeed("profile.image.gallery.01", "media-01"),
	})

	resp, err := h.read(t)
	require.NoError(t, err)

	require.NotNil(t, resp.GetCharacter().GetPrimaryImage())
	assert.Equal(t, "media-primary", resp.GetCharacter().GetPrimaryImage().GetMediaId())

	got := make([]string, 0, len(resp.GetCharacter().GetGallery()))
	for _, img := range resp.GetCharacter().GetGallery() {
		got = append(got, img.GetMediaId())
	}
	assert.Equal(t, []string{"media-00", "media-01", "media-03", "media-09"}, got,
		"gallery entries are emitted in ascending slot-name order regardless of enumeration order")

	assert.Empty(t, resp.GetCharacter().GetProfile(),
		"media rows are projected into the image fields, never duplicated into the text map")
}
