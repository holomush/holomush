// Package capability provides runtime capability enforcement for plugins.
//
// Pattern matching uses gobwas/glob with '.' as the segment separator:
//   - '*' matches a single segment (does not cross '.')
//   - '**' matches zero or more segments (crosses '.')
//
// Examples:
//   - "world.read.*" matches "world.read.location" but NOT "world.read.character.name"
//   - "world.read.**" matches both "world.read.location" AND "world.read.character.name"
//   - "**" matches any capability
package capability

import (
	"errors"
	"fmt"
	"sync"

	"github.com/gobwas/glob"
)

// compiledGrant holds a pattern and its compiled glob for efficient matching.
type compiledGrant struct {
	pattern string
	glob    glob.Glob
}

// Enforcer checks plugin capabilities at runtime.
//
// Enforcer is safe for concurrent use. The zero value is ready to use
// without calling NewEnforcer.
type Enforcer struct {
	grants map[string][]compiledGrant // plugin name -> compiled grants
	mu     sync.RWMutex
}

// NewEnforcer creates a capability enforcer.
func NewEnforcer() *Enforcer {
	return &Enforcer{
		grants: make(map[string][]compiledGrant),
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
// Pattern matching uses gobwas/glob with '.' as the segment separator:
//   - '*' matches a single segment (does not cross '.')
//   - '**' matches zero or more segments (crosses '.')
//
// Examples:
//   - "world.read.location" - exact match only
//   - "world.read.*" - matches direct children: "world.read.location", "world.read.foo"
//   - "world.read.**" - matches all descendants: "world.read.location", "world.read.char.name"
//   - "*" - matches any single-segment capability
//   - "**" - matches any capability (root super-wildcard)
//
// Invalid patterns (will return error):
//   - Empty string
//   - Invalid glob syntax (e.g., unclosed brackets)
func (e *Enforcer) SetGrants(plugin string, capabilities []string) error {
	if plugin == "" {
		return errors.New("plugin name cannot be empty")
	}

	// Compile all patterns before acquiring lock (fail-fast, atomic)
	compiled := make([]compiledGrant, len(capabilities))
	for i, pattern := range capabilities {
		if pattern == "" {
			return fmt.Errorf("capability %d: empty capability pattern", i)
		}
		// Compile with '.' as separator so '*' doesn't cross segment boundaries
		g, err := glob.Compile(pattern, '.')
		if err != nil {
			return fmt.Errorf("capability %d (%q): %w", i, pattern, err)
		}
		compiled[i] = compiledGrant{pattern: pattern, glob: g}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Initialize map if zero-value struct
	if e.grants == nil {
		e.grants = make(map[string][]compiledGrant)
	}

	e.grants[plugin] = compiled
	return nil
}

// IsRegistered returns true if the plugin has been registered via SetGrants.
// Returns false for empty plugin names (which cannot be registered via SetGrants).
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
	// Return defensive copy of pattern strings
	patterns := make([]string, len(grants))
	for i, g := range grants {
		patterns[i] = g.pattern
	}
	return patterns
}

// ListPlugins returns a list of all registered plugin names.
// Returns an empty slice (not nil) if no plugins are registered.
// The returned slice is a defensive copy; modifying it does not affect
// the enforcer's state. Order is not guaranteed.
func (e *Enforcer) ListPlugins() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.grants) == 0 {
		return []string{}
	}

	plugins := make([]string, 0, len(e.grants))
	for name := range e.grants {
		plugins = append(plugins, name)
	}
	return plugins
}

// Check returns true if the plugin has the requested capability.
//
// Returns false in these cases (no error, deny by default):
//   - Empty plugin name
//   - Empty capability string
//   - Unknown plugin (not registered via SetGrants)
//   - Plugin lacks the requested capability
//
// Pattern matching uses gobwas/glob with '.' as the segment separator:
//   - '*' matches a single segment: "world.read.*" matches "world.read.location"
//   - '**' matches multiple segments: "world.read.**" matches "world.read.char.name"
func (e *Enforcer) Check(plugin, capability string) bool {
	if capability == "" {
		return false
	}

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
		if grant.glob.Match(capability) {
			return true
		}
	}
	return false
}
