// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"net/url"
	"testing"

	"github.com/holomush/holomush/pkg/errutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginSchemaName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"prepends plugin prefix to simple name", "scenes", "plugin_scenes"},
		{"converts hyphens to underscores", "core-scenes", "plugin_core_scenes"},
		{"handles multiple hyphens", "my-cool-plugin", "plugin_my_cool_plugin"},
		{"preserves underscores in original name", "core_utils", "plugin_core_utils"},
		{"handles single character name", "x", "plugin_x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, pluginSchemaName(tt.input))
		})
	}
}

func TestPluginRoleName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"prepends holomush_plugin prefix", "scenes", "holomush_plugin_scenes"},
		{"converts hyphens to underscores", "core-scenes", "holomush_plugin_core_scenes"},
		{"handles multiple hyphens", "my-cool-plugin", "holomush_plugin_my_cool_plugin"},
		{"preserves underscores", "core_utils", "holomush_plugin_core_utils"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, pluginRoleName(tt.input))
		})
	}
}

func TestGeneratePasswordProduces256BitsOfEntropy(t *testing.T) {
	pw, err := generatePassword()
	require.NoError(t, err)

	// 32 bytes base64url-encoded without padding = 43 chars
	assert.Len(t, pw, 43)
}

func TestGeneratePasswordIsUnique(t *testing.T) {
	pw1, err := generatePassword()
	require.NoError(t, err)
	pw2, err := generatePassword()
	require.NoError(t, err)

	assert.NotEqual(t, pw1, pw2)
}

func TestPluginConnStringReplacesCredentialsAndSearchPath(t *testing.T) {
	base := "postgres://serveruser:serverpass@localhost:5432/dbname?sslmode=disable"
	got, err := pluginConnString(base, "plugin_core_scenes", "holomush_plugin_core_scenes", "s3cret")
	require.NoError(t, err)

	u, err := url.Parse(got)
	require.NoError(t, err)

	assert.Equal(t, "holomush_plugin_core_scenes", u.User.Username())
	pw, ok := u.User.Password()
	assert.True(t, ok)
	assert.Equal(t, "s3cret", pw)
	assert.Equal(t, "plugin_core_scenes", u.Query().Get("search_path"))
	assert.Equal(t, "disable", u.Query().Get("sslmode"))
	assert.Equal(t, "localhost:5432", u.Host)
	assert.Equal(t, "/dbname", u.Path)
}

func TestPluginConnStringRejectsInvalidURL(t *testing.T) {
	_, err := pluginConnString("://bad", "s", "r", "p")
	require.Error(t, err)
}

func TestNewSchemaProvisionerStoresBaseConnString(t *testing.T) {
	base := "postgres://user:pass@localhost:5432/db"
	sp := NewSchemaProvisioner(base)
	assert.Equal(t, base, sp.baseConnString)
	assert.Nil(t, sp.pool)
}

func TestCloseIsNoOpWithoutInit(t *testing.T) {
	sp := NewSchemaProvisioner("postgres://localhost/db")
	assert.NotPanics(t, func() {
		sp.Close()
	})
}

func TestValidatePostgresPasswordLiteralAcceptsGeneratedPasswords(t *testing.T) {
	// generatePassword produces base64url output which must always pass
	// the validator. Run many iterations to cover entropy surface.
	for i := 0; i < 1000; i++ {
		pw, err := generatePassword()
		require.NoError(t, err)
		assert.NoError(t, validatePostgresPasswordLiteral(pw),
			"generatePassword output must pass the literal validator: %q", pw)
	}
}

func TestValidatePostgresPasswordLiteralRejectsUnsafeCharacters(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"rejects password containing single quote", "abc'def"},
		{"rejects password containing backslash", "abc\\def"},
		{"rejects password containing NULL byte", "abc\x00def"},
		{"rejects password that is only a single quote", "'"},
		{"rejects password that is only a backslash", "\\"},
		{"rejects SQL injection attempt with quote and DDL", "x'; DROP ROLE foo; --"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePostgresPasswordLiteral(tt.input)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "PASSWORD_UNSAFE_LITERAL")
		})
	}
}

func TestValidatePostgresPasswordLiteralAcceptsSafePasswords(t *testing.T) {
	safe := []string{
		"plainalpha",
		"with-dashes_and_underscores",
		"MixedCase123",
		"base64url-like_AAAA",
		"",
	}
	for _, pw := range safe {
		assert.NoError(t, validatePostgresPasswordLiteral(pw), "expected %q to be accepted", pw)
	}
}

func TestEnsureRoleRejectsUnsafePasswordBeforeTouchingPool(t *testing.T) {
	// sp.pool is nil here; if the validator runs first as required, the
	// function must return the validation error without dereferencing
	// the pool (which would panic).
	sp := &SchemaProvisioner{}
	err := sp.ensureRole(context.Background(), "holomush_plugin_test", "evil'injection")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PASSWORD_UNSAFE_LITERAL")
}
