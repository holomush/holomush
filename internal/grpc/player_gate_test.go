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

	"github.com/holomush/holomush/internal/auth"
	authmocks "github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/world"
)

// buildGatePlayerRepo returns a MockPlayerRepository resolving playerID to a
// player whose IsGuest is isGuest.
func buildGatePlayerRepo(t *testing.T, playerID ulid.ULID, isGuest bool) *authmocks.MockPlayerRepository {
	t.Helper()
	pr := authmocks.NewMockPlayerRepository(t)
	pr.EXPECT().GetByID(mock.Anything, playerID).Return(&auth.Player{ID: playerID, IsGuest: isGuest}, nil).Maybe()
	return pr
}

// TestPlayerGateResolveAndGateDeniesAGuestSessionWithPermissionDeniedAndTheConfiguredMessage
// pins the guest gate's wire outcome on the scene-configured gate.
//
// Verifies: INV-SCENE-64
func TestPlayerGateResolveAndGateDeniesAGuestSessionWithPermissionDeniedAndTheConfiguredMessage(t *testing.T) {
	ctx := context.Background()
	playerID := idgen.New()
	ps := buildSATestPS(t, playerID)

	gate := newPlayerGate(
		buildSASessionRepo(t, ps),
		buildGatePlayerRepo(t, playerID, true),
		authmocks.NewMockCharacterRepository(t),
		sceneGuestDenialMessage,
	)

	got, err := gate.resolveAndGate(ctx, testSAToken)

	require.Error(t, err)
	assert.Nil(t, got, "a denied gate must not hand back a session")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, "guests cannot access scenes", status.Convert(err).Message(),
		"the scene surface's denial literal is asserted verbatim by internal/web tests")
}

// TestPlayerGateResolveAndGateGuestDenialMessageIsPerGateRatherThanHardCoded proves the
// parameterization is live: two gates built with different messages deny the
// same guest session with their own message, and an empty message falls back to
// the scene literal rather than producing an empty denial reason.
func TestPlayerGateResolveAndGateGuestDenialMessageIsPerGateRatherThanHardCoded(t *testing.T) {
	tests := []struct {
		name        string
		configured  string
		wantMessage string
	}{
		{"scene gate denies with the scene literal", sceneGuestDenialMessage, "guests cannot access scenes"},
		{"another facade's gate denies with its own message", "guests cannot manage characters", "guests cannot manage characters"},
		{"an empty message falls back to the scene literal", "", "guests cannot access scenes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			playerID := idgen.New()
			ps := buildSATestPS(t, playerID)

			gate := newPlayerGate(
				buildSASessionRepo(t, ps),
				buildGatePlayerRepo(t, playerID, true),
				authmocks.NewMockCharacterRepository(t),
				tt.configured,
			)

			_, err := gate.resolveAndGate(ctx, testSAToken)

			require.Error(t, err)
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
			assert.Equal(t, tt.wantMessage, status.Convert(err).Message())
		})
	}
}

// TestPlayerGateResolveAndGateReturnsTheResolvedSessionForANonGuestPlayer is the paired
// positive control for the guest denial above.
func TestPlayerGateResolveAndGateReturnsTheResolvedSessionForANonGuestPlayer(t *testing.T) {
	ctx := context.Background()
	playerID := idgen.New()
	ps := buildSATestPS(t, playerID)

	gate := newPlayerGate(
		buildSASessionRepo(t, ps),
		buildGatePlayerRepo(t, playerID, false),
		authmocks.NewMockCharacterRepository(t),
		sceneGuestDenialMessage,
	)

	got, err := gate.resolveAndGate(ctx, testSAToken)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, playerID, got.PlayerID)
}

// TestPlayerGateResolveAndGateReturnsUnimplementedWhenTheSessionRepositoryIsNotConfigured
// pins the NOT_CONFIGURED branch: a missing session repository is a deployment
// gap, not an authentication failure, so it must not read as Unauthenticated.
func TestPlayerGateResolveAndGateReturnsUnimplementedWhenTheSessionRepositoryIsNotConfigured(t *testing.T) {
	ctx := context.Background()

	gate := newPlayerGate(
		nil, // no player-session repository wired
		authmocks.NewMockPlayerRepository(t),
		authmocks.NewMockCharacterRepository(t),
		sceneGuestDenialMessage,
	)

	got, err := gate.resolveAndGate(ctx, testSAToken)

	require.Error(t, err)
	assert.Nil(t, got)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
	assert.Equal(t, "player session service not configured", status.Convert(err).Message())
}

// TestPlayerGateOwnedCharacterReturnsAnIdenticalNotFoundForAnUnparseableIDAndForANonOwnedCharacter
// pins the opacity contract across the TWO authorization outcomes. It is a
// two-way equality on purpose: distinguishing a malformed id from a
// well-formed-but-not-owned one would disclose whether the row exists. The
// infrastructure outcome is deliberately NOT in this equality — see the
// Internal test below.
//
// Verifies: INV-SCENE-63
func TestPlayerGateOwnedCharacterReturnsAnIdenticalNotFoundForAnUnparseableIDAndForANonOwnedCharacter(t *testing.T) {
	ctx := context.Background()
	playerID := idgen.New()
	// A well-formed id the player does not own: the repository reports a list
	// that does not contain it.
	otherCharID := idgen.New()

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*world.Character{}, nil).Maybe()

	gate := newPlayerGate(
		authmocks.NewMockPlayerSessionRepository(t),
		authmocks.NewMockPlayerRepository(t),
		charRepo,
		sceneGuestDenialMessage,
	)

	malformedChar, malformedErr := gate.ownedCharacter(ctx, playerID, "not-a-ulid")
	notOwnedChar, notOwnedErr := gate.ownedCharacter(ctx, playerID, otherCharID.String())

	require.Error(t, malformedErr)
	require.Error(t, notOwnedErr)
	assert.Nil(t, malformedChar)
	assert.Nil(t, notOwnedChar)

	assert.Equal(t, codes.NotFound, status.Code(malformedErr))
	assert.Equal(t, codes.NotFound, status.Code(notOwnedErr))
	assert.Equal(t, status.Convert(malformedErr).Message(), status.Convert(notOwnedErr).Message(),
		"an unparseable id and a non-owned id must be wire-identical — a difference discloses whether the row exists")
}

// TestPlayerGateOwnedCharacterReturnsInternalWithAMessageDistinctFromNotFoundWhenTheCharacterRepositoryFails
// is the distinction the opacity equality above must not swallow. An
// infrastructure failure is not an authorization outcome: the caller at
// sceneaccess_service.go branches on exactly codes.Internal to propagate an
// outage instead of rendering it as a missing character.
func TestPlayerGateOwnedCharacterReturnsInternalWithAMessageDistinctFromNotFoundWhenTheCharacterRepositoryFails(t *testing.T) {
	ctx := context.Background()
	playerID := idgen.New()
	charID := idgen.New()

	failingRepo := authmocks.NewMockCharacterRepository(t)
	failingRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return(nil, oops.Code("CHARACTER_LIST_FAILED").Errorf("database unreachable")).Once()

	failingGate := newPlayerGate(
		authmocks.NewMockPlayerSessionRepository(t),
		authmocks.NewMockPlayerRepository(t),
		failingRepo,
		sceneGuestDenialMessage,
	)

	gotChar, infraErr := failingGate.ownedCharacter(ctx, playerID, charID.String())

	require.Error(t, infraErr)
	assert.Nil(t, gotChar)
	assert.Equal(t, codes.Internal, status.Code(infraErr),
		"a repository failure must stay Internal — collapsing it into NotFound breaks the caller's branch")

	// The infra message must be distinguishable from the authorization message,
	// so a future change that masks an outage as NotFound fails here.
	emptyRepo := authmocks.NewMockCharacterRepository(t)
	emptyRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*world.Character{}, nil).Once()
	notFoundGate := newPlayerGate(
		authmocks.NewMockPlayerSessionRepository(t),
		authmocks.NewMockPlayerRepository(t),
		emptyRepo,
		sceneGuestDenialMessage,
	)
	_, notFoundErr := notFoundGate.ownedCharacter(ctx, playerID, charID.String())
	require.Error(t, notFoundErr)

	assert.NotEqual(t, status.Convert(notFoundErr).Message(), status.Convert(infraErr).Message(),
		"an infrastructure failure must not be wire-identical to an authorization denial")
}

// TestPlayerGateOwnedCharacterReturnsTheCharacterWhenThePlayerOwnsIt is the paired
// positive control for the two denial paths above.
func TestPlayerGateOwnedCharacterReturnsTheCharacterWhenThePlayerOwnsIt(t *testing.T) {
	ctx := context.Background()
	playerID := idgen.New()
	owned := &world.Character{ID: idgen.New(), Name: "Alice"}

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*world.Character{owned}, nil).Once()

	gate := newPlayerGate(
		authmocks.NewMockPlayerSessionRepository(t),
		authmocks.NewMockPlayerRepository(t),
		charRepo,
		sceneGuestDenialMessage,
	)

	got, err := gate.ownedCharacter(ctx, playerID, owned.ID.String())

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, owned.ID, got.ID)
	assert.Equal(t, "Alice", got.Name)
}
