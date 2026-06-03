// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invariants

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestINV_SCENE_5_ResolverNoPoseOrderLeak rg-asserts that resolver.go does
// not reference pose-metadata columns in attribute-construction code
// paths. INV-S9 / ADR holomush-nt2d: pose-order data is reachable
// exclusively via the gated GetPoseOrder RPC; ABAC attribute path is
// forbidden.
func TestINV_SCENE_5_ResolverNoPoseOrderLeak(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../plugins/core-scenes/resolver.go")
	require.NoError(t, err)

	forbidden := regexp.MustCompile(`\b(last_pose_at|last_pose_seq|total_pose_count|LastPoseAt|LastPoseSeq|TotalPoseCount)\b`)
	matches := forbidden.FindAll(data, -1)
	assert.Empty(t, matches,
		"INV-SCENE-5: resolver.go MUST NOT reference pose-metadata columns; INV-S9 forbids attribute-driven path to pose data")
}
