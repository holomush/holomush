// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package attribute provides attribute provider interfaces and schema registry.
package attribute

import (
	"fmt"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/samber/oops"
)

// SchemaRegistry manages attribute schema registration and validation.
// It wraps types.AttributeSchema with full validation logic.
type SchemaRegistry struct {
	schema *types.AttributeSchema
}

// NewSchemaRegistry creates a new SchemaRegistry with an empty schema.
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{
		schema: types.NewAttributeSchema(),
	}
}

// Register adds a namespace schema with full validation.
// Returns error if:
// - namespace is empty
// - namespace is already registered
// - schema is nil
// - schema has no attributes
func Register(r *SchemaRegistry, namespace string, schema *types.NamespaceSchema) error {
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}

	if r.schema.HasNamespace(namespace) {
		return fmt.Errorf("namespace already registered: %s", namespace)
	}

	if schema == nil {
		return fmt.Errorf("schema cannot be nil")
	}

	if len(schema.Attributes) == 0 {
		return fmt.Errorf("schema must have at least one attribute")
	}

	// Validate that all attribute types are valid
	for key, attrType := range schema.Attributes {
		// AttrType uses iota (0-4), so any value outside that range is invalid
		if attrType < 0 || attrType > types.AttrTypeStringList {
			return fmt.Errorf("invalid attribute type for %s.%s: %d", namespace, key, attrType)
		}
	}

	if err := r.schema.Register(namespace, schema); err != nil {
		return oops.Wrapf(err, "failed to register namespace %s", namespace)
	}
	return nil
}

// Register is the receiver method that delegates to the package-level function.
func (r *SchemaRegistry) Register(namespace string, schema *types.NamespaceSchema) error {
	return Register(r, namespace, schema)
}

// IsRegistered checks if a namespace+key pair exists in the registry.
func (r *SchemaRegistry) IsRegistered(namespace, key string) bool {
	return r.schema.IsRegistered(namespace, key)
}

// HasNamespace returns true if the given namespace has been registered.
func (r *SchemaRegistry) HasNamespace(namespace string) bool {
	return r.schema.HasNamespace(namespace)
}

// Schema returns the underlying AttributeSchema for use by the compiler.
func (r *SchemaRegistry) Schema() *types.AttributeSchema {
	return r.schema
}
