// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package errutil

import (
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AssertErrorCode asserts that err is an oops error with the given code.
func AssertErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T", err)
	assert.Equal(t, code, oopsErr.Code())
}

// AssertErrorContext asserts that err is an oops error with the given context key/value.
func AssertErrorContext(t *testing.T, err error, key string, value any) {
	t.Helper()
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T", err)
	ctx := oopsErr.Context()
	assert.Contains(t, ctx, key)
	assert.Equal(t, value, ctx[key])
}
