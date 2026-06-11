// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// Verifies: INV-PLUGIN-43
func TestResolveLoadOrderFailsFastOnUnsatisfiedRequired(t *testing.T) {
	m := &Manager{registry: NewServiceRegistry(), capVocab: NewCapabilityVocabulary()}
	discovered := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Kind: DependencyService, Name: "holomush.absent.v1.X"}}}},
	}
	_, err := m.resolveLoadOrder(discovered)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_DEPENDENCY_UNSATISFIED")
}

// Verifies: INV-PLUGIN-43
func TestResolveLoadOrderFailsFastOnDependencyCycle(t *testing.T) {
	m := &Manager{registry: NewServiceRegistry(), capVocab: NewCapabilityVocabulary()}
	// Mutual service cycle: a requires svc-b (provided by b); b requires svc-a
	// (provided by a). Kahn's algorithm cannot order this — fail closed.
	discovered := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "a", Requires: []Dependency{{Kind: DependencyService, Name: "svc-b"}}, Provides: []string{"svc-a"}}},
		{Manifest: &Manifest{Name: "b", Requires: []Dependency{{Kind: DependencyService, Name: "svc-a"}}, Provides: []string{"svc-b"}}},
	}
	_, err := m.resolveLoadOrder(discovered)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_DEPENDENCY_UNSATISFIED")
}

func TestResolveLoadOrderSucceedsWhenAllSatisfied(t *testing.T) {
	m := &Manager{registry: NewServiceRegistry(), capVocab: DefaultCapabilityVocabulary()}
	discovered := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Kind: DependencyCapability, Name: "session"}}}},
	}
	ordered, err := m.resolveLoadOrder(discovered)
	require.NoError(t, err)
	assert.Len(t, ordered, 1)
}
