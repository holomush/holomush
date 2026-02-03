// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

func TestCommandEntry_HasRequiredFields(t *testing.T) {
	entry := &CommandEntry{
		Name:         "say",
		Capabilities: []string{"rp:speak"},
		Help:         "Say something to the room",
		Usage:        "say <message>",
		HelpText:     "Speaks a message to everyone in the current location.",
		Source:       "core",
	}

	assert.Equal(t, "say", entry.Name)
	assert.Equal(t, []string{"rp:speak"}, entry.Capabilities)
	assert.Equal(t, "Say something to the room", entry.Help)
	assert.Equal(t, "say <message>", entry.Usage)
	assert.Equal(t, "Speaks a message to everyone in the current location.", entry.HelpText)
	assert.Equal(t, "core", entry.Source)
	assert.Nil(t, entry.Handler, "Handler should be nil when not set")
}

func TestCommandExecution_HasRequiredFields(t *testing.T) {
	exec := &CommandExecution{}

	// Verify all ULID fields are zero when not set
	assert.True(t, exec.CharacterID.IsZero(), "CharacterID should be zero when not set")
	assert.True(t, exec.LocationID.IsZero(), "LocationID should be zero when not set")
	assert.True(t, exec.PlayerID.IsZero(), "PlayerID should be zero when not set")
	assert.True(t, exec.SessionID.IsZero(), "SessionID should be zero when not set")

	// Verify string fields
	assert.Empty(t, exec.CharacterName, "CharacterName should be empty when not set")
	assert.Empty(t, exec.Args, "Args should be empty when not set")

	// Verify pointer fields
	assert.Nil(t, exec.Output, "Output should be nil when not set")
	assert.Nil(t, exec.Services, "Services should be nil when not set")
}

func TestServices_HasAllDependencies(t *testing.T) {
	svc := &Services{}

	assert.Nil(t, svc.World, "World service should be nil when not set")
	assert.Nil(t, svc.Session, "Session service should be nil when not set")
	assert.Nil(t, svc.Access, "Access service should be nil when not set")
	assert.Nil(t, svc.Events, "Events service should be nil when not set")
}

func TestNewServices_NilWorld_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:       nil,
		Session:     &mockSessionService{},
		Access:      &mockAccessControl{},
		Events:      &mockEventStore{},
		Broadcaster: &core.Broadcaster{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "World")
}

func TestNewServices_NilSession_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:       &world.Service{},
		Session:     nil,
		Access:      &mockAccessControl{},
		Events:      &mockEventStore{},
		Broadcaster: &core.Broadcaster{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Session")
}

func TestNewServices_NilAccess_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:       &world.Service{},
		Session:     &mockSessionService{},
		Access:      nil,
		Events:      &mockEventStore{},
		Broadcaster: &core.Broadcaster{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Access")
}

func TestNewServices_NilEvents_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:       &world.Service{},
		Session:     &mockSessionService{},
		Access:      &mockAccessControl{},
		Events:      nil,
		Broadcaster: &core.Broadcaster{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Events")
}

func TestNewServices_NilBroadcaster_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:       &world.Service{},
		Session:     &mockSessionService{},
		Access:      &mockAccessControl{},
		Events:      &mockEventStore{},
		Broadcaster: nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Broadcaster")
}

func TestNewServices_AllValid_ReturnsServices(t *testing.T) {
	worldSvc := &world.Service{}
	sessionSvc := &mockSessionService{}
	accessCtrl := &mockAccessControl{}
	eventStore := &mockEventStore{}
	broadcaster := &core.Broadcaster{}

	svc, err := NewServices(ServicesConfig{
		World:       worldSvc,
		Session:     sessionSvc,
		Access:      accessCtrl,
		Events:      eventStore,
		Broadcaster: broadcaster,
	})
	require.NoError(t, err)
	assert.Same(t, worldSvc, svc.World)
	assert.Same(t, sessionSvc, svc.Session)
	assert.Same(t, accessCtrl, svc.Access)
	assert.Same(t, eventStore, svc.Events)
	assert.Same(t, broadcaster, svc.Broadcaster)
}

func TestNewServices_MultipleNil_ReturnsFirstError(t *testing.T) {
	// When multiple fields are nil, should return error mentioning
	// World since that's checked first
	_, err := NewServices(ServicesConfig{
		World:       nil,
		Session:     nil,
		Access:      nil,
		Events:      nil,
		Broadcaster: nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "World")
}

// Mock types for testing
type mockSessionService struct{}

func (m *mockSessionService) ListActiveSessions() []*core.Session   { return nil }
func (m *mockSessionService) GetSession(_ ulid.ULID) *core.Session  { return nil }
func (m *mockSessionService) EndSession(_ ulid.ULID) error          { return nil }

type mockAccessControl struct{}

func (m *mockAccessControl) Check(_ context.Context, _, _, _ string) bool { return false }

type mockEventStore struct{}

func (m *mockEventStore) Append(_ context.Context, _ core.Event) error { return nil }
func (m *mockEventStore) Replay(_ context.Context, _ string, _ ulid.ULID, _ int) ([]core.Event, error) {
	return nil, nil
}
func (m *mockEventStore) LastEventID(_ context.Context, _ string) (ulid.ULID, error) {
	return ulid.ULID{}, nil
}
func (m *mockEventStore) Subscribe(_ context.Context, _ string) (<-chan ulid.ULID, <-chan error, error) {
	return nil, nil, nil
}

func TestCommandHandler_Signature(t *testing.T) {
	// Verify CommandHandler can be assigned a function with the correct signature
	var handler CommandHandler = func(_ context.Context, _ *CommandExecution) error {
		return nil
	}
	assert.NotNil(t, handler, "Handler should be assignable")
}
