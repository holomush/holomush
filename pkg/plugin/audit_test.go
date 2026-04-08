// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func TestAuditRecorderDenyAccumulatesHintOnContext(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	pluginsdk.Audit(ctx).Deny("not_member", "player not in channel members",
		pluginsdk.AuditAttrs{"channel.type": "public"})

	hints := pluginsdk.HarvestAuditHints(ctx)
	require.Len(t, hints, 1)
	assert.Equal(t, "not_member", hints[0].ID)
	assert.Equal(t, pluginsdk.AuditEffectDeny, hints[0].Effect)
	assert.Equal(t, "player not in channel members", hints[0].Message)
	assert.Equal(t, "public", hints[0].Attributes["channel.type"])
}

func TestAuditRecorderAllowAccumulatesHintWithCorrectEffect(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	pluginsdk.Audit(ctx).Allow("speak_ok", "message delivered", nil)

	hints := pluginsdk.HarvestAuditHints(ctx)
	require.Len(t, hints, 1)
	assert.Equal(t, pluginsdk.AuditEffectAllow, hints[0].Effect)
}

func TestAuditRecorderIsNoOpWhenNoHandlerContextAttached(t *testing.T) {
	// Plain context — no handler attachment.
	ctx := context.Background()

	// Should not panic, should silently drop.
	pluginsdk.Audit(ctx).Deny("orphan", "no context", nil)

	hints := pluginsdk.HarvestAuditHints(ctx)
	assert.Nil(t, hints)
}

func TestHarvestAuditHintsDrainsTheSlice(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())
	pluginsdk.Audit(ctx).Deny("e1", "", nil)
	pluginsdk.Audit(ctx).Deny("e2", "", nil)

	first := pluginsdk.HarvestAuditHints(ctx)
	assert.Len(t, first, 2)

	second := pluginsdk.HarvestAuditHints(ctx)
	assert.Empty(t, second, "harvest is destructive")
}

func TestAuditRecorderDenyCopiesAttributesNotReferenced(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	attrs := pluginsdk.AuditAttrs{"key": "value"}
	pluginsdk.Audit(ctx).Deny("copy_test", "", attrs)

	// Mutate the caller's map — the recorded hint should not change.
	attrs["key"] = "mutated"

	hints := pluginsdk.HarvestAuditHints(ctx)
	require.Len(t, hints, 1)
	assert.Equal(t, "value", hints[0].Attributes["key"],
		"recorder must copy the attribute map")
}

// T27a — SHOULD boundary: empty ID is silently dropped and logged.
// Rationale: the proto AuditDecisionHint has min_len=1 on the ID field.
// If the SDK accepted empty IDs, they would accumulate on the context
// and then fail proto marshaling at the response-serialization step,
// silently dropping the hint without a clear diagnostic. Fail fast at
// the SDK layer by dropping + logging, so plugin authors see the
// problem during development rather than in production.
func TestAuditRecorderDenyWithEmptyIDIsSilentlyDroppedAndLogged(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	pluginsdk.Audit(ctx).Deny("", "message with no id", nil)

	hints := pluginsdk.HarvestAuditHints(ctx)
	assert.Empty(t, hints,
		"recorder must silently drop hints with empty ID (proto min_len=1 would fail marshal)")
}

// T27a (mirror) — symmetric Allow empty-ID drop. The empty-ID guard lives
// on the shared record() helper, so Allow inherits the same behavior. This
// test makes the symmetry explicit so a future regression that special-cases
// only Deny would be caught immediately.
func TestAuditRecorderAllowWithEmptyIDIsSilentlyDroppedAndLogged(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	pluginsdk.Audit(ctx).Allow("", "allow with no id", nil)

	hints := pluginsdk.HarvestAuditHints(ctx)
	assert.Empty(t, hints,
		"recorder must silently drop Allow hints with empty ID, mirroring the Deny path")
}
