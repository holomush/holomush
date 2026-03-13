// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk_test

import (
	"context"
	"testing"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/stretchr/testify/assert"
)

func TestHandler_Interface(_ *testing.T) {
	// Verify Handler interface is properly defined
	var _ pluginsdk.Handler = (*testHandler)(nil)
}

type testHandler struct{}

func (h *testHandler) HandleEvent(_ context.Context, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return []pluginsdk.EmitEvent{
		{
			Stream:  event.Stream,
			Type:    event.Type,
			Payload: event.Payload,
		},
	}, nil
}

func TestServeConfig_HandlerRequired(t *testing.T) {
	defer func() {
		r := recover()
		assert.NotNil(t, r, "Serve should panic with nil Handler")
	}()

	pluginsdk.Serve(&pluginsdk.ServeConfig{Handler: nil})
}

func TestServeConfig_ConfigRequired(t *testing.T) {
	defer func() {
		r := recover()
		assert.NotNil(t, r, "Serve should panic with nil config")
	}()

	pluginsdk.Serve(nil)
}

func TestHandshakeConfig(t *testing.T) {
	assert.Equal(t, uint(1), pluginsdk.HandshakeConfig.ProtocolVersion, "HandshakeConfig protocol version should be 1")
	assert.Equal(t, "HOLOMUSH_PLUGIN", pluginsdk.HandshakeConfig.MagicCookieKey, "HandshakeConfig magic cookie key mismatch")
	assert.Equal(t, "holomush-v1", pluginsdk.HandshakeConfig.MagicCookieValue, "HandshakeConfig magic cookie value mismatch")
}
