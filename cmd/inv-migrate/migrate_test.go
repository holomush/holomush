// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRewriteScopeRewritesOnlyRecordedTokens(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "presence.go")
	// INV-3 belongs to PRESENCE here; a stray INV-4 is NOT recorded and must be untouched.
	if err := os.WriteFile(src, []byte("// INV-3: snapshot\nx := 1 // INV-4: unrelated\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := []rewrite{{File: src, Token: "INV-3", Canonical: "INV-PRESENCE-1"}}
	changed, err := rewriteAll(plan)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 {
		t.Fatalf("want 1 file changed, got %d", changed)
	}
	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "// INV-PRESENCE-1: snapshot\nx := 1 // INV-4: unrelated\n"
	if string(got) != want {
		t.Errorf("rewrite wrong:\n got %q\nwant %q", got, want)
	}
}

func TestRewriteIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "p.go")
	if err := os.WriteFile(src, []byte("// INV-3: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := []rewrite{{File: src, Token: "INV-3", Canonical: "INV-PRESENCE-1"}}
	if _, err := rewriteAll(plan); err != nil {
		t.Fatal(err)
	}
	changed, err := rewriteAll(plan) // second run: token already gone
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Errorf("re-run should change 0 files, changed %d", changed)
	}
}

func TestRewriteRefusesMissingFile(t *testing.T) {
	plan := []rewrite{{File: "/nope/missing.go", Token: "INV-3", Canonical: "INV-PRESENCE-1"}}
	if _, err := rewriteAll(plan); err == nil {
		t.Fatal("want error for missing recorded file, got nil")
	}
}

// TestRewriteTokenExpandsSlashShorthandCompounds covers holomush-hz0v4.14.25:
// slash-joined shorthand compounds (INV-RB-2/6/12, INV-F6/F7) must expand every
// tail component to its canonical, not leave a bare dangling suffix.
func TestRewriteTokenExpandsSlashShorthandCompounds(t *testing.T) {
	planMap := map[string]string{
		"INV-RB-2": "INV-CRYPTO-27", "INV-RB-6": "INV-CRYPTO-31", "INV-RB-12": "INV-CRYPTO-37",
		"INV-F6": "INV-CRYPTO-56", "INV-F7": "INV-CRYPTO-57",
	}
	cases := []struct {
		name, token, canonical, in, want string
	}{
		{
			"number shorthand expands every tail component", "INV-RB-2", "INV-CRYPTO-27",
			"see INV-RB-2/6/12 here", "see INV-CRYPTO-27/INV-CRYPTO-31/INV-CRYPTO-37 here",
		},
		{
			"letter shorthand expands the sibling", "INV-F6", "INV-CRYPTO-56",
			"// INV-F6/F7 fence", "// INV-CRYPTO-56/INV-CRYPTO-57 fence",
		},
		{
			"standalone token without a tail still rewrites", "INV-RB-2", "INV-CRYPTO-27",
			"// INV-RB-2 only", "// INV-CRYPTO-27 only",
		},
		{
			"unrecorded suffix is preserved verbatim (no corruption)", "INV-RB-2", "INV-CRYPTO-27",
			"INV-RB-2/99", "INV-CRYPTO-27/99",
		},
		{
			"no false match inside a longer number", "INV-RB-2", "INV-CRYPTO-27",
			"INV-RB-29 unchanged", "INV-RB-29 unchanged",
		},
		{
			"a canonical compound for a different token is untouched", "INV-RB-2", "INV-CRYPTO-27",
			"INV-CRYPTO-27/31/37", "INV-CRYPTO-27/31/37",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(rewriteToken([]byte(tc.in), tc.token, tc.canonical, planMap))
			if got != tc.want {
				t.Errorf("rewriteToken(%q, token=%q) =\n %q\nwant %q", tc.in, tc.token, got, tc.want)
			}
		})
	}
}

// TestRewriteAllExpandsCompoundFromPlan proves the planMap that drives tail
// expansion is built from the whole plan (the siblings INV-RB-6/12 are recorded
// as their own refs), and that the expansion is idempotent.
func TestRewriteAllExpandsCompoundFromPlan(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "x.go")
	if err := os.WriteFile(src, []byte("// fence INV-RB-2/6/12 here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := []rewrite{
		{File: src, Token: "INV-RB-2", Canonical: "INV-CRYPTO-27"},
		{File: src, Token: "INV-RB-6", Canonical: "INV-CRYPTO-31"},
		{File: src, Token: "INV-RB-12", Canonical: "INV-CRYPTO-37"},
	}
	if _, err := rewriteAll(plan); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "// fence INV-CRYPTO-27/INV-CRYPTO-31/INV-CRYPTO-37 here\n"
	if string(got) != want {
		t.Errorf("compound expand:\n got %q\nwant %q", got, want)
	}
	changed, err := rewriteAll(plan) // re-run: all tokens gone
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Errorf("re-run changed %d files, want 0 (idempotent)", changed)
	}
}
