// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/charname/syntax"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// The owner audience's CREATE surface.
//
// It is the one owner-audience mutation that names no existing character id, so
// it reaches the shared guest gate and NOTHING ELSE: there is no ownership to
// resolve before a row exists, and there is no version to guard against. Both
// absences are asserted — the routing census carries CreateCharacter in the
// guest-gate set and deliberately NOT in the ownership set, and 01-SPEC §9.4.2
// makes creation the one carve-out from the version guard the two edit RPCs obey.

// createCharacterBindReason is the binding reason the create pipeline stamps on
// the player↔character row. It is the SAME literal
// holomush.core.v1.CoreService.CreateCharacter already uses
// (internal/grpc/auth_handlers.go), so a character seated from the web and one
// seated from the telnet CREATE verb are indistinguishable in the binding table.
const createCharacterBindReason = "initial_bind"

// createdCharacterVersion is the characters.version a row carries the instant it
// is created, and it is the expectedVersion the profile-seeding write is
// predicated on.
//
// The literal is correct by two independent statements that are asserted rather
// than assumed: migration 000049 declares `version INTEGER NOT NULL DEFAULT 1`,
// and 01-SPEC §9.4.2 states it. The integration spec pins it, so if a future
// migration changes the default this constant fails loudly instead of the guard
// silently degrading into an unversioned write (world.Service refuses
// expectedVersion <= 0, which is the floor under this).
const createdCharacterVersion = 1

// createCharacterProfileSeed is one of the five prose values a creation form may
// submit alongside the name: the entity_properties row it seeds, and the request
// accessor carrying its value.
//
// The BYTE CAP IS DELIBERATELY ABSENT FROM THIS STRUCT. It is read from
// updateCharacterProfileMaskablePaths by path, so the create surface and the
// mask-driven edit surface cannot disagree about where the boundary sits —
// exactly one cap table exists in this package, and it is that one.
type createCharacterProfileSeed struct {
	path  string
	value func(req *characteraccessv1.CreateCharacterRequest) string
}

// createCharacterProfileSeeds is the closed set of profile rows a create may
// seed: §7.2's five SHORT prose fields, spelled as the entity_properties row
// names verbatim.
//
// It is a SLICE, not a map, so the validation order below is deterministic and a
// refusal is reproducible rather than a function of Go's map iteration. Every
// member must also be a member of updateCharacterProfileMaskablePaths — asserted
// by TestEveryCreateProfileSeedPathIsAMaskablePath, because a seed naming a path
// the mask table lacks would read a zero byte cap and refuse every non-empty
// value.
//
// The seven remaining §7.2 fields are NOT here and that is the shape, not an
// omission: the creation form collects the five that fit an identity card, and
// the other seven arrive on /characters/[id] where all twelve are editable.
var createCharacterProfileSeeds = []createCharacterProfileSeed{
	{"profile.pronouns", func(r *characteraccessv1.CreateCharacterRequest) string { return r.GetPronouns() }},
	{"profile.concept", func(r *characteraccessv1.CreateCharacterRequest) string { return r.GetConcept() }},
	{"profile.species", func(r *characteraccessv1.CreateCharacterRequest) string { return r.GetSpecies() }},
	{"profile.age", func(r *characteraccessv1.CreateCharacterRequest) string { return r.GetAge() }},
	{"profile.faction", func(r *characteraccessv1.CreateCharacterRequest) string { return r.GetFaction() }},
}

// CreateCharacter seats a new character on the authenticated caller from a
// structured identity card and answers with it in the owner audience's shape.
//
// # It writes in TWO transactions, and the create is the authoritative one
//
// auth.CharacterService.CreateBound runs the whole admission pipeline and then
// CharacterGenesisService.Create, which commits the character row, the
// player↔character binding and the genesis envelope in ONE transaction
// (INV-WORLD-4). The five `profile.*` values are entity_properties rows written
// by a DIFFERENT domain command that makes its own ABAC decision and demands
// expectedVersion >= 1; nothing in the tree writes both in one transaction, and
// widening the genesis service to do so widens a path three creation paths
// depend on.
//
// So a profile-seeding failure is LOGGED AND SWALLOWED and the RPC still returns
// success with the created character, its un-set keys simply absent from the
// `profile` map. Do NOT "fix" this into a returned error. The character exists
// and its name is reserved the instant the first transaction commits, so the
// player's natural recovery — resubmit — would collide with the character they
// just created and be told the name is already taken, which is the
// duplicate-create hazard the whole create flow exists to avoid. The gaps are
// repairable on /characters/[id], where all twelve fields are editable. This
// shape was ratified by the maintainer before implementation (plan 05-03, Q1
// option-a).
//
// # Value validation runs BEFORE the create, not after
//
// A value failing the §7.2 byte cap is a deterministic refusal the player can
// act on, so it is refused while refusing is still free — before a row exists
// and before a name is reserved. Validating after the create would put a fixable
// mistake on the wrong side of the swallow above: the player would be told the
// create succeeded and find the field they typed missing, with the same
// name-collision trap waiting for a retry.
//
// # No version guard, and no ownership resolution
//
// requireGuardedVersion is not called and CreateCharacterRequest declares no
// expected_version field (§9.4.2): a guard predicates a write on a version the
// caller last read, and a create has no row to have read. Ownership likewise has
// nothing to resolve — the caller names no existing character — which is why
// this handler is deliberately absent from characterOwnershipRPCs(), mirroring
// ListMyCharacters.
func (s *CharacterAccessServer) CreateCharacter(ctx context.Context, req *characteraccessv1.CreateCharacterRequest) (*characteraccessv1.CreateCharacterResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}

	attributes, err := collectCreateProfileSeeds(ctx, req)
	if err != nil {
		return nil, err
	}

	char, err := s.creator.CreateBound(ctx, ps.PlayerID, req.GetName(), createCharacterBindReason)
	if err != nil {
		return nil, mapCharacterCreateError(ctx, err, req.GetName())
	}

	s.seedCreatedProfile(ctx, char.ID, attributes)

	// The response is READ BACK rather than assembled from the request (§2.3).
	// What the grid stores is the normalized DISPLAY form, which may differ from
	// the submitted bytes wherever NFKC, the format-rune strip or whitespace
	// collapse changed them — and D-88's post-submit echo is the whole reason the
	// client is handed the server's copy instead of keeping its own.
	out, err := s.ownerMutationResponse(ctx, ps.PlayerID, char.ID.String())
	if err != nil {
		return nil, err
	}
	return &characteraccessv1.CreateCharacterResponse{Character: out}, nil
}

// collectCreateProfileSeeds gathers the supplied non-empty profile values into
// the (row name -> value) map the domain command takes, refusing any that fails
// its §7.2 byte cap.
//
// AN EMPTY VALUE IS NOT WRITTEN AT ALL, rather than written as an empty row. On
// the edit surface an empty value is the erasure instruction; on a create there
// is nothing to erase, and seeding a blank row would publish a field the player
// never authored. A name-only submission therefore yields an empty map and no
// second domain call.
func collectCreateProfileSeeds(ctx context.Context, req *characteraccessv1.CreateCharacterRequest) (map[string]string, error) {
	attributes := make(map[string]string, len(createCharacterProfileSeeds))
	for _, seed := range createCharacterProfileSeeds {
		value := seed.value(req)
		if value == "" {
			continue
		}
		// The cap comes from the ONE table. A seed whose path is missing there
		// reads a zero cap and refuses every non-empty value — fail-closed, and
		// pinned by a unit test so the state is unreachable rather than merely
		// survivable.
		if err := validateProfileValue(value, updateCharacterProfileMaskablePaths[seed.path].maxBytes); err != nil {
			errutil.LogErrorContext(ctx, "character access: create refused a profile field value", err,
				"path", seed.path)
			return nil, status.Errorf(codes.InvalidArgument, "%s", characterProfileFieldInvalidMessage)
		}
		attributes[seed.path] = value
	}
	return attributes, nil
}

// seedCreatedProfile writes the supplied profile rows for a character that has
// ALREADY been created, and reports nothing.
//
// The return type is the contract: this is the second of the two transactions
// and its failure may not reach the caller (see CreateCharacter). Giving it an
// error return would invite exactly the "fix" the ratified decision forbids.
//
// The failure log names the character id and every attempted path, because that
// pair is the whole of what an operator needs to finish the write by hand or to
// tell the player which fields to re-enter.
func (s *CharacterAccessServer) seedCreatedProfile(ctx context.Context, characterID ulid.ULID, attributes map[string]string) {
	if len(attributes) == 0 {
		return
	}
	caller := world.HumanCaller(access.CharacterSubject(characterID.String()))
	if err := s.worldMutator.UpdateCharacterProfileAttributes(ctx, caller, characterID, createdCharacterVersion, attributes); err != nil {
		errutil.LogErrorContext(ctx, "character access: profile seeding failed AFTER the character was created; the create stands and the fields are unset", err,
			"character_id", characterID.String(),
			"paths", strings.Join(slices.Sorted(maps.Keys(attributes)), ","))
	}
}

// mapCharacterCreateError renders a create-pipeline refusal as a gRPC status
// with an AUTHORED player-facing message.
//
// # Every message is a constant, selected by a closed switch
//
// No branch interpolates err.Error() and none formats a wrapped error with %v.
// The pipeline's errors carry the submitted name, the colliding name, the
// matched block-list pattern's neighbourhood and repository detail in their oops
// context maps; rendering any of that on the wire would turn this surface into a
// name-enumeration oracle (§6.1.2) or leak schema identifiers. The structured
// error goes to the log; the caller gets a sentence from
// internal/grpc/auth_errors.go. See .claude/rules/grpc-errors.md.
//
// # The switch reads the DEEPEST code, and that is safe HERE SPECIFICALLY
//
// oops.OopsError.Code() returns the deepest code in the chain (issue #4902), so
// a wrapping code can be shadowed by whatever it wrapped. Every code this switch
// recognizes reaches it UNWRAPPED except two, and neither shadowing is harmful:
//
//   - CHARACTER_CREATE_FAILED wraps a repository or genesis error that may carry
//     its own code. A shadowed one falls to the default arm, which returns the
//     IDENTICAL (Internal, msgCharacterCreateFailed) pair — so the caller cannot
//     tell, and there is nothing to get wrong. Pinned by a test rather than left
//     as a comment.
//   - CHARACTER_NO_STARTING_LOCATION wraps the location repository's error. A
//     coded inner error degrades FailedPrecondition to Internal — less specific,
//     never more permissive, and it discloses nothing extra.
//
// The two remapped name verdicts are the reason the rest is safe:
// auth.mapGateError REPLACES the code rather than wrapping it, precisely so the
// caller-visible contract does not silently change (character_service.go:283).
// The *syntax.ValidationError it carries forward is not an oops error, so it
// contributes no code of its own.
//
// # One code, two messages — decided on the handler's own input
//
// CHARACTER_INVALID_NAME arrives for a syntax refusal, for a blank box and for a
// submission of nothing but invisible codepoints. The three are separated by
// asking the *syntax.ValidationError chain and then the SUBMITTED STRING —
// never by matching on an error message, which would couple this switch to
// wording that exists to be changed.
func mapCharacterCreateError(ctx context.Context, err error, submitted string) error {
	code, statusCode, message := classifyCharacterCreateError(err, submitted)
	errutil.LogErrorContext(ctx, "character access: character creation refused", err,
		"create_code", code,
		"wire_code", statusCode.String())
	return status.Errorf(statusCode, "%s", message)
}

// classifyCharacterCreateError is mapCharacterCreateError's pure half: the
// closed switch, with no logging and no status construction, so the whole
// mapping table is exercisable as data.
func classifyCharacterCreateError(err error, submitted string) (createCode string, wire codes.Code, message string) {
	code := ""
	if oopsErr, ok := oops.AsOops(err); ok {
		if asString, isString := oopsErr.Code().(string); isString {
			code = asString
		}
	}

	switch code {
	case "CHARACTER_INVALID_NAME":
		return code, codes.InvalidArgument, invalidNameMessage(err, submitted)
	case "CHARACTER_NAME_TAKEN":
		// Both producers land here — the §6.1.1 uniqueness pre-check and the
		// 23505 handler over characters_normalized_name_key — and they are
		// deliberately indistinguishable on the wire. Which of the two won a race
		// is not the caller's business, and a differential would report timing.
		return code, codes.AlreadyExists, msgCharacterNameTaken
	case "CHARACTER_LIMIT_REACHED", "CHARACTER_NO_STARTING_LOCATION":
		// FailedPrecondition, pinned as SPEC amendment 4 (plan 05-03 Q3). Neither
		// is an invalid argument — the name was fine — and neither is an outage
		// the client should retry blindly: one needs a character retired first,
		// the other needs an operator.
		return code, codes.FailedPrecondition, limitOrLocationMessage(code)
	case "NAME_CONFUSABLE":
		return code, codes.InvalidArgument, msgCharacterNameConfusable
	case "NAME_BLOCKED":
		return code, codes.InvalidArgument, msgCharacterNameBlocked
	case "NAME_MIXED_SCRIPT":
		return code, codes.InvalidArgument, msgCharacterNameMixedScript
	case "NAME_UNASSIGNED_SCRIPT":
		return code, codes.InvalidArgument, msgCharacterNameUnassignedScript
	case "NAME_SKELETON_UNVERIFIABLE":
		// Unavailable, and the message reads as transient on purpose (D-30): the
		// name was not refused. The confusability corpus could not answer yet, so
		// the gate declined to adjudicate rather than guess. Telling the player to
		// pick a different name would be false.
		return code, codes.Unavailable, msgCharacterNameUnverifiable
	}
	// NAME_SKELETON_LOOKUP_FAILED, a shadowed inner code, a non-oops error and
	// anything this pipeline gains later all land here: an OUTAGE, rendered with
	// the create path's generic authored copy. Returning the same pair the
	// CHARACTER_CREATE_FAILED arm would is what makes the deepest-code shadowing
	// above invisible to the caller.
	return code, codes.Internal, msgCharacterCreateFailed
}

// invalidNameMessage picks between the three CHARACTER_INVALID_NAME copies.
//
// The syntax refusal is tested FIRST because it is the only one with a typed
// witness in the chain. The remaining two are separated on the SUBMISSION
// itself, which is the only honest question: a name whose whitespace-trimmed
// form is empty was a blank box, and one that is non-empty yet normalized away
// was invisible codepoints.
func invalidNameMessage(err error, submitted string) string {
	var verr *syntax.ValidationError
	if errors.As(err, &verr) {
		return msgCharacterInvalidName
	}
	if strings.TrimSpace(submitted) == "" {
		return msgCharacterNameBlank
	}
	return msgCharacterNameNoVisibleCharacter
}

// limitOrLocationMessage returns the authored copy for the two
// FailedPrecondition outcomes. They share a status but not a sentence: one is
// the player's own quota and the other is a server-side configuration gap, and
// telling a player they have "reached the character limit" when the grid has no
// starting location would send them off to retire a character for nothing.
func limitOrLocationMessage(code string) string {
	if code == "CHARACTER_LIMIT_REACHED" {
		return msgCharacterLimitReached
	}
	return msgCharacterNoStartingLocation
}
