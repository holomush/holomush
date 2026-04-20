// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// TODO(holomush-1tvn.14): F7 deletes this file along with EventStore.{Append,Replay,SubscribeSession,LastEventID}
//go:build integration && f6_legacy

// Package session_lifecycle_test contains integration tests for session
// lifecycle events (session_ended). These tests exercise real end-to-end paths
// through a live gRPC server backed by PostgreSQL (testcontainers) and verify
// that session_ended events are emitted with the correct payload, are correctly
// isolated to the owning session, and do not self-terminate a new Subscribe.
package session_lifecycle_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/core"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/naming"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telnet"
	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	"github.com/holomush/holomush/test/testutil"
)

var suiteT *testing.T

func TestSessionLifecycleIntegration(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Session Lifecycle as Events E2E Suite")
}

// suiteEnv holds the resources shared across all specs in the suite.
// The shared Postgres container and a fresh per-suite database are
// obtained once in BeforeSuite. Per-spec state (engine, gRPC server)
// is constructed in BeforeEach against the shared pool.
type suiteEnv struct {
	ctx                context.Context
	pool               *pgxpool.Pool
	eventStore         *store.PostgresEventStore
	sessionStore       *store.PostgresSessionStore
	playerSessionStore *store.PostgresPlayerSessionStore
	playerRepo         *authpg.PlayerRepository
	charRepo           *bootstrapsetup.CharRepoAdapter
	locRepo            *worldpg.LocationRepository
}

var env *suiteEnv

var _ = BeforeSuite(func() {
	ctx := context.Background()

	shared := testutil.SharedPostgres(suiteT)
	connStr := testutil.FreshDatabase(suiteT, shared)

	eventStore, err := store.NewPostgresEventStore(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())

	pool := eventStore.Pool()
	Expect(pool).NotTo(BeNil())

	env = &suiteEnv{
		ctx:                ctx,
		pool:               pool,
		eventStore:         eventStore,
		sessionStore:       store.NewPostgresSessionStore(pool),
		playerSessionStore: store.NewPostgresPlayerSessionStore(pool),
		playerRepo:         authpg.NewPlayerRepository(pool),
		charRepo:           bootstrapsetup.NewCharRepoAdapter(pool, worldpg.NewCharacterRepository(pool)),
		locRepo:            worldpg.NewLocationRepository(pool),
	}
})

var _ = AfterSuite(func() {
	if env == nil {
		return
	}
	if env.eventStore != nil {
		env.eventStore.Close()
	}
})

// cleanupTestData removes all test data between specs in FK-safe order.
func cleanupTestData(ctx context.Context, pool *pgxpool.Pool) {
	tables := []string{
		"session_connections",
		"player_sessions",
		"sessions",
		"events",
		"characters",
		"exits",
		"locations",
		"players",
	}
	for _, table := range tables {
		_, err := pool.Exec(ctx, "DELETE FROM "+table)
		Expect(err).NotTo(HaveOccurred(), "failed to clean table %s", table)
	}
}

// specServer holds a running gRPC server and client for a single spec.
type specServer struct {
	grpcServer    *grpc.Server
	grpcCli       *grpcpkg.Client
	engine        *core.Engine
	guestAuth     *telnet.GuestAuthenticator
	startLocation ulid.ULID
}

// newSpecServer constructs a fresh in-process gRPC CoreServer backed by the
// shared Postgres pool. Call teardown() in AfterEach.
func newSpecServer(testCtx context.Context) *specServer {
	startLocation := core.NewULID()
	loc := &world.Location{
		ID:           startLocation,
		Name:         "Test Origin",
		Description:  "Integration test starting location",
		Type:         world.LocationTypePersistent,
		ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
	}
	Expect(env.locRepo.Create(testCtx, loc)).To(Succeed())

	engine := core.NewEngine(env.eventStore)
	guestAuth := telnet.NewGuestAuthenticator(naming.NewGemstoneElementTheme(), startLocation)

	guestService, gsErr := auth.NewGuestService(
		guestAuth,
		env.playerRepo,
		env.charRepo,
		env.playerSessionStore,
	)
	Expect(gsErr).NotTo(HaveOccurred())

	// AuthService is required by the Logout RPC to invalidate the player session.
	// WithGameSessionFanout wires up session_ended fanout for evicted sessions.
	authSvc, authSvcErr := auth.NewAuthService(
		env.playerRepo,
		env.playerSessionStore,
		auth.NewArgon2idHasher(),
		auth.WithGameSessionFanout(engine, env.sessionStore),
	)
	Expect(authSvcErr).NotTo(HaveOccurred())

	policyEngine := policytest.AllowAllEngine()
	reg := command.NewRegistry()
	handlers.RegisterAll(reg)
	cmdSvc := command.NewTestServices(command.ServicesConfig{
		Engine:  policyEngine,
		Session: env.sessionStore,
		Events:  env.eventStore,
	})
	dispatcher, dispErr := command.NewDispatcher(reg, policyEngine)
	Expect(dispErr).NotTo(HaveOccurred())

	coreServer := grpcpkg.NewCoreServer(engine, env.sessionStore, dispatcher, cmdSvc,
		grpcpkg.WithEventStore(env.eventStore),
		grpcpkg.WithGuestService(guestService),
		grpcpkg.WithAuthService(authSvc),
		grpcpkg.WithPlayerRepo(env.playerRepo),
		grpcpkg.WithPlayerSessionRepo(env.playerSessionStore),
		grpcpkg.WithCharacterRepo(env.charRepo),
		grpcpkg.WithDisconnectHook(func(info session.Info) {
			if info.IsGuest {
				guestAuth.ReleaseGuest(info.CharacterName)
			}
		}),
	)

	grpcServer := grpcpkg.NewGRPCServerInsecure()
	corev1.RegisterCoreServiceServer(grpcServer, coreServer)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	go func() { _ = grpcServer.Serve(lis) }()

	grpcAddr := lis.Addr().String()
	Eventually(func() bool {
		conn, dialErr := net.DialTimeout("tcp", grpcAddr, 100*time.Millisecond)
		if dialErr != nil {
			return false
		}
		_ = conn.Close()
		return true
	}).Should(BeTrue())

	grpcCli, err := grpcpkg.NewClient(testCtx, grpcpkg.ClientConfig{Address: grpcAddr})
	Expect(err).NotTo(HaveOccurred())

	return &specServer{
		grpcServer:    grpcServer,
		grpcCli:       grpcCli,
		engine:        engine,
		guestAuth:     guestAuth,
		startLocation: startLocation,
	}
}

// teardown shuts down the gRPC server and closes the client connection.
func (s *specServer) teardown() {
	if s.grpcCli != nil {
		_ = s.grpcCli.Close()
	}
	if s.grpcServer != nil {
		s.grpcServer.Stop()
	}
}

// loginAsGuest performs the two-phase guest login and returns the resulting
// game session ID, character name, and player_session_token.
func loginAsGuest(ctx context.Context, cli *grpcpkg.Client) (sessionID, charName, token string) {
	guestResp, err := cli.CreateGuest(ctx, &corev1.CreateGuestRequest{})
	Expect(err).NotTo(HaveOccurred())
	Expect(guestResp.Success).To(BeTrue(), "guest creation should succeed: %s", guestResp.ErrorMessage)
	Expect(guestResp.Characters).To(HaveLen(1))
	Expect(guestResp.DefaultCharacterId).NotTo(BeEmpty())
	Expect(guestResp.PlayerSessionToken).NotTo(BeEmpty())

	selectResp, err := cli.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: guestResp.PlayerSessionToken,
		CharacterId:        guestResp.DefaultCharacterId,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(selectResp.Success).To(BeTrue(),
		"character selection should succeed: %s", selectResp.ErrorMessage)
	Expect(selectResp.SessionId).NotTo(BeEmpty())

	return selectResp.SessionId, selectResp.CharacterName, guestResp.PlayerSessionToken
}
