//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session_test

import (
	"context"
	"net"
	"sync"
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

// cursorCommitHookSlot is a package-level indirection for the
// CoreServer cursorCommitHook test seam. The BeforeEach installs a
// dispatcher closure that reads this variable; individual specs set it
// to a function that pauses inside replayAndSend's per-event critical
// section to drive a deterministic Finding 1 race (holomush-9ues).
//
// The slot is guarded by a mutex because the read happens on the gRPC
// server goroutine and the write happens on the test goroutine, with
// no other happens-before edge between them.
var (
	cursorHookMu sync.Mutex
	cursorHookFn func(ctx context.Context, sessionID string, eventID ulid.ULID)
)

// setCursorCommitHook installs (or clears, if fn is nil) the per-event
// cursor commit hook used by the cursor lock integration spec.
func setCursorCommitHook(fn func(ctx context.Context, sessionID string, eventID ulid.ULID)) {
	cursorHookMu.Lock()
	cursorHookFn = fn
	cursorHookMu.Unlock()
}

// dispatchCursorCommitHook is the closure handed to the CoreServer at
// construction. It reads the current cursorHookFn under the mutex and
// invokes it if non-nil. This indirection lets specs install hooks
// after the server is built without rebuilding the BeforeEach.
func dispatchCursorCommitHook(ctx context.Context, sessionID string, eventID ulid.ULID) {
	cursorHookMu.Lock()
	fn := cursorHookFn
	cursorHookMu.Unlock()
	if fn != nil {
		fn(ctx, sessionID, eventID)
	}
}

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
			return exec.Services().Events().Append(ctx, core.NewEvent(
				world.LocationStream(exec.LocationID()),
				core.EventType(pluginsdk.EventTypeSay),
				core.Actor{Kind: core.ActorCharacter, ID: exec.CharacterID().String()},
				payload,
			))
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
// and returns the resulting game session ID, character name, and
// player_session_token. The token is required by HandleCommand after
// bd-jv7z landed server-side ownership enforcement — calls that omit
// the token are rejected with "session not found".
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
		coreServer    *grpcpkg.CoreServer
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

		// Reset the cursor commit hook slot before each spec so a
		// previously-installed hook cannot leak across specs.
		setCursorCommitHook(nil)

		coreServer = grpcpkg.NewCoreServer(engine, env.sessionStore, dispatcher, cmdSvc,
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
			grpcpkg.WithCursorCommitHook(dispatchCursorCommitHook),
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
		// Use Stop instead of GracefulStop for test cleanup.
		// GracefulStop waits for in-flight Subscribe handlers to
		// return, but client-side context cancellation may not
		// propagate to the server-side stream context quickly
		// enough, causing a hang that outlasts the test timeout.
		// Tests that need GracefulStop as a server-side barrier
		// (e.g., cursor commit specs) call it explicitly in the
		// test body and set grpcServer = nil to skip this path.
		if grpcServer != nil {
			grpcServer.Stop()
		}
		if testCancel != nil {
			testCancel()
		}
	})

	Describe("Reconnect flow", func() {
		It("replays missed events when client resubscribes after disconnect", func() {
			sessionID, _, token := loginAsGuest(testCtx, grpcCli)

			// Open the first subscription. It will replay the `arrive` event
			// emitted by SelectCharacter, then enter the live loop.
			subCtx, subCancel := context.WithCancel(testCtx)
			stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId:          sessionID,
				PlayerSessionToken: token,
			})
			Expect(err).NotTo(HaveOccurred())

			// Issue a say command. The handler appends a `say` event to the
			// location stream, which the live loop forwards to the client.
			_, err = grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId:          sessionID,
				Command:            "say hello",
				PlayerSessionToken: token,
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
			}).WithTimeout(5*time.Second).WithPolling(50*time.Millisecond).
				Should(Equal(liveSayEventID), "location-stream cursor should advance to the live say event ID")

			// Append three events directly to the event store, simulating
			// activity from other characters while we are disconnected.
			missedPayloads := []string{"missed-A", "missed-B", "missed-C"}
			for _, msg := range missedPayloads {
				missed := core.NewEvent(locationStream, core.EventTypeSay, core.Actor{
					Kind: core.ActorCharacter, ID: "other",
				}, []byte(`{"character_name":"Other","message":"`+msg+`"}`))
				Expect(env.eventStore.Append(testCtx, missed)).To(Succeed())
			}

			// Re-subscribe with replay_from_cursor=true. The server should
			// read the persisted cursor and replay only the missed events.
			replayCtx, replayCancel := context.WithTimeout(testCtx, 5*time.Second)
			defer replayCancel()
			replayStream, err := grpcCli.Subscribe(replayCtx, &corev1.SubscribeRequest{
				SessionId:          sessionID,
				PlayerSessionToken: token,
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

		It("commits cursor before Subscribe handler returns on client-triggered exit", func() {
			// Scope of this test: it asserts that under the synchronous-commit
			// fix, the persisted cursor reflects the latest delivered event by
			// the time the Subscribe handler goroutine has finished exiting
			// (where exit was triggered by a client-side context cancellation).
			//
			// What this test does NOT prove: it does not deterministically
			// close Finding 1's strict contract. Finding 1 is about a fast
			// reconnect that begins WHILE the original handler is in the gap
			// between grpcStream.Send() and the cursor UPDATE. Sync-in-loop
			// shrinks that gap from "unbounded under load" (async goroutine
			// + pool wait) to "~1-10ms typical" (single sync DB round-trip),
			// but it does not eliminate it. A concurrent reconnect that hits
			// the in-flight commit window can still observe a stale cursor.
			//
			// Deterministic closure of that residual race requires read-after-
			// write consistency on the cursor at Subscribe start (e.g., a
			// SELECT FOR SHARE on the session row, an in-process per-session
			// barrier, a "reconnect cookie" sent by the client, or persist-
			// before-Send for the last event in the batch — each with its
			// own trade-offs). Tracked separately as holomush-9ues (P1).
			//
			// This test is therefore the strongest deterministic claim we
			// can make WITHOUT instrumenting production code with a test seam:
			// after the handler exits, the cursor is durable. The
			// GracefulStop() call below is the server-side barrier that
			// guarantees handler exit (ClientStream.Recv() returning a
			// local Canceled error does NOT imply server handler exit;
			// grpc-go aborts the client stream locally on context cancel).
			sessionID, _, token := loginAsGuest(testCtx, grpcCli)

			subCtx, subCancel := context.WithCancel(testCtx)
			stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId:          sessionID,
				PlayerSessionToken: token,
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId:          sessionID,
				Command:            "say hello",
				PlayerSessionToken: token,
			})
			Expect(err).NotTo(HaveOccurred())

			// Drain until the live say event arrives, capturing its ID.
			var liveSayID string
			Eventually(func() string {
				ev, recvErr := stream.Recv()
				if recvErr != nil {
					return ""
				}
				if frame := ev.GetEvent(); frame != nil && frame.GetType() == "say" {
					liveSayID = frame.GetId()
					return frame.GetType()
				}
				return ""
			}).WithTimeout(5 * time.Second).Should(Equal("say"))
			Expect(liveSayID).NotTo(BeEmpty())

			// Cancel the client subscription — triggers cancellation propagation
			// to the server, which causes the live loop's ctx.Done() case to fire
			// and the handler to begin exiting (after any in-flight replayAndSend
			// completes its sync cursor commit).
			subCancel()

			// GracefulStop is a SERVER-SIDE barrier: it blocks until all
			// in-flight handler goroutines return. By the time it unblocks,
			// the Subscribe handler has definitively exited — and because
			// the fix commits the cursor inline before the handler returns,
			// the cursor is durable for events the handler delivered.
			grpcServer.GracefulStop()
			grpcServer = nil // prevent AfterEach double-stop

			// Read the cursor — must reflect the live say event.
			locationStream := world.LocationStream(startLocation)
			sess, getErr := env.sessionStore.Get(testCtx, sessionID)
			Expect(getErr).NotTo(HaveOccurred())
			Expect(sess.EventCursors[locationStream].String()).To(Equal(liveSayID),
				"cursor must reflect the live say event after client-cancel-triggered handler exit (synced via GracefulStop barrier)")
		})

		It("commits cursor before grpcServer.GracefulStop returns (no lost writes on shutdown)", func() {
			sessionID, _, token := loginAsGuest(testCtx, grpcCli)

			subCtx, subCancel := context.WithCancel(testCtx)
			stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId:          sessionID,
				PlayerSessionToken: token,
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId:          sessionID,
				Command:            "say hello",
				PlayerSessionToken: token,
			})
			Expect(err).NotTo(HaveOccurred())

			// Drain until the live say arrives, capturing its ID.
			var liveSayID string
			Eventually(func() string {
				ev, recvErr := stream.Recv()
				if recvErr != nil {
					return ""
				}
				if frame := ev.GetEvent(); frame != nil && frame.GetType() == "say" {
					liveSayID = frame.GetId()
					return frame.GetType()
				}
				return ""
			}).WithTimeout(5 * time.Second).Should(Equal("say"))
			Expect(liveSayID).NotTo(BeEmpty())

			// Cancel the client sub and drain to EOF to free the handler goroutine.
			subCancel()
			for {
				if _, recvErr := stream.Recv(); recvErr != nil {
					break
				}
			}

			// GracefulStop blocks until in-flight RPC handlers return. The
			// Subscribe handler returns after committing the cursor inline,
			// so by the time GracefulStop unblocks, the commit is durable.
			grpcServer.GracefulStop()
			grpcServer = nil // prevent AfterEach from calling GracefulStop twice

			// Read the cursor — the session row must reflect the latest sent event.
			locationStream := world.LocationStream(startLocation)
			sess, getErr := env.sessionStore.Get(testCtx, sessionID)
			Expect(getErr).NotTo(HaveOccurred())
			Expect(sess.EventCursors[locationStream].String()).To(Equal(liveSayID),
				"cursor must reflect the latest sent event after GracefulStop returns")
		})
	})

	Describe("Cursor lock — Finding 1 deterministic closure (holomush-9ues)", func() {
		// The strict claim of this spec — stronger than the "cursor durable
		// after handler exit" spec earlier in this file — is that a
		// concurrent reconnect that begins WHILE the original Subscribe
		// handler is in the gap between Send and UpdateCursors does NOT
		// re-deliver the just-sent event. PR #198 shrunk the race window
		// from "unbounded under load" to "~1-10ms typical" but did not
		// close it. This spec uses a test seam (cursorCommitHook) to *hold*
		// the original handler in that gap so the race is observable
		// deterministically — no GracefulStop, no wall-clock timing on
		// the assertion path.
		It("blocks new Subscribe from observing stale cursor while old subscriber is mid-commit", func() {
			// 1) Establish a session and drain its initial replay so the
			//    hook fires on the *next* event (the live say) rather
			//    than the arrive event from SelectCharacter.
			sessionID, _, token := loginAsGuest(testCtx, grpcCli)

			subACtx, subACancel := context.WithCancel(testCtx)
			defer subACancel()
			streamA, err := grpcCli.Subscribe(subACtx, &corev1.SubscribeRequest{
				SessionId:          sessionID,
				PlayerSessionToken: token,
			})
			Expect(err).NotTo(HaveOccurred())

			drainUntilReplayComplete(streamA)

			// 2) Install the hook. Only the FIRST commit blocks (the
			//    live say). Subsequent commits — including B's eventual
			//    own replay batch — pass through unhindered, so the
			//    spec terminates without a deadlock if anything else
			//    happens to commit a cursor afterwards.
			//
			//    The release function is wrapped in sync.Once and
			//    registered with DeferCleanup so an early test failure
			//    cannot leave subscriber A blocked inside the hook —
			//    that would otherwise hang the suite at AfterEach's
			//    grpcServer.GracefulStop call.
			hookEntered := make(chan struct{})
			hookRelease := make(chan struct{})
			var releaseHookOnce sync.Once
			releaseHook := func() {
				releaseHookOnce.Do(func() { close(hookRelease) })
			}
			DeferCleanup(releaseHook)
			var hookFireCount int
			setCursorCommitHook(func(_ context.Context, _ string, _ ulid.ULID) {
				if hookFireCount > 0 {
					hookFireCount++
					return
				}
				hookFireCount++
				close(hookEntered)
				<-hookRelease
			})

			// 3) Trigger a live event. The handler will Send the say to
			//    streamA, then enter the hook with the cursor lock held.
			_, err = grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId:          sessionID,
				Command:            "say hello",
				PlayerSessionToken: token,
			})
			Expect(err).NotTo(HaveOccurred())

			// Drain A's stream until the say frame arrives, capturing
			// its ID. Recv runs in a goroutine because once A enters the
			// hook the live loop yields no more frames until release —
			// we only need to confirm the say landed.
			liveSayIDCh := make(chan string, 1)
			go func() {
				defer GinkgoRecover()
				for {
					ev, recvErr := streamA.Recv()
					if recvErr != nil {
						return
					}
					if frame := ev.GetEvent(); frame != nil && frame.GetType() == "say" {
						liveSayIDCh <- frame.GetId()
						return
					}
				}
			}()

			var liveSayID string
			Eventually(liveSayIDCh, 5*time.Second).Should(Receive(&liveSayID))
			Expect(liveSayID).NotTo(BeEmpty())

			// 4) Synchronize on hook entry. By the time hookEntered
			//    fires, A has Sent the event and is sitting in the
			//    pre-commit pause with the per-session cursor lock held.
			Eventually(hookEntered, 5*time.Second).Should(BeClosed())

			// 5) Open Subscribe B with replay_from_cursor=true for the
			//    SAME session. B's handler must block on the per-session
			//    cursor lock at the sessionStore.Get call, NOT read the
			//    stale cursor and start a duplicate replay.
			subBCtx, subBCancel := context.WithCancel(testCtx)
			defer subBCancel()
			streamB, err := grpcCli.Subscribe(subBCtx, &corev1.SubscribeRequest{
				SessionId:          sessionID,
				PlayerSessionToken: token,
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait for B's handler to queue on the per-session cursor
			// lock. The refcount reaches 2 when B's lock helper has
			// bumped it.
			//
			// Both code paths converge on this signal: with the fix in
			// place, B's Subscribe-side lock is the contender; with
			// the fix removed, B's per-event commit lock inside
			// sendAndCommitEvent is the contender (B can read the
			// stale cursor immediately, but can't actually Send the
			// duplicate until A releases its per-event lock). Either
			// way, refCount == 2 means B has reached a contended lock
			// acquisition behind A — race-free, no wall clock.
			Eventually(func() int {
				return coreServer.CursorLockRefCount(sessionID)
			}, 5*time.Second, 5*time.Millisecond).Should(BeNumerically(">=", 2),
				"B's handler should have queued on the per-session cursor lock")

			// 6) Release A. A finishes UpdateCursors, releases the lock;
			//    B then acquires the lock and reads the now-fresh cursor.
			releaseHook()

			// 7) Drain B's replay phase, ending at REPLAY_COMPLETE.
			//    Recv errors are propagated as test failures rather
			//    than silently treated as "replay complete" — otherwise
			//    a stream that dies before REPLAY_COMPLETE would let
			//    bSeenSayIDs stay empty and the spec pass for the
			//    wrong reason.
			var bSeenSayIDs []string
			drainResult := make(chan error, 1)
			go func() {
				defer GinkgoRecover()
				for {
					ev, recvErr := streamB.Recv()
					if recvErr != nil {
						drainResult <- recvErr
						return
					}
					if ctrl := ev.GetControl(); ctrl != nil &&
						ctrl.GetSignal() == corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE {
						drainResult <- nil
						return
					}
					if frame := ev.GetEvent(); frame != nil && frame.GetType() == "say" {
						bSeenSayIDs = append(bSeenSayIDs, frame.GetId())
					}
				}
			}()

			var drainErr error
			Eventually(drainResult, 5*time.Second).Should(Receive(&drainErr))
			Expect(drainErr).NotTo(HaveOccurred(),
				"B should reach REPLAY_COMPLETE without stream errors")

			// 8) The deterministic guarantee: B's replay does NOT contain
			//    the live say event A already received.
			Expect(bSeenSayIDs).NotTo(ContainElement(liveSayID),
				"B's replay must NOT contain the say event A received "+
					"— the per-session cursor lock should have blocked B "+
					"from reading the stale cursor")
		})
	})

	Describe("Command history", func() {
		It("persists commands across HandleCommand calls and exposes them via GetCommandHistory", func() {
			sessionID, _, token := loginAsGuest(testCtx, grpcCli)

			commands := []string{"say hello", "say world"}
			for _, cmd := range commands {
				resp, err := grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
					SessionId:          sessionID,
					Command:            cmd,
					PlayerSessionToken: token,
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
			sessionID, _, _ := loginAsGuest(testCtx, grpcCli)

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
			}).WithTimeout(5*time.Second).WithPolling(50*time.Millisecond).
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
			}).WithTimeout(5*time.Second).WithPolling(50*time.Millisecond).Should(BeTrue(),
				"expected a leave event for the expired character on the location stream")
		})
	})

	// HandleCommand ownership enforcement — bd-jv7z.
	//
	// Closes the IDOR surface where Player A could submit a command with
	// their valid player_session_token but against Player B's session_id.
	// The core server now calls auth.ValidateSessionOwnership before
	// executing, and any failure collapses to the enumeration-safe
	// "session not found" response.
	Describe("HandleCommand ownership enforcement (bd-jv7z)", func() {
		It("rejects a cross-player HandleCommand with session not found", func() {
			// Two independent guest logins — distinct players, tokens, sessions.
			sessionA, _, tokenA := loginAsGuest(testCtx, grpcCli)
			sessionB, _, tokenB := loginAsGuest(testCtx, grpcCli)
			Expect(sessionA).NotTo(Equal(sessionB))
			Expect(tokenA).NotTo(Equal(tokenB))

			// Attack: Player A's token with Player B's session_id.
			resp, err := grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId:          sessionB,
				Command:            "say stolen",
				PlayerSessionToken: tokenA,
			})
			Expect(err).NotTo(HaveOccurred(),
				"RPC returns normally — error is in the response payload")
			Expect(resp.GetSuccess()).To(BeFalse(),
				"cross-player command must be rejected")
			Expect(resp.GetError()).To(Equal("session not found"),
				"response must be enumeration-safe — same string as unknown session id")
		})

		It("permits HandleCommand when the player owns the session", func() {
			sessionID, _, token := loginAsGuest(testCtx, grpcCli)

			resp, err := grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId:          sessionID,
				Command:            "say hi",
				PlayerSessionToken: token,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.GetSuccess()).To(BeTrue(),
				"authorized command must succeed, got error: %s", resp.GetError())
		})

		It("rejects HandleCommand with empty token even for a valid session id", func() {
			sessionID, _, _ := loginAsGuest(testCtx, grpcCli)

			resp, err := grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId: sessionID,
				Command:   "say hi",
				// PlayerSessionToken intentionally empty.
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.GetSuccess()).To(BeFalse(),
				"empty token must not authorize a command")
			Expect(resp.GetError()).To(Equal("session not found"),
				"response must be enumeration-safe — same string as ownership mismatch")
		})
	})

	// Subscribe ownership enforcement — bd-jv7z.
	//
	// Closes the IDOR surface where Player A could open an event stream
	// against Player B's session_id. The core server now calls
	// auth.ValidateSessionOwnership at stream open; validation failure
	// terminates the stream with the enumeration-safe SESSION_NOT_FOUND
	// error. Ongoing revocation propagates via the existing control-frame
	// mechanism on session deletion.
	Describe("Subscribe ownership enforcement (bd-jv7z)", func() {
		It("rejects a cross-player Subscribe with session not found", func() {
			sessionA, _, tokenA := loginAsGuest(testCtx, grpcCli)
			sessionB, _, tokenB := loginAsGuest(testCtx, grpcCli)
			Expect(sessionA).NotTo(Equal(sessionB))
			Expect(tokenA).NotTo(Equal(tokenB))

			// Attack: Player A's token with Player B's session_id.
			subCtx, subCancel := context.WithTimeout(testCtx, 5*time.Second)
			defer subCancel()
			stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId:          sessionB,
				PlayerSessionToken: tokenA,
			})
			Expect(err).NotTo(HaveOccurred(),
				"stream-opening RPC returns nil; validation error surfaces on Recv")

			// First Recv should fail with a session-not-found-style error.
			_, recvErr := stream.Recv()
			Expect(recvErr).To(HaveOccurred(),
				"cross-player Subscribe must fail on first Recv")
			Expect(recvErr.Error()).To(ContainSubstring("session not found"),
				"response must be enumeration-safe — same string as unknown session id")
		})

		It("permits Subscribe when the player owns the session", func() {
			sessionID, _, token := loginAsGuest(testCtx, grpcCli)

			subCtx, subCancel := context.WithCancel(testCtx)
			defer subCancel()
			stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId:          sessionID,
				PlayerSessionToken: token,
			})
			Expect(err).NotTo(HaveOccurred())

			// Drain until REPLAY_COMPLETE arrives — proves the stream opened
			// and the live loop is running for the legitimate owner.
			drainUntilReplayComplete(stream)
		})

		It("rejects Subscribe with empty token even for a valid session id", func() {
			sessionID, _, _ := loginAsGuest(testCtx, grpcCli)

			subCtx, subCancel := context.WithTimeout(testCtx, 5*time.Second)
			defer subCancel()
			stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId: sessionID,
				// PlayerSessionToken intentionally empty.
			})
			Expect(err).NotTo(HaveOccurred())

			_, recvErr := stream.Recv()
			Expect(recvErr).To(HaveOccurred(),
				"empty token must not authorize a subscription")
			Expect(recvErr.Error()).To(ContainSubstring("session not found"),
				"response must be enumeration-safe — same string as ownership mismatch")
		})
	})
})

// drainUntilReplayComplete reads frames from the given stream until a
// REPLAY_COMPLETE control frame arrives, discarding events along the
// way. Used by specs that need to position a stream at the live loop
// before triggering further events.
//
// The read loop runs in a goroutine so the select can enforce a
// wall-clock deadline that actually bounds the total wait — a single
// blocking Recv() can otherwise hold the helper open indefinitely.
func drainUntilReplayComplete(stream interface {
	Recv() (*corev1.SubscribeResponse, error)
},
) {
	done := make(chan error, 1)
	go func() {
		defer GinkgoRecover()
		for {
			ev, err := stream.Recv()
			if err != nil {
				done <- err
				return
			}
			if ctrl := ev.GetControl(); ctrl != nil &&
				ctrl.GetSignal() == corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE {
				done <- nil
				return
			}
		}
	}()

	select {
	case err := <-done:
		Expect(err).NotTo(HaveOccurred(),
			"stream errored before REPLAY_COMPLETE")
	case <-time.After(5 * time.Second):
		Fail("timed out waiting for REPLAY_COMPLETE")
	}
}
