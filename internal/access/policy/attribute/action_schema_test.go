// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// auditedActionDeclareSet is the DECLARE set of
// .planning/phases/02.2-background-job-authorization-model/02.2-ACTION-AUDIT.md
// §1, transcribed here so drift between the audit and the code is a test
// failure rather than a boot failure.
//
// Adding a key to ActionNamespaceSchema without adding it to the audit (or the
// reverse) turns this red. That is the point: once 02.2-04 wires the production
// compiler to the populated schema registry, an action.* reference outside this
// set is a hard compile error that fails cache.Reload, which fails
// BuildABACStack, which fails boot.
var auditedActionDeclareSet = []string{
	"name",
	"dispatch_location",
	"job.trigger_event_id",
	"job.trigger_event_type",
	"job.trigger_subject",
}

// TestActionNamespaceSchemaDeclaresExactlyTheAuditedFiveKeys asserts SET
// EQUALITY, not membership: both a dropped key and an extra key go red.
//
// A membership-only assertion would let a sixth key be declared silently, and
// an undeclared-but-referenced key is precisely the boot failure the audit
// exists to prevent.
func TestActionNamespaceSchemaDeclaresExactlyTheAuditedFiveKeys(t *testing.T) {
	t.Parallel()

	got := ActionNamespaceSchema()
	require.NotNil(t, got, "ActionNamespaceSchema MUST return a schema, never nil")

	gotKeys := make([]string, 0, len(got.Attributes))
	for k := range got.Attributes {
		gotKeys = append(gotKeys, k)
	}

	assert.ElementsMatch(t, auditedActionDeclareSet, gotKeys,
		"the declared action key set MUST equal the audit's DECLARE set exactly: a missing key "+
			"becomes a boot failure once 02.2-04 lands, and an extra key is undocumented surface")
	assert.Len(t, got.Attributes, len(auditedActionDeclareSet),
		"exactly five keys — a duplicate-collapsed map would satisfy ElementsMatch alone")
}

// TestActionNamespaceSchemaTypesEveryKeyAsString pins that every declared key is
// AttrTypeString. All five are verbatim strings taken from an event envelope, a
// dispatch context, or the action verb — none is a list, bool, or number.
func TestActionNamespaceSchemaTypesEveryKeyAsString(t *testing.T) {
	t.Parallel()

	for key, attrType := range ActionNamespaceSchema().Attributes {
		assert.Equal(t, types.AttrTypeString, attrType,
			"action.%s MUST be AttrTypeString", key)
	}
}

// TestActionNamespaceSchemaKeysCarryTheDotLiterally pins research assumption A3:
// a dotted action key is ONE literal map key containing a dot, not a nested
// namespace.
//
// This is the only spelling the compiler can match. validateAttributes builds
// its lookup key as strings.Join(ref.Path, ".") and calls
// IsRegistered(namespace, key), which is an exact map lookup
// (compiler.go:157,161; types.AttributeSchema.IsRegistered). So the DSL
// reference `action.job.trigger_subject` produces the lookup pair
// ("action", "job.trigger_subject") — and a schema that instead nested a "job"
// sub-namespace would miss it and hard-error at boot.
func TestActionNamespaceSchemaKeysCarryTheDotLiterally(t *testing.T) {
	t.Parallel()

	reg := NewSchemaRegistry()
	require.NoError(t, Register(reg, "action", ActionNamespaceSchema()))

	assert.True(t, reg.IsRegistered("action", "job.trigger_subject"),
		"the dot MUST be part of the KEY: this is the exact pair the compiler looks up for "+
			"the DSL reference action.job.trigger_subject")
	assert.False(t, reg.IsRegistered("action", "job"),
		"control: a bare `job` key MUST NOT be registered — if it were, the schema would be "+
			"nesting a sub-namespace the compiler never asks for")
}

// TestActionOnlySchemaRegistryCarriesActionAndNoEntityNamespace pins the
// deliberate asymmetry 02.2-04 relies on for the compilation sites that have no
// provider set to draw on (the bootstrap seed installer and the WithRealABAC
// harness).
//
// `action` present means the hard-error branch behaves identically there. Every
// entity namespace ABSENT means provider-namespace references stay unvalidated
// at those sites, producing no warnings — which is correct, because those sites
// legitimately have no providers, not because validation was disabled.
func TestActionOnlySchemaRegistryCarriesActionAndNoEntityNamespace(t *testing.T) {
	t.Parallel()

	reg := NewActionOnlySchemaRegistry()
	require.NotNil(t, reg)

	assert.True(t, reg.HasNamespace("action"),
		"the action namespace MUST be present, or the hard-error branch is skipped entirely "+
			"at the compilation sites this registry exists to serve")
	assert.False(t, reg.HasNamespace("character"),
		"no entity namespace may be present: this registry deliberately carries action ALONE")
	assert.False(t, reg.HasNamespace("job"),
		"control: `job` is a PROVIDER-owned principal namespace, not part of the action bag")
}

// TestActionNamespaceSchemaCannotBeRegisteredTwice pins that the registration in
// BuildABACStack happens exactly once. Register rejects an already-registered
// namespace (schema.go:38-40), so a second registration site would not silently
// win or merge — it would fail the stack build. This test makes that contract
// explicit rather than leaving it to be rediscovered at boot.
func TestActionNamespaceSchemaCannotBeRegisteredTwice(t *testing.T) {
	t.Parallel()

	reg := NewSchemaRegistry()
	require.NoError(t, Register(reg, "action", ActionNamespaceSchema()),
		"the first registration MUST succeed")

	err := Register(reg, "action", ActionNamespaceSchema())
	require.Error(t, err,
		"a second registration of `action` MUST fail: the namespace is registered in exactly "+
			"one place (BuildABACStack), and a duplicate would mean two sources of truth")
	assert.Contains(t, err.Error(), "already registered")
}
