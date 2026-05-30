// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package scenepolicy implements the KindPolicy for scene-type focused
// contexts. ScenePolicy derives two streams per scene: an IC (in-character)
// stream and an OOC (out-of-character) stream.
package scenepolicy

import (
	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
)

// ScenePolicy implements focus.KindPolicy for FocusKindScene.
// It is stateless (I-9) and pure — no store access, no side effects.
type ScenePolicy struct{}

// New creates a ScenePolicy.
func New() *ScenePolicy { return &ScenePolicy{} }

// Kind returns FocusKindScene.
func (p *ScenePolicy) Kind() session.FocusKind { return session.FocusKindScene }

// StreamsFor returns the IC and OOC stream names for a scene target.
// IC is first (primary for PresentingFocus default resolution).
func (p *ScenePolicy) StreamsFor(target session.FocusKey) []string {
	id := target.TargetID.String()
	return []string{
		"scene." + id + ".ic",
		"scene." + id + ".ooc",
	}
}

// OnJoin returns BoundedTail(N) for IC and LiveOnly for OOC.
func (p *ScenePolicy) OnJoin(pctx focus.PolicyContext) ([]focus.StreamWithMode, error) {
	streams := p.StreamsFor(pctx.Target)
	return []focus.StreamWithMode{
		{Stream: streams[0], Mode: focus.ReplayModeBoundedTail, TailCount: pctx.SceneFocusReplayTail},
		{Stream: streams[1], Mode: focus.ReplayModeLiveOnly},
	}, nil
}

// OnRestore returns FromCursor for both IC and OOC (cursor-faithful replay).
func (p *ScenePolicy) OnRestore(pctx focus.PolicyContext) ([]focus.StreamWithMode, error) {
	streams := p.StreamsFor(pctx.Target)
	return []focus.StreamWithMode{
		{Stream: streams[0], Mode: focus.ReplayModeFromCursor},
		{Stream: streams[1], Mode: focus.ReplayModeFromCursor},
	}, nil
}
