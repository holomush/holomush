// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cursorpackageinternal_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/cursorpackageinternal"
)

func TestAnalyzerFlagsCursorRefsExceptFromAllowlistedPackages(t *testing.T) {
	analysistest.Run(
		t, analysistest.TestData(), cursorpackageinternal.Analyzer,
		"github.com/holomush/holomush/plugins/positive",
		"github.com/holomush/holomush/plugins/blankimport",
		"github.com/holomush/holomush/internal/eventbus/allow",
		"github.com/holomush/holomush/internal/grpc/allow",
		"github.com/holomush/holomush/internal/web/allow",
		"github.com/holomush/holomush/internal/plugin/goplugin/allow",
		"github.com/holomush/holomush/internal/plugin/hostfunc/allow",
		"github.com/holomush/holomush/internal/plugin/hostcap/allow",
	)
}
