// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	grpcpkg "github.com/holomush/holomush/internal/grpc"
	worldmocks "github.com/holomush/holomush/internal/world/worldtest"
)

func TestRepoCharacterNameResolverReturnsNamesForPresentIDs(t *testing.T) {
	repo := worldmocks.NewMockCharacterRepository(t)
	resolver := grpcpkg.NewRepoCharacterNameResolver(repo)

	id1 := ulid.MustParse("01HYXCHAR00000000000000001")
	id2 := ulid.MustParse("01HYXCHAR00000000000000002")
	repo.EXPECT().
		GetNamesByIDs(mock.Anything, []ulid.ULID{id1, id2}).
		Return(map[ulid.ULID]string{id1: "alice", id2: "bob"}, nil).
		Once()

	got, err := resolver.Names(context.Background(), []ulid.ULID{id1, id2})
	require.NoError(t, err)
	assert.Equal(t, "alice", got[id1])
	assert.Equal(t, "bob", got[id2])
}

func TestRepoCharacterNameResolverShortCircuitsOnEmptyInput(t *testing.T) {
	repo := worldmocks.NewMockCharacterRepository(t)
	resolver := grpcpkg.NewRepoCharacterNameResolver(repo)
	// No mock expectation — empty input must short-circuit with no repo call.

	got, err := resolver.Names(context.Background(), nil)
	require.NoError(t, err)
	assert.NotNil(t, got, "empty input MUST return non-nil empty map")
	assert.Empty(t, got)
}
