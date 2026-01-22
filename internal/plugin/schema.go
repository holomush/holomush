// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugin

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/invopop/jsonschema"
	"github.com/samber/oops"
	jschema "github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// schemaState holds the compiled schema and sync.Once for thread-safe initialization.
type schemaState struct {
	once   sync.Once
	schema *jschema.Schema
	err    error
}

// globalSchemaState is the package-level schema state.
var globalSchemaState = &schemaState{}

// GenerateSchema generates a JSON Schema from the Manifest struct.
func GenerateSchema() ([]byte, error) {
	r := jsonschema.Reflector{
		DoNotReference: true,
	}
	schema := r.Reflect(&Manifest{})

	// Add schema metadata
	schema.ID = jsonschema.ID(GetSchemaID())
	schema.Title = "HoloMUSH Plugin Manifest"
	schema.Description = "Schema for plugin.yaml manifest files"

	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, oops.In("schema").Hint("failed to marshal schema").Wrap(err)
	}
	// Append trailing newline for POSIX compliance
	data = append(data, '\n')
	return data, nil
}

// ValidateSchema validates YAML data against the plugin manifest JSON Schema.
func ValidateSchema(data []byte) error {
	if len(data) == 0 {
		return oops.In("schema").New("manifest data is empty")
	}

	// Parse YAML to generic interface for validation
	var yamlData any
	if err := yaml.Unmarshal(data, &yamlData); err != nil {
		return oops.In("schema").Hint("invalid YAML").Wrap(err)
	}

	// Convert YAML to JSON-compatible types (yaml.Unmarshal uses map[string]any)
	jsonData := convertToJSONTypes(yamlData)

	// Get or compile schema
	sch, err := getCompiledSchema()
	if err != nil {
		return oops.In("schema").Hint("failed to compile schema").Wrap(err)
	}

	// Validate
	if err := sch.Validate(jsonData); err != nil {
		return oops.In("schema").Hint("schema validation failed").Wrap(err)
	}

	return nil
}

// getCompiledSchema returns the cached compiled schema or compiles it.
// Thread-safe via sync.Once.
func getCompiledSchema() (*jschema.Schema, error) {
	globalSchemaState.once.Do(func() {
		globalSchemaState.schema, globalSchemaState.err = compileSchema()
	})
	return globalSchemaState.schema, globalSchemaState.err
}

// compileSchema does the actual schema compilation work.
func compileSchema() (*jschema.Schema, error) {
	schemaBytes, err := GenerateSchema()
	if err != nil {
		return nil, err
	}

	// Parse schema JSON
	var schemaData any
	err = json.Unmarshal(schemaBytes, &schemaData)
	if err != nil {
		return nil, oops.In("schema").Hint("failed to parse schema JSON").Wrap(err)
	}

	// Compile schema
	c := jschema.NewCompiler()
	err = c.AddResource("schema.json", schemaData)
	if err != nil {
		return nil, oops.In("schema").Hint("failed to add schema resource").Wrap(err)
	}

	sch, err := c.Compile("schema.json")
	if err != nil {
		return nil, oops.In("schema").Hint("failed to compile schema").Wrap(err)
	}

	return sch, nil
}

// convertToJSONTypes converts YAML-parsed data to JSON-compatible types.
// YAML uses map[string]any which is compatible, but we need to handle
// nested structures recursively.
func convertToJSONTypes(v any) any {
	switch val := v.(type) {
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, v := range val {
			result[k] = convertToJSONTypes(v)
		}
		return result
	case []any:
		result := make([]any, len(val))
		for i, v := range val {
			result[i] = convertToJSONTypes(v)
		}
		return result
	case string:
		return val
	case int:
		return val
	case int64:
		return val
	case float64:
		return val
	case bool:
		return val
	case nil:
		return nil
	default:
		// For other types, try to convert via JSON round-trip
		if b, err := json.Marshal(val); err == nil {
			var result any
			if err := json.Unmarshal(b, &result); err == nil {
				return result
			}
		}
		return val
	}
}

// ResetSchemaCache clears the cached schema. Used for testing.
// Creates a new schemaState so sync.Once can trigger again.
func ResetSchemaCache() {
	globalSchemaState = &schemaState{}
}

// GetSchemaID returns the schema $id for use in plugin.yaml files.
func GetSchemaID() string {
	return "https://holomush.dev/schemas/plugin.schema.json"
}

// FormatSchemaError formats a schema validation error for display.
func FormatSchemaError(err error) string {
	if err == nil {
		return ""
	}
	// Extract the meaningful part of the error
	msg := err.Error()
	if strings.Contains(msg, "schema validation failed:") {
		msg = strings.TrimPrefix(msg, "schema validation failed: ")
	}
	return msg
}
