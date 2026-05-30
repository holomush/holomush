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
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/cursor"
	cursorv1 "github.com/holomush/holomush/internal/eventbus/cursor/cursorv1"
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

// sceneFocusMembership returns a FocusMembership-aligned scene stream name and the
// matching FocusMembership the session needs to pass the I-17 gate.
// Stream is dot-style per INV-P4-1 / ADR holomush-s9nu.
func sceneFocusMembership(t *testing.T) (string, session.FocusMembership) {
	t.Helper()
	sceneID := ulid.MustParse("01HYXSCENE00000000000000CC")
	return dotStyleSceneIC(sceneID.String()), session.FocusMembership{
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
		Stream:    "location.01HYXYZ0C0000000000000000C",
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

// I-PRIV-5 wire opacity: missing-session denial MUST collapse to
// STREAM_ACCESS_DENIED on the wire (denial_reason=session_not_found goes to
// slog only). Top-level oops code is asserted per .claude/rules/grpc-errors.md
// to avoid double-wrap chain-walk false positives.
func TestQueryStreamHistoryReturnsStreamAccessDeniedOnUnknownSession(t *testing.T) {
	t.Parallel()
	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, newTestSessionStore(t, nil))
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "unknown",
		Stream:    "location.01HYXYZ0C0000000000000000C",
	})
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "denial must surface as an oops error")
	assert.Equal(t, "STREAM_ACCESS_DENIED", oopsErr.Code(),
		"I-PRIV-5: missing-session denial must NOT leak SESSION_NOT_FOUND to the wire")
}

// I-PRIV-5 wire opacity: expired-session denial MUST collapse to
// STREAM_ACCESS_DENIED on the wire (denial_reason=expired_session goes to
// slog only).
func TestQueryStreamHistoryReturnsStreamAccessDeniedOnExpiredSession(t *testing.T) {
	t.Parallel()
	past := time.Now().Add(-time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"e1": {ID: "e1", ExpiresAt: &past},
	})
	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "e1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
	})
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "denial must surface as an oops error")
	assert.Equal(t, "STREAM_ACCESS_DENIED", oopsErr.Code(),
		"I-PRIV-5: expired-session denial must NOT leak SESSION_EXPIRED to the wire")
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
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Count:     -1,
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_ARGUMENT")
}

func TestQueryStreamHistoryRejectsMalformedCursor(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Cursor:    []byte("not-valid-cursor-bytes"),
	})
	require.Error(t, err)
	// gRPC status error with code InvalidArgument
	s2, ok := status.FromError(err)
	require.True(t, ok, "expected gRPC status error")
	assert.Equal(t, codes.InvalidArgument, s2.Code())
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
		Stream:    dotStyleSceneIC("not-a-ulid"),
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
		Stream:    "character." + otherID.String(),
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
		Stream:    "character." + charID.String(),
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
		Stream:    "location.01HYXYZ0C0000000000000000C",
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
		Stream:    "location.01HYXYZ0C0000000000000000C",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")
}

func TestQueryStreamHistoryBusErrorSurfacesAsInternal(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future, LocationID: locID},
	})
	reader := &fakeHistoryReader{err: errors.New("bus down")}
	s := newQueryStreamHistoryServer(t, reader, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INTERNAL")
}

func TestQueryStreamHistoryHasMoreReflectsCountPlusOne(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future, LocationID: locID},
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
		Stream:    "location.01HYXYZ0C0000000000000000C",
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
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future, LocationID: locID},
	})
	reader := &fakeHistoryReader{}
	s := newQueryStreamHistoryServer(t, reader, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Count:     0,
	})
	require.NoError(t, err)
	assert.Equal(t, defaultHistoryPageSize+1, reader.gotQ.PageSize)
}

func TestQueryStreamHistoryCountCappedAtMax(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future, LocationID: locID},
	})
	reader := &fakeHistoryReader{}
	s := newQueryStreamHistoryServer(t, reader, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Count:     99_999,
	})
	require.NoError(t, err)
	assert.Equal(t, maxHistoryPageSize+1, reader.gotQ.PageSize)
}

func TestQueryStreamHistoryCursorForwardsToBus(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future, LocationID: locID},
	})
	beforeID := core.NewULID()
	const beforeSeq uint64 = 42
	cursorBytes, encErr := cursor.Encode(cursor.Cursor{
		Version: cursor.CurrentVersion,
		Epoch:   cursor.CurrentEpoch(),
		Owner:   cursor.Owner{Kind: cursor.OwnerHost},
		Host:    &cursor.HostCursor{Seq: beforeSeq, ID: beforeID},
	})
	require.NoError(t, encErr)

	reader := &fakeHistoryReader{}
	s := newQueryStreamHistoryServer(t, reader, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Cursor:    cursorBytes,
		Count:     5,
	})
	require.NoError(t, err)
	assert.Equal(t, beforeID, reader.gotQ.BeforeID)
	assert.Equal(t, beforeSeq, reader.gotQ.BeforeSeq)
}

func TestQueryStreamHistoryNotBeforeMsForwardsToBus(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	// Drift fix (holomush-9mxr Task 10): Postgres COALESCEs a zero LocationArrivedAt
	// to now() on INSERT, making scopeFloor = now() for location streams. Set an
	// explicit LocationArrivedAt well before notBefore so the floor does not override it.
	arrivedAt := time.Now().Add(-3 * time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future, LocationID: locID, LocationArrivedAt: arrivedAt},
	})
	notBefore := time.Now().Add(-2 * time.Hour).UnixMilli()
	reader := &fakeHistoryReader{}
	s := newQueryStreamHistoryServer(t, reader, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId:   "s1",
		Stream:      "location.01HYXYZ0C0000000000000000C",
		NotBeforeMs: notBefore,
		Count:       5,
	})
	require.NoError(t, err)
	// NotBefore is threaded into the bus query as UTC time.
	assert.Equal(t, notBefore, reader.gotQ.NotBefore.UnixMilli())
	assert.Equal(t, eventbus.DirectionBackward, reader.gotQ.Direction)
}

// TestQueryStreamHistoryRejectsCursorWithStaleEpoch covers the epoch-mismatch
// branch: a cursor whose Epoch != CurrentEpoch() gets FailedPrecondition.
func TestQueryStreamHistoryRejectsCursorWithStaleEpoch(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})
	// Encode a cursor with Epoch=1 (current epoch is 0).
	staleEpochCursor, encErr := cursor.Encode(cursor.Cursor{
		Version: cursor.CurrentVersion,
		Epoch:   1, // not CurrentEpoch()
		Owner:   cursor.Owner{Kind: cursor.OwnerHost},
		Host:    &cursor.HostCursor{Seq: 10, ID: core.NewULID()},
	})
	require.NoError(t, encErr)

	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Cursor:    staleEpochCursor,
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "expected gRPC status error")
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

// TestQueryStreamHistoryRejectsUnknownCursorOwnerKind covers the default:
// branch in cursor.Decode (OwnerKind out of range). We construct a proto
// Cursor with OwnerKind(99) directly via cursorv1, marshal it, and send the
// bytes to the handler. Decode hits the default: branch and returns
// EVENTBUS_CURSOR_INVALID, which the handler surfaces as InvalidArgument.
func TestQueryStreamHistoryRejectsUnknownCursorOwnerKind(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future},
	})

	// Craft a proto Cursor with an out-of-range OwnerKind (99) and version=1
	// so it passes the version check but hits the default: branch in Decode.
	pb := &cursorv1.Cursor{
		Version: cursor.CurrentVersion,
		Epoch:   cursor.CurrentEpoch(),
		Owner:   &cursorv1.Owner{Kind: cursorv1.OwnerKind(99)},
	}
	cursorBytes, err := proto.Marshal(pb)
	require.NoError(t, err)

	s := newQueryStreamHistoryServer(t, &fakeHistoryReader{}, sess)
	_, err = s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Cursor:    cursorBytes,
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "expected gRPC status error")
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// TestQueryStreamHistoryWithPluginCursorRewrapsFrames covers the OwnerPlugin
// cursor path: the handler decodes the plugin cursor, routes to the bus, and
// re-wraps each EventFrame's cursor as OwnerPlugin.
func TestQueryStreamHistoryWithPluginCursorRewrapsFrames(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	charID := ulid.MustParse("01HYXYZCHAR0000000000000CH")
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", CharacterID: charID, ExpiresAt: &future, LocationID: locID},
	})

	// Build a plugin cursor with a 16-byte inner ULID.
	innerID := core.NewULID()
	pluginCursorBytes, encErr := cursor.Encode(cursor.Cursor{
		Version: cursor.CurrentVersion,
		Epoch:   cursor.CurrentEpoch(),
		Owner:   cursor.Owner{Kind: cursor.OwnerPlugin, PluginName: "core-scenes"},
		Plugin:  innerID[:],
	})
	require.NoError(t, encErr)

	// Fake reader returns one event.
	evt := eventbus.Event{
		ID:        core.NewULID(),
		Subject:   eventbus.Subject("events.main.location.01HYXYZ0C0000000000000000C"),
		Type:      "scene.pose",
		Timestamp: time.Now(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte("payload"),
		Seq:       5,
	}
	reader := &fakeHistoryReader{events: []eventbus.Event{evt}}

	s := newQueryStreamHistoryServer(t, reader, sess)
	resp, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Cursor:    pluginCursorBytes,
		Count:     10,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetEvents(), 1)

	// The BeforeID forwarded to the bus must equal the inner ULID.
	assert.Equal(t, innerID, reader.gotQ.BeforeID)

	// Each frame's cursor must be re-wrapped as OwnerPlugin.
	frameCursor := resp.GetEvents()[0].GetCursor()
	require.NotEmpty(t, frameCursor, "EventFrame must carry a cursor")
	decoded, decErr := cursor.Decode(frameCursor)
	require.NoError(t, decErr)
	assert.Equal(t, cursor.OwnerPlugin, decoded.Owner.Kind)
	assert.Equal(t, "core-scenes", decoded.Owner.PluginName)
}

// TestQueryStreamHistoryWithPluginCursorStringInner covers the plugin cursor
// path where the inner bytes are a string-encoded ULID (not raw 16 bytes).
func TestQueryStreamHistoryWithPluginCursorStringInner(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	charID := ulid.MustParse("01HYXYZCHAR0000000000000CH")
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", CharacterID: charID, ExpiresAt: &future, LocationID: locID},
	})

	// Build a plugin cursor whose inner bytes are the string form of a ULID
	// (26 bytes, not 16).
	innerID := core.NewULID()
	innerStr := innerID.String() // 26-char string ULID
	pluginCursorBytes, encErr := cursor.Encode(cursor.Cursor{
		Version: cursor.CurrentVersion,
		Epoch:   cursor.CurrentEpoch(),
		Owner:   cursor.Owner{Kind: cursor.OwnerPlugin, PluginName: "core-scenes"},
		Plugin:  []byte(innerStr),
	})
	require.NoError(t, encErr)

	reader := &fakeHistoryReader{}
	s := newQueryStreamHistoryServer(t, reader, sess)
	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Cursor:    pluginCursorBytes,
		Count:     5,
	})
	require.NoError(t, err)
	// The handler must parse the string ULID and forward the correct BeforeID.
	assert.Equal(t, innerID, reader.gotQ.BeforeID)
}

// TestQueryStreamHistoryEventFrameCarriesCursor verifies that each returned
// EventFrame has a non-empty Cursor field (set by encodeEventCursor).
func TestQueryStreamHistoryEventFrameCarriesCursor(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future, LocationID: locID},
	})
	evt := eventbus.Event{
		ID:        core.NewULID(),
		Subject:   eventbus.Subject("events.main.location.01HYXYZ0C0000000000000000C"),
		Type:      "scene.pose",
		Timestamp: time.Now(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte("p"),
		Seq:       7,
	}
	reader := &fakeHistoryReader{events: []eventbus.Event{evt}}
	s := newQueryStreamHistoryServer(t, reader, sess)
	resp, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Count:     5,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetEvents(), 1)

	frameCursor := resp.GetEvents()[0].GetCursor()
	require.NotEmpty(t, frameCursor, "EventFrame.Cursor must be non-empty")

	// Cursor must decode as a valid OwnerHost cursor.
	decoded, decErr := cursor.Decode(frameCursor)
	require.NoError(t, decErr)
	assert.Equal(t, cursor.OwnerHost, decoded.Owner.Kind)
	require.NotNil(t, decoded.Host)
	assert.Equal(t, uint64(7), decoded.Host.Seq)
	assert.Equal(t, evt.ID, decoded.Host.ID)
}

// TestQueryStreamHistoryNextCursorSetWhenHasMore verifies that NextCursor is
// populated on the response when has_more is true, and empty when false.
func TestQueryStreamHistoryNextCursorSetWhenHasMore(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future, LocationID: locID},
	})
	// 4 events with count=3 → has_more=true, NextCursor from oldest frame.
	evts := make([]eventbus.Event, 4)
	for i := range evts {
		evts[i] = eventbus.Event{
			ID:        core.NewULID(),
			Subject:   eventbus.Subject("events.main.location.01HYXYZ0C0000000000000000C"),
			Type:      "scene.pose",
			Timestamp: time.Now(),
			Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
			Payload:   []byte("p"),
			Seq:       uint64(i + 1),
		}
	}
	reader := &fakeHistoryReader{events: evts}
	s := newQueryStreamHistoryServer(t, reader, sess)
	resp, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Count:     3,
	})
	require.NoError(t, err)
	assert.True(t, resp.GetHasMore())
	assert.NotEmpty(t, resp.GetNextCursor(), "NextCursor must be set when has_more is true")

	// No-more case: 2 events with count=3 → has_more=false, NextCursor empty.
	s2 := newQueryStreamHistoryServer(t, &fakeHistoryReader{events: evts[:2]}, sess)
	resp2, err2 := s2.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Count:     3,
	})
	require.NoError(t, err2)
	assert.False(t, resp2.GetHasMore())
	assert.Empty(t, resp2.GetNextCursor(), "NextCursor must be empty when has_more is false")
}

// TestMapHistoryErrorTranslatesErrCursorInvalidToInvalidArgument verifies
// that mapHistoryError maps ErrCursorInvalid → gRPC InvalidArgument.
func TestMapHistoryErrorTranslatesErrCursorInvalidToInvalidArgument(t *testing.T) {
	t.Parallel()
	wrapped := oops.Code("WRAPPER").Wrap(eventbus.ErrCursorInvalid)
	got := mapHistoryError(wrapped, "test-session", "location:test")
	st, ok := status.FromError(got)
	require.True(t, ok, "expected gRPC status error")
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// TestMapHistoryErrorTranslatesErrCursorStaleToFailedPrecondition verifies
// that mapHistoryError maps ErrCursorStale → gRPC FailedPrecondition.
func TestMapHistoryErrorTranslatesErrCursorStaleToFailedPrecondition(t *testing.T) {
	t.Parallel()
	wrapped := oops.Code("WRAPPER").Wrap(eventbus.ErrCursorStale)
	got := mapHistoryError(wrapped, "test-session", "location:test")
	st, ok := status.FromError(got)
	require.True(t, ok, "expected gRPC status error")
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

// TestMapHistoryErrorTranslatesErrCursorLagToUnavailable verifies that
// mapHistoryError maps ErrCursorLag → gRPC Unavailable.
func TestMapHistoryErrorTranslatesErrCursorLagToUnavailable(t *testing.T) {
	t.Parallel()
	wrapped := oops.Code("WRAPPER").Wrap(eventbus.ErrCursorLag)
	got := mapHistoryError(wrapped, "test-session", "location:test")
	st, ok := status.FromError(got)
	require.True(t, ok, "expected gRPC status error")
	assert.Equal(t, codes.Unavailable, st.Code())
}

// TestMapHistoryErrorPassesThroughUnknownError verifies that mapHistoryError
// does not transform errors it does not recognise.
func TestMapHistoryErrorPassesThroughUnknownError(t *testing.T) {
	t.Parallel()
	orig := oops.Errorf("some other error")
	got := mapHistoryError(orig, "test-session", "location:test")
	// mapHistoryError must pass through errors it does not recognise unchanged.
	assert.Equal(t, orig, got)
}

// TestMapHistoryErrorTranslatesPermissionDeniedToOpaqueOopsCode verifies that
// a gRPC PermissionDenied returned by the plugin collapses into the same
// opaque oops code the outer I-17 gate uses, preventing information leak
// about which authorization wall caught the caller.
func TestMapHistoryErrorTranslatesPermissionDeniedToOpaqueOopsCode(t *testing.T) {
	t.Parallel()

	pluginErr := status.Error(codes.PermissionDenied, "scene audit access denied")
	got := mapHistoryError(pluginErr, "test-session", "location:test")
	require.Error(t, got)

	oopsErr, ok := oops.AsOops(got)
	require.True(t, ok, "translated error MUST be oops-wrapped")
	assert.Equal(t, "STREAM_ACCESS_DENIED", oopsErr.Code(),
		"PermissionDenied from the plugin MUST collapse into the same opaque oops code the outer I-17 gate uses")

	// Log-parity assertion (G1): server-side observability requires the
	// same context fields the outer I-17 gate attaches at
	// internal/grpc/query_stream_history.go:170-173.
	ctx := oopsErr.Context()
	assert.Equal(t, "test-session", ctx["session_id"],
		"PermissionDenied translation MUST attach session_id to the oops chain for log parity")
	assert.Equal(t, "location:test", ctx["stream"],
		"PermissionDenied translation MUST attach stream to the oops chain for log parity")
}

// TestMapHistoryErrorPassesThroughInvalidArgument verifies that an
// InvalidArgument status from the plugin propagates as-is to the client.
func TestMapHistoryErrorPassesThroughInvalidArgument(t *testing.T) {
	t.Parallel()

	pluginErr := status.Error(codes.InvalidArgument, "subject malformed")
	got := mapHistoryError(pluginErr, "test-session", "location:test")
	require.Error(t, got)

	st, ok := status.FromError(got)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// TestMapHistoryErrorRetainsCursorInvalidDispatchForNonStatusErrors verifies
// that the existing eventbus.ErrCursorInvalid → InvalidArgument dispatch
// still applies when no gRPC status is present on the error.
func TestMapHistoryErrorRetainsCursorInvalidDispatchForNonStatusErrors(t *testing.T) {
	t.Parallel()

	got := mapHistoryError(eventbus.ErrCursorInvalid, "test-session", "location:test")
	require.Error(t, got)
	st, ok := status.FromError(got)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code(),
		"existing cursor-error dispatch MUST still apply when no gRPC status is present")
}

// TestMapHistoryErrorPassesThroughInvalidArgumentWithDetails verifies that
// status.WithDetails proto messages attached by the plugin survive
// translation through mapHistoryError. Goal G2: pass-through MUST preserve
// the gRPC code AND any structured details. Bare-status pass-through is
// covered separately by TestMapHistoryErrorPassesThroughInvalidArgument.
func TestMapHistoryErrorPassesThroughInvalidArgumentWithDetails(t *testing.T) {
	t.Parallel()

	detail := &errdetails.BadRequest{
		FieldViolations: []*errdetails.BadRequest_FieldViolation{
			{Field: "subject", Description: "malformed"},
		},
	}
	pluginStatus, withErr := status.New(codes.InvalidArgument, "subject malformed").WithDetails(detail)
	require.NoError(t, withErr, "WithDetails MUST succeed for canonical errdetails proto")

	got := mapHistoryError(pluginStatus.Err(), "test-session", "location:test")
	require.Error(t, got)

	gotStatus, ok := status.FromError(got)
	require.True(t, ok, "translated error MUST carry a gRPC status")
	assert.Equal(t, codes.InvalidArgument, gotStatus.Code(),
		"InvalidArgument MUST pass through")

	details := gotStatus.Details()
	require.Len(t, details, 1, "exactly one detail proto MUST round-trip")

	gotDetail, ok := details[0].(*errdetails.BadRequest)
	require.True(t, ok, "detail proto MUST round-trip as *errdetails.BadRequest")
	assert.True(t, proto.Equal(detail, gotDetail),
		"detail proto MUST be byte-equal to the input via proto.Equal")
}

// TestQueryStreamHistoryEncodeEventCursorWithZeroSeq verifies that
// encodeEventCursor still produces a decodable OwnerHost cursor when Seq==0
// (no JetStream metadata populated).
func TestQueryStreamHistoryEncodeEventCursorWithZeroSeq(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", ExpiresAt: &future, LocationID: locID},
	})
	// Event without Seq (Seq==0) — encodeEventCursor must not fail.
	evt := eventbus.Event{
		ID:        core.NewULID(),
		Subject:   eventbus.Subject("events.main.location.01HYXYZ0C0000000000000000C"),
		Type:      "scene.pose",
		Timestamp: time.Now(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte("p"),
		Seq:       0,
	}
	reader := &fakeHistoryReader{events: []eventbus.Event{evt}}
	s := newQueryStreamHistoryServer(t, reader, sess)
	resp, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Count:     5,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetEvents(), 1)
	// Cursor must be decodable even with Seq==0.
	frameCursor := resp.GetEvents()[0].GetCursor()
	require.NotEmpty(t, frameCursor)
	decoded, decErr := cursor.Decode(frameCursor)
	require.NoError(t, decErr)
	assert.Equal(t, uint64(0), decoded.Host.Seq)
}

// TestQueryStreamHistoryThreadsCallerFromSession asserts that the handler
// derives Caller (Actor) from the authenticated session record and threads
// it through to the HistoryReader's HistoryQuery. This is the producer side
// of the I-23 caller invariant: plugin-owned subjects gate on q.Caller.
func TestQueryStreamHistoryThreadsCallerFromSession(t *testing.T) {
	t.Parallel()

	charID := ulid.MustParse("01HYXCHAR0000000000000000C")
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"sess-1": {
			ID:          "sess-1",
			CharacterID: charID,
			LocationID:  locID,
			ExpiresAt:   &future,
		},
	})

	reader := &fakeHistoryReader{}
	server := newQueryStreamHistoryServer(t, reader, sess)

	// Location stream: the hard-gate passes because the session is seeded at
	// the queried location. The test focuses on caller threading.
	_, err := server.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "sess-1",
		Stream:    "location." + locID.String(),
		Count:     10,
	})
	require.NoError(t, err)

	assert.Equal(t, eventbus.ActorKindCharacter, reader.gotQ.Caller.Kind,
		"handler MUST set Caller.Kind = ActorKindCharacter")
	assert.Equal(t, charID, reader.gotQ.Caller.ID,
		"handler MUST set Caller.ID from info.CharacterID")
}

// TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode covers
// spec §6.5 case 2: the outer I-17 membership gate passes (the session is a
// scene member), so the request reaches the HistoryReader. The plugin then
// returns a gRPC PermissionDenied (e.g., due to a mid-read membership
// revocation, or a plugin-internal authz wall). The handler MUST collapse
// this into the SAME opaque STREAM_ACCESS_DENIED oops code the outer gate
// uses — clients cannot distinguish "outer wall caught" from "plugin wall
// caught", preventing information leak about authz topology.
func TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode(t *testing.T) {
	t.Parallel()

	stream, focus := sceneFocusMembership(t)
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {
			ID:               "s1",
			CharacterID:      ulid.MustParse("01HYXCHAR0000000000000000C"),
			ExpiresAt:        &future,
			FocusMemberships: []session.FocusMembership{focus},
		},
	})

	reader := &fakeHistoryReader{
		err: status.Error(codes.PermissionDenied, "scene audit access denied"),
	}
	s := newQueryStreamHistoryServer(t, reader, sess)

	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    stream,
		Count:     10,
	})
	require.Error(t, err)

	// Assert the TOP-LEVEL oops code, not any code somewhere in the chain.
	// errutil.AssertErrorCode walks the chain via errors.Is, which would
	// pass even if the handler double-wrapped STREAM_ACCESS_DENIED inside
	// an outer INTERNAL — defeating the opacity contract. oops.AsOops
	// returns the OUTERMOST oops node, so .Code() asserts the actual
	// client-visible top-level code.
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "translated error MUST be an oops error at the top level")
	assert.Equal(t, "STREAM_ACCESS_DENIED", oopsErr.Code(),
		"top-level oops code MUST equal the outer I-17 gate's code (no double-wrap with INTERNAL)")

	// G1 regression guard: the plugin-path translation MUST attach the
	// same context the outer I-17 gate does. Without these the server
	// log loses session_id/stream when the plugin wall catches.
	ctx := oopsErr.Context()
	assert.Equal(t, "s1", ctx["session_id"],
		"plugin-path translation MUST attach session_id from the request")
	assert.Equal(t, stream, ctx["stream"],
		"plugin-path translation MUST attach stream from the request")
}

func TestQueryStreamHistoryPassesIdentityFromBindingsToHistoryQuery(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	charID := core.NewULID()
	playerID := core.NewULID()
	bindingID := "bnd-test-001"
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")

	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {
			ID:          "s1",
			CharacterID: charID,
			PlayerID:    playerID,
			LocationID:  locID,
			ExpiresAt:   &future,
		},
	})
	reader := &fakeHistoryReader{}
	s := &CoreServer{
		sessionStore:  sess,
		historyReader: reader,
		accessEngine:  policytest.AllowAllEngine(),
		bindings:      &fakeBindingRepo{bindingID: bindingID},
		cryptoEnabled: true, // required to activate binding lookup (Phase 3b gate)
	}

	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Count:     10,
	})
	require.NoError(t, err)
	assert.Equal(t, eventbus.IdentityKindCharacter, reader.gotQ.Identity.Kind,
		"HistoryQuery.Identity.Kind must be IdentityKindCharacter when bindings are wired")
	assert.Equal(t, playerID.String(), reader.gotQ.Identity.PlayerID,
		"HistoryQuery.Identity.PlayerID must match session's PlayerID")
	assert.Equal(t, charID.String(), reader.gotQ.Identity.CharacterID,
		"HistoryQuery.Identity.CharacterID must match session's CharacterID")
	assert.Equal(t, bindingID, reader.gotQ.Identity.BindingID,
		"HistoryQuery.Identity.BindingID must match the binding returned by Current")
}

func TestQueryStreamHistoryBindingLookupFailureReturnsError(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	charID := core.NewULID()
	playerID := core.NewULID()
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")

	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {
			ID:          "s1",
			CharacterID: charID,
			PlayerID:    playerID,
			LocationID:  locID,
			ExpiresAt:   &future,
		},
	})
	reader := &fakeHistoryReader{}
	s := &CoreServer{
		sessionStore:  sess,
		historyReader: reader,
		accessEngine:  policytest.AllowAllEngine(),
		bindings:      &fakeBindingRepo{err: errors.New("db error")},
		cryptoEnabled: true, // required to activate binding lookup (Phase 3b gate)
	}

	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Count:     10,
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "HISTORY_BINDING_LOOKUP_FAILED")
}

func TestQueryStreamHistoryPassesZeroIdentityWhenBindingsNil(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	charID := core.NewULID()
	playerID := core.NewULID()
	locID := ulid.MustParse("01HYXYZ0C0000000000000000C")

	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {
			ID:          "s1",
			CharacterID: charID,
			PlayerID:    playerID,
			LocationID:  locID,
			ExpiresAt:   &future,
		},
	})
	reader := &fakeHistoryReader{}
	// No bindings wired — zero-value SessionIdentity flows through.
	s := newQueryStreamHistoryServer(t, reader, sess)

	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location.01HYXYZ0C0000000000000000C",
		Count:     10,
	})
	require.NoError(t, err)
	assert.Equal(t, eventbus.IdentityKindUnknown, reader.gotQ.Identity.Kind,
		"when bindings is nil, HistoryQuery.Identity must be zero-value (IdentityKindUnknown)")
}

// TestQueryStreamHistoryFiltersAuditOnlyEvents is the symmetric counterpart
// of TestDispatchDeliverySkipsAuditOnlyEvents (subscribe path) for the
// history-read path (QueryStreamHistory). AUDIT_ONLY events MUST NOT be
// returned to client streams via either the live subscribe filter at
// internal/grpc/server.go (~line 1019) or the history filter at
// fetchHistoryFramesFromBus. Both surfaces must agree — otherwise a client
// could read host-emit security audit content (crypto.totp_*, crypto.policy_set)
// by paginating history even though live subscribe drops it.
//
// The fake reader returns a mix of normal and AUDIT_ONLY events; only the
// non-audit ones must surface in the response.
func TestQueryStreamHistoryFiltersAuditOnlyEvents(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	charID := ulid.MustParse("01HYXYZCHAR0000000000000CH")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {ID: "s1", CharacterID: charID, ExpiresAt: &future},
	})
	// Mix of audit-only and normal events on the same character stream.
	// fakeHistoryReader returns events newest-first; fetchHistoryFramesFromBus
	// reverses to oldest-first.
	stream := eventbus.Subject("events.main.character." + charID.String())
	auditEvent := eventbus.Event{
		ID:        core.NewULID(),
		Subject:   stream,
		Type:      "crypto.totp_locked",
		Timestamp: time.Now(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte(`{}`),
		Rendering: &eventbus.RenderingMetadata{
			DisplayTarget: eventbus.EventChannelAuditOnly,
			SourcePlugin:  "builtin",
			Category:      "system",
		},
	}
	normalEvent := eventbus.Event{
		ID:        core.NewULID(),
		Subject:   stream,
		Type:      "scene.pose",
		Timestamp: time.Now(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte("p"),
	}
	reader := &fakeHistoryReader{events: []eventbus.Event{normalEvent, auditEvent}}
	s := newQueryStreamHistoryServer(t, reader, sess)

	resp, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "character." + charID.String(),
		Count:     10,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetEvents(), 1,
		"AUDIT_ONLY events MUST NOT be returned via QueryStreamHistory (symmetric with dispatchDelivery)")
	assert.Equal(t, "scene.pose", resp.GetEvents()[0].GetType(),
		"only the non-audit event survives the filter")
}

// TestQueryStreamHistory_LocationHardGate_DeniedWhenNotInLocation verifies
// that a session requesting a location stream is denied when the session's
// LocationID does not match the requested location (I-PRIV-1 Tier 1).
// Uses DenyAllEngine so the staff override path (I-PRIV-6) returns false,
// exercising the hard-gate denial without ABAC interference.
func TestQueryStreamHistory_LocationHardGate_DeniedWhenNotInLocation(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	locA := ulid.MustParse("01HYXLOCA0000000000000000A")
	locB := ulid.MustParse("01HYXLOCB0000000000000000B")
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {
			ID:         "s1",
			LocationID: locA,
			ExpiresAt:  &future,
		},
	})
	// DenyAllEngine ensures staffOverride returns false, so the I-PRIV-1
	// hard-gate rejects the request when the session is at a different location.
	s := &CoreServer{
		sessionStore:  sess,
		historyReader: &fakeHistoryReader{},
		accessEngine:  policytest.DenyAllEngine(),
	}

	resp, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    "location." + locB.String(),
	})
	require.Nil(t, resp)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")
}

// TestQueryStreamHistory_StaffOverride_BypassesHardGate verifies that a
// session whose character is granted read_unrestricted_history via ABAC can
// query a location stream even when not co-located with that location
// (I-PRIV-6 / ADR wxty). The staff override path bypasses the I-PRIV-1
// location hard-gate.
func TestQueryStreamHistory_StaffOverride_BypassesHardGate(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	locA := ulid.MustParse("01HYXLOCA0000000000000000A")
	locB := ulid.MustParse("01HYXLOCB0000000000000000B")
	charID := ulid.MustParse("01HYXCHAR00000000000000001")

	sess := newTestSessionStore(t, map[string]*session.Info{
		"staff-1": {
			ID:          "staff-1",
			CharacterID: charID,
			LocationID:  locA, // in locA, but will query locB
			ExpiresAt:   &future,
		},
	})

	// staffGrantEngine grants read_unrestricted_history for the character
	// subject (any resource), matching what the real seed policy does.
	staffEngine := policytest.NewGrantEngine()
	staffEngine.Grant(
		"character:"+charID.String(),
		"read_unrestricted_history",
		"stream:*",
	)

	reader := &fakeHistoryReader{events: nil}
	s := &CoreServer{
		sessionStore:  sess,
		historyReader: reader,
		accessEngine:  staffEngine,
	}

	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "staff-1",
		Stream:    "location." + locB.String(),
	})
	require.NoError(t, err, "staff role must bypass location hard-gate (I-PRIV-6)")
}

// TestQueryStreamHistoryNotAfterMsPlumbing pins the wire contract for
// holomush-iu8j (cursor-bounded backfill). The handler MUST plumb
// req.NotAfterMs into HistoryQuery.NotAfter (UTC, ms precision) so
// the tier reads honor the Subscribe-attach-moment bound the client
// sends; req.NotAfterMs=0 MUST stay zero on the HistoryQuery
// (back-compat with legacy clients that don't know about the new
// field).
//
// Invariants pinned: I-IU8J-1 (NotAfter is forwarded with ms-precision
// UTC semantics), I-IU8J-2 (zero-as-unbounded). Tier-level filtering
// coverage lives in internal/eventbus/history/*_test.go.
func TestQueryStreamHistoryNotAfterMsPlumbing(t *testing.T) {
	t.Parallel()

	// Pick a deterministic attach moment outside any subtest so all
	// cases share the same expected non-zero value.
	attachMoment := time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Millisecond)

	tests := []struct {
		name        string
		notAfterMs  int64
		wantNotZero bool      // when true, want NotAfter.Equal(attachMoment); when false, want NotAfter.IsZero()
		want        time.Time // only consulted when wantNotZero == true
	}{
		{
			name:        "non-zero ms forwards as UTC time at ms precision (I-IU8J-1)",
			notAfterMs:  attachMoment.UnixMilli(),
			wantNotZero: true,
			want:        attachMoment,
		},
		{
			name:        "zero stays zero on HistoryQuery — back-compat with legacy clients (I-IU8J-2)",
			notAfterMs:  0,
			wantNotZero: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Fresh session + reader per case so reader.gotQ captures
			// THIS case's request and parallel subtests don't share state.
			future := time.Now().Add(time.Hour)
			locA := ulid.MustParse("01HYXLOCA0000000000000000A")
			charID := ulid.MustParse("01HYXCHAR00000000000000001")
			sess := newTestSessionStore(t, map[string]*session.Info{
				"sess-1": {
					ID:          "sess-1",
					CharacterID: charID,
					LocationID:  locA,
					ExpiresAt:   &future,
				},
			})
			reader := &fakeHistoryReader{events: nil}
			s := newQueryStreamHistoryServer(t, reader, sess)

			_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
				SessionId:  "sess-1",
				Stream:     "location." + locA.String(),
				NotAfterMs: tc.notAfterMs,
			})
			require.NoError(t, err, "request setup should succeed")

			if tc.wantNotZero {
				require.True(t, reader.gotQ.NotAfter.Equal(tc.want),
					"HistoryQuery.NotAfter MUST equal time.UnixMilli(req.NotAfterMs).UTC(); got %s want %s",
					reader.gotQ.NotAfter, tc.want)
			} else {
				require.True(t, reader.gotQ.NotAfter.IsZero(),
					"HistoryQuery.NotAfter MUST be zero when req.NotAfterMs == 0 (back-compat with legacy clients); got %s",
					reader.gotQ.NotAfter)
			}
		})
	}
}
