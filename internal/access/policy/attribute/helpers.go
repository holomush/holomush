// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// parseEntityID extracts the entity ID from a resource ID with the
// expected type prefix. Returns the ID and true if the resource ID
// matches the expected type, otherwise empty string and false.
//
// Use this helper for providers whose resource ID is "<type>:<name>"
// where the name is a non-ULID string (Command, Exit, Scene, Stream).
// For ULID-typed providers (Location, Character, Property, Object)
// use [parseEntityResource] instead.
func parseEntityID(resourceID, expectedType string) (string, bool) {
	prefix := expectedType + ":"
	if !strings.HasPrefix(resourceID, prefix) {
		return "", false
	}
	return resourceID[len(prefix):], true
}

// parseEntityResource extracts a ULID from a typed resource ID of the
// shape "<type>:<ulid>". Centralizes the three branches every
// AttributeProvider with this ID grammar needs to encode
// (holomush-o8g6: LocationProvider / CharacterProvider /
// PropertyProvider / ObjectProvider).
//
// Returns:
//   - (id, true, nil) — well-formed resource ID matching expectedType
//   - (zero, false, nil) — wrong type prefix (peer provider handles), OR
//     matching type but ID is not a ULID (wildcard tolerance per
//     holomush-g776: capability grants emit "<type>:*", and the engine
//     evaluates target-type seed matches without per-instance attrs)
//   - (zero, false, error) — malformed grammar (no colon delimiter at
//     all). Distinct from the wildcard case: the resource ID does not
//     conform to the type:id shape and reflects a caller bug.
//
// The (false, nil) overload combines peer-type and wildcard cases
// because both produce the same engine behavior: per-instance
// attributes are not consulted. Returning an error in either case
// would fail-closed the entire bootstrap chain (holomush-g776
// regression history).
func parseEntityResource(entityID, expectedType string) (ulid.ULID, bool, error) {
	parts := strings.SplitN(entityID, ":", 2)
	if len(parts) != 2 {
		return ulid.ULID{}, false, oops.Code("INVALID_RESOURCE_ID").
			With("entity_id", entityID).
			With("expected_type", expectedType).
			Errorf("invalid entity ID format: expected 'type:id'")
	}
	if parts[0] != expectedType {
		return ulid.ULID{}, false, nil
	}
	id, err := ulid.Parse(parts[1])
	if err != nil {
		// Wildcard tolerance per holomush-g776: capability grants emit
		// "<type>:*" (e.g. CreateObject at internal/world/service.go:449).
		// The engine evaluates wildcard patterns via target-type seed
		// matching, no per-instance attrs needed. Returning the parse
		// error would fail-closed every CreateX call.
		return ulid.ULID{}, false, nil //nolint:nilerr // wildcard refs intentionally bypass provider; documented above
	}
	return id, true, nil
}
