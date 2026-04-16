// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package stream_history_test

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	accessTypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// newTestSession returns a session.Info pre-populated with sensible defaults
// for QueryStreamHistory integration tests. Callers override specific fields
// (e.g., FocusMemberships) before calling sessionStore.Set.
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

// appendEventToStream writes a say event to the given stream via the shared
// event store and returns the event ID. Events are read back in ascending
// ULID order, so successive calls produce monotonically increasing IDs
// thanks to core.NewULID's monotonic guarantee.
func appendEventToStream(ctx context.Context, stream string) ulid.ULID {
	e := core.NewEvent(stream, core.EventTypeSay, core.Actor{
		Kind: core.ActorCharacter,
		ID:   "test-actor",
	}, []byte(`{"message":"test"}`))
	Expect(env.eventStore.Append(ctx, e)).To(Succeed())
	return e.ID
}

// buildHistoryRequest creates a QueryStreamHistoryRequest with a deterministic
// RequestId for response-meta echo assertions.
func buildHistoryRequest(sessionID, stream string, count int32) *corev1.QueryStreamHistoryRequest {
	return &corev1.QueryStreamHistoryRequest{
		Meta:      &corev1.RequestMeta{RequestId: "req-" + sessionID, Timestamp: timestamppb.Now()},
		SessionId: sessionID,
		Stream:    stream,
		Count:     count,
	}
}

// newCoreServerForTest constructs a real CoreServer wired to the suite's
// PostgresEventStore and PostgresSessionStore, with a real (but minimal)
// command dispatcher + services. The ABAC engine is caller-chosen so
// individual specs can toggle permit/deny behaviour without reconstructing
// the rest of the stack.
//
// The dispatcher is required by NewCoreServer but is never invoked by
// QueryStreamHistory, so we register an empty command registry. We use
// policytest.AllowAllEngine for the dispatcher's own authorization because
// it is unrelated to stream-read authorization.
func newCoreServerForTest(engine accessTypes.AccessPolicyEngine) *grpcpkg.CoreServer {
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
		grpcpkg.WithAccessEngine(engine),
	)
}

var _ = Describe("CoreService.QueryStreamHistory", func() {
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

	Describe("QueryStreamHistoryReturnsEventsForSubscribedStream", func() {
		It("returns events in ascending order with has_more reflecting pagination state", func() {
			// Arrange: real session for a character at a known location,
			// with an ABAC engine that grants read on the location stream.
			charID := ulid.Make()
			locID := ulid.Make()
			sessionID := core.NewULID().String()
			stream := world.LocationStream(locID)

			grant := policytest.NewGrantEngine()
			grant.Grant("character:"+charID.String(), accessTypes.ActionRead, "stream:"+stream)
			server := newCoreServerForTest(grant)

			Expect(env.sessionStore.Set(testCtx, sessionID, newTestSession(sessionID, charID, locID))).To(Succeed())

			// Append three events to the location stream.
			ids := []ulid.ULID{
				appendEventToStream(testCtx, stream),
				appendEventToStream(testCtx, stream),
				appendEventToStream(testCtx, stream),
			}

			// Act: request with count=10, larger than the 3 available events.
			req := buildHistoryRequest(sessionID, stream, 10)
			req.Meta.RequestId = "echo-request-42"
			resp, err := server.QueryStreamHistory(testCtx, req)

			// Assert: response contains all 3 events, ascending order, no more pages.
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Events).To(HaveLen(3))
			Expect(resp.HasMore).To(BeFalse())
			for i, id := range ids {
				Expect(resp.Events[i].Id).To(Equal(id.String()),
					"events must be returned in ascending order; index %d", i)
				Expect(resp.Events[i].Stream).To(Equal(stream))
				Expect(resp.Events[i].Type).To(Equal(string(core.EventTypeSay)))
			}
			// RequestId is echoed back via response meta.
			Expect(resp.Meta).NotTo(BeNil())
			Expect(resp.Meta.RequestId).To(Equal("echo-request-42"))
		})
	})

	Describe("QueryStreamHistoryDeniesNonMemberSceneStream", func() {
		It("returns STREAM_ACCESS_DENIED when the session has no focus membership for the scene (I-17)", func() {
			// Arrange: session has NO FocusMembership for the scene.
			// The ABAC engine is permissive — the test proves I-17 short-circuits BEFORE ABAC.
			charID := ulid.Make()
			locID := ulid.Make()
			sessionID := core.NewULID().String()
			foreignScene := ulid.Make()
			stream := "scene:" + foreignScene.String() + ":ic"

			server := newCoreServerForTest(policytest.AllowAllEngine())

			// Session with no FocusMemberships at all.
			info := newTestSession(sessionID, charID, locID)
			info.FocusMemberships = nil
			Expect(env.sessionStore.Set(testCtx, sessionID, info)).To(Succeed())

			// Append an event to the scene stream — its existence MUST NOT
			// matter: membership denial is pre-query.
			appendEventToStream(testCtx, stream)

			// Act.
			_, err := server.QueryStreamHistory(testCtx, buildHistoryRequest(sessionID, stream, 10))

			// Assert: STREAM_ACCESS_DENIED code on the oops error.
			Expect(err).To(HaveOccurred())
			oopsErr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue(), "expected oops error, got %T", err)
			Expect(oopsErr.Code()).To(Equal("STREAM_ACCESS_DENIED"))
		})
	})

	Describe("QueryStreamHistoryPaginatesCorrectly", func() {
		It("walks backward through pages via before_id with non-overlapping results", func() {
			// Arrange: 7 events on a location stream, page size 3 → 3 pages (3, 3, 1).
			charID := ulid.Make()
			locID := ulid.Make()
			sessionID := core.NewULID().String()
			stream := world.LocationStream(locID)

			grant := policytest.NewGrantEngine()
			grant.Grant("character:"+charID.String(), accessTypes.ActionRead, "stream:"+stream)
			server := newCoreServerForTest(grant)

			Expect(env.sessionStore.Set(testCtx, sessionID, newTestSession(sessionID, charID, locID))).To(Succeed())

			ids := make([]ulid.ULID, 0, 7)
			for range 7 {
				ids = append(ids, appendEventToStream(testCtx, stream))
			}

			// Page 1: newest 3 → ids[4..6], has_more=true.
			page1, err := server.QueryStreamHistory(testCtx, buildHistoryRequest(sessionID, stream, 3))
			Expect(err).NotTo(HaveOccurred())
			Expect(page1.Events).To(HaveLen(3))
			Expect(page1.HasMore).To(BeTrue())
			Expect(page1.Events[0].Id).To(Equal(ids[4].String()))
			Expect(page1.Events[1].Id).To(Equal(ids[5].String()))
			Expect(page1.Events[2].Id).To(Equal(ids[6].String()))

			// Page 2: before_id = oldest of page 1 → ids[1..3], has_more=true.
			req2 := buildHistoryRequest(sessionID, stream, 3)
			req2.BeforeId = page1.Events[0].Id
			page2, err := server.QueryStreamHistory(testCtx, req2)
			Expect(err).NotTo(HaveOccurred())
			Expect(page2.Events).To(HaveLen(3))
			Expect(page2.HasMore).To(BeTrue())
			Expect(page2.Events[0].Id).To(Equal(ids[1].String()))
			Expect(page2.Events[1].Id).To(Equal(ids[2].String()))
			Expect(page2.Events[2].Id).To(Equal(ids[3].String()))

			// Page 3: before_id = oldest of page 2 → ids[0], has_more=false.
			req3 := buildHistoryRequest(sessionID, stream, 3)
			req3.BeforeId = page2.Events[0].Id
			page3, err := server.QueryStreamHistory(testCtx, req3)
			Expect(err).NotTo(HaveOccurred())
			Expect(page3.Events).To(HaveLen(1))
			Expect(page3.HasMore).To(BeFalse())
			Expect(page3.Events[0].Id).To(Equal(ids[0].String()))

			// Regression: no event ID appears in more than one page. Any
			// overlap would indicate a bug where beforeID is inclusive or
			// the SQL filter is off by one.
			seen := make(map[string]int, 7)
			for _, pg := range [][]*corev1.EventFrame{page1.Events, page2.Events, page3.Events} {
				for _, ev := range pg {
					seen[ev.Id]++
				}
			}
			for id, count := range seen {
				Expect(count).To(Equal(1), "event %s appeared in more than one page", id)
			}
			Expect(seen).To(HaveLen(7), "all 7 events should appear exactly once across the 3 pages")
		})
	})

	Describe("QueryStreamHistoryAdminReadsPublicStream", func() {
		It("returns events from a non-co-located location stream when ABAC permits (admin override)", func() {
			// The spec's admin-override case reduces to: the ABAC engine
			// returns permit even for a non-co-located public stream. We
			// model the admin seed policy ("admin" in roles → permit any)
			// with policytest.AllowAllEngine, which is the permit-always
			// engine — the same effect an admin decision produces on a
			// public stream. This keeps the test focused on QueryStreamHistory's
			// contract (ABAC-permits → events returned for public streams)
			// without dragging in the full attribute-provider wiring, which
			// is tested exhaustively in seed_smoke_test.go.
			charID := ulid.Make()
			locA := ulid.Make()
			locB := ulid.Make() // character is NOT at locB
			sessionID := core.NewULID().String()

			server := newCoreServerForTest(policytest.AllowAllEngine())

			info := newTestSession(sessionID, charID, locA)
			Expect(env.sessionStore.Set(testCtx, sessionID, info)).To(Succeed())

			// Event on a location the character is NOT co-located with —
			// the default player-location-stream-read policy would DENY,
			// but admin-full-access (modeled by AllowAllEngine) PERMITS.
			streamB := world.LocationStream(locB)
			id := appendEventToStream(testCtx, streamB)

			// Act.
			resp, err := server.QueryStreamHistory(testCtx, buildHistoryRequest(sessionID, streamB, 10))

			// Assert: admin read succeeds despite non-co-location.
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Events).To(HaveLen(1))
			Expect(resp.Events[0].Id).To(Equal(id.String()))
			Expect(resp.HasMore).To(BeFalse())
		})
	})
})
