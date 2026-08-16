// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// This file is `package policy_test` — an EXTERNAL test package — on purpose,
// for the same reason seed_profile_smoke_test.go is.
//
// The test below needs internal/plugin/hostcap, and hostcap imports
// internal/plugin. internal/plugin in turn imports internal/access/policy (the
// install-time `action` gate in internal/plugin/policy_installer.go), so an
// IN-PACKAGE test file importing hostcap would be importing a package that
// depends on the package under test — `import cycle not allowed in test`. An
// external test package is exempt because it compiles as a separate package.
//
// It previously lived in seed_smoke_test.go (`package policy`) and used that
// file's unexported createSeedEngine. The exported equivalent is
// abactest.NewSeedEngine, which builds the same engine through the real
// NewCompiler → NewCache → Reload → NewEngine path AND registers the `action`
// namespace — so this file compiles the seed corpus under the live action gate
// rather than a weaker one, which is strictly closer to production than the
// helper it replaces.
package policy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/testsupport/abactest"
)

// Verifies: INV-PLUGIN-50
func TestEverySeededCapabilityResourceHasDefaultPermit(t *testing.T) {
	t.Parallel()

	// Drift guard: every served, non-exempt, NON-scope-eligible capability method
	// in hostcap.Descriptors MUST be authorized by a default-permit seed at the
	// type level (resource "<type>:*", exactly how the interceptor evaluates a
	// non-scoped call). A capability added later without its seed would fail
	// closed at runtime; this test catches that at build time — the seed-side
	// analogue of the INV-PLUGIN-52 extractor-completeness meta-test.
	//
	// Scope-eligible methods are intentionally skipped: they are gated by the
	// own-location seed and proven by the scoped smoke tests, which require a
	// concrete resource + dispatch_location this type-level probe does not supply.
	// Exempt (self-gated) capabilities short-circuit before the ABAC gate.
	engine := abactest.NewSeedEngine(t) // unconditional permits resolve without providers

	require.NotEmpty(t, hostcap.Descriptors,
		"control: an empty descriptor set would make every assertion below vacuous")

	probed := 0
	for token, desc := range hostcap.Descriptors {
		if hostcap.IsDeclarationExempt(token) {
			continue
		}
		for method, md := range desc.Methods {
			if len(md.Scopes) > 0 {
				continue
			}
			probed++
			t.Run(token+"/"+method, func(t *testing.T) {
				decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
					Subject:  access.PluginSubject("drift-probe"),
					Action:   md.Action,
					Resource: md.Resource + ":*",
				})
				require.NoError(t, err)
				assert.True(t, decision.IsAllowed(),
					"non-exempt non-scoped capability %s/%s (action=%q resource=%q) has no default-permit seed — it would fail closed at the interceptor; add a seed:plugin-cap-* permit for resource %q",
					token, method, md.Action, md.Resource, md.Resource)
			})
		}
	}

	require.NotZero(t, probed,
		"control: every descriptor was exempt or scope-eligible, so this drift guard asserted nothing")
}
