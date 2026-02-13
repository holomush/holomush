// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import "strings"

// parseEntityID extracts the entity ID from a resource ID with the expected type prefix.
// Returns the ID and true if the resource ID matches the expected type, otherwise empty string and false.
func parseEntityID(resourceID, expectedType string) (string, bool) {
	prefix := expectedType + ":"
	if !strings.HasPrefix(resourceID, prefix) {
		return "", false
	}
	return resourceID[len(prefix):], true
}
