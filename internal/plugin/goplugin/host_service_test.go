// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// stubCoordinator records calls for assertion.
type stubCoordinator struct {
	joinCalls           []focusCall
	leaveCalls          []focusCall
	leaveByTargetCalls  []session.FocusKey
	leaveByTargetResult session.LeaveByTargetResult
	leaveByTargetErr    error
	presentCalls        []focusCall
	joinErr             error
	leaveErr            error
	presentErr          error
}

type focusCall struct {
	sessionID string
	key       session.FocusKey
}

func (s *stubCoordinator) JoinFocus(_ context.Context, sid string, target session.FocusKey) error {
	s.joinCalls = append(s.joinCalls, focusCall{sid, target})
	return s.joinErr
}

func (s *stubCoordinator) LeaveFocus(_ context.Context, sid string, target session.FocusKey) error {
	s.leaveCalls = append(s.leaveCalls, focusCall{sid, target})
	return s.leaveErr
}

func (s *stubCoordinator) LeaveFocusByTarget(_ context.Context, target session.FocusKey) (session.LeaveByTargetResult, error) {
	s.leaveByTargetCalls = append(s.leaveByTargetCalls, target)
	return s.leaveByTargetResult, s.leaveByTargetErr
}

func (s *stubCoordinator) PresentFocus(_ context.Context, sid string, target session.FocusKey) error {
	s.presentCalls = append(s.presentCalls, focusCall{sid, target})
	return s.presentErr
}

func (s *stubCoordinator) RestoreFocus(_ context.Context, _ string) (focus.RestorePlan, error) {
	return focus.RestorePlan{}, nil
}

var _ focus.Coordinator = (*stubCoordinator)(nil)

// stubEventStore implements core.EventStore with only ReplayTail wired.
type stubEventStore struct {
	core.EventStore  // embed to satisfy interface; panics on unimplemented methods
	replayTailCalls  []replayTailCall
	replayTailResult []core.Event
	replayTailErr    error
}

type replayTailCall struct {
	stream    string
	count     int
	notBefore time.Time
	beforeID  ulid.ULID
}

func (s *stubEventStore) ReplayTail(_ context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]core.Event, error) {
	s.replayTailCalls = append(s.replayTailCalls, replayTailCall{stream, count, notBefore, beforeID})
	return s.replayTailResult, s.replayTailErr
}

func newTestServer(fc focus.Coordinator, es core.EventStore) *pluginHostServiceServer {
	h := &Host{
		plugins:          make(map[string]*loadedPlugin),
		focusCoordinator: fc,
		eventStore:       es,
	}
	return &pluginHostServiceServer{
		host:       h,
		pluginName: "test-plugin",
	}
}

func TestJoinFocusDelegatesToCoordinatorWithParsedFocusKey(t *testing.T) {
	fc := &stubCoordinator{}
	srv := newTestServer(fc, nil)

	targetID := ulid.Make()
	resp, err := srv.JoinFocus(context.Background(), &pluginv1.PluginHostServiceJoinFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: targetID.String(),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Len(t, fc.joinCalls, 1)
	assert.Equal(t, "sess-1", fc.joinCalls[0].sessionID)
	assert.Equal(t, session.FocusKindScene, fc.joinCalls[0].key.Kind)
	assert.Equal(t, targetID, fc.joinCalls[0].key.TargetID)
}

func TestJoinFocusReturnsErrorWhenCoordinatorFails(t *testing.T) {
	fc := &stubCoordinator{joinErr: oops.Code("FOCUS_ALREADY_MEMBER").Errorf("already member")}
	srv := newTestServer(fc, nil)

	_, err := srv.JoinFocus(context.Background(), &pluginv1.PluginHostServiceJoinFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
}

func TestJoinFocusReturnsErrorWhenCoordinatorIsNil(t *testing.T) {
	srv := newTestServer(nil, nil)

	_, err := srv.JoinFocus(context.Background(), &pluginv1.PluginHostServiceJoinFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
}

func TestLeaveFocusDelegatesToCoordinatorWithParsedFocusKey(t *testing.T) {
	fc := &stubCoordinator{}
	srv := newTestServer(fc, nil)

	targetID := ulid.Make()
	resp, err := srv.LeaveFocus(context.Background(), &pluginv1.PluginHostServiceLeaveFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: targetID.String(),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Len(t, fc.leaveCalls, 1)
	assert.Equal(t, "sess-1", fc.leaveCalls[0].sessionID)
	assert.Equal(t, session.FocusKindScene, fc.leaveCalls[0].key.Kind)
	assert.Equal(t, targetID, fc.leaveCalls[0].key.TargetID)
}

func TestLeaveFocusReturnsErrorWhenCoordinatorIsNil(t *testing.T) {
	srv := newTestServer(nil, nil)

	_, err := srv.LeaveFocus(context.Background(), &pluginv1.PluginHostServiceLeaveFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
}

func TestLeaveFocusByTargetDelegatesToCoordinatorAndMapsResult(t *testing.T) {
	fc := &stubCoordinator{
		leaveByTargetResult: session.LeaveByTargetResult{
			Succeeded:    3,
			TotalScanned: 4,
			Failed:       []session.FailedLeave{{SessionID: "sess-fail"}},
		},
	}
	srv := newTestServer(fc, nil)

	targetID := ulid.Make()
	resp, err := srv.LeaveFocusByTarget(context.Background(), &pluginv1.PluginHostServiceLeaveFocusByTargetRequest{
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: targetID.String(),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int32(3), resp.GetSucceeded())
	assert.Equal(t, int32(4), resp.GetTotalScanned())
	assert.Equal(t, []string{"sess-fail"}, resp.GetFailedSessionIds())

	require.Len(t, fc.leaveByTargetCalls, 1)
	assert.Equal(t, session.FocusKindScene, fc.leaveByTargetCalls[0].Kind)
	assert.Equal(t, targetID, fc.leaveByTargetCalls[0].TargetID)
}

func TestLeaveFocusByTargetReturnsErrorWhenCoordinatorIsNil(t *testing.T) {
	srv := newTestServer(nil, nil)

	_, err := srv.LeaveFocusByTarget(context.Background(), &pluginv1.PluginHostServiceLeaveFocusByTargetRequest{
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
}

func TestLeaveFocusByTargetReturnsErrorForInvalidTarget(t *testing.T) {
	fc := &stubCoordinator{}
	srv := newTestServer(fc, nil)

	_, err := srv.LeaveFocusByTarget(context.Background(), &pluginv1.PluginHostServiceLeaveFocusByTargetRequest{
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: "not-a-ulid",
		},
	})
	require.Error(t, err, "invalid target_id surfaces as RPC error; coordinator MUST NOT be called")
	assert.Empty(t, fc.leaveByTargetCalls)
}

func TestLeaveFocusByTargetReturnsErrorWhenHostIsNil(t *testing.T) {
	srv := &pluginHostServiceServer{host: nil, pluginName: "test-plugin"}

	_, err := srv.LeaveFocusByTarget(context.Background(), &pluginv1.PluginHostServiceLeaveFocusByTargetRequest{
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin host service is not configured")
}

func TestClampCountToInt32HandlesBounds(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int32
	}{
		{"negative clamps to zero", -5, 0},
		{"zero passes through", 0, 0},
		{"small positive passes through", 42, 42},
		{"max int32 passes through", math.MaxInt32, math.MaxInt32},
		{"overflow clamps to MaxInt32", math.MaxInt32 + 1, math.MaxInt32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, clampCountToInt32(tt.in))
		})
	}
}

func TestLeaveFocusByTargetReturnsRpcErrorOnEnumerationFailure(t *testing.T) {
	fc := &stubCoordinator{
		leaveByTargetErr: oops.Code("FOCUS_SWEEP_LIST_FAILED").Errorf("store down"),
	}
	srv := newTestServer(fc, nil)

	resp, err := srv.LeaveFocusByTarget(context.Background(), &pluginv1.PluginHostServiceLeaveFocusByTargetRequest{
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err, "enumeration failure surfaces as RPC error")
	assert.Nil(t, resp, "no response on enumeration failure — per-session errors would ride on the response instead")
}

func TestPresentFocusDelegatesToCoordinatorWithParsedFocusKey(t *testing.T) {
	fc := &stubCoordinator{}
	srv := newTestServer(fc, nil)

	targetID := ulid.Make()
	resp, err := srv.PresentFocus(context.Background(), &pluginv1.PluginHostServicePresentFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: targetID.String(),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Len(t, fc.presentCalls, 1)
	assert.Equal(t, "sess-1", fc.presentCalls[0].sessionID)
	assert.Equal(t, session.FocusKindScene, fc.presentCalls[0].key.Kind)
	assert.Equal(t, targetID, fc.presentCalls[0].key.TargetID)
}

func TestPresentFocusReturnsErrorWhenCoordinatorIsNil(t *testing.T) {
	srv := newTestServer(nil, nil)

	_, err := srv.PresentFocus(context.Background(), &pluginv1.PluginHostServicePresentFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
}

func TestPresentFocusReturnsErrorWhenCoordinatorFails(t *testing.T) {
	fc := &stubCoordinator{presentErr: oops.Code("FOCUS_NOT_MEMBER").Errorf("not member")}
	srv := newTestServer(fc, nil)

	_, err := srv.PresentFocus(context.Background(), &pluginv1.PluginHostServicePresentFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
}

func TestQueryStreamHistoryDelegatesToEventStore(t *testing.T) {
	es := &stubEventStore{
		replayTailResult: []core.Event{
			{Stream: "channel:abc", Type: "say", Payload: []byte(`{"text":"hi"}`)},
		},
	}
	srv := newTestServer(nil, es)

	resp, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream: "channel:abc",
		Count:  10,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetEvents(), 1)
	assert.Equal(t, "channel:abc", resp.GetEvents()[0].GetStream())

	require.Len(t, es.replayTailCalls, 1)
	assert.Equal(t, "channel:abc", es.replayTailCalls[0].stream)
	assert.Equal(t, 10, es.replayTailCalls[0].count)
}

func TestQueryStreamHistoryCapsCountAt500(t *testing.T) {
	es := &stubEventStore{}
	srv := newTestServer(nil, es)

	_, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream: "channel:abc",
		Count:  1000,
	})
	require.NoError(t, err)

	require.Len(t, es.replayTailCalls, 1)
	assert.Equal(t, 500, es.replayTailCalls[0].count)
}

func TestQueryStreamHistoryConvertsNotBeforeMs(t *testing.T) {
	es := &stubEventStore{}
	srv := newTestServer(nil, es)

	_, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream:      "channel:abc",
		Count:       10,
		NotBeforeMs: 1700000000000,
	})
	require.NoError(t, err)

	require.Len(t, es.replayTailCalls, 1)
	assert.Equal(t, time.UnixMilli(1700000000000).UTC(), es.replayTailCalls[0].notBefore)
}

func TestQueryStreamHistoryReturnsErrorWhenEventStoreIsNil(t *testing.T) {
	srv := newTestServer(nil, nil)

	_, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream: "channel:abc",
		Count:  10,
	})
	require.Error(t, err)
}

func TestJoinFocusReturnsErrorWhenHostIsNil(t *testing.T) {
	srv := &pluginHostServiceServer{pluginName: "test-plugin"}
	_, err := srv.JoinFocus(context.Background(), &pluginv1.PluginHostServiceJoinFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin host service is not configured")
}

func TestLeaveFocusReturnsErrorWhenHostIsNil(t *testing.T) {
	srv := &pluginHostServiceServer{pluginName: "test-plugin"}
	_, err := srv.LeaveFocus(context.Background(), &pluginv1.PluginHostServiceLeaveFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin host service is not configured")
}

func TestPresentFocusReturnsErrorWhenHostIsNil(t *testing.T) {
	srv := &pluginHostServiceServer{pluginName: "test-plugin"}
	_, err := srv.PresentFocus(context.Background(), &pluginv1.PluginHostServicePresentFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin host service is not configured")
}

func TestQueryStreamHistoryReturnsErrorWhenHostIsNil(t *testing.T) {
	srv := &pluginHostServiceServer{pluginName: "test-plugin"}
	_, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream: "channel:abc",
		Count:  10,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin host service is not configured")
}

func TestQueryStreamHistoryRejectsNegativeCount(t *testing.T) {
	es := &stubEventStore{}
	srv := newTestServer(nil, es)

	_, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream: "channel:abc",
		Count:  -5,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "count must be non-negative")
}

func TestProtoToFocusKeyReturnsErrorForNilKey(t *testing.T) {
	_, err := protoToFocusKey(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "focus key is required")
}

func TestProtoToFocusKeyReturnsErrorForInvalidULID(t *testing.T) {
	_, err := protoToFocusKey(&pluginv1.FocusKey{
		Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
		TargetId: "not-a-ulid",
	})
	require.Error(t, err)
}

func TestProtoToFocusKindReturnsErrorForUnspecified(t *testing.T) {
	_, err := protoToFocusKind(pluginv1.FocusKind_FOCUS_KIND_UNSPECIFIED)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported focus kind")
}

func TestCoreEventToProtoConvertsAllFields(t *testing.T) {
	eventID := ulid.Make()
	ts := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	e := core.Event{
		ID:        eventID,
		Stream:    "scene:abc:ic",
		Type:      "say",
		Timestamp: ts,
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-1"},
		Payload:   []byte(`{"text":"hello"}`),
	}

	pe := coreEventToProto(e)
	assert.Equal(t, eventID.String(), pe.GetId())
	assert.Equal(t, "scene:abc:ic", pe.GetStream())
	assert.Equal(t, "say", pe.GetType())
	assert.Equal(t, ts.UnixMilli(), pe.GetTimestamp())
	assert.Equal(t, "character", pe.GetActorKind())
	assert.Equal(t, "char-1", pe.GetActorId())
	assert.Equal(t, `{"text":"hello"}`, pe.GetPayload())
}
