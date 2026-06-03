// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	crand "crypto/rand"
	"errors"
	"io"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/history"
	"github.com/holomush/holomush/internal/session"
)

// INV-SCENE-9: Late-joining participants MUST see only IC events from
// `FocusMembership.JoinedAt` forward when reading via `QueryStreamHistory`.
// Pose-order computation remains scene-global; display via `GetPoseOrder` is
// unaffected by the caller's `joined_at` and is pinned separately by T27.
//
// # End-to-end shape pinned by this spec
//
// The chain that delivers INV-SCENE-9 in production is:
//
//  1. `CoreServer.QueryStreamHistory` (internal/grpc/query_stream_history.go)
//     loads the caller's `session.Info` and calls
//     `streamScopeFloor(info, req.Stream)` to compute the per-session temporal
//     floor. For a scene IC subject, the scene branch in
//     `internal/grpc/scope_floor.go` returns the matching
//     `FocusMembership.JoinedAt`.
//  2. The handler then takes `MAX(client-supplied NotBefore, scopeFloor)` and
//     forwards it as `HistoryQuery.NotBefore` to
//     `eventbus.HistoryReader.QueryHistory`.
//  3. The hot-tier reader (`internal/eventbus/history/hot_jetstream.go`)
//     filters every candidate event with
//     `if !q.NotBefore.IsZero() && ev.Timestamp.Before(q.NotBefore) { reject }`
//     — see hot_jetstream.go:353. `NotBefore` is inclusive (round4 test pins
//     boundary semantics).
//
// This spec exercises (2)+(3) directly against a real embedded JetStream bus,
// computing the `NotBefore` value the way the substrate would (i.e.,
// `FocusMembership.JoinedAt`). T13 already pinned (1) at the unit level via
// `scope_floor_test.go::TestStreamScopeFloor_SceneSubjects_INV_SCENE_9`; T26 pins
// (2)+(3) end-to-end on the post-migration code. The two together cover
// INV-SCENE-9 from the session-membership timestamp through to the JetStream
// candidate-event filter.
//
// # Why we don't build a CoreServer here
//
// The full `QueryStreamHistory` handler requires a session store, an identity
// registry, a binding manager, an access engine, and a history reader.
// Standing all of that up in an integration test adds heavyweight wiring
// without exercising any logic that isn't already covered by unit tests
// (T13's scope_floor pin) or this spec (the substrate filter end-to-end).
// The Reader.QueryHistory call path is the shared production code; pinning it
// here with a `NotBefore` value sourced from a real `session.Info` exercises
// the substrate's INV-SCENE-9 contract authentically.
//
// # Pre-Phase-4 regression context (spec §3.3)
//
// Pre-Phase-4, `scope_floor.go`'s scene branch matched the colon-style
// `scene:<id>:ic` prefix, but every production caller emitted NATS dot-style
// (`events.<gameID>.scene.<sceneID>.ic`). So `streamScopeFloor` silently fell
// through to the default branch and returned `time.Time{}` for every real
// scene subject — the temporal floor was wired up but never actually fired.
// Late joiners therefore saw the entire scene's history, in direct violation
// of INV-SCENE-9.
//
// T13 migrated the scope_floor scene branch to dot-style; this test pins the
// post-migration property end-to-end. The git diff between this file and the
// absence of it (combined with `scope_floor_test.go`'s diff between colon-
// and dot-style fixtures) is the auditable artifact for the bug-fix moment.
//
// Spec: docs/superpowers/specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md §3.3, INV-SCENE-9.
// ADRs: holomush-r4th (dot-style scene subjects), holomush-nt2d (I-17 plugin-code gate; layered above scope-floor for participation check).
// Bead: holomush-5rh.13.26.
var _ = Describe("INV-SCENE-9: late-joiner temporal floor", func() {
	It("history reader filters pre-join events for late participants", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		bus := freshBus()
		pub := bus.Bus.Publisher()
		Expect(pub).NotTo(BeNil())

		// Deterministic scene ULID — keeps subjects parse-clean and matches the
		// production dot-style shape (events.<gameID>.scene.<sceneID>.ic).
		sceneID := ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader)
		sceneICSubject := eventbus.Subject("events.main.scene." + sceneID.String() + ".ic")

		// Four well-separated timestamps in the recent past so the test cannot
		// be fooled by clock jitter or sub-millisecond rounding. We base them
		// off `time.Now()` rather than a fixed wall date because the hot tier's
		// forward read uses `DeliverByStartTimePolicy` with
		// `start = max(edge, NotBefore)`, and JetStream resolves that start
		// time against the message INGESTION (server wall) time — not against
		// the application's `event.Timestamp` field. If we pinned t0..t3 in
		// the future relative to wall-now, JS would refuse to deliver any
		// message because `OptStartTime` would be after every ingestion stamp.
		// Anchoring t0..t3 in the recent past sidesteps that without weakening
		// the assertions: the in-process `matchesQuery` filter (hot_jetstream.go:353)
		// still discriminates by `event.Timestamp`, which is the path
		// `streamScopeFloor` actually feeds.
		// Post-gfo6: pgnanos preserves ns end-to-end; no truncation needed.
		base := time.Now().UTC().Add(-5 * time.Hour)
		t0 := base
		t1 := base.Add(1 * time.Hour)
		t2 := base.Add(2 * time.Hour)
		t3 := base.Add(3 * time.Hour)

		// Alice — early participant. JoinedAt = t0; she should see every pose.
		// CharacterID values are placeholders so the session.Info is well-shaped;
		// they're not consumed by the assertions below (the test exercises the
		// substrate's NotBefore filter, not session-store routing).
		alice := &session.Info{
			CharacterID: ulid.Make(),
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: t0},
			},
		}

		// Bob — late joiner. JoinedAt = t2; he should see only t2 and t3 poses.
		bob := &session.Info{
			CharacterID: ulid.Make(),
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: t2},
			},
		}

		// Publish four pose events on the scene IC subject at staggered
		// timestamps. ULIDs are minted with `ulid.New(ulid.Timestamp(ts), ...)`
		// so the ULID's embedded time matches the Timestamp field; the
		// hot-tier filter keys off `ev.Timestamp` per hot_jetstream.go:353.
		poses := []eventbus.Event{
			mintPoseAt(sceneICSubject, t0, "alice waves hello"),
			mintPoseAt(sceneICSubject, t1, "alice sips her drink"),
			mintPoseAt(sceneICSubject, t2, "bob steps in from the hallway"),
			mintPoseAt(sceneICSubject, t3, "bob nods at alice"),
		}
		for i := range poses {
			Expect(pub.Publish(ctx, poses[i])).To(Succeed())
		}
		// Publish barrier — guarantee every event has been acked by the server
		// before we open a HistoryReader.
		bus.AwaitStreamLastSeq(suiteT, uint64(len(poses)), 5*time.Second)

		// Build a Reader against the real JS hot tier. No PG pool — every
		// event sits well inside JetStream retention so cold-tier crossover
		// is never consulted (selectStartTier picks hot).
		//
		// The clock is pinned to t3 + 10m. The retention edge is
		// `now - streamMaxAge + safetyMargin` = `t3 + 10m - 30d + 1h`, which
		// is far older than any event in this spec, so the hot tier's
		// tier-boundary filter (`ev.Timestamp.Before(edge)`) never rejects
		// our events. That keeps the spec focused exclusively on the
		// NotBefore filter — the value `streamScopeFloor` feeds — which is
		// the one INV-SCENE-9 turns on.
		now := t3.Add(10 * time.Minute)
		reader := history.NewReader(
			bus.JS, nil, 30*24*time.Hour,
			func() time.Time { return now },
		)

		// Bob's QueryStreamHistory path: NotBefore is sourced from his
		// FocusMembership.JoinedAt — the value `streamScopeFloor` would
		// compute for a dot-style scene IC subject when called with bob's
		// session.Info (T13's unit test pins that derivation).
		bobFloor := bob.FocusMemberships[0].JoinedAt
		Expect(bobFloor).To(Equal(t2),
			"sanity: bob's FocusMembership.JoinedAt MUST be the value we publish at")

		bobStream, err := reader.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   sceneICSubject,
			NotBefore: bobFloor,
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = bobStream.Close() })

		bobEvents := drainHistory(ctx, bobStream)

		// INV-SCENE-9 ASSERTION: Bob's history MUST contain ONLY events at or
		// after his JoinedAt (t2 and t3). The pre-join events (t0 and t1)
		// MUST be filtered by the substrate.
		bobIDs := idSet(bobEvents)
		Expect(bobIDs).To(HaveLen(2),
			"INV-SCENE-9: bob's history MUST contain exactly 2 events (t2, t3) — got %d", len(bobIDs))
		Expect(bobIDs).To(HaveKey(poses[2].ID.String()),
			"INV-SCENE-9: bob MUST see the t2 pose (his join moment — NotBefore is inclusive)")
		Expect(bobIDs).To(HaveKey(poses[3].ID.String()),
			"INV-SCENE-9: bob MUST see the t3 pose (after his join)")
		Expect(bobIDs).NotTo(HaveKey(poses[0].ID.String()),
			"INV-SCENE-9 violation guard: bob MUST NOT see the t0 pose (pre-join leak)")
		Expect(bobIDs).NotTo(HaveKey(poses[1].ID.String()),
			"INV-SCENE-9 violation guard: bob MUST NOT see the t1 pose (pre-join leak)")

		// Alice (positive control): with NotBefore = her JoinedAt at t0, she
		// MUST see every pose. This proves the absence in Bob's response is
		// a genuine NotBefore filter — not a dead reader, not a stream that
		// dropped events, not a subject mismatch.
		aliceFloor := alice.FocusMemberships[0].JoinedAt
		Expect(aliceFloor).To(Equal(t0))
		aliceStream, err := reader.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   sceneICSubject,
			NotBefore: aliceFloor,
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = aliceStream.Close() })

		aliceEvents := drainHistory(ctx, aliceStream)
		aliceIDs := idSet(aliceEvents)
		Expect(aliceIDs).To(HaveLen(4),
			"positive control: alice's history MUST contain all 4 poses (her JoinedAt is at t0, before every pose)")
		for i := range poses {
			Expect(aliceIDs).To(HaveKey(poses[i].ID.String()),
				"positive control: alice MUST see pose at index %d", i)
		}

		// GetPoseOrder semantics:
		// GetPoseOrder computes scene-globally — it is NOT subject to the
		// temporal floor that QueryStreamHistory applies, by design. A late
		// joiner sees the full pose ordering for the scene regardless of
		// JoinedAt. End-to-end coverage of that property lives in
		// plugins/core-scenes (T27 / holomush-5rh.13.27). T26 deliberately
		// scopes itself to the QueryStreamHistory path where the temporal
		// floor lives.
	})
})

// mintPoseAt builds a well-formed pose event on the given subject with both
// the ULID's embedded time and the Event.Timestamp field set to ts. The
// hot-tier filter at hot_jetstream.go:353 keys off Event.Timestamp; we
// align ULID time as well so any downstream consumer that orders by ULID
// also sees the intended sequence.
func mintPoseAt(subject eventbus.Subject, ts time.Time, body string) eventbus.Event {
	id, err := ulid.New(ulid.Timestamp(ts), crand.Reader)
	if err != nil {
		panic(err)
	}
	return eventbus.Event{
		ID:        id,
		Subject:   subject,
		Type:      eventbus.Type("scene_pose"),
		Timestamp: ts.UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte(body),
	}
}

// drainHistory pulls every event from stream until io.EOF, failing on any
// other error. Mirrors test/integration/eventbus_e2e/cross_tier_query_test.go's
// drainStream but uses Ginkgo Expect rather than t.Fatalf so failures land in
// the surrounding spec's report.
func drainHistory(ctx context.Context, stream eventbus.HistoryStream) []eventbus.Event {
	var out []eventbus.Event
	deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		e, err := stream.Next(deadline)
		if errors.Is(err, io.EOF) {
			return out
		}
		Expect(err).NotTo(HaveOccurred(), "drainHistory: unexpected Next error")
		out = append(out, e)
	}
}

// idSet collects event IDs into a set keyed by ULID-string. Used to assert
// presence/absence without depending on cross-event delivery ordering.
func idSet(events []eventbus.Event) map[string]struct{} {
	out := make(map[string]struct{}, len(events))
	for _, e := range events {
		out[e.ID.String()] = struct{}{}
	}
	return out
}
