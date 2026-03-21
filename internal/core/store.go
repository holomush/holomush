// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"errors"

	"github.com/oklog/ulid/v2"
)

// ErrStreamEmpty is returned when a stream has no events.
var ErrStreamEmpty = errors.New("stream is empty")

// EventStore persists and retrieves events.
type EventStore interface {
	// Append persists an event to a stream.
	Append(ctx context.Context, event Event) error

	// Replay returns up to limit events from a stream, starting after afterID.
	// If afterID is zero ULID, starts from beginning.
	Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]Event, error)

	// LastEventID returns the most recent event ID for a stream.
	LastEventID(ctx context.Context, stream string) (ulid.ULID, error)

	// Subscribe starts listening for new events on the given stream.
	// Returns a channel of event IDs and an error channel.
	// The caller should use Replay() to fetch full events by ID.
	// Channels are closed when context is cancelled.
	Subscribe(ctx context.Context, stream string) (eventCh <-chan ulid.ULID, errCh <-chan error, err error)
}
