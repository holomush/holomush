// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
)

// TestHumanCallerCarriesItsSubjectVerbatim asserts that HumanCaller wraps the
// already-built subject string without re-deriving or re-prefixing it. Byte
// identity matters beyond authorization: this same string becomes the
// world-change outbox envelope Actor via buildIntent / buildMoveIntent.
func TestHumanCallerCarriesItsSubjectVerbatim(t *testing.T) {
	c := HumanCaller("character:01ABC")
	assert.Equal(t, "character:01ABC", c.subject)
	assert.False(t, c.system, "HumanCaller must never set the system flag")

	// "system:bootstrap" is a normally policy-evaluated subject. engine.go:92
	// tests exact string equality against "system", not a prefix, so this must
	// pass through verbatim and must NOT be system-kind.
	boot := HumanCaller("system:bootstrap")
	assert.Equal(t, "system:bootstrap", boot.subject)
	assert.False(t, boot.system, "system:bootstrap must not be system-kind")
}

// TestHumanCallerAcceptsAnEmptySubjectWithoutPanicking pins the deliberate
// deviation from the access.*Subject panic-on-empty convention. The fail-closed
// guard lives one layer down in types.NewAccessRequest; panicking here would
// break the nine empty-subject subtests of the malformed-access-params table at
// internal/world/service_test.go:6079.
func TestHumanCallerAcceptsAnEmptySubjectWithoutPanicking(t *testing.T) {
	var c Caller
	require.NotPanics(t, func() { c = HumanCaller("") })
	assert.Empty(t, c.subject)
	assert.False(t, c.system)
}

// TestSystemCallerYieldsTheBareSystemSubject asserts byte equality against the
// same literal the ABAC S1 gate compares (internal/access/policy/engine.go:92),
// so a future rename of that literal is caught here.
func TestSystemCallerYieldsTheBareSystemSubject(t *testing.T) {
	c := SystemCaller()
	assert.Equal(t, "system", c.subject)
	assert.True(t, c.system, "SystemCaller must derive its own system flag")
}

// TestHumanCallerCarriesNoAttributes asserts neither constructor populates the
// attribute channel in 02.1, so types.NewAccessRequest normalizes it to a nil
// Attributes field — byte-identical to the hardcoded nil it replaces.
func TestHumanCallerCarriesNoAttributes(t *testing.T) {
	assert.Empty(t, HumanCaller("character:01ABC").attrs)
	assert.Empty(t, SystemCaller().attrs)
}

// TestSystemCallerStampsTheSystemMarkerOnlyOnTheDerivedContext asserts the
// caller value can influence the context the S1 gate reads, WITHOUT mutating
// the caller-supplied context. The marker must not be able to outlive the
// evaluation and reach repositories or the outbox.
func TestSystemCallerStampsTheSystemMarkerOnlyOnTheDerivedContext(t *testing.T) {
	t.Run("system caller derives a marked context", func(t *testing.T) {
		inputCtx := context.Background()
		evalCtx := SystemCaller().evalContext(inputCtx)

		assert.True(t, access.IsSystemContext(evalCtx),
			"derived context must carry the system marker")
		assert.False(t, access.IsSystemContext(inputCtx),
			"input context must be unchanged")
	})

	t.Run("human caller derives an unmarked context", func(t *testing.T) {
		inputCtx := context.Background()
		evalCtx := HumanCaller("character:01ABC").evalContext(inputCtx)

		assert.False(t, access.IsSystemContext(evalCtx),
			"a human caller must never stamp the system marker")
		assert.False(t, access.IsSystemContext(inputCtx),
			"input context must be unchanged")
	})
}
