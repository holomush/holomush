// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPublishCmdFixture wires a scenePlugin over a fakeStore seeded so that
// StartScenePublish succeeds for an ended scene within budget. Returns the
// plugin plus the scene and (valid-ULID) caller ids.
func newPublishCmdFixture(t *testing.T, state SceneState) (*scenePlugin, string, string) {
	t.Helper()
	sceneID := ulid.Make().String()
	callerID := ulid.Make().String()
	store := newFakeStore()
	store.scenes[sceneID] = &SceneRow{ID: sceneID, OwnerID: callerID, State: string(state)}
	store.installRoster(sceneID, callerID) // caller is the owner-participant
	store.maxPublishAttempts[sceneID] = 3
	store.attemptCounts[sceneID] = AttemptCounts{}
	return &scenePlugin{service: NewSceneServiceImpl(store)}, sceneID, callerID
}

// TestHandlePublishStartsAttempt — the bare "scene publish #<id>" form starts a
// publish-vote attempt and reports the attempt number.
func TestHandlePublishStartsAttempt(t *testing.T) {
	t.Parallel()
	p, sceneID, callerID := newPublishCmdFixture(t, SceneStateEnded)

	resp, err := p.handlePublish(context.Background(),
		pluginsdk.CommandRequest{CharacterID: callerID}, "#"+sceneID)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "attempt #1")
	assert.Contains(t, resp.Output, sceneID)
}

// TestHandlePublishRejectsUnresolvableScene — a non-"#" arg can't be resolved
// to a scene; the command errors (REF_INVALID) without calling the service.
func TestHandlePublishRejectsUnresolvableScene(t *testing.T) {
	t.Parallel()
	p, _, callerID := newPublishCmdFixture(t, SceneStateEnded)

	resp, err := p.handlePublish(context.Background(),
		pluginsdk.CommandRequest{CharacterID: callerID}, "garbage")

	require.NoError(t, err)
	assert.NotEqual(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "scene reference must use")
}

// TestHandlePublishDeniesNonParticipant — a non-participant cannot start a
// publish vote, even with an explicit "#<id>" for an ended scene (closes the
// gap where the publish ABAC policy is inert at command dispatch).
func TestHandlePublishDeniesNonParticipant(t *testing.T) {
	t.Parallel()
	p, sceneID, _ := newPublishCmdFixture(t, SceneStateEnded)
	outsider := ulid.Make().String() // valid ULID, NOT on the roster

	resp, err := p.handlePublish(context.Background(),
		pluginsdk.CommandRequest{CharacterID: outsider}, "#"+sceneID)

	require.NoError(t, err)
	assert.NotEqual(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "not a participant")
}

// TestHandlePublishSurfacesStartError — a precondition failure from
// StartScenePublish (here: scene not ended) surfaces as a command error, not a
// success.
func TestHandlePublishSurfacesStartError(t *testing.T) {
	t.Parallel()
	p, sceneID, callerID := newPublishCmdFixture(t, SceneStateActive)

	resp, err := p.handlePublish(context.Background(),
		pluginsdk.CommandRequest{CharacterID: callerID}, "#"+sceneID)

	require.NoError(t, err)
	assert.NotEqual(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Could not start publish vote")
}

// newVoteCmdFixture wires a scenePlugin with a COLLECTING attempt for a scene
// and `voter` on its roster, so handleVote's CastPublishSceneVote path
// succeeds. Returns the plugin, scene id, and the on-roster voter id.
func newVoteCmdFixture(t *testing.T) (*scenePlugin, string, string) {
	t.Helper()
	sceneID := ulid.Make().String()
	attemptID := ulid.Make().String()
	voter := ulid.Make().String()
	store := newFakeStore()
	store.installPublishedAttempt(attemptID, sceneID, StatusCollecting)
	store.installVoters(attemptID, voter)
	return &scenePlugin{service: NewSceneServiceImpl(store)}, sceneID, voter
}

// TestHandleVoteYesRecordsVote — "scene publish vote yes #<id>" casts a yes vote
// on the scene's active attempt (routed through handlePublish).
func TestHandleVoteYesRecordsVote(t *testing.T) {
	t.Parallel()
	p, sceneID, voter := newVoteCmdFixture(t)

	resp, err := p.handlePublish(context.Background(),
		pluginsdk.CommandRequest{CharacterID: voter}, "vote yes #"+sceneID)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "recorded")
	assert.Contains(t, resp.Output, "yes")
}

// TestHandleVoteChangeReportsChanged — re-casting a different vote reports the
// change.
func TestHandleVoteChangeReportsChanged(t *testing.T) {
	t.Parallel()
	p, sceneID, voter := newVoteCmdFixture(t)
	req := pluginsdk.CommandRequest{CharacterID: voter}

	_, err := p.handlePublish(context.Background(), req, "vote yes #"+sceneID)
	require.NoError(t, err)
	resp, err := p.handlePublish(context.Background(), req, "vote no #"+sceneID)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "changed")
	assert.Contains(t, resp.Output, "no")
}

// TestHandleVoteRejectsNonVoter — a character not on the frozen roster cannot
// vote (CastVote → SCENE_PUBLISH_NOT_A_VOTER).
func TestHandleVoteRejectsNonVoter(t *testing.T) {
	t.Parallel()
	p, sceneID, _ := newVoteCmdFixture(t)
	outsider := ulid.Make().String()

	resp, err := p.handlePublish(context.Background(),
		pluginsdk.CommandRequest{CharacterID: outsider}, "vote yes #"+sceneID)

	require.NoError(t, err)
	assert.NotEqual(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Could not cast vote")
}

// TestHandleVoteNoActiveAttempt — voting on a scene with no active attempt is a
// command error.
func TestHandleVoteNoActiveAttempt(t *testing.T) {
	t.Parallel()
	sceneID := ulid.Make().String()
	voter := ulid.Make().String()
	p := &scenePlugin{service: NewSceneServiceImpl(newFakeStore())}

	resp, err := p.handlePublish(context.Background(),
		pluginsdk.CommandRequest{CharacterID: voter}, "vote yes #"+sceneID)

	require.NoError(t, err)
	assert.NotEqual(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "no active publish vote")
}

// TestHandleVoteRejectsBadDirection — a vote direction other than yes|no is a
// usage error.
func TestHandleVoteRejectsBadDirection(t *testing.T) {
	t.Parallel()
	p, sceneID, voter := newVoteCmdFixture(t)

	resp, err := p.handlePublish(context.Background(),
		pluginsdk.CommandRequest{CharacterID: voter}, "vote maybe #"+sceneID)

	require.NoError(t, err)
	assert.NotEqual(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Usage: scene publish vote")
}
