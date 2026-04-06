// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package auth_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"
	"github.com/testcontainers/testcontainers-go"

	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

func TestPlayerSessionLifecycle(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Player Session Lifecycle Integration Suite")
}

// testEnv holds all resources needed for integration tests.
type testEnv struct {
	ctx       context.Context
	pool      *pgxpool.Pool
	container testcontainers.Container

	// Stores / repos
	playerSessionStore *store.PostgresPlayerSessionStore
	playerRepo         *authpg.PlayerRepository
	charRepo           *authCharRepoAdapter
	locRepo            *worldpg.LocationRepository
	sessionStore       *store.PostgresSessionStore
	eventStore         *store.PostgresEventStore

	// Services
	authService *auth.Service
	hasher      auth.PasswordHasher
}

var env *testEnv

var _ = BeforeSuite(func() {
	var err error
	env, err = setupTestEnv()
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	if env != nil {
		env.cleanup()
	}
})

func setupTestEnv() (*testEnv, error) {
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	if err != nil {
		return nil, err
	}
	container := pgEnv.Container
	connStr := pgEnv.ConnStr

	migrator, err := store.NewMigrator(connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}
	if err := migrator.Up(); err != nil {
		_ = migrator.Close()
		_ = container.Terminate(ctx)
		return nil, err
	}
	_ = migrator.Close()

	eventStore, err := store.NewPostgresEventStore(ctx, connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}

	pool := eventStore.Pool()

	playerSessionStore := store.NewPostgresPlayerSessionStore(pool)
	playerRepo := authpg.NewPlayerRepository(pool)
	hasher := auth.NewArgon2idHasher()

	authService, err := auth.NewAuthService(playerRepo, playerSessionStore, hasher)
	if err != nil {
		eventStore.Close()
		_ = container.Terminate(ctx)
		return nil, err
	}

	return &testEnv{
		ctx:                ctx,
		pool:               pool,
		container:          container,
		playerSessionStore: playerSessionStore,
		playerRepo:         playerRepo,
		charRepo:           &authCharRepoAdapter{pool: pool, charRepo: worldpg.NewCharacterRepository(pool)},
		locRepo:            worldpg.NewLocationRepository(pool),
		sessionStore:       store.NewPostgresSessionStore(pool),
		eventStore:         eventStore,
		authService:        authService,
		hasher:             hasher,
	}, nil
}

func (e *testEnv) cleanup() {
	if e.eventStore != nil {
		e.eventStore.Close()
	}
	if e.container != nil {
		_ = e.container.Terminate(e.ctx)
	}
}

// cleanupTestData removes all test data between specs in FK-safe order.
// session_connections → player_sessions → sessions → characters → locations → players
func cleanupTestData(ctx context.Context, pool *pgxpool.Pool) {
	_, err := pool.Exec(ctx, "DELETE FROM session_connections")
	Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(ctx, "DELETE FROM player_sessions")
	Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(ctx, "DELETE FROM sessions")
	Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(ctx, "DELETE FROM characters")
	Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(ctx, "DELETE FROM locations")
	Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(ctx, "DELETE FROM players")
	Expect(err).NotTo(HaveOccurred())
}

// createTestPlayer creates a player with hashed password and returns the player + raw password.
func createTestPlayer(ctx context.Context, username, password string) *auth.Player {
	hash, err := env.hasher.Hash(password)
	Expect(err).NotTo(HaveOccurred())

	player, err := auth.NewPlayer(username, nil, hash)
	Expect(err).NotTo(HaveOccurred())

	err = env.playerRepo.Create(ctx, player)
	Expect(err).NotTo(HaveOccurred())

	return player
}

// createTestLocation creates a location in the database.
func createTestLocation(ctx context.Context, name string) *world.Location {
	loc := &world.Location{
		ID:           ulid.Make(),
		Name:         name,
		Description:  "A test location",
		Type:         world.LocationTypePersistent,
		ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
	}
	err := env.locRepo.Create(ctx, loc)
	Expect(err).NotTo(HaveOccurred())
	return loc
}

// createTestCharacter creates a character in the database for a given player at a location.
func createTestCharacter(ctx context.Context, playerID ulid.ULID, name string, locationID ulid.ULID) *world.Character {
	char, err := world.NewCharacter(playerID, name)
	Expect(err).NotTo(HaveOccurred())
	char.LocationID = &locationID
	err = env.charRepo.Create(ctx, char)
	Expect(err).NotTo(HaveOccurred())
	return char
}

// loginPlayer authenticates and creates a player session, returning the raw token and session.
func loginPlayer(ctx context.Context, username, password string) (rawToken string, ps *auth.PlayerSession) {
	player, err := env.authService.ValidateCredentials(ctx, username, password)
	Expect(err).NotTo(HaveOccurred())

	rawToken, tokenHash, err := auth.GenerateSessionToken()
	Expect(err).NotTo(HaveOccurred())

	ps, err = auth.NewPlayerSession(player.ID, tokenHash, "test-agent", "127.0.0.1", auth.PlayerSessionTTL)
	Expect(err).NotTo(HaveOccurred())

	err = env.playerSessionStore.Create(ctx, ps)
	Expect(err).NotTo(HaveOccurred())

	return rawToken, ps
}

// authCharRepoAdapter wraps pgxpool.Pool to implement auth.CharacterRepository.
// Mirrors cmd/holomush/auth_adapters.go for integration test use.
type authCharRepoAdapter struct {
	pool     *pgxpool.Pool
	charRepo *worldpg.CharacterRepository
}

func (a *authCharRepoAdapter) Create(ctx context.Context, char *world.Character) error {
	return a.charRepo.Create(ctx, char)
}

func (a *authCharRepoAdapter) ExistsByName(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := a.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM characters WHERE LOWER(name) = LOWER($1))", name,
	).Scan(&exists)
	if err != nil {
		return false, oops.Code("CHARACTER_EXISTS_CHECK_FAILED").With("name", name).Wrap(err)
	}
	return exists, nil
}

func (a *authCharRepoAdapter) CountByPlayer(ctx context.Context, playerID ulid.ULID) (int, error) {
	var count int
	err := a.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM characters WHERE player_id = $1", playerID.String(),
	).Scan(&count)
	if err != nil {
		return 0, oops.Code("CHARACTER_COUNT_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	return count, nil
}

func (a *authCharRepoAdapter) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error) {
	rows, err := a.pool.Query(ctx,
		`SELECT id, player_id, name, description, location_id, created_at
		 FROM characters WHERE player_id = $1 ORDER BY name`, playerID.String(),
	)
	if err != nil {
		return nil, oops.Code("CHARACTER_LIST_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	defer rows.Close()

	var chars []*world.Character
	for rows.Next() {
		var c world.Character
		var idStr, pidStr string
		var locStr *string
		if scanErr := rows.Scan(&idStr, &pidStr, &c.Name, &c.Description, &locStr, &c.CreatedAt); scanErr != nil {
			return nil, oops.Code("CHARACTER_SCAN_FAILED").Wrap(scanErr)
		}
		var parseErr error
		c.ID, parseErr = ulid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.Code("CHARACTER_ULID_DECODE_FAILED").With("field", "id").Wrap(parseErr)
		}
		c.PlayerID, parseErr = ulid.Parse(pidStr)
		if parseErr != nil {
			return nil, oops.Code("CHARACTER_ULID_DECODE_FAILED").With("field", "player_id").Wrap(parseErr)
		}
		if locStr != nil {
			lid, locParseErr := ulid.Parse(*locStr)
			if locParseErr != nil {
				return nil, oops.Code("CHARACTER_ULID_DECODE_FAILED").With("field", "location_id").Wrap(locParseErr)
			}
			c.LocationID = &lid
		}
		chars = append(chars, &c)
	}
	if rows.Err() != nil {
		return nil, oops.Code("CHARACTER_ROWS_FAILED").Wrap(rows.Err())
	}
	return chars, nil
}

// Compile-time interface check.
var _ auth.CharacterRepository = (*authCharRepoAdapter)(nil)
