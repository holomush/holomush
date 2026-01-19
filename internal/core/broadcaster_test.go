// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"testing"
	"time"
)

func TestBroadcaster_Subscribe(t *testing.T) {
	bc := NewBroadcaster()

	ch := bc.Subscribe("location:test")
	if ch == nil {
		t.Fatal("Expected channel")
	}

	// Broadcast event
	event := Event{ID: NewULID(), Stream: "location:test", Type: EventTypeSay}
	bc.Broadcast(event)

	select {
	case received := <-ch:
		if received.ID != event.ID {
			t.Errorf("Event ID mismatch")
		}
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
		if ok {
			t.Error("Channel should be closed")
		}
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
		if received.ID != event.ID {
			t.Errorf("ch1: Event ID mismatch")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("ch1: Timeout")
	}

	select {
	case received := <-ch2:
		if received.ID != event.ID {
			t.Errorf("ch2: Event ID mismatch")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("ch2: Timeout")
	}
}
