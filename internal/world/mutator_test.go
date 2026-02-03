// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import "testing"

// TestServiceImplementsMutator verifies that Service implements the Mutator interface.
// This is a compile-time check to ensure the interface contract is maintained.
func TestServiceImplementsMutator(t *testing.T) {
	// This test exists to verify the interface at test time.
	// The actual compile-time check is in mutator.go via:
	// var _ Mutator = (*Service)(nil)
	var _ Mutator = (*Service)(nil)
}
