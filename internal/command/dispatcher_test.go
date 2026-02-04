// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/holomush/holomush/internal/access/accesstest"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

// stubServices creates a minimal non-nil Services for tests that don't
// actually use the services. This prevents nil pointer panics while
// allowing the dispatcher to proceed with command execution.
func stubServices() *Services {
	svc, _ := NewServices(ServicesConfig{
		World:       &world.Service{},
		Session:     &stubSessionService{},
		Access:      &stubAccessControl{},
		Events:      &stubEventStore{},
		Broadcaster: &core.Broadcaster{},
	})
	return svc
}

// Stub types for dispatcher tests - minimal implementations that satisfy interfaces
type stubSessionService struct{}

func (s *stubSessionService) ListActiveSessions() []*core.Session  { return nil }
func (s *stubSessionService) GetSession(_ ulid.ULID) *core.Session { return nil }
func (s *stubSessionService) EndSession(_ ulid.ULID) error         { return nil }

type stubAccessControl struct{}

func (s *stubAccessControl) Check(_ context.Context, _, _, _ string) bool { return false }

type stubEventStore struct{}

func (s *stubEventStore) Append(_ context.Context, _ core.Event) error { return nil }
func (s *stubEventStore) Replay(_ context.Context, _ string, _ ulid.ULID, _ int) ([]core.Event, error) {
	return nil, nil
}
func (s *stubEventStore) LastEventID(_ context.Context, _ string) (ulid.ULID, error) {
	return ulid.ULID{}, nil
}
func (s *stubEventStore) Subscribe(_ context.Context, _ string) (<-chan ulid.ULID, <-chan error, error) {
	return nil, nil, nil
}

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

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
	}

	err = dispatcher.Dispatch(context.Background(), "echo hello world", exec)
	require.NoError(t, err)
	assert.Equal(t, "hello world", capturedArgs)
	assert.Equal(t, "echoed: hello world", output.String())
}

func TestDispatcher_UnknownCommand(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()
	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	}

	dispErr := dispatcher.Dispatch(context.Background(), "nonexistent", exec)
	require.Error(t, dispErr)
	assert.Contains(t, PlayerMessage(dispErr), "Unknown command")

	// Verify error code
	oopsErr, ok := oops.AsOops(dispErr)
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
	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
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
	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	}

	dispErr := dispatcher.Dispatch(context.Background(), "", exec)
	require.Error(t, dispErr)

	// Verify it's a parse error
	oopsErr, ok := oops.AsOops(dispErr)
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

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
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

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
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

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	charID := ulid.Make()
	exec := &CommandExecution{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
	}

	err = dispatcher.Dispatch(context.Background(), "failing", exec)
	require.Error(t, err)
	assert.Equal(t, handlerErr, err)
}

func TestDispatcher_HandlerError_LogsWarning(t *testing.T) {
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

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	// Capture log output
	var logBuf bytes.Buffer
	oldLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(oldLogger)

	var output bytes.Buffer
	charID := ulid.Make()
	exec := &CommandExecution{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
	}

	dispatchErr := dispatcher.Dispatch(context.Background(), "failing", exec)
	require.Error(t, dispatchErr)

	// Verify log output
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "command execution failed")
	assert.Contains(t, logOutput, "failing")
	assert.Contains(t, logOutput, charID.String())
	assert.Contains(t, logOutput, "handler failed")
}

func TestDispatcher_WhitespaceInput(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()
	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	}

	// Only whitespace
	dispErr := dispatcher.Dispatch(context.Background(), "   ", exec)
	require.Error(t, dispErr)

	// Tabs only
	dispErr = dispatcher.Dispatch(context.Background(), "\t\t", exec)
	require.Error(t, dispErr)
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

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
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

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	}

	err = dispatcher.Dispatch(context.Background(), "say hello   world", exec)
	require.NoError(t, err)
	assert.Equal(t, "hello   world", capturedArgs)
}

func TestNewDispatcher_NilRegistry(t *testing.T) {
	mockAccess := accesstest.NewMockAccessControl()
	dispatcher, err := NewDispatcher(nil, mockAccess)
	require.Error(t, err)
	assert.Nil(t, dispatcher)
	assert.Equal(t, ErrNilRegistry, err)
}

func TestNewDispatcher_NilAccessControl(t *testing.T) {
	reg := NewRegistry()
	dispatcher, err := NewDispatcher(reg, nil)
	require.Error(t, err)
	assert.Nil(t, dispatcher)
	assert.Equal(t, ErrNilAccessControl, err)
}

func TestNewDispatcher_WithAliasCache(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	// Without option - no alias cache
	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)
	assert.Nil(t, dispatcher.aliasCache)

	// With option - alias cache set
	cache := NewAliasCache()
	dispatcher, err = NewDispatcher(reg, mockAccess, WithAliasCache(cache))
	require.NoError(t, err)
	assert.Equal(t, cache, dispatcher.aliasCache)

	// With nil cache option - alias cache nil (explicit)
	dispatcher, err = NewDispatcher(reg, mockAccess, WithAliasCache(nil))
	require.NoError(t, err)
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

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)
	// No alias cache set

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		PlayerID:    ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
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

	// Set up alias cache with some aliases that won't match
	cache := NewAliasCache()
	cache.LoadSystemAliases(map[string]string{
		"l": "look",
	})

	dispatcher, err := NewDispatcher(reg, mockAccess, WithAliasCache(cache))
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		PlayerID:    ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
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

	// Set up alias cache with system alias
	cache := NewAliasCache()
	cache.LoadSystemAliases(map[string]string{
		"l": "look",
	})

	dispatcher, err := NewDispatcher(reg, mockAccess, WithAliasCache(cache))
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		PlayerID:    ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
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

	// Set up alias cache with player alias
	playerID := ulid.Make()
	cache := NewAliasCache()
	cache.LoadPlayerAliases(playerID, map[string]string{
		"greet": "say Hello everyone!",
	})

	dispatcher, err := NewDispatcher(reg, mockAccess, WithAliasCache(cache))
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		PlayerID:    playerID,
		Output:      &output,
		Services:    stubServices(),
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

	dispatcher, err := NewDispatcher(reg, mockAccess, WithAliasCache(cache))
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		PlayerID:    playerID,
		Output:      &output,
		Services:    stubServices(),
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

	cache := NewAliasCache()
	cache.LoadSystemAliases(map[string]string{
		"s": "say",
	})

	dispatcher, err := NewDispatcher(reg, mockAccess, WithAliasCache(cache))
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		PlayerID:    ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
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

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

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

func TestDispatcher_ContextCancellation(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	// Channel to signal handler received cancellation
	handlerStarted := make(chan struct{})
	handlerDone := make(chan struct{})
	var receivedCtxErr error

	err := reg.Register(CommandEntry{
		Name:         "slow",
		Capabilities: nil,
		Handler: func(ctx context.Context, _ *CommandExecution) error {
			close(handlerStarted)
			// Wait for context cancellation or timeout
			<-ctx.Done()
			receivedCtxErr = ctx.Err()
			close(handlerDone)
			return ctx.Err()
		},
		Source: "test",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Run dispatch in goroutine since handler blocks
	dispatchDone := make(chan error)
	go func() {
		dispatchDone <- dispatcher.Dispatch(ctx, "slow", exec)
	}()

	// Wait for handler to start
	<-handlerStarted

	// Cancel context
	cancel()

	// Wait for handler to complete
	<-handlerDone

	// Verify handler received cancellation
	assert.Equal(t, context.Canceled, receivedCtxErr)

	// Verify dispatch returned the cancellation error
	dispatchErr := <-dispatchDone
	assert.ErrorIs(t, dispatchErr, context.Canceled)
}

func TestDispatcher_ContextAlreadyCancelled(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	var receivedCtx context.Context
	err := reg.Register(CommandEntry{
		Name:         "check",
		Capabilities: nil,
		Handler: func(ctx context.Context, _ *CommandExecution) error {
			receivedCtx = ctx
			// Return immediately if already cancelled
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		},
		Source: "test",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	}

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Dispatch with cancelled context
	dispatchErr := dispatcher.Dispatch(ctx, "check", exec)

	// Handler should have received the cancelled context
	require.NotNil(t, receivedCtx)
	assert.Equal(t, context.Canceled, receivedCtx.Err())

	// Dispatch should return cancellation error
	assert.ErrorIs(t, dispatchErr, context.Canceled)
}

func TestDispatcher_NilServices(t *testing.T) {
	reg := NewRegistry()
	mockAccess := accesstest.NewMockAccessControl()

	// Register a command that accesses Services.World (would panic if nil)
	err := reg.Register(CommandEntry{
		Name:         "checkservices",
		Capabilities: nil,
		Handler: func(_ context.Context, exec *CommandExecution) error {
			// This would panic if Services is nil
			_ = exec.Services.World
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := &CommandExecution{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    nil, // Explicitly nil Services
	}

	// Dispatch should return an error instead of panicking
	dispatchErr := dispatcher.Dispatch(context.Background(), "checkservices", exec)
	require.Error(t, dispatchErr)
	assert.Contains(t, PlayerMessage(dispatchErr), "services")

	// Verify error code
	oopsErr, ok := oops.AsOops(dispatchErr)
	require.True(t, ok)
	assert.Equal(t, CodeNilServices, oopsErr.Code())
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

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	t.Run("direct command sets InvokedAs to command name", func(t *testing.T) {
		capturedInvokedAs = ""
		exec := &CommandExecution{
			CharacterID: charID,
			PlayerID:    playerID,
			Output:      &bytes.Buffer{},
			Services:    stubServices(),
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
		dispatcherWithAlias, dispErr := NewDispatcher(reg, mockAccess, WithAliasCache(cache))
		require.NoError(t, dispErr)

		capturedInvokedAs = ""
		exec := &CommandExecution{
			CharacterID: charID,
			PlayerID:    playerID,
			Output:      &bytes.Buffer{},
			Services:    stubServices(),
		}

		dispatchErr := dispatcherWithAlias.Dispatch(context.Background(), ";'s eyes widen", exec)
		require.NoError(t, dispatchErr)
		assert.Equal(t, ";", capturedInvokedAs, "InvokedAs should be the original command before alias resolution")
	})
}
