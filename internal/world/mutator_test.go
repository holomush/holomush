// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
)

// TestMutator_NewServiceReturnsMutator verifies that NewService returns a value
// that satisfies the Mutator interface and is non-nil.
func TestMutator_NewServiceReturnsMutator(t *testing.T) {
	engine := policytest.NewGrantEngine()
	svc := NewService(ServiceConfig{Engine: engine})
	require.NotNil(t, svc)

	var m Mutator = svc
	assert.NotNil(t, m)
}
