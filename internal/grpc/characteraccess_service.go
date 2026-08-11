// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/profilevis"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// characterAccessWorldReader is the narrow interface CharacterAccessServer needs
// from world.Service — only the two authorized reads the public profile path
// makes.
//
// THE ABSENCES ARE LOAD-BEARING (PROFILE-10, D-79). Neither
// PropertyRepository.ListByParent nor PropertyReader.ListByParent is in this
// type set, so a direct repository call from this facade is a compile error
// rather than a review finding: property rows may be reached only through
// world.Service.ListPropertiesByParent, which per-property ABAC-filters them and
// aborts on an infrastructure failure instead of masking it as an empty result.
// Criterion 5's enforcement is the type system; there is deliberately no
// ruleguard rule and no meta-test alongside it.
type characterAccessWorldReader interface {
	ListPropertiesByParent(ctx context.Context, subjectID world.Caller, parentType string, parentID ulid.ULID) ([]*world.EntityProperty, error)
	GetCharacterDescription(ctx context.Context, subjectID world.Caller, characterID ulid.ULID) (world.CharacterDescription, error)
}

// characterAccessProfileVisibility is the narrow interface CharacterAccessServer
// needs from profilevis — only the reachability gate and the per-attribute
// conjunction.
//
// It carries no single-attribute entry point on purpose: 01-SPEC §8.5.1's two
// terms are ANDed inside profilevis.AttributeVisible, and a facade that could
// reach one term alone could reduce the conjunction to the additive shape
// §8.5.1.1 exists to prevent.
type characterAccessProfileVisibility interface {
	Reachable(ctx context.Context, viewerSubject, characterID string) (bool, error)
	VisibleAttributes(ctx context.Context, viewerSubject, characterID string, properties []profilevis.Property) (map[string]profilevis.Property, error)
}

// characterProfileNotFoundMessage is the ONE generic wire message every
// non-resolving profile read returns — below the reachability floor, denied at
// the description gate, or naming no row at all. 01-SPEC §8.7 requires the cases
// to be indistinguishable, and §9.6.1 asserts that over the wire, so the
// literal is shared rather than respelled per branch. It deliberately does not
// contain the internal code string.
const characterProfileNotFoundMessage = "character profile not found"

// CharacterAccessServer is the host-side facade for the web character surface.
// It constructs the viewer principal itself and owns every reachability and
// visibility decision, so the gateway forwards bytes and computes nothing.
type CharacterAccessServer struct {
	characteraccessv1.UnimplementedCharacterAccessServiceServer

	world      characterAccessWorldReader
	profileVis characterAccessProfileVisibility

	// playerSessionRepo and playerRepo are the viewer-identity seam. D-83 locks
	// that the viewer tier is resolved IN THE FACADE, at viewer-principal
	// construction, and that the facade is the ONLY layer holding the session —
	// a facade with no session repository cannot satisfy that sentence.
	playerSessionRepo auth.PlayerSessionRepository
	playerRepo        auth.PlayerRepository
}

// NewCharacterAccessServer constructs a CharacterAccessServer. All four
// dependencies are required; there are no optional setters, because every one of
// them is on the single RPC this facade serves.
func NewCharacterAccessServer(
	worldReader characterAccessWorldReader,
	profileVis characterAccessProfileVisibility,
	playerSessionRepo auth.PlayerSessionRepository,
	playerRepo auth.PlayerRepository,
) *CharacterAccessServer {
	return &CharacterAccessServer{
		world:             worldReader,
		profileVis:        profileVis,
		playerSessionRepo: playerSessionRepo,
		playerRepo:        playerRepo,
	}
}

// viewerIdentity is the closed set resolveViewerTier switches over: a tier token
// and, on the two non-anonymous rungs, the player ULID that tier carries.
//
// It exists so the I/O step and the mapping step are separable. Splitting them
// is what keeps the switch total over a closed set and unit-testable without
// repositories.
type viewerIdentity struct {
	tier     string
	playerID string
}

// resolveViewerIdentity turns the request's optional raw session token into the
// rung the reading principal actually occupies. It performs the I/O;
// resolveViewerTier is the pure mapping over its result.
//
// IT IS NOT playerGate.resolveAndGate, and the two are different questions on
// purpose: that helper loads the player and returns PermissionDenied for ANY
// guest (sceneaccess_service.go, INV-SCENE-64), while the public profile read
// must YIELD a guest rung — a guest reading a public profile is an ordinary web
// visitor, not an intruder. Borrowing the scene facade's gate here would refuse
// exactly the caller this surface exists to serve.
//
// AN UNRESOLVABLE TOKEN DEGRADES TO `anonymous` RATHER THAN ERRORING, and that
// is fail-CLOSED, not fail-open: anonymous is the LEAST-privileged rung, so the
// fallback can only ever narrow what the caller sees. It is also the correct
// product behavior — a logged-out visitor whose stale cookie is still attached
// must get the public page, not a 401. Expired, unknown, malformed and the
// NOT_CONFIGURED code all take this path, logged at debug.
//
// A playerRepo.GetByID FAILURE is the one non-nil error. A rung is a policy
// answer and an outage is not one (01-SPEC §8.10): rendering an identity-
// resolution outage as a successful anonymous-rung read would silently downgrade
// an authenticated viewer's profile and look like a working feature.
func (s *CharacterAccessServer) resolveViewerIdentity(ctx context.Context, rawToken string) (viewerIdentity, error) {
	anonymous := viewerIdentity{tier: access.ViewerTierAnonymous}

	if rawToken == "" {
		return anonymous, nil
	}

	ps, err := resolvePlayerSessionWithRepo(ctx, s.playerSessionRepo, rawToken)
	if err != nil {
		slog.DebugContext(ctx, "character access: session token did not resolve; reading as anonymous",
			"error", err)
		return anonymous, nil
	}

	player, err := s.playerRepo.GetByID(ctx, ps.PlayerID)
	if err != nil {
		errutil.LogErrorContext(ctx, "character access: player lookup failed", err,
			"player_id", ps.PlayerID.String())
		return viewerIdentity{}, err //nolint:wrapcheck // classified by the caller into codes.Internal
	}

	tier := access.ViewerTierPlayer
	if player.IsGuest {
		tier = access.ViewerTierGuest
	}
	return viewerIdentity{tier: tier, playerID: ps.PlayerID.String()}, nil
}

// resolveViewerTier maps a resolved identity onto its ABAC subject string. It is
// pure, and it is an EXHAUSTIVE SWITCH WITH A DENYING DEFAULT, modelled on
// world.Selectable: an identity carrying a tier outside the closed set yields no
// subject and a false ok, which the caller turns into a denial rather than into
// a subject no policy matches.
//
// THE PLAYER ID IS OMITTED ON THE ANONYMOUS RUNG, never empty-stringed. That is
// the omit-don't-sentinel rule from .claude/rules/abac-providers.md, and it is
// already enforced by construction here: access.ViewerSubject PANICS when the
// anonymous rung is handed a non-empty identifier, and panics when either
// non-anonymous rung is handed an empty one.
func resolveViewerTier(id viewerIdentity) (string, bool) {
	switch id.tier {
	case access.ViewerTierAnonymous:
		return access.ViewerSubject(access.ViewerTierAnonymous, ""), true
	case access.ViewerTierGuest, access.ViewerTierPlayer:
		if id.playerID == "" {
			return "", false
		}
		return access.ViewerSubject(id.tier, id.playerID), true
	default:
		return "", false
	}
}

// mapProfileError renders a profilevis failure as a gRPC status.
//
// CodeProfileUnreachable is a POLICY answer and takes the uniform §8.7
// not-found-equivalent. CodeEvaluationFailed is an OUTAGE and takes Internal
// with a generic message, logged first — §8.10 forbids masking it as a
// legitimately sparse profile. An inner error is never interpolated into a
// returned status message (.claude/rules/grpc-errors.md).
func mapProfileError(ctx context.Context, err error) error {
	var oe oops.OopsError
	if errors.As(err, &oe) && oe.Code() == profilevis.CodeProfileUnreachable {
		return status.Error(codes.NotFound, characterProfileNotFoundMessage) //nolint:wrapcheck // gRPC status error at handler boundary
	}
	errutil.LogErrorContext(ctx, "character access: profile visibility evaluation failed", err)
	return status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
}

// GetCharacterProfile resolves one character's public profile for the rung the
// request's optional token resolves to.
//
// ONE CODE PATH, NO SELF-DETECTION BRANCH (D-70): the projection is identical
// regardless of who is viewing. An owner reading their own profile through this
// RPC sees exactly what a stranger sees; the richer owner audience is a
// different RPC with a different message type.
//
// ORDER MATTERS. Reachability is evaluated FIRST and INDEPENDENTLY of any
// per-field result (§8.4.2), and a denial short-circuits to the uniform outcome
// before the character row is ever read — so a below-floor profile and an id
// naming no row cannot be separated by timing, by body size, or by which
// downstream call was made.
func (s *CharacterAccessServer) GetCharacterProfile(ctx context.Context, req *characteraccessv1.GetCharacterProfileRequest) (*characteraccessv1.GetCharacterProfileResponse, error) {
	characterID, err := ulid.Parse(req.GetCharacterId())
	if err != nil {
		// A malformed id takes the same opaque outcome as a well-formed one that
		// names no row: "that is not a ULID" is still a statement about which
		// characters exist.
		return nil, status.Error(codes.NotFound, characterProfileNotFoundMessage) //nolint:wrapcheck // gRPC status error at handler boundary
	}

	identity, err := s.resolveViewerIdentity(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	viewerSubject, ok := resolveViewerTier(identity)
	if !ok {
		slog.WarnContext(ctx, "character access: unrecognized viewer rung denied", "tier", identity.tier)
		return nil, status.Error(codes.NotFound, characterProfileNotFoundMessage) //nolint:wrapcheck // gRPC status error at handler boundary
	}

	reachable, err := s.profileVis.Reachable(ctx, viewerSubject, characterID.String())
	if err != nil {
		return nil, mapProfileError(ctx, err)
	}
	if !reachable {
		return nil, status.Error(codes.NotFound, characterProfileNotFoundMessage) //nolint:wrapcheck // gRPC status error at handler boundary
	}

	desc, err := s.world.GetCharacterDescription(ctx, world.HumanCaller(viewerSubject), characterID)
	if err != nil {
		return nil, s.mapDescriptionError(ctx, err)
	}

	// The profile map, primary image and gallery stay empty in the tracer slice;
	// the viewer-filtered property slice arrives with plan 04-02.
	return &characteraccessv1.GetCharacterProfileResponse{
		Character: projectPublic(characterID.String(), desc, nil),
	}, nil
}

// mapDescriptionError renders a world.Service read failure as a gRPC status.
//
// A POLICY DENIAL COLLAPSES INTO THE SAME UNIFORM NOT-FOUND as an absent row,
// rather than surfacing PermissionDenied. A distinguishable "you may not read
// this character's description" discloses that the character exists, which is
// the class of leak §8.7 closes; and since the description is a field of the
// profile rather than a separate resource, a denial here has the same meaning to
// a viewer as an unreachable profile. In the shipped v0.13 corpus this branch is
// unreachable — seed:viewer-character-description-read permits every rung
// unconditionally — so it is the fail-closed floor under a game that raises the
// description's floor (§7.4), not a live behavior.
//
// An EVALUATION failure is the exception and stays Internal, for §8.10's reason.
//
// world.ErrNotFound IS TESTED FIRST, AND THE ORDER IS LOAD-BEARING. An absent
// character id reaches this function carrying BOTH sentinels, because the ABAC
// gate resolves character attributes before the row is ever read and
// attribute.CharacterProvider turns a missing row into a resolver error that
// checkAccess classifies as ErrAccessEvaluationFailed. Testing the evaluation
// sentinel first would therefore render a nonexistent character as Internal
// while a below-floor one renders as NotFound — a differential that discloses
// exactly which character ids exist, which is the leak §8.7 closes and which the
// differential spec in test/integration/access/character_profile_read_test.go
// caught. A not-found arriving through the attribute resolver is still a
// not-found; it is not an outage, and §8.10's rule is about outages.
func (s *CharacterAccessServer) mapDescriptionError(ctx context.Context, err error) error {
	if errors.Is(err, world.ErrNotFound) {
		return status.Error(codes.NotFound, characterProfileNotFoundMessage) //nolint:wrapcheck // gRPC status error at handler boundary
	}
	if errors.Is(err, world.ErrPermissionDenied) {
		return status.Error(codes.NotFound, characterProfileNotFoundMessage) //nolint:wrapcheck // gRPC status error at handler boundary
	}
	if errors.Is(err, world.ErrAccessEvaluationFailed) {
		errutil.LogErrorContext(ctx, "character access: description access evaluation failed", err)
		return status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	errutil.LogErrorContext(ctx, "character access: character description read failed", err)
	return status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
}
