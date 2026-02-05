// Copyright 2026 HoloMUSH Contributors

package property

import (
	"context"
	"fmt"

	"github.com/oklog/ulid/v2"
)

type namePropertyDefinition struct{}

func (namePropertyDefinition) Validate(entityType string) error {
	if _, ok := SharedEntityMutatorRegistry().Lookup(entityType); !ok {
		return fmt.Errorf("invalid entity type: %s", entityType)
	}
	return nil
}

func (namePropertyDefinition) Get(ctx context.Context, querier WorldQuerier, entityType string, entityID ulid.ULID) (string, error) {
	mutator, ok := SharedEntityMutatorRegistry().Lookup(entityType)
	if !ok {
		return "", fmt.Errorf("entity mutator not found for type: %s", entityType)
	}
	return mutator.GetName(ctx, querier, entityID)
}

func (namePropertyDefinition) Set(ctx context.Context, querier WorldQuerier, mutator WorldMutator, subjectID string, entityType string, entityID ulid.ULID, value string) error {
	entityMutator, ok := SharedEntityMutatorRegistry().Lookup(entityType)
	if !ok {
		return fmt.Errorf("entity mutator not found for type: %s", entityType)
	}
	return entityMutator.SetName(ctx, querier, mutator, subjectID, entityID, value)
}
