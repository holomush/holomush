// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"sort"
	"strings"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
)

// SubjectOwner identifies who owns a given subject pattern. A zero value
// (empty PluginName) is the host fallback — used as the return value from
// Resolve when no declared pattern matches the concrete subject.
type SubjectOwner struct {
	// PluginName is the manifest-declared plugin name. Empty means the
	// host owns the subject.
	PluginName string

	// Pattern is the manifest-declared NATS subject pattern. Supports
	// `*` (matches exactly one token) and `>` (matches one-or-more
	// remaining tokens; MUST be the final token). Example:
	// "events.*.scene.>".
	Pattern string
}

// OwnerMap is the startup-built mapping of subject patterns to owners.
// Resolve uses longest-token-count-wins with a literal-score tiebreak.
//
// An empty/nil OwnerMap resolves every subject to the host owner,
// preserving Phase A behavior where no plugins yet declare ownership.
type OwnerMap struct {
	entries []ownerEntry // sorted: longest-depth first, literal-score tiebreak
}

// ownerEntry is a precomputed pattern-tokenization to avoid re-splitting
// on every Resolve call. The matching hot path reads tokens[] only.
type ownerEntry struct {
	tokens []string
	owner  SubjectOwner
}

// NewOwnerMap builds an OwnerMap from a list of declarations.
//
// Returns ErrSubjectOwnershipConflict (via oops.Code
// AUDIT_SUBJECT_OWNERSHIP_CONFLICT) when two distinct plugins declare
// the same exact pattern. An identical pattern declared twice by the
// SAME plugin is treated as a dedup no-op — loading the same manifest
// twice should not be a fatal error.
//
// Returns AUDIT_INVALID_SUBJECT_PATTERN when a pattern contains `>` in
// a non-terminal position (NATS rejects this at subscribe time; we
// surface it early so manifests cannot ship a dead pattern).
//
// Nil decls is valid and yields an empty (host-everything) OwnerMap.
func NewOwnerMap(decls []SubjectOwner) (*OwnerMap, error) {
	seen := make(map[string]SubjectOwner, len(decls))
	for _, d := range decls {
		if err := validatePattern(d.Pattern); err != nil {
			return nil, err
		}
		if existing, ok := seen[d.Pattern]; ok {
			if existing.PluginName != d.PluginName {
				return nil, oops.
					Code("AUDIT_SUBJECT_OWNERSHIP_CONFLICT").
					With("pattern", d.Pattern).
					With("existing_plugin", existing.PluginName).
					With("conflicting_plugin", d.PluginName).
					Wrap(eventbus.ErrSubjectOwnershipConflict)
			}
			// Same plugin re-declaring same pattern: tolerate as dedup.
			continue
		}
		seen[d.Pattern] = d
	}

	entries := make([]ownerEntry, 0, len(seen))
	for _, d := range seen {
		entries = append(entries, ownerEntry{
			tokens: strings.Split(d.Pattern, "."),
			owner:  d,
		})
	}
	// Sort stable by (depth desc, literal-score desc). Resolve picks the
	// first match; deeper/more-literal patterns therefore win.
	sort.SliceStable(entries, func(i, j int) bool {
		if len(entries[i].tokens) != len(entries[j].tokens) {
			return len(entries[i].tokens) > len(entries[j].tokens)
		}
		return literalScore(entries[i].tokens) > literalScore(entries[j].tokens)
	})
	return &OwnerMap{entries: entries}, nil
}

// validatePattern enforces NATS wildcard positional rules. Specifically,
// `>` MUST only appear as the final token. NATS itself rejects this at
// subscribe time, but we want the failure to surface at manifest-load
// time so a broken manifest cannot reach Start().
func validatePattern(pattern string) error {
	if pattern == "" {
		return oops.
			Code("AUDIT_INVALID_SUBJECT_PATTERN").
			With("pattern", pattern).
			Errorf("empty subject pattern")
	}
	tokens := strings.Split(pattern, ".")
	for i, tok := range tokens {
		if tok == "" {
			return oops.
				Code("AUDIT_INVALID_SUBJECT_PATTERN").
				With("pattern", pattern).
				With("token_index", i).
				Errorf("empty token in pattern")
		}
		// NATS subject syntax: * and > must each occupy an ENTIRE token
		// (no substring use like "foo*" or "ma*in"). > is additionally
		// valid only as the final token.
		if strings.Contains(tok, "*") && tok != "*" {
			return oops.
				Code("AUDIT_INVALID_SUBJECT_PATTERN").
				With("pattern", pattern).
				With("token_index", i).
				With("token", tok).
				Errorf(`"*" wildcard MUST occupy an entire token`)
		}
		if strings.Contains(tok, ">") {
			if tok != ">" {
				return oops.
					Code("AUDIT_INVALID_SUBJECT_PATTERN").
					With("pattern", pattern).
					With("token_index", i).
					With("token", tok).
					Errorf(`">" wildcard MUST occupy an entire token`)
			}
			if i != len(tokens)-1 {
				return oops.
					Code("AUDIT_INVALID_SUBJECT_PATTERN").
					With("pattern", pattern).
					With("token_index", i).
					Errorf(`">" wildcard MUST be the last token`)
			}
		}
	}
	return nil
}

// Resolve returns the owner for a concrete subject. Returns the zero
// SubjectOwner (empty PluginName) when no declared pattern matches,
// which callers interpret as "host owns this subject".
//
// A nil receiver resolves every subject to host; this preserves Phase A
// behavior for projections constructed without an explicit owner map.
func (m *OwnerMap) Resolve(subject string) SubjectOwner {
	if m == nil {
		return SubjectOwner{}
	}
	tokens := strings.Split(subject, ".")
	for _, e := range m.entries {
		if matches(e.tokens, tokens) {
			return e.owner
		}
	}
	return SubjectOwner{}
}

// HostExcludedSubjects returns the plugin-owned entries only. The host
// audit projection's handler uses this (indirectly, via Resolve) to
// ack-and-skip plugin-owned messages; the accessor itself is useful for
// operators to introspect what the host will skip.
//
// Nil receiver returns an empty slice.
func (m *OwnerMap) HostExcludedSubjects() []SubjectOwner {
	if m == nil {
		return nil
	}
	out := make([]SubjectOwner, 0, len(m.entries))
	for _, e := range m.entries {
		if e.owner.PluginName != "" {
			out = append(out, e.owner)
		}
	}
	return out
}

// matches applies NATS wildcard semantics:
//   - `*` matches exactly one token (no zero-token match).
//   - `>` matches one-or-more remaining tokens and MUST be the final
//     token (enforced at construction time).
//   - literal tokens match byte-for-byte.
//
// Returns true iff every pattern token has a matching concrete token
// AND no concrete tokens remain unmatched (unless `>` consumed the tail).
func matches(pattern, concrete []string) bool {
	for i, pt := range pattern {
		switch pt {
		case ">":
			// `>` matches one-or-more remaining concrete tokens; a
			// trailing `>` cannot match zero tokens (NATS semantics).
			return i < len(concrete)
		case "*":
			// `*` matches exactly one concrete token.
			if i >= len(concrete) {
				return false
			}
		default:
			if i >= len(concrete) || pt != concrete[i] {
				return false
			}
		}
	}
	return len(pattern) == len(concrete)
}

// literalScore counts the number of concrete (non-wildcard) tokens in a
// pattern. Used as the tiebreak when two patterns have the same token
// depth: the one with more literals wins, so `events.main.scene.lit`
// beats `events.main.scene.*` for the subject ending in `lit`.
func literalScore(tokens []string) int {
	n := 0
	for _, t := range tokens {
		if t != "*" && t != ">" {
			n++
		}
	}
	return n
}
