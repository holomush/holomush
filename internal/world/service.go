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

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/observability"
)

// ErrPermissionDenied is returned when an operation is not authorized.
var ErrPermissionDenied = errors.New("permission denied")

// ErrAccessEvaluationFailed is returned when the policy engine fails to evaluate a request.
// Callers can use errors.Is to distinguish engine failures from policy denials.
var ErrAccessEvaluationFailed = errors.New("access evaluation failed")

// ServiceConfig holds dependencies for WorldService.
type ServiceConfig struct {
	LocationRepo  LocationRepository
	ExitRepo      ExitRepository
	ObjectRepo    ObjectRepository
	SceneRepo     SceneRepository
	CharacterRepo CharacterRepository
	PropertyRepo  PropertyRepository
	Engine        types.AccessPolicyEngine
	EventEmitter  EventEmitter
	Transactor    Transactor
}

// Service provides authorized access to world model operations.
// All operations check authorization before delegating to repositories.
type Service struct {
	locationRepo  LocationRepository
	exitRepo      ExitRepository
	objectRepo    ObjectRepository
	sceneRepo     SceneRepository
	characterRepo CharacterRepository
	propertyRepo  PropertyRepository
	engine        types.AccessPolicyEngine
	eventEmitter  EventEmitter
	transactor    Transactor
}

// NewService creates a new Service with the given configuration.
// Panics if Engine is nil, as it is required for all operations.
func NewService(cfg ServiceConfig) *Service {
	if cfg.Engine == nil {
		panic("world.NewService: Engine is required")
	}
	if cfg.EventEmitter == nil {
		slog.Warn("world.NewService: EventEmitter not configured, operations requiring event emission will fail")
	}
	if cfg.PropertyRepo == nil || cfg.Transactor == nil {
		slog.Warn("world.NewService: PropertyRepo and Transactor not configured, delete operations will fail (spec: 05-storage-audit.md §108-119 requires transactional cascade)")
	}
	return &Service{
		locationRepo:  cfg.LocationRepo,
		exitRepo:      cfg.ExitRepo,
		objectRepo:    cfg.ObjectRepo,
		sceneRepo:     cfg.SceneRepo,
		characterRepo: cfg.CharacterRepo,
		propertyRepo:  cfg.PropertyRepo,
		engine:        cfg.Engine,
		eventEmitter:  cfg.EventEmitter,
		transactor:    cfg.Transactor,
	}
}

// checkAccess evaluates an access request using the ABAC policy engine.
// Returns nil if allowed, or an error with appropriate oops error codes:
//
//   - ErrPermissionDenied       → oops.Code("{entityPrefix}_ACCESS_DENIED")
//   - ErrAccessEvaluationFailed → oops.Code("{entityPrefix}_ACCESS_EVALUATION_FAILED")
//   - all other errors          → oops.Code("{entityPrefix}_ACCESS_EVALUATION_FAILED")
//
// The entityPrefix parameter determines the error code prefix (e.g., "LOCATION", "EXIT").
// Unknown errors (context errors, DB failures, etc.) are classified as evaluation
// failures rather than denials to avoid poisoning metrics and user feedback.
func (s *Service) checkAccess(ctx context.Context, subject, action, resource, entityPrefix string) error {
	req, reqErr := types.NewAccessRequest(subject, action, resource)
	if reqErr != nil {
		slog.ErrorContext(ctx, "invalid access request",
			"error", reqErr, "subject", subject, "action", action, "resource", resource)
		observability.RecordEngineFailure(entityPrefix + "_access_check")
		return oops.Code(entityPrefix + "_ACCESS_EVALUATION_FAILED").
			Wrap(fmt.Errorf("%w: %w", ErrAccessEvaluationFailed, reqErr))
	}
	decision, err := s.engine.Evaluate(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "access evaluation failed",
			"error", err, "subject", subject, "action", action, "resource", resource)
		observability.RecordEngineFailure(entityPrefix + "_access_check")
		return oops.Code(entityPrefix + "_ACCESS_EVALUATION_FAILED").
			Wrap(fmt.Errorf("%w: %w", ErrAccessEvaluationFailed, err))
	}
	if !decision.IsAllowed() {
		deniedErr := oops.With("reason", decision.Reason).With("policy_id", decision.PolicyID).Wrap(ErrPermissionDenied)
		return oops.Code(entityPrefix + "_ACCESS_DENIED").Wrap(deniedErr)
	}
	return nil
}

// GetLocation retrieves a location by ID after checking read authorization.
func (s *Service) GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*Location, error) {
	if s.locationRepo == nil {
		return nil, oops.Code("LOCATION_GET_FAILED").Errorf("location repository not configured")
	}
	resource := fmt.Sprintf("location:%s", id.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, "LOCATION"); err != nil {
		return nil, err
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
	if err := s.checkAccess(ctx, subjectID, "write", "location:*", "LOCATION"); err != nil {
		return err
	}
	if loc == nil {
		return oops.Code("LOCATION_INVALID").Errorf("location is nil")
	}
	// Assign ID before validation since Validate() now requires non-zero ID
	if loc.ID.IsZero() {
		loc.ID = ulid.Make()
	}
	if err := loc.Validate(); err != nil {
		return oops.Code("LOCATION_INVALID").Wrap(err)
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
	if err := s.checkAccess(ctx, subjectID, "write", resource, "LOCATION"); err != nil {
		return err
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

// DeleteLocation deletes a location and its properties after checking delete authorization.
// Both deletions occur in the same database transaction per spec (05-storage-audit.md §110-119).
// Returns an error if PropertyRepo or Transactor are not configured.
func (s *Service) DeleteLocation(ctx context.Context, subjectID string, id ulid.ULID) error {
	if s.locationRepo == nil {
		return oops.Code("LOCATION_DELETE_FAILED").Errorf("location repository not configured")
	}
	if s.propertyRepo == nil {
		return oops.Code("LOCATION_DELETE_FAILED").Errorf("property repository required for cascade delete (spec: 05-storage-audit.md §108-119)")
	}
	if s.transactor == nil {
		return oops.Code("LOCATION_DELETE_FAILED").Errorf("transactor required for transactional cascade delete (spec: 05-storage-audit.md §117)")
	}
	resource := fmt.Sprintf("location:%s", id.String())
	if err := s.checkAccess(ctx, subjectID, "delete", resource, "LOCATION"); err != nil {
		return err
	}
	deleteFn := func(ctx context.Context) error {
		if err := s.propertyRepo.DeleteByParent(ctx, "location", id); err != nil {
			return oops.Code("LOCATION_DELETE_FAILED").
				With("operation", "delete_location_properties").
				Wrapf(err, "delete properties for location %s", id)
		}
		if err := s.locationRepo.Delete(ctx, id); err != nil {
			if errors.Is(err, ErrNotFound) {
				return oops.Code("LOCATION_NOT_FOUND").Wrapf(err, "delete location %s", id)
			}
			return oops.Code("LOCATION_DELETE_FAILED").Wrapf(err, "delete location %s", id)
		}
		return nil
	}
	if err := s.transactor.InTransaction(ctx, deleteFn); err != nil {
		return oops.Code("LOCATION_DELETE_FAILED").Wrap(err)
	}
	return nil
}

// GetExit retrieves an exit by ID after checking read authorization.
func (s *Service) GetExit(ctx context.Context, subjectID string, id ulid.ULID) (*Exit, error) {
	if s.exitRepo == nil {
		return nil, oops.Code("EXIT_GET_FAILED").Errorf("exit repository not configured")
	}
	resource := fmt.Sprintf("exit:%s", id.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, "EXIT"); err != nil {
		return nil, err
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
//
// Returns a ValidationError if the id, name, aliases, visibility, lock type,
// lock data, or visible_to are invalid.
// Returns ErrSelfReferentialExit if from and to locations are the same.
func (s *Service) CreateExit(ctx context.Context, subjectID string, exit *Exit) error {
	if s.exitRepo == nil {
		return oops.Code("EXIT_CREATE_FAILED").Errorf("exit repository not configured")
	}
	if err := s.checkAccess(ctx, subjectID, "write", "exit:*", "EXIT"); err != nil {
		return err
	}
	if exit == nil {
		return oops.Code("EXIT_INVALID").Errorf("exit is nil")
	}
	// Assign ID before validation so Validate() doesn't reject zero ID
	if exit.ID.IsZero() {
		exit.ID = ulid.Make()
	}
	if err := exit.Validate(); err != nil {
		return oops.Code("EXIT_INVALID").Wrap(err)
	}
	if err := s.exitRepo.Create(ctx, exit); err != nil {
		return oops.Code("EXIT_CREATE_FAILED").Wrapf(err, "create exit %s", exit.ID)
	}
	return nil
}

// UpdateExit updates an existing exit after checking write authorization.
//
// Returns a ValidationError if the id, name, aliases, visibility, lock type,
// lock data, or visible_to are invalid.
// Returns ErrSelfReferentialExit if from and to locations are the same.
func (s *Service) UpdateExit(ctx context.Context, subjectID string, exit *Exit) error {
	if s.exitRepo == nil {
		return oops.Code("EXIT_UPDATE_FAILED").Errorf("exit repository not configured")
	}
	if exit == nil {
		return oops.Code("EXIT_INVALID").Errorf("exit is nil")
	}
	resource := fmt.Sprintf("exit:%s", exit.ID.String())
	if err := s.checkAccess(ctx, subjectID, "write", resource, "EXIT"); err != nil {
		return err
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
	if err := s.checkAccess(ctx, subjectID, "delete", resource, "EXIT"); err != nil {
		return err
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

// GetExitsByLocation retrieves all exits from a location after checking read authorization.
func (s *Service) GetExitsByLocation(ctx context.Context, subjectID string, locationID ulid.ULID) ([]*Exit, error) {
	if s.exitRepo == nil {
		return nil, oops.Code("EXIT_LIST_FAILED").Errorf("exit repository not configured")
	}
	resource := fmt.Sprintf("location:%s", locationID.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, "EXIT"); err != nil {
		return nil, err
	}
	exits, err := s.exitRepo.ListFromLocation(ctx, locationID)
	if err != nil {
		return nil, oops.Code("EXIT_LIST_FAILED").Wrapf(err, "list exits from location %s", locationID)
	}
	return exits, nil
}

// GetObject retrieves an object by ID after checking read authorization.
func (s *Service) GetObject(ctx context.Context, subjectID string, id ulid.ULID) (*Object, error) {
	if s.objectRepo == nil {
		return nil, oops.Code("OBJECT_GET_FAILED").Errorf("object repository not configured")
	}
	resource := fmt.Sprintf("object:%s", id.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, "OBJECT"); err != nil {
		return nil, err
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
	if err := s.checkAccess(ctx, subjectID, "write", "object:*", "OBJECT"); err != nil {
		return err
	}
	if obj == nil {
		return oops.Code("OBJECT_INVALID").Errorf("object is nil")
	}
	// Assign ID before validation so Validate() doesn't reject zero ID
	if obj.ID.IsZero() {
		obj.ID = ulid.Make()
	}
	if err := obj.Validate(); err != nil {
		return oops.Code("OBJECT_INVALID").Wrap(err)
	}
	if err := obj.ValidateContainment(); err != nil {
		return oops.Code("OBJECT_INVALID").Wrap(err)
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
	if err := s.checkAccess(ctx, subjectID, "write", resource, "OBJECT"); err != nil {
		return err
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

// DeleteObject deletes an object and its properties after checking delete authorization.
// Both deletions occur in the same database transaction per spec (05-storage-audit.md §110-119).
// Returns an error if PropertyRepo or Transactor are not configured.
func (s *Service) DeleteObject(ctx context.Context, subjectID string, id ulid.ULID) error {
	if s.objectRepo == nil {
		return oops.Code("OBJECT_DELETE_FAILED").Errorf("object repository not configured")
	}
	if s.propertyRepo == nil {
		return oops.Code("OBJECT_DELETE_FAILED").Errorf("property repository required for cascade delete (spec: 05-storage-audit.md §108-119)")
	}
	if s.transactor == nil {
		return oops.Code("OBJECT_DELETE_FAILED").Errorf("transactor required for transactional cascade delete (spec: 05-storage-audit.md §117)")
	}
	resource := fmt.Sprintf("object:%s", id.String())
	if err := s.checkAccess(ctx, subjectID, "delete", resource, "OBJECT"); err != nil {
		return err
	}
	deleteFn := func(ctx context.Context) error {
		if err := s.propertyRepo.DeleteByParent(ctx, "object", id); err != nil {
			return oops.Code("OBJECT_DELETE_FAILED").
				With("operation", "delete_object_properties").
				Wrapf(err, "delete properties for object %s", id)
		}
		if err := s.objectRepo.Delete(ctx, id); err != nil {
			if errors.Is(err, ErrNotFound) {
				return oops.Code("OBJECT_NOT_FOUND").Wrapf(err, "delete object %s", id)
			}
			return oops.Code("OBJECT_DELETE_FAILED").Wrapf(err, "delete object %s", id)
		}
		return nil
	}
	if err := s.transactor.InTransaction(ctx, deleteFn); err != nil {
		return oops.Code("OBJECT_DELETE_FAILED").Wrap(err)
	}
	return nil
}

// MoveObject moves an object to a new containment (location, character inventory, or another object).
// Emits a "move" event for plugins after successful database update.
//
// Event emission follows eventual consistency: the database move succeeds atomically first,
// then an event is emitted. If event emission fails after all retries (3 retries, 4 total attempts) are exhausted:
//   - Returns EVENT_EMIT_FAILED error (from events.go, wrapped with move context)
//   - Error context includes move_succeeded=true to indicate the database change persisted
//   - Callers should NOT retry the move (it already succeeded in the database)
//   - Callers may choose to log the event failure and treat the user operation as successful
//
// Returns EVENT_EMITTER_MISSING error if no emitter was configured (system misconfiguration).
//
// This design ensures data consistency while surfacing event delivery failures to callers.
func (s *Service) MoveObject(ctx context.Context, subjectID string, id ulid.ULID, to Containment) error {
	if s.objectRepo == nil {
		return oops.Code("OBJECT_MOVE_FAILED").Errorf("object repository not configured")
	}
	resource := fmt.Sprintf("object:%s", id.String())
	if err := s.checkAccess(ctx, subjectID, "write", resource, "OBJECT"); err != nil {
		return err
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

	// Emit move event - failures are propagated to the caller
	payload := MovePayload{
		EntityType: EntityTypeObject,
		EntityID:   id,
		FromType:   from.Type(),
		FromID:     from.ID(), // Can be nil for first-time placements
		ToType:     to.Type(),
		ToID:       *to.ID(), // Safe: to.Validate() ensures one field is set
	}
	if err := EmitMoveEvent(ctx, s.eventEmitter, payload); err != nil {
		// Add OBJECT_MOVE_EVENT_FAILED at top level for error categorization
		// Inner error has EVENT_EMIT_FAILED code from events.go
		return oops.Code("OBJECT_MOVE_EVENT_FAILED").
			With("object_id", id.String()).
			With("move_succeeded", true).
			Wrapf(err, "move completed but event emission failed")
	}

	return nil
}

// DeleteCharacter deletes a character and its properties after checking delete authorization.
// Both deletions occur in the same database transaction per spec (05-storage-audit.md §110-119).
// Returns an error if PropertyRepo or Transactor are not configured.
func (s *Service) DeleteCharacter(ctx context.Context, subjectID string, id ulid.ULID) error {
	if s.characterRepo == nil {
		return oops.Code("CHARACTER_DELETE_FAILED").Errorf("character repository not configured")
	}
	if s.propertyRepo == nil {
		return oops.Code("CHARACTER_DELETE_FAILED").Errorf("property repository required for cascade delete (spec: 05-storage-audit.md §108-119)")
	}
	if s.transactor == nil {
		return oops.Code("CHARACTER_DELETE_FAILED").Errorf("transactor required for transactional cascade delete (spec: 05-storage-audit.md §117)")
	}
	resource := fmt.Sprintf("character:%s", id.String())
	if err := s.checkAccess(ctx, subjectID, "delete", resource, "CHARACTER"); err != nil {
		return err
	}
	deleteFn := func(ctx context.Context) error {
		if err := s.propertyRepo.DeleteByParent(ctx, "character", id); err != nil {
			return oops.Code("CHARACTER_DELETE_FAILED").
				With("operation", "delete_character_properties").
				Wrapf(err, "delete properties for character %s", id)
		}
		if err := s.characterRepo.Delete(ctx, id); err != nil {
			if errors.Is(err, ErrNotFound) {
				return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "delete character %s", id)
			}
			return oops.Code("CHARACTER_DELETE_FAILED").Wrapf(err, "delete character %s", id)
		}
		return nil
	}
	if err := s.transactor.InTransaction(ctx, deleteFn); err != nil {
		return oops.Code("CHARACTER_DELETE_FAILED").Wrap(err)
	}
	return nil
}

// GetCharacter retrieves a character by ID after checking read authorization.
func (s *Service) GetCharacter(ctx context.Context, subjectID string, id ulid.ULID) (*Character, error) {
	if s.characterRepo == nil {
		return nil, oops.Code("CHARACTER_GET_FAILED").Errorf("character repository not configured")
	}
	resource := fmt.Sprintf("character:%s", id.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, "CHARACTER"); err != nil {
		return nil, err
	}
	char, err := s.characterRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "get character %s", id)
		}
		return nil, oops.Code("CHARACTER_GET_FAILED").Wrapf(err, "get character %s", id)
	}
	return char, nil
}

// GetCharactersByLocation retrieves characters at a location with pagination after checking list_characters authorization.
// Note: This decomposes the legacy compound resource "location:<id>:characters" into
// resource="location:<id>" with action="list_characters" per ADR #76 (Compound Resource Decomposition,
// see docs/specs/2026-02-05-full-abac-design.md §7.3).
func (s *Service) GetCharactersByLocation(ctx context.Context, subjectID string, locationID ulid.ULID, opts ListOptions) ([]*Character, error) {
	if s.characterRepo == nil {
		return nil, oops.Code("CHARACTER_QUERY_FAILED").Errorf("character repository not configured")
	}
	resource := fmt.Sprintf("location:%s", locationID.String())
	if err := s.checkAccess(ctx, subjectID, "list_characters", resource, "CHARACTER"); err != nil {
		return nil, err
	}
	chars, err := s.characterRepo.GetByLocation(ctx, locationID, opts)
	if err != nil {
		return nil, oops.Code("CHARACTER_QUERY_FAILED").Wrapf(err, "get characters by location %s", locationID)
	}
	return chars, nil
}

// AddSceneParticipant adds a character to a scene after checking write authorization.
// Returns ErrInvalidParticipantRole if the role is not valid.
func (s *Service) AddSceneParticipant(ctx context.Context, subjectID string, sceneID, characterID ulid.ULID, role ParticipantRole) error {
	if s.sceneRepo == nil {
		return oops.Code("SCENE_ADD_PARTICIPANT_FAILED").Errorf("scene repository not configured")
	}
	resource := fmt.Sprintf("scene:%s", sceneID.String())
	if err := s.checkAccess(ctx, subjectID, "write", resource, "SCENE"); err != nil {
		return err
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
	if err := s.checkAccess(ctx, subjectID, "write", resource, "SCENE"); err != nil {
		return err
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
	if err := s.checkAccess(ctx, subjectID, "read", resource, "SCENE"); err != nil {
		return nil, err
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

// MoveCharacter moves a character to a new location.
// Emits a "move" event for plugins after successful database update.
//
// Event emission follows eventual consistency: the database move succeeds atomically first,
// then an event is emitted. If event emission fails after all retries (3 retries, 4 total attempts) are exhausted:
//   - Returns EVENT_EMIT_FAILED error (from events.go, wrapped with move context)
//   - Error context includes move_succeeded=true to indicate the database change persisted
//   - Callers should NOT retry the move (it already succeeded in the database)
//   - Callers may choose to log the event failure and treat the user operation as successful
//
// Returns EVENT_EMITTER_MISSING error if no emitter was configured (system misconfiguration).
//
// This design ensures data consistency while surfacing event delivery failures to callers.
func (s *Service) MoveCharacter(ctx context.Context, subjectID string, characterID, toLocationID ulid.ULID) error {
	if s.characterRepo == nil {
		return oops.Code("CHARACTER_MOVE_FAILED").Errorf("character repository not configured")
	}
	resource := fmt.Sprintf("character:%s", characterID.String())
	if err := s.checkAccess(ctx, subjectID, "write", resource, "CHARACTER"); err != nil {
		return err
	}

	// Get current location for the move event
	char, err := s.characterRepo.Get(ctx, characterID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "move character %s", characterID)
		}
		return oops.Code("CHARACTER_MOVE_FAILED").Wrapf(err, "get character %s", characterID)
	}

	// Verify destination location exists
	if s.locationRepo == nil {
		return oops.Code("CHARACTER_MOVE_FAILED").Errorf("location repository not configured")
	}
	if _, err := s.locationRepo.Get(ctx, toLocationID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("LOCATION_NOT_FOUND").Wrapf(err, "move character to location %s", toLocationID)
		}
		return oops.Code("CHARACTER_MOVE_FAILED").Wrapf(err, "verify destination location %s", toLocationID)
	}

	// Update character location
	if err := s.characterRepo.UpdateLocation(ctx, characterID, &toLocationID); err != nil {
		return oops.Code("CHARACTER_MOVE_FAILED").Wrapf(err, "update character %s location", characterID)
	}

	// Build move payload
	var fromType ContainmentType
	var fromID *ulid.ULID
	if char.LocationID == nil {
		fromType = ContainmentTypeNone
		fromID = nil
	} else {
		fromType = ContainmentTypeLocation
		fromID = char.LocationID
	}

	payload := MovePayload{
		EntityType: EntityTypeCharacter,
		EntityID:   characterID,
		FromType:   fromType,
		FromID:     fromID,
		ToType:     ContainmentTypeLocation,
		ToID:       toLocationID,
	}
	if err := EmitMoveEvent(ctx, s.eventEmitter, payload); err != nil {
		// Add CHARACTER_MOVE_EVENT_FAILED at top level for error categorization
		// Inner error has EVENT_EMIT_FAILED code from events.go
		return oops.Code("CHARACTER_MOVE_EVENT_FAILED").
			With("character_id", characterID.String()).
			With("move_succeeded", true).
			Wrapf(err, "move completed but event emission failed")
	}

	return nil
}

// ExamineLocation allows a character to examine a location.
// Emits an examine event for plugins after validation and authorization.
// Returns EVENT_EMITTER_MISSING error if no emitter was configured (system misconfiguration).
// Returns EVENT_EMIT_FAILED error if event emission fails after retries.
func (s *Service) ExamineLocation(ctx context.Context, subjectID string, characterID, targetLocationID ulid.ULID) error {
	if s.characterRepo == nil {
		return oops.Code("EXAMINE_FAILED").Errorf("character repository not configured")
	}
	if s.locationRepo == nil {
		return oops.Code("EXAMINE_FAILED").Errorf("location repository not configured")
	}

	// Get the examining character
	char, err := s.characterRepo.Get(ctx, characterID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "examine character %s not found", characterID)
		}
		return oops.Code("EXAMINE_FAILED").Wrapf(err, "get examining character %s", characterID)
	}

	// Character must be in the world to examine anything
	if char.LocationID == nil {
		return oops.Code("EXAMINE_FAILED").Errorf("character %s not in world", characterID)
	}

	// Get the target location
	targetLoc, err := s.locationRepo.Get(ctx, targetLocationID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("LOCATION_NOT_FOUND").Wrapf(err, "target location %s not found", targetLocationID)
		}
		return oops.Code("EXAMINE_FAILED").Wrapf(err, "get target location %s", targetLocationID)
	}

	// Check authorization to read the target
	resource := fmt.Sprintf("location:%s", targetLocationID.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, "EXAMINE"); err != nil {
		return err
	}

	// Build and emit examine event
	payload := ExaminePayload{
		CharacterID: characterID,
		TargetType:  TargetTypeLocation,
		TargetID:    targetLocationID,
		TargetName:  targetLoc.Name,
		LocationID:  *char.LocationID,
	}
	if err := EmitExamineEvent(ctx, s.eventEmitter, payload); err != nil {
		return oops.Code("EXAMINE_LOCATION_EVENT_FAILED").
			With("character_id", characterID.String()).
			With("target_id", targetLocationID.String()).
			Wrapf(err, "examine location event emission failed")
	}
	return nil
}

// ExamineObject allows a character to examine an object.
// Emits an examine event for plugins after validation and authorization.
// Returns EVENT_EMITTER_MISSING error if no emitter was configured (system misconfiguration).
// Returns EVENT_EMIT_FAILED error if event emission fails after retries.
func (s *Service) ExamineObject(ctx context.Context, subjectID string, characterID, targetObjectID ulid.ULID) error {
	if s.characterRepo == nil {
		return oops.Code("EXAMINE_FAILED").Errorf("character repository not configured")
	}
	if s.objectRepo == nil {
		return oops.Code("EXAMINE_FAILED").Errorf("object repository not configured")
	}

	// Get the examining character
	char, err := s.characterRepo.Get(ctx, characterID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "examine character %s not found", characterID)
		}
		return oops.Code("EXAMINE_FAILED").Wrapf(err, "get examining character %s", characterID)
	}

	// Character must be in the world to examine anything
	if char.LocationID == nil {
		return oops.Code("EXAMINE_FAILED").Errorf("character %s not in world", characterID)
	}

	// Get the target object
	targetObj, err := s.objectRepo.Get(ctx, targetObjectID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("OBJECT_NOT_FOUND").Wrapf(err, "target object %s not found", targetObjectID)
		}
		return oops.Code("EXAMINE_FAILED").Wrapf(err, "get target object %s", targetObjectID)
	}

	// Check authorization to read the target
	resource := fmt.Sprintf("object:%s", targetObjectID.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, "EXAMINE"); err != nil {
		return err
	}

	// Build and emit examine event
	payload := ExaminePayload{
		CharacterID: characterID,
		TargetType:  TargetTypeObject,
		TargetID:    targetObjectID,
		TargetName:  targetObj.Name,
		LocationID:  *char.LocationID,
	}
	if err := EmitExamineEvent(ctx, s.eventEmitter, payload); err != nil {
		return oops.Code("EXAMINE_OBJECT_EVENT_FAILED").
			With("character_id", characterID.String()).
			With("target_id", targetObjectID.String()).
			Wrapf(err, "examine object event emission failed")
	}
	return nil
}

// ExamineCharacter allows a character to examine another character.
// Emits an examine event for plugins after validation and authorization.
// Returns EVENT_EMITTER_MISSING error if no emitter was configured (system misconfiguration).
// Returns EVENT_EMIT_FAILED error if event emission fails after retries.
func (s *Service) ExamineCharacter(ctx context.Context, subjectID string, characterID, targetCharacterID ulid.ULID) error {
	if s.characterRepo == nil {
		return oops.Code("EXAMINE_FAILED").Errorf("character repository not configured")
	}

	// Get the examining character
	char, err := s.characterRepo.Get(ctx, characterID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "examine character %s not found", characterID)
		}
		return oops.Code("EXAMINE_FAILED").Wrapf(err, "get examining character %s", characterID)
	}

	// Character must be in the world to examine anything
	if char.LocationID == nil {
		return oops.Code("EXAMINE_FAILED").Errorf("character %s not in world", characterID)
	}

	// Get the target character
	targetChar, err := s.characterRepo.Get(ctx, targetCharacterID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "target character %s not found", targetCharacterID)
		}
		return oops.Code("EXAMINE_FAILED").Wrapf(err, "get target character %s", targetCharacterID)
	}

	// Check authorization to read the target
	resource := fmt.Sprintf("character:%s", targetCharacterID.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, "EXAMINE"); err != nil {
		return err
	}

	// Build and emit examine event
	payload := ExaminePayload{
		CharacterID: characterID,
		TargetType:  TargetTypeCharacter,
		TargetID:    targetCharacterID,
		TargetName:  targetChar.Name,
		LocationID:  *char.LocationID,
	}
	if err := EmitExamineEvent(ctx, s.eventEmitter, payload); err != nil {
		return oops.Code("EXAMINE_CHARACTER_EVENT_FAILED").
			With("character_id", characterID.String()).
			With("target_id", targetCharacterID.String()).
			Wrapf(err, "examine character event emission failed")
	}
	return nil
}

// FindLocationByName searches for a location by name after checking read authorization.
// Returns ErrNotFound if no location matches.
func (s *Service) FindLocationByName(ctx context.Context, subjectID, name string) (*Location, error) {
	if s.locationRepo == nil {
		return nil, oops.Code("LOCATION_FIND_FAILED").Errorf("location repository not configured")
	}
	// Check read authorization for location wildcard (searching locations)
	if err := s.checkAccess(ctx, subjectID, "read", "location:*", "LOCATION"); err != nil {
		return nil, err
	}
	loc, err := s.locationRepo.FindByName(ctx, name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, oops.Code("LOCATION_NOT_FOUND").With("name", name).Wrap(err)
		}
		return nil, oops.Code("LOCATION_FIND_FAILED").With("name", name).Wrap(err)
	}
	return loc, nil
}
