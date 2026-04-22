// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/pkg/errutil"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// TODO(holomush-l60y): refactor the repetitive TestQueryStreamHistory*
// functions into a single table-driven test. Deferred as it would churn
// every test body simultaneously; tracked as follow-up.

// fakeHistoryReader returns a canned slice (newest-first to match the
// production bus contract) or a pre-seeded error. fetchHistoryFramesFromBus
// reverses the slice internally.
type fakeHistoryReader struct {
	events []eventbus.Event
	err    error
	gotQ   eventbus.HistoryQuery
}

func (f *fakeHistoryReader) QueryHistory(_ context.Context, q eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
	f.gotQ = q
	if f.err != nil {
		return nil, f.err
	}
	return &fakeHistoryStream{events: f.events}, nil
}

type fakeHistoryStream struct {
	events []eventbus.Event
	idx    int
}

func (f *fakeHistoryStream) Next(_ context.Context) (eventbus.Event, error) {
	if f.idx >= len(f.events) {
		return eventbus.Event{}, io.EOF
	}
	e := f.events[f.idx]
	f.idx++
	return e, nil
}

func (f *fakeHistoryStream) Close() error { return nil }

// sceneFocusKey returns a FocusMembership-aligned scene stream name and the
// matching FocusMembership the session needs to pass the I-17 gate.
func sceneFocusMembership(t *testing.T) (string, session.FocusMembership) {
	t.Helper()
	sceneID := ulid.MustParse("01HYXSCENE00000000000000CC")
	return "scene:" + sceneID.String() + ":ic", session.FocusMembership{
		Kind:     session.FocusKindScene,
		TargetID: sceneID,
	}
}

// newQueryStreamHistoryServer builds a CoreServer with the given history
// reader + session store for unit-testing QueryStreamHistory.
func newQueryStreamHistoryServer(t *testing.T, reader eventbus.HistoryReader, sess session.Store) *CoreServer {
	t.Helper()
	return &CoreServer{
		sessionStore:  sess,
		historyReader: reader,
		accessEngine:  policytest.AllowAllEngine(),
	}
}

func TestQueryStreamHistoryRejectsMissingHistoryReader(t *testing.T) {
	t.Parallel()
	s := &CoreServer{sessionStore: newTestSessionStore(t, nil)}
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "sess-1",
		Stream:    "location:01HYXYZ0C0000000000000000C",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INTERNAL")
}

func TestQueryStreamHistoryRejectsMissingSessionID(t *testing.T) {
	t.Parallel()
	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, newTestSessionStore(t, nil))
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		Stream: "location:x",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_ARGUMENT")
}

func TestQueryStreamHistoryReturnsSessionNotFound(t *testing.T) {
	t.Parallel()
	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, newTestSessionStore(t, nil))
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "unknown",
		Stream:    "location:01HYXYZ0C0000000000000000C",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}

func TestQueryStreamHistoryReturnsSessionExpired(t *testing.T) {
	t.Parallel()
	past := time.Now().Add(-time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"e1": {ID: "e1", ExpiresAt: &past},
	})
	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "e1",
		Stream:    "location:01HYXYZ0C0000000000000000C",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_EXPIRED")
}

func TestQueryStreamHistoryRejectsEmptyStream(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_ARGUMENT")
}

func TestQueryStreamHistoryRejectsNegativeCount(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location:01HYXYZ0C0000000000000000C",
		Count:     -1,
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_ARGUMENT")
}

func TestQueryStreamHistoryRejectsMalformedBeforeID(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location:01HYXYZ0C0000000000000000C",
		BeforeId:  "not-a-ulid",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_ARGUMENT")
}

func TestQueryStreamHistoryRejectsMalformedSceneStream(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "scene:not-a-ulid:ic",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_ARGUMENT")
}

func TestQueryStreamHistoryEnforcesMembershipGateForPrivateStream(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	charID := ulid.MustParse("01HYXYZCHAR0000000000000CH")
	otherID := ulid.MustParse("01HYXYZOTHER000000000000CH")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {
			ID: "s1", CharacterID: charID, ExpiresAt: &future,
		},
	})
	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, sess)
	// character stream for a DIFFERENT character → membership denied.
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "character:" + otherID.String(),
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")
}

func TestQueryStreamHistoryAllowsOwnerOfPrivateCharacterStream(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	charID := ulid.MustParse("01HYXYZCHAR0000000000000CH")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", CharacterID: charID, ExpiresAt: &future},
	})
	reader := &fakeHistoryReader{events: []eventbus.Event{{
		ID:        core.NewULID(),
		Subject:   eventbus.Subject("events.main.character." + charID.String()),
		Type:      "scene.pose",
		Timestamp: time.Now(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte("p"),
	}}}
	s := newQueryStreamHistoryServer(t, reader, sess)
	resp, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "character:" + charID.String(),
		Count:     10,
	})
	require.NoError(t, err)
	assert.Len(t, resp.GetEvents(), 1)
	assert.False(t, resp.GetHasMore())
}

func TestQueryStreamHistoryAllowsSceneMemberForScenePrivateStream(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	charID := ulid.MustParse("01HYXYZCHAR0000000000000CH")
	sceneStream, fm := sceneFocusMembership(t)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {
			ID:               "s1",
			CharacterID:      charID,
			ExpiresAt:        &future,
			FocusMemberships: []session.FocusMembership{fm},
		},
	})
	reader := &fakeHistoryReader{}
	s := newQueryStreamHistoryServer(t, reader, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    sceneStream,
		Count:     10,
	})
	require.NoError(t, err)
}

func TestQueryStreamHistoryDenysSceneNonMember(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	charID := ulid.MustParse("01HYXYZCHAR0000000000000CH")
	sceneStream, _ := sceneFocusMembership(t)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {
			ID:          "s1",
			CharacterID: charID,
			ExpiresAt:   &future,
			// No FocusMemberships.
		},
	})
	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    sceneStream,
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")
}

func TestQueryStreamHistoryRejectsPublicStreamWithoutAccessEngine(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	// No access engine wired.
	s := &CoreServer{
		sessionStore:  sess,
		historyReader: &fakeHistoryReader{},
	}
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location:01HYXYZ0C0000000000000000C",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")
}

func TestQueryStreamHistoryDeniedByABAC(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	s := &CoreServer{
		sessionStore:  sess,
		historyReader: &fakeHistoryReader{},
		accessEngine:  policytest.DenyAllEngine(),
	}
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location:01HYXYZ0C0000000000000000C",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")
}

func TestQueryStreamHistoryBusErrorSurfacesAsInternal(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	reader := &fakeHistoryReader{err: errors.New("bus down")}
	s := newQueryStreamHistoryServer(t, reader, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location:01HYXYZ0C0000000000000000C",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INTERNAL")
}

func TestQueryStreamHistoryHasMoreReflectsCountPlusOne(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	// 4 events, count=3 → has_more=true; response trims to 3 (newest).
	evts := make([]eventbus.Event, 0, 4)
	for i := 0; i < 4; i++ {
		evts = append(evts, eventbus.Event{
			ID:        core.NewULID(),
			Subject:   eventbus.Subject("events.main.location.01HYXYZ0C0000000000000000C"),
			Type:      "scene.pose",
			Timestamp: time.Now(),
			Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
			Payload:   []byte("p"),
		})
	}
	reader := &fakeHistoryReader{events: evts}
	s := newQueryStreamHistoryServer(t, reader, sess)
	resp, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location:01HYXYZ0C0000000000000000C",
		Count:     3,
	})
	require.NoError(t, err)
	assert.True(t, resp.GetHasMore())
	assert.Len(t, resp.GetEvents(), 3)
	// PageSize forwarded with +1.
	assert.Equal(t, 4, reader.gotQ.PageSize)
}

func TestQueryStreamHistoryCountDefaultsWhenZero(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	reader := &fakeHistoryReader{}
	s := newQueryStreamHistoryServer(t, reader, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location:01HYXYZ0C0000000000000000C",
		Count:     0,
	})
	require.NoError(t, err)
	assert.Equal(t, defaultHistoryPageSize+1, reader.gotQ.PageSize)
}

func TestQueryStreamHistoryCountCappedAtMax(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	reader := &fakeHistoryReader{}
	s := newQueryStreamHistoryServer(t, reader, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location:01HYXYZ0C0000000000000000C",
		Count:     99_999,
	})
	require.NoError(t, err)
	assert.Equal(t, maxHistoryPageSize+1, reader.gotQ.PageSize)
}

func TestQueryStreamHistoryBeforeIDForwardsToBus(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	before := core.NewULID()
	reader := &fakeHistoryReader{}
	s := newQueryStreamHistoryServer(t, reader, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location:01HYXYZ0C0000000000000000C",
		BeforeId:  before.String(),
		Count:     5,
	})
	require.NoError(t, err)
	assert.Equal(t, before, reader.gotQ.BeforeID)
}

func TestQueryStreamHistoryNotBeforeMsForwardsToBus(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	notBefore := time.Now().Add(-2 * time.Hour).UnixMilli()
	reader := &fakeHistoryReader{}
	s := newQueryStreamHistoryServer(t, reader, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId:   "s1",
		Stream:      "location:01HYXYZ0C0000000000000000C",
		NotBeforeMs: notBefore,
		Count:       5,
	})
	require.NoError(t, err)
	// NotBefore is threaded into the bus query as UTC time.
	assert.Equal(t, notBefore, reader.gotQ.NotBefore.UnixMilli())
	assert.Equal(t, eventbus.DirectionBackward, reader.gotQ.Direction)
}
