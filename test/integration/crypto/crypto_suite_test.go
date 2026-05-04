// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package crypto_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// suiteT captures the *testing.T from Ginkgo bootstrap so spec bodies can
// pass it to helpers that take testing.TB (e.g., testutil.SharedPostgres).
// Mirrors the eventbus_e2e/cursor_concurrent_suite_test.go pattern.
var suiteT *testing.T

// TestCrypto runs the Ginkgo specs for the crypto integration suite.
// Plain testing.T functions in this package (emit_test.go, metadata_only_
// test.go) coexist; Ginkgo only runs Describe/It blocks.
func TestCrypto(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Crypto Integration Suite")
}
