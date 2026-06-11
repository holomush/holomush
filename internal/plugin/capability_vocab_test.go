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
