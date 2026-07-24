// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// TestResolveLoadOrderPolicyFatalPerErrorClass verifies that defaultResolvePolicy
// is fatal for each distinct error class surfaced in ResolveResult (Unsatisfied
// and Cycles). DUPLICATE_* classes are bare Go errors returned by
// ResolveDependencyOrder before the result is built, so they are never in
// res.Unsatisfied/Cycles and are correctly not modelled as policy cases here.
func TestResolveLoadOrderPolicyFatalPerErrorClass(t *testing.T) {
	cases := []struct {
		name string
		res  *ResolveResult
	}{
		{"unsatisfied requires", &ResolveResult{Unsatisfied: []UnsatisfiedDep{{Reason: "UNSATISFIED_CAPABILITY"}}}},
		{"cycle", &ResolveResult{Cycles: [][]string{{"a", "b", "a"}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := applyResolvePolicy(c.res, defaultResolvePolicy)
			require.Error(t, err)
			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok, "error must be an oops error to assert its code")
			assert.Equal(t, "PLUGIN_DEPENDENCY_UNSATISFIED", oopsErr.Code())
		})
	}
}

func TestResolveLoadOrderPolicyAllowsCleanResult(t *testing.T) {
	require.NoError(t, applyResolvePolicy(&ResolveResult{}, defaultResolvePolicy))
}

// Verifies: INV-PLUGIN-43
func TestResolveLoadOrderFailsFastOnUnsatisfiedRequired(t *testing.T) {
	m := &PluginLoader{registry: NewServiceRegistry(), capVocab: NewCapabilityVocabulary()}
	discovered := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Kind: DependencyService, Name: "holomush.absent.v1.X"}}}},
	}
	_, err := m.resolveLoadOrder(discovered)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_DEPENDENCY_UNSATISFIED")
}

// Verifies: INV-PLUGIN-43
func TestResolveLoadOrderFailsFastOnDependencyCycle(t *testing.T) {
	m := &PluginLoader{registry: NewServiceRegistry(), capVocab: NewCapabilityVocabulary()}
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
	m := &PluginLoader{registry: NewServiceRegistry(), capVocab: DefaultCapabilityVocabulary()}
	discovered := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Kind: DependencyCapability, Name: "session"}}}},
	}
	res, err := m.resolveLoadOrder(discovered)
	require.NoError(t, err)
	assert.Len(t, res.Ordered, 1)
}
