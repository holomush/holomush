// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/observability"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/pkg/errutil"
)

// defaultGameID is the single-game identity used when ServiceConfig.GameID is
// empty. Phase 5 runs one game ("main"); the outbox feed counter, per-game
// ordering, and consumer watermarks are all keyed by it, ready for multi-game
// later (wmodel round-9 R6-5).
const defaultGameID = "main"

// Envelope taxonomy for the world-change feed. The versioned taxonomy schema
// registry lands in 05-09; until then these literal kinds/version identify the
// intent-level payload shape.
const (
	kindCharacterMoved             = "character_moved"
	kindCharacterPreferencesUpdate = "character_preferences_update"
	worldSchemaVersion             = 1
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
	Transactor    Transactor
	// OutboxWriter persists the same-tx world-change envelope for guarded write
	// commands routed through the write executor (05-06 wires MoveCharacter; the
	// subsystem injection is 05-07). Injected as an interface so package world
	// imports neither internal/world/outbox nor internal/world/postgres.
	OutboxWriter OutboxWriter
	// GameID keys the outbox feed counter and the outbox row's game_id. Defaults to
	// "main" when empty (single-game Phase 5).
	GameID string
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
	transactor    Transactor
	movementHook  MovementHook
	// mutator is the write executor + write-requires-envelope seam. It owns the
	// private write repos + transactor + injected OutboxWriter (05-06). Nil until
	// an OutboxWriter is configured; MoveCharacter reports a configuration error if
	// so.
	mutator *worldMutator
	gameID  string
}

// NewService creates a new Service with the given configuration.
// Panics if Engine is nil, as it is required for all operations.
func NewService(cfg ServiceConfig) *Service {
	if cfg.Engine == nil {
		panic("world.NewService: Engine is required")
	}
	if cfg.PropertyRepo == nil || cfg.Transactor == nil {
		slog.Warn("world.NewService: PropertyRepo and Transactor not configured, delete operations will fail (spec: 05-storage-audit.md §108-119 requires transactional cascade)")
	}
	if cfg.OutboxWriter == nil {
		slog.Warn("world.NewService: OutboxWriter not configured, envelope-emitting write commands (MoveCharacter) will fail (subsystem wiring lands in 05-07)")
	}
	gameID := cfg.GameID
	if gameID == "" {
		gameID = defaultGameID
	}
	var mutator *worldMutator
	if cfg.OutboxWriter != nil && cfg.Transactor != nil {
		mutator = newWorldMutator(cfg.CharacterRepo, cfg.Transactor, cfg.OutboxWriter)
	}
	return &Service{
		locationRepo:  cfg.LocationRepo,
		exitRepo:      cfg.ExitRepo,
		objectRepo:    cfg.ObjectRepo,
		sceneRepo:     cfg.SceneRepo,
		characterRepo: cfg.CharacterRepo,
		propertyRepo:  cfg.PropertyRepo,
		engine:        cfg.Engine,
		transactor:    cfg.Transactor,
		movementHook:  NoopMovementHook{},
		mutator:       mutator,
		gameID:        gameID,
	}
}

// SetMovementHook registers a hook that is invoked after each successful
// character location update and before the move event is emitted.
// Passing nil resets to the no-op default.
func (s *Service) SetMovementHook(h MovementHook) {
	if h == nil {
		s.movementHook = NoopMovementHook{}
		return
	}
	s.movementHook = h
}

// entityPrefix is a typed string for checkAccess error code prefixes.
// Using a distinct type prevents typos in free-form prefix strings from
// producing unhandled error codes.
type entityPrefix string

const (
	prefixLocation  entityPrefix = "LOCATION"
	prefixExit      entityPrefix = "EXIT"
	prefixObject    entityPrefix = "OBJECT"
	prefixCharacter entityPrefix = "CHARACTER"
	prefixScene     entityPrefix = "SCENE"
	prefixProperty  entityPrefix = "PROPERTY"
)

// KnownEntityPrefixes returns all entity prefix strings.
// Exported so cross-package tests can stay in sync without hardcoding.
func KnownEntityPrefixes() []string {
	return []string{
		string(prefixLocation),
		string(prefixExit),
		string(prefixObject),
		string(prefixCharacter),
		string(prefixScene),
		string(prefixProperty),
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
//
// Metrics: calls observability.RecordEngineFailure in all error paths. The
// holomush_engine_failures_total counter uses a package-level Prometheus var
// that is not exported; metric increments are verified by integration tests.
func (s *Service) checkAccess(ctx context.Context, subject, action, resource string, prefix entityPrefix) error {
	metricKey := strings.ToLower(string(prefix)) + "_access_check"
	failCode := string(prefix) + "_ACCESS_EVALUATION_FAILED"
	denyCode := string(prefix) + "_ACCESS_DENIED"

	req, reqErr := types.NewAccessRequest(subject, action, resource, nil)
	if reqErr != nil {
		// Defensive: all call sites should use typed helpers
		// (access.CharacterSubject, access.LocationResource, etc.) that panic on
		// empty input, and action strings are hardcoded literals. Kept as defense
		// in depth against future call sites that might bypass the typed helpers.
		errutil.LogErrorContext(ctx, "invalid access request",
			reqErr, "subject", subject, "action", action, "resource", resource)
		observability.RecordEngineFailure(metricKey)
		return oops.Code(failCode).
			Wrap(errors.Join(ErrAccessEvaluationFailed, reqErr))
	}
	decision, err := s.engine.Evaluate(ctx, req)
	if err != nil {
		errutil.LogErrorContext(ctx, "access evaluation failed",
			err, "subject", subject, "action", action, "resource", resource)
		observability.RecordEngineFailure(metricKey)
		return oops.Code(failCode).
			Wrap(errors.Join(ErrAccessEvaluationFailed, err))
	}
	if !decision.IsAllowed() {
		// Infrastructure failures (session resolution, DB errors) should return
		// ErrAccessEvaluationFailed, not ErrPermissionDenied, so callers and users
		// can distinguish transient failures from policy denials.
		if decision.IsInfraFailure() {
			slog.ErrorContext(ctx, "access check infrastructure failure",
				"policy_id", decision.PolicyID(), "reason", decision.Reason(),
				"subject", subject, "action", action, "resource", resource)
			observability.RecordEngineFailure(metricKey)
			return oops.Code(failCode).
				With("reason", decision.Reason()).
				With("policy_id", decision.PolicyID()).
				Wrap(ErrAccessEvaluationFailed)
		}
		return oops.Code(denyCode).
			With("reason", decision.Reason()).
			With("policy_id", decision.PolicyID()).
			Wrap(ErrPermissionDenied)
	}
	return nil
}

// GetLocation retrieves a location by ID after checking read authorization.
func (s *Service) GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*Location, error) {
	if s.locationRepo == nil {
		return nil, oops.Code("LOCATION_GET_FAILED").Errorf("location repository not configured")
	}
	resource := access.LocationResource(id.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, prefixLocation); err != nil {
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
	if err := s.checkAccess(ctx, subjectID, "write", access.LocationResource("*"), prefixLocation); err != nil {
		return err
	}
	if loc == nil {
		return oops.Code("LOCATION_INVALID").Errorf("location is nil")
	}
	// Assign ID before validation since Validate() now requires non-zero ID
	if loc.ID.IsZero() {
		loc.ID = idgen.New()
	}
	if err := loc.Validate(); err != nil {
		return oops.Code("LOCATION_INVALID").Wrap(err)
	}
	if _, err := s.locationRepo.Create(ctx, loc); err != nil {
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
	resource := access.LocationResource(loc.ID.String())
	if err := s.checkAccess(ctx, subjectID, "write", resource, prefixLocation); err != nil {
		return err
	}
	if err := loc.Validate(); err != nil {
		return oops.Code("LOCATION_INVALID").Wrap(err)
	}
	if _, err := s.locationRepo.Update(ctx, loc); err != nil {
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
	resource := access.LocationResource(id.String())
	if err := s.checkAccess(ctx, subjectID, "delete", resource, prefixLocation); err != nil {
		return err
	}
	deleteFn := func(ctx context.Context) error {
		if err := s.propertyRepo.DeleteByParent(ctx, "location", id); err != nil {
			return oops.Code("LOCATION_DELETE_FAILED").
				With("operation", "delete_location_properties").
				Wrapf(err, "delete properties for location %s", id)
		}
		if _, err := s.locationRepo.Delete(ctx, id, 0); err != nil {
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
	resource := access.ExitResource(id.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, prefixExit); err != nil {
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
	if err := s.checkAccess(ctx, subjectID, "write", access.ExitResource("*"), prefixExit); err != nil {
		return err
	}
	if exit == nil {
		return oops.Code("EXIT_INVALID").Errorf("exit is nil")
	}
	// Assign ID before validation so Validate() doesn't reject zero ID
	if exit.ID.IsZero() {
		exit.ID = idgen.New()
	}
	if err := exit.Validate(); err != nil {
		return oops.Code("EXIT_INVALID").Wrap(err)
	}
	if _, err := s.exitRepo.Create(ctx, exit); err != nil {
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
	resource := access.ExitResource(exit.ID.String())
	if err := s.checkAccess(ctx, subjectID, "write", resource, prefixExit); err != nil {
		return err
	}
	if err := exit.Validate(); err != nil {
		return oops.Code("EXIT_INVALID").Wrap(err)
	}
	if _, err := s.exitRepo.Update(ctx, exit); err != nil {
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
	resource := access.ExitResource(id.String())
	if err := s.checkAccess(ctx, subjectID, "delete", resource, prefixExit); err != nil {
		return err
	}
	_, err := s.exitRepo.Delete(ctx, id, 0)
	if err != nil {
		// Check if this is a cleanup result from bidirectional exit handling
		var cleanupResult *BidirectionalCleanupResult
		if errors.As(err, &cleanupResult) {
			// Log cleanup issues at appropriate level
			if cleanupResult.IsSevere() {
				// Severe: operation was rolled back, primary delete did NOT complete
				slog.ErrorContext(ctx, "bidirectional exit delete rolled back",
					"exit_id", cleanupResult.ExitID.String(),
					"error", cleanupResult.Error())
				return oops.Code("EXIT_DELETE_FAILED").Wrapf(err, "delete exit %s", id)
			}
			// Non-severe: primary delete succeeded, return exit was just not found
			slog.InfoContext(ctx, "bidirectional exit cleanup notice: return exit already deleted",
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
	resource := access.LocationResource(locationID.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, prefixLocation); err != nil {
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
	resource := access.ObjectResource(id.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, prefixObject); err != nil {
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
	if err := s.checkAccess(ctx, subjectID, "write", access.ObjectResource("*"), prefixObject); err != nil {
		return err
	}
	if obj == nil {
		return oops.Code("OBJECT_INVALID").Errorf("object is nil")
	}
	// Assign ID before validation so Validate() doesn't reject zero ID
	if obj.ID.IsZero() {
		obj.ID = idgen.New()
	}
	if err := obj.Validate(); err != nil {
		return oops.Code("OBJECT_INVALID").Wrap(err)
	}
	if err := obj.ValidateContainment(); err != nil {
		return oops.Code("OBJECT_INVALID").Wrap(err)
	}
	if _, err := s.objectRepo.Create(ctx, obj); err != nil {
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
	resource := access.ObjectResource(obj.ID.String())
	if err := s.checkAccess(ctx, subjectID, "write", resource, prefixObject); err != nil {
		return err
	}
	if err := obj.Validate(); err != nil {
		return oops.Code("OBJECT_INVALID").Wrap(err)
	}
	if err := obj.ValidateContainment(); err != nil {
		return oops.Code("OBJECT_INVALID").Wrap(err)
	}
	if _, err := s.objectRepo.Update(ctx, obj); err != nil {
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
	resource := access.ObjectResource(id.String())
	if err := s.checkAccess(ctx, subjectID, "delete", resource, prefixObject); err != nil {
		return err
	}
	deleteFn := func(ctx context.Context) error {
		if err := s.propertyRepo.DeleteByParent(ctx, "object", id); err != nil {
			return oops.Code("OBJECT_DELETE_FAILED").
				With("operation", "delete_object_properties").
				Wrapf(err, "delete properties for object %s", id)
		}
		if _, err := s.objectRepo.Delete(ctx, id, 0); err != nil {
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
//
// The object-move envelope is not yet routed through the same-tx outbox — object
// commands migrate to the write executor in 05-10/05-11. This slice (05-06) deletes
// the post-commit emit path (D-03) and routes only MoveCharacter through the outbox;
// MoveObject performs the guarded containment write and returns.
func (s *Service) MoveObject(ctx context.Context, subjectID string, id ulid.ULID, to Containment) error {
	if s.objectRepo == nil {
		return oops.Code("OBJECT_MOVE_FAILED").Errorf("object repository not configured")
	}
	resource := access.ObjectResource(id.String())
	if err := s.checkAccess(ctx, subjectID, "write", resource, prefixObject); err != nil {
		return err
	}
	if err := to.Validate(); err != nil {
		return oops.Code("OBJECT_INVALID").Wrap(err)
	}

	// Verify the object exists before moving.
	if _, err := s.objectRepo.Get(ctx, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("OBJECT_NOT_FOUND").Wrapf(err, "move object %s", id)
		}
		return oops.Code("OBJECT_MOVE_FAILED").Wrapf(err, "get object %s", id)
	}

	if _, err := s.objectRepo.Move(ctx, id, to, 0); err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("OBJECT_NOT_FOUND").Wrapf(err, "move object %s", id)
		}
		return oops.Code("OBJECT_MOVE_FAILED").Wrapf(err, "move object %s", id)
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
	resource := access.CharacterResource(id.String())
	if err := s.checkAccess(ctx, subjectID, "delete", resource, prefixCharacter); err != nil {
		return err
	}
	deleteFn := func(ctx context.Context) error {
		if err := s.propertyRepo.DeleteByParent(ctx, "character", id); err != nil {
			return oops.Code("CHARACTER_DELETE_FAILED").
				With("operation", "delete_character_properties").
				Wrapf(err, "delete properties for character %s", id)
		}
		if _, err := s.characterRepo.Delete(ctx, id, 0); err != nil {
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
	resource := access.CharacterResource(id.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, prefixCharacter); err != nil {
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

// UpdateCharacterDescription sets a character's description after checking write authorization.
func (s *Service) UpdateCharacterDescription(ctx context.Context, subjectID string, characterID ulid.ULID, description string) error {
	if s.characterRepo == nil {
		return oops.Code("CHARACTER_UPDATE_FAILED").Errorf("character repository not configured")
	}
	resource := access.CharacterResource(characterID.String())
	if err := s.checkAccess(ctx, subjectID, "write", resource, prefixCharacter); err != nil {
		return err
	}
	char, err := s.characterRepo.Get(ctx, characterID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "get character %s", characterID)
		}
		return oops.Code("CHARACTER_GET_FAILED").Wrapf(err, "get character %s", characterID)
	}
	char.Description = description
	if _, err := s.characterRepo.Update(ctx, char); err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "update character %s", characterID)
		}
		return oops.Code("CHARACTER_UPDATE_FAILED").Wrapf(err, "update character %s", characterID)
	}
	return nil
}

// UpdateCharacterPreferences persists a character's whole preferences bag
// (pre-marshaled JSONB) through the guarded/versioned/envelope world path — the
// folded-in character-settings write (round-4 C5 / D-05). The former raw
// UPDATE characters in internal/store is replaced by this command so the write is
// version-guarded (MODEL-03) and emits exactly one character_preferences_update
// envelope in the SAME transaction (INV-WORLD-4 for the characters table).
//
// It is an RMW with NO caller-supplied version: it reads the current character
// version internally and CASes on it, so two concurrent settings writes race the
// read-then-CAS and exactly one wins — the other surfaces WORLD_CONCURRENT_EDIT
// (D-02: no auto-retry; the settings caller surfaces the typed error rather than
// silently clobbering).
//
// It deliberately runs NO checkAccess: this is the settings-subsystem persistence
// primitive (the authorization decision is made at the settings command layer),
// and the prior raw store path had no ABAC — gating it here would be an
// out-of-scope behavior change (D-05). The envelope actor is the character
// causing its own settings change.
func (s *Service) UpdateCharacterPreferences(ctx context.Context, characterID ulid.ULID, prefs []byte) error {
	if s.characterRepo == nil {
		return oops.Code("CHARACTER_PREFERENCES_UPDATE_FAILED").Errorf("character repository not configured")
	}
	if s.mutator == nil {
		return oops.Code("CHARACTER_PREFERENCES_UPDATE_FAILED").Errorf("world write executor not configured (OutboxWriter + Transactor required)")
	}

	// Read the current character: its version is the CAS guard.
	char, err := s.characterRepo.Get(ctx, characterID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "update preferences for character %s", characterID)
		}
		return oops.Code("CHARACTER_PREFERENCES_UPDATE_FAILED").Wrapf(err, "get character %s", characterID)
	}

	intent, err := s.buildPreferencesIntent(characterID, prefs)
	if err != nil {
		return oops.Code("CHARACTER_PREFERENCES_UPDATE_FAILED").Wrapf(err, "build preferences intent for character %s", characterID)
	}

	if _, err := s.mutator.updateCharacterPreferences(ctx, intent, characterID, prefs, char.Version); err != nil {
		if errors.Is(err, ErrConcurrentEdit) {
			// Surface the typed conflict unchanged (D-02: no auto-retry).
			return oops.Code(CodeConcurrentEdit).
				With("character_id", characterID.String()).
				Wrap(err)
		}
		if errors.Is(err, ErrNotFound) {
			return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "update preferences for character %s", characterID)
		}
		return oops.Code("CHARACTER_PREFERENCES_UPDATE_FAILED").Wrapf(err, "update preferences for character %s", characterID)
	}
	return nil
}

// preferencesIntentPayload is the new-values-only, erasure-safe preferences
// payload persisted in the envelope intent: the character id and the whole
// (pre-marshaled) preferences bag as opaque JSON. Settings bags are not secrets.
type preferencesIntentPayload struct {
	CharacterID string          `json:"character_id"`
	Preferences json.RawMessage `json:"preferences"`
}

// buildPreferencesIntent constructs the character_preferences_update
// EnvelopeIntent. The actor is the character causing its own settings change; the
// payload is new-values-only. The intent omits epoch/feed_position/manifest — the
// writer owns those.
func (s *Service) buildPreferencesIntent(characterID ulid.ULID, prefs []byte) (wmodel.EnvelopeIntent, error) {
	raw := json.RawMessage(prefs)
	if len(raw) == 0 {
		raw = json.RawMessage(`null`)
	}
	payload, err := json.Marshal(preferencesIntentPayload{
		CharacterID: characterID.String(),
		Preferences: raw,
	})
	if err != nil {
		return wmodel.EnvelopeIntent{}, oops.Wrapf(err, "marshal preferences intent payload")
	}
	return wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		GameID:        s.gameID,
		Kind:          kindCharacterPreferencesUpdate,
		SchemaVersion: worldSchemaVersion,
		Actor:         access.CharacterSubject(characterID.String()),
		AggregateType: wmodel.AggregateCharacter,
		AggregateID:   characterID,
		Payload:       payload,
	}), nil
}

// GetCharactersByLocation retrieves characters at a location with pagination after checking list_characters authorization.
// Note: This decomposes the legacy compound resource "location:<id>:characters" into
// resource="location:<id>" with action="list_characters" per ADR #76 (Compound Resource Decomposition,
// see docs/specs/2026-02-05-full-abac-design.md §7.3).
// Error codes use LOCATION_* prefix (not CHARACTER_*) because the gated resource is the location.
func (s *Service) GetCharactersByLocation(ctx context.Context, subjectID string, locationID ulid.ULID, opts ListOptions) ([]*Character, error) {
	if s.characterRepo == nil {
		return nil, oops.Code("CHARACTER_QUERY_FAILED").Errorf("character repository not configured")
	}
	resource := access.LocationResource(locationID.String())
	if err := s.checkAccess(ctx, subjectID, "list_characters", resource, prefixLocation); err != nil {
		return nil, err
	}
	chars, err := s.characterRepo.GetByLocation(ctx, locationID, opts)
	if err != nil {
		return nil, oops.Code("CHARACTER_QUERY_FAILED").Wrapf(err, "get characters by location %s", locationID)
	}
	return chars, nil
}

// Round-5 D-07: AddSceneParticipant/RemoveSceneParticipant were removed — the
// vestigial world scene-participant write surface had no production caller. The
// read surface (ListSceneParticipants) is KEPT.

// ListSceneParticipants lists all participants in a scene after checking read authorization.
func (s *Service) ListSceneParticipants(ctx context.Context, subjectID string, sceneID ulid.ULID) ([]SceneParticipant, error) {
	if s.sceneRepo == nil {
		return nil, oops.Code("SCENE_LIST_PARTICIPANTS_FAILED").Errorf("scene repository not configured")
	}
	resource := access.SceneResource(sceneID.String())
	if err := s.checkAccess(ctx, subjectID, "read", resource, prefixScene); err != nil {
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
//
// The character move and its ONE move envelope commit in the SAME transaction via
// the write executor's same-tx outbox (05-06) — there is no post-commit emit step
// to lose on a broker blip (the M2 dual-write window is closed for this command).
// A failed or no-op move (version conflict, missing destination) writes no
// envelope.
//
// The movement hook fires AFTER that transaction commits, because it propagates the
// new location to the session store — a separate connection pool that cannot enroll
// in the world transaction. A hook failure is operational degradation, not a
// command failure: the move and its envelope are already durable, so the hook error
// is logged + counted and MoveCharacter returns SUCCESS (the session's derived
// location may lag until re-sync — see MovementHook).
func (s *Service) MoveCharacter(ctx context.Context, subjectID string, characterID, toLocationID ulid.ULID) error {
	if s.characterRepo == nil {
		return oops.Code("CHARACTER_MOVE_FAILED").Errorf("character repository not configured")
	}
	resource := access.CharacterResource(characterID.String())
	if err := s.checkAccess(ctx, subjectID, "write", resource, prefixCharacter); err != nil {
		return err
	}

	// Read the current character: its version is the CAS guard and its current
	// location is the new-values-only intent's from-location field.
	char, err := s.characterRepo.Get(ctx, characterID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "move character %s", characterID)
		}
		return oops.Code("CHARACTER_MOVE_FAILED").Wrapf(err, "get character %s", characterID)
	}

	// Verify destination location exists (a pre-commit failure emits no envelope).
	if s.locationRepo == nil {
		return oops.Code("CHARACTER_MOVE_FAILED").Errorf("location repository not configured")
	}
	if _, locErr := s.locationRepo.Get(ctx, toLocationID); locErr != nil {
		if errors.Is(locErr, ErrNotFound) {
			return oops.Code("LOCATION_NOT_FOUND").Wrapf(locErr, "move character to location %s", toLocationID)
		}
		return oops.Code("CHARACTER_MOVE_FAILED").Wrapf(locErr, "verify destination location %s", toLocationID)
	}

	if s.mutator == nil {
		return oops.Code("CHARACTER_MOVE_FAILED").Errorf("world write executor not configured (OutboxWriter + Transactor required)")
	}

	// Build the intent-level, new-values-only envelope intent (no manifest, no
	// epoch/feed_position — those are the writer's to allocate).
	intent, err := s.buildMoveIntent(char, subjectID, characterID, toLocationID)
	if err != nil {
		return oops.Code("CHARACTER_MOVE_FAILED").Wrapf(err, "build move intent for character %s", characterID)
	}

	// Route the guarded character-location write + its move envelope through the
	// same-tx outbox seam. The character's read version is the CAS guard.
	if _, err := s.mutator.moveCharacter(ctx, intent, characterID, toLocationID, char.Version); err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "move character %s", characterID)
		}
		return oops.Code("CHARACTER_MOVE_FAILED").Wrapf(err, "update character %s location", characterID)
	}

	// The state change AND its move envelope have now committed atomically. Fire the
	// movement hook post-commit; a failure is operational degradation (log + metric,
	// return success) — never a command failure after the commit (round-5 finding 3).
	arrivedAt := time.Now().UTC()
	if hookErr := s.movementHook.OnCharacterMoved(ctx, characterID, toLocationID, arrivedAt); hookErr != nil {
		observability.RecordMovementHookFailure()
		slog.WarnContext(ctx, "movement hook failed after committed move; session-derived location may lag until re-sync",
			"character_id", characterID.String(),
			"to_location_id", toLocationID.String(),
			"error", hookErr)
	}

	return nil
}

// moveIntentPayload is the new-values-only, erasure-safe move payload persisted in
// the envelope intent — no secrets, intent-level.
type moveIntentPayload struct {
	CharacterID    string  `json:"character_id"`
	ToLocationID   string  `json:"to_location_id"`
	FromLocationID *string `json:"from_location_id,omitempty"`
}

// buildMoveIntent constructs the character-move EnvelopeIntent from the read
// character and the command inputs. It carries the service's game id, the mover as
// actor, and a new-values-only payload (from/to location). The intent deliberately
// omits epoch/feed_position/manifest — the writer owns those.
func (s *Service) buildMoveIntent(char *Character, subjectID string, characterID, toLocationID ulid.ULID) (wmodel.EnvelopeIntent, error) {
	p := moveIntentPayload{
		CharacterID:  characterID.String(),
		ToLocationID: toLocationID.String(),
	}
	if char.LocationID != nil {
		from := char.LocationID.String()
		p.FromLocationID = &from
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return wmodel.EnvelopeIntent{}, oops.Wrapf(err, "marshal move intent payload")
	}
	return wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		GameID:        s.gameID,
		Kind:          kindCharacterMoved,
		SchemaVersion: worldSchemaVersion,
		Actor:         subjectID,
		AggregateType: wmodel.AggregateCharacter,
		AggregateID:   characterID,
		Payload:       payload,
	}), nil
}

// The world-layer Examine{Location,Object,Character} commands were removed in
// 05-06: their sole behavior was the post-commit examine emit path (deleted in
// this slice, D-03), they had zero production callers, and an examine is a READ,
// not a world-state change — so it is dropped from the world-change feed (RESEARCH
// Open Question 1). The core-objects plugin owns the player-facing `examine`
// command and its own `object_examine` notification.

// FindLocationByName searches for a location by name after checking read authorization.
// Returns ErrNotFound if no location matches.
func (s *Service) FindLocationByName(ctx context.Context, subjectID, name string) (*Location, error) {
	if s.locationRepo == nil {
		return nil, oops.Code("LOCATION_FIND_FAILED").Errorf("location repository not configured")
	}
	// Check read authorization for location wildcard (searching locations)
	if err := s.checkAccess(ctx, subjectID, "read", access.LocationResource("*"), prefixLocation); err != nil {
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

// GetObjectsByLocation returns objects at a location after checking read authorization.
func (s *Service) GetObjectsByLocation(ctx context.Context, subjectID string, locationID ulid.ULID) ([]*Object, error) {
	if s.objectRepo == nil {
		return nil, oops.Code("OBJECT_QUERY_FAILED").Errorf("object repository not configured")
	}
	resource := access.LocationResource(locationID.String())
	if err := s.checkAccess(ctx, subjectID, "list_objects", resource, prefixLocation); err != nil {
		return nil, err
	}
	objs, err := s.objectRepo.ListAtLocation(ctx, locationID)
	if err != nil {
		return nil, oops.Code("OBJECT_QUERY_FAILED").Wrapf(err, "get objects by location %s", locationID)
	}
	return objs, nil
}

// ListPropertiesByParent returns the subset of properties on the given
// parent that the principal is permitted to read. Implements per-property
// ABAC filtering: fetches all properties from the repository, then
// invokes the access engine once per property with a property-shaped
// resource. Outcomes per property:
//
//   - permit → property included in the returned slice
//   - ErrPermissionDenied (deny decision) → property filtered out SILENTLY
//     (normal case — most properties on another principal default-deny)
//   - ErrAccessEvaluationFailed (engine error, resolver timeout, etc.)
//     → propagate wrapped error, abort the call
//
// Infra failures MUST be visible to callers; silently masking them as
// "no visible properties" would create ghost-data scenarios. Per
// holomush-72ou design spec INV-2 + INV-2b.
func (s *Service) ListPropertiesByParent(ctx context.Context, subjectID, parentType string, parentID ulid.ULID) ([]*EntityProperty, error) {
	if s.propertyRepo == nil {
		return nil, oops.Code("PROPERTY_QUERY_FAILED").Errorf("property repository not configured")
	}
	all, err := s.propertyRepo.ListByParent(ctx, parentType, parentID)
	if err != nil {
		return nil, oops.Code("PROPERTY_QUERY_FAILED").Wrapf(err, "list properties for %s %s", parentType, parentID)
	}
	visible := make([]*EntityProperty, 0, len(all))
	for _, prop := range all {
		resource := access.PropertyResource(prop.ID.String())
		checkErr := s.checkAccess(ctx, subjectID, "read", resource, prefixProperty)
		switch {
		case checkErr == nil:
			visible = append(visible, prop)
		case errors.Is(checkErr, ErrPermissionDenied):
			// Normal default-deny — filter silently. Continue.
		case errors.Is(checkErr, ErrAccessEvaluationFailed):
			// Infra failure — abort the call. INV-2b: no ghost-data.
			return nil, checkErr
		default:
			// Defensive: unrecognized error from checkAccess. Treat as
			// infra failure (fail-closed) and propagate.
			return nil, checkErr
		}
	}
	return visible, nil
}
