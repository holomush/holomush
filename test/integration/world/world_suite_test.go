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

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/test/testutil"
)

var suiteT *testing.T

// delErr discards the *wmodel.MutationDelta a world repository write now returns,
// yielding just the error — a mechanical 05-14 test bridge (behavior-preserving).
func delErr(_ *wmodel.MutationDelta, err error) error { return err }

// seedSceneParticipant inserts a scene_participants row directly. The world-layer
// scene-participant write surface was removed in 05-14 (D-07); reads still
// SELECT/JOIN the kept public.scene_participants table, so read specs seed via SQL.
func seedSceneParticipant(ctx context.Context, pool *pgxpool.Pool, sceneID, characterID ulid.ULID, role world.ParticipantRole) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO scene_participants (scene_id, character_id, role, joined_at)
		VALUES ($1, $2, $3, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
		ON CONFLICT (scene_id, character_id) DO UPDATE SET role = $3
	`, sceneID.String(), characterID.String(), role.String())
	return err
}

func TestWorld(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "World Model Integration Suite")
}

// testEnv holds all resources needed for integration tests.
type testEnv struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	eventStore *store.PostgresEventStore

	// Repositories
	Locations  *worldpg.LocationRepository
	Exits      *worldpg.ExitRepository
	Objects    *worldpg.ObjectRepository
	Scenes     *worldpg.SceneRepository
	Characters *worldpg.CharacterRepository
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

	shared := testutil.SharedPostgres(suiteT)
	connStr := testutil.FreshDatabase(suiteT, shared)

	eventStore, err := store.NewPostgresEventStore(ctx, connStr)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		eventStore.Close()
		return nil, err
	}

	return &testEnv{
		ctx:        ctx,
		pool:       pool,
		eventStore: eventStore,
		Locations:  worldpg.NewLocationRepository(pool),
		Exits:      worldpg.NewExitRepository(pool),
		Objects:    worldpg.NewObjectRepository(pool),
		Scenes:     worldpg.NewSceneRepository(pool),
		Characters: worldpg.NewCharacterRepository(pool),
	}, nil
}

func (e *testEnv) cleanup() {
	if e.pool != nil {
		e.pool.Close()
	}
	if e.eventStore != nil {
		e.eventStore.Close()
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
	obj, err := world.NewObjectWithID(core.NewULID(), name, containment)
	Expect(err).NotTo(HaveOccurred(), "failed to create test object")
	obj.Description = description
	return obj
}

// createTestCharacterID creates a real character in the database for testing.
// It creates both a player and character record, returning the character ID.
// This function uses GinkgoRecover to handle panics from Expect.
func createTestCharacterID() ulid.ULID {
	ctx := context.Background()
	playerID := core.NewULID()
	charID := core.NewULID()

	// Need a location for the character - create one if needed
	locID := core.NewULID()
	_, err := env.pool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy)
		VALUES ($1, 'Test Location', 'For character creation.', 'persistent', 'last:0')`,
		locID.String())
	Expect(err).NotTo(HaveOccurred(), "failed to create location for character")

	// Create player (use full charID to ensure unique username)
	_, err = env.pool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash)
		VALUES ($1, $2, 'test_hash')`,
		playerID.String(), "testplayer_"+charID.String())
	Expect(err).NotTo(HaveOccurred(), "failed to create player")

	// Create character
	_, err = env.pool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, location_id)
		VALUES ($1, $2, $3, $4)`,
		charID.String(), playerID.String(), "TestChar_"+charID.String()[:8], locID.String())
	Expect(err).NotTo(HaveOccurred(), "failed to create character")

	return charID
}

// cleanupLocations removes all locations from the test database.
func cleanupLocations(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, "DELETE FROM exits")
	_, _ = pool.Exec(ctx, "DELETE FROM objects")
	_, _ = pool.Exec(ctx, "DELETE FROM scene_participants")
	_, _ = pool.Exec(ctx, "DELETE FROM sessions")
	_, _ = pool.Exec(ctx, "DELETE FROM player_character_bindings")
	_, _ = pool.Exec(ctx, "DELETE FROM characters")
	_, _ = pool.Exec(ctx, "DELETE FROM locations")
	_, _ = pool.Exec(ctx, "DELETE FROM players")
}
