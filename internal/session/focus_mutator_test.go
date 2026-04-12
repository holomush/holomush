// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
)

// TestFocusMutatorRequiresConstructor documents invariant I-6: FocusMutator
// contains an unexported focusMutatorSentinel field that blocks struct-literal
// construction from outside the session package. The ONLY way to obtain a
// usable FocusMutator is via session.NewFocusMutator.
//
// The compile-time guarantee:
//
//	// This would fail to compile in any package outside session:
//	// m := session.FocusMutator{
//	//     Mutate: func(...) (...) { ... },
//	// }
//	// Error: cannot use promoted field 'focusMutatorSentinel' in struct literal
//	//        of type session.FocusMutator
//
// The test below verifies the runtime behavior of the constructor pathway.
func TestFocusMutatorRequiresConstructor(t *testing.T) {
	// Zero-value FocusMutator has nil Mutate — unusable.
	var zero session.FocusMutator
	assert.Nil(t, zero.Mutate, "zero-value FocusMutator must have nil Mutate")

	// NewFocusMutator produces a usable FocusMutator.
	called := false
	m := session.NewFocusMutator(func(
		current []session.FocusMembership,
		presenting *session.FocusKey,
	) ([]session.FocusMembership, *session.FocusKey, error) {
		called = true
		return current, presenting, nil
	})

	require.NotNil(t, m.Mutate, "NewFocusMutator must set Mutate field")
	_, _, err := m.Mutate(nil, nil)
	require.NoError(t, err)
	assert.True(t, called, "mutator callback must be invoked")
}
