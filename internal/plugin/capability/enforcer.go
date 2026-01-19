// Package capability provides runtime capability enforcement for plugins.
package capability

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Enforcer checks plugin capabilities at runtime.
//
// Enforcer is safe for concurrent use. The zero value is ready to use
// without calling NewEnforcer.
type Enforcer struct {
	grants map[string][]string // plugin name -> granted capabilities
	mu     sync.RWMutex
}

// NewEnforcer creates a capability enforcer.
func NewEnforcer() *Enforcer {
	return &Enforcer{
		grants: make(map[string][]string),
	}
}

// SetGrants configures capabilities for a plugin. Returns an error if the
// plugin name is empty or any capability pattern is invalid.
//
// The capabilities slice is copied, so callers may safely modify it after
// the call returns. Calling SetGrants again for the same plugin replaces
// all previous grants. If validation fails, no changes are made to the
// enforcer's state (atomic all-or-nothing semantics).
//
// Valid patterns:
//   - Exact: "world.read.location"
//   - Wildcard suffix: "world.read.*" (matches "world.read.location", "world.read.foo")
//   - Root wildcard: ".*" (matches any non-empty capability)
//
// Invalid patterns (will return error):
//   - Empty string
//   - Bare star: "*"
//   - Middle wildcard: "world.*.read"
//   - Double wildcard: "world.*.*"
//   - Star without dot prefix: "world*"
//   - Trailing dot: "world.read."
func (e *Enforcer) SetGrants(plugin string, capabilities []string) error {
	if plugin == "" {
		return errors.New("plugin name cannot be empty")
	}

	// Validate all patterns before acquiring lock
	for i, cap := range capabilities {
		if err := validatePattern(cap); err != nil {
			return fmt.Errorf("capability %d (%q): %w", i, cap, err)
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Initialize map if zero-value struct
	if e.grants == nil {
		e.grants = make(map[string][]string)
	}

	// Defensive copy to prevent caller from mutating stored slice
	copied := make([]string, len(capabilities))
	copy(copied, capabilities)
	e.grants[plugin] = copied
	return nil
}

// validatePattern checks if a capability pattern is well-formed.
func validatePattern(pattern string) error {
	if pattern == "" {
		return errors.New("empty capability pattern")
	}
	if pattern == "*" {
		return errors.New("bare '*' not valid; use '.*' for root wildcard")
	}
	if strings.Count(pattern, "*") > 1 {
		return errors.New("multiple wildcards not supported")
	}
	if strings.Contains(pattern, "*") && !strings.HasSuffix(pattern, ".*") {
		return errors.New("wildcards only valid at end with '.*' suffix")
	}
	// Reject trailing dots (except for wildcards like "world.*")
	if strings.HasSuffix(pattern, ".") && !strings.HasSuffix(pattern, ".*") {
		return errors.New("trailing dot not allowed; use '.*' for wildcard suffix")
	}
	return nil
}

// IsRegistered returns true if the plugin has been registered via SetGrants.
// Returns false for empty plugin names.
// This helps distinguish "plugin not registered" from "plugin lacks capability".
func (e *Enforcer) IsRegistered(plugin string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.grants == nil {
		return false
	}
	_, ok := e.grants[plugin]
	return ok
}

// RemoveGrants unregisters a plugin, removing all its capabilities.
// Safe to call for unknown plugins or on a zero-value Enforcer.
func (e *Enforcer) RemoveGrants(plugin string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.grants == nil {
		return
	}
	delete(e.grants, plugin)
}

// GetGrants returns a copy of the capabilities granted to a plugin.
// Returns nil if the plugin is not registered.
// The returned slice is a defensive copy; modifying it does not affect
// the enforcer's state.
func (e *Enforcer) GetGrants(plugin string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.grants == nil {
		return nil
	}
	grants, ok := e.grants[plugin]
	if !ok {
		return nil
	}
	// Return defensive copy
	copied := make([]string, len(grants))
	copy(copied, grants)
	return copied
}

// Check returns true if the plugin has the requested capability.
// Returns false for empty capability strings.
//
// Supports wildcard grants: "world.read.*" matches "world.read.location".
// The root wildcard ".*" matches any non-empty capability.
func (e *Enforcer) Check(plugin, capability string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Handle zero-value struct
	if e.grants == nil {
		return false
	}

	grants, ok := e.grants[plugin]
	if !ok {
		return false
	}

	for _, grant := range grants {
		if matchCapability(grant, capability) {
			return true
		}
	}
	return false
}

// matchCapability handles wildcard matching.
// "world.read.*" matches "world.read.location" and "world.read.character.name".
// ".*" is the root wildcard and matches any non-empty capability.
func matchCapability(grant, requested string) bool {
	if grant == requested {
		return true
	}
	if strings.HasSuffix(grant, ".*") {
		if grant == ".*" {
			return requested != ""
		}
		prefix := strings.TrimSuffix(grant, "*")
		return strings.HasPrefix(requested, prefix)
	}
	return false
}
