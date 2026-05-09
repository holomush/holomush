// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package approval

import (
	"time"

	"github.com/oklog/ulid/v2"
)

// RequestID is the 16-byte ULID PK of an admin_approvals row.
type RequestID [16]byte

// String returns the ULID-formatted string.
func (r RequestID) String() string { return ulid.ULID(r).String() }

// OpenRequest is the minimal input to create a pending approval row.
type OpenRequest struct {
	PrimaryPlayerID string
	OpKind          string
	OpArgsHash      []byte
}

// Approval is a snapshot of an admin_approvals row.
type Approval struct {
	RequestID          RequestID
	PrimaryPlayerID    string
	OpKind             string
	OpArgsHash         []byte
	ExpiresAt          time.Time
	ApprovedAt         *time.Time
	ApprovedByPlayerID string
	CreatedAt          time.Time
}

// Clock abstracts time.Now for deterministic tests of WaitForApproval's
// deadline arithmetic. Server-side now() is used for expires_at predicates;
// the Clock affects only client-side deadline computation.
type Clock interface {
	Now() time.Time
}
