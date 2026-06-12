// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

// CapabilityVocabulary is the controlled set of valid host-capability names a
// manifest may reference via `requires: [{capability: <name>}]` (spec §1). It
// holds the full taxonomy (spec §1); each token backs one
// `holomush.plugin.host.v1` service.
type CapabilityVocabulary struct {
	names map[string]struct{}
}

// NewCapabilityVocabulary returns an empty vocabulary.
func NewCapabilityVocabulary() *CapabilityVocabulary {
	return &CapabilityVocabulary{names: make(map[string]struct{})}
}

// Register adds a capability name to the vocabulary.
func (v *CapabilityVocabulary) Register(name string) { v.names[name] = struct{}{} }

// Has reports whether name is a registered capability.
func (v *CapabilityVocabulary) Has(name string) bool {
	_, ok := v.names[name]
	return ok
}

// DefaultCapabilityVocabulary returns the full host-capability taxonomy
// (sub-spec 2, spec §1). Each name maps to exactly one capability-scoped service
// in holomush.plugin.host.v1. Ambient substrate (log, new_request_id, stdlib,
// config) is intentionally absent — it is not a capability (spec §4).
func DefaultCapabilityVocabulary() *CapabilityVocabulary {
	v := NewCapabilityVocabulary()
	for _, name := range []string{
		"world.query", "world.mutation", "property", "session", "session.admin",
		"focus", "eval", "emit", "settings", "kv",
		"stream.history", "stream.subscription", "audit", "command-registry",
	} {
		v.Register(name)
	}
	return v
}
