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
	"github.com/holomush/holomush/internal/access/policy/types"
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

// characterAccessWorldMutator is the narrow interface CharacterAccessServer
// needs from world.Service — only the two authorized WRITE commands the owner
// mutation surface issues, and no third.
//
// THE ABSENCES ARE LOAD-BEARING HERE TOO, and this is where the D-79 compile
// fence matters most. PropertyRepository.ListByParent, PropertyReader.ListByParent
// and every other property-repository method are deliberately absent from this
// type set, exactly as they are from characterAccessWorldReader, so a direct
// property write from this facade is a compile error rather than a review
// finding. The profile-attribute write reaches entity_properties rows ONLY
// through world.Service.UpdateCharacterProfileAttributes, which owns the
// create/update/delete partition, the caller-version CAS and the
// same-transaction envelope. This facade names the two commands and nothing
// beneath them.
//
// IT IS A SEPARATE INTERFACE FROM characterAccessWorldReader ON PURPOSE. Widening
// the reader with these two methods would need no wiring at all — *world.Service
// already satisfies both — which is precisely its appeal and precisely why it is
// wrong: the type's name and its contract both say READER, and a reader that
// writes hands the next reader a precedent for routing any domain call through
// it. One interface per capability.
type characterAccessWorldMutator interface {
	UpdateCharacterDescription(ctx context.Context, subjectID world.Caller, characterID ulid.ULID, description string) error
	UpdateCharacterProfileAttributes(ctx context.Context, caller world.Caller, characterID ulid.ULID, expectedVersion int, attributes map[string]string) error
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

// characterAccessDirectoryReader is the narrow interface CharacterAccessServer
// needs to ENUMERATE characters — one method, and no second.
//
// Its signature is verbatim auth.CharacterRepository.ListAll
// (internal/auth/character_service.go:38-42), which is already the directory
// seam the retired RPC read through: id and name only, fetch-all with no
// pagination, ordered by name ascending, carrying no connection state. It is
// satisfied WITHOUT NEW PRODUCTION CODE by bootstrapsetup.CharRepoAdapter at the
// production wiring site and by the harness's own adapter under test.
//
// THE ABSENCES ARE LOAD-BEARING HERE TOO (PROFILE-10, D-79). Adding a third and
// a fourth interface to this facade does not weaken the compile fence, because
// the fence IS the absence of PropertyReader.ListByParent and
// PropertyRepository.ListByParent from every interface the facade holds. This
// one names ListAll and nothing else, so it cannot reach a property row.
//
// world.Service was deliberately NOT widened with a ListCharacters instead. A
// new ABAC-gated domain read would have to choose between one bulk decision and
// N per-row decisions — the choice 01-SPEC §9.2 already makes at the facade (one
// directory decision plus the §8.7 membership pass) — so a second,
// differently-shaped authorization point inside world for the same question is
// the duplicate gate §2.6 warns against.
type characterAccessDirectoryReader interface {
	ListAll(ctx context.Context) ([]*world.Character, error)
}

// characterAccessPolicyEvaluator is the narrow slice of the ABAC engine this
// facade needs to make ONE raw authorization decision of its own: 01-SPEC
// §9.2's directory gate, on access.CharacterDirectoryResource().
//
// The signature is verbatim (*policy.Engine).Evaluate
// (internal/access/policy/engine.go:82), and identical to the shipped
// consumer-side slice profilevis.PolicyEvaluator (profilevis.go:99-101) — the
// same one-method shape declared at the consumer for the same reason. It is
// satisfied at the production wiring site by the *policy.Engine already in
// scope, and under test by the harness's types.AccessPolicyEngine, whose first
// method is this one. No new production type exists anywhere for it.
//
// A METHOD ON profilevis.Evaluator WAS REJECTED. It would have needed no wiring
// at all, which is its whole appeal; but that package is scoped by its own doc
// to §8.5.1's per-attribute conjunction and §8.4.2 reachability — profile
// VISIBILITY. A bulk directory-enumeration gate is a different question on a
// different resource type, and widening that package to host it makes its name a
// lie and invites the next reader to route more §9.x decisions through it.
//
// GATING ON profilevis.Reachable INSTEAD IS THE ONE THING THIS SEAM EXISTS TO
// PREVENT. Per-character reachability is the §8.7 MEMBERSHIP rule; substituting
// it for the gate deletes §9.2's decision altogether and makes "the directory is
// closed" and "this character is not listed" the same sentence.
//
// THE ABSENCES ARE LOAD-BEARING, as on every sibling: it names Evaluate and
// nothing else, so it cannot reach a property row either.
type characterAccessPolicyEvaluator interface {
	Evaluate(ctx context.Context, req types.AccessRequest) (types.Decision, error)
}

// characterProfileNotFoundMessage is the ONE generic wire message every
// non-resolving profile read returns — below the reachability floor, denied at
// the description gate, or naming no row at all. 01-SPEC §8.7 requires the cases
// to be indistinguishable, and §9.6.1 asserts that over the wire, so the
// literal is shared rather than respelled per branch. It deliberately does not
// contain the internal code string.
const characterProfileNotFoundMessage = "character profile not found"

// characterGuestDenialMessage is the wire message the character facade's OWNER
// audience denies a guest with. It is deliberately NOT sceneGuestDenialMessage:
// a caller reaching ListMyCharacters never touched the scene subsystem, and a
// denial naming one would disclose the shape of a surface the request never
// reached (01-SPEC §9.6's wire-opacity posture). It names no subsystem the
// caller cannot see and no reason beyond the one that applies.
const characterGuestDenialMessage = "guests do not have characters of their own"

// characterAccessLogPrefix names the CHARACTER surface in the log lines the
// shared gate emits on this facade's behalf. It is the same prefix every other
// log line in this facade already uses, so an operator triaging a
// character-surface outage finds the gate's lines with the same grep.
const characterAccessLogPrefix = "character access"

// characterParentType is the entity_properties.parent_type discriminator the
// profile enumeration filters on. Profile rows hang off the character row, not
// off a location or an object.
const characterParentType = "character"

// codeProfileJoinDivergence marks the one impossible-by-construction outcome of
// the admissibility-set/value-source join: the visibility evaluator named a row
// id the enumeration never supplied. See resolveVisibleProfile.
const codeProfileJoinDivergence = "CHARACTER_PROFILE_ATTRIBUTE_JOIN_DIVERGENCE"

// CharacterAccessServer is the host-side facade for the web character surface.
// It constructs the viewer principal itself and owns every reachability and
// visibility decision, so the gateway forwards bytes and computes nothing.
type CharacterAccessServer struct {
	characteraccessv1.UnimplementedCharacterAccessServiceServer

	// playerGate is the shared OWNER-audience gate (plan 04-02): the one guest
	// gate (INV-SCENE-64) and the one server-side ownership resolution
	// (INV-SCENE-63) in this package. It is embedded BARE, so both methods and
	// all three repository handles promote onto this type.
	//
	// IT IS ALSO THE VIEWER-IDENTITY SEAM'S STORAGE. D-83 locks that the viewer
	// tier is resolved IN THE FACADE, at viewer-principal construction, and that
	// the facade is the ONLY layer holding the session — a facade with no
	// session repository cannot satisfy that sentence. Those handles live here
	// rather than in a second pair of fields of the facade's own: two copies of
	// one repository handle on one struct is a divergence waiting to happen, so
	// resolveViewerIdentity reads s.playerSessionRepo and s.playerRepo through
	// promotion.
	//
	// EMBEDDING THE GATE DOES NOT UNIFY THE TWO AUDIENCES. resolveAndGate
	// denies EVERY guest; the public read must YIELD a guest rung. Both
	// resolvers ship and answer different questions — see resolveViewerIdentity.
	playerGate

	world characterAccessWorldReader
	// worldMutator is the owner mutation surface's ONLY route to the domain.
	// It is satisfied at both construction sites by the same *world.Service
	// value `world` above receives — one added constructor argument, no new
	// production type and no third construction site.
	worldMutator characterAccessWorldMutator
	profileVis   characterAccessProfileVisibility
	// policyEval makes the ONE raw ABAC decision this facade owns: §9.2's
	// directory gate. Every other authorization question here is asked through
	// profileVis or through world.Service's own checkAccess, and this handle
	// exists precisely so the directory gate is not approximated by one of them.
	policyEval characterAccessPolicyEvaluator
	// directory is the character enumeration the public directory reads, and the
	// only bulk read this facade performs.
	directory characterAccessDirectoryReader
}

// NewCharacterAccessServer constructs a CharacterAccessServer. Every dependency
// is required; there are no optional setters.
//
// charRepo is the one handle the owner audience adds over plan 04-01's four:
// the session and player repositories were already here for the viewer-identity
// seam, so building the shared gate costs one argument rather than three.
//
// worldMutator is the one handle the owner MUTATION surface adds. Both call
// sites pass the same *world.Service value they already pass for worldReader, so
// the write surface costs one argument and no new production type.
//
// policyEval and directoryReader are the two the public DIRECTORY adds, and
// neither introduces a production type either: policyEval is satisfied by the
// *policy.Engine already in scope at the wiring site (the same engine the
// profilevis.Evaluator a line away is built over), and directoryReader by the
// character-repository adapter already built there for charRepo.
func NewCharacterAccessServer(
	worldReader characterAccessWorldReader,
	worldMutator characterAccessWorldMutator,
	profileVis characterAccessProfileVisibility,
	policyEval characterAccessPolicyEvaluator,
	directoryReader characterAccessDirectoryReader,
	playerSessionRepo auth.PlayerSessionRepository,
	playerRepo auth.PlayerRepository,
	charRepo auth.CharacterRepository,
) *CharacterAccessServer {
	return &CharacterAccessServer{
		playerGate:   newPlayerGate(playerSessionRepo, playerRepo, charRepo, characterGuestDenialMessage, characterAccessLogPrefix),
		world:        worldReader,
		worldMutator: worldMutator,
		profileVis:   profileVis,
		policyEval:   policyEval,
		directory:    directoryReader,
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

	profile, err := s.resolveVisibleProfile(ctx, viewerSubject, characterID)
	if err != nil {
		return nil, err
	}

	return &characteraccessv1.GetCharacterProfileResponse{
		Character: projectPublic(characterID.String(), desc, profile),
	}, nil
}

// resolveVisibleProfile returns the (name, value) pairs 01-SPEC §8.5.1's
// conjunction admits for this viewer, and nothing else.
//
// # One enumeration, two views of it
//
// ListPropertiesByParent is called EXACTLY ONCE, with the viewer's own Caller.
// That call applies TERM B only — it evaluates `read` on `property:<id>` and
// nothing else. Its result slice is then walked ONCE and read two ways:
//
//   - byID, keyed on the row id — the VALUE SOURCE;
//   - candidates, carrying that same id and the row name — the EVALUATOR INPUT.
//
// VisibleAttributes then re-evaluates reachability and, per row, term A
// (read_profile_attribute) AND term B (read). Its returned map is the
// ADMISSIBILITY SET, NOT A VALUE SOURCE: profilevis.Property is {ID, Name} and
// nothing else, carrying no value, no image payload and no gallery entry by
// design. A projection built from it alone yields a response with the right
// KEYS and EMPTY VALUES — which is why the values are joined back out of byID.
//
// Do NOT "fix" a missing value by enumerating a second time, by widening
// profilevis.Property to carry one, or by reaching for a property repository.
// The last does not compile (the facade's narrow interfaces name no
// ListByParent, D-79) and the first two are exactly the unfiltered-source
// improvisation criterion 5 forbids. byID is built from the IDENTICAL
// term-B-filtered slice that fed the evaluator; there is no second read and no
// unfiltered source anywhere in this function.
//
// # Term B is deliberately evaluated twice
//
// Once by the enumeration and once inside the conjunction. `A and B and B` is
// `A and B`, so the redundancy costs a policy evaluation and changes no verdict.
// The alternative — enumerating with a non-viewer caller — either requests the
// system bypass (handing the facade every row, contradicting criterion 5
// outright) or empties the compile fence of meaning. The redundancy is the
// cheaper of the two.
//
// # A divergence is an error
//
// An id in the visible map with no row in byID cannot happen: both sets come
// from one slice. If it does, the evaluator returned something the enumeration
// never supplied, and neither available shortcut is acceptable — emitting the
// field with a zero value is the empty-value regression, and dropping it
// silently renders a broken evaluator as a legitimately sparse profile, which
// §8.10 forbids. It returns Internal through the same logged path an evaluation
// failure takes.
func (s *CharacterAccessServer) resolveVisibleProfile(ctx context.Context, viewerSubject string, characterID ulid.ULID) (map[string]string, error) {
	rows, err := s.world.ListPropertiesByParent(ctx, world.HumanCaller(viewerSubject), characterParentType, characterID)
	if err != nil {
		// An enumeration failure is an OUTAGE. world.Service filters an ordinary
		// policy denial out of the slice silently and returns an error only when
		// nothing could be evaluated, so surfacing this as a successful sparse
		// profile would be §8.10's forbidden masking.
		errutil.LogErrorContext(ctx, "character access: profile property enumeration failed", err,
			"character_id", characterID.String())
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}

	byID := make(map[string]*world.EntityProperty, len(rows))
	candidates := make([]profilevis.Property, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		id := row.ID.String()
		byID[id] = row
		candidates = append(candidates, profilevis.Property{ID: id, Name: row.Name})
	}

	visible, err := s.profileVis.VisibleAttributes(ctx, viewerSubject, characterID.String(), candidates)
	if err != nil {
		return nil, mapProfileError(ctx, err)
	}

	admitted := make(map[string]string, len(visible))
	for name, prop := range visible {
		row, ok := byID[prop.ID]
		if !ok {
			divergence := oops.Code(codeProfileJoinDivergence).
				With("attribute", name).
				With("property_id", prop.ID).
				With("character_id", characterID.String()).
				Errorf("the visibility evaluator admitted a property row the enumeration never supplied")
			errutil.LogErrorContext(ctx, "character access: profile attribute join diverged", divergence)
			return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
		}
		if row.Value == nil {
			// A NULL value column is a flag-style row, not a blank field. Either
			// way it is OMITTED: §7.5 requires a blank field and a withheld field
			// to be indistinguishable on the wire. projectPublic drops the
			// empty-string case for the same reason.
			continue
		}
		admitted[name] = *row.Value
	}
	return admitted, nil
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
