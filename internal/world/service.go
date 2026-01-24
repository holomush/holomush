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
	AccessControl AccessControl
}

// Service provides authorized access to world model operations.
// All operations check authorization before delegating to repositories.
type Service struct {
	locationRepo  LocationRepository
	exitRepo      ExitRepository
	objectRepo    ObjectRepository
	sceneRepo     SceneRepository
	accessControl AccessControl
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
		accessControl: cfg.AccessControl,
	}
}

// GetLocation retrieves a location by ID after checking read authorization.
func (s *Service) GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*Location, error) {
	if s.locationRepo == nil {
		return nil, oops.Errorf("location repository not configured")
	}
	resource := fmt.Sprintf("location:%s", id.String())
	if !s.accessControl.Check(ctx, subjectID, "read", resource) {
		return nil, ErrPermissionDenied
	}
	loc, err := s.locationRepo.Get(ctx, id)
	if err != nil {
		return nil, oops.Wrapf(err, "get location %s", id)
	}
	return loc, nil
}

// CreateLocation creates a new location after checking write authorization.
// The location ID is generated if not set.
// Returns a ValidationError if the name or description is invalid.
func (s *Service) CreateLocation(ctx context.Context, subjectID string, loc *Location) error {
	if s.locationRepo == nil {
		return oops.Errorf("location repository not configured")
	}
	if !s.accessControl.Check(ctx, subjectID, "write", "location:*") {
		return ErrPermissionDenied
	}
	if loc == nil {
		return oops.Errorf("location is nil")
	}
	if err := ValidateName(loc.Name); err != nil {
		return err
	}
	if err := ValidateDescription(loc.Description); err != nil {
		return err
	}
	if err := loc.Type.Validate(); err != nil {
		return err
	}
	if loc.ID.IsZero() {
		loc.ID = ulid.Make()
	}
	if err := s.locationRepo.Create(ctx, loc); err != nil {
		return oops.Wrapf(err, "create location %s", loc.ID)
	}
	return nil
}

// UpdateLocation updates an existing location after checking write authorization.
// Returns a ValidationError if the name or description is invalid.
func (s *Service) UpdateLocation(ctx context.Context, subjectID string, loc *Location) error {
	if s.locationRepo == nil {
		return oops.Errorf("location repository not configured")
	}
	if loc == nil {
		return oops.Errorf("location is nil")
	}
	resource := fmt.Sprintf("location:%s", loc.ID.String())
	if !s.accessControl.Check(ctx, subjectID, "write", resource) {
		return ErrPermissionDenied
	}
	if err := ValidateName(loc.Name); err != nil {
		return err
	}
	if err := ValidateDescription(loc.Description); err != nil {
		return err
	}
	if err := loc.Type.Validate(); err != nil {
		return err
	}
	if err := s.locationRepo.Update(ctx, loc); err != nil {
		return oops.Wrapf(err, "update location %s", loc.ID)
	}
	return nil
}

// DeleteLocation deletes a location after checking delete authorization.
func (s *Service) DeleteLocation(ctx context.Context, subjectID string, id ulid.ULID) error {
	if s.locationRepo == nil {
		return oops.Errorf("location repository not configured")
	}
	resource := fmt.Sprintf("location:%s", id.String())
	if !s.accessControl.Check(ctx, subjectID, "delete", resource) {
		return ErrPermissionDenied
	}
	if err := s.locationRepo.Delete(ctx, id); err != nil {
		return oops.Wrapf(err, "delete location %s", id)
	}
	return nil
}

// GetExit retrieves an exit by ID after checking read authorization.
func (s *Service) GetExit(ctx context.Context, subjectID string, id ulid.ULID) (*Exit, error) {
	if s.exitRepo == nil {
		return nil, oops.Errorf("exit repository not configured")
	}
	resource := fmt.Sprintf("exit:%s", id.String())
	if !s.accessControl.Check(ctx, subjectID, "read", resource) {
		return nil, ErrPermissionDenied
	}
	exit, err := s.exitRepo.Get(ctx, id)
	if err != nil {
		return nil, oops.Wrapf(err, "get exit %s", id)
	}
	return exit, nil
}

// CreateExit creates a new exit after checking write authorization.
// The exit ID is generated if not set.
// Returns a ValidationError if the name, aliases, visibility, lock type, lock data, or visible_to are invalid.
func (s *Service) CreateExit(ctx context.Context, subjectID string, exit *Exit) error {
	if s.exitRepo == nil {
		return oops.Errorf("exit repository not configured")
	}
	if !s.accessControl.Check(ctx, subjectID, "write", "exit:*") {
		return ErrPermissionDenied
	}
	if exit == nil {
		return oops.Errorf("exit is nil")
	}
	if err := ValidateName(exit.Name); err != nil {
		return err
	}
	if err := ValidateAliases(exit.Aliases); err != nil {
		return err
	}
	if err := exit.Visibility.Validate(); err != nil {
		return err
	}
	if exit.Locked {
		if err := exit.LockType.Validate(); err != nil {
			return err
		}
		if err := ValidateLockData(exit.LockData); err != nil {
			return err
		}
	}
	if exit.Visibility == VisibilityList {
		if err := ValidateVisibleTo(exit.VisibleTo); err != nil {
			return err
		}
	}
	if exit.ID.IsZero() {
		exit.ID = ulid.Make()
	}
	if err := s.exitRepo.Create(ctx, exit); err != nil {
		return oops.Wrapf(err, "create exit %s", exit.ID)
	}
	return nil
}

// UpdateExit updates an existing exit after checking write authorization.
// Returns a ValidationError if the name, aliases, visibility, lock type, lock data, or visible_to are invalid.
func (s *Service) UpdateExit(ctx context.Context, subjectID string, exit *Exit) error {
	if s.exitRepo == nil {
		return oops.Errorf("exit repository not configured")
	}
	if exit == nil {
		return oops.Errorf("exit is nil")
	}
	resource := fmt.Sprintf("exit:%s", exit.ID.String())
	if !s.accessControl.Check(ctx, subjectID, "write", resource) {
		return ErrPermissionDenied
	}
	if err := ValidateName(exit.Name); err != nil {
		return err
	}
	if err := ValidateAliases(exit.Aliases); err != nil {
		return err
	}
	if err := exit.Visibility.Validate(); err != nil {
		return err
	}
	if exit.Locked {
		if err := exit.LockType.Validate(); err != nil {
			return err
		}
		if err := ValidateLockData(exit.LockData); err != nil {
			return err
		}
	}
	if exit.Visibility == VisibilityList {
		if err := ValidateVisibleTo(exit.VisibleTo); err != nil {
			return err
		}
	}
	if err := s.exitRepo.Update(ctx, exit); err != nil {
		return oops.Wrapf(err, "update exit %s", exit.ID)
	}
	return nil
}

// DeleteExit deletes an exit after checking delete authorization.
// For bidirectional exits, the return exit is deleted atomically.
// Non-severe cleanup issues (return not found) are logged but don't fail the operation.
// Severe cleanup issues (find/delete errors) cause a full rollback - the operation fails.
func (s *Service) DeleteExit(ctx context.Context, subjectID string, id ulid.ULID) error {
	if s.exitRepo == nil {
		return oops.Errorf("exit repository not configured")
	}
	resource := fmt.Sprintf("exit:%s", id.String())
	if !s.accessControl.Check(ctx, subjectID, "delete", resource) {
		return ErrPermissionDenied
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
				return oops.Wrapf(err, "delete exit %s", id)
			}
			// Non-severe: primary delete succeeded, return exit was just not found
			slog.Debug("bidirectional exit cleanup notice",
				"exit_id", cleanupResult.ExitID.String(),
				"message", cleanupResult.Error())
			return nil
		}
		// Actual delete failure
		return oops.Wrapf(err, "delete exit %s", id)
	}
	return nil
}

// GetObject retrieves an object by ID after checking read authorization.
func (s *Service) GetObject(ctx context.Context, subjectID string, id ulid.ULID) (*Object, error) {
	if s.objectRepo == nil {
		return nil, oops.Errorf("object repository not configured")
	}
	resource := fmt.Sprintf("object:%s", id.String())
	if !s.accessControl.Check(ctx, subjectID, "read", resource) {
		return nil, ErrPermissionDenied
	}
	obj, err := s.objectRepo.Get(ctx, id)
	if err != nil {
		return nil, oops.Wrapf(err, "get object %s", id)
	}
	return obj, nil
}

// CreateObject creates a new object after checking write authorization.
// The object ID is generated if not set.
// Returns a ValidationError if the name or description is invalid.
func (s *Service) CreateObject(ctx context.Context, subjectID string, obj *Object) error {
	if s.objectRepo == nil {
		return oops.Errorf("object repository not configured")
	}
	if !s.accessControl.Check(ctx, subjectID, "write", "object:*") {
		return ErrPermissionDenied
	}
	if obj == nil {
		return oops.Errorf("object is nil")
	}
	if err := ValidateName(obj.Name); err != nil {
		return err
	}
	if err := ValidateDescription(obj.Description); err != nil {
		return err
	}
	if obj.ID.IsZero() {
		obj.ID = ulid.Make()
	}
	if err := s.objectRepo.Create(ctx, obj); err != nil {
		return oops.Wrapf(err, "create object %s", obj.ID)
	}
	return nil
}

// UpdateObject updates an existing object after checking write authorization.
// Returns a ValidationError if the name or description is invalid.
func (s *Service) UpdateObject(ctx context.Context, subjectID string, obj *Object) error {
	if s.objectRepo == nil {
		return oops.Errorf("object repository not configured")
	}
	if obj == nil {
		return oops.Errorf("object is nil")
	}
	resource := fmt.Sprintf("object:%s", obj.ID.String())
	if !s.accessControl.Check(ctx, subjectID, "write", resource) {
		return ErrPermissionDenied
	}
	if err := ValidateName(obj.Name); err != nil {
		return err
	}
	if err := ValidateDescription(obj.Description); err != nil {
		return err
	}
	if err := s.objectRepo.Update(ctx, obj); err != nil {
		return oops.Wrapf(err, "update object %s", obj.ID)
	}
	return nil
}

// DeleteObject deletes an object after checking delete authorization.
func (s *Service) DeleteObject(ctx context.Context, subjectID string, id ulid.ULID) error {
	if s.objectRepo == nil {
		return oops.Errorf("object repository not configured")
	}
	resource := fmt.Sprintf("object:%s", id.String())
	if !s.accessControl.Check(ctx, subjectID, "delete", resource) {
		return ErrPermissionDenied
	}
	if err := s.objectRepo.Delete(ctx, id); err != nil {
		return oops.Wrapf(err, "delete object %s", id)
	}
	return nil
}

// AddSceneParticipant adds a character to a scene after checking write authorization.
// Returns ErrInvalidParticipantRole if the role is not valid.
func (s *Service) AddSceneParticipant(ctx context.Context, subjectID string, sceneID, characterID ulid.ULID, role ParticipantRole) error {
	if s.sceneRepo == nil {
		return oops.Errorf("scene repository not configured")
	}
	resource := fmt.Sprintf("scene:%s", sceneID.String())
	if !s.accessControl.Check(ctx, subjectID, "write", resource) {
		return ErrPermissionDenied
	}
	if err := role.Validate(); err != nil {
		return err
	}
	if err := s.sceneRepo.AddParticipant(ctx, sceneID, characterID, role); err != nil {
		return oops.Wrapf(err, "add participant %s to scene %s", characterID, sceneID)
	}
	return nil
}

// RemoveSceneParticipant removes a character from a scene after checking write authorization.
func (s *Service) RemoveSceneParticipant(ctx context.Context, subjectID string, sceneID, characterID ulid.ULID) error {
	if s.sceneRepo == nil {
		return oops.Errorf("scene repository not configured")
	}
	resource := fmt.Sprintf("scene:%s", sceneID.String())
	if !s.accessControl.Check(ctx, subjectID, "write", resource) {
		return ErrPermissionDenied
	}
	if err := s.sceneRepo.RemoveParticipant(ctx, sceneID, characterID); err != nil {
		return oops.Wrapf(err, "remove participant %s from scene %s", characterID, sceneID)
	}
	return nil
}

// ListSceneParticipants lists all participants in a scene after checking read authorization.
func (s *Service) ListSceneParticipants(ctx context.Context, subjectID string, sceneID ulid.ULID) ([]SceneParticipant, error) {
	if s.sceneRepo == nil {
		return nil, oops.Errorf("scene repository not configured")
	}
	resource := fmt.Sprintf("scene:%s", sceneID.String())
	if !s.accessControl.Check(ctx, subjectID, "read", resource) {
		return nil, ErrPermissionDenied
	}
	participants, err := s.sceneRepo.ListParticipants(ctx, sceneID)
	if err != nil {
		return nil, oops.Wrapf(err, "list participants for scene %s", sceneID)
	}
	return participants, nil
}
