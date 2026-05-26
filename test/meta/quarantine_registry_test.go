// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// beadRE matches a holomush bead id wherever it appears (registry rows or
// in-code markers).
var beadRE = regexp.MustCompile(`holomush-[a-z0-9.]+`)

// registryBeadRE pulls the bead id from a `bead:` key line in the registry.
var registryBeadRE = regexp.MustCompile(`(?m)^\s*bead:\s*(holomush-[a-z0-9.]+)`)

// markerLineRE matches an in-code quarantine marker line and is used to scope
// bead extraction to lines that actually mark a spec quarantined:
//   - Go:        quarantinetest.Skip(t, "holomush-xxxx")
//   - Ginkgo:    Skip("quarantined: holomush-xxxx")  (or Label("quarantine", "holomush-xxxx"))
//   - Playwright '@quarantine' tag line carries an adjacent '@holomush-xxxx' tag
var markerLineRE = regexp.MustCompile(`quarantinetest\.Skip\(|quarantined:|@quarantine|Label\("quarantine"`)

// TestQuarantineRegistryBijection enforces INV-2: every in-code quarantine
// marker maps to exactly one test/quarantine.yaml row and vice versa.
func TestQuarantineRegistryBijection(t *testing.T) {
	root := findRepoRoot(t)

	registry := registryBeads(t, root)
	markers := markerBeads(t, root)

	sort.Strings(registry)
	sort.Strings(markers)

	require.Equal(t, registry, markers,
		"quarantine marker set and test/quarantine.yaml must be identical "+
			"(registry=%v markers=%v)", registry, markers)
}

func registryBeads(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "test", "quarantine.yaml"))
	require.NoError(t, err, "read test/quarantine.yaml")
	out := newQuarantineBeadSet()
	for _, m := range registryBeadRE.FindAllStringSubmatch(string(data), -1) {
		out.add(m[1])
	}
	return out.slice()
}

func markerBeads(t *testing.T, root string) []string {
	t.Helper()
	out := newQuarantineBeadSet()
	rootFS, err := os.OpenRoot(root)
	require.NoError(t, err, "open repo root")
	defer func() { _ = rootFS.Close() }()

	// Files that contain marker-shaped lines that are NOT real quarantine
	// markers and MUST be excluded from the walk:
	//   - this meta-test's own regex literals; and
	//   - the quarantinetest helper package's self-tests, which exercise
	//     Skip with a sample bead id but quarantine no real spec.
	// Real quarantine markers live in actual integration/E2E specs.
	metaSelf := filepath.Join("test", "meta", "quarantine_registry_test.go")
	helperPkg := filepath.Join("internal", "testsupport", "quarantinetest") + string(filepath.Separator)

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
		name := d.Name()
		// Scan Go test files and Playwright specs only.
		isGoTest := strings.HasSuffix(name, "_test.go")
		isSpec := strings.HasSuffix(name, ".spec.ts")
		if !isGoTest && !isSpec {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		// Skip files whose marker-shaped lines are not real markers.
		if rel == metaSelf || strings.HasPrefix(rel, helperPkg) {
			return nil
		}
		f, openErr := rootFS.Open(rel)
		if openErr != nil {
			return openErr
		}
		data, readErr := io.ReadAll(f)
		_ = f.Close()
		if readErr != nil {
			return readErr
		}
		for _, line := range strings.Split(string(data), "\n") {
			if !markerLineRE.MatchString(line) {
				continue
			}
			for _, b := range beadRE.FindAllString(line, -1) {
				out.add(b)
			}
		}
		return nil
	})
	require.NoError(t, err, "walk repo for markers")
	return out.slice()
}

// quarantineBeadSet is a tiny string set used to collect bead ids without duplicates.
type quarantineBeadSet struct{ m map[string]struct{} }

func newQuarantineBeadSet() *quarantineBeadSet { return &quarantineBeadSet{m: map[string]struct{}{}} }
func (s *quarantineBeadSet) add(v string)      { s.m[v] = struct{}{} }
func (s *quarantineBeadSet) slice() []string {
	out := make([]string, 0, len(s.m))
	for k := range s.m {
		out = append(out, k)
	}
	return out
}
