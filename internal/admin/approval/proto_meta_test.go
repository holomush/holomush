// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package approval

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestProtoDeterministicMarshalLockedToVendoredProtobuf locks the
// google.golang.org/protobuf module pin per INV-D18. The pin is
// load-bearing on op_args_hash cross-binary stability (INV-D8).
func TestProtoDeterministicMarshalLockedToVendoredProtobuf(t *testing.T) {
	data, err := os.ReadFile("../../../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	src := string(data)
	re := regexp.MustCompile(`google\.golang\.org/protobuf v[0-9]+\.[0-9]+\.[0-9]+`)
	if !re.MatchString(src) {
		t.Fatalf("go.mod must pin google.golang.org/protobuf to a specific semver per INV-D18")
	}
	// Negate: no replace directive without explicit master-spec amendment.
	if strings.Contains(src, "replace google.golang.org/protobuf") {
		t.Fatalf("replace directive on protobuf-go is a chain-breaking change; treat as master-spec amendment")
	}
}
