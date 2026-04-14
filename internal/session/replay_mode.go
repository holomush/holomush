// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import "fmt"

// ReplayMode controls how the live loop replays events when processing a
// stream addition. It determines cursor-initialization semantics:
// FROM_CURSOR reads the existing watermark, LIVE_ONLY advances to tail,
// BOUNDED_TAIL sets a new baseline from the last N events.
//
// Produced by focus.KindPolicy implementations and the FocusCoordinator.
// Consumed by the Subscribe live loop in internal/grpc.
type ReplayMode int

const (
	// ReplayModeFromCursor replays from the stored per-stream cursor in
	// session.Info.EventCursors, or from ULID zero if no cursor is set.
	ReplayModeFromCursor ReplayMode = iota

	// ReplayModeBoundedTail replays the most recent TailCount events on
	// the stream (optionally bounded by NotBefore), then advances the
	// cursor to the tail. Used by scene focus-switch IC catch-up.
	ReplayModeBoundedTail

	// ReplayModeLiveOnly advances the cursor to the current stream tail
	// without replaying anything. Used by channels for mid-session joins.
	ReplayModeLiveOnly
)

// String returns a human-readable name for the replay mode.
func (m ReplayMode) String() string {
	switch m {
	case ReplayModeFromCursor:
		return "from_cursor"
	case ReplayModeBoundedTail:
		return "bounded_tail"
	case ReplayModeLiveOnly:
		return "live_only"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}
