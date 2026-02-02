// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/accesstest"
)

func TestDispatcher_Dispatch(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	// Register a test command
	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "echo",
		Capabilities: []string{"test.echo"},
		Handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			_, _ = exec.Output.Write([]byte("echoed: " + exec.Args))
			return nil
		},
		Source: "test",
	})
	require.NoError(t, err)

	// Grant capability
	charID := ulid.Make()
	mockAccess.Grant("char:"+charID.String(), "execute", "test.echo")

	dispatcher := NewDispatcher(reg, mockAccess)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: charID,
		Output:      &output,
	}

	err = dispatcher.Dispatch(context.Background(), "echo hello world", exec)
	require.NoError(t, err)
	assert.Equal(t, "hello world", capturedArgs)
	assert.Equal(t, "echoed: hello world", output.String())
}

func TestDispatcher_UnknownCommand(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()
	dispatcher := NewDispatcher(reg, mockAccess)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
	}

	err := dispatcher.Dispatch(context.Background(), "nonexistent", exec)
	require.Error(t, err)
	assert.Contains(t, PlayerMessage(err), "Unknown command")

	// Verify error code
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, CodeUnknownCommand, oopsErr.Code())
}

func TestDispatcher_PermissionDenied(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	err := reg.Register(CommandEntry{
		Name:         "admin",
		Capabilities: []string{"admin.manage"},
		Handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Source:       "core",
	})
	require.NoError(t, err)

	// Don't grant capability
	dispatcher := NewDispatcher(reg, mockAccess)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
	}

	err = dispatcher.Dispatch(context.Background(), "admin", exec)
	require.Error(t, err)
	assert.Contains(t, PlayerMessage(err), "permission")

	// Verify error code
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, CodePermissionDenied, oopsErr.Code())
}

func TestDispatcher_EmptyInput(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()
	dispatcher := NewDispatcher(reg, mockAccess)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
	}

	err := dispatcher.Dispatch(context.Background(), "", exec)
	require.Error(t, err)

	// Verify it's a parse error
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "EMPTY_INPUT", oopsErr.Code())
}

func TestDispatcher_MultipleCapabilities(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	// Register command requiring multiple capabilities
	err := reg.Register(CommandEntry{
		Name:         "dangerous",
		Capabilities: []string{"admin.manage", "admin.danger"},
		Handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Source:       "core",
	})
	require.NoError(t, err)

	charID := ulid.Make()
	subject := "char:" + charID.String()

	// Only grant one capability
	mockAccess.Grant(subject, "execute", "admin.manage")

	dispatcher := NewDispatcher(reg, mockAccess)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: charID,
		Output:      &output,
	}

	// Should fail - missing admin.danger
	err = dispatcher.Dispatch(context.Background(), "dangerous", exec)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, CodePermissionDenied, oopsErr.Code())

	// Now grant the second capability
	mockAccess.Grant(subject, "execute", "admin.danger")

	// Should succeed
	err = dispatcher.Dispatch(context.Background(), "dangerous", exec)
	require.NoError(t, err)
}

func TestDispatcher_NoCapabilitiesRequired(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	// Register command with no capabilities required
	executed := false
	err := reg.Register(CommandEntry{
		Name:         "public",
		Capabilities: nil, // No capabilities required
		Handler: func(_ context.Context, _ *CommandExecution) error {
			executed = true
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher := NewDispatcher(reg, mockAccess)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
	}

	// Should succeed without any grants
	err = dispatcher.Dispatch(context.Background(), "public", exec)
	require.NoError(t, err)
	assert.True(t, executed)
}

func TestDispatcher_HandlerError(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	handlerErr := errors.New("handler failed")
	err := reg.Register(CommandEntry{
		Name:         "failing",
		Capabilities: nil,
		Handler: func(_ context.Context, _ *CommandExecution) error {
			return handlerErr
		},
		Source: "test",
	})
	require.NoError(t, err)

	dispatcher := NewDispatcher(reg, mockAccess)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
	}

	err = dispatcher.Dispatch(context.Background(), "failing", exec)
	require.Error(t, err)
	assert.Equal(t, handlerErr, err)
}

func TestDispatcher_WhitespaceInput(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()
	dispatcher := NewDispatcher(reg, mockAccess)

	var output bytes.Buffer
	exec := &CommandExecution{Output: &output}

	// Only whitespace
	err := dispatcher.Dispatch(context.Background(), "   ", exec)
	require.Error(t, err)

	// Tabs only
	err = dispatcher.Dispatch(context.Background(), "\t\t", exec)
	require.Error(t, err)
}

func TestDispatcher_CommandWithNoArgs(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "look",
		Capabilities: nil,
		Handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher := NewDispatcher(reg, mockAccess)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
	}

	err = dispatcher.Dispatch(context.Background(), "look", exec)
	require.NoError(t, err)
	assert.Equal(t, "", capturedArgs)
}

func TestDispatcher_PreservesWhitespaceInArgs(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "say",
		Capabilities: nil,
		Handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher := NewDispatcher(reg, mockAccess)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
	}

	err = dispatcher.Dispatch(context.Background(), "say hello   world", exec)
	require.NoError(t, err)
	assert.Equal(t, "hello   world", capturedArgs)
}

func TestDispatcher_SetAliasCache(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()
	dispatcher := NewDispatcher(reg, mockAccess)

	// Initially nil
	assert.Nil(t, dispatcher.aliasCache)

	// Set cache
	cache := NewAliasCache()
	dispatcher.SetAliasCache(cache)
	assert.Equal(t, cache, dispatcher.aliasCache)

	// Set to nil
	dispatcher.SetAliasCache(nil)
	assert.Nil(t, dispatcher.aliasCache)
}

func TestDispatcher_WithoutAliasCache(t *testing.T) {
	// Ensure dispatcher works exactly as before when no alias cache is set
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "look",
		Capabilities: nil,
		Handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher := NewDispatcher(reg, mockAccess)
	// No alias cache set

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		PlayerID:    ulid.Make(),
		Output:      &output,
	}

	err = dispatcher.Dispatch(context.Background(), "look here", exec)
	require.NoError(t, err)
	assert.Equal(t, "here", capturedArgs)
}

func TestDispatcher_WithAliasCache_NoAliasMatch(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "look",
		Capabilities: nil,
		Handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher := NewDispatcher(reg, mockAccess)

	// Set up alias cache with some aliases that won't match
	cache := NewAliasCache()
	cache.LoadSystemAliases(map[string]string{
		"l": "look",
	})
	dispatcher.SetAliasCache(cache)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		PlayerID:    ulid.Make(),
		Output:      &output,
	}

	// Input is an actual command, not an alias
	err = dispatcher.Dispatch(context.Background(), "look here", exec)
	require.NoError(t, err)
	assert.Equal(t, "here", capturedArgs)
}

func TestDispatcher_WithAliasCache_SystemAliasExpanded(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "look",
		Capabilities: nil,
		Handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher := NewDispatcher(reg, mockAccess)

	// Set up alias cache with system alias
	cache := NewAliasCache()
	cache.LoadSystemAliases(map[string]string{
		"l": "look",
	})
	dispatcher.SetAliasCache(cache)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		PlayerID:    ulid.Make(),
		Output:      &output,
	}

	// Use alias 'l' which should expand to 'look'
	err = dispatcher.Dispatch(context.Background(), "l around", exec)
	require.NoError(t, err)
	assert.Equal(t, "around", capturedArgs)
}

func TestDispatcher_WithAliasCache_PlayerAliasExpanded(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "say",
		Capabilities: nil,
		Handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher := NewDispatcher(reg, mockAccess)

	// Set up alias cache with player alias
	playerID := ulid.Make()
	cache := NewAliasCache()
	cache.LoadPlayerAliases(playerID, map[string]string{
		"greet": "say Hello everyone!",
	})
	dispatcher.SetAliasCache(cache)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		PlayerID:    playerID,
		Output:      &output,
	}

	// Use alias 'greet' which should expand to 'say Hello everyone!'
	err = dispatcher.Dispatch(context.Background(), "greet", exec)
	require.NoError(t, err)
	assert.Equal(t, "Hello everyone!", capturedArgs)
}

func TestDispatcher_WithAliasCache_PlayerAliasOverridesSystem(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "say",
		Capabilities: nil,
		Handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher := NewDispatcher(reg, mockAccess)

	playerID := ulid.Make()
	cache := NewAliasCache()
	// System alias
	cache.LoadSystemAliases(map[string]string{
		"hi": "say hello from system",
	})
	// Player alias with same name (should override)
	cache.LoadPlayerAliases(playerID, map[string]string{
		"hi": "say hello from player",
	})
	dispatcher.SetAliasCache(cache)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		PlayerID:    playerID,
		Output:      &output,
	}

	// Player alias should take precedence
	err = dispatcher.Dispatch(context.Background(), "hi", exec)
	require.NoError(t, err)
	assert.Equal(t, "hello from player", capturedArgs)
}

func TestDispatcher_WithAliasCache_AliasWithExtraArgs(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "say",
		Capabilities: nil,
		Handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher := NewDispatcher(reg, mockAccess)

	cache := NewAliasCache()
	cache.LoadSystemAliases(map[string]string{
		"s": "say",
	})
	dispatcher.SetAliasCache(cache)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		PlayerID:    ulid.Make(),
		Output:      &output,
	}

	// 's' expands to 'say', with extra args appended
	err = dispatcher.Dispatch(context.Background(), "s this is my message", exec)
	require.NoError(t, err)
	assert.Equal(t, "this is my message", capturedArgs)
}

func TestDispatcher_NoCharacter(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	err := reg.Register(CommandEntry{
		Name:         "test",
		Capabilities: []string{},
		Handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Source:       "core",
	})
	require.NoError(t, err)

	dispatcher := NewDispatcher(reg, mockAccess)

	var output bytes.Buffer
	exec := &CommandExecution{
		// CharacterID intentionally left as zero value
		Output: &output,
	}

	err = dispatcher.Dispatch(context.Background(), "test", exec)
	require.Error(t, err)
	assert.Contains(t, PlayerMessage(err), "character")

	// Verify error code
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, CodeNoCharacter, oopsErr.Code())
}

func TestDispatcher_InvokedAs(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	// Register a test command that captures InvokedAs
	var capturedInvokedAs string
	err := reg.Register(CommandEntry{
		Name:         "pose",
		Capabilities: []string{"comms.pose"},
		Handler: func(_ context.Context, exec *CommandExecution) error {
			capturedInvokedAs = exec.InvokedAs
			return nil
		},
		Source: "test",
	})
	require.NoError(t, err)

	charID := ulid.Make()
	playerID := ulid.Make()
	mockAccess.Grant("char:"+charID.String(), "execute", "comms.pose")

	dispatcher := NewDispatcher(reg, mockAccess)

	t.Run("direct command sets InvokedAs to command name", func(t *testing.T) {
		capturedInvokedAs = ""
		exec := &CommandExecution{
			CharacterID: charID,
			PlayerID:    playerID,
			Output:      &bytes.Buffer{},
		}

		err := dispatcher.Dispatch(context.Background(), "pose waves", exec)
		require.NoError(t, err)
		assert.Equal(t, "pose", capturedInvokedAs)
	})

	t.Run("alias preserves original invoked name", func(t *testing.T) {
		// Set up alias cache with ; -> pose (for possessive poses like ";'s eyes widen")
		cache := NewAliasCache()
		cache.LoadSystemAliases(map[string]string{
			";": "pose",
		})
		dispatcher.SetAliasCache(cache)

		capturedInvokedAs = ""
		exec := &CommandExecution{
			CharacterID: charID,
			PlayerID:    playerID,
			Output:      &bytes.Buffer{},
		}

		err := dispatcher.Dispatch(context.Background(), ";'s eyes widen", exec)
		require.NoError(t, err)
		assert.Equal(t, ";", capturedInvokedAs, "InvokedAs should be the original command before alias resolution")
	})
}
