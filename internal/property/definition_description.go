// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package property

import (
	"context"
	"fmt"

	"github.com/oklog/ulid/v2"
)

type descriptionDefinition struct{}

func (descriptionDefinition) Validate(entityType string) error {
	if _, ok := SharedEntityMutatorRegistry().Lookup(entityType); !ok {
		return fmt.Errorf("invalid entity type: %s", entityType)
	}
	return nil
}

func (descriptionDefinition) Get(ctx context.Context, querier WorldQuerier, entityType string, entityID ulid.ULID) (string, error) {
	mutator, ok := SharedEntityMutatorRegistry().Lookup(entityType)
	if !ok {
		return "", fmt.Errorf("entity mutator not found for type: %s", entityType)
	}
	val, err := mutator.GetDescription(ctx, querier, entityID)
	if err != nil {
		return "", fmt.Errorf("get description: %w", err)
	}
	return val, nil
}

func (descriptionDefinition) Set(ctx context.Context, querier WorldQuerier, mutator WorldMutator, subjectID, entityType string, entityID ulid.ULID, value string) error {
	entityMutator, ok := SharedEntityMutatorRegistry().Lookup(entityType)
	if !ok {
		return fmt.Errorf("entity mutator not found for type: %s", entityType)
	}
	if err := entityMutator.SetDescription(ctx, querier, mutator, subjectID, entityID, value); err != nil {
		return fmt.Errorf("set description: %w", err)
	}
	return nil
}
