// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package resilience_test is the two-replica world-model resilience suite
// (OPS-05, #4791). It boots N in-process CoreServer replicas over ONE real NATS
// JetStream container and ONE shared Postgres database (D-03) and characterizes
// how the direct-write CRUD world model behaves under concurrency and failure:
//
//   - M12 last-write-wins: concurrent writes to the same world row from two
//     replicas, and which update survives (plan 02);
//   - M2 dual-write window: the gap between the Postgres commit and the
//     post-commit event-log notification, observed under a broker flap (plan 03);
//   - substrate proofs: two replicas genuinely share one database and one broker,
//     and agree on the game id (this file's boot smoke).
//
// The suite is deliberately opt-in (D-05). Two-replica chaos stands up two full
// stacks plus a NATS container per suite, so it is CI-resource-sensitive and is
// NOT part of the required Integration Test PR gate. It runs on the existing
// nightly Quarantine Health lane (via HOLOMUSH_RUN_QUARANTINED=1) and locally on
// demand. It is NOT a test/quarantine.yaml entry: it is not a flaky spec awaiting
// a fix, it is an intentionally heavyweight suite that reuses the same env toggle
// as the gating mechanism. Accordingly, no file in this package may contain any
// of the in-code quarantine-marker patterns enforced by the bijection meta-test
// (see the regex at test/meta/quarantine_registry_test.go:31) — the gate below
// uses quarantinetest.Enabled() only.
package resilience_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/testsupport/quarantinetest"
)

// suiteT exposes the *testing.T from TestWorldModelResilience so Ginkgo Describe
// blocks can pass it to integrationtest.Start and natstest helpers (both require
// *testing.T / testing.TB — Ginkgo's GinkgoT() does not satisfy those directly).
var suiteT *testing.T

// TestWorldModelResilience is the Ginkgo entry point. The name is stable so
// `task test:int -- -run TestWorldModelResilience ./test/integration/resilience/`
// selects it. It gates FIRST on quarantinetest.Enabled(): with the env unset
// (the required Integration Test PR lane) it skips before booting anything, so
// zero specs run and no CI resources are consumed (D-05).
func TestWorldModelResilience(t *testing.T) {
	if !quarantinetest.Enabled() {
		t.Skipf("resilience harness is nightly/opt-in: set %s=1 to run (#4791)", quarantinetest.EnvVar)
	}
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "World-Model Resilience Suite")
}
