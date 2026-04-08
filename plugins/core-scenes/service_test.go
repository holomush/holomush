// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// fakeStore is an in-memory sceneStorer used by service unit tests. It
// supports configurable error injection so tests can exercise the error
// branches of the service layer.
type fakeStore struct {
	scenes             map[string]*SceneRow
	participants       map[string]map[string]string // sceneID → characterID → role
	createErr          error
	createWithOwnerErr error
	getErr             error
	addParticipantErr  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		scenes:       make(map[string]*SceneRow),
		participants: make(map[string]map[string]string),
	}
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

func (f *fakeStore) CreateWithOwner(ctx context.Context, row *SceneRow) error {
	if f.createWithOwnerErr != nil {
		return f.createWithOwnerErr
	}
	if err := f.Create(ctx, row); err != nil {
		return err
	}
	if f.participants[row.ID] == nil {
		f.participants[row.ID] = make(map[string]string)
	}
	f.participants[row.ID][row.OwnerID] = "owner"
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

func (f *fakeStore) GetWithMembership(ctx context.Context, id string) (*SceneRow, []string, []string, error) {
	row, err := f.Get(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	var participants, invitees []string
	for cid, role := range f.participants[id] {
		switch role {
		case "owner", "member":
			participants = append(participants, cid)
		case "invited":
			invitees = append(invitees, cid)
		}
	}
	return row, participants, invitees, nil
}

func (f *fakeStore) AddParticipant(_ context.Context, sceneID, characterID string) (*ParticipantRow, ParticipantOpResult, error) {
	if f.addParticipantErr != nil {
		return nil, OpNoChange, f.addParticipantErr
	}
	scene, ok := f.scenes[sceneID]
	if !ok {
		return nil, OpNoChange, oops.Code("SCENE_NOT_FOUND").With("scene_id", sceneID).Errorf("not found")
	}
	if scene.State != string(SceneStateActive) && scene.State != string(SceneStatePaused) {
		return nil, OpNoChange, oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", sceneID).With("current_state", scene.State).Errorf("cannot join")
	}
	if f.participants[sceneID] == nil {
		f.participants[sceneID] = make(map[string]string)
	}
	existing, exists := f.participants[sceneID][characterID]
	if exists {
		if existing == "invited" {
			f.participants[sceneID][characterID] = "member"
			return &ParticipantRow{SceneID: sceneID, CharacterID: characterID, Role: "member"}, OpPromoted, nil
		}
		return &ParticipantRow{SceneID: sceneID, CharacterID: characterID, Role: existing}, OpNoChange, nil
	}
	if scene.Visibility == string(SceneVisibilityPrivate) {
		return nil, OpNoChange, oops.Code("SCENE_JOIN_NOT_INVITED").
			With("scene_id", sceneID).With("character_id", characterID).Errorf("not invited")
	}
	f.participants[sceneID][characterID] = "member"
	return &ParticipantRow{SceneID: sceneID, CharacterID: characterID, Role: "member"}, OpInserted, nil
}

func (f *fakeStore) End(_ context.Context, id string) (*SceneRow, error) {
	row, ok := f.scenes[id]
	if !ok {
		return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
	}
	if row.State != string(SceneStateActive) && row.State != string(SceneStatePaused) {
		return nil, oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", id).
			With("op", "end").
			With("current_state", row.State).
			Errorf("cannot end")
	}
	row.State = string(SceneStateEnded)
	now := time.Now().UTC()
	row.EndedAt = &now
	cp := *row
	return &cp, nil
}

func (f *fakeStore) Pause(_ context.Context, id string) (*SceneRow, error) {
	row, ok := f.scenes[id]
	if !ok {
		return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
	}
	if row.State != string(SceneStateActive) {
		return nil, oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", id).
			With("op", "pause").
			With("current_state", row.State).
			Errorf("cannot pause")
	}
	row.State = string(SceneStatePaused)
	cp := *row
	return &cp, nil
}

func (f *fakeStore) Resume(_ context.Context, id string) (*SceneRow, error) {
	row, ok := f.scenes[id]
	if !ok {
		return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
	}
	if row.State != string(SceneStatePaused) {
		return nil, oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", id).
			With("op", "resume").
			With("current_state", row.State).
			Errorf("cannot resume")
	}
	row.State = string(SceneStateActive)
	cp := *row
	return &cp, nil
}

func (f *fakeStore) Update(_ context.Context, id string, update *SceneUpdate) (*SceneRow, error) {
	row, ok := f.scenes[id]
	if !ok {
		return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
	}
	if update == nil || !update.HasChanges() {
		// No-op: return a copy of the current row, mirroring the real
		// store's "no-op returns current state" contract.
		cp := *row
		return &cp, nil
	}
	if row.State != string(SceneStateActive) && row.State != string(SceneStatePaused) {
		return nil, oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", id).
			With("op", "update").
			With("current_state", row.State).
			Errorf("cannot update")
	}
	if update.Title != nil {
		row.Title = *update.Title
	}
	if update.Description != nil {
		row.Description = *update.Description
	}
	if update.Visibility != nil {
		row.Visibility = *update.Visibility
	}
	if update.PoseOrder != nil {
		row.PoseOrder = *update.PoseOrder
	}
	if update.LocationID != nil {
		if *update.LocationID == "" {
			row.LocationID = nil
		} else {
			loc := *update.LocationID
			row.LocationID = &loc
		}
	}
	if update.UpdateContentWarnings {
		row.ContentWarnings = update.ContentWarnings
	}
	if update.UpdateTags {
		row.Tags = update.Tags
	}
	cp := *row
	return &cp, nil
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

	resp, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-known",
	})
	require.NoError(t, err)
	assert.Equal(t, "scene-known", resp.GetScene().GetId())
	assert.Equal(t, "Existing", resp.GetScene().GetTitle())
}

func TestSceneServiceGetSceneReturnsNotFoundWhenSceneIsMissing(t *testing.T) {
	svc := NewSceneServiceImpl(newFakeStore())

	_, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-missing",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceGetSceneReturnsInternalForUnknownStoreError(t *testing.T) {
	store := newFakeStore()
	store.getErr = errors.New("connection refused")
	svc := NewSceneServiceImpl(store)

	_, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-x",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestSceneServiceEndSceneTransitionsScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:         "scene-1",
		Title:      "Test",
		OwnerID:    "char-alice",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}
	svc := NewSceneServiceImpl(store)

	resp, err := svc.EndScene(context.Background(), &scenev1.EndSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "scene-1", resp.GetScene().GetId())
	assert.Equal(t, string(SceneStateEnded), resp.GetScene().GetState())
}

func TestSceneServiceEndSceneReturnsNotFoundForMissingScene(t *testing.T) {
	svc := NewSceneServiceImpl(newFakeStore())

	_, err := svc.EndScene(context.Background(), &scenev1.EndSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-missing",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceEndSceneReturnsFailedPreconditionForEndedScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-ended"] = &SceneRow{
		ID:    "scene-ended",
		State: string(SceneStateEnded),
	}
	svc := NewSceneServiceImpl(store)

	_, err := svc.EndScene(context.Background(), &scenev1.EndSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-ended",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSceneServicePauseSceneTransitionsScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:         "scene-1",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}
	svc := NewSceneServiceImpl(store)

	resp, err := svc.PauseScene(context.Background(), &scenev1.PauseSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-1",
	})
	require.NoError(t, err)
	assert.Equal(t, string(SceneStatePaused), resp.GetScene().GetState())
}

func TestSceneServicePauseSceneReturnsNotFoundForMissingScene(t *testing.T) {
	svc := NewSceneServiceImpl(newFakeStore())

	_, err := svc.PauseScene(context.Background(), &scenev1.PauseSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-missing",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServicePauseSceneReturnsFailedPreconditionForAlreadyPausedScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-paused"] = &SceneRow{
		ID:    "scene-paused",
		State: string(SceneStatePaused),
	}
	svc := NewSceneServiceImpl(store)

	_, err := svc.PauseScene(context.Background(), &scenev1.PauseSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-paused",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSceneServiceResumeSceneTransitionsScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:         "scene-1",
		State:      string(SceneStatePaused),
		Visibility: string(SceneVisibilityOpen),
	}
	svc := NewSceneServiceImpl(store)

	resp, err := svc.ResumeScene(context.Background(), &scenev1.ResumeSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-1",
	})
	require.NoError(t, err)
	assert.Equal(t, string(SceneStateActive), resp.GetScene().GetState())
}

func TestSceneServiceResumeSceneReturnsNotFoundForMissingScene(t *testing.T) {
	svc := NewSceneServiceImpl(newFakeStore())

	_, err := svc.ResumeScene(context.Background(), &scenev1.ResumeSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-missing",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceResumeSceneReturnsFailedPreconditionForActiveScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-active"] = &SceneRow{
		ID:    "scene-active",
		State: string(SceneStateActive),
	}
	svc := NewSceneServiceImpl(store)

	_, err := svc.ResumeScene(context.Background(), &scenev1.ResumeSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-active",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSceneServiceUpdateSceneAppliesTitleChange(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:         "scene-1",
		Title:      "Original",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}
	svc := NewSceneServiceImpl(store)

	resp, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-1",
		Title:       "Updated",
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Updated", resp.GetScene().GetTitle())
}

func TestSceneServiceUpdateSceneRejectsEndedScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-ended"] = &SceneRow{
		ID:    "scene-ended",
		State: string(SceneStateEnded),
	}
	svc := NewSceneServiceImpl(store)

	_, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-ended",
		Title:       "Try",
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSceneServiceUpdateSceneAppliesContentWarnings(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:              "scene-1",
		Title:           "T",
		State:           string(SceneStateActive),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{"violence"},
	}
	svc := NewSceneServiceImpl(store)

	_, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		CharacterId:     "char-alice",
		SceneId:         "scene-1",
		ContentWarnings: []string{"violence", "death"},
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"content_warnings"}},
	})
	require.NoError(t, err)
	got := store.scenes["scene-1"]
	assert.ElementsMatch(t, []string{"violence", "death"}, got.ContentWarnings)
}

func TestSceneServiceUpdateSceneRejectsEmptyTitleInMask(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:         "scene-1",
		Title:      "Original",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}
	svc := NewSceneServiceImpl(store)

	_, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-1",
		Title:       "   ",
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "title")
}

func TestSceneServiceUpdateSceneRejectsUnknownMaskPath(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:    "scene-1",
		State: string(SceneStateActive),
	}
	svc := NewSceneServiceImpl(store)

	_, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-1",
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"owner_id"}},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "unknown update_mask path")
}

func TestSceneServiceUpdateSceneEmptyMaskIsNoOp(t *testing.T) {
	// Clients commonly send either an omitted UpdateMask (nil) or an
	// explicit empty FieldMask with no paths. Both MUST be treated as a
	// no-op. Table-driven so a future serialization form can be added
	// without duplicating the fixture setup.
	cases := []struct {
		name string
		mask *fieldmaskpb.FieldMask
	}{
		{"nil update_mask is a no-op", nil},
		{"explicit empty update_mask paths is a no-op", &fieldmaskpb.FieldMask{Paths: []string{}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			store.scenes["scene-1"] = &SceneRow{
				ID:         "scene-1",
				Title:      "Unchanged",
				State:      string(SceneStateActive),
				Visibility: string(SceneVisibilityOpen),
			}
			svc := NewSceneServiceImpl(store)

			resp, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
				CharacterId: "char-alice",
				SceneId:     "scene-1",
				UpdateMask:  tc.mask,
			})
			require.NoError(t, err)
			assert.Equal(t, "Unchanged", resp.GetScene().GetTitle())
			// Store row must also be untouched — the no-op path MUST NOT
			// emit any mutation to the fake store.
			assert.Equal(t, "Unchanged", store.scenes["scene-1"].Title)
		})
	}
}

func TestSceneServiceJoinSceneInsertsMemberAndReturnsSuccess(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-js-1", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	svc := NewSceneServiceImpl(store)

	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		CharacterId: "char-bob",
		SceneId:     "scene-js-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "member", store.participants["scene-js-1"]["char-bob"])
}

func TestSceneServiceJoinSceneMapsNotInvitedToPermissionDenied(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-js-priv", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityPrivate),
	}))
	svc := NewSceneServiceImpl(store)

	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		CharacterId: "char-bob", SceneId: "scene-js-priv",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

func TestSceneServiceJoinSceneMapsNotFoundToNotFound(t *testing.T) {
	svc := NewSceneServiceImpl(newFakeStore())
	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		CharacterId: "char-bob", SceneId: "scene-missing",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceJoinSceneMapsTransitionForbiddenToFailedPrecondition(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-js-ended", OwnerID: "char-alice",
		State: string(SceneStateEnded), Visibility: string(SceneVisibilityOpen),
	}))
	svc := NewSceneServiceImpl(store)
	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		CharacterId: "char-bob", SceneId: "scene-js-ended",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}
