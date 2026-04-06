// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// --- Proto conversion tests ---

func TestSceneRowToProtoConvertsAllFields(t *testing.T) {
	loc := "loc-123"
	ended := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	created := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)

	row := &SceneRow{
		ID:              "scene-abc",
		Title:           "Test Scene",
		Description:     "A test description",
		LocationID:      &loc,
		OwnerID:         "owner-1",
		State:           "active",
		PoseOrder:       "round-robin",
		Visibility:      "open",
		ContentWarnings: []string{"violence"},
		Tags:            []string{"action", "drama"},
		CreatedAt:       created,
		EndedAt:         &ended,
	}

	participants := []*ParticipantRow{
		{
			SceneID:     "scene-abc",
			CharacterID: "char-1",
			Role:        "owner",
			JoinedAt:    created,
		},
		{
			SceneID:     "scene-abc",
			CharacterID: "char-2",
			Role:        "member",
			JoinedAt:    created.Add(time.Minute),
		},
	}

	info := sceneRowToProto(row, participants)

	assert.Equal(t, "scene-abc", info.Id)
	assert.Equal(t, "Test Scene", info.Title)
	assert.Equal(t, "A test description", info.Description)
	assert.Equal(t, "loc-123", info.LocationId)
	assert.Equal(t, "owner-1", info.OwnerId)
	assert.Equal(t, "active", info.State)
	assert.Equal(t, "round-robin", info.PoseOrderMode)
	assert.Equal(t, "open", info.Visibility)
	assert.Equal(t, []string{"violence"}, info.ContentWarnings)
	assert.Equal(t, []string{"action", "drama"}, info.Tags)
	assert.Equal(t, timestamppb.New(created), info.CreatedAt)
	assert.Equal(t, timestamppb.New(ended), info.EndedAt)
	require.Len(t, info.Participants, 2)
	assert.Equal(t, "char-1", info.Participants[0].CharacterId)
	assert.Equal(t, "owner", info.Participants[0].Role)
	assert.Equal(t, "char-2", info.Participants[1].CharacterId)
	assert.Equal(t, "member", info.Participants[1].Role)
}

func TestSceneRowToProtoOmitsOptionalFieldsWhenNil(t *testing.T) {
	row := &SceneRow{
		ID:        "scene-min",
		Title:     "Minimal",
		OwnerID:   "owner-1",
		State:     "active",
		PoseOrder: "free",
		CreatedAt: time.Now().UTC(),
	}

	info := sceneRowToProto(row, nil)

	assert.Equal(t, "scene-min", info.Id)
	assert.Equal(t, "", info.LocationId)
	assert.Nil(t, info.EndedAt)
	assert.Empty(t, info.Participants)
}

func TestParticipantRowToProtoConvertsFields(t *testing.T) {
	joined := time.Date(2026, 4, 5, 11, 0, 0, 0, time.UTC)
	row := &ParticipantRow{
		SceneID:     "scene-1",
		CharacterID: "char-42",
		Role:        "invited",
		JoinedAt:    joined,
	}

	info := participantRowToProto(row)

	assert.Equal(t, "char-42", info.CharacterId)
	assert.Equal(t, "invited", info.Role)
	assert.Equal(t, timestamppb.New(joined), info.JoinedAt)
}

// --- nilIfEmpty tests ---

func TestNilIfEmptyReturnsNilForEmptyString(t *testing.T) {
	assert.Nil(t, nilIfEmpty(""))
}

func TestNilIfEmptyReturnsPointerForNonEmptyString(t *testing.T) {
	result := nilIfEmpty("hello")
	require.NotNil(t, result)
	assert.Equal(t, "hello", *result)
}

// --- mapStoreError tests ---

func TestMapStoreErrorReturnsNilForNilError(t *testing.T) {
	assert.NoError(t, mapStoreError(nil, "test"))
}

// --- Validation tests (nil store — only validation paths hit) ---

func TestCreateSceneRejectsEmptyCharacterID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.CreateScene(t.Context(), &scenev1.CreateSceneRequest{})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "character_id")
}

func TestCreateSceneRejectsEmptyTitle(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.CreateScene(t.Context(), &scenev1.CreateSceneRequest{
		CharacterId: "char-1",
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "title")
}

func TestGetSceneRejectsEmptySceneID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.GetScene(t.Context(), &scenev1.GetSceneRequest{})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "scene_id")
}

func TestEndSceneRejectsEmptyCharacterID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.EndScene(t.Context(), &scenev1.EndSceneRequest{})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "character_id")
}

func TestEndSceneRejectsEmptySceneID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.EndScene(t.Context(), &scenev1.EndSceneRequest{
		CharacterId: "char-1",
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "scene_id")
}

func TestJoinSceneRejectsEmptyCharacterID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.JoinScene(t.Context(), &scenev1.JoinSceneRequest{})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "character_id")
}

func TestJoinSceneRejectsEmptySceneID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.JoinScene(t.Context(), &scenev1.JoinSceneRequest{
		CharacterId: "char-1",
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "scene_id")
}

func TestLeaveSceneRejectsEmptyCharacterID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.LeaveScene(t.Context(), &scenev1.LeaveSceneRequest{})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "character_id")
}

func TestLeaveSceneRejectsEmptySceneID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.LeaveScene(t.Context(), &scenev1.LeaveSceneRequest{
		CharacterId: "char-1",
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "scene_id")
}

func TestInviteToSceneRejectsEmptyCharacterID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.InviteToScene(t.Context(), &scenev1.InviteToSceneRequest{})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "character_id")
}

func TestInviteToSceneRejectsEmptySceneID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.InviteToScene(t.Context(), &scenev1.InviteToSceneRequest{
		CharacterId: "char-1",
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "scene_id")
}

func TestInviteToSceneRejectsEmptyTargetCharacterID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.InviteToScene(t.Context(), &scenev1.InviteToSceneRequest{
		CharacterId: "char-1",
		SceneId:     "scene-1",
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "target_character_id")
}

func TestCastPublishVoteRejectsEmptyCharacterID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.CastPublishVote(t.Context(), &scenev1.CastPublishVoteRequest{})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "character_id")
}

func TestCastPublishVoteRejectsEmptySceneID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.CastPublishVote(t.Context(), &scenev1.CastPublishVoteRequest{
		CharacterId: "char-1",
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "scene_id")
}

func TestGetPoseOrderRejectsEmptySceneID(t *testing.T) {
	svc := NewSceneServiceImpl(nil)
	_, err := svc.GetPoseOrder(t.Context(), &scenev1.GetPoseOrderRequest{})
	requireGRPCCode(t, err, codes.InvalidArgument)
	assert.Contains(t, err.Error(), "scene_id")
}

// --- Compile-time interface check ---

var _ scenev1.SceneServiceServer = (*SceneServiceImpl)(nil)

// --- Test helpers ---

func requireGRPCCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "expected gRPC status error, got: %v", err)
	assert.Equal(t, code, st.Code(), "expected gRPC code %s, got %s: %s", code, st.Code(), st.Message())
}
