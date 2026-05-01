// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// Documentation file (not compiled). Each function below documents a
// pattern the DEKMaterialNo* rules in gorules/rules.go SHOULD flag.
// (The Phase 2 plan originally split these into a separate
// gorules/dek_no_serialize.go file, but golangci-lint v2.11's gocritic
// ruleguard config silently fails to load multi-file rule sets, so the
// rules were concatenated into the single rules.go per the plan's
// fallback guidance.) To smoke-test a rule, copy a function into a
// real package, save, and run `task lint` — the rule should fire on
// the marked line.
//
// This file is not built (build tag prevents compilation); it exists
// for reviewer reference only.

//go:build ignore_fixture
// +build ignore_fixture

package documentation

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func leakViaJSON(m *dek.Material) ([]byte, error) {
	return json.Marshal(m) // EXPECT: INV-27: dek.Material MUST NOT be passed to encoding/json
}

func leakViaFmtSprintf(m *dek.Material) string {
	return fmt.Sprintf("%v", m) // EXPECT: INV-27: dek.Material MUST NOT be passed to fmt formatting
}

func leakViaLogPrintf(m *dek.Material) {
	log.Printf("material: %v", m) // EXPECT: INV-27: dek.Material MUST NOT be passed to log functions
}

func leakViaSlogInfo(m *dek.Material) {
	slog.Info("dek", "material", m) // EXPECT: INV-27: dek.Material MUST NOT be passed to log/slog
}
