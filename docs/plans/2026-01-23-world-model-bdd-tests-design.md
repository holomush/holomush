# World Model BDD Integration Tests Design

**Status:** Draft
**Date:** 2026-01-23
**Epic:** holomush-x2t (Epic 4: World Model)

## Overview

This document defines BDD-style integration tests for the world model using Ginkgo/Gomega. The tests provide feature-level acceptance specs that read like user stories, plus repository-level specs for data layer coverage.

### Goals

- Comprehensive BDD specs for all world model use cases
- Feature-level tests that document system behavior
- Repository-level tests for data layer coverage
- Clear separation from unit tests (which remain table-driven)

### Non-Goals

- Replacing existing unit tests in `internal/world/`
- End-to-end tests involving gRPC/telnet protocols
- Performance or load testing

## Directory Structure

```text
test/integration/world/
  world_suite_test.go       # Ginkgo suite setup, testcontainers, shared helpers

  # Repository-level specs (data layer)
  location_repo_test.go     # LocationRepository BDD specs
  exit_repo_test.go         # ExitRepository BDD specs
  object_repo_test.go       # ObjectRepository BDD specs
  scene_repo_test.go        # SceneRepository BDD specs

  # Feature-level specs (use case layer)
  movement_test.go          # Character/object movement through world
  objects_test.go           # Object pickup, drop, containers, giving
  scenes_test.go            # Scene creation, shadowing, participants
  locations_test.go         # Location CRUD, types, replay policy
```

## Test Infrastructure

### Suite Setup (`world_suite_test.go`)

```go
//go:build integration

package world_test

import (
    "context"
    "testing"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "github.com/testcontainers/testcontainers-go/wait"

    "github.com/holomush/holomush/internal/store"
    worldpg "github.com/holomush/holomush/internal/world/postgres"
)

func TestWorld(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "World Model Integration Suite")
}

type testEnv struct {
    ctx        context.Context
    pool       *pgxpool.Pool
    container  testcontainers.Container
    eventStore *store.PostgresEventStore

    // Repositories
    Locations worldpg.LocationRepository
    Exits     worldpg.ExitRepository
    Objects   worldpg.ObjectRepository
    Scenes    worldpg.SceneRepository
}

var env *testEnv

var _ = BeforeSuite(func() {
    var err error
    env, err = setupWorldTestEnv()
    Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
    env.cleanup()
})

func setupWorldTestEnv() (*testEnv, error) {
    ctx := context.Background()

    container, err := postgres.Run(ctx,
        "postgres:18-alpine",
        postgres.WithDatabase("holomush_test"),
        postgres.WithUsername("holomush"),
        postgres.WithPassword("holomush"),
        testcontainers.WithWaitStrategy(
            wait.ForLog("database system is ready to accept connections").
                WithOccurrence(2).
                WithStartupTimeout(30*time.Second),
        ),
    )
    if err != nil {
        return nil, err
    }

    connStr, err := container.ConnectionString(ctx, "sslmode=disable")
    if err != nil {
        return nil, err
    }

    eventStore, err := store.NewPostgresEventStore(ctx, connStr)
    if err != nil {
        return nil, err
    }

    if err := eventStore.Migrate(ctx); err != nil {
        return nil, err
    }

    pool, err := pgxpool.New(ctx, connStr)
    if err != nil {
        return nil, err
    }

    return &testEnv{
        ctx:        ctx,
        pool:       pool,
        container:  container,
        eventStore: eventStore,
        Locations:  worldpg.NewLocationRepository(pool),
        Exits:      worldpg.NewExitRepository(pool),
        Objects:    worldpg.NewObjectRepository(pool),
        Scenes:     worldpg.NewSceneRepository(pool),
    }, nil
}

func (e *testEnv) cleanup() {
    if e.pool != nil {
        e.pool.Close()
    }
    if e.eventStore != nil {
        e.eventStore.Close()
    }
    if e.container != nil {
        _ = e.container.Terminate(e.ctx)
    }
}
```

## Feature Specs

### Movement (`movement_test.go`)

```go
var _ = Describe("Character Movement", func() {
    Describe("Moving through exits", func() {
        It("moves character from origin to destination via exit", func() {})
        It("updates character's location_id in database", func() {})
        It("generates move event with correct payload", func() {})
    })

    Describe("Bidirectional exits", func() {
        It("allows movement in both directions", func() {})
        It("uses return_name for the reverse direction", func() {})
    })

    Describe("Exit visibility", func() {
        Context("when visibility is 'all'", func() {
            It("shows exit to any character", func() {})
        })
        Context("when visibility is 'owner'", func() {
            It("shows exit only to location owner", func() {})
            It("hides exit from non-owners", func() {})
        })
        Context("when visibility is 'list'", func() {
            It("shows exit only to characters in visible_to list", func() {})
        })
    })

    Describe("Locked exits", func() {
        Context("with key lock", func() {
            It("blocks movement without key object", func() {})
            It("allows movement when character holds key", func() {})
        })
        Context("with password lock", func() {
            It("blocks movement without correct password", func() {})
        })
    })

    Describe("Exit name matching", func() {
        It("matches by exact name (case-insensitive)", func() {})
        It("matches by alias", func() {})
        It("matches by fuzzy search with threshold", func() {})
    })
})
```

### Object Handling (`objects_test.go`)

```go
var _ = Describe("Object Handling", func() {
    Describe("Picking up objects", func() {
        It("moves object from location to character inventory", func() {})
        It("generates move event with entity_type 'object'", func() {})
        It("fails when object is in another character's inventory", func() {})
    })

    Describe("Dropping objects", func() {
        It("moves object from character inventory to location", func() {})
        It("places object at character's current location", func() {})
    })

    Describe("Container operations", func() {
        Context("putting objects in containers", func() {
            It("moves object into container object", func() {})
            It("fails when target is not a container", func() {})
            It("fails when exceeding max nesting depth (3)", func() {})
        })

        Context("taking objects from containers", func() {
            It("moves object from container to character", func() {})
        })

        Context("circular containment prevention", func() {
            It("prevents putting container inside itself", func() {})
            It("prevents A->B->A circular chains", func() {})
            It("prevents deep circular chains A->B->C->A", func() {})
        })
    })

    Describe("Giving objects", func() {
        It("transfers object between characters", func() {})
        It("generates object_give event", func() {})
        It("requires both characters at same location", func() {})
    })

    Describe("Containment invariants", func() {
        It("ensures object is in exactly one place", func() {})
        It("clears previous containment when moving", func() {})
    })
})
```

### Scene Management (`scenes_test.go`)

```go
var _ = Describe("Scene Management", func() {
    Describe("Creating scenes", func() {
        It("creates scene with type 'scene'", func() {})
        It("sets default replay policy to 'last:-1' (full history)", func() {})
        It("assigns creator as scene owner", func() {})
    })

    Describe("Scene shadowing", func() {
        Context("when shadowing a persistent location", func() {
            It("inherits name from parent when scene name is empty", func() {})
            It("inherits description from parent when empty", func() {})
            It("uses own name/description when provided (override)", func() {})
        })

        It("lists all scenes shadowing a location via GetShadowedBy", func() {})
    })

    Describe("Scene isolation", func() {
        It("scene has separate event stream from shadowed location", func() {})
        It("events in scene don't appear in parent location stream", func() {})
        It("events in parent don't appear in scene stream", func() {})
    })

    Describe("Scene participants", func() {
        It("adds participant with 'member' role by default", func() {})
        It("supports 'owner', 'member', 'invited' roles", func() {})
        It("removes participant from scene", func() {})
        It("lists all participants in a scene", func() {})
        It("lists all scenes a character participates in", func() {})
    })

    Describe("Scene lifecycle", func() {
        It("archives scene by setting archived_at", func() {})
        It("excludes archived scenes from active queries", func() {})
    })
})
```

### Location Management (`locations_test.go`)

```go
var _ = Describe("Location Management", func() {
    Describe("Location types", func() {
        It("creates persistent locations (permanent world rooms)", func() {})
        It("creates scene locations (temporary RP rooms)", func() {})
        It("creates instance locations (future instanced content)", func() {})
    })

    Describe("Replay policy", func() {
        Context("persistent locations", func() {
            It("defaults to 'last:0' (no replay)", func() {})
        })
        Context("scene locations", func() {
            It("defaults to 'last:-1' (full history)", func() {})
        })
        It("parses 'last:N' format correctly", func() {})
        It("supports 'last:10', 'last:50' for limited replay", func() {})
    })

    Describe("Location CRUD", func() {
        It("creates location with name and description", func() {})
        It("retrieves location by ID", func() {})
        It("updates location properties", func() {})
        It("deletes location and cascades to exits", func() {})
        It("returns ErrNotFound for nonexistent location", func() {})
    })

    Describe("Location queries", func() {
        It("lists all locations by type", func() {})
        It("orders results by created_at descending", func() {})
    })

    Describe("Location ownership", func() {
        It("tracks owner_id for builder permissions", func() {})
        It("allows nil owner for system-created locations", func() {})
    })
})
```

## Repository Specs

### Location Repository (`location_repo_test.go`)

```go
var _ = Describe("LocationRepository", func() {
    Describe("Create", func() {
        It("persists all location fields", func() {})
        It("handles nil optional fields (owner_id, shadows_id)", func() {})
        It("sets created_at timestamp", func() {})
    })

    Describe("Get", func() {
        It("retrieves location with all fields", func() {})
        It("returns ErrNotFound for missing ID", func() {})
    })

    Describe("Update", func() {
        It("updates mutable fields", func() {})
        It("returns ErrNotFound for missing ID", func() {})
    })

    Describe("Delete", func() {
        It("removes location from database", func() {})
        It("returns ErrNotFound for missing ID", func() {})
    })

    Describe("ListByType", func() {
        It("returns only locations of specified type", func() {})
        It("returns empty slice when no matches", func() {})
    })

    Describe("GetShadowedBy", func() {
        It("returns scenes that shadow the location", func() {})
        It("returns empty slice when no shadows", func() {})
    })
})
```

### Exit Repository (`exit_repo_test.go`)

```go
var _ = Describe("ExitRepository", func() {
    Describe("Create", func() {
        It("creates exit with all fields", func() {})
        It("auto-creates return exit when bidirectional", func() {})
        It("persists aliases as array", func() {})
        It("persists lock_data as JSONB", func() {})
        It("persists visible_to as array", func() {})
    })

    Describe("Delete", func() {
        It("removes exit from database", func() {})
        It("removes return exit when bidirectional", func() {})
        It("handles missing return exit gracefully", func() {})
    })

    Describe("FindByName", func() {
        It("matches exact name case-insensitively", func() {})
        It("matches aliases", func() {})
        It("returns ErrNotFound when no match", func() {})
    })

    Describe("FindByNameFuzzy", func() {
        It("returns matches above threshold", func() {})
        It("orders by similarity descending", func() {})
        It("validates threshold bounds (0.0-1.0)", func() {})
    })

    Describe("ListFromLocation", func() {
        It("returns all exits from location", func() {})
        It("orders by name", func() {})
    })
})
```

### Object Repository (`object_repo_test.go`)

```go
var _ = Describe("ObjectRepository", func() {
    Describe("Create", func() {
        It("creates object with containment", func() {})
        It("validates containment (exactly one set)", func() {})
    })

    Describe("Move", func() {
        It("updates containment atomically", func() {})
        It("validates target is container when moving to object", func() {})
        It("enforces max nesting depth", func() {})
        It("prevents circular containment", func() {})
        It("prevents self-containment", func() {})
    })

    Describe("ListAtLocation", func() {
        It("returns objects at location", func() {})
        It("returns empty slice for empty location", func() {})
    })

    Describe("ListHeldBy", func() {
        It("returns objects held by character", func() {})
    })

    Describe("ListContainedIn", func() {
        It("returns objects inside container", func() {})
    })
})
```

### Scene Repository (`scene_repo_test.go`)

```go
var _ = Describe("SceneRepository", func() {
    Describe("AddParticipant", func() {
        It("adds character to scene with role", func() {})
        It("updates role if already participant", func() {})
    })

    Describe("RemoveParticipant", func() {
        It("removes character from scene", func() {})
    })

    Describe("ListParticipants", func() {
        It("returns all participants with roles", func() {})
    })

    Describe("GetScenesFor", func() {
        It("returns scenes character participates in", func() {})
    })
})
```

## Running Tests

```bash
# Run all world model integration tests
task test:integration -- -v ./test/integration/world/...

# Run specific feature
task test:integration -- -v ./test/integration/world/... -ginkgo.focus="Movement"

# Run with verbose output
task test:integration -- -v ./test/integration/world/... -ginkgo.v
```

## Test Data Strategy

- Each `Describe` block creates its own test fixtures in `BeforeEach`
- Fixtures are cleaned up in `AfterEach` or via database truncation
- Helper functions create common fixtures:
  - `createTestLocation(name string) *world.Location`
  - `createTestExit(from, to ulid.ULID, name string) *world.Exit`
  - `createTestObject(name string, containment world.Containment) *world.Object`
  - `createTestCharacter(name string) ulid.ULID`

## Acceptance Criteria

- [ ] All feature specs pass against Postgres 18
- [ ] All repository specs pass against Postgres 18
- [ ] Tests run in CI via `task test:integration`
- [ ] No flaky tests (deterministic ordering, proper cleanup)
- [ ] Coverage complements existing unit tests

## References

- [World Model Design Spec](../specs/2026-01-22-world-model-design.md)
- [Epic 4 Implementation Plan](2026-01-22-epic-4-world-model-implementation.md)
- [Ginkgo Documentation](https://onsi.github.io/ginkgo/)
