// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package policy_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/admin/policy"
)

// TestPolicyIntegration is the Ginkgo entry point for the
// internal/admin/policy integration suite. The shared testPool /
// testcontainer setup lives in TestMain (verifier_integration_test.go);
// any Describe / Context / It nodes in this package are picked up by
// RunSpecs.
//
// Coexists with classic-style Test* functions (emitter_test.go,
// verifier_integration_test.go::TestVerifyChainAgainstRealEventsAudit,
// etc.) — Go's test runner invokes both surfaces, sharing testPool.
func TestPolicyIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "internal/admin/policy Integration Suite")
}

// cleanupSubjectGinkgo is the Ginkgo equivalent of cleanupSubject
// (emitter_test.go). Pre-deletes any leftover events_audit rows for the
// subject and registers a DeferCleanup that re-deletes after the
// surrounding It / BeforeEach completes.
func cleanupSubjectGinkgo(subject string) {
	GinkgoHelper()
	_, _ = testPool.Exec(context.Background(),
		`DELETE FROM events_audit WHERE subject = $1`, subject)
	DeferCleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM events_audit WHERE subject = $1`, subject)
	})
}

// chainStateCleanupGinkgo is the Ginkgo equivalent of chainStateCleanup
// (verifier_integration_test.go). Drops the bootstrap_metadata row that
// records chain-init for (gameID, policyName) both BEFORE the spec runs
// (so a stale row from a prior run doesn't make the spec order-dependent)
// and after via DeferCleanup. Mirrors cleanupSubjectGinkgo's pre+post shape.
//
// Post Phase 5 sub-epic E migration 000030: bootstrap_metadata is keyed
// on (chain_name, scope_key); chain_name = SubjectPrefix from
// PolicySetChainFor(gameID).
func chainStateCleanupGinkgo(gameID, policyName string) {
	GinkgoHelper()
	chainName := policy.PolicySetChainFor(gameID).SubjectPrefix
	_, _ = testPool.Exec(context.Background(),
		`DELETE FROM bootstrap_metadata WHERE chain_name = $1 AND scope_key = $2`,
		chainName, policyName)
	DeferCleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM bootstrap_metadata WHERE chain_name = $1 AND scope_key = $2`,
			chainName, policyName)
	})
}
