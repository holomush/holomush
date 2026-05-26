// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package quarantinetest gates known-flaky integration/E2E specs. A spec
// marked with Skip self-skips in gating runs (env unset) and runs in the
// nightly lane (HOLOMUSH_RUN_QUARANTINED=1). Every Skip MUST cite an open
// bead and have a matching row in test/quarantine.yaml (enforced by the
// bijection meta-test in test/meta). Production code MUST NOT import this
// package (depguard-enforced). See
// docs/superpowers/specs/2026-05-25-tier-split-quality-gates-design.md.
package quarantinetest

import (
	"os"
	"testing"
)

// EnvVar toggles whether quarantined specs run. Set to "1" in the nightly
// lane; unset everywhere else (so quarantined specs self-skip in gating CI).
const EnvVar = "HOLOMUSH_RUN_QUARANTINED"

// Enabled reports whether quarantined specs should run.
func Enabled() bool { return os.Getenv(EnvVar) == "1" }

// Skip skips the test as quarantined unless Enabled(). bead MUST be the
// tracking bead id (e.g. "holomush-q55b") and MUST appear in
// test/quarantine.yaml.
func Skip(t *testing.T, bead string) {
	t.Helper()
	if !Enabled() {
		t.Skipf("quarantined: %s (set %s=1 to run)", bead, EnvVar)
	}
}
