// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import (
	"context"
	"sync"

	"github.com/gobwas/glob"
	"github.com/holomush/holomush/internal/plugin/capability"
	"github.com/samber/oops"
)

// StaticAccessControl implements AccessControl with static role definitions.
// This is the MVP implementation before full ABAC.
type StaticAccessControl struct {
	roles    map[string][]compiledPermission // roleName → compiled permission patterns
	subjects map[string]string               // subjectID → roleName
	resolver LocationResolver
	enforcer *capability.Enforcer
	mu       sync.RWMutex
}

// compiledPermission holds a permission pattern and its compiled glob.
type compiledPermission struct {
	pattern string
	glob    glob.Glob
}

// NewStaticAccessControl creates a new static access controller.
// If resolver is nil, NullResolver is used.
// If enforcer is nil, plugin checks always return false.
func NewStaticAccessControl(resolver LocationResolver, enforcer *capability.Enforcer) *StaticAccessControl {
	if resolver == nil {
		resolver = NullResolver{}
	}

	// Compile default roles
	defaultRoles := DefaultRoles()
	compiledRoles := make(map[string][]compiledPermission, len(defaultRoles))
	for role, perms := range defaultRoles {
		compiled := make([]compiledPermission, 0, len(perms))
		for _, p := range perms {
			// Use ':' as separator for permission patterns
			g, err := glob.Compile(p, ':')
			if err != nil {
				// Skip invalid patterns (shouldn't happen with defaults)
				continue
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
	}
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
	case "char", "session":
		return s.checkRole(ctx, subject, action, resource)
	default:
		return false
	}
}

// checkPlugin delegates to capability.Enforcer.
func (s *StaticAccessControl) checkPlugin(pluginID, action, resource string) bool {
	if s.enforcer == nil {
		return false
	}
	// Convert action:resource to capability format: action.resource
	capStr := action + "." + resource
	return s.enforcer.Check(pluginID, capStr)
}

// checkRole checks if subject's role allows the action on resource.
func (s *StaticAccessControl) checkRole(_ context.Context, subject, action, resource string) bool {
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

	// Match against role permissions
	for _, perm := range permissions {
		if perm.glob.Match(requested) {
			return true
		}
	}

	return false
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
