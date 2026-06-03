// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestNoneProvider_Wrap_RefusesWithTypedError(t *testing.T) {
	// INV-CRYPTO-20: NoneProvider.Wrap MUST refuse and surface a typed error.
	provider := kek.NewNoneProviderForUnitTest() // skips DB check; tests Wrap path only
	_, _, err := provider.Wrap(context.Background(), make([]byte, 32))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CRYPTO_NONE_PROVIDER_WRAP_REFUSED")
}

func TestNoneProvider_Unwrap_RefusesWithTypedError(t *testing.T) {
	provider := kek.NewNoneProviderForUnitTest()
	_, err := provider.Unwrap(context.Background(), []byte("anything"), "any-key-id")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CRYPTO_NONE_PROVIDER_UNWRAP_REFUSED")
}

func TestNoneProvider_HealthCheck_Succeeds(t *testing.T) {
	// HealthCheck has no preconditions; NoneProvider is "healthy" in
	// the sense that it functions as designed (refuses crypto ops).
	provider := kek.NewNoneProviderForUnitTest()
	assert.NoError(t, provider.HealthCheck(context.Background()))
}

func TestNoneProvider_Name_IsNone(t *testing.T) {
	provider := kek.NewNoneProviderForUnitTest()
	assert.Equal(t, "none", provider.Name())
}
