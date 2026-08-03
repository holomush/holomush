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
		annotation, annErr := gooseAnnotation(line)
		if annErr != nil {
			// goose would refuse the whole file. Sectioning it anyway would hand a
			// caller SQL from a migration that cannot apply.
			return "", fmt.Errorf("line %d: %w", i+1, annErr)
		}
		switch annotation {
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
		// The error is discarded deliberately: the loop above scanned every line in
		// the file and already returned for any goose refuses, so nothing reaching
		// here can carry one.
		annotation, _ := gooseAnnotation(line)
		switch annotation {
		case "StatementBegin", "StatementEnd":
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n"), nil
}

// gooseSupportedAnnotations is goose's own supportedAnnotations set, in goose's
// own spelling (v3.27.3 internal/sqlparser/parser.go:291-310). It is re-declared
// rather than imported because the package holding it is internal to that module
// and therefore unimportable from here.
//
// The spellings here are the CANONICAL forms gooseAnnotation returns, so every
// caller can keep switching on a plain string constant even though the engine —
// and now this recognizer — accept any case variant of them.
var gooseSupportedAnnotations = []string{
	"Up",
	"Down",
	"StatementBegin",
	"StatementEnd",
	"NO TRANSACTION",
	envsubOn,
	envsubOff,
}

// gooseAnnotation reports the goose annotation carried by line, canonicalised to
// goose's own spelling ("Up", "StatementBegin", "ENVSUB ON", ...). It returns ""
// with a nil error when the line is not an annotation line at all.
//
// The error is non-nil when the line IS an annotation line — a `--` comment
// mentioning "+goose", which is the exact gate goose applies at
// v3.27.3 internal/sqlparser/parser.go:126 — but one that goose's
// extractAnnotation (:319-350) REFUSES. goose aborts the entire migration in that
// case, so callers must surface it rather than reading the line as ordinary SQL.
//
// This function exists to mirror extractAnnotation step for step, because the
// guards built on it are only as faithful as their agreement with the engine they
// guard. The two ways they previously disagreed both mattered:
//
//   - goose folds case (strings.EqualFold, :345) and TrimSpaces (:336) whatever
//     separates "+goose" from the directive, so `-- +goose envsub on` and
//     `-- +goose\tStatementEnd` are honoured. A recognizer requiring a literal
//     "+goose " and exact case went blind on them — and a missed StatementEnd
//     fails OPEN, because the scanner keeps believing it is inside a wrap that
//     goose has already closed.
//   - goose is STRICTER than a TrimSpace-first parse about leading whitespace
//     (:320-322): it hard-errors, so ` -- +goose Up` is a migration that cannot
//     apply at all.
func gooseAnnotation(line string) (string, error) {
	// goose's own gate: a "--" comment mentioning "+goose" anywhere.
	if !strings.HasPrefix(strings.TrimSpace(line), "--") || !strings.Contains(line, "+goose") {
		return "", nil
	}

	// Past this point the line IS an annotation to goose, so every rejection
	// below aborts the whole migration rather than being ignored.
	if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
		return "", fmt.Errorf("%q contains leading whitespace", line)
	}

	// Every "--", then the FIRST "+goose" only — a second one is an error, which
	// is how goose refuses two annotations sharing a line.
	cmd := strings.ReplaceAll(line, "--", "")
	cmd = strings.Replace(cmd, "+goose", "", 1)
	if strings.Contains(cmd, "+goose") {
		return "", fmt.Errorf("%q contains multiple '+goose' annotations", cmd)
	}

	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", fmt.Errorf("%q carries an empty annotation", line)
	}

	for _, supported := range gooseSupportedAnnotations {
		if strings.EqualFold(supported, cmd) {
			return supported, nil
		}
	}
	return "", fmt.Errorf("%q is not supported", cmd)
}
