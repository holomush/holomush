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
		srv:    NewCharacterAccessServer(reader, &recordingWorldMutator{t: t, failOnCall: true}, &profilevis.Evaluator{Engine: engine}, &failOnCallDirectoryGate{t: t}, &failOnCallDirectoryReader{t: t}, sessionRepo, playerRepo, authmocks.NewMockCharacterRepository(t), &failOnCallCreator{t: t}),
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
	srv := NewCharacterAccessServer(reader, &recordingWorldMutator{t: t, failOnCall: true}, vis, &failOnCallDirectoryGate{t: t}, &failOnCallDirectoryReader{t: t}, sessionRepo, playerRepo, authmocks.NewMockCharacterRepository(t), &failOnCallCreator{t: t})

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

// The criterion-2 sentinels. Each is far longer than a real profile value would
// need to be, carries a hyphenated uppercase marker, and contains characters no
// proto field name may contain — so a match in a marshaled body is the seeded
// value and cannot be an accidental collision with framing, a field name, or
// another fixture.
//
// They are seeded NON-EMPTY on purpose. A withholding test written against an
// empty fixture passes while the property under test is false, which is exactly
// the vacuity PORTAL-10 names.
const (
	biographySentinel     = "SENTINEL-BIOGRAPHY-BELOW-ANON-FLOOR-4f9c2ba7"
	rpPreferencesSentinel = "SENTINEL-RP-PREFERENCES-BELOW-ANON-FLOOR-1d63e805"
)

// TestGetCharacterProfileWithholdsABelowFloorFieldFromTheMarshaledBytes is
// criterion 2 (D-80): the guarantee is asserted at the WIRE, not at the Go
// value. A response whose profile map omitted the key while some other field
// still carried the text would satisfy a Go-level assertion and leak anyway.
//
// PLAIN proto.Marshal IS CORRECT HERE, and deliberately not
// proto.MarshalOptions{Deterministic: true}. Byte ABSENCE is not an ordering
// property: no permutation of a message that does not contain the sentinel can
// produce bytes that do. The determinism test elsewhere in this file needs the
// opt-in because it compares two encodings for EQUALITY; this one does not, and
// unifying the two would suggest determinism is load-bearing for absence when it
// is not.
func TestGetCharacterProfileWithholdsABelowFloorFieldFromTheMarshaledBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		attribute string
		sentinel  string
	}{
		{"the biography block", "profile.biography", biographySentinel},
		{"the OOC RP-preferences block", "profile.rp_preferences", rpPreferencesSentinel},
	}

	for _, tt := range tests {
		t.Run(tt.name+" is absent from an anonymous viewer's bytes and present for a guest", func(t *testing.T) {
			t.Parallel()

			seeds := []profileSeed{publicSeed(tt.attribute, tt.sentinel)}

			anonResp, err := newProfileHarness(t, access.ViewerTierAnonymous, seeds).read(t)
			require.NoError(t, err, "a below-floor field withholds the FIELD, never the whole profile")
			anonBody, err := proto.Marshal(anonResp)
			require.NoError(t, err)
			assert.NotContains(t, string(anonBody), tt.sentinel,
				"%s sits at the GUEST floor; its bytes MUST NOT appear anywhere in an anonymous viewer's response", tt.attribute)

			// Paired positive control, on the same fixture through the same code
			// path. Without it the assertion above cannot distinguish "the floor
			// worked" from "nothing was seeded".
			guestResp, err := newProfileHarness(t, access.ViewerTierGuest, seeds).read(t)
			require.NoError(t, err)
			guestBody, err := proto.Marshal(guestResp)
			require.NoError(t, err)
			assert.Contains(t, string(guestBody), tt.sentinel,
				"the guest rung clears %s's floor, so the sentinel MUST reach the wire — proving the fixture was populated", tt.attribute)
		})
	}
}

// TestGetCharacterProfileDeniesAnUnrecognizedViewerTierWithNoPrincipalAtAll is
// threat T-04-09. The property is not merely "an unrecognized rung is denied"
// but that the default arm produces NO VIEWER PRINCIPAL AT ALL — an empty
// subject, so there is nothing downstream that could match a policy by accident.
//
// SCOPE, STATED PLAINLY. The denial half is asserted on resolveViewerTier
// directly because the unrecognized rung is UNREACHABLE through
// resolveViewerIdentity by construction: that function returns one of exactly
// three tier constants. Driving the handler with a fabricated identity would
// require a seam that does not exist, and inventing one to make an end-to-end
// assertion possible would be testing the seam rather than the switch. The
// positive control IS driven end to end, so the pair still distinguishes "the
// default arm denies" from "this path denies everything".
func TestGetCharacterProfileDeniesAnUnrecognizedViewerTierWithNoPrincipalAtAll(t *testing.T) {
	t.Parallel()

	unrecognized := []viewerIdentity{
		{tier: "spectator", playerID: idgen.New().String()},
		{tier: "moderator", playerID: idgen.New().String()},
		{tier: "ANONYMOUS", playerID: ""},
		{tier: " anonymous", playerID: ""},
		{},
	}

	for _, id := range unrecognized {
		subject, ok := resolveViewerTier(id)
		assert.False(t, ok, "tier %q is outside the closed set and MUST be denied by the default arm", id.tier)
		assert.Empty(t, subject,
			"the default arm MUST produce no subject at all — a fabricated subject string would fail closed only by luck")
	}

	// Paired positive control: the same field, the same handler, at a rung the
	// switch does recognize.
	h := newProfileHarness(t, access.ViewerTierPlayer, []profileSeed{
		publicSeed("profile.biography", "Born in the salt flats."),
	})
	resp, err := h.read(t)
	require.NoError(t, err)
	assert.Equal(t, "Born in the salt flats.", resp.GetCharacter().GetProfile()["profile.biography"],
		"a RECOGNIZED rung publishes the same field the unrecognized ones are denied")
}

// TestGetCharacterProfileDeniesASystemVisibilityRowAtEveryRung covers the fifth
// value in entity_properties' visibility CHECK vocabulary
// (public|private|restricted|system|admin), which no §8.6 row discusses.
//
// It is denied because NO term-B policy matches it, not because anything
// enumerates it — which is the default-deny engine doing its job, and is the
// reason a new visibility value cannot publish itself by being added to the
// CHECK constraint alone.
func TestGetCharacterProfileDeniesASystemVisibilityRowAtEveryRung(t *testing.T) {
	t.Parallel()

	for _, tier := range []string{access.ViewerTierAnonymous, access.ViewerTierGuest, access.ViewerTierPlayer} {
		t.Run("the "+tier+" rung is denied a system-visibility row", func(t *testing.T) {
			t.Parallel()

			systemRow := profileSeed{
				id:         idgen.New(),
				name:       "profile.pronouns",
				value:      "they/them",
				visibility: "system",
			}
			resp, err := newProfileHarness(t, tier, []profileSeed{systemRow}).read(t)
			require.NoError(t, err)

			_, present := resp.GetCharacter().GetProfile()["profile.pronouns"]
			assert.False(t, present,
				"visibility=system matches no term-B permit, so the conjunction is false at every rung")

			// Paired positive control: the SAME name and the SAME value at the
			// public visibility, at the same rung. Only the row's visibility
			// differs, so the denial above is that column and nothing else.
			publicResp, err := newProfileHarness(t, tier, []profileSeed{
				publicSeed("profile.pronouns", "they/them"),
			}).read(t)
			require.NoError(t, err)
			assert.Equal(t, "they/them", publicResp.GetCharacter().GetProfile()["profile.pronouns"])
		})
	}
}

// TestGetCharacterProfileResolvesNameAndPronounsAtTheSeededReachabilityFloor is
// §8.8's minimum-identity floor, driven FROM THE CORPUS rather than from a
// hardcoded rung name: the floor is discovered by probing the ladder from the
// bottom, so raising seed:profile-reachable moves this test with it instead of
// turning it red for the wrong reason.
//
// THE BELOW-FLOOR LEG IS NOT ASSERTED HERE, and its absence is deliberate rather
// than an omission. In the shipped v0.13 corpus the reachability floor is the
// BOTTOM rung, so no rung below it exists to drive — a below-floor viewer can
// only be produced by narrowing the corpus, which the unit tier's full-corpus
// engine cannot do. That leg lives in
// test/integration/access/character_profile_read_test.go, where profileCorpusStore
// swaps the reachability permit for a guest-and-player-only variant and the
// anonymous rung genuinely falls below the floor.
func TestGetCharacterProfileResolvesNameAndPronounsAtTheSeededReachabilityFloor(t *testing.T) {
	t.Parallel()

	// The §8.2 ladder, LOWEST rung first. Order is the test's own, not an
	// ordinal the policies compare on — §8.2.1 makes clearing set membership.
	ladder := []string{access.ViewerTierAnonymous, access.ViewerTierGuest, access.ViewerTierPlayer}
	seeds := []profileSeed{publicSeed("profile.pronouns", "they/them")}

	floor := ""
	for _, tier := range ladder {
		if _, err := newProfileHarness(t, tier, seeds).read(t); err == nil {
			floor = tier
			break
		}
	}
	require.NotEmpty(t, floor, "some rung MUST clear the seeded reachability floor, or the surface is unreachable outright")
	assert.Equal(t, ladder[0], floor,
		"v0.13 seeds the reachability floor at the bottom rung; a change here means the corpus moved, not that this test broke")

	resp, err := newProfileHarness(t, floor, seeds).read(t)
	require.NoError(t, err)
	assert.Equal(t, profileTestCharacterName, resp.GetCharacter().GetName(),
		"a viewer EXACTLY at the floor receives the name")
	assert.Equal(t, "they/them", resp.GetCharacter().GetProfile()["profile.pronouns"],
		"...and the pronouns, which §8.8 makes the other half of the minimum public identity")
}
