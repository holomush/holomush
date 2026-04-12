// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/settings"
)

func TestValidateNamespaceAcceptsRegisteredNamespace(t *testing.T) {
	assert.NoError(t, settings.ValidateNamespace("scenes.focus.replay_tail_default"))
}

func TestValidateNamespaceAcceptsCoreNamespace(t *testing.T) {
	assert.NoError(t, settings.ValidateNamespace("core.server_name"))
}

func TestValidateNamespaceAcceptsChannelsNamespace(t *testing.T) {
	assert.NoError(t, settings.ValidateNamespace("channels.max_per_player"))
}

func TestValidateNamespaceAcceptsAuthNamespace(t *testing.T) {
	assert.NoError(t, settings.ValidateNamespace("auth.session_timeout"))
}

func TestValidateNamespaceRejectsUnknownNamespace(t *testing.T) {
	err := settings.ValidateNamespace("unknown.some_key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown namespace")
}

func TestValidateNamespaceRejectsKeyWithoutDot(t *testing.T) {
	err := settings.ValidateNamespace("nodot")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be dot-namespaced")
}

func TestValidateNamespaceRejectsEmptyKey(t *testing.T) {
	err := settings.ValidateNamespace("")
	assert.Error(t, err)
}

func TestRegisteredNamespacesContainsExpectedEntries(t *testing.T) {
	expected := []string{"core", "scenes", "channels", "auth"}
	assert.Equal(t, expected, settings.RegisteredNamespaces)
}
