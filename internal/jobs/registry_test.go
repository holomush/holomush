// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package jobs_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/jobs"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestRegistryDeclaredWritesReportsAnUnregisteredJobAsAbsent pins the liveness
// answer the ABAC provider depends on: an unregistered job resolves to nothing,
// NOT to an empty declaration. The distinction is load-bearing — an empty slice
// is a resolved value a containsAll condition would evaluate against.
func TestRegistryDeclaredWritesReportsAnUnregisteredJobAsAbsent(t *testing.T) {
	r := jobs.NewRegistry()

	writes, ok := r.DeclaredWrites("never-registered")
	assert.False(t, ok, "an unregistered job MUST report absent, not an empty declaration")
	assert.Nil(t, writes)
	assert.False(t, r.IsJobRunning("never-registered"))
}

// TestRegistryDeclaredWritesReturnsACopy is the tamper fence. The returned
// slice is stamped straight into an ABAC attribute bag, so a caller mutating it
// MUST NOT be able to change what a later evaluation sees.
func TestRegistryDeclaredWritesReturnsACopy(t *testing.T) {
	r := jobs.NewRegistry()
	require.NoError(t, r.Register("fixture", []string{"character"}))

	got, ok := r.DeclaredWrites("fixture")
	require.True(t, ok)
	got[0] = "location"

	again, ok := r.DeclaredWrites("fixture")
	require.True(t, ok)
	assert.Equal(t, []string{"character"}, again,
		"mutating the returned slice MUST NOT widen what the registry reports")
}

// TestRegisterCopiesTheCallersSliceDefensively is the same fence on the way in:
// a caller reusing its own backing array after registration must not be able to
// rewrite the declaration.
func TestRegisterCopiesTheCallersSliceDefensively(t *testing.T) {
	r := jobs.NewRegistry()

	declared := []string{"character"}
	require.NoError(t, r.Register("fixture", declared))
	declared[0] = "location"

	got, ok := r.DeclaredWrites("fixture")
	require.True(t, ok)
	assert.Equal(t, []string{"character"}, got)
}

// TestRegisterRejectsAnIncompleteDeclaration pins both required arguments. An
// empty name keys a job no subject string can name; an empty writes list
// declares a job that may write nothing. Either is a call-site bug, so both are
// rejected loudly rather than accepted as a silently narrower grant.
func TestRegisterRejectsAnIncompleteDeclaration(t *testing.T) {
	r := jobs.NewRegistry()

	t.Run("empty name", func(t *testing.T) {
		err := r.Register("", []string{"character"})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "JOB_REGISTRATION_INVALID")
	})

	t.Run("empty writes", func(t *testing.T) {
		err := r.Register("fixture", nil)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "JOB_REGISTRATION_INVALID")
		assert.False(t, r.IsJobRunning("fixture"),
			"a rejected registration MUST NOT mark the job running")
	})
}

// TestUnregisterEndsLivenessAndIsIdempotent covers the other half of D-49: once
// a job stops, its authority stops with it.
func TestUnregisterEndsLivenessAndIsIdempotent(t *testing.T) {
	r := jobs.NewRegistry()
	require.NoError(t, r.Register("fixture", []string{"character"}))
	require.True(t, r.IsJobRunning("fixture"))

	r.Unregister("fixture")
	assert.False(t, r.IsJobRunning("fixture"))
	_, ok := r.DeclaredWrites("fixture")
	assert.False(t, ok)

	assert.NotPanics(t, func() { r.Unregister("fixture") },
		"Unregister must be idempotent so a deferred cleanup on a failed start is safe")
}
