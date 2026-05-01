// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestNewManager_RejectsNilProvider verifies NewManager returns
// DEK_MANAGER_DEPENDENCY_NIL when the kek.Provider argument is nil,
// rather than returning a Manager that nil-panics on first GetOrCreate.
func TestNewManager_RejectsNilProvider(t *testing.T) {
	_, err := dek.NewManager(nil, &dek.Store{}, dek.NewCache(dek.CacheConfig{}))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_MANAGER_DEPENDENCY_NIL")
	errutil.AssertErrorContext(t, err, "dependency", "provider")
}

// TestNewManager_RejectsNilStore verifies the store nil-check path.
func TestNewManager_RejectsNilStore(t *testing.T) {
	_, err := dek.NewManager(kek.NewNoneProviderForUnitTest(), nil, dek.NewCache(dek.CacheConfig{}))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_MANAGER_DEPENDENCY_NIL")
	errutil.AssertErrorContext(t, err, "dependency", "store")
}

// TestNewManager_RejectsNilCache verifies the cache nil-check path.
func TestNewManager_RejectsNilCache(t *testing.T) {
	_, err := dek.NewManager(kek.NewNoneProviderForUnitTest(), &dek.Store{}, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_MANAGER_DEPENDENCY_NIL")
	errutil.AssertErrorContext(t, err, "dependency", "cache")
}

// TestManager_NotConfigured_GuardsGetOrCreate verifies that a Manager
// built via NewManagerForUnitTest returns DEK_MANAGER_NOT_CONFIGURED on
// GetOrCreate instead of dereferencing nil collaborators.
func TestManager_NotConfigured_GuardsGetOrCreate(t *testing.T) {
	m := dek.NewManagerForUnitTest()
	_, err := m.GetOrCreate(context.Background(), dek.ContextID{Type: "scene", ID: "x"}, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_MANAGER_NOT_CONFIGURED")
}

// TestManager_NotConfigured_GuardsResolve verifies the same guard for
// the decrypt path.
func TestManager_NotConfigured_GuardsResolve(t *testing.T) {
	m := dek.NewManagerForUnitTest()
	_, err := m.Resolve(context.Background(), codec.KeyID(1), 1)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_MANAGER_NOT_CONFIGURED")
}
