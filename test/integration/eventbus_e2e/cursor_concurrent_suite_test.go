// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// suiteT captures the testing.T from the Ginkgo bootstrap so spec bodies can
// invoke local helpers (freshPool, drainStream, currentStreamLastSeq,
// buildReader) which take *testing.T. Mirrors the world_suite_test.go pattern.
var suiteT *testing.T

// TestEventbusE2E is the Ginkgo entry point for the entire eventbus_e2e
// package. Renamed from TestCursorConcurrentSpecs when holomush-cz4s
// landed the rest of the directory's conversions; the variable suiteT
// (set on line below) is still required by cursor_concurrent_test.go's
// helpers which take *testing.T.
func TestEventbusE2E(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "EventbusE2E Suite")
}
