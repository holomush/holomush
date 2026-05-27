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
