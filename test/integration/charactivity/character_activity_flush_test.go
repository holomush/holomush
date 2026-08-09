// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package charactivity_test

import (
	"context"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/charactivity"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/world"
)

// flushInterval keeps the suite fast. It deliberately carries NO claim about
// racing the first tick: the ticker starts at Activate, inside
// integrationtest.Start, an unbounded time before emit() runs (an AuthedPlayer
// provisioning and several DB round trips intervene), so its phase is
// uncorrelated with this spec and no assertion below may depend on a tick
// having — or not having — already fired. Production runs five minutes: the
// column lags by one interval BY CONSTRUCTION (D-42).
const flushInterval = 250 * time.Millisecond

var _ = Describe("Character activity", func() {
	var (
		ctx    context.Context
		srv    *integrationtest.Server
		charID ulid.ULID
	)

	BeforeEach(func() {
		ctx = context.Background()
		// MemoryStorage is passed EXPLICITLY. A KV bucket carries its own
		// storage config and does not inherit the stream's, and FileStorage is
		// the zero value of jetstream.StorageType — so an omitted parameter
		// would silently put this bucket on disk inside a memory harness.
		srv = integrationtest.Start(suiteT,
			integrationtest.WithCharacterActivity(jetstream.MemoryStorage, flushInterval))
		charID = srv.AuthedPlayer(ctx, "Flushee").CharacterID
	})

	// lastActiveAt reads the column verbatim.
	lastActiveAt := func() int64 {
		var got int64
		Expect(srv.Pool().QueryRow(ctx,
			`SELECT last_active_at FROM characters WHERE id = $1`, charID.String()).Scan(&got)).To(Succeed())
		return got
	}

	characterVersion := func() int64 {
		var got int64
		Expect(srv.Pool().QueryRow(ctx,
			`SELECT version FROM characters WHERE id = $1`, charID.String()).Scan(&got)).To(Succeed())
		return got
	}

	outboxRows := func() int64 {
		var got int64
		Expect(srv.Pool().QueryRow(ctx,
			`SELECT count(*) FROM outbox WHERE aggregate_id = $1`, charID.String()).Scan(&got)).To(Succeed())
		return got
	}

	// bufferedValue reads the character's key out of the live KV bucket.
	bufferedValue := func() (string, error) {
		kv, err := srv.Bus().JS.KeyValue(ctx, charactivity.BucketName)
		if err != nil {
			return "", err
		}
		entry, err := kv.Get(ctx, charID.String())
		if err != nil {
			return "", err
		}
		return string(entry.Value()), nil
	}

	// bufferedOrFlushed reports the activity timestamp WHEREVER it currently
	// lives: in the KV buffer before a tick drains it, or in the column after.
	//
	// Written this way on purpose. The ticker's phase is uncorrelated with this
	// spec (see flushInterval), so a poll that insisted on reading the key
	// would fail permanently the moment a tick drained it first — the key never
	// comes back, so every subsequent poll returns ErrKeyNotFound and the
	// Eventually times out. Reading through to the column keeps the assertion
	// about the VALUE, which is the part that matters, and removes the
	// dependence on when the tick lands.
	bufferedOrFlushed := func() string {
		if v, err := bufferedValue(); err == nil {
			return v
		}
		return strconv.FormatInt(lastActiveAt(), 10)
	}

	// emit publishes one character-actor event through the production
	// publisher and returns the timestamp it carried.
	emit := func() int64 {
		subject, err := eventbus.Qualify(srv.Bus().Bus.GameID(), "character."+charID.String())
		Expect(err).NotTo(HaveOccurred())
		ev := eventbus.NewEvent(
			subject,
			eventbus.Type("say"),
			eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: charID},
			[]byte(`{"body":"hello"}`),
		)
		pub := srv.Bus().Bus.Publisher()
		Expect(pub).NotTo(BeNil())
		Expect(pub.Publish(ctx, ev)).To(Succeed())
		return ev.Timestamp.UnixNano()
	}

	Describe("a character-actor event", func() {
		It("buffers into the KV bucket without any database write, then flushes into last_active_at", func() {
			Expect(lastActiveAt()).To(Equal(world.NeverActive),
				"a fresh character has never been active")
			versionBefore := characterVersion()
			outboxBefore := outboxRows()

			at := emit()

			// SYNCHRONOUS, before any await: the emit path itself performed no
			// database write — the deciding property of D-42. Publish has
			// returned, so anything the emit path was going to write is
			// already written. This is the assertion that used to sit AFTER a
			// polling wait, where a tick landing in between failed it outright.
			Expect(lastActiveAt()).To(Equal(world.NeverActive),
				"the emit path MUST NOT touch Postgres; only the flush writes the column")

			// The timestamp reaches the pipeline. Asserted wherever it
			// currently lives rather than on the KEY SURVIVING: a tick may
			// drain the buffer at any moment, and requiring the key to still
			// be there would make this a race against the ticker's phase.
			Eventually(bufferedOrFlushed, 5*time.Second, 20*time.Millisecond).
				Should(Equal(strconv.FormatInt(at, 10)),
					"the listener buffers the event timestamp as decimal epoch nanoseconds")

			// One tick later, the flusher has drained the buffer through
			// INV-WORLD-4's fourth sanctioned writer.
			Eventually(lastActiveAt, 10*time.Second, 20*time.Millisecond).Should(Equal(at),
				"the flush advances the column to the buffered timestamp")

			// Verifies: INV-WORLD-4 — writer (4) is the ENVELOPE-EXEMPT one.
			Expect(characterVersion()).To(Equal(versionBefore),
				"last_active_at is an operational column: the flush bumps no version")
			Expect(outboxRows()).To(Equal(outboxBefore),
				"the flush emits no world-change envelope — one per flushed character per tick would flood the feed")

			// The flushed key is gone, so the bucket does not grow without bound.
			Eventually(func() error {
				_, err := bufferedValue()
				return err
			}, 5*time.Second, 20*time.Millisecond).Should(MatchError(jetstream.ErrKeyNotFound))
		})
	})
})
