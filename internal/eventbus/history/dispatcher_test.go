// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// TestDecodeAuthorizeAndDispatchIdentityCodecPasses asserts that an
// identity-codec event passes through the dispatcher unchanged: no
// AuthGuard gate, no decryption, payload preserved byte-for-byte.
func TestDecodeAuthorizeAndDispatchIdentityCodecPasses(t *testing.T) {
	envelope := &eventbusv1.Event{
		Id:      makeULIDBytes(t),
		Subject: "events.game1.world.location.loc-01ABC.test",
		Type:    "core-test:hello",
		Payload: []byte("hello, world"),
	}

	ev, metaOnly, err := decodeAuthorizeAndDispatch(
		context.Background(),
		envelope,
		codec.NameIdentity, // identity codec — no AuthGuard
		codec.KeyID(0),     // unused for identity
		uint32(0),          // unused for identity
		eventbus.SessionIdentity{}, // identity (not consulted on identity codec)
		nil,                        // guard (not consulted on identity codec)
		nil,                        // dekMgr (not consulted on identity codec)
		nil,                        // auditEm (not consulted on identity codec)
	)
	require.NoError(t, err)
	assert.False(t, metaOnly, "identity codec must not be metadata-only")
	assert.Equal(t, []byte("hello, world"), ev.Payload, "payload bytes preserved")
}

// makeULIDBytes returns a 16-byte ULID for test envelopes.
func makeULIDBytes(t *testing.T) []byte {
	t.Helper()
	return []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1}
}
