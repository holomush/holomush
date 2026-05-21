// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// ptr32 is a test helper for the optional PosesSinceLast field.
func ptr32(v uint32) *uint32 { return &v }

// makeEntry builds a PoseOrderEntry for table-driven renderer tests.
func makeEntry(charID, charName string, eligible bool, posesSinceLast *uint32) *scenev1.PoseOrderEntry {
	return &scenev1.PoseOrderEntry{
		CharacterId:    charID,
		CharacterName:  charName,
		Eligible:       eligible,
		PosesSinceLast: posesSinceLast,
	}
}

// TestRenderPoseOrder_Empty verifies the "(no participants)" branch.
func TestRenderPoseOrder_Empty(t *testing.T) {
	t.Parallel()
	resp := &scenev1.GetPoseOrderResponse{
		Mode:           "free",
		TotalPoseCount: 0,
	}
	out := renderPoseOrder("sc-1", resp)
	assert.Contains(t, out, "(no participants)")
}

// TestRenderPoseOrder_FreeMode verifies the free-mode flat participant list.
func TestRenderPoseOrder_FreeMode(t *testing.T) {
	t.Parallel()
	resp := &scenev1.GetPoseOrderResponse{
		Mode:           "free",
		TotalPoseCount: 3,
		Entries: []*scenev1.PoseOrderEntry{
			makeEntry("char-alice", "Alice", true, nil),
			makeEntry("char-bob", "Bob", true, nil),
		},
	}
	out := renderPoseOrder("sc-free", resp)
	assert.Contains(t, out, "Participants:")
	assert.Contains(t, out, "Alice")
	assert.Contains(t, out, "Bob")
	assert.NotContains(t, out, "Next to pose")
	assert.NotContains(t, out, "Eligible")
}

// TestRenderPoseOrder_StrictMode verifies "→ Next to pose" + "Then:" sections.
func TestRenderPoseOrder_StrictMode(t *testing.T) {
	t.Parallel()
	resp := &scenev1.GetPoseOrderResponse{
		Mode:           "strict",
		TotalPoseCount: 5,
		Entries: []*scenev1.PoseOrderEntry{
			makeEntry("char-alice", "Alice", true, nil),
			makeEntry("char-bob", "Bob", false, nil),
			makeEntry("char-carol", "Carol", false, nil),
		},
	}
	out := renderPoseOrder("sc-strict", resp)
	assert.Contains(t, out, "Next to pose: Alice")
	assert.Contains(t, out, "Then:")
	assert.Contains(t, out, "Bob")
	assert.Contains(t, out, "Carol")
}

// TestRenderPoseOrder_StrictMode_NoHead verifies correct output when no
// eligible participant exists (edge case: all on cooldown).
func TestRenderPoseOrder_StrictMode_NoHead(t *testing.T) {
	t.Parallel()
	resp := &scenev1.GetPoseOrderResponse{
		Mode:           "strict",
		TotalPoseCount: 2,
		Entries: []*scenev1.PoseOrderEntry{
			makeEntry("char-alice", "Alice", false, nil),
		},
	}
	out := renderPoseOrder("sc-strict-nohead", resp)
	// No eligible head → no "Next to pose" line; non-eligible goes into Then.
	assert.NotContains(t, out, "Next to pose")
	assert.Contains(t, out, "Then:")
	assert.Contains(t, out, "Alice")
}

// TestRenderPoseOrder_3prMode verifies Eligible + Cooldown groups with
// "needs N more" annotation.
func TestRenderPoseOrder_3prMode(t *testing.T) {
	t.Parallel()
	resp := &scenev1.GetPoseOrderResponse{
		Mode:           "3pr",
		TotalPoseCount: 6,
		Entries: []*scenev1.PoseOrderEntry{
			makeEntry("char-alice", "Alice", true, ptr32(3)),
			makeEntry("char-bob", "Bob", false, ptr32(1)),
		},
	}
	out := renderPoseOrder("sc-3pr", resp)
	assert.Contains(t, out, "Eligible to pose (poses_since_last ≥ 3):")
	assert.Contains(t, out, "Alice")
	assert.Contains(t, out, "Cooldown")
	// Bob has posesSinceLast=1; threshold=3 → needs 2 more.
	assert.Contains(t, out, "Bob (needs 2 more)")
}

// TestRenderPoseOrder_5prMode verifies threshold 5 in annotation.
func TestRenderPoseOrder_5prMode(t *testing.T) {
	t.Parallel()
	resp := &scenev1.GetPoseOrderResponse{
		Mode:           "5pr",
		TotalPoseCount: 10,
		Entries: []*scenev1.PoseOrderEntry{
			makeEntry("char-alice", "Alice", true, ptr32(5)),
			makeEntry("char-bob", "Bob", false, ptr32(3)),
		},
	}
	out := renderPoseOrder("sc-5pr", resp)
	assert.Contains(t, out, "Eligible to pose (poses_since_last ≥ 5):")
	assert.Contains(t, out, "Alice")
	// Bob has posesSinceLast=3; threshold=5 → needs 2 more.
	assert.Contains(t, out, "Bob (needs 2 more)")
}

// TestRenderPoseOrder_UnknownMode verifies unrecognised mode falls back to
// the flat participant list (same as free).
func TestRenderPoseOrder_UnknownMode(t *testing.T) {
	t.Parallel()
	resp := &scenev1.GetPoseOrderResponse{
		Mode:           "future-mode",
		TotalPoseCount: 1,
		Entries: []*scenev1.PoseOrderEntry{
			makeEntry("char-alice", "Alice", true, nil),
		},
	}
	out := renderPoseOrder("sc-unknown", resp)
	assert.Contains(t, out, "Participants:")
	assert.Contains(t, out, "Alice")
}

// TestRenderPoseOrder_DisplayNameFallback verifies character_id is shown when
// character_name is empty (Phase 4 default before nameResolver wiring).
func TestRenderPoseOrder_DisplayNameFallback(t *testing.T) {
	t.Parallel()
	resp := &scenev1.GetPoseOrderResponse{
		Mode:           "free",
		TotalPoseCount: 0,
		Entries: []*scenev1.PoseOrderEntry{
			makeEntry("char-raw-id", "", true, nil),
		},
	}
	out := renderPoseOrder("sc-fallback", resp)
	assert.Contains(t, out, "char-raw-id")
}

// TestRenderPoseOrder_HeaderFields verifies the header always includes scene
// ID, mode, and total pose count.
func TestRenderPoseOrder_HeaderFields(t *testing.T) {
	t.Parallel()
	resp := &scenev1.GetPoseOrderResponse{
		Mode:           "free",
		TotalPoseCount: 42,
		Entries: []*scenev1.PoseOrderEntry{
			makeEntry("char-alice", "Alice", true, nil),
		},
	}
	out := renderPoseOrder("my-scene", resp)
	assert.True(t, strings.HasPrefix(out, "Scene my-scene"), "header must start with scene ID")
	assert.Contains(t, out, "free")
	assert.Contains(t, out, "42 total poses")
}
