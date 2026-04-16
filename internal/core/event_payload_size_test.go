// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core_test

import (
	"testing"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePayloadAcceptsPayloadBelowLimit(t *testing.T) {
	payload := make([]byte, 1024)
	require.NoError(t, core.ValidatePayload(payload))
}

func TestValidatePayloadAcceptsPayloadAtLimit(t *testing.T) {
	payload := make([]byte, core.MaxPayloadSize)
	require.NoError(t, core.ValidatePayload(payload))
}

func TestValidatePayloadRejectsPayloadAboveLimit(t *testing.T) {
	payload := make([]byte, core.MaxPayloadSize+1)
	err := core.ValidatePayload(payload)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENT_PAYLOAD_TOO_LARGE")
	errutil.AssertErrorContext(t, err, "payload_size", core.MaxPayloadSize+1)
	errutil.AssertErrorContext(t, err, "max_payload_size", core.MaxPayloadSize)
}

func TestValidatePayloadAcceptsEmptyPayload(t *testing.T) {
	require.NoError(t, core.ValidatePayload(nil))
	require.NoError(t, core.ValidatePayload([]byte{}))
}

func TestMaxPayloadSizeIs64KiB(t *testing.T) {
	assert.Equal(t, 64*1024, core.MaxPayloadSize)
}
