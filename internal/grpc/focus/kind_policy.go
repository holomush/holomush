// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package focus implements the Coordinator — the sole authoritative
// mutator of a session's focused-context state. It encapsulates transition
// semantics (join, leave, present, restore) and dispatches per-kind replay
// policy to KindPolicy implementations.
package focus

import (
	"time"

	"github.com/holomush/holomush/internal/session"
)

// ReplayMode is an alias for session.ReplayMode. Defined here for
// backward compatibility with existing focus package consumers.
// New code SHOULD import session.ReplayMode directly.
type ReplayMode = session.ReplayMode

// Re-export constants so existing focus.ReplayModeXxx references compile.
const (
	ReplayModeFromCursor  = session.ReplayModeFromCursor
	ReplayModeBoundedTail = session.ReplayModeBoundedTail
	ReplayModeLiveOnly    = session.ReplayModeLiveOnly
)

// StreamWithMode pairs a stream name with its replay mode and optional
// mode-specific parameters.
type StreamWithMode struct {
	Stream    string
	Mode      ReplayMode
	TailCount int       // for ReplayModeBoundedTail
	NotBefore time.Time // for ReplayModeBoundedTail
}

// PolicyContext carries the preference-resolved inputs a kind policy
// needs. Constructed by the coordinator before dispatching to the policy,
// so the policy remains stateless and test-pure.
type PolicyContext struct {
	SessionID string
	Target    session.FocusKey

	// SceneFocusReplayTail is the number of most recent IC contributions
	// replayed on focus-switch into a scene. Resolved by the coordinator
	// from the settings.Chain before calling the policy.
	SceneFocusReplayTail int
}

// KindPolicy encapsulates the per-kind replay policy for a focused
// context. Implementations MUST be stateless (invariant I-9). Instances
// are registered in the coordinator constructor keyed by FocusKind.
//
// Implementations are pure functions: they take inputs from PolicyContext
// and return decisions as StreamWithMode values. No side effects, no store
// access, no registry access.
type KindPolicy interface {
	// Kind returns the FocusKind this policy handles.
	Kind() session.FocusKind

	// StreamsFor derives the stream names a membership of this kind implies
	// for a given target. Return order matters — the first stream is the
	// "primary" used for PresentingFocus default resolution.
	StreamsFor(target session.FocusKey) []string

	// OnJoin returns the per-stream replay policy to apply when the
	// membership is first created.
	OnJoin(pctx PolicyContext) ([]StreamWithMode, error)

	// OnRestore returns the per-stream replay policy to apply when the
	// membership is restored on reconnect.
	OnRestore(pctx PolicyContext) ([]StreamWithMode, error)
}

// NullPolicy is a bootstrapping KindPolicy that returns empty streams
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
func (p *NullPolicy) OnJoin(_ PolicyContext) ([]StreamWithMode, error) { return nil, nil }

// OnRestore returns nil — no replay for null policy.
func (p *NullPolicy) OnRestore(_ PolicyContext) ([]StreamWithMode, error) { return nil, nil }
