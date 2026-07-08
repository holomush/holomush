// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// canonicalNonExemptTokens mirrors the non-exempt host-capability tokens in
// internal/plugin/capability_vocab.go (CapabilityServiceNames) minus the exempt
// set (emit, command-registry). It is a literal here ON PURPOSE: pkg/plugin must
// NOT import internal/plugin (internal/plugin imports pkg/plugin — importing back
// would cycle). If a capability token is added to the canonical vocab, add it
// here too; this list is the in-package drift guard for the requirements map.
var canonicalNonExemptTokens = map[string]bool{
	"world.query": true, "world.mutation": true, "property": true,
	"session": true, "session.admin": true, "focus": true, "eval": true,
	"settings": true, "kv": true, "stream.history": true,
	"stream.subscription": true, "audit": true,
}

// TestHostCapabilityRequirementsTokensAreCanonical asserts every token listed in
// the requirements map is a known non-exempt capability token.
func TestHostCapabilityRequirementsTokensAreCanonical(t *testing.T) {
	for _, req := range hostCapabilityRequirements {
		for _, tok := range req.tokens {
			assert.Truef(t, canonicalNonExemptTokens[tok],
				"%s lists token %q which is not a known non-exempt capability token", req.awareName, tok)
		}
	}
}

// TestHostCapabilityRequirementsCoverKnownAwareNames is a guard list: the set of
// *Aware interface names in hostCapabilityRequirements must equal the known set.
// Adding a new SetXxx host-client injection in sdk.go without a registry row
// fails this test.
func TestHostCapabilityRequirementsCoverKnownAwareNames(t *testing.T) {
	got := map[string]bool{}
	for _, req := range hostCapabilityRequirements {
		got[req.awareName] = true
	}
	want := []string{
		"EventSinkAware", "FocusClientAware", "HostEvaluatorAware",
		"SettingsClientAware", "SnapshotDecryptorAware", "CommandListerAware",
		"StreamSubscriptionAware",
	}
	for _, name := range want {
		assert.Truef(t, got[name], "hostCapabilityRequirements missing %s", name)
	}
	assert.Lenf(t, got, len(want), "hostCapabilityRequirements has an unexpected *Aware row; update want[] and the sdk.go injection block together")
}
