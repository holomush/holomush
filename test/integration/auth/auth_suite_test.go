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
	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/eventbus"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/naming"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/presence"
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
	guestGenesis, err := auth.NewCharacterGenesisService(worldCharRepo, guestTransactor, guestBindingRepo, worldpg.NewOutboxStore(pool), worldpg.NewReapingGuard(pool))
	if err != nil {
		eventStore.Close()
		return nil, oops.Wrap(err)
	}
	guestReaping, err := auth.NewCharacterReapingService(worldCharRepo, worldCharRepo, worldpg.NewPropertyRepository(pool), guestBindingRepo, guestTransactor, worldpg.NewOutboxStore(pool), playerRepo, playerRepo)
	if err != nil {
		eventStore.Close()
		return nil, oops.Wrap(err)
	}
	suiteNameGate := &charname.Gate{Skeletons: worldpg.NewSkeletonLookup(pool)}
	guestService, err := auth.NewGuestService(guestAuth, playerRepo, charRepo, playerSessionStore, guestGenesis, guestReaping, suiteNameGate)
	if err != nil {
		eventStore.Close()
		return nil, oops.Wrap(err)
	}

	// Wire a real *presence.Emitter. WebSelectCharacter → SelectCharacter calls
	// presence.EmitArrive (internal/grpc/auth_handlers.go:310), which would
	// nil-deref a nil presence emitter. Mirrors
	// test/integration/phase1_5_test.go:257
	// (pub := &noopPublisher{}; pres := presence.NewEmitter(pub, gameID)).
	presenceEmitter := presence.NewEmitter(&noopPublisher{}, func() string { return "main" })

	coreServer := holoGRPC.NewCoreServer(
		presenceEmitter,
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
	err = env.charRepo.Create(ctx, char, admitSuiteName(ctx, name))
	Expect(err).NotTo(HaveOccurred())
	return char
}

// admitSuiteName mints a real charname.Admitted for a suite fixture name.
//
// charname.Admitted has exactly one constructor by design (plan 02-06), so a
// fixture runs a real gate. The corpus is repaired first because a freshly
// migrated database carries the 000001_baseline bootstrap character with no
// name_skeleton, and the gate correctly refuses to adjudicate against a corpus
// it cannot verify (D-30).
func admitSuiteName(ctx context.Context, name string) charname.Admitted {
	backfillSuiteSkeletons(ctx, env.pool)
	gate := &charname.Gate{Skeletons: worldpg.NewSkeletonLookup(env.pool)}
	admitted, err := gate.Admit(ctx, name)
	Expect(err).NotTo(HaveOccurred())
	return admitted
}

// backfillSuiteSkeletons stands in for plan 02-12's 000055 Go migration.
//
// It takes the pool explicitly because the gate specs run against their own
// fresh database rather than the suite's shared env.pool.
func backfillSuiteSkeletons(ctx context.Context, pool *pgxpool.Pool) {
	GinkgoHelper()
	rows, err := pool.Query(ctx, `
		SELECT id, name FROM characters
		WHERE normalized_name IS NULL
		   OR name_skeleton IS NULL
		   OR name_skeleton_unicode_version IS NULL
	`)
	Expect(err).NotTo(HaveOccurred())
	type pending struct{ id, key, skeleton string }
	var todo []pending
	for rows.Next() {
		var id, name string
		Expect(rows.Scan(&id, &name)).To(Succeed())
		normalized, nErr := charname.Normalize(name)
		if nErr != nil {
			todo = append(todo, pending{id: id, key: id, skeleton: id})
			continue
		}
		todo = append(todo, pending{id: id, key: normalized.Key, skeleton: charname.Skeleton(normalized.Key)})
	}
	Expect(rows.Err()).NotTo(HaveOccurred())
	rows.Close()
	for _, p := range todo {
		_, execErr := pool.Exec(ctx, `
			UPDATE characters
			SET normalized_name = $2, name_skeleton = $3, name_skeleton_unicode_version = $4
			WHERE id = $1
		`, p.id, p.key, p.skeleton, charname.UnicodeVersion)
		Expect(execErr).NotTo(HaveOccurred())
	}
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

func (a *authCharRepoAdapter) Create(ctx context.Context, char *world.Character, name charname.Admitted) error {
	// Discards the *wmodel.MutationDelta return (05-14 wave-1 compatibility bridge).
	_, err := a.charRepo.Create(ctx, char, name)
	return err
}

// ExistsByNormalizedName carries the SAME predicate every other site carries;
// see setup.CharRepoAdapter.ExistsByNormalizedName. The transitional NULL branch
// was removed with migration 000056.
func (a *authCharRepoAdapter) ExistsByNormalizedName(ctx context.Context, key string, excluding *ulid.ULID) (bool, error) {
	var excludingArg *string
	if excluding != nil {
		s := excluding.String()
		excludingArg = &s
	}
	var exists bool
	err := a.pool.QueryRow(
		ctx,
		`SELECT EXISTS(
			SELECT 1 FROM characters
			WHERE normalized_name = $1
			  AND ($2::text IS NULL OR id::text <> $2)
		)`, key, excludingArg,
	).Scan(&exists)
	if err != nil {
		return false, oops.Code("CHARACTER_EXISTS_CHECK_FAILED").With("name", key).Wrap(err)
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
		`SELECT id, player_id, name, description, location_id, created_at, status
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
		// status is read because this adapter feeds CoreServer.SelectCharacter,
		// whose lifecycle gate (INV-WORLD-5) fails closed on a blank Status. A
		// test adapter that omitted it would assert against a row shape
		// production no longer produces.
		var statusStr string
		if scanErr := rows.Scan(&idStr, &pidStr, &c.Name, &c.Description, &locStr, &createdAt, &statusStr); scanErr != nil {
			return nil, oops.Code("CHARACTER_SCAN_FAILED").Wrap(scanErr)
		}
		c.CreatedAt = createdAt.Time()
		parsedStatus, statusErr := world.ParseStatus(statusStr)
		if statusErr != nil {
			return nil, oops.Code("CHARACTER_STATUS_DECODE_FAILED").With("status", statusStr).Wrap(statusErr)
		}
		c.Status = parsedStatus
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

// noopPublisher is a stub eventbus.Publisher for tests that don't exercise
// event functionality. Mirrors test/integration/phase1_5_test.go.
type noopPublisher struct{}

func (n *noopPublisher) Publish(_ context.Context, _ eventbus.Event) error { return nil }

var _ eventbus.Publisher = (*noopPublisher)(nil)

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
