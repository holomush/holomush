// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugin_test

import (
	"context"
	"testing"

	"github.com/holomush/holomush/pkg/plugin"
	"github.com/stretchr/testify/assert"
)

func TestHandler_Interface(_ *testing.T) {
	// Verify Handler interface is properly defined
	var _ plugin.Handler = (*testHandler)(nil)
}

type testHandler struct{}

func (h *testHandler) HandleEvent(_ context.Context, event plugin.Event) ([]plugin.EmitEvent, error) {
	return []plugin.EmitEvent{
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

	plugin.Serve(&plugin.ServeConfig{Handler: nil})
}

func TestServeConfig_ConfigRequired(t *testing.T) {
	defer func() {
		r := recover()
		assert.NotNil(t, r, "Serve should panic with nil config")
	}()

	plugin.Serve(nil)
}

func TestHandshakeConfig(t *testing.T) {
	assert.Equal(t, uint(1), plugin.HandshakeConfig.ProtocolVersion, "HandshakeConfig protocol version should be 1")
	assert.Equal(t, "HOLOMUSH_PLUGIN", plugin.HandshakeConfig.MagicCookieKey, "HandshakeConfig magic cookie key mismatch")
	assert.Equal(t, "holomush-v1", plugin.HandshakeConfig.MagicCookieValue, "HandshakeConfig magic cookie value mismatch")
}
