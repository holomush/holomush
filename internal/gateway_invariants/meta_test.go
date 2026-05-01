// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package gateway_invariants_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAllGatewayRegistryInvariantsHaveTests asserts that every numbered
// invariant from the spec has at least one test function whose name
// references it.
func TestAllGatewayRegistryInvariantsHaveTests(t *testing.T) {
	invariants := []string{
		"INV-GW-1",
		"INV-GW-2",
		"INV-GW-3",
		"INV-GW-3a",
		"INV-GW-4",
		"INV-GW-5",
		"INV-GW-6",
		"INV-GW-7",
		"INV-GW-8",
		"INV-GW-9",
		"INV-GW-10",
		"INV-GW-11",
		// INV-GW-12 is forward-declared for Phase 3 — excluded.
		"INV-GW-13",
		"INV-GW-14",
		"INV-GW-15",
		"INV-GW-16",
	}

	testFiles := []string{
		"../../internal/eventbus/rendering_publisher_test.go",
		"../../internal/eventbus/rendering_publisher_internal_test.go",
		"../../internal/eventbus/publisher_test.go",
		"../../internal/eventbus/types_proto_sync_test.go",
		"../../internal/eventbus/audit/projection_unit_test.go",
		"../../internal/web/translate_test.go",
		"../../internal/telnet/gateway_handler_test.go",
		"../../internal/core/registry_test.go",
		"../../internal/core/exports_test.go",
		"../../internal/plugin/manager_test.go",
		"../../cmd/holomush/gateway_imports_test.go",
		"../../test/integration/eventbus_e2e/rendering_completeness_test.go",
	}

	// Word-boundary regex per invariant. Substring matching false-positives:
	// "INV-GW-1" would match "INV-GW-10"/.../"INV-GW-16", and "INV-GW-3"
	// would match "INV-GW-3a". \b prevents both because digit-digit and
	// digit-letter are mid-word transitions in Go's regexp engine.
	invariantPattern := make(map[string]*regexp.Regexp, len(invariants))
	for _, inv := range invariants {
		invariantPattern[inv] = regexp.MustCompile(`\b` + regexp.QuoteMeta(inv) + `\b`)
	}

	invariantToFile := make(map[string][]string)
	fset := token.NewFileSet()
	for _, path := range testFiles {
		absPath, err := filepath.Abs(path)
		require.NoError(t, err)
		f, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
		require.NoError(t, err, "failed to parse %s", path)
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				for _, inv := range invariants {
					if invariantPattern[inv].MatchString(c.Text) {
						invariantToFile[inv] = append(invariantToFile[inv], path)
					}
				}
			}
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Doc == nil {
				continue
			}
			docText := fn.Doc.Text()
			for _, inv := range invariants {
				if invariantPattern[inv].MatchString(docText) {
					invariantToFile[inv] = append(invariantToFile[inv], path)
				}
			}
		}
	}

	for _, inv := range invariants {
		files := invariantToFile[inv]
		if len(files) == 0 {
			t.Errorf("invariant %s has no test referencing it", inv)
		}
	}
}

// TestInvariantTokenBoundariesRejectFalsePositives pins the word-boundary
// matcher's discrimination between similar invariant IDs. Without \b,
// "INV-GW-1" would match a comment that only references "INV-GW-10",
// silently masking a genuinely untested invariant.
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
