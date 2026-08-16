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
	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/jobs"
	"github.com/holomush/holomush/internal/testsupport/abactest"
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

	engine := policy.NewEngine(resolver, cache, unusedSessionResolver{}, newProbeAuditLogger(t))

	return newProbeServiceWithEngine(t, engine, loc, nil)
}

// newProbeServiceWithEngine wires an ALREADY-BUILT engine into a world.Service.
// It is the single Service-wiring path: newProbeService delegates to it after
// building its own single-policy engine, and the 02.2 job proofs call it
// directly with an abactest.NewSeedEngine over the WHOLE shipped seed corpus.
//
// The corpus matters. A hand-copied DSL literal in this file would open a drift
// window against the seed that actually ships — and the seed that ships is the
// text Phase 3 copies byte-for-byte — so the job proofs must source their
// policy from policy.SeedPolicies(), which is exactly what a custom-DSL builder
// cannot do.
func newProbeServiceWithEngine(
	t *testing.T,
	engine *policy.Engine,
	loc *world.Location,
	charRepo world.CharacterRepository,
) *world.Service {
	t.Helper()

	return world.NewService(world.ServiceConfig{
		LocationRepo:  stubLocationReader{loc: loc},
		CharacterRepo: charRepo,
		Engine:        engine,
	})
}

// newProbeAuditLogger builds the WAL-backed, output-discarding audit logger both
// engine builders need, and registers its cleanup.
func newProbeAuditLogger(t *testing.T) *audit.Logger {
	t.Helper()

	walPath := filepath.Join(t.TempDir(), "world-caller-probe-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, discardAuditWriter{}, walPath)
	t.Cleanup(func() {
		if err := auditLogger.Close(); err != nil {
			t.Logf("world-caller probe: closing the audit logger: %v", err)
		}
	})
	return auditLogger
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
//
// Verifies: INV-WORLD-8
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

// --- 02.2 job-principal proofs (AUTHZ-02) ----------------------------------

// The fixture vocabulary. `fixture` is a FIXTURE job: it has no production
// consumer, and seed:job-fixture-instance-scoped exists to prove the model
// end-to-end, not to authorize anything real. Phase 3 brings job:retirement and
// job:activity-flush and their own grants (D-52).
const (
	fixtureJobName    = "fixture"
	fixtureEventType  = "fixture_triggered"
	fixtureEventID    = "01ARZ3NDEKTSV4RRFFQ69G5FB0"
	fixtureCharacter  = "01ARZ3NDEKTSV4RRFFQ69G5FB1"
	unrelatedCharacer = "01ARZ3NDEKTSV4RRFFQ69G5FB2"
)

// recordingCharacterRepo is a world.CharacterRepository double that RECORDS the
// ids it was asked for. The recording is the point: reaching characterRepo.Get
// is positive proof that checkAccess returned nil, which a "no permission-denied
// error" assertion alone cannot establish — that assertion is also satisfied by
// failing earlier for an unrelated reason.
//
// Every write method errors rather than returning a zero value, so a future
// caller that reaches one fails loudly.
type recordingCharacterRepo struct {
	char     *world.Character
	getCalls []ulid.ULID
}

var errProbeCharacterWriteUnsupported = oops.Code("WORLD_CALLER_PROBE_CHARACTER_WRITE_UNSUPPORTED").
	Errorf("recordingCharacterRepo is read-only")

func (r *recordingCharacterRepo) Get(_ context.Context, id ulid.ULID) (*world.Character, error) {
	r.getCalls = append(r.getCalls, id)
	if r.char == nil {
		return nil, world.ErrNotFound
	}
	return r.char, nil
}

func (*recordingCharacterRepo) GetByLocation(
	context.Context, ulid.ULID, world.ListOptions,
) ([]*world.Character, error) {
	return nil, world.ErrNotFound
}

func (*recordingCharacterRepo) IsOwnedByPlayer(context.Context, ulid.ULID, ulid.ULID) (bool, error) {
	return false, nil
}

func (*recordingCharacterRepo) GetNamesByIDs(
	context.Context, []ulid.ULID,
) (map[ulid.ULID]string, error) {
	return map[ulid.ULID]string{}, nil
}

func (*recordingCharacterRepo) Create(
	context.Context, *world.Character, charname.Admitted,
) (*wmodel.MutationDelta, error) {
	return nil, errProbeCharacterWriteUnsupported
}

func (*recordingCharacterRepo) Update(
	context.Context, *world.Character,
) (*wmodel.MutationDelta, error) {
	return nil, errProbeCharacterWriteUnsupported
}

func (*recordingCharacterRepo) Rename(
	context.Context, ulid.ULID, charname.Admitted, int, wmodel.EnvelopeIntent,
) (*wmodel.MutationDelta, error) {
	return nil, errProbeCharacterWriteUnsupported
}

func (*recordingCharacterRepo) Delete(context.Context, ulid.ULID, int) (*wmodel.MutationDelta, error) {
	return nil, errProbeCharacterWriteUnsupported
}

func (*recordingCharacterRepo) UpdateLocation(
	context.Context, ulid.ULID, *ulid.ULID, int,
) (*wmodel.MutationDelta, error) {
	return nil, errProbeCharacterWriteUnsupported
}

func (*recordingCharacterRepo) UpdatePreferences(
	context.Context, ulid.ULID, []byte, int,
) (*wmodel.MutationDelta, error) {
	return nil, errProbeCharacterWriteUnsupported
}

func (*recordingCharacterRepo) SetStatus(
	context.Context, ulid.ULID, world.Status, int,
) (*wmodel.MutationDelta, error) {
	return nil, errProbeCharacterWriteUnsupported
}

// newFixtureJobProbe builds the shared fixture: a live registry carrying the
// `fixture` job with `character` in its declared capability class, a real engine
// over the WHOLE shipped seed corpus with the job provider registered, and a
// recording character repository.
//
// Only the job provider is needed. bags.Resource["id"] is stamped
// provider-independently by the resolver, and `resource is character` target
// matching is prefix-only, so no character attribute provider participates in
// either assertion.
func newFixtureJobProbe(t *testing.T, charID ulid.ULID) (*world.Service, *recordingCharacterRepo) {
	t.Helper()

	reg := jobs.NewRegistry()
	require.NoError(t, reg.Register(fixtureJobName, []string{"character"}))

	engine := abactest.NewSeedEngine(t, attribute.NewJobProvider(reg))
	repo := &recordingCharacterRepo{char: &world.Character{ID: charID, Name: "Fixture"}}

	return newProbeServiceWithEngine(t, engine, nil, repo), repo
}

// TestJobCallerWritesOnlyTheAggregateItsProvenanceNames is the phase tracer's
// load-bearing proof, and it is PAIRED on purpose.
//
// The PERMIT half settles research assumption A6: trigger_subject normalizes to
// the BARE aggregate ULID, so `action.job.trigger_subject == resource.id`
// actually matches. Phase 3 plan 03-04 copies that normalization byte-for-byte,
// so a deny-only proof would leave the whole binding unverified.
//
// The DENY half is what makes the permit mean something. The mismatch is
// introduced by changing ONLY the provenance value — the resource ULID is the
// same variable in both subtests — so the denial can only be caused by the
// instance-scoping conjunct.
// Verifies: INV-ACCESS-13
func TestJobCallerWritesOnlyTheAggregateItsProvenanceNames(t *testing.T) {
	charID := ulid.MustParse(fixtureCharacter)

	t.Run("permits the character the provenance names", func(t *testing.T) {
		svc, repo := newFixtureJobProbe(t, charID)

		caller := world.JobCaller(fixtureJobName, world.Provenance{
			EventID:   fixtureEventID,
			EventType: fixtureEventType,
			Subject:   charID.String(),
		})

		err := svc.UpdateCharacterDescription(context.Background(), caller, charID, "retired")

		// (a) The write is NOT denied. The call still terminates in an error —
		// the probe wires no mutator, so it stops at CHARACTER_UPDATE_FAILED —
		// which is expected and is not a denial.
		// RETARGETED by plan 02.2-02 (D-58), together with the deny half below:
		// checkAccess now composes the deny code from the principal kind, so a
		// JobCaller can no longer produce the unqualified entity-only form at
		// all. Left un-retargeted this negative would go quietly VACUOUS rather
		// than red — asserting the absence of a string production cannot emit.
		if oopsErr, ok := oops.AsOops(err); ok {
			require.NotEqual(t, "JOB_CHARACTER_ACCESS_DENIED", oopsErr.Code(),
				"a live fixture job MUST be permitted to write the character its provenance names")
		}

		// (b) The repository was reached. checkAccess runs BEFORE
		// characterRepo.Get, so a recorded Get is positive proof it returned nil
		// rather than the call failing earlier for an unrelated reason.
		require.Equal(t, []ulid.ULID{charID}, repo.getCalls,
			"reaching characterRepo.Get is the positive proof that checkAccess permitted")
	})

	t.Run("denies a different character", func(t *testing.T) {
		svc, repo := newFixtureJobProbe(t, charID)

		caller := world.JobCaller(fixtureJobName, world.Provenance{
			EventID:   fixtureEventID,
			EventType: fixtureEventType,
			// The ONLY difference from the permit case.
			Subject: unrelatedCharacer,
		})

		err := svc.UpdateCharacterDescription(context.Background(), caller, charID, "retired")

		require.Error(t, err)
		// RETARGETED by plan 02.2-02 (D-58) from the unqualified entity-only
		// code: checkAccess composes it from the principal kind now. This
		// stayed an EXACT code comparison on purpose: it was NOT softened to a
		// suffix/substring match, skipped, or deleted. A loosened match would
		// let the deny pass for reasons other than the provenance mismatch,
		// which is the one thing this subtest exists to prove — and instance
		// scoping is the phase's load-bearing claim.
		errutil.AssertErrorCode(t, err, "JOB_CHARACTER_ACCESS_DENIED")
		// Pins that the DENY branch produced the code, not an evaluation
		// failure that happens to classify nearby.
		require.ErrorIs(t, err, world.ErrPermissionDenied)
		require.Empty(t, repo.getCalls,
			"a denied write MUST NOT reach the repository")
	})
}

// TestJobCallerCarriesExactlyTheProvenanceTriple asserts SET EQUALITY, not
// containment: the key set is the whole security claim. A fourth key would mean
// some other channel reached the action bag through the job door, which is the
// door T-02.2-04 exists to keep shut.
func TestJobCallerCarriesExactlyTheProvenanceTriple(t *testing.T) {
	caller := world.JobCaller(fixtureJobName, world.Provenance{
		EventID:   fixtureEventID,
		EventType: fixtureEventType,
		Subject:   fixtureCharacter,
	})

	assert.Equal(t, "job:"+fixtureJobName, caller.SubjectForTest())

	keys := make([]string, 0, len(caller.AttrsForTest()))
	for k := range caller.AttrsForTest() {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t,
		[]string{"job.trigger_event_id", "job.trigger_event_type", "job.trigger_subject"},
		keys,
		"JobCaller must emit exactly the D-54 triple, every key under the job. prefix")

	assert.Equal(t, fixtureCharacter, caller.AttrsForTest()["job.trigger_subject"],
		"trigger_subject carries the BARE aggregate ULID, byte-comparable to resource.id")
}

// TestScheduledJobCallerCarriesNoPerExecutionAttributes pins D-68's coarse
// half: a timer-driven job has no triggering event, so it carries NO
// per-execution attributes — and specifically no empty-string sentinel, which
// would be fail-OPEN (.claude/rules/abac-providers.md).
// Verifies: INV-ACCESS-13
func TestScheduledJobCallerCarriesNoPerExecutionAttributes(t *testing.T) {
	caller := world.ScheduledJobCaller(fixtureJobName)

	assert.Equal(t, "job:"+fixtureJobName, caller.SubjectForTest())
	assert.Empty(t, caller.AttrsForTest())
	for k, v := range caller.AttrsForTest() {
		assert.NotEqual(t, "", v,
			"key %q carries an empty-string sentinel, which matches any unresolved peer", k)
	}
}

// TestJobCallersNeverTakeTheSystemBypass keeps the job principal disjoint from
// the S1 gate: neither job constructor may set the system flag or stamp the
// ambient system marker on the derived evaluation context.
func TestJobCallersNeverTakeTheSystemBypass(t *testing.T) {
	callers := map[string]world.Caller{
		"JobCaller": world.JobCaller(fixtureJobName, world.Provenance{
			EventID:   fixtureEventID,
			EventType: fixtureEventType,
			Subject:   fixtureCharacter,
		}),
		"ScheduledJobCaller": world.ScheduledJobCaller(fixtureJobName),
	}

	for name, caller := range callers {
		t.Run(name, func(t *testing.T) {
			assert.False(t, caller.IsSystemForTest(),
				"a job caller must never request the S1 system bypass")
			assert.False(t, access.IsSystemContext(caller.EvalContextForTest(context.Background())),
				"a job caller must never stamp the ambient system marker")
		})
	}
}
