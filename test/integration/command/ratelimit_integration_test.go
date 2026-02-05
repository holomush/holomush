// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package command_test

import (
	"bytes"
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/accesstest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

// stubServices creates a minimal non-nil Services for tests that don't
// actually use the services.
func stubServices() *command.Services {
	svc, _ := command.NewServices(command.ServicesConfig{
		World:       &world.Service{},
		Session:     &stubSessionService{},
		Access:      &stubAccessControl{},
		Events:      &stubEventStore{},
		Broadcaster: &core.Broadcaster{},
	})
	return svc
}

// Stub implementations for integration tests
type stubSessionService struct{}

func (s *stubSessionService) ListActiveSessions() []*core.Session  { return nil }
func (s *stubSessionService) GetSession(_ ulid.ULID) *core.Session { return nil }
func (s *stubSessionService) EndSession(_ ulid.ULID) error         { return nil }

type stubAccessControl struct{}

func (s *stubAccessControl) Check(_ context.Context, _, _, _ string) bool { return false }

type stubEventStore struct{}

func (s *stubEventStore) Append(_ context.Context, _ core.Event) error { return nil }
func (s *stubEventStore) Replay(_ context.Context, _ string, _ ulid.ULID, _ int) ([]core.Event, error) {
	return nil, nil
}

func (s *stubEventStore) LastEventID(_ context.Context, _ string) (ulid.ULID, error) {
	return ulid.ULID{}, nil
}

func (s *stubEventStore) Subscribe(_ context.Context, _ string) (<-chan ulid.ULID, <-chan error, error) {
	return nil, nil, nil
}

var _ = Describe("Rate Limiting Integration", func() {
	var (
		registry   *command.Registry
		dispatcher *command.Dispatcher
		mockAccess *accesstest.MockAccessControl
	)

	BeforeEach(func() {
		registry = command.NewRegistry()
		mockAccess = accesstest.NewMockAccessControl()
	})

	Describe("End-to-end rate limiting", func() {
		var (
			rateLimiter *command.RateLimiter
			executed    int
		)

		BeforeEach(func() {
			executed = 0

			// Register a test command
			entry, err := command.NewCommandEntry(command.CommandEntryConfig{
				Name: "test",
				Help: "Test command for rate limiting",
				Handler: func(_ context.Context, _ *command.CommandExecution) error {
					executed++
					return nil
				},
				Source: "test",
			})
			Expect(err).NotTo(HaveOccurred())
			err = registry.Register(*entry)
			Expect(err).NotTo(HaveOccurred())

			// Create rate limiter with low burst capacity for testing
			rateLimiter = command.NewRateLimiter(command.RateLimiterConfig{
				BurstCapacity: 3,
				SustainedRate: 10.0, // 10 tokens/second for faster test recovery
			})

			var dispErr error
			dispatcher, dispErr = command.NewDispatcher(registry, mockAccess,
				command.WithRateLimiter(rateLimiter))
			Expect(dispErr).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if rateLimiter != nil {
				rateLimiter.Close()
			}
		})

		It("allows commands up to burst capacity", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sessionID := ulid.Make()
			charID := ulid.Make()

			// Execute commands up to burst capacity
			for i := 0; i < 3; i++ {
				exec := command.NewTestExecution(command.CommandExecutionConfig{
					CharacterID: charID,
					SessionID:   sessionID,
					Output:      &bytes.Buffer{},
					Services:    stubServices(),
				})
				err := dispatcher.Dispatch(ctx, "test", exec)
				Expect(err).NotTo(HaveOccurred(), "Command %d should succeed", i+1)
			}

			Expect(executed).To(Equal(3))
		})

		It("blocks commands after burst capacity is exhausted", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sessionID := ulid.Make()
			charID := ulid.Make()

			// Exhaust burst capacity
			for i := 0; i < 3; i++ {
				exec := command.NewTestExecution(command.CommandExecutionConfig{
					CharacterID: charID,
					SessionID:   sessionID,
					Output:      &bytes.Buffer{},
					Services:    stubServices(),
				})
				err := dispatcher.Dispatch(ctx, "test", exec)
				Expect(err).NotTo(HaveOccurred())
			}

			// Next command should be rate limited
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   sessionID,
				Output:      &bytes.Buffer{},
				Services:    stubServices(),
			})
			err := dispatcher.Dispatch(ctx, "test", exec)
			Expect(err).To(HaveOccurred())

			// Verify error code
			oopsErr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			Expect(oopsErr.Code()).To(Equal(command.CodeRateLimited))

			// Verify player-friendly message
			msg := command.PlayerMessage(err)
			Expect(msg).To(ContainSubstring("slow down"))
		})

		It("returns correct cooldown time when rate limited", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sessionID := ulid.Make()
			charID := ulid.Make()

			// Exhaust burst capacity
			for i := 0; i < 3; i++ {
				exec := command.NewTestExecution(command.CommandExecutionConfig{
					CharacterID: charID,
					SessionID:   sessionID,
					Output:      &bytes.Buffer{},
					Services:    stubServices(),
				})
				err := dispatcher.Dispatch(ctx, "test", exec)
				Expect(err).NotTo(HaveOccurred())
			}

			// Rate limited command should include cooldown
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   sessionID,
				Output:      &bytes.Buffer{},
				Services:    stubServices(),
			})
			err := dispatcher.Dispatch(ctx, "test", exec)
			Expect(err).To(HaveOccurred())

			oopsErr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			// Cooldown should be present in context
			cooldown, hasCooldown := oopsErr.Context()["cooldown_ms"]
			Expect(hasCooldown).To(BeTrue())
			Expect(cooldown).To(BeNumerically(">", 0))
		})

		It("allows commands again after token refill", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sessionID := ulid.Make()
			charID := ulid.Make()

			// Exhaust burst capacity
			for i := 0; i < 3; i++ {
				exec := command.NewTestExecution(command.CommandExecutionConfig{
					CharacterID: charID,
					SessionID:   sessionID,
					Output:      &bytes.Buffer{},
					Services:    stubServices(),
				})
				err := dispatcher.Dispatch(ctx, "test", exec)
				Expect(err).NotTo(HaveOccurred())
			}

			// Should be rate limited
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   sessionID,
				Output:      &bytes.Buffer{},
				Services:    stubServices(),
			})
			err := dispatcher.Dispatch(ctx, "test", exec)
			Expect(err).To(HaveOccurred())

			// Wait for token refill (100ms = 1 token at 10 tokens/second)
			time.Sleep(150 * time.Millisecond)

			// Should be allowed again
			exec2 := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   sessionID,
				Output:      &bytes.Buffer{},
				Services:    stubServices(),
			})
			err = dispatcher.Dispatch(ctx, "test", exec2)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Session independence", func() {
		var rateLimiter *command.RateLimiter

		BeforeEach(func() {
			entry, err := command.NewCommandEntry(command.CommandEntryConfig{
				Name: "test",
				Handler: func(_ context.Context, _ *command.CommandExecution) error {
					return nil
				},
				Source: "test",
			})
			Expect(err).NotTo(HaveOccurred())
			err = registry.Register(*entry)
			Expect(err).NotTo(HaveOccurred())

			rateLimiter = command.NewRateLimiter(command.RateLimiterConfig{
				BurstCapacity: 1,
				SustainedRate: 0.1, // Very slow refill
			})

			var dispErr error
			dispatcher, dispErr = command.NewDispatcher(registry, mockAccess,
				command.WithRateLimiter(rateLimiter))
			Expect(dispErr).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if rateLimiter != nil {
				rateLimiter.Close()
			}
		})

		It("maintains independent rate limits for different sessions", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			session1 := ulid.Make()
			session2 := ulid.Make()

			// Session 1 uses its token
			exec1 := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: ulid.Make(),
				SessionID:   session1,
				Output:      &bytes.Buffer{},
				Services:    stubServices(),
			})
			err := dispatcher.Dispatch(ctx, "test", exec1)
			Expect(err).NotTo(HaveOccurred())

			// Session 1 is now rate limited
			err = dispatcher.Dispatch(ctx, "test", exec1)
			Expect(err).To(HaveOccurred())

			oopsErr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			Expect(oopsErr.Code()).To(Equal(command.CodeRateLimited))

			// Session 2 should still have its own token
			exec2 := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: ulid.Make(),
				SessionID:   session2,
				Output:      &bytes.Buffer{},
				Services:    stubServices(),
			})
			err = dispatcher.Dispatch(ctx, "test", exec2)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rate limits each session independently under concurrent load", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			sessions := make([]ulid.ULID, 5)
			for i := range sessions {
				sessions[i] = ulid.Make()
			}

			// Each session should be able to execute exactly one command
			for _, sessionID := range sessions {
				exec := command.NewTestExecution(command.CommandExecutionConfig{
					CharacterID: ulid.Make(),
					SessionID:   sessionID,
					Output:      &bytes.Buffer{},
					Services:    stubServices(),
				})
				err := dispatcher.Dispatch(ctx, "test", exec)
				Expect(err).NotTo(HaveOccurred())
			}

			// All sessions should now be rate limited
			for _, sessionID := range sessions {
				exec := command.NewTestExecution(command.CommandExecutionConfig{
					CharacterID: ulid.Make(),
					SessionID:   sessionID,
					Output:      &bytes.Buffer{},
					Services:    stubServices(),
				})
				err := dispatcher.Dispatch(ctx, "test", exec)
				Expect(err).To(HaveOccurred())
			}
		})
	})

	Describe("Admin bypass capability", func() {
		var rateLimiter *command.RateLimiter

		BeforeEach(func() {
			entry, err := command.NewCommandEntry(command.CommandEntryConfig{
				Name: "test",
				Handler: func(_ context.Context, _ *command.CommandExecution) error {
					return nil
				},
				Source: "test",
			})
			Expect(err).NotTo(HaveOccurred())
			err = registry.Register(*entry)
			Expect(err).NotTo(HaveOccurred())

			rateLimiter = command.NewRateLimiter(command.RateLimiterConfig{
				BurstCapacity: 1,
				SustainedRate: 0.1, // Very slow refill
			})

			var dispErr error
			dispatcher, dispErr = command.NewDispatcher(registry, mockAccess,
				command.WithRateLimiter(rateLimiter))
			Expect(dispErr).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if rateLimiter != nil {
				rateLimiter.Close()
			}
		})

		It("exempts characters with bypass capability from rate limiting", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			adminCharID := ulid.Make()
			sessionID := ulid.Make()

			// Grant bypass capability to admin character
			mockAccess.Grant("char:"+adminCharID.String(), "execute", command.CapabilityRateLimitBypass)

			// Admin should be able to execute many commands without rate limiting
			for i := 0; i < 10; i++ {
				exec := command.NewTestExecution(command.CommandExecutionConfig{
					CharacterID: adminCharID,
					SessionID:   sessionID,
					Output:      &bytes.Buffer{},
					Services:    stubServices(),
				})
				err := dispatcher.Dispatch(ctx, "test", exec)
				Expect(err).NotTo(HaveOccurred(), "Admin command %d should succeed", i+1)
			}
		})

		It("still rate limits characters without bypass capability", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			adminCharID := ulid.Make()
			regularCharID := ulid.Make()
			adminSession := ulid.Make()
			regularSession := ulid.Make()

			// Grant bypass only to admin
			mockAccess.Grant("char:"+adminCharID.String(), "execute", command.CapabilityRateLimitBypass)

			// Admin can execute multiple commands
			for i := 0; i < 3; i++ {
				exec := command.NewTestExecution(command.CommandExecutionConfig{
					CharacterID: adminCharID,
					SessionID:   adminSession,
					Output:      &bytes.Buffer{},
					Services:    stubServices(),
				})
				err := dispatcher.Dispatch(ctx, "test", exec)
				Expect(err).NotTo(HaveOccurred())
			}

			// Regular user hits rate limit after first command
			exec1 := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: regularCharID,
				SessionID:   regularSession,
				Output:      &bytes.Buffer{},
				Services:    stubServices(),
			})
			err := dispatcher.Dispatch(ctx, "test", exec1)
			Expect(err).NotTo(HaveOccurred())

			exec2 := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: regularCharID,
				SessionID:   regularSession,
				Output:      &bytes.Buffer{},
				Services:    stubServices(),
			})
			err = dispatcher.Dispatch(ctx, "test", exec2)
			Expect(err).To(HaveOccurred())

			oopsErr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			Expect(oopsErr.Code()).To(Equal(command.CodeRateLimited))
		})
	})

	Describe("Rate limiting with aliases", func() {
		var rateLimiter *command.RateLimiter

		BeforeEach(func() {
			entry, err := command.NewCommandEntry(command.CommandEntryConfig{
				Name: "look",
				Handler: func(_ context.Context, _ *command.CommandExecution) error {
					return nil
				},
				Source: "core",
			})
			Expect(err).NotTo(HaveOccurred())
			err = registry.Register(*entry)
			Expect(err).NotTo(HaveOccurred())

			aliasCache := command.NewAliasCache()
			aliasCache.LoadSystemAliases(map[string]string{
				"l": "look",
			})

			rateLimiter = command.NewRateLimiter(command.RateLimiterConfig{
				BurstCapacity: 2,
				SustainedRate: 1.0,
			})

			var dispErr error
			dispatcher, dispErr = command.NewDispatcher(registry, mockAccess,
				command.WithAliasCache(aliasCache),
				command.WithRateLimiter(rateLimiter))
			Expect(dispErr).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if rateLimiter != nil {
				rateLimiter.Close()
			}
		})

		It("applies rate limiting after alias resolution", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sessionID := ulid.Make()
			playerID := ulid.Make()
			charID := ulid.Make()

			// Use alias - should succeed
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   sessionID,
				PlayerID:    playerID,
				Output:      &bytes.Buffer{},
				Services:    stubServices(),
			})
			err := dispatcher.Dispatch(ctx, "l around", exec)
			Expect(err).NotTo(HaveOccurred())

			// Use direct command - should succeed
			err = dispatcher.Dispatch(ctx, "look here", exec)
			Expect(err).NotTo(HaveOccurred())

			// Third command (alias) should be rate limited
			err = dispatcher.Dispatch(ctx, "l again", exec)
			Expect(err).To(HaveOccurred())

			oopsErr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			Expect(oopsErr.Code()).To(Equal(command.CodeRateLimited))
		})
	})

	Describe("Configuration validation", func() {
		It("uses default values when configuration is not provided", func() {
			rl := command.NewRateLimiter(command.RateLimiterConfig{})
			defer rl.Close()

			// Should use defaults: BurstCapacity=10, SustainedRate=2.0
			sessionID := ulid.Make()
			for i := 0; i < 10; i++ {
				allowed, _ := rl.Allow(sessionID)
				Expect(allowed).To(BeTrue(), "Command %d should be allowed with default burst", i+1)
			}

			// 11th should be rate limited
			allowed, cooldown := rl.Allow(sessionID)
			Expect(allowed).To(BeFalse())
			Expect(cooldown).To(BeNumerically(">", 0))
		})

		It("respects custom burst capacity", func() {
			rl := command.NewRateLimiter(command.RateLimiterConfig{
				BurstCapacity: 5,
				SustainedRate: 1.0,
			})
			defer rl.Close()

			sessionID := ulid.Make()
			for i := 0; i < 5; i++ {
				allowed, _ := rl.Allow(sessionID)
				Expect(allowed).To(BeTrue())
			}

			allowed, _ := rl.Allow(sessionID)
			Expect(allowed).To(BeFalse())
		})
	})
})
