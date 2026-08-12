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

// recordedDefaultWrite is one observed PlayerRepository.UpdateDefaultCharacter
// call. Both arguments are recorded, not just the character: a handler that
// wrote the RIGHT character onto the WRONG player's row would satisfy an
// assertion that looked only at the target.
type recordedDefaultWrite struct {
	playerID    ulid.ULID
	characterID ulid.ULID
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
	// defaultWrites is every UpdateDefaultCharacter call the facade made,
	// in order.
	defaultWrites *[]recordedDefaultWrite
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
	// vanishAfterWrite makes the SECOND roster lookup — the POST-COMMIT
	// re-read — return an empty roster, modelling a concurrent DeleteCharacter
	// or reap landing in the window after the domain write committed. The first
	// lookup, the pre-write ownership gate, still succeeds.
	vanishAfterWrite bool
	// postWriteListErr fails the SECOND roster lookup with a repository error,
	// the post-commit counterpart of listErr.
	postWriteListErr error
	// defaultWriteErr is what the player repository's narrow
	// default_character_id write returns.
	defaultWriteErr error
	// failOnDefaultWrite leaves UpdateDefaultCharacter UNREGISTERED on the
	// player-repository mock. Reaching it then fails the test through mockery's
	// unexpected-call panic, which is what several specs here need: their
	// behavior is "the handler refuses BEFORE any write", and a double that
	// quietly returned nil would let a regression that DOES write pass.
	failOnDefaultWrite bool
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

	defaultWrites := &[]recordedDefaultWrite{}
	if !f.failOnDefaultWrite {
		playerRepo.EXPECT().UpdateDefaultCharacter(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(func(_ context.Context, pid, characterID ulid.ULID) error {
				*defaultWrites = append(*defaultWrites, recordedDefaultWrite{playerID: pid, characterID: characterID})
				return f.defaultWriteErr
			}).Maybe()
	}

	owned := f.owned
	if owned == nil {
		owned = []*world.Character{ownedCharacterFixture(playerID, "Ada", world.StatusActive)}
	}

	charRepo := authmocks.NewMockCharacterRepository(t)
	switch {
	case f.vanishAfterWrite:
		charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return(owned, nil).Once()
		charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return(nil, nil).Maybe()
	case f.postWriteListErr != nil:
		charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return(owned, nil).Once()
		charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return(nil, f.postWriteListErr).Maybe()
	default:
		charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return(owned, f.listErr).Maybe()
	}

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
		srv:           NewCharacterAccessServer(reader, mutator, &failOnCallProfileVisibility{t: t}, &failOnCallDirectoryGate{t: t}, &failOnCallDirectoryReader{t: t}, sessionRepo, playerRepo, charRepo, &failOnCallCreator{t: t}),
		reader:        reader,
		mutator:       mutator,
		owned:         first,
		playerID:      playerID,
		token:         writeTestToken,
		defaultWrites: defaultWrites,
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

// descriptionRequest builds a well-formed guarded description edit.
func (h *writeHarness) descriptionRequest(description string) *characteraccessv1.UpdateCharacterDescriptionRequest {
	return &characteraccessv1.UpdateCharacterDescriptionRequest{
		CharacterId:        h.owned.ID.String(),
		PlayerSessionToken: h.token,
		ExpectedVersion:    int32(h.owned.Version), //nolint:gosec // characters.version is a 32-bit INTEGER column
		Description:        description,
	}
}

// TestUpdateCharacterDescriptionReachesTheShippedWorldCommand is behaviors 1 and
// 2: the handler drives world.Service.UpdateCharacterDescription — the command
// that carries the same-transaction outbox emission and, as of issue #4954, the
// validation — rather than a repository or a parallel write path.
func TestUpdateCharacterDescriptionReachesTheShippedWorldCommand(t *testing.T) {
	t.Parallel()

	h := newWriteHarness(t, writeFixture{})

	resp, err := h.srv.UpdateCharacterDescription(context.Background(),
		h.descriptionRequest("A tall figure wrapped in a travelling cloak."))
	require.NoError(t, err)
	require.NotNil(t, resp.GetCharacter())
	assert.Equal(t, h.owned.ID.String(), resp.GetCharacter().GetId())

	require.Len(t, h.mutator.descWrites, 1, "exactly one domain command call")
	assert.Empty(t, h.mutator.profileWrites, "the description is a COLUMN, not a profile.* row")
	got := h.mutator.descWrites[0]
	assert.Equal(t, "A tall figure wrapped in a travelling cloak.", got.description)
	assert.Equal(t, h.owned.ID, got.characterID)
	assert.Equal(t, world.HumanCaller(access.CharacterSubject(h.owned.ID.String())), got.caller,
		"the subject string becomes the outbox envelope actor")
}

// TestUpdateCharacterDescriptionAcceptsAnEmptyDescription: clearing the column is
// a supported edit. The domain validator returns early for the empty string.
func TestUpdateCharacterDescriptionAcceptsAnEmptyDescription(t *testing.T) {
	t.Parallel()

	h := newWriteHarness(t, writeFixture{})

	_, err := h.srv.UpdateCharacterDescription(context.Background(), h.descriptionRequest(""))
	require.NoError(t, err)
	require.Len(t, h.mutator.descWrites, 1)
	assert.Empty(t, h.mutator.descWrites[0].description)
}

// TestUpdateCharacterDescriptionConvertsTheDomainsValidationRejection is
// behavior 3, and the emphasis is on CONVERTS.
//
// The cap, the UTF-8 rule and the control-character rule are enforced by the
// DOMAIN (issue #4954, closed in this plan's Task 2). At this tier the handler's
// only job is to render that rejection as codes.InvalidArgument with a generic
// message. There is deliberately NO unit test asserting the handler itself
// rejects a 4001-byte value against a double — that would be asserting a
// facade-side check this plan forbids. The boundary cases run at the integration
// tier, against the real world.Service.
func TestUpdateCharacterDescriptionConvertsTheDomainsValidationRejection(t *testing.T) {
	t.Parallel()

	inner := &world.ValidationError{Field: "description", Message: "exceeds maximum length of 4000"}
	h := newWriteHarness(t, writeFixture{
		descErr: oops.Code(world.CodeCharacterInvalid).Wrap(inner),
	})

	_, err := h.srv.UpdateCharacterDescription(context.Background(), h.descriptionRequest("anything"))
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	msg := status.Convert(err).Message()
	assert.Equal(t, characterValueInvalidMessage, msg)
	assert.NotContains(t, msg, "exceeds maximum length",
		"the inner error is never interpolated into the wire message")
	assert.NotContains(t, msg, world.CodeCharacterInvalid)
}

// TestUpdateCharacterDescriptionDeniesEveryOwnershipCauseUniformly pins the
// gate error matrix row 3 on the SECOND mutation surface. Both mutations reach
// it through the one ownedCharacterForMutation wrapper, so this is a check that
// neither handler grew its own resolution.
func TestUpdateCharacterDescriptionDeniesEveryOwnershipCauseUniformly(t *testing.T) {
	t.Parallel()

	h := newWriteHarness(t, writeFixture{failOnWrite: true})

	unparseable := h.descriptionRequest("x")
	unparseable.CharacterId = "not-a-ulid"
	noSuchRow := h.descriptionRequest("x")
	noSuchRow.CharacterId = idgen.New().String()

	_, unparseableErr := h.srv.UpdateCharacterDescription(context.Background(), unparseable)
	_, noSuchRowErr := h.srv.UpdateCharacterDescription(context.Background(), noSuchRow)
	require.Error(t, unparseableErr)
	require.Error(t, noSuchRowErr)

	assert.Equal(t, codes.PermissionDenied, status.Code(unparseableErr))
	assert.Equal(t, codes.PermissionDenied, status.Code(noSuchRowErr))
	require.Equal(t, status.Convert(unparseableErr).Message(), status.Convert(noSuchRowErr).Message())
	assert.Equal(t, characterNotOwnedMessage, status.Convert(noSuchRowErr).Message())
}

// TestUpdateCharacterDescriptionRejectsAnUnguardedWriteBeforeAnyDomainCall is
// behavior 7.
func TestUpdateCharacterDescriptionRejectsAnUnguardedWriteBeforeAnyDomainCall(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		version int32
	}{
		{"absent or explicitly zero", 0},
		{"negative", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newWriteHarness(t, writeFixture{failOnWrite: true})

			req := h.descriptionRequest("a new description")
			req.ExpectedVersion = tc.version

			_, err := h.srv.UpdateCharacterDescription(context.Background(), req)
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.Empty(t, h.mutator.descWrites)
		})
	}
}

// TestUpdateCharacterDescriptionRejectsAStaleVersionBeforeTheDomainCall is
// behavior 8.
//
// The shipped domain command takes NO expectedVersion parameter, so this compare
// is the ONLY place a stale client version is detected — see the documenting
// test below for what that does and does not buy.
func TestUpdateCharacterDescriptionRejectsAStaleVersionBeforeTheDomainCall(t *testing.T) {
	t.Parallel()

	h := newWriteHarness(t, writeFixture{failOnWrite: true})
	require.Equal(t, 7, h.owned.Version, "the fixture's stored version")

	req := h.descriptionRequest("a new description")
	req.ExpectedVersion = 3

	_, err := h.srv.UpdateCharacterDescription(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.Aborted, status.Code(err))
	assert.Equal(t, characterConcurrentEditMessage, status.Convert(err).Message())
	assert.Empty(t, h.mutator.descWrites, "a stale client never reaches the domain")
}

// TestUpdateCharacterDescriptionDoesNotDetectAWriterInsideTheReadToReReadWindow
// names the LIMIT of the description path's guard, so it is pinned as known
// behavior rather than mistaken for a guarantee.
//
// world.Service.UpdateCharacterDescription takes no expectedVersion parameter:
// it re-reads the row and uses its OWN freshly-read char.Version as the CAS
// guard. The facade's compare is therefore a TOCTOU NARROWING, not a guarantee —
// it catches a client submitting an out-of-date version (the common real case, a
// stale edit form) and does NOT catch a writer landing between the facade's read
// and the domain's re-read. That writer still last-write-wins.
//
// Below, two writers both read version 7 and both submit it. Both are admitted
// and BOTH reach the domain, so the second silently overwrites the first — the
// facade compares only against the version it read, and nothing it sends
// constrains the domain's write. Closing the window is issue #4956 (extend the
// domain signature); this test must be REPLACED, not deleted, when that lands.
func TestUpdateCharacterDescriptionDoesNotDetectAWriterInsideTheReadToReReadWindow(t *testing.T) {
	t.Parallel()

	h := newWriteHarness(t, writeFixture{})

	_, firstErr := h.srv.UpdateCharacterDescription(context.Background(), h.descriptionRequest("written by the first writer"))
	_, secondErr := h.srv.UpdateCharacterDescription(context.Background(), h.descriptionRequest("written by the second writer"))

	require.NoError(t, firstErr)
	require.NoError(t, secondErr, "the second writer is NOT refused — this is the window, not a bug in the test")
	require.Len(t, h.mutator.descWrites, 2, "both writers reach the domain")
	assert.Equal(t, "written by the second writer", h.mutator.descWrites[1].description,
		"last write wins inside the read-to-re-read window (issue #4956)")
}

// TestMutationsReportAPostCommitReReadFailureAsInternalNeverAsAnOwnershipRefusal
// pins the post-COMMIT leg's error mapping.
//
// ownerMutationResponse runs only AFTER the domain write returned nil, so no
// branch of it may claim an authorization outcome. Routing it through
// ownedCharacterForMutation — whose PermissionDenied rewrite is correct for the
// PRE-write gate, where all three ownership causes genuinely collapse — told a
// client "you do not own this character" for an edit that LANDED, and a client
// reacting by retrying with the version it still holds then gets Aborted. Both
// natural recoveries point away from the truth.
//
// The two failure shapes are driven separately because they arrive from the gate
// with different codes: the row vanishing in the window arrives as NotFound (the
// gate does not log it), a repository outage as Internal (the gate does).
func TestMutationsReportAPostCommitReReadFailureAsInternalNeverAsAnOwnershipRefusal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		fixture writeFixture
	}{
		{"the character row vanished in the window after the write committed", writeFixture{vanishAfterWrite: true}},
		{"the roster repository failed after the write committed", writeFixture{postWriteListErr: oops.Code("REPO_DOWN").Errorf("roster unavailable")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			t.Run("UpdateCharacterProfile", func(t *testing.T) {
				t.Parallel()
				h := newWriteHarness(t, tc.fixture)
				req := h.profileRequest("profile.pronouns")
				req.Pronouns = "they/them"

				_, err := h.srv.UpdateCharacterProfile(context.Background(), req)

				require.Error(t, err)
				assert.Equal(t, codes.Internal, status.Code(err),
					"the write COMMITTED; only the read-back failed, and an outage is not a policy answer (§8.10)")
				assert.NotEqual(t, codes.PermissionDenied, status.Code(err),
					"a committed edit must never be reported as an ownership refusal")
				assert.NotEqual(t, characterNotOwnedMessage, status.Convert(err).Message())
				require.Len(t, h.mutator.profileWrites, 1,
					"the domain write must have been reached — otherwise this spec proves nothing about the POST-commit leg")
			})

			t.Run("UpdateCharacterDescription", func(t *testing.T) {
				t.Parallel()
				h := newWriteHarness(t, tc.fixture)

				_, err := h.srv.UpdateCharacterDescription(context.Background(),
					h.descriptionRequest("a tall figure in a travelling cloak"))

				require.Error(t, err)
				assert.Equal(t, codes.Internal, status.Code(err))
				assert.NotEqual(t, characterNotOwnedMessage, status.Convert(err).Message())
				require.Len(t, h.mutator.descWrites, 1,
					"the domain write must have been reached — otherwise this spec proves nothing about the POST-commit leg")
			})
		})
	}
}

// The default-character write's refusal paths (IDENT-05).
//
// EVERY DENIAL BELOW IS ASSERTED AT THE WIRE, and every one carries a permitted
// twin on the SAME fixture (PORTAL-10 rule 2). Two consequences for how these
// read:
//
//   - status.Code(err) and status.Convert(err).Message() are the assertions.
//     errutil.AssertErrorCode and oops.AsOops(err).Code() both resolve the
//     DEEPEST code in the chain under the pinned samber/oops v1.22.0 (#4902), so
//     neither says anything about what the caller actually received. They are
//     not used here.
//   - a denial with no twin proves only that SOMETHING was refused. A handler
//     that refused every request would satisfy it. The twin is what separates
//     "this rule fires" from "nothing works".

// setDefaultRequest builds a well-formed default-character request for one id.
func (h *writeHarness) setDefaultRequest(characterID string) *characteraccessv1.SetDefaultCharacterRequest {
	return &characteraccessv1.SetDefaultCharacterRequest{
		CharacterId:        characterID,
		PlayerSessionToken: h.token,
	}
}

// TestSetDefaultCharacterDeniesAGuestAndPermitsTheSameFixturesOwner is the
// guest gate reached through this surface, with its paired positive control.
func TestSetDefaultCharacterDeniesAGuestAndPermitsTheSameFixturesOwner(t *testing.T) {
	t.Parallel()

	t.Run("denies a guest, before any write", func(t *testing.T) {
		t.Parallel()
		h := newWriteHarness(t, writeFixture{isGuest: true, failOnDefaultWrite: true})

		resp, err := h.srv.SetDefaultCharacter(context.Background(), h.setDefaultRequest(h.owned.ID.String()))
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
		assert.Equal(t, characterGuestDenialMessage, status.Convert(err).Message(),
			"the CHARACTER facade's own guest message — a caller here never touched the scene subsystem")
	})

	t.Run("permits the same fixture's non-guest owner", func(t *testing.T) {
		t.Parallel()
		h := newWriteHarness(t, writeFixture{})

		resp, err := h.srv.SetDefaultCharacter(context.Background(), h.setDefaultRequest(h.owned.ID.String()))
		require.NoError(t, err)
		require.Len(t, resp.GetCharacters(), 1, "the response is the caller's roster, not an acknowledgement")
		assert.Equal(t, h.owned.ID.String(), resp.GetCharacters()[0].GetId())

		require.Len(t, *h.defaultWrites, 1, "exactly one narrow write per accepted request")
		assert.Equal(t, h.playerID, (*h.defaultWrites)[0].playerID)
		assert.Equal(t, h.owned.ID, (*h.defaultWrites)[0].characterID)
	})
}

// TestSetDefaultCharacterCollapsesAnUnparseableAndANotOwnedIdOntoOneOutcome
// asserts the two refusals are byte-identical, in ONE test body so a future edit
// that gives either leg its own literal fails here rather than passing two
// independently-green specs.
//
// The comparison is the point: the ids are compared as ULIDs, never as names, so
// "not a ULID at all" and "a real ULID belonging to someone else" must be
// indistinguishable — otherwise a caller learns which character ids parse and,
// worse, which exist.
func TestSetDefaultCharacterCollapsesAnUnparseableAndANotOwnedIdOntoOneOutcome(t *testing.T) {
	t.Parallel()

	h := newWriteHarness(t, writeFixture{failOnDefaultWrite: true})

	_, unparseableErr := h.srv.SetDefaultCharacter(context.Background(), h.setDefaultRequest("Ada"))
	require.Error(t, unparseableErr)

	_, notOwnedErr := h.srv.SetDefaultCharacter(context.Background(), h.setDefaultRequest(idgen.New().String()))
	require.Error(t, notOwnedErr)

	assert.Equal(t, codes.PermissionDenied, status.Code(unparseableErr))
	assert.Equal(t, status.Code(notOwnedErr), status.Code(unparseableErr),
		"a malformed id and a well-formed id the caller does not own take the SAME status")
	assert.Equal(t, characterNotOwnedMessage, status.Convert(unparseableErr).Message())
	assert.Equal(t, status.Convert(notOwnedErr).Message(), status.Convert(unparseableErr).Message(),
		"and the SAME message — a divergence here is an existence oracle")
	assert.Empty(t, *h.defaultWrites)
}

// TestSetDefaultCharacterRefusesARetiredCharacterAndPermitsTheActiveSibling is
// Q4, with its paired positive control on the same roster.
//
// The refusal exists because world.Service's retire path clears
// default_character_id in the same transaction as the status write; permitting
// the set here would rebuild exactly the state that code prevents, through a
// path that transaction cannot see.
func TestSetDefaultCharacterRefusesARetiredCharacterAndPermitsTheActiveSibling(t *testing.T) {
	t.Parallel()

	// ONE roster carrying both lifecycle states, so the two legs below are the
	// same fixture answering differently rather than two fixtures built to
	// disagree. ownedCharacterFixture's playerID argument is immaterial to the
	// gate — ownedCharacter matches ids against whatever ListByPlayer returned
	// for the SESSION's player — so seeding it here rather than from the harness
	// costs nothing and keeps the roster a single value both legs share.
	rosterPlayerID := idgen.New()
	active := ownedCharacterFixture(rosterPlayerID, "Ada", world.StatusActive)
	retired := ownedCharacterFixture(rosterPlayerID, "Withdrawn", world.StatusRetired)
	roster := []*world.Character{active, retired}

	t.Run("refuses the retired character with FailedPrecondition and writes nothing", func(t *testing.T) {
		t.Parallel()
		h := newWriteHarness(t, writeFixture{owned: roster, failOnDefaultWrite: true})

		resp, err := h.srv.SetDefaultCharacter(context.Background(), h.setDefaultRequest(retired.ID.String()))
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.FailedPrecondition, status.Code(err),
			"ownership was already proven, so this is a precondition failure and not an ownership refusal")
		assert.Equal(t, characterNotPlayableMessage, status.Convert(err).Message())
	})

	t.Run("permits the active sibling on the same roster", func(t *testing.T) {
		t.Parallel()
		h := newWriteHarness(t, writeFixture{owned: roster})

		resp, err := h.srv.SetDefaultCharacter(context.Background(), h.setDefaultRequest(active.ID.String()))
		require.NoError(t, err)
		require.Len(t, resp.GetCharacters(), 2, "the roster carries the retired entry too (D-90, §4.5)")

		require.Len(t, *h.defaultWrites, 1)
		assert.Equal(t, active.ID, (*h.defaultWrites)[0].characterID)
	})
}

// TestSetDefaultCharacterReturnsInternalAndNoRosterWhenTheNarrowWriteFails is
// the outage leg. The repository's failure is an outage, not an authorization
// answer (§8.10), and its text never reaches the caller.
func TestSetDefaultCharacterReturnsInternalAndNoRosterWhenTheNarrowWriteFails(t *testing.T) {
	t.Parallel()

	h := newWriteHarness(t, writeFixture{
		defaultWriteErr: oops.Code("PLAYER_UPDATE_DEFAULT_CHARACTER_FAILED").
			With("id", "the-players-id").
			Errorf("connection refused reaching players"),
	})

	resp, err := h.srv.SetDefaultCharacter(context.Background(), h.setDefaultRequest(h.owned.ID.String()))
	require.Error(t, err)
	assert.Nil(t, resp, "no roster is returned on the failure path")
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, "internal error", status.Convert(err).Message())
	assert.NotContains(t, status.Convert(err).Message(), "connection refused",
		"the inner error is logged, never interpolated into the returned status")
}

// TestSetDefaultCharacterWireMessagesCarryNoInternalCodeString sweeps every
// refusal leg for the internal code strings this surface stamps.
//
// The codes are useful in a log and are a disclosure on the wire: they name
// which internal rule fired, which is precisely what the uniform ownership
// message exists to withhold.
func TestSetDefaultCharacterWireMessagesCarryNoInternalCodeString(t *testing.T) {
	t.Parallel()

	internalCodes := []string{
		codeCharacterNotOwned,
		codeCharacterNotPlayable,
		"PLAYER_UPDATE_DEFAULT_CHARACTER_FAILED",
	}

	t.Run("the ownership refusal", func(t *testing.T) {
		t.Parallel()
		h := newWriteHarness(t, writeFixture{failOnDefaultWrite: true})
		_, err := h.srv.SetDefaultCharacter(context.Background(), h.setDefaultRequest(idgen.New().String()))
		require.Error(t, err)
		for _, code := range internalCodes {
			assert.NotContains(t, status.Convert(err).Message(), code)
		}
	})

	t.Run("the retired refusal", func(t *testing.T) {
		t.Parallel()
		retired := ownedCharacterFixture(idgen.New(), "Withdrawn", world.StatusRetired)
		h := newWriteHarness(t, writeFixture{
			failOnDefaultWrite: true,
			owned:              []*world.Character{retired},
		})
		_, err := h.srv.SetDefaultCharacter(context.Background(), h.setDefaultRequest(retired.ID.String()))
		require.Error(t, err)
		for _, code := range internalCodes {
			assert.NotContains(t, status.Convert(err).Message(), code)
		}
	})

	t.Run("the repository outage", func(t *testing.T) {
		t.Parallel()
		h := newWriteHarness(t, writeFixture{
			defaultWriteErr: oops.Code("PLAYER_UPDATE_DEFAULT_CHARACTER_FAILED").Errorf("boom"),
		})
		_, err := h.srv.SetDefaultCharacter(context.Background(), h.setDefaultRequest(h.owned.ID.String()))
		require.Error(t, err)
		for _, code := range internalCodes {
			assert.NotContains(t, status.Convert(err).Message(), code)
		}
	})
}
