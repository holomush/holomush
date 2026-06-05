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
// google.golang.org/protobuf module pin per INV-CRYPTO-85. The pin is
// load-bearing on op_args_hash cross-binary stability (INV-CRYPTO-75).
func TestProtoDeterministicMarshalLockedToVendoredProtobuf(t *testing.T) {
	data, err := os.ReadFile("../../../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	src := string(data)
	// Lock the EXACT version (mirrors INV-CRYPTO-80's JCS pin in
	// internal/admin/policy/jcs_meta_test.go). A bump of this
	// dependency is a chain-breaking master-spec amendment per INV-CRYPTO-75 —
	// the test must fail loudly when the version changes so that the
	// op_args_hash audit chain is re-validated against the new encoder.
	re := regexp.MustCompile(`google\.golang\.org/protobuf v1\.36\.11\b`)
	if !re.MatchString(src) {
		t.Fatalf("go.mod must pin google.golang.org/protobuf to v1.36.11 per INV-CRYPTO-85 / INV-CRYPTO-75 (chain-breaking on bump)")
	}
	// Negate: no replace directive without explicit master-spec amendment.
	if strings.Contains(src, "replace google.golang.org/protobuf") {
		t.Fatalf("replace directive on protobuf-go is a chain-breaking change; treat as master-spec amendment")
	}
}
