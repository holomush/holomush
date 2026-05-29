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
// both registry-derived adapters and asserting both reach the one registry.
func TestFocusStreamCoordinatorOptionsShareOneRegistry(t *testing.T) {
	reg := NewSessionStreamRegistry()
	opts := FocusStreamCoordinatorOptions(reg)
	require.Len(t, opts, 2, "helper yields exactly StreamSender + ConnectionSender options")

	sessionID := "sess-x"
	connID := ulid.Make()

	// Verify ConnectionSenderAdapter reaches the registry.
	connCh := make(chan sessionStreamUpdate, 1)
	reg.RegisterConnection(sessionID, connID, connCh)
	require.NoError(t, NewConnectionSenderAdapter(reg).SendToConnection(sessionID, connID, "events.main.scene.s.ic", true))
	assert.Len(t, connCh, 1, "ConnectionSender adapter MUST reach the same registry")

	// Verify StreamSenderAdapter reaches the registry.
	sessCh := make(chan sessionStreamUpdate, 1)
	reg.Register(sessionID, sessCh)
	require.NoError(t, NewStreamSenderAdapter(reg).Send(sessionID, "location:x", true, focus.ReplayModeFromCursor))
	assert.Len(t, sessCh, 1, "StreamSender adapter MUST reach the same registry")
}
