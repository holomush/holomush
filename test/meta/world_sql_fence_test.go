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
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This meta-test is the RAW-WORLD-SQL FENCE (INV-WORLD-WRITER-BOUNDARY, bound in
// 05-12). It enforces that CORE/WORLD-table mutation SQL lives ONLY inside the
// sanctioned writer boundary internal/world/postgres, so no code path escapes the
// version guard (MODEL-03) and the transactional outbox envelope (INV-WORLD-4).
//
// It is a REAL source-scanning fence, NOT a depguard rule: depguard matches
// IMPORTED PACKAGES, not SQL string literals, so it CANNOT express this
// invariant (Codex finding 6). The Go scan parses each file with go/ast and
// inspects STRING LITERALS ONLY — it does not naively grep the whole file, so
// comments and identifiers never false-positive.
//
// Scope (round-4 C5 / D-05): the fenced set is the CORE/WORLD tables only —
// locations, exits, characters, objects, entity_properties, outbox,
// world_feed_counter. scene_participants is EXCLUDED and the plugins/ tree is NOT
// scanned: there are two distinct scene_participants tables —
// plugin_core_scenes.scene_participants (the core-scenes plugin's OWN schema,
// written correctly OUTSIDE the world fence) and vestigial public.scene_participants
// (no live prod writer outside internal/world) — so Codex's round-4 "plugin
// escapes the fence" finding is a schema-blind false positive, REJECTED here.
// Issue #4815 tracks verify-or-remove of the vestigial world scene_repo and any
// future outbox-for-plugin-owned-tables work (NOT Phase 5).
//
// The fence is HONESTLY green against the current tree ONLY because 05-09 Task 2
// folded internal/store/character_settings_repo.go's raw `UPDATE characters SET
// preferences` into internal/world/postgres first (round-4 C5). Before that
// fold-in this assertion would have been false.
//
// Durable layering exception (round-9 both-reviewer MEDIUM/LOW): the reaping
// guard internal/world/postgres/reaping_guard.go (added in 05-16, R6-2)
// legitimately reads the AUTH players table (`SELECT reaping_at ... FOR UPDATE`)
// on the WORLD tx connection — a LOCKING SELECT, not a world-table MUTATION, so it
// is OUTSIDE this fence's INSERT/UPDATE/DELETE scope and is REQUIRED for the
// FOR UPDATE serialization with player_repo.MarkReaping. It is an intentional,
// durable auth-table-read-on-the-world-conn exception a future layering pass MUST
// NOT "fix away"; it is orthogonal to this mutation fence and does not weaken it.

// fencedWorldTables is the CORE/WORLD-table set the fence covers. scene_participants
// is DELIBERATELY absent (round-4 C5 / D-05). entity_properties STAYS IN
// (property_repo mutates it in-boundary; without it a raw property write outside
// the repo would go uncaught).
var fencedWorldTables = []string{
	"locations", "exits", "characters", "objects",
	"entity_properties", "outbox", "world_feed_counter",
}

// worldSQLFenceAllowMarker lets a migration line opt a world-table data DML out of
// the migration scan (an epoch-reset / registered exception).
const worldSQLFenceAllowMarker = "world-sql-fence:allow"

// worldWriteSQLRegexp matches a mutation statement (INSERT INTO / UPDATE /
// DELETE FROM) targeting a fenced world table, optionally public-qualified. It is
// case-insensitive and word-boundary anchored, so it does not fire on a longer
// identifier (e.g. scene_objects) or on a SELECT read.
func worldWriteSQLRegexp() *regexp.Regexp {
	tables := strings.Join(fencedWorldTables, "|")
	return regexp.MustCompile(`(?i)\b(?:insert\s+into|update|delete\s+from)\s+(?:public\.)?(` + tables + `)\b`)
}

// scanGoStringLiteralsForWorldWriteSQL parses Go source and returns the fenced
// tables that appear in a mutation-SQL STRING LITERAL. It walks the AST and
// inspects only token.STRING BasicLits — comments and identifiers are never
// considered (the parse-Go-not-grep property).
func scanGoStringLiteralsForWorldWriteSQL(t *testing.T, filename string, src []byte, re *regexp.Regexp) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	require.NoError(t, err, "parse %s", filename)

	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		val, uErr := strconv.Unquote(lit.Value)
		if uErr != nil {
			val = lit.Value
		}
		if m := re.FindStringSubmatch(val); m != nil {
			hits = append(hits, strings.ToLower(m[1]))
		}
		return true
	})
	return hits
}

// scanMigrationForWorldDML returns the fenced tables that a migration's DATA DML
// (INSERT/UPDATE/DELETE) targets. Schema DDL (CREATE/ALTER/DROP TABLE, ADD/DROP
// COLUMN, index/constraint ops) never matches the mutation regex, so it is
// permitted. A line (or its immediately preceding line) carrying the
// world-sql-fence:allow marker is exempt (epoch-reset / registered exception).
func scanMigrationForWorldDML(sql string, re *regexp.Regexp) []string {
	var hits []string
	lines := strings.Split(sql, "\n")
	for i, line := range lines {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if strings.Contains(line, worldSQLFenceAllowMarker) {
			continue
		}
		if i > 0 && strings.Contains(lines[i-1], worldSQLFenceAllowMarker) {
			continue
		}
		hits = append(hits, strings.ToLower(m[1]))
	}
	return hits
}

// writerBoundaryDir is the ONLY directory allowlisted to carry raw world-table
// mutation SQL — the sanctioned writer boundary.
const writerBoundaryDir = "internal/world/postgres"

// isWriterBoundaryPath reports whether a repo-relative path is inside the
// allowlisted writer boundary.
func isWriterBoundaryPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	return rel == writerBoundaryDir || strings.HasPrefix(rel, writerBoundaryDir+"/")
}

// isRegisteredExceptionMigration reports whether a migration file is on the small,
// explicit registered-exception allowlist — the pre-Phase-5 bootstrap seed
// 000001_baseline (player/location/character INSERTs at :385-395).
func isRegisteredExceptionMigration(base string) bool {
	return strings.HasPrefix(base, "000001_baseline")
}

// isGeneratedGo reports whether Go source carries the standard generated-file
// marker; generated files are skipped by the fence.
func isGeneratedGo(src []byte) bool {
	head := src
	if len(head) > 2048 {
		head = head[:2048]
	}
	return strings.Contains(string(head), "// Code generated")
}

// worldFenceSkipDir reports whether a repo-relative directory MUST NOT be scanned
// by the Go source fence: the base skip set plus the plugins/ tree (D-05 — plugin
// schemas are outside the world fence), the top-level test/ tree, test-support
// trees, and non-Go asset trees.
func worldFenceSkipDir(rel, name string) bool {
	if _, ok := skipDirs[name]; ok {
		return true
	}
	switch name {
	case "plugins", "testsupport", "testdata", "web", "site", "coverage":
		return true
	}
	rel = filepath.ToSlash(rel)
	if rel == "test" || strings.HasPrefix(rel, "test/") {
		return true
	}
	return false
}

// TestNoRawWorldSQLOutsideWriterBoundary is the fence: it scans production Go
// (excluding _test.go, generated, the plugins/ tree, and test-support trees) and
// the migration tree, failing on any core/world-table mutation SQL outside the
// writer boundary / registered exceptions. It is HONESTLY green because 05-09
// Task 2 folded the character_settings escape into internal/world/postgres first.
func TestNoRawWorldSQLOutsideWriterBoundary(t *testing.T) {
	root := findRepoRoot(t)
	re := worldWriteSQLRegexp()

	// --- Go source scan ---
	var violations []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if worldFenceSkipDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if isWriterBoundaryPath(rel) {
			return nil // allowlisted writer boundary
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if isGeneratedGo(src) {
			return nil
		}
		for _, table := range scanGoStringLiteralsForWorldWriteSQL(t, path, src, re) {
			violations = append(violations, filepath.ToSlash(rel)+": mutates "+table)
		}
		return nil
	})
	require.NoError(t, walkErr)
	assert.Empty(t, violations,
		"raw core/world-table mutation SQL must live ONLY in %s (route the write through the guarded/enveloped boundary)", writerBoundaryDir)

	// --- Migration scan (round-6 Codex MEDIUM: the Go fence's blind spot) ---
	migFiles, globErr := filepath.Glob(filepath.Join(root, "internal", "store", "migrations", "*.sql"))
	require.NoError(t, globErr)
	require.NotEmpty(t, migFiles, "migration tree must exist")
	var migViolations []string
	for _, mf := range migFiles {
		base := filepath.Base(mf)
		if isRegisteredExceptionMigration(base) {
			continue
		}
		data, readErr := os.ReadFile(mf)
		require.NoError(t, readErr)
		for _, table := range scanMigrationForWorldDML(string(data), re) {
			migViolations = append(migViolations, base+": mutates "+table)
		}
	}
	assert.Empty(t, migViolations,
		"a new migration must not INSERT/UPDATE/DELETE a world table without an envelope/epoch — mark it %q or route the write through the boundary", worldSQLFenceAllowMarker)
}

// TestWorldSQLFenceFlagsMutationOutsideBoundary is the NEGATIVE fixture: the Go
// scanner MUST flag core/world-table mutation SQL in a synthetic file.
func TestWorldSQLFenceFlagsMutationOutsideBoundary(t *testing.T) {
	re := worldWriteSQLRegexp()
	src := []byte(`package fixture

func escape() {
	insertLoc := "INSERT INTO locations (id, name) VALUES ($1, $2)"
	updChar := ` + "`UPDATE characters SET description = $1 WHERE id = $2`" + `
	delObj := "DELETE FROM objects WHERE id = $1"
	_ = insertLoc
	_ = updChar
	_ = delObj
}
`)
	hits := scanGoStringLiteralsForWorldWriteSQL(t, "fixture.go", src, re)
	assert.ElementsMatch(t, []string{"locations", "characters", "objects"}, hits,
		"the fence must flag INSERT/UPDATE/DELETE against core/world tables")
}

// TestWorldSQLFenceIgnoresReadsAndComments proves reads (SELECT) and comment text
// are never flagged — the parse-Go-not-grep property.
func TestWorldSQLFenceIgnoresReadsAndComments(t *testing.T) {
	re := worldWriteSQLRegexp()
	src := []byte(`package fixture

// This comment mentions INSERT INTO characters but is not a literal.
func read() {
	q := "SELECT id, name FROM characters WHERE id = $1"
	_ = q
}
`)
	hits := scanGoStringLiteralsForWorldWriteSQL(t, "fixture.go", src, re)
	assert.Empty(t, hits, "reads and comments must not be flagged")
}

// TestWorldSQLFenceDoesNotFlagSceneParticipants proves the D-05 rejection: a
// plugins/-tree scene_participants write is NOT flagged (scene_participants is not
// in the fenced set, and the plugins/ tree is not scanned).
func TestWorldSQLFenceDoesNotFlagSceneParticipants(t *testing.T) {
	re := worldWriteSQLRegexp()
	src := []byte(`package fixture

func pluginWrite() {
	q := "INSERT INTO scene_participants (scene_id, character_id) VALUES ($1, $2)"
	_ = q
}
`)
	hits := scanGoStringLiteralsForWorldWriteSQL(t, "plugin_fixture.go", src, re)
	assert.Empty(t, hits, "scene_participants is out of the world fence (D-05, #4815)")

	// The plugins/ tree, test-support trees, and the top-level test/ tree are excluded.
	assert.True(t, worldFenceSkipDir("plugins", "plugins"))
	assert.True(t, worldFenceSkipDir("internal/testsupport", "testsupport"))
	assert.True(t, worldFenceSkipDir("test", "test"))
	assert.False(t, worldFenceSkipDir("internal/store", "store"))
}

// TestWorldSQLFenceAllowlistPathIsWriterBoundary proves the positive allowlist:
// SQL inside internal/world/postgres passes; anywhere else is fenced.
func TestWorldSQLFenceAllowlistPathIsWriterBoundary(t *testing.T) {
	assert.True(t, isWriterBoundaryPath("internal/world/postgres"))
	assert.True(t, isWriterBoundaryPath("internal/world/postgres/character_repo.go"))
	assert.False(t, isWriterBoundaryPath("internal/store/character_settings_repo.go"))
	assert.False(t, isWriterBoundaryPath("internal/world/service.go"))
}

// TestWorldSQLFenceFlagsNewMigrationWorldDML proves the migration channel is
// closed: a NEW migration with world-table data DML and no marker is flagged.
func TestWorldSQLFenceFlagsNewMigrationWorldDML(t *testing.T) {
	re := worldWriteSQLRegexp()
	sql := `-- 000099_bad.up.sql
INSERT INTO characters (id, player_id, name) VALUES ('x', 'y', 'z');
UPDATE locations SET name = 'renamed' WHERE id = 'a';
`
	hits := scanMigrationForWorldDML(sql, re)
	assert.ElementsMatch(t, []string{"characters", "locations"}, hits,
		"a new migration mutating a world table without a marker must fail the fence")
	assert.False(t, isRegisteredExceptionMigration("000099_bad.up.sql"))
}

// TestWorldSQLFenceAllowsMigrationDDL proves schema DDL is permitted in any
// migration (only data DML is fenced).
func TestWorldSQLFenceAllowsMigrationDDL(t *testing.T) {
	re := worldWriteSQLRegexp()
	sql := `CREATE TABLE IF NOT EXISTS characters (id TEXT PRIMARY KEY);
ALTER TABLE characters ADD COLUMN IF NOT EXISTS preferences JSONB;
DROP TABLE IF EXISTS objects;
CREATE INDEX idx_char ON characters (name);
`
	assert.Empty(t, scanMigrationForWorldDML(sql, re), "schema DDL must be permitted")
}

// TestWorldSQLFenceAllowsMarkedAndBaselineMigrations proves the registered
// exceptions: the baseline seed file and a marker-annotated DML line pass.
func TestWorldSQLFenceAllowsMarkedAndBaselineMigrations(t *testing.T) {
	re := worldWriteSQLRegexp()

	assert.True(t, isRegisteredExceptionMigration("000001_baseline.up.sql"))
	assert.True(t, isRegisteredExceptionMigration("000001_baseline.down.sql"))

	sameLine := `UPDATE characters SET version = 1; -- world-sql-fence:allow epoch reset`
	assert.Empty(t, scanMigrationForWorldDML(sameLine, re), "same-line marker exempts the DML")

	precedingLine := `-- world-sql-fence:allow registered backfill
INSERT INTO objects (id, name) VALUES ('a', 'b');`
	assert.Empty(t, scanMigrationForWorldDML(precedingLine, re), "preceding-line marker exempts the DML")
}

// TestWorldSQLFenceIsNotADepguardRule proves the fence is source-scanning, not a
// depguard rule (Codex finding 6): no fenced-table SQL deny rule leaked into
// .golangci.yaml.
func TestWorldSQLFenceIsNotADepguardRule(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".golangci.yaml"))
	require.NoError(t, err)
	cfg := string(data)
	assert.NotContains(t, cfg, worldSQLFenceAllowMarker,
		"the world-SQL fence must be a source-scanning meta-test, not a .golangci.yaml rule")
	assert.NotContains(t, cfg, "world_feed_counter",
		"no world-table SQL depguard rule belongs in .golangci.yaml (depguard matches packages, not SQL)")
}
