// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// actorCapturingHost is a minimal Host implementation that captures the ctx
// passed into DeliverEvent so tests can assert on its actor-metadata channel.
//
// It lives in package plugins (internal test file) so we can drive the
// unexported Subscriber.deliverAsync directly. We can't use the generated
// mocks package here: mocks imports internal/plugin, which would create a
// self-import cycle when this internal test is compiled.
type actorCapturingHost struct {
	mu          sync.Mutex
	capturedCtx context.Context
	called      bool
}

func (h *actorCapturingHost) Load(context.Context, *Manifest, string) error { return nil }
func (h *actorCapturingHost) Unload(context.Context, string) error          { return nil }
func (h *actorCapturingHost) Plugins() []string                             { return []string{"test-plugin"} }

func (h *actorCapturingHost) PluginEmitRegistry(string) ([]string, bool) { return nil, false }
func (h *actorCapturingHost) Close(context.Context) error                { return nil }

func (h *actorCapturingHost) DeliverCommand(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return nil, nil
}

func (h *actorCapturingHost) DeliverEvent(ctx context.Context, _ string, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.capturedCtx = ctx
	h.called = true
	return nil, nil
}

func (h *actorCapturingHost) QuerySessionStreams(context.Context, string, SessionStreamsRequest) ([]string, error) {
	return nil, nil
}

// noopEmitter discards emitted events; deliverAsync's emit loop is fine with
// an empty emit slice from DeliverEvent above, but we need a concrete
// EventEmitter to construct a Subscriber.
type noopEmitter struct{}

func (noopEmitter) EmitPluginEvent(context.Context, string, pluginsdk.EmitEvent) error {
	return nil
}

// TestSubscriberStampsCharacterActorBeforeDeliverEvent asserts that the
// production Subscriber populates core.ActorFromContext(ctx) BEFORE calling
// Host.DeliverEvent, so the host's outgoing-metadata injection and token
// issuance see the upstream actor.
//
// This test invokes the production Subscriber.deliverAsync directly (this
// file is in package plugins, the same package as subscriber.go, so it has
// access to unexported methods).
func TestSubscriberStampsCharacterActorBeforeDeliverEvent(t *testing.T) {
	t.Parallel()

	host := &actorCapturingHost{}
	sub := NewSubscriber(host, noopEmitter{})

	charID := ulid.MustParse("01HX0000000000000000000000")
	event := pluginsdk.Event{
		ID:        ulid.MustParse("01HEVENT00000000000000000C").String(),
		Stream:    "location:01HLOC0000000000000000000",
		Type:      "say",
		Timestamp: 0,
		ActorKind: pluginsdk.ActorCharacter,
		ActorID:   charID.String(),
		Payload:   `{"message":"hi"}`,
	}

	sub.deliverAsync(context.Background(), "test-plugin", event)
	sub.Stop() // waits on s.wg

	host.mu.Lock()
	defer host.mu.Unlock()
	require.True(t, host.called, "DeliverEvent must have been invoked")
	require.NotNil(t, host.capturedCtx, "captured ctx must not be nil")
	got, ok := core.ActorFromContext(host.capturedCtx)
	require.True(t, ok, "core.ActorFromContext MUST return ok=true at DeliverEvent boundary")
	assert.Equal(t, core.ActorCharacter, got.Kind)
	assert.Equal(t, charID.String(), got.ID)
}

// TestSubscriberStampsSystemActorBeforeDeliverEvent — same shape, ActorSystem case.
func TestSubscriberStampsSystemActorBeforeDeliverEvent(t *testing.T) {
	t.Parallel()

	host := &actorCapturingHost{}
	sub := NewSubscriber(host, noopEmitter{})

	event := pluginsdk.Event{
		ID:        ulid.MustParse("01HEVENT00000000000000000S").String(),
		Stream:    "system:health",
		Type:      "tick",
		ActorKind: pluginsdk.ActorSystem,
		ActorID:   core.ActorSystemID,
	}

	sub.deliverAsync(context.Background(), "test-plugin", event)
	sub.Stop()

	host.mu.Lock()
	defer host.mu.Unlock()
	require.True(t, host.called, "DeliverEvent must have been invoked")
	require.NotNil(t, host.capturedCtx)
	got, ok := core.ActorFromContext(host.capturedCtx)
	require.True(t, ok, "core.ActorFromContext MUST return ok=true at DeliverEvent boundary")
	assert.Equal(t, core.ActorSystem, got.Kind)
	assert.Equal(t, core.ActorSystemID, got.ID)
}
