// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// repoRoot returns the repository root by walking up from this test file's
// directory until it finds a `.jj` or `.git` marker. Tests in this file
// read repo-relative files and must run from any cwd.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		for _, marker := range []string{".jj", ".git"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root from %s", filepath.Dir(file))
		}
		dir = parent
	}
}

// grepFile returns lines (1-indexed) in path matching pattern, or empty
// slice on miss. Uses os.ReadFile + regexp to keep gosec happy (no
// subprocess execution).
func grepFile(t *testing.T, path string, pattern *regexp.Regexp) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var hits []string
	for i, line := range strings.Split(string(data), "\n") {
		if pattern.MatchString(line) {
			hits = append(hits, strings.TrimSpace(line)+" (line "+itoa(i+1)+")")
		}
	}
	return hits
}

// TestProtoHasNoLegacyIDField asserts INV-PLUGIN-15 at the proto schema layer:
// `legacy_id` MUST NOT appear as an active field in the eventbus proto.
// `reserved` declarations (which intentionally name the deleted field to
// prevent accidental reuse of field number 3) are explicitly permitted.
func TestProtoHasNoLegacyIDField(t *testing.T) {
	root := repoRoot(t)
	hits := grepFile(t,
		filepath.Join(root, "api/proto/holomush/eventbus/v1/eventbus.proto"),
		regexp.MustCompile(`legacy_id`))
	// Filter out `reserved` declarations and explanatory comments — those
	// are part of the deletion-defense convention, not field references.
	var active []string
	for _, line := range hits {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "reserved ") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		active = append(active, line)
	}
	if len(active) > 0 {
		t.Fatalf("legacy_id MUST NOT exist as an active field in eventbus.proto:\n%s", strings.Join(active, "\n"))
	}
}

// TestRegeneratedPbGoHasNoLegacyId asserts INV-PLUGIN-15 at the regenerated
// proto Go code: `LegacyId` MUST NOT appear.
func TestRegeneratedPbGoHasNoLegacyId(t *testing.T) {
	root := repoRoot(t)
	hits := grepFile(t,
		filepath.Join(root, "pkg/proto/holomush/eventbus/v1/eventbus.pb.go"),
		regexp.MustCompile(`LegacyId`))
	if len(hits) > 0 {
		t.Fatalf("LegacyId MUST NOT exist in regenerated eventbus.pb.go:\n%s", strings.Join(hits, "\n"))
	}
}

// TestEventbusActorStructHasNoLegacyID asserts INV-PLUGIN-15 at the Go struct
// layer: `LegacyID` MUST NOT exist on eventbus.Actor.
func TestEventbusActorStructHasNoLegacyID(t *testing.T) {
	root := repoRoot(t)
	hits := grepFile(t,
		filepath.Join(root, "internal/eventbus/types.go"),
		regexp.MustCompile(`LegacyID`))
	if len(hits) > 0 {
		t.Fatalf("LegacyID MUST NOT exist in eventbus.Actor:\n%s", strings.Join(hits, "\n"))
	}
}

// TestPublisherHasNoLegacyIDReferences asserts INV-PLUGIN-15 at the publisher
// layer: no LegacyID/legacy_id/header references remain.
func TestPublisherHasNoLegacyIDReferences(t *testing.T) {
	root := repoRoot(t)
	hits := grepFile(t,
		filepath.Join(root, "internal/eventbus/publisher.go"),
		regexp.MustCompile(`LegacyID|legacy_id|App-Actor-Legacy-ID`))
	if len(hits) > 0 {
		t.Fatalf("LegacyID-related symbols MUST NOT exist in publisher.go:\n%s", strings.Join(hits, "\n"))
	}
}
