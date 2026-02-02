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
	exec := &CommandExecution{Output: &output}

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
	exec := &CommandExecution{Output: &output}

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
