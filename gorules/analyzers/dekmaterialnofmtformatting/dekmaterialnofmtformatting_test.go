// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dekmaterialnofmtformatting_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/dekmaterialnofmtformatting"
)

func TestAnalyzerFlagsDEKMaterialPassedToFmtFormattingSinks(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), dekmaterialnofmtformatting.Analyzer,
		"github.com/holomush/holomush/plugins/positive",
		"github.com/holomush/holomush/plugins/negative",
	)
}
