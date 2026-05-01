// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holomushlint_test

import (
	"testing"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

func TestPackagePathMatchesAnyExactAndPrefix(t *testing.T) {
	allow := []string{
		"github.com/holomush/holomush/internal/eventbus",
		"github.com/holomush/holomush/internal/grpc",
	}
	cases := []struct {
		path string
		want bool
	}{
		{"github.com/holomush/holomush/internal/eventbus", true},
		{"github.com/holomush/holomush/internal/eventbus/codec", true},
		{"github.com/holomush/holomush/internal/grpc/handlers", true},
		{"github.com/holomush/holomush/internal/web", false},
		// Boundary: prefix-without-slash must NOT match.
		{"github.com/holomush/holomush/internal/eventbusx", false},
	}
	for _, tc := range cases {
		if got := holomushlint.PackagePathMatchesAny(tc.path, allow); got != tc.want {
			t.Errorf("path=%q: got %v, want %v", tc.path, got, tc.want)
		}
	}
}

// IsDEKMaterial is exercised end-to-end by the dekmaterialno* analyzer
// tests via analysistest; a unit-test variant requires constructing a
// types.Type for an external package, which is unwieldy here. The
// analyzer testdata is the authoritative coverage path.
