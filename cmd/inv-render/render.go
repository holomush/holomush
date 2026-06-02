// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
package main

import (
	"fmt"
	"strings"

	"github.com/holomush/holomush/internal/invregistry"
)

// Generated-region marker names. The renderer rewrites only the content
// between the BEGIN/END markers for each name; everything else in
// invariants.md is hand-authored prose and left untouched.
const (
	regionScopeIndex      = "scope-index"
	regionInvariantTables = "invariant-tables"
)

// Render returns invariants.md content with the generated regions refreshed
// from doc. It is a pure function of (doc, currentMD): same inputs always
// produce the same output, and Render is idempotent
// (Render(Render(x)) == Render(x)) because each region is replaced wholesale
// between fixed markers. It errors if a required marker pair is absent —
// missing markers are a structural defect, never silently tolerated.
func Render(doc invregistry.Doc, currentMD string) (string, error) {
	out, err := replaceRegion(currentMD, regionScopeIndex, renderScopeIndex(doc.Scopes))
	if err != nil {
		return "", err
	}
	tables, err := renderInvariantTables(doc)
	if err != nil {
		return "", err
	}
	out, err = replaceRegion(out, regionInvariantTables, tables)
	if err != nil {
		return "", err
	}
	return out, nil
}

// renderScopeIndex renders the scope summary table in YAML declaration order.
func renderScopeIndex(scopes []invregistry.Scope) string {
	var b strings.Builder
	b.WriteString("| Scope | Description | Boundary |\n")
	b.WriteString("|-------|-------------|----------|\n")
	for i := range scopes {
		s := &scopes[i]
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", s.Name, cell(s.Description), cell(s.Boundary))
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderInvariantTables renders one table per scope that has invariants, in
// scope-declaration order, each row in YAML order. Scopes with no invariants
// are omitted. When the registry has no invariants at all, a placeholder line
// is emitted so the region is never empty.
//
// It fails loud if any invariant's scope is not a declared scope: such an
// entry is keyed into byScope under a name no rendering pass reads, so it would
// silently vanish from invariants.md and `-check` would still pass — defeating
// the generate-and-diff guard. The meta-test does not catch this (it derives
// scope membership from the invariant ID prefix, not the scope field), so the
// check belongs here.
func renderInvariantTables(doc invregistry.Doc) (string, error) {
	known := make(map[string]struct{}, len(doc.Scopes))
	for i := range doc.Scopes {
		known[doc.Scopes[i].Name] = struct{}{}
	}
	byScope := make(map[string][]*invregistry.Entry, len(doc.Scopes))
	var orphans []string
	for i := range doc.Invariants {
		e := &doc.Invariants[i]
		if _, ok := known[e.Scope]; !ok {
			orphans = append(orphans, fmt.Sprintf("%s (scope %q)", e.ID, e.Scope))
			continue
		}
		byScope[e.Scope] = append(byScope[e.Scope], e)
	}
	if len(orphans) > 0 {
		return "", fmt.Errorf("invariant(s) reference an undeclared scope (typo or removed scope?): %s", strings.Join(orphans, ", "))
	}
	var b strings.Builder
	wrote := false
	for i := range doc.Scopes {
		s := &doc.Scopes[i]
		entries := byScope[s.Name]
		if len(entries) == 0 {
			continue
		}
		wrote = true
		fmt.Fprintf(&b, "### `%s`\n\n", s.Name)
		b.WriteString("| ID | Summary | Legacy | Binding |\n")
		b.WriteString("|----|---------|--------|---------|\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n",
				e.ID, cell(e.Summary), legacyCell(e.Legacy), cell(bindingOrDash(e.Binding)))
		}
		b.WriteString("\n")
	}
	if !wrote {
		return "_No invariants migrated yet — populated per-scope by the holomush-hz0v4.14 migration._", nil
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// legacyCell renders the legacy-ID list. Each legacy value is recorded as
// "<token>@<origin-spec>"; only the token is shown (backtick-wrapped,
// comma-separated). An empty list renders as an em dash.
func legacyCell(legacy []string) string {
	if len(legacy) == 0 {
		return "—"
	}
	toks := make([]string, 0, len(legacy))
	for _, l := range legacy {
		tok := l
		if i := strings.IndexByte(l, '@'); i >= 0 {
			tok = l[:i]
		}
		toks = append(toks, "`"+cell(tok)+"`")
	}
	return strings.Join(toks, ", ")
}

func bindingOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// cell normalizes a string for safe inclusion in a markdown table cell:
// whitespace runs (including folded-scalar newlines) collapse to single
// spaces, and literal pipes are escaped so they do not break the column.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.Join(strings.Fields(s), " ")
}

// replaceRegion replaces the text between the BEGIN/END markers for name,
// preserving the marker lines themselves. The markers are matched as:
//
//	<!-- BEGIN GENERATED: <name> ...optional note... -->
//	<!-- END GENERATED: <name> -->
//
// The replacement body is surrounded by blank lines so the markers and the
// generated content stay visually separated and the markdown renders cleanly.
func replaceRegion(md, name, body string) (string, error) {
	beginPrefix := fmt.Sprintf("<!-- BEGIN GENERATED: %s", name)
	endMarker := fmt.Sprintf("<!-- END GENERATED: %s -->", name)

	lines := strings.Split(md, "\n")
	beginIdx, endIdx := -1, -1
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if beginIdx == -1 && strings.HasPrefix(trimmed, beginPrefix) {
			beginIdx = i
			continue
		}
		if beginIdx != -1 && trimmed == endMarker {
			endIdx = i
			break
		}
	}
	if beginIdx == -1 {
		return "", fmt.Errorf("invariants.md: missing BEGIN marker for generated region %q", name)
	}
	if endIdx == -1 {
		return "", fmt.Errorf("invariants.md: missing END marker for generated region %q (BEGIN at line %d)", name, beginIdx+1)
	}

	rebuilt := make([]string, 0, len(lines))
	rebuilt = append(rebuilt, lines[:beginIdx+1]...) // through the BEGIN marker
	rebuilt = append(rebuilt, "", body, "")          // blank / body / blank
	rebuilt = append(rebuilt, lines[endIdx:]...)     // from the END marker onward
	return strings.Join(rebuilt, "\n"), nil
}
