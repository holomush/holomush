// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
)

func TestErrUnknownCommand(t *testing.T) {
	err := ErrUnknownCommand("foo")
	assert.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	assert.True(t, ok)
	assert.Equal(t, "UNKNOWN_COMMAND", oopsErr.Code())
	assert.Equal(t, "foo", oopsErr.Context()["command"])
}

func TestErrPermissionDenied(t *testing.T) {
	err := ErrPermissionDenied("boot", "admin.boot")
	oopsErr, _ := oops.AsOops(err)
	assert.Equal(t, "PERMISSION_DENIED", oopsErr.Code())
	assert.Equal(t, "boot", oopsErr.Context()["command"])
	assert.Equal(t, "admin.boot", oopsErr.Context()["capability"])
}

func TestErrInvalidArgs(t *testing.T) {
	err := ErrInvalidArgs("look", "look [target]")
	oopsErr, _ := oops.AsOops(err)
	assert.Equal(t, "INVALID_ARGS", oopsErr.Code())
	assert.Equal(t, "look", oopsErr.Context()["command"])
	assert.Equal(t, "look [target]", oopsErr.Context()["usage"])
}

func TestWorldError(t *testing.T) {
	err := WorldError("There's no exit to the north.", nil)
	oopsErr, _ := oops.AsOops(err)
	assert.Equal(t, "WORLD_ERROR", oopsErr.Code())
	assert.Equal(t, "There's no exit to the north.", oopsErr.Context()["message"])
}

func TestWorldError_WithCause(t *testing.T) {
	cause := oops.Errorf("database connection failed")
	err := WorldError("There's no exit to the north.", cause)

	oopsErr, ok := oops.AsOops(err)
	assert.True(t, ok)
	assert.Equal(t, "WORLD_ERROR", oopsErr.Code())
	assert.Equal(t, "There's no exit to the north.", oopsErr.Context()["message"])
}

func TestErrRateLimited(t *testing.T) {
	err := ErrRateLimited(1000)
	oopsErr, _ := oops.AsOops(err)
	assert.Equal(t, "RATE_LIMITED", oopsErr.Code())
	assert.Equal(t, int64(1000), oopsErr.Context()["cooldown_ms"])
}

func TestErrCircularAlias(t *testing.T) {
	err := ErrCircularAlias("loop")
	oopsErr, _ := oops.AsOops(err)
	assert.Equal(t, "CIRCULAR_ALIAS", oopsErr.Code())
	assert.Equal(t, "loop", oopsErr.Context()["alias"])
	assert.Contains(t, err.Error(), "circular reference detected")
}

func TestErrTargetNotFound(t *testing.T) {
	err := ErrTargetNotFound("NonexistentPlayer")
	assert.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	assert.True(t, ok)
	assert.Equal(t, "TARGET_NOT_FOUND", oopsErr.Code())
	assert.Equal(t, "NonexistentPlayer", oopsErr.Context()["target"])
	assert.Contains(t, err.Error(), "player not found")
	assert.Contains(t, err.Error(), "NonexistentPlayer")
}

func TestPlayerMessage(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "world error with message",
			err:      WorldError("There's no exit to the north.", nil),
			expected: "There's no exit to the north.",
		},
		{
			name:     "unknown command",
			err:      ErrUnknownCommand("foo"),
			expected: "Unknown command. Try 'help'.",
		},
		{
			name:     "permission denied",
			err:      ErrPermissionDenied("boot", "admin.boot"),
			expected: "You don't have permission to do that.",
		},
		{
			name:     "invalid args with usage",
			err:      ErrInvalidArgs("look", "look [target]"),
			expected: "Usage: look [target]",
		},
		{
			name:     "rate limited",
			err:      ErrRateLimited(1000),
			expected: "Too many commands. Please slow down.",
		},
		{
			name:     "circular alias",
			err:      ErrCircularAlias("loop"),
			expected: "Alias rejected: circular reference detected (expansion depth exceeded)",
		},
		{
			name:     "target not found with name",
			err:      ErrTargetNotFound("Alice"),
			expected: "Target not found: Alice",
		},
		{
			name:     "no character",
			err:      ErrNoCharacter(),
			expected: "No character selected. Please select a character first.",
		},
		{
			name:     "generic error",
			err:      oops.Errorf("something broke"),
			expected: "Something went wrong. Try again.",
		},
		{
			name:     "invalid args with empty usage",
			err:      ErrInvalidArgs("foo", ""),
			expected: "Invalid arguments.",
		},
		{
			name:     "nil error",
			err:      nil,
			expected: "Something went wrong. Try again.",
		},
		{
			name:     "invalid name - empty alias",
			err:      ValidateAliasName(""),
			expected: "alias name cannot be empty",
		},
		{
			name:     "invalid name - too long",
			err:      ValidateAliasName("thisaliasnameiswaywaytoolong"),
			expected: "alias name exceeds maximum length of 20",
		},
		{
			name:     "invalid name - bad pattern",
			err:      ValidateAliasName("123bad"),
			expected: "alias name must start with a letter and contain only letters, digits, or _!?@#$%^+-",
		},
		{
			name:     "no alias cache",
			err:      oops.Code(CodeNoAliasCache).Errorf("alias operations require a configured alias cache"),
			expected: "Alias system is not available. Contact the server administrator.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := PlayerMessage(tt.err)
			assert.Equal(t, tt.expected, msg)
		})
	}
}
