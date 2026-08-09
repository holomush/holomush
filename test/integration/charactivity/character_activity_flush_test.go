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

// flushInterval is short enough to keep the suite fast and long enough that the
// pre-flush assertions below are not racing the very first tick. Production
// runs five minutes: the column lags by one interval BY CONSTRUCTION (D-42).
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

			// The EMIT path performed no database write — the deciding
			// property of D-42. The buffered key appears while the column is
			// still the never-active sentinel.
			Eventually(bufferedValue, 5*time.Second, 20*time.Millisecond).
				Should(Equal(strconv.FormatInt(at, 10)),
					"the listener buffers the event timestamp as decimal epoch nanoseconds")
			Expect(lastActiveAt()).To(Equal(world.NeverActive),
				"the emit path MUST NOT touch Postgres; only the flush ticker writes the column")

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
