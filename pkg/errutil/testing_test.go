// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package errutil_test

import (
	"testing"

	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestAssertErrorCode_MatchingCode(t *testing.T) {
	err := oops.Code("MY_CODE").Errorf("test error")
	// Should not fail
	errutil.AssertErrorCode(t, err, "MY_CODE")
}

func TestAssertErrorContext_MatchingKeyValue(t *testing.T) {
	err := oops.With("user_id", "123").Errorf("test error")
	// Should not fail
	errutil.AssertErrorContext(t, err, "user_id", "123")
}
