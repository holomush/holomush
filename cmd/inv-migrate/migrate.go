// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Command inv-migrate rewrites in-code invariant annotations from a legacy
// token to the canonical INV-<SCOPE>-N id, driven entirely by the registry's
// recorded {file, token} refs. It NEVER matches on bare INV-N across the tree;
// it only touches sites the registry recorded. See
// docs/superpowers/specs/2026-06-01-invariant-registry-migration-redesign.md.
package main

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// rewrite is one recorded site: replace whole-token Token with Canonical in File.
type rewrite struct {
	File      string
	Token     string
	Canonical string
}

// rewriteAll applies each rewrite to its file. Whole-token match only (so INV-3
// never matches inside INV-31). Idempotent: a file whose Token is already absent
// is left unchanged. Returns the count of DISTINCT files actually modified (a
// file with multiple recorded token rewrites counts once). A recorded file that
// does not exist is an error (the registry is stale — fail loud).
func rewriteAll(plan []rewrite) (int, error) {
	// planMap (legacy token → canonical) over the WHOLE plan lets rewriteToken
	// expand slash-joined shorthand compounds whose tail components are sibling
	// legacy tokens recorded as their own refs (holomush-hz0v4.14.25).
	planMap := make(map[string]string, len(plan))
	for _, r := range plan {
		planMap[r.Token] = r.Canonical
	}
	changedFiles := map[string]bool{}
	for _, r := range plan {
		data, err := os.ReadFile(r.File)
		if err != nil {
			return len(changedFiles), fmt.Errorf("recorded ref unreadable %s: %w", r.File, err)
		}
		out := rewriteToken(data, r.Token, r.Canonical, planMap)
		if bytes.Equal(out, data) {
			continue
		}
		if err := os.WriteFile(r.File, out, 0o644); err != nil { //nolint:gosec // G306: source files are 0644 by repo convention
			return len(changedFiles), fmt.Errorf("write %s: %w", r.File, err)
		}
		changedFiles[r.File] = true
	}
	return len(changedFiles), nil
}

// compoundTailRE captures an optional slash-joined shorthand tail after a token:
// the `/6/12` in `INV-RB-2/6/12` or the `/F7` in `INV-F6/F7`. Each tail segment
// is a sibling invariant's final component under the leading token's family.
var compoundTailRE = `((?:/[A-Za-z0-9]+)+)?`

// rewriteToken replaces whole-token `token` with `canonical`, and additionally
// expands a slash-joined shorthand tail (`token/<suffix>/<suffix>…`). Authors
// sometimes abbreviate a run of sibling invariants as `INV-RB-2/6/12` (drop the
// `INV-RB-` prefix) or `INV-F6/F7` (drop only `INV-`); a plain whole-token
// rewrite leaves the bare suffixes dangling (`INV-CRYPTO-27/6/12`,
// `INV-CRYPTO-56/F7`), which the residual guard's `\bINV-\d+\b` cannot see
// (holomush-hz0v4.14.23/.26 hand-fixes). Here each suffix is reconstructed as
// `<prefix><suffix>` (prefix = token up to and including its last '-') and, IF
// that reconstructed legacy token is itself a recorded ref (present in planMap),
// replaced with its canonical full token. An unrecognised suffix is preserved
// verbatim so a cross-family, fractional, or already-canonical tail is never
// corrupted — the closed-world rule (touch only recorded tokens) still holds.
// Whole-token match only: INV-3 never matches inside INV-31.
func rewriteToken(data []byte, token, canonical string, planMap map[string]string) []byte {
	prefix := token[:strings.LastIndexByte(token, '-')+1]
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(token) + compoundTailRE + `\b`)
	return re.ReplaceAllFunc(data, func(m []byte) []byte {
		tail := m[len(token):] // "" or "/s1/s2…"
		var b bytes.Buffer
		b.WriteString(canonical)
		if len(tail) > 0 {
			for _, suf := range strings.Split(string(tail[1:]), "/") {
				b.WriteByte('/')
				if canon, ok := planMap[prefix+suf]; ok {
					b.WriteString(canon)
				} else {
					b.WriteString(suf) // unrecognised suffix: preserve verbatim
				}
			}
		}
		return b.Bytes()
	})
}
