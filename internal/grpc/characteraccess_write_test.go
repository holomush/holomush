// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/auth"
	authmocks "github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/world"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// This file drives the owner audience's two MUTATION surfaces.
//
// Two properties separate it from the owner READS next door and both are
// asserted here rather than assumed:
//
//   - the ownership denial is PermissionDenied, not the gate's own NotFound
//     (04-02's gate error matrix row 3), and it is UNIFORM across an
//     unparseable id, an id naming no row, and a well-formed id the caller does
//     not own;
//   - a mutation without a usable expected_version is REFUSED at the boundary,
//     before any domain call — which is why every fixture that must not write
//     wires a mutator double that fails the test the moment it is reached.

const writeTestToken = "write-surface-session-token"

// recordedProfileWrite is one observed UpdateCharacterProfileAttributes call.
//
// The caller is recorded BY VALUE for the same reason the owner reads record
// theirs: world.Caller's fields are unexported but the struct is comparable, so
// a spec can pin WHICH principal the facade built rather than merely that a
// write happened.
type recordedProfileWrite struct {
	caller          world.Caller
	characterID     ulid.ULID
	expectedVersion int
	attrs           map[string]string
}

// recordedDescriptionWrite is one observed UpdateCharacterDescription call.
type recordedDescriptionWrite struct {
	caller      world.Caller
	characterID ulid.ULID
	description string
}

// recordingWorldMutator is the domain double behind characterAccessWorldMutator.
//
// failOnCall is the load-bearing mode: several behaviors here are "the handler
// returns BEFORE any domain write", and a double that quietly recorded the call
// and returned nil would let a regression that does write pass as a success.
type recordingWorldMutator struct {
	t          *testing.T
	failOnCall bool
	profileErr error
	descErr    error

	profileWrites []recordedProfileWrite
	descWrites    []recordedDescriptionWrite
}

func (m *recordingWorldMutator) UpdateCharacterProfileAttributes(_ context.Context, caller world.Caller, characterID ulid.ULID, expectedVersion int, attributes map[string]string) error {
	m.t.Helper()
	if m.failOnCall {
		m.t.Fatal("the handler MUST NOT reach the domain profile write on this path")
	}
	copied := make(map[string]string, len(attributes))
	for k, v := range attributes {
		copied[k] = v
	}
	m.profileWrites = append(m.profileWrites, recordedProfileWrite{
		caller:          caller,
		characterID:     characterID,
		expectedVersion: expectedVersion,
		attrs:           copied,
	})
	return m.profileErr
}

func (m *recordingWorldMutator) UpdateCharacterDescription(_ context.Context, caller world.Caller, characterID ulid.ULID, description string) error {
	m.t.Helper()
	if m.failOnCall {
		m.t.Fatal("the handler MUST NOT reach the domain description write on this path")
	}
	m.descWrites = append(m.descWrites, recordedDescriptionWrite{
		caller:      caller,
		characterID: characterID,
		description: description,
	})
	return m.descErr
}

// writeHarness is one wired facade plus the fixture identifiers a mutation spec
// drives it with.
type writeHarness struct {
	srv      *CharacterAccessServer
	reader   *ownerWorldReader
	mutator  *recordingWorldMutator
	owned    *world.Character
	playerID ulid.ULID
	token    string
}

// writeFixture describes the caller and the domain doubles a spec wants.
type writeFixture struct {
	// isGuest makes the resolved player a guest, so resolveAndGate denies.
	isGuest bool
	// owned is the roster charRepo.ListByPlayer returns. When nil the harness
	// seeds exactly one owned character.
	owned []*world.Character
	// listErr fails the roster lookup — the gate's Internal branch.
	listErr error
	// rows is what the post-write property enumeration returns.
	rows []*world.EntityProperty
	// failOnWrite makes every mutator method fail the test on call.
	failOnWrite bool
	// profileErr / descErr are what the mutator returns.
	profileErr error
	descErr    error
}

func newWriteHarness(t *testing.T, f writeFixture) *writeHarness {
	t.Helper()

	playerID := idgen.New()
	ps := &auth.PlayerSession{ID: idgen.New(), PlayerID: playerID, TokenHash: auth.HashSessionToken(writeTestToken)}

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, ps.TokenHash).Return(ps, nil).Maybe()
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, mock.Anything).
		Return(nil, oops.Code("PLAYER_SESSION_NOT_FOUND").Errorf("no such session")).Maybe()
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, ps.ID, auth.PlayerSessionTTL).Return(nil).Maybe()

	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).
		Return(&auth.Player{ID: playerID, IsGuest: f.isGuest}, nil).Maybe()

	owned := f.owned
	if owned == nil {
		owned = []*world.Character{ownedCharacterFixture(playerID, "Ada", world.StatusActive)}
	}

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return(owned, f.listErr).Maybe()

	reader := &ownerWorldReader{
		rows: f.rows,
		desc: world.CharacterDescription{Name: profileTestCharacterName, Description: profileTestDescription},
	}
	mutator := &recordingWorldMutator{
		t:          t,
		failOnCall: f.failOnWrite,
		profileErr: f.profileErr,
		descErr:    f.descErr,
	}

	var first *world.Character
	if len(owned) > 0 {
		first = owned[0]
	}

	return &writeHarness{
		srv:      NewCharacterAccessServer(reader, mutator, &failOnCallProfileVisibility{t: t}, sessionRepo, playerRepo, charRepo),
		reader:   reader,
		mutator:  mutator,
		owned:    first,
		playerID: playerID,
		token:    writeTestToken,
	}
}

// profileRequest builds a well-formed guarded profile edit over the given mask
// paths. Individual specs override fields afterwards.
func (h *writeHarness) profileRequest(paths ...string) *characteraccessv1.UpdateCharacterProfileRequest {
	return &characteraccessv1.UpdateCharacterProfileRequest{
		CharacterId:        h.owned.ID.String(),
		PlayerSessionToken: h.token,
		ExpectedVersion:    int32(h.owned.Version), //nolint:gosec // characters.version is a 32-bit INTEGER column
		UpdateMask:         &fieldmaskpb.FieldMask{Paths: paths},
	}
}

// TestUpdateCharacterProfileAppliesATwoPathMaskAndReturnsTheUpdatedOwnCharacter
// is behavior 1.
func TestUpdateCharacterProfileAppliesATwoPathMaskAndReturnsTheUpdatedOwnCharacter(t *testing.T) {
	t.Parallel()

	pronouns := "they/them"
	h := newWriteHarness(t, writeFixture{rows: []*world.EntityProperty{
		{ID: idgen.New(), Name: "profile.pronouns", Value: &pronouns},
	}})

	req := h.profileRequest("profile.pronouns", "profile.concept")
	req.Pronouns = "they/them"
	req.Concept = "a wandering archivist"
	// A field OUTSIDE the mask is immaterial and MUST NOT be written.
	req.Biography = "this must never reach the domain"

	resp, err := h.srv.UpdateCharacterProfile(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.GetCharacter())
	assert.Equal(t, h.owned.ID.String(), resp.GetCharacter().GetId())

	require.Len(t, h.mutator.profileWrites, 1, "exactly one domain write per accepted request")
	got := h.mutator.profileWrites[0]
	assert.Equal(t, map[string]string{
		"profile.pronouns": "they/them",
		"profile.concept":  "a wandering archivist",
	}, got.attrs, "only the masked paths are written; an unmasked field is immaterial")
	assert.Equal(t, h.owned.ID, got.characterID)
	assert.Equal(t, world.HumanCaller(access.CharacterSubject(h.owned.ID.String())), got.caller,
		"the write is driven with the OWNING CHARACTER's subject, never a viewer principal")
}

// TestUpdateCharacterProfileRejectsAMaskPathOutsideTheAllowlist is behavior 2.
func TestUpdateCharacterProfileRejectsAMaskPathOutsideTheAllowlist(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"role", "name", "status", "version", "profile.nickname"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			h := newWriteHarness(t, writeFixture{failOnWrite: true})

			resp, err := h.srv.UpdateCharacterProfile(context.Background(), h.profileRequest(path))
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

// TestUpdateCharacterProfileRejectsAContainerPrefixRatherThanExpandingIt is
// behavior 3 — 01-SPEC §9.5 rule 2: `profile` MUST NOT reach
// `profile.rp_preferences`.
func TestUpdateCharacterProfileRejectsAContainerPrefixRatherThanExpandingIt(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"profile", "profile.", "profile.rp_preferences.tone"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			h := newWriteHarness(t, writeFixture{failOnWrite: true})

			_, err := h.srv.UpdateCharacterProfile(context.Background(), h.profileRequest(path))
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err),
				"paths are matched as exact strings — no prefix, glob or dotted-subtree expansion")
		})
	}
}

// TestUpdateCharacterProfileTreatsAnEmptyMaskAsANoOpSuccessAfterOwnership is
// behavior 4. The short-circuit sits AFTER ownership, so a no-op cannot be used
// as an existence oracle.
func TestUpdateCharacterProfileTreatsAnEmptyMaskAsANoOpSuccessAfterOwnership(t *testing.T) {
	t.Parallel()

	t.Run("an owner's empty mask succeeds and writes nothing", func(t *testing.T) {
		t.Parallel()
		h := newWriteHarness(t, writeFixture{failOnWrite: true})

		resp, err := h.srv.UpdateCharacterProfile(context.Background(), h.profileRequest())
		require.NoError(t, err)
		require.NotNil(t, resp.GetCharacter())
		assert.Equal(t, h.owned.ID.String(), resp.GetCharacter().GetId())
	})

	t.Run("a non-owner's empty mask is denied exactly like any other mutation", func(t *testing.T) {
		t.Parallel()
		h := newWriteHarness(t, writeFixture{failOnWrite: true})

		req := h.profileRequest()
		req.CharacterId = idgen.New().String()

		_, err := h.srv.UpdateCharacterProfile(context.Background(), req)
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
		assert.Equal(t, characterNotOwnedMessage, status.Convert(err).Message(),
			"a no-op discloses nothing about the row")
	})
}

// TestUpdateCharacterProfileDeniesEveryOwnershipCauseUniformly is behavior 5
// (review finding MEDIUM-2). Raising the status above NotFound costs no opacity
// precisely because the three causes are indistinguishable on the wire.
func TestUpdateCharacterProfileDeniesEveryOwnershipCauseUniformly(t *testing.T) {
	t.Parallel()

	h := newWriteHarness(t, writeFixture{failOnWrite: true})

	unparseable := h.profileRequest("profile.pronouns")
	unparseable.CharacterId = "not-a-ulid"

	noSuchRow := h.profileRequest("profile.pronouns")
	noSuchRow.CharacterId = idgen.New().String()

	otherPlayersCharacter := h.profileRequest("profile.pronouns")
	otherPlayersCharacter.CharacterId = ownedCharacterFixture(idgen.New(), "Zed", world.StatusActive).ID.String()

	_, unparseableErr := h.srv.UpdateCharacterProfile(context.Background(), unparseable)
	_, noSuchRowErr := h.srv.UpdateCharacterProfile(context.Background(), noSuchRow)
	_, notOwnedErr := h.srv.UpdateCharacterProfile(context.Background(), otherPlayersCharacter)

	require.Error(t, unparseableErr)
	require.Error(t, noSuchRowErr)
	require.Error(t, notOwnedErr)

	assert.Equal(t, codes.PermissionDenied, status.Code(unparseableErr))
	assert.Equal(t, codes.PermissionDenied, status.Code(noSuchRowErr))
	assert.Equal(t, codes.PermissionDenied, status.Code(notOwnedErr))

	unparseableMsg := status.Convert(unparseableErr).Message()
	noSuchRowMsg := status.Convert(noSuchRowErr).Message()
	notOwnedMsg := status.Convert(notOwnedErr).Message()
	require.Equal(t, unparseableMsg, noSuchRowMsg, "an unparseable id and one naming no row are wire-identical")
	require.Equal(t, noSuchRowMsg, notOwnedMsg, "and both are identical to a character the caller does not own")

	assert.NotContains(t, notOwnedMsg, "CHARACTER_NOT_OWNED",
		"the internal code never reaches the wire message")
}

// TestUpdateCharacterProfilePropagatesTheGatesInternalFailureVerbatim is row 4
// of the gate error matrix: an infrastructure failure is NOT an authorization
// outcome and must survive ownedCharacterForMutation's rewrite intact.
func TestUpdateCharacterProfilePropagatesTheGatesInternalFailureVerbatim(t *testing.T) {
	t.Parallel()

	h := newWriteHarness(t, writeFixture{
		failOnWrite: true,
		listErr:     oops.Code("CHARACTER_LIST_FAILED").Errorf("roster lookup exploded"),
	})

	_, err := h.srv.UpdateCharacterProfile(context.Background(), h.profileRequest("profile.pronouns"))
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err),
		"an outage must not be laundered into a PermissionDenied that reads as a policy answer")
	assert.NotEqual(t, characterNotOwnedMessage, status.Convert(err).Message())
}

// TestUpdateCharacterProfileMaskIsOrderIndependentAndDuplicatesAreIdempotent is
// behavior 6 (01-SPEC §9.5 rule 3).
func TestUpdateCharacterProfileMaskIsOrderIndependentAndDuplicatesAreIdempotent(t *testing.T) {
	t.Parallel()

	canonical := newWriteHarness(t, writeFixture{})
	shuffled := newWriteHarness(t, writeFixture{})

	build := func(h *writeHarness, paths ...string) *characteraccessv1.UpdateCharacterProfileRequest {
		req := h.profileRequest(paths...)
		req.Pronouns = "she/her"
		req.Concept = "a lighthouse keeper"
		req.Species = "human"
		return req
	}

	_, err := canonical.srv.UpdateCharacterProfile(context.Background(),
		build(canonical, "profile.pronouns", "profile.concept", "profile.species"))
	require.NoError(t, err)

	_, err = shuffled.srv.UpdateCharacterProfile(context.Background(),
		build(shuffled, "profile.species", "profile.concept", "profile.pronouns", "profile.concept"))
	require.NoError(t, err)

	require.Len(t, canonical.mutator.profileWrites, 1)
	require.Len(t, shuffled.mutator.profileWrites, 1)
	assert.Equal(t, canonical.mutator.profileWrites[0].attrs, shuffled.mutator.profileWrites[0].attrs,
		"the mask is a SET: reordering and duplication change nothing")
}

// TestUpdateCharacterProfileEnforcesByteMeasuredCapsAtTheBoundary is behavior 7.
func TestUpdateCharacterProfileEnforcesByteMeasuredCapsAtTheBoundary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		path     string
		size     int
		accepted bool
	}{
		{"a short field at exactly the cap", "profile.pronouns", world.MaxNameLength, true},
		{"a short field one byte past the cap", "profile.pronouns", world.MaxNameLength + 1, false},
		{"a long field at exactly the cap", "profile.biography", world.MaxDescriptionLength, true},
		{"a long field one byte past the cap", "profile.biography", world.MaxDescriptionLength + 1, false},
		// The long cap does NOT leak onto a short field.
		{"a short field at the long cap", "profile.pronouns", world.MaxDescriptionLength, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newWriteHarness(t, writeFixture{failOnWrite: !tc.accepted})

			req := h.profileRequest(tc.path)
			value := strings.Repeat("a", tc.size)
			req.Pronouns = value
			req.Biography = value

			_, err := h.srv.UpdateCharacterProfile(context.Background(), req)
			if tc.accepted {
				require.NoError(t, err)
				require.Len(t, h.mutator.profileWrites, 1)
				assert.Len(t, h.mutator.profileWrites[0].attrs[tc.path], tc.size)
				return
			}
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

// TestUpdateCharacterProfileMeasuresCapsInBytesNotRunes is behavior 8. The
// domain rule (world.ValidateDescription) is byte-measured, so the facade must
// be too or the two disagree about where the boundary is.
func TestUpdateCharacterProfileMeasuresCapsInBytesNotRunes(t *testing.T) {
	t.Parallel()

	// "é" is two bytes: 60 runes is well under the 100-RUNE reading of the cap
	// and 120 bytes is over the 100-BYTE one.
	value := strings.Repeat("é", 60)
	require.Less(t, len([]rune(value)), world.MaxNameLength)
	require.Greater(t, len(value), world.MaxNameLength)

	h := newWriteHarness(t, writeFixture{failOnWrite: true})
	req := h.profileRequest("profile.pronouns")
	req.Pronouns = value

	_, err := h.srv.UpdateCharacterProfile(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestUpdateCharacterProfileRejectsMalformedProseIsBehaviorNine is behavior 9.
//
// The control-character fixture is deliberately NOT "\r":
// world.hasControlCharsExceptWhitespace permits carriage return alongside
// newline and tab (validation.go:191), so a "\r" fixture would be permanently
// RED against correct code.
func TestUpdateCharacterProfileRejectsMalformedProseIsBehaviorNine(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		value string
	}{
		{"invalid UTF-8", string([]byte{0xff, 0xfe, 0x41})},
		{"an ANSI escape", "\x1b[31mred"},
		{"a BEL", "ring\x07ring"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newWriteHarness(t, writeFixture{failOnWrite: true})

			req := h.profileRequest("profile.biography")
			req.Biography = tc.value

			_, err := h.srv.UpdateCharacterProfile(context.Background(), req)
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}

	t.Run("newline, carriage return and tab are all legal prose", func(t *testing.T) {
		t.Parallel()
		h := newWriteHarness(t, writeFixture{})

		req := h.profileRequest("profile.biography")
		req.Biography = "first line\r\nsecond line\tindented"

		_, err := h.srv.UpdateCharacterProfile(context.Background(), req)
		require.NoError(t, err)
		require.Len(t, h.mutator.profileWrites, 1)
	})
}

// TestUpdateCharacterProfileRejectsAnUnguardedWriteBeforeAnyDomainCall is
// behavior 10. Absent and explicit zero take the SAME branch: proto3 cannot tell
// them apart and neither is ever an instruction to write without the guard.
func TestUpdateCharacterProfileRejectsAnUnguardedWriteBeforeAnyDomainCall(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		version int32
	}{
		{"absent", 0},
		{"explicitly zero", 0},
		{"negative", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newWriteHarness(t, writeFixture{failOnWrite: true})

			req := h.profileRequest("profile.pronouns")
			req.ExpectedVersion = tc.version
			req.Pronouns = "they/them"

			_, err := h.srv.UpdateCharacterProfile(context.Background(), req)
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.Empty(t, h.mutator.profileWrites)
		})
	}
}

// TestUpdateCharacterProfileSurfacesAStaleVersionAsAbortedWithoutRetrying is
// behavior 11. The profile path's guard is a GENUINE CAS: the caller's version
// is threaded verbatim into the domain command's version-predicated UPDATE, so
// this spec also pins that threading against a future refactor that drops it.
func TestUpdateCharacterProfileSurfacesAStaleVersionAsAbortedWithoutRetrying(t *testing.T) {
	t.Parallel()

	conflict := oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit)
	h := newWriteHarness(t, writeFixture{profileErr: conflict})

	req := h.profileRequest("profile.pronouns")
	req.ExpectedVersion = 3
	req.Pronouns = "they/them"

	_, err := h.srv.UpdateCharacterProfile(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.Aborted, status.Code(err))

	require.Len(t, h.mutator.profileWrites, 1, "the loser is surfaced, never retried")
	assert.Equal(t, 3, h.mutator.profileWrites[0].expectedVersion,
		"the CALLER's expected_version reaches the domain command verbatim")
}

// TestUpdateCharacterProfileDeniesAGuestAndAnUnauthenticatedCaller is behavior
// 12 — the shared gate's two denials, reached through the mutation surface.
func TestUpdateCharacterProfileDeniesAGuestAndAnUnauthenticatedCaller(t *testing.T) {
	t.Parallel()

	t.Run("a guest", func(t *testing.T) {
		t.Parallel()
		h := newWriteHarness(t, writeFixture{isGuest: true, failOnWrite: true})

		_, err := h.srv.UpdateCharacterProfile(context.Background(), h.profileRequest("profile.pronouns"))
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
		assert.Equal(t, characterGuestDenialMessage, status.Convert(err).Message())
	})

	t.Run("an unresolvable session", func(t *testing.T) {
		t.Parallel()
		h := newWriteHarness(t, writeFixture{failOnWrite: true})

		req := h.profileRequest("profile.pronouns")
		req.PlayerSessionToken = "not-a-real-session-token"

		_, err := h.srv.UpdateCharacterProfile(context.Background(), req)
		require.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})
}

// TestUpdateCharacterProfileMaskablePathsAreExactlyTheTwelve pins the allowlist
// against the domain's own closed set drifting apart from it. The two are
// deliberately duplicated (04-09: the facade allowlist is a genuine second gate,
// not an alias), so a spec that only exercised one would not notice a divergence.
func TestUpdateCharacterProfileMaskablePathsAreExactlyTheTwelve(t *testing.T) {
	t.Parallel()

	require.Len(t, updateCharacterProfileMaskablePaths, 12,
		"01-SPEC §7.2 declares exactly twelve prose profile fields")

	for path, field := range updateCharacterProfileMaskablePaths {
		assert.True(t, strings.HasPrefix(path, "profile."),
			"%s: mask paths are the §7.2 property names verbatim", path)
		assert.NotNil(t, field.value, "%s: every path resolves to a request accessor", path)
		assert.Contains(t, []int{world.MaxNameLength, world.MaxDescriptionLength}, field.maxBytes,
			"%s: caps reuse the shipped world constants rather than minting new numbers", path)
	}
}
