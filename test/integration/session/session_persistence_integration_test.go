//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session_test

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/core"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telnet"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// authenticateGuest authenticates as a guest and returns the session ID and character name.
func authenticateGuest(ctx context.Context, cli *grpcpkg.Client) (sessionID, charName string) {
	resp, err := cli.Authenticate(ctx, &corev1.AuthenticateRequest{
		Username: "guest",
		Password: "",
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(resp.Success).To(BeTrue(), "auth should succeed, got error: %s", resp.Error)
	return resp.SessionId, resp.CharacterName
}

var _ = Describe("Session Persistence", func() {
	var (
		testCtx      context.Context
		testCancel   context.CancelFunc
		container    *postgres.PostgresContainer
		grpcServer   *grpc.Server
		grpcCli      *grpcpkg.Client
		sessionStore *store.PostgresSessionStore
		eventStore   *store.PostgresEventStore
		guestAuth    *telnet.GuestAuthenticator
		reaper       *session.Reaper
		reaperCtx    context.Context
		reaperCancel context.CancelFunc

		startLocation ulid.ULID
	)

	BeforeEach(func() {
		testCtx, testCancel = context.WithTimeout(context.Background(), 10*time.Minute)

		// 1. Start PostgreSQL container
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

		// 2. Run migrations
		migrator, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migrator.Up()).To(Succeed())
		_ = migrator.Close()

		// 3. Create event store (also provides the pool)
		eventStore, err = store.NewPostgresEventStore(testCtx, connStr)
		Expect(err).NotTo(HaveOccurred())

		// 4. Create session store sharing the same pool
		pool := eventStore.Pool()
		Expect(pool).NotTo(BeNil(), "pool must be available for session store")
		sessionStore = store.NewPostgresSessionStore(pool)

		// 5. Create core components
		startLocation = ulid.Make()
		sessions := core.NewSessionManager()
		engine := core.NewEngine(eventStore, sessions)

		// 6. Create GuestAuthenticator
		guestAuth = telnet.NewGuestAuthenticator(telnet.NewGemstoneElementTheme(), startLocation)

		// 7. Create CoreServer with PostgresSessionStore and session defaults
		coreServer := grpcpkg.NewCoreServer(engine, sessions, sessionStore,
			grpcpkg.WithAuthenticator(guestAuth),
			grpcpkg.WithEventStore(eventStore),
			grpcpkg.WithSessionDefaults(grpcpkg.SessionDefaults{
				TTL:        2 * time.Second, // Short TTL for testing
				MaxHistory: 500,
				MaxReplay:  1000,
			}),
			grpcpkg.WithDisconnectHook(func(info session.Info) {
				if info.IsGuest {
					guestAuth.ReleaseGuest(info.CharacterName)
				}
			}),
		)

		// 8. Start gRPC server (insecure for integration tests)
		grpcServer = grpcpkg.NewGRPCServerInsecure()
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
			conn.Close()
			return true
		}).Should(BeTrue())

		// 9. Create gRPC client (insecure)
		grpcCli, err = grpcpkg.NewClient(testCtx, grpcpkg.ClientConfig{
			Address: grpcAddr,
		})
		Expect(err).NotTo(HaveOccurred())

		// 10. Start reaper with short interval for TTL tests
		reaperCtx, reaperCancel = context.WithCancel(testCtx)
		reaper = session.NewReaper(sessionStore, session.ReaperConfig{
			Interval: 100 * time.Millisecond,
			OnExpired: func(info *session.Info) {
				// Emit leave event via engine on expiry
				char := core.CharacterRef{
					ID:         info.CharacterID,
					Name:       info.CharacterName,
					LocationID: info.LocationID,
				}
				_ = engine.HandleDisconnect(reaperCtx, char, "session expired")
			},
		})
		go reaper.Run(reaperCtx)
	})

	AfterEach(func() {
		if reaperCancel != nil {
			reaperCancel()
		}
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

	Describe("Reconnect flow", func() {
		It("replays missed events then switches to live", func() {
			// Authenticate as guest
			sessionID, _ := authenticateGuest(testCtx, grpcCli)

			// Subscribe (no replay) to start receiving live events
			subCtx, subCancel := context.WithCancel(testCtx)
			stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId: sessionID,
			})
			Expect(err).NotTo(HaveOccurred())

			// Send a say command to generate an event and advance the cursor
			_, err = grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId: sessionID,
				Command:   "say hello",
			})
			Expect(err).NotTo(HaveOccurred())

			// Read events until the say event to advance the cursor.
			// With LISTEN/NOTIFY, arrive events from authentication may be
			// delivered before the say (they were silently dropped by the
			// old broadcaster).
			var ev *corev1.SubscribeResponse
			Eventually(func() string {
				var recvErr error
				ev, recvErr = stream.Recv()
				Expect(recvErr).NotTo(HaveOccurred())
				return ev.Type
			}).Should(Equal("say"))

			// Cancel the subscription (simulates disconnect)
			subCancel()

			// Poll until cursor persists (best-effort goroutine writes async)
			locationStream := "location:" + startLocation.String()
			Eventually(func() bool {
				sess, err := sessionStore.Get(testCtx, sessionID)
				if err != nil {
					return false
				}
				_, hasCursor := sess.EventCursors[locationStream]
				return hasCursor
			}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(BeTrue())

			// Append events directly to the event store to simulate activity
			// while the client was disconnected
			for i := 0; i < 3; i++ {
				missedEvent := core.Event{
					ID:        core.NewULID(),
					Stream:    locationStream,
					Type:      core.EventTypeSay,
					Payload:   []byte(`{"message":"missed-` + string(rune('A'+i)) + `"}`),
					Timestamp: time.Now(),
					Actor:     core.Actor{Kind: core.ActorCharacter, ID: "other"},
				}
				Expect(eventStore.Append(testCtx, missedEvent)).To(Succeed())
			}

			// Re-subscribe with replay_from_cursor=true
			replayCtx, replayCancel := context.WithTimeout(testCtx, 5*time.Second)
			defer replayCancel()

			replayStream, err := grpcCli.Subscribe(replayCtx, &corev1.SubscribeRequest{
				SessionId:        sessionID,
				ReplayFromCursor: true,
			})
			Expect(err).NotTo(HaveOccurred())

			// Should receive the 3 missed events
			var replayed []*corev1.SubscribeResponse
			for i := 0; i < 3; i++ {
				ev, err := replayStream.Recv()
				Expect(err).NotTo(HaveOccurred())
				replayed = append(replayed, ev)
			}
			Expect(replayed).To(HaveLen(3))
			for _, rev := range replayed {
				Expect(rev.Type).To(Equal("say"))
			}
		})

		PIt("sends replay_complete marker after replay finishes (web proto only)")
	})

	Describe("Command history", func() {
		It("persists commands across disconnect/reconnect", func() {
			// Authenticate
			sessionID, _ := authenticateGuest(testCtx, grpcCli)

			// Send commands
			_, err := grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId: sessionID,
				Command:   "say hello",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId: sessionID,
				Command:   "say world",
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify commands are persisted in PostgresSessionStore
			history, err := sessionStore.GetCommandHistory(testCtx, sessionID)
			Expect(err).NotTo(HaveOccurred())
			Expect(history).To(Equal([]string{"say hello", "say world"}))
		})

		It("enforces per-session cap", func() {
			// Authenticate — the CoreServer is configured with MaxHistory=500,
			// but we'll override via the session store directly for this test.
			sessionID, _ := authenticateGuest(testCtx, grpcCli)

			// Override the session's MaxHistory to 3 for this test
			info, err := sessionStore.Get(testCtx, sessionID)
			Expect(err).NotTo(HaveOccurred())
			info.MaxHistory = 3
			Expect(sessionStore.Set(testCtx, sessionID, info)).To(Succeed())

			// Send 5 commands
			for i := 1; i <= 5; i++ {
				_, err := grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
					SessionId: sessionID,
					Command:   "say msg" + string(rune('0'+i)),
				})
				Expect(err).NotTo(HaveOccurred())
			}

			// Verify only the last 3 are retained
			history, err := sessionStore.GetCommandHistory(testCtx, sessionID)
			Expect(err).NotTo(HaveOccurred())
			Expect(history).To(HaveLen(3))
			Expect(history).To(Equal([]string{"say msg3", "say msg4", "say msg5"}))
		})
	})

	Describe("TTL expiration", func() {
		It("deletes detached session when it expires", func() {
			// Authenticate
			sessionID, _ := authenticateGuest(testCtx, grpcCli)

			// Verify session exists
			info, err := sessionStore.Get(testCtx, sessionID)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Status).To(Equal(session.StatusActive))

			// Force-detach the session with an already-expired time
			now := time.Now()
			pastExpiry := now.Add(-1 * time.Second)
			Expect(sessionStore.UpdateStatus(testCtx, sessionID,
				session.StatusDetached, &now, &pastExpiry)).To(Succeed())

			// Wait for reaper to run (100ms interval + buffer)
			Eventually(func() error {
				_, err := sessionStore.Get(testCtx, sessionID)
				return err
			}, 2*time.Second, 50*time.Millisecond).Should(HaveOccurred(),
				"session should be deleted by reaper after expiry")
		})

		It("creates new session on reconnect after expiration", func() {
			// Authenticate first session
			session1, name1 := authenticateGuest(testCtx, grpcCli)
			_ = name1

			// Force-expire session1
			now := time.Now()
			pastExpiry := now.Add(-1 * time.Second)
			Expect(sessionStore.UpdateStatus(testCtx, session1,
				session.StatusDetached, &now, &pastExpiry)).To(Succeed())

			// Wait for reaper to clean it up
			Eventually(func() error {
				_, err := sessionStore.Get(testCtx, session1)
				return err
			}, 2*time.Second, 50*time.Millisecond).Should(HaveOccurred())

			// Authenticate again (new guest session)
			session2, _ := authenticateGuest(testCtx, grpcCli)

			// New session should have a different ID
			Expect(session2).NotTo(Equal(session1))

			// New session should be active
			info, err := sessionStore.Get(testCtx, session2)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Status).To(Equal(session.StatusActive))
		})
	})

	Describe("Explicit quit", func() {
		It("terminates session immediately without detach", func() {
			// Authenticate
			sessionID, _ := authenticateGuest(testCtx, grpcCli)

			// Verify session exists
			_, err := sessionStore.Get(testCtx, sessionID)
			Expect(err).NotTo(HaveOccurred())

			// Send quit command
			resp, err := grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId: sessionID,
				Command:   "quit",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Success).To(BeTrue())

			// Session should be deleted immediately (not detached)
			_, err = sessionStore.Get(testCtx, sessionID)
			Expect(err).To(HaveOccurred(), "session should be deleted after quit")
		})
	})

	Describe("Concurrent reattach", func() {
		It("only one client wins the race", func() {
			// Create a session directly in detached state
			sessionID := core.NewULID().String()
			now := time.Now()
			expiresAt := now.Add(5 * time.Minute) // won't expire during test
			info := &session.Info{
				ID:            sessionID,
				CharacterID:   ulid.Make(),
				CharacterName: "Test_Character",
				LocationID:    startLocation,
				IsGuest:       false,
				Status:        session.StatusDetached,
				GridPresent:   true,
				EventCursors:  map[string]ulid.ULID{},
				TTLSeconds:    300,
				MaxHistory:    500,
				DetachedAt:    &now,
				ExpiresAt:     &expiresAt,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			Expect(sessionStore.Set(testCtx, sessionID, info)).To(Succeed())

			// Two goroutines race to reattach
			const racers = 2
			var wg sync.WaitGroup
			var wins atomic.Int32
			var losses atomic.Int32

			wg.Add(racers)
			start := make(chan struct{})
			for i := 0; i < racers; i++ {
				go func() {
					defer GinkgoRecover()
					defer wg.Done()
					<-start // synchronize start
					ok, err := sessionStore.ReattachCAS(testCtx, sessionID)
					Expect(err).NotTo(HaveOccurred())
					if ok {
						wins.Add(1)
					} else {
						losses.Add(1)
					}
				}()
			}

			close(start) // fire!
			wg.Wait()

			// Exactly one should win
			Expect(wins.Load()).To(Equal(int32(1)), "exactly one racer should win")
			Expect(losses.Load()).To(Equal(int32(1)), "exactly one racer should lose")

			// Session should be active now
			updated, err := sessionStore.Get(testCtx, sessionID)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Status).To(Equal(session.StatusActive))
			Expect(updated.DetachedAt).To(BeNil())
			Expect(updated.ExpiresAt).To(BeNil())
		})
	})

	Describe("Empty cursors on reconnect", func() {
		PIt("sends replay_complete immediately without replay (web proto only)")
	})
})
