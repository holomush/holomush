// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
)

// This file is the ACE test-naming ratchet. `.claude/rules/testing.md` makes one
// binding requirement of a test name — that it communicate an Action, a
// Condition and an Expectation — and grades the avoidance of underscores only as
// a SHOULD NOT. The two are routinely conflated, and the difference is large: a
// predicate that flags every underscore-form name returns over a thousand hits,
// the overwhelming majority of which are already full sentences that merely use
// `_` as a separator between the unit under test and the behaviour clause
// (TestCascadeDelete_Object_RollsBackPropertiesOnParentDeleteFail is not a
// defect). Renaming those would be a multi-thousand-line diff that improves
// nothing.
//
// The ratchet therefore enforces the MUST, not the SHOULD. It flags an
// underscore-form name whose FINAL segment tokenises to a single CamelCase word,
// because such a name terminates in a bare topic — a method or a noun — and so
// carries no expectation clause at all: TestStatus_String, TestClient_Subscribe,
// TestPropertyRepository_Delete. That is the population that genuinely fails the
// convention.
//
// Two exemptions, and deliberately no mechanism for a third. A growing allowlist
// is precisely what this requirement rejected, so a name that seems to need an
// exemption almost certainly needs a better name instead.
//
// This file excludes ITSELF from the walk. Its placeholder vocabulary below
// necessarily contains marker-shaped literals, and a walker carrying the text it
// searches for flags itself otherwise. `quarantine_registry_test.go` establishes
// the same self-exclusion for the same reason.
//
// Shared helpers `findRepoRoot` and `skipDirs` live in `meta_helpers_test.go`
// and are reused here rather than redeclared. Local helpers carry an `ace`
// prefix or suffix to avoid colliding with the identically-shaped helpers other
// meta-tests in this package define.
//
// This ratchet enforces a naming convention, not a system-behaviour guarantee,
// so it carries no invariant-registry entry and no binding annotation — matching
// the two ratchets it sits beside, which are likewise unregistered.

// aceConventionRule is cited in every failure message so a contributor who trips
// the ratchet is pointed at the rule rather than left guessing.
const aceConventionRule = ".claude/rules/testing.md (Test Naming (ACE))"

// aceSelfPath is this file, excluded from its own walk (see the note above).
var aceSelfPath = filepath.Join("test", "meta", "ace_naming_registry_test.go")

// acePinnedNames are the only two names exempted by identity rather than by
// prefix. Both are pinned externally by the history-scope privacy design spec,
// which names them verbatim as the evidence for its floor-preservation claims.
// They are realised as Ginkgo containers rather than `func Test` symbols, so the
// declaration walk does not currently reach them; the entries are kept so that
// converting either to a plain Go test cannot silently make it a violation.
var acePinnedNames = map[string]struct{}{
	"TestPrivacy_ReattachWithinTTLPreservesFloor": {},
	"TestPrivacy_TTLExpiryEndsSessionFreshFloor":  {},
}

// aceInvariantBindingPrefix marks the TestINV_<SCOPE>_<N>_<Behaviour> family.
// Their underscores encode a registry identifier that roughly a hundred test
// files reference through `// Verifies:` annotations, and that the registry's own
// meta-tests and human reviewers read directly. Renaming them would destroy that
// identifier-to-test readability, so the family is exempt by prefix.
const aceInvariantBindingPrefix = "TestINV_"

// acePlaceholderLabels is the vocabulary of subtest and table-case labels that
// name no behaviour. A label matches only on an exact, case-insensitive,
// space-trimmed comparison: "empty name" and "empty list" are fine, bare "empty"
// is not.
var acePlaceholderLabels = map[string]struct{}{
	"success": {}, "happy path": {}, "sad path": {}, "error case": {},
	"error": {}, "failure": {}, "ok": {}, "basic": {}, "simple": {},
	"empty": {}, "negative": {}, "positive": {}, "valid": {}, "invalid": {},
	"default": {}, "normal": {}, "works": {}, "test": {}, "case": {},
	"test 1": {}, "test 2": {}, "test 3": {},
}

// aceViolation is one offending declaration or label.
type aceViolation struct {
	file   string
	line   int
	detail string
}

func (v aceViolation) String() string {
	return v.file + ":" + strconv.Itoa(v.line) + ": " + v.detail
}

// aceSplitCamelWords splits a CamelCase segment into words, treating a run of
// consecutive capitals as one acronym so that ULID, DSN and ID each count as a
// single token rather than one token per letter.
func aceSplitCamelWords(s string) []string {
	if s == "" {
		return nil
	}
	r := []rune(s)
	var out []string
	start := 0
	for i := 1; i < len(r); i++ {
		prev, cur := r[i-1], r[i]
		boundary := unicode.IsLower(prev) && unicode.IsUpper(cur)
		if unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(r) && unicode.IsLower(r[i+1]) {
			boundary = true
		}
		if unicode.IsDigit(prev) && unicode.IsUpper(cur) {
			boundary = true
		}
		if boundary {
			out = append(out, string(r[start:i]))
			start = i
		}
	}
	return append(out, string(r[start:]))
}

// aceDeclaresSubtests reports whether a test body contains any `X.Run(...)` call.
// A function that drives subtests is the sanctioned TestType_Method exception:
// the parent names the unit under test and the subtest strings carry the
// sentences, so its own name is not required to be one.
func aceDeclaresSubtests(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "Run" && len(call.Args) >= 1 {
			found = true
			return false
		}
		return true
	})
	return found
}

// aceIsSliceLit reports whether a composite literal is a slice or array literal.
// This is the discriminator that separates a table-driven case set from an
// ordinary struct literal: a table is a slice whose elements carry a name field,
// whereas `world.Location{Name: "Test"}` and `CommandEntryConfig{Name: "test"}`
// are domain fixtures that merely happen to have a field spelled the same way.
// Without this restriction the label check fires on dozens of fixtures whose
// values are data, not descriptions.
func aceIsSliceLit(c *ast.CompositeLit) bool {
	_, ok := c.Type.(*ast.ArrayType)
	return ok
}

// aceLabelIsPlaceholder reports whether a label is a bare placeholder.
func aceLabelIsPlaceholder(s string) bool {
	_, bad := acePlaceholderLabels[strings.ToLower(strings.TrimSpace(s))]
	return bad
}

// aceStringLit returns the unquoted value of a string literal expression.
func aceStringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	val, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return val, true
}

// aceWalkTestFiles parses every Go test file in the repository except this one
// and invokes visit with its parsed syntax tree.
func aceWalkTestFiles(t *testing.T, visit func(rel string, fset *token.FileSet, f *ast.File)) {
	t.Helper()
	root := findRepoRoot(t)

	rootFS, err := os.OpenRoot(root)
	require.NoError(t, err, "open repo root")
	defer func() { _ = rootFS.Close() }()

	seen := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == aceSelfPath {
			return nil
		}
		fh, openErr := rootFS.Open(rel)
		if openErr != nil {
			return openErr
		}
		src, readErr := io.ReadAll(fh)
		_ = fh.Close()
		if readErr != nil {
			return readErr
		}
		fset := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fset, rel, src, 0)
		if parseErr != nil {
			return parseErr
		}
		seen++
		visit(rel, fset, parsed)
		return nil
	})
	require.NoError(t, err, "walk repo for test files")

	// A walk that silently matched nothing would make every assertion below
	// vacuous, so the file count is asserted rather than assumed.
	require.Greater(t, seen, 500,
		"expected the walk to reach the repository's test files; got %d", seen)
}

// TestACENamingRatchetFindsNoTopicStyleTestNames fails when a test declaration's
// name ends in a bare single-word topic instead of an expectation clause.
func TestACENamingRatchetFindsNoTopicStyleTestNames(t *testing.T) {
	t.Parallel()

	var violations []aceViolation
	checked := 0

	aceWalkTestFiles(t, func(rel string, fset *token.FileSet, f *ast.File) {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			name := fn.Name.Name
			if !strings.HasPrefix(name, "Test") || name == "Test" {
				continue
			}
			// Only `func TestX(t *testing.T)` — not fuzz targets or helpers.
			if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
				continue
			}
			checked++

			if strings.HasPrefix(name, aceInvariantBindingPrefix) {
				continue
			}
			if _, pinned := acePinnedNames[name]; pinned {
				continue
			}
			segments := strings.Split(name, "_")
			if len(segments) < 2 {
				continue // no underscore: nothing for this predicate to judge
			}
			if aceDeclaresSubtests(fn) {
				continue // sanctioned TestType_Method-with-subtests exception
			}
			tail := segments[len(segments)-1]
			if len(aceSplitCamelWords(tail)) > 1 {
				continue
			}
			violations = append(violations, aceViolation{
				file: rel, line: fset.Position(fn.Pos()).Line,
				detail: name + " — final segment " + strconv.Quote(tail) +
					" is a single word, so the name states a topic and no expectation",
			})
		}
	})

	require.Greater(t, checked, 1000,
		"expected to inspect the repository's test declarations; got %d", checked)

	if len(violations) > 0 {
		sort.Slice(violations, func(i, j int) bool {
			if violations[i].file != violations[j].file {
				return violations[i].file < violations[j].file
			}
			return violations[i].line < violations[j].line
		})
		lines := make([]string, 0, len(violations))
		for _, v := range violations {
			lines = append(lines, "  "+v.String())
		}
		t.Fatalf("%d test name(s) end in a bare topic rather than an expectation.\n"+
			"A test name MUST state an action, a condition and an expectation — see %s.\n"+
			"Rename the test; do NOT add an exemption.\n%s",
			len(violations), aceConventionRule, strings.Join(lines, "\n"))
	}
}

// TestACENamingRatchetFindsNoVaguePlaceholderSubtestLabels fails when a subtest
// or table-case label is a bare placeholder that describes no behaviour.
func TestACENamingRatchetFindsNoVaguePlaceholderSubtestLabels(t *testing.T) {
	t.Parallel()

	var violations []aceViolation
	labels := 0

	aceWalkTestFiles(t, func(rel string, fset *token.FileSet, f *ast.File) {
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.CallExpr:
				sel, ok := v.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Run" || len(v.Args) == 0 {
					return true
				}
				s, ok := aceStringLit(v.Args[0])
				if !ok {
					return true // dynamic label, e.g. t.Run(tt.name, ...)
				}
				labels++
				if aceLabelIsPlaceholder(s) {
					violations = append(violations, aceViolation{
						file: rel, line: fset.Position(v.Args[0].Pos()).Line,
						detail: "subtest label " + strconv.Quote(s) + " names no behaviour",
					})
				}
			case *ast.CompositeLit:
				if !aceIsSliceLit(v) {
					return true
				}
				for _, el := range v.Elts {
					inner, ok := el.(*ast.CompositeLit)
					if !ok {
						continue
					}
					for _, e := range inner.Elts {
						kv, ok := e.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						key, ok := kv.Key.(*ast.Ident)
						if !ok {
							continue
						}
						switch strings.ToLower(key.Name) {
						case "name", "desc", "description":
						default:
							continue
						}
						s, ok := aceStringLit(kv.Value)
						if !ok {
							continue
						}
						labels++
						if aceLabelIsPlaceholder(s) {
							violations = append(violations, aceViolation{
								file: rel, line: fset.Position(kv.Value.Pos()).Line,
								detail: "table-case label " + strconv.Quote(s) + " names no behaviour",
							})
						}
					}
				}
			}
			return true
		})
	})

	require.Greater(t, labels, 500,
		"expected to inspect the repository's subtest and table-case labels; got %d", labels)

	if len(violations) > 0 {
		sort.Slice(violations, func(i, j int) bool {
			if violations[i].file != violations[j].file {
				return violations[i].file < violations[j].file
			}
			return violations[i].line < violations[j].line
		})
		lines := make([]string, 0, len(violations))
		for _, v := range violations {
			lines = append(lines, "  "+v.String())
		}
		t.Fatalf("%d subtest/table-case label(s) describe no behaviour.\n"+
			"A subtest label MUST be a lowercase sentence describing what the case exercises — see %s.\n%s",
			len(violations), aceConventionRule, strings.Join(lines, "\n"))
	}
}
