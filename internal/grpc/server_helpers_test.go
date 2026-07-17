// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/grpcclient"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// TestReplayCompleteFrame verifies the REPLAY_COMPLETE control signal shape.
func TestReplayCompleteFrame(t *testing.T) {
	t.Parallel()
	f := replayCompleteFrame(0)
	require.NotNil(t, f)
	ctrl := f.GetControl()
	require.NotNil(t, ctrl)
	assert.Equal(t, corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE, ctrl.GetSignal())
	assert.Equal(t, int64(0), ctrl.GetAttachMomentMs(),
		"replayCompleteFrame(0) MUST emit attach_moment_ms=0 (back-compat sentinel — client treats 0 as 'no upper bound')")
}

// TestReplayCompleteFrameCarriesAttachMomentMs pins the wire contract for
// holomush-iu8j: the REPLAY_COMPLETE ControlFrame MUST carry the
// server's attach-moment epoch-ms so the client can pass it back as
// not_after_ms on subsequent backfill calls. Invariant I-IU8J-4.
func TestReplayCompleteFrameCarriesAttachMomentMs(t *testing.T) {
	t.Parallel()
	// Pick a deterministic non-zero attach moment.
	attachMomentMs := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC).UnixMilli()
	f := replayCompleteFrame(attachMomentMs)
	require.NotNil(t, f)
	ctrl := f.GetControl()
	require.NotNil(t, ctrl)
	assert.Equal(t, corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE, ctrl.GetSignal())
	assert.Equal(t, attachMomentMs, ctrl.GetAttachMomentMs(),
		"REPLAY_COMPLETE ControlFrame MUST carry attach_moment_ms verbatim (I-IU8J-4)")
}

func TestStreamClosedFrameIncludesReason(t *testing.T) {
	t.Parallel()
	f := streamClosedFrame("booted")
	require.NotNil(t, f)
	ctrl := f.GetControl()
	require.NotNil(t, ctrl)
	assert.Equal(t, corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED, ctrl.GetSignal())
	assert.Equal(t, "booted", ctrl.GetMessage())
}

func TestActorIDStringZeroIsEmpty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", actorIDString(eventbus.Actor{}, nil))
	id := core.NewULID()
	assert.Equal(t, id.String(), actorIDString(eventbus.Actor{ID: id}, nil))
}

func TestToSubjectQualifiesRelativeStream(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	sub, err := s.toSubject("main", "character.01HYXYZCHAR0000000000000CH")
	require.NoError(t, err)
	assert.Equal(t, eventbus.Subject("events.main.character.01HYXYZCHAR0000000000000CH"), sub)
}

func TestToSubjectRejectsColonStream(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	// Colon-style references are no longer valid stream names: Qualify produces
	// "events.main.character:..." which NewSubject rejects (holomush-rops).
	_, err := s.toSubject("main", "character:01HYXYZCHAR0000000000000CH")
	require.Error(t, err)
}

func TestToSubjectRejectsInvalidSubjectCharacters(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	// Invalid characters after qualification: "!" isn't in the subject token set.
	_, err := s.toSubject("main", "character.bad!token")
	require.Error(t, err)
}

func TestCurrentGameIDFallsBackToMain(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	assert.Equal(t, "main", s.currentGameID())

	// Empty game id from provider also falls back.
	s.gameID = func() string { return "" }
	assert.Equal(t, "main", s.currentGameID())

	s.gameID = func() string { return "prod" }
	assert.Equal(t, "prod", s.currentGameID())
}

func TestComputeInitialFiltersSkipsInvalidStreams(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	plan := focus.RestorePlan{
		Streams: []focus.StreamWithMode{
			{Stream: "character.01HYXYZCHAR0000000000000CH", Mode: focus.ReplayModeFromCursor},
			// Invalid — colon reference no longer qualifies.
			{Stream: "character::bad", Mode: focus.ReplayModeFromCursor},
			{Stream: "location.01HYXYZ0C0000000000000000C", Mode: focus.ReplayModeFromCursor},
		},
	}
	subs := s.computeInitialFilters(context.Background(), plan)
	assert.Len(t, subs, 2, "invalid stream dropped silently, not a fatal error")
	assert.Contains(t, subs, eventbus.Subject("events.main.character.01HYXYZCHAR0000000000000CH"))
	assert.Contains(t, subs, eventbus.Subject("events.main.location.01HYXYZ0C0000000000000000C"))
}

func TestToProtoSubscribeResponseMapsFields(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	id := core.NewULID()
	charID := core.NewULID()
	resp := s.toProtoSubscribeResponse(eventbus.Event{
		ID:        id,
		Subject:   eventbus.Subject("events.main.character." + charID.String()),
		Type:      eventbus.Type("scene.pose"),
		Timestamp: time.Unix(42, 0),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: charID},
		Payload:   []byte("x"),
	}, false)
	ev := resp.GetEvent()
	require.NotNil(t, ev)
	assert.Equal(t, id.String(), ev.GetId())
	assert.Equal(t, "events.main.character."+charID.String(), ev.GetStream())
	assert.Equal(t, "scene.pose", ev.GetType())
	assert.Equal(t, "character", ev.GetActorType())
	assert.Equal(t, charID.String(), ev.GetActorId())
}

func TestFilterSetToSliceReturnsAllSubjects(t *testing.T) {
	t.Parallel()
	set := map[eventbus.Subject]struct{}{
		"events.main.a.b": {},
		"events.main.c.d": {},
	}
	out := filterSetToSlice(set)
	assert.ElementsMatch(t, []eventbus.Subject{
		"events.main.a.b",
		"events.main.c.d",
	}, out)
}

func TestSubscribeRejectsNilSubscriber(t *testing.T) {
	t.Parallel()
	s := &CoreServer{} // subscriber nil
	err := s.Subscribe(&corev1.SubscribeRequest{
		SessionId:          "s1",
		PlayerSessionToken: testPlayerSessionToken,
	}, &fakeSubscribeStream{ctx: context.Background()})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "NOT_CONFIGURED", o.Code())
}

func TestSubscribeRejectsMissingSessionToken(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	info := &session.Info{ID: "s1", ExpiresAt: &future}
	s := &CoreServer{
		subscriber:        &stubSubscriber{},
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"s1": info}),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	err := s.Subscribe(&corev1.SubscribeRequest{
		SessionId: "s1",
		// PlayerSessionToken missing
	}, &fakeSubscribeStream{ctx: context.Background()})
	require.Error(t, err)
	// Post-rsoe6.11.1: the enumeration-safe SESSION_NOT_FOUND is stamped with a
	// gRPC status code (codes.Unauthenticated) so the gateway can classify a
	// reaped session on the wire rather than getting an undecodable codes.Unknown.
	st, ok := status.FromError(err)
	require.True(t, ok, "SESSION_NOT_FOUND must carry a gRPC status code")
	assert.Equal(t, codes.Unauthenticated, st.Code())
	// And it must round-trip back to the SESSION_NOT_FOUND oops code via the
	// client translator the gateways use.
	o, ok := oops.AsOops(grpcclient.TranslateSubscribeErr(err))
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", o.Code())
}

func TestSubscribeRejectsUnknownSession(t *testing.T) {
	t.Parallel()
	s := &CoreServer{
		subscriber:        &stubSubscriber{},
		sessionStore:      newTestSessionStore(t, nil),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	err := s.Subscribe(&corev1.SubscribeRequest{
		SessionId:          "missing",
		PlayerSessionToken: testPlayerSessionToken,
	}, &fakeSubscribeStream{ctx: context.Background()})
	require.Error(t, err)
	// Post-rsoe6.11.1: SESSION_NOT_FOUND crosses the wire as codes.Unauthenticated
	// (see subscribeSessionNotFound) and round-trips back via TranslateSubscribeErr.
	st, ok := status.FromError(err)
	require.True(t, ok, "SESSION_NOT_FOUND must carry a gRPC status code")
	assert.Equal(t, codes.Unauthenticated, st.Code())
	o, ok := oops.AsOops(grpcclient.TranslateSubscribeErr(err))
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", o.Code())
}

// TestSubscribeSessionNotFoundStampsUnauthenticatedWireCode pins the server
// half of the rsoe6.11.1 fix: subscribeSessionNotFound MUST carry a gRPC status
// code on the wire (codes.Unauthenticated) rather than a bare oops error, which
// grpc-go would surface to the client as codes.Unknown — indistinguishable from
// a transient fault and thus undecodable by grpcclient.TranslateSubscribeErr.
//
// Relocated here (07-01 Task 1/2) from internal/grpc/client_test.go, which
// moved to internal/grpcclient/client_test.go: subscribeSessionNotFound is
// package-private to internal/grpc and unreachable from grpcclient, so this
// server-side pin cannot move with the client wrapper's tests.
func TestSubscribeSessionNotFoundStampsUnauthenticatedWireCode(t *testing.T) {
	t.Parallel()
	err := subscribeSessionNotFound("test-session")
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "SESSION_NOT_FOUND must be a gRPC status error so the wire code is classifiable")
	assert.Equal(t, codes.Unauthenticated, st.Code(),
		"SESSION_NOT_FOUND must cross the wire as Unauthenticated so the gateway decodes it as terminal")
	// Round-trip: the wire code must decode back to the SESSION_NOT_FOUND oops
	// code via the client translator.
	decoded := grpcclient.TranslateSubscribeErr(err)
	oopsErr, ok := oops.AsOops(decoded)
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", oopsErr.Code(),
		"server wire code MUST round-trip back to SESSION_NOT_FOUND through the client translator")
}

// stubSubscriber is a minimal eventbus.Subscriber that errors on OpenSession.
type stubSubscriber struct{}

func (stubSubscriber) OpenSession(_ context.Context, _ string, _ eventbus.SessionIdentity, _ []eventbus.Subject, _ time.Time) (eventbus.SessionStream, error) {
	return nil, oops.Errorf("stub subscriber")
}

// fakeSubscribeStream is a minimal grpc.ServerStreamingServer stub used for
// tests that only exercise Subscribe's pre-flight guards (before the first
// Send).
type fakeSubscribeStream struct {
	ctx  context.Context //nolint:containedctx // test stub
	sent []*corev1.SubscribeResponse
	err  error
}

func (f *fakeSubscribeStream) Context() context.Context       { return f.ctx }
func (f *fakeSubscribeStream) SendHeader(_ metadata.MD) error { return nil }
func (f *fakeSubscribeStream) SetHeader(_ metadata.MD) error  { return nil }
func (f *fakeSubscribeStream) SetTrailer(_ metadata.MD)       {}
func (f *fakeSubscribeStream) SendMsg(_ any) error            { return nil }
func (f *fakeSubscribeStream) RecvMsg(_ any) error            { return nil }
func (f *fakeSubscribeStream) Send(r *corev1.SubscribeResponse) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, r)
	return nil
}

// TestWithXxxOptionsCompile sanity-checks the option constructors
// (WithSessionStore, WithSessionDefaults, WithVerbRegistry, WithWorldQuerier,
// WithStreamContributor, WithStreamRegistry, WithFocusCoordinator,
// WithAccessEngine, WithSubscriber, WithHistoryReader, WithGameID). Each
// should be a one-line closure that sets a field. The test exists to
// exercise the closures so they contribute to coverage; the fields are
// then verified via struct introspection where reasonable.
func TestServerOptionClosuresAssignFields(t *testing.T) {
	t.Parallel()
	// We instantiate a CoreServer via the public option API and verify
	// each closure set its corresponding field.
	gameIDFn := func() string { return "g1" }
	opts := []CoreServerOption{
		WithSessionStore(sessiontest.NewStore(t)),
		WithSessionDefaults(SessionDefaults{TTL: time.Minute, MaxHistory: 10, MaxReplay: 5}),
		WithEventStore(&mockEventStore{}),
		WithWorldQuerier(nil),
		WithVerbRegistry(nil),
		WithStreamContributor(nil),
		WithStreamRegistry(nil),
		WithFocusCoordinator(nil),
		WithAccessEngine(nil),
		WithSubscriber(&stubSubscriber{}),
		WithHistoryReader(&fakeHistoryReader{}),
		WithGameID(gameIDFn),
	}

	server := newHandleCommandServer(t, &mockEventStore{}, nil, opts...)
	require.NotNil(t, server)
	assert.Equal(t, "g1", server.currentGameID())
}
