// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import "github.com/oklog/ulid/v2"

// ulidToStringPtr converts a ULID pointer to a string pointer for SQL parameters.
// Returns nil if the input is nil.
func ulidToStringPtr(id *ulid.ULID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}
