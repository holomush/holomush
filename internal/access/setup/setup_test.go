// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/jobs"
)

func TestNoopSessionResolver_ReturnsInvalid(t *testing.T) {
	r := setup.NewNoopSessionResolver()
	session, err := r.ResolveSession(context.Background(), "test-session")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
	assert.Empty(t, session, "session must be empty on fail-closed")
}

// TestJobProviderOwnsTheJobNamespaceInTheResolver pins BY NAME that registering
// attribute.NewJobProvider is what puts `job` into a resolver's registered
// namespace set.
//
// Why a named assertion rather than leaving it to the seed-coverage guards:
// those guards only notice a missing `job` provider while some seed REFERENCES
// principal.job.* . Today exactly one does (seed:job-fixture-instance-scoped).
// The day that fixture seed is retired or renamed, both guards go quiet and the
// registration could be dropped with no signal — re-opening the holomush-g776 /
// xxel bug class for the next job seed. This test fails on the dropped
// registration regardless of what the corpus happens to contain.
//
// It asserts the narrower, still-real property rather than driving
// BuildABACStack: BuildABACStack opens a SQL bridge and pings it, so it cannot
// run in the unit tier. The PRODUCTION-path assertion — `job` in the resolver
// BuildABACStack actually builds, and a non-nil ABACStack.JobProvider — lives in
// TestBuildABACStackRegistersTheJobNamespaceInTheProductionResolver
// (buildabacstack_seed_coverage_integ_test.go).
func TestJobProviderOwnsTheJobNamespaceInTheResolver(t *testing.T) {
	resolver := attribute.NewResolver(attribute.NewSchemaRegistry())

	// Paired negative control: without the registration, nothing else in a
	// fresh resolver supplies `job`. Without this the assertion below could
	// pass because some other provider claimed the namespace.
	require.NotContains(t, resolver.RegisteredNamespaces(), "job",
		"control: a fresh resolver MUST NOT already own the job namespace, or the "+
			"assertion below proves nothing about the job provider")

	// A nil registry is deliberate and sufficient here: registration owns the
	// namespace, and liveness resolution (which the registry drives) is proven
	// in internal/access/policy/attribute/job_provider_test.go.
	require.NoError(t, resolver.RegisterProvider(attribute.NewJobProvider(nil)))

	assert.Contains(t, resolver.RegisteredNamespaces(), "job",
		"registering the job provider MUST make `job` a resolver-owned namespace: without it "+
			"every seed gating on principal.job.* silently default-denies with no startup signal")
}

// TestJobRegistrySeamIsOneSharedTypeAcrossBothConfigs pins that the subsystem
// config and the stack config name the SAME interface type for the job liveness
// registry.
//
// The point is singularity. cmd/holomush constructs exactly one jobs.Registry
// and it must reach attribute.JobProvider unchanged; if either config declared a
// locally-defined twin, a refactor could quietly wire the ABAC provider to one
// registry while a future job subsystem registers into another — and the failure
// would be silent, since an empty registry answers "not running" for every job
// and every job-gating seed simply default-denies.
//
// The forwarding of the INSTANCE is proven end to end at the integration tier
// (BuildABACStack); this is the compile-time-plus-reflection half.
func TestJobRegistrySeamIsOneSharedTypeAcrossBothConfigs(t *testing.T) {
	want := reflect.TypeOf((*attribute.JobRegistry)(nil)).Elem()

	subField, ok := reflect.TypeOf(setup.ABACSubsystemConfig{}).FieldByName("JobRegistry")
	require.True(t, ok, "ABACSubsystemConfig MUST carry JobRegistry — it is the only path by "+
		"which cmd/holomush's registry reaches the ABAC stack")
	assert.Equal(t, want, subField.Type,
		"ABACSubsystemConfig.JobRegistry MUST be the shared attribute.JobRegistry interface")

	stackField, ok := reflect.TypeOf(setup.ABACConfig{}).FieldByName("JobRegistry")
	require.True(t, ok, "ABACConfig MUST carry JobRegistry — the field the job provider is built from")
	assert.Equal(t, want, stackField.Type,
		"ABACConfig.JobRegistry MUST be the SAME attribute.JobRegistry interface as the "+
			"subsystem config's, so the two cannot be wired to different registries")

	// The production implementation must actually satisfy that seam.
	var _ attribute.JobRegistry = jobs.NewRegistry()
}
