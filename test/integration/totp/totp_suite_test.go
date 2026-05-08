// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package totp_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// suiteT captures *testing.T from Ginkgo bootstrap so spec bodies can pass
// it to helpers that take testing.TB (e.g., testutil.SharedPostgres).
// Mirrors the eventbus_e2e and crypto suite patterns.
var suiteT *testing.T

// TestTOTP runs the Ginkgo specs for the TOTP substrate integration suite.
// Per spec §"Testing approach" / R5 Option Y: PG + KEK only — no eventbus,
// no audit-publisher assertions. Sub-epic D's E2E covers events_audit
// once the OperatorAuthProvider lands.
func TestTOTP(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "TOTP Substrate Integration Suite")
}
