// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap_test

import (
	"testing"

	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
