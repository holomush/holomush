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

// statementBlockViolations reports every place in a goose migration body where a
// dollar-quoted region is not enclosed by `-- +goose StatementBegin` /
// `-- +goose StatementEnd`, plus any unbalanced marker. It returns one
// human-readable string per problem — each naming the file and 1-based line
// number, so a CI failure is actionable without re-deriving it — and nil when the
// body is clean.
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
	for _, entry := range entries {
		name := entry.Name()
		content, readErr := migrationsFS.ReadFile("migrations/" + name)
		require.NoError(t, readErr, "should read embedded migration %s", name)

		body := string(content)
		violations := statementBlockViolations(name, body)
		// assert, not require: one mis-wrapped migration must not hide the rest.
		assert.Emptyf(t, violations, "dollar-quote wrapping violations:\n%s", strings.Join(violations, "\n"))

		if dollarQuoteTag.MatchString(body) {
			filesCarryingTags++
		}
	}

	// Non-vacuity. Without this, a scanner that matched nothing at all — a broken
	// tag pattern, a renamed corpus, an empty embed — would read as "clean".
	// The exact census is deliberately NOT asserted: a legitimate new `$$`
	// migration must not turn this red.
	assert.Positive(t, filesCarryingTags,
		"no embedded migration carries a dollar-quote tag — the scan is vacuous, not clean")
}
