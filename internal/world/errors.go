// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"fmt"

	"github.com/oklog/ulid/v2"
)

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
