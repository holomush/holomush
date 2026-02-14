// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/accesstest"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
)

func TestBootHandler_NoArgs(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "",
		Output:        &buf,
		Services:      command.NewTestServices(command.ServicesConfig{}),
	})

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
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(char, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Admin",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session:     sessionMgr,
			World:       worldService,
			Broadcaster: broadcaster,
		}),
	})

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
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(char, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Admin going to bed",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session:     sessionMgr,
			World:       worldService,
			Broadcaster: broadcaster,
		}),
	})

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
	accessControl.Grant(access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String())
	accessControl.Grant(access.SubjectCharacter+executorID.String(), "read", "character:"+targetID.String())

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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "RegularUser",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: sessionMgr,
			World:   worldService,
			Engine:  policytest.DenyAllEngine(),
		}),
	})

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
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "NonexistentPlayer",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: sessionMgr,
			World:   worldService,
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeTargetNotFound, oopsErr.Code())

	// Verify target name is captured in context
	target, ok := oopsErr.Context()["target"].(string)
	require.True(t, ok)
	assert.Equal(t, "NonexistentPlayer", target)

	// Verify PlayerMessage returns appropriate user-facing message
	playerMsg := command.PlayerMessage(err)
	assert.Equal(t, "Target not found: NonexistentPlayer", playerMsg)
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
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+targetID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session:     sessionMgr,
			World:       worldService,
			Engine:      policytest.AllowAllEngine(),
			Broadcaster: broadcaster,
		}),
	})

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
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+targetID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker Being disruptive",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session:     sessionMgr,
			World:       worldService,
			Engine:      policytest.AllowAllEngine(),
			Broadcaster: broadcaster,
		}),
	})

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
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+targetID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "troublemaker", // lowercase
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: sessionMgr,
			World:   worldService,
			Engine:  policytest.AllowAllEngine(),
		}),
	})

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
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration - accessible
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+targetID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	// Hidden character - access denied (may not be called if target found first)
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+hiddenID.String()).
		Return(false).Maybe()

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: sessionMgr,
			World:   worldService,
			Engine:  policytest.AllowAllEngine(),
		}),
	})

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
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+targetID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSessionMgr,
			World:   worldService,
			Engine:  policytest.AllowAllEngine(),
		}),
	})

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

func TestBootHandler_LogsUnexpectedGetCharacterErrors(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	errorCharID := ulid.Make()
	execConn := ulid.Make()
	targetConn := ulid.Make()
	errorConn := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, execConn)
	sessionMgr.Connect(targetID, targetConn)
	sessionMgr.Connect(errorCharID, errorConn)

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

	// Capture logs
	var logBuf bytes.Buffer
	originalLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(originalLogger)

	// Session iteration order is non-deterministic, so all lookups may or may not happen
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration - accessible
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+targetID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	// Error character - access allowed but repo returns unexpected error
	unexpectedErr := errors.New("database connection timeout")
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+errorCharID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, errorCharID).
		Return(nil, unexpectedErr).Maybe()

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: sessionMgr,
			World:   worldService,
			Engine:  policytest.AllowAllEngine(),
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify target session was ended
	targetSession := sessionMgr.GetSession(targetID)
	assert.Nil(t, targetSession, "Target session should be ended")

	// The error character lookup may or may not have happened depending on iteration order.
	// If it did happen, the error should have been logged.
	logOutput := logBuf.String()
	if logOutput != "" {
		// If we have any log output, verify it contains the expected content
		assert.Contains(t, logOutput, "unexpected error looking up character")
		assert.Contains(t, logOutput, "target_name")
		assert.Contains(t, logOutput, "session_char_id")
		assert.Contains(t, logOutput, "database connection timeout")
	}
	// Note: We don't fail if there's no log output because the error character
	// might not have been processed before the target was found.
}

func TestBootHandler_SystemErrorWhenAllLookupsFailWithUnexpectedErrors(t *testing.T) {
	executorID := ulid.Make()
	errorCharID := ulid.Make()
	execConn := ulid.Make()
	errorConn := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, execConn)
	sessionMgr.Connect(errorCharID, errorConn)

	execChar := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// Capture logs (suppress during test)
	var logBuf bytes.Buffer
	originalLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(originalLogger)

	// Executor lookup - may or may not happen depending on iteration order
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Error character lookup - returns unexpected database error
	dbError := errors.New("database connection timeout")
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+errorCharID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, errorCharID).
		Return(nil, dbError).Maybe()

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "NonexistentPlayer", // Target doesn't exist, but errors occurred
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: sessionMgr,
			World:   worldService,
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)

	// Verify error is a WorldError (system error), not TargetNotFound
	assert.Equal(t, command.CodeWorldError, oopsErr.Code())

	// Verify user-facing message indicates system error
	playerMsg := command.PlayerMessage(err)
	assert.Contains(t, playerMsg, "system error")
	assert.Contains(t, playerMsg, "Try again")
}

func TestBootHandler_NoLoggingForExpectedErrors(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	notFoundCharID := ulid.Make()
	deniedCharID := ulid.Make()
	execConn := ulid.Make()
	targetConn := ulid.Make()
	notFoundConn := ulid.Make()
	deniedConn := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, execConn)
	sessionMgr.Connect(targetID, targetConn)
	sessionMgr.Connect(notFoundCharID, notFoundConn)
	sessionMgr.Connect(deniedCharID, deniedConn)

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

	// Capture logs
	var logBuf bytes.Buffer
	originalLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(originalLogger)

	// Session iteration order is non-deterministic, so all lookups may or may not happen
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration - accessible
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+targetID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	// Not found character - access allowed but repo returns ErrNotFound (expected, no logging)
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+notFoundCharID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, notFoundCharID).
		Return(nil, world.ErrNotFound).Maybe()

	// Permission denied character - access check fails (expected, no logging)
	accessControl.EXPECT().
		Check(mock.Anything, access.SubjectCharacter+executorID.String(), "read", "character:"+deniedCharID.String()).
		Return(false).Maybe()

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: sessionMgr,
			World:   worldService,
			Engine:  policytest.AllowAllEngine(),
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify target session was ended
	targetSession := sessionMgr.GetSession(targetID)
	assert.Nil(t, targetSession, "Target session should be ended")

	// Verify no error logs were written for expected errors (ErrNotFound, ErrPermissionDenied)
	logOutput := logBuf.String()
	assert.Empty(t, logOutput, "Expected errors should not be logged")
}
