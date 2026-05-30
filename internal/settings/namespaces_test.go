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
	for _, ns := range expected {
		assert.Contains(t, settings.RegisteredNamespaces, ns, "missing expected namespace %q", ns)
	}
}

func TestValidateNamespaceRejectsReservedPluginSegment(t *testing.T) {
	err := settings.ValidateNamespace("plugin.core-scenes")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reserved")
}

func TestValidateNamespaceDoesNotReserveOtherSegmentsContainingPlugin(t *testing.T) {
	// Only the exact top-level segment "plugin" is reserved; a registered
	// namespace remains valid and a non-"plugin" segment is rejected as unknown
	// (not as reserved).
	err := settings.ValidateNamespace("plugins.foo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown namespace")
}

func TestReservedNamespaceConstantIsPlugin(t *testing.T) {
	assert.Equal(t, "plugin", settings.ReservedNamespace)
	assert.NotContains(t, settings.RegisteredNamespaces, settings.ReservedNamespace,
		"reserved namespace must not also be registered")
}
