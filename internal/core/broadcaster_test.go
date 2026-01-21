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
