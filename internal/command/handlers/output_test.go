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

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/command"
)

func setTestLogger(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() {
		slog.SetDefault(prev)
	})

	return &buf
}

type failingWriter struct {
	n   int
	err error
}

func (writer failingWriter) Write(_ []byte) (int, error) {
	return writer.n, writer.err
}

func TestLogOutputError_LogsAtWarnLevel(t *testing.T) {
	buf := setTestLogger(t)

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
	buf := setTestLogger(t)

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
	buf := setTestLogger(t)

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
			buf := setTestLogger(t)

			ctx := context.Background()
			testErr := errors.New("test error")

			logOutputError(ctx, cmd, "01JKWK0000TESTCHARACTER00", 10, testErr)

			output := buf.String()
			assert.True(t, strings.Contains(output, `"command":"`+cmd+`"`),
				"expected command %q in output: %s", cmd, output)
		})
	}
}

func TestWriteOutput_LogsErrorOnWriteFailure(t *testing.T) {
	buf := setTestLogger(t)
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: ulid.MustParse("01HQ1234567890ABCDEFGH1001"),
		Output:      &failingWriter{n: 7, err: errors.New("write failed")},
	})

	writeOutput(context.Background(), exec, "look", "Hello")

	output := buf.String()
	assert.Contains(t, output, `"command":"look"`)
	assert.Contains(t, output, `"character_id":"01HQ1234567890ABCDEFGH1001"`)
	assert.Contains(t, output, `"bytes_written":7`)
	assert.Contains(t, output, `"error":"write failed"`)
}

func TestWriteOutputf_LogsErrorOnWriteFailure(t *testing.T) {
	buf := setTestLogger(t)
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: ulid.MustParse("01HQ1234567890ABCDEFGH1002"),
		Output:      &failingWriter{n: 3, err: errors.New("format failed")},
	})

	writeOutputf(context.Background(), exec, "who", "Hello %s", "world")

	output := buf.String()
	assert.Contains(t, output, `"command":"who"`)
	assert.Contains(t, output, `"character_id":"01HQ1234567890ABCDEFGH1002"`)
	assert.Contains(t, output, `"bytes_written":3`)
	assert.Contains(t, output, `"error":"format failed"`)
}

func TestHandleError_WritesUserMessageAndReturnsWorldError(t *testing.T) {
	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: ulid.MustParse("01HQ1234567890ABCDEFGH0004"),
		Output:      &buf,
	})

	err := handleError(context.Background(), exec, "create", "Failed to create object.", "create object failed", errors.New("boom"))

	assert.Equal(t, "Failed to create object.\n", buf.String())

	oopsErr, ok := oops.AsOops(err)
	assert.True(t, ok)
	assert.Equal(t, command.CodeWorldError, oopsErr.Code())
	assert.Equal(t, "create object failed", oopsErr.Context()["message"])
}

func TestWriteOutputWithWorldError_WritesMessageAndReturnsWorldError(t *testing.T) {
	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: ulid.MustParse("01HQ1234567890ABCDEFGH0001"),
		Output:      &buf,
	})

	err := writeOutputWithWorldError(context.Background(), exec, "create", "Failed to create object.", errors.New("boom"))

	assert.Equal(t, "Failed to create object.\n", buf.String())

	oopsErr, ok := oops.AsOops(err)
	assert.True(t, ok)
	assert.Equal(t, command.CodeWorldError, oopsErr.Code())
	assert.Equal(t, "Failed to create object.", oopsErr.Context()["message"])
}

func TestWriteOutputfWithWorldError_WritesFormattedMessageAndReturnsWorldError(t *testing.T) {
	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: ulid.MustParse("01HQ1234567890ABCDEFGH0002"),
		Output:      &buf,
	})

	err := writeOutputfWithWorldError(context.Background(), exec, "set", "Unknown property: %s\n", errors.New("boom"), "desc")

	assert.Equal(t, "Unknown property: desc\n", buf.String())

	oopsErr, ok := oops.AsOops(err)
	assert.True(t, ok)
	assert.Equal(t, command.CodeWorldError, oopsErr.Code())
	assert.Equal(t, "Unknown property: desc\n", oopsErr.Context()["message"])
}

func TestWriteLocationOutput_WritesNameAndDescription(t *testing.T) {
	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: ulid.MustParse("01HQ1234567890ABCDEFGH0003"),
		Output:      &buf,
	})

	writeLocationOutput(context.Background(), exec, "look", "The Atrium", "Sunlight floods the hall.")

	assert.Equal(t, "The Atrium\nSunlight floods the hall.\n", buf.String())
}
