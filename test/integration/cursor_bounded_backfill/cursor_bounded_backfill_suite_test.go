// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package cursor_bounded_backfill_test exercises the holomush-iu8j
// cursor-bounded backfill end-to-end:
//
//   - server-side: QueryStreamHistory honors NotAfterMs derived from the
//     Subscribe attach moment (filtering events with timestamp > bound)
//   - server-side: REPLAY_COMPLETE ControlFrame carries attach_moment_ms
//     populated from the server's wall clock at OpenSession-return time
//   - client-relevant: backfill scoped to attach_moment_ms cannot return
//     events that arrived after the Subscribe attach (the structural
//     fix for the holomush-fujt connect-time replay/backfill race)
//
// Tests use the integrationtest harness so the full gRPC handler runs
// against real Postgres (testcontainers) + embedded NATS JetStream.
package cursor_bounded_backfill_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// suiteT exposes the *testing.T from TestCursorBoundedBackfill so Ginkgo
// Describe blocks can pass it to integrationtest.Start (which requires
// *testing.T — Ginkgo's GinkgoT() does not satisfy that interface
// directly). Mirrors the pattern in test/integration/privacy/.
var suiteT *testing.T

// TestCursorBoundedBackfill is the Ginkgo entry point for the suite.
func TestCursorBoundedBackfill(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cursor-Bounded Backfill (holomush-iu8j) Integration Suite")
}
