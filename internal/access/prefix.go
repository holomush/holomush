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
)

// Session error code constants for infrastructure-level session errors.
const (
	ErrCodeSessionInvalid    = "infra:session-invalid"
	ErrCodeSessionStoreError = "infra:session-store-error"
)

// knownPrefixes lists all valid entity reference prefixes for validation.
var knownPrefixes = []string{
	SubjectCharacter,
	SubjectPlugin,
	SubjectSession,
	ResourceCharacter,
	ResourceLocation,
	ResourceObject,
	ResourceCommand,
	ResourceProperty,
	ResourceStream,
	ResourceExit,
	ResourceScene,
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
