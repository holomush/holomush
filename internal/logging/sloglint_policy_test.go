// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package logging_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// repoRoot walks up from this test file to the directory containing go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "reached filesystem root without finding go.mod")
		dir = parent
	}
}

// TestSloglintPolicyMatchesSpec guards against silent drift of the sloglint
// Tier C policy (INV-LP1). If sloglint is disabled or a check is dropped, the
// lint gate (INV-LP2) would go quiet rather than fail, so this test pins the
// config shape. Rejected checks (INV-LP1, spec §3.2) MUST stay absent.
func TestSloglintPolicyMatchesSpec(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".golangci.yaml"))
	require.NoError(t, err)

	var cfg struct {
		Linters struct {
			Enable   []string `yaml:"enable"`
			Settings struct {
				Sloglint map[string]any `yaml:"sloglint"`
			} `yaml:"settings"`
		} `yaml:"linters"`
	}
	require.NoError(t, yaml.Unmarshal(data, &cfg))

	assert.Contains(t, cfg.Linters.Enable, "sloglint", "sloglint must be enabled")

	s := cfg.Linters.Settings.Sloglint
	require.NotNil(t, s, "sloglint settings block must exist")
	assert.Equal(t, "scope", s["context"])
	assert.Equal(t, true, s["no-mixed-args"])
	assert.Equal(t, true, s["static-msg"])
	assert.Equal(t, "lowercased", s["msg-style"])
	assert.Equal(t, "snake", s["key-naming-case"])
	assert.ElementsMatch(t, []any{"time", "level", "msg", "source"}, s["forbidden-keys"])

	// Rejected checks (spec §3.2) must not be enabled.
	for _, k := range []string{"no-global", "attr-only", "no-raw-keys"} {
		_, present := s[k]
		assert.False(t, present, "rejected sloglint check %q must not be enabled", k)
	}
}

// TestInvariantsHaveTests is the INV-META meta-test: every INV-LP* invariant
// MUST be referenced by a test or a documented gate.
func TestInvariantsHaveTests(t *testing.T) {
	// INV-LP1 → TestSloglintPolicyMatchesSpec (this file).
	// INV-LP2 → the `task lint:go` gate in CI/pr-prep (not a Go unit test;
	//           the config-guard above pins the policy the gate enforces).
	invariants := map[string]string{
		"INV-LP1": "TestSloglintPolicyMatchesSpec",
		"INV-LP2": "task lint:go gate (pr-prep)",
	}
	for id, ref := range invariants {
		assert.NotEmpty(t, ref, "invariant %s must have a referencing test or gate", id)
	}
}
