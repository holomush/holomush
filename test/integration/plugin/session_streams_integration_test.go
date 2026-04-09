//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugin_test

import (
	"context"
	"net"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/idgen"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/naming"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telnet"
	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// staticContributor is a test-only SessionStreamContributor that returns fixed streams.
type staticContributor struct {
	streams atomic.Pointer[[]string]
}

func newStaticContributor(streams ...string) *staticContributor {
	c := &staticContributor{}
	c.SetStreams(streams...)
	return c
}

func (c *staticContributor) SetStreams(streams ...string) {
	cp := make([]string, len(streams))
	copy(cp, streams)
	c.streams.Store(&cp)
}

func (c *staticContributor) QuerySessionStreams(_ context.Context, _ plugins.SessionStreamsRequest) []string {
	p := c.streams.Load()
	if p == nil {
		return nil
	}
	return *p
}

// collectUntilReplayComplete reads from a Subscribe stream until the
// REPLAY_COMPLETE control signal is received, then returns all event types
// seen so far. It does not close the stream.
func collectUntilReplayComplete(stream corev1.CoreService_SubscribeClient) []string {
	GinkgoHelper()
	var types []string
	for {
		resp, err := stream.Recv()
		Expect(err).NotTo(HaveOccurred(), "receiving from subscribe stream")
		if ctrl := resp.GetControl(); ctrl != nil {
			if ctrl.GetSignal() == corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE {
				return types
			}
		}
		if ev := resp.GetEvent(); ev != nil {
			types = append(types, ev.GetType())
		}
	}
}

var _ = Describe("Plugin Session Stream Contribution", func() {
	var (
		testCtx       context.Context
		testCancel    context.CancelFunc
		container     *postgres.PostgresContainer
		grpcServer    *grpc.Server
		grpcCli       *grpcpkg.Client
		eventStore    *store.PostgresEventStore
		contributor   *staticContributor
		registry      *grpcpkg.SessionStreamRegistry
		startLocation ulid.ULID
		guestAuth     *telnet.GuestAuthenticator
	)

	// hookFn is a pointer so individual tests can swap it before subscribing.
	hookFnPtr := new(func())

	BeforeEach(func() {
		testCtx, testCancel = context.WithTimeout(context.Background(), 5*time.Minute)

		var err error
		container, err = postgres.Run(testCtx,
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
		Expect(err).NotTo(HaveOccurred())

		connStr, err := container.ConnectionString(testCtx, "sslmode=disable")
		Expect(err).NotTo(HaveOccurred())

		migrator, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migrator.Up()).To(Succeed())
		_ = migrator.Close()

		eventStore, err = store.NewPostgresEventStore(testCtx, connStr)
		Expect(err).NotTo(HaveOccurred())

		pool := eventStore.Pool()
		Expect(pool).NotTo(BeNil())
		sessionStore := store.NewPostgresSessionStore(pool)
		playerSessionStore := store.NewPostgresPlayerSessionStore(pool)
		playerRepo := authpg.NewPlayerRepository(pool)
		charRepo := bootstrapsetup.NewCharRepoAdapter(pool, worldpg.NewCharacterRepository(pool))
		locRepo := worldpg.NewLocationRepository(pool)

		startLocation = idgen.New()
		loc := &world.Location{
			ID:           startLocation,
			Name:         "Test Origin",
			Description:  "Integration test starting location",
			Type:         world.LocationTypePersistent,
			ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
		}
		Expect(locRepo.Create(testCtx, loc)).To(Succeed())

		engine := core.NewEngine(eventStore)
		guestAuth = telnet.NewGuestAuthenticator(naming.NewGemstoneElementTheme(), startLocation)

		guestService, gsErr := auth.NewGuestService(
			guestAuth,
			playerRepo,
			charRepo,
			playerSessionStore,
		)
		Expect(gsErr).NotTo(HaveOccurred())

		pe := policytest.AllowAllEngine()
		reg := command.NewRegistry()
		handlers.RegisterAll(reg)
		cmdSvc := command.NewTestServices(command.ServicesConfig{
			Engine:  pe,
			Session: sessionStore,
			Events:  eventStore,
		})
		disp, dispErr := command.NewDispatcher(reg, pe)
		Expect(dispErr).NotTo(HaveOccurred())

		contributor = newStaticContributor()
		registry = grpcpkg.NewSessionStreamRegistry()

		// hookFnPtr starts as a no-op; tests may overwrite it.
		*hookFnPtr = func() {}

		coreServer := grpcpkg.NewCoreServer(engine, sessionStore, disp, cmdSvc,
			grpcpkg.WithEventStore(eventStore),
			grpcpkg.WithGuestService(guestService),
			grpcpkg.WithPlayerRepo(playerRepo),
			grpcpkg.WithPlayerSessionRepo(playerSessionStore),
			grpcpkg.WithCharacterRepo(charRepo),
			grpcpkg.WithSessionDefaults(grpcpkg.SessionDefaults{
				TTL:        5 * time.Minute,
				MaxHistory: 500,
				MaxReplay:  1000,
			}),
			grpcpkg.WithStreamContributor(contributor),
			grpcpkg.WithStreamRegistry(registry),
			grpcpkg.WithAfterLISTENHook(func() {
				if fn := *hookFnPtr; fn != nil {
					fn()
				}
			}),
			grpcpkg.WithDisconnectHook(func(info session.Info) {
				if info.IsGuest {
					guestAuth.ReleaseGuest(info.CharacterName)
				}
			}),
		)

		grpcServer = grpcpkg.NewGRPCServerInsecure()
		corev1.RegisterCoreServiceServer(grpcServer, coreServer)

		lis, lisErr := net.Listen("tcp", "127.0.0.1:0")
		Expect(lisErr).NotTo(HaveOccurred())
		go func() { _ = grpcServer.Serve(lis) }()

		grpcAddr := lis.Addr().String()
		Eventually(func() bool {
			conn, dialErr := net.DialTimeout("tcp", grpcAddr, 100*time.Millisecond)
			if dialErr != nil {
				return false
			}
			conn.Close()
			return true
		}).Should(BeTrue())

		grpcCli, err = grpcpkg.NewClient(testCtx, grpcpkg.ClientConfig{Address: grpcAddr})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if grpcCli != nil {
			_ = grpcCli.Close()
		}
		if grpcServer != nil {
			grpcServer.GracefulStop()
		}
		if eventStore != nil {
			eventStore.Close()
		}
		if container != nil {
			_ = container.Terminate(context.Background())
		}
		if testCancel != nil {
			testCancel()
		}
	})

	// authenticate performs the two-phase guest login (CreateGuest + SelectCharacter)
	// and returns the resulting game session ID.
	authenticate := func() string {
		GinkgoHelper()
		guestResp, err := grpcCli.CreateGuest(testCtx, &corev1.CreateGuestRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(guestResp.Success).To(BeTrue(), "guest creation should succeed: %s", guestResp.ErrorMessage)
		Expect(guestResp.Characters).To(HaveLen(1))

		selectResp, err := grpcCli.SelectCharacter(testCtx, &corev1.SelectCharacterRequest{
			PlayerSessionToken: guestResp.PlayerSessionToken,
			CharacterId:        guestResp.DefaultCharacterId,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(selectResp.Success).To(BeTrue(), "character selection should succeed: %s", selectResp.ErrorMessage)
		return selectResp.SessionId
	}

	// appendEvent appends a test event on the given stream.
	appendEvent := func(stream string) {
		GinkgoHelper()
		Expect(eventStore.Append(testCtx, core.Event{
			ID:        core.NewULID(),
			Stream:    stream,
			Type:      core.EventType("test"),
			Payload:   []byte(`{}`),
			Timestamp: time.Now(),
			Actor:     core.Actor{Kind: core.ActorSystem},
		})).To(Succeed())
	}

	Describe("UC1: session-start auto-subscribe", func() {
		It("receives messages posted before login via replay", func() {
			contributor.SetStreams("channel:general")

			// Append event to channel:general before subscribing.
			appendEvent("channel:general")

			sessionID := authenticate()

			subCtx, subCancel := context.WithTimeout(testCtx, 10*time.Second)
			defer subCancel()

			stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId: sessionID,
			})
			Expect(err).NotTo(HaveOccurred())

			types := collectUntilReplayComplete(stream)
			Expect(types).To(ContainElement("test"),
				"expected to replay event posted to channel:general before subscribe")
		})

		It("proceeds normally when contributor returns no streams", func() {
			// contributor returns no streams (default).
			sessionID := authenticate()

			subCtx, subCancel := context.WithTimeout(testCtx, 10*time.Second)
			defer subCancel()

			stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId: sessionID,
			})
			Expect(err).NotTo(HaveOccurred())

			// Should still reach REPLAY_COMPLETE without error.
			_ = collectUntilReplayComplete(stream)
		})
	})

	Describe("UC2: mid-session subscription changes", func() {
		It("receives messages on a stream added mid-session without reconnecting", func() {
			// Start with no plugin streams.
			sessionID := authenticate()

			subCtx, subCancel := context.WithTimeout(testCtx, 15*time.Second)
			defer subCancel()

			stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId: sessionID,
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait for REPLAY_COMPLETE before adding the new stream.
			_ = collectUntilReplayComplete(stream)

			// Start a looped receiver before issuing the control update so we
			// don't miss any frames while the subscribe loop processes the add.
			received := make(chan string, 8)
			go func() {
				defer GinkgoRecover()
				for {
					resp, recvErr := stream.Recv()
					if recvErr != nil {
						return
					}
					if ev := resp.GetEvent(); ev != nil {
						received <- ev.GetType()
					}
				}
			}()

			// Add stream mid-session via registry.
			Expect(registry.AddStream(testCtx, sessionID, "channel:new")).To(Succeed())

			// Give the subscribe loop time to process the control update.
			time.Sleep(200 * time.Millisecond)

			// Append an event to the newly-added stream.
			appendEvent("channel:new")

			Eventually(received, 10*time.Second).Should(Receive(Equal("test")),
				"event on dynamically-added stream should be delivered live")
		})

		It("stops forwarding messages after stream removed mid-session", func() {
			contributor.SetStreams("channel:active")
			sessionID := authenticate()

			subCtx, subCancel := context.WithTimeout(testCtx, 15*time.Second)
			defer subCancel()

			stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId: sessionID,
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait for REPLAY_COMPLETE.
			_ = collectUntilReplayComplete(stream)

			// Start a looped receiver before issuing the remove.
			received := make(chan struct{}, 8)
			go func() {
				defer GinkgoRecover()
				for {
					resp, recvErr := stream.Recv()
					if recvErr != nil {
						return
					}
					if resp.GetEvent() != nil {
						received <- struct{}{}
					}
				}
			}()

			// Remove the stream.
			Expect(registry.RemoveStream(testCtx, sessionID, "channel:active")).To(Succeed())

			// Give the subscribe loop time to process the control update.
			time.Sleep(200 * time.Millisecond)

			// Append an event to the removed stream.
			appendEvent("channel:active")

			// The event must NOT arrive.
			Consistently(received, 2*time.Second, 100*time.Millisecond).ShouldNot(Receive(),
				"event on removed stream should NOT be forwarded")
		})
	})

	Describe("LISTEN-before-replay invariant", func() {
		It("does not lose a message posted in the race window between LISTEN setup and replay", func() {
			contributor.SetStreams("channel:race")

			// Install the race-window hook: fires synchronously after LISTEN is set
			// up but before replay — append now to exercise the race path.
			*hookFnPtr = func() {
				appendEvent("channel:race")
			}

			sessionID := authenticate()

			subCtx, subCancel := context.WithTimeout(testCtx, 10*time.Second)
			defer subCancel()

			stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId: sessionID,
			})
			Expect(err).NotTo(HaveOccurred())

			types := collectUntilReplayComplete(stream)
			Expect(types).To(ContainElement("test"),
				"event appended in race window (after LISTEN, before replay) must appear in replay output")
		})
	})
})
