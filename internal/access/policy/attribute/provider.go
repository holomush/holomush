// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// AttributeProvider resolves attributes for a specific namespace.
// Providers return attributes for subjects and resources.
//
// IMPORTANT: Providers MUST return all numeric attributes as float64.
// This ensures consistent type handling in policy evaluation.
//
// Example:
//
//	type CharacterProvider struct {}
//
//	func (p *CharacterProvider) Namespace() string {
//		return "character"
//	}
//
//	func (p *CharacterProvider) ResolveSubject(ctx context.Context, subjectID string) (map[string]any, error) {
//		// Query database for character attributes
//		char, err := p.repo.GetCharacter(ctx, subjectID)
//		if err != nil {
//			return nil, err
//		}
//		return map[string]any{
//			"name":  char.Name,
//			"level": float64(char.Level),  // MUST be float64, not int
//			"role":  char.Role,
//		}, nil
//	}
//
//	func (p *CharacterProvider) ResolveResource(ctx context.Context, resourceID string) (map[string]any, error) {
//		// Character provider doesn't resolve resources
//		return nil, nil
//	}
//
//	func (p *CharacterProvider) Schema() *types.NamespaceSchema {
//		return &types.NamespaceSchema{
//			Attributes: map[string]types.AttrType{
//				"name":  types.AttrTypeString,
//				"level": types.AttrTypeFloat,
//				"role":  types.AttrTypeString,
//			},
//		}
//	}
//
//nolint:revive // AttributeProvider is the canonical name from the ABAC spec
type AttributeProvider interface {
	// Namespace returns the attribute namespace this provider handles.
	// Example: "character", "location", "reputation"
	Namespace() string

	// ResolveSubject resolves attributes for a subject (principal).
	// Returns nil, nil if this provider doesn't handle the given subject type.
	// MUST return all numeric attributes as float64.
	ResolveSubject(ctx context.Context, subjectID string) (map[string]any, error)

	// ResolveResource resolves attributes for a resource.
	// Returns nil, nil if this provider doesn't handle the given resource type.
	// MUST return all numeric attributes as float64.
	ResolveResource(ctx context.Context, resourceID string) (map[string]any, error)

	// Schema returns the namespace schema defining the attributes
	// this provider contributes.
	Schema() *types.NamespaceSchema
}

// EnvironmentProvider resolves environment-level attributes (no entity context).
// Environment attributes are global state that doesn't depend on specific
// subjects or resources.
//
// Example:
//
//	type EnvProvider struct {}
//
//	func (p *EnvProvider) Namespace() string {
//		return "env"
//	}
//
//	func (p *EnvProvider) Resolve(ctx context.Context) (map[string]any, error) {
//		now := time.Now().UTC()
//		return map[string]any{
//			"time":        now.Format(time.RFC3339),
//			"hour":        float64(now.Hour()),  // MUST be float64
//			"minute":      float64(now.Minute()),
//			"day_of_week": strings.ToLower(now.Weekday().String()),
//			"maintenance": false,
//		}, nil
//	}
//
//	func (p *EnvProvider) Schema() *types.NamespaceSchema {
//		return &types.NamespaceSchema{
//			Attributes: map[string]types.AttrType{
//				"time":        types.AttrTypeString,
//				"hour":        types.AttrTypeFloat,
//				"minute":      types.AttrTypeFloat,
//				"day_of_week": types.AttrTypeString,
//				"maintenance": types.AttrTypeBool,
//			},
//		}
//	}
type EnvironmentProvider interface {
	// Namespace returns the attribute namespace this provider handles.
	// Example: "env", "weather"
	Namespace() string

	// Resolve resolves environment attributes.
	// MUST return all numeric attributes as float64.
	Resolve(ctx context.Context) (map[string]any, error)

	// Schema returns the namespace schema defining the attributes
	// this provider contributes.
	Schema() *types.NamespaceSchema
}
