// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// ErrPermissionDenied is returned when an operation is not authorized.
var ErrPermissionDenied = errors.New("permission denied")

// AccessControl defines the interface for authorization checks.
// This mirrors internal/access.AccessControl to avoid coupling world to access package.
type AccessControl interface {
	Check(ctx context.Context, subject, action, resource string) bool
}

// ServiceConfig holds dependencies for WorldService.
type ServiceConfig struct {
	LocationRepo  LocationRepository
	ExitRepo      ExitRepository
	ObjectRepo    ObjectRepository
	SceneRepo     SceneRepository
	CharacterRepo CharacterRepository
	AccessControl AccessControl
	EventEmitter  EventEmitter
}

// Service provides authorized access to world model operations.
// All operations check authorization before delegating to repositories.
type Service struct {
	locationRepo  LocationRepository
	exitRepo      ExitRepository
	objectRepo    ObjectRepository
	sceneRepo     SceneRepository
	characterRepo CharacterRepository
	accessControl AccessControl
	eventEmitter  EventEmitter
}

// NewService creates a new Service with the given configuration.
// Panics if AccessControl is nil, as it is required for all operations.
func NewService(cfg ServiceConfig) *Service {
	if cfg.AccessControl == nil {
		panic("world.NewService: AccessControl is required")
	}
	return &Service{
		locationRepo:  cfg.LocationRepo,
		exitRepo:      cfg.ExitRepo,
		objectRepo:    cfg.ObjectRepo,
		sceneRepo:     cfg.SceneRepo,
		characterRepo: cfg.CharacterRepo,
		accessControl: cfg.AccessControl,
		eventEmitter:  cfg.EventEmitter,
	}
}

// GetLocation retrieves a location by ID after checking read authorization.
func (s *Service) GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*Location, error) {
	if s.locationRepo == nil {
		return nil, oops.Code("LOCATION_GET_FAILED").Errorf("location repository not configured")
	}
	resource := fmt.Sprintf("location:%s", id.String())
	if !s.accessControl.Check(ctx, subjectID, "read", resource) {
		return nil, oops.Code("LOCATION_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	loc, err := s.locationRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, oops.Code("LOCATION_NOT_FOUND").Wrapf(err, "get location %s", id)
		}
		return nil, oops.Code("LOCATION_GET_FAILED").Wrapf(err, "get location %s", id)
	}
	return loc, nil
}

// CreateLocation creates a new location after checking write authorization.
// The location ID is generated if not set.
// Returns a ValidationError if the name or description is invalid.
func (s *Service) CreateLocation(ctx context.Context, subjectID string, loc *Location) error {
	if s.locationRepo == nil {
		return oops.Code("LOCATION_CREATE_FAILED").Errorf("location repository not configured")
	}
	if !s.accessControl.Check(ctx, subjectID, "write", "location:*") {
		return oops.Code("LOCATION_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	if loc == nil {
		return oops.Code("LOCATION_INVALID").Errorf("location is nil")
	}
	if err := loc.Validate(); err != nil {
		return oops.Code("LOCATION_INVALID").Wrap(err)
	}
	if loc.ID.IsZero() {
		loc.ID = ulid.Make()
	}
	if err := s.locationRepo.Create(ctx, loc); err != nil {
		return oops.Code("LOCATION_CREATE_FAILED").Wrapf(err, "create location %s", loc.ID)
	}
	return nil
}

// UpdateLocation updates an existing location after checking write authorization.
// Returns a ValidationError if the name or description is invalid.
func (s *Service) UpdateLocation(ctx context.Context, subjectID string, loc *Location) error {
	if s.locationRepo == nil {
		return oops.Code("LOCATION_UPDATE_FAILED").Errorf("location repository not configured")
	}
	if loc == nil {
		return oops.Code("LOCATION_INVALID").Errorf("location is nil")
	}
	resource := fmt.Sprintf("location:%s", loc.ID.String())
	if !s.accessControl.Check(ctx, subjectID, "write", resource) {
		return oops.Code("LOCATION_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	if err := loc.Validate(); err != nil {
		return oops.Code("LOCATION_INVALID").Wrap(err)
	}
	if err := s.locationRepo.Update(ctx, loc); err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("LOCATION_NOT_FOUND").Wrapf(err, "update location %s", loc.ID)
		}
		return oops.Code("LOCATION_UPDATE_FAILED").Wrapf(err, "update location %s", loc.ID)
	}
	return nil
}

// DeleteLocation deletes a location after checking delete authorization.
func (s *Service) DeleteLocation(ctx context.Context, subjectID string, id ulid.ULID) error {
	if s.locationRepo == nil {
		return oops.Code("LOCATION_DELETE_FAILED").Errorf("location repository not configured")
	}
	resource := fmt.Sprintf("location:%s", id.String())
	if !s.accessControl.Check(ctx, subjectID, "delete", resource) {
		return oops.Code("LOCATION_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	if err := s.locationRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("LOCATION_NOT_FOUND").Wrapf(err, "delete location %s", id)
		}
		return oops.Code("LOCATION_DELETE_FAILED").Wrapf(err, "delete location %s", id)
	}
	return nil
}

// GetExit retrieves an exit by ID after checking read authorization.
func (s *Service) GetExit(ctx context.Context, subjectID string, id ulid.ULID) (*Exit, error) {
	if s.exitRepo == nil {
		return nil, oops.Code("EXIT_GET_FAILED").Errorf("exit repository not configured")
	}
	resource := fmt.Sprintf("exit:%s", id.String())
	if !s.accessControl.Check(ctx, subjectID, "read", resource) {
		return nil, oops.Code("EXIT_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	exit, err := s.exitRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, oops.Code("EXIT_NOT_FOUND").Wrapf(err, "get exit %s", id)
		}
		return nil, oops.Code("EXIT_GET_FAILED").Wrapf(err, "get exit %s", id)
	}
	return exit, nil
}

// CreateExit creates a new exit after checking write authorization.
// The exit ID is generated if not set.
// Returns a ValidationError if the name, aliases, visibility, lock type, lock data, or visible_to are invalid.
func (s *Service) CreateExit(ctx context.Context, subjectID string, exit *Exit) error {
	if s.exitRepo == nil {
		return oops.Code("EXIT_CREATE_FAILED").Errorf("exit repository not configured")
	}
	if !s.accessControl.Check(ctx, subjectID, "write", "exit:*") {
		return oops.Code("EXIT_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	if exit == nil {
		return oops.Code("EXIT_INVALID").Errorf("exit is nil")
	}
	if err := exit.Validate(); err != nil {
		return oops.Code("EXIT_INVALID").Wrap(err)
	}
	if exit.ID.IsZero() {
		exit.ID = ulid.Make()
	}
	if err := s.exitRepo.Create(ctx, exit); err != nil {
		return oops.Code("EXIT_CREATE_FAILED").Wrapf(err, "create exit %s", exit.ID)
	}
	return nil
}

// UpdateExit updates an existing exit after checking write authorization.
// Returns a ValidationError if the name, aliases, visibility, lock type, lock data, or visible_to are invalid.
func (s *Service) UpdateExit(ctx context.Context, subjectID string, exit *Exit) error {
	if s.exitRepo == nil {
		return oops.Code("EXIT_UPDATE_FAILED").Errorf("exit repository not configured")
	}
	if exit == nil {
		return oops.Code("EXIT_INVALID").Errorf("exit is nil")
	}
	resource := fmt.Sprintf("exit:%s", exit.ID.String())
	if !s.accessControl.Check(ctx, subjectID, "write", resource) {
		return oops.Code("EXIT_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	if err := exit.Validate(); err != nil {
		return oops.Code("EXIT_INVALID").Wrap(err)
	}
	if err := s.exitRepo.Update(ctx, exit); err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("EXIT_NOT_FOUND").Wrapf(err, "update exit %s", exit.ID)
		}
		return oops.Code("EXIT_UPDATE_FAILED").Wrapf(err, "update exit %s", exit.ID)
	}
	return nil
}

// DeleteExit deletes an exit after checking delete authorization.
// For bidirectional exits, the return exit is deleted atomically.
// Non-severe cleanup issues (return not found) are logged but don't fail the operation.
// Severe cleanup issues (find/delete errors) cause a full rollback - the operation fails.
func (s *Service) DeleteExit(ctx context.Context, subjectID string, id ulid.ULID) error {
	if s.exitRepo == nil {
		return oops.Code("EXIT_DELETE_FAILED").Errorf("exit repository not configured")
	}
	resource := fmt.Sprintf("exit:%s", id.String())
	if !s.accessControl.Check(ctx, subjectID, "delete", resource) {
		return oops.Code("EXIT_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	err := s.exitRepo.Delete(ctx, id)
	if err != nil {
		// Check if this is a cleanup result from bidirectional exit handling
		var cleanupResult *BidirectionalCleanupResult
		if errors.As(err, &cleanupResult) {
			// Log cleanup issues at appropriate level
			if cleanupResult.IsSevere() {
				// Severe: operation was rolled back, primary delete did NOT complete
				slog.Error("bidirectional exit delete rolled back",
					"exit_id", cleanupResult.ExitID.String(),
					"error", cleanupResult.Error())
				return oops.Code("EXIT_DELETE_FAILED").Wrapf(err, "delete exit %s", id)
			}
			// Non-severe: primary delete succeeded, return exit was just not found
			slog.Info("bidirectional exit cleanup notice: return exit already deleted",
				"exit_id", cleanupResult.ExitID.String(),
				"to_location_id", cleanupResult.ToLocationID.String(),
				"return_name", cleanupResult.ReturnName)
			return nil
		}
		// Actual delete failure
		if errors.Is(err, ErrNotFound) {
			return oops.Code("EXIT_NOT_FOUND").Wrapf(err, "delete exit %s", id)
		}
		return oops.Code("EXIT_DELETE_FAILED").Wrapf(err, "delete exit %s", id)
	}
	return nil
}

// GetObject retrieves an object by ID after checking read authorization.
func (s *Service) GetObject(ctx context.Context, subjectID string, id ulid.ULID) (*Object, error) {
	if s.objectRepo == nil {
		return nil, oops.Code("OBJECT_GET_FAILED").Errorf("object repository not configured")
	}
	resource := fmt.Sprintf("object:%s", id.String())
	if !s.accessControl.Check(ctx, subjectID, "read", resource) {
		return nil, oops.Code("OBJECT_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	obj, err := s.objectRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, oops.Code("OBJECT_NOT_FOUND").Wrapf(err, "get object %s", id)
		}
		return nil, oops.Code("OBJECT_GET_FAILED").Wrapf(err, "get object %s", id)
	}
	return obj, nil
}

// CreateObject creates a new object after checking write authorization.
// The object ID is generated if not set.
// Returns a ValidationError if the name or description is invalid.
func (s *Service) CreateObject(ctx context.Context, subjectID string, obj *Object) error {
	if s.objectRepo == nil {
		return oops.Code("OBJECT_CREATE_FAILED").Errorf("object repository not configured")
	}
	if !s.accessControl.Check(ctx, subjectID, "write", "object:*") {
		return oops.Code("OBJECT_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	if obj == nil {
		return oops.Code("OBJECT_INVALID").Errorf("object is nil")
	}
	if err := obj.Validate(); err != nil {
		return oops.Code("OBJECT_INVALID").Wrap(err)
	}
	if err := obj.ValidateContainment(); err != nil {
		return oops.Code("OBJECT_INVALID").Wrap(err)
	}
	if obj.ID.IsZero() {
		obj.ID = ulid.Make()
	}
	if err := s.objectRepo.Create(ctx, obj); err != nil {
		return oops.Code("OBJECT_CREATE_FAILED").Wrapf(err, "create object %s", obj.ID)
	}
	return nil
}

// UpdateObject updates an existing object after checking write authorization.
// Returns a ValidationError if the name or description is invalid.
func (s *Service) UpdateObject(ctx context.Context, subjectID string, obj *Object) error {
	if s.objectRepo == nil {
		return oops.Code("OBJECT_UPDATE_FAILED").Errorf("object repository not configured")
	}
	if obj == nil {
		return oops.Code("OBJECT_INVALID").Errorf("object is nil")
	}
	resource := fmt.Sprintf("object:%s", obj.ID.String())
	if !s.accessControl.Check(ctx, subjectID, "write", resource) {
		return oops.Code("OBJECT_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	if err := obj.Validate(); err != nil {
		return oops.Code("OBJECT_INVALID").Wrap(err)
	}
	if err := obj.ValidateContainment(); err != nil {
		return oops.Code("OBJECT_INVALID").Wrap(err)
	}
	if err := s.objectRepo.Update(ctx, obj); err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("OBJECT_NOT_FOUND").Wrapf(err, "update object %s", obj.ID)
		}
		return oops.Code("OBJECT_UPDATE_FAILED").Wrapf(err, "update object %s", obj.ID)
	}
	return nil
}

// DeleteObject deletes an object after checking delete authorization.
func (s *Service) DeleteObject(ctx context.Context, subjectID string, id ulid.ULID) error {
	if s.objectRepo == nil {
		return oops.Code("OBJECT_DELETE_FAILED").Errorf("object repository not configured")
	}
	resource := fmt.Sprintf("object:%s", id.String())
	if !s.accessControl.Check(ctx, subjectID, "delete", resource) {
		return oops.Code("OBJECT_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	if err := s.objectRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("OBJECT_NOT_FOUND").Wrapf(err, "delete object %s", id)
		}
		return oops.Code("OBJECT_DELETE_FAILED").Wrapf(err, "delete object %s", id)
	}
	return nil
}

// MoveObject moves an object to a new containment after checking write authorization.
// Returns ErrInvalidContainment if the target containment is invalid.
func (s *Service) MoveObject(ctx context.Context, subjectID string, id ulid.ULID, to Containment) error {
	if s.objectRepo == nil {
		return oops.Code("OBJECT_MOVE_FAILED").Errorf("object repository not configured")
	}
	resource := fmt.Sprintf("object:%s", id.String())
	if !s.accessControl.Check(ctx, subjectID, "write", resource) {
		return oops.Code("OBJECT_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	if err := to.Validate(); err != nil {
		return oops.Code("OBJECT_INVALID").Wrap(err)
	}

	// Get current containment for the move event
	obj, err := s.objectRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("OBJECT_NOT_FOUND").Wrapf(err, "move object %s", id)
		}
		return oops.Code("OBJECT_MOVE_FAILED").Wrapf(err, "get object %s", id)
	}
	from := obj.Containment()

	if err := s.objectRepo.Move(ctx, id, to); err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("OBJECT_NOT_FOUND").Wrapf(err, "move object %s", id)
		}
		return oops.Code("OBJECT_MOVE_FAILED").Wrapf(err, "move object %s", id)
	}

	// Emit move event (non-blocking - failures are logged but don't fail the operation)
	payload := MovePayload{
		EntityType: EntityTypeObject,
		EntityID:   id,
		FromType:   from.Type(),
		FromID:     from.ID(), // Can be nil for first-time placements
		ToType:     to.Type(),
		ToID:       *to.ID(), // Safe: to.Validate() ensures one field is set
	}
	if err := EmitMoveEvent(ctx, s.eventEmitter, payload); err != nil {
		slog.Warn("failed to emit move event",
			"object_id", id.String(),
			"from_type", from.Type(),
			"to_type", to.Type(),
			"error", err)
	}

	return nil
}

// AddSceneParticipant adds a character to a scene after checking write authorization.
// Returns ErrInvalidParticipantRole if the role is not valid.
func (s *Service) AddSceneParticipant(ctx context.Context, subjectID string, sceneID, characterID ulid.ULID, role ParticipantRole) error {
	if s.sceneRepo == nil {
		return oops.Code("SCENE_ADD_PARTICIPANT_FAILED").Errorf("scene repository not configured")
	}
	resource := fmt.Sprintf("scene:%s", sceneID.String())
	if !s.accessControl.Check(ctx, subjectID, "write", resource) {
		return oops.Code("SCENE_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	if err := role.Validate(); err != nil {
		return oops.Code("SCENE_INVALID").Wrap(err)
	}
	if err := s.sceneRepo.AddParticipant(ctx, sceneID, characterID, role); err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("SCENE_NOT_FOUND").Wrapf(err, "add participant %s to scene %s", characterID, sceneID)
		}
		return oops.Code("SCENE_ADD_PARTICIPANT_FAILED").Wrapf(err, "add participant %s to scene %s", characterID, sceneID)
	}
	return nil
}

// RemoveSceneParticipant removes a character from a scene after checking write authorization.
func (s *Service) RemoveSceneParticipant(ctx context.Context, subjectID string, sceneID, characterID ulid.ULID) error {
	if s.sceneRepo == nil {
		return oops.Code("SCENE_REMOVE_PARTICIPANT_FAILED").Errorf("scene repository not configured")
	}
	resource := fmt.Sprintf("scene:%s", sceneID.String())
	if !s.accessControl.Check(ctx, subjectID, "write", resource) {
		return oops.Code("SCENE_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	if err := s.sceneRepo.RemoveParticipant(ctx, sceneID, characterID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("SCENE_NOT_FOUND").Wrapf(err, "remove participant %s from scene %s", characterID, sceneID)
		}
		return oops.Code("SCENE_REMOVE_PARTICIPANT_FAILED").Wrapf(err, "remove participant %s from scene %s", characterID, sceneID)
	}
	return nil
}

// ListSceneParticipants lists all participants in a scene after checking read authorization.
func (s *Service) ListSceneParticipants(ctx context.Context, subjectID string, sceneID ulid.ULID) ([]SceneParticipant, error) {
	if s.sceneRepo == nil {
		return nil, oops.Code("SCENE_LIST_PARTICIPANTS_FAILED").Errorf("scene repository not configured")
	}
	resource := fmt.Sprintf("scene:%s", sceneID.String())
	if !s.accessControl.Check(ctx, subjectID, "read", resource) {
		return nil, oops.Code("SCENE_ACCESS_DENIED").Wrap(ErrPermissionDenied)
	}
	participants, err := s.sceneRepo.ListParticipants(ctx, sceneID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, oops.Code("SCENE_NOT_FOUND").Wrapf(err, "list participants for scene %s", sceneID)
		}
		return nil, oops.Code("SCENE_LIST_PARTICIPANTS_FAILED").Wrapf(err, "list participants for scene %s", sceneID)
	}
	return participants, nil
}

