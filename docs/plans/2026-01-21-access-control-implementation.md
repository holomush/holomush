# Core Access Control Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the AccessControl interface and StaticAccessControl for role-based authorization in HoloMUSH.

**Architecture:** Single `AccessControl` interface wrapping static roles with dynamic token resolution (`$self`, `$here`). Delegates plugin checks to existing `capability.Enforcer`. Broadcaster filters event delivery based on permissions.

**Tech Stack:** Go 1.23, gobwas/glob for pattern matching, testify for assertions

---

## Prerequisites

**Design spec:** `docs/specs/2026-01-21-access-control-design.md`

**Existing code to understand:**

- `internal/plugin/capability/enforcer.go` - Pattern matching with gobwas/glob
- `internal/core/broadcaster.go` - Event distribution
- `internal/core/session.go` - Session management

**Run before starting:**

```bash
task test  # Ensure baseline passes
```

---

## Task 1: Create AccessControl Interface

**Files:**

- Create: `internal/access/access.go`
- Test: `internal/access/access_test.go`

**Step 1: Write the failing test**

```go
// internal/access/access_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
    "context"
    "testing"

    "github.com/holomush/holomush/internal/access"
    "github.com/stretchr/testify/assert"
)

func TestAccessControl_Interface(t *testing.T) {
    // Verify interface can be implemented
    var _ access.AccessControl = (*mockAccessControl)(nil)
}

type mockAccessControl struct{}

func (m *mockAccessControl) Check(_ context.Context, _, _, _ string) bool {
    return true
}

func TestParseSubject(t *testing.T) {
    tests := []struct {
        name           string
        subject        string
        expectedPrefix string
        expectedID     string
    }{
        {"character", "char:01ABC", "char", "01ABC"},
        {"session", "session:01XYZ", "session", "01XYZ"},
        {"plugin", "plugin:echo-bot", "plugin", "echo-bot"},
        {"system", "system", "system", ""},
        {"no prefix", "invalid", "", "invalid"},
        {"empty", "", "", ""},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            prefix, id := access.ParseSubject(tt.subject)
            assert.Equal(t, tt.expectedPrefix, prefix)
            assert.Equal(t, tt.expectedID, id)
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/access/... -v`
Expected: FAIL with "no Go files in /internal/access"

**Step 3: Write minimal implementation**

```go
// internal/access/access.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package access provides authorization for HoloMUSH.
//
// All parameters use prefixed string format:
//   - subject: "char:01ABC", "session:01XYZ", "plugin:echo-bot", "system"
//   - action: "read", "write", "emit", "execute", "grant"
//   - resource: "location:01ABC", "character:*", "stream:location:*"
package access

import (
    "context"
    "strings"
)

// AccessControl checks permissions for all subjects in HoloMUSH.
// This is the single entry point for all authorization.
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/access/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/access/
git commit -m "feat(access): add AccessControl interface and ParseSubject

Define core AccessControl interface with Check method.
Add ParseSubject helper for prefix:id format parsing.

Part of Epic 3: Core Access Control.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 2: Create LocationResolver Interface

**Files:**

- Create: `internal/access/resolver.go`
- Test: `internal/access/resolver_test.go`

**Step 1: Write the failing test**

```go
// internal/access/resolver_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
    "context"
    "testing"

    "github.com/holomush/holomush/internal/access"
)

func TestLocationResolver_Interface(t *testing.T) {
    // Verify interface can be implemented
    var _ access.LocationResolver = (*mockLocationResolver)(nil)
}

type mockLocationResolver struct {
    locations  map[string]string
    characters map[string][]string
}

func (m *mockLocationResolver) CurrentLocation(_ context.Context, charID string) (string, error) {
    if loc, ok := m.locations[charID]; ok {
        return loc, nil
    }
    return "", nil
}

func (m *mockLocationResolver) CharactersAt(_ context.Context, locationID string) ([]string, error) {
    return m.characters[locationID], nil
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/access/... -v`
Expected: FAIL with "access.LocationResolver undefined"

**Step 3: Write minimal implementation**

```go
// internal/access/resolver.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import "context"

// LocationResolver provides location information for dynamic token resolution.
// This interface allows AccessControl to resolve $here tokens without
// depending directly on the world model.
type LocationResolver interface {
    // CurrentLocation returns the location ID for a character.
    // Returns empty string if character has no location.
    CurrentLocation(ctx context.Context, charID string) (string, error)

    // CharactersAt returns character IDs present at a location.
    // Returns empty slice if location is empty or doesn't exist.
    CharactersAt(ctx context.Context, locationID string) ([]string, error)
}

// NullResolver is a LocationResolver that returns no locations.
// Use when location-based permissions are not needed.
type NullResolver struct{}

// CurrentLocation always returns empty string.
func (NullResolver) CurrentLocation(_ context.Context, _ string) (string, error) {
    return "", nil
}

// CharactersAt always returns empty slice.
func (NullResolver) CharactersAt(_ context.Context, _ string) ([]string, error) {
    return nil, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/access/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/access/resolver.go internal/access/resolver_test.go
git commit -m "feat(access): add LocationResolver interface

Interface for resolving character locations for \$here token.
Includes NullResolver for when location permissions not needed.

Part of Epic 3: Core Access Control.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 3: Define Permission Groups

**Files:**

- Create: `internal/access/permissions.go`
- Test: `internal/access/permissions_test.go`

**Step 1: Write the failing test**

```go
// internal/access/permissions_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
    "testing"

    "github.com/holomush/holomush/internal/access"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestDefaultRoles(t *testing.T) {
    roles := access.DefaultRoles()

    require.Contains(t, roles, "player")
    require.Contains(t, roles, "builder")
    require.Contains(t, roles, "admin")

    // Player has basic permissions
    assert.Contains(t, roles["player"], "read:character:$self")
    assert.Contains(t, roles["player"], "emit:stream:location:$here")

    // Builder has world modification
    assert.Contains(t, roles["builder"], "write:location:*")
    assert.Contains(t, roles["builder"], "execute:command:dig")

    // Admin has full access
    assert.Contains(t, roles["admin"], "read:**")
    assert.Contains(t, roles["admin"], "grant:**")
}

func TestRoleComposition(t *testing.T) {
    roles := access.DefaultRoles()

    // Builder includes player permissions
    for _, perm := range []string{"read:character:$self", "emit:stream:location:$here"} {
        assert.Contains(t, roles["builder"], perm, "builder should include player permission: %s", perm)
    }

    // Admin includes all permissions
    for _, perm := range roles["builder"] {
        assert.Contains(t, roles["admin"], perm, "admin should include builder permission: %s", perm)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/access/... -v -run TestDefaultRoles`
Expected: FAIL with "access.DefaultRoles undefined"

**Step 3: Write minimal implementation**

```go
// internal/access/permissions.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

// Permission groups define reusable sets of permissions.
// Roles compose these groups rather than inheriting.

var playerPowers = []string{
    // Self access
    "read:character:$self",
    "write:character:$self",

    // Current location access
    "read:location:$here",
    "read:character:$here:*",
    "read:object:$here:*",
    "emit:stream:location:$here",

    // Basic commands
    "execute:command:say",
    "execute:command:pose",
    "execute:command:look",
    "execute:command:move",
}

var builderPowers = []string{
    // World modification
    "write:location:*",
    "write:object:*",
    "delete:object:*",

    // Builder commands
    "execute:command:dig",
    "execute:command:create",
    "execute:command:set",
    "execute:command:link",
}

var adminPowers = []string{
    // Full access
    "read:**",
    "write:**",
    "delete:**",
    "emit:**",
    "execute:**",
    "grant:**",
}

// DefaultRoles returns the default role definitions.
// Roles compose permission groups explicitly (no inheritance).
func DefaultRoles() map[string][]string {
    return map[string][]string{
        "player":  playerPowers,
        "builder": compose(playerPowers, builderPowers),
        "admin":   compose(playerPowers, builderPowers, adminPowers),
    }
}

// compose merges multiple permission slices into one.
func compose(groups ...[]string) []string {
    total := 0
    for _, g := range groups {
        total += len(g)
    }
    result := make([]string, 0, total)
    for _, g := range groups {
        result = append(result, g...)
    }
    return result
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/access/... -v -run TestDefaultRoles`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/access/permissions.go internal/access/permissions_test.go
git commit -m "feat(access): add default role and permission definitions

Define player, builder, admin roles with explicit composition.
Player: self and current location access.
Builder: world modification + player permissions.
Admin: full access to all resources.

Part of Epic 3: Core Access Control.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 4: Implement StaticAccessControl Core

**Files:**

- Create: `internal/access/static.go`
- Test: `internal/access/static_test.go`

**Step 1: Write the failing test for system subject**

```go
// internal/access/static_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
    "context"
    "testing"

    "github.com/holomush/holomush/internal/access"
    "github.com/stretchr/testify/assert"
)

func TestStaticAccessControl_SystemAlwaysAllowed(t *testing.T) {
    ac := access.NewStaticAccessControl(nil, nil)

    ctx := context.Background()

    // System can do anything
    assert.True(t, ac.Check(ctx, "system", "read", "anything"))
    assert.True(t, ac.Check(ctx, "system", "write", "location:*"))
    assert.True(t, ac.Check(ctx, "system", "grant", "character:admin"))
}

func TestStaticAccessControl_UnknownSubjectDenied(t *testing.T) {
    ac := access.NewStaticAccessControl(nil, nil)

    ctx := context.Background()

    // Unknown subjects are denied by default
    assert.False(t, ac.Check(ctx, "char:unknown", "read", "anything"))
    assert.False(t, ac.Check(ctx, "session:unknown", "read", "anything"))
    assert.False(t, ac.Check(ctx, "", "read", "anything"))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/access/... -v -run TestStaticAccessControl`
Expected: FAIL with "access.NewStaticAccessControl undefined"

**Step 3: Write minimal implementation**

```go
// internal/access/static.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import (
    "context"
    "sync"

    "github.com/gobwas/glob"
    "github.com/holomush/holomush/internal/plugin/capability"
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
    capability := action + "." + resource
    return s.enforcer.Check(pluginID, capability)
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

    // Match against role permissions
    for _, perm := range permissions {
        if perm.glob.Match(requested) {
            return true
        }
    }

    return false
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/access/... -v -run TestStaticAccessControl`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/access/static.go internal/access/static_test.go
git commit -m "feat(access): add StaticAccessControl implementation

Implements AccessControl with static role-based checks.
System subject always allowed.
Unknown subjects denied by default.
Plugin subjects delegate to capability.Enforcer.

Part of Epic 3: Core Access Control.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 5: Add Role Assignment Methods

**Files:**

- Modify: `internal/access/static.go`
- Test: `internal/access/static_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/access/static_test.go

func TestStaticAccessControl_RoleAssignment(t *testing.T) {
    ac := access.NewStaticAccessControl(nil, nil)
    ctx := context.Background()

    // Initially no role
    assert.Equal(t, "", ac.GetRole("char:01ABC"))
    assert.False(t, ac.Check(ctx, "char:01ABC", "read", "character:01ABC"))

    // Assign player role
    err := ac.AssignRole("char:01ABC", "player")
    assert.NoError(t, err)
    assert.Equal(t, "player", ac.GetRole("char:01ABC"))

    // Revoke role
    err = ac.RevokeRole("char:01ABC")
    assert.NoError(t, err)
    assert.Equal(t, "", ac.GetRole("char:01ABC"))
}

func TestStaticAccessControl_AssignRoleValidation(t *testing.T) {
    ac := access.NewStaticAccessControl(nil, nil)

    // Empty subject fails
    err := ac.AssignRole("", "player")
    assert.Error(t, err)

    // Empty role fails
    err = ac.AssignRole("char:01ABC", "")
    assert.Error(t, err)

    // Unknown role fails
    err = ac.AssignRole("char:01ABC", "superadmin")
    assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/access/... -v -run TestStaticAccessControl_Role`
Expected: FAIL with "ac.AssignRole undefined"

**Step 3: Write minimal implementation**

```go
// Add to internal/access/static.go

import "github.com/samber/oops"

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

// GetRole returns the current role for a subject.
// Returns empty string if subject has no role assigned.
func (s *StaticAccessControl) GetRole(subject string) string {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.subjects[subject]
}

// RevokeRole removes role assignment (subject becomes unauthorized).
// Returns error if subject is empty. Safe to call for subjects without roles.
func (s *StaticAccessControl) RevokeRole(subject string) error {
    if subject == "" {
        return oops.In("access").Code("INVALID_SUBJECT").New("subject cannot be empty")
    }

    s.mu.Lock()
    delete(s.subjects, subject)
    s.mu.Unlock()

    return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/access/... -v -run TestStaticAccessControl_Role`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/access/static.go internal/access/static_test.go
git commit -m "feat(access): add role assignment methods

AssignRole, GetRole, RevokeRole for managing subject roles.
Validates subject/role are non-empty and role exists.
Uses oops for structured error handling.

Part of Epic 3: Core Access Control.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 6: Implement \$self Token Resolution

**Files:**

- Modify: `internal/access/static.go`
- Test: `internal/access/static_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/access/static_test.go

func TestStaticAccessControl_SelfToken(t *testing.T) {
    ac := access.NewStaticAccessControl(nil, nil)
    ctx := context.Background()

    // Assign player role (has read:character:$self)
    err := ac.AssignRole("char:01ABC", "player")
    require.NoError(t, err)

    // Can read own character ($self resolves to 01ABC)
    assert.True(t, ac.Check(ctx, "char:01ABC", "read", "character:01ABC"))

    // Cannot read other character
    assert.False(t, ac.Check(ctx, "char:01ABC", "read", "character:01XYZ"))

    // Can write own character
    assert.True(t, ac.Check(ctx, "char:01ABC", "write", "character:01ABC"))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/access/... -v -run TestStaticAccessControl_SelfToken`
Expected: FAIL (returns false for self access because $self not resolved)

**Step 3: Modify implementation**

```go
// Modify checkRole in internal/access/static.go

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

    // Resolve $self token in permissions
    _, subjectID := ParseSubject(subject)

    for _, perm := range permissions {
        // Resolve $self in the permission pattern
        resolvedPattern := s.resolveSelfToken(perm.pattern, subjectID)

        // Recompile if pattern changed
        if resolvedPattern != perm.pattern {
            g, err := glob.Compile(resolvedPattern, ':')
            if err != nil {
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

// resolveSelfToken replaces $self with the subject's ID.
func (s *StaticAccessControl) resolveSelfToken(pattern, subjectID string) string {
    return strings.ReplaceAll(pattern, "$self", subjectID)
}
```

Also add `"strings"` to imports.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/access/... -v -run TestStaticAccessControl_SelfToken`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/access/static.go internal/access/static_test.go
git commit -m "feat(access): implement \$self token resolution

Resolves \$self to subject's character ID at check time.
Enables player-powers permissions like read:character:\$self.

Part of Epic 3: Core Access Control.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 7: Implement \$here Token Resolution

**Files:**

- Modify: `internal/access/static.go`
- Test: `internal/access/static_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/access/static_test.go

func TestStaticAccessControl_HereToken(t *testing.T) {
    resolver := &testLocationResolver{
        locations: map[string]string{
            "01ABC": "location:room1",
        },
    }
    ac := access.NewStaticAccessControl(resolver, nil)
    ctx := context.Background()

    // Assign player role (has read:location:$here)
    err := ac.AssignRole("char:01ABC", "player")
    require.NoError(t, err)

    // Can read current location
    assert.True(t, ac.Check(ctx, "char:01ABC", "read", "location:room1"))

    // Cannot read other location
    assert.False(t, ac.Check(ctx, "char:01ABC", "read", "location:room2"))

    // Can emit to current location stream
    assert.True(t, ac.Check(ctx, "char:01ABC", "emit", "stream:location:room1"))
}

type testLocationResolver struct {
    locations  map[string]string   // charID → locationID
    characters map[string][]string // locationID → charIDs
}

func (r *testLocationResolver) CurrentLocation(_ context.Context, charID string) (string, error) {
    if loc, ok := r.locations[charID]; ok {
        return loc, nil
    }
    return "", nil
}

func (r *testLocationResolver) CharactersAt(_ context.Context, locationID string) ([]string, error) {
    return r.characters[locationID], nil
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/access/... -v -run TestStaticAccessControl_HereToken`
Expected: FAIL (returns false because $here not resolved)

**Step 3: Modify implementation**

```go
// Modify checkRole in internal/access/static.go

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

    // Resolve tokens
    _, subjectID := ParseSubject(subject)
    currentLocation := s.resolveCurrentLocation(ctx, subjectID)

    for _, perm := range permissions {
        // Resolve tokens in the permission pattern
        resolvedPattern := s.resolveTokens(perm.pattern, subjectID, currentLocation)

        // Recompile if pattern changed
        if resolvedPattern != perm.pattern {
            g, err := glob.Compile(resolvedPattern, ':')
            if err != nil {
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
    result := strings.ReplaceAll(pattern, "$self", subjectID)
    if locationID != "" {
        result = strings.ReplaceAll(result, "$here", locationID)
    }
    return result
}

// resolveCurrentLocation gets the character's current location.
func (s *StaticAccessControl) resolveCurrentLocation(ctx context.Context, charID string) string {
    if s.resolver == nil {
        return ""
    }
    loc, err := s.resolver.CurrentLocation(ctx, charID)
    if err != nil {
        return ""
    }
    // Strip "location:" prefix if present for matching
    return strings.TrimPrefix(loc, "location:")
}

// Remove the old resolveSelfToken function.
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/access/... -v -run TestStaticAccessControl_HereToken`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/access/static.go internal/access/static_test.go
git commit -m "feat(access): implement \$here token resolution

Resolves \$here to character's current location at check time.
Uses LocationResolver interface to query location.
Enables location-scoped permissions like read:location:\$here.

Part of Epic 3: Core Access Control.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 8: Add Plugin Delegation Tests

**Files:**

- Modify: `internal/access/static_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/access/static_test.go

func TestStaticAccessControl_PluginDelegation(t *testing.T) {
    enforcer := capability.NewEnforcer()
    err := enforcer.SetGrants("echo-bot", []string{"emit.stream.location.*"})
    require.NoError(t, err)

    ac := access.NewStaticAccessControl(nil, enforcer)
    ctx := context.Background()

    // Plugin with capability can emit
    assert.True(t, ac.Check(ctx, "plugin:echo-bot", "emit", "stream.location.room1"))

    // Plugin without capability cannot emit to session
    assert.False(t, ac.Check(ctx, "plugin:echo-bot", "emit", "stream.session.abc"))

    // Unknown plugin denied
    assert.False(t, ac.Check(ctx, "plugin:unknown", "emit", "stream.location.room1"))
}

func TestStaticAccessControl_PluginNilEnforcer(t *testing.T) {
    ac := access.NewStaticAccessControl(nil, nil)
    ctx := context.Background()

    // Without enforcer, all plugin checks fail
    assert.False(t, ac.Check(ctx, "plugin:echo-bot", "emit", "stream.location.room1"))
}
```

**Step 2: Run test to verify it passes**

Run: `go test ./internal/access/... -v -run TestStaticAccessControl_Plugin`
Expected: PASS (should already work from Task 4)

**Step 3: Commit**

```bash
git add internal/access/static_test.go
git commit -m "test(access): add plugin delegation tests

Verify plugin subjects delegate to capability.Enforcer.
Test nil enforcer returns false for all plugin checks.

Part of Epic 3: Core Access Control.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 9: Create Test Helpers Package

**Files:**

- Create: `internal/access/accesstest/mock.go`

**Step 1: Write the mock helpers**

```go
// internal/access/accesstest/mock.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package accesstest provides test helpers for access control.
package accesstest

import (
    "context"

    "github.com/holomush/holomush/internal/access"
)

// AllowAll is an AccessControl that allows everything.
type AllowAll struct{}

// Check always returns true.
func (AllowAll) Check(_ context.Context, _, _, _ string) bool {
    return true
}

// DenyAll is an AccessControl that denies everything.
type DenyAll struct{}

// Check always returns false.
func (DenyAll) Check(_ context.Context, _, _, _ string) bool {
    return false
}

// MapResolver is a simple LocationResolver backed by maps.
type MapResolver struct {
    Locations  map[string]string   // charID → locationID
    Characters map[string][]string // locationID → charIDs
}

// CurrentLocation returns the location for a character.
func (r *MapResolver) CurrentLocation(_ context.Context, charID string) (string, error) {
    return r.Locations[charID], nil
}

// CharactersAt returns characters at a location.
func (r *MapResolver) CharactersAt(_ context.Context, locationID string) ([]string, error) {
    return r.Characters[locationID], nil
}

// Verify interfaces are satisfied.
var (
    _ access.AccessControl    = AllowAll{}
    _ access.AccessControl    = DenyAll{}
    _ access.LocationResolver = (*MapResolver)(nil)
)
```

**Step 2: Run test to verify compilation**

Run: `go build ./internal/access/...`
Expected: SUCCESS

**Step 3: Commit**

```bash
git add internal/access/accesstest/
git commit -m "feat(access): add test helpers package

AllowAll, DenyAll for simple test scenarios.
MapResolver for testing location-based permissions.

Part of Epic 3: Core Access Control.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 10: Integration with Broadcaster

**Files:**

- Modify: `internal/core/broadcaster.go`
- Test: `internal/core/broadcaster_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/core/broadcaster_test.go

func TestBroadcaster_WithAccessControl(t *testing.T) {
    // Create access control that allows specific subject
    ac := access.NewStaticAccessControl(nil, nil)
    err := ac.AssignRole("char:allowed", "admin") // admin can read everything
    require.NoError(t, err)

    b := core.NewBroadcasterWithAccessControl(ac)

    // Subscribe two subjects
    allowedCh := b.SubscribeWithSubject("room:1", "char:allowed")
    deniedCh := b.SubscribeWithSubject("room:1", "char:denied")

    // Broadcast event
    event := core.Event{
        Stream: "room:1",
        Type:   core.EventTypeSay,
    }
    b.Broadcast(context.Background(), event)

    // Allowed subject receives event
    select {
    case e := <-allowedCh:
        assert.Equal(t, core.EventTypeSay, e.Type)
    default:
        t.Error("allowed subject should receive event")
    }

    // Denied subject does not receive event
    select {
    case <-deniedCh:
        t.Error("denied subject should not receive event")
    default:
        // Expected
    }
}
```

**Step 2: Note: This is Phase 3.4 work**

The Broadcaster integration requires modifying core types and is part of Phase 3.4 (Event System Integration). This task documents the expected behavior but implementation is deferred to holomush-ql5.6.

**Step 3: Commit test as pending**

```bash
git add internal/core/broadcaster_test.go
git commit -m "test(core): add pending broadcaster access control test

Documents expected behavior for Phase 3.4.
Test will fail until Broadcaster integration complete.

Part of Epic 3: Core Access Control.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 11: Final Verification

**Files:**

- All access package files

**Step 1: Run all tests**

```bash
task test
```

Expected: All tests pass

**Step 2: Run linter**

```bash
task lint
```

Expected: No errors

**Step 3: Check coverage**

```bash
task test:coverage
```

Expected: >80% coverage for internal/access

**Step 4: Final commit**

```bash
git add .
git commit -m "feat(access): complete Phase 3.1-3.3 implementation

AccessControl interface with StaticAccessControl implementation.
Role composition model with player, builder, admin roles.
Dynamic token resolution (\$self, \$here).
Plugin delegation to capability.Enforcer.
Test helpers in accesstest package.

Completes tasks holomush-ql5.3, holomush-ql5.4, holomush-ql5.5.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Summary

| Task | Description                | Files                 |
| ---- | -------------------------- | --------------------- |
| 1    | AccessControl interface    | `access.go`           |
| 2    | LocationResolver interface | `resolver.go`         |
| 3    | Permission groups          | `permissions.go`      |
| 4    | StaticAccessControl core   | `static.go`           |
| 5    | Role assignment            | `static.go`           |
| 6    | \$self resolution          | `static.go`           |
| 7    | \$here resolution          | `static.go`           |
| 8    | Plugin delegation tests    | `static_test.go`      |
| 9    | Test helpers               | `accesstest/mock.go`  |
| 10   | Broadcaster integration    | Deferred to Phase 3.4 |
| 11   | Final verification         | All files             |

---

## Acceptance Criteria

- [ ] AccessControl interface defined with `Check(ctx, subject, action, resource) bool`
- [ ] StaticAccessControl implements role-based checks
- [ ] Dynamic tokens (`$self`, `$here`) resolve at check time
- [ ] Plugin subjects delegate to capability.Enforcer
- [ ] Role assignment methods (AssignRole, GetRole, RevokeRole)
- [ ] Test helpers in accesstest package
- [ ] All tests pass with >80% coverage
- [ ] Linting passes
