// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invariants

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestINV_P4_1_NoColonStyleSceneSubjects rg-asserts that no production
// code path in plugins/core-scenes/ or scene-aware substrate code
// contains a "scene:" string literal in pub/sub-topic context. ABAC
// Resource ID context (NewAccessRequest, Grant) is excluded — that's
// policy-DSL serialization, not a topic (spec §3.2).
//
// Phase 4 (holomush-5rh.13) migrated scenes to NATS dot-style; INV-P4-1
// pins the migration as a CI-enforced invariant. The broader sweep for
// non-scene colon-style (location:*, character:*, etc.) is tracked by
// holomush-rops.
func TestINV_P4_1_NoColonStyleSceneSubjects(t *testing.T) {
	t.Parallel()
	targets := []string{
		"../../../plugins/core-scenes/service.go",
		"../../../plugins/core-scenes/commands.go",
		"../../../plugins/core-scenes/audit.go",
		"../../../plugins/core-scenes/store.go",
		"../../../plugins/core-scenes/resolver.go",
		"../../../plugins/core-scenes/main.go",
		"../../../plugins/core-scenes/poseorder.go", // exists after T9
		"../../../internal/grpc/stream_access.go",
		"../../../internal/grpc/scope_floor.go",
		"../../../internal/grpc/query_stream_history.go",
	}

	// Match "scene:" string literals.
	pattern := regexp.MustCompile(`"scene:"`)

	// ABAC policy-DSL context markers — a containing line that
	// includes one of these substrings is a false-positive (ABAC
	// resource ID, not a topic).
	abacContextMarkers := []string{
		"NewAccessRequest",
		".Grant(",
		"resource:",
		"Resource:",
	}

	for _, path := range targets {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				// File doesn't exist yet (e.g. poseorder.go before T9
				// lands). Skip with a soft message; T28 coverage
				// meta-test catches missing files separately.
				t.Skipf("file does not exist yet: %s", path)
				return
			}
			require.NoError(t, err)

			matches := pattern.FindAllIndex(data, -1)
			for _, m := range matches {
				lineNum := bytes.Count(data[:m[0]], []byte("\n")) + 1
				lineStart := bytes.LastIndexByte(data[:m[0]], '\n') + 1
				lineEnd := lineStart + bytes.IndexByte(data[lineStart:], '\n')
				if lineEnd < lineStart {
					lineEnd = len(data)
				}
				line := string(data[lineStart:lineEnd])

				// Skip false-positives (ABAC policy-DSL context).
				abacContext := false
				for _, marker := range abacContextMarkers {
					if strings.Contains(line, marker) {
						abacContext = true
						break
					}
				}
				if abacContext {
					continue
				}

				t.Errorf("INV-P4-1 violated: %s:%d uses colon-style scene subject in pub/sub-topic context:\n  %s",
					path, lineNum, strings.TrimSpace(line))
			}
		})
	}
}
