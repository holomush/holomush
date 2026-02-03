// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/accesstest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
)

func TestBootHandler_NoArgs(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "",
		Output:        &buf,
		Services:      &command.Services{},
	}

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
}

func TestBootHandler_SelfBoot_Success(t *testing.T) {
	executorID := ulid.Make()
	connID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, connID)

	char := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)
	broadcaster := core.NewBroadcaster()

	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+executorID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(char, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Admin",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			World:       worldService,
			Broadcaster: broadcaster,
		},
	}

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify session was ended
	session := sessionMgr.GetSession(executorID)
	assert.Nil(t, session, "Session should be ended after self-boot")

	// Verify output message
	output := buf.String()
	assert.Contains(t, output, "Disconnecting")
}

func TestBootHandler_SelfBoot_WithReason(t *testing.T) {
	executorID := ulid.Make()
	connID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, connID)

	char := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)
	broadcaster := core.NewBroadcaster()

	// Subscribe to capture the notification event
	ch := broadcaster.Subscribe("session:" + executorID.String())

	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+executorID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(char, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Admin going to bed",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			World:       worldService,
			Broadcaster: broadcaster,
		},
	}

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify notification was sent
	select {
	case event := <-ch:
		assert.Equal(t, core.EventTypeSystem, event.Type)
		assert.Contains(t, string(event.Payload), "going to bed")
	default:
		t.Error("Expected notification event to be broadcast")
	}
}

func TestBootHandler_BootOthers_WithoutCapability(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	execConn := ulid.Make()
	targetConn := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, execConn)
	sessionMgr.Connect(targetID, targetConn)

	execChar := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "RegularUser",
	}
	targetChar := &world.Character{
		ID:       targetID,
		PlayerID: playerID,
		Name:     "Troublemaker",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	// Use selective mock that allows read but denies admin.boot
	accessControl := accesstest.NewMockAccessControl()
	// Grant read access to characters (needed for findCharacterByName)
	accessControl.Grant("char:"+executorID.String(), "read", "character:"+executorID.String())
	accessControl.Grant("char:"+executorID.String(), "read", "character:"+targetID.String())
	// Do NOT grant execute access to admin.boot

	// Session iteration order is non-deterministic, so executor lookup may or may not happen
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "RegularUser",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: &command.Services{
			Session: sessionMgr,
			World:   worldService,
			Access:  accessControl,
		},
	}

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodePermissionDenied, oopsErr.Code())

	// Verify target session still exists (was not booted)
	targetSession := sessionMgr.GetSession(targetID)
	assert.NotNil(t, targetSession, "Target session should still exist")
}

func TestBootHandler_TargetNotFound(t *testing.T) {
	executorID := ulid.Make()
	execConn := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, execConn)

	execChar := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// Executor character lookup for self-boot check
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+executorID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "NonexistentPlayer",
		Output:        &buf,
		Services: &command.Services{
			Session: sessionMgr,
			World:   worldService,
		},
	}

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeTargetNotFound, oopsErr.Code())
}

func TestBootHandler_Success(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	execConn := ulid.Make()
	targetConn := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, execConn)
	sessionMgr.Connect(targetID, targetConn)

	execChar := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}
	targetChar := &world.Character{
		ID:       targetID,
		PlayerID: playerID,
		Name:     "Troublemaker",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)
	broadcaster := core.NewBroadcaster()

	// Session iteration order is non-deterministic, so executor lookup may or may not happen
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+executorID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+targetID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	// Capability check for booting another user
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "execute", "admin.boot").
		Return(true)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			World:       worldService,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify target session was ended
	targetSession := sessionMgr.GetSession(targetID)
	assert.Nil(t, targetSession, "Target session should be ended")

	// Verify executor session still exists
	execSession := sessionMgr.GetSession(executorID)
	assert.NotNil(t, execSession, "Executor session should still exist")

	// Verify output message
	output := buf.String()
	assert.Contains(t, output, "Troublemaker")
	assert.Contains(t, output, "booted")
}

func TestBootHandler_SuccessWithReason(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	execConn := ulid.Make()
	targetConn := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, execConn)
	sessionMgr.Connect(targetID, targetConn)

	execChar := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}
	targetChar := &world.Character{
		ID:       targetID,
		PlayerID: playerID,
		Name:     "Troublemaker",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)
	broadcaster := core.NewBroadcaster()

	// Subscribe to capture the notification event
	ch := broadcaster.Subscribe("session:" + targetID.String())

	// Session iteration order is non-deterministic, so executor lookup may or may not happen
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+executorID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+targetID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	// Capability check for booting another user
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "execute", "admin.boot").
		Return(true)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker Being disruptive",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			World:       worldService,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify target session was ended
	targetSession := sessionMgr.GetSession(targetID)
	assert.Nil(t, targetSession, "Target session should be ended")

	// Verify output includes reason
	output := buf.String()
	assert.Contains(t, output, "Troublemaker")
	assert.Contains(t, output, "booted")
	assert.Contains(t, output, "Being disruptive")

	// Verify notification was sent to target
	select {
	case event := <-ch:
		assert.Equal(t, core.EventTypeSystem, event.Type)
		assert.Contains(t, string(event.Payload), "Being disruptive")
		assert.Contains(t, string(event.Payload), "Admin")
	default:
		t.Error("Expected notification event to be broadcast to target")
	}
}

func TestBootHandler_CaseInsensitiveMatch(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	execConn := ulid.Make()
	targetConn := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, execConn)
	sessionMgr.Connect(targetID, targetConn)

	execChar := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}
	targetChar := &world.Character{
		ID:       targetID,
		PlayerID: playerID,
		Name:     "Troublemaker",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// Session iteration order is non-deterministic, so executor lookup may or may not happen
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+executorID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+targetID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	// Capability check for booting another user
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "execute", "admin.boot").
		Return(true)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "troublemaker", // lowercase
		Output:        &buf,
		Services: &command.Services{
			Session: sessionMgr,
			World:   worldService,
			Access:  accessControl,
		},
	}

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify target session was ended
	targetSession := sessionMgr.GetSession(targetID)
	assert.Nil(t, targetSession, "Target session should be ended")
}

func TestBootHandler_SkipsInaccessibleCharacters(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	hiddenID := ulid.Make()
	execConn := ulid.Make()
	targetConn := ulid.Make()
	hiddenConn := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, execConn)
	sessionMgr.Connect(targetID, targetConn)
	sessionMgr.Connect(hiddenID, hiddenConn)

	execChar := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}
	targetChar := &world.Character{
		ID:       targetID,
		PlayerID: playerID,
		Name:     "Troublemaker",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// Session iteration order is non-deterministic, so all lookups may or may not happen
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+executorID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration - accessible
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+targetID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	// Hidden character - access denied (may not be called if target found first)
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+hiddenID.String()).
		Return(false).Maybe()

	// Capability check for booting another user
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "execute", "admin.boot").
		Return(true)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: &command.Services{
			Session: sessionMgr,
			World:   worldService,
			Access:  accessControl,
		},
	}

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify target session was ended
	targetSession := sessionMgr.GetSession(targetID)
	assert.Nil(t, targetSession, "Target session should be ended")

	// Verify hidden session still exists (wasn't incorrectly targeted)
	hiddenSession := sessionMgr.GetSession(hiddenID)
	assert.NotNil(t, hiddenSession, "Hidden session should still exist")
}

func TestBootHandler_EndSessionError(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	execConn := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, execConn)
	// Note: target is NOT connected, so EndSession will fail

	execChar := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}
	targetChar := &world.Character{
		ID:       targetID,
		PlayerID: playerID,
		Name:     "Troublemaker",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// Session iteration finds only executor (target not connected)
	// But we need to simulate a situation where we find the target name
	// Let's create a mock session manager that returns target in list but fails EndSession
	mockSessionMgr := &mockSessionManagerWithEndSessionError{
		underlying: sessionMgr,
		targetID:   targetID,
	}
	// Add target to underlying so it appears in ListActiveSessions
	sessionMgr.Connect(targetID, ulid.Make())

	// Executor lookup may or may not happen
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+executorID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+targetID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	// Capability check for booting another user
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "execute", "admin.boot").
		Return(true)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: &command.Services{
			Session: mockSessionMgr,
			World:   worldService,
			Access:  accessControl,
		},
	}

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeWorldError, oopsErr.Code())
	assert.Contains(t, oopsErr.Context()["message"], "Unable to boot player")
}

// mockSessionManagerWithEndSessionError wraps a real session manager but fails EndSession for a specific target.
type mockSessionManagerWithEndSessionError struct {
	underlying *core.SessionManager
	targetID   ulid.ULID
}

func (m *mockSessionManagerWithEndSessionError) ListActiveSessions() []*core.Session {
	return m.underlying.ListActiveSessions()
}

func (m *mockSessionManagerWithEndSessionError) GetSession(charID ulid.ULID) *core.Session {
	return m.underlying.GetSession(charID)
}

func (m *mockSessionManagerWithEndSessionError) Connect(charID, connID ulid.ULID) *core.Session {
	return m.underlying.Connect(charID, connID)
}

func (m *mockSessionManagerWithEndSessionError) EndSession(charID ulid.ULID) error {
	if charID == m.targetID {
		return errors.New("session already ended")
	}
	return m.underlying.EndSession(charID)
}
