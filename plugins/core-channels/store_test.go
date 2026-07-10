// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/test/testutil"
)

// newTestChannelStore opens a channelStore against a fresh raw database on the
// shared Postgres container. Plugin migrations are applied internally by
// NewChannelStore. Uses RawDatabase (not FreshDatabase) so the plugin owns its
// schema without colliding with the core baseline migrations — mirrors the
// core-scenes inline-pool pattern (each store test composes its own database).
func newTestChannelStore() *channelStore {
	GinkgoHelper()
	setupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	DeferCleanup(cancel)

	connStr := testutil.RawDatabase(suiteT, sharedPG)
	store, err := NewChannelStore(setupCtx, connStr)
	Expect(err).NotTo(HaveOccurred(), "failed to open channel store")
	DeferCleanup(store.Close)
	return store
}

// expectCode asserts err is an oops error carrying the given top-level code.
func expectCode(err error, code string) {
	GinkgoHelper()
	Expect(err).To(HaveOccurred())
	o, ok := oops.AsOops(err)
	Expect(ok).To(BeTrue(), "expected an oops error, got %v", err)
	Expect(o.Code()).To(Equal(code))
}

// countOpsEvents returns the number of channel_ops_events rows for channelID of
// the given kind.
func countOpsEvents(store *channelStore, channelID string, kind channelOpsEventKind) int {
	GinkgoHelper()
	var n int
	err := store.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM channel_ops_events WHERE channel_id = $1 AND kind = $2`,
		channelID, string(kind)).Scan(&n)
	Expect(err).NotTo(HaveOccurred())
	return n
}

// countRows returns the total row count of the named table.
func countRows(store *channelStore, table string) int {
	GinkgoHelper()
	var n int
	err := store.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n)
	Expect(err).NotTo(HaveOccurred())
	return n
}

var _ = Describe("channelStore CRUD + membership", func() {
	var (
		ctx   context.Context
		store *channelStore
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = newTestChannelStore()
	})

	Describe("CreateChannel", func() {
		It("persists a channel row and an owner membership", func() {
			row := &channelRow{Name: "Public", Type: string(channelTypePublic), OwnerID: "char-owner"}
			Expect(store.CreateChannel(ctx, row)).To(Succeed())
			Expect(row.ID).NotTo(BeEmpty())

			got, err := store.GetByID(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Name).To(Equal("Public"))
			Expect(got.OwnerID).To(Equal("char-owner"))
			Expect(got.Archived).To(BeFalse())

			_, members, _, _, err := store.GetWithMembership(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(members).To(ConsistOf("char-owner"))
			Expect(countOpsEvents(store, row.ID, opsKindLifecycleCreated)).To(Equal(1))
		})

		It("rejects a case-different same name (unique lower(name))", func() {
			Expect(store.CreateChannel(ctx, &channelRow{Name: "Public", OwnerID: "o1"})).To(Succeed())
			err := store.CreateChannel(ctx, &channelRow{Name: "public", OwnerID: "o2"})
			expectCode(err, "CHANNEL_NAME_TAKEN")
		})

		It("rejects an invalid channel name at the store boundary", func() {
			err := store.CreateChannel(ctx, &channelRow{Name: "bad name!", OwnerID: "o1"})
			expectCode(err, "CHANNEL_NAME_INVALID")
		})
	})

	Describe("membership", func() {
		var channelID string

		BeforeEach(func() {
			row := &channelRow{Name: "Ooc", OwnerID: "char-owner"}
			Expect(store.CreateChannel(ctx, row)).To(Succeed())
			channelID = row.ID
		})

		It("JoinChannel is idempotent — a double join makes one membership row", func() {
			Expect(store.JoinChannel(ctx, channelID, "char-a")).To(Succeed())
			Expect(store.JoinChannel(ctx, channelID, "char-a")).To(Succeed())

			_, members, _, _, err := store.GetWithMembership(ctx, channelID)
			Expect(err).NotTo(HaveOccurred())
			Expect(members).To(ConsistOf("char-owner", "char-a"))
			Expect(countOpsEvents(store, channelID, opsKindMembershipJoin)).To(Equal(1))
		})

		It("LeaveChannel removes the membership", func() {
			Expect(store.JoinChannel(ctx, channelID, "char-a")).To(Succeed())
			Expect(store.LeaveChannel(ctx, channelID, "char-a")).To(Succeed())

			_, members, _, _, err := store.GetWithMembership(ctx, channelID)
			Expect(err).NotTo(HaveOccurred())
			Expect(members).To(ConsistOf("char-owner"))
			Expect(countOpsEvents(store, channelID, opsKindMembershipLeave)).To(Equal(1))
		})

		It("forbids the owner from leaving", func() {
			expectCode(store.LeaveChannel(ctx, channelID, "char-owner"), "CHANNEL_OWNER_CANNOT_LEAVE")
		})

		It("returns not-found when leaving a channel the character is not in", func() {
			expectCode(store.LeaveChannel(ctx, channelID, "stranger"), "CHANNEL_MEMBERSHIP_NOT_FOUND")
		})

		It("prevents a banned character from rejoining", func() {
			Expect(store.JoinChannel(ctx, channelID, "char-b")).To(Succeed())
			Expect(store.SetBanned(ctx, channelID, "char-b", true)).To(Succeed())
			expectCode(store.JoinChannel(ctx, channelID, "char-b"), "CHANNEL_BANNED")
			Expect(countOpsEvents(store, channelID, opsKindModerationBan)).To(Equal(1))
		})

		It("returns not-found when joining a channel that does not exist", func() {
			expectCode(store.JoinChannel(ctx, "no-such-channel", "char-a"), "CHANNEL_NOT_FOUND")
		})

		It("SetMuted records a moderation.mute ops event", func() {
			Expect(store.JoinChannel(ctx, channelID, "char-c")).To(Succeed())
			Expect(store.SetMuted(ctx, channelID, "char-c", true)).To(Succeed())
			_, _, _, muted, err := store.GetWithMembership(ctx, channelID)
			Expect(err).NotTo(HaveOccurred())
			Expect(muted).To(ConsistOf("char-c"))
			Expect(countOpsEvents(store, channelID, opsKindModerationMute)).To(Equal(1))
		})

		It("SetMuted on a non-member returns not-found", func() {
			expectCode(store.SetMuted(ctx, channelID, "ghost", true), "CHANNEL_MEMBERSHIP_NOT_FOUND")
		})
	})

	Describe("ListForCharacter", func() {
		It("returns exactly the channels the character is a member of", func() {
			c1 := &channelRow{Name: "Alpha", OwnerID: "owner"}
			c2 := &channelRow{Name: "Beta", OwnerID: "owner"}
			Expect(store.CreateChannel(ctx, c1)).To(Succeed())
			Expect(store.CreateChannel(ctx, c2)).To(Succeed())
			Expect(store.JoinChannel(ctx, c1.ID, "char-x")).To(Succeed())

			list, err := store.ListForCharacter(ctx, "char-x")
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(1))
			Expect(list[0].Name).To(Equal("Alpha"))
		})

		It("returns an empty slice for a non-member", func() {
			list, err := store.ListForCharacter(ctx, "nobody")
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(BeEmpty())
		})
	})

	Describe("name lookup", func() {
		It("GetByName is case-insensitive", func() {
			row := &channelRow{Name: "Staff", Type: string(channelTypeAdmin), OwnerID: "owner"}
			Expect(store.CreateChannel(ctx, row)).To(Succeed())

			got, err := store.GetByName(ctx, "sTaFf")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(row.ID))
			Expect(got.Type).To(Equal(string(channelTypeAdmin)))
		})

		It("returns a typed NOT_FOUND for an absent id", func() {
			_, err := store.GetByID(ctx, "missing")
			expectCode(err, "CHANNEL_NOT_FOUND")
		})

		It("returns a typed NOT_FOUND for an absent name", func() {
			_, err := store.GetByName(ctx, "missing")
			expectCode(err, "CHANNEL_NOT_FOUND")
		})
	})

	Describe("default-channel seeding", func() {
		It("seeds Public with is_default=true and no membership rows", func() {
			Expect(store.SeedDefaultChannels(ctx, defaultChannels)).To(Succeed())

			got, err := store.GetByName(ctx, "Public")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.IsDefault).To(BeTrue())
			Expect(got.Type).To(Equal(string(channelTypePublic)))
			Expect(got.OwnerID).To(Equal(systemOwnerID))
			Expect(countRows(store, "channel_memberships")).To(Equal(0), "seeding MUST create no membership rows")
		})

		It("is idempotent — a second seed adds zero rows", func() {
			Expect(store.SeedDefaultChannels(ctx, defaultChannels)).To(Succeed())
			before := countRows(store, "channels")
			Expect(store.SeedDefaultChannels(ctx, defaultChannels)).To(Succeed())
			Expect(countRows(store, "channels")).To(Equal(before), "second seed MUST NOT duplicate a default")
		})

		It("ListDefaultChannels returns the seeded set", func() {
			Expect(store.SeedDefaultChannels(ctx, defaultChannels)).To(Succeed())
			defaults, err := store.ListDefaultChannels(ctx)
			Expect(err).NotTo(HaveOccurred())
			names := make([]string, 0, len(defaults))
			for _, d := range defaults {
				names = append(names, d.Name)
			}
			Expect(names).To(ContainElement("Public"))
			Expect(defaults).To(HaveLen(len(defaultChannels)))
		})

		It("ListDefaultChannels is empty before any seed", func() {
			defaults, err := store.ListDefaultChannels(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(defaults).To(BeEmpty())
		})

		It("a collision with a pre-existing user channel is a no-op (existing row wins)", func() {
			Expect(store.CreateChannel(ctx, &channelRow{Name: "Public", OwnerID: "real-owner"})).To(Succeed())
			Expect(store.SeedDefaultChannels(ctx, defaultChannels)).To(Succeed())

			got, err := store.GetByName(ctx, "public")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.OwnerID).To(Equal("real-owner"), "user channel MUST win the collision")
			Expect(got.IsDefault).To(BeFalse())

			defaults, err := store.ListDefaultChannels(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(defaults).To(BeEmpty(), "the pre-existing user channel is not a default")
		})
	})

	Describe("DeleteChannel (soft archive)", func() {
		It("sets archived=true and leaves the row present — never a hard delete", func() {
			row := &channelRow{Name: "Doomed", OwnerID: "owner"}
			Expect(store.CreateChannel(ctx, row)).To(Succeed())

			Expect(store.DeleteChannel(ctx, row.ID, "owner")).To(Succeed())

			got, err := store.GetByID(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred(), "row MUST still exist after soft archive")
			Expect(got.Archived).To(BeTrue())
			Expect(countOpsEvents(store, row.ID, opsKindLifecycleArchived)).To(Equal(1))
		})

		It("returns not-found for an absent channel", func() {
			expectCode(store.DeleteChannel(ctx, "missing", "owner"), "CHANNEL_NOT_FOUND")
		})
	})
})
