// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package cursor_bounded_backfill_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// I-IU8J-1, I-IU8J-3, I-IU8J-4 end-to-end.
//
// The bead's load-bearing claim: a backfill query scoped to
// attach_moment_ms CANNOT return events whose timestamp is after the
// Subscribe attach. That removes the connect-time race by construction
// — backfill never observes a post-attach event, so a command sent
// during the connect window cannot be picked up by backfill regardless
// of timing.
//
// Test pattern:
//
//  1. Publish a pre-attach event E1 with timestamp T1 (via the
//     integrationtest harness's Server.publish helpers, or via an
//     AuthedPlayer's emit path so the timestamp is authoritative).
//  2. Connect Alice (REPLAY_COMPLETE arrives with attach_moment_ms=T2).
//  3. Publish a post-attach event E3 with timestamp T3 > T2.
//  4. Alice's backfill query, scoped to NotAfterMs=T2, MUST return E1
//     and MUST NOT return E3.
var _ = Describe("Cursor-bounded backfill (holomush-iu8j)", func() {
	var (
		ts  *integrationtest.Server
		ctx context.Context
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
		DeferCleanup(cancel)
		ts = integrationtest.Start(suiteT)
		DeferCleanup(func() { ts.Stop() })
	})

	It("Subscribe emits attach_moment_ms > 0 on REPLAY_COMPLETE (I-IU8J-4)", func() {
		alice := ts.ConnectAuthed(ctx, "Alice")
		defer alice.Logout(ctx)

		attachMs := alice.AttachMomentMs()
		Expect(attachMs).To(BeNumerically(">", int64(0)),
			"REPLAY_COMPLETE ControlFrame MUST carry a non-zero attach_moment_ms (I-IU8J-4); "+
				"a zero value would tell the client 'no upper bound' which would re-open the fujt race")

		// Server-side attach moment is captured AFTER OpenSession returns,
		// so it must be at or before "now" (modulo clock skew on the
		// testcontainer host).
		Expect(attachMs).To(BeNumerically("<=", time.Now().UTC().UnixMilli()),
			"attach_moment_ms MUST be at or before the test's observed wall clock — "+
				"a future-dated attach moment would mean backfill scoping is wrong by construction")
	})

	It("query history with NotAfterMs INCLUDES pre-attach events (I-IU8J-3 boundary inclusive)", func() {
		alice := ts.ConnectAuthed(ctx, "Alice")
		defer alice.Logout(ctx)

		locStream := "location." + alice.LocationID.String()

		// Publish a pre-attach-ish event. Use the integrationtest harness's
		// EmitDirectEvent so the event reaches events_audit via the same
		// publish path the production server uses; its timestamp is
		// captured at publish time (time.Now().UTC()).
		Expect(alice.EmitDirectEvent(ctx, locStream, "core-communication:pose",
			[]byte(`{"character_name":"Alice","action":"waves before attach."}`))).
			To(Succeed())

		// Use a future-dated NotAfterMs (well past 'now') so the
		// published event is unambiguously inside the window. This pins
		// the "pre-bound event is returned" invariant independently of
		// the more sensitive "exactly at-the-boundary" case (covered by
		// the unit-level matchesQuery boundary tests).
		notAfterMs := time.Now().UTC().Add(1 * time.Hour).UnixMilli()

		Eventually(func() int {
			events, err := alice.QueryStreamHistoryBounded(ctx, locStream, notAfterMs)
			if err != nil {
				return -1
			}
			return len(events)
		}, "5s", "100ms").Should(BeNumerically(">=", 1),
			"a published event with timestamp < NotAfterMs MUST be returned by the bounded query — "+
				"asserts the NotAfter filter does NOT incorrectly exclude pre-bound events (I-IU8J-1 contrapositive)")
	})

	It("query history with NotAfterMs EXCLUDES post-attach events (I-IU8J-1 — the race-elimination invariant)", func() {
		alice := ts.ConnectAuthed(ctx, "Alice")
		defer alice.Logout(ctx)

		locStream := "location." + alice.LocationID.String()
		attachMs := alice.AttachMomentMs()
		Expect(attachMs).To(BeNumerically(">", int64(0)),
			"need attach moment to scope the bounded query")

		// Wait past the attach moment, then publish. The published
		// event's timestamp will be > attachMs by construction (publish
		// uses time.Now() server-side at the moment of publish).
		// 50ms is well above ms-precision rounding error.
		time.Sleep(50 * time.Millisecond)
		const postAttachMarker = "iu8j:post-attach"
		Expect(alice.EmitDirectEvent(ctx, locStream, postAttachMarker,
			[]byte(`{"character_name":"Alice","action":"speaks AFTER attach."}`))).
			To(Succeed())

		// Backfill scoped to attachMs MUST NOT return this event.
		// Consistently (poll for 2s) — we want to catch eventual-
		// consistency drift as well as the immediate case.
		Consistently(func() bool {
			events, err := alice.QueryStreamHistoryBounded(ctx, locStream, attachMs)
			// Fail fast on RPC errors rather than masking them as
			// "no marker found" — a broken QueryStreamHistoryBounded
			// would otherwise silently let this spec pass (CodeRabbit
			// finding on PR #4234).
			Expect(err).NotTo(HaveOccurred())
			for _, ev := range events {
				if ev.GetType() == postAttachMarker {
					return true
				}
			}
			return false
		}, "2s", "100ms").Should(BeFalse(),
			"backfill query scoped to NotAfterMs=attachMs MUST NOT return events published AFTER attach — "+
				"this is the structural fix that eliminates the holomush-fujt connect-time race. "+
				"If this test fails, the cursor-bounded backfill is leaking post-attach events into "+
				"the dimmed-scrollback rendering path, and the fujt race surface is still open.")
	})
})
