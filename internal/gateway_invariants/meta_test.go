// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package gateway_invariants_test

import (
	"regexp"
	"testing"
)

// TestInvariantTokenBoundariesRejectFalsePositives pins the word-boundary
// matcher's discrimination between similar invariant IDs. Without \b,
// "INV-GW-1" would match a comment that only references "INV-GW-10",
// silently masking a genuinely untested invariant.
//
// The gateway per-family coverage test (TestAllGatewayRegistryInvariantsHaveTests)
// was retired by holomush-hz0v4.14.12 — the GW family migrated to INV-EVENTBUS-1..16
// and is now owned by the registry provenance guard (test/meta/invariant_registry_test.go).
// The INV-GW-* tokens below are RETAINED deliberately as regex fixtures: they document
// the exact digit-digit (1 vs 10) and digit-letter (3 vs 3a) boundary hazards the
// migration tool and the guard both rely on \b to avoid; they are not invariant
// annotations.
func TestInvariantTokenBoundariesRejectFalsePositives(t *testing.T) {
	cases := []struct {
		needle string
		text   string
		want   bool
	}{
		{"INV-GW-1", "// reference to INV-GW-1 here", true},
		{"INV-GW-1", "// reference to INV-GW-10 here", false},
		{"INV-GW-1", "// reference to INV-GW-16 here", false},
		{"INV-GW-3", "// reference to INV-GW-3 here", true},
		{"INV-GW-3", "// reference to INV-GW-3a here", false},
		{"INV-GW-3a", "// reference to INV-GW-3a here", true},
		{"INV-GW-10", "// reference to INV-GW-10 here", true},
		{"INV-GW-1", "INV-GW-1.", true}, // trailing punctuation is a boundary
	}
	for _, tc := range cases {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(tc.needle) + `\b`)
		got := re.MatchString(tc.text)
		if got != tc.want {
			t.Errorf("match(%q, %q) = %v, want %v", tc.needle, tc.text, got, tc.want)
		}
	}
}
