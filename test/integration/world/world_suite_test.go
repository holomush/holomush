// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/presence"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/testsupport/chartest"
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

	// Lifecycle-proof collaborators (02-04). These stand up the REAL services
	// the INV-WORLD-5/-6 specs must drive: a genuine ABAC engine over the seeded
	// policy corpus, the authorized world.Service delete, the guest-reaping
	// deleter, the real character-creation path the name-reclaim attempt goes
	// through, and the CoreServer whose SelectCharacter is the tree's ONLY
	// character-selection path.
	engine       *policy.Engine
	roleResolver *staticWorldRoleResolver
	worldService *world.Service
	reaping      *auth.CharacterReapingService
	characters   *auth.CharacterService
	// startLocationID is the LocRepoAdapter's starting-location pointer target.
	// Specs assign through it in BeforeEach after cleaning locations.
	startLocationID    *ulid.ULID
	coreServer         *holoGRPC.CoreServer
	playerSessionStore *store.PostgresPlayerSessionStore
}

// staticWorldRoleResolver hands the ABAC CharacterProvider a per-subject role
// list, so a spec can grant the "admin" role the seeded corpus already keys the
// character-delete permit on. The ENGINE and the POLICY are real; only the role
// source is a fixture — a canned-decision engine would make "the delete released
// the name" pass because nothing was authorizing anything.
type staticWorldRoleResolver struct {
	mu    sync.Mutex
	roles map[string][]string
}

func (s *staticWorldRoleResolver) GetRoles(_ context.Context, subject string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.roles[subject]
}

func (s *staticWorldRoleResolver) Grant(subject string, roles ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roles[subject] = roles
}

// noopWorldSessionResolver fails every session resolution; the lifecycle specs
// address subjects as characters, never as sessions.
type noopWorldSessionResolver struct{}

func (n *noopWorldSessionResolver) ResolveSession(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("session resolution not configured in the world suite")
}

type noopWorldPartitionCreator struct{}

func (n *noopWorldPartitionCreator) EnsurePartitions(_ context.Context, _ int) error { return nil }

// discardAuditWriter satisfies the audit sink the policy engine requires.
type discardAuditWriter struct{}

func (discardAuditWriter) WriteSync(_ context.Context, _ audit.Event) error { return nil }
func (discardAuditWriter) WriteAsync(_ audit.Event) error                   { return nil }
func (discardAuditWriter) Close() error                                     { return nil }

// noopWorldPublisher backs the presence emitter CoreServer.SelectCharacter
// calls on the success path. The lifecycle specs assert on the RPC response,
// never on emitted events.
type noopWorldPublisher struct{}

func (n *noopWorldPublisher) Publish(_ context.Context, _ eventbus.Event) error { return nil }

var _ eventbus.Publisher = (*noopWorldPublisher)(nil)

// unusedWorldSubscriber satisfies the Subscribe wiring; no spec here streams.
type unusedWorldSubscriber struct{}

func (u *unusedWorldSubscriber) OpenSession(_ context.Context, _ string, _ eventbus.SessionIdentity, _ []eventbus.Subject, _ time.Time) (eventbus.SessionStream, error) {
	return nil, fmt.Errorf("subscribe not implemented in the world suite")
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

	// Repair the character-name corpus: 000001_baseline seeds a bootstrap
	// character with no name_skeleton, and charname.Gate correctly refuses to
	// adjudicate against a corpus it cannot verify (D-30). Stands in for plan
	// 02-12's 000055 Go migration.
	if bErr := backfillWorldSuiteSkeletons(ctx, pool); bErr != nil {
		pool.Close()
		eventStore.Close()
		return nil, bErr
	}

	locRepo := worldpg.NewLocationRepository(pool)
	charRepo := worldpg.NewCharacterRepository(pool)

	lifecycle, err := setupLifecycleServices(ctx, pool, locRepo, charRepo)
	if err != nil {
		pool.Close()
		eventStore.Close()
		return nil, err
	}

	lifecycle.ctx = ctx
	lifecycle.pool = pool
	lifecycle.eventStore = eventStore
	lifecycle.Locations = locRepo
	lifecycle.Exits = worldpg.NewExitRepository(pool)
	lifecycle.Objects = worldpg.NewObjectRepository(pool)
	lifecycle.Scenes = worldpg.NewSceneRepository(pool)
	lifecycle.Characters = charRepo
	return lifecycle, nil
}

// setupLifecycleServices assembles the REAL services the INV-WORLD-5/-6 specs
// drive. It mirrors internal/testsupport/integrationtest/harness.go's CoreServer
// assembly and test/integration/access/access_suite_test.go's engine assembly,
// with one deliberate difference: the policy engine here is genuinely real
// (real providers, real compiled seed corpus) rather than an allow-all fake, so
// the authorized character delete is authorized by a policy rather than by a
// stub that would permit anything.
func setupLifecycleServices(
	ctx context.Context,
	pool *pgxpool.Pool,
	locRepo *worldpg.LocationRepository,
	charRepo *worldpg.CharacterRepository,
) (*testEnv, error) {
	pStore := policystore.NewPostgresStore(pool)
	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)

	roleResolver := &staticWorldRoleResolver{roles: make(map[string][]string)}
	if err := resolver.RegisterProvider(attribute.NewCharacterProvider(charRepo, roleResolver)); err != nil {
		return nil, err
	}
	if err := resolver.RegisterProvider(attribute.NewLocationProvider(locRepo)); err != nil {
		return nil, err
	}

	compiler := policy.NewCompiler(registry.Schema())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := policy.Bootstrap(ctx, &noopWorldPartitionCreator{}, pStore, compiler, logger, policy.BootstrapOptions{}); err != nil {
		return nil, err
	}
	cache := policy.NewCache(pStore, compiler)
	if err := cache.Reload(ctx); err != nil {
		return nil, err
	}
	auditLogger := audit.NewLogger(audit.ModeAll, discardAuditWriter{},
		filepath.Join(os.TempDir(), fmt.Sprintf("holomush-world-lifecycle-audit-%d.jsonl", os.Getpid())))
	engine := policy.NewEngine(resolver, cache, &noopWorldSessionResolver{}, auditLogger)

	transactor := worldpg.NewTransactor(pool)
	propRepo := worldpg.NewPropertyRepository(pool)
	outbox := worldpg.NewOutboxStore(pool)
	bindingRepo := worldpg.NewBindingRepository(pool)

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo:  locRepo,
		ExitRepo:      worldpg.NewExitRepository(pool),
		ObjectRepo:    worldpg.NewObjectRepository(pool),
		SceneRepo:     worldpg.NewSceneRepository(pool),
		CharacterRepo: charRepo,
		PropertyRepo:  propRepo,
		Engine:        engine,
		Transactor:    transactor,
		OutboxWriter:  outbox,
	})

	playerRepo := authpg.NewPlayerRepository(pool)
	reaping, err := auth.NewCharacterReapingService(
		charRepo, charRepo, propRepo, bindingRepo, transactor, outbox, playerRepo, playerRepo,
	)
	if err != nil {
		return nil, err
	}

	genesis, err := auth.NewCharacterGenesisService(
		charRepo, transactor, bindingRepo, outbox, worldpg.NewReapingGuard(pool),
	)
	if err != nil {
		return nil, err
	}

	startLocationID := new(ulid.ULID)
	authCharRepo := bootstrapsetup.NewCharRepoAdapter(pool, charRepo)
	characters, err := auth.NewCharacterService(
		authCharRepo, bootstrapsetup.NewLocRepoAdapter(startLocationID, locRepo), genesis,
		&charname.Gate{Skeletons: worldpg.NewSkeletonLookup(pool)},
	)
	if err != nil {
		return nil, err
	}

	// The CoreServer whose SelectCharacter is the ONLY character-selection path
	// in the tree, and therefore the only seam INV-WORLD-5's "excluded from
	// character selection" clause can honestly be bound against.
	dispatcher, err := command.NewDispatcher(command.NewRegistry(), engine)
	if err != nil {
		return nil, err
	}
	playerSessionStore := store.NewPostgresPlayerSessionStore(pool)
	sessionStore := store.NewPostgresSessionStore(pool)
	coreServer := holoGRPC.NewCoreServer(
		presence.NewEmitter(&noopWorldPublisher{}, func() string { return "main" }),
		sessionStore,
		dispatcher,
		command.NewTestServices(command.ServicesConfig{Engine: engine}),
		holoGRPC.WithPlayerSessionRepo(playerSessionStore),
		holoGRPC.WithPlayerRepo(playerRepo),
		holoGRPC.WithCharacterRepo(authCharRepo),
		holoGRPC.WithSessionStore(sessionStore),
		holoGRPC.WithSubscriber(&unusedWorldSubscriber{}),
	)

	return &testEnv{
		engine:             engine,
		roleResolver:       roleResolver,
		worldService:       worldService,
		reaping:            reaping,
		characters:         characters,
		startLocationID:    startLocationID,
		coreServer:         coreServer,
		playerSessionStore: playerSessionStore,
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
		INSERT INTO characters (id, player_id, name, location_id, normalized_name, name_skeleton, name_skeleton_unicode_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		append([]any{charID.String(), playerID.String(), "TestChar_" + charID.String()[20:], locID.String()},
			chartest.Columns("TestChar_"+charID.String()[20:])...)...)
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

// backfillWorldSuiteSkeletons populates the identity columns of every characters
// row missing them.
func backfillWorldSuiteSkeletons(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `
		SELECT id, name FROM characters
		WHERE normalized_name IS NULL
		   OR name_skeleton IS NULL
		   OR name_skeleton_unicode_version IS NULL
	`)
	if err != nil {
		return err
	}
	type pending struct{ id, key, skeleton string }
	var todo []pending
	for rows.Next() {
		var id, name string
		if scanErr := rows.Scan(&id, &name); scanErr != nil {
			rows.Close()
			return scanErr
		}
		normalized, nErr := charname.Normalize(name)
		if nErr != nil {
			todo = append(todo, pending{id: id, key: id, skeleton: id})
			continue
		}
		todo = append(todo, pending{id: id, key: normalized.Key, skeleton: charname.Skeleton(normalized.Key)})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		rows.Close()
		return rowsErr
	}
	rows.Close()
	for _, p := range todo {
		if _, execErr := pool.Exec(ctx, `
			UPDATE characters
			SET normalized_name = $2, name_skeleton = $3, name_skeleton_unicode_version = $4
			WHERE id = $1
		`, p.id, p.key, p.skeleton, charname.UnicodeVersion); execErr != nil {
			return execErr
		}
	}
	return nil
}
