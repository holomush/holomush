// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/authguard/audit"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/pkg/errutil"
)

// stubPublisher captures published events for assertion.
type stubPublisher struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (s *stubPublisher) Publish(_ context.Context, e eventbus.Event) error {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
	return nil
}

func (s *stubPublisher) snapshot() []eventbus.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]eventbus.Event, len(s.events))
	copy(out, s.events)
	return out
}

// blockingPublisher blocks until its block channel is closed.
type blockingPublisher struct{ block chan struct{} }

func (b *blockingPublisher) Publish(_ context.Context, _ eventbus.Event) error {
	<-b.block
	return nil
}

func TestNewQueuedEmitterRejectsNilPublisher(t *testing.T) {
	_, err := audit.NewQueuedEmitter(nil)
	errutil.AssertErrorCode(t, err, "AUDIT_EMITTER_DEPENDENCY_NIL")
}

func TestEmitPluginDecryptEnqueuesAndDrainPublishesToConfiguredGameID(t *testing.T) {
	pub := &stubPublisher{}
	emitter, err := audit.NewQueuedEmitter(pub, audit.WithGameID("test-game"))
	require.NoError(t, err)

	rec := audit.PluginDecryptRecord{
		PluginName: "mod-filter",
		EventID:    idgen.New(),
	}
	require.NoError(t, emitter.EmitPluginDecrypt(t.Context(), rec))

	// Wait for the drain goroutine to publish.
	require.Eventually(t, func() bool {
		return len(pub.snapshot()) > 0
	}, 2*time.Second, time.Millisecond, "drain goroutine did not publish within deadline")

	events := pub.snapshot()
	require.Len(t, events, 1)
	assert.Equal(t, eventbus.Subject("audit.test-game.plugin_decrypt.mod-filter"), events[0].Subject)
	assert.Equal(t, eventbus.Type("audit:plugin_decrypt"), events[0].Type)
	assert.False(t, events[0].Sensitive)
	assert.Equal(t, eventbus.ActorKindSystem, events[0].Actor.Kind)
}

func TestShouldThrottleReturnsFalseForUnknownPlugin(t *testing.T) {
	pub := &stubPublisher{}
	emitter, err := audit.NewQueuedEmitter(pub)
	require.NoError(t, err)
	assert.False(t, emitter.ShouldThrottle("never-seen-plugin"))
}

func TestEmitPluginDecryptReturnsAuditQueueFullWhenCapacityReached(t *testing.T) {
	// blockingPublisher prevents drain from consuming entries, ensuring queue fills.
	blockingPub := &blockingPublisher{block: make(chan struct{})}
	emitter, err := audit.NewQueuedEmitter(blockingPub, audit.WithCapacity(2))
	require.NoError(t, err)

	rec := audit.PluginDecryptRecord{PluginName: "mod-filter"}
	// Emit until we get AUDIT_QUEUE_FULL (capacity 2; drain is blocked).
	var lastErr error
	for i := 0; i < 5; i++ {
		if err := emitter.EmitPluginDecrypt(t.Context(), rec); err != nil {
			lastErr = err
			break
		}
	}
	errutil.AssertErrorCode(t, lastErr, "AUDIT_QUEUE_FULL")
	assert.True(t, emitter.ShouldThrottle("mod-filter"))

	// Unblock publisher to allow drain goroutine to exit cleanly.
	close(blockingPub.block)
}

func TestShutdownDrainsWithinDeadline(t *testing.T) {
	pub := &stubPublisher{}
	emitter, err := audit.NewQueuedEmitter(pub, audit.WithGameID("test-game"))
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		require.NoError(t, emitter.EmitPluginDecrypt(t.Context(), audit.PluginDecryptRecord{
			PluginName: "mod-filter",
		}))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, emitter.Shutdown(ctx))
}
