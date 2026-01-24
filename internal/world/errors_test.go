// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/world"
)

func TestBidirectionalCleanupResult_Error(t *testing.T) {
	exitID := ulid.Make()
	toLocationID := ulid.Make()
	returnExitID := ulid.Make()
	underlyingErr := errors.New("connection timeout")

	tests := []struct {
		name        string
		result      *world.BidirectionalCleanupResult
		wantContain string
	}{
		{
			name: "nil issue returns empty string",
			result: &world.BidirectionalCleanupResult{
				ExitID:       exitID,
				ToLocationID: toLocationID,
				ReturnName:   "south",
				Issue:        nil,
			},
			wantContain: "",
		},
		{
			name: "return not found",
			result: &world.BidirectionalCleanupResult{
				ExitID:       exitID,
				ToLocationID: toLocationID,
				ReturnName:   "south",
				Issue: &world.CleanupIssue{
					Type: world.CleanupReturnNotFound,
				},
			},
			wantContain: "not found",
		},
		{
			name: "find error",
			result: &world.BidirectionalCleanupResult{
				ExitID:       exitID,
				ToLocationID: toLocationID,
				ReturnName:   "south",
				Issue: &world.CleanupIssue{
					Type: world.CleanupFindError,
					Err:  underlyingErr,
				},
			},
			wantContain: "connection timeout",
		},
		{
			name: "delete error",
			result: &world.BidirectionalCleanupResult{
				ExitID:       exitID,
				ToLocationID: toLocationID,
				ReturnName:   "south",
				Issue: &world.CleanupIssue{
					Type:         world.CleanupDeleteError,
					ReturnExitID: returnExitID,
					Err:          underlyingErr,
				},
			},
			wantContain: "failed to delete",
		},
		{
			name: "unknown issue type",
			result: &world.BidirectionalCleanupResult{
				ExitID:       exitID,
				ToLocationID: toLocationID,
				ReturnName:   "south",
				Issue: &world.CleanupIssue{
					Type: world.CleanupIssueType("unknown"),
				},
			},
			wantContain: "unknown cleanup issue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errStr := tt.result.Error()
			if tt.wantContain == "" {
				assert.Empty(t, errStr)
			} else {
				assert.Contains(t, errStr, tt.wantContain)
			}
		})
	}
}

func TestBidirectionalCleanupResult_Unwrap(t *testing.T) {
	underlyingErr := errors.New("database error")

	t.Run("unwrap with nil issue returns nil", func(t *testing.T) {
		result := &world.BidirectionalCleanupResult{
			ExitID: ulid.Make(),
			Issue:  nil,
		}
		assert.Nil(t, result.Unwrap())
	})

	t.Run("unwrap with nil Err returns nil", func(t *testing.T) {
		result := &world.BidirectionalCleanupResult{
			ExitID: ulid.Make(),
			Issue: &world.CleanupIssue{
				Type: world.CleanupReturnNotFound,
				Err:  nil,
			},
		}
		assert.Nil(t, result.Unwrap())
	})

	t.Run("unwrap returns underlying error", func(t *testing.T) {
		result := &world.BidirectionalCleanupResult{
			ExitID: ulid.Make(),
			Issue: &world.CleanupIssue{
				Type: world.CleanupFindError,
				Err:  underlyingErr,
			},
		}
		assert.Equal(t, underlyingErr, result.Unwrap())
	})

	t.Run("errors.Is works through unwrap", func(t *testing.T) {
		sentinelErr := errors.New("sentinel")
		result := &world.BidirectionalCleanupResult{
			ExitID: ulid.Make(),
			Issue: &world.CleanupIssue{
				Type: world.CleanupFindError,
				Err:  sentinelErr,
			},
		}
		assert.True(t, errors.Is(result, sentinelErr))
	})
}

func TestBidirectionalCleanupResult_IsSevere(t *testing.T) {
	tests := []struct {
		name      string
		issueType world.CleanupIssueType
		wantSevere bool
	}{
		{"nil issue is not severe", "", false},
		{"return not found is not severe", world.CleanupReturnNotFound, false},
		{"find error is severe", world.CleanupFindError, true},
		{"delete error is severe", world.CleanupDeleteError, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var issue *world.CleanupIssue
			if tt.issueType != "" {
				issue = &world.CleanupIssue{Type: tt.issueType}
			}
			result := &world.BidirectionalCleanupResult{
				ExitID: ulid.Make(),
				Issue:  issue,
			}
			assert.Equal(t, tt.wantSevere, result.IsSevere())
		})
	}
}
