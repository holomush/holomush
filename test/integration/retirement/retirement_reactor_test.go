// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package retirement_test

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/presence"
	"github.com/holomush/holomush/internal/retirement"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/world"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// fanoutTimeout bounds every Eventually below. The fanout is EVENTUALLY
// consistent by construction (D-36): the write commits, the relay drains, the
// durable consumer delivers, and only then does the handler act. Nothing here
// may assert synchronously.
const fanoutTimeout = 30 * time.Second

// fanoutPoll is the Eventually polling interval.
const fanoutPoll = 100 * time.Millisecond

// busCapture accumulates every message published on events.> so a spec can
// assert on emissions that happened while it was waiting.
//
// A core NATS subscription is the right instrument here: JetStream publishes are
// ordinary NATS messages on their subject, so this observes exactly the bytes
// the reactor's own consumer sees, without opening a second durable consumer
// that would compete for deliveries.
type busCapture struct {
	mu   sync.Mutex
	msgs []*nats.Msg
	sub  *nats.Subscription
}

// captureBus subscribes to every game event and drains into memory. The
// subscription is flushed before returning, so a publish issued after this call
// cannot be missed.
func captureBus(conn *nats.Conn) *busCapture {
	c := &busCapture{}
	sub, err := conn.Subscribe("events.>", func(m *nats.Msg) {
		c.mu.Lock()
		c.msgs = append(c.msgs, m)
		c.mu.Unlock()
	})
	Expect(err).NotTo(HaveOccurred(), "subscribe to the game feed")
	Expect(conn.Flush()).To(Succeed(), "flush the capture subscription before any publish")
	c.sub = sub
	return c
}

func (c *busCapture) stop() {
	if c.sub != nil {
		_ = c.sub.Unsubscribe() //nolint:errcheck // teardown of a test-only subscription
	}
}

// matching returns every captured message on subject carrying the given
// App-Event-Type header.
func (c *busCapture) matching(subject, eventType string) []*nats.Msg {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*nats.Msg
	for _, m := range c.msgs {
		if m.Subject == subject && m.Header.Get(eventbus.HeaderEventType) == eventType {
			out = append(out, m)
		}
	}
	return out
}

// count is matching's arity, shaped for Eventually/Consistently.
func (c *busCapture) count(subject, eventType string) func() int {
	return func() int { return len(c.matching(subject, eventType)) }
}

// payloadOf proto-unmarshals the wire event and returns its JSON payload. The
// bus carries a proto eventbusv1.Event whose Payload is the emitter's JSON
// body; reading the body without this step reads protobuf framing, not data.
func payloadOf(m *nats.Msg) []byte {
	var wire eventbusv1.Event
	Expect(proto.Unmarshal(m.Data, &wire)).To(Succeed(), "unmarshal the wire event")
	return wire.GetPayload()
}

var _ = Describe("Character retirement, carried end to end by the real relay, consumer and handler", func() {
	var (
		ctx     context.Context
		srv     *integrationtest.Server
		feed    *busCapture
		charID  ulid.ULID
		sessID  string
		oldLoc  ulid.ULID
		startAt ulid.ULID

		leaveSubject     string
		characterSubject string
	)

	// outboxKinds returns the committed envelope kinds for the character, in
	// the feed order the writer allocated.
	outboxKinds := func() []string {
		rows, err := srv.Pool().Query(ctx,
			`SELECT kind FROM outbox WHERE aggregate_id = $1 ORDER BY epoch, feed_position`,
			charID.String())
		Expect(err).NotTo(HaveOccurred())
		defer rows.Close()
		var kinds []string
		for rows.Next() {
			var k string
			Expect(rows.Scan(&k)).To(Succeed())
			kinds = append(kinds, k)
		}
		Expect(rows.Err()).NotTo(HaveOccurred())
		return kinds
	}

	// feedPositionOf returns the feed position of the single envelope of the
	// given kind for this character.
	feedPositionOf := func(kind string) int64 {
		var pos int64
		Expect(srv.Pool().QueryRow(ctx,
			`SELECT feed_position FROM outbox WHERE aggregate_id = $1 AND kind = $2`,
			charID.String(), kind).Scan(&pos)).To(Succeed())
		return pos
	}

	countKind := func(kind string) func() int64 {
		return func() int64 {
			var n int64
			Expect(srv.Pool().QueryRow(ctx,
				`SELECT count(*) FROM outbox WHERE aggregate_id = $1 AND kind = $2`,
				charID.String(), kind).Scan(&n)).To(Succeed())
			return n
		}
	}

	sessionRows := func() int64 {
		var n int64
		Expect(srv.Pool().QueryRow(ctx,
			`SELECT count(*) FROM sessions WHERE id = $1`, sessID).Scan(&n)).To(Succeed())
		return n
	}

	locationOf := func() string {
		var loc *string
		Expect(srv.Pool().QueryRow(ctx,
			`SELECT location_id FROM characters WHERE id = $1`, charID.String()).Scan(&loc)).To(Succeed())
		if loc == nil {
			return ""
		}
		return *loc
	}

	BeforeEach(func() {
		ctx = context.Background()
		srv = integrationtest.Start(suiteT,
			integrationtest.WithOutboxRelay(),
			integrationtest.WithRetirementReactor())

		sess := srv.ConnectAuthed(ctx, "Retiree")
		charID = sess.CharacterID
		sessID = sess.SessionID
		oldLoc = sess.LocationID
		startAt = srv.RetirementStartLocation()
		Expect(oldLoc).NotTo(Equal(startAt),
			"the fanout's destination MUST differ from where the character stands, "+
				"or the reactor's already-there gate correctly makes the move unobservable")

		gameID := srv.Bus().Bus.GameID()
		sub, err := eventbus.Qualify(gameID, "location."+oldLoc.String())
		Expect(err).NotTo(HaveOccurred())
		leaveSubject = string(sub)
		sub, err = eventbus.Qualify(gameID, "character."+charID.String())
		Expect(err).NotTo(HaveOccurred())
		characterSubject = string(sub)

		// Capture BEFORE acting: leave and session_ended are emitted during the
		// fanout, and a subscription opened afterwards would miss them.
		feed = captureBus(srv.Bus().Conn)
		DeferCleanup(feed.stop)

		caller := world.HumanCaller(charID.String())
		char, err := srv.World().GetCharacter(ctx, caller, charID)
		Expect(err).NotTo(HaveOccurred())

		// The retirement itself. Nothing synthetic is published: this commits an
		// outbox row and returns, and every effect asserted below is produced by
		// the REAL relay publishing it and the REAL durable consumer delivering
		// it to the reactor.
		Expect(srv.World().RetireCharacter(ctx, caller, charID, char.Version)).To(Succeed())
	})

	It("ends the session, announces the leave at the location left, emits session_ended with cause retired, and moves the character to the starting location", func() {
		Eventually(sessionRows, fanoutTimeout, fanoutPoll).Should(BeZero(),
			"the reactor ends the retiree's session")

		Eventually(feed.count(leaveSubject, "leave"), fanoutTimeout, fanoutPoll).Should(Equal(1),
			"the leave is announced at the location the character LEFT, not the one they end up at")
		var leave presence.LeavePayload
		Expect(json.Unmarshal(payloadOf(feed.matching(leaveSubject, "leave")[0]), &leave)).To(Succeed())
		Expect(leave.Reason).To(Equal("retired"))

		Eventually(feed.count(characterSubject, "session_ended"), fanoutTimeout, fanoutPoll).Should(Equal(1))
		var ended core.SessionEndedPayload
		Expect(json.Unmarshal(payloadOf(feed.matching(characterSubject, "session_ended")[0]), &ended)).To(Succeed())
		Expect(ended.Cause).To(Equal(core.SessionEndedCauseRetired),
			"session_ended MUST carry the retired cause, not a generic one")
		Expect(ended.SessionID).To(Equal(sessID))

		Eventually(locationOf, fanoutTimeout, fanoutPoll).Should(Equal(startAt.String()),
			"the retiree is moved to the configured starting location")
	})

	It("commits the character_retired envelope ahead of character_moved in the world feed", func() {
		Eventually(outboxKinds, fanoutTimeout, fanoutPoll).Should(
			ContainElement("character_moved"),
			"the move envelope lands only after the whole chain has run",
		)

		retiredAt := feedPositionOf("character_retired")
		movedAt := feedPositionOf("character_moved")
		Expect(retiredAt).To(BeNumerically("<", movedAt),
			"the trigger MUST precede the effect it caused in feed order (IDENT-10)")
	})

	It("produces no second leave and no second move when the same character_retired event is delivered again", func() {
		// Settle the first delivery completely.
		Eventually(feed.count(leaveSubject, "leave"), fanoutTimeout, fanoutPoll).Should(Equal(1))
		Eventually(countKind("character_moved"), fanoutTimeout, fanoutPoll).Should(Equal(int64(1)))

		original := feed.matching(characterSubject, "character_retired")
		Expect(original).To(HaveLen(1), "the relay published exactly one character_retired")

		cons, err := srv.Bus().JS.Consumer(ctx, eventbus.StreamName, retirement.DefaultConsumerName)
		Expect(err).NotTo(HaveOccurred())
		ackedBefore := func() uint64 {
			info, infoErr := cons.Info(ctx)
			Expect(infoErr).NotTo(HaveOccurred())
			return info.AckFloor.Consumer
		}
		before := ackedBefore()

		// Redeliver: the SAME subject, the SAME body bytes, the SAME
		// App-Event-Type. Only Nats-Msg-Id is refreshed, because JetStream would
		// otherwise dedup a byte-identical republish and the reactor would never
		// see a second delivery at all — a redelivery test that silently tests
		// nothing. The reactor reads the id only as provenance, never as a gate.
		hdr := nats.Header{}
		for k, v := range original[0].Header {
			hdr[k] = append([]string(nil), v...)
		}
		hdr.Set(eventbus.HeaderMsgID, ulid.Make().String())
		_, err = srv.Bus().JS.PublishMsg(ctx, &nats.Msg{
			Subject: original[0].Subject,
			Header:  hdr,
			Data:    original[0].Data,
		})
		Expect(err).NotTo(HaveOccurred())

		// Prove the reactor actually CONSUMED the redelivery. Without this the
		// assertions below would hold just as well if the message never arrived.
		Eventually(ackedBefore, fanoutTimeout, fanoutPoll).Should(BeNumerically(">", before),
			"the reactor acked the redelivered event")

		Expect(feed.count(leaveSubject, "leave")()).To(Equal(1),
			"a second delivery finds no session to end, so it emits no second leave")
		Expect(countKind("character_moved")()).To(Equal(int64(1)),
			"a second delivery finds the character already at the starting location, so it emits no second move")
	})
})

var _ = Describe("The retirement job's world authority", func() {
	var (
		ctx   context.Context
		srv   *integrationtest.Server
		mine  ulid.ULID
		other ulid.ULID
		dest  ulid.ULID
	)

	BeforeEach(func() {
		ctx = context.Background()
		// The REAL seeded engine: seed:job-retirement-instance-scoped is what is
		// under test, and the allow-all default would permit both halves below
		// and prove nothing. WithRetirementReactor supplies the job's LIVENESS —
		// without a registered, running job the provider stamps no attributes and
		// BOTH halves deny, which the positive control catches.
		srv = integrationtest.Start(suiteT,
			integrationtest.WithRealABAC(),
			integrationtest.WithRetirementReactor())
		mine = srv.AuthedPlayer(ctx, "Subject").CharacterID
		other = srv.AuthedPlayer(ctx, "Bystander").CharacterID
		dest = srv.RetirementStartLocation()
	})

	// callerFor builds the caller the reactor builds: the job identity plus the
	// provenance triple derived from the message it is handling.
	callerFor := func(aggregate ulid.ULID) world.Caller {
		return world.JobCaller(retirement.JobName, world.Provenance{
			EventID:   ulid.Make().String(),
			EventType: "character_retired",
			Subject:   aggregate.String(),
		})
	}

	It("denies a move against an aggregate other than the one whose event it is handling", func() {
		err := srv.World().MoveCharacter(ctx, callerFor(mine), other, dest)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(world.ErrPermissionDenied),
			"the instance fence is what stops a mis-decoded aggregate from being corrupted")
		oopsErr, ok := oops.AsOops(err)
		Expect(ok).To(BeTrue(), "a world denial is an oops error")
		Expect(oopsErr.Code()).To(Equal("JOB_CHARACTER_ACCESS_DENIED"),
			"the deny carries the job-qualified code shape (02.2 D-58)")
	})

	It("permits the move against the aggregate whose event it is handling", func() {
		// The paired positive control. Without it the denial above passes just as
		// well when the job was never registered, when the seed never installed,
		// or when the attribute provider stamps nothing at all.
		Expect(srv.World().MoveCharacter(ctx, callerFor(mine), mine, dest)).To(Succeed())

		var loc *string
		Expect(srv.Pool().QueryRow(ctx,
			`SELECT location_id FROM characters WHERE id = $1`, mine.String()).Scan(&loc)).To(Succeed())
		Expect(loc).NotTo(BeNil())
		Expect(*loc).To(Equal(dest.String()))
	})
})
