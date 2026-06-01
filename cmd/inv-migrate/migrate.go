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
)

// rewrite is one recorded site: replace whole-token Token with Canonical in File.
type rewrite struct {
	File      string
	Token     string
	Canonical string
}

// rewriteAll applies each rewrite to its file. Whole-token match only (so INV-3
// never matches inside INV-31). Idempotent: a file whose Token is already absent
// is left unchanged. Returns the count of files actually modified. A recorded
// file that does not exist is an error (the registry is stale — fail loud).
func rewriteAll(plan []rewrite) (int, error) {
	changed := 0
	for _, r := range plan {
		data, err := os.ReadFile(r.File)
		if err != nil {
			return changed, fmt.Errorf("recorded ref unreadable %s: %w", r.File, err)
		}
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(r.Token) + `\b`)
		out := re.ReplaceAll(data, []byte(r.Canonical))
		if bytes.Equal(out, data) {
			continue
		}
		if err := os.WriteFile(r.File, out, 0o644); err != nil { //nolint:gosec // G306: source files are 0644 by repo convention
			return changed, fmt.Errorf("write %s: %w", r.File, err)
		}
		changed++
	}
	return changed, nil
}
