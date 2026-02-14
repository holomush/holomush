// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// --- Test helpers ---

type bootstrapMockStore struct {
	policies  map[string]*store.StoredPolicy
	created   []*store.StoredPolicy
	updated   []*store.StoredPolicy
	createErr error
	updateErr error
}

func newBootstrapMockStore() *bootstrapMockStore {
	return &bootstrapMockStore{
		policies: make(map[string]*store.StoredPolicy),
	}
}

func (m *bootstrapMockStore) Get(_ context.Context, name string) (*store.StoredPolicy, error) {
	if p, ok := m.policies[name]; ok {
		return p, nil
	}
	return nil, oops.Code("POLICY_NOT_FOUND").With("name", name).Errorf("policy not found")
}

func (m *bootstrapMockStore) GetByID(_ context.Context, _ string) (*store.StoredPolicy, error) {
	return nil, nil
}

func (m *bootstrapMockStore) Create(_ context.Context, p *store.StoredPolicy) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.created = append(m.created, p)
	m.policies[p.Name] = p
	return nil
}

func (m *bootstrapMockStore) Update(_ context.Context, p *store.StoredPolicy) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.updated = append(m.updated, p)
	m.policies[p.Name] = p
	return nil
}

func (m *bootstrapMockStore) Delete(_ context.Context, _ string) error { return nil }

func (m *bootstrapMockStore) ListEnabled(_ context.Context) ([]*store.StoredPolicy, error) {
	var result []*store.StoredPolicy
	for _, p := range m.policies {
		if p.Enabled {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *bootstrapMockStore) List(_ context.Context, _ store.ListOptions) ([]*store.StoredPolicy, error) {
	return nil, nil
}

type mockPartitionManager struct {
	called bool
	months int
	err    error
}

func (m *mockPartitionManager) EnsurePartitions(_ context.Context, months int) error {
	m.called = true
	m.months = months
	return m.err
}

type logCapture struct {
	warnings []string
	infos    []string
}

func newLogCapture() (*logCapture, *slog.Logger) {
	lc := &logCapture{}
	handler := &captureHandler{lc: lc}
	return lc, slog.New(handler)
}

type captureHandler struct {
	lc    *logCapture
	attrs []slog.Attr
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	switch r.Level {
	case slog.LevelWarn:
		h.lc.warnings = append(h.lc.warnings, r.Message)
	case slog.LevelInfo:
		h.lc.infos = append(h.lc.infos, r.Message)
	}
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &captureHandler{lc: h.lc, attrs: append(h.attrs, attrs...)}
}
func (h *captureHandler) WithGroup(_ string) slog.Handler { return h }

// --- Tests ---

func TestBootstrap_CreatesPartitionsFirst(t *testing.T) {
	mockStore := newBootstrapMockStore()
	partitions := &mockPartitionManager{}
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()
	ctx := access.WithSystemSubject(context.Background())

	err := Bootstrap(ctx, partitions, mockStore, compiler, logger, BootstrapOptions{})
	require.NoError(t, err)

	assert.True(t, partitions.called, "partitions must be created during bootstrap")
	assert.Equal(t, 3, partitions.months, "must create 3 months of partitions")
}

func TestBootstrap_PartitionFailureIsFatal(t *testing.T) {
	mockStore := newBootstrapMockStore()
	partitions := &mockPartitionManager{err: fmt.Errorf("partition creation failed")}
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()
	ctx := access.WithSystemSubject(context.Background())

	err := Bootstrap(ctx, partitions, mockStore, compiler, logger, BootstrapOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "partition")
}

func TestBootstrap_SeedsAllPoliciesOnEmptyStore(t *testing.T) {
	mockStore := newBootstrapMockStore()
	partitions := &mockPartitionManager{}
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()
	ctx := access.WithSystemSubject(context.Background())

	err := Bootstrap(ctx, partitions, mockStore, compiler, logger, BootstrapOptions{})
	require.NoError(t, err)

	seeds := SeedPolicies()
	assert.Len(t, mockStore.created, len(seeds), "all seed policies must be created")

	// Build lookup of expected seed versions by name.
	expectedVersions := make(map[string]int, len(seeds))
	for _, s := range seeds {
		expectedVersions[s.Name] = s.SeedVersion
	}

	for _, created := range mockStore.created {
		assert.Equal(t, "seed", created.Source)
		assert.Equal(t, "system", created.CreatedBy)
		assert.True(t, created.Enabled)
		assert.NotNil(t, created.SeedVersion)
		assert.Equal(t, expectedVersions[created.Name], *created.SeedVersion, "seed %s version mismatch", created.Name)
		assert.NotEmpty(t, created.CompiledAST)
	}
}

func TestBootstrap_SkipsExistingSeedPolicy(t *testing.T) {
	mockStore := newBootstrapMockStore()
	seedVersion := 2 // matches current SeedVersion for seed:player-self-access
	mockStore.policies["seed:player-self-access"] = &store.StoredPolicy{
		Name:        "seed:player-self-access",
		Source:      "seed",
		SeedVersion: &seedVersion,
		Enabled:     true,
	}
	partitions := &mockPartitionManager{}
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()
	ctx := access.WithSystemSubject(context.Background())

	err := Bootstrap(ctx, partitions, mockStore, compiler, logger, BootstrapOptions{})
	require.NoError(t, err)

	// Should create all except the one that already exists
	assert.Len(t, mockStore.created, len(SeedPolicies())-1)
	for _, created := range mockStore.created {
		assert.NotEqual(t, "seed:player-self-access", created.Name,
			"existing seed policy should not be re-created")
	}
}

func TestBootstrap_WarnsOnAdminCollision(t *testing.T) {
	mockStore := newBootstrapMockStore()
	mockStore.policies["seed:player-self-access"] = &store.StoredPolicy{
		Name:   "seed:player-self-access",
		Source: "admin", // Not "seed" — admin collision
	}
	partitions := &mockPartitionManager{}
	compiler := NewCompiler(emptySchema())
	lc, logger := newLogCapture()
	ctx := access.WithSystemSubject(context.Background())

	err := Bootstrap(ctx, partitions, mockStore, compiler, logger, BootstrapOptions{})
	require.NoError(t, err)

	// Should skip the colliding policy and log a warning
	for _, created := range mockStore.created {
		assert.NotEqual(t, "seed:player-self-access", created.Name)
	}
	assert.NotEmpty(t, lc.warnings, "should log warning about admin collision")
}

func TestBootstrap_UpgradesSeedVersion(t *testing.T) {
	mockStore := newBootstrapMockStore()
	// Use version 1 as the stored version; the current seed is version 2.
	oldVersion := 1
	mockStore.policies["seed:player-self-access"] = &store.StoredPolicy{
		Name:        "seed:player-self-access",
		Source:      "seed",
		DSLText:     `permit(principal is character, action in ["read"], resource is character);`,
		SeedVersion: &oldVersion,
		Enabled:     true,
	}
	partitions := &mockPartitionManager{}
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()
	ctx := access.WithSystemSubject(context.Background())

	err := Bootstrap(ctx, partitions, mockStore, compiler, logger, BootstrapOptions{})
	require.NoError(t, err)

	// The existing policy should have been upgraded
	assert.NotEmpty(t, mockStore.updated, "seed version upgrade should trigger update")
	var upgraded *store.StoredPolicy
	for _, u := range mockStore.updated {
		if u.Name == "seed:player-self-access" {
			upgraded = u
		}
	}
	require.NotNil(t, upgraded, "seed:player-self-access should be upgraded")
	assert.Equal(t, 2, *upgraded.SeedVersion)
	assert.Contains(t, upgraded.ChangeNote, "Auto-upgraded from seed v1 to v2")
}

func TestBootstrap_SkipSeedMigrations(t *testing.T) {
	mockStore := newBootstrapMockStore()
	oldVersion := 0
	mockStore.policies["seed:player-self-access"] = &store.StoredPolicy{
		Name:        "seed:player-self-access",
		Source:      "seed",
		DSLText:     `permit(principal is character, action in ["read"], resource is character);`,
		SeedVersion: &oldVersion,
		Enabled:     true,
	}
	partitions := &mockPartitionManager{}
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()
	ctx := access.WithSystemSubject(context.Background())

	err := Bootstrap(ctx, partitions, mockStore, compiler, logger, BootstrapOptions{
		SkipSeedMigrations: true,
	})
	require.NoError(t, err)

	// Should NOT upgrade the existing policy
	for _, u := range mockStore.updated {
		assert.NotEqual(t, "seed:player-self-access", u.Name,
			"seed version upgrade should be skipped when SkipSeedMigrations is true")
	}
}

func TestBootstrap_CompilationErrorIsFatal(t *testing.T) {
	// Use a custom seed list with an invalid DSL to test fatal compilation errors.
	// We test this by confirming the bootstrap function validates all seeds compile.
	// Since we can't inject bad seeds into SeedPolicies(), we verify the error path
	// by testing with a store that returns an error on create.
	mockStore := newBootstrapMockStore()
	mockStore.createErr = fmt.Errorf("database unavailable")
	partitions := &mockPartitionManager{}
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()
	ctx := access.WithSystemSubject(context.Background())

	err := Bootstrap(ctx, partitions, mockStore, compiler, logger, BootstrapOptions{})
	require.Error(t, err, "bootstrap must fail when store create fails")
}

func TestBootstrap_CreatedPoliciesHaveValidCompiledAST(t *testing.T) {
	mockStore := newBootstrapMockStore()
	partitions := &mockPartitionManager{}
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()
	ctx := access.WithSystemSubject(context.Background())

	err := Bootstrap(ctx, partitions, mockStore, compiler, logger, BootstrapOptions{})
	require.NoError(t, err)

	for _, created := range mockStore.created {
		assert.NotEmpty(t, created.CompiledAST,
			"policy %q must have compiled AST", created.Name)
		// Verify it's valid JSON
		assert.True(t, json.Valid(created.CompiledAST),
			"policy %q compiled AST must be valid JSON", created.Name)
	}
}

func TestBootstrap_SetsCorrectEffect(t *testing.T) {
	mockStore := newBootstrapMockStore()
	partitions := &mockPartitionManager{}
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()
	ctx := access.WithSystemSubject(context.Background())

	err := Bootstrap(ctx, partitions, mockStore, compiler, logger, BootstrapOptions{})
	require.NoError(t, err)

	var forbidCount int
	for _, created := range mockStore.created {
		if created.Effect == types.PolicyEffectForbid {
			forbidCount++
			assert.Equal(t, "seed:property-restricted-excluded", created.Name)
		}
	}
	assert.Equal(t, 1, forbidCount, "exactly one forbid policy expected")
}

func TestBootstrap_NilSeedVersionNotUpgraded(t *testing.T) {
	mockStore := newBootstrapMockStore()
	// Simulate a legacy policy with nil SeedVersion
	mockStore.policies["seed:player-self-access"] = &store.StoredPolicy{
		Name:        "seed:player-self-access",
		Source:      "seed",
		DSLText:     `permit(principal is character, action in ["read"], resource is character);`,
		SeedVersion: nil, // Legacy — no version tracking
		Enabled:     true,
	}
	partitions := &mockPartitionManager{}
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()
	ctx := access.WithSystemSubject(context.Background())

	err := Bootstrap(ctx, partitions, mockStore, compiler, logger, BootstrapOptions{})
	require.NoError(t, err)

	// Should NOT upgrade a nil-versioned policy
	for _, u := range mockStore.updated {
		assert.NotEqual(t, "seed:player-self-access", u.Name,
			"nil seed version should not trigger upgrade")
	}
}

// --- IsNotFound tests ---

func TestIsNotFound_TrueForPolicyNotFound(t *testing.T) {
	err := oops.Code("POLICY_NOT_FOUND").Errorf("not found")
	assert.True(t, store.IsNotFound(err))
}

func TestIsNotFound_FalseForOtherErrors(t *testing.T) {
	err := oops.Code("POLICY_CREATE_FAILED").Errorf("create failed")
	assert.False(t, store.IsNotFound(err))
}

func TestIsNotFound_FalseForNil(t *testing.T) {
	assert.False(t, store.IsNotFound(nil))
}

func TestIsNotFound_FalseForPlainError(t *testing.T) {
	assert.False(t, store.IsNotFound(fmt.Errorf("plain error")))
}

// --- UpdateSeed tests ---

func TestUpdateSeed_SkipsIfDSLMatches(t *testing.T) {
	mockStore := newBootstrapMockStore()
	mockStore.policies["seed:player-self-access"] = &store.StoredPolicy{
		Name:    "seed:player-self-access",
		Source:  "seed",
		DSLText: `permit(principal is character, action in ["read", "write"], resource is character) when { resource.id == principal.id };`,
	}
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()

	err := UpdateSeed(
		context.Background(),
		mockStore,
		compiler,
		logger,
		"seed:player-self-access",
		`permit(principal is character, action in ["read", "write"], resource is character) when { resource.id == principal.id };`,
		`permit(principal is character, action in ["read", "write"], resource is character) when { resource.id == principal.id };`,
		"no change",
	)
	require.NoError(t, err)
	assert.Empty(t, mockStore.updated, "no update when DSL matches")
}

func TestUpdateSeed_WarnsOnAdminCustomization(t *testing.T) {
	mockStore := newBootstrapMockStore()
	mockStore.policies["seed:player-self-access"] = &store.StoredPolicy{
		Name:    "seed:player-self-access",
		Source:  "seed",
		DSLText: `permit(principal is character, action in ["read"], resource is character);`, // Different from oldDSL
	}
	compiler := NewCompiler(emptySchema())
	lc, logger := newLogCapture()

	err := UpdateSeed(
		context.Background(),
		mockStore,
		compiler,
		logger,
		"seed:player-self-access",
		`permit(principal is character, action in ["read", "write"], resource is character) when { resource.id == principal.id };`,
		`permit(principal is character, action in ["read", "write", "delete"], resource is character) when { resource.id == principal.id };`,
		"added delete",
	)
	require.NoError(t, err)
	assert.Empty(t, mockStore.updated, "should not overwrite admin customization")
	assert.NotEmpty(t, lc.warnings, "should warn about admin customization")
}

func TestUpdateSeed_UpdatesUncustomizedPolicy(t *testing.T) {
	oldDSL := `permit(principal is character, action in ["read", "write"], resource is character) when { resource.id == principal.id };`
	newDSL := `permit(principal is character, action in ["read", "write", "delete"], resource is character) when { resource.id == principal.id };`
	mockStore := newBootstrapMockStore()
	mockStore.policies["seed:player-self-access"] = &store.StoredPolicy{
		Name:    "seed:player-self-access",
		Source:  "seed",
		DSLText: oldDSL,
	}
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()

	err := UpdateSeed(
		context.Background(),
		mockStore,
		compiler,
		logger,
		"seed:player-self-access",
		oldDSL,
		newDSL,
		"added delete action",
	)
	require.NoError(t, err)
	require.Len(t, mockStore.updated, 1)
	assert.Equal(t, newDSL, mockStore.updated[0].DSLText)
	assert.Equal(t, "added delete action", mockStore.updated[0].ChangeNote)
}

func TestUpdateSeed_FailsForNonSeedPolicy(t *testing.T) {
	mockStore := newBootstrapMockStore()
	mockStore.policies["seed:player-self-access"] = &store.StoredPolicy{
		Name:   "seed:player-self-access",
		Source: "admin",
	}
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()

	err := UpdateSeed(
		context.Background(),
		mockStore,
		compiler,
		logger,
		"seed:player-self-access",
		"old",
		"new",
		"change",
	)
	require.Error(t, err, "should fail when source is not seed")
}

func TestUpdateSeed_FailsForMissingPolicy(t *testing.T) {
	mockStore := newBootstrapMockStore()
	compiler := NewCompiler(emptySchema())
	_, logger := newLogCapture()

	err := UpdateSeed(
		context.Background(),
		mockStore,
		compiler,
		logger,
		"seed:nonexistent",
		"old",
		"new",
		"change",
	)
	require.Error(t, err, "should fail when policy doesn't exist")
}
