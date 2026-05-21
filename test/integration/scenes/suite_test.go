// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package scenes_test contains Ginkgo integration specs for scene-domain
// invariants exercised against the full eventbus substrate (real embedded
// JetStream via eventbustest). Specs pin invariants from the Scenes Phase 4
// design:
//
//	docs/superpowers/specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md
//
// Suite conventions mirror test/integration/eventbus_e2e:
//
//   - Every spec uses freshBus() for an embedded JetStream bus scoped to the
//     spec (via DeferCleanup), so per-spec NATS servers do not accumulate.
//   - No time.Sleep — synchronization is via context deadlines on Next().
package scenes_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/eventbustest"
)

// suiteT captures the testing.T from the Ginkgo bootstrap so spec bodies can
// build resources scoped per-spec (the embedded bus, in particular). Mirrors
// the eventbus_e2e/cursor_concurrent_suite_test.go pattern.
var suiteT *testing.T

// TestScenes is the Ginkgo entry point for the scenes integration suite.
func TestScenes(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scenes Integration Suite")
}

// specTB wraps the suite-level *testing.T but redirects Cleanup to Ginkgo's
// DeferCleanup so per-spec resources (NATS embedded bus) tear down when the
// It finishes — not when the whole suite ends. Without this, every spec's
// eventbustest.New would accumulate live NATS servers for the entire suite
// run, eventually starving the suite of resources.
type specTB struct {
	*testing.T
}

func (s *specTB) Cleanup(fn func()) {
	DeferCleanup(fn)
}

// freshBus returns an embedded NATS+JetStream bus whose cleanup is scoped to
// the current Ginkgo spec (not the whole suite). Use this in spec bodies
// (BeforeEach or It) instead of eventbustest.New(suiteT).
func freshBus() *eventbustest.Embedded {
	return eventbustest.New(&specTB{T: suiteT})
}
