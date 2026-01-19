// Package capability provides runtime capability enforcement for plugins.
package capability

import (
	"strings"
	"sync"
)

// Enforcer checks plugin capabilities at runtime.
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

// SetGrants configures capabilities for a plugin.
func (e *Enforcer) SetGrants(plugin string, capabilities []string) {
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
}

// Check returns true if the plugin has the requested capability.
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
// ".*" is the root wildcard and matches everything.
func matchCapability(grant, requested string) bool {
	if grant == requested {
		return true
	}
	if strings.HasSuffix(grant, ".*") {
		// Root wildcard matches everything
		if grant == ".*" {
			return requested != ""
		}
		prefix := strings.TrimSuffix(grant, "*")
		return strings.HasPrefix(requested, prefix)
	}
	return false
}
