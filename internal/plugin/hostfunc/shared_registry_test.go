// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
)

func TestSharedRegistryDefaultsSharedWithCommand(t *testing.T) {
	funcs := New(nil)
	services := command.NewTestServices(command.ServicesConfig{})

	require.NotNil(t, services.PropertyRegistry())
	require.NotNil(t, funcs.propertyRegistry)
	require.Same(t, services.PropertyRegistry(), funcs.propertyRegistry)
}

func TestSharedRegistryIsNonNilOnNewFunctions(t *testing.T) {
	// The property registry is always initialized (never nil) so plugins can
	// safely call get_property / set_property without a nil-check guard.
	funcs := New(nil)
	assert.NotNil(t, funcs.propertyRegistry, "propertyRegistry must be non-nil after New()")
}

func TestSharedRegistryIndependentInstances(t *testing.T) {
	// Each call to New() shares the same global registry singleton, so two
	// independent Functions instances refer to the same registry object.
	f1 := New(nil)
	f2 := New(nil)
	assert.Same(t, f1.propertyRegistry, f2.propertyRegistry,
		"all Functions instances share the same global property registry")
}
