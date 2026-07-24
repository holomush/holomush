// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/eventbus"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
	sessionmocks "github.com/holomush/holomush/internal/session/mocks"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// --- stubs -----------------------------------------------------------------
//
// SubscribeHandler's collaborators are narrow enough that hand-rolled stubs
// suffice for everything the generated mocks do not already cover. This is the
// point of the extraction (D-02): the unit is exercisable without the CoreServer
// wiring or the integration harness.

// stubDelivery is a minimal eventbus.Delivery. It records Ack/Nack so the
// tests can assert the delivery-disposition half of dispatchDelivery.
type stubDelivery struct {
	event        eventbus.Event
	metadataOnly bool
	acked        int
	nacked       int
}

func (d *stubDelivery) Event() eventbus.Event { return d.event }
func (d *stubDelivery) MetadataOnly() bool    { return d.metadataOnly }
func (d *stubDelivery) Ack() error            { d.acked++; return nil }
func (d *stubDelivery) Nack() error           { d.nacked++; return nil }
func (d *stubDelivery) InProgress() error     { return nil }

// stubSendStream captures frames written by the handler. The embedded
// grpc.ServerStream is nil — the handler only calls Send, so any other method
// would panic loudly rather than pass silently.
type stubSendStream struct {
	grpc.ServerStream
	ctx  context.Context
	sent []*corev1.SubscribeResponse
}

func (s *stubSendStream) Send(resp *corev1.SubscribeResponse) error {
	s.sent = append(s.sent, resp)
	return nil
}

func (s *stubSendStream) Context() context.Context { return s.ctx }

// stubSceneMute is a SceneMuteChecker whose verdict and error are fixed per
// test. A nil *stubSceneMute is NOT the same as a nil interface — tests that
// want the nil-checker branch pass a literal nil interface value.
type stubSceneMute struct {
	suppress bool
	err      error
}

func (m *stubSceneMute) ShouldSuppress(_ context.Context, _, _, _ string) (bool, error) {
	return m.suppress, m.err
}

// minimalDeps returns a SubscribeDeps populated only with what the test at hand
// needs. Every field is optional at construction time; the handler resolves
// nil collaborators at call time exactly as CoreServer did.
func minimalDeps() grpcpkg.SubscribeDeps {
	return grpcpkg.SubscribeDeps{
		GameID: func() string { return "main" },
	}
}

// --- Test 1: SC1 proof ------------------------------------------------------

// TestNewSubscribeHandlerConstructsFromOnlyItsOwnCollaborators is the
// operational proof of ARCH-01 success criterion 1 (D-02): the extracted unit
// is constructible from an external test package with narrow collaborators
// only — no *CoreServer, no integrationtest harness, no build tag.
func TestNewSubscribeHandlerConstructsFromOnlyItsOwnCollaborators(t *testing.T) {
	store := sessionmocks.NewMockStore(t)

	h := grpcpkg.NewSubscribeHandler(grpcpkg.SubscribeDeps{
		SessionStore: store,
		SceneMute:    &stubSceneMute{},
		GameID:       func() string { return "main" },
	})

	require.NotNil(t, h)
}

// --- Test 2: pure translation helpers --------------------------------------

func TestSubscribeHandlerToSubjectQualifiesRelativeStreamReference(t *testing.T) {
	h := grpcpkg.NewSubscribeHandler(minimalDeps())

	got, err := grpcpkg.ExportToSubject(h, "main", "character.01HYXCHAR00000000000000001")

	require.NoError(t, err)
	assert.Equal(t, "events.main.character.01HYXCHAR00000000000000001", string(got))
}

func TestSubscribeHandlerToSubjectRejectsColonStyleStreamReference(t *testing.T) {
	h := grpcpkg.NewSubscribeHandler(minimalDeps())

	_, err := grpcpkg.ExportToSubject(h, "main", "character:01HYXCHAR00000000000000001")

	require.Error(t, err)
}

func TestSubscribeHandlerComputeInitialFiltersDropsUnqualifiableStreams(t *testing.T) {
	h := grpcpkg.NewSubscribeHandler(minimalDeps())

	plan := focus.RestorePlan{Streams: []focus.StreamWithMode{
		{Stream: "character.01HYXCHAR00000000000000001", Mode: focus.ReplayModeFromCursor},
		{Stream: "scene:not-a-dot-reference", Mode: focus.ReplayModeFromCursor},
		{Stream: "location.01HYXLOC000000000000000001", Mode: focus.ReplayModeFromCursor},
	}}

	got := grpcpkg.ExportComputeInitialFilters(context.Background(), h, plan)

	require.Len(t, got, 2, "the colon-style reference MUST be dropped, not fatal")
	assert.Equal(t, "events.main.character.01HYXCHAR00000000000000001", string(got[0]))
	assert.Equal(t, "events.main.location.01HYXLOC000000000000000001", string(got[1]))
}

// --- Test 3: sceneMute fail-OPEN semantics (INV-SCENE-62) -------------------

// sceneBadgeFixture wires the store reads dispatchDelivery performs on the
// badge-downgrade path: a session Get and a per-connection GetConnection whose
// FocusKey is nil (i.e. the connection is NOT focused on the scene).
func sceneBadgeFixture(t *testing.T) (*sessionmocks.MockStore, *session.Info, ulid.ULID, string, *stubDelivery) {
	t.Helper()

	sceneID := "01HYXSCENE0000000000000001"
	info := &session.Info{
		ID:          "01HYXSESS00000000000000001",
		CharacterID: ulid.MustParse("01HYXCHAR00000000000000001"),
		PlayerID:    ulid.MustParse("01HYXPLYR00000000000000001"),
	}
	connID := ulid.MustParse("01HYXCONN00000000000000001")

	store := sessionmocks.NewMockStore(t)
	store.EXPECT().Get(mock.Anything, info.ID).Return(info, nil).Once()
	store.EXPECT().GetConnection(mock.Anything, connID).
		Return(&session.Connection{ID: connID, SessionID: info.ID}, nil).Once()

	subject, err := eventbus.NewSubject("events.main.scene." + sceneID + ".ic")
	require.NoError(t, err)
	typ, err := eventbus.NewType("say")
	require.NoError(t, err)

	delivery := &stubDelivery{event: eventbus.Event{
		ID:        ulid.MustParse("01HYXEVNT00000000000000001"),
		Subject:   subject,
		Type:      typ,
		Timestamp: time.Now(),
		Payload:   []byte(`{}`),
	}}

	return store, info, connID, sceneID, delivery
}

func TestSubscribeHandlerDispatchDeliveryDeliversSceneBadgeWhenSceneMuteIsNil(t *testing.T) {
	store, info, connID, sceneID, delivery := sceneBadgeFixture(t)

	// Nil checker — the fail-OPEN default (INV-SCENE-62).
	h := grpcpkg.NewSubscribeHandler(grpcpkg.SubscribeDeps{
		SessionStore: store,
		SceneMute:    nil,
		GameID:       func() string { return "main" },
	})
	stream := &stubSendStream{ctx: context.Background()}

	err := grpcpkg.ExportDispatchDelivery(context.Background(), h, info, delivery, stream, &connID)

	require.NoError(t, err)
	require.Len(t, stream.sent, 1, "nil sceneMute MUST still deliver the badge (fail-open)")
	ctrl := stream.sent[0].GetControl()
	require.NotNil(t, ctrl)
	assert.Equal(t, corev1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY, ctrl.GetSignal())
	assert.Equal(t, sceneID, ctrl.GetSceneId())
	assert.Equal(t, 1, delivery.acked)
}

func TestSubscribeHandlerDispatchDeliveryDeliversSceneBadgeWhenSceneMuteErrors(t *testing.T) {
	store, info, connID, sceneID, delivery := sceneBadgeFixture(t)

	// A checker that fails MUST also fail OPEN — mute is a preference, not
	// access control, and the badge frame is already content-free.
	h := grpcpkg.NewSubscribeHandler(grpcpkg.SubscribeDeps{
		SessionStore: store,
		SceneMute:    &stubSceneMute{err: oops.Errorf("mute backend unavailable")},
		GameID:       func() string { return "main" },
	})
	stream := &stubSendStream{ctx: context.Background()}

	err := grpcpkg.ExportDispatchDelivery(context.Background(), h, info, delivery, stream, &connID)

	require.NoError(t, err)
	require.Len(t, stream.sent, 1, "a sceneMute error MUST still deliver the badge (fail-open)")
	ctrl := stream.sent[0].GetControl()
	require.NotNil(t, ctrl)
	assert.Equal(t, corev1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY, ctrl.GetSignal())
	assert.Equal(t, sceneID, ctrl.GetSceneId())
	assert.Equal(t, 1, delivery.acked)
}

func TestSubscribeHandlerDispatchDeliverySuppressesSceneBadgeWhenSceneMuteSaysSuppress(t *testing.T) {
	store, info, connID, _, delivery := sceneBadgeFixture(t)

	h := grpcpkg.NewSubscribeHandler(grpcpkg.SubscribeDeps{
		SessionStore: store,
		SceneMute:    &stubSceneMute{suppress: true},
		GameID:       func() string { return "main" },
	})
	stream := &stubSendStream{ctx: context.Background()}

	err := grpcpkg.ExportDispatchDelivery(context.Background(), h, info, delivery, stream, &connID)

	require.NoError(t, err)
	assert.Empty(t, stream.sent, "an explicit suppress verdict drops the badge")
	assert.Equal(t, 1, delivery.acked, "drop paths MUST ack or JetStream redelivers forever")
}

// --- Test 4: identityRegistry nil fallback ---------------------------------

func TestSubscribeHandlerToProtoSubscribeResponseFallsBackToULIDStringWhenIdentityRegistryIsNil(t *testing.T) {
	// Nil registry — non-character actors fall back to ULID-string form.
	h := grpcpkg.NewSubscribeHandler(grpcpkg.SubscribeDeps{
		IdentityRegistry: nil,
		GameID:           func() string { return "main" },
	})

	actorID := ulid.MustParse("01HYXPLUG00000000000000001")
	subject, err := eventbus.NewSubject("events.main.character.01HYXCHAR00000000000000001")
	require.NoError(t, err)
	typ, err := eventbus.NewType("say")
	require.NoError(t, err)

	ev := eventbus.Event{
		ID:        ulid.MustParse("01HYXEVNT00000000000000002"),
		Subject:   subject,
		Type:      typ,
		Timestamp: time.Now(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindPlugin, ID: actorID},
		Payload:   []byte(`{}`),
	}

	got := grpcpkg.ExportToProtoSubscribeResponse(h, ev, false)

	frame := got.GetEvent()
	require.NotNil(t, frame)
	assert.Equal(t, actorID.String(), frame.GetActorId(),
		"a nil identityRegistry MUST fall back to ULID-string form, not error or blank")
}
