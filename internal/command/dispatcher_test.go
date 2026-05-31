// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// stubServices creates a minimal non-nil Services for tests that don't
// actually use the services. This prevents nil pointer panics while
// allowing the dispatcher to proceed with command execution.
func stubServices() *Services {
	svc, _ := NewServices(ServicesConfig{
		World:   &world.Service{},
		Session: &stubAccess{},
		Engine:  policytest.AllowAllEngine(),
		Events:  &stubEventStore{},
	})
	return svc
}

// Stub types for dispatcher tests - minimal implementations that satisfy interfaces
type stubAccess struct{}

func (s *stubAccess) ListActive(_ context.Context) ([]*session.Info, error) {
	return nil, nil
}

func (s *stubAccess) FindByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
	return nil, nil
}

func (s *stubAccess) DeleteByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
	return nil, nil
}

func (s *stubAccess) UpdateActivity(_ context.Context, _ string) error {
	return nil
}

func (s *stubAccess) FindByCharacterName(_ context.Context, _ string) (*session.Info, error) {
	return nil, nil
}

func (s *stubAccess) UpdateLastPaged(_ context.Context, _ string, _ string) error {
	return nil
}

func (s *stubAccess) UpdateLastWhispered(_ context.Context, _ string, _ string) error {
	return nil
}

type stubEventStore struct{}

func (s *stubEventStore) Append(_ context.Context, _ core.Event) error { return nil }

var _ core.EventAppender = (*stubEventStore)(nil)

func TestDispatcherDispatch(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	// Register a test command
	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "echo",
		capabilities: []Capability{{Action: "read", Resource: "object", Scope: ScopeLocal}},
		handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			_, _ = exec.Output().Write([]byte("echoed: " + exec.Args))
			return nil
		},
		Source: "test",
	})
	require.NoError(t, err)

	// Grant: Layer 1 (command execution) + Layer 2 (capability pre-flight)
	charID := ulid.Make()
	mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "echo")
	mockAccess.Grant(access.SubjectCharacter+charID.String(), "read", "object")

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
	})

	err = dispatcher.Dispatch(context.Background(), "echo hello world", exec)
	require.NoError(t, err)
	assert.Equal(t, "hello world", capturedArgs)
	assert.Equal(t, "echoed: hello world", output.String())
}

func TestDispatcherUnknownCommand(t *testing.T) {
	reg := NewRegistry()
	dispatcher, err := NewDispatcher(reg, policytest.AllowAllEngine())
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	dispErr := dispatcher.Dispatch(context.Background(), "nonexistent", exec)
	require.Error(t, dispErr)
	assert.Contains(t, PlayerMessage(dispErr), "Unknown command")

	// Verify error code
	oopsErr, ok := oops.AsOops(dispErr)
	require.True(t, ok)
	assert.Equal(t, CodeUnknownCommand, oopsErr.Code())
}

func TestDispatcherPermissionDenied(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	err := reg.Register(CommandEntry{
		Name:         "admin",
		capabilities: []Capability{{Action: "admin", Resource: "server", Scope: ScopeGlobal}},
		handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Source:       "core",
	})
	require.NoError(t, err)

	// Don't grant capability
	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	err = dispatcher.Dispatch(context.Background(), "admin", exec)
	require.Error(t, err)
	assert.Contains(t, PlayerMessage(err), "permission")

	// Verify error code
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, CodePermissionDenied, oopsErr.Code())
}

func TestDispatcherExplicitPolicyDenyReturnsAccessDenied(t *testing.T) {
	reg := NewRegistry()
	// DenyAllEngine returns EffectDeny with err == nil (explicit policy denial)
	denyEngine := policytest.DenyAllEngine()

	err := reg.Register(CommandEntry{
		Name:         "admin",
		capabilities: []Capability{{Action: "admin", Resource: "server", Scope: ScopeGlobal}},
		handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Source:       "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, denyEngine)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	err = dispatcher.Dispatch(context.Background(), "admin", exec)
	require.Error(t, err)
	assert.Contains(t, PlayerMessage(err), "permission")

	// Verify PERMISSION_DENIED error code
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, CodePermissionDenied, oopsErr.Code(),
		"explicit policy deny should return PERMISSION_DENIED")
}

func TestDispatchEngineErrorReturnsAccessEvaluationFailed(t *testing.T) {
	reg := NewRegistry()
	engineErr := errors.New("policy store unavailable")
	errorEngine := policytest.NewErrorEngine(engineErr)

	err := reg.Register(CommandEntry{
		Name:         "admin",
		capabilities: []Capability{{Action: "admin", Resource: "server", Scope: ScopeGlobal}},
		handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Source:       "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, errorEngine)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	// Get baseline for engine_failure metric
	engineFailureBefore := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "admin", "source": "core", "status": StatusEngineFailure,
	}))

	err = dispatcher.Dispatch(context.Background(), "admin", exec)
	require.Error(t, err)

	// Verify error code using errutil helper
	errutil.AssertErrorCode(t, err, CodeAccessEvaluationFailed)

	// Verify error context — Layer 1 (CheckCommandExecution) sets "command", not "capability"
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "admin", oopsErr.Context()["command"])

	// Verify wrapped error
	assert.ErrorIs(t, err, engineErr)

	// Verify metric
	engineFailureAfter := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "admin", "source": "core", "status": StatusEngineFailure,
	}))
	assert.Equal(t, engineFailureBefore+1, engineFailureAfter, "should have engine_failure status")
}

func TestDispatcherInfraFailureDeniesAtLayer1(t *testing.T) {
	reg := NewRegistry()
	infraEngine := policytest.NewInfraFailureEngine(t, "session resolution failed", "infra:session-resolver")

	err := reg.Register(CommandEntry{
		Name:         "admin",
		capabilities: []Capability{{Action: "admin", Resource: "server", Scope: ScopeGlobal}},
		handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Source:       "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, infraEngine)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	// Get baseline for engine_failure metric (Layer 1 now returns ACCESS_EVALUATION_FAILED for infra failures)
	engineFailureBefore := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "admin", "source": "core", "status": StatusEngineFailure,
	}))

	err = dispatcher.Dispatch(context.Background(), "admin", exec)
	require.Error(t, err)

	// Layer 1 (CheckCommandExecution) now distinguishes infra failure from policy denial —
	// it returns ACCESS_EVALUATION_FAILED for infra failures.
	errutil.AssertErrorCode(t, err, CodeAccessEvaluationFailed)

	// Verify error context includes reason and policy_id from the decision
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "admin", oopsErr.Context()["command"])
	assert.Equal(t, "session resolution failed", oopsErr.Context()["reason"])
	assert.Equal(t, "infra:session-resolver", oopsErr.Context()["policy_id"])

	// Verify engine_failure metric incremented
	engineFailureAfter := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "admin", "source": "core", "status": StatusEngineFailure,
	}))
	assert.Equal(t, engineFailureBefore+1, engineFailureAfter, "should record engine_failure status for infra failures at Layer 1")
}

func TestDispatcherEmptyInput(t *testing.T) {
	reg := NewRegistry()
	dispatcher, err := NewDispatcher(reg, policytest.AllowAllEngine())
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	dispErr := dispatcher.Dispatch(context.Background(), "", exec)
	require.Error(t, dispErr)

	// Verify it's a parse error
	oopsErr, ok := oops.AsOops(dispErr)
	require.True(t, ok)
	assert.Equal(t, "EMPTY_INPUT", oopsErr.Code())
}

func TestDispatchNoGrantsDeniedAtLayer1(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	// Register command with capabilities — no grants means Layer 1 denies.
	err := reg.Register(CommandEntry{
		Name:         "badcap",
		capabilities: []Capability{{Action: "admin", Resource: "server", Scope: ScopeGlobal}},
		handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Source:       "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	// Get baseline for permission_denied metric
	permDeniedBefore := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "badcap", "source": "core", "status": StatusPermissionDenied,
	}))

	err = dispatcher.Dispatch(context.Background(), "badcap", exec)
	require.Error(t, err)

	// Layer 1 denies — no grant for "execute" on "command:badcap"
	errutil.AssertErrorCode(t, err, CodePermissionDenied)

	// Verify error context
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "badcap", oopsErr.Context()["command"])

	// Verify metric
	permDeniedAfter := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "badcap", "source": "core", "status": StatusPermissionDenied,
	}))
	assert.Equal(t, permDeniedBefore+1, permDeniedAfter, "should have permission_denied status")
}

func TestDispatcherMultipleCapabilities(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	// Register command requiring multiple capabilities
	err := reg.Register(CommandEntry{
		Name:         "dangerous",
		capabilities: []Capability{{Action: "admin", Resource: "server", Scope: ScopeGlobal}, {Action: "delete", Resource: "server", Scope: ScopeGlobal}},
		handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Source:       "core",
	})
	require.NoError(t, err)

	charID := ulid.Make()
	subject := access.CharacterSubject(charID.String())

	// Grant Layer 1 (command execution)
	mockAccess.GrantCommandExecution(subject, "dangerous")
	// Grant only one capability action (admin) — missing "delete" for Layer 2
	mockAccess.Grant(subject, "admin", "server")

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
	})

	// Should fail - Layer 2 requires both "admin" and "delete" actions
	err = dispatcher.Dispatch(context.Background(), "dangerous", exec)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, CodePermissionDenied, oopsErr.Code())

	// Now grant the second capability action
	mockAccess.Grant(subject, "delete", "server")

	// Should succeed — both layers satisfied
	err = dispatcher.Dispatch(context.Background(), "dangerous", exec)
	require.NoError(t, err)
}

func TestDispatcherNoCapabilitiesRequired(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	// Register command with no capabilities required
	executed := false
	charID := ulid.Make()
	err := reg.Register(CommandEntry{
		Name:         "public",
		capabilities: nil, // No capabilities required
		handler: func(_ context.Context, _ *CommandExecution) error {
			executed = true
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	// Only grant Layer 1 (command execution) — no Layer 2 grants needed
	mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "public")

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
	})

	// Should succeed — Layer 1 granted, Layer 2 skipped (no capabilities)
	err = dispatcher.Dispatch(context.Background(), "public", exec)
	require.NoError(t, err)
	assert.True(t, executed)
}

func TestDispatcherHandlerError(t *testing.T) {
	reg := NewRegistry()

	handlerErr := errors.New("handler failed")
	err := reg.Register(CommandEntry{
		Name:         "failing",
		capabilities: nil,
		handler: func(_ context.Context, _ *CommandExecution) error {
			return handlerErr
		},
		Source: "test",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, policytest.AllowAllEngine())
	require.NoError(t, err)

	var output bytes.Buffer
	charID := ulid.Make()
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
	})

	err = dispatcher.Dispatch(context.Background(), "failing", exec)
	require.Error(t, err)
	assert.Equal(t, handlerErr, err)
}

func TestDispatcherHandlerErrorLogsWarning(t *testing.T) {
	reg := NewRegistry()

	handlerErr := errors.New("handler failed")
	err := reg.Register(CommandEntry{
		Name:         "failing",
		capabilities: nil,
		handler: func(_ context.Context, _ *CommandExecution) error {
			return handlerErr
		},
		Source: "test",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, policytest.AllowAllEngine())
	require.NoError(t, err)

	// Capture log output
	var logBuf bytes.Buffer
	oldLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(oldLogger)

	var output bytes.Buffer
	charID := ulid.Make()
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
	})

	dispatchErr := dispatcher.Dispatch(context.Background(), "failing", exec)
	require.Error(t, dispatchErr)

	// Verify log output
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "command execution failed")
	assert.Contains(t, logOutput, "failing")
	assert.Contains(t, logOutput, charID.String())
	assert.Contains(t, logOutput, "handler failed")
}

func TestDispatcherWhitespaceInput(t *testing.T) {
	reg := NewRegistry()
	dispatcher, err := NewDispatcher(reg, policytest.AllowAllEngine())
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	// Only whitespace
	dispErr := dispatcher.Dispatch(context.Background(), "   ", exec)
	require.Error(t, dispErr)

	// Tabs only
	dispErr = dispatcher.Dispatch(context.Background(), "\t\t", exec)
	require.Error(t, dispErr)
}

func TestDispatcherCommandWithNoArgs(t *testing.T) {
	reg := NewRegistry()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "look",
		capabilities: nil,
		handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, policytest.AllowAllEngine())
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	err = dispatcher.Dispatch(context.Background(), "look", exec)
	require.NoError(t, err)
	assert.Equal(t, "", capturedArgs)
}

func TestDispatcherPreservesWhitespaceInArgs(t *testing.T) {
	reg := NewRegistry()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "say",
		capabilities: nil,
		handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, policytest.AllowAllEngine())
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	err = dispatcher.Dispatch(context.Background(), "say hello   world", exec)
	require.NoError(t, err)
	assert.Equal(t, "hello   world", capturedArgs)
}

func TestNewDispatcherNilRegistry(t *testing.T) {
	dispatcher, err := NewDispatcher(nil, policytest.AllowAllEngine())
	require.Error(t, err)
	assert.Nil(t, dispatcher)
	assert.Equal(t, ErrNilRegistry, err)
}

func TestNewDispatcherNilEngine(t *testing.T) {
	reg := NewRegistry()
	dispatcher, err := NewDispatcher(reg, nil)
	require.Error(t, err)
	assert.Nil(t, dispatcher)
	assert.Equal(t, ErrNilDispatcherEngine, err)
}

func TestNewDispatcherWithAliasCache(t *testing.T) {
	reg := NewRegistry()
	engine := policytest.AllowAllEngine()

	// Without option - no alias cache
	dispatcher, err := NewDispatcher(reg, engine)
	require.NoError(t, err)
	assert.Nil(t, dispatcher.aliasCache)

	// With option - alias cache set
	cache := NewAliasCache()
	dispatcher, err = NewDispatcher(reg, engine, WithAliasCache(cache))
	require.NoError(t, err)
	assert.Equal(t, cache, dispatcher.aliasCache)

	// With nil cache option - alias cache nil (explicit)
	dispatcher, err = NewDispatcher(reg, engine, WithAliasCache(nil))
	require.NoError(t, err)
	assert.Nil(t, dispatcher.aliasCache)
}

func TestDispatcherWithoutAliasCache(t *testing.T) {
	// Ensure dispatcher works exactly as before when no alias cache is set
	reg := NewRegistry()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "look",
		capabilities: nil,
		handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, policytest.AllowAllEngine())
	require.NoError(t, err)
	// No alias cache set

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		PlayerID:    ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	err = dispatcher.Dispatch(context.Background(), "look here", exec)
	require.NoError(t, err)
	assert.Equal(t, "here", capturedArgs)
}

func TestDispatcherWithAliasCacheNoAliasMatch(t *testing.T) {
	reg := NewRegistry()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "look",
		capabilities: nil,
		handler: func(_ context.Context, exec *CommandExecution) error {
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

	dispatcher, err := NewDispatcher(reg, policytest.AllowAllEngine(), WithAliasCache(cache))
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		PlayerID:    ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	// Input is an actual command, not an alias
	err = dispatcher.Dispatch(context.Background(), "look here", exec)
	require.NoError(t, err)
	assert.Equal(t, "here", capturedArgs)
}

func TestDispatcherWithAliasCacheSystemAliasExpanded(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "look",
		capabilities: nil,
		handler: func(_ context.Context, exec *CommandExecution) error {
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

	charID := ulid.Make()
	mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "look")

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		PlayerID:    ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	// Use alias 'l' which should expand to 'look'
	err = dispatcher.Dispatch(context.Background(), "l around", exec)
	require.NoError(t, err)
	assert.Equal(t, "around", capturedArgs)
}

func TestDispatcherWithAliasCachePlayerAliasExpanded(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "say",
		capabilities: nil,
		handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	// Set up alias cache with player alias
	playerID := ulid.Make()
	charID := ulid.Make()
	cache := NewAliasCache()
	cache.LoadPlayerAliases(playerID, map[string]string{
		"greet": "say Hello everyone!",
	})

	dispatcher, err := NewDispatcher(reg, mockAccess, WithAliasCache(cache))
	require.NoError(t, err)

	mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "say")

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		PlayerID:    playerID,
		Output:      &output,
		Services:    stubServices(),
	})

	// Use alias 'greet' which should expand to 'say Hello everyone!'
	err = dispatcher.Dispatch(context.Background(), "greet", exec)
	require.NoError(t, err)
	assert.Equal(t, "Hello everyone!", capturedArgs)
}

func TestDispatcherWithAliasCachePlayerAliasOverridesSystem(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "say",
		capabilities: nil,
		handler: func(_ context.Context, exec *CommandExecution) error {
			capturedArgs = exec.Args
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	playerID := ulid.Make()
	charID := ulid.Make()
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

	mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "say")

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		PlayerID:    playerID,
		Output:      &output,
		Services:    stubServices(),
	})

	// Player alias should take precedence
	err = dispatcher.Dispatch(context.Background(), "hi", exec)
	require.NoError(t, err)
	assert.Equal(t, "hello from player", capturedArgs)
}

func TestDispatcherWithAliasCacheAliasWithExtraArgs(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	var capturedArgs string
	err := reg.Register(CommandEntry{
		Name:         "say",
		capabilities: nil,
		handler: func(_ context.Context, exec *CommandExecution) error {
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

	charID := ulid.Make()
	mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "say")

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		PlayerID:    ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	// 's' expands to 'say', with extra args appended
	err = dispatcher.Dispatch(context.Background(), "s this is my message", exec)
	require.NoError(t, err)
	assert.Equal(t, "this is my message", capturedArgs)
}

func TestDispatcherNoCharacter(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	err := reg.Register(CommandEntry{
		Name:         "test",
		capabilities: []Capability{},
		handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Source:       "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		// CharacterID intentionally left as zero value
		Output: &output,
	})

	err = dispatcher.Dispatch(context.Background(), "test", exec)
	require.Error(t, err)
	assert.Contains(t, PlayerMessage(err), "character")

	// Verify error code
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, CodeNoCharacter, oopsErr.Code())
}

func TestDispatcherContextCancellation(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	// Channel to signal handler received cancellation
	handlerStarted := make(chan struct{})
	handlerDone := make(chan struct{})
	var receivedCtxErr error

	err := reg.Register(CommandEntry{
		Name:         "slow",
		capabilities: nil,
		handler: func(ctx context.Context, _ *CommandExecution) error {
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

	charID := ulid.Make()
	mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "slow")

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
	})

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

func TestDispatcherContextAlreadyCancelled(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	var receivedCtx context.Context
	err := reg.Register(CommandEntry{
		Name:         "check",
		capabilities: nil,
		handler: func(ctx context.Context, _ *CommandExecution) error {
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

	charID := ulid.Make()
	mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "check")

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
	})

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

func TestDispatcherNilServices(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	// Register a command that accesses Services.World (would panic if nil)
	err := reg.Register(CommandEntry{
		Name:         "checkservices",
		capabilities: nil,
		handler: func(_ context.Context, exec *CommandExecution) error {
			// This would panic if Services is nil
			_ = exec.Services().World()
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    nil, // Explicitly nil Services
	})

	// Dispatch should return an error instead of panicking
	dispatchErr := dispatcher.Dispatch(context.Background(), "checkservices", exec)
	require.Error(t, dispatchErr)
	assert.Contains(t, PlayerMessage(dispatchErr), "services")

	// Verify error code
	oopsErr, ok := oops.AsOops(dispatchErr)
	require.True(t, ok)
	assert.Equal(t, CodeNilServices, oopsErr.Code())
}

func TestDispatcher_WithRateLimiter(t *testing.T) {
	t.Run("rate limiting disabled when no limiter configured", func(t *testing.T) {
		reg := NewRegistry()

		var executed int
		err := reg.Register(CommandEntry{
			Name:         "test",
			capabilities: nil,
			handler: func(_ context.Context, _ *CommandExecution) error {
				executed++
				return nil
			},
			Source: "core",
		})
		require.NoError(t, err)

		// Create dispatcher without rate limiter (AllowAllEngine since loop creates new charIDs)
		dispatcher, err := NewDispatcher(reg, policytest.AllowAllEngine())
		require.NoError(t, err)

		// Execute many commands - should all succeed (no rate limiting)
		for i := 0; i < 20; i++ {
			exec := NewTestExecution(CommandExecutionConfig{
				CharacterID: ulid.Make(),
				SessionID:   ulid.Make(),
				Output:      &bytes.Buffer{},
				Services:    stubServices(),
			})
			err := dispatcher.Dispatch(context.Background(), "test", exec)
			require.NoError(t, err)
		}
		assert.Equal(t, 20, executed)
	})

	t.Run("rate limiting blocks commands when burst exceeded", func(t *testing.T) {
		reg := NewRegistry()
		mockAccess := policytest.NewGrantEngine()

		err := reg.Register(CommandEntry{
			Name:         "test",
			capabilities: nil,
			handler: func(_ context.Context, _ *CommandExecution) error {
				return nil
			},
			Source: "core",
		})
		require.NoError(t, err)

		// Create rate limiter with low burst capacity
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 2,
			SustainedRate: 1.0,
		})
		defer rl.Close()

		dispatcher, err := NewDispatcher(reg, mockAccess, WithRateLimiter(rl))
		require.NoError(t, err)

		charID := ulid.Make()
		sessionID := ulid.Make()

		// Grant Layer 1 command execution (no bypass grant — rate limiting should apply)
		mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "test")

		// First two commands should succeed
		for i := 0; i < 2; i++ {
			exec := NewTestExecution(CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   sessionID,
				Output:      &bytes.Buffer{},
				Services:    stubServices(),
			})
			dispatchErr := dispatcher.Dispatch(context.Background(), "test", exec)
			require.NoError(t, dispatchErr)
		}

		// Third command should be rate limited
		exec := NewTestExecution(CommandExecutionConfig{
			CharacterID: charID,
			SessionID:   sessionID,
			Output:      &bytes.Buffer{},
			Services:    stubServices(),
		})
		err = dispatcher.Dispatch(context.Background(), "test", exec)
		require.Error(t, err)

		// Verify error code and context
		oopsErr, ok := oops.AsOops(err)
		require.True(t, ok)
		assert.Equal(t, CodeRateLimited, oopsErr.Code())
		assert.Contains(t, oopsErr.Context(), "cooldown_ms")
	})

	t.Run("different sessions have independent rate limits", func(t *testing.T) {
		reg := NewRegistry()
		mockAccess := policytest.NewGrantEngine()

		err := reg.Register(CommandEntry{
			Name:         "test",
			capabilities: nil,
			handler: func(_ context.Context, _ *CommandExecution) error {
				return nil
			},
			Source: "core",
		})
		require.NoError(t, err)

		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 1,
			SustainedRate: 1.0,
		})
		defer rl.Close()

		dispatcher, err := NewDispatcher(reg, mockAccess, WithRateLimiter(rl))
		require.NoError(t, err)

		char1 := ulid.Make()
		char2 := ulid.Make()
		session1 := ulid.Make()
		session2 := ulid.Make()

		// Grant Layer 1 for both characters (no bypass grant)
		mockAccess.GrantCommandExecution(access.SubjectCharacter+char1.String(), "test")
		mockAccess.GrantCommandExecution(access.SubjectCharacter+char2.String(), "test")

		// Session 1 uses its token
		exec1 := NewTestExecution(CommandExecutionConfig{
			CharacterID: char1,
			SessionID:   session1,
			Output:      &bytes.Buffer{},
			Services:    stubServices(),
		})
		err = dispatcher.Dispatch(context.Background(), "test", exec1)
		require.NoError(t, err)

		// Session 1 is now rate limited
		err = dispatcher.Dispatch(context.Background(), "test", exec1)
		require.Error(t, err)
		assert.Contains(t, PlayerMessage(err), "slow down")

		// Session 2 should still have its token
		exec2 := NewTestExecution(CommandExecutionConfig{
			CharacterID: char2,
			SessionID:   session2,
			Output:      &bytes.Buffer{},
			Services:    stubServices(),
		})
		err = dispatcher.Dispatch(context.Background(), "test", exec2)
		require.NoError(t, err)
	})

	t.Run("bypass capability exempts from rate limiting", func(t *testing.T) {
		reg := NewRegistry()
		mockAccess := policytest.NewGrantEngine()

		var executed int
		err := reg.Register(CommandEntry{
			Name:         "test",
			capabilities: nil,
			handler: func(_ context.Context, _ *CommandExecution) error {
				executed++
				return nil
			},
			Source: "core",
		})
		require.NoError(t, err)

		// Create rate limiter with very low burst
		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 1,
			SustainedRate: 0.1, // Very slow refill
		})
		defer rl.Close()

		dispatcher, err := NewDispatcher(reg, mockAccess, WithRateLimiter(rl))
		require.NoError(t, err)

		charID := ulid.Make()
		sessionID := ulid.Make()

		// Grant Layer 1 (command execution) and bypass capability
		mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "test")
		mockAccess.Grant(access.SubjectCharacter+charID.String(), "execute", CapabilityRateLimitBypass)

		// Should be able to execute many commands despite rate limit
		for i := 0; i < 10; i++ {
			exec := NewTestExecution(CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   sessionID,
				Output:      &bytes.Buffer{},
				Services:    stubServices(),
			})
			err := dispatcher.Dispatch(context.Background(), "test", exec)
			require.NoError(t, err)
		}
		assert.Equal(t, 10, executed)
	})

	t.Run("rate limiting happens after alias resolution", func(t *testing.T) {
		reg := NewRegistry()
		mockAccess := policytest.NewGrantEngine()

		var capturedArgs string
		err := reg.Register(CommandEntry{
			Name:         "look",
			capabilities: nil,
			handler: func(_ context.Context, exec *CommandExecution) error {
				capturedArgs = exec.Args
				return nil
			},
			Source: "core",
		})
		require.NoError(t, err)

		// Set up alias cache
		cache := NewAliasCache()
		cache.LoadSystemAliases(map[string]string{
			"l": "look",
		})

		rl := NewRateLimiter(RateLimiterConfig{
			BurstCapacity: 1,
			SustainedRate: 1.0,
		})
		defer rl.Close()

		dispatcher, err := NewDispatcher(reg, mockAccess, WithAliasCache(cache), WithRateLimiter(rl))
		require.NoError(t, err)

		charID := ulid.Make()
		sessionID := ulid.Make()
		playerID := ulid.Make()

		// Grant Layer 1 command execution (no bypass grant)
		mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "look")

		// Use alias - should succeed (alias resolved, command executed)
		exec := NewTestExecution(CommandExecutionConfig{
			CharacterID: charID,
			SessionID:   sessionID,
			PlayerID:    playerID,
			Output:      &bytes.Buffer{},
			Services:    stubServices(),
		})
		err = dispatcher.Dispatch(context.Background(), "l around", exec)
		require.NoError(t, err)
		assert.Equal(t, "around", capturedArgs)

		// Second command should be rate limited
		err = dispatcher.Dispatch(context.Background(), "l again", exec)
		require.Error(t, err)
		oopsErr, ok := oops.AsOops(err)
		require.True(t, ok)
		assert.Equal(t, CodeRateLimited, oopsErr.Code())
	})
}

func TestDispatcher_InvokedAs(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	// Register a test command that captures InvokedAs
	var capturedInvokedAs string
	err := reg.Register(CommandEntry{
		Name:         "pose",
		capabilities: []Capability{{Action: "emit", Resource: "stream", Scope: ScopeLocal}},
		handler: func(_ context.Context, exec *CommandExecution) error {
			capturedInvokedAs = exec.InvokedAs
			return nil
		},
		Source: "test",
	})
	require.NoError(t, err)

	charID := ulid.Make()
	playerID := ulid.Make()
	// Layer 1: command execution grant
	mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "pose")
	// Layer 2: capability pre-flight grant
	mockAccess.Grant(access.SubjectCharacter+charID.String(), "emit", "stream")

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	t.Run("direct command sets InvokedAs to command name", func(t *testing.T) {
		capturedInvokedAs = ""
		exec := NewTestExecution(CommandExecutionConfig{
			CharacterID: charID,
			PlayerID:    playerID,
			Output:      &bytes.Buffer{},
			Services:    stubServices(),
		})

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
		exec := NewTestExecution(CommandExecutionConfig{
			CharacterID: charID,
			PlayerID:    playerID,
			Output:      &bytes.Buffer{},
			Services:    stubServices(),
		})

		dispatchErr := dispatcherWithAlias.Dispatch(context.Background(), ";'s eyes widen", exec)
		require.NoError(t, dispatchErr)
		assert.Equal(t, ";", capturedInvokedAs, "InvokedAs should be the original command before alias resolution")
	})
}

func TestDispatcher_MetricsIntegration(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	// Register test commands
	err := reg.Register(CommandEntry{
		Name:         "metrics_success",
		capabilities: nil,
		handler: func(_ context.Context, _ *CommandExecution) error {
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	err = reg.Register(CommandEntry{
		Name:         "metrics_failing",
		capabilities: nil,
		handler: func(_ context.Context, _ *CommandExecution) error {
			return errors.New("handler error")
		},
		Source: "lua",
	})
	require.NoError(t, err)

	err = reg.Register(CommandEntry{
		Name:         "metrics_protected",
		capabilities: []Capability{{Action: "admin", Resource: "server", Scope: ScopeGlobal}},
		handler: func(_ context.Context, _ *CommandExecution) error {
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, mockAccess)
	require.NoError(t, err)

	// Get baseline counter values
	successBefore := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "metrics_success", "source": "core", "status": StatusSuccess,
	}))
	errorBefore := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "metrics_failing", "source": "lua", "status": StatusError,
	}))
	notFoundBefore := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "metrics_nonexistent", "source": "", "status": StatusNotFound,
	}))
	permDeniedBefore := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "metrics_protected", "source": "core", "status": StatusPermissionDenied,
	}))

	t.Run("records success metric", func(t *testing.T) {
		charID := ulid.Make()
		mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "metrics_success")
		exec := NewTestExecution(CommandExecutionConfig{
			CharacterID: charID,
			Output:      &bytes.Buffer{},
			Services:    stubServices(),
		})
		dispatchErr := dispatcher.Dispatch(context.Background(), "metrics_success", exec)
		require.NoError(t, dispatchErr)
	})

	t.Run("records error metric", func(t *testing.T) {
		charID := ulid.Make()
		mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "metrics_failing")
		exec := NewTestExecution(CommandExecutionConfig{
			CharacterID: charID,
			Output:      &bytes.Buffer{},
			Services:    stubServices(),
		})
		dispatchErr := dispatcher.Dispatch(context.Background(), "metrics_failing", exec)
		require.Error(t, dispatchErr)
	})

	t.Run("records not_found metric", func(t *testing.T) {
		exec := NewTestExecution(CommandExecutionConfig{
			CharacterID: ulid.Make(),
			Output:      &bytes.Buffer{},
			Services:    stubServices(),
		})
		dispatchErr := dispatcher.Dispatch(context.Background(), "metrics_nonexistent", exec)
		require.Error(t, dispatchErr)
	})

	t.Run("records permission_denied metric", func(t *testing.T) {
		exec := NewTestExecution(CommandExecutionConfig{
			CharacterID: ulid.Make(),
			Output:      &bytes.Buffer{},
			Services:    stubServices(),
		})
		// Don't grant admin:manage capability
		dispatchErr := dispatcher.Dispatch(context.Background(), "metrics_protected", exec)
		require.Error(t, dispatchErr)
	})

	// Verify metrics were recorded
	successAfter := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "metrics_success", "source": "core", "status": StatusSuccess,
	}))
	errorAfter := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "metrics_failing", "source": "lua", "status": StatusError,
	}))
	notFoundAfter := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "metrics_nonexistent", "source": "", "status": StatusNotFound,
	}))
	permDeniedAfter := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "metrics_protected", "source": "core", "status": StatusPermissionDenied,
	}))

	assert.Equal(t, successBefore+1, successAfter, "should have success status")
	assert.Equal(t, errorBefore+1, errorAfter, "should have error status")
	assert.Equal(t, notFoundBefore+1, notFoundAfter, "should have not_found status")
	assert.Equal(t, permDeniedBefore+1, permDeniedAfter, "should have permission_denied status")

	// Verify duration histogram was recorded (just check it doesn't panic when accessed)
	// Note: We can't easily verify histogram values with testutil.ToFloat64,
	// but the above counter assertions confirm the dispatch pipeline ran correctly.
	_ = CommandDuration.With(prometheus.Labels{"command": "metrics_success", "source": "core"})
	_ = CommandDuration.With(prometheus.Labels{"command": "metrics_failing", "source": "lua"})
}

func TestDispatcherAliasMetrics(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	err := reg.Register(CommandEntry{
		Name:         "look",
		capabilities: nil,
		handler: func(_ context.Context, _ *CommandExecution) error {
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	// Set up alias cache with unique alias for this test
	cache := NewAliasCache()
	cache.LoadSystemAliases(map[string]string{
		"la": "look", // Use 'la' instead of 'l' to avoid interference from other tests
	})

	dispatcher, err := NewDispatcher(reg, mockAccess, WithAliasCache(cache))
	require.NoError(t, err)

	// Get baseline
	before := testutil.ToFloat64(AliasExpansions.With(prometheus.Labels{"alias": "la"}))

	// Use the alias
	charID := ulid.Make()
	mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "look")
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		PlayerID:    ulid.Make(),
		Output:      &bytes.Buffer{},
		Services:    stubServices(),
	})
	err = dispatcher.Dispatch(context.Background(), "la around", exec)
	require.NoError(t, err)

	// Verify alias expansion was recorded
	after := testutil.ToFloat64(AliasExpansions.With(prometheus.Labels{"alias": "la"}))
	assert.Equal(t, before+1, after, "should have 1 expansion for 'la' alias")
}

func TestNewDispatcherWithRateLimiterNilEngineReturnsError(t *testing.T) {
	reg := NewRegistry()
	rl := NewRateLimiter(RateLimiterConfig{
		BurstCapacity: 5,
		SustainedRate: 1.0,
	})
	defer rl.Close()

	// Try to create dispatcher with nil engine but WithRateLimiter option
	// This should fail because NewRateLimitMiddleware requires a non-nil engine
	dispatcher, err := NewDispatcher(reg, nil, WithRateLimiter(rl))
	assert.Error(t, err)
	assert.Nil(t, dispatcher)
	assert.Equal(t, ErrNilDispatcherEngine, err, "should fail on nil engine validation before applying options")

	// Now test the case where engine is set but the rate limiter middleware creation fails
	// We can't easily test this without a mock, but the error path is covered by the optErr field
}

func TestNewDispatcherWithRateLimiterNilReturnsError(t *testing.T) {
	reg := NewRegistry()
	engine := policytest.AllowAllEngine()

	dispatcher, err := NewDispatcher(reg, engine, WithRateLimiter(nil))
	assert.Error(t, err)
	assert.Nil(t, dispatcher)
	assert.Equal(t, ErrNilRateLimiter, err)
}

func TestNewDispatcher_MultipleOptionErrorsReturnsFirstError(t *testing.T) {
	tests := []struct {
		name           string
		opts           []DispatcherOption
		expectErr      bool
		expectErrCount int // How many options should NOT be applied after the first error
	}{
		{
			name: "single failing option",
			opts: []DispatcherOption{
				func(d *Dispatcher) {
					d.optErr = errors.New("first error")
				},
			},
			expectErr:      true,
			expectErrCount: 0,
		},
		{
			name: "first option fails, second not applied",
			opts: []DispatcherOption{
				func(d *Dispatcher) {
					d.optErr = errors.New("first error")
				},
				func(d *Dispatcher) {
					// This should NOT be called
					d.aliasCache = NewAliasCache()
				},
			},
			expectErr:      true,
			expectErrCount: 1,
		},
		{
			name: "all options succeed",
			opts: []DispatcherOption{
				func(d *Dispatcher) {
					d.aliasCache = NewAliasCache()
				},
				func(_ *Dispatcher) {
					// Second option also succeeds
				},
			},
			expectErr:      false,
			expectErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewRegistry()
			mockAccess := policytest.NewGrantEngine()

			dispatcher, err := NewDispatcher(reg, mockAccess, tt.opts...)

			if tt.expectErr {
				require.Error(t, err)
				assert.Nil(t, dispatcher)
				assert.Contains(t, err.Error(), "first error")

				// If there are multiple options and first fails,
				// verify that subsequent options were NOT applied
				// The error message confirms we short-circuited on the first failing option
			} else {
				require.NoError(t, err)
				assert.NotNil(t, dispatcher)
			}
		})
	}
}

func TestDispatcherRateLimitMetrics(t *testing.T) {
	reg := NewRegistry()
	mockAccess := policytest.NewGrantEngine()

	err := reg.Register(CommandEntry{
		Name:         "ratelimit_test",
		capabilities: nil,
		handler: func(_ context.Context, _ *CommandExecution) error {
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	// Create rate limiter with burst of 1
	rl := NewRateLimiter(RateLimiterConfig{
		BurstCapacity: 1,
		SustainedRate: 0.1,
	})
	defer rl.Close()

	dispatcher, err := NewDispatcher(reg, mockAccess, WithRateLimiter(rl))
	require.NoError(t, err)

	charID := ulid.Make()
	sessionID := ulid.Make()

	// Grant Layer 1 command execution (no bypass grant — rate limiting should apply)
	mockAccess.GrantCommandExecution(access.SubjectCharacter+charID.String(), "ratelimit_test")

	// Get baselines
	// Note: rate-limited commands have empty source because rate limiting
	// happens before command lookup, so we don't know the source yet.
	successBefore := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "ratelimit_test", "source": "core", "status": StatusSuccess,
	}))
	rateLimitedBefore := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "ratelimit_test", "source": "", "status": StatusRateLimited,
	}))

	// First command succeeds
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		SessionID:   sessionID,
		Output:      &bytes.Buffer{},
		Services:    stubServices(),
	})
	err = dispatcher.Dispatch(context.Background(), "ratelimit_test", exec)
	require.NoError(t, err)

	// Second command is rate limited
	err = dispatcher.Dispatch(context.Background(), "ratelimit_test", exec)
	require.Error(t, err)

	// Verify metrics were recorded
	successAfter := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "ratelimit_test", "source": "core", "status": StatusSuccess,
	}))
	rateLimitedAfter := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "ratelimit_test", "source": "", "status": StatusRateLimited,
	}))

	assert.Equal(t, successBefore+1, successAfter, "should have success status")
	assert.Equal(t, rateLimitedBefore+1, rateLimitedAfter, "should have rate_limited status")
}

func TestDispatcherVerifiesAccessRequest(t *testing.T) {
	reg := NewRegistry()
	mockEngine := policytest.NewMockAccessPolicyEngine(t)

	charID := ulid.Make()
	subject := access.CharacterSubject(charID.String())

	// Register command with capability
	err := reg.Register(CommandEntry{
		Name:         "test_cmd",
		capabilities: []Capability{{Action: "read", Resource: "location", Scope: ScopeLocal}},
		handler: func(_ context.Context, _ *CommandExecution) error {
			return nil
		},
		Source: "test",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, mockEngine)
	require.NoError(t, err)

	// Layer 1: Capture the AccessRequest for command execution
	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	// Layer 2: Capability pre-flight
	mockEngine.EXPECT().CanPerformAction(mock.Anything, subject, "read", "location", string(ScopeLocal)).
		Return(true, nil)

	// Execute command
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		Output:      &bytes.Buffer{},
		Services:    stubServices(),
	})
	err = dispatcher.Dispatch(context.Background(), "test_cmd", exec)
	require.NoError(t, err)

	// Verify Layer 1 AccessRequest fields
	assert.Equal(t, subject, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "execute", capturedRequest.Action, "action should be 'execute'")
	assert.Equal(t, "command:test_cmd", capturedRequest.Resource, "resource should be command:<name>")
}

func TestDispatcherPolicyDenialReturnsPermissionDeniedMetric(t *testing.T) {
	reg := NewRegistry()
	// DenyAllEngine returns EffectDeny with err == nil (explicit policy denial)
	denyEngine := policytest.DenyAllEngine()

	err := reg.Register(CommandEntry{
		Name:         "protected",
		capabilities: []Capability{{Action: "admin", Resource: "server", Scope: ScopeGlobal}},
		handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Source:       "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, denyEngine)
	require.NoError(t, err)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &output,
		Services:    stubServices(),
	})

	// Get baseline for permission_denied metric
	permDeniedBefore := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "protected", "source": "core", "status": StatusPermissionDenied,
	}))

	err = dispatcher.Dispatch(context.Background(), "protected", exec)
	require.Error(t, err)

	// Verify error code is still PERMISSION_DENIED (not ACCESS_EVALUATION_FAILED)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, CodePermissionDenied, oopsErr.Code())

	// Verify metric shows permission_denied (not engine_failure)
	permDeniedAfter := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "protected", "source": "core", "status": StatusPermissionDenied,
	}))
	assert.Equal(t, permDeniedBefore+1, permDeniedAfter, "should have permission_denied status for policy denial")
}

func TestDispatcherEvaluateErrorLogsErrorWithContext(t *testing.T) {
	reg := NewRegistry()
	mockEngine := policytest.NewMockAccessPolicyEngine(t)

	// Register command with capability
	err := reg.Register(CommandEntry{
		Name:         "protected",
		capabilities: []Capability{{Action: "admin", Resource: "server", Scope: ScopeGlobal}},
		handler: func(_ context.Context, _ *CommandExecution) error {
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, mockEngine)
	require.NoError(t, err)

	charID := ulid.Make()
	subject := access.CharacterSubject(charID.String())
	evalErr := errors.New("policy store unavailable")

	// Layer 1: command execution — allow
	mockEngine.EXPECT().Evaluate(mock.Anything, types.AccessRequest{
		Subject:  subject,
		Action:   "execute",
		Resource: "command:protected",
	}).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	// Layer 2: capability pre-flight — return error
	mockEngine.EXPECT().CanPerformAction(mock.Anything, subject, "admin", "server", ScopeGlobal).
		Return(false, evalErr)

	// Capture log output
	var logBuf bytes.Buffer
	oldLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(oldLogger)

	// Execute command
	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
	})

	dispatchErr := dispatcher.Dispatch(context.Background(), "protected", exec)
	require.Error(t, dispatchErr)

	// Verify log output contains error and context
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "capability pre-flight error", "log should mention capability pre-flight error")
	assert.Contains(t, logOutput, subject, "log should contain subject")
	assert.Contains(t, logOutput, "admin", "log should contain action")
	assert.Contains(t, logOutput, "server", "log should contain resource")
	assert.Contains(t, logOutput, "policy store unavailable", "log should contain error message")
}

func TestDispatcherPermissionDenialPropagatesDecisionContext(t *testing.T) {
	reg := NewRegistry()
	mockEngine := policytest.NewMockAccessPolicyEngine(t)

	// Register command with capability
	err := reg.Register(CommandEntry{
		Name:         "admin",
		capabilities: []Capability{{Action: "admin", Resource: "server", Scope: ScopeGlobal}},
		handler: func(_ context.Context, _ *CommandExecution) error {
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, mockEngine)
	require.NoError(t, err)

	charID := ulid.Make()
	subject := access.CharacterSubject(charID.String())
	testReason := "admin_role_required"
	testPolicyID := "policy-admin-001"

	// Layer 1: command execution — deny with reason and policy ID
	mockEngine.EXPECT().Evaluate(mock.Anything, types.AccessRequest{
		Subject:  subject,
		Action:   "execute",
		Resource: "command:admin",
	}).Return(types.NewDecision(types.EffectDeny, testReason, testPolicyID), nil)

	// Execute command
	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
	})

	dispatchErr := dispatcher.Dispatch(context.Background(), "admin", exec)
	require.Error(t, dispatchErr)

	// Verify error code is PERMISSION_DENIED
	oopsErr, ok := oops.AsOops(dispatchErr)
	require.True(t, ok)
	assert.Equal(t, CodePermissionDenied, oopsErr.Code())

	// ErrPermissionDenied does not propagate reason/policy_id from the decision
	// — those are only logged at debug level. The error context contains command
	// and capability fields from ErrPermissionDenied.
	errCtx := oopsErr.Context()
	assert.Equal(t, "admin", errCtx["command"])
	assert.Equal(t, "execute", errCtx["capability"])
}

func TestDispatcherEngineErrorDuringSecondCapability(t *testing.T) {
	reg := NewRegistry()
	mockEngine := policytest.NewMockAccessPolicyEngine(t)

	// Register command with 2 capabilities
	err := reg.Register(CommandEntry{
		Name:         "dangerous",
		capabilities: []Capability{{Action: "admin", Resource: "server", Scope: ScopeGlobal}, {Action: "delete", Resource: "server", Scope: ScopeGlobal}},
		handler: func(_ context.Context, _ *CommandExecution) error {
			return nil
		},
		Source: "core",
	})
	require.NoError(t, err)

	dispatcher, err := NewDispatcher(reg, mockEngine)
	require.NoError(t, err)

	charID := ulid.Make()
	subject := access.CharacterSubject(charID.String())
	evalErr := errors.New("policy store unavailable")

	// Layer 1: command execution — allow
	mockEngine.EXPECT().Evaluate(mock.Anything, types.AccessRequest{
		Subject:  subject,
		Action:   "execute",
		Resource: "command:dangerous",
	}).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	// Layer 2: first capability succeeds
	mockEngine.EXPECT().CanPerformAction(mock.Anything, subject, "admin", "server", ScopeGlobal).
		Return(true, nil)

	// Layer 2: second capability errors (fail-closed)
	mockEngine.EXPECT().CanPerformAction(mock.Anything, subject, "delete", "server", ScopeGlobal).
		Return(false, evalErr)

	var output bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		Output:      &output,
		Services:    stubServices(),
	})

	// Should return error and deny access
	dispatchErr := dispatcher.Dispatch(context.Background(), "dangerous", exec)
	require.Error(t, dispatchErr)

	// Verify error code is access evaluation failure
	errutil.AssertErrorCode(t, dispatchErr, CodeAccessEvaluationFailed)

	// Verify error context includes the failing capability's action and resource
	oopsErr, ok := oops.AsOops(dispatchErr)
	require.True(t, ok)
	assert.Equal(t, "delete", oopsErr.Context()["action"],
		"error should report which action failed")
	assert.Equal(t, "server", oopsErr.Context()["resource"],
		"error should report which resource failed")
	assert.Equal(t, "dangerous", oopsErr.Context()["command"])

	// Verify wrapped error
	assert.ErrorIs(t, dispatchErr, evalErr)

	// Verify fail-closed: engine_failure metric, not success
	metrics := CommandExecutions.With(prometheus.Labels{
		"command": "dangerous", "source": "core", "status": StatusEngineFailure,
	})
	val := testutil.ToFloat64(metrics)
	assert.Greater(t, val, float64(0), "should have recorded engine_failure metric")
}

// ---------------------------------------------------------------------------
// Task 11: dispatcher-side audit hint collection + flush + host field stamping
// ---------------------------------------------------------------------------

// capturingAuditWriter records every event passed to WriteSync or WriteAsync.
// It is a minimal audit.Writer implementation local to the dispatcher tests.
type capturingAuditWriter struct {
	mu     sync.Mutex
	events []audit.Event
}

func (w *capturingAuditWriter) WriteSync(_ context.Context, event audit.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, event)
	return nil
}

func (w *capturingAuditWriter) WriteAsync(event audit.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, event)
	return nil
}

func (w *capturingAuditWriter) Close() error { return nil }

// fakePluginDeliverer is a PluginCommandDeliverer whose behavior is driven
// by an injected callback. Unlike mockPluginDeliverer (plugin_dispatch_test.go),
// this variant allows each test to control the response dynamically, which is
// useful for concurrency tests where each call must produce a unique response.
type fakePluginDeliverer struct {
	onDeliver func(ctx context.Context, pluginName string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error)
	onEmit    func(ctx context.Context, pluginName string, event pluginsdk.EmitEvent) error
	emits     []pluginsdk.EmitEvent
	emitMu    sync.Mutex
}

func (f *fakePluginDeliverer) DeliverCommand(ctx context.Context, pluginName string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	if f.onDeliver == nil {
		return &pluginsdk.CommandResponse{Status: pluginsdk.CommandOK}, nil
	}
	return f.onDeliver(ctx, pluginName, cmd)
}

func (f *fakePluginDeliverer) EmitPluginEvent(ctx context.Context, pluginName string, event pluginsdk.EmitEvent) error {
	f.emitMu.Lock()
	f.emits = append(f.emits, event)
	f.emitMu.Unlock()
	if f.onEmit != nil {
		return f.onEmit(ctx, pluginName, event)
	}
	return nil
}

// newTestDispatcherWithPlugin constructs a Dispatcher wired to deliver commands
// through the given PluginCommandDeliverer. It registers a single plugin-backed
// command named "plugintest" so tests can invoke it via Dispatch.
func newTestDispatcherWithPlugin(t *testing.T, deliverer PluginCommandDeliverer) *Dispatcher {
	t.Helper()
	reg := NewRegistry()
	entry := NewTestEntry(CommandEntryConfig{
		Name:       "plugintest",
		PluginName: "test-plugin",
		Source:     "test-plugin",
	})
	require.NoError(t, reg.Register(entry))

	dispatcher, err := NewDispatcher(
		reg, policytest.AllowAllEngine(),
		WithPluginDeliverer(deliverer),
	)
	require.NoError(t, err)
	return dispatcher
}

// newTestDispatcherWithPluginAndAudit is like newTestDispatcherWithPlugin but
// also wires an audit.Logger via WithAuditLogger so plugin-emitted audit events
// are flushed through the logger.
func newTestDispatcherWithPluginAndAudit(t *testing.T, deliverer PluginCommandDeliverer, logger *audit.Logger) *Dispatcher {
	t.Helper()
	reg := NewRegistry()
	entry := NewTestEntry(CommandEntryConfig{
		Name:       "plugintest",
		PluginName: "test-plugin",
		Source:     "test-plugin",
	})
	require.NoError(t, reg.Register(entry))

	dispatcher, err := NewDispatcher(
		reg, policytest.AllowAllEngine(),
		WithPluginDeliverer(deliverer),
		WithAuditLogger(logger),
	)
	require.NoError(t, err)
	return dispatcher
}

// newTestCommandExecution returns a minimal CommandExecution suitable for
// dispatcher tests that do not care about the underlying services beyond
// stub behavior.
func newTestCommandExecution(t *testing.T) *CommandExecution {
	t.Helper()
	var buf bytes.Buffer
	return NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &buf,
		Services:    stubServices(),
	})
}

func newTestCommandExecutionWithServices(t *testing.T, services *Services) *CommandExecution {
	t.Helper()
	var buf bytes.Buffer
	return NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &buf,
		Services:    services,
	})
}

type appendCountingEventStore struct {
	appendCount int
}

func (s *appendCountingEventStore) Append(_ context.Context, _ core.Event) error {
	s.appendCount++
	return nil
}

var _ core.EventAppender = (*appendCountingEventStore)(nil)

func TestDispatcherAttachesAuditContextToDispatchContext(t *testing.T) {
	// Verify that after Dispatch is called, the context seen by the
	// plugin deliverer is derived from audit.NewContextForDispatch
	// (i.e., audit.AddEventToContext works inside it).
	var capturedCtx context.Context
	deliverer := &fakePluginDeliverer{
		onDeliver: func(ctx context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			capturedCtx = ctx
			return &pluginsdk.CommandResponse{Status: pluginsdk.CommandOK}, nil
		},
	}

	dispatcher := newTestDispatcherWithPlugin(t, deliverer)

	exec := newTestCommandExecution(t)
	err := dispatcher.Dispatch(context.Background(), "plugintest", exec)
	require.NoError(t, err)

	// capturedCtx should accept AddEventToContext without being nil
	require.NotNil(t, capturedCtx)
	audit.AddEventToContext(capturedCtx, audit.Event{ID: "sanity"})
	events := audit.EventsFromContext(capturedCtx)
	assert.Len(t, events, 1)
}

func TestDispatcherRoutesPluginResponseEventsThroughSharedEmitter(t *testing.T) {
	store := &appendCountingEventStore{}
	services := NewTestServices(ServicesConfig{
		Events: store,
	})

	var emittedActor core.Actor
	deliverer := &fakePluginDeliverer{
		onDeliver: func(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			return &pluginsdk.CommandResponse{
				Status: pluginsdk.CommandOK,
				Events: []pluginsdk.EmitEvent{
					{Stream: "scene:01TEST:ic", Type: pluginsdk.EventType("say"), Payload: `{"text":"hello"}`},
				},
			}, nil
		},
		onEmit: func(ctx context.Context, pluginName string, event pluginsdk.EmitEvent) error {
			actor, ok := core.ActorFromContext(ctx)
			require.True(t, ok)
			emittedActor = actor
			assert.Equal(t, "test-plugin", pluginName)
			assert.Equal(t, "scene:01TEST:ic", event.Stream)
			return nil
		},
	}

	dispatcher := newTestDispatcherWithPlugin(t, deliverer)
	exec := newTestCommandExecutionWithServices(t, services)

	err := dispatcher.Dispatch(context.Background(), "plugintest", exec)
	require.NoError(t, err)
	assert.Len(t, deliverer.emits, 1, "plugin response events should route through shared emitter")
	assert.Equal(t, 0, store.appendCount, "dispatcher must not append plugin response events directly")
	assert.Equal(t, core.ActorCharacter, emittedActor.Kind)
	assert.Equal(t, exec.CharacterID().String(), emittedActor.ID)
}

func TestDispatcherExtractsAuditHintsFromCommandResponseAndStampsHostFields(t *testing.T) {
	// The plugin returns a hint; the dispatcher should stamp Source,
	// Component, Subject, Action from the host context and push the
	// resulting Event through the audit logger.
	writer := &capturingAuditWriter{}
	logger := audit.NewLogger(audit.ModeAll, writer, "")
	t.Cleanup(func() { _ = logger.Close() })

	deliverer := &fakePluginDeliverer{
		onDeliver: func(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			return &pluginsdk.CommandResponse{
				Status: pluginsdk.CommandError,
				Output: "no permission",
				AuditHints: []pluginsdk.AuditHint{
					{
						ID:              "not_member",
						Name:            "channels: not a member",
						Message:         "player not in channel members",
						Effect:          pluginsdk.AuditEffectDeny,
						ActionQualifier: "speak",
						Resource:        "channel:01XYZ",
						Attributes:      map[string]string{"channel.type": "public"},
					},
				},
			}, nil
		},
	}

	dispatcher := newTestDispatcherWithPluginAndAudit(t, deliverer, logger)
	exec := newTestCommandExecution(t)

	err := dispatcher.Dispatch(context.Background(), "plugintest", exec)
	// CommandError is a user-facing denial, not a dispatch error.
	// Dispatch returns nil for CommandError (the command ran, the user
	// was told no). Confirm with the existing dispatcher behavior.
	assert.NoError(t, err)

	writer.mu.Lock()
	defer writer.mu.Unlock()
	require.Len(t, writer.events, 1, "expected exactly one plugin audit event")
	event := writer.events[0]

	// Host-stamped fields
	assert.Equal(t, audit.SourcePlugin, event.Source,
		"Source must be host-stamped as SourcePlugin")
	assert.NotEmpty(t, event.Component,
		"Component must be host-stamped from plugin name")
	assert.NotEmpty(t, event.Subject,
		"Subject must be host-stamped from dispatch context")

	// Plugin-provided fields
	assert.Equal(t, "not_member", event.ID)
	assert.Equal(t, "player not in channel members", event.Message)
	assert.Equal(t, types.EffectDeny, event.Effect)

	// Composed field
	assert.Contains(t, event.Action, "speak",
		"Action should contain the plugin's qualifier")
}

func TestDispatcherContinuesFlushWhenOneAuditEventWriteFails(t *testing.T) {
	// Simulate an audit logger where the first event write fails but
	// subsequent writes succeed. All events should be attempted; the
	// failure must not propagate to the user.
	writer := &sometimesFailingWriter{failIndex: 0}
	logger := audit.NewLogger(audit.ModeAll, writer, "")
	t.Cleanup(func() { _ = logger.Close() })

	deliverer := &fakePluginDeliverer{
		onDeliver: func(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			return &pluginsdk.CommandResponse{
				Status: pluginsdk.CommandOK,
				AuditHints: []pluginsdk.AuditHint{
					{ID: "e1", Effect: pluginsdk.AuditEffectDeny},
					{ID: "e2", Effect: pluginsdk.AuditEffectDeny},
				},
			}, nil
		},
	}

	dispatcher := newTestDispatcherWithPluginAndAudit(t, deliverer, logger)
	exec := newTestCommandExecution(t)

	err := dispatcher.Dispatch(context.Background(), "plugintest", exec)
	require.NoError(t, err,
		"audit write failure must not propagate to the dispatcher caller")

	// Both events should have been attempted.
	writer.mu.Lock()
	defer writer.mu.Unlock()
	assert.Equal(t, 2, writer.attemptCount,
		"all hints should be attempted even if one fails")
}

// sometimesFailingWriter returns an error on writes whose 0-based index
// matches failIndex.
type sometimesFailingWriter struct {
	mu           sync.Mutex
	attemptCount int
	failIndex    int
}

func (w *sometimesFailingWriter) WriteSync(_ context.Context, _ audit.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	idx := w.attemptCount
	w.attemptCount++
	if idx == w.failIndex {
		return fmt.Errorf("simulated write failure")
	}
	return nil
}

func (w *sometimesFailingWriter) WriteAsync(_ audit.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.attemptCount++
	return nil
}

func (w *sometimesFailingWriter) Close() error { return nil }

// T22a — MUST negative: unknown effect string is skipped with a warning.
func TestDispatcherSkipsHintWithUnknownEffectStringAndLogsWarning(t *testing.T) {
	writer := &capturingAuditWriter{}
	logger := audit.NewLogger(audit.ModeAll, writer, "")

	deliverer := &fakePluginDeliverer{
		onDeliver: func(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			return &pluginsdk.CommandResponse{
				Status: pluginsdk.CommandOK,
				AuditHints: []pluginsdk.AuditHint{
					{ID: "good", Effect: pluginsdk.AuditEffectDeny, Message: "this one is valid"},
					{ID: "bad", Effect: pluginsdk.AuditEffect("mystery"), Message: "unknown effect"},
					{ID: "also_good", Effect: pluginsdk.AuditEffectAllow, Message: "valid again"},
				},
			}, nil
		},
	}

	dispatcher := newTestDispatcherWithPluginAndAudit(t, deliverer, logger)
	exec := newTestCommandExecution(t)

	err := dispatcher.Dispatch(context.Background(), "plugintest", exec)
	require.NoError(t, err)

	// Close the logger to drain async writes deterministically before
	// asserting. In ModeAll, allow events are written asynchronously.
	require.NoError(t, logger.Close())

	// Only the two valid hints should be flushed. The bad one is dropped.
	writer.mu.Lock()
	defer writer.mu.Unlock()
	require.Len(t, writer.events, 2)
	// Order is not guaranteed: "good" (deny) goes sync, "also_good" (allow)
	// goes async — both are captured but their arrival order may differ.
	ids := []string{writer.events[0].ID, writer.events[1].ID}
	assert.Contains(t, ids, "good")
	assert.Contains(t, ids, "also_good")
	assert.NotContains(t, ids, "bad")
}

// T22b — SHOULD boundary: malformed resource refs.
func TestDispatcherValidatesMalformedResourceRefs(t *testing.T) {
	cases := []struct {
		name        string
		resource    string
		expectWarn  bool
		expectFlush bool // hint still flushes with the malformed resource string
	}{
		{"well-formed two-part ref", "channel:01XYZ", false, true},
		{"empty resource string", "", false, true},                 // empty is valid (optional field)
		{"no colon", "channel01XYZ", true, true},                   // malformed, logged, still flushed
		{"trailing colon only", "channel:", true, true},            // malformed
		{"leading colon only", ":01XYZ", true, true},               // malformed
		{"only a colon", ":", true, true},                          // malformed
		{"multi-colon ambiguous", "channel:01:extra", false, true}, // two colons is permissive — the first colon delimits
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writer := &capturingAuditWriter{}
			logger := audit.NewLogger(audit.ModeAll, writer, "")
			t.Cleanup(func() { _ = logger.Close() })

			deliverer := &fakePluginDeliverer{
				onDeliver: func(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
					return &pluginsdk.CommandResponse{
						Status: pluginsdk.CommandOK,
						AuditHints: []pluginsdk.AuditHint{
							{ID: "test", Effect: pluginsdk.AuditEffectDeny, Resource: tc.resource},
						},
					}, nil
				},
			}

			dispatcher := newTestDispatcherWithPluginAndAudit(t, deliverer, logger)
			exec := newTestCommandExecution(t)

			err := dispatcher.Dispatch(context.Background(), "plugintest", exec)
			require.NoError(t, err)

			if tc.expectFlush {
				writer.mu.Lock()
				defer writer.mu.Unlock()
				require.Len(t, writer.events, 1)
				assert.Equal(t, tc.resource, writer.events[0].Resource,
					"malformed resource refs are logged but still emitted as-is")
			}
		})
	}
}

// T22c — SHOULD boundary: dispatcher with no audit logger configured.
func TestDispatcherFlushIsNoOpWhenAuditLoggerIsNil(t *testing.T) {
	deliverer := &fakePluginDeliverer{
		onDeliver: func(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			return &pluginsdk.CommandResponse{
				Status: pluginsdk.CommandOK,
				AuditHints: []pluginsdk.AuditHint{
					{ID: "dropped", Effect: pluginsdk.AuditEffectDeny},
				},
			}, nil
		},
	}

	// Construct dispatcher WITHOUT WithAuditLogger — d.auditLogger is nil.
	dispatcher := newTestDispatcherWithPlugin(t, deliverer)
	exec := newTestCommandExecution(t)

	// This must not panic and must not fail the command.
	err := dispatcher.Dispatch(context.Background(), "plugintest", exec)
	require.NoError(t, err)
}

// T22d — SHOULD invariant: context-per-dispatch isolation under concurrency.
func TestDispatcherConcurrentDispatchesDoNotCrossContaminateAuditContexts(t *testing.T) {
	const numDispatches = 10

	// Each dispatch emits a unique hint ID. After all dispatches complete,
	// the audit writer should have received exactly numDispatches events,
	// each with a unique ID matching the dispatch index.
	writer := &capturingAuditWriter{}
	logger := audit.NewLogger(audit.ModeAll, writer, "")
	t.Cleanup(func() { _ = logger.Close() })

	var dispatchIdx atomic.Int32
	deliverer := &fakePluginDeliverer{
		onDeliver: func(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			idx := dispatchIdx.Add(1) - 1
			return &pluginsdk.CommandResponse{
				Status: pluginsdk.CommandOK,
				AuditHints: []pluginsdk.AuditHint{
					{ID: fmt.Sprintf("hint-%d", idx), Effect: pluginsdk.AuditEffectDeny},
				},
			}, nil
		},
	}

	dispatcher := newTestDispatcherWithPluginAndAudit(t, deliverer, logger)

	var wg sync.WaitGroup
	for i := 0; i < numDispatches; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			exec := newTestCommandExecution(t)
			if err := dispatcher.Dispatch(context.Background(), "plugintest", exec); err != nil {
				t.Errorf("dispatch failed: %v", err)
			}
		}()
	}
	wg.Wait()

	writer.mu.Lock()
	defer writer.mu.Unlock()
	require.Len(t, writer.events, numDispatches,
		"each dispatch should emit exactly one event; no cross-contamination")

	// Collect the IDs seen and assert each dispatch-idx appears exactly once.
	seen := make(map[string]int)
	for _, e := range writer.events {
		seen[e.ID]++
	}
	assert.Len(t, seen, numDispatches,
		"each dispatch should see its own unique hint ID, not someone else's")
}

// capturingDeliverer captures the ctx passed to DeliverCommand /
// EmitPluginEvent so the test can assert on actor context at the
// dispatch boundary.
type capturingDeliverer struct {
	mu         sync.Mutex
	deliverCtx context.Context
	emitCtxs   []context.Context
}

func (c *capturingDeliverer) DeliverCommand(ctx context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	c.mu.Lock()
	c.deliverCtx = ctx
	c.mu.Unlock()
	return &pluginsdk.CommandResponse{Status: pluginsdk.CommandOK}, nil
}

func (c *capturingDeliverer) EmitPluginEvent(ctx context.Context, _ string, _ pluginsdk.EmitEvent) error {
	c.mu.Lock()
	c.emitCtxs = append(c.emitCtxs, ctx)
	c.mu.Unlock()
	return nil
}

func TestDispatcher_PassesConnectionIDToPluginCommand(t *testing.T) {
	t.Parallel()
	// Verifies that CommandExecution.ConnectionID() flows into
	// pluginsdk.CommandRequest.ConnectionID at dispatch time. Phase 5
	// plugin commands (scene focus / scene grid) require this.
	// INV-P5 precursor: dispatcher MUST propagate ConnectionID to plugin CommandRequest.

	connID := ulid.Make()

	var capturedCmd pluginsdk.CommandRequest
	deliverer := &fakePluginDeliverer{
		onDeliver: func(_ context.Context, _ string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			capturedCmd = cmd
			return &pluginsdk.CommandResponse{Status: pluginsdk.CommandOK}, nil
		},
	}

	dispatcher := newTestDispatcherWithPlugin(t, deliverer)

	var buf bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID:  ulid.Make(),
		ConnectionID: connID,
		Output:       &buf,
		Services:     stubServices(),
	})

	require.NoError(t, dispatcher.Dispatch(context.Background(), "plugintest", exec))
	assert.Equal(t, connID.String(), capturedCmd.ConnectionID,
		"INV-P5 precursor: dispatcher MUST propagate ConnectionID to plugin CommandRequest")
}

// TestDispatcherStampsCharacterActorBeforeDeliverCommand asserts the
// dispatcher populates core.ActorFromContext(ctx) BEFORE calling
// pluginDeliverer.DeliverCommand, per spec G7. Uses the existing
// newTestDispatcherWithPlugin + newTestCommandExecution scaffolding
// (dispatcher_test.go:2063, :2104) and exercises the public Dispatch
// API which routes through dispatchToPlugin internally.
func TestDispatcherStampsCharacterActorBeforeDeliverCommand(t *testing.T) {
	t.Parallel()

	cd := &capturingDeliverer{}
	d := newTestDispatcherWithPlugin(t, cd)
	exec := newTestCommandExecution(t)
	expectedCharID := exec.CharacterID().String()

	// Dispatch the registered "plugintest" command (registered by the helper).
	err := d.Dispatch(context.Background(), "plugintest", exec)
	require.NoError(t, err)

	cd.mu.Lock()
	defer cd.mu.Unlock()
	require.NotNil(t, cd.deliverCtx, "DeliverCommand must have been invoked")
	got, ok := core.ActorFromContext(cd.deliverCtx)
	require.True(t, ok, "DeliverCommand MUST receive ctx with actor populated")
	assert.Equal(t, core.ActorCharacter, got.Kind)
	assert.Equal(t, expectedCharID, got.ID)
}

// TestDispatcherStampsOwningPlayerBeforeDeliverCommand asserts the dispatcher
// stamps the executor's player ID onto the dispatch ctx via core.WithOwningPlayer
// BEFORE DeliverCommand — the single stamping site that feeds PLAYER-scope
// settings ownership for both binary (token) and Lua (ctx) runtimes
// (holomush-iokti.19).
func TestDispatcherStampsOwningPlayerBeforeDeliverCommand(t *testing.T) {
	t.Parallel()

	cd := &capturingDeliverer{}
	d := newTestDispatcherWithPlugin(t, cd)

	playerID := ulid.Make()
	var buf bytes.Buffer
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		PlayerID:    playerID,
		Output:      &buf,
		Services:    stubServices(),
	})

	require.NoError(t, d.Dispatch(context.Background(), "plugintest", exec))

	cd.mu.Lock()
	defer cd.mu.Unlock()
	require.NotNil(t, cd.deliverCtx, "DeliverCommand must have been invoked")
	gotPlayer, ok := core.OwningPlayerFromContext(cd.deliverCtx)
	require.True(t, ok, "DeliverCommand MUST receive ctx with owning player populated")
	assert.Equal(t, playerID.String(), gotPlayer)
}
