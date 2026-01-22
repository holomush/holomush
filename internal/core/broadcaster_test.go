// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcaster_Subscribe(t *testing.T) {
	bc := NewBroadcaster()

	ch := bc.Subscribe("location:test")
	require.NotNil(t, ch, "Expected channel")

	// Broadcast event
	event := Event{ID: NewULID(), Stream: "location:test", Type: EventTypeSay}
	bc.Broadcast(event)

	select {
	case received := <-ch:
		assert.Equal(t, event.ID, received.ID)
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for event")
	}
}

func TestBroadcaster_Unsubscribe(t *testing.T) {
	bc := NewBroadcaster()

	ch := bc.Subscribe("location:test")
	bc.Unsubscribe("location:test", ch)

	// Channel should be closed
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "Channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Error("Channel should be closed immediately")
	}
}

func TestBroadcaster_MultipleSubscribers(t *testing.T) {
	bc := NewBroadcaster()

	ch1 := bc.Subscribe("location:test")
	ch2 := bc.Subscribe("location:test")

	event := Event{ID: NewULID(), Stream: "location:test", Type: EventTypeSay}
	bc.Broadcast(event)

	// Both should receive
	select {
	case received := <-ch1:
		assert.Equal(t, event.ID, received.ID, "ch1: Event ID mismatch")
	case <-time.After(100 * time.Millisecond):
		t.Error("ch1: Timeout")
	}

	select {
	case received := <-ch2:
		assert.Equal(t, event.ID, received.ID, "ch2: Event ID mismatch")
	case <-time.After(100 * time.Millisecond):
		t.Error("ch2: Timeout")
	}
}

// TestBroadcaster_WithAccessControl documents expected behavior for Phase 3.4.
// This test will fail until Broadcaster integration is complete.
// See: docs/specs/2026-01-21-access-control-design.md (Event System Integration)
//
// Expected API changes:
//   - NewBroadcasterWithAccessControl(ac AccessControl) *Broadcaster
//   - SubscribeWithSubject(stream, subject string) <-chan Event
//   - Broadcast(ctx context.Context, event Event) - checks read permission
func TestBroadcaster_WithAccessControl(t *testing.T) {
	t.Skip("Pending: Broadcaster access control integration (Phase 3.4)")

	// When implemented, this test should verify:
	// 1. Broadcaster accepts optional AccessControl
	// 2. Subscribers with subject can be added via SubscribeWithSubject
	// 3. Events are filtered at delivery based on read permission
	// 4. Unauthorized subscribers are silently skipped
	// 5. Backward compatible (existing Subscribe API still works)
	//
	// Example test logic:
	//   ac := access.NewStaticAccessControl(nil, nil)
	//   ac.AssignRole("char:allowed", "admin")  // admin can read everything
	//   b := NewBroadcasterWithAccessControl(ac)
	//   allowedCh := b.SubscribeWithSubject("room:1", "char:allowed")
	//   deniedCh := b.SubscribeWithSubject("room:1", "char:denied")
	//   b.Broadcast(ctx, event)
	//   // allowedCh receives event, deniedCh does not
}
