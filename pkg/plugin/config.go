// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"github.com/go-viper/mapstructure/v2"
	"github.com/samber/oops"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// DecodeConfig decodes the host-delivered opaque plugin_config map into T,
// reading `mapstructure:` struct tags. String values are coerced to the struct
// field type (durations via StringToTimeDurationHookFunc; ints/bools via weak
// typing). Defaults and required-key enforcement are applied host-side
// (MergePluginConfig); DecodeConfig is pure string→typed conversion. koanf is
// not used — there is no file/provider layering, just one in-memory map.
func DecodeConfig[T any](config *pluginv1.ServiceConfig) (T, error) {
	var out T
	raw := make(map[string]any, len(config.GetPluginConfig()))
	for k, v := range config.GetPluginConfig() {
		raw[k] = v
	}
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
		WeaklyTypedInput: true,
		TagName:          "mapstructure",
		Result:           &out,
	})
	if err != nil {
		// NewDecoder only errors when Result is not a non-nil pointer; &out
		// always is. Wrapped defensively so a future DecoderConfig change
		// cannot silently swallow a constructor failure.
		return out, oops.Code("PLUGIN_CONFIG_DECODE_FAILED").Wrap(err)
	}
	if err := dec.Decode(raw); err != nil {
		return out, oops.Code("PLUGIN_CONFIG_DECODE_FAILED").Wrap(err)
	}
	return out, nil
}

// ResolveGameID returns the game id a plugin should use when constructing a
// fully-qualified "events.<game_id>.…" subject directly (rather than
// emitting a domain-relative reference for the host to qualify). It reads
// ServiceConfig.GameId — populated by goplugin.Host.Init from the same
// gameIDProvider-resolved value every host-side subject qualification uses
// (internal/eventbus.Subsystem.GameID()) — falling back to "main"
// (eventbus.Config's own default) when config is nil or GameId is unset, so
// a test harness constructing ServiceConfig without the field keeps working.
//
// A plugin that always publishes a hardcoded "main" here will silently
// diverge from every subscriber once the host resolves a non-"main" game id
// (holomush debug e2e-scene-pose-regression): the emit lands on
// "events.main.…" while subscribers filter on "events.<real_game_id>.…", and
// neither side errors.
func ResolveGameID(config *pluginv1.ServiceConfig) string {
	if config != nil && config.GetGameId() != "" {
		return config.GetGameId()
	}
	return "main"
}
