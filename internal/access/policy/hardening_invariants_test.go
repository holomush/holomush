// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"os"
	"strings"
	"testing"
)

// TestPluginABACHardeningSourceCodeDoesNotContainPreflightSentinel is a
// static assertion that the legacy synthetic preflight sentinel has been
// removed from the engine. The Plugin ABAC Hardening spec (2026-04-07)
// requires this literal to be deleted from engine.go — not filtered —
// because the invariant "plugin providers never see synthetic IDs" is
// enforced by construction, not by runtime filtering.
//
// If this test fails, somebody has re-introduced the synthetic preflight
// path. Read docs/superpowers/specs/2026-04-07-plugin-abac-hardening-design.md
// for the rationale before suppressing this test.
func TestPluginABACHardeningSourceCodeDoesNotContainPreflightSentinel(t *testing.T) {
	data, err := os.ReadFile("engine.go")
	if err != nil {
		t.Fatalf("failed to read engine.go: %v", err)
	}
	if strings.Contains(string(data), "__preflight__") {
		t.Errorf("engine.go contains the synthetic preflight sentinel " +
			"'__preflight__'. This literal was removed in the Plugin ABAC " +
			"Hardening work (spec 2026-04-07). If you need to re-introduce " +
			"preflight-aware behavior, read the spec first.")
	}
}
