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

// validHexKEK is 64 hex chars decoding to 32 bytes.
const validHexKEK = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestEnvSource_Load_ReturnsKEKBytes(t *testing.T) {
	t.Setenv("HOLOMUSH_TEST_KEK", validHexKEK)
	src := kek.NewEnvSource("HOLOMUSH_TEST_KEK", false /* prodMode */)
	bytes, err := src.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, bytes, 32)
}

func TestEnvSource_Load_RefusesInProdMode(t *testing.T) {
	t.Setenv("HOLOMUSH_TEST_KEK", validHexKEK)
	src := kek.NewEnvSource("HOLOMUSH_TEST_KEK", true /* prodMode */)
	_, err := src.Load(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_ENV_SOURCE_PROD_FORBIDDEN")
}

func TestEnvSource_Load_FailsOnMissingEnvVar(t *testing.T) {
	src := kek.NewEnvSource("HOLOMUSH_NEVER_SET_KEK", false)
	_, err := src.Load(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_ENV_VAR_MISSING")
}

func TestEnvSource_Load_FailsOnWrongLength(t *testing.T) {
	t.Setenv("HOLOMUSH_TEST_KEK_SHORT", "deadbeef") // 8 hex chars -> 4 bytes
	src := kek.NewEnvSource("HOLOMUSH_TEST_KEK_SHORT", false)
	_, err := src.Load(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_ENV_VAR_WRONG_LENGTH")
}

func TestEnvSource_Load_FailsOnNonHex(t *testing.T) {
	t.Setenv("HOLOMUSH_TEST_KEK_BAD", "not hex at all !!!!")
	src := kek.NewEnvSource("HOLOMUSH_TEST_KEK_BAD", false)
	_, err := src.Load(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_ENV_VAR_NOT_HEX")
}

func TestEnvSource_Persist_Refused(t *testing.T) {
	src := kek.NewEnvSource("HOLOMUSH_TEST_KEK", false)
	err := src.Persist(context.Background(), make([]byte, 32))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_ENV_SOURCE_READ_ONLY")
}

func TestEnvSource_Name_IsLocalAEADEnv(t *testing.T) {
	src := kek.NewEnvSource("HOLOMUSH_TEST_KEK", false)
	assert.Equal(t, "local-aead/env", src.Name())
}
