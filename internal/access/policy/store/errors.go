// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"github.com/samber/oops"
)

// IsNotFound returns true if the error is a POLICY_NOT_FOUND error
// from the policy store.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return false
	}
	return oopsErr.Code() == "POLICY_NOT_FOUND"
}
