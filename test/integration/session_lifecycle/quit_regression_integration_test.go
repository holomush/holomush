// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package session_lifecycle_test

import (
	"context"
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/core"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

var _ = Describe("Quit Regression", func() {
	var (
		testCtx    context.Context
		testCancel context.CancelFunc
		srv        *specServer
	)

	BeforeEach(func() {
		testCtx, testCancel = context.WithTimeout(context.Background(), 2*time.Minute)
		cleanupTestData(testCtx, env.pool)
		srv = newSpecServer(testCtx)
	})

	AfterEach(func() {
		srv.teardown()
		if testCancel != nil {
			testCancel()
		}
	})

	Describe("session_ended event on character stream", func() {
		It("persists session_ended with cause=quit when player quits", func() {
			sessionID, _, token := loginAsGuest(testCtx, srv.grpcCli)

			// Fetch session to get character ID before quit cleans it up.
			sessInfo, err := env.sessionStore.Get(testCtx, sessionID)
			Expect(err).NotTo(HaveOccurred())
			charID := sessInfo.CharacterID

			// Issue the quit command.
			resp, err := srv.grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId:          sessionID,
				Command:            "quit",
				PlayerSessionToken: token,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Success).To(BeTrue())

			// Verify session_ended was persisted on the character stream.
			charStream := core.StreamPrefixCharacter + charID.String()
			Eventually(func() *core.SessionEndedPayload {
				events, replayErr := env.eventStore.Replay(testCtx, charStream, ulid.ULID{}, 100)
				if replayErr != nil {
					return nil
				}
				for _, e := range events {
					if e.Type == core.EventTypeSessionEnded {
						var p core.SessionEndedPayload
						if jsonErr := json.Unmarshal(e.Payload, &p); jsonErr != nil {
							return nil
						}
						return &p
					}
				}
				return nil
			}, 5*time.Second, 50*time.Millisecond).Should(SatisfyAll(
				Not(BeNil()),
				WithTransform(func(p *core.SessionEndedPayload) string { return p.SessionID }, Equal(sessionID)),
				WithTransform(func(p *core.SessionEndedPayload) string { return p.Cause }, Equal(core.SessionEndedCauseQuit)),
				WithTransform(func(p *core.SessionEndedPayload) string { return p.Reason }, ContainSubstring("Goodbye")),
			))
		})

		It("persists session_ended even when client context cancels mid-quit", func() {
			// Connect and get a game session.
			sessionID, _, token := loginAsGuest(testCtx, srv.grpcCli)

			sessInfo, err := env.sessionStore.Get(testCtx, sessionID)
			Expect(err).NotTo(HaveOccurred())
			charID := sessInfo.CharacterID

			// Issue quit in a goroutine and cancel the context immediately —
			// the cancel races with the handler, simulating a real mid-quit
			// client drop. The cancel must arrive BEFORE HandleCommand
			// returns, not after (otherwise it tests nothing — the server
			// has already completed).
			quitCtx, quitCancel := context.WithCancel(testCtx)
			type quitResult struct {
				resp *corev1.HandleCommandResponse
				err  error
			}
			done := make(chan quitResult, 1)
			go func() {
				resp, err := srv.grpcCli.HandleCommand(quitCtx, &corev1.HandleCommandRequest{
					SessionId:          sessionID,
					Command:            "quit",
					PlayerSessionToken: token,
				})
				done <- quitResult{resp: resp, err: err}
			}()
			// Cancel immediately — this races the quit handler.
			quitCancel()

			// Wait for the RPC to return (success or cancellation) before
			// asserting on persisted events.
			var qr quitResult
			Eventually(done, 10*time.Second, 10*time.Millisecond).Should(Receive(&qr))

			// The quit may succeed or the context may have raced: in either case,
			// the session_ended event MUST be persisted because EndSession uses a
			// decoupled background context.
			if qr.err != nil {
				// If the RPC was cancelled before the server finished, tolerate it.
				st, ok := status.FromError(qr.err)
				Expect(ok).To(BeTrue())
				Expect(st.Code()).To(BeElementOf(codes.Canceled, codes.OK))
			} else {
				Expect(qr.resp.Success).To(BeTrue())
			}

			charStream := core.StreamPrefixCharacter + charID.String()
			Eventually(func() bool {
				events, replayErr := env.eventStore.Replay(testCtx, charStream, ulid.ULID{}, 100)
				if replayErr != nil {
					return false
				}
				for _, e := range events {
					if e.Type == core.EventTypeSessionEnded {
						var p core.SessionEndedPayload
						if jsonErr := json.Unmarshal(e.Payload, &p); jsonErr != nil {
							return false
						}
						return p.Cause == core.SessionEndedCauseQuit
					}
				}
				return false
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue(),
				"session_ended event should persist even if client ctx cancelled mid-quit")
		})
	})

	Describe("replay isolation", func() {
		It("does not self-terminate a new Subscribe when a prior non-matching session_ended is replayed on the same character stream", func() {
			// Create a session and get its character stream.
			sessionID2, _, token2 := loginAsGuest(testCtx, srv.grpcCli)
			sessInfo2, err := env.sessionStore.Get(testCtx, sessionID2)
			Expect(err).NotTo(HaveOccurred())
			charID := sessInfo2.CharacterID
			charStream := core.StreamPrefixCharacter + charID.String()

			// Manufacture a prior session_ended event on THIS character's
			// stream that references a DIFFERENT session ID. This is the
			// scenario a naive implementation gets wrong: a stale terminal
			// event from a previous session replayed into a new Subscribe
			// would incorrectly close the new stream.
			staleSessionID := core.NewULID().String()
			stalePayload, marshalErr := json.Marshal(core.SessionEndedPayload{
				SessionID:   staleSessionID,
				CharacterID: charID.String(),
				Cause:       core.SessionEndedCauseQuit,
				Reason:      "stale",
			})
			Expect(marshalErr).NotTo(HaveOccurred())
			staleEvent := core.NewEvent(
				charStream,
				core.EventTypeSessionEnded,
				core.Actor{Kind: core.ActorCharacter, ID: charID.String()},
				stalePayload,
			)
			Expect(env.eventStore.Append(testCtx, staleEvent)).To(Succeed())

			// Sanity: the stale event is now in the stream and will be
			// replayed when a new Subscribe opens.
			Eventually(func() bool {
				events, replayErr := env.eventStore.Replay(testCtx, charStream, ulid.ULID{}, 100)
				if replayErr != nil {
					return false
				}
				for _, e := range events {
					if e.Type == core.EventTypeSessionEnded {
						var p core.SessionEndedPayload
						if jsonErr := json.Unmarshal(e.Payload, &p); jsonErr != nil {
							return false
						}
						if p.SessionID == staleSessionID {
							return true
						}
					}
				}
				return false
			}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(),
				"stale session_ended must be persisted before Subscribe opens")

			// Open Subscribe and drain it for a moment — it must forward the
			// non-matching session_ended frame WITHOUT emitting STREAM_CLOSED
			// (the control signal must only fire for session_ended events
			// that reference the currently-subscribed session).
			subCtx, subCancel := context.WithTimeout(testCtx, 3*time.Second)
			defer subCancel()

			stream, subErr := srv.grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId:          sessionID2,
				PlayerSessionToken: token2,
			})
			Expect(subErr).NotTo(HaveOccurred())

			gotStreamClosed := false
			for {
				msg, recvErr := stream.Recv()
				if recvErr != nil {
					// Context deadline/cancel at 3s is expected.
					break
				}
				ctrl := msg.GetControl()
				if ctrl != nil && ctrl.Signal == corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED {
					gotStreamClosed = true
					break
				}
			}
			Expect(gotStreamClosed).To(BeFalse(),
				"a prior session_ended that does NOT match the current session MUST NOT terminate a new Subscribe")
		})
	})
})
