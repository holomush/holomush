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

			// Issue quit with a context we cancel immediately after.
			quitCtx, quitCancel := context.WithCancel(testCtx)
			resp, err := srv.grpcCli.HandleCommand(quitCtx, &corev1.HandleCommandRequest{
				SessionId:          sessionID,
				Command:            "quit",
				PlayerSessionToken: token,
			})
			// Cancel after the call returns — simulates a client drop.
			quitCancel()

			// The quit may succeed or the context may have raced: in either case,
			// the session_ended event MUST be persisted because EndSession uses a
			// decoupled background context.
			if err != nil {
				// If the RPC was cancelled before the server finished, tolerate it.
				st, ok := status.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(st.Code()).To(BeElementOf(codes.Canceled, codes.OK))
			} else {
				Expect(resp.Success).To(BeTrue())
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
		It("does not self-terminate a new Subscribe when prior session_ended is in replay", func() {
			// Session 1: player quits — this writes a session_ended event to the
			// character stream that will be present in replay for a future session.
			sessionID1, _, token1 := loginAsGuest(testCtx, srv.grpcCli)
			sessInfo1, err := env.sessionStore.Get(testCtx, sessionID1)
			Expect(err).NotTo(HaveOccurred())
			charID := sessInfo1.CharacterID

			resp, err := srv.grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId:          sessionID1,
				Command:            "quit",
				PlayerSessionToken: token1,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Success).To(BeTrue())

			// Wait for session_ended to be persisted before creating a new session.
			charStream := core.StreamPrefixCharacter + charID.String()
			Eventually(func() bool {
				events, replayErr := env.eventStore.Replay(testCtx, charStream, ulid.ULID{}, 100)
				if replayErr != nil {
					return false
				}
				for _, e := range events {
					if e.Type == core.EventTypeSessionEnded {
						return true
					}
				}
				return false
			}, 5*time.Second, 50*time.Millisecond).Should(BeTrue())

			// Session 2: a NEW guest session on a different character — we use a
			// fresh guest login. The prior session_ended is for session1/char1 only.
			// The new Subscribe must NOT terminate due to that prior event.
			sessionID2, _, token2 := loginAsGuest(testCtx, srv.grpcCli)
			Expect(sessionID2).NotTo(Equal(sessionID1))

			// Open Subscribe and drain it for a moment — it should NOT close itself.
			subCtx, subCancel := context.WithTimeout(testCtx, 3*time.Second)
			defer subCancel()

			stream, subErr := srv.grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
				SessionId:          sessionID2,
				PlayerSessionToken: token2,
			})
			Expect(subErr).NotTo(HaveOccurred())

			// Drain frames for up to 3 seconds; we must NOT receive STREAM_CLOSED.
			gotStreamClosed := false
			for {
				msg, recvErr := stream.Recv()
				if recvErr != nil {
					// context deadline/cancel is expected at 3s — that's fine.
					break
				}
				ctrl := msg.GetControl()
				if ctrl != nil && ctrl.Signal == corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED {
					gotStreamClosed = true
					break
				}
			}
			Expect(gotStreamClosed).To(BeFalse(),
				"a prior session_ended for a different session MUST NOT terminate a new Subscribe")
		})
	})
})
