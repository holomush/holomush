// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"time"

	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/idgen"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// SceneServiceImpl implements scenev1.SceneServiceServer backed by SceneStore.
type SceneServiceImpl struct {
	scenev1.UnimplementedSceneServiceServer
	store *SceneStore
}

// NewSceneServiceImpl creates a SceneServiceImpl with the given store.
func NewSceneServiceImpl(store *SceneStore) *SceneServiceImpl {
	return &SceneServiceImpl{store: store}
}

// CreateScene generates a new scene ID, persists it, and adds the owner as
// a participant with role "owner".
func (s *SceneServiceImpl) CreateScene(ctx context.Context, req *scenev1.CreateSceneRequest) (*scenev1.CreateSceneResponse, error) {
	if req.GetSessionId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "session_id is required")
	}
	if req.GetTitle() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "title is required")
	}

	now := time.Now().UTC()
	sceneID := idgen.New().String()

	visibility := req.GetVisibility()
	if visibility == "" {
		visibility = "open"
	}
	poseOrder := req.GetPoseOrderMode()
	if poseOrder == "" {
		poseOrder = "free"
	}

	row := &SceneRow{
		ID:              sceneID,
		Title:           req.GetTitle(),
		Description:     req.GetDescription(),
		LocationID:      nilIfEmpty(req.GetLocationId()),
		OwnerID:         req.GetSessionId(),
		State:           "active",
		PoseOrder:       poseOrder,
		Visibility:      visibility,
		ContentWarnings: req.GetContentWarnings(),
		Tags:            req.GetTags(),
		CreatedAt:       now,
	}

	if err := s.store.CreateScene(ctx, row); err != nil {
		return nil, mapStoreError(err, "create_scene")
	}

	ownerParticipant := &ParticipantRow{
		SceneID:     sceneID,
		CharacterID: req.GetSessionId(),
		Role:        "owner",
		JoinedAt:    now,
	}
	if err := s.store.AddParticipant(ctx, ownerParticipant); err != nil {
		return nil, mapStoreError(err, "add_owner_participant")
	}

	participants, err := s.store.ListParticipants(ctx, sceneID)
	if err != nil {
		return nil, mapStoreError(err, "list_participants")
	}

	return &scenev1.CreateSceneResponse{
		Scene: sceneRowToProto(row, participants),
	}, nil
}

// GetScene retrieves a scene by ID along with its participants.
func (s *SceneServiceImpl) GetScene(ctx context.Context, req *scenev1.GetSceneRequest) (*scenev1.GetSceneResponse, error) {
	if req.GetSceneId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "scene_id is required")
	}

	scene, err := s.store.GetScene(ctx, req.GetSceneId())
	if err != nil {
		return nil, mapStoreError(err, "get_scene")
	}

	participants, err := s.store.ListParticipants(ctx, req.GetSceneId())
	if err != nil {
		return nil, mapStoreError(err, "list_participants")
	}

	return &scenev1.GetSceneResponse{
		Scene: sceneRowToProto(scene, participants),
	}, nil
}

// ListScenes returns scenes filtered by open visibility.
func (s *SceneServiceImpl) ListScenes(ctx context.Context, req *scenev1.ListScenesRequest) (*scenev1.ListScenesResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	offset := int(req.GetOffset())
	if offset < 0 {
		offset = 0
	}

	openVis := "open"
	scenes, err := s.store.ListScenes(ctx, nil, &openVis, limit, offset)
	if err != nil {
		return nil, mapStoreError(err, "list_scenes")
	}

	resp := &scenev1.ListScenesResponse{
		Scenes: make([]*scenev1.SceneInfo, 0, len(scenes)),
	}
	for _, sc := range scenes {
		resp.Scenes = append(resp.Scenes, sceneRowToProto(sc, nil))
	}
	return resp, nil
}

// EndScene transitions a scene from active/paused to ended.
func (s *SceneServiceImpl) EndScene(ctx context.Context, req *scenev1.EndSceneRequest) (*scenev1.EndSceneResponse, error) {
	if req.GetSessionId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "session_id is required")
	}
	if req.GetSceneId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "scene_id is required")
	}

	scene, err := s.store.GetScene(ctx, req.GetSceneId())
	if err != nil {
		return nil, mapStoreError(err, "get_scene")
	}

	if scene.State != "active" && scene.State != "paused" {
		return nil, status.Errorf(codes.FailedPrecondition, "scene must be active or paused to end, current state: %s", scene.State)
	}

	now := time.Now().UTC()
	scene.State = "ended"
	scene.EndedAt = &now

	if err := s.store.UpdateScene(ctx, scene); err != nil {
		return nil, mapStoreError(err, "end_scene")
	}

	return &scenev1.EndSceneResponse{}, nil
}

// JoinScene adds the caller as a member participant of a scene.
func (s *SceneServiceImpl) JoinScene(ctx context.Context, req *scenev1.JoinSceneRequest) (*scenev1.JoinSceneResponse, error) {
	if req.GetSessionId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "session_id is required")
	}
	if req.GetSceneId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "scene_id is required")
	}

	scene, err := s.store.GetScene(ctx, req.GetSceneId())
	if err != nil {
		return nil, mapStoreError(err, "get_scene")
	}

	if scene.State != "active" {
		return nil, status.Errorf(codes.FailedPrecondition, "scene must be active to join, current state: %s", scene.State)
	}
	if scene.Visibility != "open" {
		return nil, status.Errorf(codes.PermissionDenied, "scene is not open for joining")
	}

	participant := &ParticipantRow{
		SceneID:     req.GetSceneId(),
		CharacterID: req.GetSessionId(),
		Role:        "member",
		JoinedAt:    time.Now().UTC(),
	}
	if err := s.store.AddParticipant(ctx, participant); err != nil {
		return nil, mapStoreError(err, "join_scene")
	}

	return &scenev1.JoinSceneResponse{}, nil
}

// LeaveScene removes a participant from a scene.
func (s *SceneServiceImpl) LeaveScene(ctx context.Context, req *scenev1.LeaveSceneRequest) (*scenev1.LeaveSceneResponse, error) {
	if req.GetSessionId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "session_id is required")
	}
	if req.GetSceneId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "scene_id is required")
	}

	if err := s.store.RemoveParticipant(ctx, req.GetSceneId(), req.GetSessionId()); err != nil {
		return nil, mapStoreError(err, "leave_scene")
	}

	return &scenev1.LeaveSceneResponse{}, nil
}

// InviteToScene adds a character as an invited participant.
func (s *SceneServiceImpl) InviteToScene(ctx context.Context, req *scenev1.InviteToSceneRequest) (*scenev1.InviteToSceneResponse, error) {
	if req.GetSessionId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "session_id is required")
	}
	if req.GetSceneId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "scene_id is required")
	}
	if req.GetCharacterId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "character_id is required")
	}

	participant := &ParticipantRow{
		SceneID:     req.GetSceneId(),
		CharacterID: req.GetCharacterId(),
		Role:        "invited",
		JoinedAt:    time.Now().UTC(),
	}
	if err := s.store.AddParticipant(ctx, participant); err != nil {
		return nil, mapStoreError(err, "invite_to_scene")
	}

	return &scenev1.InviteToSceneResponse{}, nil
}

// CastPublishVote records a participant's publish vote for a scene.
func (s *SceneServiceImpl) CastPublishVote(ctx context.Context, req *scenev1.CastPublishVoteRequest) (*scenev1.CastPublishVoteResponse, error) {
	if req.GetSessionId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "session_id is required")
	}
	if req.GetSceneId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "scene_id is required")
	}

	participants, err := s.store.ListParticipants(ctx, req.GetSceneId())
	if err != nil {
		return nil, mapStoreError(err, "list_participants")
	}

	var found *ParticipantRow
	for _, p := range participants {
		if p.CharacterID == req.GetSessionId() {
			found = p
			break
		}
	}
	if found == nil {
		return nil, status.Errorf(codes.NotFound, "participant not found in scene")
	}

	vote := req.GetVote()
	found.PublishVote = &vote
	if err := s.store.AddParticipant(ctx, found); err != nil {
		return nil, mapStoreError(err, "cast_publish_vote")
	}

	return &scenev1.CastPublishVoteResponse{}, nil
}

// GetPoseOrder returns the scene's pose order mode and empty entries.
// Pose order is derived from the event stream (future work).
func (s *SceneServiceImpl) GetPoseOrder(ctx context.Context, req *scenev1.GetPoseOrderRequest) (*scenev1.GetPoseOrderResponse, error) {
	if req.GetSceneId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "scene_id is required")
	}

	scene, err := s.store.GetScene(ctx, req.GetSceneId())
	if err != nil {
		return nil, mapStoreError(err, "get_scene")
	}

	participants, err := s.store.ListParticipants(ctx, req.GetSceneId())
	if err != nil {
		return nil, mapStoreError(err, "list_participants")
	}

	entries := make([]*scenev1.PoseOrderEntry, 0, len(participants))
	for _, p := range participants {
		entries = append(entries, &scenev1.PoseOrderEntry{
			CharacterId: p.CharacterID,
			IsEligible:  true,
		})
	}

	return &scenev1.GetPoseOrderResponse{
		Mode:    scene.PoseOrder,
		Entries: entries,
	}, nil
}

// sceneRowToProto converts a SceneRow and optional participants to a SceneInfo proto.
func sceneRowToProto(scene *SceneRow, participants []*ParticipantRow) *scenev1.SceneInfo {
	info := &scenev1.SceneInfo{
		Id:              scene.ID,
		Title:           scene.Title,
		Description:     scene.Description,
		OwnerId:         scene.OwnerID,
		State:           scene.State,
		PoseOrderMode:   scene.PoseOrder,
		ContentWarnings: scene.ContentWarnings,
		Tags:            scene.Tags,
		Visibility:      scene.Visibility,
		CreatedAt:       timestamppb.New(scene.CreatedAt),
	}
	if scene.LocationID != nil {
		info.LocationId = *scene.LocationID
	}
	if scene.EndedAt != nil {
		info.EndedAt = timestamppb.New(*scene.EndedAt)
	}
	for _, p := range participants {
		info.Participants = append(info.Participants, participantRowToProto(p))
	}
	return info
}

// participantRowToProto converts a ParticipantRow to a ParticipantInfo proto.
func participantRowToProto(p *ParticipantRow) *scenev1.ParticipantInfo {
	return &scenev1.ParticipantInfo{
		CharacterId: p.CharacterID,
		Role:        p.Role,
		JoinedAt:    timestamppb.New(p.JoinedAt),
	}
}

// nilIfEmpty returns nil for empty strings, a pointer to s otherwise.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// mapStoreError converts oops-coded store errors to gRPC status errors.
func mapStoreError(err error, operation string) error {
	if err == nil {
		return nil
	}
	if oopsErr, ok := oops.AsOops(err); ok {
		switch oopsErr.Code() {
		case "SCENE_NOT_FOUND":
			return status.Errorf(codes.NotFound, "scene not found")
		case "SCENE_CREATE_FAILED":
			return status.Errorf(codes.Internal, "failed to create scene")
		case "SCENE_UPDATE_FAILED":
			return status.Errorf(codes.Internal, "failed to update scene")
		case "SCENE_ADD_PARTICIPANT_FAILED":
			return status.Errorf(codes.Internal, "failed to add participant")
		case "SCENE_REMOVE_PARTICIPANT_FAILED":
			return status.Errorf(codes.Internal, "failed to remove participant")
		case "SCENE_LIST_FAILED":
			return status.Errorf(codes.Internal, "failed to list scenes")
		}
	}
	return status.Errorf(codes.Internal, "%s failed: %v", operation, err)
}
