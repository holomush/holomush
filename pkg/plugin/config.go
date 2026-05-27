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
