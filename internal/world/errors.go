// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"errors"
	"fmt"

	"github.com/oklog/ulid/v2"
)

// ErrNotFound is returned when a requested entity does not exist.
var ErrNotFound = errors.New("not found")

// ErrConcurrentEdit is the single typed conflict signal for the world-model
// optimistic-concurrency guard (MODEL-03). A version-predicated CAS write or
// delete that finds the row's version has moved wraps this sentinel with
// CodeConcurrentEdit. The world.Service boundary propagates it unchanged (D-02:
// no UX mapping in this phase). It is deliberately distinct from ErrNotFound so
// a lost update is never mistaken for a missing row.
var ErrConcurrentEdit = errors.New("concurrent edit")

// CodeConcurrentEdit is the oops code the guarded repos stamp on a conflict
// wrapping ErrConcurrentEdit. Asserted with errutil.AssertErrorCode.
const CodeConcurrentEdit = "WORLD_CONCURRENT_EDIT"

// ErrFeedLockTimeout is returned when the per-game feed_position counter's
// FOR UPDATE lock cannot be acquired within the allocator's bounded
// lock/statement timeout (MODEL-04). The per-game counter serializes all
// same-game writes; a stuck lock surfaces this typed error rather than blocking
// the mutation transaction indefinitely.
var ErrFeedLockTimeout = errors.New("world feed counter lock timeout")

// CodeFeedLockTimeout is the oops code the feed-counter allocator stamps when the
// FOR UPDATE lock acquisition times out. Asserted with errutil.AssertErrorCode.
const CodeFeedLockTimeout = "WORLD_FEED_LOCK_TIMEOUT"

// ErrNoEventEmitter is returned when an operation requires event emission but no emitter is configured.
// This indicates a misconfiguration - production systems should always have an EventEmitter.
var ErrNoEventEmitter = errors.New("event emitter not configured")

// ErrSelfReferentialExit is returned when an exit's from and to locations are the same.
var ErrSelfReferentialExit = errors.New("self-referential exit: from and to locations cannot be the same")

// CleanupIssueType identifies the type of issue during bidirectional exit cleanup.
type CleanupIssueType string

const (
	// CleanupReturnNotFound indicates the return exit was not found (may have been deleted).
	CleanupReturnNotFound CleanupIssueType = "return_not_found"
	// CleanupFindError indicates an error occurred while finding the return exit.
	CleanupFindError CleanupIssueType = "find_error"
	// CleanupDeleteError indicates an error occurred while deleting the return exit.
	CleanupDeleteError CleanupIssueType = "delete_error"
)

// CleanupIssue represents an issue that occurred during bidirectional exit cleanup.
type CleanupIssue struct {
	Type         CleanupIssueType
	ReturnExitID ulid.ULID // Only set for CleanupDeleteError
	Err          error     // Underlying error, if any
}

// BidirectionalCleanupResult contains information about cleanup issues
// that occurred during bidirectional exit deletion.
// The primary exit was deleted successfully, but cleanup of the return exit may have issues.
type BidirectionalCleanupResult struct {
	ExitID       ulid.ULID
	ToLocationID ulid.ULID
	ReturnName   string
	Issue        *CleanupIssue // nil if cleanup succeeded
}

// Error implements the error interface.
func (r *BidirectionalCleanupResult) Error() string {
	if r.Issue == nil {
		return ""
	}
	switch r.Issue.Type {
	case CleanupReturnNotFound:
		return fmt.Sprintf("return exit %q not found at location %s during cleanup (may have been already deleted)",
			r.ReturnName, r.ToLocationID)
	case CleanupFindError:
		return fmt.Sprintf("failed to find return exit %q at location %s: %v",
			r.ReturnName, r.ToLocationID, r.Issue.Err)
	case CleanupDeleteError:
		return fmt.Sprintf("failed to delete return exit %s: %v",
			r.Issue.ReturnExitID, r.Issue.Err)
	default:
		return fmt.Sprintf("unknown cleanup issue for exit %s", r.ExitID)
	}
}

// Unwrap returns the underlying error.
func (r *BidirectionalCleanupResult) Unwrap() error {
	if r.Issue != nil {
		return r.Issue.Err
	}
	return nil
}

// IsSevere returns true if the cleanup issue represents an actual error
// that requires attention (not just "return exit already deleted").
func (r *BidirectionalCleanupResult) IsSevere() bool {
	if r.Issue == nil {
		return false
	}
	return r.Issue.Type == CleanupFindError || r.Issue.Type == CleanupDeleteError
}
