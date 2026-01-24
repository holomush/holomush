# World Model Design Improvements Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Address all security, maintainability, and extensibility issues identified in the world model design review.

**Architecture:** Add service layer with ABAC authorization wrapping repositories. Add comprehensive input validation to domain types. Fix transaction safety issues. Improve code organization.

**Tech Stack:** Go 1.23, PostgreSQL, oops error handling, ABAC via internal/access

---

## Phase 1: Foundation - Service Layer & Authorization (P0)

### Task 1.1: Create WorldService with LocationRepository wrapper

**Issue:** holomush-72r

**Files:**

- Create: `internal/world/service.go`
- Create: `internal/world/service_test.go`
- Modify: `internal/world/errors.go` (add ErrPermissionDenied)

**Step 1: Write failing test for permission denied**

```go
// service_test.go
func TestWorldService_DeleteLocation_PermissionDenied(t *testing.T) {
    ctrl := gomock.NewController(t)
    mockAC := mocks.NewMockAccessControl(ctrl)
    mockRepo := worldtest.NewMockLocationRepository(t)

    svc := world.NewWorldService(world.ServiceConfig{
        LocationRepo: mockRepo,
        AccessControl: mockAC,
    })

    mockAC.EXPECT().Check(gomock.Any(), "user-123", "delete", "location:loc-456").Return(false)

    err := svc.DeleteLocation(context.Background(), "user-123", ulid.MustParse("loc-456"))
    assert.ErrorIs(t, err, world.ErrPermissionDenied)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/world/... -run TestWorldService_DeleteLocation_PermissionDenied -v`
Expected: FAIL - WorldService not defined

**Step 3: Define ServiceConfig and WorldService**

```go
// service.go
package world

import (
    "context"
    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"
)

// ErrPermissionDenied is returned when a subject lacks permission for an operation.
var ErrPermissionDenied = errors.New("permission denied")

// AccessControl defines the authorization interface.
type AccessControl interface {
    Check(ctx context.Context, subjectID, action, resource string) bool
}

// ServiceConfig holds dependencies for WorldService.
type ServiceConfig struct {
    LocationRepo  LocationRepository
    ExitRepo      ExitRepository
    ObjectRepo    ObjectRepository
    SceneRepo     SceneRepository
    AccessControl AccessControl
}

// WorldService wraps repositories with authorization checks.
type WorldService struct {
    locationRepo  LocationRepository
    exitRepo      ExitRepository
    objectRepo    ObjectRepository
    sceneRepo     SceneRepository
    ac            AccessControl
}

// NewWorldService creates a new WorldService.
func NewWorldService(cfg ServiceConfig) *WorldService {
    return &WorldService{
        locationRepo:  cfg.LocationRepo,
        exitRepo:      cfg.ExitRepo,
        objectRepo:    cfg.ObjectRepo,
        sceneRepo:     cfg.SceneRepo,
        ac:            cfg.AccessControl,
    }
}

// DeleteLocation removes a location after checking permissions.
func (s *WorldService) DeleteLocation(ctx context.Context, subjectID string, id ulid.ULID) error {
    if !s.ac.Check(ctx, subjectID, "delete", "location:"+id.String()) {
        return oops.Code("PERMISSION_DENIED").With("subject", subjectID).With("location", id.String()).Wrap(ErrPermissionDenied)
    }
    return s.locationRepo.Delete(ctx, id)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/world/... -run TestWorldService_DeleteLocation_PermissionDenied -v`
Expected: PASS

**Step 5: Add tests for other Location operations**

Add tests for: GetLocation, CreateLocation, UpdateLocation, ListLocationsByType

**Step 6: Implement remaining Location service methods**

**Step 7: Commit**

```bash
git add internal/world/service.go internal/world/service_test.go internal/world/errors.go
git commit -m "feat(world): add WorldService with Location authorization"
```

---

### Task 1.2: Add Exit service methods with authorization

**Issue:** holomush-72r (continued)

**Files:**

- Modify: `internal/world/service.go`
- Modify: `internal/world/service_test.go`

**Step 1: Write failing test for CreateExit authorization**

```go
func TestWorldService_CreateExit_ChecksWritePermission(t *testing.T) {
    // Test that creating exit requires write permission on from_location
    mockAC.EXPECT().Check(ctx, subjectID, "write", "location:"+fromLocID.String()).Return(true)
    mockAC.EXPECT().Check(ctx, subjectID, "write", "location:"+toLocID.String()).Return(true)
    // ...
}
```

**Step 2-6:** Implement CreateExit, GetExit, UpdateExit, DeleteExit, ListExitsFromLocation, FindExitByName

**Step 7: Commit**

```bash
git commit -m "feat(world): add Exit service methods with authorization"
```

---

### Task 1.3: Add Object service methods with Move authorization

**Issue:** holomush-72r (continued)

**Files:**

- Modify: `internal/world/service.go`
- Modify: `internal/world/service_test.go`

**Step 1: Write failing test for MoveObject authorization**

```go
func TestWorldService_MoveObject_ChecksTargetPermission(t *testing.T) {
    // Must have write permission on object AND target container/location/character
    mockAC.EXPECT().Check(ctx, subjectID, "write", "object:"+objectID.String()).Return(true)
    mockAC.EXPECT().Check(ctx, subjectID, "write", "location:"+targetLocID.String()).Return(true)
    // ...
}
```

**Step 2-6:** Implement all Object service methods

**Step 7: Commit**

```bash
git commit -m "feat(world): add Object service methods with Move authorization"
```

---

### Task 1.4: Add Scene service methods with role checks

**Issue:** holomush-72r (continued)

**Files:**

- Modify: `internal/world/service.go`
- Modify: `internal/world/service_test.go`

**Step 1: Write test for AddParticipant requires scene ownership or admin**

**Step 2-6:** Implement Scene service methods

**Step 7: Commit**

```bash
git commit -m "feat(world): add Scene service methods with authorization"
```

---

## Phase 2: Input Validation (P1)

### Task 2.1: Add validation types and constants

**Issue:** holomush-b5y

**Files:**

- Create: `internal/world/validation.go`
- Create: `internal/world/validation_test.go`

**Step 1: Write failing test for ValidateName**

```go
func TestValidateName(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"valid", "The Tavern", false},
        {"empty", "", true},
        {"too long", strings.Repeat("a", 101), true},
        {"control chars", "Name\x00", true},
        {"invalid utf8", string([]byte{0xff, 0xfe}), true},
    }
    // ...
}
```

**Step 2: Run test - should fail**

**Step 3: Implement ValidateName**

```go
// validation.go
const (
    MaxNameLength        = 100
    MaxDescriptionLength = 4000
    MaxAliasCount        = 10
    MaxVisibleToCount    = 100
    MaxLockDataSize      = 1024
)

func ValidateName(name string) error {
    if len(name) == 0 {
        return errors.New("name cannot be empty")
    }
    if len(name) > MaxNameLength {
        return fmt.Errorf("name exceeds maximum length of %d", MaxNameLength)
    }
    if !utf8.ValidString(name) {
        return errors.New("name contains invalid UTF-8")
    }
    for _, r := range name {
        if unicode.IsControl(r) && r != '\n' && r != '\t' {
            return errors.New("name contains forbidden control characters")
        }
    }
    return nil
}
```

**Step 4: Run test - should pass**

**Step 5: Add tests for ValidateDescription, ValidateAliases, ValidateLockData, ValidateVisibleTo**

**Step 6: Implement remaining validation functions**

**Step 7: Commit**

```bash
git commit -m "feat(world): add input validation functions"
```

---

### Task 2.2: Add ParticipantRole type with validation

**Issue:** holomush-7rz

**Files:**

- Modify: `internal/world/repository.go`
- Create: `internal/world/scene.go`
- Create: `internal/world/scene_test.go`
- Modify: `internal/world/postgres/scene_repo.go`

**Step 1: Write failing test for ParticipantRole.Validate**

```go
func TestParticipantRole_Validate(t *testing.T) {
    tests := []struct {
        role    world.ParticipantRole
        wantErr bool
    }{
        {world.ParticipantRoleOwner, false},
        {world.ParticipantRoleMember, false},
        {world.ParticipantRoleInvited, false},
        {world.ParticipantRole("admin"), true},
        {world.ParticipantRole(""), true},
    }
    // ...
}
```

**Step 2: Implement ParticipantRole type**

```go
// scene.go
type ParticipantRole string

const (
    ParticipantRoleOwner   ParticipantRole = "owner"
    ParticipantRoleMember  ParticipantRole = "member"
    ParticipantRoleInvited ParticipantRole = "invited"
)

func (r ParticipantRole) Validate() error {
    switch r {
    case ParticipantRoleOwner, ParticipantRoleMember, ParticipantRoleInvited:
        return nil
    default:
        return fmt.Errorf("invalid participant role: %s", r)
    }
}
```

**Step 3: Update SceneRepository interface**

```go
// repository.go
AddParticipant(ctx context.Context, sceneID, characterID ulid.ULID, role ParticipantRole) error
```

**Step 4: Update SceneRepository implementation to validate**

**Step 5: Update tests**

**Step 6: Commit**

```bash
git commit -m "feat(world): add ParticipantRole type with validation"
```

---

### Task 2.3: Add LocationType validation

**Issue:** holomush-4vm

**Files:**

- Modify: `internal/world/location.go`
- Modify: `internal/world/location_test.go`

**Step 1: Write failing test**

```go
func TestLocationType_Validate(t *testing.T) {
    assert.NoError(t, world.LocationTypePersistent.Validate())
    assert.NoError(t, world.LocationTypeScene.Validate())
    assert.NoError(t, world.LocationTypeInstance.Validate())
    assert.Error(t, world.LocationType("invalid").Validate())
}
```

**Step 2: Implement Validate method**

**Step 3: Run tests**

**Step 4: Commit**

```bash
git commit -m "feat(world): add LocationType.Validate method"
```

---

### Task 2.4: Integrate validation into domain types

**Issue:** holomush-b5y (continued)

**Files:**

- Modify: `internal/world/location.go`
- Modify: `internal/world/exit.go`
- Modify: `internal/world/object.go`

**Step 1: Add Location.Validate method**

```go
func (l *Location) Validate() error {
    if err := ValidateName(l.Name); err != nil {
        return fmt.Errorf("location name: %w", err)
    }
    if err := ValidateDescription(l.Description); err != nil {
        return fmt.Errorf("location description: %w", err)
    }
    if err := l.Type.Validate(); err != nil {
        return err
    }
    return nil
}
```

**Step 2: Add Exit.Validate, Object.Validate methods**

**Step 3: Call validation in service layer Create/Update methods**

**Step 4: Commit**

```bash
git commit -m "feat(world): integrate validation into domain types"
```

---

## Phase 3: Transaction Safety (P1)

### Task 3.1: Make bidirectional exit deletion transactional

**Issue:** holomush-pfc

**Files:**

- Modify: `internal/world/postgres/exit_repo.go`
- Modify: `internal/world/postgres/exit_repo_test.go` (integration)

**Step 1: Write failing integration test**

```go
func TestExitRepository_Delete_BidirectionalAtomic(t *testing.T) {
    // Create bidirectional exit
    // Delete primary - verify both deleted in single transaction
    // Verify no orphaned return exit on rollback
}
```

**Step 2: Refactor Delete to use transaction**

```go
func (r *ExitRepository) Delete(ctx context.Context, id ulid.ULID) error {
    tx, err := r.pool.Begin(ctx)
    if err != nil {
        return oops.With("operation", "begin transaction").Wrap(err)
    }
    defer tx.Rollback(ctx)

    // Get exit info within transaction
    exit, err := r.getExitTx(ctx, tx, id)
    // ...

    // Delete both exits in same transaction
    // ...

    return tx.Commit(ctx)
}
```

**Step 3: Run integration tests**

**Step 4: Commit**

```bash
git commit -m "fix(world): make bidirectional exit deletion transactional"
```

---

### Task 3.2: Fix TOCTOU in Object.Move with SELECT FOR UPDATE

**Issue:** holomush-24f

**Files:**

- Modify: `internal/world/postgres/object_repo.go`

**Step 1: Write test demonstrating race condition fix**

**Step 2: Add FOR UPDATE to container validation query**

```go
err = tx.QueryRow(ctx, `
    SELECT is_container FROM objects WHERE id = $1 FOR UPDATE
`, to.ObjectID.String()).Scan(&isContainer)
```

**Step 3: Run tests**

**Step 4: Commit**

```bash
git commit -m "fix(world): add SELECT FOR UPDATE to prevent Move TOCTOU"
```

---

## Phase 4: Clean Architecture Fixes (P0-P1)

### Task 4.1: Remove internal/core dependency from postgres layer

**Issue:** holomush-ipz
**Depends on:** holomush-72r (service layer must exist first)

**Files:**

- Modify: `internal/world/postgres/exit_repo.go`
- Modify: `internal/world/service.go`

**Step 1: Move ID generation to service layer**

```go
// service.go
func (s *WorldService) CreateExit(ctx context.Context, subjectID string, exit *Exit) error {
    // Generate IDs in service layer
    if exit.ID.IsZero() {
        exit.ID = ulid.Make()
    }
    // ... authorization checks ...
    return s.exitRepo.Create(ctx, exit)
}
```

**Step 2: Update exit_repo.go to require caller-provided IDs**

**Step 3: Remove `internal/core` import from exit_repo.go**

**Step 4: Run tests**

**Step 5: Commit**

```bash
git commit -m "refactor(world): move ID generation to service layer"
```

---

### Task 4.2: Replace slog logging with structured errors

**Issue:** holomush-26l
**Depends on:** holomush-72r

**Files:**

- Create: `internal/world/postgres/errors.go` (add BidirectionalCleanupError)
- Modify: `internal/world/postgres/exit_repo.go`
- Modify: `internal/world/service.go` (handle error and log)

**Step 1: Define structured error type**

```go
// errors.go
type BidirectionalCleanupError struct {
    PrimaryExitID ulid.ULID
    ReturnExitID  ulid.ULID
    Cause         error
}

func (e *BidirectionalCleanupError) Error() string {
    return fmt.Sprintf("failed to cleanup return exit %s after deleting primary %s: %v",
        e.ReturnExitID, e.PrimaryExitID, e.Cause)
}

func (e *BidirectionalCleanupError) Unwrap() error {
    return e.Cause
}
```

**Step 2: Update Delete to return error instead of logging**

**Step 3: Handle error in service layer**

```go
// service.go
func (s *WorldService) DeleteExit(ctx context.Context, subjectID string, id ulid.ULID) error {
    // ... authorization ...
    err := s.exitRepo.Delete(ctx, id)

    var cleanupErr *postgres.BidirectionalCleanupError
    if errors.As(err, &cleanupErr) {
        // Log at service layer
        slog.Error("orphaned return exit after deletion",
            "primary_exit", cleanupErr.PrimaryExitID,
            "return_exit", cleanupErr.ReturnExitID,
            "error", cleanupErr.Cause)
        // Still return nil - primary delete succeeded
        return nil
    }
    return err
}
```

**Step 4: Remove slog import from exit_repo.go**

**Step 5: Commit**

```bash
git commit -m "refactor(world): replace slog with structured errors in exit_repo"
```

---

## Phase 5: Domain Invariants (P2)

### Task 5.1: Enforce invariants in Object.SetContainment

**Issue:** holomush-cgc

**Files:**

- Modify: `internal/world/object.go`
- Modify: `internal/world/object_test.go`

**Step 1: Write test for SetContainment validation**

```go
func TestObject_SetContainment_ValidatesInput(t *testing.T) {
    obj := &world.Object{ID: ulid.Make()}

    // Valid containment
    locID := ulid.Make()
    err := obj.SetContainment(world.Containment{LocationID: &locID})
    assert.NoError(t, err)

    // Invalid containment (none set)
    err = obj.SetContainment(world.Containment{})
    assert.Error(t, err)

    // Invalid containment (multiple set)
    charID := ulid.Make()
    err = obj.SetContainment(world.Containment{LocationID: &locID, CharacterID: &charID})
    assert.Error(t, err)
}
```

**Step 2: Make SetContainment return error**

```go
func (o *Object) SetContainment(c Containment) error {
    if err := c.Validate(); err != nil {
        return err
    }
    o.LocationID = c.LocationID
    o.HeldByCharacterID = c.CharacterID
    o.ContainedInObjectID = c.ObjectID
    return nil
}
```

**Step 3: Update callers**

**Step 4: Commit**

```bash
git commit -m "feat(world): enforce containment invariants in SetContainment"
```

---

### Task 5.2: Make exit visibility check atomic

**Issue:** holomush-077
**Depends on:** holomush-72r

**Files:**

- Modify: `internal/world/postgres/exit_repo.go`
- Modify: `internal/world/repository.go`

**Step 1: Add IsVisibleToCharacter to ExitRepository interface**

```go
// IsVisibleToCharacter checks if a character can see an exit.
// Fetches location owner atomically to prevent spoofing.
IsVisibleToCharacter(ctx context.Context, exitID, characterID ulid.ULID) (bool, error)
```

**Step 2: Implement with atomic query**

```go
func (r *ExitRepository) IsVisibleToCharacter(ctx context.Context, exitID, characterID ulid.ULID) (bool, error) {
    var visibility string
    var locationOwnerID *string
    var visibleTo []string

    err := r.pool.QueryRow(ctx, `
        SELECT e.visibility, l.owner_id, e.visible_to
        FROM exits e
        JOIN locations l ON e.from_location_id = l.id
        WHERE e.id = $1
    `, exitID.String()).Scan(&visibility, &locationOwnerID, &visibleTo)
    // ...
}
```

**Step 3: Commit**

```bash
git commit -m "feat(world): add atomic IsVisibleToCharacter check"
```

---

### Task 5.3: Make maxNestingDepth configurable

**Issue:** holomush-9sm

**Files:**

- Modify: `internal/world/postgres/object_repo.go`

**Step 1: Write test for configurable depth**

**Step 2: Add field to ObjectRepository**

```go
type ObjectRepository struct {
    pool            *pgxpool.Pool
    maxNestingDepth int
}

func NewObjectRepository(pool *pgxpool.Pool, opts ...ObjectRepoOption) *ObjectRepository {
    r := &ObjectRepository{pool: pool, maxNestingDepth: DefaultMaxNestingDepth}
    for _, opt := range opts {
        opt(r)
    }
    return r
}

func WithMaxNestingDepth(depth int) ObjectRepoOption {
    return func(r *ObjectRepository) {
        r.maxNestingDepth = depth
    }
}
```

**Step 3: Update checkNestingDepthTx to use configurable value**

**Step 4: Commit**

```bash
git commit -m "feat(world): make object nesting depth configurable"
```

---

## Phase 6: Code Organization (P3-P4)

### Task 6.1: Reduce scan helper duplication

**Issue:** holomush-s0u

**Files:**

- Modify: `internal/world/postgres/location_repo.go`
- Modify: `internal/world/postgres/object_repo.go`

**Step 1: Extract scanLocation helper from scanLocations**

**Step 2: Use in Get() method**

**Step 3: Same for Object**

**Step 4: Commit**

```bash
git commit -m "refactor(world): extract scan helpers to reduce duplication"
```

---

### Task 6.2: Move reusable helpers to helpers.go

**Issue:** holomush-co6

**Files:**

- Modify: `internal/world/postgres/helpers.go`
- Modify: `internal/world/postgres/exit_repo.go`

**Step 1: Move ulidsToStrings, stringsToULIDs, nullableString, etc.**

**Step 2: Update imports in exit_repo.go**

**Step 3: Commit**

```bash
git commit -m "refactor(world): consolidate helpers in helpers.go"
```

---

### Task 6.3: Extract CTE recursion depth constant

**Issue:** holomush-voc

**Files:**

- Modify: `internal/world/postgres/object_repo.go`

**Step 1: Add constant**

```go
const maxCTERecursionDepth = 100
```

**Step 2: Replace hardcoded 100 with constant**

**Step 3: Commit**

```bash
git commit -m "refactor(world): extract CTE recursion depth constant"
```

---

### Task 6.4: Rename FindByNameFuzzy to database-agnostic name

**Issue:** holomush-dop

**Files:**

- Modify: `internal/world/repository.go`
- Modify: `internal/world/postgres/exit_repo.go`
- Modify: all callers

**Step 1: Rename to FindExitBySimilarity**

**Step 2: Update all references**

**Step 3: Commit**

```bash
git commit -m "refactor(world): rename FindByNameFuzzy to FindExitBySimilarity"
```

---

## Post-Implementation Checklist

- [ ] All tests pass: `task test`
- [ ] Lint passes: `task lint`
- [ ] Coverage >80%: `task test:coverage`
- [ ] Update design spec with service layer
- [ ] Update CLAUDE.md if new patterns introduced
- [ ] Close beads issues: `bd close holomush-72r holomush-ipz ...`
- [ ] Create PR for review

---

## Issue Summary

| Issue ID     | Title                                     | Priority | Phase |
| ------------ | ----------------------------------------- | -------- | ----- |
| holomush-72r | Add service layer with ABAC authorization | P0       | 1     |
| holomush-ipz | Remove internal/core dependency           | P0       | 4     |
| holomush-26l | Replace slog with structured errors       | P1       | 4     |
| holomush-b5y | Add input validation                      | P1       | 2     |
| holomush-pfc | Bidirectional exit deletion transactional | P1       | 3     |
| holomush-7rz | Add ParticipantRole type                  | P1       | 2     |
| holomush-cgc | Enforce Object.SetContainment invariants  | P2       | 5     |
| holomush-077 | Atomic visibility check                   | P2       | 5     |
| holomush-9sm | Configurable maxNestingDepth              | P2       | 5     |
| holomush-24f | Fix Move TOCTOU                           | P2       | 3     |
| holomush-4vm | Add LocationType validation               | P2       | 2     |
| holomush-s0u | Reduce scan helper duplication            | P3       | 6     |
| holomush-co6 | Move helpers to helpers.go                | P3       | 6     |
| holomush-voc | Extract CTE recursion constant            | P4       | 6     |
| holomush-dop | Rename FindByNameFuzzy                    | P4       | 6     |
