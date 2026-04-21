// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"errors"
)

// ErrStreamEmpty is returned when a stream has no events.
var ErrStreamEmpty = errors.New("stream is empty")

// EventAppender persists a single event to a stream.
// This is the minimal write interface; F7 removed the legacy EventStore
// (Append/Replay/Subscribe/SubscribeSession/UpdateCursors) that was backed
// by the PG events table. All new writes go through the JetStream event bus;
// the core Engine and command handlers only need Append.
type EventAppender interface {
	Append(ctx context.Context, event Event) error
}
