// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"bytes"
	"context"
	"log/slog"
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
		sceneAccessLogPrefix,
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
				sceneAccessLogPrefix,
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
		sceneAccessLogPrefix,
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
		sceneAccessLogPrefix,
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
		sceneAccessLogPrefix,
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
		sceneAccessLogPrefix,
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
		sceneAccessLogPrefix,
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
		sceneAccessLogPrefix,
	)

	got, err := gate.ownedCharacter(ctx, playerID, owned.ID.String())

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, owned.ID, got.ID)
	assert.Equal(t, "Alice", got.Name)
}

// TestPlayerGateAttributesItsFailureLogsToTheConfiguredSurfaceAndCarriesTheOopsCode
// pins the two remaining log lines the shared gate emits.
//
// Both were byte-identical carry-overs from the scene facade, hardcoded to
// "scene access" and written as a bare slog.ErrorContext(ctx, msg, "error", err).
// Moving them into shared code multiplied their blast radius: they now fire for
// ListMyCharacters, GetMyCharacter, UpdateCharacterProfile and
// UpdateCharacterDescription, so an operator triaging a character-surface outage
// greps "character access" and finds nothing while the scene facade's dashboards
// absorb the noise. The phase's own reasoning for a per-facade DENIAL message
// applies verbatim to a per-facade log message.
//
// The oops-code assertion is the second half: .claude/rules/grpc-errors.md
// requires errutil.LogErrorContext, which lifts the oops code and context map
// into structured fields. The bare form flattens the error to a string and loses
// the code an operator would filter on in Loki or Sentry.
func TestPlayerGateAttributesItsFailureLogsToTheConfiguredSurfaceAndCarriesTheOopsCode(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		wantScene bool
	}{
		{"the scene gate keeps naming the scene surface", sceneAccessLogPrefix, true},
		{"the character gate names the character surface", characterAccessLogPrefix, false},
		{"an empty prefix falls back to the scene surface rather than emitting a bare line", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			playerID := idgen.New()

			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})))
			t.Cleanup(func() { slog.SetDefault(prev) })

			// The roster lookup fails — the gate's one logged branch in
			// ownedCharacter.
			failingRepo := authmocks.NewMockCharacterRepository(t)
			failingRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
				Return(nil, oops.Code("CHARACTER_LIST_FAILED").Errorf("database unreachable")).Once()

			// The player lookup fails — the gate's one logged branch in
			// resolveAndGate.
			ps := buildSATestPS(t, playerID)
			failingPlayerRepo := authmocks.NewMockPlayerRepository(t)
			failingPlayerRepo.EXPECT().GetByID(mock.Anything, playerID).
				Return(nil, oops.Code("PLAYER_LOOKUP_FAILED").Errorf("database unreachable")).Once()

			gate := newPlayerGate(
				buildSASessionRepo(t, ps),
				failingPlayerRepo,
				failingRepo,
				sceneGuestDenialMessage,
				tt.prefix,
			)

			_, charErr := gate.ownedCharacter(ctx, playerID, idgen.New().String())
			_, sessErr := gate.resolveAndGate(ctx, testSAToken)
			require.Error(t, charErr)
			require.Error(t, sessErr)

			logged := buf.String()
			wantPrefix, otherPrefix := characterAccessLogPrefix, sceneAccessLogPrefix
			if tt.wantScene {
				wantPrefix, otherPrefix = sceneAccessLogPrefix, characterAccessLogPrefix
			}

			assert.Contains(t, logged, wantPrefix+": list characters failed")
			assert.Contains(t, logged, wantPrefix+": player lookup failed")
			assert.NotContains(t, logged, otherPrefix+":",
				"a gate MUST NOT attribute its failures to a surface the caller never touched")

			// errutil.LogErrorContext lifts the oops code into a structured field;
			// a bare slog.ErrorContext(ctx, msg, "error", err) would not.
			assert.Contains(t, logged, "CHARACTER_LIST_FAILED",
				"the oops code must reach the log as a structured field, not be flattened into a string")
			assert.Contains(t, logged, "PLAYER_LOOKUP_FAILED")
		})
	}
}
