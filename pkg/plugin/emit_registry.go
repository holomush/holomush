// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"sort"
	"sync"
)

// EmitRegistry accumulates the set of event types a binary plugin can
// emit. Plugins register types during construction or in Init. The host
// reads the set via InitResponse.registered_emit_types and validates
// against manifest's crypto.emits per INV-PLUGIN-32.
type EmitRegistry struct {
	mu    sync.Mutex
	types map[string]struct{}
}

// NewEmitRegistry returns a fresh, empty EmitRegistry ready for use.
func NewEmitRegistry() *EmitRegistry {
	return &EmitRegistry{types: make(map[string]struct{})}
}

// RegisterEmitType records a single event-type string. Duplicate registrations
// are idempotent.
func (r *EmitRegistry) RegisterEmitType(eventType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types[eventType] = struct{}{}
}

// RegisterEmitTypes records a batch of event-type strings. Duplicate
// registrations (within the batch or against existing state) are idempotent.
func (r *EmitRegistry) RegisterEmitTypes(eventTypes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range eventTypes {
		r.types[t] = struct{}{}
	}
}

// RegisteredEmitTypes returns the sorted set of all registered event-type
// strings. The returned slice is freshly allocated; callers may retain or
// mutate it without affecting the registry.
func (r *EmitRegistry) RegisteredEmitTypes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.types))
	for t := range r.types {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// EmitTypeRegistrar is the optional interface binary plugins implement
// to expose their EmitRegistry to the host via InitResponse.
//
// Plugins with non-empty crypto.emits MUST implement this interface;
// the substrate validator fails load on mismatch. Plugins without
// crypto.emits are out of INV-PLUGIN-32 scope (per INV-M1) and may skip.
type EmitTypeRegistrar interface {
	EmitRegistry() *EmitRegistry
}
