// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package chain_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// suiteT captures the *testing.T from Ginkgo bootstrap so spec bodies can
// pass it to helpers that take testing.TB (e.g., testutil.SharedPostgres).
// Mirrors the eventbus_e2e and crypto suite patterns.
var suiteT *testing.T

// TestChainIntegration is the Ginkgo entry point for the
// internal/eventbus/audit/chain integration suite.
func TestChainIntegration(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Audit Chain Integration Suite")
}
