// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Unit-tier negative/error-branch coverage for core-scenes pure helpers
// (holomush-psr40). These exercise validation and defense-in-depth branches
// that the integration suite reaches only on the happy path, pinning them at
// the fast unit tier so a regression is caught without Docker.

package main

import (
	"errors"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// TestMapTransitionError translates store oops codes to gRPC status codes and
// returns nil for everything it does not explicitly map.
func TestMapTransitionError(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for a non-oops error", func(t *testing.T) {
		assert.Nil(t, mapTransitionError(errors.New("plain"), "s1"))
	})

	t.Run("returns nil for an unmapped oops code", func(t *testing.T) {
		err := oops.Code("SCENE_SOMETHING_ELSE").Errorf("nope")
		assert.Nil(t, mapTransitionError(err, "s1"))
	})

	t.Run("maps SCENE_NOT_FOUND to NotFound", func(t *testing.T) {
		err := oops.Code("SCENE_NOT_FOUND").Errorf("gone")
		got := mapTransitionError(err, "s1")
		require.Error(t, got)
		assert.Equal(t, codes.NotFound, status.Code(got))
	})

	t.Run("maps SCENE_TRANSITION_FORBIDDEN to FailedPrecondition", func(t *testing.T) {
		err := oops.Code("SCENE_TRANSITION_FORBIDDEN").Errorf("bad state")
		got := mapTransitionError(err, "s1")
		require.Error(t, got)
		assert.Equal(t, codes.FailedPrecondition, status.Code(got))
	})
}

// TestRowToProto_PopulatesLocationWhenSet covers the row.LocationID != nil
// branch (the nil branch is exercised by the integration happy path).
func TestRowToProto_PopulatesLocationWhenSet(t *testing.T) {
	t.Parallel()

	loc := "loc-01ABC"
	now := time.Unix(1700000000, 0)
	info := rowToProto(&SceneRow{ID: "s1", Title: "T", LocationID: &loc}, now)
	assert.Equal(t, "loc-01ABC", info.GetLocationId())
	assert.Equal(t, "s1", info.GetId())

	infoNoLoc := rowToProto(&SceneRow{ID: "s2"}, now)
	assert.Empty(t, infoNoLoc.GetLocationId(), "absent location stays empty")
}

// TestBuildSceneUpdate exercises every validation branch in the update-mask
// switch, including the empty-value rejections and the unknown-path default.
func TestBuildSceneUpdate(t *testing.T) {
	t.Parallel()

	mask := func(paths ...string) *scenev1.UpdateSceneRequest {
		return &scenev1.UpdateSceneRequest{UpdateMask: &fieldmaskpb.FieldMask{Paths: paths}}
	}
	assertInvalidArg := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	}

	t.Run("rejects whitespace-only title", func(t *testing.T) {
		req := mask("title")
		req.Title = "   "
		_, err := buildSceneUpdate(req)
		assertInvalidArg(t, err)
	})

	t.Run("rejects empty visibility", func(t *testing.T) {
		req := mask("visibility")
		req.Visibility = ""
		_, err := buildSceneUpdate(req)
		assertInvalidArg(t, err)
	})

	t.Run("rejects empty pose_order_mode", func(t *testing.T) {
		req := mask("pose_order_mode")
		req.PoseOrderMode = ""
		_, err := buildSceneUpdate(req)
		assertInvalidArg(t, err)
	})

	t.Run("rejects unknown update_mask path", func(t *testing.T) {
		_, err := buildSceneUpdate(mask("not_a_field"))
		assertInvalidArg(t, err)
	})

	t.Run("accepts a valid multi-field mask", func(t *testing.T) {
		req := mask("title", "description", "location_id", "content_warnings", "tags")
		req.Title = "Hello"
		req.Description = "d"
		req.LocationId = "" // empty clears location
		req.ContentWarnings = []string{"cw"}
		req.Tags = []string{"t"}
		got, err := buildSceneUpdate(req)
		require.NoError(t, err)
		require.NotNil(t, got.Title)
		assert.Equal(t, "Hello", *got.Title)
		require.NotNil(t, got.LocationID)
		assert.Equal(t, "", *got.LocationID)
		assert.True(t, got.UpdateContentWarnings)
		assert.True(t, got.UpdateTags)
	})
}

// TestEligibleByThreshold pins the 3pr/5pr cooldown eligibility including the
// defense-in-depth branches (negative seq, seq > total) that the schema makes
// logically impossible but the code guards anyway.
func TestEligibleByThreshold(t *testing.T) {
	t.Parallel()

	p := func(v int32) *int32 { return &v }
	tests := []struct {
		name      string
		total     uint32
		last      *int32
		threshold uint32
		want      bool
	}{
		{"never-posed is always eligible", 10, nil, 3, true},
		{"negative seq treated as eligible (operator drift)", 10, p(-1), 3, true},
		{"seq greater than total treated as eligible", 5, p(9), 3, true},
		{"below threshold is ineligible", 4, p(2), 3, false},
		{"exactly at threshold is eligible", 5, p(2), 3, true},
		{"above threshold is eligible", 10, p(2), 3, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, eligibleByThreshold(tt.total, tt.last, tt.threshold))
		})
	}
}

// TestPosesSinceLast pins the UX counter including the nil, negative-seq, and
// seq>total defensive branches.
func TestPosesSinceLast(t *testing.T) {
	t.Parallel()

	p := func(v int32) *int32 { return &v }
	tests := []struct {
		name  string
		total uint32
		last  *int32
		want  uint32
	}{
		{"nil last returns total", 7, nil, 7},
		{"negative seq returns total", 7, p(-3), 7},
		{"seq greater than total returns zero", 4, p(9), 0},
		{"normal difference", 10, p(4), 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, posesSinceLast(tt.total, tt.last))
		})
	}
}

// TestItoa covers the hand-rolled non-negative int formatter including the
// zero short-circuit and multi-digit path.
func TestItoa(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "0", itoa(0))
	assert.Equal(t, "1", itoa(1))
	assert.Equal(t, "9", itoa(9))
	assert.Equal(t, "16", itoa(16))
}

// TestLatestAttemptID covers the empty-slice branch (returns false) and the
// newest-is-last branch.
func TestLatestAttemptID(t *testing.T) {
	t.Parallel()

	id, ok := latestAttemptID(nil)
	assert.False(t, ok)
	assert.Empty(t, id)

	id, ok = latestAttemptID([]PublishedScene{{ID: "a"}, {ID: "b"}})
	assert.True(t, ok)
	assert.Equal(t, "b", id, "ListSceneAttempts is ASC; newest is last")
}

// TestPublishedAttemptID covers the never-published branch (returns false) and
// the match branch.
func TestPublishedAttemptID(t *testing.T) {
	t.Parallel()

	id, ok := publishedAttemptID([]PublishedScene{{ID: "a", Status: StatusAttemptFailed}})
	assert.False(t, ok)
	assert.Empty(t, id)

	id, ok = publishedAttemptID([]PublishedScene{
		{ID: "a", Status: StatusAttemptFailed},
		{ID: "b", Status: StatusPublished},
	})
	assert.True(t, ok)
	assert.Equal(t, "b", id)
}

// TestRecordError covers the nil-error early return (the uncovered branch) and
// confirms a non-nil error does not panic on a real (no-op) span.
func TestRecordError(t *testing.T) {
	t.Parallel()

	span := noop.Span{}
	assert.NotPanics(t, func() { recordError(span, nil) })
	assert.NotPanics(t, func() { recordError(span, errors.New("boom")) })
}
