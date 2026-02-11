// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package policy defines constants and utilities for ABAC entity references.
package policy

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
	ResourceLocation = "location:"
	ResourceObject   = "object:"
	ResourceCommand  = "command:"
	ResourceProperty = "property:"
	ResourceStream   = "stream:"
	ResourceExit     = "exit:"
	ResourceScene    = "scene:"
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
	ResourceLocation,
	ResourceObject,
	ResourceCommand,
	ResourceProperty,
	ResourceStream,
	ResourceExit,
	ResourceScene,
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
			return typeName, id, nil
		}
	}

	return "", "", oops.
		Code("INVALID_ENTITY_REF").
		With("ref", ref).
		Errorf("unknown entity reference prefix: %s", ref)
}
