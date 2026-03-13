# World Model BDD Integration Tests Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement BDD-style integration tests for the world model using Ginkgo/Gomega.

**Architecture:** Suite setup with testcontainers for Postgres 18, shared test environment with repositories, helper functions for fixtures, separate spec files per domain.

**Tech Stack:** Go 1.23+, Ginkgo v2, Gomega, testcontainers-go, pgx/v5

---

## Phase 1: Test Infrastructure

### Task 1.1: Create Suite Setup

**Files:**

- Create: `test/integration/world/world_suite_test.go`

**Step 1: Create directory and suite file**

Create `test/integration/world/world_suite_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
    "context"
    "testing"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/oklog/ulid/v2"
    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega"    //nolint:revive // gomega convention
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "github.com/testcontainers/testcontainers-go/wait"

    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/internal/store"
    "github.com/holomush/holomush/internal/world"
    worldpg "github.com/holomush/holomush/internal/world/postgres"
)

func TestWorld(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "World Model Integration Suite")
}

// testEnv holds all resources needed for integration tests.
type testEnv struct {
    ctx        context.Context
    pool       *pgxpool.Pool
    container  testcontainers.Container
    eventStore *store.PostgresEventStore

    // Repositories
    Locations *worldpg.LocationRepository
    Exits     *worldpg.ExitRepository
    Objects   *worldpg.ObjectRepository
    Scenes    *worldpg.SceneRepository
}

var env *testEnv

var _ = BeforeSuite(func() {
    var err error
    env, err = setupWorldTestEnv()
    Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
    if env != nil {
        env.cleanup()
    }
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
        _ = container.Terminate(ctx)
        return nil, err
    }

    eventStore, err := store.NewPostgresEventStore(ctx, connStr)
    if err != nil {
        _ = container.Terminate(ctx)
        return nil, err
    }

    if err := eventStore.Migrate(ctx); err != nil {
        eventStore.Close()
        _ = container.Terminate(ctx)
        return nil, err
    }

    pool, err := pgxpool.New(ctx, connStr)
    if err != nil {
        eventStore.Close()
        _ = container.Terminate(ctx)
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

// Helper functions for creating test fixtures

func createTestLocation(name, description string, locType world.LocationType) *world.Location {
    return &world.Location{
        ID:           core.NewULID(),
        Type:         locType,
        Name:         name,
        Description:  description,
        ReplayPolicy: world.DefaultReplayPolicy(locType),
        CreatedAt:    time.Now(),
    }
}

func createTestExit(fromID, toID ulid.ULID, name string) *world.Exit {
    return &world.Exit{
        ID:             core.NewULID(),
        FromLocationID: fromID,
        ToLocationID:   toID,
        Name:           name,
        Bidirectional:  false,
        Visibility:     world.VisibilityAll,
        CreatedAt:      time.Now(),
    }
}

func createTestObject(name, description string, containment world.Containment) *world.Object {
    return &world.Object{
        ID:                  core.NewULID(),
        Name:                name,
        Description:         description,
        LocationID:          containment.LocationID,
        HeldByCharacterID:   containment.CharacterID,
        ContainedInObjectID: containment.ObjectID,
        IsContainer:         false,
        CreatedAt:           time.Now(),
    }
}

// createTestCharacterID creates a fake character ID for testing.
// Note: This doesn't create a real character in the database.
func createTestCharacterID() ulid.ULID {
    return core.NewULID()
}

// cleanupLocations removes all locations from the test database.
func cleanupLocations(ctx context.Context, pool *pgxpool.Pool) {
    _, _ = pool.Exec(ctx, "DELETE FROM exits")
    _, _ = pool.Exec(ctx, "DELETE FROM objects")
    _, _ = pool.Exec(ctx, "DELETE FROM scene_participants")
    _, _ = pool.Exec(ctx, "DELETE FROM locations")
}
```

**Step 2: Run test to verify suite compiles**

Run: `task test:integration -- -v ./test/integration/world/... -ginkgo.v`

Expected: Suite runs with 0 specs (no specs defined yet)

**Step 3: Commit**

```bash
git add test/integration/world/world_suite_test.go
git commit -m "feat(world): add BDD integration test suite setup"
```

---

## Phase 2: Repository BDD Specs

### Task 2.1: Location Repository Specs

**Files:**

- Create: `test/integration/world/location_repo_test.go`

**Step 1: Write the spec file**

Create `test/integration/world/location_repo_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
    "context"
    "time"

    "github.com/oklog/ulid/v2"
    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega"    //nolint:revive // gomega convention

    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/internal/world"
    "github.com/holomush/holomush/internal/world/postgres"
)

var _ = Describe("LocationRepository", func() {
    var ctx context.Context

    BeforeEach(func() {
        ctx = context.Background()
        cleanupLocations(ctx, env.pool)
    })

    Describe("Create", func() {
        It("persists all location fields", func() {
            loc := createTestLocation("Test Room", "A room for testing.", world.LocationTypePersistent)

            err := env.Locations.Create(ctx, loc)
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Locations.Get(ctx, loc.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.Name).To(Equal("Test Room"))
            Expect(got.Description).To(Equal("A room for testing."))
            Expect(got.Type).To(Equal(world.LocationTypePersistent))
            Expect(got.ReplayPolicy).To(Equal("last:0"))
        })

        It("handles nil optional fields (owner_id, shadows_id)", func() {
            loc := createTestLocation("No Owner", "A room without owner.", world.LocationTypePersistent)
            loc.OwnerID = nil
            loc.ShadowsID = nil

            err := env.Locations.Create(ctx, loc)
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Locations.Get(ctx, loc.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.OwnerID).To(BeNil())
            Expect(got.ShadowsID).To(BeNil())
        })

        It("sets created_at timestamp", func() {
            before := time.Now().Add(-time.Second)
            loc := createTestLocation("Timed Room", "Testing timestamps.", world.LocationTypePersistent)

            err := env.Locations.Create(ctx, loc)
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Locations.Get(ctx, loc.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.CreatedAt).To(BeTemporally(">=", before))
        })
    })

    Describe("Get", func() {
        It("retrieves location with all fields", func() {
            ownerID := createTestCharacterID()
            loc := createTestLocation("Full Room", "All fields set.", world.LocationTypeScene)
            loc.OwnerID = &ownerID
            loc.ReplayPolicy = "last:-1"

            err := env.Locations.Create(ctx, loc)
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Locations.Get(ctx, loc.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.ID).To(Equal(loc.ID))
            Expect(got.Name).To(Equal("Full Room"))
            Expect(got.Type).To(Equal(world.LocationTypeScene))
            Expect(got.OwnerID).NotTo(BeNil())
            Expect(*got.OwnerID).To(Equal(ownerID))
        })

        It("returns ErrNotFound for missing ID", func() {
            _, err := env.Locations.Get(ctx, ulid.Make())
            Expect(err).To(HaveOccurred())
            Expect(err).To(MatchError(ContainSubstring("not found")))
        })
    })

    Describe("Update", func() {
        It("updates mutable fields", func() {
            loc := createTestLocation("Original", "Original description.", world.LocationTypePersistent)
            err := env.Locations.Create(ctx, loc)
            Expect(err).NotTo(HaveOccurred())

            loc.Name = "Updated"
            loc.Description = "Updated description."
            err = env.Locations.Update(ctx, loc)
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Locations.Get(ctx, loc.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.Name).To(Equal("Updated"))
            Expect(got.Description).To(Equal("Updated description."))
        })

        It("returns ErrNotFound for missing ID", func() {
            loc := createTestLocation("Ghost", "Doesn't exist.", world.LocationTypePersistent)
            err := env.Locations.Update(ctx, loc)
            Expect(err).To(HaveOccurred())
            Expect(err).To(MatchError(ContainSubstring("not found")))
        })
    })

    Describe("Delete", func() {
        It("removes location from database", func() {
            loc := createTestLocation("To Delete", "Will be deleted.", world.LocationTypePersistent)
            err := env.Locations.Create(ctx, loc)
            Expect(err).NotTo(HaveOccurred())

            err = env.Locations.Delete(ctx, loc.ID)
            Expect(err).NotTo(HaveOccurred())

            _, err = env.Locations.Get(ctx, loc.ID)
            Expect(err).To(MatchError(ContainSubstring("not found")))
        })

        It("returns ErrNotFound for missing ID", func() {
            err := env.Locations.Delete(ctx, ulid.Make())
            Expect(err).To(HaveOccurred())
            Expect(err).To(MatchError(ContainSubstring("not found")))
        })
    })

    Describe("ListByType", func() {
        BeforeEach(func() {
            // Create mix of location types
            persistent := createTestLocation("Persistent", "Persistent room.", world.LocationTypePersistent)
            scene := createTestLocation("Scene", "Scene room.", world.LocationTypeScene)

            Expect(env.Locations.Create(ctx, persistent)).To(Succeed())
            Expect(env.Locations.Create(ctx, scene)).To(Succeed())
        })

        It("returns only locations of specified type", func() {
            scenes, err := env.Locations.ListByType(ctx, world.LocationTypeScene)
            Expect(err).NotTo(HaveOccurred())
            Expect(scenes).To(HaveLen(1))
            Expect(scenes[0].Type).To(Equal(world.LocationTypeScene))
        })

        It("returns empty slice when no matches", func() {
            instances, err := env.Locations.ListByType(ctx, world.LocationTypeInstance)
            Expect(err).NotTo(HaveOccurred())
            Expect(instances).To(BeEmpty())
        })
    })

    Describe("GetShadowedBy", func() {
        It("returns scenes that shadow the location", func() {
            parent := createTestLocation("Tavern", "A cozy tavern.", world.LocationTypePersistent)
            Expect(env.Locations.Create(ctx, parent)).To(Succeed())

            scene := createTestLocation("", "", world.LocationTypeScene)
            scene.ShadowsID = &parent.ID
            Expect(env.Locations.Create(ctx, scene)).To(Succeed())

            shadows, err := env.Locations.GetShadowedBy(ctx, parent.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(shadows).To(HaveLen(1))
            Expect(shadows[0].ShadowsID).NotTo(BeNil())
            Expect(*shadows[0].ShadowsID).To(Equal(parent.ID))
        })

        It("returns empty slice when no shadows", func() {
            loc := createTestLocation("Lonely", "No shadows.", world.LocationTypePersistent)
            Expect(env.Locations.Create(ctx, loc)).To(Succeed())

            shadows, err := env.Locations.GetShadowedBy(ctx, loc.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(shadows).To(BeEmpty())
        })
    })
})
```

**Step 2: Run tests to verify they pass**

Run: `task test:integration -- -v ./test/integration/world/... -ginkgo.focus="LocationRepository"`

Expected: All specs pass

**Step 3: Commit**

```bash
git add test/integration/world/location_repo_test.go
git commit -m "feat(world): add LocationRepository BDD specs"
```

---

### Task 2.2: Exit Repository Specs

**Files:**

- Create: `test/integration/world/exit_repo_test.go`

**Step 1: Write the spec file**

Create `test/integration/world/exit_repo_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
    "context"

    "github.com/oklog/ulid/v2"
    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega"    //nolint:revive // gomega convention

    "github.com/holomush/holomush/internal/world"
)

var _ = Describe("ExitRepository", func() {
    var ctx context.Context
    var room1, room2 *world.Location

    BeforeEach(func() {
        ctx = context.Background()
        cleanupLocations(ctx, env.pool)

        // Create two rooms for exit tests
        room1 = createTestLocation("Room One", "First room.", world.LocationTypePersistent)
        room2 = createTestLocation("Room Two", "Second room.", world.LocationTypePersistent)
        Expect(env.Locations.Create(ctx, room1)).To(Succeed())
        Expect(env.Locations.Create(ctx, room2)).To(Succeed())
    })

    Describe("Create", func() {
        It("creates exit with all fields", func() {
            exit := createTestExit(room1.ID, room2.ID, "north")
            exit.Aliases = []string{"n", "forward"}
            exit.Visibility = world.VisibilityAll

            err := env.Exits.Create(ctx, exit)
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Exits.Get(ctx, exit.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.Name).To(Equal("north"))
            Expect(got.Aliases).To(ConsistOf("n", "forward"))
            Expect(got.FromLocationID).To(Equal(room1.ID))
            Expect(got.ToLocationID).To(Equal(room2.ID))
        })

        It("auto-creates return exit when bidirectional", func() {
            exit := createTestExit(room1.ID, room2.ID, "north")
            exit.Bidirectional = true
            exit.ReturnName = "south"

            err := env.Exits.Create(ctx, exit)
            Expect(err).NotTo(HaveOccurred())

            // Find return exit
            returnExit, err := env.Exits.FindByName(ctx, room2.ID, "south")
            Expect(err).NotTo(HaveOccurred())
            Expect(returnExit.FromLocationID).To(Equal(room2.ID))
            Expect(returnExit.ToLocationID).To(Equal(room1.ID))
        })

        It("persists aliases as array", func() {
            exit := createTestExit(room1.ID, room2.ID, "door")
            exit.Aliases = []string{"d", "doorway", "entrance"}

            err := env.Exits.Create(ctx, exit)
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Exits.Get(ctx, exit.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.Aliases).To(HaveLen(3))
            Expect(got.Aliases).To(ContainElements("d", "doorway", "entrance"))
        })

        It("persists lock_data as JSONB", func() {
            exit := createTestExit(room1.ID, room2.ID, "vault")
            exit.Locked = true
            exit.LockType = world.LockTypeKey
            exit.LockData = map[string]any{"key_object_id": "abc123"}

            err := env.Exits.Create(ctx, exit)
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Exits.Get(ctx, exit.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.Locked).To(BeTrue())
            Expect(got.LockType).To(Equal(world.LockTypeKey))
            Expect(got.LockData["key_object_id"]).To(Equal("abc123"))
        })

        It("persists visible_to as array", func() {
            charID := createTestCharacterID()
            exit := createTestExit(room1.ID, room2.ID, "secret")
            exit.Visibility = world.VisibilityList
            exit.VisibleTo = []ulid.ULID{charID}

            err := env.Exits.Create(ctx, exit)
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Exits.Get(ctx, exit.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.Visibility).To(Equal(world.VisibilityList))
            Expect(got.VisibleTo).To(HaveLen(1))
            Expect(got.VisibleTo[0]).To(Equal(charID))
        })
    })

    Describe("Delete", func() {
        It("removes exit from database", func() {
            exit := createTestExit(room1.ID, room2.ID, "north")
            Expect(env.Exits.Create(ctx, exit)).To(Succeed())

            err := env.Exits.Delete(ctx, exit.ID)
            Expect(err).NotTo(HaveOccurred())

            _, err = env.Exits.Get(ctx, exit.ID)
            Expect(err).To(MatchError(ContainSubstring("not found")))
        })

        It("removes return exit when bidirectional", func() {
            exit := createTestExit(room1.ID, room2.ID, "north")
            exit.Bidirectional = true
            exit.ReturnName = "south"
            Expect(env.Exits.Create(ctx, exit)).To(Succeed())

            // Verify return exit exists
            _, err := env.Exits.FindByName(ctx, room2.ID, "south")
            Expect(err).NotTo(HaveOccurred())

            // Delete primary exit
            err = env.Exits.Delete(ctx, exit.ID)
            Expect(err).NotTo(HaveOccurred())

            // Return exit should also be gone
            _, err = env.Exits.FindByName(ctx, room2.ID, "south")
            Expect(err).To(MatchError(ContainSubstring("not found")))
        })

        It("handles missing return exit gracefully", func() {
            exit := createTestExit(room1.ID, room2.ID, "north")
            exit.Bidirectional = true
            exit.ReturnName = "south"
            Expect(env.Exits.Create(ctx, exit)).To(Succeed())

            // Manually delete return exit first
            returnExit, _ := env.Exits.FindByName(ctx, room2.ID, "south")
            _, _ = env.pool.Exec(ctx, "DELETE FROM exits WHERE id = $1", returnExit.ID.String())

            // Deleting primary should not error
            err := env.Exits.Delete(ctx, exit.ID)
            Expect(err).NotTo(HaveOccurred())
        })
    })

    Describe("FindByName", func() {
        BeforeEach(func() {
            exit := createTestExit(room1.ID, room2.ID, "North Door")
            exit.Aliases = []string{"n", "door"}
            Expect(env.Exits.Create(ctx, exit)).To(Succeed())
        })

        It("matches exact name case-insensitively", func() {
            found, err := env.Exits.FindByName(ctx, room1.ID, "north door")
            Expect(err).NotTo(HaveOccurred())
            Expect(found.Name).To(Equal("North Door"))
        })

        It("matches aliases", func() {
            found, err := env.Exits.FindByName(ctx, room1.ID, "n")
            Expect(err).NotTo(HaveOccurred())
            Expect(found.Name).To(Equal("North Door"))

            found, err = env.Exits.FindByName(ctx, room1.ID, "door")
            Expect(err).NotTo(HaveOccurred())
            Expect(found.Name).To(Equal("North Door"))
        })

        It("returns ErrNotFound when no match", func() {
            _, err := env.Exits.FindByName(ctx, room1.ID, "nonexistent")
            Expect(err).To(MatchError(ContainSubstring("not found")))
        })
    })

    Describe("FindByNameFuzzy", func() {
        BeforeEach(func() {
            exits := []*world.Exit{
                createTestExit(room1.ID, room2.ID, "northern passage"),
                createTestExit(room1.ID, room2.ID, "southern gate"),
            }
            for _, e := range exits {
                Expect(env.Exits.Create(ctx, e)).To(Succeed())
            }
        })

        It("returns matches above threshold", func() {
            found, err := env.Exits.FindByNameFuzzy(ctx, room1.ID, "north", 0.3)
            Expect(err).NotTo(HaveOccurred())
            Expect(found.Name).To(Equal("northern passage"))
        })

        It("returns ErrNotFound when below threshold", func() {
            _, err := env.Exits.FindByNameFuzzy(ctx, room1.ID, "xyz", 0.9)
            Expect(err).To(MatchError(ContainSubstring("not found")))
        })

        It("validates threshold bounds (0.0-1.0)", func() {
            _, err := env.Exits.FindByNameFuzzy(ctx, room1.ID, "test", -0.1)
            Expect(err).To(HaveOccurred())

            _, err = env.Exits.FindByNameFuzzy(ctx, room1.ID, "test", 1.1)
            Expect(err).To(HaveOccurred())
        })
    })

    Describe("ListFromLocation", func() {
        It("returns all exits from location", func() {
            room3 := createTestLocation("Room Three", "Third room.", world.LocationTypePersistent)
            Expect(env.Locations.Create(ctx, room3)).To(Succeed())

            Expect(env.Exits.Create(ctx, createTestExit(room1.ID, room2.ID, "north"))).To(Succeed())
            Expect(env.Exits.Create(ctx, createTestExit(room1.ID, room3.ID, "east"))).To(Succeed())

            exits, err := env.Exits.ListFromLocation(ctx, room1.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(exits).To(HaveLen(2))
        })

        It("orders by name", func() {
            room3 := createTestLocation("Room Three", "Third room.", world.LocationTypePersistent)
            Expect(env.Locations.Create(ctx, room3)).To(Succeed())

            Expect(env.Exits.Create(ctx, createTestExit(room1.ID, room2.ID, "zulu"))).To(Succeed())
            Expect(env.Exits.Create(ctx, createTestExit(room1.ID, room3.ID, "alpha"))).To(Succeed())

            exits, err := env.Exits.ListFromLocation(ctx, room1.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(exits[0].Name).To(Equal("alpha"))
            Expect(exits[1].Name).To(Equal("zulu"))
        })
    })
})
```

**Step 2: Run tests to verify they pass**

Run: `task test:integration -- -v ./test/integration/world/... -ginkgo.focus="ExitRepository"`

Expected: All specs pass

**Step 3: Commit**

```bash
git add test/integration/world/exit_repo_test.go
git commit -m "feat(world): add ExitRepository BDD specs"
```

---

### Task 2.3: Object Repository Specs

**Files:**

- Create: `test/integration/world/object_repo_test.go`

**Step 1: Write the spec file**

Create `test/integration/world/object_repo_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
    "context"

    "github.com/oklog/ulid/v2"
    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega"    //nolint:revive // gomega convention

    "github.com/holomush/holomush/internal/world"
)

var _ = Describe("ObjectRepository", func() {
    var ctx context.Context
    var room *world.Location
    var charID ulid.ULID

    BeforeEach(func() {
        ctx = context.Background()
        cleanupLocations(ctx, env.pool)

        room = createTestLocation("Test Room", "For object tests.", world.LocationTypePersistent)
        Expect(env.Locations.Create(ctx, room)).To(Succeed())

        charID = createTestCharacterID()
    })

    Describe("Create", func() {
        It("creates object with containment in location", func() {
            obj := createTestObject("Sword", "A sharp sword.", world.Containment{LocationID: &room.ID})

            err := env.Objects.Create(ctx, obj)
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Objects.Get(ctx, obj.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.Name).To(Equal("Sword"))
            Expect(got.LocationID).NotTo(BeNil())
            Expect(*got.LocationID).To(Equal(room.ID))
        })

        It("creates object with containment held by character", func() {
            obj := createTestObject("Shield", "A sturdy shield.", world.Containment{CharacterID: &charID})

            err := env.Objects.Create(ctx, obj)
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Objects.Get(ctx, obj.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.HeldByCharacterID).NotTo(BeNil())
            Expect(*got.HeldByCharacterID).To(Equal(charID))
        })
    })

    Describe("Move", func() {
        It("updates containment atomically", func() {
            obj := createTestObject("Gem", "A sparkling gem.", world.Containment{LocationID: &room.ID})
            Expect(env.Objects.Create(ctx, obj)).To(Succeed())

            err := env.Objects.Move(ctx, obj.ID, world.Containment{CharacterID: &charID})
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Objects.Get(ctx, obj.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.LocationID).To(BeNil())
            Expect(got.HeldByCharacterID).NotTo(BeNil())
            Expect(*got.HeldByCharacterID).To(Equal(charID))
        })

        It("validates target is container when moving to object", func() {
            container := createTestObject("Bag", "A leather bag.", world.Containment{LocationID: &room.ID})
            container.IsContainer = false // NOT a container
            Expect(env.Objects.Create(ctx, container)).To(Succeed())

            item := createTestObject("Coin", "A gold coin.", world.Containment{LocationID: &room.ID})
            Expect(env.Objects.Create(ctx, item)).To(Succeed())

            err := env.Objects.Move(ctx, item.ID, world.Containment{ObjectID: &container.ID})
            Expect(err).To(HaveOccurred())
            Expect(err.Error()).To(ContainSubstring("not a container"))
        })

        It("allows moving to actual container", func() {
            container := createTestObject("Chest", "A wooden chest.", world.Containment{LocationID: &room.ID})
            container.IsContainer = true
            Expect(env.Objects.Create(ctx, container)).To(Succeed())

            item := createTestObject("Ring", "A gold ring.", world.Containment{LocationID: &room.ID})
            Expect(env.Objects.Create(ctx, item)).To(Succeed())

            err := env.Objects.Move(ctx, item.ID, world.Containment{ObjectID: &container.ID})
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Objects.Get(ctx, item.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.ContainedInObjectID).NotTo(BeNil())
            Expect(*got.ContainedInObjectID).To(Equal(container.ID))
        })

        It("enforces max nesting depth", func() {
            // Create chain: container1 -> container2 -> container3
            c1 := createTestObject("Box 1", "Container 1.", world.Containment{LocationID: &room.ID})
            c1.IsContainer = true
            Expect(env.Objects.Create(ctx, c1)).To(Succeed())

            c2 := createTestObject("Box 2", "Container 2.", world.Containment{ObjectID: &c1.ID})
            c2.IsContainer = true
            Expect(env.Objects.Create(ctx, c2)).To(Succeed())

            c3 := createTestObject("Box 3", "Container 3.", world.Containment{ObjectID: &c2.ID})
            c3.IsContainer = true
            Expect(env.Objects.Create(ctx, c3)).To(Succeed())

            // Try to add item to c3 (would be depth 4)
            item := createTestObject("Pebble", "A small pebble.", world.Containment{LocationID: &room.ID})
            Expect(env.Objects.Create(ctx, item)).To(Succeed())

            err := env.Objects.Move(ctx, item.ID, world.Containment{ObjectID: &c3.ID})
            Expect(err).To(HaveOccurred())
            Expect(err.Error()).To(ContainSubstring("nesting depth"))
        })

        It("prevents circular containment", func() {
            c1 := createTestObject("Container A", "First container.", world.Containment{LocationID: &room.ID})
            c1.IsContainer = true
            Expect(env.Objects.Create(ctx, c1)).To(Succeed())

            c2 := createTestObject("Container B", "Second container.", world.Containment{ObjectID: &c1.ID})
            c2.IsContainer = true
            Expect(env.Objects.Create(ctx, c2)).To(Succeed())

            // Try to put c1 into c2 (circular: c2 is already in c1)
            err := env.Objects.Move(ctx, c1.ID, world.Containment{ObjectID: &c2.ID})
            Expect(err).To(HaveOccurred())
            Expect(err.Error()).To(ContainSubstring("circular"))
        })

        It("prevents self-containment", func() {
            container := createTestObject("Self Box", "Tries to contain itself.", world.Containment{LocationID: &room.ID})
            container.IsContainer = true
            Expect(env.Objects.Create(ctx, container)).To(Succeed())

            err := env.Objects.Move(ctx, container.ID, world.Containment{ObjectID: &container.ID})
            Expect(err).To(HaveOccurred())
        })
    })

    Describe("ListAtLocation", func() {
        It("returns objects at location", func() {
            Expect(env.Objects.Create(ctx, createTestObject("Obj1", "First.", world.Containment{LocationID: &room.ID}))).To(Succeed())
            Expect(env.Objects.Create(ctx, createTestObject("Obj2", "Second.", world.Containment{LocationID: &room.ID}))).To(Succeed())

            objects, err := env.Objects.ListAtLocation(ctx, room.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(objects).To(HaveLen(2))
        })

        It("returns empty slice for empty location", func() {
            emptyRoom := createTestLocation("Empty", "Nothing here.", world.LocationTypePersistent)
            Expect(env.Locations.Create(ctx, emptyRoom)).To(Succeed())

            objects, err := env.Objects.ListAtLocation(ctx, emptyRoom.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(objects).To(BeEmpty())
        })
    })

    Describe("ListHeldBy", func() {
        It("returns objects held by character", func() {
            Expect(env.Objects.Create(ctx, createTestObject("Held1", "First held.", world.Containment{CharacterID: &charID}))).To(Succeed())
            Expect(env.Objects.Create(ctx, createTestObject("Held2", "Second held.", world.Containment{CharacterID: &charID}))).To(Succeed())

            objects, err := env.Objects.ListHeldBy(ctx, charID)
            Expect(err).NotTo(HaveOccurred())
            Expect(objects).To(HaveLen(2))
        })
    })

    Describe("ListContainedIn", func() {
        It("returns objects inside container", func() {
            container := createTestObject("Backpack", "A sturdy backpack.", world.Containment{LocationID: &room.ID})
            container.IsContainer = true
            Expect(env.Objects.Create(ctx, container)).To(Succeed())

            Expect(env.Objects.Create(ctx, createTestObject("Item1", "First item.", world.Containment{ObjectID: &container.ID}))).To(Succeed())
            Expect(env.Objects.Create(ctx, createTestObject("Item2", "Second item.", world.Containment{ObjectID: &container.ID}))).To(Succeed())

            objects, err := env.Objects.ListContainedIn(ctx, container.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(objects).To(HaveLen(2))
        })
    })
})
```

**Step 2: Run tests to verify they pass**

Run: `task test:integration -- -v ./test/integration/world/... -ginkgo.focus="ObjectRepository"`

Expected: All specs pass

**Step 3: Commit**

```bash
git add test/integration/world/object_repo_test.go
git commit -m "feat(world): add ObjectRepository BDD specs"
```

---

### Task 2.4: Scene Repository Specs

**Files:**

- Create: `test/integration/world/scene_repo_test.go`

**Step 1: Write the spec file**

Create `test/integration/world/scene_repo_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
    "context"

    "github.com/oklog/ulid/v2"
    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega"    //nolint:revive // gomega convention

    "github.com/holomush/holomush/internal/world"
)

var _ = Describe("SceneRepository", func() {
    var ctx context.Context
    var scene *world.Location
    var charID1, charID2 ulid.ULID

    BeforeEach(func() {
        ctx = context.Background()
        cleanupLocations(ctx, env.pool)

        scene = createTestLocation("RP Scene", "A roleplay scene.", world.LocationTypeScene)
        Expect(env.Locations.Create(ctx, scene)).To(Succeed())

        charID1 = createTestCharacterID()
        charID2 = createTestCharacterID()
    })

    Describe("AddParticipant", func() {
        It("adds character to scene with role", func() {
            err := env.Scenes.AddParticipant(ctx, scene.ID, charID1, "owner")
            Expect(err).NotTo(HaveOccurred())

            participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(participants).To(HaveLen(1))
            Expect(participants[0].CharacterID).To(Equal(charID1))
            Expect(participants[0].Role).To(Equal("owner"))
        })

        It("updates role if already participant", func() {
            Expect(env.Scenes.AddParticipant(ctx, scene.ID, charID1, "member")).To(Succeed())
            Expect(env.Scenes.AddParticipant(ctx, scene.ID, charID1, "owner")).To(Succeed())

            participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(participants).To(HaveLen(1))
            Expect(participants[0].Role).To(Equal("owner"))
        })
    })

    Describe("RemoveParticipant", func() {
        It("removes character from scene", func() {
            Expect(env.Scenes.AddParticipant(ctx, scene.ID, charID1, "member")).To(Succeed())

            err := env.Scenes.RemoveParticipant(ctx, scene.ID, charID1)
            Expect(err).NotTo(HaveOccurred())

            participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(participants).To(BeEmpty())
        })
    })

    Describe("ListParticipants", func() {
        It("returns all participants with roles", func() {
            Expect(env.Scenes.AddParticipant(ctx, scene.ID, charID1, "owner")).To(Succeed())
            Expect(env.Scenes.AddParticipant(ctx, scene.ID, charID2, "member")).To(Succeed())

            participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(participants).To(HaveLen(2))
        })
    })

    Describe("GetScenesFor", func() {
        It("returns scenes character participates in", func() {
            scene2 := createTestLocation("Second Scene", "Another scene.", world.LocationTypeScene)
            Expect(env.Locations.Create(ctx, scene2)).To(Succeed())

            Expect(env.Scenes.AddParticipant(ctx, scene.ID, charID1, "owner")).To(Succeed())
            Expect(env.Scenes.AddParticipant(ctx, scene2.ID, charID1, "member")).To(Succeed())

            scenes, err := env.Scenes.GetScenesFor(ctx, charID1)
            Expect(err).NotTo(HaveOccurred())
            Expect(scenes).To(HaveLen(2))
        })
    })
})
```

**Step 2: Run tests to verify they pass**

Run: `task test:integration -- -v ./test/integration/world/... -ginkgo.focus="SceneRepository"`

Expected: All specs pass

**Step 3: Commit**

```bash
git add test/integration/world/scene_repo_test.go
git commit -m "feat(world): add SceneRepository BDD specs"
```

---

## Phase 3: Feature-Level BDD Specs

### Task 3.1: Location Management Feature Specs

**Files:**

- Create: `test/integration/world/locations_test.go`

**Step 1: Write the spec file**

Create `test/integration/world/locations_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
    "context"

    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega"    //nolint:revive // gomega convention

    "github.com/holomush/holomush/internal/world"
)

var _ = Describe("Location Management", func() {
    var ctx context.Context

    BeforeEach(func() {
        ctx = context.Background()
        cleanupLocations(ctx, env.pool)
    })

    Describe("Location types", func() {
        It("creates persistent locations (permanent world rooms)", func() {
            loc := createTestLocation("Town Square", "The center of town.", world.LocationTypePersistent)
            Expect(env.Locations.Create(ctx, loc)).To(Succeed())

            got, err := env.Locations.Get(ctx, loc.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.Type).To(Equal(world.LocationTypePersistent))
        })

        It("creates scene locations (temporary RP rooms)", func() {
            loc := createTestLocation("Private Meeting", "A private scene.", world.LocationTypeScene)
            Expect(env.Locations.Create(ctx, loc)).To(Succeed())

            got, err := env.Locations.Get(ctx, loc.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.Type).To(Equal(world.LocationTypeScene))
        })

        It("creates instance locations (future instanced content)", func() {
            loc := createTestLocation("Dungeon Instance", "An instanced dungeon.", world.LocationTypeInstance)
            Expect(env.Locations.Create(ctx, loc)).To(Succeed())

            got, err := env.Locations.Get(ctx, loc.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.Type).To(Equal(world.LocationTypeInstance))
        })
    })

    Describe("Replay policy", func() {
        Context("persistent locations", func() {
            It("defaults to 'last:0' (no replay)", func() {
                loc := createTestLocation("No Replay", "Testing default.", world.LocationTypePersistent)
                Expect(env.Locations.Create(ctx, loc)).To(Succeed())

                got, err := env.Locations.Get(ctx, loc.ID)
                Expect(err).NotTo(HaveOccurred())
                Expect(got.ReplayPolicy).To(Equal("last:0"))
            })
        })

        Context("scene locations", func() {
            It("defaults to 'last:-1' (full history)", func() {
                loc := createTestLocation("Full Replay", "Testing scene default.", world.LocationTypeScene)
                Expect(env.Locations.Create(ctx, loc)).To(Succeed())

                got, err := env.Locations.Get(ctx, loc.ID)
                Expect(err).NotTo(HaveOccurred())
                Expect(got.ReplayPolicy).To(Equal("last:-1"))
            })
        })

        It("parses 'last:N' format correctly", func() {
            Expect(world.ParseReplayPolicy("last:0")).To(Equal(0))
            Expect(world.ParseReplayPolicy("last:10")).To(Equal(10))
            Expect(world.ParseReplayPolicy("last:-1")).To(Equal(-1))
        })

        It("supports custom replay limits", func() {
            loc := createTestLocation("Limited Replay", "Custom replay.", world.LocationTypePersistent)
            loc.ReplayPolicy = "last:50"
            Expect(env.Locations.Create(ctx, loc)).To(Succeed())

            got, err := env.Locations.Get(ctx, loc.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.ReplayPolicy).To(Equal("last:50"))
            Expect(world.ParseReplayPolicy(got.ReplayPolicy)).To(Equal(50))
        })
    })

    Describe("Location ownership", func() {
        It("tracks owner_id for builder permissions", func() {
            ownerID := createTestCharacterID()
            loc := createTestLocation("Built Room", "A builder-owned room.", world.LocationTypePersistent)
            loc.OwnerID = &ownerID
            Expect(env.Locations.Create(ctx, loc)).To(Succeed())

            got, err := env.Locations.Get(ctx, loc.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.OwnerID).NotTo(BeNil())
            Expect(*got.OwnerID).To(Equal(ownerID))
        })

        It("allows nil owner for system-created locations", func() {
            loc := createTestLocation("System Room", "Created by system.", world.LocationTypePersistent)
            loc.OwnerID = nil
            Expect(env.Locations.Create(ctx, loc)).To(Succeed())

            got, err := env.Locations.Get(ctx, loc.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.OwnerID).To(BeNil())
        })
    })
})
```

**Step 2: Run tests to verify they pass**

Run: `task test:integration -- -v ./test/integration/world/... -ginkgo.focus="Location Management"`

Expected: All specs pass

**Step 3: Commit**

```bash
git add test/integration/world/locations_test.go
git commit -m "feat(world): add Location Management BDD feature specs"
```

---

### Task 3.2: Scene Management Feature Specs

**Files:**

- Create: `test/integration/world/scenes_test.go`

**Step 1: Write the spec file**

Create `test/integration/world/scenes_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
    "context"

    "github.com/oklog/ulid/v2"
    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega"    //nolint:revive // gomega convention

    "github.com/holomush/holomush/internal/world"
)

var _ = Describe("Scene Management", func() {
    var ctx context.Context
    var ownerID ulid.ULID

    BeforeEach(func() {
        ctx = context.Background()
        cleanupLocations(ctx, env.pool)
        ownerID = createTestCharacterID()
    })

    Describe("Creating scenes", func() {
        It("creates scene with type 'scene'", func() {
            scene := createTestLocation("My Scene", "A private scene.", world.LocationTypeScene)
            Expect(env.Locations.Create(ctx, scene)).To(Succeed())

            got, err := env.Locations.Get(ctx, scene.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.Type).To(Equal(world.LocationTypeScene))
        })

        It("sets default replay policy to 'last:-1' (full history)", func() {
            scene := createTestLocation("RP Scene", "For roleplay.", world.LocationTypeScene)
            Expect(env.Locations.Create(ctx, scene)).To(Succeed())

            got, err := env.Locations.Get(ctx, scene.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.ReplayPolicy).To(Equal("last:-1"))
        })

        It("assigns creator as scene owner", func() {
            scene := createTestLocation("Owned Scene", "Has an owner.", world.LocationTypeScene)
            scene.OwnerID = &ownerID
            Expect(env.Locations.Create(ctx, scene)).To(Succeed())

            got, err := env.Locations.Get(ctx, scene.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.OwnerID).NotTo(BeNil())
            Expect(*got.OwnerID).To(Equal(ownerID))
        })
    })

    Describe("Scene shadowing", func() {
        var tavern *world.Location

        BeforeEach(func() {
            tavern = createTestLocation("The Tavern", "A cozy tavern with a roaring fire.", world.LocationTypePersistent)
            Expect(env.Locations.Create(ctx, tavern)).To(Succeed())
        })

        Context("when shadowing a persistent location", func() {
            It("inherits name from parent when scene name is empty", func() {
                scene := createTestLocation("", "", world.LocationTypeScene)
                scene.ShadowsID = &tavern.ID
                Expect(env.Locations.Create(ctx, scene)).To(Succeed())

                got, err := env.Locations.Get(ctx, scene.ID)
                Expect(err).NotTo(HaveOccurred())
                Expect(got.ShadowsID).NotTo(BeNil())

                // Get effective name from parent
                effectiveName := got.EffectiveName(tavern)
                Expect(effectiveName).To(Equal("The Tavern"))
            })

            It("inherits description from parent when empty", func() {
                scene := createTestLocation("", "", world.LocationTypeScene)
                scene.ShadowsID = &tavern.ID
                Expect(env.Locations.Create(ctx, scene)).To(Succeed())

                got, err := env.Locations.Get(ctx, scene.ID)
                Expect(err).NotTo(HaveOccurred())

                // Get effective description from parent
                effectiveDesc, err := got.EffectiveDescription(tavern)
                Expect(err).NotTo(HaveOccurred())
                Expect(effectiveDesc).To(Equal("A cozy tavern with a roaring fire."))
            })

            It("uses own name/description when provided (override)", func() {
                scene := createTestLocation("Private Room", "The back room of the tavern.", world.LocationTypeScene)
                scene.ShadowsID = &tavern.ID
                Expect(env.Locations.Create(ctx, scene)).To(Succeed())

                got, err := env.Locations.Get(ctx, scene.ID)
                Expect(err).NotTo(HaveOccurred())

                effectiveName := got.EffectiveName(tavern)
                Expect(effectiveName).To(Equal("Private Room"))

                effectiveDesc, err := got.EffectiveDescription(tavern)
                Expect(err).NotTo(HaveOccurred())
                Expect(effectiveDesc).To(Equal("The back room of the tavern."))
            })
        })

        It("lists all scenes shadowing a location via GetShadowedBy", func() {
            scene1 := createTestLocation("Scene 1", "", world.LocationTypeScene)
            scene1.ShadowsID = &tavern.ID
            Expect(env.Locations.Create(ctx, scene1)).To(Succeed())

            scene2 := createTestLocation("Scene 2", "", world.LocationTypeScene)
            scene2.ShadowsID = &tavern.ID
            Expect(env.Locations.Create(ctx, scene2)).To(Succeed())

            shadows, err := env.Locations.GetShadowedBy(ctx, tavern.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(shadows).To(HaveLen(2))
        })
    })

    Describe("Scene participants", func() {
        var scene *world.Location
        var char1, char2 ulid.ULID

        BeforeEach(func() {
            scene = createTestLocation("RP Scene", "A scene.", world.LocationTypeScene)
            Expect(env.Locations.Create(ctx, scene)).To(Succeed())
            char1 = createTestCharacterID()
            char2 = createTestCharacterID()
        })

        It("adds participant with 'member' role by default", func() {
            Expect(env.Scenes.AddParticipant(ctx, scene.ID, char1, "member")).To(Succeed())

            participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(participants).To(HaveLen(1))
            Expect(participants[0].Role).To(Equal("member"))
        })

        It("supports 'owner', 'member', 'invited' roles", func() {
            Expect(env.Scenes.AddParticipant(ctx, scene.ID, char1, "owner")).To(Succeed())
            Expect(env.Scenes.AddParticipant(ctx, scene.ID, char2, "invited")).To(Succeed())

            participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(participants).To(HaveLen(2))
        })

        It("removes participant from scene", func() {
            Expect(env.Scenes.AddParticipant(ctx, scene.ID, char1, "member")).To(Succeed())
            Expect(env.Scenes.RemoveParticipant(ctx, scene.ID, char1)).To(Succeed())

            participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(participants).To(BeEmpty())
        })

        It("lists all participants in a scene", func() {
            Expect(env.Scenes.AddParticipant(ctx, scene.ID, char1, "owner")).To(Succeed())
            Expect(env.Scenes.AddParticipant(ctx, scene.ID, char2, "member")).To(Succeed())

            participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(participants).To(HaveLen(2))
        })

        It("lists all scenes a character participates in", func() {
            scene2 := createTestLocation("Scene 2", "Another scene.", world.LocationTypeScene)
            Expect(env.Locations.Create(ctx, scene2)).To(Succeed())

            Expect(env.Scenes.AddParticipant(ctx, scene.ID, char1, "owner")).To(Succeed())
            Expect(env.Scenes.AddParticipant(ctx, scene2.ID, char1, "member")).To(Succeed())

            scenes, err := env.Scenes.GetScenesFor(ctx, char1)
            Expect(err).NotTo(HaveOccurred())
            Expect(scenes).To(HaveLen(2))
        })
    })
})
```

**Step 2: Run tests to verify they pass**

Run: `task test:integration -- -v ./test/integration/world/... -ginkgo.focus="Scene Management"`

Expected: All specs pass

**Step 3: Commit**

```bash
git add test/integration/world/scenes_test.go
git commit -m "feat(world): add Scene Management BDD feature specs"
```

---

### Task 3.3: Exit and Movement Feature Specs

**Files:**

- Create: `test/integration/world/movement_test.go`

**Step 1: Write the spec file**

Create `test/integration/world/movement_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
    "context"

    "github.com/oklog/ulid/v2"
    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega"    //nolint:revive // gomega convention

    "github.com/holomush/holomush/internal/world"
)

var _ = Describe("Character Movement", func() {
    var ctx context.Context
    var room1, room2 *world.Location

    BeforeEach(func() {
        ctx = context.Background()
        cleanupLocations(ctx, env.pool)

        room1 = createTestLocation("Starting Room", "Where journeys begin.", world.LocationTypePersistent)
        room2 = createTestLocation("Destination", "Where journeys end.", world.LocationTypePersistent)
        Expect(env.Locations.Create(ctx, room1)).To(Succeed())
        Expect(env.Locations.Create(ctx, room2)).To(Succeed())
    })

    Describe("Bidirectional exits", func() {
        It("allows movement in both directions", func() {
            exit := createTestExit(room1.ID, room2.ID, "north")
            exit.Bidirectional = true
            exit.ReturnName = "south"
            Expect(env.Exits.Create(ctx, exit)).To(Succeed())

            // Can find exit going north from room1
            northExit, err := env.Exits.FindByName(ctx, room1.ID, "north")
            Expect(err).NotTo(HaveOccurred())
            Expect(northExit.ToLocationID).To(Equal(room2.ID))

            // Can find exit going south from room2
            southExit, err := env.Exits.FindByName(ctx, room2.ID, "south")
            Expect(err).NotTo(HaveOccurred())
            Expect(southExit.ToLocationID).To(Equal(room1.ID))
        })

        It("uses return_name for the reverse direction", func() {
            exit := createTestExit(room1.ID, room2.ID, "doorway")
            exit.Bidirectional = true
            exit.ReturnName = "back"
            Expect(env.Exits.Create(ctx, exit)).To(Succeed())

            returnExit, err := env.Exits.FindByName(ctx, room2.ID, "back")
            Expect(err).NotTo(HaveOccurred())
            Expect(returnExit.Name).To(Equal("back"))
        })
    })

    Describe("Exit visibility", func() {
        var ownerID, otherID ulid.ULID

        BeforeEach(func() {
            ownerID = createTestCharacterID()
            otherID = createTestCharacterID()
            room1.OwnerID = &ownerID
            Expect(env.Locations.Update(ctx, room1)).To(Succeed())
        })

        Context("when visibility is 'all'", func() {
            It("shows exit to any character", func() {
                exit := createTestExit(room1.ID, room2.ID, "public door")
                exit.Visibility = world.VisibilityAll
                Expect(env.Exits.Create(ctx, exit)).To(Succeed())

                got, _ := env.Exits.Get(ctx, exit.ID)
                Expect(got.IsVisibleTo(otherID, nil)).To(BeTrue())
                Expect(got.IsVisibleTo(ownerID, nil)).To(BeTrue())
            })
        })

        Context("when visibility is 'owner'", func() {
            It("shows exit only to location owner", func() {
                exit := createTestExit(room1.ID, room2.ID, "owner door")
                exit.Visibility = world.VisibilityOwner
                Expect(env.Exits.Create(ctx, exit)).To(Succeed())

                got, _ := env.Exits.Get(ctx, exit.ID)
                Expect(got.IsVisibleTo(ownerID, &ownerID)).To(BeTrue())
            })

            It("hides exit from non-owners", func() {
                exit := createTestExit(room1.ID, room2.ID, "secret door")
                exit.Visibility = world.VisibilityOwner
                Expect(env.Exits.Create(ctx, exit)).To(Succeed())

                got, _ := env.Exits.Get(ctx, exit.ID)
                Expect(got.IsVisibleTo(otherID, &ownerID)).To(BeFalse())
            })
        })

        Context("when visibility is 'list'", func() {
            It("shows exit only to characters in visible_to list", func() {
                allowedID := createTestCharacterID()
                exit := createTestExit(room1.ID, room2.ID, "vip door")
                exit.Visibility = world.VisibilityList
                exit.VisibleTo = []ulid.ULID{allowedID}
                Expect(env.Exits.Create(ctx, exit)).To(Succeed())

                got, _ := env.Exits.Get(ctx, exit.ID)
                Expect(got.IsVisibleTo(allowedID, nil)).To(BeTrue())
                Expect(got.IsVisibleTo(otherID, nil)).To(BeFalse())
            })
        })
    })

    Describe("Locked exits", func() {
        Context("with key lock", func() {
            It("stores lock configuration", func() {
                keyObjectID := "key-12345"
                exit := createTestExit(room1.ID, room2.ID, "locked door")
                exit.Locked = true
                exit.LockType = world.LockTypeKey
                exit.LockData = map[string]any{"key_object_id": keyObjectID}
                Expect(env.Exits.Create(ctx, exit)).To(Succeed())

                got, err := env.Exits.Get(ctx, exit.ID)
                Expect(err).NotTo(HaveOccurred())
                Expect(got.Locked).To(BeTrue())
                Expect(got.LockType).To(Equal(world.LockTypeKey))
                Expect(got.LockData["key_object_id"]).To(Equal(keyObjectID))
            })
        })

        Context("with password lock", func() {
            It("stores password hash in lock_data", func() {
                exit := createTestExit(room1.ID, room2.ID, "password door")
                exit.Locked = true
                exit.LockType = world.LockTypePassword
                exit.LockData = map[string]any{"password_hash": "hashed_secret"}
                Expect(env.Exits.Create(ctx, exit)).To(Succeed())

                got, err := env.Exits.Get(ctx, exit.ID)
                Expect(err).NotTo(HaveOccurred())
                Expect(got.LockType).To(Equal(world.LockTypePassword))
                Expect(got.LockData["password_hash"]).To(Equal("hashed_secret"))
            })
        })
    })

    Describe("Exit name matching", func() {
        BeforeEach(func() {
            exit := createTestExit(room1.ID, room2.ID, "Northern Gate")
            exit.Aliases = []string{"n", "north", "gate"}
            Expect(env.Exits.Create(ctx, exit)).To(Succeed())
        })

        It("matches by exact name (case-insensitive)", func() {
            found, err := env.Exits.FindByName(ctx, room1.ID, "northern gate")
            Expect(err).NotTo(HaveOccurred())
            Expect(found.Name).To(Equal("Northern Gate"))

            found, err = env.Exits.FindByName(ctx, room1.ID, "NORTHERN GATE")
            Expect(err).NotTo(HaveOccurred())
            Expect(found.Name).To(Equal("Northern Gate"))
        })

        It("matches by alias", func() {
            found, err := env.Exits.FindByName(ctx, room1.ID, "n")
            Expect(err).NotTo(HaveOccurred())
            Expect(found.Name).To(Equal("Northern Gate"))

            found, err = env.Exits.FindByName(ctx, room1.ID, "gate")
            Expect(err).NotTo(HaveOccurred())
            Expect(found.Name).To(Equal("Northern Gate"))
        })

        It("matches by fuzzy search with threshold", func() {
            found, err := env.Exits.FindByNameFuzzy(ctx, room1.ID, "nort", 0.3)
            Expect(err).NotTo(HaveOccurred())
            Expect(found.Name).To(Equal("Northern Gate"))
        })
    })
})
```

**Step 2: Run tests to verify they pass**

Run: `task test:integration -- -v ./test/integration/world/... -ginkgo.focus="Character Movement"`

Expected: All specs pass

**Step 3: Commit**

```bash
git add test/integration/world/movement_test.go
git commit -m "feat(world): add Movement BDD feature specs"
```

---

### Task 3.4: Object Handling Feature Specs

**Files:**

- Create: `test/integration/world/objects_test.go`

**Step 1: Write the spec file**

Create `test/integration/world/objects_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
    "context"

    "github.com/oklog/ulid/v2"
    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega"    //nolint:revive // gomega convention

    "github.com/holomush/holomush/internal/world"
)

var _ = Describe("Object Handling", func() {
    var ctx context.Context
    var room *world.Location
    var charID ulid.ULID

    BeforeEach(func() {
        ctx = context.Background()
        cleanupLocations(ctx, env.pool)

        room = createTestLocation("Item Room", "For object tests.", world.LocationTypePersistent)
        Expect(env.Locations.Create(ctx, room)).To(Succeed())

        charID = createTestCharacterID()
    })

    Describe("Picking up objects", func() {
        It("moves object from location to character inventory", func() {
            obj := createTestObject("Sword", "A sharp blade.", world.Containment{LocationID: &room.ID})
            Expect(env.Objects.Create(ctx, obj)).To(Succeed())

            err := env.Objects.Move(ctx, obj.ID, world.Containment{CharacterID: &charID})
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Objects.Get(ctx, obj.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.LocationID).To(BeNil())
            Expect(got.HeldByCharacterID).NotTo(BeNil())
            Expect(*got.HeldByCharacterID).To(Equal(charID))
        })
    })

    Describe("Dropping objects", func() {
        It("moves object from character inventory to location", func() {
            obj := createTestObject("Shield", "A sturdy shield.", world.Containment{CharacterID: &charID})
            Expect(env.Objects.Create(ctx, obj)).To(Succeed())

            err := env.Objects.Move(ctx, obj.ID, world.Containment{LocationID: &room.ID})
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Objects.Get(ctx, obj.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.HeldByCharacterID).To(BeNil())
            Expect(got.LocationID).NotTo(BeNil())
            Expect(*got.LocationID).To(Equal(room.ID))
        })
    })

    Describe("Container operations", func() {
        var container *world.Object

        BeforeEach(func() {
            container = createTestObject("Backpack", "A leather backpack.", world.Containment{LocationID: &room.ID})
            container.IsContainer = true
            Expect(env.Objects.Create(ctx, container)).To(Succeed())
        })

        Context("putting objects in containers", func() {
            It("moves object into container object", func() {
                item := createTestObject("Gem", "A sparkling gem.", world.Containment{LocationID: &room.ID})
                Expect(env.Objects.Create(ctx, item)).To(Succeed())

                err := env.Objects.Move(ctx, item.ID, world.Containment{ObjectID: &container.ID})
                Expect(err).NotTo(HaveOccurred())

                got, err := env.Objects.Get(ctx, item.ID)
                Expect(err).NotTo(HaveOccurred())
                Expect(got.ContainedInObjectID).NotTo(BeNil())
                Expect(*got.ContainedInObjectID).To(Equal(container.ID))
            })

            It("fails when target is not a container", func() {
                nonContainer := createTestObject("Rock", "Just a rock.", world.Containment{LocationID: &room.ID})
                nonContainer.IsContainer = false
                Expect(env.Objects.Create(ctx, nonContainer)).To(Succeed())

                item := createTestObject("Pebble", "A tiny pebble.", world.Containment{LocationID: &room.ID})
                Expect(env.Objects.Create(ctx, item)).To(Succeed())

                err := env.Objects.Move(ctx, item.ID, world.Containment{ObjectID: &nonContainer.ID})
                Expect(err).To(HaveOccurred())
            })

            It("fails when exceeding max nesting depth (3)", func() {
                // Create 3-level nesting
                level1 := container
                level2 := createTestObject("Pouch", "A small pouch.", world.Containment{ObjectID: &level1.ID})
                level2.IsContainer = true
                Expect(env.Objects.Create(ctx, level2)).To(Succeed())

                level3 := createTestObject("Wallet", "A tiny wallet.", world.Containment{ObjectID: &level2.ID})
                level3.IsContainer = true
                Expect(env.Objects.Create(ctx, level3)).To(Succeed())

                // Try to add to level3 (would be depth 4)
                tooDeep := createTestObject("Coin", "A gold coin.", world.Containment{LocationID: &room.ID})
                Expect(env.Objects.Create(ctx, tooDeep)).To(Succeed())

                err := env.Objects.Move(ctx, tooDeep.ID, world.Containment{ObjectID: &level3.ID})
                Expect(err).To(HaveOccurred())
                Expect(err.Error()).To(ContainSubstring("nesting depth"))
            })
        })

        Context("taking objects from containers", func() {
            It("moves object from container to character", func() {
                item := createTestObject("Ring", "A gold ring.", world.Containment{ObjectID: &container.ID})
                Expect(env.Objects.Create(ctx, item)).To(Succeed())

                err := env.Objects.Move(ctx, item.ID, world.Containment{CharacterID: &charID})
                Expect(err).NotTo(HaveOccurred())

                got, err := env.Objects.Get(ctx, item.ID)
                Expect(err).NotTo(HaveOccurred())
                Expect(got.ContainedInObjectID).To(BeNil())
                Expect(got.HeldByCharacterID).NotTo(BeNil())
            })
        })

        Context("circular containment prevention", func() {
            It("prevents putting container inside itself", func() {
                err := env.Objects.Move(ctx, container.ID, world.Containment{ObjectID: &container.ID})
                Expect(err).To(HaveOccurred())
            })

            It("prevents A->B->A circular chains", func() {
                containerB := createTestObject("Box B", "Another container.", world.Containment{ObjectID: &container.ID})
                containerB.IsContainer = true
                Expect(env.Objects.Create(ctx, containerB)).To(Succeed())

                // Try to put container (A) inside containerB (which is already in A)
                err := env.Objects.Move(ctx, container.ID, world.Containment{ObjectID: &containerB.ID})
                Expect(err).To(HaveOccurred())
                Expect(err.Error()).To(ContainSubstring("circular"))
            })

            It("prevents deep circular chains A->B->C->A", func() {
                containerB := createTestObject("Box B", "Container B.", world.Containment{ObjectID: &container.ID})
                containerB.IsContainer = true
                Expect(env.Objects.Create(ctx, containerB)).To(Succeed())

                containerC := createTestObject("Box C", "Container C.", world.Containment{ObjectID: &containerB.ID})
                containerC.IsContainer = true
                Expect(env.Objects.Create(ctx, containerC)).To(Succeed())

                // Try to put container (A) inside containerC (A->B->C, trying to make C->A)
                err := env.Objects.Move(ctx, container.ID, world.Containment{ObjectID: &containerC.ID})
                Expect(err).To(HaveOccurred())
                Expect(err.Error()).To(ContainSubstring("circular"))
            })
        })
    })

    Describe("Containment invariants", func() {
        It("ensures object is in exactly one place", func() {
            obj := createTestObject("Unique", "Can only be one place.", world.Containment{LocationID: &room.ID})
            Expect(env.Objects.Create(ctx, obj)).To(Succeed())

            // Move to character
            err := env.Objects.Move(ctx, obj.ID, world.Containment{CharacterID: &charID})
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Objects.Get(ctx, obj.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.LocationID).To(BeNil())
            Expect(got.HeldByCharacterID).NotTo(BeNil())
            Expect(got.ContainedInObjectID).To(BeNil())
        })

        It("clears previous containment when moving", func() {
            container := createTestObject("Chest", "A wooden chest.", world.Containment{LocationID: &room.ID})
            container.IsContainer = true
            Expect(env.Objects.Create(ctx, container)).To(Succeed())

            obj := createTestObject("Jewel", "A precious jewel.", world.Containment{ObjectID: &container.ID})
            Expect(env.Objects.Create(ctx, obj)).To(Succeed())

            // Move from container to room
            err := env.Objects.Move(ctx, obj.ID, world.Containment{LocationID: &room.ID})
            Expect(err).NotTo(HaveOccurred())

            got, err := env.Objects.Get(ctx, obj.ID)
            Expect(err).NotTo(HaveOccurred())
            Expect(got.ContainedInObjectID).To(BeNil())
            Expect(got.LocationID).NotTo(BeNil())
        })
    })
})
```

**Step 2: Run tests to verify they pass**

Run: `task test:integration -- -v ./test/integration/world/... -ginkgo.focus="Object Handling"`

Expected: All specs pass

**Step 3: Commit**

```bash
git add test/integration/world/objects_test.go
git commit -m "feat(world): add Object Handling BDD feature specs"
```

---

## Phase 4: Verification

### Task 4.1: Run Full Test Suite

**Step 1: Run all world model integration tests**

Run: `task test:integration -- -v ./test/integration/world/... -ginkgo.v`

Expected: All specs pass (approximately 80+ specs)

**Step 2: Verify no regressions in existing tests**

Run: `task test`

Expected: All unit tests pass

**Step 3: Final commit with updated design document status**

Update `docs/plans/2026-01-23-world-model-bdd-tests-design.md` status from Draft to Implemented.

```bash
git add docs/plans/2026-01-23-world-model-bdd-tests-design.md
git commit -m "docs(world): mark BDD tests design as implemented"
```

---

## Summary

| Phase | Tasks   | Description                                              |
| ----- | ------- | -------------------------------------------------------- |
| 1     | 1.1     | Test infrastructure (suite setup, helpers)               |
| 2     | 2.1-2.4 | Repository BDD specs (Location, Exit, Object, Scene)     |
| 3     | 3.1-3.4 | Feature BDD specs (Locations, Scenes, Movement, Objects) |
| 4     | 4.1     | Full suite verification                                  |

**Total: 9 files, ~1200 lines of BDD specs**

## Test Commands Reference

```bash
# Run all world integration tests
task test:integration -- -v ./test/integration/world/...

# Run with verbose Ginkgo output
task test:integration -- -v ./test/integration/world/... -ginkgo.v

# Run specific feature
task test:integration -- -v ./test/integration/world/... -ginkgo.focus="Movement"

# Run specific repository
task test:integration -- -v ./test/integration/world/... -ginkgo.focus="LocationRepository"
```
