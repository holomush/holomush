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

	"github.com/holomush/holomush/internal/grpc/focus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// ---------- Test fakes ----------

// fakeFocusCoordinator is a minimal focus.Coordinator implementation that
// returns a canned RestorePlan (or error) from RestoreFocus.
type fakeFocusCoordinator struct {
	plan focus.RestorePlan
	err  error
}

func (f *fakeFocusCoordinator) RestoreFocus(_ context.Context, _ string) (focus.RestorePlan, error) {
	return f.plan, f.err
}

func (f *fakeFocusCoordinator) JoinFocus(_ context.Context, _ string, _ session.FocusKey) error {
	panic("unused in list_session_streams_test")
}

func (f *fakeFocusCoordinator) LeaveFocus(_ context.Context, _ string, _ session.FocusKey) error {
	panic("unused in list_session_streams_test")
}

func (f *fakeFocusCoordinator) LeaveFocusByTarget(_ context.Context, _ session.FocusKey) (session.LeaveByTargetResult, error) {
	panic("unused in list_session_streams_test")
}

func (f *fakeFocusCoordinator) PresentFocus(_ context.Context, _ string, _ session.FocusKey) error {
	panic("unused in list_session_streams_test")
}

func (f *fakeFocusCoordinator) IsAnyConnFocused(_ context.Context, _, _ ulid.ULID) (bool, error) {
	panic("unused in list_session_streams_test")
}

func (f *fakeFocusCoordinator) RestoreConnectionFocus(_ context.Context, _ string, _ ulid.ULID) error {
	panic("unused in list_session_streams_test")
}

func (f *fakeFocusCoordinator) SetConnectionFocus(_ context.Context, _ ulid.ULID, _ *session.FocusKey, _ bool) (focus.SetConnectionFocusResult, error) {
	panic("unused in list_session_streams_test")
}

func (f *fakeFocusCoordinator) AutoFocusOnJoin(_ context.Context, _, _ ulid.ULID) (focus.AutoFocusOnJoinResponse, error) {
	panic("unused in list_session_streams_test")
}

func (f *fakeFocusCoordinator) GetConnectionFocus(_ context.Context, _ ulid.ULID) (*session.FocusKey, error) {
	panic("unused in list_session_streams_test")
}

// fakeStreamContributor is a minimal SessionStreamContributor implementation
// that returns a canned stream list.
type fakeStreamContributor struct {
	streams []string
}

func (f *fakeStreamContributor) QuerySessionStreams(_ context.Context, _ plugins.SessionStreamsRequest) []string {
	return f.streams
}

// ---------- Tests ----------

func TestListSessionStreamsRequiresSessionID(t *testing.T) {
	s := &CoreServer{}
	s.buildHandlers()
	_, err := s.ListSessionStreams(context.Background(), &corev1.ListSessionStreamsRequest{})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT", o.Code())
}

// TestListSessionStreamsRejectsMissingToken verifies the ownership gate
// collapses a missing player_session_token to SESSION_NOT_FOUND (bd-jv7z).
func TestListSessionStreamsRejectsMissingToken(t *testing.T) {
	charID := ulid.MustParse("01HYXYZCHAR0000000000000CH")
	future := time.Now().Add(time.Hour)
	info := &session.Info{
		ID:          "sess-1",
		CharacterID: charID,
		ExpiresAt:   &future,
	}
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-1": info}),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.buildHandlers()
	_, err := s.ListSessionStreams(context.Background(), &corev1.ListSessionStreamsRequest{
		SessionId: "sess-1",
		// PlayerSessionToken intentionally omitted.
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", o.Code())
}

func TestListSessionStreamsReturnsSessionNotFoundOnMiss(t *testing.T) {
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, nil),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.buildHandlers()
	_, err := s.ListSessionStreams(context.Background(), &corev1.ListSessionStreamsRequest{
		SessionId:          "missing",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", o.Code())
}

func TestListSessionStreamsReturnsSessionExpiredForExpiredSession(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	expired := &session.Info{
		ID:        "sess-expired",
		ExpiresAt: &past,
	}
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-expired": expired}),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.buildHandlers()
	_, err := s.ListSessionStreams(context.Background(), &corev1.ListSessionStreamsRequest{
		SessionId:          "sess-expired",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_EXPIRED", o.Code())
}

func TestListSessionStreamsReturnsRestoreFocusStreams(t *testing.T) {
	charID := ulid.MustParse("01HYXYZCHAR0000000000000CH")
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	sceneStream := "scene:01HYXSCENE00000000000000CC:ic"
	future := time.Now().Add(time.Hour)
	info := &session.Info{
		ID:          "sess-1",
		CharacterID: charID,
		LocationID:  locID,
		ExpiresAt:   &future,
	}
	fakeCoord := &fakeFocusCoordinator{
		plan: focus.RestorePlan{
			Streams: []focus.StreamWithMode{
				{Stream: world.CharacterStream(charID), Mode: focus.ReplayModeFromCursor},
				{Stream: world.LocationStream(locID), Mode: focus.ReplayModeFromCursor},
				{Stream: sceneStream, Mode: focus.ReplayModeFromCursor},
			},
		},
	}
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-1": info}),
		focusCoordinator:  fakeCoord,
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.buildHandlers()

	resp, err := s.ListSessionStreams(context.Background(), &corev1.ListSessionStreamsRequest{
		SessionId:          "sess-1",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		world.CharacterStream(charID),
		world.LocationStream(locID),
		sceneStream,
	}, resp.GetStreams())
}

func TestListSessionStreamsFallsBackWhenCoordinatorNil(t *testing.T) {
	charID := ulid.MustParse("01HYXYZCHAR0000000000000CH")
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	future := time.Now().Add(time.Hour)
	info := &session.Info{
		ID:          "sess-2",
		CharacterID: charID,
		LocationID:  locID,
		ExpiresAt:   &future,
	}
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-2": info}),
		focusCoordinator:  nil, // explicitly nil
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.buildHandlers()

	resp, err := s.ListSessionStreams(context.Background(), &corev1.ListSessionStreamsRequest{
		SessionId:          "sess-2",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.Contains(t, resp.GetStreams(), world.CharacterStream(charID))
	assert.Contains(t, resp.GetStreams(), world.LocationStream(locID))
}

// TestListSessionStreamsIncludesPluginContributedStreamsInFallback verifies
// the coordinator-nil fallback path matches Subscribe's ambient-stream
// assembly by also querying plugin-contributed streams via
// streamContributor. See server.go:787-816 for the mirrored Subscribe path.
func TestListSessionStreamsIncludesPluginContributedStreamsInFallback(t *testing.T) {
	charID := ulid.MustParse("01HYXYZCHAR0000000000000CH")
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	future := time.Now().Add(time.Hour)
	info := &session.Info{
		ID:          "sess-plugin",
		CharacterID: charID,
		LocationID:  locID,
		ExpiresAt:   &future,
	}
	fakeContrib := &fakeStreamContributor{streams: []string{"plugin:chat:general"}}
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-plugin": info}),
		focusCoordinator:  nil,
		streamContributor: fakeContrib,
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.buildHandlers()

	resp, err := s.ListSessionStreams(context.Background(), &corev1.ListSessionStreamsRequest{
		SessionId:          "sess-plugin",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.Contains(t, resp.GetStreams(), world.CharacterStream(charID))
	assert.Contains(t, resp.GetStreams(), world.LocationStream(locID))
	assert.Contains(t, resp.GetStreams(), "plugin:chat:general")
}

// TestListSessionStreamsEchoesRequestIDInResponseMeta verifies that the
// handler populates ResponseMeta with the incoming RequestId, matching the
// pattern used by QueryStreamHistory for request correlation.
func TestListSessionStreamsEchoesRequestIDInResponseMeta(t *testing.T) {
	charID := ulid.MustParse("01HYXYZCHAR0000000000000CH")
	future := time.Now().Add(time.Hour)
	info := &session.Info{
		ID:          "sess-meta",
		CharacterID: charID,
		ExpiresAt:   &future,
	}
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-meta": info}),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.buildHandlers()

	resp, err := s.ListSessionStreams(context.Background(), &corev1.ListSessionStreamsRequest{
		Meta:               &corev1.RequestMeta{RequestId: "req-abc"},
		SessionId:          "sess-meta",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	require.NotNil(t, resp.GetMeta())
	assert.Equal(t, "req-abc", resp.GetMeta().GetRequestId())
}
