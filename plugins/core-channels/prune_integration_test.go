// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

var _ = Describe("channel retention prune sweep (D-07)", func() {
	var (
		ctx        context.Context
		store      *channelStore
		auditStore *ChannelAuditStore
		now        time.Time
	)

	// countLog returns the number of channel_log rows for the given subject.
	countLog := func(subject string) int {
		GinkgoHelper()
		var n int
		Expect(store.pool.QueryRow(ctx,
			`SELECT count(*) FROM channel_log WHERE subject = $1`, subject).Scan(&n)).To(Succeed())
		return n
	}

	// createChannel inserts a channel with the given type + optional retention
	// override and returns its id.
	createChannel := func(name, chanType string, retentionDays *int) string {
		GinkgoHelper()
		row := &channelRow{Name: name, Type: chanType, OwnerID: "char-owner", RetentionDays: retentionDays}
		Expect(store.CreateChannel(ctx, row)).To(Succeed())
		return row.ID
	}

	newPruner := func() *channelPruner {
		return &channelPruner{
			store:         store,
			gameID:        "main",
			defaultWindow: 720 * time.Hour,
			interval:      time.Hour,
			now:           func() time.Time { return now },
		}
	}

	BeforeEach(func() {
		ctx = context.Background()
		store = newTestChannelStore()
		auditStore = NewChannelAuditStore(store.Pool())
		now = time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	})

	It("deletes rows older than the default window and preserves in-window rows", func() {
		id := createChannel("Public", "public", nil)
		subject := dotStyleChannelSubject("main", id)
		insertLogRow(auditStore, subject, now.Add(-1000*time.Hour))        // outside 720h → deleted
		edge := insertLogRow(auditStore, subject, now.Add(-720*time.Hour)) // exactly at edge → preserved (strict <)
		_ = edge
		insertLogRow(auditStore, subject, now.Add(-1*time.Hour)) // inside → preserved
		Expect(countLog(subject)).To(Equal(3))

		Expect(newPruner().sweep(ctx)).To(Succeed())

		Expect(countLog(subject)).To(Equal(2), "only the row older than the window is pruned; the edge row is preserved")
	})

	It("honors a per-channel retention_days override", func() {
		one := 1
		id := createChannel("Fast", "private", &one) // 1-day retention
		subject := dotStyleChannelSubject("main", id)
		insertLogRow(auditStore, subject, now.Add(-25*time.Hour)) // outside 24h → deleted
		insertLogRow(auditStore, subject, now.Add(-1*time.Hour))  // inside → preserved
		Expect(countLog(subject)).To(Equal(2))

		Expect(newPruner().sweep(ctx)).To(Succeed())

		Expect(countLog(subject)).To(Equal(1), "the per-channel 1-day window prunes the 25h-old row")
	})

	It("never prunes an admin channel configured for unlimited retention", func() {
		id := createChannel("Staff", "admin", nil) // admin + NULL retention = unlimited
		subject := dotStyleChannelSubject("main", id)
		insertLogRow(auditStore, subject, now.Add(-5000*time.Hour)) // ancient
		Expect(countLog(subject)).To(Equal(1))

		Expect(newPruner().sweep(ctx)).To(Succeed())

		Expect(countLog(subject)).To(Equal(1), "unlimited-retention admin history MUST survive the sweep")
	})
})
