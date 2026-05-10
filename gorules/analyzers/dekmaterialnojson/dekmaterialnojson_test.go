// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dekmaterialnojson_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/dekmaterialnojson"
)

func TestAnalyzerFlagsDEKMaterialPassedToJSONSinks(t *testing.T) {
	analysistest.Run(
		t, analysistest.TestData(), dekmaterialnojson.Analyzer,
		"github.com/holomush/holomush/plugins/positive",
		"github.com/holomush/holomush/plugins/negative",
	)
}
