// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl

import (
	"os"
	"testing"
)

// TestGenerateEBNF outputs the EBNF grammar to stdout when DUMP_EBNF=1.
// Usage: DUMP_EBNF=1 go test -run TestGenerateEBNF -v ./internal/access/policy/dsl/
func TestGenerateEBNF(t *testing.T) {
	if os.Getenv("DUMP_EBNF") != "1" {
		t.Skip("set DUMP_EBNF=1 to output grammar")
	}
	t.Log("\n" + parser.String())
}

// TestEBNFNotEmpty ensures the parser produces a non-empty EBNF grammar.
func TestEBNFNotEmpty(t *testing.T) {
	ebnf := EBNF()
	if len(ebnf) == 0 {
		t.Fatal("EBNF() returned empty string")
	}
}
