// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// cmd/internal/gen-confusables is a build-time codegen tool that emits the
// UTS #39 confusables mapping table into internal/charname as Go source.
//
// Invoked via go:generate in internal/charname/doc.go:
//
//	//go:generate go run github.com/holomush/holomush/cmd/internal/gen-confusables
//
// or via the Taskfile:
//
//	task generate:confusables
//
// # Why generated rather than a dependency
//
// The skeleton computed from this table is a security gate (character-name
// impersonation). The two Go modules that implement UTS #39 §4 correctly carry
// 4 and 1 GitHub stars respectively, and neither exposes its Unicode version as
// a program-readable value. Generating the table here makes the version an
// exported Go constant emitted by the same run that emits the table, so
// characters.name_skeleton_unicode_version can never disagree with the data the
// skeleton was computed from.
//
// # The input is content-addressed
//
// Version-addressing the URL prevents an accidental UPGRADE. It does not
// prevent upstream replacement of the bytes at that URL, and a substituted
// input silently reshapes a security gate. So the downloaded (or -input) bytes
// are verified against confusablesSHA256 BEFORE any code is emitted, and a
// mismatch is a hard failure naming both digests.
//
// # Refreshing the pin deliberately
//
//  1. Download the new file:
//     curl -sSfL -o /tmp/confusables.txt <new version-addressed URL>
//  2. Review the diff against the currently pinned version.
//  3. Update confusablesURL, pinnedUnicodeVersion and confusablesSHA256 below
//     (shasum -a 256 /tmp/confusables.txt).
//  4. Re-run `task generate:confusables` and commit the regenerated table
//     together with this file's constants — Taskfile's sources:/generates:
//     declaration and CI's drift check make that a single change or a red build.
//
// # Usage
//
//	go run ./cmd/internal/gen-confusables                  # downloads the pinned URL
//	go run ./cmd/internal/gen-confusables -input FILE      # offline; same digest check
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// pinnedUnicodeVersion is the Unicode version this table is generated from.
	// It is asserted against the input file's own `# Version:` header, and is
	// emitted as charname.UnicodeVersion.
	pinnedUnicodeVersion = "17.0.0"

	// confusablesURL is version-addressed, never the /latest/ form: /latest/
	// silently changes content under a fixed URL, which is precisely the shape
	// a pinned security-gate input must not have.
	//
	// NOTE ON THE PATH SHAPE: the security data for a given Unicode version
	// lives at /Public/<version>/security/, NOT at /Public/security/<version>/.
	// The latter form exists for versions up to 16.0.0 only; it 404s for
	// 17.0.0. Verified 2026-08-04.
	confusablesURL = "https://www.unicode.org/Public/" + pinnedUnicodeVersion + "/security/confusables.txt"

	// confusablesSHA256 is the digest of the bytes at confusablesURL. See the
	// package doc comment for the deliberate-refresh procedure.
	confusablesSHA256 = "091c7f82fc39ef208faf8f94d29c244de99254675e09de163160c810d13ef22a"

	// outputRelPath is where the generated table lands, relative to the module
	// root.
	outputRelPath = "internal/charname/confusables_table_gen.go"

	downloadTimeout = 60 * time.Second
)

// config carries everything generate needs, so the whole pipeline is testable
// without touching the network or the repo.
type config struct {
	inputPath      string // when empty, the pinned URL is downloaded
	outputPath     string
	expectedSHA256 string
	wantVersion    string
}

func main() {
	input := flag.String("input", "", "path to a local confusables.txt (offline mode); the digest check still applies")
	output := flag.String("output", "", "output path for the generated table (default: <module root>/"+outputRelPath+")")
	flag.Parse()

	out := *output
	if out == "" {
		out = filepath.Join(findModuleRoot(), outputRelPath)
	}

	if err := generate(config{
		inputPath:      *input,
		outputPath:     out,
		expectedSHA256: confusablesSHA256,
		wantVersion:    pinnedUnicodeVersion,
	}); err != nil {
		log.Fatalf("gen-confusables: %v", err)
	}

	fmt.Printf("wrote %s (Unicode %s)\n", out, pinnedUnicodeVersion)
}

// generate reads the confusables data, verifies its digest and version, and
// writes the Go table. Nothing is written unless every check passes.
func generate(cfg config) error {
	raw, err := loadInput(cfg.inputPath)
	if err != nil {
		return err
	}

	// Content-addressing gate. Deliberately BEFORE parsing and before any
	// write: a substituted input must not reach the emitter at all.
	if got := sha256Hex(raw); got != cfg.expectedSHA256 {
		return fmt.Errorf(
			"confusables input digest mismatch: expected sha256 %s, observed sha256 %s "+
				"(refresh the pin deliberately per the gen-confusables package doc, or investigate a substituted input)",
			cfg.expectedSHA256, got,
		)
	}

	version, table, err := parseConfusables(raw)
	if err != nil {
		return err
	}
	if version != cfg.wantVersion {
		return fmt.Errorf("confusables input declares Unicode version %s but this generator pins %s", version, cfg.wantVersion)
	}
	if len(table) == 0 {
		return fmt.Errorf("confusables input parsed to an empty mapping table")
	}

	src := render(version, table)
	if err := os.WriteFile(cfg.outputPath, []byte(src), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", cfg.outputPath, err)
	}
	return nil
}

func loadInput(path string) ([]byte, error) {
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading -input %s: %w", path, err)
		}
		return raw, nil
	}
	return download(confusablesURL)
}

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url) //nolint:noctx // one-shot build-time tool; the client carries a timeout
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // best-effort cleanup

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: unexpected status %s", url, resp.Status)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	return raw, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// parseConfusables extracts the `# Version:` header and every
// `source ; target… ; MA` mapping line from confusables.txt.
//
// The source is always a single code point in the MA table; the target is a
// space-separated sequence of one or more code points.
func parseConfusables(raw []byte) (version string, table map[rune]string, err error) {
	table = make(map[rune]string, 8192)

	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := parseVersionHeader(line); ok {
			version = v
			continue
		}
		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Split(line, ";")
		if len(fields) < 3 {
			continue
		}
		src, srcErr := parseCodePoint(strings.TrimSpace(fields[0]))
		if srcErr != nil {
			return "", nil, fmt.Errorf("parsing source code point in %q: %w", line, srcErr)
		}
		target, tgtErr := parseCodePointSequence(strings.TrimSpace(fields[1]))
		if tgtErr != nil {
			return "", nil, fmt.Errorf("parsing target sequence in %q: %w", line, tgtErr)
		}
		table[src] = target
	}

	if version == "" {
		return "", nil, fmt.Errorf("confusables input carries no `# Version:` header")
	}
	return version, table, nil
}

func parseVersionHeader(line string) (string, bool) {
	const prefix = "# Version:"
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, prefix)), true
}

func parseCodePoint(s string) (rune, error) {
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, err
	}
	if v > uint64(utf8.MaxRune) {
		return 0, fmt.Errorf("code point U+%X is above the Unicode maximum", v)
	}
	return rune(v), nil
}

func parseCodePointSequence(s string) (string, error) {
	var b strings.Builder
	for _, field := range strings.Fields(s) {
		r, err := parseCodePoint(field)
		if err != nil {
			return "", err
		}
		b.WriteRune(r)
	}
	return b.String(), nil
}

// render emits the generated Go source. Keys are sorted so a regeneration on an
// unchanged input is byte-identical.
func render(version string, table map[rune]string) string {
	keys := make([]rune, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var b strings.Builder
	b.WriteString("// SPDX-License-Identifier: Apache-2.0\n")
	b.WriteString("// Copyright 2026 HoloMUSH Contributors\n\n")
	b.WriteString("// Code generated by gen-confusables. DO NOT EDIT.\n")
	b.WriteString("// Source: " + confusablesURL + "\n\n")
	b.WriteString("package charname\n\n")
	b.WriteString("// UnicodeVersion is the Unicode version of the confusables data this\n")
	b.WriteString("// package's Skeleton function is computed from. It is parsed from the\n")
	b.WriteString("// pinned confusables.txt's own `# Version:` header by the generator, so the\n")
	b.WriteString("// constant and the table below cannot disagree.\n")
	b.WriteString("//\n")
	b.WriteString("// Every write path that stores a skeleton MUST also store this value in\n")
	b.WriteString("// characters.name_skeleton_unicode_version, so a partial or interrupted\n")
	b.WriteString("// recompute after a Unicode upgrade is queryable rather than silent.\n")
	b.WriteString("const UnicodeVersion = " + strconv.Quote(version) + "\n\n")
	b.WriteString("// confusables is the UTS #39 MA (multi-script any-case) mapping: each source\n")
	b.WriteString("// rune maps to its confusable prototype sequence.\n")
	b.WriteString("var confusables = map[rune]string{\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "\t0x%04X: %s,\n", k, quoteEscaped(table[k]))
	}
	b.WriteString("}\n")
	return b.String()
}

// quoteEscaped renders a target sequence as an all-ASCII Go string literal, so
// the generated file is diff-friendly and free of invisible codepoints.
func quoteEscaped(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r > 0xFFFF:
			fmt.Fprintf(&b, "\\U%08X", r)
		default:
			fmt.Fprintf(&b, "\\u%04X", r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func findModuleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	log.Fatal("could not find module root (go.mod)")
	return ""
}
