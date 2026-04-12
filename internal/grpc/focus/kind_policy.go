// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package focus implements the FocusCoordinator — the sole authoritative
// mutator of a session's focused-context state. It encapsulates transition
// semantics (join, leave, present, restore) and dispatches per-kind replay
// policy to FocusKindPolicy implementations.
//
// ReplayMode is defined here (not in the parent internal/grpc package)
// because the dependency graph is grpc → focus. Defining the type in the
// lower-level package that produces replay decisions avoids duplication
// and lets grpc use focus.ReplayMode directly.
package focus

import (
	"fmt"
	"time"

	"github.com/holomush/holomush/internal/session"
)

// ReplayMode controls how the live loop replays events when processing a
// stream addition. The coordinator and kind policies produce ReplayMode
// values; the live loop in internal/grpc consumes them.
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

// StreamWithMode pairs a stream name with its replay mode and optional
// mode-specific parameters.
type StreamWithMode struct {
	Stream    string
	Mode      ReplayMode
	TailCount int       // for ReplayModeBoundedTail
	NotBefore time.Time // for ReplayModeBoundedTail
}

// FocusPolicyContext carries the preference-resolved inputs a kind policy
// needs. Constructed by the coordinator before dispatching to the policy,
// so the policy remains stateless and test-pure.
type FocusPolicyContext struct {
	SessionID string
	Target    session.FocusKey

	// SceneFocusReplayTail is the number of most recent IC contributions
	// replayed on focus-switch into a scene. Resolved by the coordinator
	// from the settings.Chain before calling the policy.
	SceneFocusReplayTail int
}

// FocusKindPolicy encapsulates the per-kind replay policy for a focused
// context. Implementations MUST be stateless (invariant I-9). Instances
// are registered in the coordinator constructor keyed by FocusKind.
//
// Implementations are pure functions: they take inputs from FocusPolicyContext
// and return decisions as StreamWithMode values. No side effects, no store
// access, no registry access.
type FocusKindPolicy interface {
	// Kind returns the FocusKind this policy handles.
	Kind() session.FocusKind

	// StreamsFor derives the stream names a membership of this kind implies
	// for a given target. Return order matters — the first stream is the
	// "primary" used for PresentingFocus default resolution.
	StreamsFor(target session.FocusKey) []string

	// OnJoin returns the per-stream replay policy to apply when the
	// membership is first created.
	OnJoin(pctx FocusPolicyContext) ([]StreamWithMode, error)

	// OnRestore returns the per-stream replay policy to apply when the
	// membership is restored on reconnect.
	OnRestore(pctx FocusPolicyContext) ([]StreamWithMode, error)
}

// NullPolicy is a bootstrapping FocusKindPolicy that returns empty streams
// for all operations. It allows the coordinator to construct and pass tests
// before real kind policies (ScenePolicy) are registered.
type NullPolicy struct {
	kind session.FocusKind
}

// NewNullPolicy creates a NullPolicy for the given kind.
func NewNullPolicy(kind session.FocusKind) *NullPolicy {
	return &NullPolicy{kind: kind}
}

// Kind returns the FocusKind this null policy handles.
func (p *NullPolicy) Kind() session.FocusKind { return p.kind }

// StreamsFor returns nil — no streams for null policy.
func (p *NullPolicy) StreamsFor(_ session.FocusKey) []string { return nil }

// OnJoin returns nil — no replay for null policy.
func (p *NullPolicy) OnJoin(_ FocusPolicyContext) ([]StreamWithMode, error) { return nil, nil }

// OnRestore returns nil — no replay for null policy.
func (p *NullPolicy) OnRestore(_ FocusPolicyContext) ([]StreamWithMode, error) { return nil, nil }
