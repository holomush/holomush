// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migrationsDirName is the directory, relative to this package, holding the
// migration chain. A `package store` test runs with its working directory set to
// the package directory, so the plain relative name resolves.
const migrationsDirName = "migrations"

// migrationsImportPath is the package the blank import in migrations_register.go
// names. Go migrations register via init() side effects, so this import is the
// only thing that puts them into the chain.
const migrationsImportPath = "github.com/holomush/holomush/internal/store/migrations"

// goosePackagePath is goose's module path, used to resolve the file-local name
// of the goose import rather than assuming the identifier is spelled "goose".
const goosePackagePath = "github.com/pressly/goose/v3"

// versionedGoMigrationName matches a Go migration file: the same NNNNNN_name
// shape the SQL corpus uses, with a .go suffix. goose derives the version from
// runtime.Caller(1)'s filename, so the name IS the version declaration.
//
// Files that do not match — doc.go above all — are not migrations and carry no
// registration obligation.
var versionedGoMigrationName = regexp.MustCompile(`^\d{6}_[A-Za-z0-9_]+\.go$`)

// gooseNoTxMarker matches the `// goose-no-tx: <reason>` comment D-10 requires
// beside any non-transactional registration. The trailing \S is what makes a
// bare `// goose-no-tx:` with no reason fail to satisfy the requirement.
var gooseNoTxMarker = regexp.MustCompile(`(?m)^\s*//\s*goose-no-tx:\s*\S`)

// goMigrationRegistrationViolations reports every reason a Go migration file
// would be silently absent from, or unsafely present in, the goose chain. It
// returns one human-readable string per problem and nil when the file is clean.
//
// It is pure (name + body in, violation strings out) for the same reason
// statementBlockViolations is: the corpus it guards contains ZERO versioned .go
// files today, so a corpus-only check would be vacuous. The synthetic table
// below is what proves the guard works; the corpus walk is what applies it.
//
// Two obligations are enforced:
//
//  1. A versioned .go file MUST register itself in init(). goose's own
//     unregistered-Go guard cannot cover this, because it depends on the file
//     being visible to goose's source collector and the parent package embeds
//     `migrations/*.sql` only.
//  2. A file registering via AddMigrationNoTxContext MUST carry a
//     `// goose-no-tx: <reason>` comment (D-10).
//
// Detection is AST-based rather than textual so that a registration inside a
// comment, or one in a function that is not init(), is correctly read as absent.
func goMigrationRegistrationViolations(name, body string) []string {
	if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
		return nil
	}
	if !versionedGoMigrationName.MatchString(name) {
		// doc.go and any other un-prefixed file: not a migration, no obligation.
		return nil
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, body, parser.ParseComments)
	if err != nil {
		return []string{fmt.Sprintf(
			"%s: does not parse as Go (%v) — a versioned migration that cannot be parsed "+
				"cannot be verified to register itself", name, err,
		)}
	}

	registrations := gooseRegistrationsInInit(file)

	var violations []string
	if len(registrations) == 0 {
		violations = append(violations, fmt.Sprintf(
			"%s: no goose.AddMigrationContext registration in init() — a Go migration joins the "+
				"chain only when its init() registers it, and `//go:embed migrations/*.sql` never "+
				"embeds .go, so goose's own unregistered-Go guard cannot fire here. This migration "+
				"would be SILENTLY absent: correct version counts, consistent schema, green suite, "+
				"and the Go step simply never ran. See INV-STORE-11.", name,
		))
	}

	for _, fn := range registrations {
		if !strings.Contains(fn, "NoTx") {
			continue
		}
		if !gooseNoTxMarker.MatchString(body) {
			violations = append(violations, fmt.Sprintf(
				"%s: registers with goose.%s but carries no `// goose-no-tx: <reason>` comment "+
					"naming the statement that forbids a transaction (D-10). This is a correctness "+
					"requirement, not a style rule: a migration that must roll back and leave the "+
					"schema at the prior version on failure only does so inside a transaction.",
				name, fn,
			))
		}
	}

	return violations
}

// gooseRegistrationsInInit returns the names of the goose Add*Migration*
// functions actually CALLED from a func init() in file.
//
// Both halves matter. Restricting to init() rejects a registration parked in a
// helper nothing calls; requiring a real *ast.CallExpr rejects a registration
// that has been commented out — the shape a textual scan reports as present.
func gooseRegistrationsInInit(file *ast.File) []string {
	local := gooseLocalName(file)
	if local == "" {
		return nil // goose is not imported, so nothing here can be registering.
	}

	var found []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "init" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall {
				return true
			}
			sel, isSel := call.Fun.(*ast.SelectorExpr)
			if !isSel {
				return true
			}
			pkg, isIdent := sel.X.(*ast.Ident)
			if !isIdent || pkg.Name != local {
				return true
			}
			if strings.HasPrefix(sel.Sel.Name, "AddMigration") ||
				strings.HasPrefix(sel.Sel.Name, "AddNamedMigration") {
				found = append(found, sel.Sel.Name)
			}
			return true
		})
	}
	return found
}

// gooseLocalName returns the identifier goose is bound to in file, honouring an
// import alias, or "" when goose is not imported at all.
func gooseLocalName(file *ast.File) string {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != goosePackagePath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "goose"
	}
	return ""
}

// TestGoMigrationRegistrationViolationsFlagsUnregisteredAndUnjustifiedNoTxFiles
// drives the pure scanner with synthetic bodies.
//
// These cases — not the corpus walk — are what demonstrate the guard can fail.
// Phase 2 adds the first versioned .go file; until then the corpus contains
// none, so every corpus assertion is trivially satisfied and proves nothing on
// its own.
func TestGoMigrationRegistrationViolationsFlagsUnregisteredAndUnjustifiedNoTxFiles(t *testing.T) {
	const imports = "package migrations\n\n" +
		"import (\n\t\"context\"\n\t\"database/sql\"\n\n\t\"github.com/pressly/goose/v3\"\n)\n\n"
	const bodies = "func upBackfill(ctx context.Context, tx *sql.Tx) error { return nil }\n" +
		"func downBackfill(ctx context.Context, tx *sql.Tx) error { return nil }\n"
	const noTxBodies = "func upIndex(ctx context.Context, db *sql.DB) error { return nil }\n" +
		"func downIndex(ctx context.Context, db *sql.DB) error { return nil }\n"

	tests := []struct {
		name      string
		file      string
		body      string
		wantCount int
		wantIn    string
	}{
		{
			name:      "accepts a versioned file whose init registers with AddMigrationContext",
			file:      "000055_backfill_names.go",
			body:      imports + "func init() {\n\tgoose.AddMigrationContext(upBackfill, downBackfill)\n}\n\n" + bodies,
			wantCount: 0,
		},
		{
			name:      "flags the same file once its init registration is removed",
			file:      "000055_backfill_names.go",
			body:      imports + bodies,
			wantCount: 1,
			wantIn:    "never embeds .go",
		},
		{
			name: "flags a registration that has been commented out rather than called",
			file: "000055_backfill_names.go",
			body: imports +
				"func init() {\n\t// goose.AddMigrationContext(upBackfill, downBackfill)\n}\n\n" + bodies,
			wantCount: 1,
			wantIn:    "no goose.AddMigrationContext registration in init()",
		},
		{
			name: "accepts AddMigrationNoTxContext when a goose-no-tx reason is present",
			file: "000056_concurrent_index.go",
			body: imports +
				"func init() {\n\t// goose-no-tx: CREATE INDEX CONCURRENTLY cannot run in a transaction block.\n" +
				"\tgoose.AddMigrationNoTxContext(upIndex, downIndex)\n}\n\n" + noTxBodies,
			wantCount: 0,
		},
		{
			name: "flags AddMigrationNoTxContext with no goose-no-tx reason",
			file: "000056_concurrent_index.go",
			body: imports +
				"func init() {\n\tgoose.AddMigrationNoTxContext(upIndex, downIndex)\n}\n\n" + noTxBodies,
			wantCount: 1,
			wantIn:    "goose-no-tx",
		},
		{
			name:      "exempts doc.go, which carries no version prefix and registers nothing",
			file:      "doc.go",
			body:      "// Package migrations holds the migration chain.\npackage migrations\n",
			wantCount: 0,
		},
		{
			name:      "skips _test.go files entirely",
			file:      "helpers_test.go",
			body:      "package migrations\n\nfunc helper() {}\n",
			wantCount: 0,
		},
		{
			name:      "flags a versioned file that does not parse",
			file:      "000057_broken.go",
			body:      "package migrations\n\nfunc init( {\n",
			wantCount: 1,
			wantIn:    "does not parse as Go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := goMigrationRegistrationViolations(tt.file, tt.body)
			require.Len(t, got, tt.wantCount, "violations: %s", strings.Join(got, " | "))
			if tt.wantIn != "" {
				assert.Contains(t, strings.Join(got, "\n"), tt.wantIn)
			}
		})
	}
}

// TestGoMigrationRegistrationHoldsAcrossTheMigrationsCorpus applies the guard to
// the real directory. INV-STORE-11.
//
// HONEST SCOPE: `internal/store/migrations/` currently contains exactly one .go
// file — doc.go, which is deliberately un-prefixed and therefore not a
// migration. The registration clause of this walk is consequently trivially
// satisfied today, and a reader must not mistake it for coverage. Phase 2's
// 000055 is the first file that makes it bite. What the walk does prove now is
// that the directory is readable, is scanned, and contains Go source at all —
// the non-vacuity floor below — so a renamed corpus or an unreadable directory
// cannot be reported as clean.
//
// INV-STORE-11 is registered binding: pending for exactly that reason. No
// `Verifies:` annotation is claimed here until Phase 2's 000055 gives the
// registration clause something it can actually reject.
func TestGoMigrationRegistrationHoldsAcrossTheMigrationsCorpus(t *testing.T) {
	entries, err := os.ReadDir(migrationsDirName)
	require.NoError(t, err, "should read %s/", migrationsDirName)

	goFiles, versionedMigrations := 0, 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		goFiles++
		if versionedGoMigrationName.MatchString(name) {
			versionedMigrations++
		}

		content, readErr := os.ReadFile(filepath.Join(migrationsDirName, name))
		require.NoError(t, readErr, "should read %s", name)

		// assert, not require: one unregistered migration must not hide the rest.
		violations := goMigrationRegistrationViolations(name, string(content))
		assert.Emptyf(t, violations, "Go migration registration violations:\n%s",
			strings.Join(violations, "\n"))
	}

	// Non-vacuity floor. Without this, a renamed directory, a suffix typo, or a
	// scan that matched nothing at all would read as "clean". doc.go guarantees
	// this is at least 1. The versioned count is deliberately NOT asserted
	// positive — it is legitimately 0 until Phase 2 lands the first Go migration,
	// and asserting otherwise would make this test red for the wrong reason.
	assert.Positive(t, goFiles,
		"no .go file found in %s/ — the scan is vacuous, not clean; %s is a Go package (D-08) "+
			"and must contain at least doc.go", migrationsDirName, migrationsDirName)
	t.Logf("scanned %d .go file(s) in %s/, of which %d are versioned migrations",
		goFiles, migrationsDirName, versionedMigrations)
}

// TestExactlyOneBlankImportWiresTheMigrationsPackageIntoStore pins the wiring
// that makes every Go migration's init() run at all.
//
// The corpus guard above checks that each migration registers ITSELF. This
// checks that anything runs those init()s in the first place. Both holes have
// the same signature — silent absence from the chain with a green suite — but
// only this one is reachable by a tidy pass, an unused-import sweep, or an
// editor helpfully removing an import with no referenced symbols.
//
// It is a static AST scan rather than a runtime assertion because there is
// nothing to observe at runtime: with zero Go migrations registered, an intact
// blank import and a deleted one produce byte-identical behaviour.
func TestExactlyOneBlankImportWiresTheMigrationsPackageIntoStore(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err, "should read the store package directory")

	fset := token.NewFileSet()
	var sites []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		require.NoError(t, parseErr, "should parse %s", name)

		for _, imp := range file.Imports {
			path, unqErr := strconv.Unquote(imp.Path.Value)
			if unqErr != nil || path != migrationsImportPath {
				continue
			}
			if imp.Name == nil || imp.Name.Name != "_" {
				t.Errorf("%s imports %s without the blank identifier; the import exists to run "+
					"init() side effects and must stay blank", name, migrationsImportPath)
				continue
			}
			sites = append(sites, name)
		}
	}

	require.Len(t, sites, 1,
		"expected exactly one blank import of %s in package store, found %d (%v). Zero means every "+
			"Go migration's init() stops running and they vanish from the chain SILENTLY — goose "+
			"applies the SQL corpus, counts are correct, and the suite passes.",
		migrationsImportPath, len(sites), sites)
	assert.Equal(t, "migrations_register.go", sites[0],
		"the blank import lives in its own file so its reason is not buried in an import block")
}
