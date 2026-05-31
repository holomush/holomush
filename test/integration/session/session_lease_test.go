// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package session_test

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
)

// Connection lease (I-LIVE-2 / I-SEC-1). Each spec provisions its own fresh
// database via sessiontest.NewStoreWithPool — ListLapsedConnections scans the
// whole table, so these specs need isolation from the shared suite database.
var _ = Describe("Connection lease", func() {
	It("RefreshConnection bumps last_seen_at so ListLapsedConnections excludes it", func() {
		ctx := context.Background()
		store, pool := sessiontest.NewStoreWithPool(suiteT)

		ps := sessiontest.NewPlayerSession()
		sessiontest.SeedPlayerSession(suiteT, pool, ps)
		sess := sessiontest.NewActiveSession(ps)
		Expect(store.Set(ctx, sess.ID, sess)).To(Succeed())

		connID := ulid.Make()
		Expect(store.AddConnection(ctx, &session.Connection{
			ID: connID, SessionID: sess.ID, ClientType: "terminal",
			ConnectedAt: time.Now().Add(-time.Hour), // stale connect time
		})).To(Succeed())

		// AddConnection stamps last_seen_at = connected_at (stale here), so the
		// connection is initially lapsed relative to a 45s TTL.
		lapsed, err := store.ListLapsedConnections(ctx, time.Now().Add(-45*time.Second))
		Expect(err).NotTo(HaveOccurred())
		Expect(lapsed).To(HaveLen(1), "stale-connect connection is lapsed before refresh")
		// Assert the projected fields so a column-order regression in the scan path is caught.
		Expect(lapsed[0].ID).To(Equal(connID))
		Expect(lapsed[0].SessionID).To(Equal(sess.ID))
		Expect(lapsed[0].ClientType).To(Equal("terminal"))

		// Refresh bumps last_seen_at to now.
		Expect(store.RefreshConnection(ctx, connID, sess.ID)).To(Succeed())

		lapsed, err = store.ListLapsedConnections(ctx, time.Now().Add(-45*time.Second))
		Expect(err).NotTo(HaveOccurred())
		Expect(lapsed).To(BeEmpty(), "refreshed connection is no longer lapsed")
	})

	It("RefreshConnection returns CONNECTION_NOT_FOUND for a missing connection", func() {
		ctx := context.Background()
		store, _ := sessiontest.NewStoreWithPool(suiteT)

		err := store.RefreshConnection(ctx, ulid.Make(), "nonexistent-session")
		Expect(err).To(HaveOccurred())
		o, ok := oops.AsOops(err)
		Expect(ok).To(BeTrue())
		Expect(o.Code()).To(Equal("CONNECTION_NOT_FOUND"))
	})

	// I-SEC-1: a connection can only be refreshed by its owning session. Pairing
	// a valid connection ULID with a different session_id matches zero rows and
	// returns CONNECTION_NOT_FOUND — indistinguishable from an absent connection
	// — so a caller cannot keep another session's connection alive.
	It("RefreshConnection rejects a refresh from a non-owning session", func() {
		ctx := context.Background()
		store, pool := sessiontest.NewStoreWithPool(suiteT)

		ps := sessiontest.NewPlayerSession()
		sessiontest.SeedPlayerSession(suiteT, pool, ps)
		sess := sessiontest.NewActiveSession(ps)
		Expect(store.Set(ctx, sess.ID, sess)).To(Succeed())

		connID := ulid.Make()
		Expect(store.AddConnection(ctx, &session.Connection{
			ID: connID, SessionID: sess.ID, ClientType: "terminal",
		})).To(Succeed())

		// A non-owning session_id must NOT refresh this connection.
		err := store.RefreshConnection(ctx, connID, "foreign-session-"+ulid.Make().String())
		Expect(err).To(HaveOccurred(), "a non-owning session must not refresh another session's connection")
		o, ok := oops.AsOops(err)
		Expect(ok).To(BeTrue())
		Expect(o.Code()).To(Equal("CONNECTION_NOT_FOUND"))

		// The owning session refreshes it successfully.
		Expect(store.RefreshConnection(ctx, connID, sess.ID)).To(Succeed())
	})
})
