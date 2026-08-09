// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
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

// TestActionNamespaceIsRegisteredWithTheAuditedKeySet pins the D-60 registration
// step BuildABACStack performs at its `// 10b.` slot, on the same
// stack-equivalent seam plan 02.2-02 used for the job provider: BuildABACStack
// itself opens a SQL bridge and pings it, so it cannot run in the unit tier.
//
// What this proves is that the exact call BuildABACStack makes —
// attribute.Register(schemaReg, "action", attribute.ActionNamespaceSchema()) —
// yields a registry in which every audited key resolves.
//
// As of 02.2-04 that registration is LOAD-BEARING: BuildABACStack now builds its
// compiler on schemaReg.Schema(), so a key missing from the audited set below
// hard-errors at cache.Reload and fails boot. (Before 02.2-04 the compiler was
// built on a separate, never-populated schema and this registration was a no-op.)
// The two tests that pin the wiring itself are
// TestBuildABACStackReloadsTheCacheAfterTheActionRegistration and
// TestCacheReloadNamesBothThePolicyAndTheUndeclaredActionKey, below.
func TestActionNamespaceIsRegisteredWithTheAuditedKeySet(t *testing.T) {
	t.Parallel()

	schemaReg := attribute.NewSchemaRegistry()

	// Paired negative control: without the registration nothing else supplies
	// `action`. Without this, the assertions below could pass because some
	// other construction step had already claimed the namespace.
	require.False(t, schemaReg.HasNamespace("action"),
		"control: a fresh schema registry MUST NOT already carry `action`, or the "+
			"assertions below prove nothing about the registration step")

	// The verbatim call BuildABACStack makes at its `// 10b.` slot.
	require.NoError(t, attribute.Register(schemaReg, "action", attribute.ActionNamespaceSchema()))

	require.True(t, schemaReg.HasNamespace("action"))
	for _, key := range []string{
		"name",
		"dispatch_location",
		"job.trigger_event_id",
		"job.trigger_event_type",
		"job.trigger_subject",
	} {
		assert.True(t, schemaReg.IsRegistered("action", key),
			"action.%s MUST be registered: the compiler is wired to this registry, so an "+
				"undeclared key referenced by any compiled policy hard-errors and fails boot", key)
	}
}

// --- The 02.2-04 wiring (D-66) ---

// actionGatePolicyStore is a minimal store.PolicyStore serving a fixed policy
// list. Only ListEnabled is exercised — Cache.Reload calls nothing else — but the
// remaining methods are stubbed rather than omitted because the interface is the
// contract, and a future caller reaching for one should get a zero value here
// rather than a compile break in an unrelated test.
type actionGatePolicyStore struct{ policies []*policystore.StoredPolicy }

func (s actionGatePolicyStore) ListEnabled(_ context.Context) ([]*policystore.StoredPolicy, error) {
	return s.policies, nil
}

func (actionGatePolicyStore) Create(_ context.Context, _ *policystore.StoredPolicy) error { return nil }

func (actionGatePolicyStore) Get(_ context.Context, _ string) (*policystore.StoredPolicy, error) {
	return nil, nil
}

func (actionGatePolicyStore) GetByID(_ context.Context, _ string) (*policystore.StoredPolicy, error) {
	return nil, nil
}

func (actionGatePolicyStore) Update(_ context.Context, _ *policystore.StoredPolicy) error { return nil }

func (actionGatePolicyStore) Delete(_ context.Context, _ string) error { return nil }

func (actionGatePolicyStore) CreateBatch(_ context.Context, _ []*policystore.StoredPolicy) error {
	return nil
}

func (actionGatePolicyStore) DeleteBySource(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

func (actionGatePolicyStore) ReplaceBySource(_ context.Context, _, _ string, _ []*policystore.StoredPolicy) error {
	return nil
}

func (actionGatePolicyStore) List(_ context.Context, _ policystore.ListOptions) ([]*policystore.StoredPolicy, error) {
	return nil, nil
}

// TestCacheReloadNamesBothThePolicyAndTheUndeclaredActionKey is ROADMAP criterion
// 3's proof at the grain an operator experiences it: the reload that
// BuildABACStack performs fails, and the failure text is sufficient on its own to
// locate and fix the offending row without reading Go source.
//
// The two naming halves come from different files and would regress
// independently — cache.go composes `%q (id=%s)` around the policy, compiler.go
// names the key — so both are asserted in one test. A message that loses either
// half goes red here.
func TestCacheReloadNamesBothThePolicyAndTheUndeclaredActionKey(t *testing.T) {
	t.Parallel()

	// The exact registry composition BuildABACStack's compiler is now built on,
	// as far as `action` is concerned.
	schemaReg := attribute.NewSchemaRegistry()
	require.NoError(t, attribute.Register(schemaReg, "action", attribute.ActionNamespaceSchema()))
	compiler := policy.NewCompiler(schemaReg.Schema())

	store := actionGatePolicyStore{policies: []*policystore.StoredPolicy{{
		ID:      "01JQ00000000000000000000B1",
		Name:    "seed:undeclared-action-key",
		Source:  "seed",
		DSLText: `permit(principal, action, resource) when { action.not_declared == "x" };`,
		Enabled: true,
	}}}

	err := policy.NewCache(store, compiler).Reload(context.Background())

	require.Error(t, err, "an undeclared action.* key MUST fail the reload — silently "+
		"default-denying is the failure mode D-67 exists to prevent")
	got := err.Error()
	assert.Contains(t, got, "seed:undeclared-action-key", "the failure MUST name the offending policy")
	assert.Contains(t, got, "01JQ00000000000000000000B1", "the failure MUST name the policy's DB id")
	assert.Contains(t, got, "action.not_declared", "the failure MUST name the offending key")
}

// TestEveryShippedSeedCompilesUnderTheLiveActionGate is the regression half of
// 02.2-04: turning a dead branch into a boot gate is only safe if the corpus the
// gate now sees is clean. It iterates the WHOLE corpus — sampling would let a
// single bad seed reach a deployment.
//
// The registry is `action` ONLY, and that is the CORRECT scope, not a weaker
// substitute for "every production provider's schema". The compiler validates by
// DSL ROOT (principal | resource | action | env), never by provider name, so
// adding provider schemas would change nothing about which references get
// checked: no provider registers a namespace named `resource` or `principal`, so
// every reference under those roots is skipped either way. `action` is the only
// branch this test can exercise and the only one it needs to.
//
// (The full provider set is also unobtainable here: the only thing that builds it
// is BuildABACStack, which opens a SQL bridge and pings it, and hand-registering
// the providers would recreate the hardcoded-mirror drift hazard
// seed_coverage_test.go warns about. Do not "strengthen" this test into that
// shape.)
func TestEveryShippedSeedCompilesUnderTheLiveActionGate(t *testing.T) {
	t.Parallel()

	compiler := policy.NewCompiler(attribute.NewActionOnlySchemaRegistry().Schema())

	seeds := policy.SeedPolicies()
	require.NotEmpty(t, seeds, "control: an empty corpus would make this test vacuous")

	for _, seed := range seeds {
		t.Run(seed.Name, func(t *testing.T) {
			_, _, err := compiler.Compile(seed.DSLText)
			require.NoError(t, err,
				"shipped seed %q MUST compile under the live action gate — if this is an "+
					"unregistered action.* key, that is a REAL finding: fix the seed or add the "+
					"key to the audit, do NOT widen the schema to make this pass", seed.Name)
		})
	}
}

// TestBuildABACStackReloadsTheCacheAfterTheActionRegistration pins research
// Pitfall 2 (threat T-02.2-18) as a SOURCE-ORDER property.
//
// A source-order assertion is legitimate here because the hazard IS source order
// and no runtime seam exposes it: BuildABACStack opens a SQL bridge and pings it,
// so the unit tier cannot call it, and by the time it returns both steps have
// already run in whatever order the file happens to declare. Compiling the boot
// snapshot BEFORE the providers and the `action` schema register would validate
// boot against an empty registry while every later poller/invalidation reload
// validated against a populated one — a bad policy would pass boot and then kill
// the first reload, which is strictly worse than the uniform no-op it replaced.
//
// Comment lines are skipped deliberately: prose in this file's neighbours names
// both call sites, and a gate that a comment can satisfy is not a gate. The
// exactly-one requirements are what make the position comparison meaningful.
func TestBuildABACStackReloadsTheCacheAfterTheActionRegistration(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("setup.go")
	require.NoError(t, err, "go test runs with the package directory as cwd")

	var registerLine, reloadLine, registerHits, reloadHits int
	for i, raw := range strings.Split(string(src), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "//") {
			continue
		}
		if strings.Contains(line, `attribute.Register(schemaReg, "action"`) {
			registerHits++
			registerLine = i + 1
		}
		if strings.Contains(line, "cache.Reload(") {
			reloadHits++
			reloadLine = i + 1
		}
	}

	require.Equal(t, 1, registerHits,
		"expected exactly one non-comment `action` registration in setup.go, found %d", registerHits)
	require.Equal(t, 1, reloadHits,
		"expected exactly one non-comment cache.Reload call in setup.go, found %d", reloadHits)
	assert.Greater(t, reloadLine, registerLine,
		"cache.Reload (line %d) MUST run AFTER the action registration (line %d), or boot compiles "+
			"against an empty schema while every later reload compiles against a populated one",
		reloadLine, registerLine)
}
