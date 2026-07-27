// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestDepguardTestOnlyConstructRulesPresent guards INV-1/INV-2 against silent
// deletion — the exact failure mode (a config claim silently diverging from
// reality) this work was created to correct (holomush-1eps2).
func TestDepguardTestOnlyConstructRulesPresent(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".golangci.yaml"))
	require.NoError(t, err, "read .golangci.yaml")

	// The pinned set MUST match the CONFIGURED deny set exactly, not a subset
	// of it. A pin that covers only some entries lets the uncovered ones be
	// deleted silently — the same config-diverges-from-reality failure this
	// test exists to catch. natstest was configured but unpinned until phase 09
	// (QUAL-04); integrationtest was added there.
	//
	// "Exactly" is why this parses the YAML rather than running Contains over
	// the file text. A Contains loop only walks the pinned slice, so it checks
	// pinned ⊆ configured and says nothing about the reverse: a sixth deny entry
	// added to .golangci.yaml without touching this test would be unpinned, and
	// a later change could delete it silently — reproducing the exact failure
	// the paragraph above claims to prevent.
	pinned := []string{
		"github.com/holomush/holomush/internal/eventbus/eventbustest",
		"github.com/holomush/holomush/internal/core/coretest",
		"github.com/holomush/holomush/internal/testsupport/quarantinetest",
		"github.com/holomush/holomush/internal/testsupport/natstest",
		"github.com/holomush/holomush/internal/testsupport/integrationtest",
	}

	var cfgDoc struct {
		Linters struct {
			Settings struct {
				Depguard struct {
					Rules map[string]struct {
						Deny []struct {
							Pkg string `yaml:"pkg"`
						} `yaml:"deny"`
					} `yaml:"rules"`
				} `yaml:"depguard"`
			} `yaml:"settings"`
		} `yaml:"linters"`
	}
	require.NoError(t, yaml.Unmarshal(data, &cfgDoc), "parse .golangci.yaml")

	const ruleName = "no-test-only-constructs-in-production"
	rule, ok := cfgDoc.Linters.Settings.Depguard.Rules[ruleName]
	require.Truef(t, ok,
		"depguard rule %q MUST exist in .golangci.yaml (holomush-1eps2 INV-1/INV-2); without "+
			"it every assertion below would range over an empty deny list", ruleName)

	configured := make([]string, 0, len(rule.Deny))
	for _, d := range rule.Deny {
		configured = append(configured, d.Pkg)
	}
	require.NotEmptyf(t, configured, "depguard rule %q has an empty deny list", ruleName)

	require.ElementsMatch(t, pinned, configured,
		"the pinned set and the CONFIGURED deny set MUST be identical. An entry present in "+
			"the config but missing here is unpinned and can be deleted silently later; an "+
			"entry pinned here but missing from the config is a rule that has already been "+
			"deleted (holomush-1eps2 INV-1/INV-2)")
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
