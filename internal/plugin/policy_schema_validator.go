// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/dsl"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// ValidateManifestPolicySchemas verifies that every attribute reference in
// each manifest policy's DSL exists in the plugin's declared schema for the
// policy's resource type. Returns a non-nil error on the first mismatch so
// the plugin load fails before any policy is installed.
//
// schemas is the schema map discovered via GetSchema during plugin load.
// Plugins without resource_types (schemas == nil or empty) are out of scope
// for this check and return nil.
//
// Unparseable policies are skipped — ValidatePluginPolicy has already
// rejected them by the time this function runs.
//
// The function returns on the FIRST mismatch rather than aggregating errors.
// Rationale: a plugin author fixing a typo will re-run the load anyway, and
// aggregation produces harder-to-read error messages for agent consumers.
// If multi-error reporting is needed later, it is a non-breaking follow-up.
func ValidateManifestPolicySchemas(
	manifest *Manifest,
	schemas map[string]*types.NamespaceSchema,
) error {
	if len(schemas) == 0 {
		return nil
	}

	for _, mp := range manifest.Policies {
		parsed, err := dsl.Parse(mp.DSL)
		if err != nil {
			// Unparseable policies were already rejected by ValidatePluginPolicy.
			// Skip them silently here to avoid double-reporting.
			continue
		}
		if parsed.Target == nil || parsed.Target.Resource == nil {
			continue
		}
		rt := parsed.Target.Resource.Type
		if rt == "" {
			continue
		}
		schema, ok := schemas[rt]
		if !ok || schema == nil {
			// Resource type not in this plugin's schema map. Either the
			// policy targets a core type (already rejected by
			// ValidatePluginPolicy for non-trusted plugins) or a type the
			// plugin declared but GetSchema didn't return (separate bug
			// outside the scope of this validator). Skip.
			continue
		}
		for _, attr := range referencedResourceAttrs(parsed) {
			if _, known := schema.Attributes[attr]; !known {
				// Build the list of valid attribute names for the error.
				validKeys := make([]string, 0, len(schema.Attributes))
				for k := range schema.Attributes {
					validKeys = append(validKeys, k)
				}
				return oops.
					Code("PLUGIN_SCHEMA_VALIDATION_FAILED").
					In("plugin").
					With("plugin", manifest.Name).
					With("policy", mp.Name).
					With("resource_type", rt).
					With("attribute", attr).
					With("schema_keys", validKeys).
					Errorf("policy %q references attribute %q on resource type %q which is not in the declared schema",
						mp.Name, attr, rt)
			}
		}
	}

	return nil
}
