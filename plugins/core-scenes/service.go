// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
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

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
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
	CreateWithOwner(ctx context.Context, row *SceneRow) error
	Get(ctx context.Context, id string) (*SceneRow, error)
	GetWithMembership(ctx context.Context, id string) (*SceneRow, []string, []string, error)
	End(ctx context.Context, id string) (*SceneRow, error)
	Pause(ctx context.Context, id string) (*SceneRow, error)
	Resume(ctx context.Context, id string) (*SceneRow, error)
	Update(ctx context.Context, id string, update *SceneUpdate) (*SceneRow, error)
	AddParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, ParticipantOpResult, error)
	RemoveParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, error)
	InviteParticipant(ctx context.Context, sceneID, inviterID, targetID string) (*ParticipantRow, error)
	KickParticipant(ctx context.Context, sceneID, kickerID, targetID string) (*ParticipantRow, error)
	TransferOwnership(ctx context.Context, sceneID, currentOwnerID, newOwnerID string) error
	ListParticipants(ctx context.Context, sceneID string) ([]ParticipantRow, error)
	GetParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, error)
}

// SceneServiceImpl implements scenev1.SceneServiceServer for Phase 1.
//
// The store field is wired by main()'s Init via direct field assignment
// after NewSceneStore returns. The pre-allocated zero-value SceneServiceImpl
// is registered with the gRPC server in RegisterServices, before Init is
// called, so the field assignment in Init wires the store after RegisterServices.
type SceneServiceImpl struct {
	scenev1.UnimplementedSceneServiceServer
	store     sceneStorer
	eventSink pluginsdk.EventSink
}

// NewSceneServiceImpl returns a service backed by the given store.
// Used by tests; main() constructs the service directly with a nil store
// and assigns it after Init.
func NewSceneServiceImpl(store sceneStorer) *SceneServiceImpl {
	return &SceneServiceImpl{store: store}
}

// SetEventSink installs the host callback event sink used for service-owned
// emissions from the binary plugin.
func (s *SceneServiceImpl) SetEventSink(sink pluginsdk.EventSink) {
	s.eventSink = sink
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

	visibility := SceneVisibilityOpen
	if v := req.GetVisibility(); v != "" {
		visibility = SceneVisibility(v)
	}

	row := &SceneRow{
		ID:              id,
		Title:           title,
		Description:     req.GetDescription(),
		OwnerID:         req.GetCharacterId(),
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(visibility),
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	if loc := req.GetLocationId(); loc != "" {
		row.LocationID = &loc
	}
	intent, err := s.sceneCreatedIntent(row)
	if err != nil {
		recordError(span, err)
		slog.WarnContext(ctx, "scene.service.create_scene emit-intent error",
			"subject_id", req.GetCharacterId(),
			"scene_id", id,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to prepare scene event: %v", err)
	}
	if s.eventSink == nil {
		err := oops.Code("SCENE_EVENT_SINK_NOT_CONFIGURED").
			With("scene_id", row.ID).
			New("scene event sink is not configured")
		recordError(span, err)
		slog.WarnContext(ctx, "scene.service.create_scene emit preflight error",
			"subject_id", req.GetCharacterId(),
			"scene_id", id,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to prepare scene event: %v", err)
	}

	if err := s.store.CreateWithOwner(ctx, row); err != nil {
		recordError(span, err)
		slog.WarnContext(ctx, "scene.service.create_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", id,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to create scene: %v", err)
	}

	if err := s.eventSink.Emit(ctx, intent); err != nil {
		err = oops.Code("SCENE_EVENT_EMIT_FAILED").
			With("scene_id", row.ID).
			Wrap(err)
		recordError(span, err)
		// Do not delete persisted scene rows here. CreateWithOwner already
		// appended lifecycle ops events, and those rows are append-only.
		slog.WarnContext(ctx, "scene.service.create_scene emit error",
			"subject_id", req.GetCharacterId(),
			"scene_id", id,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to emit scene event: %v", err)
	}
	metricSceneCreated(string(visibility), false)
	slog.InfoContext(ctx, "scene.service.create_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", id,
		"title", title,
	)

	return &scenev1.CreateSceneResponse{
		Scene: rowToProto(row, time.Now().UTC()),
	}, nil
}

func (s *SceneServiceImpl) sceneCreatedIntent(row *SceneRow) (pluginsdk.EmitIntent, error) {
	if row == nil {
		return pluginsdk.EmitIntent{}, nil
	}

	payload, err := json.Marshal(map[string]string{
		"kind":     "scene.lifecycle.created",
		"scene_id": row.ID,
		"owner_id": row.OwnerID,
		"title":    row.Title,
	})
	if err != nil {
		return pluginsdk.EmitIntent{}, oops.Code("SCENE_EVENT_PAYLOAD_MARSHAL_FAILED").
			With("scene_id", row.ID).
			Wrap(err)
	}

	return pluginsdk.EmitIntent{
		Subject: "scene:" + row.ID,
		Type:    pluginsdk.HostEventTypeSystem,
		Payload: string(payload),
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

// EndScene transitions a scene to the ended state. Only the scene owner is
// authorized (gated by ABAC end-own-scene policy). The transition is
// rejected if the scene is already ended or archived (FailedPrecondition).
//
// The store's End method uses Postgres RETURNING * to atomically return
// the post-update row, so this handler doesn't need a separate Get call.
func (s *SceneServiceImpl) EndScene(ctx context.Context, req *scenev1.EndSceneRequest) (*scenev1.EndSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.lifecycle.end",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	row, err := s.store.End(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		if grpcErr := mapTransitionError(err, req.GetSceneId()); grpcErr != nil {
			return nil, grpcErr
		}
		slog.WarnContext(ctx, "scene.lifecycle.end store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to end scene: %v", err)
	}

	metricSceneStateTransition(string(SceneStateActive)+"_or_paused", "ended", "rpc")
	slog.InfoContext(ctx, "scene.lifecycle.end ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", row.ID,
	)

	return &scenev1.EndSceneResponse{Scene: rowToProto(row, row.CreatedAt)}, nil
}

// PauseScene transitions an active scene to paused. Owner-only.
func (s *SceneServiceImpl) PauseScene(ctx context.Context, req *scenev1.PauseSceneRequest) (*scenev1.PauseSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.lifecycle.pause",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	row, err := s.store.Pause(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		if grpcErr := mapTransitionError(err, req.GetSceneId()); grpcErr != nil {
			return nil, grpcErr
		}
		slog.WarnContext(ctx, "scene.lifecycle.pause store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to pause scene: %v", err)
	}

	metricSceneStateTransition("active", "paused", "rpc")
	slog.InfoContext(ctx, "scene.lifecycle.pause ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", row.ID,
	)

	return &scenev1.PauseSceneResponse{Scene: rowToProto(row, row.CreatedAt)}, nil
}

// ResumeScene transitions a paused scene to active. Phase 2 is owner-only;
// Phase 3 widens to any member per spec D6 (async safety).
func (s *SceneServiceImpl) ResumeScene(ctx context.Context, req *scenev1.ResumeSceneRequest) (*scenev1.ResumeSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.lifecycle.resume",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	row, err := s.store.Resume(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		if grpcErr := mapTransitionError(err, req.GetSceneId()); grpcErr != nil {
			return nil, grpcErr
		}
		slog.WarnContext(ctx, "scene.lifecycle.resume store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to resume scene: %v", err)
	}

	metricSceneStateTransition("paused", "active", "rpc")
	slog.InfoContext(ctx, "scene.lifecycle.resume ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", row.ID,
	)

	return &scenev1.ResumeSceneResponse{Scene: rowToProto(row, row.CreatedAt)}, nil
}

// UpdateScene applies a partial update to mutable scene metadata. Owner-only.
// Rejected for ended/archived scenes. Empty mask updates (no fields specified)
// succeed as no-ops without touching the database.
//
// The update is driven by req.UpdateMask: each path in the mask is a field
// name to apply from the request. Per-field semantic validation (e.g.,
// "title cannot be empty when in the mask") happens in the switch statement
// in buildSceneUpdate; protovalidate constraints in scene.proto handle the
// wire-level max_len / enum-value checks.
func (s *SceneServiceImpl) UpdateScene(ctx context.Context, req *scenev1.UpdateSceneRequest) (*scenev1.UpdateSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.update_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	update, err := buildSceneUpdate(req)
	if err != nil {
		recordError(span, err)
		return nil, err // already a gRPC status error
	}

	row, err := s.store.Update(ctx, req.GetSceneId(), update)
	if err != nil {
		recordError(span, err)
		if grpcErr := mapTransitionError(err, req.GetSceneId()); grpcErr != nil {
			return nil, grpcErr
		}
		slog.WarnContext(ctx, "scene.service.update_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to update scene: %v", err)
	}

	slog.InfoContext(ctx, "scene.service.update_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", row.ID,
	)

	return &scenev1.UpdateSceneResponse{Scene: rowToProto(row, row.CreatedAt)}, nil
}

// buildSceneUpdate iterates the request's FieldMask and constructs a store
// SceneUpdate. Each mask path is matched to the corresponding request field
// AND validated semantically (e.g., title cannot be empty even though the
// proto annotation allows max_len-only).
//
// Returns a gRPC status error directly if validation fails — the caller
// passes it through unchanged.
//
// Unknown mask paths return InvalidArgument so clients can't silently send
// updates that get dropped.
func buildSceneUpdate(req *scenev1.UpdateSceneRequest) (*SceneUpdate, error) {
	update := &SceneUpdate{}
	for _, path := range req.GetUpdateMask().GetPaths() {
		switch path {
		case "title":
			t := strings.TrimSpace(req.GetTitle())
			if t == "" {
				return nil, status.Errorf(codes.InvalidArgument, "title cannot be empty or whitespace-only")
			}
			update.Title = &t
		case "description":
			d := req.GetDescription()
			update.Description = &d
		case "visibility":
			v := req.GetVisibility()
			if v == "" {
				return nil, status.Errorf(codes.InvalidArgument, "visibility cannot be empty when in update_mask")
			}
			update.Visibility = &v
		case "pose_order_mode":
			p := req.GetPoseOrderMode()
			if p == "" {
				return nil, status.Errorf(codes.InvalidArgument, "pose_order_mode cannot be empty when in update_mask")
			}
			update.PoseOrder = &p
		case "location_id":
			l := req.GetLocationId()
			update.LocationID = &l // empty string clears the location
		case "content_warnings":
			update.ContentWarnings = req.GetContentWarnings()
			update.UpdateContentWarnings = true
		case "tags":
			update.Tags = req.GetTags()
			update.UpdateTags = true
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unknown update_mask path: %q", path)
		}
	}
	return update, nil
}

// JoinScene attempts to add the calling character to a scene. The store
// method handles all eligibility checks (open vs private, state, etc.).
//
// Per design decision P3.D5, the operation is idempotent: same-character
// retries return success without polluting the audit log with extra
// membership.join events. The store's ParticipantOpResult enum drives
// the emit-or-not decision inside the store transaction.
func (s *SceneServiceImpl) JoinScene(ctx context.Context, req *scenev1.JoinSceneRequest) (*scenev1.JoinSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.join_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	_, _, err := s.store.AddParticipant(ctx, req.GetSceneId(), req.GetCharacterId())
	if err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) {
			switch oe.Code() {
			case "SCENE_NOT_FOUND":
				return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
			case "SCENE_TRANSITION_FORBIDDEN":
				return nil, status.Errorf(codes.FailedPrecondition,
					"scene cannot be joined in its current state: %v", err)
			case "SCENE_JOIN_NOT_INVITED":
				return nil, status.Errorf(codes.PermissionDenied,
					"character not invited to private scene")
			}
		}
		slog.WarnContext(ctx, "scene.service.join_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to join scene: %v", err)
	}

	slog.InfoContext(ctx, "scene.service.join_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
	)

	return &scenev1.JoinSceneResponse{}, nil
}

// LeaveScene removes the calling character from a scene. Per design decision
// P3.D7, scene owners cannot leave their own scene — they must use scene end
// or transfer ownership first. The service-layer pre-check returns
// FailedPrecondition with an actionable hint message.
//
// The store's RemoveParticipant ALSO has a `WHERE role <> 'owner'` filter
// for defense-in-depth.
func (s *SceneServiceImpl) LeaveScene(ctx context.Context, req *scenev1.LeaveSceneRequest) (*scenev1.LeaveSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.leave_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	// Service-layer owner-leave pre-check. Reads the scene first so we can
	// give the user a helpful message before hitting the store's defensive
	// WHERE filter (which would return SCENE_OWNER_CANNOT_LEAVE — same
	// outcome but the error path is uglier).
	sceneRow, err := s.store.Get(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SCENE_NOT_FOUND" {
			return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
		}
		return nil, status.Errorf(codes.Internal, "failed to load scene: %v", err)
	}
	if sceneRow.OwnerID == req.GetCharacterId() {
		err := status.Errorf(codes.FailedPrecondition,
			"scene owners cannot leave; use `scene end` to terminate the scene or transfer ownership first")
		recordError(span, err)
		return nil, err
	}

	if _, err := s.store.RemoveParticipant(ctx, req.GetSceneId(), req.GetCharacterId()); err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) {
			switch oe.Code() {
			case "SCENE_PARTICIPANT_NOT_FOUND":
				return nil, status.Errorf(codes.NotFound, "character not in scene")
			case "SCENE_OWNER_CANNOT_LEAVE":
				// Defense-in-depth path — should never trigger after the
				// service-layer pre-check above, but mapped for completeness.
				return nil, status.Errorf(codes.FailedPrecondition,
					"scene owners cannot leave")
			}
		}
		slog.WarnContext(ctx, "scene.service.leave_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to leave scene: %v", err)
	}

	slog.InfoContext(ctx, "scene.service.leave_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
	)

	return &scenev1.LeaveSceneResponse{}, nil
}

// InviteToScene adds an 'invited' participant row for the target character.
// ABAC enforces owner-only invite at the dispatcher layer.
func (s *SceneServiceImpl) InviteToScene(ctx context.Context, req *scenev1.InviteToSceneRequest) (*scenev1.InviteToSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.invite_to_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
		attribute.String("target_id", req.GetTargetCharacterId()),
	)
	defer span.End()

	if _, err := s.store.InviteParticipant(ctx, req.GetSceneId(), req.GetCharacterId(), req.GetTargetCharacterId()); err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SCENE_INVITE_TARGET_ALREADY_MEMBER" {
			return nil, status.Errorf(codes.AlreadyExists, "character is already a member of this scene")
		}
		slog.WarnContext(ctx, "scene.service.invite_to_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"target_id", req.GetTargetCharacterId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to invite: %v", err)
	}

	slog.InfoContext(ctx, "scene.service.invite_to_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
		"target_id", req.GetTargetCharacterId(),
	)
	return &scenev1.InviteToSceneResponse{}, nil
}

// KickFromScene removes a target character from a scene. ABAC enforces
// owner-only kick at the dispatcher layer. The store's WHERE filter is
// the defense-in-depth layer that prevents owner removal.
func (s *SceneServiceImpl) KickFromScene(ctx context.Context, req *scenev1.KickFromSceneRequest) (*scenev1.KickFromSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.kick_from_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
		attribute.String("target_id", req.GetTargetCharacterId()),
	)
	defer span.End()

	if _, err := s.store.KickParticipant(ctx, req.GetSceneId(), req.GetCharacterId(), req.GetTargetCharacterId()); err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) {
			switch oe.Code() {
			case "SCENE_PARTICIPANT_NOT_FOUND":
				return nil, status.Errorf(codes.NotFound, "target not in scene")
			case "SCENE_KICK_FORBIDDEN":
				return nil, status.Errorf(codes.FailedPrecondition,
					"scene owner cannot be kicked")
			}
		}
		slog.WarnContext(ctx, "scene.service.kick_from_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"target_id", req.GetTargetCharacterId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to kick: %v", err)
	}

	slog.InfoContext(ctx, "scene.service.kick_from_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
		"target_id", req.GetTargetCharacterId(),
	)
	return &scenev1.KickFromSceneResponse{}, nil
}

// TransferOwnership reassigns ownership of a scene from the calling character
// to a target member. ABAC enforces owner-only transfer at the dispatcher.
// Per design decision P3.D8, the target MUST be an existing member; the
// previous owner becomes a member.
func (s *SceneServiceImpl) TransferOwnership(ctx context.Context, req *scenev1.TransferOwnershipRequest) (*scenev1.TransferOwnershipResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.transfer_ownership",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
		attribute.String("new_owner", req.GetNewOwnerCharacterId()),
	)
	defer span.End()

	if err := s.store.TransferOwnership(ctx, req.GetSceneId(), req.GetCharacterId(), req.GetNewOwnerCharacterId()); err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) {
			switch oe.Code() {
			case "SCENE_NOT_FOUND":
				return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
			case "SCENE_NOT_OWNER":
				return nil, status.Errorf(codes.PermissionDenied,
					"only the scene owner can transfer ownership")
			case "SCENE_TRANSITION_FORBIDDEN":
				return nil, status.Errorf(codes.FailedPrecondition,
					"scene cannot have ownership transferred in its current state")
			case "SCENE_TRANSFER_TARGET_NOT_MEMBER":
				return nil, status.Errorf(codes.FailedPrecondition,
					"transfer target must be an existing member of the scene")
			}
		}
		slog.WarnContext(ctx, "scene.service.transfer_ownership store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"new_owner", req.GetNewOwnerCharacterId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to transfer ownership: %v", err)
	}

	slog.InfoContext(ctx, "scene.service.transfer_ownership ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
		"new_owner", req.GetNewOwnerCharacterId(),
	)
	return &scenev1.TransferOwnershipResponse{}, nil
}

// mapTransitionError translates store-layer transition errors into gRPC
// status errors. Returns nil if the error is not a transition error
// (caller should fall through to a generic Internal status).
func mapTransitionError(err error, sceneID string) error {
	var oe oops.OopsError
	if !errors.As(err, &oe) {
		return nil
	}
	switch oe.Code() {
	case "SCENE_NOT_FOUND":
		return status.Errorf(codes.NotFound, "scene not found: %s", sceneID)
	case "SCENE_TRANSITION_FORBIDDEN":
		return status.Errorf(codes.FailedPrecondition,
			"scene transition forbidden: %v", err)
	}
	return nil
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
		Id:              row.ID,
		Title:           row.Title,
		Description:     row.Description,
		OwnerId:         row.OwnerID,
		State:           row.State,
		Visibility:      row.Visibility,
		PoseOrderMode:   row.PoseOrder,
		ContentWarnings: row.ContentWarnings,
		Tags:            row.Tags,
		CreatedAt:       timestamppb.New(createdAt),
	}
	if row.LocationID != nil {
		info.LocationId = *row.LocationID
	}
	return info
}
