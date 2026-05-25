// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDepguardTestOnlyConstructRulesPresent guards INV-1/INV-2 against silent
// deletion — the exact failure mode (a config claim silently diverging from
// reality) this work was created to correct (holomush-1eps2).
func TestDepguardTestOnlyConstructRulesPresent(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".golangci.yaml"))
	require.NoError(t, err, "read .golangci.yaml")
	cfg := string(data)

	for _, pkg := range []string{
		"github.com/holomush/holomush/internal/eventbus/eventbustest",
		"github.com/holomush/holomush/internal/core/coretest",
	} {
		require.Contains(t, cfg, pkg,
			"depguard deny rule for %q missing from .golangci.yaml (holomush-1eps2 INV-1/INV-2)", pkg)
	}
}

// TestTaskfileIntHasNoPackageList guards INV-3: the test:int recipe must run
// ./... (honoring CLI_ARGS) and must NOT re-introduce an enumerated package
// list (holomush-1eps2, absorbs holomush-bmtd).
func TestTaskfileIntHasNoPackageList(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "Taskfile.yaml"))
	require.NoError(t, err, "read Taskfile.yaml")
	tf := string(data)

	// Isolate the test:int recipe block: from its key line to the next
	// 2-space-indented task key.
	loc := regexp.MustCompile(`(?m)^  test:int:[ \t]*$`).FindStringIndex(tf)
	require.NotNil(t, loc, "test:int target not found in Taskfile.yaml")
	after := tf[loc[1]:]
	block := after
	if next := regexp.MustCompile(`(?m)^  \S`).FindStringIndex(after); next != nil {
		block = after[:next[0]]
	}

	require.Contains(t, block, "CLI_ARGS",
		"test:int must honor CLI_ARGS (holomush-1eps2 INV-3 / bmtd)")
	require.NotContains(t, block, "./internal/",
		"test:int must not enumerate packages; use ./... (holomush-1eps2 INV-3)")
}
