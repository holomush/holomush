// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCapabilityVocabularyHasRegisteredName(t *testing.T) {
	v := NewCapabilityVocabulary()
	v.Register("world.query")
	assert.True(t, v.Has("world.query"))
	assert.False(t, v.Has("not.a.capability"))
}

func TestDefaultCapabilityVocabularyCoversFoundationMinimum(t *testing.T) {
	v := DefaultCapabilityVocabulary()
	for _, name := range []string{"session", "property", "world.query"} {
		assert.True(t, v.Has(name), "default vocabulary MUST include %q", name)
	}
}

// Verifies: INV-PLUGIN-48
func TestDefaultCapabilityVocabularyIsTheFullTaxonomy(t *testing.T) {
	v := DefaultCapabilityVocabulary() // white-box: capability_vocab_test.go is package plugins
	want := []string{
		"world.query", "world.mutation", "property", "session", "session.admin",
		"focus", "eval", "emit", "settings", "kv",
		"stream.history", "stream.subscription", "audit", "command-registry",
	}
	for _, name := range want {
		assert.True(t, v.Has(name), "vocabulary must contain %q", name)
	}
	// Ambient substrate is NOT a capability (spec §4 / INV-PLUGIN-48).
	for _, ambient := range []string{"log", "new_request_id", "config"} {
		assert.False(t, v.Has(ambient), "ambient %q must NOT be a capability token", ambient)
	}
	// alias is delivered at the command layer, never a capability (spec §Background).
	assert.False(t, v.Has("alias"), "alias must NOT be a capability token")
}
