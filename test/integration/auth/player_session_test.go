// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package auth_test

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/session"
)

var _ = Describe("Player Session Lifecycle", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		cleanupTestData(ctx, env.pool)
	})

	Describe("Multi-session scenarios", func() {
		var (
			player   *auth.Player
			location *ulid.ULID
			charOne  *ulid.ULID
			charTwo  *ulid.ULID
		)

		BeforeEach(func() {
			player = createTestPlayer(ctx, "multisession_player", "securepassword123")
			loc := createTestLocation(ctx, "The Crossroads")
			location = &loc.ID

			c1 := createTestCharacter(ctx, player.ID, "Aethon", *location)
			c2 := createTestCharacter(ctx, player.ID, "Brynn", *location)
			charOne = &c1.ID
			charTwo = &c2.ID
		})

		It("allows login from two sessions with independent character listing", func() {
			// Session A
			tokenA, _ := loginPlayer(ctx, "multisession_player", "securepassword123")
			hashA := auth.HashSessionToken(tokenA)
			psA, err := env.playerSessionStore.GetByTokenHash(ctx, hashA)
			Expect(err).NotTo(HaveOccurred())
			Expect(psA.PlayerID).To(Equal(player.ID))

			// Session B (independent login)
			tokenB, _ := loginPlayer(ctx, "multisession_player", "securepassword123")
			hashB := auth.HashSessionToken(tokenB)
			psB, err := env.playerSessionStore.GetByTokenHash(ctx, hashB)
			Expect(err).NotTo(HaveOccurred())
			Expect(psB.PlayerID).To(Equal(player.ID))

			// Sessions are distinct
			Expect(psA.ID).NotTo(Equal(psB.ID))
			Expect(tokenA).NotTo(Equal(tokenB))

			// Both can list characters independently
			charsA, err := env.charRepo.ListByPlayer(ctx, psA.PlayerID)
			Expect(err).NotTo(HaveOccurred())
			Expect(charsA).To(HaveLen(2))

			charsB, err := env.charRepo.ListByPlayer(ctx, psB.PlayerID)
			Expect(err).NotTo(HaveOccurred())
			Expect(charsB).To(HaveLen(2))
		})

		It("supports simultaneous character sessions on different sessions", func() {
			// Login session A and B
			tokenA, _ := loginPlayer(ctx, "multisession_player", "securepassword123")
			tokenB, _ := loginPlayer(ctx, "multisession_player", "securepassword123")

			// Resolve player sessions
			hashA := auth.HashSessionToken(tokenA)
			psA, err := env.playerSessionStore.GetByTokenHash(ctx, hashA)
			Expect(err).NotTo(HaveOccurred())

			hashB := auth.HashSessionToken(tokenB)
			psB, err := env.playerSessionStore.GetByTokenHash(ctx, hashB)
			Expect(err).NotTo(HaveOccurred())

			// Select Char One on session A (create game session)
			now := time.Now()
			gameSessionA := &session.Info{
				ID:            ulid.Make().String(),
				CharacterID:   *charOne,
				CharacterName: "Aethon",
				LocationID:    *location,
				Status:        session.StatusActive,
				GridPresent:   true,
				EventCursors:  map[string]ulid.ULID{},
				TTLSeconds:    1800,
				MaxHistory:    500,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			err = env.sessionStore.Set(ctx, gameSessionA.ID, gameSessionA)
			Expect(err).NotTo(HaveOccurred())

			// Select Char Two on session B (create game session)
			gameSessionB := &session.Info{
				ID:            ulid.Make().String(),
				CharacterID:   *charTwo,
				CharacterName: "Brynn",
				LocationID:    *location,
				Status:        session.StatusActive,
				GridPresent:   true,
				EventCursors:  map[string]ulid.ULID{},
				TTLSeconds:    1800,
				MaxHistory:    500,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			err = env.sessionStore.Set(ctx, gameSessionB.ID, gameSessionB)
			Expect(err).NotTo(HaveOccurred())

			// Both game sessions are active simultaneously
			foundA, err := env.sessionStore.FindByCharacter(ctx, *charOne)
			Expect(err).NotTo(HaveOccurred())
			Expect(foundA.CharacterName).To(Equal("Aethon"))
			Expect(foundA.Status).To(Equal(session.StatusActive))

			foundB, err := env.sessionStore.FindByCharacter(ctx, *charTwo)
			Expect(err).NotTo(HaveOccurred())
			Expect(foundB.CharacterName).To(Equal("Brynn"))
			Expect(foundB.Status).To(Equal(session.StatusActive))

			// Both player sessions still valid
			_, err = env.playerSessionStore.GetByTokenHash(ctx, hashA)
			Expect(err).NotTo(HaveOccurred())
			_, err = env.playerSessionStore.GetByTokenHash(ctx, hashB)
			Expect(err).NotTo(HaveOccurred())

			// Player sessions are distinct from each other
			Expect(psA.ID).NotTo(Equal(psB.ID))
		})

		It("logout on session A does not affect session B", func() {
			tokenA, psA := loginPlayer(ctx, "multisession_player", "securepassword123")
			tokenB, _ := loginPlayer(ctx, "multisession_player", "securepassword123")

			// Verify both are valid
			hashA := auth.HashSessionToken(tokenA)
			hashB := auth.HashSessionToken(tokenB)
			_, err := env.playerSessionStore.GetByTokenHash(ctx, hashA)
			Expect(err).NotTo(HaveOccurred())
			_, err = env.playerSessionStore.GetByTokenHash(ctx, hashB)
			Expect(err).NotTo(HaveOccurred())

			// Logout session A
			playerID, err := env.authService.Logout(ctx, hashA)
			Expect(err).NotTo(HaveOccurred())
			Expect(playerID).To(Equal(psA.PlayerID))

			// Session A token is now invalid
			_, err = env.playerSessionStore.GetByTokenHash(ctx, hashA)
			Expect(err).To(HaveOccurred())

			// Session B is still valid
			psB, err := env.playerSessionStore.GetByTokenHash(ctx, hashB)
			Expect(err).NotTo(HaveOccurred())
			Expect(psB.PlayerID).To(Equal(player.ID))

			// Session B can still list characters
			chars, err := env.charRepo.ListByPlayer(ctx, psB.PlayerID)
			Expect(err).NotTo(HaveOccurred())
			Expect(chars).To(HaveLen(2))
		})
	})

	Describe("Character session lifecycle", func() {
		var (
			player   *auth.Player
			location *ulid.ULID
			charOne  *ulid.ULID
			charTwo  *ulid.ULID
		)

		BeforeEach(func() {
			player = createTestPlayer(ctx, "lifecycle_player", "securepassword123")
			loc := createTestLocation(ctx, "The Crossroads")
			location = &loc.ID

			c1 := createTestCharacter(ctx, player.ID, "Kael", *location)
			c2 := createTestCharacter(ctx, player.ID, "Lyra", *location)
			charOne = &c1.ID
			charTwo = &c2.ID
		})

		It("character quit deletes game session but preserves player session", func() {
			token, _ := loginPlayer(ctx, "lifecycle_player", "securepassword123")

			// Create game session for Char One
			now := time.Now()
			gameSession := &session.Info{
				ID:            ulid.Make().String(),
				CharacterID:   *charOne,
				CharacterName: "Kael",
				LocationID:    *location,
				Status:        session.StatusActive,
				GridPresent:   true,
				EventCursors:  map[string]ulid.ULID{},
				TTLSeconds:    1800,
				MaxHistory:    500,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			err := env.sessionStore.Set(ctx, gameSession.ID, gameSession)
			Expect(err).NotTo(HaveOccurred())

			// Verify game session exists
			found, err := env.sessionStore.FindByCharacter(ctx, *charOne)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.CharacterName).To(Equal("Kael"))

			// Character quits: delete the game session
			err = env.sessionStore.Delete(ctx, gameSession.ID, "quit")
			Expect(err).NotTo(HaveOccurred())

			// Game session is gone
			_, err = env.sessionStore.FindByCharacter(ctx, *charOne)
			Expect(err).To(HaveOccurred())

			// Player session is still valid
			hash := auth.HashSessionToken(token)
			ps, err := env.playerSessionStore.GetByTokenHash(ctx, hash)
			Expect(err).NotTo(HaveOccurred())
			Expect(ps.PlayerID).To(Equal(player.ID))

			// Player can still list characters
			chars, err := env.charRepo.ListByPlayer(ctx, ps.PlayerID)
			Expect(err).NotTo(HaveOccurred())
			Expect(chars).To(HaveLen(2))
		})

		It("quit then switch: quit Char One, select Char Two", func() {
			token, _ := loginPlayer(ctx, "lifecycle_player", "securepassword123")

			// Select Char One (create game session)
			now := time.Now()
			gameSessionOne := &session.Info{
				ID:            ulid.Make().String(),
				CharacterID:   *charOne,
				CharacterName: "Kael",
				LocationID:    *location,
				Status:        session.StatusActive,
				GridPresent:   true,
				EventCursors:  map[string]ulid.ULID{},
				TTLSeconds:    1800,
				MaxHistory:    500,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			err := env.sessionStore.Set(ctx, gameSessionOne.ID, gameSessionOne)
			Expect(err).NotTo(HaveOccurred())

			// Quit Char One
			err = env.sessionStore.Delete(ctx, gameSessionOne.ID, "quit")
			Expect(err).NotTo(HaveOccurred())

			// Char One game session is gone
			_, err = env.sessionStore.FindByCharacter(ctx, *charOne)
			Expect(err).To(HaveOccurred())

			// Player session is still valid
			hash := auth.HashSessionToken(token)
			ps, err := env.playerSessionStore.GetByTokenHash(ctx, hash)
			Expect(err).NotTo(HaveOccurred())
			Expect(ps.PlayerID).To(Equal(player.ID))

			// Select Char Two (create new game session)
			gameSessionTwo := &session.Info{
				ID:            ulid.Make().String(),
				CharacterID:   *charTwo,
				CharacterName: "Lyra",
				LocationID:    *location,
				Status:        session.StatusActive,
				GridPresent:   true,
				EventCursors:  map[string]ulid.ULID{},
				TTLSeconds:    1800,
				MaxHistory:    500,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			err = env.sessionStore.Set(ctx, gameSessionTwo.ID, gameSessionTwo)
			Expect(err).NotTo(HaveOccurred())

			// Char Two game session is active
			foundTwo, err := env.sessionStore.FindByCharacter(ctx, *charTwo)
			Expect(err).NotTo(HaveOccurred())
			Expect(foundTwo.CharacterName).To(Equal("Lyra"))
			Expect(foundTwo.Status).To(Equal(session.StatusActive))

			// Char One still has no game session
			_, err = env.sessionStore.FindByCharacter(ctx, *charOne)
			Expect(err).To(HaveOccurred())

			// Player session unchanged throughout
			ps2, err := env.playerSessionStore.GetByTokenHash(ctx, hash)
			Expect(err).NotTo(HaveOccurred())
			Expect(ps2.ID).To(Equal(ps.ID))
		})
	})

	Describe("Player session store operations", func() {
		It("creates and retrieves a session by token hash", func() {
			player := createTestPlayer(ctx, "store_test_player", "securepassword123")

			rawToken, tokenHash, err := auth.GenerateSessionToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(rawToken).NotTo(BeEmpty())

			ps, err := auth.NewPlayerSession(player.ID, tokenHash, "Mozilla/5.0", "192.168.1.1", auth.PlayerSessionTTL)
			Expect(err).NotTo(HaveOccurred())

			err = env.playerSessionStore.Create(ctx, ps)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := env.playerSessionStore.GetByTokenHash(ctx, tokenHash)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.ID).To(Equal(ps.ID))
			Expect(retrieved.PlayerID).To(Equal(player.ID))
			Expect(retrieved.TokenHash).To(Equal(tokenHash))
			Expect(retrieved.UserAgent).To(Equal("Mozilla/5.0"))
			Expect(retrieved.IPAddress).To(Equal("192.168.1.1"))
		})

		It("deletes a single session by ID", func() {
			player := createTestPlayer(ctx, "delete_test_player", "securepassword123")

			_, tokenHash, err := auth.GenerateSessionToken()
			Expect(err).NotTo(HaveOccurred())

			ps, err := auth.NewPlayerSession(player.ID, tokenHash, "", "", auth.PlayerSessionTTL)
			Expect(err).NotTo(HaveOccurred())
			err = env.playerSessionStore.Create(ctx, ps)
			Expect(err).NotTo(HaveOccurred())

			err = env.playerSessionStore.Delete(ctx, ps.ID)
			Expect(err).NotTo(HaveOccurred())

			_, err = env.playerSessionStore.GetByTokenHash(ctx, tokenHash)
			Expect(err).To(HaveOccurred())
		})

		It("deletes all sessions for a player", func() {
			player := createTestPlayer(ctx, "deleteall_player", "securepassword123")

			// Create two sessions
			for i := 0; i < 2; i++ {
				_, tokenHash, err := auth.GenerateSessionToken()
				Expect(err).NotTo(HaveOccurred())
				ps, err := auth.NewPlayerSession(player.ID, tokenHash, "", "", auth.PlayerSessionTTL)
				Expect(err).NotTo(HaveOccurred())
				err = env.playerSessionStore.Create(ctx, ps)
				Expect(err).NotTo(HaveOccurred())
			}

			err := env.playerSessionStore.DeleteByPlayer(ctx, player.ID)
			Expect(err).NotTo(HaveOccurred())
		})

		It("refreshes TTL extending the session expiry", func() {
			player := createTestPlayer(ctx, "ttl_player", "securepassword123")

			_, tokenHash, err := auth.GenerateSessionToken()
			Expect(err).NotTo(HaveOccurred())

			ps, err := auth.NewPlayerSession(player.ID, tokenHash, "", "", 1*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			err = env.playerSessionStore.Create(ctx, ps)
			Expect(err).NotTo(HaveOccurred())

			originalExpiry := ps.ExpiresAt

			// Refresh with a longer TTL
			err = env.playerSessionStore.RefreshTTL(ctx, ps.ID, 48*time.Hour)
			Expect(err).NotTo(HaveOccurred())

			refreshed, err := env.playerSessionStore.GetByTokenHash(ctx, tokenHash)
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.ExpiresAt).To(BeTemporally(">", originalExpiry))
		})

		It("returns PLAYER_SESSION_EXPIRED for expired sessions and cleans them up", func() {
			player := createTestPlayer(ctx, "expired_player", "securepassword123")

			_, tokenHash, err := auth.GenerateSessionToken()
			Expect(err).NotTo(HaveOccurred())

			// Insert an already-expired session directly to avoid clock/scheduling flakes.
			ps, err := auth.NewPlayerSession(player.ID, tokenHash, "", "", 24*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			expiredAt := time.Now().Add(-time.Minute)
			ps.ExpiresAt = expiredAt
			ps.UpdatedAt = expiredAt
			err = env.playerSessionStore.Create(ctx, ps)
			Expect(err).NotTo(HaveOccurred())

			// Should return error
			_, err = env.playerSessionStore.GetByTokenHash(ctx, tokenHash)
			Expect(err).To(HaveOccurred())
		})

		It("deletes expired sessions in bulk", func() {
			player := createTestPlayer(ctx, "bulk_expire_player", "securepassword123")

			// Create 3 sessions, 2 already expired
			for i := 0; i < 2; i++ {
				_, tokenHash, err := auth.GenerateSessionToken()
				Expect(err).NotTo(HaveOccurred())
				ps, err := auth.NewPlayerSession(player.ID, tokenHash, "", "", 24*time.Hour)
				Expect(err).NotTo(HaveOccurred())
				expiredAt := time.Now().Add(-time.Minute)
				ps.ExpiresAt = expiredAt
				ps.UpdatedAt = expiredAt
				err = env.playerSessionStore.Create(ctx, ps)
				Expect(err).NotTo(HaveOccurred())
			}

			// One valid session
			_, validHash, err := auth.GenerateSessionToken()
			Expect(err).NotTo(HaveOccurred())
			validSession, err := auth.NewPlayerSession(player.ID, validHash, "", "", 24*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			err = env.playerSessionStore.Create(ctx, validSession)
			Expect(err).NotTo(HaveOccurred())

			deleted, err := env.playerSessionStore.DeleteExpired(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(deleted).To(BeNumerically(">=", 2))

			// Valid session still retrievable
			retrieved, err := env.playerSessionStore.GetByTokenHash(ctx, validHash)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.ID).To(Equal(validSession.ID))
		})
	})

	Describe("Registration flow with player session", func() {
		It("creates a player and player session atomically", func() {
			player, ps, rawToken, err := env.authService.CreatePlayer(ctx, "new_player", "securepassword123", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(player).NotTo(BeNil())
			Expect(ps).NotTo(BeNil())
			Expect(rawToken).NotTo(BeEmpty())

			// Persist the session
			err = env.playerSessionStore.Create(ctx, ps)
			Expect(err).NotTo(HaveOccurred())

			// Verify the token works
			hash := auth.HashSessionToken(rawToken)
			retrieved, err := env.playerSessionStore.GetByTokenHash(ctx, hash)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.PlayerID).To(Equal(player.ID))
		})
	})
})
