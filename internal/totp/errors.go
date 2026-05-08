// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"time"

	"github.com/samber/oops"
)

// Sentinel errors for TOTP operations. Callers SHOULD match via errutil.AssertErrorCode
// or oops.GetCode rather than direct equality, as oops errors carry structured context.
var (
	// ErrBootstrapAlreadyConsumed is returned when a one-time bootstrap token has already been used.
	ErrBootstrapAlreadyConsumed = oops.Code("TOTP_BOOTSTRAP_CONSUMED").
					Errorf("TOTP bootstrap already consumed")
	// ErrAlreadyEnrolled is returned when attempting to enroll a player who is already enrolled.
	ErrAlreadyEnrolled = oops.Code("TOTP_ALREADY_ENROLLED").
				Errorf("TOTP already enrolled for this player")
	// ErrNotEnrolled is returned when a TOTP operation requires enrollment that has not occurred.
	ErrNotEnrolled = oops.Code("TOTP_NOT_ENROLLED").
			Errorf("TOTP not enrolled for this player")
	// ErrInvalidRecoveryCode is returned when a recovery code is invalid or already used.
	ErrInvalidRecoveryCode = oops.Code("TOTP_INVALID_RECOVERY_CODE").
				Errorf("invalid recovery attempt")
)

// NewErrTOTPLocked returns a TOTP_LOCKED error carrying the lock expiry time in the "until" context key.
func NewErrTOTPLocked(until time.Time) error {
	return oops.Code("TOTP_LOCKED").
		With("until", until).
		Errorf("TOTP verification locked until %s", until.Format(time.RFC3339))
}
