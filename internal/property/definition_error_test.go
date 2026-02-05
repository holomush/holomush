// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package property

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamePropertyDefinition_MissingMutator(t *testing.T) {
	def := namePropertyDefinition{}

	_, err := def.Get(context.Background(), nil, "widget", ulid.ULID{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entity mutator not found for type: widget")

	err = def.Set(context.Background(), nil, nil, "subject", "widget", ulid.ULID{}, "value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entity mutator not found for type: widget")
}

func TestDescriptionPropertyDefinition_MissingMutator(t *testing.T) {
	def := descriptionPropertyDefinition{}

	_, err := def.Get(context.Background(), nil, "widget", ulid.ULID{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entity mutator not found for type: widget")

	err = def.Set(context.Background(), nil, nil, "subject", "widget", ulid.ULID{}, "value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entity mutator not found for type: widget")
}
