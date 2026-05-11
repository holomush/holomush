// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"errors"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"
)

// TestExtractOopsCode_AllPaths exercises the unexported extractOopsCode
// helper across the error shapes it must classify: plain stdlib errors,
// oops errors with a string code, oops errors without a code, and nil.
func TestExtractOopsCode_AllPaths(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "plain stdlib error returns UNKNOWN",
			err:  errors.New("plain"),
			want: "UNKNOWN",
		},
		{
			name: "oops error with code returns code",
			err:  oops.Code("FOO").Errorf("x"),
			want: "FOO",
		},
		{
			name: "oops error without code returns UNKNOWN",
			err:  oops.Errorf("x"),
			want: "UNKNOWN",
		},
		{
			name: "nil error returns UNKNOWN",
			err:  nil,
			want: "UNKNOWN",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, extractOopsCode(tc.err))
		})
	}
}

// TestCheckpointViewToProto_OptionalFields verifies that the optional fields
// (CompletedAt, OldDEKID, NewDEKID) on CheckpointView are wired into the
// resulting RekeyStatusResponse only when present, matching the oneof
// semantics of the proto.
func TestCheckpointViewToProto_OptionalFields(t *testing.T) {
	completed := time.Date(2026, 5, 11, 12, 34, 56, 0, time.UTC)
	newDEK := int64(42)

	cases := []struct {
		name       string
		view       CheckpointView
		wantCompAt bool
		wantOldDEK *int64
		wantNewDEK *int64
	}{
		{
			name: "all optionals absent",
			view: CheckpointView{
				RequestID: [16]byte{0x01},
				Status:    "phase1_complete",
				// CompletedAt nil, OldDEKID 0, NewDEKID nil
			},
			wantCompAt: false,
			wantOldDEK: nil,
			wantNewDEK: nil,
		},
		{
			name: "completed_at present only",
			view: CheckpointView{
				RequestID:   [16]byte{0x02},
				Status:      "complete",
				CompletedAt: &completed,
			},
			wantCompAt: true,
			wantOldDEK: nil,
			wantNewDEK: nil,
		},
		{
			name: "old_dek_id non-zero only",
			view: CheckpointView{
				RequestID: [16]byte{0x03},
				Status:    "phase2_complete",
				OldDEKID:  7,
			},
			wantCompAt: false,
			wantOldDEK: int64Ptr(7),
			wantNewDEK: nil,
		},
		{
			name: "new_dek_id present only",
			view: CheckpointView{
				RequestID: [16]byte{0x04},
				Status:    "phase3_complete",
				NewDEKID:  &newDEK,
			},
			wantCompAt: false,
			wantOldDEK: nil,
			wantNewDEK: &newDEK,
		},
		{
			name: "all optionals present",
			view: CheckpointView{
				RequestID:   [16]byte{0x05},
				Status:      "complete",
				CompletedAt: &completed,
				OldDEKID:    7,
				NewDEKID:    &newDEK,
			},
			wantCompAt: true,
			wantOldDEK: int64Ptr(7),
			wantNewDEK: &newDEK,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := checkpointViewToProto(tc.view)
			require.NotNil(t, res)
			// Required fields always populated.
			require.Equal(t, tc.view.RequestID[:], res.RequestId)
			require.Equal(t, tc.view.Status, res.Status)

			if tc.wantCompAt {
				require.NotNil(t, res.CompletedAt, "CompletedAt must be set when view.CompletedAt is non-nil")
				require.True(t, res.CompletedAt.AsTime().Equal(completed))
			} else {
				require.Nil(t, res.CompletedAt, "CompletedAt must be nil when view.CompletedAt is nil")
			}

			if tc.wantOldDEK == nil {
				require.Nil(t, res.OldDekId, "OldDekId must be nil when view.OldDEKID is zero")
			} else {
				require.NotNil(t, res.OldDekId)
				require.Equal(t, *tc.wantOldDEK, *res.OldDekId)
			}

			if tc.wantNewDEK == nil {
				require.Nil(t, res.NewDekId, "NewDekId must be nil when view.NewDEKID is nil")
			} else {
				require.NotNil(t, res.NewDekId)
				require.Equal(t, *tc.wantNewDEK, *res.NewDekId)
			}
		})
	}
}

func int64Ptr(v int64) *int64 { return &v }
