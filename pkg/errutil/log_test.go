// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package errutil_test

import (
	"bytes"
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
