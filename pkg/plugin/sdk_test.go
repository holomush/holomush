// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugin_test

import (
	"context"
	"testing"

	"github.com/holomush/holomush/pkg/plugin"
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
		if r := recover(); r == nil {
			t.Error("Serve should panic with nil Handler")
		}
	}()

	plugin.Serve(&plugin.ServeConfig{Handler: nil})
}

func TestServeConfig_ConfigRequired(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Serve should panic with nil config")
		}
	}()

	plugin.Serve(nil)
}

func TestHandshakeConfig(t *testing.T) {
	if plugin.HandshakeConfig.ProtocolVersion != 1 {
		t.Error("HandshakeConfig protocol version should be 1")
	}
	if plugin.HandshakeConfig.MagicCookieKey != "HOLOMUSH_PLUGIN" {
		t.Error("HandshakeConfig magic cookie key mismatch")
	}
	if plugin.HandshakeConfig.MagicCookieValue != "holomush-v1" {
		t.Error("HandshakeConfig magic cookie value mismatch")
	}
}
