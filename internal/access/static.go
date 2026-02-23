// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/gobwas/glob"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/plugin/capability"
)

// StaticAccessControl implements AccessControl with static role definitions.
// This is the MVP implementation before full ABAC.
//
// Thread-safety: roles is immutable after construction and requires no synchronization.
// Only subjects is mutable and protected by mu.
type StaticAccessControl struct {
	roles    map[string][]compiledPermission // roleName → compiled permission patterns (immutable)
	subjects map[string]string               // subjectID → roleName (mutable, protected by mu)
	resolver LocationResolver
	enforcer *capability.Enforcer
	mu       sync.RWMutex // protects subjects only
}

// compiledPermission holds a permission pattern and its compiled glob.
type compiledPermission struct {
	pattern string
	glob    glob.Glob
}

// NewStaticAccessControl creates a new static access controller with default roles.
// If resolver is nil, NullResolver is used.
// If enforcer is nil, plugin checks always return false.
//
// Panics if default roles contain invalid permission patterns (configuration bug).
func NewStaticAccessControl(resolver LocationResolver, enforcer *capability.Enforcer) *StaticAccessControl {
	ac, err := NewStaticAccessControlWithRoles(DefaultRoles(), resolver, enforcer)
	if err != nil {
		// DefaultRoles() patterns are hardcoded and should always be valid.
		// If they fail to compile, it's a code bug that should fail fast.
		panic("invalid permission pattern in DefaultRoles: " + err.Error())
	}
	return ac
}

// NewStaticAccessControlWithRoles creates a new static access controller with custom roles.
// If resolver is nil, NullResolver is used.
// If enforcer is nil, plugin checks always return false.
//
// Returns error if any permission pattern fails to compile (invalid glob syntax).
func NewStaticAccessControlWithRoles(roles map[string][]string, resolver LocationResolver, enforcer *capability.Enforcer) (*StaticAccessControl, error) {
	if resolver == nil {
		resolver = NullResolver{}
	}

	// Compile roles
	compiledRoles := make(map[string][]compiledPermission, len(roles))
	for role, perms := range roles {
		compiled := make([]compiledPermission, 0, len(perms))
		for _, p := range perms {
			// Use ':' as separator for permission patterns
			g, err := glob.Compile(p, ':')
			if err != nil {
				return nil, oops.In("access").
					Code("INVALID_PERMISSION_PATTERN").
					With("role", role).
					With("pattern", p).
					Wrap(err)
			}
			compiled = append(compiled, compiledPermission{pattern: p, glob: g})
		}
		compiledRoles[role] = compiled
	}

	return &StaticAccessControl{
		roles:    compiledRoles,
		subjects: make(map[string]string),
		resolver: resolver,
		enforcer: enforcer,
	}, nil
}

// Check implements AccessControl.
func (s *StaticAccessControl) Check(ctx context.Context, subject, action, resource string) bool {
	// System always allowed
	if subject == "system" {
		return true
	}

	// Empty subject denied
	if subject == "" {
		return false
	}

	prefix, id := ParseSubject(subject)

	switch prefix {
	case "plugin":
		return s.checkPlugin(id, action, resource)
	case "char", "character", "session":
		// Migration note: During the Phase 7.6 migration to AccessPolicyEngine, both
		// "char:" and "character:" subject prefixes are accepted. New code MUST use
		// the SubjectCharacter constant ("character:"). The legacy "char:" prefix is
		// scheduled for removal in Phase 7.7 (tracked by holomush-c6qch).
		return s.checkRole(ctx, subject, action, resource)
	default:
		return false
	}
}

// checkPlugin delegates to capability.Enforcer.
func (s *StaticAccessControl) checkPlugin(pluginID, action, resource string) bool {
	if s.enforcer == nil {
		// Log at warn level - nil enforcer is a configuration error that means
		// all plugin permissions will be denied
		slog.Warn("plugin permission check with nil enforcer",
			"plugin", pluginID,
			"action", action,
			"resource", resource)
		return false
	}
	// Convert action:resource to capability format: action.resource
	capStr := action + "." + resource
	allowed := s.enforcer.Check(pluginID, capStr)
	if !allowed {
		slog.Debug("plugin permission denied by enforcer",
			"plugin", pluginID,
			"capability", capStr)
	}
	return allowed
}

// checkRole checks if subject's role allows the action on resource.
func (s *StaticAccessControl) checkRole(ctx context.Context, subject, action, resource string) bool {
	s.mu.RLock()
	role := s.subjects[subject]
	s.mu.RUnlock()

	if role == "" {
		return false // Unknown subject
	}

	permissions := s.roles[role]
	if permissions == nil {
		return false
	}

	requested := action + ":" + resource

	// Extract subject ID for token resolution
	_, subjectID := ParseSubject(subject)
	currentLocation := s.resolveCurrentLocation(ctx, subjectID)

	// Match against role permissions
	for _, perm := range permissions {
		// Resolve tokens in the permission pattern
		resolvedPattern := s.resolveTokens(perm.pattern, subjectID, currentLocation)

		// If pattern changed, recompile and match
		if resolvedPattern != perm.pattern {
			g, err := glob.Compile(resolvedPattern, ':')
			if err != nil {
				// Log at warn level - pattern compilation failure causes silent permission denial
				slog.Warn("failed to compile resolved permission pattern",
					"subject", subject,
					"action", action,
					"pattern", perm.pattern,
					"resolved", resolvedPattern,
					"error", err)
				continue
			}
			if g.Match(requested) {
				return true
			}
		} else if perm.glob.Match(requested) {
			return true
		}
	}

	return false
}

// resolveTokens replaces $self and $here with actual values.
func (s *StaticAccessControl) resolveTokens(pattern, subjectID, locationID string) string {
	r := strings.NewReplacer("$self", subjectID, "$here", locationID)
	return r.Replace(pattern)
}

// resolveCurrentLocation gets the character's current location.
// Returns empty string on error (fail-closed security posture).
func (s *StaticAccessControl) resolveCurrentLocation(ctx context.Context, charID string) string {
	if s.resolver == nil {
		return ""
	}
	loc, err := s.resolver.CurrentLocation(ctx, charID)
	if err != nil {
		// Log at warn level - infrastructure issue affecting permission checks
		slog.Warn("failed to resolve current location for access check",
			"charID", charID,
			"error", err)
		return ""
	}
	return loc
}

// AssignRole sets the role for a subject.
// Returns error if subject or role is empty, or role is unknown.
func (s *StaticAccessControl) AssignRole(subject, role string) error {
	if subject == "" {
		return oops.In("access").Code("INVALID_SUBJECT").New("subject cannot be empty")
	}
	if role == "" {
		return oops.In("access").Code("INVALID_ROLE").New("role cannot be empty")
	}
	if _, ok := s.roles[role]; !ok {
		return oops.In("access").Code("UNKNOWN_ROLE").With("role", role).New("unknown role")
	}

	s.mu.Lock()
	s.subjects[subject] = role
	s.mu.Unlock()

	return nil
}

// RevokeRole removes a subject's role assignment.
// Returns error if subject is empty.
func (s *StaticAccessControl) RevokeRole(subject string) error {
	if subject == "" {
		return oops.In("access").Code("INVALID_SUBJECT").New("subject cannot be empty")
	}

	s.mu.Lock()
	delete(s.subjects, subject)
	s.mu.Unlock()

	return nil
}

// GetRole returns the role assigned to a subject, or empty string if none.
func (s *StaticAccessControl) GetRole(subject string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subjects[subject]
}
