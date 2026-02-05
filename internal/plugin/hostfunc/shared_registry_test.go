// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/plugin/capability"
)

func TestSharedRegistry_DefaultsSharedWithCommand(t *testing.T) {
	funcs := New(nil, capability.NewEnforcer())
	services := command.NewTestServices(command.ServicesConfig{})

	require.NotNil(t, services.PropertyRegistry())
	require.NotNil(t, funcs.propertyRegistry)
	require.Same(t, services.PropertyRegistry(), funcs.propertyRegistry)
}
