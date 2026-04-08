// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import "context"

// contextKey is the unexported type used as the map key for storing the
// dispatch event slice on a context. Using an unexported named type
// prevents collisions with other packages that might use context.WithValue.
type contextKey struct{}

// eventsKey is the sentinel value looked up on contexts carrying a
// dispatch-scoped event slice.
var eventsKey = contextKey{}

// NewContextForDispatch returns a derived context with an empty event
// slice attached. Call this at the start of any operation whose emitted
// events should be flushed at completion (typically command dispatch).
//
// The returned context can be passed across goroutine boundaries, but
// the slice itself is NOT concurrency-safe. If a caller emits events
// from multiple goroutines, it MUST serialize emission externally.
func NewContextForDispatch(ctx context.Context) context.Context {
	events := &[]Event{}
	return context.WithValue(ctx, eventsKey, events)
}

// AddEventToContext appends an event to the slice attached to ctx.
// If no slice is attached (ctx was not derived from NewContextForDispatch),
// the call is a silent no-op. This is intentional: code paths that may
// run inside or outside a dispatch context can emit events unconditionally
// without branching on whether accumulation is active.
func AddEventToContext(ctx context.Context, event Event) {
	if events, ok := ctx.Value(eventsKey).(*[]Event); ok {
		*events = append(*events, event)
	}
}

// EventsFromContext returns and clears the event slice attached to ctx.
// Returns nil if no slice was attached. The clear is destructive — a
// subsequent call on the same context returns an empty slice, not the
// same values again. This prevents double-flush during partial failures.
func EventsFromContext(ctx context.Context) []Event {
	events, ok := ctx.Value(eventsKey).(*[]Event)
	if !ok {
		return nil
	}
	drained := *events
	*events = nil
	return drained
}
