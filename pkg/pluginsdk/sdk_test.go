// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk_test

import (
	"context"
	"testing"

	"github.com/holomush/holomush/pkg/pluginsdk"
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
		if r := recover(); r == nil {
			t.Error("Serve should panic with nil Handler")
		}
	}()

	pluginsdk.Serve(&pluginsdk.ServeConfig{Handler: nil})
}

func TestServeConfig_ConfigRequired(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Serve should panic with nil config")
		}
	}()

	pluginsdk.Serve(nil)
}

func TestHandshakeConfig(t *testing.T) {
	if pluginsdk.HandshakeConfig.ProtocolVersion != 1 {
		t.Error("HandshakeConfig protocol version should be 1")
	}
	if pluginsdk.HandshakeConfig.MagicCookieKey != "HOLOMUSH_PLUGIN" {
		t.Error("HandshakeConfig magic cookie key mismatch")
	}
	if pluginsdk.HandshakeConfig.MagicCookieValue != "holomush-v1" {
		t.Error("HandshakeConfig magic cookie value mismatch")
	}
}
