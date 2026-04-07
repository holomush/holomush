// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// sceneStorer is the persistence interface required by SceneServiceImpl.
// Defined here so the service layer is not coupled to the concrete
// SceneStore type — tests can substitute a fake implementation.
//
// Phase 1: Create + Get
// Phase 2: + End, Pause, Resume, Update — all return the post-update row
//
//	via Postgres RETURNING so the service handler doesn't need a
//	separate Get call (eliminates a class of races).
type sceneStorer interface {
	Create(ctx context.Context, row *SceneRow) error
	Get(ctx context.Context, id string) (*SceneRow, error)
	End(ctx context.Context, id string) (*SceneRow, error)
	Pause(ctx context.Context, id string) (*SceneRow, error)
	Resume(ctx context.Context, id string) (*SceneRow, error)
}

// SceneServiceImpl implements scenev1.SceneServiceServer for Phase 1.
//
// The store field is wired by main()'s Init via direct field assignment
// after NewSceneStore returns. The pre-allocated zero-value SceneServiceImpl
// is registered with the gRPC server in RegisterServices, before Init is
// called, so the field assignment in Init wires the store after RegisterServices.
type SceneServiceImpl struct {
	scenev1.UnimplementedSceneServiceServer
	store sceneStorer
}

// NewSceneServiceImpl returns a service backed by the given store.
// Used by tests; main() constructs the service directly with a nil store
// and assigns it after Init.
func NewSceneServiceImpl(store sceneStorer) *SceneServiceImpl {
	return &SceneServiceImpl{store: store}
}

// CreateScene generates a new scene ID, persists the scene, and returns it.
// The caller (host) is responsible for ensuring ABAC has authorised the
// command-execute action; per-resource ABAC for the new scene happens at
// the read path.
//
// Per-field validation (character_id non-empty, title min_len: 1, etc.)
// happens via the protovalidate interceptor before this handler runs.
func (s *SceneServiceImpl) CreateScene(ctx context.Context, req *scenev1.CreateSceneRequest) (*scenev1.CreateSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.create_scene",
		attribute.String("subject_id", req.GetCharacterId()),
	)
	defer span.End()

	// Title is trimmed before storage so empty-only-after-trim becomes
	// empty after trimming. The protovalidate annotation rejects empty
	// titles at unmarshal time, but a title of "   " (spaces) passes
	// protovalidate's min_len check and would be stored as a blank
	// title without this trim. Service-level cleanup, not validation.
	title := strings.TrimSpace(req.GetTitle())
	if title == "" {
		recordError(span, errors.New("title cannot be whitespace-only"))
		return nil, status.Errorf(codes.InvalidArgument, "title cannot be whitespace-only")
	}

	id, err := newSceneID()
	if err != nil {
		recordError(span, err)
		return nil, status.Errorf(codes.Internal, "failed to generate scene id: %v", err)
	}
	span.SetAttributes(attribute.String("scene_id", id))

	row := &SceneRow{
		ID:              id,
		Title:           title,
		Description:     req.GetDescription(),
		OwnerID:         req.GetCharacterId(),
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	if loc := req.GetLocationId(); loc != "" {
		row.LocationID = &loc
	}

	if err := s.store.Create(ctx, row); err != nil {
		recordError(span, err)
		slog.WarnContext(ctx, "scene.service.create_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", id,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to create scene: %v", err)
	}

	metricSceneCreated(string(SceneVisibilityOpen), false)
	slog.InfoContext(ctx, "scene.service.create_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", id,
		"title", title,
	)

	return &scenev1.CreateSceneResponse{
		Scene: rowToProto(row, time.Now().UTC()),
	}, nil
}

// GetScene loads a scene by ID and returns it. The host's ABAC engine has
// already evaluated the read-own-scene policy before this RPC is invoked,
// so the service does not perform an additional ownership check.
//
// Per-field validation (scene_id non-empty) happens via the protovalidate
// interceptor before this handler runs.
func (s *SceneServiceImpl) GetScene(ctx context.Context, req *scenev1.GetSceneRequest) (*scenev1.GetSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.get_scene",
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	row, err := s.store.Get(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SCENE_NOT_FOUND" {
			return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
		}
		slog.WarnContext(ctx, "scene.service.get_scene store error",
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to get scene: %v", err)
	}

	slog.InfoContext(ctx, "scene.service.get_scene ok",
		"scene_id", row.ID,
	)

	return &scenev1.GetSceneResponse{
		Scene: rowToProto(row, row.CreatedAt),
	}, nil
}

// newSceneID generates a ULID using crypto/rand for entropy. Per project
// convention, math/rand is forbidden everywhere — see CLAUDE.md.
func newSceneID() (string, error) {
	ms := ulid.Timestamp(time.Now())
	id, err := ulid.New(ms, rand.Reader)
	if err != nil {
		return "", oops.Code("SCENE_ID_GEN_FAILED").Wrap(err)
	}
	return "scene-" + id.String(), nil
}

// rowToProto converts a SceneRow to the proto representation.
//
// createdAt is passed in to allow CreateScene (which has not re-fetched
// from the database) to use the host's wall clock; GetScene passes the
// row's actual CreatedAt.
func rowToProto(row *SceneRow, createdAt time.Time) *scenev1.SceneInfo {
	info := &scenev1.SceneInfo{
		Id:          row.ID,
		Title:       row.Title,
		Description: row.Description,
		OwnerId:     row.OwnerID,
		State:       row.State,
		Visibility:  row.Visibility,
		CreatedAt:   timestamppb.New(createdAt),
	}
	if row.LocationID != nil {
		info.LocationId = *row.LocationID
	}
	return info
}
