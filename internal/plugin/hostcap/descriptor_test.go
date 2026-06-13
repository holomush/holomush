// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap_test

import (
	"testing"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryServedCapabilityHasADescriptor pre-stages the INV-PLUGIN-52
// completeness gate: every served host.v1 capability token MUST have a
// non-empty Descriptors entry so the fail-closed interceptor never denies a
// legitimately-classified method as UNCLASSIFIED_CAPABILITY_METHOD.
func TestEveryServedCapabilityHasADescriptor(t *testing.T) {
	for token := range plugins.CapabilityServiceNames {
		cd, ok := hostcap.Descriptors[token]
		require.True(t, ok, "capability %q must have a descriptor entry", token)
		require.NotEmpty(t, cd.Methods, "capability %q descriptor must list methods", token)
	}
}

func TestDescriptorClassifiesEvalMethods(t *testing.T) {
	d, ok := hostcap.Descriptors["eval"]
	require.True(t, ok, "eval capability has a descriptor")
	m, ok := d.Methods["Evaluate"]
	require.True(t, ok)
	assert.Equal(t, hostcap.ClassRead, m.Class)
	assert.Empty(t, m.Scopes, "eval is not scope-eligible")
}

func TestWorldMutationIsScopeEligibleWithExtractor(t *testing.T) {
	m := hostcap.Descriptors["world.mutation"].Methods["CreateExit"]
	assert.Equal(t, hostcap.ClassWrite, m.Class)
	assert.Contains(t, m.Scopes, "own-location")
	require.NotNil(t, m.Extract, "scope-eligible method must carry an extractor")
}

func TestCreateLocationIsNotScopeEligible(t *testing.T) {
	m := hostcap.Descriptors["world.mutation"].Methods["CreateLocation"]
	assert.Empty(t, m.Scopes, "CreateLocation has no pre-existing location operand")
	assert.Nil(t, m.Extract)
}
