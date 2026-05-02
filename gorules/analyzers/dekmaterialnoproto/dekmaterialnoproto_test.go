// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dekmaterialnoproto_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/dekmaterialnoproto"
)

func TestAnalyzerFlagsDEKMaterialPassedToProtoSinks(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), dekmaterialnoproto.Analyzer,
		"github.com/holomush/holomush/plugins/positive",
		"github.com/holomush/holomush/plugins/negative",
	)
}
