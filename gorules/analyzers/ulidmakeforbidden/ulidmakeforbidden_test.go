// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package ulidmakeforbidden_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/ulidmakeforbidden"
)

func TestAnalyzerFlagsUlidMakeAndIgnoresNegativeCases(t *testing.T) {
	analysistest.Run(
		t, analysistest.TestData(), ulidmakeforbidden.Analyzer,
		"example.com/positive",
		"example.com/negative",
	)
}
