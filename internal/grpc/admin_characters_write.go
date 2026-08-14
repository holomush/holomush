// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// The three admin CHARACTER WRITES, and the authorization argument behind them.
// Written for `abac-reviewer`, because this is the surface where an operator
// mutates a character they do not own.
//
// # TWO gates, not one, and neither is optional
//
// The FIRST is the admin-portal interceptor's `admin_section:characters` WRITE
// decision (D-99), made from the fail-closed section.AdminDescriptors table
// before any handler here runs. The action is `write`, distinct from the reads'
// `read`, so a policy may grant one without the other (§10.4).
//
// The SECOND is world.Service's own checkAccess on `character:<id>`, run inside
// each reused domain command: "write" for the profile edit, "retire" and
// "unretire" for the lifecycle transitions. That gate is reached by a
// PLAYER-flavoured caller (D-104 — Caller.subject is carried verbatim into the
// envelope Actor), and it is satisfied by exactly one seed policy,
// seed:admin-character-administration. Without it every write here is
// DEFAULT-DENIED one layer below the interceptor: seed:admin-full-access
// requires `principal is character` and never fires for a player principal, and
// seed:admin-section-access is scoped `resource is admin_section`.
//
// No handler here evaluates policy of its own, and none bypasses either gate.
// There is no raw repository call and no boolean isAdmin anywhere in this file.
//
// # What bounds the write
//
// adminProfileMaskablePaths — 01-SPEC §10.6's thirteen paths, `description`
// plus the twelve §7.2 `profile.*` names. Paths are compared by EXACT-STRING map
// lookup: no prefix, no wildcard, no glob, no dotted-subtree expansion (§9.5
// rule 2). An unlisted path is REJECTED, not ignored, so a direct gRPC caller
// cannot drive an unadvertised mutation through the mask. `roles` is not among
// them; role mutation is out of scope for v0.13 (PORTAL-08, #4899) and the
// schema-level fence for it is TestAdminCharacterMessagesCarryNoRoleBearingField
// in test/meta, which walks the generated descriptors rather than this source —
// because the risk §10.6 names is a future field on a GENERATED message, which
// no grep over this file could see.
//
// # There is no delete
//
// world.Service.DeleteCharacter is irreversible and cascades entity_properties
// (§4.4). No AdminDeleteCharacter RPC exists, and `delete` is absent from
// seed:admin-character-administration's action list, so the same guarantee holds
// at the policy layer where an RPC-level omission cannot be quietly undone.
// Retire is the only admin-reachable lifecycle exit and it is reversible.

package grpc

import (
	"context"
	"errors"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

// The admin section and action every write here is evaluated under, carried into
// the emitted envelope payload so an auditor learns which surface the change came
// through (§10.7). They mirror this file's three AdminDescriptors entries; a
// divergence would put a section in the audit trail that authorized nothing.
const (
	adminCharacterAuditSection = "characters"
	adminCharacterAuditAction  = "write"
)

// The static caller-fault messages. Each names nothing about the request, so a
// caller the section gate already permitted still learns nothing from the text.
const (
	adminCharacterMaskPathMessage    = "update_mask names a path outside the writable set"
	adminCharacterVersionMessage     = "expected_version is required and must be 1 or greater"
	adminCharacterFieldValueMessage  = "a submitted value is not acceptable"
	adminCharacterConcurrentMessage  = "the character changed since it was read"
	adminCharacterLifecycleMessage   = "the character is not in a state that permits this transition"
	adminCharacterWriterNotConfigStr = "internal error"
)

// adminProfileMaskField is what one maskable path resolves to: the request
// accessor carrying its new value, and the BYTE cap that value must satisfy.
//
// The accessor lives ON the allowlist entry rather than in a parallel switch so
// each of the thirteen §10.6 names is written down exactly ONCE in this file.
// A second table keyed by the same names is a divergence waiting to happen — and
// a mask path with no accessor, or an accessor with no allowlist entry, could not
// then exist.
//
// isDescription marks the ONE entry that is a COLUMN on characters rather than an
// entity_properties row, so it travels to the domain through
// world.WithDescription instead of through the attributes map.
type adminProfileMaskField struct {
	maxBytes      int
	isDescription bool
	value         func(req *adminportalv1.AdminUpdateCharacterRequest) string
}

// adminProfileMaskablePaths is the closed set of update_mask paths
// AdminUpdateCharacter accepts: 01-SPEC §10.6's THIRTEEN, which is `description`
// PLUS the twelve §7.2 profile names spelled as the entity_properties row names
// verbatim (§7.1).
//
// THIRTEEN, NOT TWELVE. Its sibling updateCharacterProfileMaskablePaths
// (characteraccess_write.go) carries only the twelve, because that RPC does not
// own `description` — the owner surface has its own UpdateCharacterDescription.
// `description` is easy to lose here precisely because every OTHER entry shares
// the `profile.` prefix, and dropping it is a real ADMIN-04 scope loss rather
// than a cosmetic difference. Never assert this list by counting a `profile.`
// prefix: thirteen paths yield TWELVE such matches, so a count of thirteen is
// unsatisfiable by a correct implementation and the only ways to "fix" it are to
// invent a thirteenth profile path or to delete `description`.
// TestAdminProfileMaskAllowlistMatchesSpec asserts SET EQUALITY against the
// checked-in §10.6 list, in both directions.
//
// The caps reuse the shipped world constants rather than minting new numbers.
// The seven short single-line fields cap at world.MaxNameLength; the five long
// multi-paragraph fields at world.MaxDescriptionLength. Both are measured in
// BYTES, exactly as world.ValidateDescription measures, so the admin Sheet's
// counter and the server cannot disagree about where the boundary is — and a
// ~34-rune CJK value that looks short is refused at the same boundary a 101-byte
// ASCII value is. `description`'s own cap lives in the DOMAIN
// (world.Character.SetDescription, D-82), so its entry carries the same number
// for the client's benefit and the domain remains the enforcing layer.
var adminProfileMaskablePaths = map[string]adminProfileMaskField{
	"description": {world.MaxDescriptionLength, true, func(r *adminportalv1.AdminUpdateCharacterRequest) string {
		return r.GetDescription()
	}},
	"profile.pronouns": {world.MaxNameLength, false, func(r *adminportalv1.AdminUpdateCharacterRequest) string {
		return r.GetPronouns()
	}},
	"profile.concept": {world.MaxNameLength, false, func(r *adminportalv1.AdminUpdateCharacterRequest) string {
		return r.GetConcept()
	}},
	"profile.species": {world.MaxNameLength, false, func(r *adminportalv1.AdminUpdateCharacterRequest) string {
		return r.GetSpecies()
	}},
	"profile.age": {world.MaxNameLength, false, func(r *adminportalv1.AdminUpdateCharacterRequest) string {
		return r.GetAge()
	}},
	"profile.faction": {world.MaxNameLength, false, func(r *adminportalv1.AdminUpdateCharacterRequest) string {
		return r.GetFaction()
	}},
	"profile.currently": {world.MaxNameLength, false, func(r *adminportalv1.AdminUpdateCharacterRequest) string {
		return r.GetCurrently()
	}},
	"profile.timezone": {world.MaxNameLength, false, func(r *adminportalv1.AdminUpdateCharacterRequest) string {
		return r.GetTimezone()
	}},
	"profile.appearance": {world.MaxDescriptionLength, false, func(r *adminportalv1.AdminUpdateCharacterRequest) string {
		return r.GetAppearance()
	}},
	"profile.personality": {world.MaxDescriptionLength, false, func(r *adminportalv1.AdminUpdateCharacterRequest) string {
		return r.GetPersonality()
	}},
	"profile.biography": {world.MaxDescriptionLength, false, func(r *adminportalv1.AdminUpdateCharacterRequest) string {
		return r.GetBiography()
	}},
	"profile.rumors": {world.MaxDescriptionLength, false, func(r *adminportalv1.AdminUpdateCharacterRequest) string {
		return r.GetRumors()
	}},
	"profile.rp_preferences": {world.MaxDescriptionLength, false, func(r *adminportalv1.AdminUpdateCharacterRequest) string {
		return r.GetRpPreferences()
	}},
}

// requireAdminGuardedVersion refuses a mutation whose expected_version is absent,
// zero or negative, BEFORE any domain call.
//
// Absent and explicit zero take the same branch and proto3's inability to tell
// them apart therefore costs nothing. Zero is never read as "write without the
// guard" and is never silently defaulted to the row's current version — the
// latter would turn every stale operator into a silent last-write-wins on a
// character they do not own.
func requireAdminGuardedVersion(ctx context.Context, expectedVersion int32) error {
	if expectedVersion > 0 {
		return nil
	}
	errutil.LogErrorContext(ctx, "admin characters: guarded mutation refused without a version",
		oops.Code("ADMIN_CHARACTER_VERSION_REQUIRED").
			With("expected_version", expectedVersion).
			Errorf("a guarded admin mutation requires a caller-supplied expected_version >= 1"))
	return status.Errorf(codes.InvalidArgument, adminCharacterVersionMessage)
}

// adminCharacterAuditContext is the one AuditContext every write here supplies.
func adminCharacterAuditContext() world.AuditContext {
	return world.AuditContext{Section: adminCharacterAuditSection, Action: adminCharacterAuditAction}
}

// adminWriteCaller builds the PLAYER-flavoured domain caller.
//
// D-104: Caller.subject is carried verbatim into the envelope Actor, so a
// character-flavoured caller would write a durable player-to-alt linkage into the
// RETAINED events_audit. The acting character is deliberately not recorded
// anywhere on this path.
func adminWriteCaller(playerID ulid.ULID) world.Caller {
	return world.HumanCaller(access.PlayerSubject(playerID.String()))
}

// AdminUpdateCharacter applies a partial edit to one character's thirteen
// §10.6-writable values.
//
// # ONE request is ONE domain write
//
// The mask is `description` PLUS twelve `profile.*` paths, but
// world.Service.UpdateCharacterProfileAttributes REJECTS `description` (its
// closed §7.2 name set) and `description` has its own domain command. Two domain
// calls would each bump characters.version, so the caller's SINGLE
// expected_version could not fund both: the second fails its own precheck, one
// RPC emits two envelopes in two transactions, and a partial commit is reachable.
//
// So the description travels as a world.WithDescription OPTION into the same
// call, under the same checkAccess, the same transaction, the same single version
// bump and the same one envelope.
//
// THE ADMIN PATH MUST NOT ROUTE A DESCRIPTION EDIT THROUGH
// UpdateCharacterDescription, for a second and independent reason: that command
// emits kindCharacterUpdated, whose declared payload carries a `description`
// STRING — the prose VALUE. Routing an admin description edit through it would
// write player-authored prose into the retained events_audit, which is exactly
// what D-103 forbids and what this phase's INV-PRIVACY registration asserts
// cannot happen.
//
// # An EMPTY mask is a NO-OP SUCCESS
//
// 01-SPEC §9.5 rule 4 says so, and §10.6 restates it for admin character editing
// by name: a request with no paths changes nothing and returns success. This
// response carries the AdminCharacter including its version, so it has exactly
// the channel world.Service's own empty-partition no-op says the domain METHOD
// lacks — that method refuses a stale caller because it returns only an error and
// has no way to hand the current version back.
//
// Authorization, existence and the expected_version guard all run FIRST. An empty
// mask is not a way to skip a check.
//
// ONE CLAUSE DIVERGES from the player facade, and this is a divergence rather
// than parity. characteraccess_write.go answers a STALE empty-mask caller with
// SUCCESS — its own comment reads "A STALE CALLER IS ANSWERED, NOT REFUSED" and
// its code returns the projection without ever comparing expectedVersion. This
// path returns Aborted instead. The facade's reasoning does not transfer: its
// caller is editing their OWN character and a stale version costs one wasted
// round trip, while this caller is editing SOMEONE ELSE'S on an audited surface,
// and a stale version guard means the request was composed against a view of that
// character which has since changed — possibly by the owner, possibly by another
// operator. Refusing is what makes the operator re-read before acting on a stale
// picture.
func (s *AdminPortalServer) AdminUpdateCharacter(
	ctx context.Context,
	req *adminportalv1.AdminUpdateCharacterRequest,
) (*adminportalv1.AdminUpdateCharacterResponse, error) {
	player, charID, err := s.adminWritePrecondition(ctx, req.GetCharacterId(), req.GetExpectedVersion())
	if err != nil {
		return nil, err
	}

	// The mask is a SET (§9.5 rule 3): collecting into a map makes ordering
	// irrelevant and a duplicated entry apply exactly once, by construction
	// rather than by a dedupe pass a later edit could drop.
	paths := req.GetUpdateMask().GetPaths()
	attributes := make(map[string]string, len(paths))
	descriptionSupplied := false
	descriptionValue := ""
	for _, path := range paths {
		field, ok := adminProfileMaskablePaths[path]
		if !ok {
			errutil.LogErrorContext(ctx, "admin characters: profile mask path rejected",
				oops.Code("ADMIN_CHARACTER_MASK_PATH_UNSUPPORTED").
					With("character_id", charID.String()).
					With("path", path).
					Errorf("update_mask path is outside the closed allowlist"))
			return nil, status.Errorf(codes.InvalidArgument, adminCharacterMaskPathMessage)
		}
		value := field.value(req)
		if valueErr := validateProfileValue(value, field.maxBytes); valueErr != nil {
			errutil.LogErrorContext(ctx, "admin characters: field value rejected", valueErr,
				"character_id", charID.String(), "path", path)
			return nil, status.Errorf(codes.InvalidArgument, adminCharacterFieldValueMessage)
		}
		if field.isDescription {
			// A COLUMN on characters, not a property row: it must NOT enter
			// attributes, whose closed §7.2 name-set validation would reject it.
			descriptionSupplied = true
			descriptionValue = value
			continue
		}
		attributes[path] = value
	}

	if len(paths) == 0 {
		// The §9.5 rule 4 no-op, reached only AFTER the guards above.
		return s.adminCharacterWriteResponseUpdate(ctx, charID)
	}

	opts := []world.ProfileUpdateOption{
		world.WithAuditContext(adminCharacterAuditContext()),
		// ADMIN-ONLY. Inside the transaction, after the rows are read and under
		// the CAS, a requested name whose stored value already equals the
		// requested value is dropped from the write partition — so a mask naming
		// only unchanged fields is a TRUE no-op: no row rewrite, no version bump,
		// no envelope. A handler precheck could not deliver this: the domain
		// rewrites an equal-valued row unconditionally regardless of what a
		// handler decided first, and a pre-transaction comparison reads rows it
		// holds no lock on. The player facade supplies no option and keeps its
		// documented unconditional-rewrite behaviour.
		world.WithSkipUnchangedProperties(),
	}
	if descriptionSupplied {
		opts = append(opts, world.WithDescription(descriptionValue))
	}

	if s.characterWriter == nil {
		return nil, adminCharacterWriterMissing(ctx)
	}
	if writeErr := s.characterWriter.UpdateCharacterProfileAttributes(
		ctx, adminWriteCaller(player), charID, int(req.GetExpectedVersion()), attributes, opts...,
	); writeErr != nil {
		return nil, mapAdminCharacterWriteError(ctx, writeErr, charID)
	}

	return s.adminCharacterWriteResponseUpdate(ctx, charID)
}

// AdminRetireCharacter soft-retires one character through the CANONICAL
// world.Service.RetireCharacter.
//
// Reusing that command rather than writing an admin-only lifecycle path is what
// keeps an admin transition and any future player-initiated one from diverging
// (ADMIN-05). It also inherits the whole shipped guard chain — the version
// precheck ahead of the lifecycle guard, and the CAS carrying the CALLER's
// expected version — none of which is restated here.
//
// A second call on an already-retired character is refused by that lifecycle
// guard before any write, so exactly one character.retired envelope can ever
// exist for one retirement.
func (s *AdminPortalServer) AdminRetireCharacter(
	ctx context.Context,
	req *adminportalv1.AdminRetireCharacterRequest,
) (*adminportalv1.AdminRetireCharacterResponse, error) {
	player, charID, err := s.adminWritePrecondition(ctx, req.GetCharacterId(), req.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	if s.characterWriter == nil {
		return nil, adminCharacterWriterMissing(ctx)
	}
	if writeErr := s.characterWriter.RetireCharacter(
		ctx, adminWriteCaller(player), charID, int(req.GetExpectedVersion()),
		world.WithAuditContext(adminCharacterAuditContext()),
	); writeErr != nil {
		return nil, mapAdminCharacterWriteError(ctx, writeErr, charID)
	}
	row, err := s.adminCharacterRowAfterWrite(ctx, charID)
	if err != nil {
		return nil, err
	}
	return &adminportalv1.AdminRetireCharacterResponse{Character: row}, nil
}

// AdminUnretireCharacter returns one retired character to play through the
// CANONICAL world.Service.UnretireCharacter, for the same reuse reason retire
// gives.
//
// It does NOT restore players.default_character_id: retire cleared that pointer
// in its own transaction and the old value is preserved nowhere, so the player
// re-selects a default. That asymmetry is by design.
func (s *AdminPortalServer) AdminUnretireCharacter(
	ctx context.Context,
	req *adminportalv1.AdminUnretireCharacterRequest,
) (*adminportalv1.AdminUnretireCharacterResponse, error) {
	player, charID, err := s.adminWritePrecondition(ctx, req.GetCharacterId(), req.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	if s.characterWriter == nil {
		return nil, adminCharacterWriterMissing(ctx)
	}
	if writeErr := s.characterWriter.UnretireCharacter(
		ctx, adminWriteCaller(player), charID, int(req.GetExpectedVersion()),
		world.WithAuditContext(adminCharacterAuditContext()),
	); writeErr != nil {
		return nil, mapAdminCharacterWriteError(ctx, writeErr, charID)
	}
	row, err := s.adminCharacterRowAfterWrite(ctx, charID)
	if err != nil {
		return nil, err
	}
	return &adminportalv1.AdminUnretireCharacterResponse{Character: row}, nil
}

// adminWritePrecondition runs the three checks every write here shares, in the
// order §9.5 requires: the version guard, the id parse, and existence.
//
// EXISTENCE IS CHECKED BEFORE THE DOMAIN CALL so an empty-mask no-op cannot be
// used as a way to skip it, and so a missing character answers NotFound rather
// than whatever the domain would have said. The version guard runs FIRST because
// a zero version is a caller fault that needs no read to diagnose.
func (s *AdminPortalServer) adminWritePrecondition(
	ctx context.Context,
	characterID string,
	expectedVersion int32,
) (playerID, charID ulid.ULID, err error) {
	playerSession, ok := AdminPlayerFromContext(ctx)
	if !ok {
		// Unreachable through the gated server: the interceptor stashes the
		// player before it calls any handler. Reached only by a composition that
		// mounted this service WITHOUT the gate, which must fail closed.
		errutil.LogErrorContext(ctx, "admin portal: no resolved player on context", nil)
		return ulid.ULID{}, ulid.ULID{}, status.Errorf(codes.Internal, adminCharacterWriterNotConfigStr)
	}
	if versionErr := requireAdminGuardedVersion(ctx, expectedVersion); versionErr != nil {
		return ulid.ULID{}, ulid.ULID{}, versionErr
	}
	id, parseErr := ulid.Parse(characterID)
	if parseErr != nil {
		// An unparseable id and an absent row answer IDENTICALLY, exactly as the
		// read path answers them, so neither is an existence oracle.
		return ulid.ULID{}, ulid.ULID{}, status.Errorf(codes.NotFound, adminCharacterNotFoundMessage)
	}
	if s.characters == nil {
		return ulid.ULID{}, ulid.ULID{}, adminCharacterReaderMissing(ctx, "character")
	}
	if _, err := s.characters.AdminGetCharacterRow(ctx, id); err != nil {
		if errors.Is(err, world.ErrNotFound) {
			return ulid.ULID{}, ulid.ULID{}, status.Errorf(codes.NotFound, adminCharacterNotFoundMessage)
		}
		return ulid.ULID{}, ulid.ULID{}, mapAdminCharacterError(ctx, err, "operation", "admin_character_write_precondition")
	}
	return playerSession.PlayerID, id, nil
}

// adminCharacterRowAfterWrite re-reads the row so the response carries the
// POST-WRITE state — in particular the bumped characters.version the client sends
// back as its next expected_version. Guessing it client-side is how a correct
// client becomes a stale one after its first successful edit.
//
// EVERY CALLER REACHES IT ONLY AFTER THE DOMAIN WRITE COMMITTED, so no branch of
// it may claim an authorization outcome or a NotFound: the row being gone in the
// window would tell the operator "no such character" for an edit that LANDED.
// codes.Internal is the honest answer for the whole leg — the write is durable,
// only the read-back failed, and the recovery is to re-read.
func (s *AdminPortalServer) adminCharacterRowAfterWrite(
	ctx context.Context,
	charID ulid.ULID,
) (*adminportalv1.AdminCharacter, error) {
	if s.characters == nil {
		return nil, adminCharacterReaderMissing(ctx, "character")
	}
	row, err := s.characters.AdminGetCharacterRow(ctx, charID)
	if err != nil {
		errutil.LogErrorContext(ctx, "admin characters: post-write read-back failed", err,
			"character_id", charID.String())
		return nil, status.Errorf(codes.Internal, adminCharacterWriterNotConfigStr)
	}
	return adminCharacterMessage(row), nil
}

func (s *AdminPortalServer) adminCharacterWriteResponseUpdate(
	ctx context.Context,
	charID ulid.ULID,
) (*adminportalv1.AdminUpdateCharacterResponse, error) {
	row, err := s.adminCharacterRowAfterWrite(ctx, charID)
	if err != nil {
		return nil, err
	}
	return &adminportalv1.AdminUpdateCharacterResponse{Character: row}, nil
}

// adminCharacterWriterMissing refuses a call this server was not wired to answer.
//
// It is codes.Internal and NEVER a silent success: reporting success for a write
// that reached no domain command would tell an operator they changed something
// they did not.
func adminCharacterWriterMissing(ctx context.Context) error {
	errutil.LogErrorContext(ctx, "admin characters: world character writer not configured", nil)
	return status.Errorf(codes.Internal, adminCharacterWriterNotConfigStr)
}

// mapAdminCharacterWriteError translates a DOMAIN error at THIS one layer.
//
// Translating at exactly one layer is not tidiness: a double translation breaks
// status.FromError chain-walking, because the inner conversion produces a fresh
// error carrying no GRPCStatus method. No inner error is ever formatted into a
// status message — the id and the domain error go to the log only.
func mapAdminCharacterWriteError(ctx context.Context, err error, charID ulid.ULID) error {
	switch {
	case errors.Is(err, world.ErrConcurrentEdit):
		return status.Errorf(codes.Aborted, adminCharacterConcurrentMessage)
	case errors.Is(err, world.ErrNotFound):
		return status.Errorf(codes.NotFound, adminCharacterNotFoundMessage)
	case errors.Is(err, world.ErrPermissionDenied):
		// The world-layer gate refused. It is reported with the SAME opaque
		// message the section gate uses, so the two layers are indistinguishable
		// on the wire and neither becomes an oracle for the other.
		errutil.LogErrorContext(ctx, "admin characters: world-layer authorization refused the write", err,
			"character_id", charID.String())
		return status.Errorf(codes.PermissionDenied, adminDeniedMessage)
	}
	switch adminSectionCode(err) {
	case "CHARACTER_ALREADY_RETIRED", "CHARACTER_NOT_RETIRED":
		return status.Errorf(codes.FailedPrecondition, adminCharacterLifecycleMessage)
	case "CHARACTER_NOT_FOUND":
		return status.Errorf(codes.NotFound, adminCharacterNotFoundMessage)
	case "CHARACTER_VERSION_REQUIRED":
		return status.Errorf(codes.InvalidArgument, adminCharacterVersionMessage)
	case "CHARACTER_PROFILE_ATTRIBUTE_UNKNOWN":
		return status.Errorf(codes.InvalidArgument, adminCharacterMaskPathMessage)
	case world.CodeCharacterInvalid:
		return status.Errorf(codes.InvalidArgument, adminCharacterFieldValueMessage)
	}
	errutil.LogErrorContext(ctx, "admin characters: write failed", err, "character_id", charID.String())
	return status.Errorf(codes.Internal, adminCharacterWriterNotConfigStr)
}
