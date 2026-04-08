// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
)

func TestNewContextForDispatchAttachesEmptyEventSlice(t *testing.T) {
	ctx := audit.NewContextForDispatch(context.Background())
	events := audit.EventsFromContext(ctx)
	assert.Empty(t, events, "fresh dispatch context should have no events")
}

func TestAddEventToContextAppendsToAttachedSlice(t *testing.T) {
	ctx := audit.NewContextForDispatch(context.Background())
	audit.AddEventToContext(ctx, audit.Event{
		ID:        "test-event",
		Source:    audit.SourcePlugin,
		Component: "test-plugin",
		Effect:    types.EffectDeny,
	})

	events := audit.EventsFromContext(ctx)
	assert.Len(t, events, 1)
	assert.Equal(t, "test-event", events[0].ID)
}

func TestAddEventToContextIsNoOpWhenNoSliceAttached(t *testing.T) {
	// Baseline context with no dispatch attachment.
	ctx := context.Background()
	audit.AddEventToContext(ctx, audit.Event{ID: "orphan"})

	events := audit.EventsFromContext(ctx)
	assert.Nil(t, events, "plain context should have no attached slice")
}

func TestEventsFromContextDrainsTheSlice(t *testing.T) {
	ctx := audit.NewContextForDispatch(context.Background())
	audit.AddEventToContext(ctx, audit.Event{ID: "e1"})
	audit.AddEventToContext(ctx, audit.Event{ID: "e2"})

	first := audit.EventsFromContext(ctx)
	assert.Len(t, first, 2)

	second := audit.EventsFromContext(ctx)
	assert.Empty(t, second, "second call should return empty — drain is destructive")
}

func TestAddEventToContextIsSafeForMultipleCalls(t *testing.T) {
	ctx := audit.NewContextForDispatch(context.Background())
	for i := 0; i < 10; i++ {
		audit.AddEventToContext(ctx, audit.Event{ID: "bulk"})
	}
	events := audit.EventsFromContext(ctx)
	assert.Len(t, events, 10)
}
