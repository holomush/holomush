// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"sync"

	"github.com/samber/oops"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// VerbRegistration holds rendering metadata for an event type.
type VerbRegistration struct {
	Type          string
	Category      string // "communication", "movement", "state", "system", "command"
	Format        string // "speech", "action", "narrative", "notification", "error", "snapshot", "delta"
	Label         string // "says", "telepathically sends" -- required when Format is "speech"
	DisplayTarget corev1.EventChannel
	MetadataKeys  []MetadataKey
	Source        string // "builtin" or plugin name -- tracks ownership for unload; required (publishability invariant)
}

// MetadataKey declares a well-known metadata field for an event type.
type MetadataKey struct {
	Key         string
	Description string
	ValueType   string // "string", "bool", "object", "array"
}

// SourceInfo records origin metadata for a verb registration.
type SourceInfo struct {
	Source  string // "builtin" or plugin manifest name
	Version string // host build version for builtin, manifest version for plugins
}

// VerbRegistry maps event types to their rendering metadata.
type VerbRegistry struct {
	mu      sync.RWMutex
	types   map[string]VerbRegistration
	sources map[string]string // source name -> version
}

// NewVerbRegistry creates an empty registry.
func NewVerbRegistry() *VerbRegistry {
	return &VerbRegistry{
		types:   make(map[string]VerbRegistration),
		sources: make(map[string]string),
	}
}

// Register adds a type. Returns error if duplicate or invalid.
func (r *VerbRegistry) Register(reg VerbRegistration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registerNoLock(reg)
}

// registerNoLock performs validation and insertion. Caller MUST hold r.mu.
func (r *VerbRegistry) registerNoLock(reg VerbRegistration) error {
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
	// Publishability checks: RenderingPublisher.Publish stamps these onto
	// RenderingMetadata, which protovalidate rejects when DisplayTarget is
	// EVENT_CHANNEL_UNSPECIFIED or SourcePlugin is empty. Reject at
	// registration time so callers fail fast with a useful error.
	if reg.DisplayTarget == corev1.EventChannel_EVENT_CHANNEL_UNSPECIFIED {
		return oops.Code("INVALID_REGISTRATION").
			With("type", reg.Type).
			Errorf("display target must not be EVENT_CHANNEL_UNSPECIFIED")
	}
	if reg.Source == "" {
		return oops.Code("INVALID_REGISTRATION").
			With("type", reg.Type).
			Errorf("source must not be empty")
	}
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

// Unregister removes a single verb by event type. Returns true if found.
func (r *VerbRegistry) Unregister(eventType string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.types[eventType]; !exists {
		return false
	}
	delete(r.types, eventType)
	return true
}

// UnregisterBySource removes all verbs registered by a given source.
// Returns the count of removed entries.
func (r *VerbRegistry) UnregisterBySource(source string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for key, reg := range r.types {
		if reg.Source == source {
			delete(r.types, key)
			count++
		}
	}
	return count
}

// RegisterWithSource adds a type and records the source's version atomically.
// Returns error if duplicate or invalid. Version must be non-empty so the
// resulting RenderingMetadata satisfies protovalidate at publish time.
func (r *VerbRegistry) RegisterWithSource(reg VerbRegistration, version string) error {
	if version == "" {
		return oops.Code("INVALID_REGISTRATION").
			With("type", reg.Type).
			With("source", reg.Source).
			Errorf("version must not be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.registerNoLock(reg); err != nil {
		return err
	}
	r.sources[reg.Source] = version
	return nil
}

// SourceVersion returns the version recorded for a given source name.
// Returns "" if source is unknown.
func (r *VerbRegistry) SourceVersion(source string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sources[source]
}
