// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// SchemaChanges describes the differences between an old and new namespace schema.
type SchemaChanges struct {
	Added       []string
	Removed     []string
	TypeChanged []string
}

// HasBreakingChanges returns true if any removals or type changes were detected.
func (sc SchemaChanges) HasBreakingChanges() bool {
	return len(sc.Removed) > 0 || len(sc.TypeChanged) > 0
}

// IsEmpty returns true if no changes were detected.
func (sc SchemaChanges) IsEmpty() bool {
	return len(sc.Added) == 0 && len(sc.Removed) == 0 && len(sc.TypeChanged) == 0
}

// DetectSchemaChanges compares two namespace schemas and returns the differences.
func DetectSchemaChanges(oldSchema, newSchema *types.NamespaceSchema) SchemaChanges {
	var changes SchemaChanges

	for key, newType := range newSchema.Attributes {
		oldType, exists := oldSchema.Attributes[key]
		if !exists {
			changes.Added = append(changes.Added, key)
		} else if oldType != newType {
			changes.TypeChanged = append(changes.TypeChanged, key)
		}
	}

	for key := range oldSchema.Attributes {
		if _, exists := newSchema.Attributes[key]; !exists {
			changes.Removed = append(changes.Removed, key)
		}
	}

	return changes
}

// PolicyReference records a policy that references a removed attribute.
type PolicyReference struct {
	DSLText   string
	Attribute string
}

// ScanPoliciesForAttributes scans DSL texts for references to removed namespace attributes.
func ScanPoliciesForAttributes(namespace string, removedKeys, dslTexts []string) []PolicyReference {
	var refs []PolicyReference

	for _, dsl := range dslTexts {
		for _, key := range removedKeys {
			pattern := fmt.Sprintf("%s.%s", namespace, key)
			if strings.Contains(dsl, pattern) {
				refs = append(refs, PolicyReference{
					DSLText:   dsl,
					Attribute: key,
				})
			}
		}
	}

	return refs
}

// LogSchemaChanges logs schema changes at appropriate severity levels.
func LogSchemaChanges(namespace string, changes SchemaChanges) {
	for _, key := range changes.Added {
		slog.Info("schema evolution: attribute added",
			"namespace", namespace,
			"attribute", key,
		)
	}

	for _, key := range changes.TypeChanged {
		slog.Warn("schema evolution: attribute type changed — existing policies may break",
			"namespace", namespace,
			"attribute", key,
		)
	}

	for _, key := range changes.Removed {
		slog.Warn("schema evolution: attribute removed — scanning policies for references",
			"namespace", namespace,
			"attribute", key,
		)
	}
}

// EvaluateNamespaceRemoval checks if a namespace can be safely removed.
func EvaluateNamespaceRemoval(namespace string, dslTexts []string) error {
	for _, dsl := range dslTexts {
		if strings.Contains(dsl, namespace+".") {
			return fmt.Errorf("cannot remove namespace %q: referenced by enabled policies", namespace)
		}
	}
	return nil
}

// UpdateNamespace replaces a namespace schema with a new version.
func (r *SchemaRegistry) UpdateNamespace(namespace string, newSchema *types.NamespaceSchema, dslTexts []string) (SchemaChanges, error) {
	oldNS := r.schema.GetNamespace(namespace)
	if oldNS == nil {
		if err := r.Register(namespace, newSchema); err != nil {
			return SchemaChanges{}, err
		}
		return SchemaChanges{}, nil
	}

	changes := DetectSchemaChanges(oldNS, newSchema)
	if changes.IsEmpty() {
		return changes, nil
	}

	LogSchemaChanges(namespace, changes)

	if len(changes.Removed) > 0 && len(dslTexts) > 0 {
		refs := ScanPoliciesForAttributes(namespace, changes.Removed, dslTexts)
		for _, ref := range refs {
			slog.Warn("policy references removed attribute — mark for review",
				"namespace", namespace,
				"attribute", ref.Attribute,
			)
		}
	}

	r.schema.Replace(namespace, newSchema)

	return changes, nil
}

// RemoveNamespace removes a namespace from the registry.
func (r *SchemaRegistry) RemoveNamespace(namespace string, dslTexts []string) error {
	if err := EvaluateNamespaceRemoval(namespace, dslTexts); err != nil {
		return err
	}
	r.schema.Remove(namespace)
	slog.Error("schema evolution: namespace removed",
		"namespace", namespace,
	)
	return nil
}
