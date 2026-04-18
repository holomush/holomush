// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package list_session_streams_test

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// newTestSession returns a session.Info pre-populated with sensible defaults
// for ListSessionStreams integration tests. The RPC does not consult focus
// memberships or cursors, so defaults are minimal.
func newTestSession(id string, charID, locID ulid.ULID) *session.Info {
	now := time.Now().UTC()
	return &session.Info{
		ID:             id,
		CharacterID:    charID,
		CharacterName:  "Alice",
		LocationID:     locID,
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

// newCoreServerForTest constructs a real CoreServer wired to the suite's
// PostgresEventStore and PostgresSessionStore. ListSessionStreams only needs
// the session store + focusCoordinator (nil here, so the fallback path runs).
//
// The dispatcher is required by NewCoreServer but is never invoked by
// ListSessionStreams, so we register an empty command registry.
func newCoreServerForTest() *grpcpkg.CoreServer {
	coreEngine := core.NewEngine(env.eventStore)

	reg := command.NewRegistry()
	cmdSvc := command.NewTestServices(command.ServicesConfig{
		Engine:  policytest.AllowAllEngine(),
		Session: env.sessionStore,
		Events:  env.eventStore,
	})
	dispatcher, err := command.NewDispatcher(reg, policytest.AllowAllEngine())
	Expect(err).NotTo(HaveOccurred())

	return grpcpkg.NewCoreServer(
		coreEngine,
		env.sessionStore,
		dispatcher,
		cmdSvc,
		grpcpkg.WithEventStore(env.eventStore),
		grpcpkg.WithAccessEngine(policytest.AllowAllEngine()),
	)
}

var _ = Describe("CoreService.ListSessionStreams", func() {
	var (
		testCtx    context.Context
		testCancel context.CancelFunc
	)

	BeforeEach(func() {
		testCtx, testCancel = context.WithTimeout(context.Background(), 30*time.Second)
		cleanupTestData(testCtx, env.pool)
	})

	AfterEach(func() {
		if testCancel != nil {
			testCancel()
		}
	})

	It("returns character and location streams for a fresh session via the fallback path", func() {
		// Arrange: real session persisted to Postgres; focusCoordinator is nil
		// on this server, so the handler runs the Subscribe-parity fallback
		// that emits character + location streams.
		charID := ulid.Make()
		locID := ulid.Make()
		sessionID := core.NewULID().String()

		server := newCoreServerForTest()
		Expect(env.sessionStore.Set(testCtx, sessionID, newTestSession(sessionID, charID, locID))).To(Succeed())

		// Act.
		resp, err := server.ListSessionStreams(testCtx, &corev1.ListSessionStreamsRequest{
			SessionId: sessionID,
		})

		// Assert: both ambient streams present, encoded with the canonical prefixes.
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())
		Expect(resp.GetStreams()).To(ContainElement(world.CharacterStream(charID)))
		Expect(resp.GetStreams()).To(ContainElement(world.LocationStream(locID)))
		// Sanity-check the exact prefix format callers rely on.
		Expect(resp.GetStreams()).To(ContainElement("character:" + charID.String()))
		Expect(resp.GetStreams()).To(ContainElement("location:" + locID.String()))
	})

	It("rejects a request with empty session_id", func() {
		server := newCoreServerForTest()

		_, err := server.ListSessionStreams(testCtx, &corev1.ListSessionStreamsRequest{})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("session_id is required"))
	})
})
