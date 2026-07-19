// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

func TestDecodeConfigDecodesAllSupportedFieldTypes(t *testing.T) {
	type demoCfg struct {
		VoteWindow time.Duration `mapstructure:"vote_window"`
		MaxTries   int           `mapstructure:"max_tries"`
		Enabled    bool          `mapstructure:"enabled"`
		Label      string        `mapstructure:"label"`
	}
	sc := &pluginv1.ServiceConfig{PluginConfig: map[string]string{
		"vote_window": "168h", "max_tries": "3", "enabled": "true", "label": "x",
	}}
	got, err := DecodeConfig[demoCfg](sc)
	require.NoError(t, err)
	require.Equal(t, 168*time.Hour, got.VoteWindow)
	require.Equal(t, 3, got.MaxTries)
	require.True(t, got.Enabled)
	require.Equal(t, "x", got.Label)
}

func TestDecodeConfigNilSafeWhenNoPluginConfig(t *testing.T) {
	type demoCfg struct {
		VoteWindow time.Duration `mapstructure:"vote_window"`
	}
	got, err := DecodeConfig[demoCfg](&pluginv1.ServiceConfig{}) // no plugin_config
	require.NoError(t, err)
	require.Zero(t, got.VoteWindow)
}

func TestResolveGameIDReturnsHostSuppliedValue(t *testing.T) {
	sc := &pluginv1.ServiceConfig{GameId: "01KXVJHWPYXZ9NGPBC3V0C9WD0"}
	require.Equal(t, "01KXVJHWPYXZ9NGPBC3V0C9WD0", ResolveGameID(sc))
}

func TestResolveGameIDFallsBackToMainWhenUnset(t *testing.T) {
	require.Equal(t, "main", ResolveGameID(&pluginv1.ServiceConfig{}))
}

func TestResolveGameIDFallsBackToMainWhenConfigNil(t *testing.T) {
	require.Equal(t, "main", ResolveGameID(nil))
}
