// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package codeckeybytesallowlist_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/codeckeybytesallowlist"
)

func TestAnalyzerFlagsCodecKeyBytesReadsExceptFromAllowlist(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), codeckeybytesallowlist.Analyzer,
		"github.com/holomush/holomush/plugins/positive",
		"github.com/holomush/holomush/plugins/negative",
		"github.com/holomush/holomush/internal/eventbus/codec/allow",
		"github.com/holomush/holomush/internal/eventbus/crypto/allow",
	)
}
