// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBrokerServicesExtractsBrokerIDs(t *testing.T) {
	services := map[string]string{
		"holomush.world.v1.WorldService": "broker:42",
		"holomush.scene.v1.SceneService": "broker:7",
	}

	result, err := ParseBrokerServices(services)
	require.NoError(t, err)

	assert.Equal(t, uint32(42), result["holomush.world.v1.WorldService"])
	assert.Equal(t, uint32(7), result["holomush.scene.v1.SceneService"])
}

func TestParseBrokerServicesRejectsInvalidFormat(t *testing.T) {
	services := map[string]string{
		"holomush.world.v1.WorldService": "invalid:format",
	}

	_, err := ParseBrokerServices(services)
	require.Error(t, err)
}

func TestParseBrokerServicesRejectsMissingPrefix(t *testing.T) {
	services := map[string]string{
		"holomush.world.v1.WorldService": "42",
	}

	_, err := ParseBrokerServices(services)
	require.Error(t, err)
}

func TestParseBrokerServicesHandlesEmptyMap(t *testing.T) {
	result, err := ParseBrokerServices(map[string]string{})
	require.NoError(t, err)
	assert.Empty(t, result)
}
