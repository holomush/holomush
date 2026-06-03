// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsEligibleVoterRole pins INV-SCENE-28: only owner and member are eligible
// publish-roster voters; invited (and any other role) is not.
func TestIsEligibleVoterRole(t *testing.T) {
	t.Parallel()
	assert.True(t, IsEligibleVoterRole("owner"))
	assert.True(t, IsEligibleVoterRole("member"))
	assert.False(t, IsEligibleVoterRole("invited"), "INV-SCENE-28: invited members are NOT on the voter roster")
	assert.False(t, IsEligibleVoterRole(""))
	assert.False(t, IsEligibleVoterRole("admin"))
}
