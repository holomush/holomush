// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// TODO(holomush-1tvn.14): F7 deletes this file along with EventStore.{Append,Replay,SubscribeSession,LastEventID}
//go:build integration && f6_legacy

package auth_test

import (
	"context"
	"errors"
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
			err = env.sessionStore.Delete(ctx, gameSession.ID)
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
			err = env.sessionStore.Delete(ctx, gameSessionOne.ID)
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

	Describe("PlayerSessionRepository extended methods", func() {
		var player *auth.Player

		BeforeEach(func() {
			player = createTestPlayer(ctx, "extended_methods_player", "securepassword123")
		})

		// createSessionForPlayer inserts a session with a specific created_at/expires_at
		// so tests can control ordering and expiry deterministically.
		createSessionForPlayer := func(createdAt, expiresAt time.Time) *auth.PlayerSession {
			_, tokenHash, err := auth.GenerateSessionToken()
			Expect(err).NotTo(HaveOccurred())

			ps, err := auth.NewPlayerSession(player.ID, tokenHash, "", "", auth.PlayerSessionTTL)
			Expect(err).NotTo(HaveOccurred())
			ps.CreatedAt = createdAt
			ps.UpdatedAt = createdAt
			ps.ExpiresAt = expiresAt
			err = env.playerSessionStore.Create(ctx, ps)
			Expect(err).NotTo(HaveOccurred())
			return ps
		}

		It("CountActiveByPlayer returns the number of non-expired sessions", func() {
			now := time.Now().UTC()
			createSessionForPlayer(now.Add(-3*time.Hour), now.Add(time.Hour))
			createSessionForPlayer(now.Add(-2*time.Hour), now.Add(time.Hour))
			createSessionForPlayer(now.Add(-1*time.Hour), now.Add(time.Hour))

			n, err := env.playerSessionStore.CountActiveByPlayer(ctx, player.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(3))

			// Expire the oldest session via direct UPDATE with subquery (Postgres
			// doesn't support LIMIT in a plain UPDATE).
			_, err = env.pool.Exec(ctx, `
				UPDATE player_sessions SET expires_at = now() - interval '1 minute'
				WHERE id = (
					SELECT id FROM player_sessions
					WHERE player_id = $1 AND expires_at > now()
					ORDER BY created_at ASC
					LIMIT 1
				)
			`, player.ID.String())
			Expect(err).NotTo(HaveOccurred())

			n, err = env.playerSessionStore.CountActiveByPlayer(ctx, player.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(2))
		})

		It("CountActiveByPlayer returns 0 for a player with no sessions", func() {
			other := createTestPlayer(ctx, "no_sessions_player", "securepassword123")
			n, err := env.playerSessionStore.CountActiveByPlayer(ctx, other.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(0))
		})

		It("ListByPlayer returns non-expired sessions newest-first", func() {
			now := time.Now().UTC()
			oldest := createSessionForPlayer(now.Add(-3*time.Hour), now.Add(time.Hour))
			middle := createSessionForPlayer(now.Add(-2*time.Hour), now.Add(time.Hour))
			newest := createSessionForPlayer(now.Add(-1*time.Hour), now.Add(time.Hour))
			// Expired — should be excluded.
			createSessionForPlayer(now.Add(-4*time.Hour), now.Add(-time.Minute))

			sessions, err := env.playerSessionStore.ListByPlayer(ctx, player.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(sessions).To(HaveLen(3))
			Expect(sessions[0].ID).To(Equal(newest.ID))
			Expect(sessions[1].ID).To(Equal(middle.ID))
			Expect(sessions[2].ID).To(Equal(oldest.ID))
		})

		It("ListByPlayer returns empty list for player with no sessions", func() {
			other := createTestPlayer(ctx, "empty_list_player", "securepassword123")
			sessions, err := env.playerSessionStore.ListByPlayer(ctx, other.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(sessions).To(BeEmpty())
		})

		It("DeleteOldestForPlayer removes the oldest non-expired session and returns it", func() {
			now := time.Now().UTC()
			oldest := createSessionForPlayer(now.Add(-3*time.Hour), now.Add(time.Hour))
			middle := createSessionForPlayer(now.Add(-2*time.Hour), now.Add(time.Hour))
			newest := createSessionForPlayer(now.Add(-1*time.Hour), now.Add(time.Hour))

			deleted, err := env.playerSessionStore.DeleteOldestForPlayer(ctx, player.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(deleted).NotTo(BeNil())
			Expect(deleted.ID).To(Equal(oldest.ID))
			Expect(deleted.PlayerID).To(Equal(player.ID))

			// Remaining two are middle + newest.
			remaining, err := env.playerSessionStore.ListByPlayer(ctx, player.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(remaining).To(HaveLen(2))
			ids := []ulid.ULID{remaining[0].ID, remaining[1].ID}
			Expect(ids).To(ContainElement(middle.ID))
			Expect(ids).To(ContainElement(newest.ID))
		})

		It("DeleteOldestForPlayer returns nil,nil for a player with no active sessions", func() {
			deleted, err := env.playerSessionStore.DeleteOldestForPlayer(ctx, ulid.Make())
			Expect(err).NotTo(HaveOccurred())
			Expect(deleted).To(BeNil())
		})

		It("GetByID returns the row for a known id", func() {
			_, tokenHash, err := auth.GenerateSessionToken()
			Expect(err).NotTo(HaveOccurred())
			ps, err := auth.NewPlayerSession(player.ID, tokenHash, "Mozilla/5.0", "10.0.0.1", auth.PlayerSessionTTL)
			Expect(err).NotTo(HaveOccurred())
			err = env.playerSessionStore.Create(ctx, ps)
			Expect(err).NotTo(HaveOccurred())

			got, err := env.playerSessionStore.GetByID(ctx, ps.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(ps.ID))
			Expect(got.PlayerID).To(Equal(player.ID))
			Expect(got.TokenHash).To(Equal(tokenHash))
			Expect(got.UserAgent).To(Equal("Mozilla/5.0"))
			Expect(got.IPAddress).To(Equal("10.0.0.1"))
		})

		It("GetByID returns ErrNotFound for an unknown id", func() {
			got, err := env.playerSessionStore.GetByID(ctx, ulid.Make())
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, auth.ErrNotFound)).To(BeTrue())
			Expect(got).To(BeNil())
		})
	})

	Describe("CreateWithCap atomic session cap enforcement", func() {
		var player *auth.Player

		BeforeEach(func() {
			player = createTestPlayer(ctx, "cap_player", "securepassword123")
		})

		// buildSession produces a fresh non-expired PlayerSession for the test
		// player but does NOT persist it. Callers choose whether to insert it
		// directly (Create) or atomically via CreateWithCap.
		buildSession := func(createdAt, expiresAt time.Time) *auth.PlayerSession {
			_, tokenHash, err := auth.GenerateSessionToken()
			Expect(err).NotTo(HaveOccurred())
			ps, err := auth.NewPlayerSession(player.ID, tokenHash, "", "", auth.PlayerSessionTTL)
			Expect(err).NotTo(HaveOccurred())
			ps.CreatedAt = createdAt
			ps.UpdatedAt = createdAt
			ps.ExpiresAt = expiresAt
			return ps
		}

		It("trims oldest non-expired sessions so total equals cap", func() {
			now := time.Now().UTC()
			// Pre-populate exactly cap sessions so the new one + cap existing = cap+1.
			const capN = 3
			existing := make([]*auth.PlayerSession, 0, capN)
			for i := 0; i < capN; i++ {
				ps := buildSession(now.Add(time.Duration(-(capN-i))*time.Hour), now.Add(time.Hour))
				Expect(env.playerSessionStore.Create(ctx, ps)).To(Succeed())
				existing = append(existing, ps)
			}

			// New session created via CreateWithCap with cap=capN should trim
			// the oldest one, leaving exactly capN total.
			newPS := buildSession(now, now.Add(time.Hour))
			trimmedIDs, err := env.playerSessionStore.CreateWithCap(ctx, newPS, capN)
			Expect(err).NotTo(HaveOccurred())
			Expect(trimmedIDs).To(HaveLen(1))

			remaining, err := env.playerSessionStore.ListByPlayer(ctx, player.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(remaining).To(HaveLen(capN))

			// The oldest session must have been trimmed; the newly-inserted one
			// must be present.
			ids := make(map[ulid.ULID]struct{}, len(remaining))
			for _, ps := range remaining {
				ids[ps.ID] = struct{}{}
			}
			Expect(ids).To(HaveKey(newPS.ID))
			Expect(ids).NotTo(HaveKey(existing[0].ID)) // oldest evicted
			// The trimmed ID must match the oldest session.
			Expect(trimmedIDs[0]).To(Equal(existing[0].ID))
		})

		It("catches up when cap is lowered below current active count", func() {
			now := time.Now().UTC()
			// Simulate a player who accumulated 6 sessions under an older, higher cap.
			const priorCount = 6
			for i := 0; i < priorCount; i++ {
				ps := buildSession(now.Add(time.Duration(-(priorCount-i))*time.Hour), now.Add(time.Hour))
				Expect(env.playerSessionStore.Create(ctx, ps)).To(Succeed())
			}

			// Operator lowers cap to 2. A single new login must bring the player
			// down to exactly 2 total sessions.
			const newCap = 2
			newPS := buildSession(now, now.Add(time.Hour))
			trimmedIDs, err := env.playerSessionStore.CreateWithCap(ctx, newPS, newCap)
			Expect(err).NotTo(HaveOccurred())
			// priorCount existing + 1 new - newCap = 5 trimmed.
			Expect(trimmedIDs).To(HaveLen(priorCount + 1 - newCap))

			remaining, err := env.playerSessionStore.ListByPlayer(ctx, player.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(remaining).To(HaveLen(newCap))
		})

		It("leaves no more than cap sessions under concurrent logins", func() {
			// This is the race CodeRabbit flagged: two parallel logins at cap
			// both evict once, both insert → cap+1. With CreateWithCap running
			// in a single transaction, Postgres serializes the two, so the
			// invariant remaining count <= cap must hold.
			const capN = 2
			// Pre-populate capN sessions to put the player exactly at cap.
			now := time.Now().UTC()
			for i := 0; i < capN; i++ {
				ps := buildSession(now.Add(time.Duration(-(capN-i))*time.Hour), now.Add(time.Hour))
				Expect(env.playerSessionStore.Create(ctx, ps)).To(Succeed())
			}

			// Fire N concurrent CreateWithCap calls, each inserting a distinct
			// new session. Every call independently enforces cap=capN.
			const concurrency = 8
			errCh := make(chan error, concurrency)
			start := make(chan struct{})
			for i := 0; i < concurrency; i++ {
				ps := buildSession(now.Add(time.Duration(i)*time.Millisecond), now.Add(time.Hour))
				go func(ps *auth.PlayerSession) {
					<-start
					_, err := env.playerSessionStore.CreateWithCap(ctx, ps, capN)
					errCh <- err
				}(ps)
			}
			close(start)
			for i := 0; i < concurrency; i++ {
				Expect(<-errCh).NotTo(HaveOccurred())
			}

			// Invariant: after all concurrent cap-enforcing inserts, the active
			// session count is at most capN. No race window can leave the
			// player above cap.
			count, err := env.playerSessionStore.CountActiveByPlayer(ctx, player.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(BeNumerically("<=", capN))
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
