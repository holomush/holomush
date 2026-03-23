// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
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
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(&session.Info{
		ID:          ulid.Make().String(),
		CharacterID: executorID,
		Status:      session.StatusActive,
	})

	char := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessPolicyEngine(t)
	store := core.NewMemoryEventStore()

	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(char, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Admin",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
			Events:  store,
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrSessionEnded), "self-boot should return ErrSessionEnded")

	// Verify output message
	output := buf.String()
	assert.Contains(t, output, "Disconnecting")
}

func TestBootHandler_SelfBoot_WithReason(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(&session.Info{
		ID:          ulid.Make().String(),
		CharacterID: executorID,
		Status:      session.StatusActive,
	})

	char := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessPolicyEngine(t)
	store := core.NewMemoryEventStore()

	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(char, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Admin going to bed",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
			Events:  store,
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrSessionEnded), "self-boot should return ErrSessionEnded")

	// Verify notification event was appended to session stream
	events, replayErr := store.Replay(context.Background(), "session:"+executorID.String(), ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	require.Len(t, events, 1)
	event := events[0]
	assert.Equal(t, core.EventTypeSystem, event.Type)
	assert.Contains(t, string(event.Payload), "going to bed")
}

func TestBootHandler_BootOthers_WithoutCapability(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: targetID, Status: session.StatusActive},
	)

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
	accessControl := policytest.NewGrantEngine()
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
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "RegularUser",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
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
	targetSess, _ := mockSession.FindByCharacter(context.Background(), targetID)
	assert.NotNil(t, targetSess, "Target session should still exist")
}

func TestBootHandler_EngineError_ReturnsAccessEvaluationFailed(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: targetID, Status: session.StatusActive},
	)

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
	// Two engines: WorldService gets a grant engine so character lookups succeed,
	// while the boot capability check (exec.Services().Engine()) gets an error engine
	// to simulate a policy store outage. This tests that boot-specific capability
	// failures surface correctly even when world-level access checks pass.
	worldEngine := policytest.NewGrantEngine()
	worldEngine.Grant(access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String())
	worldEngine.Grant(access.SubjectCharacter+executorID.String(), "read", "character:"+targetID.String())

	// Session iteration order is non-deterministic
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        worldEngine,
	})

	engineErr := errors.New("policy store unavailable")
	errorEngine := policytest.NewErrorEngine(engineErr)

	// Capture log output
	var logBuf bytes.Buffer
	oldLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(oldLogger)

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "RegularUser",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
			Engine:  errorEngine,
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeAccessEvaluationFailed, oopsErr.Code())

	// Verify target session still exists (was not booted)
	targetSess, _ := mockSession.FindByCharacter(context.Background(), targetID)
	assert.NotNil(t, targetSess, "Target session should still exist")

	// Verify log output contains error and context
	logOutput := logBuf.String()
	subjectID := access.CharacterSubject(executorID.String())
	assert.Contains(t, logOutput, "boot access evaluation failed", "log should mention boot access evaluation failure")
	assert.Contains(t, logOutput, subjectID, "log should contain subject")
	assert.Contains(t, logOutput, "execute", "log should contain action")
	assert.Contains(t, logOutput, "admin.boot", "log should contain resource (capability)")
	assert.Contains(t, logOutput, "policy store unavailable", "log should contain error message")
}

func TestBootHandler_InfraFailure_ReturnsAccessEvaluationFailed(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: targetID, Status: session.StatusActive},
	)

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
	// Use a grant engine for world service (character lookups succeed)
	worldEngine := policytest.NewGrantEngine()
	worldEngine.Grant(access.SubjectCharacter+executorID.String(), "read", "character:"+executorID.String())
	worldEngine.Grant(access.SubjectCharacter+executorID.String(), "read", "character:"+targetID.String())

	// Session iteration order is non-deterministic
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        worldEngine,
	})

	// Use infra failure engine for the command's access check
	infraEngine := policytest.NewInfraFailureEngine(t, "session resolution failed", "infra:session-resolver")

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "RegularUser",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
			Engine:  infraEngine,
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)

	// Verify ACCESS_EVALUATION_FAILED code (NOT PERMISSION_DENIED)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeAccessEvaluationFailed, oopsErr.Code())

	// Verify target session still exists (was not booted)
	targetSess, _ := mockSession.FindByCharacter(context.Background(), targetID)
	assert.NotNil(t, targetSess, "Target session should still exist")
}

func TestBootHandler_TargetNotFound(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
	)

	execChar := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

	// Executor character lookup for self-boot check
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "NonexistentPlayer",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
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
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: targetID, Status: session.StatusActive},
	)

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
	accessControl := worldtest.NewMockAccessPolicyEngine(t)
	store := core.NewMemoryEventStore()

	// Session iteration order is non-deterministic, so executor lookup may or may not happen
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(targetID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
			Engine:  policytest.AllowAllEngine(),
			Events:  store,
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify target session was removed
	targetSess, _ := mockSession.FindByCharacter(context.Background(), targetID)
	assert.Nil(t, targetSess, "Target session should be ended")

	// Verify executor session still exists
	execSess, _ := mockSession.FindByCharacter(context.Background(), executorID)
	assert.NotNil(t, execSess, "Executor session should still exist")

	// Verify output message
	output := buf.String()
	assert.Contains(t, output, "Troublemaker")
	assert.Contains(t, output, "booted")
}

func TestBootHandler_SuccessWithReason(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: targetID, Status: session.StatusActive},
	)

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
	accessControl := worldtest.NewMockAccessPolicyEngine(t)
	store := core.NewMemoryEventStore()

	// Session iteration order is non-deterministic, so executor lookup may or may not happen
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(targetID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker Being disruptive",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
			Engine:  policytest.AllowAllEngine(),
			Events:  store,
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify target session was removed
	targetSess, _ := mockSession.FindByCharacter(context.Background(), targetID)
	assert.Nil(t, targetSess, "Target session should be ended")

	// Verify output includes reason
	output := buf.String()
	assert.Contains(t, output, "Troublemaker")
	assert.Contains(t, output, "booted")
	assert.Contains(t, output, "Being disruptive")

	// Verify notification event was appended to target's session stream
	events, replayErr := store.Replay(context.Background(), "session:"+targetID.String(), ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	require.Len(t, events, 1)
	event := events[0]
	assert.Equal(t, core.EventTypeSystem, event.Type)
	assert.Contains(t, string(event.Payload), "Being disruptive")
	assert.Contains(t, string(event.Payload), "Admin")
}

func TestBootHandler_CaseInsensitiveMatch(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: targetID, Status: session.StatusActive},
	)

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
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

	// Session iteration order is non-deterministic, so executor lookup may or may not happen
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(targetID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "troublemaker", // lowercase
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
			Engine:  policytest.AllowAllEngine(),
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify target session was removed
	targetSess, _ := mockSession.FindByCharacter(context.Background(), targetID)
	assert.Nil(t, targetSess, "Target session should be ended")
}

func TestBootHandler_SkipsInaccessibleCharacters(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	hiddenID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: targetID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: hiddenID, Status: session.StatusActive},
	)

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
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

	// Session iteration order is non-deterministic, so all lookups may or may not happen
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration - accessible
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(targetID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	// Hidden character - access denied (may not be called if target found first)
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(hiddenID.String())}).
		Return(types.NewDecision(types.EffectDeny, "", ""), nil).Maybe()

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
			Engine:  policytest.AllowAllEngine(),
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify target session was removed
	targetSess, _ := mockSession.FindByCharacter(context.Background(), targetID)
	assert.Nil(t, targetSess, "Target session should be ended")

	// Verify hidden session still exists (wasn't incorrectly targeted)
	hiddenSess, _ := mockSession.FindByCharacter(context.Background(), hiddenID)
	assert.NotNil(t, hiddenSess, "Hidden session should still exist")
}

func TestBootHandler_EndSessionError(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	playerID := ulid.Make()

	mockSession := &errorOnDeleteSessionAccess{
		MockSessionAccess: testutil.NewMockSessionAccess(
			&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
			&session.Info{ID: ulid.Make().String(), CharacterID: targetID, Status: session.StatusActive},
		),
		targetCharID: targetID,
	}

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
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

	// Executor lookup may or may not happen
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(targetID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
			Engine:  policytest.AllowAllEngine(),
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)

	// DeleteByCharacter failed — verify the error propagates.
	// The oops chain carries the WORLD_ERROR code with a "message" context key.
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Contains(t, fmt.Sprint(oopsErr.Context()["message"]), "Unable to boot player")
}

// errorOnDeleteSessionAccess wraps a MockSessionAccess but fails DeleteByCharacter for a specific target.
type errorOnDeleteSessionAccess struct {
	*testutil.MockSessionAccess
	targetCharID ulid.ULID
}

func (m *errorOnDeleteSessionAccess) DeleteByCharacter(ctx context.Context, charID ulid.ULID, reason string) (*session.Info, error) {
	if charID == m.targetCharID {
		return nil, oops.Code("SESSION_NOT_FOUND").Errorf("session not found")
	}
	return m.MockSessionAccess.DeleteByCharacter(ctx, charID, reason)
}

func TestBootHandler_LogsUnexpectedGetCharacterErrors(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	errorCharID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: targetID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: errorCharID, Status: session.StatusActive},
	)

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
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

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
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration - accessible
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(targetID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	// Error character - access allowed but repo returns unexpected error
	unexpectedErr := errors.New("database connection timeout")
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(errorCharID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, errorCharID).
		Return(nil, unexpectedErr).Maybe()

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
			Engine:  policytest.AllowAllEngine(),
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify target session was removed
	targetSess, _ := mockSession.FindByCharacter(context.Background(), targetID)
	assert.Nil(t, targetSess, "Target session should be ended")

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
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: errorCharID, Status: session.StatusActive},
	)

	execChar := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

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
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Error character lookup - returns unexpected database error
	dbError := errors.New("database connection timeout")
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(errorCharID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, errorCharID).
		Return(nil, dbError).Maybe()

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "NonexistentPlayer", // Target doesn't exist, but errors occurred
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
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
	assert.Contains(t, playerMsg, "Please try again shortly")
}

func TestBootHandler_BootOthers_IncludesDecisionContext(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: targetID, Status: session.StatusActive},
	)

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
	accessControl := policytest.NewGrantEngine()
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
		Engine:        accessControl,
	})

	// Create a mock engine that denies admin.boot with specific reason and policy ID
	bootEngine := worldtest.NewMockAccessPolicyEngine(t)
	bootEngine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  access.CharacterSubject(executorID.String()),
			Action:   "execute",
			Resource: "admin.boot",
		}).
		Return(types.NewDecision(types.EffectDeny, "policy violation detected", "policy-123"), nil)

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "RegularUser",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
			Engine:  bootEngine,
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodePermissionDenied, oopsErr.Code())

	// Verify decision context is propagated
	ctx := oopsErr.Context()
	assert.Equal(t, "policy violation detected", ctx["reason"], "decision reason should be propagated")
	assert.Equal(t, "policy-123", ctx["policy_id"], "policy ID should be propagated")

	// Verify target session still exists (was not booted)
	targetSess, _ := mockSession.FindByCharacter(context.Background(), targetID)
	assert.NotNil(t, targetSess, "Target session should still exist")
}

func TestBootHandler_MixedEngineAndDBErrors_ReturnsWorldError(t *testing.T) {
	executorID := ulid.Make()
	engineErrCharID := ulid.Make()
	dbErrCharID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: engineErrCharID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: dbErrCharID, Status: session.StatusActive},
	)

	execChar := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

	// Suppress logs during test
	var logBuf bytes.Buffer
	originalLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(originalLogger)

	// Executor lookup - may or may not happen depending on iteration order
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Engine-failure character: access allowed but world returns an ACCESS_EVALUATION_FAILED-coded error
	engineErr := oops.Code("ACCESS_EVALUATION_FAILED").Errorf("access engine outage")
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(engineErrCharID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, engineErrCharID).
		Return(nil, engineErr).Maybe()

	// DB-failure character: access allowed but world returns a generic database error
	dbErr := errors.New("database connection timeout")
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(dbErrCharID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, dbErrCharID).
		Return(nil, dbErr).Maybe()

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "NonexistentPlayer", // Target not in any session
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)

	// Mixed engine + DB errors must fall through to CodeWorldError, not CodeAccessEvaluationFailed
	assert.Equal(t, command.CodeWorldError, oopsErr.Code())

	// Verify user-facing message indicates system error
	playerMsg := command.PlayerMessage(err)
	assert.Contains(t, playerMsg, "system error")
	assert.Contains(t, playerMsg, "try again")
}

func TestBootHandler_NoLoggingForExpectedErrors(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	notFoundCharID := ulid.Make()
	deniedCharID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: targetID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: notFoundCharID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: deniedCharID, Status: session.StatusActive},
	)

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
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

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
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration - accessible
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(targetID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	// Not found character - access allowed but repo returns ErrNotFound (expected, no logging)
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(notFoundCharID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, notFoundCharID).
		Return(nil, world.ErrNotFound).Maybe()

	// Permission denied character - access check fails (expected, no logging)
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(deniedCharID.String())}).
		Return(types.NewDecision(types.EffectDeny, "", ""), nil).Maybe()

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
			Engine:  policytest.AllowAllEngine(),
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify target session was removed
	targetSess, _ := mockSession.FindByCharacter(context.Background(), targetID)
	assert.Nil(t, targetSess, "Target session should be ended")

	// Verify no error logs were written for expected errors (ErrNotFound, ErrPermissionDenied)
	logOutput := logBuf.String()
	assert.Empty(t, logOutput, "Expected errors should not be logged")
}

func TestBootHandler_SkipsCharacterWithEngineErrorDuringLookup(t *testing.T) {
	executorID := ulid.Make()
	targetID := ulid.Make()
	evalFailID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: targetID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: evalFailID, Status: session.StatusActive},
	)

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
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

	// Capture log output
	var logBuf bytes.Buffer
	originalLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(originalLogger)

	// Executor lookup - may or may not happen
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// Target lookup during session iteration - succeeds
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(targetID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	characterRepo.EXPECT().
		Get(mock.Anything, targetID).
		Return(targetChar, nil)

	// One character lookup fails with engine error - should be counted and skipped
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(evalFailID.String())}).
		Return(types.Decision{}, errors.New("policy store unavailable")).Maybe()

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Troublemaker",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
			Engine:  policytest.AllowAllEngine(),
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify target session was removed (search continued past engine error)
	targetSess, _ := mockSession.FindByCharacter(context.Background(), targetID)
	assert.Nil(t, targetSess, "Target session should be ended")

	// Verify the character with engine error was skipped (session still exists)
	evalFailSess, _ := mockSession.FindByCharacter(context.Background(), evalFailID)
	assert.NotNil(t, evalFailSess, "Character with engine error should be skipped, not booted")

	// Verify output indicates success
	output := buf.String()
	assert.Contains(t, output, "Troublemaker")
	assert.Contains(t, output, "booted")

	// Verify engine error was logged (by world.Service.checkAccess)
	logOutput := logBuf.String()
	if strings.Contains(logOutput, "access evaluation failed") {
		// If engine error occurred during iteration, verify it was logged
		assert.Contains(t, logOutput, "policy store unavailable", "log should contain error message")
	}
}

func TestCheckCapability(t *testing.T) {
	ctx := context.Background()
	executorID := ulid.Make()
	subjectID := access.CharacterSubject(executorID.String())

	tests := []struct {
		name           string
		engine         types.AccessPolicyEngine
		subject        string
		capability     string
		cmdName        string
		expectedError  string
		expectedCode   string
		checkLogs      bool
		expectedLogMsg string
	}{
		{
			name:          "permission granted",
			engine:        policytest.AllowAllEngine(),
			subject:       subjectID,
			capability:    "admin.boot",
			cmdName:       "boot",
			expectedError: "",
			expectedCode:  "",
		},
		{
			name:          "permission denied",
			engine:        policytest.DenyAllEngine(),
			subject:       subjectID,
			capability:    "admin.boot",
			cmdName:       "boot",
			expectedError: "permission denied",
			expectedCode:  command.CodePermissionDenied,
		},
		{
			name:           "engine evaluation failure",
			engine:         policytest.NewErrorEngine(errors.New("policy store unavailable")),
			subject:        subjectID,
			capability:     "admin.boot",
			cmdName:        "boot",
			expectedError:  "policy store unavailable",
			expectedCode:   command.CodeAccessEvaluationFailed,
			checkLogs:      true,
			expectedLogMsg: "boot access evaluation failed",
		},
		{
			name:           "request construction failure - empty subject",
			engine:         policytest.AllowAllEngine(),
			subject:        "",
			capability:     "admin.boot",
			cmdName:        "boot",
			expectedError:  "subject must not be empty",
			expectedCode:   command.CodeAccessEvaluationFailed,
			checkLogs:      true,
			expectedLogMsg: "boot access request construction failed",
		},
		{
			name:           "infrastructure failure decision",
			engine:         policytest.NewInfraFailureEngine(t, "session resolution failed", "infra:session-resolver"),
			subject:        subjectID,
			capability:     "admin.boot",
			cmdName:        "boot",
			expectedError:  "capability check failed",
			expectedCode:   command.CodeAccessEvaluationFailed,
			checkLogs:      true,
			expectedLogMsg: "boot access check infrastructure failure",
		},
		{
			name:           "request construction failure - empty capability",
			engine:         policytest.AllowAllEngine(),
			subject:        subjectID,
			capability:     "",
			cmdName:        "test",
			expectedError:  "resource must not be empty",
			expectedCode:   command.CodeAccessEvaluationFailed,
			checkLogs:      true,
			expectedLogMsg: "test access request construction failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture logs if needed
			var logBuf bytes.Buffer
			var oldLogger *slog.Logger
			if tt.checkLogs {
				oldLogger = slog.Default()
				testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))
				slog.SetDefault(testLogger)
				defer slog.SetDefault(oldLogger)
			}

			err := command.CheckCapability(ctx, tt.engine, tt.subject, tt.capability, tt.cmdName)

			if tt.expectedError == "" {
				require.NoError(t, err)
				return
			}

			// Expect an error
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)

			// Check error code
			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok)
			assert.Equal(t, tt.expectedCode, oopsErr.Code())

			// Check logs if needed
			if tt.checkLogs {
				logOutput := logBuf.String()
				assert.Contains(t, logOutput, tt.expectedLogMsg, "log should contain expected message")
				assert.Contains(t, logOutput, tt.subject, "log should contain subject")
				assert.Contains(t, logOutput, "execute", "log should contain action")
				assert.Contains(t, logOutput, tt.capability, "log should contain capability")
			}
		})
	}
}

func TestCheckCapability_EvalErr_IncludesErrCapabilityCheckFailed(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())

	// Engine returns a Go error (evalErr != nil path)
	engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))

	err := command.CheckCapability(ctx, engine, subjectID, "admin.boot", "boot")
	require.Error(t, err)

	// After the fix, errors.Is should match ErrCapabilityCheckFailed
	assert.ErrorIs(t, err, command.ErrCapabilityCheckFailed,
		"evalErr branch should include ErrCapabilityCheckFailed in the error chain")
}

func TestBootHandler_AccessEvaluationFailedReturnsSystemError(t *testing.T) {
	executorID := ulid.Make()
	evalFail1ID := ulid.Make()
	evalFail2ID := ulid.Make()
	playerID := ulid.Make()

	mockSession := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: executorID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: evalFail1ID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: evalFail2ID, Status: session.StatusActive},
	)

	execChar := &world.Character{
		ID:       executorID,
		PlayerID: playerID,
		Name:     "Admin",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

	// Capture log output
	var logBuf bytes.Buffer
	originalLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(originalLogger)

	// Executor lookup - may or may not happen
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(executorID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, executorID).
		Return(execChar, nil).Maybe()

	// All character lookups fail with access evaluation errors
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(evalFail1ID.String())}).
		Return(types.Decision{}, errors.New("policy store unavailable")).Maybe()
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executorID.String()), Action: "read", Resource: access.CharacterResource(evalFail2ID.String())}).
		Return(types.Decision{}, errors.New("policy store unavailable")).Maybe()

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "NonexistentPlayer",
		Output:        &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			Session: mockSession,
			World:   worldService,
		}),
	})

	err := BootHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)

	// Should return ACCESS_EVALUATION_FAILED when all errors are engine failures
	assert.Equal(t, command.CodeAccessEvaluationFailed, oopsErr.Code())

	// Verify user-facing message maps to the access evaluation failure message
	playerMsg := command.PlayerMessage(err)
	assert.Contains(t, playerMsg, "Permission check failed")

	// Verify log output contains errors and context (logged by world.Service.checkAccess)
	logOutput := logBuf.String()
	subjectID := access.CharacterSubject(executorID.String())
	assert.Contains(t, logOutput, "access evaluation failed", "log should mention access evaluation failure")
	assert.Contains(t, logOutput, subjectID, "log should contain subject")
	assert.Contains(t, logOutput, "read", "log should contain action")
	assert.Contains(t, logOutput, "policy store unavailable", "log should contain error message")
	// Should have logged at least one character lookup failure
	resource1 := access.CharacterResource(evalFail1ID.String())
	resource2 := access.CharacterResource(evalFail2ID.String())
	assert.True(t, strings.Contains(logOutput, resource1) || strings.Contains(logOutput, resource2),
		"log should contain at least one character resource ID")
}
