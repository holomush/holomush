// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import (
	"strings"

	"github.com/samber/oops"
)

// Subject prefix constants identify the type of entity making a request.
const (
	SubjectCharacter = "character:"
	SubjectPlugin    = "plugin:"
	SubjectSystem    = "system"
	SubjectSession   = "session:"
	SubjectPlayer    = "player:"
	// SubjectViewer is the web-viewer subject namespace (01-SPEC §8.4.1). A
	// viewer is the tier-ladder principal of the portal read path; it is
	// deliberately distinct from SubjectCharacter and SubjectPlayer, both of
	// which already match shipped seeds and would bypass the tier floor.
	SubjectViewer = "viewer:"
)

// Viewer tier tokens name the three rungs of the 01-SPEC §8.4.1 tier ladder.
// They are exported so no call site spells a tier as a string literal — a
// misspelled literal would build a subject no policy matches, which fails
// closed silently.
const (
	// ViewerTierAnonymous is the un-authenticated rung. It carries NO player
	// identifier: the subject string is the complete "viewer:anonymous".
	ViewerTierAnonymous = "anonymous"
	// ViewerTierGuest is the ephemeral-guest rung, carrying a player ULID.
	ViewerTierGuest = "guest"
	// ViewerTierPlayer is the registered-player rung, carrying a player ULID.
	ViewerTierPlayer = "player"
)

// Resource prefix constants identify the type of entity being accessed.
const (
	ResourceCharacter = "character:"
	ResourceLocation  = "location:"
	ResourceObject    = "object:"
	ResourceCommand   = "command:"
	ResourceProperty  = "property:"
	ResourceStream    = "stream:"
	ResourceExit      = "exit:"
	ResourceScene     = "scene:"
	ResourceKV        = "kv:"
	// ResourceCharacterDirectory is the singleton character-directory resource (no instance id).
	ResourceCharacterDirectory = "character_directory:"
	// ResourceProfile is the profile-reachability resource (01-SPEC §8.4.2).
	// It is deliberately NOT character: — reachability governs whether the
	// profile resolves at all, and character:<id>/read is already permitted
	// for character subjects by seed:admin-full-access.
	ResourceProfile = "profile:"
	// ResourceAdminSection is the admin-section resource family (01-SPEC
	// §10.4). The underscore-bearing type follows ResourceCharacterDirectory,
	// the in-tree precedent that such a type parses and matches through
	// `resource is <type>`.
	ResourceAdminSection = "admin_section:"
)

// Session error code constants.
// ErrCodeSessionInvalid uses "infra:" because invalid sessions are transient
// infrastructure failures (session expired/revoked) that callers should treat
// as ErrAccessEvaluationFailed, not hard policy denials.
const (
	ErrCodeSessionInvalid    = "infra:session-invalid"
	ErrCodeSessionStoreError = "infra:session-store-error"
)

// knownPrefixes lists all valid entity reference prefixes for validation.
var knownPrefixes = []string{
	SubjectCharacter,
	SubjectPlugin,
	SubjectSession,
	SubjectPlayer,
	SubjectViewer,
	ResourceCharacter,
	ResourceLocation,
	ResourceObject,
	ResourceCommand,
	ResourceProperty,
	ResourceStream,
	ResourceExit,
	ResourceScene,
	ResourceKV,
	ResourceCharacterDirectory,
	ResourceProfile,
	ResourceAdminSection,
}

// PluginSubject returns a properly formatted plugin subject identifier.
// Panics if name is empty, since an empty subject bypasses access control.
func PluginSubject(name string) string {
	if name == "" {
		panic("access.PluginSubject: empty name would bypass access control")
	}
	return SubjectPlugin + name
}

// CharacterSubject returns a properly formatted character subject identifier.
// This eliminates scattered string concatenation patterns throughout the codebase.
// Panics if charID is empty, since an empty subject bypasses access control.
func CharacterSubject(charID string) string {
	if charID == "" {
		panic("access.CharacterSubject: empty charID would bypass access control")
	}
	return SubjectCharacter + charID
}

// PlayerSubject returns the canonical ABAC subject ID for a player
// ("player:<ulid>"). Players are a Subject-namespace identity alongside
// characters; PlayerAttributeProvider resolves this namespace.
//
// Panics on empty playerID, mirroring the safety guard in the other
// helpers in this package — empty subject strings would silently bypass
// access control if returned as the bare prefix.
func PlayerSubject(playerID string) string {
	if playerID == "" {
		panic("access.PlayerSubject: empty playerID would bypass access control")
	}
	return SubjectPlayer + playerID
}

// ViewerSubject returns the canonical ABAC subject ID for a web viewer, per
// 01-SPEC §8.4.1's subject-form table:
//
//	anonymous → "viewer:anonymous"
//	guest     → "viewer:guest:<player-ulid>"
//	player    → "viewer:player:<player-ulid>"
//
// TRUST POSITION. The tier token MUST be derived from the server-side session
// state the gateway already authenticated. It MUST NOT be read from a
// client-supplied header, query parameter, cookie value or request field — a
// client-chosen tier is an authorization decision made by the attacker. This
// constructor sits in the same trust position [PlayerSubject] occupies today.
//
// Panic guard, and its one deliberate exception. Empty subject strings would
// silently bypass access control if returned as the bare prefix, so this panics
// on an empty playerID for the guest and player rungs, where an empty id would
// yield "viewer:guest:" or "viewer:player:" — a subject whose id half is empty.
// The ANONYMOUS rung is the exception: §8.4.1 gives it no identifier at all, so
// an empty second argument is the CORRECT call there and "viewer:anonymous" is a
// complete subject, not a bare prefix. It panics instead when the anonymous rung
// is handed a NON-empty identifier, because that signals a caller confused about
// which rung it is on.
//
// It also panics on any tier token outside the three ViewerTier* constants: an
// unrecognized tier MUST NOT be silently carried into a subject string.
func ViewerSubject(tier, playerID string) string {
	switch tier {
	case ViewerTierAnonymous:
		if playerID != "" {
			panic("access.ViewerSubject: anonymous rung takes no playerID — caller is on the wrong rung")
		}
		return SubjectViewer + tier
	case ViewerTierGuest, ViewerTierPlayer:
		if playerID == "" {
			panic("access.ViewerSubject: empty playerID would bypass access control")
		}
		return SubjectViewer + tier + ":" + playerID
	default:
		panic("access.ViewerSubject: unrecognized viewer tier would bypass access control")
	}
}

// CharacterResource returns a properly formatted character resource identifier.
// Note: ResourceCharacter has the same string value as SubjectCharacter ("character:").
// This is intentional: a character can be both a subject (who is acting) and a resource
// (what is being acted upon). The prefix is identical but the semantic role differs
// based on context (subject vs. resource parameter in access checks).
// Panics if charID is empty, since an empty resource creates an invalid reference.
func CharacterResource(charID string) string {
	if charID == "" {
		panic("access.CharacterResource: empty charID would create invalid resource reference")
	}
	return ResourceCharacter + charID
}

// LocationResource returns a properly formatted location resource identifier.
// It panics if locationID is empty.
func LocationResource(locationID string) string {
	if locationID == "" {
		panic("access.LocationResource: empty locationID would create invalid resource reference")
	}
	return ResourceLocation + locationID
}

// ExitResource returns a properly formatted exit resource identifier.
// It panics if exitID is empty.
func ExitResource(exitID string) string {
	if exitID == "" {
		panic("access.ExitResource: empty exitID would create invalid resource reference")
	}
	return ResourceExit + exitID
}

// ObjectResource returns a properly formatted object resource identifier.
// It panics if objectID is empty.
func ObjectResource(objectID string) string {
	if objectID == "" {
		panic("access.ObjectResource: empty objectID would create invalid resource reference")
	}
	return ResourceObject + objectID
}

// SceneResource returns a properly formatted scene resource identifier.
// It panics if sceneID is empty.
func SceneResource(sceneID string) string {
	if sceneID == "" {
		panic("access.SceneResource: empty sceneID would create invalid resource reference")
	}
	return ResourceScene + sceneID
}

// CommandResource returns a properly formatted command resource identifier.
// It panics if commandName is empty.
func CommandResource(commandName string) string {
	if commandName == "" {
		panic("access.CommandResource: empty commandName would create invalid resource reference")
	}
	return ResourceCommand + commandName
}

// PropertyResource returns a properly formatted property resource identifier.
// It panics if propPath is empty.
func PropertyResource(propPath string) string {
	if propPath == "" {
		panic("access.PropertyResource: empty propPath would create invalid resource reference")
	}
	return ResourceProperty + propPath
}

// StreamResource returns a properly formatted stream resource identifier.
// It panics if streamID is empty.
func StreamResource(streamID string) string {
	if streamID == "" {
		panic("access.StreamResource: empty streamID would create invalid resource reference")
	}
	return ResourceStream + streamID
}

// CharacterDirectoryResource returns the singleton directory resource ref.
// There is no per-instance variant: the character directory is server-wide.
func CharacterDirectoryResource() string { return ResourceCharacterDirectory + "all" }

// ProfileResource returns the profile-reachability resource reference for a
// character ("profile:<character-ulid>"), per 01-SPEC §8.4.2. Reachability is
// its own resource type: it governs whether the profile resolves at all, which
// no entity_properties row and no character attribute can express.
//
// It panics if characterID is empty — a bare "profile:" prefix would be an
// invalid reference that no policy target matches, failing closed invisibly.
func ProfileResource(characterID string) string {
	if characterID == "" {
		panic("access.ProfileResource: empty characterID would create invalid resource reference")
	}
	return ResourceProfile + characterID
}

// AdminSectionResource returns the admin-section resource reference
// ("admin_section:<section-id>"), per 01-SPEC §10.4. seed:admin-section-access
// is scoped by resource TYPE rather than by enumerated id, so this one family
// covers every current and future section at zero additional policy cost.
//
// It panics if sectionID is empty — a bare "admin_section:" prefix would be an
// invalid reference.
func AdminSectionResource(sectionID string) string {
	if sectionID == "" {
		panic("access.AdminSectionResource: empty sectionID would create invalid resource reference")
	}
	return ResourceAdminSection + sectionID
}

// KVResource returns a properly formatted key-value store resource identifier.
// Panics if namespace or key is empty, since either would create an invalid reference.
func KVResource(namespace, key string) string {
	if namespace == "" {
		panic("access.KVResource: empty namespace would bypass access control")
	}
	if key == "" {
		panic("access.KVResource: empty key would bypass access control")
	}
	return ResourceKV + namespace + ":" + key
}

// ParseEntityRef parses an entity reference string into its type name and ID.
// Returns an INVALID_ENTITY_REF error for empty, unknown, or legacy prefixes.
func ParseEntityRef(ref string) (typeName, id string, err error) {
	if ref == "" {
		return "", "", oops.
			Code("INVALID_ENTITY_REF").
			With("ref", ref).
			Errorf("empty entity reference")
	}

	if ref == SubjectSystem {
		return "system", "", nil
	}

	for _, prefix := range knownPrefixes {
		if strings.HasPrefix(ref, prefix) {
			typeName = strings.TrimSuffix(prefix, ":")
			id = ref[len(prefix):]
			if id == "" {
				return "", "", oops.
					Code("INVALID_ENTITY_REF").
					With("ref", ref).
					Errorf("empty ID in entity reference")
			}
			return typeName, id, nil
		}
	}

	return "", "", oops.
		Code("INVALID_ENTITY_REF").
		With("ref", ref).
		Errorf("unknown entity reference prefix: %s", ref)
}
