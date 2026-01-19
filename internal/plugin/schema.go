package plugin

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"
	jschema "github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// schemaCache holds the compiled schema to avoid recompilation.
var schemaCache *jschema.Schema

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
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}
	return data, nil
}

// ValidateSchema validates YAML data against the plugin manifest JSON Schema.
func ValidateSchema(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("manifest data is empty")
	}

	// Parse YAML to generic interface for validation
	var yamlData any
	if err := yaml.Unmarshal(data, &yamlData); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}

	// Convert YAML to JSON-compatible types (yaml.Unmarshal uses map[string]any)
	jsonData := convertToJSONTypes(yamlData)

	// Get or compile schema
	sch, err := getCompiledSchema()
	if err != nil {
		return fmt.Errorf("failed to compile schema: %w", err)
	}

	// Validate
	if err := sch.Validate(jsonData); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	return nil
}

// getCompiledSchema returns the cached compiled schema or compiles it.
func getCompiledSchema() (*jschema.Schema, error) {
	if schemaCache != nil {
		return schemaCache, nil
	}

	schemaBytes, err := GenerateSchema()
	if err != nil {
		return nil, err
	}

	// Parse schema JSON
	var schemaData any
	if err := json.Unmarshal(schemaBytes, &schemaData); err != nil {
		return nil, fmt.Errorf("failed to parse schema JSON: %w", err)
	}

	// Compile schema
	c := jschema.NewCompiler()
	if err := c.AddResource("schema.json", schemaData); err != nil {
		return nil, fmt.Errorf("failed to add schema resource: %w", err)
	}

	sch, err := c.Compile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", err)
	}

	schemaCache = sch
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
func ResetSchemaCache() {
	schemaCache = nil
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
