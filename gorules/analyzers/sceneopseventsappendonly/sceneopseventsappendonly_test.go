// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package sceneopseventsappendonly_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/sceneopseventsappendonly"
)

func TestAnalyzerFlagsForbiddenSQLAgainstSceneOpsEvents(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), sceneopseventsappendonly.Analyzer,
		"example.com/positive",
		"example.com/negative",
	)
}
