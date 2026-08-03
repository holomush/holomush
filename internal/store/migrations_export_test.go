// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"fmt"
	"strings"
)

// MigrationSQLForTest returns the embedded source of the named migration file
// (e.g. "000053_sessions_location_index.sql").
//
// It exists so that a migration spec can execute the migration's OWN SQL
// instead of re-typing it as a Go string literal. A re-typed copy makes the
// spec unfalsifiable: it asserts the idempotency of the string the test wrote,
// so deleting IF NOT EXISTS from the migration file leaves the spec green.
//
// migrationsFS is unexported and the specs live in package store_test; both
// packages are linked into the same test binary, so an export_test.go accessor
// is the narrowest seam that does not widen the production surface.
func MigrationSQLForTest(name string) (string, error) {
	data, err := migrationsFS.ReadFile("migrations/" + name)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// MigrationSectionForTest returns exactly ONE direction — "up" or "down" — of
// the named goose migration file (e.g. "000053_sessions_location_index.sql"),
// with every "-- +goose StatementBegin" / "-- +goose StatementEnd" parser
// directive removed so the returned text can be handed straight to db.Exec.
//
// It exists because a goose migration is a SINGLE file carrying both bodies.
// Handing a whole merged file to Exec runs the DOWN body as well: the 000053
// idempotency spec would drop the index it just asserted, and the 000052
// re-apply spec would drop the partitioned parent it just probed. That is not
// hypothetical — it was observed before this function existed, as
// "the index MUST be restored before this spec returns / Expected <string>:
// not to be empty". Any future edit that widens this back to whole-file reads
// reintroduces that destructive behavior, silently, in a passing-looking spec.
//
// It shares MigrationSQLForTest's reason for living: a spec must execute the
// migration's OWN SQL rather than a re-typed Go string literal, because a
// re-typed copy asserts the idempotency of the string the test itself wrote.
//
// Errors are returned bare (no oops wrap): this is a test seam, not a trust
// boundary, and the sibling accessor above sets the same convention.
func MigrationSectionForTest(name, direction string) (string, error) {
	src, err := MigrationSQLForTest(name)
	if err != nil {
		return "", err
	}
	section, err := migrationSection(src, direction)
	if err != nil {
		return "", fmt.Errorf("migration %s: %w", name, err)
	}
	return section, nil
}

// migrationSection is the pure parsing half of MigrationSectionForTest, split
// out so the error paths (unknown direction, a source missing either
// annotation) can be covered without inventing fixture files on the embedded
// filesystem.
//
// A missing annotation is an ERROR rather than a fallback to the whole file:
// the fallback is precisely the destructive behavior this seam prevents.
func migrationSection(src, direction string) (string, error) {
	if direction != "up" && direction != "down" {
		return "", fmt.Errorf("unknown direction %q (want %q or %q)", direction, "up", "down")
	}

	lines := strings.Split(src, "\n")
	upAt, downAt := -1, -1
	for i, line := range lines {
		switch gooseAnnotation(line) {
		case "Up":
			if upAt < 0 {
				upAt = i
			}
		case "Down":
			if downAt < 0 {
				downAt = i
			}
		}
	}

	switch {
	case upAt < 0:
		return "", fmt.Errorf("no %q annotation found", "-- +goose Up")
	case downAt < 0:
		return "", fmt.Errorf("no %q annotation found", "-- +goose Down")
	case downAt < upAt:
		return "", fmt.Errorf("%q precedes %q", "-- +goose Down", "-- +goose Up")
	}

	body := lines[downAt+1:]
	if direction == "up" {
		body = lines[upAt+1 : downAt]
	}

	// Only the block markers are stripped, and deliberately so. They are removed
	// because Postgres would reject them as SQL; every other goose annotation is a
	// plain `--` comment that Exec ignores. In particular the interpolation
	// annotation is NOT stripped: statementBlockViolations rejects it across the
	// whole embedded corpus in the untagged lane, so a section carrying one is
	// already impossible — and stripping it here would hide it from any spec that
	// executed the section rather than surfacing it.
	kept := make([]string, 0, len(body))
	for _, line := range body {
		switch gooseAnnotation(line) {
		case "StatementBegin", "StatementEnd":
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n"), nil
}

// gooseAnnotation returns the directive carried by a "-- +goose <directive>"
// comment line (e.g. "Up", "Down", "StatementBegin"), or "" when the line is
// not a goose annotation. Leading whitespace and the spacing after "--" are
// tolerated, matching goose's own lenient parse.
func gooseAnnotation(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "--") {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "--"))
	if !strings.HasPrefix(rest, "+goose ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(rest, "+goose "))
}
