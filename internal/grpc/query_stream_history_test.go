// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// TODO(holomush-1tvn.14): These tests are tagged f4_legacy because they depend
// on the countingReplayTailStore / MemoryEventStore.ReplayTail path that
// existed before F4 wired the JetStream/PostgreSQL tier crossover reader. The
// tests cover the auth/validation steps (steps 1–6) which are unchanged and
// still exercised by the legacy fallback path. F7 will drop the ReplayTail
// fallback entirely and replace these with reader-backed tests.
//
//go:build f4_legacy

package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	accessTypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/pkg/errutil"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// ---------- Test helpers ----------

// queryHistoryFixture holds the common state threaded through tests.
type queryHistoryFixture struct {
	server     *CoreServer
	eventStore *core.MemoryEventStore
	sessStore  *session.MemStore
	sessionID  string
	charID     ulid.ULID
	locID      ulid.ULID
	sceneID    ulid.ULID
}

// newQueryStreamHistoryTestServer builds a CoreServer with a MemoryEventStore,
// session.MemStore and a configurable ABAC engine. The session is
// pre-populated active, with a single scene focus membership on sceneID.
func newQueryStreamHistoryTestServer(t *testing.T, engine accessTypes.AccessPolicyEngine) *queryHistoryFixture {
	t.Helper()
	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	charID := core.NewULID()
	sessionID := core.NewULID().String()
	locID := core.NewULID()
	sceneID := core.NewULID()

	require.NoError(t, sessStore.Set(context.Background(), sessionID, &session.Info{
		ID:            sessionID,
		CharacterID:   charID,
		CharacterName: "Alice",
		LocationID:    locID,
		Status:        session.StatusActive,
		EventCursors:  map[string]ulid.ULID{},
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
		},
	}))

	s := &CoreServer{
		engine:       core.NewEngine(eventStore),
		eventStore:   eventStore,
		sessionStore: sessStore,
		accessEngine: engine,
	}

	return &queryHistoryFixture{
		server:     s,
		eventStore: eventStore,
		sessStore:  sessStore,
		sessionID:  sessionID,
		charID:     charID,
		locID:      locID,
		sceneID:    sceneID,
	}
}

// appendTestEvent appends a test event with the given stream, returning the event.
func appendTestEvent(t *testing.T, store *core.MemoryEventStore, stream string) core.Event {
	t.Helper()
	e := core.NewEvent(stream, core.EventTypeSay, core.Actor{
		Kind: core.ActorCharacter, ID: "test-actor",
	}, []byte(`{"message":"test"}`))
	require.NoError(t, store.Append(context.Background(), e))
	return e
}

// locationStream returns "location:<id>".
func locationStream(id ulid.ULID) string { return "location:" + id.String() }

// sceneICStream returns "scene:<id>:ic".
func sceneICStream(id ulid.ULID) string { return "scene:" + id.String() + ":ic" }

// characterStream returns "character:<id>".
func characterStream(id ulid.ULID) string { return "character:" + id.String() }

// buildHistoryRequest is a small helper to reduce boilerplate.
func buildHistoryRequest(sessionID, stream string, count int32) *corev1.QueryStreamHistoryRequest {
	return &corev1.QueryStreamHistoryRequest{
		Meta:      &corev1.RequestMeta{RequestId: "req-" + sessionID, Timestamp: timestamppb.Now()},
		SessionId: sessionID,
		Stream:    stream,
		Count:     count,
	}
}

// ---------- §8.1 Unit tests ----------

func TestQueryStreamHistoryReturnsEventsForAuthorizedLocationStream(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f := newQueryStreamHistoryTestServer(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	e1 := appendTestEvent(t, f.eventStore, stream)
	e2 := appendTestEvent(t, f.eventStore, stream)

	resp, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	require.NoError(t, err)
	require.Len(t, resp.Events, 2)
	assert.Equal(t, e1.ID.String(), resp.Events[0].Id)
	assert.Equal(t, e2.ID.String(), resp.Events[1].Id)
	assert.False(t, resp.HasMore)
}

func TestQueryStreamHistoryReturnsEventsForFocusDerivedSceneStream(t *testing.T) {
	// Use an engine that would ERROR if called — proves ABAC is NOT
	// consulted for private scene streams.
	errEngine := policytest.NewErrorEngine(errors.New("engine must not be called for private streams"))
	f := newQueryStreamHistoryTestServer(t, errEngine)
	stream := sceneICStream(f.sceneID)
	appendTestEvent(t, f.eventStore, stream)

	resp, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
}

func TestQueryStreamHistoryReturnsEventsForOwnCharacterStream(t *testing.T) {
	errEngine := policytest.NewErrorEngine(errors.New("engine must not be called for private streams"))
	f := newQueryStreamHistoryTestServer(t, errEngine)
	stream := characterStream(f.charID)
	appendTestEvent(t, f.eventStore, stream)

	resp, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
}

func TestQueryStreamHistoryRejectsEmptySessionID(t *testing.T) {
	f := newQueryStreamHistoryTestServer(t, policytest.AllowAllEngine())
	req := buildHistoryRequest("", locationStream(f.locID), 10)

	_, err := f.server.QueryStreamHistory(context.Background(), req)
	errutil.AssertErrorCode(t, err, "INVALID_ARGUMENT")
}

func TestQueryStreamHistoryRejectsEmptyStream(t *testing.T) {
	f := newQueryStreamHistoryTestServer(t, policytest.AllowAllEngine())
	req := buildHistoryRequest(f.sessionID, "", 10)

	_, err := f.server.QueryStreamHistory(context.Background(), req)
	errutil.AssertErrorCode(t, err, "INVALID_ARGUMENT")
}

func TestQueryStreamHistoryRejectsNegativeCount(t *testing.T) {
	f := newQueryStreamHistoryTestServer(t, policytest.AllowAllEngine())
	req := buildHistoryRequest(f.sessionID, locationStream(f.locID), -1)

	_, err := f.server.QueryStreamHistory(context.Background(), req)
	errutil.AssertErrorCode(t, err, "INVALID_ARGUMENT")
}

func TestQueryStreamHistoryRejectsMalformedBeforeID(t *testing.T) {
	f := newQueryStreamHistoryTestServer(t, policytest.AllowAllEngine())
	req := buildHistoryRequest(f.sessionID, locationStream(f.locID), 10)
	req.BeforeId = "not-a-ulid"

	_, err := f.server.QueryStreamHistory(context.Background(), req)
	errutil.AssertErrorCode(t, err, "INVALID_ARGUMENT")
}

// countingReplayTailStore wraps a MemoryEventStore so tests can observe the
// count parameter forwarded to ReplayTail.
type countingReplayTailStore struct {
	*core.MemoryEventStore
	lastCount     int
	lastBeforeID  ulid.ULID
	lastNotBefore time.Time
	lastStream    string
	calls         int
}

func (c *countingReplayTailStore) ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]core.Event, error) {
	c.calls++
	c.lastStream = stream
	c.lastCount = count
	c.lastBeforeID = beforeID
	c.lastNotBefore = notBefore
	return c.MemoryEventStore.ReplayTail(ctx, stream, count, notBefore, beforeID)
}

// newCountingFixture builds a fixture whose eventStore is a counting wrapper.
func newCountingFixture(t *testing.T, engine accessTypes.AccessPolicyEngine) (*queryHistoryFixture, *countingReplayTailStore) {
	t.Helper()
	f := newQueryStreamHistoryTestServer(t, engine)
	cs := &countingReplayTailStore{MemoryEventStore: f.eventStore}
	f.server.eventStore = cs
	return f, cs
}

func TestQueryStreamHistoryDefaultsCountTo150WhenZero(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f, cs := newCountingFixture(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	_, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 0))
	require.NoError(t, err)
	assert.Equal(t, defaultHistoryPageSize+1, cs.lastCount, "count=0 should default to 150, fetched as 151 for has_more detection")
}

func TestQueryStreamHistoryClampsCountAbove500(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f, cs := newCountingFixture(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	_, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 1000))
	require.NoError(t, err)
	assert.Equal(t, maxHistoryPageSize+1, cs.lastCount, "count=1000 should clamp to 500, fetched as 501 for has_more detection")
}

func TestQueryStreamHistoryRejectsExpiredSession(t *testing.T) {
	f := newQueryStreamHistoryTestServer(t, policytest.AllowAllEngine())
	// Mutate the session to be expired.
	past := time.Now().Add(-1 * time.Hour)
	require.NoError(t, f.sessStore.Set(context.Background(), f.sessionID, &session.Info{
		ID:          f.sessionID,
		CharacterID: f.charID,
		LocationID:  f.locID,
		Status:      session.StatusDetached,
		ExpiresAt:   &past,
	}))

	_, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, locationStream(f.locID), 10))
	errutil.AssertErrorCode(t, err, "SESSION_EXPIRED")
}

func TestQueryStreamHistoryRejectsNonexistentSession(t *testing.T) {
	f := newQueryStreamHistoryTestServer(t, policytest.AllowAllEngine())

	_, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest("nonexistent-session", locationStream(f.locID), 10))
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}

func TestQueryStreamHistoryRejectsWhenEventStoreIsNil(t *testing.T) {
	f := newQueryStreamHistoryTestServer(t, policytest.AllowAllEngine())
	f.server.eventStore = nil

	_, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, locationStream(f.locID), 10))
	errutil.AssertErrorCode(t, err, "INTERNAL")
}

func TestQueryStreamHistoryDeniesUnauthorizedPublicStream(t *testing.T) {
	f := newQueryStreamHistoryTestServer(t, policytest.DenyAllEngine())
	stream := locationStream(f.locID)
	appendTestEvent(t, f.eventStore, stream)

	_, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")
}

func TestQueryStreamHistoryAdminCanReadAnyPublicStream(t *testing.T) {
	// AllowAllEngine simulates the admin policy granting read on any
	// stream. We use a non-co-located location stream to demonstrate
	// admin can read any public stream.
	f := newQueryStreamHistoryTestServer(t, policytest.AllowAllEngine())
	otherLoc := core.NewULID()
	stream := locationStream(otherLoc)
	e1 := appendTestEvent(t, f.eventStore, stream)

	resp, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	assert.Equal(t, e1.ID.String(), resp.Events[0].Id)
}

func TestQueryStreamHistoryPassesBeforeIDToReplayTail(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f, cs := newCountingFixture(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	beforeID := core.NewULID()
	req := buildHistoryRequest(f.sessionID, stream, 10)
	req.BeforeId = beforeID.String()

	_, err := f.server.QueryStreamHistory(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, beforeID, cs.lastBeforeID)
}

func TestQueryStreamHistoryPassesNotBeforeToReplayTail(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f, cs := newCountingFixture(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	floor := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	req := buildHistoryRequest(f.sessionID, stream, 10)
	req.NotBeforeMs = floor.UnixMilli()

	_, err := f.server.QueryStreamHistory(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, cs.lastNotBefore.Equal(floor), "expected notBefore=%s, got %s", floor, cs.lastNotBefore)
}

func TestQueryStreamHistorySetsHasMoreWhenMoreEventsExist(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f := newQueryStreamHistoryTestServer(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	// 5 events, count=3 → fetch 4, return 3, has_more=true.
	for i := 0; i < 5; i++ {
		appendTestEvent(t, f.eventStore, stream)
	}

	resp, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 3))
	require.NoError(t, err)
	assert.Len(t, resp.Events, 3)
	assert.True(t, resp.HasMore)
}

func TestQueryStreamHistorySetsHasMoreAtMaxPageSize(t *testing.T) {
	// Regression test for the count+1 interaction at the ceiling. When
	// count=500 (user-facing max) and 501+ events exist, has_more MUST be
	// true. This requires the store-level cap to be >= 501 so the
	// handler's count+1 probe actually retrieves the 501st event — if the
	// store silently capped at 500, has_more would be incorrectly false.
	grant := policytest.NewGrantEngine()
	f := newQueryStreamHistoryTestServer(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	// 501 events on the stream.
	for i := 0; i < 501; i++ {
		appendTestEvent(t, f.eventStore, stream)
	}

	resp, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, maxHistoryPageSize))
	require.NoError(t, err)
	assert.Len(t, resp.Events, maxHistoryPageSize, "must return exactly count events")
	assert.True(t, resp.HasMore, "has_more must be true when count=500 and 501 events exist")
}

func TestQueryStreamHistorySetsHasMoreFalseWhenNoMore(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f := newQueryStreamHistoryTestServer(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	for i := 0; i < 3; i++ {
		appendTestEvent(t, f.eventStore, stream)
	}

	resp, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	require.NoError(t, err)
	assert.Len(t, resp.Events, 3)
	assert.False(t, resp.HasMore)
}

func TestQueryStreamHistoryReturnsEventsInAscendingOrder(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f := newQueryStreamHistoryTestServer(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	events := make([]core.Event, 0, 4)
	for i := 0; i < 4; i++ {
		events = append(events, appendTestEvent(t, f.eventStore, stream))
	}

	resp, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	require.NoError(t, err)
	require.Len(t, resp.Events, 4)
	for i, e := range events {
		assert.Equal(t, e.ID.String(), resp.Events[i].Id, "event %d out of order", i)
	}
}

func TestQueryStreamHistoryIncludesResponseMeta(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f := newQueryStreamHistoryTestServer(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	req := buildHistoryRequest(f.sessionID, stream, 10)
	req.Meta.RequestId = "echo-me-123"

	resp, err := f.server.QueryStreamHistory(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.Meta)
	assert.Equal(t, "echo-me-123", resp.Meta.RequestId)
}

// ---------- §8.2 Invariant tests (I-17) ----------

func TestQueryStreamHistoryI17DeniesNonMemberSceneStreamRead(t *testing.T) {
	// Engine returns an error if called — proves the membership gate
	// short-circuits BEFORE ABAC evaluation for private streams (I-17).
	engine := policytest.NewErrorEngine(errors.New("ABAC must not be consulted for private streams"))
	f := newQueryStreamHistoryTestServer(t, engine)

	foreignScene := core.NewULID() // not in session's FocusMemberships
	stream := sceneICStream(foreignScene)

	_, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")
}

func TestQueryStreamHistoryI17DeniesAdminNonMemberSceneStreamRead(t *testing.T) {
	// AllowAllEngine would grant admin, but I-17 is a hard gate that
	// ignores ABAC for private streams — admin does NOT override.
	f := newQueryStreamHistoryTestServer(t, policytest.AllowAllEngine())
	foreignScene := core.NewULID()
	stream := sceneICStream(foreignScene)

	_, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")
}

func TestQueryStreamHistoryI17DeniesOtherCharacterPersonalStreamRead(t *testing.T) {
	// Use an engine that would error if called — confirms character
	// streams also bypass ABAC for non-owners.
	engine := policytest.NewErrorEngine(errors.New("ABAC must not be consulted for private streams"))
	f := newQueryStreamHistoryTestServer(t, engine)

	otherChar := core.NewULID()
	stream := characterStream(otherChar)

	_, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")
}

func TestQueryStreamHistoryI17PermitsMemberSceneStreamRead(t *testing.T) {
	engine := policytest.NewErrorEngine(errors.New("ABAC must not be consulted for private streams"))
	f := newQueryStreamHistoryTestServer(t, engine)
	// Session already has FocusMembership{Kind:Scene, TargetID: sceneID}.
	stream := sceneICStream(f.sceneID)
	appendTestEvent(t, f.eventStore, stream)

	resp, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
}

func TestQueryStreamHistoryI17PermitsOwnCharacterStreamRead(t *testing.T) {
	engine := policytest.NewErrorEngine(errors.New("ABAC must not be consulted for private streams"))
	f := newQueryStreamHistoryTestServer(t, engine)
	stream := characterStream(f.charID)
	appendTestEvent(t, f.eventStore, stream)

	resp, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
}

func TestQueryStreamHistoryI17DeniesAfterFocusMembershipRemoved(t *testing.T) {
	// Regression test: if a character's FocusMembership is removed, reads
	// of that scene stream MUST immediately deny, proving I-17 is
	// membership-based (evaluated on every request) rather than a
	// one-time check. Simulates a character leaving a scene.
	engine := policytest.NewErrorEngine(errors.New("ABAC must not be consulted for private streams"))
	f := newQueryStreamHistoryTestServer(t, engine)
	// Fixture already seeded FocusMemberships with sceneID.
	stream := sceneICStream(f.sceneID)
	appendTestEvent(t, f.eventStore, stream)

	// First request: permitted (membership present).
	req := buildHistoryRequest(f.sessionID, stream, 10)
	_, err := f.server.QueryStreamHistory(context.Background(), req)
	require.NoError(t, err, "first request must be permitted with membership present")

	// Simulate leave: persist a session with no memberships.
	require.NoError(t, f.sessStore.Set(context.Background(), f.sessionID, &session.Info{
		ID:               f.sessionID,
		CharacterID:      f.charID,
		CharacterName:    "Alice",
		LocationID:       f.locID,
		Status:           session.StatusActive,
		EventCursors:     map[string]ulid.ULID{},
		FocusMemberships: nil,
	}))

	// Second request: denied (membership removed).
	_, err = f.server.QueryStreamHistory(context.Background(), req)
	errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")
}

func TestQueryStreamHistoryI17DeniesSceneStreamWithMalformedTargetID(t *testing.T) {
	f := newQueryStreamHistoryTestServer(t, policytest.AllowAllEngine())
	stream := "scene:not-a-ulid:ic"

	_, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	errutil.AssertErrorCode(t, err, "INVALID_ARGUMENT")
}

// ---------- §8.3 Boundary tests ----------

func TestQueryStreamHistoryReturnsEmptyEventsForEmptyStream(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f := newQueryStreamHistoryTestServer(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	resp, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 10))
	require.NoError(t, err)
	assert.Empty(t, resp.Events)
	assert.False(t, resp.HasMore)
}

func TestQueryStreamHistoryPaginationWalksBackwardCorrectly(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f := newQueryStreamHistoryTestServer(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	// 7 events, page size 3 → 3 pages (3, 3, 1).
	events := make([]core.Event, 0, 7)
	for i := 0; i < 7; i++ {
		events = append(events, appendTestEvent(t, f.eventStore, stream))
	}

	// Page 1: latest 3 → events[4..6], has_more=true.
	r1, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 3))
	require.NoError(t, err)
	require.Len(t, r1.Events, 3)
	assert.True(t, r1.HasMore)
	assert.Equal(t, events[4].ID.String(), r1.Events[0].Id)
	assert.Equal(t, events[6].ID.String(), r1.Events[2].Id)

	// Page 2: use before_id = oldest of page 1 → events[1..3], has_more=true.
	req2 := buildHistoryRequest(f.sessionID, stream, 3)
	req2.BeforeId = r1.Events[0].Id
	r2, err := f.server.QueryStreamHistory(context.Background(), req2)
	require.NoError(t, err)
	require.Len(t, r2.Events, 3)
	assert.True(t, r2.HasMore)
	assert.Equal(t, events[1].ID.String(), r2.Events[0].Id)
	assert.Equal(t, events[3].ID.String(), r2.Events[2].Id)

	// Page 3: before_id = oldest of page 2 → events[0], has_more=false.
	req3 := buildHistoryRequest(f.sessionID, stream, 3)
	req3.BeforeId = r2.Events[0].Id
	r3, err := f.server.QueryStreamHistory(context.Background(), req3)
	require.NoError(t, err)
	require.Len(t, r3.Events, 1)
	assert.False(t, r3.HasMore)
	assert.Equal(t, events[0].ID.String(), r3.Events[0].Id)
}

func TestQueryStreamHistoryBeforeIDAtOldestEventReturnsEmpty(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f := newQueryStreamHistoryTestServer(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	oldest := appendTestEvent(t, f.eventStore, stream)
	appendTestEvent(t, f.eventStore, stream)
	appendTestEvent(t, f.eventStore, stream)

	req := buildHistoryRequest(f.sessionID, stream, 10)
	req.BeforeId = oldest.ID.String()

	resp, err := f.server.QueryStreamHistory(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, resp.Events)
	assert.False(t, resp.HasMore)
}

func TestQueryStreamHistoryCountExactlyMatchesAvailableEvents(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f := newQueryStreamHistoryTestServer(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	for i := 0; i < 5; i++ {
		appendTestEvent(t, f.eventStore, stream)
	}

	resp, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 5))
	require.NoError(t, err)
	assert.Len(t, resp.Events, 5)
	assert.False(t, resp.HasMore)
}

func TestQueryStreamHistoryCountOneLessThanAvailable(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f := newQueryStreamHistoryTestServer(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	for i := 0; i < 5; i++ {
		appendTestEvent(t, f.eventStore, stream)
	}

	resp, err := f.server.QueryStreamHistory(context.Background(), buildHistoryRequest(f.sessionID, stream, 4))
	require.NoError(t, err)
	assert.Len(t, resp.Events, 4)
	assert.True(t, resp.HasMore)
}

func TestQueryStreamHistoryNotBeforeFiltersOldEvents(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f := newQueryStreamHistoryTestServer(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	// Hand-craft 3 events: first two in the past, last now. MemoryEventStore
	// filters by Timestamp.Before(notBefore).
	now := time.Now()
	past := now.Add(-24 * time.Hour)
	oldEvent := core.Event{
		ID:        core.NewULID(),
		Stream:    stream,
		Type:      core.EventTypeSay,
		Timestamp: past,
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "old"},
		Payload:   []byte(`{}`),
	}
	require.NoError(t, f.eventStore.Append(context.Background(), oldEvent))
	// Ensure second append is newer via NewEvent's fresh timestamp.
	newEvent := appendTestEvent(t, f.eventStore, stream)

	floor := now.Add(-1 * time.Minute)
	req := buildHistoryRequest(f.sessionID, stream, 10)
	req.NotBeforeMs = floor.UnixMilli()

	resp, err := f.server.QueryStreamHistory(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	assert.Equal(t, newEvent.ID.String(), resp.Events[0].Id)
}

func TestQueryStreamHistoryNotBeforeAndBeforeIDCombined(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f, cs := newCountingFixture(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	// Just verify both filters are forwarded.
	floor := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	beforeID := core.NewULID()
	req := buildHistoryRequest(f.sessionID, stream, 10)
	req.NotBeforeMs = floor.UnixMilli()
	req.BeforeId = beforeID.String()

	_, err := f.server.QueryStreamHistory(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, cs.lastNotBefore.Equal(floor))
	assert.Equal(t, beforeID, cs.lastBeforeID)
}

func TestQueryStreamHistoryBeforeIDWithZeroULIDIgnored(t *testing.T) {
	grant := policytest.NewGrantEngine()
	f, cs := newCountingFixture(t, grant)
	stream := locationStream(f.locID)
	grant.Grant("character:"+f.charID.String(), accessTypes.ActionRead, "stream:"+stream)

	req := buildHistoryRequest(f.sessionID, stream, 10)
	req.BeforeId = "" // empty, not "00000000000000000000000000"

	_, err := f.server.QueryStreamHistory(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, cs.lastBeforeID.IsZero(), "empty before_id should translate to zero ULID (no cursor filter)")
}
