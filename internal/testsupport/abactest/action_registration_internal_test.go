// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package abactest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestSeedSchemaRegistryCarriesActionSoABadSeedFailsInTheUnitTier closes the
// site-4 coverage gap from the 02.2-04 compilation-site inventory.
//
// abactest.go is a non-_test.go, untagged file that compiles the FULL
// policy.SeedPolicies() corpus, so it runs in the UNIT tier — `task test`, on
// every developer machine and every CI run. Before 02.2-04 its registry was real
// but carried no `action` namespace, so HasNamespace("action") was false, the
// compiler's hard-error branch was skipped, and a seed with a typo'd action.* key
// compiled clean here and only failed at boot. That skip WAS the gap.
//
// This test drives newSeedSchemaRegistry — the real function NewSeedEngine calls
// — rather than a hand-built copy of the registry it produces, so it goes red on
// a dropped registration instead of quietly asserting its own mirror.
func TestSeedSchemaRegistryCarriesActionSoABadSeedFailsInTheUnitTier(t *testing.T) {
	t.Parallel()

	registry, _ := newSeedSchemaRegistry(t)

	require.True(t, registry.HasNamespace("action"),
		"the registry NewSeedEngine compiles the corpus against MUST carry `action`, or the "+
			"compiler's hard-error branch is skipped and a bad seed reaches a deployment")

	_, _, err := policy.NewCompiler(registry.Schema()).Compile(
		`permit(principal, action, resource) when { action.typo_key == "x" };`,
	)

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "POLICY_UNREGISTERED_ACTION_ATTRIBUTE")
	assert.Contains(t, err.Error(), "action.typo_key")
}

// TestSeedSchemaRegistryStillCarriesTheCallersProviders is the paired control for
// the test above: registering `action` must ADD a namespace, never replace the
// provider set. Swapping the registry for an action-only one would silently drop
// the provider-namespace coverage every existing NewSeedEngine caller depends on.
func TestSeedSchemaRegistryStillCarriesTheCallersProviders(t *testing.T) {
	t.Parallel()

	provider := attribute.NewJobProvider(nil)
	registry, resolver := newSeedSchemaRegistry(t, provider)

	assert.Contains(t, resolver.RegisteredNamespaces(), provider.Namespace(),
		"the caller's provider MUST still own its namespace after the action registration")
	assert.True(t, registry.HasNamespace("action"))
}
