// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// The owner audience's MUTATION surface.
//
// STRUCTURAL WRITES ARRIVE HERE, NEVER THROUGH THE COMMAND PARSER. A web form or
// button is a machine-initiated structural write and takes a typed RPC; the
// command path (HandleCommand / sendCommand) is reserved for human
// conversational verbs typed into a terminal. See .claude/rules/gateway-boundary.md.
//
// EVERY MUTATION IN THIS FILE IS GUARDED. An absent or zero expected_version is a
// rejection, not a permission — the repository layer below does accept zero as an
// unversioned write, which is exactly why this boundary refuses it first.

// characterNotOwnedMessage is the ONE generic wire message every ownership
// failure on the mutation surface returns — an unparseable id, an id naming no
// row, and a well-formed id the caller does not own alike.
//
// UNIFORMITY IS WHY THE STATUS CAN BE RAISED. 01-SPEC §9.6 (`:2219`) maps
// CHARACTER_NOT_OWNED to PermissionDenied and scopes that row to an
// owner-audience MUTATION, while the owner READS keep the gate's own NotFound.
// A PermissionDenied that was reachable ONLY for a real row would disclose which
// character ids exist; because all three causes collapse to this single literal,
// the higher status costs no opacity. It deliberately does not contain the
// internal code string.
const characterNotOwnedMessage = "no such character on your roster"

// characterVersionRequiredMessage is the wire message a mutation missing its
// optimistic-concurrency token is refused with.
const characterVersionRequiredMessage = "expected_version is required and must be positive"

// characterMaskPathMessage is the wire message an unlisted update_mask path is
// refused with, phrased exactly as the shipped scene precedent phrases its own.
const characterMaskPathMessage = "update_mask contains an unsupported path"

// characterProfileFieldInvalidMessage is the wire message a prose value failing
// the cap, the UTF-8 rule or the control-character rule is refused with. It names
// no field and interpolates no inner error (.claude/rules/grpc-errors.md); the
// structured reason goes to errutil.LogErrorContext.
const characterProfileFieldInvalidMessage = "a profile field value is not acceptable"

// characterConcurrentEditMessage is the wire message the loser of a concurrent
// edit receives.
const characterConcurrentEditMessage = "the character was modified by someone else; re-read it and retry"

// The internal oops codes this file stamps. Every one is transcribed from
// 01-SPEC §9.6's table; none is invented here except
// codeCharacterProfileFieldInvalid, which §9.6 leaves to the handler because
// D-82 puts the §7.2 caps in the handler.
const (
	codeCharacterNotOwned            = "CHARACTER_NOT_OWNED"
	codeCharacterVersionRequired     = "CHARACTER_VERSION_REQUIRED"
	codeCharacterMaskPathUnsupported = "CHARACTER_MASK_PATH_UNSUPPORTED"
	codeCharacterProfileFieldInvalid = "CHARACTER_PROFILE_FIELD_INVALID"
)

// profileMaskField is what one maskable path resolves to: the request accessor
// carrying its new value, and the BYTE cap that value must satisfy.
//
// The accessor lives on the allowlist entry rather than in a parallel switch so
// each of the twelve §7.2 names is written down exactly ONCE in this file. A
// second table keyed by the same names is a divergence waiting to happen — and
// a mask path with no accessor, or an accessor with no allowlist entry, could
// not then exist.
type profileMaskField struct {
	maxBytes int
	value    func(req *characteraccessv1.UpdateCharacterProfileRequest) string
}

// updateCharacterProfileMaskablePaths is the closed set of update_mask paths the
// profile edit accepts — the twelve prose fields 01-SPEC §7.2 declares, spelled
// as the entity_properties row names verbatim (§7.1).
//
// Paths are compared by EXACT-STRING map lookup: no prefix matching, no
// wildcard, no glob, no dotted-subtree expansion (§9.5 rule 2). `profile` MUST
// NOT reach `profile.rp_preferences`. An unlisted path is rejected rather than
// ignored, so a direct gRPC caller cannot drive an unadvertised mutation through
// the mask — the rationale the shipped scene precedent states for its own
// allowlist (updateSceneMaskablePaths).
//
// It deliberately DUPLICATES world.profileAttributeNames rather than aliasing
// it. 04-09 keeps that set unexported precisely so this allowlist is a genuine
// second gate: the domain closes its own write surface, and the facade closes
// the surface the web advertises.
//
// The caps reuse the shipped world constants rather than minting new numbers.
// The seven short single-line fields cap at world.MaxNameLength; the five long
// multi-paragraph fields cap at world.MaxDescriptionLength. Both are measured in
// BYTES, exactly as world.ValidateDescription measures, so the facade and the
// domain cannot disagree about where the boundary is.
var updateCharacterProfileMaskablePaths = map[string]profileMaskField{
	"profile.pronouns": {world.MaxNameLength, func(r *characteraccessv1.UpdateCharacterProfileRequest) string {
		return r.GetPronouns()
	}},
	"profile.concept": {world.MaxNameLength, func(r *characteraccessv1.UpdateCharacterProfileRequest) string {
		return r.GetConcept()
	}},
	"profile.species": {world.MaxNameLength, func(r *characteraccessv1.UpdateCharacterProfileRequest) string {
		return r.GetSpecies()
	}},
	"profile.age": {world.MaxNameLength, func(r *characteraccessv1.UpdateCharacterProfileRequest) string {
		return r.GetAge()
	}},
	"profile.faction": {world.MaxNameLength, func(r *characteraccessv1.UpdateCharacterProfileRequest) string {
		return r.GetFaction()
	}},
	"profile.currently": {world.MaxNameLength, func(r *characteraccessv1.UpdateCharacterProfileRequest) string {
		return r.GetCurrently()
	}},
	"profile.timezone": {world.MaxNameLength, func(r *characteraccessv1.UpdateCharacterProfileRequest) string {
		return r.GetTimezone()
	}},
	"profile.appearance": {world.MaxDescriptionLength, func(r *characteraccessv1.UpdateCharacterProfileRequest) string {
		return r.GetAppearance()
	}},
	"profile.personality": {world.MaxDescriptionLength, func(r *characteraccessv1.UpdateCharacterProfileRequest) string {
		return r.GetPersonality()
	}},
	"profile.biography": {world.MaxDescriptionLength, func(r *characteraccessv1.UpdateCharacterProfileRequest) string {
		return r.GetBiography()
	}},
	"profile.rumors": {world.MaxDescriptionLength, func(r *characteraccessv1.UpdateCharacterProfileRequest) string {
		return r.GetRumors()
	}},
	"profile.rp_preferences": {world.MaxDescriptionLength, func(r *characteraccessv1.UpdateCharacterProfileRequest) string {
		return r.GetRpPreferences()
	}},
}

// ownedCharacterForMutation resolves the caller's ownership of charIDStr for a
// MUTATION and rewrites the shared gate's outcome onto the mutation surface's
// row of the phase gate error matrix.
//
// IT ROUTES THROUGH THE SHARED GATE; IT DOES NOT REPLACE IT. playerGate.ownedCharacter
// stays the ONE server-side ownership resolution in this package (INV-SCENE-63)
// and is not modified: its codes.NotFound is correct for the owner READ surfaces,
// which consume it unchanged. Only the mapping is per-surface.
//
// A codes.Internal is PROPAGATED VERBATIM, copying the shipped consumer at
// sceneaccess_service.go:657-663. An infrastructure failure is row 4 of the
// matrix, not an authorization outcome: laundering an unreachable repository
// into a PermissionDenied would render an outage as a policy answer, which
// 01-SPEC §8.10 forbids.
func (s *CharacterAccessServer) ownedCharacterForMutation(ctx context.Context, playerID ulid.ULID, charIDStr string) (*world.Character, error) {
	char, err := s.ownedCharacter(ctx, playerID, charIDStr)
	if err != nil {
		if status.Code(err) == codes.Internal {
			return nil, err
		}
		return nil, status.Error(codes.PermissionDenied, characterNotOwnedMessage) //nolint:wrapcheck // gRPC status error at handler boundary
	}
	return char, nil
}

// requireGuardedVersion refuses a mutation whose expected_version is absent,
// zero or negative, BEFORE any domain call.
//
// Absent and explicit zero take the same branch and proto3's inability to tell
// them apart therefore costs nothing. Zero is never read as "write without the
// guard" and is never silently defaulted to the row's current version — the
// latter would turn every stale client into a silent last-write-wins.
func requireGuardedVersion(ctx context.Context, expectedVersion int32, characterID ulid.ULID) error {
	if expectedVersion > 0 {
		return nil
	}
	errutil.LogErrorContext(ctx, "character access: guarded mutation refused without a version",
		oops.Code(codeCharacterVersionRequired).
			With("character_id", characterID.String()).
			With("expected_version", expectedVersion).
			Errorf("a guarded mutation requires a caller-supplied expected_version >= 1"))
	return status.Error(codes.InvalidArgument, characterVersionRequiredMessage) //nolint:wrapcheck // gRPC status error at handler boundary
}

// validateProfileValue applies the caps to one prose value, in BYTES.
//
// IT DELEGATES THE SHARED RULES TO THE DOMAIN rather than re-implementing them:
// world.ValidateDescription owns valid-UTF-8, the control-character rule
// (newline, carriage return and tab are all permitted — validation.go:191) and
// the long cap, and calling it here is what makes a facade/domain disagreement
// about those rules impossible. The only rule added on top is the SHORTER
// per-field cap, which the domain has no opinion about because D-82 places the
// §7.2 caps in the handler and 04-09's domain command deliberately does not
// enforce them.
//
// An EMPTY value is legal: it is the erasure instruction (04-09 deletes the row).
func validateProfileValue(value string, maxBytes int) error {
	if err := world.ValidateDescription(value); err != nil {
		return err //nolint:wrapcheck // classified by the caller into codes.InvalidArgument
	}
	if len(value) > maxBytes {
		return oops.Code(codeCharacterProfileFieldInvalid).
			With("max_bytes", maxBytes).
			With("got_bytes", len(value)).
			Errorf("value exceeds its byte cap")
	}
	return nil
}

// UpdateCharacterProfile applies a partial edit to an owned character's stored
// `profile.*` rows (01-SPEC §7.2) under a closed update_mask allowlist and the
// caller's expected_version.
//
// # Gate ordering is normative (§9.5)
//
// Session, then ownership, then the version guard, then the mask allowlist, then
// the empty-mask no-op, then value validation, then the domain write.
// Authorization is asserted BEFORE the mask is applied and the empty-mask
// short-circuit sits AFTER authorization, so a no-op request cannot be used by an
// unauthorized caller to drive downstream work or as an existence oracle.
//
// # The guard is a genuine CAS
//
// The caller's expected_version is threaded STRAIGHT INTO the domain command's
// expectedVersion parameter, which predicates its UPDATE on it. The handler does
// NOT re-read the character and substitute its current version — that would
// silently convert every stale client into a last-write-wins. The rejection here
// is a fast fail at the boundary; the guard itself is the domain's
// version-predicated write. Of two concurrent mutations carrying the same
// expected_version exactly one succeeds, and the loser is SURFACED as
// codes.Aborted rather than retried: a retry loop at the facade would reintroduce
// last-write-wins one layer up.
//
// # Which layer owns which cap
//
// The §7.2 profile-attribute caps are enforced HERE (D-82) because 04-09's domain
// command deliberately does not enforce them. The in-world `characters.description`
// cap is the opposite case and is enforced in the DOMAIN — see
// UpdateCharacterDescription below. Exactly one layer owns each field; do not add
// a second, divergent check for either.
func (s *CharacterAccessServer) UpdateCharacterProfile(ctx context.Context, req *characteraccessv1.UpdateCharacterProfileRequest) (*characteraccessv1.UpdateCharacterProfileResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacterForMutation(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	if versionErr := requireGuardedVersion(ctx, req.GetExpectedVersion(), char.ID); versionErr != nil {
		return nil, versionErr
	}

	// The mask is a SET (§9.5 rule 3): collecting into a map makes ordering
	// irrelevant and a duplicated entry apply exactly once, by construction
	// rather than by a dedupe pass a later edit could drop.
	paths := req.GetUpdateMask().GetPaths()
	attributes := make(map[string]string, len(paths))
	for _, path := range paths {
		field, ok := updateCharacterProfileMaskablePaths[path]
		if !ok {
			errutil.LogErrorContext(ctx, "character access: profile mask path rejected",
				oops.Code(codeCharacterMaskPathUnsupported).
					With("character_id", char.ID.String()).
					With("path", path).
					Errorf("update_mask path is outside the closed allowlist"))
			return nil, status.Error(codes.InvalidArgument, characterMaskPathMessage) //nolint:wrapcheck // gRPC status error at handler boundary
		}
		attributes[path] = field.value(req)
	}

	if len(attributes) == 0 {
		// An empty mask is a no-op success, never "apply every field" (§9.5 rule
		// 4). It returns the character UNCHANGED so a client re-reads current
		// state rather than guessing it. No domain write is reached; the
		// enumeration below is the same authorized read GetMyCharacter performs.
		profile, profileErr := s.ownedProfileAttributes(ctx, char.ID)
		if profileErr != nil {
			return nil, profileErr
		}
		return &characteraccessv1.UpdateCharacterProfileResponse{Character: projectOwner(char, profile)}, nil
	}

	for _, path := range slices.Sorted(maps.Keys(attributes)) {
		if valueErr := validateProfileValue(attributes[path], updateCharacterProfileMaskablePaths[path].maxBytes); valueErr != nil {
			errutil.LogErrorContext(ctx, "character access: profile field value rejected", valueErr,
				"character_id", char.ID.String(), "path", path)
			return nil, status.Error(codes.InvalidArgument, characterProfileFieldInvalidMessage) //nolint:wrapcheck // gRPC status error at handler boundary
		}
	}

	caller := world.HumanCaller(access.CharacterSubject(char.ID.String()))
	if writeErr := s.worldMutator.UpdateCharacterProfileAttributes(ctx, caller, char.ID, int(req.GetExpectedVersion()), attributes); writeErr != nil {
		return nil, s.mapCharacterWriteError(ctx, writeErr, char.ID)
	}

	out, err := s.ownerMutationResponse(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	return &characteraccessv1.UpdateCharacterProfileResponse{Character: out}, nil
}

// ownerMutationResponse re-resolves the character and its stored profile rows
// after a mutation, so the response carries the POST-WRITE state — in particular
// the bumped characters.version the client sends back as its next
// expected_version. Guessing it client-side is how a correct client becomes a
// stale one after its first successful edit.
func (s *CharacterAccessServer) ownerMutationResponse(ctx context.Context, playerID ulid.ULID, charIDStr string) (*characteraccessv1.OwnCharacter, error) {
	char, err := s.ownedCharacterForMutation(ctx, playerID, charIDStr)
	if err != nil {
		return nil, err
	}
	profile, err := s.ownedProfileAttributes(ctx, char.ID)
	if err != nil {
		return nil, err
	}
	return projectOwner(char, profile), nil
}

// mapCharacterWriteError renders a world.Service write failure as a gRPC status.
//
// A CONCURRENT EDIT is the typed conflict signal and maps to codes.Aborted —
// 01-SPEC §9.6 maps WORLD_CONCURRENT_EDIT there, and Phase 4 introduces that
// wire mapping. It is surfaced, never retried.
//
// A NOT-FOUND OR A POLICY DENIAL arriving here collapses into the SAME uniform
// PermissionDenied the ownership resolution returns. Both are only reachable
// AFTER ownership was proven — a row deleted in the window, or a policy corpus
// that declines the write — so collapsing them discloses nothing and keeps the
// mutation surface's wire behavior uniform on every ownership-shaped cause.
//
// EVERYTHING ELSE IS AN OUTAGE and stays Internal with a generic message, logged
// first (§8.10). An inner error is never interpolated into a returned status.
func (s *CharacterAccessServer) mapCharacterWriteError(ctx context.Context, err error, characterID ulid.ULID) error {
	if errors.Is(err, world.ErrConcurrentEdit) {
		errutil.LogErrorContext(ctx, "character access: mutation lost a concurrent edit", err,
			"character_id", characterID.String())
		return status.Error(codes.Aborted, characterConcurrentEditMessage) //nolint:wrapcheck // gRPC status error at handler boundary
	}
	if errors.Is(err, world.ErrNotFound) || errors.Is(err, world.ErrPermissionDenied) {
		errutil.LogErrorContext(ctx, "character access: mutation refused by the domain", err,
			"character_id", characterID.String())
		return status.Error(codes.PermissionDenied, characterNotOwnedMessage) //nolint:wrapcheck // gRPC status error at handler boundary
	}
	errutil.LogErrorContext(ctx, "character access: character mutation failed", err,
		"character_id", characterID.String())
	return status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
}
