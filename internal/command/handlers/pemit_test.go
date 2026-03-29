// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

func makePemitExec(
	t *testing.T,
	senderID ulid.ULID,
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
		CharacterName: "GM",
		LocationID:    ulid.Make(),
		SessionID:     ulid.Make(),
		Output:        &buf,
		Services:      svc,
	})
	exec.Args = args
	return exec, &buf
}

func TestPemitHandler_Valid(t *testing.T) {
	senderID := ulid.Make()
	targetID := ulid.Make()

	mock := testutil.NewMockSessionAccess()
	mock.AddSession(senderID, "GM")
	mock.AddSession(targetID, "Sean")

	store := &captureStore{}
	exec, buf := makePemitExec(t, senderID, "Sean=You hear a whisper", store, mock)

	err := PemitHandler(context.Background(), exec)
	require.NoError(t, err)

	// One event emitted to target's character stream.
	require.Len(t, store.events, 1)
	ev := store.events[0]
	assert.Equal(t, core.EventTypePemit, ev.Type)
	assert.Equal(t, world.CharacterStream(targetID), ev.Stream)
	assert.Contains(t, string(ev.Payload), "You hear a whisper")

	// Sender sees confirmation.
	assert.Contains(t, buf.String(), "Pemit sent to Sean.")
}

func TestPemitHandler_CaseInsensitiveTarget(t *testing.T) {
	senderID := ulid.Make()
	targetID := ulid.Make()

	mock := testutil.NewMockSessionAccess()
	mock.AddSession(senderID, "GM")
	mock.AddSession(targetID, "Sean")

	store := &captureStore{}
	exec, buf := makePemitExec(t, senderID, "sean=A shadow passes over you.", store, mock)

	err := PemitHandler(context.Background(), exec)
	require.NoError(t, err)

	require.Len(t, store.events, 1)
	assert.Equal(t, world.CharacterStream(targetID), store.events[0].Stream)
	assert.Contains(t, buf.String(), "Pemit sent to Sean.")
}

func TestPemitHandler_MissingEquals(t *testing.T) {
	senderID := ulid.Make()

	mock := testutil.NewMockSessionAccess()
	store := &captureStore{}
	exec, _ := makePemitExec(t, senderID, "just a message", store, mock)

	err := PemitHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
	assert.Empty(t, store.events)
}

func TestPemitHandler_EmptyMessage(t *testing.T) {
	senderID := ulid.Make()

	mock := testutil.NewMockSessionAccess()
	store := &captureStore{}
	exec, _ := makePemitExec(t, senderID, "Sean=", store, mock)

	err := PemitHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
	assert.Empty(t, store.events)
}

func TestPemitHandler_UnknownCharacter(t *testing.T) {
	senderID := ulid.Make()

	mock := testutil.NewMockSessionAccess()
	mock.AddSession(senderID, "GM")
	// "Nobody" is not in the mock.

	store := &captureStore{}
	exec, buf := makePemitExec(t, senderID, "Nobody=hello", store, mock)

	err := PemitHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Empty(t, store.events)
	assert.Contains(t, buf.String(), `"Nobody"`)
}

func TestPemitHandler_CharacterNotOnline(t *testing.T) {
	senderID := ulid.Make()

	mock := testutil.NewMockSessionAccess()
	mock.AddSession(senderID, "GM")
	// Gandalf exists in world but has no active session — mock returns nil for unknown names.

	store := &captureStore{}
	exec, buf := makePemitExec(t, senderID, "Gandalf=hello", store, mock)

	err := PemitHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Empty(t, store.events)
	assert.Contains(t, buf.String(), "Gandalf")
}

func TestPemitHandler_PayloadContainsSenderInfo(t *testing.T) {
	senderID := ulid.Make()
	targetID := ulid.Make()

	mock := testutil.NewMockSessionAccess()
	mock.AddSession(senderID, "GM")
	mock.AddSession(targetID, "Player")

	store := &captureStore{}
	exec, _ := makePemitExec(t, senderID, "Player=Secret event!", store, mock)

	err := PemitHandler(context.Background(), exec)
	require.NoError(t, err)

	require.Len(t, store.events, 1)
	payload := string(store.events[0].Payload)
	assert.Contains(t, payload, "Secret event!")
	assert.Contains(t, payload, "GM") // sender_name in payload
	assert.Contains(t, payload, senderID.String())
	assert.Contains(t, payload, targetID.String())
}
