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

	"github.com/holomush/holomush/internal/core"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

var _ = Describe("Disconnect isolation", func() {
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

	Describe("single-connection quit", func() {
		It("emits session_ended with cause=quit only for the quitting session", func() {
			// Log in two separate guest sessions (different characters).
			sessionA, _, tokenA := loginAsGuest(testCtx, srv.grpcCli)
			sessionB, _, tokenB := loginAsGuest(testCtx, srv.grpcCli)
			Expect(sessionA).NotTo(Equal(sessionB))

			// Capture character IDs before quit destroys the sessions.
			infoA, err := env.sessionStore.Get(testCtx, sessionA)
			Expect(err).NotTo(HaveOccurred())
			charIDA := infoA.CharacterID

			infoB, err := env.sessionStore.Get(testCtx, sessionB)
			Expect(err).NotTo(HaveOccurred())
			charIDB := infoB.CharacterID

			// Session A quits.
			resp, err := srv.grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId:          sessionA,
				Command:            "quit",
				PlayerSessionToken: tokenA,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Success).To(BeTrue())

			// Character A's stream MUST have session_ended.
			streamA := core.StreamPrefixCharacter + charIDA.String()
			Eventually(func() bool {
				events, replayErr := env.eventStore.Replay(testCtx, streamA, ulid.ULID{}, 100)
				if replayErr != nil {
					return false
				}
				for _, e := range events {
					if e.Type == core.EventTypeSessionEnded {
						var p core.SessionEndedPayload
						if jsonErr := json.Unmarshal(e.Payload, &p); jsonErr != nil {
							return false
						}
						return p.Cause == core.SessionEndedCauseQuit && p.SessionID == sessionA
					}
				}
				return false
			}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(),
				"session A's character stream should have session_ended with cause=quit")

			// Session B's character stream MUST NOT have session_ended.
			// B is still active — its stream should be free of session_ended events.
			streamB := core.StreamPrefixCharacter + charIDB.String()
			Consistently(func() bool {
				events, replayErr := env.eventStore.Replay(testCtx, streamB, ulid.ULID{}, 100)
				if replayErr != nil {
					return false
				}
				for _, e := range events {
					if e.Type == core.EventTypeSessionEnded {
						return true // found one — Consistently will fail
					}
				}
				return false
			}, 500*time.Millisecond, 50*time.Millisecond).Should(BeFalse(),
				"session B's character stream must NOT have a session_ended event after only A quit")

			// Session B is still usable (can issue commands).
			resp2, cmdErr := srv.grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
				SessionId:          sessionB,
				Command:            "quit",
				PlayerSessionToken: tokenB,
			})
			Expect(cmdErr).NotTo(HaveOccurred())
			Expect(resp2.Success).To(BeTrue())
		})
	})
})
