// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestHumanCallerCarriesItsSubjectVerbatim asserts that HumanCaller wraps the
// already-built subject string without re-deriving or re-prefixing it. Byte
// identity matters beyond authorization: this same string becomes the
// world-change outbox envelope Actor via buildIntent / buildMoveIntent.
func TestHumanCallerCarriesItsSubjectVerbatim(t *testing.T) {
	c := world.HumanCaller("character:01ABC")
	assert.Equal(t, "character:01ABC", c.SubjectForTest())
	assert.False(t, c.IsSystemForTest(), "HumanCaller must never set the system flag")

	// "system:bootstrap" is a normally policy-evaluated subject. engine.go:92
	// tests exact string equality against "system", not a prefix, so this must
	// pass through verbatim and must NOT be system-kind.
	boot := world.HumanCaller("system:bootstrap")
	assert.Equal(t, "system:bootstrap", boot.SubjectForTest())
	assert.False(t, boot.IsSystemForTest(), "system:bootstrap must not be system-kind")
}

// TestHumanCallerAcceptsAnEmptySubjectWithoutPanicking pins the deliberate
// deviation from the access.*Subject panic-on-empty convention. The fail-closed
// guard lives one layer down in types.NewAccessRequest; panicking here would
// break the nine empty-subject subtests of the malformed-access-params table at
// internal/world/service_test.go:6079.
func TestHumanCallerAcceptsAnEmptySubjectWithoutPanicking(t *testing.T) {
	var c world.Caller
	require.NotPanics(t, func() { c = world.HumanCaller("") })
	assert.Empty(t, c.SubjectForTest())
	assert.False(t, c.IsSystemForTest())
}

// TestSystemCallerYieldsTheBareSystemSubject asserts byte equality against the
// same literal the ABAC S1 gate compares (internal/access/policy/engine.go:92),
// so a future rename of that literal is caught here.
func TestSystemCallerYieldsTheBareSystemSubject(t *testing.T) {
	c := world.SystemCaller()
	assert.Equal(t, "system", c.SubjectForTest())
	assert.True(t, c.IsSystemForTest(), "SystemCaller must derive its own system flag")
}

// TestHumanCallerCarriesNoAttributes asserts neither constructor populates the
// attribute channel in 02.1, so types.NewAccessRequest normalizes it to a nil
// Attributes field — byte-identical to the hardcoded nil it replaces.
func TestHumanCallerCarriesNoAttributes(t *testing.T) {
	assert.Empty(t, world.HumanCaller("character:01ABC").AttrsForTest())
	assert.Empty(t, world.SystemCaller().AttrsForTest())
}

// TestSystemCallerStampsTheSystemMarkerOnlyOnTheDerivedContext asserts the
// caller value can influence the context the S1 gate reads, WITHOUT mutating
// the caller-supplied context. The marker must not be able to outlive the
// evaluation and reach repositories or the outbox.
func TestSystemCallerStampsTheSystemMarkerOnlyOnTheDerivedContext(t *testing.T) {
	t.Run("system caller derives a marked context", func(t *testing.T) {
		inputCtx := context.Background()
		evalCtx := world.SystemCaller().EvalContextForTest(inputCtx)

		assert.True(t, access.IsSystemContext(evalCtx),
			"derived context must carry the system marker")
		assert.False(t, access.IsSystemContext(inputCtx),
			"input context must be unchanged")
	})

	t.Run("human caller derives an unmarked context", func(t *testing.T) {
		inputCtx := context.Background()
		evalCtx := world.HumanCaller("character:01ABC").EvalContextForTest(inputCtx)

		assert.False(t, access.IsSystemContext(evalCtx),
			"a human caller must never stamp the system marker")
		assert.False(t, access.IsSystemContext(inputCtx),
			"input context must be unchanged")
	})
}

// --- Service-driving proofs (ROADMAP criteria 2 and 4) ---------------------
//
// These three compile only after checkAccess and GetLocation take a Caller, so
// they land in the flip commit rather than the additive one.

// tracerProbeKey is the caller-supplied attribute key the criterion-2 proof
// conditions its policy on. It is deliberately NOT a registered `action`
// namespace key: internal/access/policy/compiler.go skips the unregistered
// action namespace, so such a policy compiles today, and registering the
// namespace is Phase 02.2's work (D-59).
const tracerProbeKey = "tracer_probe"

// stubLocationReader is a minimal world.LocationReader. package world cannot
// import internal/world/worldtest (that package imports world, so an in-package
// test file importing it is an import cycle), hence the hand-rolled double.
type stubLocationReader struct{ loc *world.Location }

func (s stubLocationReader) Get(context.Context, ulid.ULID) (*world.Location, error) {
	if s.loc == nil {
		return nil, world.ErrNotFound
	}
	return s.loc, nil
}

func (stubLocationReader) ListByType(context.Context, world.LocationType) ([]*world.Location, error) {
	return nil, world.ErrNotFound
}

func (stubLocationReader) GetShadowedBy(context.Context, ulid.ULID) ([]*world.Location, error) {
	return nil, world.ErrNotFound
}

func (stubLocationReader) FindByName(context.Context, string) (*world.Location, error) {
	return nil, world.ErrNotFound
}

// The Service only ever reads through this double; the three write methods
// exist to satisfy world.LocationRepository (which ServiceConfig.LocationRepo
// requires) and error rather than returning a zero value, so a future caller
// that reaches one fails loudly.
func (stubLocationReader) Create(context.Context, *world.Location) (*wmodel.MutationDelta, error) {
	return nil, errProbeWriteUnsupported
}

func (stubLocationReader) Update(context.Context, *world.Location) (*wmodel.MutationDelta, error) {
	return nil, errProbeWriteUnsupported
}

func (stubLocationReader) Delete(context.Context, ulid.ULID, int) (*wmodel.MutationDelta, error) {
	return nil, errProbeWriteUnsupported
}

var errProbeWriteUnsupported = oops.Code("WORLD_CALLER_PROBE_WRITE_UNSUPPORTED").
	Errorf("stubLocationReader is read-only")

// singlePolicyStore is an in-memory store.PolicyStore serving exactly one
// policy. Cache.Reload calls ListEnabled and nothing else; every other method
// errors so a future caller fails loudly rather than seeing an empty store.
// Modelled on internal/testsupport/abactest's seedStore, which package world
// cannot use here because it serves the seed corpus rather than a custom DSL.
type singlePolicyStore struct{ dsl string }

var errProbeStoreUnsupported = oops.Code("WORLD_CALLER_PROBE_STORE_UNSUPPORTED").
	Errorf("singlePolicyStore serves ListEnabled only")

func (s singlePolicyStore) ListEnabled(context.Context) ([]*policystore.StoredPolicy, error) {
	v := 1
	return []*policystore.StoredPolicy{{
		ID:          "world-caller-probe-1",
		Name:        "seed:world-caller-probe",
		Description: "criterion-2 proof: permit only when the caller-supplied action attribute matches",
		Source:      "seed",
		DSLText:     s.dsl,
		Enabled:     true,
		SeedVersion: &v,
	}}, nil
}

func (singlePolicyStore) Create(context.Context, *policystore.StoredPolicy) error {
	return errProbeStoreUnsupported
}

func (singlePolicyStore) Get(context.Context, string) (*policystore.StoredPolicy, error) {
	return nil, errProbeStoreUnsupported
}

func (singlePolicyStore) GetByID(context.Context, string) (*policystore.StoredPolicy, error) {
	return nil, errProbeStoreUnsupported
}

func (singlePolicyStore) Update(context.Context, *policystore.StoredPolicy) error {
	return errProbeStoreUnsupported
}
func (singlePolicyStore) Delete(context.Context, string) error { return errProbeStoreUnsupported }

func (singlePolicyStore) CreateBatch(context.Context, []*policystore.StoredPolicy) error {
	return errProbeStoreUnsupported
}

func (singlePolicyStore) DeleteBySource(context.Context, string, string) (int64, error) {
	return 0, errProbeStoreUnsupported
}

func (singlePolicyStore) ReplaceBySource(context.Context, string, string, []*policystore.StoredPolicy) error {
	return errProbeStoreUnsupported
}

func (singlePolicyStore) List(context.Context, policystore.ListOptions) ([]*policystore.StoredPolicy, error) {
	return nil, errProbeStoreUnsupported
}

// discardAuditWriter satisfies audit.Writer without retaining anything.
type discardAuditWriter struct{}

func (discardAuditWriter) WriteSync(context.Context, audit.Event) error { return nil }
func (discardAuditWriter) WriteAsync(audit.Event) error                 { return nil }
func (discardAuditWriter) Close() error                                 { return nil }

// unusedSessionResolver errors on every call — no policy here resolves a
// session, so reaching it means the request under test was shaped wrong.
type unusedSessionResolver struct{}

func (unusedSessionResolver) ResolveSession(context.Context, string) (string, error) {
	return "", oops.Code("WORLD_CALLER_PROBE_SESSION_UNSUPPORTED").
		Errorf("the world-caller probe engine resolves no sessions")
}

// newProbeService builds a Service backed by a REAL policy.Engine carrying the
// single supplied DSL policy, compiled and installed through the exported
// NewCompiler → NewCache → Reload → NewEngine path. A policytest fake would
// return a canned decision and prove nothing about the attribute channel.
func newProbeService(t *testing.T, dsl string, loc *world.Location) *world.Service {
	t.Helper()

	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)

	compiler := policy.NewCompiler(registry.Schema())
	cache := policy.NewCache(singlePolicyStore{dsl: dsl}, compiler)
	require.NoError(t, cache.Reload(context.Background()),
		"the probe policy MUST compile and load through the exported cache path")

	walPath := filepath.Join(t.TempDir(), "world-caller-probe-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, discardAuditWriter{}, walPath)
	t.Cleanup(func() {
		if err := auditLogger.Close(); err != nil {
			t.Logf("world-caller probe: closing the audit logger: %v", err)
		}
	})

	engine := policy.NewEngine(resolver, cache, unusedSessionResolver{}, auditLogger)

	return world.NewService(world.ServiceConfig{
		LocationRepo: stubLocationReader{loc: loc},
		Engine:       engine,
	})
}

// TestWorldServiceCallerAttributesReachActionBag is ROADMAP criterion 2: a
// caller-carried attribute must surface in the policy DSL as action.<key>.
//
// The proof is PAIRED. A permit-only assertion cannot distinguish "the
// attribute arrived" from "the policy permits unconditionally" — the deny half
// is what makes the attribute value load-bearing.
//
// The attribute-carrying caller is built by same-package composite literal on
// purpose: no production code, exported or unexported, may inject attributes
// before Phase 02.2 defines the vocabulary (D-62).
//
// This is the ATTRIBUTE-FORWARDING half of INV-WORLD-8; the parameter-shape half
// is pinned structurally by test/meta/world_caller_census_test.go.
//
// Verifies: INV-WORLD-8
func TestWorldServiceCallerAttributesReachActionBag(t *testing.T) {
	const dsl = `permit(principal is character, action in ["read"], resource is location) ` +
		`when { action.` + tracerProbeKey + ` == "expected" };`

	locID := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	subject := access.CharacterSubject("01ARZ3NDEKTSV4RRFFQ69G5FAW")

	t.Run("matching attribute permits", func(t *testing.T) {
		svc := newProbeService(t, dsl, &world.Location{ID: locID, Name: "Probe Room"})

		caller := world.NewCallerWithAttrsForTest(subject, map[string]any{tracerProbeKey: "expected"})

		loc, err := svc.GetLocation(context.Background(), caller, locID)
		require.NoError(t, err,
			"the caller attribute must reach the DSL as action.%s and permit", tracerProbeKey)
		assert.Equal(t, "Probe Room", loc.Name)
	})

	t.Run("mismatched attribute denies", func(t *testing.T) {
		svc := newProbeService(t, dsl, &world.Location{ID: locID, Name: "Probe Room"})

		caller := world.NewCallerWithAttrsForTest(subject, map[string]any{tracerProbeKey: "mismatched"})

		_, err := svc.GetLocation(context.Background(), caller, locID)
		require.Error(t, err,
			"a mismatched attribute value must deny — otherwise the permit above proves nothing")
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_DENIED")
	})
}

// TestWorldServiceSystemCallerSatisfiesTheS1GateWithoutAmbientContext is
// ROADMAP criterion 4: SystemCaller() alone must satisfy the ABAC S1
// double-gate, with the call site supplying a plain context.Background() and no
// access.WithSystemSubject of its own.
//
// The negative half is what proves the context marker is load-bearing rather
// than the subject string alone: the same bare "system" subject wrapped as a
// HumanCaller — which stamps no marker — must be REJECTED.
func TestWorldServiceSystemCallerSatisfiesTheS1GateWithoutAmbientContext(t *testing.T) {
	// A policy that permits nothing relevant: the S1 bypass short-circuits
	// before policy lookup, so a permit here would confuse the two mechanisms.
	const dsl = `permit(principal is character, action in ["write"], resource is scene);`

	locID := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")

	t.Run("system caller takes the bypass with a plain context", func(t *testing.T) {
		svc := newProbeService(t, dsl, &world.Location{ID: locID, Name: "System Room"})

		loc, err := svc.GetLocation(context.Background(), world.SystemCaller(), locID)
		require.NoError(t, err,
			"SystemCaller must satisfy BOTH halves of the S1 gate unaided")
		assert.Equal(t, "System Room", loc.Name)
	})

	t.Run("a bare system subject without the marker is rejected", func(t *testing.T) {
		svc := newProbeService(t, dsl, &world.Location{ID: locID, Name: "System Room"})

		_, err := svc.GetLocation(context.Background(), world.HumanCaller("system"), locID)
		require.Error(t, err,
			"the S1 gate must reject a system subject carrying no system context")
		// SYSTEM_SUBJECT_REJECTED is the engine's own code, reached by chain
		// walk through checkAccess's LOCATION_ACCESS_EVALUATION_FAILED wrapper.
		// Asserting the inner code names the S1 rejection precisely rather than
		// the generic evaluation-failure classification around it.
		errutil.AssertErrorCode(t, err, "SYSTEM_SUBJECT_REJECTED")
	})
}

// TestWorldServiceEmptyHumanCallerFailsClosed pins the other half of
// HumanCaller's deliberate no-panic deviation: an empty subject must not panic
// and must not be permitted — it is classified as an evaluation failure by the
// load-bearing request-error branch in checkAccess.
func TestWorldServiceEmptyHumanCallerFailsClosed(t *testing.T) {
	const dsl = `permit(principal is character, action in ["read"], resource is location);`

	locID := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	svc := newProbeService(t, dsl, &world.Location{ID: locID, Name: "Probe Room"})

	var err error
	require.NotPanics(t, func() {
		_, err = svc.GetLocation(context.Background(), world.HumanCaller(""), locID)
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
}
