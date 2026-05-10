// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// adminAuthSuiteT captures *testing.T from Ginkgo bootstrap so the spec body
// can pass it to helpers that take testing.TB (e.g., testutil.SharedPostgres).
// Mirrors the eventbus_e2e and crypto suite patterns.
//
//nolint:gochecknoglobals // Ginkgo suite-level *testing.T capture (standard pattern across this repo).
var adminAuthSuiteT *testing.T

// TestAdminAuthenticateE2E runs the Ginkgo specs for the admin Authenticate /
// ResetTOTP lifecycle, booted through the production runCoreWithDeps entry
// point. See admin_authenticate_e2e_test.go for spec bodies.
//
// Lives in cmd/holomush/ (package main) rather than test/integration/admin/
// because runCoreWithDeps is package-private to main and the plan's hard
// requirement (full-server boot via runCoreWithDeps; no subsystem-direct
// instantiation) cannot otherwise be satisfied without exporting the entry
// point — a production-API change well outside T25's scope. Documented in
// commit body. Plan: docs/plans/2026-04-23-phase5-sub-epic-d-exec.md T25.
func TestAdminAuthenticateE2E(t *testing.T) {
	adminAuthSuiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Admin Authenticate E2E Lifecycle Suite")
}
