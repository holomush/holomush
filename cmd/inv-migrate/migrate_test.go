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
