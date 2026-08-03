// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dollarQuoteTag matches a PostgreSQL dollar-quote delimiter — `$$` or `$tag$`.
//
// The optional tag MUST start with an identifier character. That is not a
// stylistic choice: Postgres itself rejects a digit-leading dollar-quote tag, and
// the naive `\$[A-Za-z0-9_]*\$` matches the seeded bcrypt hash
// '$2a$10$N9qo8uLOickgx2ZMRZoMye' in 000001_baseline.sql, which is an ordinary
// string literal. A scanner using the naive form reports a false violation there
// — and a *converter* using it would wrap that line, swallowing the rest of the
// baseline's semicolons into one statement.
var dollarQuoteTag = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)?\$`)

// The two annotations that switch goose's environment-variable interpolation on
// and off, as gooseAnnotation reports them.
//
// These are re-declared here rather than imported because goose keeps its own
// copies in an internal package (github.com/pressly/goose/v3/internal/sqlparser),
// which is unimportable from outside that module. Matching them exactly is
// nonetheless sufficient: goose compares the extracted annotation against its
// constants by equality and hard-errors on anything else ("unknown annotation"),
// so a differently-spelled variant is a migration that cannot apply at all.
const (
	envsubOn  = "ENVSUB ON"
	envsubOff = "ENVSUB OFF"

	// envsubAnnotationLabel names the offence without spelling the annotation
	// token, so this violation message can be quoted into a migration comment or
	// a doc without becoming an unparseable annotation in its own right.
	envsubAnnotationLabel = "goose environment-interpolation annotation"
)

// statementBlockViolations reports every place in a goose migration body where a
// dollar-quoted region is not enclosed by `-- +goose StatementBegin` /
// `-- +goose StatementEnd`, plus any unbalanced marker, plus any use of goose's
// environment-interpolation annotation. It returns one human-readable string per
// problem — each naming the file and 1-based line number, so a CI failure is
// actionable without re-deriving it — and nil when the body is clean.
//
// It is pure (strings in, strings out) so the table-driven test above can drive
// it with synthetic broken bodies. The alternative — proving the guard by
// breaking a real migration on disk — risks leaving the corpus corrupted.
func statementBlockViolations(name, body string) []string {
	var violations []string

	inBlock := false
	openedAt := 0

	for i, line := range strings.Split(body, "\n") {
		lineNo := i + 1

		switch gooseAnnotation(line) {
		case "StatementBegin":
			if inBlock {
				violations = append(violations, fmt.Sprintf(
					"%s:%d: -- +goose StatementBegin while still inside the block opened at line %d",
					name, lineNo, openedAt,
				))
				continue
			}
			inBlock = true
			openedAt = lineNo
			continue
		case "StatementEnd":
			if !inBlock {
				violations = append(violations, fmt.Sprintf(
					"%s:%d: -- +goose StatementEnd with no preceding -- +goose StatementBegin",
					name, lineNo,
				))
				continue
			}
			inBlock = false
			continue
		case envsubOn, envsubOff:
			// Interpolation is enabled PER MIGRATION by this in-file annotation;
			// goose exposes no provider-level switch to decline it, and NewMigrator
			// passes none. So the rule's MUST NOT has no mechanical backstop other
			// than this scan. It fires regardless of block state because goose reads
			// its annotations the same way.
			violations = append(violations, fmt.Sprintf(
				"%s:%d: %s — goose interpolates environment variables into the SQL between "+
					"these markers, which makes the applied schema a function of whoever ran "+
					"the migration and turns the environment into a config-injection surface "+
					"into DDL; anything a migration needs at runtime belongs in Go: %s",
				name, lineNo, envsubAnnotationLabel, strings.TrimSpace(line),
			))
			continue
		}

		if inBlock {
			continue
		}
		if tag := dollarQuoteTag.FindString(line); tag != "" {
			violations = append(violations, fmt.Sprintf(
				"%s:%d: dollar-quote delimiter %q outside a -- +goose StatementBegin/StatementEnd "+
					"block — goose splits on semicolons line by line and will tear this body apart: %s",
				name, lineNo, tag, strings.TrimSpace(line),
			))
		}
	}

	if inBlock {
		violations = append(violations, fmt.Sprintf(
			"%s:%d: -- +goose StatementBegin has no matching -- +goose StatementEnd",
			name, openedAt,
		))
	}

	return violations
}

// TestStatementBlockViolationsFlagsDollarQuotesOutsideStatementBlocks drives the
// pure scanner with synthetic bodies. Keeping the scanner pure (name + body in,
// violation strings out) is what makes a demonstrated RED possible without
// corrupting the real corpus on disk.
func TestStatementBlockViolationsFlagsDollarQuotesOutsideStatementBlocks(t *testing.T) {
	const wrapped = "-- +goose Up\n" +
		"-- +goose StatementBegin\n" +
		"DO $$\nBEGIN\n  PERFORM 1;\nEND $$;\n" +
		"-- +goose StatementEnd\n"

	tests := []struct {
		name      string
		body      string
		wantCount int
		wantIn    string
	}{
		{
			name:      "accepts a DO block enclosed by StatementBegin and StatementEnd",
			body:      wrapped,
			wantCount: 0,
		},
		{
			name: "flags the same DO block once its wrapper markers are removed",
			body: "-- +goose Up\n" +
				"DO $$\nBEGIN\n  PERFORM 1;\nEND $$;\n",
			wantCount: 2, // the opening `DO $$` line and the closing `END $$;` line
			wantIn:    "fixture.sql:2:",
		},
		{
			name: "ignores the bcrypt literal because a dollar-quote tag cannot start with a digit",
			body: "-- +goose Up\nINSERT INTO players (id, username, password_hash)\n" +
				"VALUES ('01KDVDNA00041061050R3GG28A', 'testuser', '$2a$10$N9qo8uLOickgx2ZMRZoMye');\n",
			wantCount: 0,
		},
		{
			name:      "flags a StatementBegin that is never closed",
			body:      "-- +goose Up\n-- +goose StatementBegin\nDO $$\nBEGIN\nEND $$;\n",
			wantCount: 1,
			wantIn:    "has no matching",
		},
		{
			name:      "flags a StatementEnd that precedes any StatementBegin",
			body:      "-- +goose Up\n-- +goose StatementEnd\n",
			wantCount: 1,
			wantIn:    "no preceding",
		},
		{
			name:      "reports nothing for an empty body",
			body:      "",
			wantCount: 0,
		},
		{
			name: "flags both ENVSUB annotations bracketing an interpolated body",
			body: "-- +goose Up\n-- +goose ENVSUB ON\n" +
				"CREATE TABLE t (x text DEFAULT '${HOLOMUSH_TENANT}');\n" +
				"-- +goose ENVSUB OFF\n",
			wantCount: 2,
			wantIn:    "fixture.sql:2:",
		},
		{
			name: "flags an ENVSUB annotation even inside a StatementBegin block",
			body: "-- +goose Up\n-- +goose StatementBegin\n-- +goose ENVSUB ON\n" +
				"DO $$\nBEGIN\nEND $$;\n-- +goose StatementEnd\n",
			wantCount: 1,
			wantIn:    "environment into a config-injection surface",
		},
		{
			name:      "accepts the annotations goose recognises that HoloMUSH does allow",
			body:      "-- +goose NO TRANSACTION\n-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;\n",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statementBlockViolations("fixture.sql", tt.body)
			require.Len(t, got, tt.wantCount, "violations: %s", strings.Join(got, " | "))
			if tt.wantIn != "" {
				assert.Contains(t, strings.Join(got, "\n"), tt.wantIn)
			}
		})
	}
}

// TestEveryDollarQuotedMigrationBodyIsWrappedInStatementBeginEnd is D-13's guard.
//
// goose's parser splits a migration on semicolons line by line. A `DO $$ ... $$;`
// body that is not enclosed by `-- +goose StatementBegin` / `-- +goose StatementEnd`
// is therefore torn apart at every internal semicolon. Under the default
// transactional mode the tear is loud and atomic ("ERROR: unterminated
// dollar-quoted string", whole migration rolled back), but a migration carrying
// `-- +goose NO TRANSACTION` has no such protection, and a torn leading fragment
// that happens to be valid standalone SQL applies partially either way.
//
// This file carries NO build constraint, deliberately, and MUST NOT grow one:
// `task test` does not compile integration-tagged files, and the whole point of
// this guard is that a mis-wrapped migration fails the fast lane that gates every
// PR rather than surviving to review or to a Docker-only job. (The token itself
// is left unspelled here so a text gate asserting this file's freedom from a
// build constraint cannot be defeated by this very sentence.)
func TestEveryDollarQuotedMigrationBodyIsWrappedInStatementBeginEnd(t *testing.T) {
	entries, err := migrationsFS.ReadDir("migrations")
	require.NoError(t, err, "should read embedded migrations directory")
	require.NotEmpty(t, entries, "embedded migrations directory should not be empty")

	filesCarryingTags := 0
	filesCarryingAnnotations := 0
	for _, entry := range entries {
		name := entry.Name()
		content, readErr := migrationsFS.ReadFile("migrations/" + name)
		require.NoError(t, readErr, "should read embedded migration %s", name)

		body := string(content)
		violations := statementBlockViolations(name, body)
		// assert, not require: one mis-wrapped migration must not hide the rest.
		assert.Emptyf(t, violations, "migration format violations:\n%s", strings.Join(violations, "\n"))

		if dollarQuoteTag.MatchString(body) {
			filesCarryingTags++
		}
		for _, line := range strings.Split(body, "\n") {
			if gooseAnnotation(line) != "" {
				filesCarryingAnnotations++
				break
			}
		}
	}

	// Non-vacuity. Without this, a scanner that matched nothing at all — a broken
	// tag pattern, a renamed corpus, an empty embed — would read as "clean".
	// The exact census is deliberately NOT asserted: a legitimate new `$$`
	// migration must not turn this red.
	assert.Positive(t, filesCarryingTags,
		"no embedded migration carries a dollar-quote tag — the scan is vacuous, not clean")

	// The ENVSUB arm cannot be proven non-vacuous by a corpus census the way the
	// dollar-quote arm can: a clean corpus carries ZERO interpolation annotations
	// by construction, so counting them would assert 0 > 0 forever. Its RED is
	// demonstrated on synthetic bodies by the table-driven test above. What the
	// corpus CAN still prove is that the recognizer hosting that arm is alive — if
	// gooseAnnotation regressed to returning "" for every line, the ENVSUB case
	// (and the block tracking beside it) would go silently dead while this scan
	// kept reporting "clean".
	assert.Positive(t, filesCarryingAnnotations,
		"no embedded migration carries a recognised goose annotation — the annotation "+
			"recognizer is dead, so every check keyed on it is vacuous")
}
