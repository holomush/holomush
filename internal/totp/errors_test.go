// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp_test

import (
	"testing"
	"time"

	"github.com/holomush/holomush/internal/totp"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestErrorCodesPresent(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{totp.ErrBootstrapAlreadyConsumed, "TOTP_BOOTSTRAP_CONSUMED"},
		{totp.ErrAlreadyEnrolled, "TOTP_ALREADY_ENROLLED"},
		{totp.ErrNotEnrolled, "TOTP_NOT_ENROLLED"},
		{totp.ErrInvalidRecoveryCode, "TOTP_INVALID_RECOVERY_CODE"},
	}
	for _, tc := range cases {
		errutil.AssertErrorCode(t, tc.err, tc.code)
	}
}

func TestNewErrTOTPLockedCarriesUntil(t *testing.T) {
	until := time.Date(2026, 5, 7, 12, 30, 0, 0, time.UTC)
	err := totp.NewErrTOTPLocked(until)
	errutil.AssertErrorCode(t, err, "TOTP_LOCKED")
	errutil.AssertErrorContext(t, err, "until", until)
}
