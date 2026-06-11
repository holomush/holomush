// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

// CapabilityVocabulary is the controlled set of valid host-capability names a
// manifest may reference via `requires: [{capability: <name>}]` (spec §1). The
// FULL taxonomy is defined in sub-spec 2; the foundation registers only the
// minimum the four reclassified manifests require.
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

// DefaultCapabilityVocabulary returns the foundation's minimal vocabulary —
// only the names the four reclassified manifests (spec §4) depend on. Sub-spec
// 2 replaces this with the full taxonomy bound to capability-scoped contracts.
func DefaultCapabilityVocabulary() *CapabilityVocabulary {
	v := NewCapabilityVocabulary()
	for _, name := range []string{"session", "property", "world.query"} {
		v.Register(name)
	}
	return v
}
