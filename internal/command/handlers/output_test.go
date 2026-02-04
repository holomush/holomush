// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogOutputError_LogsAtWarnLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	ctx := context.Background()
	testErr := errors.New("connection reset by peer")

	logOutputError(ctx, "look", "01JKWK0000TESTCHARACTER01", 42, testErr)

	output := buf.String()
	assert.Contains(t, output, "WARN")
	assert.Contains(t, output, "failed to write command output")
	assert.Contains(t, output, "look")
	assert.Contains(t, output, "01JKWK0000TESTCHARACTER01")
	assert.Contains(t, output, "42")
	assert.Contains(t, output, "connection reset by peer")
}

func TestLogOutputError_IncludesAllStructuredFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	ctx := context.Background()
	testErr := errors.New("broken pipe")

	logOutputError(ctx, "move", "01JKWK0000TESTCHARACTER02", 128, testErr)

	output := buf.String()
	// Verify all required structured fields are present
	assert.Contains(t, output, `"command":"move"`)
	assert.Contains(t, output, `"character_id":"01JKWK0000TESTCHARACTER02"`)
	assert.Contains(t, output, `"bytes_written":128`)
	assert.Contains(t, output, `"error":"broken pipe"`)
}

func TestLogOutputError_HandlesZeroBytesWritten(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	ctx := context.Background()
	testErr := errors.New("write: resource temporarily unavailable")

	logOutputError(ctx, "who", "01JKWK0000TESTCHARACTER03", 0, testErr)

	output := buf.String()
	assert.Contains(t, output, `"bytes_written":0`)
}

func TestLogOutputError_HandlesVariousCommands(t *testing.T) {
	commands := []string{
		"look", "move", "who", "wall", "shutdown", "boot", "quit", "create", "set",
	}

	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
			slog.SetDefault(logger)

			ctx := context.Background()
			testErr := errors.New("test error")

			logOutputError(ctx, cmd, "01JKWK0000TESTCHARACTER00", 10, testErr)

			output := buf.String()
			assert.True(t, strings.Contains(output, `"command":"`+cmd+`"`),
				"expected command %q in output: %s", cmd, output)
		})
	}
}
