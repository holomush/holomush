// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package wmodel_test

import (
	"go/build"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world/wmodel"
)

func TestMutationDeltaCarriesPrimaryAndAffectedAggregates(t *testing.T) {
	primaryID := ulid.Make()
	cascadeID := ulid.Make()

	delta := wmodel.MutationDelta{
		Primary: wmodel.AffectedAggregate{
			Type:          wmodel.AggregateExit,
			ID:            primaryID,
			BeforeVersion: 3,
			AfterVersion:  0,
			Tombstone:     true,
		},
		Affected: []wmodel.AffectedAggregate{
			{
				Type:      wmodel.AggregateExit,
				ID:        cascadeID,
				Tombstone: true,
			},
		},
	}

	assert.Equal(t, primaryID, delta.Primary.ID)
	assert.Equal(t, wmodel.AggregateExit, delta.Primary.Type)
	assert.Equal(t, 3, delta.Primary.BeforeVersion)
	assert.True(t, delta.Primary.Tombstone, "delete delta marks the primary as a tombstone")
	require.Len(t, delta.Affected, 1, "cascade (reverse exit) reported as an affected aggregate")
	assert.Equal(t, cascadeID, delta.Affected[0].ID)
	assert.True(t, delta.Affected[0].Tombstone)
}

func TestAggregateTypeConstantsCoverEveryWorldAggregate(t *testing.T) {
	got := []wmodel.AggregateType{
		wmodel.AggregateLocation,
		wmodel.AggregateExit,
		wmodel.AggregateCharacter,
		wmodel.AggregateObject,
		wmodel.AggregateScene,
	}
	want := []wmodel.AggregateType{"location", "exit", "character", "object", "scene"}
	assert.Equal(t, want, got)
}

// TestWmodelIsCycleNeutralLeaf proves the leaf-package invariant: wmodel must
// import none of internal/world, internal/world/postgres, internal/world/outbox,
// so the world <-> outbox <-> postgres import cycles cannot form.
func TestWmodelIsCycleNeutralLeaf(t *testing.T) {
	pkg, err := build.ImportDir(".", build.ImportComment)
	require.NoError(t, err)

	forbidden := map[string]struct{}{
		"github.com/holomush/holomush/internal/world":          {},
		"github.com/holomush/holomush/internal/world/postgres": {},
		"github.com/holomush/holomush/internal/world/outbox":   {},
	}
	for _, imp := range pkg.Imports {
		_, bad := forbidden[imp]
		assert.False(t, bad, "wmodel is a leaf package and must not import %s", imp)
	}
}
