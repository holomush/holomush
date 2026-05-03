// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package noremoteclockcompare_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/noremoteclockcompare"
)

func TestAnalyzerFlagsRemoteClockComparesAndIgnoresLocalOnes(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), noremoteclockcompare.Analyzer,
		"example.com/positive",
		"example.com/negative",
	)
}
