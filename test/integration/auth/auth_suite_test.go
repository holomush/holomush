// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/naming"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telnet"
	"github.com/holomush/holomush/internal/web"
	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/test/testutil"
)

// delErr discards the *wmodel.MutationDelta a world repository write now returns,
// yielding just the error — a mechanical 05-14 test bridge (behavior-preserving).
func delErr(_ *wmodel.MutationDelta, err error) error { return err }

var suiteT *testing.T

func TestPlayerSessionLifecycle(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Player Session Lifecycle Integration Suite")
}

// testEnv holds all resources needed for integration tests.
type testEnv struct {
	ctx  context.Context
	pool *pgxpool.Pool

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

	// In-process gateway+core stack (for multi-tab integration tests).
	coreServer *holoGRPC.CoreServer
	webHandler *web.Handler

	// guestStartLocationID is the location ULID wired into the GuestService's
	// namer at suite setup. The locations row with this ID MUST exist before
	// any spec calls WebCreateGuest (the cleanupTestData helper deletes
	// locations between specs, so specs are responsible for re-creating it
	// in their BeforeEach). See multi_tab_test.go for the canonical pattern.
	guestStartLocationID ulid.ULID
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

	shared := testutil.SharedPostgres(suiteT)
	connStr := testutil.FreshDatabase(suiteT, shared)

	eventStore, err := store.NewPostgresEventStore(ctx, connStr)
	if err != nil {
		return nil, err
	}

	pool := eventStore.Pool()

	playerSessionStore := store.NewPostgresPlayerSessionStore(pool)
	playerRepo := authpg.NewPlayerRepository(pool)
	hasher := auth.NewArgon2idHasher()

	authService, err := auth.NewAuthService(playerRepo, playerSessionStore, hasher)
	if err != nil {
		eventStore.Close()
		return nil, err
	}

	worldCharRepo := worldpg.NewCharacterRepository(pool)
	charRepo := &authCharRepoAdapter{pool: pool, charRepo: worldCharRepo}
	sessionStore := store.NewPostgresSessionStore(pool)

	// Build the in-process gateway+core stack. Auth/multi-tab specs exercise
	// the unary RPC surface only; the dispatcher and command services are
	// non-nil to satisfy NewCoreServer's invariant but use the AllowAll
	// policy engine so command paths are not blocked by ABAC. Mirrors
	// test/integration/phase1_5_test.go newMinimalDispatcher() (lines 45-53).
	pe := policytest.AllowAllEngine()
	dispatcher, err := command.NewDispatcher(command.NewRegistry(), pe)
	if err != nil {
		eventStore.Close()
		return nil, oops.Wrap(err)
	}
	cmdServices := command.NewTestServices(command.ServicesConfig{Engine: pe})

	// Wire a real *auth.GuestService so WebCreateGuest can succeed in
	// multi-tab specs. Without this, CoreServer.CreateGuest short-circuits
	// with Success=false, ErrorMessage="guest login not configured" (see
	// internal/grpc/auth_handlers.go:578-586). The start-location ULID is
	// recorded on testEnv so specs can create the FK target row in BeforeEach.
	guestStartLocationID := ulid.Make()
	guestAuth := telnet.NewGuestAuthenticator(naming.NewGemstoneElementTheme(), guestStartLocationID)
	guestBindingRepo := worldpg.NewBindingRepository(pool)
	guestTransactor := worldpg.NewTransactor(pool)
	guestGenesis, err := auth.NewCharacterGenesisService(worldCharRepo, guestTransactor, guestBindingRepo, worldpg.NewOutboxStore(pool))
	if err != nil {
		eventStore.Close()
		return nil, oops.Wrap(err)
	}
	guestService, err := auth.NewGuestService(guestAuth, playerRepo, charRepo, playerSessionStore, guestGenesis)
	if err != nil {
		eventStore.Close()
		return nil, oops.Wrap(err)
	}

	// Wire a real *core.Engine. WebSelectCharacter → SelectCharacter calls
	// engine.HandleConnect (internal/grpc/auth_handlers.go:310), which would
	// nil-deref a nil engine. Mirrors test/integration/phase1_5_test.go:257
	// (eventStore := &noopEventStore{}; engine := core.NewEngine(eventStore)).
	eventStoreNoop := &noopEventStore{}
	engine := core.NewEngine(eventStoreNoop)

	coreServer := holoGRPC.NewCoreServer(
		engine,
		sessionStore,
		dispatcher,
		cmdServices,
		holoGRPC.WithAuthService(authService),
		holoGRPC.WithPlayerSessionRepo(playerSessionStore),
		holoGRPC.WithPlayerRepo(playerRepo),
		holoGRPC.WithCharacterRepo(charRepo),
		holoGRPC.WithSessionStore(sessionStore),
		holoGRPC.WithGuestService(guestService),
		// Wire a stub Subscriber so Subscribe gets past the early
		// nil-subscriber guard at internal/grpc/server.go:657 and reaches
		// the ownership-validation path that the multi-tab Subscribe-path
		// post-logout spec asserts on. The stub is never actually invoked
		// in any spec — every Subscribe call in this suite uses a
		// stale/invalid token, so ValidateSessionOwnership rejects before
		// OpenSession would be called.
		holoGRPC.WithSubscriber(&unusedSubscriber{}),
	)

	webHandler := web.NewHandler(&coreClientShim{s: coreServer})

	return &testEnv{
		ctx:                  ctx,
		pool:                 pool,
		playerSessionStore:   playerSessionStore,
		playerRepo:           playerRepo,
		charRepo:             charRepo,
		locRepo:              worldpg.NewLocationRepository(pool),
		sessionStore:         sessionStore,
		eventStore:           eventStore,
		authService:          authService,
		hasher:               hasher,
		coreServer:           coreServer,
		webHandler:           webHandler,
		guestStartLocationID: guestStartLocationID,
	}, nil
}

func (e *testEnv) cleanup() {
	if e.eventStore != nil {
		e.eventStore.Close()
	}
}

// cleanupTestData removes all test data between specs in FK-safe order.
// session_connections → player_sessions → sessions → player_character_bindings → characters → locations → players
func cleanupTestData(ctx context.Context, pool *pgxpool.Pool) {
	_, err := pool.Exec(ctx, "DELETE FROM session_connections")
	Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(ctx, "DELETE FROM player_sessions")
	Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(ctx, "DELETE FROM sessions")
	Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(ctx, "DELETE FROM player_character_bindings")
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
	_, err := env.locRepo.Create(ctx, loc)
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
	// Discards the *wmodel.MutationDelta return (05-14 wave-1 compatibility bridge).
	_, err := a.charRepo.Create(ctx, char)
	return err
}

func (a *authCharRepoAdapter) ExistsByName(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := a.pool.QueryRow(
		ctx,
		"SELECT EXISTS(SELECT 1 FROM characters WHERE LOWER(name) = LOWER($1))", name,
	).Scan(&exists)
	if err != nil {
		return false, oops.Code("CHARACTER_EXISTS_CHECK_FAILED").With("name", name).Wrap(err)
	}
	return exists, nil
}

func (a *authCharRepoAdapter) CountByPlayer(ctx context.Context, playerID ulid.ULID) (int, error) {
	var count int
	err := a.pool.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM characters WHERE player_id = $1", playerID.String(),
	).Scan(&count)
	if err != nil {
		return 0, oops.Code("CHARACTER_COUNT_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	return count, nil
}

func (a *authCharRepoAdapter) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error) {
	rows, err := a.pool.Query(
		ctx,
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
		var createdAt pgnanos.Time
		if scanErr := rows.Scan(&idStr, &pidStr, &c.Name, &c.Description, &locStr, &createdAt); scanErr != nil {
			return nil, oops.Code("CHARACTER_SCAN_FAILED").Wrap(scanErr)
		}
		c.CreatedAt = createdAt.Time()
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

func (a *authCharRepoAdapter) ListAll(ctx context.Context) ([]*world.Character, error) {
	return a.charRepo.ListAll(ctx)
}

// Compile-time interface check.
var _ auth.CharacterRepository = (*authCharRepoAdapter)(nil)

// noopEventStore is a stub EventAppender for tests that don't exercise event
// functionality. Mirrors test/integration/phase1_5_test.go:36-41.
type noopEventStore struct{}

func (n *noopEventStore) Append(_ context.Context, _ core.Event) error { return nil }

var _ core.EventAppender = (*noopEventStore)(nil)

// unusedSubscriber satisfies eventbus.Subscriber so the Subscribe handler
// reaches the ownership-validation path. Returns an error if OpenSession
// is ever called — that would mean a spec advanced past validation, which
// no spec in this suite is meant to do (every Subscribe call uses a
// stale/invalid token). The returned error is distinctively coded so a
// reorder regression in CoreServer.Subscribe (validation moved after
// OpenSession) would surface as TEST_SUITE_BUG, not as the SESSION_NOT_FOUND
// the spec asserts on.
type unusedSubscriber struct{}

func (unusedSubscriber) OpenSession(_ context.Context, _ string, _ eventbus.SessionIdentity, _ []eventbus.Subject, _ time.Time) (eventbus.SessionStream, error) {
	return nil, oops.Code("TEST_SUITE_BUG").Errorf("unusedSubscriber.OpenSession invoked: a spec reached the subscriber call without expecting to")
}

var _ eventbus.Subscriber = unusedSubscriber{}
