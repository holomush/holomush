// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Generator versions are stamped into every committed stub (pkg/proto,
// internal/eventbus/cursor/cursorv1, web/src/lib/connect), so a codegen pin
// bump that lands without regeneration leaves main claiming one generator
// version in buf.gen*.yaml and another in the files. These tests pin the gate
// that makes that impossible to merge green, which is what lets Renovate's
// codegen groups auto-merge again (issue #5059).

// codegenPins are the Renovate-tracked packages whose bumps rewrite generated
// output: the Go BSR plugin pins and their go.mod runtimes, and the web es pin.
var codegenPins = []string{
	"google.golang.org/protobuf",
	"connectrpc.com/connect",
	"google.golang.org/grpc/cmd/protoc-gen-go-grpc",
	"@bufbuild/protoc-gen-es",
}

func TestCILintJobRegeneratesCodegenAndFailsOnDrift(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yaml"))
	require.NoError(t, err)

	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(data, &wf))
	lint, ok := wf.Jobs["lint"]
	require.True(t, ok, "ci.yaml has no lint job")

	runs := make([]string, 0, len(lint.Steps))
	for _, s := range lint.Steps {
		runs = append(runs, s.Run)
	}
	joined := strings.Join(runs, "\n")
	assert.Contains(t, joined, "task proto:check", "Lint job must regenerate Go stubs and fail on drift")
	assert.Contains(t, joined, "task web:generate:check", "Lint job must regenerate web Connect clients and fail on drift")
}

func TestTaskfileCodegenCheckTasksExistAndGatePrPrep(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "Taskfile.yaml"))
	require.NoError(t, err)

	var tf struct {
		Tasks map[string]struct {
			Cmds []any `yaml:"cmds"`
		} `yaml:"tasks"`
	}
	require.NoError(t, yaml.Unmarshal(data, &tf))

	for _, name := range []string{"proto:check", "web:generate:check"} {
		task, ok := tf.Tasks[name]
		require.True(t, ok, "Taskfile.yaml must define %s", name)
		body := flattenCmds(task.Cmds)
		assert.Contains(t, body, "--force", "%s must force regeneration (the generate task is otherwise skipped as up-to-date)", name)
		assert.Contains(t, body, "git status --porcelain", "%s must fail on modified OR untracked generated files", name)
	}

	fast, ok := tf.Tasks["pr-prep:fast:run"]
	require.True(t, ok)
	lane := flattenCmds(fast.Cmds)
	assert.Contains(t, lane, "proto:check", "pr-prep fast lane must mirror the CI codegen gate")
	assert.Contains(t, lane, "web:generate:check", "pr-prep fast lane must mirror the CI codegen gate")
}

// flattenCmds renders go-task cmds (plain strings, {cmd: ...} and {task: ...}
// items) into one searchable string.
func flattenCmds(cmds []any) string {
	var b strings.Builder
	for _, c := range cmds {
		switch v := c.(type) {
		case string:
			b.WriteString(v)
		case map[string]any:
			for k, val := range v {
				if s, ok := val.(string); ok {
					b.WriteString(k + ": " + s)
				}
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestRenovateCodegenGroupsAutoMergeMinorAndPatchOnly(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "renovate.json"))
	require.NoError(t, err)

	var cfg struct {
		PackageRules []struct {
			GroupName         string   `json:"groupName"`
			MatchPackageNames []string `json:"matchPackageNames"`
			MatchUpdateTypes  []string `json:"matchUpdateTypes"`
			Automerge         *bool    `json:"automerge"`
		} `json:"packageRules"`
	}
	require.NoError(t, json.Unmarshal(data, &cfg))

	seenGroups := map[string]bool{}
	automergeCovered := map[string]bool{}
	for _, r := range cfg.PackageRules {
		if r.GroupName == "buf codegen" || r.GroupName == "buf codegen go" {
			seenGroups[r.GroupName] = true
			if r.Automerge != nil {
				assert.True(t, *r.Automerge, "group %q must not disable automerge: CI's codegen stale-diff gate is the safeguard now", r.GroupName)
			}
		}
		if r.Automerge != nil && *r.Automerge && len(r.MatchUpdateTypes) > 0 {
			for _, ut := range r.MatchUpdateTypes {
				require.NotEqual(t, "major", ut, "codegen automerge rule must not cover majors")
			}
			for _, p := range r.MatchPackageNames {
				automergeCovered[p] = true
			}
		}
	}
	require.True(t, seenGroups["buf codegen"] && seenGroups["buf codegen go"], "both codegen groups must still exist")
	for _, p := range codegenPins {
		assert.True(t, automergeCovered[p], "codegen pin %q needs an explicit minor/patch automerge rule (the regex-manager pin inherits none from the gomod/npm rules)", p)
	}
}
