// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This is the CHARACTER-NAME ADMISSION CENSUS (Phase 2 §12.1 rule 1).
//
// # What it is for
//
// It is the COMPLEMENT of world_sql_fence_test.go, not a duplicate. The fence
// proves that no `characters` mutation SQL exists OUTSIDE internal/world/postgres.
// This census proves that INSIDE that boundary, every write of characters.name
// is gated by an admission token, writes its three derived identity columns in
// the same statement, and serializes on the skeleton. Neither is sufficient
// alone, and both must be green.
//
// # What it deliberately does NOT do
//
// It is a BACKSTOP, not the primary mechanism. The primary mechanism is the
// type: charname.Admitted has exactly one constructor — (*charname.Gate).Admit —
// so a name write that skipped the gate is not expressible in Go. A census that
// must be kept honest is weaker than a shape that cannot be wrong. The census
// exists because the shape still leaves freedom in three places the compiler
// cannot see: whether the identity columns travel with the name, whether the
// write serializes, and whether the set of name-writing methods grew.
//
// The previous shape of this census scanned SQL string literals alone, and
// could not work: a Phase 3 RenameCharacter that reused the pre-existing
// CharacterRepository.Update would introduce NO new SQL literal, so the census
// would stay green while the new write site bypassed the gate — exactly the
// case it exists to catch.
//
// # The Phase 3 obligation
//
// RenameCharacter MUST obtain its charname.Admitted from the gate. Rule D turns
// RED if a third name-admitting method appears in the writer boundary without a
// deliberate update to expectedNameAdmittingMethods below.
//
// # Scanned file set
//
// All five rules scan .go files ONLY, excluding _test.go and excluding
// generated files — mirroring the fence's own Go scan
// (world_sql_fence_test.go:211 and :222). That filter is deliberate, not
// incidental: character_repo_test.go's stale-version CAS fixture may issue a
// direct versioned UPDATE from a test function that takes no charname.Admitted,
// and a rule A that scanned _test.go would go RED on a legitimate fixture. The
// cheap responses to that RED — widening the rule, or deleting the fixture —
// both cost more than they save.

// admissionWriterBoundaryDir is the sanctioned writer boundary: the only place
// characters mutation SQL may live (world_sql_fence_test.go proves that half).
const admissionWriterBoundaryDir = "internal/world/postgres"

// admissionCharnameDir is the package tree that owns the admission token.
const admissionCharnameDir = "internal/charname"

// admittedTypeName is the admission token's type name.
const admittedTypeName = "Admitted"

// skeletonGuardHelper is the writer boundary's skeleton-serialization helper —
// the one that takes the transaction-scoped advisory lock and re-checks
// skeleton non-collision inside the write transaction (D-30 part 2).
const skeletonGuardHelper = "guardSkeleton"

// expectedNameAdmittingMethods is rule D's checked-in expected set: the
// internal/world/postgres methods that take a charname.Admitted.
//
// Adding a member here is a deliberate act. Rule D compares by SET EQUALITY, so
// removing a member is RED and adding an unlisted name-admitting method is also
// RED — inequality in EITHER direction fails.
var expectedNameAdmittingMethods = []string{
	"(*CharacterRepository).Create",
	"(*CharacterRepository).Rename",
}

var (
	// charInsertRe matches an INSERT INTO characters and captures its column list.
	charInsertRe = regexp.MustCompile(`(?is)\binsert\s+into\s+(?:public\.)?characters\s*\(([^)]*)\)`)
	// charUpdateHeadRe matches the head of an UPDATE characters SET statement.
	charUpdateHeadRe = regexp.MustCompile(`(?is)\bupdate\s+(?:public\.)?characters\s+set\s+`)
	// admissionWhereRe bounds an UPDATE's SET clause.
	admissionWhereRe = regexp.MustCompile(`(?i)\bwhere\b`)
	// bareNameColumnRe matches the name column on a word boundary, so
	// normalized_name and name_skeleton never false-positive: `_` is a word
	// character, so neither carries a boundary adjacent to `name`.
	bareNameColumnRe = regexp.MustCompile(`\bname\b`)
	// nameAssignRe matches an assignment to the name column in a SET clause.
	nameAssignRe = regexp.MustCompile(`\bname\s*=`)
)

// identityColumns are the three derived columns that MUST travel with every
// characters.name write. Each is matched on a word boundary, so name_skeleton
// does not satisfy name_skeleton_unicode_version.
var identityColumns = []string{
	"normalized_name",
	"name_skeleton",
	"name_skeleton_unicode_version",
}

// writesCharacterName reports whether a SQL string writes the name column of
// characters — either an INSERT INTO characters whose column list contains
// name, or an UPDATE characters SET assigning name.
//
// A statement writing only the three DERIVED identity columns is the backfill
// shape and is deliberately NOT a name write: the rule is one-directional.
func writesCharacterName(sql string) bool {
	if m := charInsertRe.FindStringSubmatch(sql); m != nil && bareNameColumnRe.MatchString(m[1]) {
		return true
	}
	loc := charUpdateHeadRe.FindStringIndex(sql)
	if loc == nil {
		return false
	}
	setClause := sql[loc[1]:]
	if w := admissionWhereRe.FindStringIndex(setClause); w != nil {
		setClause = setClause[:w[0]]
	}
	return nameAssignRe.MatchString(setClause)
}

// missingIdentityColumns returns the identity columns a name-writing statement
// fails to write in the SAME statement (rule B).
func missingIdentityColumns(sql string) []string {
	var missing []string
	for _, col := range identityColumns {
		if !regexp.MustCompile(`\b` + col + `\b`).MatchString(sql) {
			missing = append(missing, col)
		}
	}
	return missing
}

// admissionFuncFacts is what the census derives from ONE function declaration.
type admissionFuncFacts struct {
	// Key is the census identity: "(*CharacterRepository).Create" for a pointer
	// method, "(Gate).Check" for a value method, "Normalize" for a free function.
	Key string
	// File is the repo-relative path the declaration lives in.
	File string
	// HasAdmittedParam reports whether any parameter names charname.Admitted.
	HasAdmittedParam bool
	// ReturnsAdmitted reports whether any RESULT names charname.Admitted (rule C).
	ReturnsAdmitted bool
	// IsMethod reports whether the declaration has a receiver. Rule D is a
	// census of METHODS: the package's guardSkeleton helper also takes a token,
	// but it READS the skeleton to serialize the write and writes nothing
	// itself, so it is not a name-admitting method. Rules A and E still cover
	// it — they key on functions that write characters.name, receiver or not.
	IsMethod bool
	// SQLLiterals are the string literals appearing anywhere in the body.
	SQLLiterals []string
	// CalledFuncs are the names of functions called anywhere in the body.
	CalledFuncs []string
}

// namesAdmittedType reports whether a type expression names the admission
// token, in either the qualified form used outside internal/charname
// (charname.Admitted) or the bare form used inside it (Admitted).
func namesAdmittedType(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return namesAdmittedType(e.X)
	case *ast.Ident:
		return e.Name == admittedTypeName
	case *ast.SelectorExpr:
		pkg, ok := e.X.(*ast.Ident)
		return ok && pkg.Name == "charname" && e.Sel.Name == admittedTypeName
	}
	return false
}

// funcDeclKey renders a declaration's census identity.
func funcDeclKey(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	var buf strings.Builder
	buf.WriteString("(")
	switch rt := fn.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		buf.WriteString("*")
		if id, ok := rt.X.(*ast.Ident); ok {
			buf.WriteString(id.Name)
		}
	case *ast.Ident:
		buf.WriteString(rt.Name)
	}
	buf.WriteString(").")
	buf.WriteString(fn.Name.Name)
	return buf.String()
}

// scanGoForAdmissionFacts parses Go source and returns one record per function
// declaration. It walks the AST and inspects only declarations, string literals
// and call expressions — comments and identifiers in prose are never considered
// (the parse-Go-not-grep property the fence establishes).
func scanGoForAdmissionFacts(t *testing.T, filename string, src []byte) []admissionFuncFacts {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	require.NoError(t, err, "parse %s", filename)

	var out []admissionFuncFacts
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		facts := admissionFuncFacts{
			Key:      funcDeclKey(fn),
			File:     filename,
			IsMethod: fn.Recv != nil && len(fn.Recv.List) > 0,
		}

		if fn.Type.Params != nil {
			for _, p := range fn.Type.Params.List {
				if namesAdmittedType(p.Type) {
					facts.HasAdmittedParam = true
				}
			}
		}
		if fn.Type.Results != nil {
			for _, r := range fn.Type.Results.List {
				if namesAdmittedType(r.Type) {
					facts.ReturnsAdmitted = true
				}
			}
		}
		if fn.Body != nil {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BasicLit:
					if node.Kind == token.STRING {
						val, uErr := strconv.Unquote(node.Value)
						if uErr != nil {
							val = node.Value
						}
						facts.SQLLiterals = append(facts.SQLLiterals, val)
					}
				case *ast.CallExpr:
					switch callee := node.Fun.(type) {
					case *ast.Ident:
						facts.CalledFuncs = append(facts.CalledFuncs, callee.Name)
					case *ast.SelectorExpr:
						facts.CalledFuncs = append(facts.CalledFuncs, callee.Sel.Name)
					}
				}
				return true
			})
		}
		out = append(out, facts)
	}
	return out
}

// admissionCensusScansFile reports whether a file is in the census's scanned
// set: .go, not _test.go, not generated. Stated as a predicate rather than left
// implicit in a walk, so a control can assert it.
func admissionCensusScansFile(path string, src []byte) bool {
	if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
		return false
	}
	return !isGeneratedGo(src)
}

// collectAdmissionFacts walks a repo-relative directory tree and returns the
// facts for every function in the scanned file set.
func collectAdmissionFacts(t *testing.T, root, dir string) []admissionFuncFacts {
	t.Helper()
	var out []admissionFuncFacts
	walkErr := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		//nolint:gosec // G304: this meta-test walks the trusted in-repo source tree; path is repo-derived, not untrusted input.
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !admissionCensusScansFile(path, src) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, scanGoForAdmissionFacts(t, filepath.ToSlash(rel), src)...)
		return nil
	})
	require.NoError(t, walkErr)
	return out
}

// nameWritingFuncs returns the writer-boundary functions whose body contains at
// least one statement writing characters.name, paired with those statements.
func nameWritingFuncs(facts []admissionFuncFacts) map[string][]string {
	out := map[string][]string{}
	for _, f := range facts {
		for _, lit := range f.SQLLiterals {
			if writesCharacterName(lit) {
				out[f.Key] = append(out[f.Key], lit)
			}
		}
	}
	return out
}

// symmetricDiff returns the members of got missing from want and vice versa.
func symmetricDiff(got, want []string) (missing, unexpected []string) {
	inWant := map[string]bool{}
	for _, w := range want {
		inWant[w] = true
	}
	inGot := map[string]bool{}
	for _, g := range got {
		inGot[g] = true
	}
	for _, w := range want {
		if !inGot[w] {
			missing = append(missing, w)
		}
	}
	for _, g := range got {
		if !inWant[g] {
			unexpected = append(unexpected, g)
		}
	}
	sort.Strings(missing)
	sort.Strings(unexpected)
	return missing, unexpected
}

// --- Rule A: every characters.name write is GATED ---

// TestEveryCharacterNameWriteInTheWriterBoundaryTakesAnAdmissionToken is rule A.
func TestEveryCharacterNameWriteInTheWriterBoundaryTakesAnAdmissionToken(t *testing.T) {
	facts := collectAdmissionFacts(t, findRepoRoot(t), admissionWriterBoundaryDir)
	writers := nameWritingFuncs(facts)
	require.NotEmpty(t, writers,
		"non-vacuity: the writer boundary must contain at least one characters.name write, "+
			"or this rule passes by scanning nothing")

	byKey := map[string]admissionFuncFacts{}
	for _, f := range facts {
		byKey[f.Key] = f
	}

	var violations []string
	for key := range writers {
		if !byKey[key].HasAdmittedParam {
			violations = append(violations, byKey[key].File+": "+key)
		}
	}
	sort.Strings(violations)
	assert.Empty(t, violations,
		"every function writing characters.name must take a charname.Admitted — "+
			"the token is the only proof the name ran the gate")
}

// --- Rule B: every characters.name write is IDENTITY-COHERENT ---

// TestEveryCharacterNameWriteAlsoWritesItsThreeIdentityColumns is rule B.
func TestEveryCharacterNameWriteAlsoWritesItsThreeIdentityColumns(t *testing.T) {
	facts := collectAdmissionFacts(t, findRepoRoot(t), admissionWriterBoundaryDir)
	writers := nameWritingFuncs(facts)
	require.NotEmpty(t, writers, "non-vacuity: at least one characters.name write must exist")

	var violations []string
	for key, stmts := range writers {
		for _, stmt := range stmts {
			if missing := missingIdentityColumns(stmt); len(missing) > 0 {
				violations = append(violations, key+" omits "+strings.Join(missing, ", "))
			}
		}
	}
	sort.Strings(violations)
	assert.Empty(t, violations,
		"a name write that leaves normalized_name stale silently defeats the UNIQUE index over it — "+
			"all four identity columns travel in ONE statement")
}

// --- Rule C: exactly ONE constructor ---

// TestAdmittedHasExactlyOneConstructorUnderInternalCharname is rule C. It is the
// AUTHORITATIVE form of the single-constructor guarantee: a grep for
// constructor-shaped names can only catch the ones someone thought to enumerate.
func TestAdmittedHasExactlyOneConstructorUnderInternalCharname(t *testing.T) {
	facts := collectAdmissionFacts(t, findRepoRoot(t), admissionCharnameDir)

	var constructors []string
	for _, f := range facts {
		if f.ReturnsAdmitted {
			constructors = append(constructors, f.Key)
		}
	}
	sort.Strings(constructors)

	assert.Equal(t, []string{"(*Gate).Admit"}, constructors,
		"charname.Admitted's guarantee IS its single constructor; a FromString, an Unchecked, "+
			"or an exported field would void the whole mechanism at zero apparent cost")
}

// --- Rule D: SET EQUALITY over the name-admitting method set ---

// TestTheSetOfNameAdmittingWriterBoundaryMethodsEqualsTheExpectedSet is rule D.
func TestTheSetOfNameAdmittingWriterBoundaryMethodsEqualsTheExpectedSet(t *testing.T) {
	facts := collectAdmissionFacts(t, findRepoRoot(t), admissionWriterBoundaryDir)

	var derived []string
	for _, f := range facts {
		if f.HasAdmittedParam && f.IsMethod {
			derived = append(derived, f.Key)
		}
	}
	sort.Strings(derived)

	missing, unexpected := symmetricDiff(derived, expectedNameAdmittingMethods)
	assert.Empty(t, missing,
		"expected name-admitting methods are absent from the tree: %v", missing)
	assert.Empty(t, unexpected,
		"the tree grew a name-admitting method not in expectedNameAdmittingMethods: %v — "+
			"Phase 3's RenameCharacter must obtain its token from the gate, and this list "+
			"must be updated deliberately", unexpected)
}

// --- Rule E: every characters.name write is SERIALIZED ---

// TestEveryCharacterNameWriteCallsTheSkeletonSerializationHelper is rule E, the
// census half of D-30 part 2. Without it a future third name-writing method can
// satisfy rules A, B and D — token parameter, four columns, listed in the
// expected set — while performing an unserialized write.
func TestEveryCharacterNameWriteCallsTheSkeletonSerializationHelper(t *testing.T) {
	facts := collectAdmissionFacts(t, findRepoRoot(t), admissionWriterBoundaryDir)
	writers := nameWritingFuncs(facts)
	require.NotEmpty(t, writers, "non-vacuity: at least one characters.name write must exist")

	byKey := map[string]admissionFuncFacts{}
	for _, f := range facts {
		byKey[f.Key] = f
	}

	var violations []string
	for key := range writers {
		called := false
		for _, c := range byKey[key].CalledFuncs {
			if c == skeletonGuardHelper {
				called = true
				break
			}
		}
		if !called {
			violations = append(violations, byKey[key].File+": "+key)
		}
	}
	sort.Strings(violations)
	assert.Empty(t, violations,
		"the skeleton index is deliberately non-unique, so nothing at the storage layer "+
			"serializes the check against the write — every name write must call %s",
		skeletonGuardHelper)
}

// --- Synthetic-source positive controls, one per rule ---

// TestAdmissionCensusFlagsANameWriteWithNoAdmissionToken is rule A's control.
func TestAdmissionCensusFlagsANameWriteWithNoAdmissionToken(t *testing.T) {
	src := []byte(`package fixture

func ungated(ctx context.Context, id, name string) error {
	q := "UPDATE characters SET name = $2, normalized_name = $3, name_skeleton = $4, name_skeleton_unicode_version = $5 WHERE id = $1"
	guardSkeleton(ctx)
	_ = q
	return nil
}
`)
	facts := scanGoForAdmissionFacts(t, "fixture.go", src)
	require.Len(t, facts, 1)
	assert.NotEmpty(t, nameWritingFuncs(facts), "the fixture must be recognised as a name write")
	assert.False(t, facts[0].HasAdmittedParam,
		"rule A must flag a writer-boundary name write with no charname.Admitted parameter")
}

// TestAdmissionCensusFlagsANameWriteMissingItsIdentityColumns is rule B's control.
func TestAdmissionCensusFlagsANameWriteMissingItsIdentityColumns(t *testing.T) {
	src := []byte(`package fixture

func incoherent(ctx context.Context, name charname.Admitted) error {
	q := "UPDATE characters SET name = $2 WHERE id = $1"
	guardSkeleton(ctx)
	_ = q
	return nil
}
`)
	facts := scanGoForAdmissionFacts(t, "fixture.go", src)
	require.Len(t, facts, 1)
	assert.True(t, facts[0].HasAdmittedParam, "the fixture is gated — only rule B must flag it")

	writers := nameWritingFuncs(facts)
	require.Contains(t, writers, "incoherent")
	assert.ElementsMatch(t,
		[]string{"normalized_name", "name_skeleton", "name_skeleton_unicode_version"},
		missingIdentityColumns(writers["incoherent"][0]),
		"rule B must flag a name write that leaves the derived identity columns stale")
}

// TestAdmissionCensusFlagsANameWriteThatDoesNotSerializeOnTheSkeleton is rule E's
// control: gated, identity-coherent, and still wrong.
func TestAdmissionCensusFlagsANameWriteThatDoesNotSerializeOnTheSkeleton(t *testing.T) {
	src := []byte(`package fixture

func unserialized(ctx context.Context, name charname.Admitted) error {
	q := "INSERT INTO characters (id, name, normalized_name, name_skeleton, name_skeleton_unicode_version) VALUES ($1, $2, $3, $4, $5)"
	_ = q
	return nil
}
`)
	facts := scanGoForAdmissionFacts(t, "fixture.go", src)
	require.Len(t, facts, 1)
	assert.True(t, facts[0].HasAdmittedParam)
	assert.NotEmpty(t, nameWritingFuncs(facts))
	assert.Empty(t, missingIdentityColumns(facts[0].SQLLiterals[0]))
	assert.NotContains(t, facts[0].CalledFuncs, skeletonGuardHelper,
		"rule E must flag a gated, identity-coherent name write that never serializes")
}

// TestAdmissionCensusFlagsASecondAdmittedConstructor is rule C's control.
func TestAdmissionCensusFlagsASecondAdmittedConstructor(t *testing.T) {
	src := []byte(`package charname

func Unchecked(s string) Admitted {
	return Admitted{display: s}
}

func (g *Gate) Admit(ctx context.Context, s string) (Admitted, error) {
	return Admitted{}, nil
}
`)
	facts := scanGoForAdmissionFacts(t, "fixture.go", src)

	var constructors []string
	for _, f := range facts {
		if f.ReturnsAdmitted {
			constructors = append(constructors, f.Key)
		}
	}
	sort.Strings(constructors)

	assert.Equal(t, []string{"(*Gate).Admit", "Unchecked"}, constructors,
		"rule C must see a convenience escape hatch as a SECOND constructor, "+
			"whatever it is named")
}

// TestAdmissionCensusRuleDFlagsBothDirectionsOfSetInequality is rule D's control.
func TestAdmissionCensusRuleDFlagsBothDirectionsOfSetInequality(t *testing.T) {
	want := []string{"(*CharacterRepository).Create", "(*CharacterRepository).Rename"}

	missing, unexpected := symmetricDiff([]string{"(*CharacterRepository).Create"}, want)
	assert.Equal(t, []string{"(*CharacterRepository).Rename"}, missing,
		"a removed name-admitting method must be RED")
	assert.Empty(t, unexpected)

	missing, unexpected = symmetricDiff(
		append(append([]string{}, want...), "(*CharacterRepository).Adopt"), want,
	)
	assert.Empty(t, missing)
	assert.Equal(t, []string{"(*CharacterRepository).Adopt"}, unexpected,
		"an unlisted name-admitting method must be RED")
}

// TestAdmissionCensusDoesNotFlagTheBackfillShapeOrReadsOrComments proves the
// rules are one-directional and parse-Go-not-grep.
func TestAdmissionCensusDoesNotFlagTheBackfillShapeOrReadsOrComments(t *testing.T) {
	src := []byte(`package fixture

// This comment mentions UPDATE characters SET name = $1 but is not a literal.
func backfill(ctx context.Context) error {
	// The backfill shape: the three DERIVED columns, computed from the row's
	// existing name, with no name write of its own.
	upd := "UPDATE characters SET normalized_name = $2, name_skeleton = $3, name_skeleton_unicode_version = $4 WHERE id = $1"
	read := "SELECT id, name FROM characters WHERE name_skeleton IS NULL"
	_, _ = upd, read
	return nil
}
`)
	facts := scanGoForAdmissionFacts(t, "fixture.go", src)
	assert.Empty(t, nameWritingFuncs(facts),
		"the backfill shape, a SELECT, and comment text must never be flagged as name writes")
}

// TestAdmissionCensusScannedFileSetExcludesTestAndGeneratedFiles proves the
// scanned set is what the doc comment says. A census that goes RED on a
// legitimate test fixture is a census someone will widen.
func TestAdmissionCensusScannedFileSetExcludesTestAndGeneratedFiles(t *testing.T) {
	testFixture := []byte(`package postgres

func seedDirectly(ctx context.Context, id, name string) {
	q := "UPDATE characters SET name = $2 WHERE id = $1"
	_ = q
}
`)
	// The fixture IS a name write with no token — it is excluded by PATH, not
	// by failing to match, which is what makes the exclusion load-bearing.
	facts := scanGoForAdmissionFacts(t, "character_repo_test.go", testFixture)
	assert.NotEmpty(t, nameWritingFuncs(facts))
	assert.False(t, facts[0].HasAdmittedParam)

	assert.False(t, admissionCensusScansFile("internal/world/postgres/character_repo_test.go", testFixture),
		"_test.go files are outside the scanned set, exactly as they are for the fence")
	assert.False(t, admissionCensusScansFile("internal/world/worldtest/mock_CharacterRepository.go",
		[]byte("// Code generated by mockery. DO NOT EDIT.\npackage worldtest\n")),
		"generated files are outside the scanned set")
	assert.True(t, admissionCensusScansFile("internal/world/postgres/character_repo.go",
		[]byte("package postgres\n")))
	assert.False(t, admissionCensusScansFile("internal/world/postgres/schema.sql", nil))
}
