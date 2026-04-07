//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session_test

import (
	"context"
	"net"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/core"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/naming"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/telnet"
	"github.com/holomush/holomush/internal/world"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// reaperInterval is the polling interval for the session reaper in tests.
// Short enough that TTL expirations are observed within a few hundred ms.
const reaperInterval = 100 * time.Millisecond

// sessionTTL is the per-session TTL configured on the test CoreServer.
// Short so that detached sessions can be force-expired and reaped quickly.
const sessionTTL = 2 * time.Second

// registerTestSayCommand adds a minimal say handler that emits a `say` event
// to the character's current location stream. The integration tests use this
// instead of the production say handler to avoid pulling in plugin/ABAC wiring.
func registerTestSayCommand(reg *command.Registry) {
	entry, err := command.NewCommandEntry(command.CommandEntryConfig{
		Name: "say",
		Handler: func(ctx context.Context, exec *command.CommandExecution) error {
			payload := []byte(`{"character_name":"` + exec.CharacterName() + `","message":"` + exec.Args + `"}`)
			return exec.Services().Events().Append(ctx, core.Event{
				ID:        ulid.Make(),
				Stream:    world.LocationStream(exec.LocationID()),
				Type:      core.EventType(pluginsdk.EventTypeSay),
				Timestamp: time.Now(),
				Actor:     core.Actor{Kind: core.ActorCharacter, ID: exec.CharacterID().String()},
				Payload:   payload,
			})
		},
		Help:   "Say something",
		Usage:  "say <message>",
		Source: "test",
	})
	if err != nil {
		panic("failed to create test say command: " + err.Error())
	}
	if err := reg.Register(*entry); err != nil {
		panic("failed to register test say command: " + err.Error())
	}
}

// loginAsGuest performs the two-phase guest login (CreateGuest + SelectCharacter)
// and returns the resulting game session ID and character name.
func loginAsGuest(ctx context.Context, cli *grpcpkg.Client) (sessionID, charName string) {
	guestResp, err := cli.CreateGuest(ctx, &corev1.CreateGuestRequest{})
	Expect(err).NotTo(HaveOccurred())
	Expect(guestResp.Success).To(BeTrue(), "guest creation should succeed: %s", guestResp.ErrorMessage)
	Expect(guestResp.Characters).To(HaveLen(1))
	Expect(guestResp.DefaultCharacterId).NotTo(BeEmpty())

	selectResp, err := cli.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: guestResp.PlayerSessionToken,
		CharacterId:        guestResp.DefaultCharacterId,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(selectResp.Success).To(BeTrue(),
		"character selection should succeed: %s", selectResp.ErrorMessage)
	Expect(selectResp.SessionId).NotTo(BeEmpty())

	return selectResp.SessionId, selectResp.CharacterName
}

var _ = Describe("Session Persistence", func() {
	var (
		testCtx       context.Context
		testCancel    context.CancelFunc
		grpcServer    *grpc.Server
		grpcCli       *grpcpkg.Client
		engine        *core.Engine
		guestAuth     *telnet.GuestAuthenticator
		reaperCtx     context.Context
		reaperCancel  context.CancelFunc
		startLocation ulid.ULID
	)

	BeforeEach(func() {
		testCtx, testCancel = context.WithTimeout(context.Background(), 2*time.Minute)

		// Reset shared state — every spec runs against an empty schema.
		cleanupTestData(testCtx, env.pool)

		// Each spec creates a fresh start location whose ID is captured by
		// the GuestAuthenticator. The character's location_id FK requires
		// that this row exist before guest creation runs.
		startLocation = ulid.Make()
		loc := &world.Location{
			ID:           startLocation,
			Name:         "Test Origin",
			Description:  "Integration test starting location",
			Type:         world.LocationTypePersistent,
			ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
		}
		Expect(env.locRepo.Create(testCtx, loc)).To(Succeed())

		// Build per-test components: engine, guest auth, guest service.
		engine = core.NewEngine(env.eventStore)
		guestAuth = telnet.NewGuestAuthenticator(naming.NewGemstoneElementTheme(), startLocation)

		guestService, gsErr := auth.NewGuestService(
			guestAuth,
			env.playerRepo,
			env.charRepo,
			env.playerSessionStore,
		)
		Expect(gsErr).NotTo(HaveOccurred())

		// Build a minimal command registry with a single test `say` command.
		// AllowAllEngine bypasses ABAC so the dispatcher accepts any subject.
		policyEngine := policytest.AllowAllEngine()
		reg := command.NewRegistry()
		handlers.RegisterAll(reg)
		registerTestSayCommand(reg)
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
			grpcpkg.WithPlayerRepo(env.playerRepo),
			grpcpkg.WithPlayerSessionRepo(env.playerSessionStore),
			grpcpkg.WithCharacterRepo(env.charRepo),
			grpcpkg.WithSessionDefaults(grpcpkg.SessionDefaults{
				TTL:        sessionTTL,
				MaxHistory: 500,
				MaxReplay:  1000,
			}),
			grpcpkg.WithDisconnectHook(func(info session.Info) {
				if info.IsGuest {
					guestAuth.ReleaseGuest(info.CharacterName)
				}
			}),
		)

		// Bind a fresh listener on an ephemeral port for this spec.
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
			_ = conn.Close()
			return true
		}).Should(BeTrue())

		grpcCli, err = grpcpkg.NewClient(testCtx, grpcpkg.ClientConfig{Address: grpcAddr})
		Expect(err).NotTo(HaveOccurred())

		// Start the reaper goroutine. The OnExpired callback emits a leave
		// event via the engine, mirroring the production wiring in
		// cmd/holomush/sub_grpc.go.
		reaperCtx, reaperCancel = context.WithCancel(testCtx)
		reaper := session.NewReaper(env.sessionStore, session.ReaperConfig{
			Interval: reaperInterval,
			OnExpired: func(info *session.Info) {
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
		if testCancel != nil {
			testCancel()
		}
	})

	Describe("Reconnect flow", func() {
		It("replays missed events when client resubscribes after disconnect", func() {
			sessionID, _ := loginAsGuest(testCtx, grpcCli)

			// Open the first subscription. It will replay the `arrive` event
			// emitted by SelectCharacter, then enter the live loop.
			subCtx, subCancel := context.WithCancel(testCtx)
			stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId: sessionID,
			})
			Expect(err).NotTo(HaveOccurred())

			// Issue a say command. The handler appends a `say` event to the
			// location stream, which the live loop forwards to the client.
			_, err = grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId: sessionID,
				Command:   "say hello",
			})
			Expect(err).NotTo(HaveOccurred())

			// Drain the stream until the say event arrives, capturing its
			// exact event ID. Replay produces the arrive event and a
			// REPLAY_COMPLETE control frame; control frames return nil from
			// GetEvent, so we filter on type instead.
			var liveSayEventIDStr string
			Eventually(func() string {
				ev, recvErr := stream.Recv()
				if recvErr != nil {
					return ""
				}
				frame := ev.GetEvent()
				if frame == nil {
					return ""
				}
				if frame.GetType() == "say" {
					liveSayEventIDStr = frame.GetId()
				}
				return frame.GetType()
			}).WithTimeout(5 * time.Second).Should(Equal("say"))
			Expect(liveSayEventIDStr).NotTo(BeEmpty(), "should have captured the live say event ID")
			liveSayEventID, parseErr := ulid.Parse(liveSayEventIDStr)
			Expect(parseErr).NotTo(HaveOccurred())

			// Cancel the subscription. The server's persistCursorAsync writes
			// the cursor in a background goroutine, so we have to poll for it.
			subCancel()

			// Wait for the persisted cursor to advance to the *exact* live
			// say event. A bare "cursor exists" check is not enough: the
			// arrive event from SelectCharacter persists its own cursor first,
			// so the existence check could pass before the say cursor lands —
			// the replay would then start from the arrive cursor and re-deliver
			// `say hello` as one of the "missed" events, masking a regression.
			locationStream := world.LocationStream(startLocation)
			Eventually(func() ulid.ULID {
				sess, getErr := env.sessionStore.Get(testCtx, sessionID)
				if getErr != nil {
					return ulid.ULID{}
				}
				return sess.EventCursors[locationStream]
			}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).
				Should(Equal(liveSayEventID), "location-stream cursor should advance to the live say event ID")

			// Append three events directly to the event store, simulating
			// activity from other characters while we are disconnected.
			missedPayloads := []string{"missed-A", "missed-B", "missed-C"}
			for _, msg := range missedPayloads {
				missed := core.Event{
					ID:        core.NewULID(),
					Stream:    locationStream,
					Type:      core.EventTypeSay,
					Payload:   []byte(`{"character_name":"Other","message":"` + msg + `"}`),
					Timestamp: time.Now(),
					Actor:     core.Actor{Kind: core.ActorCharacter, ID: "other"},
				}
				Expect(env.eventStore.Append(testCtx, missed)).To(Succeed())
			}

			// Re-subscribe with replay_from_cursor=true. The server should
			// read the persisted cursor and replay only the missed events.
			replayCtx, replayCancel := context.WithTimeout(testCtx, 5*time.Second)
			defer replayCancel()
			replayStream, err := grpcCli.Subscribe(replayCtx, &corev1.SubscribeRequest{
				SessionId:        sessionID,
				ReplayFromCursor: true,
			})
			Expect(err).NotTo(HaveOccurred())

			// Collect say events from the replay. The character stream is
			// always added to subscriptions, so command_response events from
			// the earlier say command may also be replayed — filter them out.
			var replayedSays []*corev1.EventFrame
			for len(replayedSays) < len(missedPayloads) {
				ev, recvErr := replayStream.Recv()
				Expect(recvErr).NotTo(HaveOccurred(),
					"only got %d/%d say events before stream error",
					len(replayedSays), len(missedPayloads))
				frame := ev.GetEvent()
				if frame != nil && frame.GetType() == "say" && frame.GetStream() == locationStream {
					replayedSays = append(replayedSays, frame)
				}
			}
			Expect(replayedSays).To(HaveLen(len(missedPayloads)))
		})
	})

	Describe("Command history", func() {
		It("persists commands across HandleCommand calls and exposes them via GetCommandHistory", func() {
			sessionID, _ := loginAsGuest(testCtx, grpcCli)

			commands := []string{"say hello", "say world"}
			for _, cmd := range commands {
				resp, err := grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
					SessionId: sessionID,
					Command:   cmd,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Success).To(BeTrue(), "command %q failed: %s", cmd, resp.Error)
			}

			// Verify via the gRPC GetCommandHistory RPC. This exercises the
			// full server → store → driver path that unit tests cannot.
			histResp, err := grpcCli.GetCommandHistory(testCtx, &corev1.GetCommandHistoryRequest{
				SessionId: sessionID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(histResp.Success).To(BeTrue(), "GetCommandHistory failed: %s", histResp.Error)
			Expect(histResp.Commands).To(Equal(commands))
		})
	})

	Describe("TTL expiration", func() {
		It("deletes detached session and emits leave event when reaper observes expired ttl", func() {
			sessionID, _ := loginAsGuest(testCtx, grpcCli)

			// Capture session details before forcing expiry — once the reaper
			// runs the session row is gone and Get returns an error.
			info, err := env.sessionStore.Get(testCtx, sessionID)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Status).To(Equal(session.StatusActive))
			characterID := info.CharacterID
			locationID := info.LocationID
			Expect(locationID).To(Equal(startLocation))

			// Force-detach with an already-expired time. The reaper sweeps
			// detached sessions whose ExpiresAt is in the past.
			now := time.Now()
			pastExpiry := now.Add(-1 * time.Second)
			Expect(env.sessionStore.UpdateStatus(testCtx, sessionID,
				session.StatusDetached, &now, &pastExpiry)).To(Succeed())

			// Reaper polls every 100ms — give it a generous deadline before
			// concluding it never ran. Poll a SELECT EXISTS query directly so
			// transient pool/context errors can't masquerade as "row deleted".
			Eventually(func() bool {
				var exists bool
				queryErr := env.pool.QueryRow(testCtx,
					`SELECT EXISTS(SELECT 1 FROM sessions WHERE id = $1)`,
					sessionID,
				).Scan(&exists)
				Expect(queryErr).NotTo(HaveOccurred())
				return exists
			}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).
				Should(BeFalse(), "reaper should have deleted the expired session")

			// The OnExpired callback should have emitted a leave event via
			// engine.HandleDisconnect. The leave event lives on the location
			// stream and identifies the disconnected character via Actor.ID.
			leaveStream := world.LocationStream(locationID)
			Eventually(func() bool {
				events, replayErr := env.eventStore.Replay(testCtx, leaveStream, ulid.ULID{}, 100)
				if replayErr != nil {
					return false
				}
				for _, e := range events {
					if e.Type == core.EventTypeLeave && e.Actor.ID == characterID.String() {
						return true
					}
				}
				return false
			}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(BeTrue(),
				"expected a leave event for the expired character on the location stream")
		})
	})
})
