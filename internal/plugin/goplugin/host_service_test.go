// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/grpc/focus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
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

	// Phase 5 RPC stubs.
	setConnFocusResult focus.SetConnectionFocusResult
	setConnFocusErr    error
	autoFocusResult    focus.AutoFocusOnJoinResponse
	autoFocusErr       error
	isAnyFocusedResult bool
	isAnyFocusedErr    error
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

func (s *stubCoordinator) IsAnyConnFocused(_ context.Context, _, _ ulid.ULID) (bool, error) {
	return s.isAnyFocusedResult, s.isAnyFocusedErr
}

func (s *stubCoordinator) RestoreConnectionFocus(_ context.Context, _ string, _ ulid.ULID) error {
	return nil
}

func (s *stubCoordinator) SetConnectionFocus(_ context.Context, _ ulid.ULID, _ *session.FocusKey, _ bool) (focus.SetConnectionFocusResult, error) {
	return s.setConnFocusResult, s.setConnFocusErr
}

func (s *stubCoordinator) AutoFocusOnJoin(_ context.Context, _, _ ulid.ULID) (focus.AutoFocusOnJoinResponse, error) {
	return s.autoFocusResult, s.autoFocusErr
}

var _ focus.Coordinator = (*stubCoordinator)(nil)

// stubHistoryReader implements plugins.HistoryReader for testing.
type stubHistoryReader struct {
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

func (s *stubHistoryReader) ReplayTail(_ context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]core.Event, error) {
	s.replayTailCalls = append(s.replayTailCalls, replayTailCall{stream, count, notBefore, beforeID})
	return s.replayTailResult, s.replayTailErr
}

var _ plugins.HistoryReader = (*stubHistoryReader)(nil)

func newTestServer(fc focus.Coordinator, hr plugins.HistoryReader) *pluginHostServiceServer {
	h := &Host{
		plugins:          make(map[string]*loadedPlugin),
		focusCoordinator: fc,
		historyReader:    hr,
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
	es := &stubHistoryReader{
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
	es := &stubHistoryReader{}
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
	es := &stubHistoryReader{}
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
	es := &stubHistoryReader{}
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

func TestQueryStreamHistoryPopulatesPerEventCursors(t *testing.T) {
	evID := ulid.Make()
	es := &stubHistoryReader{
		replayTailResult: []core.Event{
			{ID: evID, Stream: "channel:abc", Type: "say", Payload: []byte(`{}`)},
		},
	}
	srv := newTestServer(nil, es)

	resp, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream: "channel:abc",
		Count:  10,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetEvents(), 1)
	// Each returned event must have a non-empty cursor blob.
	assert.NotEmpty(t, resp.GetEvents()[0].GetCursor(), "per-event cursor must be set")
}

func TestQueryStreamHistoryDecodesOpaqueBeforeIDCursor(t *testing.T) {
	// Build a valid host cursor wrapping a ULID as the beforeID.
	anchorID := ulid.Make()
	es := &stubHistoryReader{}
	srv := newTestServer(nil, es)

	// Encode the cursor the same way encodeHostEventCursor does.
	cursorBytes := encodeHostEventCursor(anchorID)
	require.NotEmpty(t, cursorBytes)

	_, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream: "channel:abc",
		Count:  5,
		Cursor: cursorBytes,
	})
	require.NoError(t, err)

	require.Len(t, es.replayTailCalls, 1)
	assert.Equal(t, anchorID, es.replayTailCalls[0].beforeID, "decoded cursor ULID must be forwarded as beforeID")
}

func TestQueryStreamHistoryRejectsInvalidCursorBytes(t *testing.T) {
	es := &stubHistoryReader{}
	srv := newTestServer(nil, es)

	_, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream: "channel:abc",
		Count:  5,
		Cursor: []byte("not-a-valid-cursor"),
	})
	require.Error(t, err)
	var oe oops.OopsError
	require.ErrorAs(t, err, &oe)
	// cursor.Decode stamps EVENTBUS_CURSOR_INVALID; the host wraps with
	// INVALID_ARGUMENT context but the inner code is preserved by oops.Wrap.
	assert.Equal(t, "EVENTBUS_CURSOR_INVALID", oe.Code())
}

func TestQueryStreamHistorySetsNextCursorWhenPageFull(t *testing.T) {
	// When len(events) == count, next_cursor should be populated.
	evts := make([]core.Event, 0, 3)
	for range 3 {
		evts = append(evts, core.Event{ID: ulid.Make(), Stream: "channel:abc", Type: "say", Payload: []byte(`{}`)})
	}
	es := &stubHistoryReader{replayTailResult: evts}
	srv := newTestServer(nil, es)

	resp, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream: "channel:abc",
		Count:  3, // exactly count events returned → next_cursor populated
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.GetNextCursor(), "next_cursor must be set when page is full")
}

func TestQueryStreamHistoryNextCursorEmptyWhenFewerEventsThanCount(t *testing.T) {
	evts := []core.Event{
		{ID: ulid.Make(), Stream: "channel:abc", Type: "say", Payload: []byte(`{}`)},
	}
	es := &stubHistoryReader{replayTailResult: evts}
	srv := newTestServer(nil, es)

	resp, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream: "channel:abc",
		Count:  10, // more than returned → no more pages
	})
	require.NoError(t, err)
	assert.Empty(t, resp.GetNextCursor(), "next_cursor must be empty when page is not full")
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

// newTestHostWithEmitter constructs a Host with a real PluginEventEmitter
// wired to the given embedded JetStream bus. Manifest lookup returns the
// caller-supplied manifest when called for pluginName; nil otherwise. The
// actor resolver pulls the actor from the context (which the host's token
// flow stamps via core.WithActor) so the emit ends up with the host-vouched
// actor on the published message.
//
// pluginName is parameterized intentionally — current callers all use
// "plug-A" as the loaded plugin, but the cross-plugin leak test still
// invokes EmitEvent through a *different* server pluginName ("plug-B").
// Future tests covering manifest-mismatch and multi-plugin scenarios
// will exercise non-"plug-A" values.
//
//nolint:unparam // see comment above; helper is intentionally parametric.
func newTestHostWithEmitter(t *testing.T, bus *eventbustest.Embedded, pluginName string, manifest *plugins.Manifest) *Host {
	t.Helper()
	publisher := bus.Bus.Publisher()
	require.NotNil(t, publisher)
	emitter := plugins.NewPluginEventEmitter(
		publisher,
		func(name string) *plugins.Manifest {
			if name == pluginName {
				return manifest
			}
			return nil
		},
		func(ctx context.Context, _ string) (core.Actor, error) {
			actor, ok := core.ActorFromContext(ctx)
			if !ok {
				return core.Actor{}, oops.New("plugin event actor missing from context")
			}
			return actor, nil
		},
	)
	h := NewHost()
	h.SetEventEmitter(emitter)
	return h
}

// TestEmitEventUsesStoredActorIgnoringPluginClaim covers the load-bearing
// G1 invariant (spec §3.3.5): a plugin substituting the actor-kind/id
// headers but ferrying a valid token MUST get an event stamped with the
// host's stored actor, NOT the plugin's claim.
func TestEmitEventUsesStoredActorIgnoringPluginClaim(t *testing.T) {
	t.Parallel()
	bus := eventbustest.New(t)
	manifest := &plugins.Manifest{
		Name:                "plug-A",
		Type:                plugins.TypeBinary,
		Emits:               []string{"location"},
		ActorKindsClaimable: []string{"plugin", "character"},
	}
	h := newTestHostWithEmitter(t, bus, "plug-A", manifest)
	defer func() { _ = h.Close(context.Background()) }()

	// Construct the server struct directly (we're in package goplugin).
	s := &pluginHostServiceServer{host: h, pluginName: "plug-A"}

	// Host issues a token storing the legitimate dispatching character.
	storedID := "01HCHAR0000000000000000000"
	storedActor := core.Actor{Kind: core.ActorCharacter, ID: storedID}
	token, err := h.tokenStore.Issue("plug-A", storedActor)
	require.NoError(t, err)
	defer h.tokenStore.Revoke(token)

	// Plugin (in test simulation) substitutes a forged actor-kind/id claim
	// but ferries the valid token.
	forgedID := "01HFAKE0000000000000000000"
	md := metadata.New(map[string]string{
		"x-holomush-emit-token": token,
		"x-holomush-actor-kind": strconv.Itoa(int(pluginsdk.ActorCharacter)),
		"x-holomush-actor-id":   forgedID,
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err = s.EmitEvent(ctx, &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    "location.01HLOC0000000000000000000",
		EventType: "say",
		Payload:   []byte(`{"message":"hi"}`),
	})
	require.NoError(t, err)

	// Inspect the published event via the embedded bus.
	msgs := drainEventbusStream(t, bus.JS)
	require.Len(t, msgs, 1)
	assert.Equal(t, "character", msgs[0].Header.Get(eventbus.HeaderActorKind))
	assert.Equal(t, storedID, msgs[0].Header.Get(eventbus.HeaderActorID),
		"event MUST carry the host-stored actor, not the plugin's claim")
	assert.NotEqual(t, forgedID, msgs[0].Header.Get(eventbus.HeaderActorID))
}

// TestEmitEventMissingTokenFails covers spec §3.3.5 EMIT_TOKEN_MISSING.
func TestEmitEventMissingTokenFails(t *testing.T) {
	t.Parallel()
	bus := eventbustest.New(t)
	manifest := &plugins.Manifest{
		Name:                "plug-A",
		Type:                plugins.TypeBinary,
		Emits:               []string{"location"},
		ActorKindsClaimable: []string{"plugin", "character"},
	}
	h := newTestHostWithEmitter(t, bus, "plug-A", manifest)
	defer func() { _ = h.Close(context.Background()) }()
	s := &pluginHostServiceServer{host: h, pluginName: "plug-A"}

	md := metadata.New(map[string]string{}) // no token
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := s.EmitEvent(ctx, &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    "location:01HLOC0000000000000000000",
		EventType: "say",
		Payload:   []byte(`{}`),
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_MISSING")

	// And no event was published.
	assert.Empty(t, drainEventbusStream(t, bus.JS))
}

// TestEmitEventUnknownTokenFails covers EMIT_TOKEN_REJECTED for an
// unrecognized token (e.g., expired & swept, or fabricated by a plugin).
func TestEmitEventUnknownTokenFails(t *testing.T) {
	t.Parallel()
	bus := eventbustest.New(t)
	manifest := &plugins.Manifest{
		Name:                "plug-A",
		Type:                plugins.TypeBinary,
		Emits:               []string{"location"},
		ActorKindsClaimable: []string{"plugin", "character"},
	}
	h := newTestHostWithEmitter(t, bus, "plug-A", manifest)
	defer func() { _ = h.Close(context.Background()) }()
	s := &pluginHostServiceServer{host: h, pluginName: "plug-A"}

	md := metadata.New(map[string]string{
		"x-holomush-emit-token": "not-a-real-token",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := s.EmitEvent(ctx, &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    "location:01HLOC0000000000000000000",
		EventType: "test",
		Payload:   []byte(`{}`),
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_REJECTED")

	assert.Empty(t, drainEventbusStream(t, bus.JS))
}

// TestEmitEventCrossPluginTokenLeakFails covers cross-plugin defense:
// plug-A's token used by plug-B's server → reject. This is the headline
// G1 security guarantee — pluginName tagging in the token store catches
// any future host bug that lets plugin A's gRPC client invoke plugin B's
// server.
func TestEmitEventCrossPluginTokenLeakFails(t *testing.T) {
	t.Parallel()
	bus := eventbustest.New(t)
	manifestA := &plugins.Manifest{
		Name:                "plug-A",
		Type:                plugins.TypeBinary,
		Emits:               []string{"location"},
		ActorKindsClaimable: []string{"plugin", "character"},
	}
	h := newTestHostWithEmitter(t, bus, "plug-A", manifestA)
	defer func() { _ = h.Close(context.Background()) }()

	// Issue a token for plug-A.
	tok, err := h.tokenStore.Issue("plug-A", core.Actor{Kind: core.ActorCharacter, ID: "01HCHAR0000000000000000000"})
	require.NoError(t, err)

	// Invoke EmitEvent on plug-B's server (different pluginName). Note
	// that plug-B is NOT loaded in the host — its manifest lookup would
	// return nil — but the token check fires FIRST, so the manifest gate
	// never runs.
	sB := &pluginHostServiceServer{host: h, pluginName: "plug-B"}
	md := metadata.New(map[string]string{"x-holomush-emit-token": tok})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err = sB.EmitEvent(ctx, &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    "location:01HLOC0000000000000000000",
		EventType: "test",
		Payload:   []byte(`{}`),
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_REJECTED")

	assert.Empty(t, drainEventbusStream(t, bus.JS))
}

// TestRequestEmitTokenIssuesSelfTokenBoundToPluginActor covers the
// happy path: a plugin-served gRPC handler requests a self-token and
// receives one bound to {ActorPlugin, <pluginName-resolved-ULID>}.
// (Spec §3.3.5 self-token pattern; post-w9ml: actor IDs are ULID strings,
// resolved via the IdentityRegistry — the strict gate at
// event_emitter.go::Emit rejects non-ULID actor IDs.)
func TestRequestEmitTokenIssuesSelfTokenBoundToPluginActor(t *testing.T) {
	t.Parallel()
	plugAID := core.NewULID()
	reg := &stubIdentityRegistry{idsByName: map[string]ulid.ULID{"plug-A": plugAID}}
	h := NewHost(WithIdentityRegistry(reg))
	defer func() { _ = h.Close(context.Background()) }()
	s := &pluginHostServiceServer{host: h, pluginName: "plug-A"}

	resp, err := s.RequestEmitToken(context.Background(), &pluginv1.PluginHostServiceRequestEmitTokenRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetToken(), "self-token must be non-empty")

	// The returned token MUST resolve via the host's tokenStore to an
	// actor pinned to {ActorPlugin, <plugin-ULID-string>}. This is the
	// load-bearing G1 invariant for the self-token path: the plugin
	// cannot escalate to a character or system actor through this RPC.
	storedActor, ok := h.tokenStore.Lookup("plug-A", resp.GetToken())
	require.True(t, ok, "issued token must resolve in the host's tokenStore")
	assert.Equal(t, core.ActorPlugin, storedActor.Kind)
	assert.Equal(t, plugAID.String(), storedActor.ID,
		"actor ID MUST be the registry-resolved ULID, not the plugin name")
}

// TestRequestEmitTokenAlwaysHardcodesActorPluginAndPluginULID guards
// against future spec drift: regardless of any caller-supplied input the
// RPC MUST bind the token to {ActorPlugin, <registry-resolved-ULID>}.
// The request message is intentionally empty today; this test makes the
// hardcoded binding contract explicit so a future "extend the request"
// patch can't silently re-open the G1 forgery surface.
func TestRequestEmitTokenAlwaysHardcodesActorPluginAndPluginULID(t *testing.T) {
	t.Parallel()
	plugAID := core.NewULID()
	plugBID := core.NewULID()
	reg := &stubIdentityRegistry{idsByName: map[string]ulid.ULID{
		"plug-A": plugAID,
		"plug-B": plugBID,
	}}
	h := NewHost(WithIdentityRegistry(reg))
	defer func() { _ = h.Close(context.Background()) }()
	s := &pluginHostServiceServer{host: h, pluginName: "plug-B"}

	resp, err := s.RequestEmitToken(context.Background(), &pluginv1.PluginHostServiceRequestEmitTokenRequest{})
	require.NoError(t, err)

	storedActor, ok := h.tokenStore.Lookup("plug-B", resp.GetToken())
	require.True(t, ok)
	assert.Equal(t, core.ActorPlugin, storedActor.Kind,
		"actor kind MUST be ActorPlugin regardless of any future request fields")
	assert.Equal(t, plugBID.String(), storedActor.ID,
		"actor ID MUST be the registry-resolved ULID for the mTLS-bound plugin name, not a caller-supplied value")

	// Cross-plugin defense intact: a sibling server with a different
	// pluginName MUST NOT be able to use this token.
	_, leaked := h.tokenStore.Lookup("plug-A", resp.GetToken())
	assert.False(t, leaked, "plug-B's self-token MUST NOT resolve under plug-A")
}

// TestRequestEmitTokenReturnsErrorWhenHostIsNil covers the defensive
// nil-host check on the handler.
func TestRequestEmitTokenReturnsErrorWhenHostIsNil(t *testing.T) {
	t.Parallel()
	s := &pluginHostServiceServer{host: nil, pluginName: "plug-A"}

	_, err := s.RequestEmitToken(context.Background(), &pluginv1.PluginHostServiceRequestEmitTokenRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin host service is not configured")
}

// TestRequestEmitTokenWrapsIssueFailure verifies that a token-store Issue
// failure (e.g., crypto/rand exhaustion) surfaces as a structured error
// with the EMIT_TOKEN_ISSUE_FAILED code so callers can distinguish it
// from EMIT_TOKEN_REJECTED at the EmitEvent boundary.
func TestRequestEmitTokenWrapsIssueFailure(t *testing.T) {
	t.Parallel()
	plugAID := core.NewULID()
	reg := &stubIdentityRegistry{idsByName: map[string]ulid.ULID{"plug-A": plugAID}}
	h := NewHost(WithIdentityRegistry(reg))
	defer func() { _ = h.Close(context.Background()) }()
	// Inject a failing rand source on the tokenStore so Issue returns
	// an error from the crypto path.
	h.tokenStore.rand = failingReader{}
	s := &pluginHostServiceServer{host: h, pluginName: "plug-A"}

	_, err := s.RequestEmitToken(context.Background(), &pluginv1.PluginHostServiceRequestEmitTokenRequest{})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_ISSUE_FAILED")
}

// TestRequestEmitTokenFailsWhenPluginNotInIdentityRegistry covers the
// new failure mode introduced in w9ml: if the plugin name does not
// resolve to a ULID via the IdentityRegistry, the RPC MUST refuse to
// issue a token rather than fall back to the plugin name (which would
// fail downstream at event_emitter.go::Emit with ACTOR_ID_NOT_ULID).
func TestRequestEmitTokenFailsWhenPluginNotInIdentityRegistry(t *testing.T) {
	t.Parallel()
	reg := &stubIdentityRegistry{idsByName: map[string]ulid.ULID{}} // empty
	h := NewHost(WithIdentityRegistry(reg))
	defer func() { _ = h.Close(context.Background()) }()
	s := &pluginHostServiceServer{host: h, pluginName: "plug-A"}

	_, err := s.RequestEmitToken(context.Background(), &pluginv1.PluginHostServiceRequestEmitTokenRequest{})
	require.Error(t, err)
	// The wrapped cause is PLUGIN_UNREGISTERED_INVOKE from stampPluginActor;
	// oops surfaces the innermost code via Code(). The outer
	// EMIT_TOKEN_ISSUE_FAILED still appears in the error chain via the
	// rendered message; we assert both signals.
	errutil.AssertErrorCode(t, err, "PLUGIN_UNREGISTERED_INVOKE")
	assert.Contains(t, err.Error(), "plugin not registered in IdentityRegistry")
}

// capturingEmitter records EmitIntent calls for assertion in tests.
type capturingEmitter struct {
	intents []pluginsdk.EmitIntent
	err     error
}

func (c *capturingEmitter) Emit(_ context.Context, _ string, intent pluginsdk.EmitIntent) error {
	if c.err != nil {
		return c.err
	}
	c.intents = append(c.intents, intent)
	return nil
}

var _ plugins.PluginIntentEmitter = (*capturingEmitter)(nil)

// newTestHostServiceServerWithEmitter constructs a pluginHostServiceServer
// with a host that uses the provided custom emitter (for direct intent inspection).
func newTestHostServiceServerWithEmitter(pluginName string, emitter plugins.PluginIntentEmitter) *pluginHostServiceServer {
	h := NewHost()
	h.SetEventEmitter(emitter)
	return &pluginHostServiceServer{host: h, pluginName: pluginName}
}

// contextWithValidToken returns a context with a valid emit token for the given actor.
// The token store is accessed from the provided server's host.
func contextWithValidToken(t *testing.T, srv *pluginHostServiceServer, actor core.Actor) (context.Context, string) {
	t.Helper()
	token, err := srv.host.tokenStore.Issue(srv.pluginName, actor)
	require.NoError(t, err, "failed to issue emit token")
	return metadata.NewIncomingContext(
		context.Background(),
		metadata.New(map[string]string{"x-holomush-emit-token": token}),
	), token
}

// TestEmitEventCopiesSensitiveTrue asserts that req.Sensitive=true is
// copied to EmitIntent.Sensitive=true at the host service boundary.
func TestEmitEventCopiesSensitiveTrue(t *testing.T) {
	t.Parallel()
	captured := &capturingEmitter{}
	srv := newTestHostServiceServerWithEmitter("plug-A", captured)
	defer func() { _ = srv.host.Close(context.Background()) }()

	actor := core.Actor{Kind: core.ActorCharacter, ID: "01HCHAR0000000000000000000"}
	ctx, token := contextWithValidToken(t, srv, actor)
	defer srv.host.tokenStore.Revoke(token)

	_, err := srv.EmitEvent(ctx, &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    "location:01HLOC0000000000000000000",
		EventType: "say",
		Payload:   []byte(`{"message":"hi"}`),
		Sensitive: true,
	})
	require.NoError(t, err)
	require.Len(t, captured.intents, 1)
	assert.True(t, captured.intents[0].Sensitive,
		"req.Sensitive=true MUST translate to EmitIntent.Sensitive=true")
}

// TestEmitEventCopiesSensitiveFalseDefaultsExplicit asserts the
// proto3 zero (sensitive absent / false) → EmitIntent.Sensitive=false.
func TestEmitEventCopiesSensitiveFalseDefaultsExplicit(t *testing.T) {
	t.Parallel()
	captured := &capturingEmitter{}
	srv := newTestHostServiceServerWithEmitter("plug-A", captured)
	defer func() { _ = srv.host.Close(context.Background()) }()

	actor := core.Actor{Kind: core.ActorCharacter, ID: "01HCHAR0000000000000000000"}
	ctx, token := contextWithValidToken(t, srv, actor)
	defer srv.host.tokenStore.Revoke(token)

	_, err := srv.EmitEvent(ctx, &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    "location:01HLOC0000000000000000000",
		EventType: "say",
		Payload:   []byte(`{"message":"hi"}`),
		// Sensitive omitted (proto3 zero = false)
	})
	require.NoError(t, err)
	require.Len(t, captured.intents, 1)
	assert.False(t, captured.intents[0].Sensitive,
		"req.Sensitive absent MUST translate to EmitIntent.Sensitive=false")
}

// failingReader implements io.Reader returning a deterministic error,
// used to simulate crypto/rand exhaustion on token issuance.
type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) {
	return 0, errFailingReader
}

var errFailingReader = oops.Errorf("simulated rand exhaustion")

// TestSetConnectionFocus_DelegatesToCoordinator verifies that a successful
// SetConnectionFocus RPC delegates to the focus coordinator and returns the
// expected wire response. Delta driving is now owned by the coordinator.
func TestSetConnectionFocus_DelegatesToCoordinator(t *testing.T) {
	t.Parallel()

	connID := ulid.Make()
	sceneID := ulid.Make()
	locID := ulid.Make()

	fc := &stubCoordinator{
		setConnFocusResult: focus.SetConnectionFocusResult{
			OldFocusKey:    nil, // grid → scene transition
			SessionID:      "sess-delta",
			CharLocationID: locID,
		},
	}
	srv := newTestServer(fc, nil)

	connIDBuf := connID.Bytes()
	resp, err := srv.SetConnectionFocus(context.Background(), &pluginv1.PluginHostServiceSetConnectionFocusRequest{
		ConnectionId: connIDBuf[:],
		FocusKey: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: sceneID.String(),
		},
		IsSceneGrid: false,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	// The response echoes the new focus key back.
	require.NotNil(t, resp.GetFocusKey())
	assert.Equal(t, pluginv1.FocusKind_FOCUS_KIND_SCENE, resp.GetFocusKey().GetKind())
	assert.Equal(t, sceneID.String(), resp.GetFocusKey().GetTargetId())
}

// TestAutoFocusOnJoin_DelegatesToCoordinator verifies that a successful
// AutoFocusOnJoin RPC delegates to the focus coordinator and returns the
// expected connection count and IDs. Delta driving is now owned by the coordinator.
func TestAutoFocusOnJoin_DelegatesToCoordinator(t *testing.T) {
	t.Parallel()

	connID := ulid.Make()

	fc := &stubCoordinator{
		autoFocusResult: focus.AutoFocusOnJoinResponse{
			SessionID:            "sess-1",
			FocusedConnectionIDs: []ulid.ULID{connID},
			TotalConnectionCount: 1,
		},
	}
	srv := newTestServer(fc, nil)

	charID := ulid.Make()
	charIDBuf := charID.Bytes()
	resp, err := srv.AutoFocusOnJoin(context.Background(), &pluginv1.PluginHostServiceAutoFocusOnJoinRequest{
		CharacterId: charIDBuf[:],
		SceneId:     ulid.Make().Bytes(),
	})
	require.NoError(t, err)
	assert.Equal(t, uint32(1), resp.GetTotalConnectionCount())
	require.Len(t, resp.GetFocusedConnectionIds(), 1)
}

// TestIsAnyConnFocused_PassthroughBool verifies the simple bool passthrough
// for IsAnyConnFocused: the RPC returns the coordinator's bool result.
func TestIsAnyConnFocused_PassthroughBool(t *testing.T) {
	t.Parallel()

	charID := ulid.Make()
	sceneID := ulid.Make()

	for _, tc := range []struct {
		name   string
		result bool
	}{
		{"true_when_focused", true},
		{"false_when_not_focused", false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fc := &stubCoordinator{isAnyFocusedResult: tc.result}
			srv := newTestServer(fc, nil)

			charIDBuf := charID.Bytes()
			sceneIDBuf := sceneID.Bytes()
			resp, err := srv.IsAnyConnFocused(context.Background(), &pluginv1.PluginHostServiceIsAnyConnFocusedRequest{
				CharacterId: charIDBuf[:],
				SceneId:     sceneIDBuf[:],
			})
			require.NoError(t, err)
			assert.Equal(t, tc.result, resp.GetFocused())
		})
	}
}

// TestSetConnectionFocus_InvalidULID verifies INVALID_ULID error on bad connection_id.
func TestSetConnectionFocus_InvalidULID(t *testing.T) {
	t.Parallel()
	fc := &stubCoordinator{}
	srv := newTestServer(fc, nil)

	_, err := srv.SetConnectionFocus(context.Background(), &pluginv1.PluginHostServiceSetConnectionFocusRequest{
		ConnectionId: []byte("not-16-bytes"),
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_ULID")
}

// TestAutoFocusOnJoin_InvalidULID verifies INVALID_ULID error on bad character_id.
func TestAutoFocusOnJoin_InvalidULID(t *testing.T) {
	t.Parallel()
	fc := &stubCoordinator{}
	srv := newTestServer(fc, nil)

	_, err := srv.AutoFocusOnJoin(context.Background(), &pluginv1.PluginHostServiceAutoFocusOnJoinRequest{
		CharacterId: []byte("bad"),
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_ULID")
}

// TestIsAnyConnFocused_InvalidULID verifies INVALID_ULID error on bad character_id.
func TestIsAnyConnFocused_InvalidULID(t *testing.T) {
	t.Parallel()
	fc := &stubCoordinator{}
	srv := newTestServer(fc, nil)

	_, err := srv.IsAnyConnFocused(context.Background(), &pluginv1.PluginHostServiceIsAnyConnFocusedRequest{
		CharacterId: []byte("bad"),
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_ULID")
}

// TestAutoFocusOnJoin_ReturnsResponseShape verifies focused/skipped/failed/total
// are all populated in the proto response.
func TestAutoFocusOnJoin_ReturnsResponseShape(t *testing.T) {
	t.Parallel()

	focusedID := ulid.Make()
	skippedID := ulid.Make()
	failedID := ulid.Make()
	charID := ulid.Make()
	sceneID := ulid.Make()

	fc := &stubCoordinator{
		autoFocusResult: focus.AutoFocusOnJoinResponse{
			SessionID:            "sess-shape",
			TotalConnectionCount: 3,
			FocusedConnectionIDs: []ulid.ULID{focusedID},
			SkippedConnectionIDs: []ulid.ULID{skippedID},
			FailedConnectionIDs: []focus.AutoFocusFailure{
				{ConnectionID: failedID, Reason: "membership_absent"},
			},
		},
	}
	srv := newTestServer(fc, nil) // nil coordinator extras — best-effort skip

	charIDBuf := charID.Bytes()
	sceneIDBuf := sceneID.Bytes()
	resp, err := srv.AutoFocusOnJoin(context.Background(), &pluginv1.PluginHostServiceAutoFocusOnJoinRequest{
		CharacterId: charIDBuf[:],
		SceneId:     sceneIDBuf[:],
	})
	require.NoError(t, err)
	assert.Equal(t, uint32(3), resp.GetTotalConnectionCount())
	require.Len(t, resp.GetFocusedConnectionIds(), 1)
	require.Len(t, resp.GetSkippedConnectionIds(), 1)
	require.Len(t, resp.GetFailedConnectionIds(), 1)
	assert.Equal(t, pluginv1.FocusFailureReason_FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT,
		resp.GetFailedConnectionIds()[0].GetReason())
}

// recordingEngine satisfies types.AccessPolicyEngine, records the AccessRequest
// it receives, and returns an allow decision. Used to assert that Evaluate
// derives the subject from the dispatch token rather than from plugin-supplied
// fields.
type recordingEngine struct {
	gotReq types.AccessRequest
	called bool
}

func (e *recordingEngine) Evaluate(_ context.Context, req types.AccessRequest) (types.Decision, error) {
	e.called = true
	e.gotReq = req
	return types.NewDecision(types.EffectAllow, "ok", "p"), nil
}

func (e *recordingEngine) CanPerformAction(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}

var _ types.AccessPolicyEngine = (*recordingEngine)(nil)

// newTestHostWithEngine constructs a Host with the given engine and a loadedPlugin
// entry for pluginName so ownedResourceTypes can look it up.
func newTestHostWithEngine(t *testing.T, pluginName string, m *plugins.Manifest, eng types.AccessPolicyEngine) *Host {
	t.Helper()
	h := NewHost(WithEngine(eng))
	h.mu.Lock()
	h.plugins[pluginName] = &loadedPlugin{manifest: m}
	h.mu.Unlock()
	return h
}

// TestEvaluateDerivesSubjectFromToken verifies that Evaluate recovers the
// actor from the dispatch token and passes the derived subject to the engine,
// not any plugin-supplied value (security invariant: subject from token only).
func TestEvaluateDerivesSubjectFromToken(t *testing.T) {
	t.Parallel()
	eng := &recordingEngine{}
	manifest := &plugins.Manifest{
		Name:          "core-scenes",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"scene"},
	}
	h := newTestHostWithEngine(t, "core-scenes", manifest, eng)
	defer func() { _ = h.Close(context.Background()) }()
	srv := &pluginHostServiceServer{host: h, pluginName: "core-scenes"}

	charID := core.NewULID()
	ctx, token := contextWithValidToken(t, srv, core.Actor{Kind: core.ActorCharacter, ID: charID.String()})
	defer h.tokenStore.Revoke(token)

	resp, err := srv.Evaluate(ctx, &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   "extend_publish_attempts",
		Resource: "scene:01SCENE0000000000000000000",
	})
	require.NoError(t, err)
	assert.True(t, resp.GetAllowed())
	assert.True(t, eng.called)
	assert.Equal(t, "character:"+charID.String(), eng.gotReq.Subject)
	assert.Equal(t, "extend_publish_attempts", eng.gotReq.Action)
	assert.Equal(t, "scene:01SCENE0000000000000000000", eng.gotReq.Resource)
}

// TestEvaluateMissingTokenFailsClosed verifies that Evaluate fails closed
// when no dispatch token is present in the incoming metadata.
func TestEvaluateMissingTokenFailsClosed(t *testing.T) {
	t.Parallel()
	manifest := &plugins.Manifest{
		Name:          "core-scenes",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"scene"},
	}
	h := newTestHostWithEngine(t, "core-scenes", manifest, policytest.AllowAllEngine())
	defer func() { _ = h.Close(context.Background()) }()
	srv := &pluginHostServiceServer{host: h, pluginName: "core-scenes"}

	_, err := srv.Evaluate(context.Background(), &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   "read",
		Resource: "scene:01SCENE0000000000000000000",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_MISSING")
}

// TestEvaluateForeignResourceTypeRejected verifies that Evaluate rejects
// requests for resource types the plugin does not own.
func TestEvaluateForeignResourceTypeRejected(t *testing.T) {
	t.Parallel()
	manifest := &plugins.Manifest{
		Name:          "core-scenes",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"scene"},
	}
	h := newTestHostWithEngine(t, "core-scenes", manifest, policytest.AllowAllEngine())
	defer func() { _ = h.Close(context.Background()) }()
	srv := &pluginHostServiceServer{host: h, pluginName: "core-scenes"}
	ctx, token := contextWithValidToken(t, srv, core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()})
	defer h.tokenStore.Revoke(token)

	_, err := srv.Evaluate(ctx, &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   "read",
		Resource: "server:global",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVALUATE_UNENTITLED_TYPE")
}

// TestEvaluateEngineUnconfiguredFailsClosed verifies that Evaluate fails closed
// when no access policy engine has been configured on the host.
func TestEvaluateEngineUnconfiguredFailsClosed(t *testing.T) {
	t.Parallel()
	// Build a host with no engine (WithEngine never called → engine is nil).
	h := NewHost()
	h.mu.Lock()
	h.plugins["core-scenes"] = &loadedPlugin{manifest: &plugins.Manifest{
		Name: "core-scenes", Type: plugins.TypeBinary, ResourceTypes: []string{"scene"},
	}}
	h.mu.Unlock()
	defer func() { _ = h.Close(context.Background()) }()
	srv := &pluginHostServiceServer{host: h, pluginName: "core-scenes"}

	ctx, _ := contextWithValidToken(t, srv, core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()})

	_, err := srv.Evaluate(ctx, &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   "read",
		Resource: "scene:01SCENE0000000000000000000",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVALUATE_ENGINE_UNCONFIGURED")
}

// TestEvaluateNilHostFailsClosed verifies that Evaluate fails closed when the
// pluginHostServiceServer has no host (mirrors the nil-host tests for EmitEvent,
// JoinFocus, etc.).
func TestEvaluateNilHostFailsClosed(t *testing.T) {
	t.Parallel()
	srv := &pluginHostServiceServer{host: nil, pluginName: "core-scenes"}

	_, err := srv.Evaluate(context.Background(), &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   "read",
		Resource: "scene:01SCENE0000000000000000000",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin host service is not configured")
}

// TestEvaluateNilTokenStoreFailsClosed verifies that Evaluate returns
// EMIT_TOKEN_STORE_UNCONFIGURED when the host's tokenStore is nil.
// This fires when a host is constructed and the token store is cleared.
func TestEvaluateNilTokenStoreFailsClosed(t *testing.T) {
	t.Parallel()
	manifest := &plugins.Manifest{
		Name:          "core-scenes",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"scene"},
	}
	h := newTestHostWithEngine(t, "core-scenes", manifest, policytest.AllowAllEngine())

	// Inject a token into metadata manually (bypassing Issue so we don't need the store).
	md := metadata.New(map[string]string{"x-holomush-emit-token": "any-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	// Clear the tokenStore after constructing the host. Stop the sweeper goroutine
	// first so h.Close() doesn't race with the nil write, then skip calling Close
	// (tokenStore is nil and h.Close calls tokenStore.Close which would panic).
	h.tokenStoreCancel()
	h.mu.Lock()
	h.tokenStore = nil
	h.mu.Unlock()

	srv := &pluginHostServiceServer{host: h, pluginName: "core-scenes"}
	_, err := srv.Evaluate(ctx, &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   "read",
		Resource: "scene:01SCENE0000000000000000000",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_STORE_UNCONFIGURED")
}

// TestEvaluateRejectedTokenFailsClosed verifies that Evaluate returns
// EMIT_TOKEN_REJECTED when the dispatch token was issued for a different plugin.
// Mirrors TestEmitEventCrossPluginTokenLeakFails for the Evaluate surface.
func TestEvaluateRejectedTokenFailsClosed(t *testing.T) {
	t.Parallel()
	manifest := &plugins.Manifest{
		Name:          "core-scenes",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"scene"},
	}
	h := newTestHostWithEngine(t, "core-scenes", manifest, policytest.AllowAllEngine())
	defer func() { _ = h.Close(context.Background()) }()

	// Issue a token for plug-A; present it to plug-B's server.
	tok, err := h.tokenStore.Issue("plug-A", core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()})
	require.NoError(t, err)
	defer h.tokenStore.Revoke(tok)

	srvB := &pluginHostServiceServer{host: h, pluginName: "core-scenes"}
	md := metadata.New(map[string]string{"x-holomush-emit-token": tok})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err = srvB.Evaluate(ctx, &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   "read",
		Resource: "scene:01SCENE0000000000000000000",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_REJECTED")
}

// stubReadbackDecryptor records DecryptOwnRow calls and returns a fixed
// per-row result keyed off the row's subject so tests can drive both the
// not_owner refusal path and the happy path without real crypto deps.
type stubReadbackDecryptor struct {
	calls  []stubDecryptCall
	result func(row *pluginv1.AuditRow) *pluginv1.RowResult
}

type stubDecryptCall struct {
	pluginName string
	instanceID string
	subject    string
}

func (s *stubReadbackDecryptor) DecryptOwnRow(_ context.Context, pluginName, instanceID string, row *pluginv1.AuditRow) *pluginv1.RowResult {
	s.calls = append(s.calls, stubDecryptCall{pluginName: pluginName, instanceID: instanceID, subject: row.GetSubject()})
	if s.result != nil {
		return s.result(row)
	}
	return &pluginv1.RowResult{Id: row.GetId()}
}

// DecryptOwnRows mirrors *history.ReadbackDecryptor.DecryptOwnRows: it enforces
// the same maxDecryptBatch REJECT cap on the common path so the handler test
// (TestDecryptOwnAuditRowsCapsBatchAt500) exercises the real cap behavior — an
// over-cap batch is rejected with DECRYPT_BATCH_TOO_LARGE before any row is
// decrypted. The cap value is duplicated here only because this stub stands in
// for the production common-path decryptor in a different package.
const stubMaxDecryptBatch = 500

func (s *stubReadbackDecryptor) DecryptOwnRows(ctx context.Context, pluginName, instanceID string, rows []*pluginv1.AuditRow) ([]*pluginv1.RowResult, error) {
	if len(rows) > stubMaxDecryptBatch {
		return nil, oops.Code("DECRYPT_BATCH_TOO_LARGE").
			With("plugin", pluginName).
			With("count", len(rows)).
			Errorf("decrypt batch exceeds cap %d", stubMaxDecryptBatch)
	}
	results := make([]*pluginv1.RowResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, s.DecryptOwnRow(ctx, pluginName, instanceID, row))
	}
	return results, nil
}

func newDecryptTestServer(pluginName string, dec plugins.ReadbackDecryptor) *pluginHostServiceServer {
	h := &Host{
		plugins:           make(map[string]*loadedPlugin),
		readbackDecryptor: dec,
	}
	return &pluginHostServiceServer{host: h, pluginName: pluginName}
}

// TestDecryptOwnAuditRowsRejectsForeignSubject asserts the handler delegates
// each row to the decryptor and faithfully surfaces a not_owner refusal (the
// g1 OwnerMap gate, exercised end-to-end in package history) without leaking
// plaintext.
func TestDecryptOwnAuditRowsRejectsForeignSubject(t *testing.T) {
	t.Parallel()
	dec := &stubReadbackDecryptor{
		result: func(row *pluginv1.AuditRow) *pluginv1.RowResult {
			// core-scenes asks to decrypt a row whose subject is owned by a
			// DIFFERENT plugin → not_owner, no plaintext.
			return &pluginv1.RowResult{Id: row.GetId(), Outcome: &pluginv1.RowResult_NoPlaintextReason{NoPlaintextReason: "not_owner"}}
		},
	}
	srv := newDecryptTestServer("core-scenes", dec)

	resp, err := srv.DecryptOwnAuditRows(context.Background(), &pluginv1.DecryptOwnAuditRowsRequest{
		Rows: []*pluginv1.AuditRow{
			{Id: []byte("row-1"), Subject: "events.main.channel.01ABC.msg"},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.GetResults(), 1)
	assert.Equal(t, []byte("row-1"), resp.GetResults()[0].GetId())
	assert.Equal(t, "not_owner", resp.GetResults()[0].GetNoPlaintextReason())
	assert.Nil(t, resp.GetResults()[0].GetPlaintext(), "refused row must yield no plaintext")

	require.Len(t, dec.calls, 1)
	assert.Equal(t, "core-scenes", dec.calls[0].pluginName)
	assert.Equal(t, "events.main.channel.01ABC.msg", dec.calls[0].subject)
}

// TestDecryptOwnAuditRowsCapsBatchAt500 asserts the server REJECTS (does not
// clamp) a batch larger than maxDecryptBatch with DECRYPT_BATCH_TOO_LARGE and
// never invokes the decryptor.
func TestDecryptOwnAuditRowsCapsBatchAt500(t *testing.T) {
	t.Parallel()
	dec := &stubReadbackDecryptor{}
	srv := newDecryptTestServer("core-scenes", dec)

	rows := make([]*pluginv1.AuditRow, stubMaxDecryptBatch+1)
	for i := range rows {
		rows[i] = &pluginv1.AuditRow{Id: []byte(strconv.Itoa(i)), Subject: "events.main.scene.01ABC.ic"}
	}

	resp, err := srv.DecryptOwnAuditRows(context.Background(), &pluginv1.DecryptOwnAuditRowsRequest{Rows: rows})
	require.Error(t, err)
	assert.Nil(t, resp, "over-cap batch must be rejected, not partially served")
	errutil.AssertErrorCode(t, err, "DECRYPT_BATCH_TOO_LARGE")
	assert.Empty(t, dec.calls, "REJECT (not clamp): decryptor must not be invoked for an over-cap batch")
}

// TestDecryptOwnAuditRowsReturnsErrorWhenDecryptorNil asserts the handler
// fails closed when no read-back decryptor is wired.
func TestDecryptOwnAuditRowsReturnsErrorWhenDecryptorNil(t *testing.T) {
	t.Parallel()
	srv := newDecryptTestServer("core-scenes", nil)

	_, err := srv.DecryptOwnAuditRows(context.Background(), &pluginv1.DecryptOwnAuditRowsRequest{
		Rows: []*pluginv1.AuditRow{{Id: []byte("row-1"), Subject: "events.main.scene.01ABC.ic"}},
	})
	require.Error(t, err)
}
