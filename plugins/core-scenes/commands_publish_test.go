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
