// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"os"
	"strings"
	"testing"
)

// TestJCSCanonicalizationLockedToVendoredImpl asserts the JCS canonicalizer
// pin in go.mod. INV-D13: switching libraries / pseudo-versions is a
// chain-breaking change requiring a master-spec amendment.
func TestJCSCanonicalizationLockedToVendoredImpl(t *testing.T) {
	data, err := os.ReadFile("../../../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	src := string(data)
	if !strings.Contains(src, "github.com/cyberphone/json-canonicalization v0.0.0-20241213102144-19d51d7fe467") {
		t.Fatalf("go.mod must pin cyberphone/json-canonicalization at v0.0.0-20241213102144-19d51d7fe467 (INV-D13)")
	}
}
