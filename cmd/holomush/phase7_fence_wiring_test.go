// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// fakeRenderingInnerPublisher captures Publish calls for assertion.
type fakeRenderingInnerPublisher struct {
	published []eventbus.Event
}

func (f *fakeRenderingInnerPublisher) Publish(_ context.Context, ev eventbus.Event) error {
	f.published = append(f.published, ev)
	return nil
}

// TestViolationEmitterReachesRenderingPublisher is the regression guard for
// the bead-1r0v.5 crypto-review BLOCKING finding: the fence's audit-emit
// path goes through RenderingPublisher, which rejects unregistered event
// types with EMIT_UNKNOWN_VERB. Without `system:plugin_integrity_violation`
// in the builtin verb registry, every fence refusal silently drops the
// documented operator audit signal — INV-P7-7's audit-emit half is dead.
//
// This test wires a production-shaped emitter (newViolationEmitter) over a
// REAL RenderingPublisher backed by core.BootstrapVerbRegistry, then
// invokes EmitViolation. Pre-fix: assert fails with EMIT_UNKNOWN_VERB.
// Post-fix: publishes cleanly and the fake inner publisher captures the
// fully-stamped event.
func TestViolationEmitterReachesRenderingPublisher(t *testing.T) {
	t.Parallel()

	registry, err := core.BootstrapVerbRegistry("test-1r0v.5")
	require.NoError(t, err)

	inner := &fakeRenderingInnerPublisher{}
	rp := eventbus.NewRenderingPublisher(inner, registry)

	emitter := newViolationEmitter(rp, "test-game")

	rowID := ulid.Make()
	rowIDBytes := rowID.Bytes()
	row := &pluginauditpb.AuditRow{
		Id:      rowIDBytes[:],
		Subject: "events.test-game.scene.01ABC.ic",
		Type:    "test-plugin:secret",
		Codec:   "identity",
	}

	err = emitter.EmitViolation(
		context.Background(),
		"test-plugin",
		row,
		"sensitivity:always",
		"AUDIT_ROW_DOWNGRADE_DETECTED",
	)
	require.NoError(t, err,
		"INV-P7-7 audit emit MUST succeed through the production RenderingPublisher; "+
			"if this fails with EMIT_UNKNOWN_VERB, the system:plugin_integrity_violation "+
			"verb is missing from internal/core/builtins.go::registerBuiltinTypes")

	require.Len(t, inner.published, 1, "exactly one violation event MUST reach the inner publisher")
	got := inner.published[0]
	assert.Equal(t, "system:plugin_integrity_violation", string(got.Type))
	assert.Equal(t, "events.test-game.system.plugin_integrity_violation", string(got.Subject))
	require.NotNil(t, got.Rendering, "RenderingPublisher MUST stamp event.Rendering on the violation event")
	assert.Equal(t, "system", got.Rendering.Category)
	assert.Equal(t, "audit", got.Rendering.Format)
	assert.Equal(t, eventbus.EventChannelAuditOnly, got.Rendering.DisplayTarget)
}
