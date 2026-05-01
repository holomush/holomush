// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dekmaterialnoslog_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/dekmaterialnoslog"
)

func TestAnalyzerFlagsDEKMaterialPassedToSlogSinks(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), dekmaterialnoslog.Analyzer,
		"github.com/holomush/holomush/plugins/positive",
		"github.com/holomush/holomush/plugins/negative",
	)
}
