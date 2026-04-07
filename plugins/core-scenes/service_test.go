// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// fakeStore is an in-memory sceneStorer used by service unit tests. It
// supports configurable error injection so tests can exercise the error
// branches of the service layer.
type fakeStore struct {
	scenes    map[string]*SceneRow
	createErr error
	getErr    error
}

func newFakeStore() *fakeStore {
	return &fakeStore{scenes: make(map[string]*SceneRow)}
}

func (f *fakeStore) Create(_ context.Context, row *SceneRow) error {
	if f.createErr != nil {
		return f.createErr
	}
	if _, exists := f.scenes[row.ID]; exists {
		return oops.Code("SCENE_CREATE_FAILED").With("scene_id", row.ID).Errorf("duplicate")
	}
	cp := *row
	f.scenes[row.ID] = &cp
	return nil
}

func (f *fakeStore) Get(_ context.Context, id string) (*SceneRow, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	row, ok := f.scenes[id]
	if !ok {
		return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
	}
	return row, nil
}

func TestSceneServiceCreateScenePersistsTitleAndOwnerWhenRequestIsValid(t *testing.T) {
	store := newFakeStore()
	svc := NewSceneServiceImpl(store)

	resp, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId: "char-alice",
		Title:       "  Tea at the Manor  ",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.GetScene())
	assert.True(t, strings.HasPrefix(resp.GetScene().GetId(), "scene-"))
	assert.Equal(t, "Tea at the Manor", resp.GetScene().GetTitle(), "title should be trimmed")
	assert.Equal(t, "char-alice", resp.GetScene().GetOwnerId())
	assert.Equal(t, string(SceneStateActive), resp.GetScene().GetState())
	assert.Equal(t, string(SceneVisibilityOpen), resp.GetScene().GetVisibility())
}

func TestSceneServiceCreateSceneRejectsWhitespaceOnlyTitle(t *testing.T) {
	svc := NewSceneServiceImpl(newFakeStore())

	_, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId: "char-alice",
		Title:       "   ",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "whitespace-only")
}

func TestSceneServiceCreateSceneReturnsInternalWhenStoreFails(t *testing.T) {
	store := newFakeStore()
	store.createErr = oops.Code("SCENE_CREATE_FAILED").Errorf("boom")
	svc := NewSceneServiceImpl(store)

	_, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId: "char-alice",
		Title:       "Tea",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestSceneServiceGetSceneReturnsSceneWhenItExists(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-known"] = &SceneRow{
		ID:         "scene-known",
		Title:      "Existing",
		OwnerID:    "char-alice",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}
	svc := NewSceneServiceImpl(store)

	resp, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{SceneId: "scene-known"})
	require.NoError(t, err)
	assert.Equal(t, "scene-known", resp.GetScene().GetId())
	assert.Equal(t, "Existing", resp.GetScene().GetTitle())
}

func TestSceneServiceGetSceneReturnsNotFoundWhenSceneIsMissing(t *testing.T) {
	svc := NewSceneServiceImpl(newFakeStore())

	_, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{SceneId: "scene-missing"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceGetSceneReturnsInternalForUnknownStoreError(t *testing.T) {
	store := newFakeStore()
	store.getErr = errors.New("connection refused")
	svc := NewSceneServiceImpl(store)

	_, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{SceneId: "scene-x"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}
