// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package retirement

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/core"
	gamesession "github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// --- fakes -------------------------------------------------------------
//
// Every dependency is faked through the package's OWN consumer-defined
// interfaces (the internal/auth PresenceEmitter shape), and every fake
// appends to one shared journal so ORDER is assertable — the D-37/D-38
// requirement that the leave names the place the character left is only
// meaningful if the leave strictly precedes the move.

type journal struct{ steps []string }

func (j *journal) add(step string) { j.steps = append(j.steps, step) }

type fakeSessions struct {
	j    *journal
	info *gamesession.Info
	err  error
	// calls records every character id DeleteByCharacter was asked for.
	calls []ulid.ULID
}

func (f *fakeSessions) DeleteByCharacter(_ context.Context, characterID ulid.ULID) (*gamesession.Info, error) {
	f.calls = append(f.calls, characterID)
	f.j.add("delete_session")
	return f.info, f.err
}

type leaveCall struct {
	Char   core.CharacterRef
	Reason string
}

type sessionEndedCall struct {
	Char      core.CharacterRef
	SessionID string
	Cause     string
	Reason    string
}

type fakePresence struct {
	j         *journal
	leaveErr  error
	endedErr  error
	leaves    []leaveCall
	sessionsE []sessionEndedCall
}

func (f *fakePresence) EmitLeave(_ context.Context, char core.CharacterRef, reason string) error {
	f.leaves = append(f.leaves, leaveCall{Char: char, Reason: reason})
	f.j.add("emit_leave")
	return f.leaveErr
}

func (f *fakePresence) EmitSessionEnded(_ context.Context, char core.CharacterRef, sessionID, cause, reason string) error {
	f.sessionsE = append(f.sessionsE, sessionEndedCall{Char: char, SessionID: sessionID, Cause: cause, Reason: reason})
	f.j.add("emit_session_ended")
	return f.endedErr
}

type moveCall struct {
	Caller     world.Caller
	CharacterI ulid.ULID
	To         ulid.ULID
}

type fakeWorld struct {
	j       *journal
	char    *world.Character
	getErr  error
	moveErr error
	// getCallers records the Caller every read was made as, so the
	// provenance triple is assertable by value equality against
	// world.JobCaller (Caller's fields are unexported; reflect.DeepEqual
	// through require.Equal is the only way to inspect them).
	getCallers []world.Caller
	moves      []moveCall
}

func (f *fakeWorld) GetCharacter(_ context.Context, caller world.Caller, _ ulid.ULID) (*world.Character, error) {
	f.getCallers = append(f.getCallers, caller)
	f.j.add("get_character")
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.char, nil
}

func (f *fakeWorld) MoveCharacter(_ context.Context, caller world.Caller, characterID, to ulid.ULID) error {
	f.moves = append(f.moves, moveCall{Caller: caller, CharacterI: characterID, To: to})
	f.j.add("move_character")
	return f.moveErr
}

// --- fixture -----------------------------------------------------------

const testGameID = "main"

type fixture struct {
	j        *journal
	sessions *fakeSessions
	presence *fakePresence
	world    *fakeWorld
	startLoc ulid.ULID
	charID   ulid.ULID
	eventID  ulid.ULID
	reactor  *reactor
}

// newFixture wires a reactor whose character is RETIRED and located
// somewhere other than the starting location, with a live session — the
// happy path. Each test mutates one dimension away from it.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	j := &journal{}
	startLoc := core.NewULID()
	charID := core.NewULID()
	oldLoc := core.NewULID()

	f := &fixture{
		j:        j,
		startLoc: startLoc,
		charID:   charID,
		eventID:  core.NewULID(),
		sessions: &fakeSessions{
			j: j,
			info: &gamesession.Info{
				ID:          "01SESSION",
				CharacterID: charID,
				LocationID:  oldLoc,
			},
		},
		presence: &fakePresence{j: j},
		world: &fakeWorld{
			j: j,
			char: &world.Character{
				ID:         charID,
				Name:       "Rhea",
				LocationID: &oldLoc,
				Status:     world.StatusRetired,
			},
		},
	}

	r, err := newReactor(Config{
		Sessions:        f.sessions,
		Presence:        f.presence,
		World:           f.world,
		StartLocationID: func() ulid.ULID { return startLoc },
	})
	require.NoError(t, err)
	f.reactor = r
	return f
}

// delivery builds the reactor's view of one delivered character_retired
// message for the fixture's character.
func (f *fixture) delivery() delivery {
	return delivery{
		EventID:   f.eventID.String(),
		EventType: eventTypeCharacterRetired,
		Aggregate: f.charID.String(),
		Character: f.charID,
	}
}

// --- event-type gate ---------------------------------------------------

func TestProcessAcksAndSkipsAnEventTypeOtherThanCharacterRetired(t *testing.T) {
	f := newFixture(t)
	d := f.delivery()
	d.EventType = "character_unretired"

	require.Equal(t, ack, f.reactor.process(context.Background(), d))
	require.Empty(t, f.j.steps, "a foreign event type MUST NOT read or write anything")
}

// --- status guard ------------------------------------------------------

func TestProcessAcksAndSkipsWhenTheCharacterIsNoLongerRetired(t *testing.T) {
	f := newFixture(t)
	f.world.char.Status = world.StatusActive

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()))
	require.Equal(t, []string{"get_character"}, f.j.steps,
		"a character un-retired between emit and delivery MUST NOT be evicted")
}

func TestProcessAcksAndSkipsWhenTheStatusIsNotAKnownLifecycleValue(t *testing.T) {
	f := newFixture(t)
	f.world.char.Status = world.Status("resurrected")

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()),
		"an unparsable status is permanently unhandleable, not transient")
	require.Equal(t, []string{"get_character"}, f.j.steps,
		"INV-WORLD-5's denying default MUST suppress every effect")
}

func TestProcessRetriesWhenTheStatusReadFailsTransiently(t *testing.T) {
	f := newFixture(t)
	f.world.getErr = oops.Code("CHARACTER_GET_FAILED").Errorf("connection reset")

	require.Equal(t, retry, f.reactor.process(context.Background(), f.delivery()))
	require.Equal(t, []string{"get_character"}, f.j.steps,
		"a failed status read MUST NOT produce a partial fanout")
}

func TestProcessAcksWhenTheStatusReadIsDeniedByPolicy(t *testing.T) {
	f := newFixture(t)
	f.world.getErr = oops.Code("JOB_CHARACTER_ACCESS_DENIED").Wrap(world.ErrPermissionDenied)

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()),
		"a policy denial is not retryable — redelivering it forever buys nothing")
	require.Equal(t, []string{"get_character"}, f.j.steps)
}

// --- happy path --------------------------------------------------------

func TestProcessEndsTheSessionNotifiesTheOldLocationThenMovesToTheStartLocation(t *testing.T) {
	f := newFixture(t)

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()))
	require.Equal(t, []string{
		"get_character",
		"delete_session",
		"emit_leave",
		"emit_session_ended",
		"move_character",
	}, f.j.steps, "the leave MUST strictly precede the move so it names the place they left")
}

func TestProcessEmitsTheLeaveAtTheLocationTheCharacterIsLeaving(t *testing.T) {
	f := newFixture(t)
	oldLoc := f.sessions.info.LocationID

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()))
	require.Len(t, f.presence.leaves, 1)
	require.Equal(t, leaveCall{
		Char:   core.CharacterRef{ID: f.charID, Name: "Rhea", LocationID: oldLoc},
		Reason: leaveReasonRetired,
	}, f.presence.leaves[0])
}

// TestProcessPrefersTheCharacterRowsLocationOverTheSessionRowsStaleCopy pins
// which row is authoritative when the two disagree. The session row's
// LocationID is a copy written when the session was last updated, so a move
// that landed after that would have the leave announced at a place the
// character had already left.
func TestProcessPrefersTheCharacterRowsLocationOverTheSessionRowsStaleCopy(t *testing.T) {
	f := newFixture(t)
	currentLoc := core.NewULID()
	f.world.char.LocationID = &currentLoc // a move landed after the session row was written
	staleLoc := f.sessions.info.LocationID
	require.NotEqual(t, staleLoc, currentLoc)

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()))
	require.Len(t, f.presence.leaves, 1)
	assert.Equal(t, currentLoc, f.presence.leaves[0].Char.LocationID,
		"the character row is authoritative for where the character is")
	require.Len(t, f.presence.sessionsE, 1)
	assert.Equal(t, currentLoc, f.presence.sessionsE[0].Char.LocationID,
		"both emissions carry the same ref")
}

// TestProcessFallsBackToTheSessionLocationWhenTheCharacterHasNone covers the
// legitimate no-location case, where the session row is the only source.
func TestProcessFallsBackToTheSessionLocationWhenTheCharacterHasNone(t *testing.T) {
	f := newFixture(t)
	f.world.char.LocationID = nil

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()))
	require.Len(t, f.presence.leaves, 1)
	assert.Equal(t, f.sessions.info.LocationID, f.presence.leaves[0].Char.LocationID)
}

func TestProcessEmitsSessionEndedWithTheRetiredCause(t *testing.T) {
	f := newFixture(t)
	oldLoc := f.sessions.info.LocationID

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()))
	require.Len(t, f.presence.sessionsE, 1)
	require.Equal(t, sessionEndedCall{
		Char:      core.CharacterRef{ID: f.charID, Name: "Rhea", LocationID: oldLoc},
		SessionID: "01SESSION",
		Cause:     core.SessionEndedCauseRetired,
		Reason:    leaveReasonRetired,
	}, f.presence.sessionsE[0])
}

func TestProcessMovesTheRetireeToTheConfiguredStartingLocation(t *testing.T) {
	f := newFixture(t)

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()))
	require.Len(t, f.world.moves, 1)
	require.Equal(t, f.charID, f.world.moves[0].CharacterI)
	require.Equal(t, f.startLoc, f.world.moves[0].To)
}

// --- job identity ------------------------------------------------------

// TestProcessAuthorizesEveryWorldCallAsTheRetirementJobCarryingTheProvenanceTriple
// is the D-47 proof. Caller's fields are unexported, so equality against a
// freshly built world.JobCaller is the assertion: it pins the job name, the
// job: namespace, and all three provenance keys at once.
func TestProcessAuthorizesEveryWorldCallAsTheRetirementJobCarryingTheProvenanceTriple(t *testing.T) {
	f := newFixture(t)
	want := world.JobCaller(JobName, world.Provenance{
		EventID:   f.eventID.String(),
		EventType: eventTypeCharacterRetired,
		Subject:   f.charID.String(),
	})

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()))
	require.Len(t, f.world.getCallers, 1)
	require.Equal(t, want, f.world.getCallers[0], "the status read MUST run as the job")
	require.Len(t, f.world.moves, 1)
	require.Equal(t, want, f.world.moves[0].Caller, "the move MUST run as the job")
}

// --- idempotency under at-least-once redelivery ------------------------

func TestProcessSkipsBothNotificationsWhenRedeliveryFindsNoSessionToEnd(t *testing.T) {
	f := newFixture(t)
	f.sessions.info = nil // DeleteByCharacter's (nil, nil) absence signal

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()))
	require.Empty(t, f.presence.leaves, "a redelivery MUST NOT emit a second leave")
	require.Empty(t, f.presence.sessionsE, "a redelivery MUST NOT emit a second session_ended")
	require.Len(t, f.world.moves, 1, "the move is still owed while the character is elsewhere")
}

func TestProcessSkipsTheMoveWhenTheCharacterIsAlreadyAtTheStartingLocation(t *testing.T) {
	f := newFixture(t)
	f.world.char.LocationID = &f.startLoc

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()))
	require.Empty(t, f.world.moves, "a redelivery MUST NOT emit a second character_moved")
}

// TestProcessIsFullyIdempotentAcrossARedeliveryOfTheSameMessage drives the
// SAME message twice through a fake whose observed state advances the way
// production's would, and pins the total effect counts at one each.
func TestProcessIsFullyIdempotentAcrossARedeliveryOfTheSameMessage(t *testing.T) {
	f := newFixture(t)
	d := f.delivery()

	require.Equal(t, ack, f.reactor.process(context.Background(), d))

	// Production state after the first pass: the session row is gone and the
	// character now sits at the starting location.
	f.sessions.info = nil
	f.world.char.LocationID = &f.startLoc

	require.Equal(t, ack, f.reactor.process(context.Background(), d))

	require.Len(t, f.presence.leaves, 1, "exactly one leave across two deliveries")
	require.Len(t, f.presence.sessionsE, 1, "exactly one session_ended across two deliveries")
	require.Len(t, f.world.moves, 1, "exactly one move across two deliveries")
}

// --- offline retiree ---------------------------------------------------

func TestProcessMovesAnOfflineRetireeWithoutEmittingAnyPresenceEvent(t *testing.T) {
	f := newFixture(t)
	f.sessions.info = nil

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()))
	require.Equal(t, []string{"get_character", "delete_session", "move_character"}, f.j.steps)
}

// --- failure dispositions ----------------------------------------------

func TestProcessRetriesWhenTheSessionTeardownFails(t *testing.T) {
	f := newFixture(t)
	f.sessions.err = oops.Errorf("pool exhausted")

	require.Equal(t, retry, f.reactor.process(context.Background(), f.delivery()))
	require.Empty(t, f.world.moves, "a failed teardown MUST NOT be followed by a move")
}

func TestProcessRetriesWhenTheMoveFailsTransiently(t *testing.T) {
	f := newFixture(t)
	f.world.moveErr = oops.Code("CHARACTER_MOVE_FAILED").Errorf("deadlock detected")

	require.Equal(t, retry, f.reactor.process(context.Background(), f.delivery()),
		"acking a retryably-failed move strands the character at the old location")
}

func TestProcessAcksWhenTheMoveIsDeniedByPolicy(t *testing.T) {
	f := newFixture(t)
	f.world.moveErr = oops.Code("JOB_CHARACTER_ACCESS_DENIED").Wrap(world.ErrPermissionDenied)

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()),
		"a policy denial cannot be cured by redelivery")
}

// TestProcessTreatsAFailedPresenceEmitAsOperationalDegradation mirrors the
// eight existing synchronous fanout sites: an emit failure is logged, never
// fatal, and never blocks the move.
func TestProcessTreatsAFailedPresenceEmitAsOperationalDegradation(t *testing.T) {
	f := newFixture(t)
	f.presence.leaveErr = oops.Errorf("bus unavailable")
	f.presence.endedErr = oops.Errorf("bus unavailable")

	require.Equal(t, ack, f.reactor.process(context.Background(), f.delivery()))
	require.Len(t, f.world.moves, 1)
}

// --- wire decoding -----------------------------------------------------

func TestNewDeliveryDerivesTheCharacterFromThePayloadAndTheProvenanceFromTheSubject(t *testing.T) {
	charID := core.NewULID()
	eventID := core.NewULID()
	subject := "events." + testGameID + ".character." + charID.String()

	d, err := newDelivery(subject, retiredHeaders(eventID), retiredWireEvent(t, eventID, charID))
	require.NoError(t, err)

	require.Equal(t, eventID.String(), d.EventID)
	require.Equal(t, eventTypeCharacterRetired, d.EventType)
	require.Equal(t, charID.String(), d.Aggregate,
		"trigger_subject is the BARE aggregate ULID, never the dotted NATS subject")
	require.Equal(t, charID, d.Character)
}

// TestNewDeliveryDerivesTheCharacterIndependentlyOfTheSubject is what keeps
// the authorization check from being tautological (D-55): the resource comes
// from the handler's own decode of the body, the provenance from the
// transport subject. When they disagree the seed denies the write.
func TestNewDeliveryDerivesTheCharacterIndependentlyOfTheSubject(t *testing.T) {
	bodyChar := core.NewULID()
	subjectChar := core.NewULID()
	eventID := core.NewULID()

	d, err := newDelivery(
		"events."+testGameID+".character."+subjectChar.String(),
		retiredHeaders(eventID),
		retiredWireEvent(t, eventID, bodyChar),
	)
	require.NoError(t, err)
	require.Equal(t, bodyChar, d.Character, "the resource comes from the body")
	require.Equal(t, subjectChar.String(), d.Aggregate, "the provenance comes from the subject")
}

// TestAggregateFromSubjectRefusesEveryShapeThatIsNotTheFourTokenAggregate pins
// the positional parse. A last-token read would return the FACET of a faceted
// subject, which the instance-scoped seed then compares against resource.id --
// default-denying the write, which classifyWorldError acks as terminal. The
// fanout would be dropped with no retry behind a misleading "policy denied".
func TestAggregateFromSubjectRefusesEveryShapeThatIsNotTheFourTokenAggregate(t *testing.T) {
	charID := core.NewULID()

	require.Equal(t, charID.String(),
		aggregateFromSubject("events."+testGameID+".character."+charID.String()),
		"the canonical four-token subject yields the bare aggregate ULID")

	for _, subject := range []string{
		"events." + testGameID + ".character." + charID.String() + ".facet",
		"events." + testGameID + ".character",
		"character." + charID.String(),
		charID.String(),
		"",
	} {
		require.Empty(t, aggregateFromSubject(subject),
			"an unexpected subject shape MUST NOT masquerade as an aggregate id: %q", subject)
	}
}

func TestNewDeliveryRejectsAnUndecodableBody(t *testing.T) {
	eventID := core.NewULID()
	_, err := newDelivery("events.main.character.x", retiredHeaders(eventID), []byte("not a protobuf envelope"))
	require.Error(t, err)
}

func TestProcessAcksAnUndecodableBodyRatherThanRedeliveringItForever(t *testing.T) {
	f := newFixture(t)
	require.Equal(t, ack, f.reactor.handleDecoded(context.Background(),
		"events.main.character.x", retiredHeaders(f.eventID), []byte("garbage")))
	require.Empty(t, f.j.steps)
}

// TestHandleDecodedAcksAForeignEventTypeWithoutDecodingItsBody pins the header
// gate that keeps routine traffic off the poison path.
//
// The consumer filter (events.*.character.>) also carries presence's
// session_ended (internal/presence/session_ended.go:67 publishes on exactly
// character.<id>), whose body is NOT a world envelope. Gating on the header
// before the unmarshal is what stops every logout — and every SUCCESSFUL
// retirement, whose own step-(3) session_ended comes straight back to this
// consumer — from logging an ERROR "poison" line. A body that could never
// decode proves the decode was never attempted: the old order logged here.
func TestHandleDecodedAcksAForeignEventTypeWithoutDecodingItsBody(t *testing.T) {
	f := newFixture(t)

	capture := &errorLogCapture{}
	old := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(old)

	hdr := nats.Header{}
	hdr.Set("Nats-Msg-Id", f.eventID.String())
	hdr.Set("App-Event-Type", "session_ended")

	require.Equal(t, ack, f.reactor.handleDecoded(context.Background(),
		"events."+testGameID+".character."+f.charID.String(), hdr, []byte("not a world envelope")))
	require.Empty(t, f.j.steps, "a foreign event type triggers no effect")
	require.Zero(t, capture.errors, "routine non-world traffic MUST NOT be logged as poison")
}

// errorLogCapture counts ERROR-level records so a test can assert the absence
// of a spurious diagnostic.
type errorLogCapture struct{ errors int }

func (c *errorLogCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *errorLogCapture) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		c.errors++
	}
	return nil
}

func (c *errorLogCapture) WithAttrs([]slog.Attr) slog.Handler { return c }

func (c *errorLogCapture) WithGroup(string) slog.Handler { return c }

// --- wire fixtures -----------------------------------------------------

func retiredHeaders(eventID ulid.ULID) nats.Header {
	h := nats.Header{}
	h.Set("Nats-Msg-Id", eventID.String())
	h.Set("App-Event-Type", eventTypeCharacterRetired)
	return h
}

// retiredWireEvent builds the exact bytes the outbox relay puts on the wire:
// a proto-marshaled eventbusv1.Event whose Payload is the JSON world
// envelope.
func retiredWireEvent(t *testing.T, eventID, charID ulid.ULID) []byte {
	t.Helper()
	inner, err := json.Marshal(map[string]string{
		"character_id": charID.String(),
		"status":       "retired",
	})
	require.NoError(t, err)
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID.String(),
		"game_id":        testGameID,
		"kind":           eventTypeCharacterRetired,
		"schema_version": 1,
		"actor":          "character:" + charID.String(),
		"aggregate_type": "character",
		"aggregate_id":   charID.String(),
		"epoch":          1,
		"feed_position":  1,
		"affected":       []any{},
		"payload":        json.RawMessage(inner),
	})
	require.NoError(t, err)
	data, err := proto.Marshal(&eventbusv1.Event{
		Id:      eventID[:],
		Subject: "events." + testGameID + ".character." + charID.String(),
		Type:    eventTypeCharacterRetired,
		Payload: envelope,
	})
	require.NoError(t, err)
	return data
}
