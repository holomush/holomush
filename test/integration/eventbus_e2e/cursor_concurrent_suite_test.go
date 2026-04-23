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
//
// Other test files in this package use plain testing.T directly; they don't
// run through the Ginkgo entry point and ignore suiteT. Filed as
// holomush-suos.2 to convert the rest of the directory.
var suiteT *testing.T

// TestCursorConcurrentSpecs is the Ginkgo entry point for the
// cursor_concurrent_test.go specs. Other Test* functions in this package
// run independently of this entry point.
func TestCursorConcurrentSpecs(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cursor Concurrent Pagination Specs")
}
