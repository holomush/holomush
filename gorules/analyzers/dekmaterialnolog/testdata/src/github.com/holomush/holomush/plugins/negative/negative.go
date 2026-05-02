// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// plugins/ is OUTSIDE any allowlist (this analyzer has no allowlist),
// but lives under github.com/holomush/holomush/ for parity with the
// positive testdata (see cursorpackageinternal precedent for the path
// rationale: example.com/ would be rejected by go/types' internal-
// package visibility rule before the analyzer ever runs).
package negative

import "log"

type SafeStruct struct{ Name string }

var _ = func(s SafeStruct) {
	log.Printf("safe: %v", s)
}
