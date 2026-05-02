// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/authguard/audit"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestSessionBridgeEmitterDelegatesToUnderlyingEmitter verifies that
// SessionBridgeEmitter converts an eventbus.PluginDecryptRecord to the
// audit-local type and delegates to the wrapped Emitter without error.
func TestSessionBridgeEmitterDelegatesToUnderlyingEmitter(t *testing.T) {
	pub := &stubPublisher{}
	emitter, err := audit.NewQueuedEmitter(pub, audit.WithGameID("test-game"))
	require.NoError(t, err)

	bridge, err := audit.NewSessionBridgeEmitter(emitter)
	require.NoError(t, err)

	rec := eventbus.PluginDecryptRecord{
		PluginName:       "mod-filter",
		PluginInstanceID: "inst-01",
		EventID:          idgen.New(),
		EventSubject:     eventbus.Subject("events.main.scene.sensitive"),
		EventType:        eventbus.Type("scene.pose"),
	}

	err = bridge.EmitPluginDecrypt(context.Background(), rec)
	require.NoError(t, err)

	// Drain goroutine should publish the record within a short deadline.
	require.Eventually(t, func() bool {
		return len(pub.snapshot()) > 0
	}, 2*time.Second, time.Millisecond, "drain goroutine did not publish within deadline")

	events := pub.snapshot()
	require.Len(t, events, 1)
	assert.Equal(t,
		eventbus.Subject("audit.test-game.plugin_decrypt.mod-filter"),
		events[0].Subject,
	)
}

// TestSessionBridgeEmitterPropagatesAuditQueueFullError verifies that when
// the underlying Emitter's queue is full, EmitPluginDecrypt returns
// AUDIT_QUEUE_FULL without panicking and the caller can detect the condition.
func TestSessionBridgeEmitterPropagatesAuditQueueFullError(t *testing.T) {
	// blockingPublisher prevents drain from consuming, so queue fills up.
	blocking := &blockingPublisher{block: make(chan struct{})}
	emitter, err := audit.NewQueuedEmitter(blocking, audit.WithCapacity(1))
	require.NoError(t, err)

	bridge, err := audit.NewSessionBridgeEmitter(emitter)
	require.NoError(t, err)

	rec := eventbus.PluginDecryptRecord{
		PluginName: "mod-filter",
		EventID:    ulid.Make(),
	}

	// Emit until queue full (capacity 1; drain is blocked).
	var lastErr error
	for i := 0; i < 5; i++ {
		if emitErr := bridge.EmitPluginDecrypt(context.Background(), rec); emitErr != nil {
			lastErr = emitErr
			break
		}
	}
	require.Error(t, lastErr, "expected AUDIT_QUEUE_FULL before 5 attempts")
	errutil.AssertErrorCode(t, lastErr, "AUDIT_QUEUE_FULL")

	// Unblock drain goroutine for clean shutdown.
	close(blocking.block)
}

func TestNewSessionBridgeEmitterRejectsNilEmitter(t *testing.T) {
	_, err := audit.NewSessionBridgeEmitter(nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_SESSION_BRIDGE_NIL_EMITTER")
}
