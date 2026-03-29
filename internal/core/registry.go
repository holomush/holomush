// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"sync"

	"github.com/samber/oops"

	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// VerbRegistration holds rendering metadata for an event type.
type VerbRegistration struct {
	Type          string
	Category      string // "communication", "movement", "state", "system", "command"
	Format        string // "speech", "action", "narrative", "notification", "error", "snapshot", "delta"
	Label         string // "says", "telepathically sends" -- required when Format is "speech"
	DisplayTarget webv1.EventChannel
	MetadataKeys  []MetadataKey
}

// MetadataKey declares a well-known metadata field for an event type.
type MetadataKey struct {
	Key         string
	Description string
	ValueType   string // "string", "bool", "object", "array"
}

// VerbRegistry maps event types to their rendering metadata.
type VerbRegistry struct {
	mu    sync.RWMutex
	types map[string]VerbRegistration
}

// NewVerbRegistry creates an empty registry.
func NewVerbRegistry() *VerbRegistry {
	return &VerbRegistry{types: make(map[string]VerbRegistration)}
}

// Register adds a type. Returns error if duplicate or invalid.
func (r *VerbRegistry) Register(reg VerbRegistration) error {
	if reg.Type == "" {
		return oops.Code("INVALID_REGISTRATION").Errorf("type must not be empty")
	}
	if reg.Category == "" {
		return oops.Code("INVALID_REGISTRATION").Errorf("category must not be empty")
	}
	if reg.Format == "" {
		return oops.Code("INVALID_REGISTRATION").Errorf("format must not be empty")
	}
	if reg.Format == "speech" && reg.Label == "" {
		return oops.Code("INVALID_REGISTRATION").
			With("type", reg.Type).
			Errorf("label is required when format is speech")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.types[reg.Type]; exists {
		return oops.Code("DUPLICATE_REGISTRATION").
			With("type", reg.Type).
			Errorf("event type %q is already registered", reg.Type)
	}
	r.types[reg.Type] = reg
	return nil
}

// Lookup returns the registration for a type, or false if not found.
func (r *VerbRegistry) Lookup(eventType string) (VerbRegistration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reg, ok := r.types[eventType]
	return reg, ok
}

// All returns all registrations (for catalog/debug).
func (r *VerbRegistry) All() []VerbRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]VerbRegistration, 0, len(r.types))
	for _, reg := range r.types {
		result = append(result, reg)
	}
	return result
}
