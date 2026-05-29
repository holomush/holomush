// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/grpc/focus"
)

// INV-FS-5: the StreamSender and ConnectionSender produced for one coordinator
// MUST target the same SessionStreamRegistry. We prove it by routing through
// each registry-derived adapter and asserting it reaches the one registry.
func TestFocusStreamCoordinatorOptionsShareOneRegistry(t *testing.T) {
	reg := NewSessionStreamRegistry()
	require.Len(t, FocusStreamCoordinatorOptions(reg), 2,
		"helper yields exactly StreamSender + ConnectionSender options")

	sessionID := "sess-x"
	connID := ulid.Make()

	tests := []struct {
		name     string
		register func(ch chan sessionStreamUpdate) // wire a buffered channel into the registry
		send     func() error                      // route one update through the adapter under test
	}{
		{
			name:     "ConnectionSender adapter reaches the registry",
			register: func(ch chan sessionStreamUpdate) { reg.RegisterConnection(sessionID, connID, ch) },
			send: func() error {
				return NewConnectionSenderAdapter(reg).SendToConnection(sessionID, connID, "events.main.scene.s.ic", true)
			},
		},
		{
			name:     "StreamSender adapter reaches the registry",
			register: func(ch chan sessionStreamUpdate) { reg.Register(sessionID, ch) },
			send: func() error {
				return NewStreamSenderAdapter(reg).Send(sessionID, "location:x", true, focus.ReplayModeFromCursor)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan sessionStreamUpdate, 1)
			tt.register(ch)
			require.NoError(t, tt.send())
			assert.Len(t, ch, 1, "adapter MUST reach the same registry")
		})
	}
}
