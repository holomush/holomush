// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
)

// captureStore wraps stubEventStoreCapture to capture all appended events.
type captureStore struct {
	events []core.Event
}

func (c *captureStore) Append(_ context.Context, e core.Event) error {
	c.events = append(c.events, e)
	return nil
}

func (c *captureStore) Replay(_ context.Context, _ string, _ ulid.ULID, _ int) ([]core.Event, error) {
	return nil, nil
}

func (c *captureStore) LastEventID(_ context.Context, _ string) (ulid.ULID, error) {
	return ulid.ULID{}, nil
}

func (c *captureStore) Subscribe(_ context.Context, _ string) (<-chan ulid.ULID, <-chan error, error) {
	return nil, nil, nil
}

// makePageExec builds a CommandExecution for page handler tests.
const testSenderName = "Sean"

func makePageExec(
	t *testing.T,
	senderID ulid.ULID,
	sessionID ulid.ULID,
	args string,
	store core.EventStore,
	sessionAccess *testutil.MockSessionAccess,
) (*command.CommandExecution, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	svc := testutil.NewServicesBuilder().
		WithSession(sessionAccess).
		WithEvents(store).
		Build()
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   senderID,
		CharacterName: testSenderName,
		LocationID:    ulid.Make(),
		SessionID:     sessionID,
		Output:        &buf,
		Services:      svc,
	})
	exec.Args = args
	return exec, &buf
}

func TestPageHandler_Basic(t *testing.T) {
	senderID := ulid.Make()
	targetID := ulid.Make()
	sessionID := ulid.Make()

	mock := testutil.NewMockSessionAccess()
	mock.AddSession(senderID, "Sean")
	mock.AddSession(targetID, "Alex")

	store := &captureStore{}
	exec, buf := makePageExec(t, senderID, sessionID, "alex=Hey there", store, mock)

	err := PageHandler(context.Background(), exec)
	require.NoError(t, err)

	// Should emit exactly one event (to target's stream).
	require.Len(t, store.events, 1)
	ev := store.events[0]
	assert.Equal(t, core.EventTypePage, ev.Type)
	assert.Equal(t, world.CharacterStream(targetID), ev.Stream)
	assert.Contains(t, string(ev.Payload), "Sean pages: Hey there")
	assert.Contains(t, string(ev.Payload), `"is_pose":false`)

	// Sender sees confirmation.
	assert.Contains(t, buf.String(), "You paged Alex: Hey there")
}

func TestPageHandler_LastPaged(t *testing.T) {
	senderID := ulid.Make()
	targetID := ulid.Make()
	sessionID := ulid.Make()

	mock := testutil.NewMockSessionAccess(
		&session.Info{
			ID:            sessionID.String(),
			CharacterID:   senderID,
			CharacterName: "Sean",
			Status:        session.StatusActive,
			LastPaged:     "Alex",
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		},
		&session.Info{
			ID:            ulid.Make().String(),
			CharacterID:   targetID,
			CharacterName: "Alex",
			Status:        session.StatusActive,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		},
	)

	store := &captureStore{}
	exec, buf := makePageExec(t, senderID, sessionID, "How's it going?", store, mock)

	err := PageHandler(context.Background(), exec)
	require.NoError(t, err)

	require.Len(t, store.events, 1)
	assert.Equal(t, world.CharacterStream(targetID), store.events[0].Stream)
	assert.Contains(t, buf.String(), "You paged Alex: How's it going?")
}

func TestPageHandler_NoLastPaged(t *testing.T) {
	senderID := ulid.Make()
	sessionID := ulid.Make()

	mock := testutil.NewMockSessionAccess(
		&session.Info{
			ID:            sessionID.String(),
			CharacterID:   senderID,
			CharacterName: "Sean",
			Status:        session.StatusActive,
			LastPaged:     "", // no last-paged
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		},
	)

	store := &captureStore{}
	exec, buf := makePageExec(t, senderID, sessionID, "Hey", store, mock)

	err := PageHandler(context.Background(), exec)
	require.NoError(t, err)

	// No events emitted.
	assert.Empty(t, store.events)
	assert.Contains(t, buf.String(), "no last-paged character")
}

func TestPageHandler_Pose(t *testing.T) {
	senderID := ulid.Make()
	targetID := ulid.Make()
	sessionID := ulid.Make()

	mock := testutil.NewMockSessionAccess()
	mock.AddSession(senderID, "Sean")
	mock.AddSession(targetID, "Alex")

	store := &captureStore{}
	exec, buf := makePageExec(t, senderID, sessionID, "alex=:waves.", store, mock)

	err := PageHandler(context.Background(), exec)
	require.NoError(t, err)

	require.Len(t, store.events, 1)
	ev := store.events[0]
	assert.Equal(t, core.EventTypePage, ev.Type)
	assert.Contains(t, string(ev.Payload), "From afar, Sean waves.")
	assert.Contains(t, string(ev.Payload), `"is_pose":true`)

	// Sender sees long-distance format.
	assert.Contains(t, buf.String(), "Long distance to Alex: Sean waves.")
}

func TestPageHandler_PoseSemicolon(t *testing.T) {
	senderID := ulid.Make()
	targetID := ulid.Make()
	sessionID := ulid.Make()

	mock := testutil.NewMockSessionAccess()
	mock.AddSession(senderID, "Sean")
	mock.AddSession(targetID, "Alex")

	store := &captureStore{}
	exec, buf := makePageExec(t, senderID, sessionID, "alex=;'s jaw drops.", store, mock)

	err := PageHandler(context.Background(), exec)
	require.NoError(t, err)

	require.Len(t, store.events, 1)
	// No space between name and action.
	assert.Contains(t, string(store.events[0].Payload), "From afar, Sean's jaw drops.")
	assert.Contains(t, buf.String(), "Long distance to Alex: Sean's jaw drops.")
}

func TestPageHandler_TargetNotFound(t *testing.T) {
	senderID := ulid.Make()
	sessionID := ulid.Make()

	mock := testutil.NewMockSessionAccess()
	mock.AddSession(senderID, "Sean")
	// "nobody" is not in the mock.

	store := &captureStore{}
	exec, buf := makePageExec(t, senderID, sessionID, "nobody=Hey", store, mock)

	err := PageHandler(context.Background(), exec)
	require.NoError(t, err)

	// No events emitted.
	assert.Empty(t, store.events)
	assert.Contains(t, buf.String(), `"nobody"`)
	assert.Contains(t, buf.String(), "connected")
}

func TestPageHandler_EmptyMessage(t *testing.T) {
	senderID := ulid.Make()
	sessionID := ulid.Make()

	mock := testutil.NewMockSessionAccess()
	mock.AddSession(senderID, "Sean")

	store := &captureStore{}
	exec, buf := makePageExec(t, senderID, sessionID, "alex=", store, mock)
	_ = buf

	err := PageHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
}

func TestPageHandler_NoArgs(t *testing.T) {
	senderID := ulid.Make()
	sessionID := ulid.Make()

	mock := testutil.NewMockSessionAccess()

	store := &captureStore{}
	exec, buf := makePageExec(t, senderID, sessionID, "", store, mock)
	_ = buf

	err := PageHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
}
