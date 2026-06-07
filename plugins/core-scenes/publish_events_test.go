// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// TestEmitPublishStartedSetsSubjectTypeAndPayload verifies that
// emitPublishStarted emits on the correct IC subject, uses the correct
// event type, and marshals the proto payload with attempt metadata and roster.
func TestEmitPublishStartedSetsSubjectTypeAndPayload(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.installPublishedAttempt("pub-1", "scene-1", StatusCollecting)
	store.installVoters("pub-1", "char-1", "char-2")

	sink := &recordingEventSink{}
	em := newPublishEventEmitter(sink, store, "test-game")
	pub := &PublishedScene{
		ID:            "pub-1",
		SceneID:       "scene-1",
		AttemptNumber: 1,
		InitiatedBy:   "char-1",
		VoteWindow:    7 * 24 * time.Hour,
		CoolOffWindow: 30 * time.Minute,
	}

	err := em.emitPublishStarted(context.Background(), pub)
	require.NoError(t, err)
	require.Len(t, sink.intents, 1)
	intent := sink.intents[0]

	assert.Equal(t, dotStyleSceneSubjectIC("test-game", "scene-1"), intent.Subject)
	assert.Equal(t, pluginsdk.EventType("core-scenes:scene_publish_started"), intent.Type)
	assert.False(t, intent.Sensitive, "scene_publish_started is sensitivity:never")

	var ev scenev1.ScenePublishStartedEvent
	require.NoError(t, protojson.Unmarshal([]byte(intent.Payload), &ev))
	assert.Equal(t, "pub-1", ev.AttemptId)
	assert.Equal(t, int32(1), ev.AttemptNumber)
	assert.Equal(t, "char-1", ev.InitiatedBy)
	assert.ElementsMatch(t, []string{"char-1", "char-2"}, ev.RosterCharacterIds)
	assert.Equal(t, int64(7*24*60*60), ev.VoteWindowSeconds)
	assert.Equal(t, int64(30*60), ev.CooloffWindowSeconds)
}

// TestEmitPublishVoteCastSetsSubjectTypeAndPayload verifies that emitVoteCast
// fetches the scene_id from the header and emits the correct proto fields.
func TestEmitPublishVoteCastSetsSubjectTypeAndPayload(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.installPublishedAttempt("pub-2", "scene-2", StatusCollecting)

	sink := &recordingEventSink{}
	em := newPublishEventEmitter(sink, store, "test-game")

	result := &CastVoteResult{Vote: true, IsChange: false}
	err := em.emitVoteCast(context.Background(), "pub-2", "char-alice", result)
	require.NoError(t, err)
	require.Len(t, sink.intents, 1)
	intent := sink.intents[0]

	assert.Equal(t, dotStyleSceneSubjectIC("test-game", "scene-2"), intent.Subject)
	assert.Equal(t, pluginsdk.EventType("core-scenes:scene_publish_vote_cast"), intent.Type)
	assert.False(t, intent.Sensitive)

	var ev scenev1.ScenePublishVoteCastEvent
	require.NoError(t, protojson.Unmarshal([]byte(intent.Payload), &ev))
	assert.Equal(t, "pub-2", ev.AttemptId)
	assert.Equal(t, "char-alice", ev.CharacterId)
	assert.True(t, ev.Vote)
	assert.False(t, ev.IsChange)
}

// TestEmitPublishCoolOffStartedSetsSubjectTypeAndPayload verifies that
// emitCoolOffStarted emits on the IC facet with a future unix-ns timestamp.
func TestEmitPublishCoolOffStartedSetsSubjectTypeAndPayload(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.installPublishedAttempt("pub-3", "scene-3", StatusCollecting)

	sink := &recordingEventSink{}
	em := newPublishEventEmitter(sink, store, "test-game")

	before := time.Now().Add(30 * time.Minute).UnixNano()
	err := em.emitCoolOffStarted(context.Background(), "pub-3", 30*time.Minute)
	after := time.Now().Add(30 * time.Minute).UnixNano()

	require.NoError(t, err)
	require.Len(t, sink.intents, 1)
	intent := sink.intents[0]

	assert.Equal(t, dotStyleSceneSubjectIC("test-game", "scene-3"), intent.Subject)
	assert.Equal(t, pluginsdk.EventType("core-scenes:scene_publish_cooloff_started"), intent.Type)
	assert.False(t, intent.Sensitive)

	var ev scenev1.ScenePublishCoolOffStartedEvent
	require.NoError(t, protojson.Unmarshal([]byte(intent.Payload), &ev))
	assert.Equal(t, "pub-3", ev.AttemptId)
	assert.GreaterOrEqual(t, ev.CooloffEndsAtUnixNs, before, "cooloff_ends_at_unix_ns must be >= before-window")
	assert.LessOrEqual(t, ev.CooloffEndsAtUnixNs, after, "cooloff_ends_at_unix_ns must be <= after-window")
}

// TestEmitPublishResolvedSetsSubjectTypeAndPayload verifies that emitResolved
// encodes the final outcome, failure_reason, and tally counts correctly.
func TestEmitPublishResolvedSetsSubjectTypeAndPayload(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.installPublishedAttempt("pub-4", "scene-4", StatusCoolOff)

	sink := &recordingEventSink{}
	em := newPublishEventEmitter(sink, store, "test-game")

	reason := FailureAnyNo
	tally := &VoteTally{Yes: 1, No: 2, Pending: 0}
	err := em.emitResolved(context.Background(), "pub-4", StatusAttemptFailed, &reason, tally)
	require.NoError(t, err)
	require.Len(t, sink.intents, 1)
	intent := sink.intents[0]

	assert.Equal(t, dotStyleSceneSubjectIC("test-game", "scene-4"), intent.Subject)
	assert.Equal(t, pluginsdk.EventType("core-scenes:scene_publish_resolved"), intent.Type)
	assert.False(t, intent.Sensitive)

	var ev scenev1.ScenePublishResolvedEvent
	require.NoError(t, protojson.Unmarshal([]byte(intent.Payload), &ev))
	assert.Equal(t, "pub-4", ev.AttemptId)
	assert.Equal(t, string(StatusAttemptFailed), ev.Outcome)
	assert.Equal(t, string(FailureAnyNo), ev.FailureReason)
	assert.Equal(t, int32(1), ev.TallyYes)
	assert.Equal(t, int32(2), ev.TallyNo)
	assert.Equal(t, int32(0), ev.TallyPending)
}

// TestEmitPublishWithdrawnSetsSubjectTypeAndPayload verifies that emitWithdrawn
// emits the attempt_id and withdrawn_by fields.
func TestEmitPublishWithdrawnSetsSubjectTypeAndPayload(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.installPublishedAttempt("pub-5", "scene-5", StatusCollecting)

	sink := &recordingEventSink{}
	em := newPublishEventEmitter(sink, store, "test-game")

	err := em.emitWithdrawn(context.Background(), "pub-5", "char-owner")
	require.NoError(t, err)
	require.Len(t, sink.intents, 1)
	intent := sink.intents[0]

	assert.Equal(t, dotStyleSceneSubjectIC("test-game", "scene-5"), intent.Subject)
	assert.Equal(t, pluginsdk.EventType("core-scenes:scene_publish_withdrawn"), intent.Type)
	assert.False(t, intent.Sensitive)

	var ev scenev1.ScenePublishWithdrawnEvent
	require.NoError(t, protojson.Unmarshal([]byte(intent.Payload), &ev))
	assert.Equal(t, "pub-5", ev.AttemptId)
	assert.Equal(t, "char-owner", ev.WithdrawnBy)
}

// TestEmitPublishAttemptsExtendedSetsSubjectTypeAndPayload verifies that
// emitAttemptsExtended emits directly on the scene's IC facet (no header
// lookup needed) with the correct extension counts.
func TestEmitPublishAttemptsExtendedSetsSubjectTypeAndPayload(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	sink := &recordingEventSink{}
	em := newPublishEventEmitter(sink, store, "test-game")

	err := em.emitAttemptsExtended(context.Background(), "scene-6", "char-admin", 2, 5)
	require.NoError(t, err)
	require.Len(t, sink.intents, 1)
	intent := sink.intents[0]

	assert.Equal(t, dotStyleSceneSubjectIC("test-game", "scene-6"), intent.Subject)
	assert.Equal(t, pluginsdk.EventType("core-scenes:scene_publish_vote_attempts_extended"), intent.Type)
	assert.False(t, intent.Sensitive)

	var ev scenev1.ScenePublishVoteAttemptsExtendedEvent
	require.NoError(t, protojson.Unmarshal([]byte(intent.Payload), &ev))
	assert.Equal(t, "scene-6", ev.SceneId)
	assert.Equal(t, "char-admin", ev.AdminId)
	assert.Equal(t, int32(2), ev.Additional)
	assert.Equal(t, int32(5), ev.NewMax)
}

// TestEmitPublishStartedPropagatesSinkError verifies that a sink Emit
// failure is surfaced without modification.
func TestEmitPublishStartedPropagatesSinkError(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.installPublishedAttempt("pub-err", "scene-err", StatusCollecting)
	store.installVoters("pub-err", "char-1")

	sink := &recordingEventSink{err: assert.AnError}
	em := newPublishEventEmitter(sink, store, "test-game")
	pub := &PublishedScene{
		ID: "pub-err", SceneID: "scene-err", AttemptNumber: 1, InitiatedBy: "char-1",
		VoteWindow: 7 * 24 * time.Hour, CoolOffWindow: 30 * time.Minute,
	}

	err := em.emitPublishStarted(context.Background(), pub)
	assert.ErrorIs(t, err, assert.AnError)
}

// TestEmitPublishStartedEmitsValidJSONPayload locks the payload-encoding
// contract: the host emit path (internal/plugin/event_emitter.go) rejects any
// payload that is not valid JSON, so the emitter MUST produce valid JSON
// (protojson, not binary proto.Marshal). A regression to binary proto would
// fail json.Valid here at the unit level — before it would silently break the
// real emit path (which no current integration test exercises).
func TestEmitPublishStartedEmitsValidJSONPayload(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.installVoters("pub-json", "char-1")
	sink := &recordingEventSink{}
	em := newPublishEventEmitter(sink, store, "test-game")
	pub := &PublishedScene{
		ID: "pub-json", SceneID: "scene-json", AttemptNumber: 1, InitiatedBy: "char-1",
		VoteWindow: 7 * 24 * time.Hour, CoolOffWindow: 30 * time.Minute,
	}

	require.NoError(t, em.emitPublishStarted(context.Background(), pub))
	require.Len(t, sink.intents, 1)
	assert.True(t, json.Valid([]byte(sink.intents[0].Payload)),
		"the host emit path enforces json.Valid(payload); the emitter MUST emit valid JSON")
}

// TestEmitVoteCastReturnsErrorNotPanicWhenAttemptMissing covers the
// nil-header path: GetPublishedSceneHeader returns (nil, nil) for a missing
// attempt, and the emitter must surface a clean error rather than nil-deref.
func TestEmitVoteCastReturnsErrorNotPanicWhenAttemptMissing(t *testing.T) {
	t.Parallel()

	store := newFakeStore() // no attempt installed → GetPublishedSceneHeader returns nil,nil
	sink := &recordingEventSink{}
	em := newPublishEventEmitter(sink, store, "test-game")

	err := em.emitVoteCast(context.Background(), "missing-attempt", "char-1", &CastVoteResult{Vote: true})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_PUBLISH_NOT_FOUND")
	assert.Empty(t, sink.intents, "no event emitted when the attempt is missing")
}
