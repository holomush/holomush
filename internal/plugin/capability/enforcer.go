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
	e.grants[plugin] = capabilities
}

// Check returns true if the plugin has the requested capability.
func (e *Enforcer) Check(plugin, capability string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

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
func matchCapability(grant, requested string) bool {
	if grant == requested {
		return true
	}
	if strings.HasSuffix(grant, ".*") {
		prefix := strings.TrimSuffix(grant, "*")
		return strings.HasPrefix(requested, prefix)
	}
	return false
}
