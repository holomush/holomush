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

// Compile-time assertions: RepoCharacterNameResolver satisfies both resolver interfaces.
var (
	_ characterNameResolver = (*RepoCharacterNameResolver)(nil)
	_ sceneNameResolver     = (*RepoCharacterNameResolver)(nil)
)

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

// NamesByIDs resolves character names from string IDs (satisfies sceneNameResolver).
// Invalid ULIDs are silently skipped. Missing IDs are absent from the result.
// Empty input returns a non-nil empty map with no repo call.
func (r *RepoCharacterNameResolver) NamesByIDs(ctx context.Context, ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	parsed := make([]ulid.ULID, 0, len(ids))
	for _, s := range ids {
		if s == "" {
			continue
		}
		id, err := ulid.Parse(s)
		if err != nil {
			continue // skip invalid IDs silently
		}
		parsed = append(parsed, id)
	}
	if len(parsed) == 0 {
		return map[string]string{}, nil
	}
	byULID, err := r.repo.GetNamesByIDs(ctx, parsed)
	if err != nil {
		return nil, oops.Wrap(err)
	}
	out := make(map[string]string, len(byULID))
	for id, name := range byULID {
		out[id.String()] = name
	}
	return out, nil
}
