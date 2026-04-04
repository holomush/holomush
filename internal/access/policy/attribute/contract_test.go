// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertProviderContract runs common structural assertions for any AttributeProvider.
func assertProviderContract(t *testing.T, provider AttributeProvider) {
	t.Helper()

	t.Run("namespace is non-empty", func(t *testing.T) {
		assert.NotEmpty(t, provider.Namespace())
	})

	t.Run("schema returns non-nil definitions", func(t *testing.T) {
		schema := provider.Schema()
		assert.NotNil(t, schema)
	})

	t.Run("resolve subject returns nil for non-matching ID", func(t *testing.T) {
		attrs, err := provider.ResolveSubject(context.Background(), "unrelated:id")
		require.NoError(t, err)
		assert.Nil(t, attrs)
	})

	t.Run("resolve resource returns nil for non-matching ID", func(t *testing.T) {
		attrs, err := provider.ResolveResource(context.Background(), "unrelated:id")
		require.NoError(t, err)
		assert.Nil(t, attrs)
	})
}
