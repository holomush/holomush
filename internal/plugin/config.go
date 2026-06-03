// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugins — plugin runtime config: generic-type validation of the
// manifest config schema. The host treats keys/values opaquely w.r.t. plugin
// semantics; only generic types (duration/int/bool/string) are understood, for
// structural validation.
package plugins

import (
	"strconv"
	"time"

	"github.com/samber/oops"
)

// validScalar reports whether value parses to the declared generic type.
// string always validates. Used by manifest schema validation and the merge.
func validScalar(typ, value string) error {
	switch typ {
	case "duration":
		if _, err := time.ParseDuration(value); err != nil {
			return oops.With("value", value).Wrap(err)
		}
	case "int":
		if _, err := strconv.Atoi(value); err != nil {
			return oops.With("value", value).Wrap(err)
		}
	case "bool":
		if _, err := strconv.ParseBool(value); err != nil {
			return oops.With("value", value).Wrap(err)
		}
	case "string":
		// any string is valid
	default:
		return oops.With("type", typ).Errorf("unknown config type %q", typ)
	}
	return nil
}

// MergePluginConfig computes the effective config a plugin receives: manifest
// defaults overlaid by the server-provided override, per key (override wins).
// It enforces, opaquely w.r.t. plugin meaning:
//   - INV-PLUGIN-6: an override key not declared in schema → PLUGIN_CONFIG_UNKNOWN_KEY
//   - INV-PLUGIN-5: an effective value not parseable to its declared type → PLUGIN_CONFIG_TYPE_INVALID
//   - INV-PLUGIN-4: a required key with neither default nor override → PLUGIN_CONFIG_MISSING_REQUIRED
//
// Returns a flat map[string]string ready for opaque delivery to either runtime.
func MergePluginConfig(schema map[string]ConfigParam, override map[string]string) (map[string]string, error) {
	for k := range override {
		if _, ok := schema[k]; !ok {
			return nil, oops.Code("PLUGIN_CONFIG_UNKNOWN_KEY").
				With("key", k).Errorf("override key %q not declared in manifest config schema", k)
		}
	}
	out := make(map[string]string, len(schema))
	for key, p := range schema {
		val, has := override[key]
		if !has {
			if p.Default == "" {
				if p.Required {
					return nil, oops.Code("PLUGIN_CONFIG_MISSING_REQUIRED").
						With("key", key).Errorf("required config key %q has no default and no override", key)
				}
				continue // absent, not required → omit
			}
			val = p.Default
		}
		if err := validScalar(p.Type, val); err != nil {
			return nil, oops.Code("PLUGIN_CONFIG_TYPE_INVALID").
				With("key", key).Wrapf(err, "config key %q: value does not parse as %s", key, p.Type)
		}
		out[key] = val
	}
	return out, nil
}

// validateConfigSchema checks each declared config param has a known generic
// type and a parseable default (if any). Semantic meaning is plugin-owned.
func validateConfigSchema(schema map[string]ConfigParam) error {
	for key, p := range schema {
		switch p.Type {
		case "duration", "int", "bool", "string":
		default:
			return oops.Code("PLUGIN_CONFIG_SCHEMA_INVALID").
				With("key", key).With("type", p.Type).
				Errorf("config key %q: unknown type %q (want duration|int|bool|string)", key, p.Type)
		}
		if p.Default != "" {
			if err := validScalar(p.Type, p.Default); err != nil {
				return oops.Code("PLUGIN_CONFIG_SCHEMA_INVALID").
					With("key", key).Wrapf(err, "config key %q: default does not parse as %s", key, p.Type)
			}
		}
	}
	return nil
}
