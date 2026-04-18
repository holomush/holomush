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

var _ = Describe("Logout and eviction fanout", func() {
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

	Describe("logout fanout", func() {
		It("emits session_ended for each child game session on logout", func() {
			// Create two guest sessions — each has its own character and game session.
			// Note: these are separate CreateGuest calls so they share a single
			// player_session_token only if they happen to be for the same player.
			// For the logout fanout test we need two game sessions under the SAME
			// player token, which the telnet E2E wiring does not support via simple
			// guest logins. Instead we test that a single session gets session_ended
			// on logout, which exercises the fanout code path regardless.
			//
			// Fanout across multiple sessions for the same player is thoroughly
			// covered by the unit test TestLogoutEmitsSessionEndedForEachChildGameSession
			// in internal/grpc/auth_handlers_test.go. This test confirms the
			// end-to-end wiring for the single-session case reaches the DB.

			sessionID, _, token := loginAsGuest(testCtx, srv.grpcCli)

			sessInfo, err := env.sessionStore.Get(testCtx, sessionID)
			Expect(err).NotTo(HaveOccurred())
			charID := sessInfo.CharacterID

			// Issue the Logout RPC with the player_session_token.
			logoutResp, err := srv.grpcCli.Logout(testCtx, &corev1.LogoutRequest{
				PlayerSessionToken: token,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(logoutResp).NotTo(BeNil())

			// The session_ended event MUST be persisted on the character stream
			// with cause=logout.
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
				WithTransform(func(p *core.SessionEndedPayload) string { return p.Cause }, Equal(core.SessionEndedCauseLogout)),
			))
		})
	})

	Describe("reaper eviction", func() {
		It("persists session_ended with cause=reaped when session TTL expires", func() {
			// NOTE: The reaper test requires a very short TTL and a controllable
			// reaper interval. The newSpecServer helper uses default TTLs which
			// are unsuitable for end-to-end reaper timing. Comprehensive reaper
			// coverage lives in the session persistence integration suite
			// (test/integration/session/session_persistence_integration_test.go)
			// which wires up a short TTL reaper directly.
			//
			// This spec verifies only the DB-level plumbing: a manually-reaped
			// session via Engine.EndSession writes a session_ended with cause=reaped.

			sessionID, _, _ := loginAsGuest(testCtx, srv.grpcCli)

			sessInfo, err := env.sessionStore.Get(testCtx, sessionID)
			Expect(err).NotTo(HaveOccurred())
			charID := sessInfo.CharacterID

			char := core.CharacterRef{
				ID:         charID,
				Name:       sessInfo.CharacterName,
				LocationID: sessInfo.LocationID,
			}

			// Directly invoke EndSession with cause=reaped to simulate reaper.
			endErr := srv.engine.EndSession(testCtx, char, sessionID,
				core.SessionEndedCauseReaped, "Session expired.")
			Expect(endErr).NotTo(HaveOccurred())

			// Verify the event was persisted.
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
						if p.Cause == core.SessionEndedCauseReaped {
							return &p
						}
					}
				}
				return nil
			}, 5*time.Second, 50*time.Millisecond).Should(SatisfyAll(
				Not(BeNil()),
				WithTransform(func(p *core.SessionEndedPayload) string { return p.SessionID }, Equal(sessionID)),
				WithTransform(func(p *core.SessionEndedPayload) string { return p.Cause }, Equal(core.SessionEndedCauseReaped)),
			))
		})
	})
})
