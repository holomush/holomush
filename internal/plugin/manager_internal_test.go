// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestResolveLoadOrderFailsFastOnUnsatisfiedRequired(t *testing.T) {
	m := &Manager{registry: NewServiceRegistry(), capVocab: NewCapabilityVocabulary()}
	discovered := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Kind: DependencyService, Name: "holomush.absent.v1.X"}}}},
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
