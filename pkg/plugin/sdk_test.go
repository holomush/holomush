// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk_test

import (
	"context"
	"testing"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/stretchr/testify/assert"
)

// Compile-time interface checks.
var (
	_ pluginsdk.Handler        = (*testHandler)(nil)
	_ pluginsdk.CommandHandler = (*testCommandHandler)(nil)
)

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

type testCommandHandler struct{}

func (h *testCommandHandler) HandleCommand(_ context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return pluginsdk.OK("handled: " + req.Command), nil
}

func TestCommandHandler_WithHandler(_ *testing.T) {
	// Verify a type can implement both Handler and CommandHandler
	var _ pluginsdk.Handler = (*testFullHandler)(nil)
	var _ pluginsdk.CommandHandler = (*testFullHandler)(nil)
}

type testFullHandler struct {
	testHandler
	testCommandHandler
}

func TestHandshakeConfig(t *testing.T) {
	assert.Equal(t, uint(1), pluginsdk.HandshakeConfig.ProtocolVersion, "HandshakeConfig protocol version should be 1")
	assert.Equal(t, "HOLOMUSH_PLUGIN", pluginsdk.HandshakeConfig.MagicCookieKey, "HandshakeConfig magic cookie key mismatch")
	assert.Equal(t, "holomush-v1", pluginsdk.HandshakeConfig.MagicCookieValue, "HandshakeConfig magic cookie value mismatch")
}
