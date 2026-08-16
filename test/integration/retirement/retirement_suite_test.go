// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package retirement_test carries the full-stack proof that character
// retirement is OBSERVABLY effective (IDENT-04, ROADMAP success criterion 2):
// a RetireCharacter write commits its envelope, the REAL outbox relay publishes
// it, the REAL durable consumer delivers it, and the reactor's fanout ends the
// session, announces the leave at the location the character left, emits
// session_ended with cause retired, and moves the character to the starting
// location.
//
// It is its OWN package for a reason that is the whole point of this suite. A
// Ginkgo package has exactly one RunSpecs entry point, so `go test -run X` can
// only ever match a top-level Go test function — never a Describe. Before this
// package existed, `-run TestRetirementReactor` was pointed at
// test/integration/world/, whose only entry function is TestWorld: the filter
// matched nothing, ran nothing, and exited 0. TestRetirementReactor below is
// that filter's REAL target.
package retirement_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// suiteT captures the *testing.T from Ginkgo bootstrap so spec bodies can pass
// it to helpers that take *testing.T (integrationtest.Start).
var suiteT *testing.T

// TestRetirementReactor is this package's ONLY RunSpecs entry point.
func TestRetirementReactor(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Retirement Reactor Suite")
}
