// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
)

// characterNameResolver resolves character display names by ID. Narrow
// seam to keep the ListFocusPresence handler free of world-repo plumbing
// and to make handler tests substitutable.
type characterNameResolver interface {
	Names(ctx context.Context, ids []ulid.ULID) (map[ulid.ULID]string, error)
}

// Compile-time assertion: RepoCharacterNameResolver satisfies characterNameResolver.
var _ characterNameResolver = (*RepoCharacterNameResolver)(nil)

// RepoCharacterNameResolver is the production implementation, backed by
// world.CharacterRepository.GetNamesByIDs.
type RepoCharacterNameResolver struct {
	repo world.CharacterRepository
}

// NewRepoCharacterNameResolver constructs a resolver bound to the given repo.
func NewRepoCharacterNameResolver(repo world.CharacterRepository) *RepoCharacterNameResolver {
	return &RepoCharacterNameResolver{repo: repo}
}

// Names returns a map[id]name. Missing IDs are absent from the result.
// Empty input returns a non-nil empty map with no repo call.
func (r *RepoCharacterNameResolver) Names(ctx context.Context, ids []ulid.ULID) (map[ulid.ULID]string, error) {
	if len(ids) == 0 {
		return map[ulid.ULID]string{}, nil
	}
	names, err := r.repo.GetNamesByIDs(ctx, ids)
	if err != nil {
		return nil, oops.Wrap(err)
	}
	return names, nil
}
