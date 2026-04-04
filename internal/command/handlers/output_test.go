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

func TestLogOutputErrorLogsAtWarnLevel(t *testing.T) {
	buf := setTestLogger(t)

	ctx := context.Background()
	testErr := errors.New("connection reset by peer")

	logOutputError(ctx, "quit", "01JKWK0000TESTCHARACTER01", 42, testErr)

	output := buf.String()
	assert.Contains(t, output, "WARN")
	assert.Contains(t, output, "failed to write command output")
	assert.Contains(t, output, "quit")
	assert.Contains(t, output, "01JKWK0000TESTCHARACTER01")
	assert.Contains(t, output, "42")
	assert.Contains(t, output, "connection reset by peer")
}

func TestLogOutputErrorIncludesAllStructuredFields(t *testing.T) {
	buf := setTestLogger(t)

	ctx := context.Background()
	testErr := errors.New("broken pipe")

	logOutputError(ctx, "shutdown", "01JKWK0000TESTCHARACTER02", 128, testErr)

	output := buf.String()
	assert.Contains(t, output, `"command":"shutdown"`)
	assert.Contains(t, output, `"character_id":"01JKWK0000TESTCHARACTER02"`)
	assert.Contains(t, output, `"bytes_written":128`)
	assert.Contains(t, output, `"error":"broken pipe"`)
}

func TestLogOutputErrorHandlesZeroBytesWritten(t *testing.T) {
	buf := setTestLogger(t)

	ctx := context.Background()
	testErr := errors.New("write: resource temporarily unavailable")

	logOutputError(ctx, "quit", "01JKWK0000TESTCHARACTER03", 0, testErr)

	output := buf.String()
	assert.Contains(t, output, `"bytes_written":0`)
}

func TestLogOutputError_HandlesVariousCommands(t *testing.T) {
	commands := []string{"shutdown", "quit"}

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

func TestWriteOutputLogsErrorOnWriteFailure(t *testing.T) {
	buf := setTestLogger(t)
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: ulid.MustParse("01HQ1234567890ABCDEFGH1001"),
		Output:      &failingWriter{n: 7, err: errors.New("write failed")},
	})

	writeOutput(context.Background(), exec, "quit", "Goodbye!")

	output := buf.String()
	assert.Contains(t, output, `"command":"quit"`)
	assert.Contains(t, output, `"character_id":"01HQ1234567890ABCDEFGH1001"`)
	assert.Contains(t, output, `"bytes_written":7`)
	assert.Contains(t, output, `"error":"write failed"`)
}

func TestWriteOutputfLogsErrorOnWriteFailure(t *testing.T) {
	buf := setTestLogger(t)
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: ulid.MustParse("01HQ1234567890ABCDEFGH1002"),
		Output:      &failingWriter{n: 3, err: errors.New("format failed")},
	})

	writeOutputf(context.Background(), exec, "shutdown", "Shutting down in %d seconds...\n", 60)

	output := buf.String()
	assert.Contains(t, output, `"command":"shutdown"`)
	assert.Contains(t, output, `"character_id":"01HQ1234567890ABCDEFGH1002"`)
	assert.Contains(t, output, `"bytes_written":3`)
	assert.Contains(t, output, `"error":"format failed"`)
}
