// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/store"
)

// newTestSession creates a session.Info with sensible defaults for testing.
func newTestSession(id string) *session.Info {
	now := time.Now().UTC()
	return &session.Info{
		ID:             id,
		CharacterID:    ulid.Make(),
		CharacterName:  "TestChar",
		LocationID:     ulid.Make(),
		IsGuest:        false,
		Status:         session.StatusActive,
		GridPresent:    true,
		EventCursors:   map[string]ulid.ULID{},
		CommandHistory: []string{},
		TTLSeconds:     300,
		MaxHistory:     50,
		CreatedAt:      now,
	}
}

var _ = Describe("PostgresSessionStore", func() {
	var sessionStore *store.PostgresSessionStore
	var cleanup func()

	BeforeEach(func() {
		eventStore, cl, err := setupPostgresContainer()
		Expect(err).NotTo(HaveOccurred())
		cleanup = cl

		sessionStore = store.NewPostgresSessionStore(eventStore.Pool())
	})

	AfterEach(func() {
		cleanup()
	})

	Describe("CRUD operations", func() {
		It("creates and retrieves a session", func() {
			ctx := context.Background()
			info := newTestSession("sess-crud-create")

			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			got, err := sessionStore.Get(ctx, info.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(info.ID))
			Expect(got.CharacterID).To(Equal(info.CharacterID))
			Expect(got.CharacterName).To(Equal(info.CharacterName))
			Expect(got.LocationID).To(Equal(info.LocationID))
			Expect(got.Status).To(Equal(session.StatusActive))
			Expect(got.GridPresent).To(BeTrue())
			Expect(got.TTLSeconds).To(Equal(300))
			Expect(got.MaxHistory).To(Equal(50))
		})

		It("updates an existing session via upsert", func() {
			ctx := context.Background()
			info := newTestSession("sess-crud-upsert")

			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			info.CharacterName = "UpdatedName"
			info.GridPresent = false
			err = sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			got, err := sessionStore.Get(ctx, info.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.CharacterName).To(Equal("UpdatedName"))
			Expect(got.GridPresent).To(BeFalse())
		})

		It("deletes a session", func() {
			ctx := context.Background()
			info := newTestSession("sess-crud-delete")

			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			err = sessionStore.Delete(ctx, info.ID, "test")
			Expect(err).NotTo(HaveOccurred())

			_, err = sessionStore.Get(ctx, info.ID)
			Expect(err).To(HaveOccurred())
		})

		It("returns error for non-existent session", func() {
			ctx := context.Background()
			_, err := sessionStore.Get(ctx, "sess-nonexistent")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("FindByCharacter", func() {
		It("finds active session by character ID", func() {
			ctx := context.Background()
			info := newTestSession("sess-find-active")

			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			got, err := sessionStore.FindByCharacter(ctx, info.CharacterID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(info.ID))
			Expect(got.Status).To(Equal(session.StatusActive))
		})

		It("finds detached session by character ID", func() {
			ctx := context.Background()
			info := newTestSession("sess-find-detached")
			info.Status = session.StatusDetached
			now := time.Now().UTC()
			expires := now.Add(5 * time.Minute)
			info.DetachedAt = &now
			info.ExpiresAt = &expires

			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			got, err := sessionStore.FindByCharacter(ctx, info.CharacterID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(info.ID))
			Expect(got.Status).To(Equal(session.StatusDetached))
		})

		It("skips expired sessions", func() {
			ctx := context.Background()
			info := newTestSession("sess-find-expired")
			info.Status = session.StatusExpired

			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			_, err = sessionStore.FindByCharacter(ctx, info.CharacterID)
			Expect(err).To(HaveOccurred())
		})

		It("returns error when no active/detached session exists", func() {
			ctx := context.Background()
			_, err := sessionStore.FindByCharacter(ctx, ulid.Make())
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ReattachCAS", func() {
		It("transitions detached session to active", func() {
			ctx := context.Background()
			info := newTestSession("sess-reattach-ok")
			info.Status = session.StatusDetached
			now := time.Now().UTC()
			expires := now.Add(5 * time.Minute)
			info.DetachedAt = &now
			info.ExpiresAt = &expires

			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			ok, err := sessionStore.ReattachCAS(ctx, info.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())

			got, err := sessionStore.Get(ctx, info.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(session.StatusActive))
			Expect(got.DetachedAt).To(BeNil())
			Expect(got.ExpiresAt).To(BeNil())
		})

		It("returns false for already-active session", func() {
			ctx := context.Background()
			info := newTestSession("sess-reattach-active")

			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			ok, err := sessionStore.ReattachCAS(ctx, info.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())
		})

		It("handles concurrent reattach attempts", func() {
			ctx := context.Background()
			info := newTestSession("sess-reattach-race")
			info.Status = session.StatusDetached
			now := time.Now().UTC()
			expires := now.Add(5 * time.Minute)
			info.DetachedAt = &now
			info.ExpiresAt = &expires

			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			var wg sync.WaitGroup
			results := make([]bool, 2)
			errs := make([]error, 2)

			for i := range 2 {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					results[idx], errs[idx] = sessionStore.ReattachCAS(ctx, info.ID)
				}(i)
			}
			wg.Wait()

			Expect(errs[0]).NotTo(HaveOccurred())
			Expect(errs[1]).NotTo(HaveOccurred())

			successes := 0
			for _, ok := range results {
				if ok {
					successes++
				}
			}
			Expect(successes).To(Equal(1), "exactly one goroutine should win the CAS race")
		})
	})

	Describe("Connection tracking", func() {
		var sessID string

		BeforeEach(func() {
			ctx := context.Background()
			sessID = fmt.Sprintf("sess-conn-%s", ulid.Make().String()[:8])
			info := newTestSession(sessID)
			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())
		})

		It("adds and counts connections", func() {
			ctx := context.Background()
			conn := &session.Connection{
				ID:          ulid.Make(),
				SessionID:   sessID,
				ClientType:  "telnet",
				Streams:     []string{"location:room1"},
				ConnectedAt: time.Now().UTC(),
			}

			err := sessionStore.AddConnection(ctx, conn)
			Expect(err).NotTo(HaveOccurred())

			count, err := sessionStore.CountConnections(ctx, sessID)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))
		})

		It("removes connections", func() {
			ctx := context.Background()
			connID := ulid.Make()
			conn := &session.Connection{
				ID:          connID,
				SessionID:   sessID,
				ClientType:  "telnet",
				Streams:     []string{"location:room1"},
				ConnectedAt: time.Now().UTC(),
			}

			err := sessionStore.AddConnection(ctx, conn)
			Expect(err).NotTo(HaveOccurred())

			err = sessionStore.RemoveConnection(ctx, connID)
			Expect(err).NotTo(HaveOccurred())

			count, err := sessionStore.CountConnections(ctx, sessID)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(0))
		})

		It("counts connections by type", func() {
			ctx := context.Background()
			for _, ct := range []string{"telnet", "telnet", "comms_hub"} {
				conn := &session.Connection{
					ID:          ulid.Make(),
					SessionID:   sessID,
					ClientType:  ct,
					Streams:     []string{"location:start"},
					ConnectedAt: time.Now().UTC(),
				}
				err := sessionStore.AddConnection(ctx, conn)
				Expect(err).NotTo(HaveOccurred())
			}

			telnetCount, err := sessionStore.CountConnectionsByType(ctx, sessID, "telnet")
			Expect(err).NotTo(HaveOccurred())
			Expect(telnetCount).To(Equal(2))

			commsCount, err := sessionStore.CountConnectionsByType(ctx, sessID, "comms_hub")
			Expect(err).NotTo(HaveOccurred())
			Expect(commsCount).To(Equal(1))
		})

		It("cascades connection deletion when session is deleted", func() {
			ctx := context.Background()
			conn := &session.Connection{
				ID:          ulid.Make(),
				SessionID:   sessID,
				ClientType:  "telnet",
				Streams:     []string{"location:room1"},
				ConnectedAt: time.Now().UTC(),
			}
			err := sessionStore.AddConnection(ctx, conn)
			Expect(err).NotTo(HaveOccurred())

			err = sessionStore.Delete(ctx, sessID, "test")
			Expect(err).NotTo(HaveOccurred())

			count, err := sessionStore.CountConnections(ctx, sessID)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(0))
		})
	})

	Describe("Command history", func() {
		var sessID string

		BeforeEach(func() {
			ctx := context.Background()
			sessID = fmt.Sprintf("sess-hist-%s", ulid.Make().String()[:8])
			info := newTestSession(sessID)
			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())
		})

		It("appends commands to history", func() {
			ctx := context.Background()
			err := sessionStore.AppendCommand(ctx, sessID, "look", 50)
			Expect(err).NotTo(HaveOccurred())
			err = sessionStore.AppendCommand(ctx, sessID, "say hello", 50)
			Expect(err).NotTo(HaveOccurred())

			history, err := sessionStore.GetCommandHistory(ctx, sessID)
			Expect(err).NotTo(HaveOccurred())
			Expect(history).To(Equal([]string{"look", "say hello"}))
		})

		It("enforces history cap", func() {
			ctx := context.Background()
			maxHistory := 3
			for i := range 5 {
				err := sessionStore.AppendCommand(ctx, sessID, fmt.Sprintf("cmd%d", i), maxHistory)
				Expect(err).NotTo(HaveOccurred())
			}

			history, err := sessionStore.GetCommandHistory(ctx, sessID)
			Expect(err).NotTo(HaveOccurred())
			Expect(history).To(HaveLen(maxHistory))
			Expect(history).To(Equal([]string{"cmd2", "cmd3", "cmd4"}))
		})

		It("trims when exactly at capacity", func() {
			ctx := context.Background()
			maxHistory := 3

			// Fill to exactly cap
			for i := range maxHistory {
				err := sessionStore.AppendCommand(ctx, sessID, fmt.Sprintf("cmd%d", i), maxHistory)
				Expect(err).NotTo(HaveOccurred())
			}

			history, err := sessionStore.GetCommandHistory(ctx, sessID)
			Expect(err).NotTo(HaveOccurred())
			Expect(history).To(HaveLen(maxHistory))
			Expect(history).To(Equal([]string{"cmd0", "cmd1", "cmd2"}))

			// Add one more — should push out cmd0
			err = sessionStore.AppendCommand(ctx, sessID, "cmd3", maxHistory)
			Expect(err).NotTo(HaveOccurred())

			history, err = sessionStore.GetCommandHistory(ctx, sessID)
			Expect(err).NotTo(HaveOccurred())
			Expect(history).To(HaveLen(maxHistory))
			Expect(history).To(Equal([]string{"cmd1", "cmd2", "cmd3"}))
		})
	})

	Describe("ListExpired", func() {
		It("returns detached sessions past expiry", func() {
			ctx := context.Background()
			info := newTestSession("sess-expired-past")
			info.Status = session.StatusDetached
			past := time.Now().UTC().Add(-1 * time.Minute)
			info.DetachedAt = &past
			info.ExpiresAt = &past

			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			expired, err := sessionStore.ListExpired(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(expired).To(HaveLen(1))
			Expect(expired[0].ID).To(Equal(info.ID))
		})

		It("excludes active sessions even with expired time", func() {
			ctx := context.Background()
			info := newTestSession("sess-expired-active")
			info.Status = session.StatusActive
			past := time.Now().UTC().Add(-1 * time.Minute)
			info.ExpiresAt = &past

			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			expired, err := sessionStore.ListExpired(ctx)
			Expect(err).NotTo(HaveOccurred())
			for _, s := range expired {
				Expect(s.ID).NotTo(Equal(info.ID))
			}
		})

		It("excludes detached sessions not yet expired", func() {
			ctx := context.Background()
			info := newTestSession("sess-expired-future")
			info.Status = session.StatusDetached
			now := time.Now().UTC()
			future := now.Add(10 * time.Minute)
			info.DetachedAt = &now
			info.ExpiresAt = &future

			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			expired, err := sessionStore.ListExpired(ctx)
			Expect(err).NotTo(HaveOccurred())
			for _, s := range expired {
				Expect(s.ID).NotTo(Equal(info.ID))
			}
		})
	})

	Describe("UpdateCursors", func() {
		It("merges new cursors with existing", func() {
			ctx := context.Background()
			info := newTestSession("sess-cursors")
			cursor1 := ulid.Make()
			info.EventCursors = map[string]ulid.ULID{
				"location:room1": cursor1,
			}

			err := sessionStore.Set(ctx, info.ID, info)
			Expect(err).NotTo(HaveOccurred())

			cursor2 := ulid.Make()
			err = sessionStore.UpdateCursors(ctx, info.ID, map[string]ulid.ULID{
				"location:room2": cursor2,
			})
			Expect(err).NotTo(HaveOccurred())

			got, err := sessionStore.Get(ctx, info.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.EventCursors).To(HaveLen(2))
			Expect(got.EventCursors["location:room1"]).To(Equal(cursor1))
			Expect(got.EventCursors["location:room2"]).To(Equal(cursor2))
		})

		It("rejects a cursor regression for the same stream key", func() {
			// The CAS guard in UpdateCursors must preserve the highest cursor
			// ever stored for a (session, stream) pair. Regression attempts
			// (e.g., from a concurrent Subscribe that observed an earlier
			// event) must be silently ignored — RowsAffected==0 is not an
			// error, it just means another writer won with a higher cursor.
			ctx := context.Background()
			info := newTestSession("sess-cas-regression")
			Expect(sessionStore.Set(ctx, info.ID, info)).To(Succeed())

			// Mint two cursors with core.NewULID so they are strictly monotonic.
			// The second one is the lex-larger ("higher") cursor.
			earlier := core.NewULID()
			time.Sleep(1 * time.Millisecond)
			later := core.NewULID()
			Expect(earlier.String() < later.String()).To(BeTrue(), "earlier ULID must be lex-less than later ULID")

			streamKey := "location:room-cas"

			// First write: later (the higher cursor).
			Expect(sessionStore.UpdateCursors(ctx, info.ID, map[string]ulid.ULID{
				streamKey: later,
			})).To(Succeed())

			// Second write: earlier (a regression). Must not error, but
			// must not overwrite the stored later.
			Expect(sessionStore.UpdateCursors(ctx, info.ID, map[string]ulid.ULID{
				streamKey: earlier,
			})).To(Succeed(),
				"regression attempts must not be errors — CAS rows_affected==0 is normal")

			got, err := sessionStore.Get(ctx, info.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.EventCursors[streamKey]).To(Equal(later),
				"stored cursor must remain the higher (later) value")
		})

		It("rejects multi-key cursor writes with UNSUPPORTED", func() {
			// Current production code (replayAndSend) always writes exactly
			// one key per call. Multi-key writes cannot be handled by a
			// single-statement per-key CAS, and silently applying CAS to only
			// one key would be a correctness hole. Fail loudly so a future
			// caller that assumes multi-key works gets a clear signal.
			ctx := context.Background()
			info := newTestSession("sess-cas-multikey")
			Expect(sessionStore.Set(ctx, info.ID, info)).To(Succeed())

			err := sessionStore.UpdateCursors(ctx, info.ID, map[string]ulid.ULID{
				"location:room-a": core.NewULID(),
				"location:room-b": core.NewULID(),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("multi-key cursor updates are not supported"))
		})
	})

	Describe("UpdateFocusMemberships", func() {
		It("adds a membership and sets presenting focus", func() {
			ctx := context.Background()
			info := newTestSession("sess-ufm-add")
			Expect(sessionStore.Set(ctx, info.ID, info)).To(Succeed())

			targetID := ulid.Make()
			mutator := session.NewFocusMutator(func(
				current []session.FocusMembership,
				presenting *session.FocusKey,
			) ([]session.FocusMembership, *session.FocusKey, error) {
				Expect(current).To(BeEmpty())
				Expect(presenting).To(BeNil())
				m := session.FocusMembership{
					Kind:     session.FocusKindScene,
					TargetID: targetID,
					JoinedAt: time.Now().UTC().Truncate(time.Microsecond),
				}
				key := &session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}
				return []session.FocusMembership{m}, key, nil
			})

			Expect(sessionStore.UpdateFocusMemberships(ctx, info.ID, mutator)).To(Succeed())

			got, err := sessionStore.Get(ctx, info.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.FocusMemberships).To(HaveLen(1))
			Expect(got.FocusMemberships[0].Kind).To(Equal(session.FocusKindScene))
			Expect(got.FocusMemberships[0].TargetID).To(Equal(targetID))
			Expect(got.PresentingFocus).NotTo(BeNil())
			Expect(got.PresentingFocus.TargetID).To(Equal(targetID))
		})

		It("returns SESSION_NOT_FOUND for missing session", func() {
			ctx := context.Background()
			mutator := session.NewFocusMutator(func(
				current []session.FocusMembership,
				presenting *session.FocusKey,
			) ([]session.FocusMembership, *session.FocusKey, error) {
				return current, presenting, nil
			})

			err := sessionStore.UpdateFocusMemberships(ctx, "nonexistent", mutator)
			Expect(err).To(HaveOccurred())
		})

		It("rejects mutation on expired session", func() {
			ctx := context.Background()
			info := newTestSession("sess-ufm-expired")
			info.Status = session.StatusExpired
			Expect(sessionStore.Set(ctx, info.ID, info)).To(Succeed())

			mutator := session.NewFocusMutator(func(
				current []session.FocusMembership,
				presenting *session.FocusKey,
			) ([]session.FocusMembership, *session.FocusKey, error) {
				return current, presenting, nil
			})

			err := sessionStore.UpdateFocusMemberships(ctx, info.ID, mutator)
			Expect(err).To(HaveOccurred())
		})

		It("rolls back on mutator error", func() {
			ctx := context.Background()
			info := newTestSession("sess-ufm-rollback")
			Expect(sessionStore.Set(ctx, info.ID, info)).To(Succeed())

			mutator := session.NewFocusMutator(func(
				current []session.FocusMembership,
				presenting *session.FocusKey,
			) ([]session.FocusMembership, *session.FocusKey, error) {
				return nil, nil, fmt.Errorf("intentional error")
			})

			err := sessionStore.UpdateFocusMemberships(ctx, info.ID, mutator)
			Expect(err).To(HaveOccurred())

			got, gErr := sessionStore.Get(ctx, info.ID)
			Expect(gErr).NotTo(HaveOccurred())
			Expect(got.FocusMemberships).To(BeEmpty())
			Expect(got.PresentingFocus).To(BeNil())
		})

		It("round-trips JSONB serialization for FocusMemberships and is findable via ListByFocus", func() {
			// Extra assertion in this test covers the ListByFocus JSONB
			// containment query; keeps serialization + lookup paired so a
			// future change to FocusMembership's JSON shape trips this
			// and the ListByFocus-specific tests below together.
			ctx := context.Background()
			info := newTestSession("sess-ufm-listable")
			Expect(sessionStore.Set(ctx, info.ID, info)).To(Succeed())

			target := ulid.Make()
			mutator := session.NewFocusMutator(func(
				_ []session.FocusMembership,
				_ *session.FocusKey,
			) ([]session.FocusMembership, *session.FocusKey, error) {
				return []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: target, JoinedAt: time.Now().UTC()},
				}, nil, nil
			})
			Expect(sessionStore.UpdateFocusMemberships(ctx, info.ID, mutator)).To(Succeed())

			results, err := sessionStore.ListByFocus(ctx, session.FocusKey{
				Kind: session.FocusKindScene, TargetID: target,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].ID).To(Equal(info.ID))
		})

		It("round-trips JSONB serialization for FocusMemberships", func() {
			ctx := context.Background()
			info := newTestSession("sess-ufm-jsonb-rt")
			Expect(sessionStore.Set(ctx, info.ID, info)).To(Succeed())

			target1 := ulid.Make()
			target2 := ulid.Make()
			now := time.Now().UTC().Truncate(time.Microsecond)

			mutator := session.NewFocusMutator(func(
				current []session.FocusMembership,
				presenting *session.FocusKey,
			) ([]session.FocusMembership, *session.FocusKey, error) {
				memberships := []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: target1, JoinedAt: now},
					{Kind: session.FocusKindScene, TargetID: target2, JoinedAt: now.Add(time.Second)},
				}
				key := &session.FocusKey{Kind: session.FocusKindScene, TargetID: target1}
				return memberships, key, nil
			})

			Expect(sessionStore.UpdateFocusMemberships(ctx, info.ID, mutator)).To(Succeed())

			got, err := sessionStore.Get(ctx, info.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.FocusMemberships).To(HaveLen(2))
			Expect(got.FocusMemberships[0].TargetID).To(Equal(target1))
			Expect(got.FocusMemberships[1].TargetID).To(Equal(target2))
			Expect(got.FocusMemberships[0].JoinedAt).To(BeTemporally("~", now, time.Millisecond))
			Expect(got.PresentingFocus).NotTo(BeNil())
			Expect(got.PresentingFocus.TargetID).To(Equal(target1))
		})
	})

	Describe("ListByFocus", func() {
		// setMembership is a helper to give a session a specific FocusMembership set.
		setMembership := func(ctx context.Context, info *session.Info, memberships []session.FocusMembership) {
			Expect(sessionStore.Set(ctx, info.ID, info)).To(Succeed())
			mutator := session.NewFocusMutator(func(
				_ []session.FocusMembership,
				_ *session.FocusKey,
			) ([]session.FocusMembership, *session.FocusKey, error) {
				return memberships, nil, nil
			})
			Expect(sessionStore.UpdateFocusMemberships(ctx, info.ID, mutator)).To(Succeed())
		}

		It("returns non-expired sessions whose focus_memberships include the target", func() {
			ctx := context.Background()
			target := ulid.Make()
			otherTarget := ulid.Make()
			now := time.Now().UTC()

			active := newTestSession("lbf-active")
			setMembership(ctx, active, []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: target, JoinedAt: now},
			})

			detachedMulti := newTestSession("lbf-detached")
			detachedMulti.Status = session.StatusDetached
			setMembership(ctx, detachedMulti, []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: otherTarget, JoinedAt: now},
				{Kind: session.FocusKindScene, TargetID: target, JoinedAt: now},
			})

			nonMatch := newTestSession("lbf-nomatch")
			setMembership(ctx, nonMatch, []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: otherTarget, JoinedAt: now},
			})

			empty := newTestSession("lbf-empty")
			Expect(sessionStore.Set(ctx, empty.ID, empty)).To(Succeed())

			results, err := sessionStore.ListByFocus(ctx, session.FocusKey{
				Kind: session.FocusKindScene, TargetID: target,
			})
			Expect(err).NotTo(HaveOccurred())
			ids := []string{}
			for _, r := range results {
				ids = append(ids, r.ID)
			}
			Expect(ids).To(ConsistOf("lbf-active", "lbf-detached"))
		})

		It("excludes expired sessions even when membership matches", func() {
			ctx := context.Background()
			target := ulid.Make()
			now := time.Now().UTC()

			expired := newTestSession("lbf-expired")
			// Must be active during Set (expired sessions reject membership
			// writes), then flip to expired afterward.
			setMembership(ctx, expired, []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: target, JoinedAt: now},
			})
			expiresAt := now.Add(-time.Hour)
			Expect(sessionStore.UpdateStatus(ctx, expired.ID, session.StatusExpired, nil, &expiresAt)).To(Succeed())

			results, err := sessionStore.ListByFocus(ctx, session.FocusKey{
				Kind: session.FocusKindScene, TargetID: target,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})

		It("returns empty slice when no session holds the target", func() {
			ctx := context.Background()
			target := ulid.Make()

			results, err := sessionStore.ListByFocus(ctx, session.FocusKey{
				Kind: session.FocusKindScene, TargetID: target,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})
	})
})
