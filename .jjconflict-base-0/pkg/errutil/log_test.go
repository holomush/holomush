// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package errutil_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestLogError_WithOopsError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	err := oops.Code("TEST_ERROR").
		With("key", "value").
		Errorf("something failed")

	errutil.LogError(logger, "operation failed", err)

	var logEntry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
	assert.Equal(t, "ERROR", logEntry["level"])
	assert.Equal(t, "operation failed", logEntry["msg"])
	assert.Equal(t, "TEST_ERROR", logEntry["code"])
}

func TestLogErrorContext_WithOopsError(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil))) })

	err := oops.Code("TEST_CTX_ERROR").
		With("key", "value").
		Errorf("context failure")

	errutil.LogErrorContext(context.Background(), "ctx operation failed", err,
		"extra_key", "extra_val",
	)

	var logEntry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
	assert.Equal(t, "ERROR", logEntry["level"])
	assert.Equal(t, "ctx operation failed", logEntry["msg"])
	assert.Equal(t, "TEST_CTX_ERROR", logEntry["code"])
	assert.Equal(t, "extra_val", logEntry["extra_key"])
}

func TestLogErrorContext_WithStandardError(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil))) })

	err := errors.New("standard ctx error")

	errutil.LogErrorContext(context.Background(), "ctx operation failed", err)

	var logEntry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
	assert.Equal(t, "ERROR", logEntry["level"])
	assert.Contains(t, logEntry["error"], "standard ctx error")
}

func TestLogError_WithStandardError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	err := errors.New("standard error")

	errutil.LogError(logger, "operation failed", err)

	var logEntry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
	assert.Equal(t, "ERROR", logEntry["level"])
	assert.Contains(t, logEntry["error"], "standard error")
}
