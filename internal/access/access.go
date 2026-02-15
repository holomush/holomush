// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package access provides authorization for HoloMUSH.
//
// All parameters use prefixed string format:
//   - subject: "character:01ABC", "session:01XYZ", "plugin:echo-bot", "system"
//   - action: "read", "write", "emit", "execute", "grant"
//   - resource: "location:01ABC", "character:*", "stream:location:*"
package access

import (
	"context"
	"strings"
)

// AccessControl checks permissions for all subjects in HoloMUSH.
// This is the legacy interface retained for backward compatibility during migration to AccessPolicyEngine.
// See StaticAccessControl for dual-prefix migration details during Phase 7.6.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type AccessControl interface {
	// Check returns true if subject is allowed to perform action on resource.
	// Returns false for unknown subjects or denied permissions (deny by default).
	Check(ctx context.Context, subject, action, resource string) bool
}

// ParseSubject splits a subject string into prefix and ID.
// Returns ("system", "") for "system".
// Returns ("", subject) if no colon separator found.
func ParseSubject(subject string) (prefix, id string) {
	if subject == "" {
		return "", ""
	}
	if subject == "system" {
		return "system", ""
	}
	parts := strings.SplitN(subject, ":", 2)
	if len(parts) == 1 {
		return "", subject
	}
	return parts[0], parts[1]
}
