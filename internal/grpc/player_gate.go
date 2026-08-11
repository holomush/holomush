// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/world"
)

// sceneGuestDenialMessage is the wire message SceneAccessServer denies a guest
// with. It is asserted verbatim by internal/web's scene handler and status
// interceptor tests, so it is also the default newPlayerGate falls back to when
// a caller supplies an empty message — a miswired facade must not produce an
// empty denial reason.
const sceneGuestDenialMessage = "guests cannot access scenes"

// playerGate is the single definition of the two owner-audience authorization
// helpers every facade in this package shares: the guest gate (INV-SCENE-64)
// and server-side character-ownership resolution (INV-SCENE-63). Facades embed
// it so both methods are promoted onto the facade — a second facade cannot
// re-implement or skip either one.
type playerGate struct {
	playerSessionRepo auth.PlayerSessionRepository
	playerRepo        auth.PlayerRepository
	charRepo          auth.CharacterRepository

	// guestDenialMessage is the wire message resolveAndGate denies a guest
	// with. It is per-facade: the scene surface must keep saying
	// sceneGuestDenialMessage verbatim, while other owner-audience surfaces
	// name their own.
	guestDenialMessage string
}

// newPlayerGate builds the shared gate. An empty guestDenialMessage defaults to
// sceneGuestDenialMessage.
func newPlayerGate(
	playerSessionRepo auth.PlayerSessionRepository,
	playerRepo auth.PlayerRepository,
	charRepo auth.CharacterRepository,
	guestDenialMessage string,
) playerGate {
	if guestDenialMessage == "" {
		guestDenialMessage = sceneGuestDenialMessage
	}
	return playerGate{
		playerSessionRepo:  playerSessionRepo,
		playerRepo:         playerRepo,
		charRepo:           charRepo,
		guestDenialMessage: guestDenialMessage,
	}
}

// ownedCharacter verifies that charIDStr is a valid ULID and is owned by
// playerID (INV-SCENE-63). Returns (verified *world.Character, nil) on success
// or (nil, codes.NotFound) when the character is absent from the player's list.
// The two authorization failures — an unparseable id and a character the player
// does not own — are wire-identical so neither discloses whether the row
// exists. A repository failure is NOT an authorization outcome: it returns
// codes.Internal after logging, and callers depend on that distinction.
func (s *playerGate) ownedCharacter(ctx context.Context, playerID ulid.ULID, charIDStr string) (*world.Character, error) {
	charID, err := ulid.Parse(charIDStr)
	if err != nil {
		return nil, status.Error(codes.NotFound, "character not found") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	chars, err := s.charRepo.ListByPlayer(ctx, playerID)
	if err != nil {
		slog.ErrorContext(ctx, "scene access: list characters failed", "error", err)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	for _, c := range chars {
		if c.ID == charID {
			return c, nil
		}
	}
	return nil, status.Error(codes.NotFound, "character not found") //nolint:wrapcheck // gRPC status error at handler boundary
}

// resolveAndGate resolves the player session from rawToken, loads the player,
// and enforces the guest gate (INV-SCENE-64). Returns the validated PlayerSession
// or a gRPC status error carrying this gate's configured guestDenialMessage.
func (s *playerGate) resolveAndGate(ctx context.Context, rawToken string) (*auth.PlayerSession, error) {
	ps, err := resolvePlayerSessionWithRepo(ctx, s.playerSessionRepo, rawToken)
	if err != nil {
		if oe, ok := oops.AsOops(err); ok && oe.Code() == "NOT_CONFIGURED" {
			return nil, status.Error(codes.Unimplemented, "player session service not configured") //nolint:wrapcheck // gRPC status error at handler boundary
		}
		return nil, status.Error(codes.Unauthenticated, "unauthenticated") //nolint:wrapcheck // gRPC status error at handler boundary
	}

	player, err := s.playerRepo.GetByID(ctx, ps.PlayerID)
	if err != nil {
		slog.ErrorContext(ctx, "scene access: player lookup failed", "error", err)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	if player.IsGuest {
		return nil, status.Error(codes.PermissionDenied, s.guestDenialMessage) //nolint:wrapcheck // gRPC status error at handler boundary
	}
	return ps, nil
}
