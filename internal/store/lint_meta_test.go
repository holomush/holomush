// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestLintNoMicrosecondTruncatePasses pins INV-TS-3 via the lint task.
// If anyone reintroduces a microsecond-precision truncate call without a
// pgnanos-exempt annotation, this test fails along with the lint.
//
// The `task` shell-out is bounded by a 2-minute context so a stuck linter
// can't hang CI.
func TestLintNoMicrosecondTruncatePasses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping shell-out lint test in short mode")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "task", "lint:no-microsecond-truncate")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("INV-TS-3 violated: %s\n%s", err, strings.TrimSpace(string(out)))
	}
}
