// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import lua "github.com/yuin/gopher-lua"

// Capability is a module of Lua host functions that can be injected
// into the VM based on a plugin's manifest requires declarations.
// Each capability corresponds to a proto service contract.
type Capability interface {
	// Namespace returns the Lua global table name (e.g., "session", "alias").
	Namespace() string

	// Register injects this capability's functions into the Lua state.
	Register(L *lua.LState, pluginName string)
}

// CapabilityRegistry maps proto service names to capability modules.
type CapabilityRegistry struct {
	modules map[string]Capability
}

// NewCapabilityRegistry creates an empty registry.
func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{modules: make(map[string]Capability)}
}

// Register associates a proto service name with a capability module.
func (r *CapabilityRegistry) Register(serviceName string, cap Capability) {
	r.modules[serviceName] = cap
}

// Get returns the capability for a service name, or nil.
func (r *CapabilityRegistry) Get(serviceName string) Capability {
	return r.modules[serviceName]
}

// InjectRequired registers capability modules into the Lua state
// for each service in the requires list. Unknown services are silently
// skipped — the plugin manager validates requires before loading.
func (r *CapabilityRegistry) InjectRequired(L *lua.LState, requires []string, pluginName string) {
	for _, svc := range requires {
		if cap, ok := r.modules[svc]; ok {
			cap.Register(L, pluginName)
		}
	}
}

// List returns all registered service names.
func (r *CapabilityRegistry) List() []string {
	names := make([]string, 0, len(r.modules))
	for name := range r.modules {
		names = append(names, name)
	}
	return names
}
