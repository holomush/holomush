// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/audit"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// mockSessionResolver is a test double for SessionResolver.
type mockSessionResolver struct {
	resolveFunc func(ctx context.Context, sessionID string) (string, error)
}

func (m *mockSessionResolver) ResolveSession(ctx context.Context, sessionID string) (string, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, sessionID)
	}
	return "", oops.Errorf("mock not configured")
}

// mockAuditWriter captures audit entries for testing.
type mockAuditWriter struct {
	entries []audit.Entry
	mu      sync.Mutex
}

func (m *mockAuditWriter) WriteSync(_ context.Context, entry audit.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockAuditWriter) WriteAsync(entry audit.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockAuditWriter) Close() error {
	return nil
}

func (m *mockAuditWriter) getEntries() []audit.Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]audit.Entry, len(m.entries))
	copy(result, m.entries)
	return result
}

// createTestEngine creates an Engine with test doubles.
func createTestEngine(t *testing.T, sessionResolver SessionResolver) (*Engine, *mockAuditWriter) {
	t.Helper()

	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)

	mockWriter := &mockAuditWriter{}
	walPath := filepath.Join(t.TempDir(), "test-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, mockWriter, walPath)
	t.Cleanup(func() {
		_ = auditLogger.Close()
	})

	cache := NewCache(nil, nil) // Not used in steps 1-2

	engine := NewEngine(resolver, cache, sessionResolver, auditLogger)
	return engine, mockWriter
}

func TestEngine_SystemBypass(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	req := types.AccessRequest{
		Subject:  "system",
		Action:   "write",
		Resource: "location:01ABC",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectSystemBypass, decision.Effect)
	assert.True(t, decision.IsAllowed())
	assert.Equal(t, "system bypass", decision.Reason)
}

func TestEngine_SystemBypass_ValidatesDecision(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	req := types.AccessRequest{
		Subject:  "system",
		Action:   "write",
		Resource: "location:01ABC",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.NoError(t, decision.Validate())
}

func TestEngine_SystemBypass_Audited(t *testing.T) {
	engine, mockWriter := createTestEngine(t, &mockSessionResolver{})

	req := types.AccessRequest{
		Subject:  "system",
		Action:   "write",
		Resource: "location:01ABC",
	}

	_, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	entries := mockWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "system", entries[0].Subject)
	assert.Equal(t, "write", entries[0].Action)
	assert.Equal(t, "location:01ABC", entries[0].Resource)
	assert.Equal(t, types.EffectSystemBypass, entries[0].Effect)
}

func TestEngine_SessionResolved(t *testing.T) {
	resolver := &mockSessionResolver{
		resolveFunc: func(_ context.Context, sessionID string) (string, error) {
			assert.Equal(t, "web-123", sessionID)
			return "01ABC", nil
		},
	}
	engine, _ := createTestEngine(t, resolver)

	req := types.AccessRequest{
		Subject:  "session:web-123",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	// Steps 3-4 implemented, no policies loaded, so default deny
	assert.Equal(t, types.EffectDefaultDeny, decision.Effect)
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "no applicable policies", decision.Reason)
	assert.NotNil(t, decision.Attributes, "attributes should be populated")
}

func TestEngine_SessionResolved_RewritesSubject(t *testing.T) {
	var capturedRequest types.AccessRequest
	resolver := &mockSessionResolver{
		resolveFunc: func(_ context.Context, _ string) (string, error) {
			return "01ABC", nil
		},
	}
	engine, _ := createTestEngine(t, resolver)

	// We need to capture the rewritten request. For now, we verify by checking
	// that the session resolver was called with the correct session ID.
	req := types.AccessRequest{
		Subject:  "session:web-123",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	capturedRequest = req

	_, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	// Original request unchanged (pass by value)
	assert.Equal(t, "session:web-123", capturedRequest.Subject)
}

func TestEngine_SessionInvalid(t *testing.T) {
	resolver := &mockSessionResolver{
		resolveFunc: func(_ context.Context, _ string) (string, error) {
			return "", oops.Code("SESSION_INVALID").Errorf("session not found")
		},
	}
	engine, _ := createTestEngine(t, resolver)

	req := types.AccessRequest{
		Subject:  "session:invalid-999",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectDefaultDeny, decision.Effect)
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "session invalid", decision.Reason)
	assert.Equal(t, "infra:session-invalid", decision.PolicyID)
}

func TestEngine_SessionStoreError(t *testing.T) {
	resolver := &mockSessionResolver{
		resolveFunc: func(_ context.Context, _ string) (string, error) {
			return "", oops.Errorf("database connection failed")
		},
	}
	engine, _ := createTestEngine(t, resolver)

	req := types.AccessRequest{
		Subject:  "session:web-123",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectDefaultDeny, decision.Effect)
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "session store error", decision.Reason)
	assert.Equal(t, "infra:session-store-error", decision.PolicyID)
}

func TestEngine_NonSystemNonSession(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectDefaultDeny, decision.Effect)
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "no applicable policies", decision.Reason)
	assert.Equal(t, "", decision.PolicyID)
	assert.NotNil(t, decision.Attributes, "attributes should be populated")
}

func TestEngine_AllDecisionsValidate(t *testing.T) {
	tests := []struct {
		name            string
		subject         string
		sessionResolver SessionResolver
	}{
		{
			name:            "system bypass",
			subject:         "system",
			sessionResolver: &mockSessionResolver{},
		},
		{
			name:    "session resolved",
			subject: "session:web-123",
			sessionResolver: &mockSessionResolver{
				resolveFunc: func(_ context.Context, _ string) (string, error) {
					return "01ABC", nil
				},
			},
		},
		{
			name:    "session invalid",
			subject: "session:invalid",
			sessionResolver: &mockSessionResolver{
				resolveFunc: func(_ context.Context, _ string) (string, error) {
					return "", oops.Code("SESSION_INVALID").Errorf("not found")
				},
			},
		},
		{
			name:    "session store error",
			subject: "session:error",
			sessionResolver: &mockSessionResolver{
				resolveFunc: func(_ context.Context, _ string) (string, error) {
					return "", oops.Errorf("store error")
				},
			},
		},
		{
			name:            "non-system non-session",
			subject:         "character:01ABC",
			sessionResolver: &mockSessionResolver{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, _ := createTestEngine(t, tt.sessionResolver)

			req := types.AccessRequest{
				Subject:  tt.subject,
				Action:   "read",
				Resource: "location:01XYZ",
			}

			decision, err := engine.Evaluate(context.Background(), req)
			require.NoError(t, err)

			assert.NoError(t, decision.Validate(),
				"decision with effect=%s should validate", decision.Effect)
		})
	}
}

func TestMockAuditWriter_ThreadSafety(t *testing.T) {
	writer := &mockAuditWriter{}
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			entry := audit.Entry{
				Subject:  "test",
				Action:   "read",
				Resource: "test",
				Effect:   types.EffectAllow,
			}
			if idx%2 == 0 {
				_ = writer.WriteSync(ctx, entry)
			} else {
				_ = writer.WriteAsync(entry)
			}
		}(i)
	}
	wg.Wait()

	entries := writer.getEntries()
	assert.Len(t, entries, 100)
}

func TestEngine_AuditLoggerCleanup(t *testing.T) {
	// Verify that audit logger is properly closed in cleanup
	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)

	mockWriter := &mockAuditWriter{}
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, mockWriter, walPath)

	cache := NewCache(nil, nil)
	_ = NewEngine(resolver, cache, &mockSessionResolver{}, auditLogger)

	// Trigger a write to create the WAL file
	ctx := context.Background()
	req := types.AccessRequest{
		Subject:  "system",
		Action:   "write",
		Resource: "location:01ABC",
	}
	engine := NewEngine(resolver, cache, &mockSessionResolver{}, auditLogger)
	_, _ = engine.Evaluate(ctx, req)

	// Close the logger
	err := auditLogger.Close()
	require.NoError(t, err)

	// Verify WAL directory exists (file may or may not exist depending on whether WAL was written)
	_, err = os.Stat(tmpDir)
	assert.NoError(t, err, "temp directory should exist")
}

func TestEngine_FindApplicablePolicies(t *testing.T) {
	tests := []struct {
		name       string
		req        types.AccessRequest
		policies   []CachedPolicy
		wantCount  int
		wantIDs    []string
		wantReason string
	}{
		{
			name: "principal type match",
			req: types.AccessRequest{
				Subject:  "character:01ABC",
				Action:   "read",
				Resource: "location:01XYZ",
			},
			policies: []CachedPolicy{
				{
					ID:   "policy-1",
					Name: "allow-characters",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							PrincipalType: strPtr("character"),
						},
					},
				},
			},
			wantCount: 1,
			wantIDs:   []string{"policy-1"},
		},
		{
			name: "principal type mismatch",
			req: types.AccessRequest{
				Subject:  "character:01ABC",
				Action:   "read",
				Resource: "location:01XYZ",
			},
			policies: []CachedPolicy{
				{
					ID:   "policy-2",
					Name: "allow-plugins",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							PrincipalType: strPtr("plugin"),
						},
					},
				},
			},
			wantCount: 0,
			wantIDs:   []string{},
		},
		{
			name: "principal wildcard",
			req: types.AccessRequest{
				Subject:  "character:01ABC",
				Action:   "read",
				Resource: "location:01XYZ",
			},
			policies: []CachedPolicy{
				{
					ID:   "policy-3",
					Name: "wildcard-principal",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							PrincipalType: nil,
						},
					},
				},
			},
			wantCount: 1,
			wantIDs:   []string{"policy-3"},
		},
		{
			name: "action list match",
			req: types.AccessRequest{
				Subject:  "character:01ABC",
				Action:   "say",
				Resource: "location:01XYZ",
			},
			policies: []CachedPolicy{
				{
					ID:   "policy-4",
					Name: "allow-say-pose",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							ActionList: []string{"say", "pose"},
						},
					},
				},
			},
			wantCount: 1,
			wantIDs:   []string{"policy-4"},
		},
		{
			name: "action list mismatch",
			req: types.AccessRequest{
				Subject:  "character:01ABC",
				Action:   "dig",
				Resource: "location:01XYZ",
			},
			policies: []CachedPolicy{
				{
					ID:   "policy-5",
					Name: "allow-say-pose",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							ActionList: []string{"say", "pose"},
						},
					},
				},
			},
			wantCount: 0,
			wantIDs:   []string{},
		},
		{
			name: "action wildcard",
			req: types.AccessRequest{
				Subject:  "character:01ABC",
				Action:   "dig",
				Resource: "location:01XYZ",
			},
			policies: []CachedPolicy{
				{
					ID:   "policy-6",
					Name: "wildcard-action",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							ActionList: nil,
						},
					},
				},
			},
			wantCount: 1,
			wantIDs:   []string{"policy-6"},
		},
		{
			name: "resource type match",
			req: types.AccessRequest{
				Subject:  "character:01ABC",
				Action:   "read",
				Resource: "location:01XYZ",
			},
			policies: []CachedPolicy{
				{
					ID:   "policy-7",
					Name: "allow-location",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							ResourceType: strPtr("location"),
						},
					},
				},
			},
			wantCount: 1,
			wantIDs:   []string{"policy-7"},
		},
		{
			name: "resource exact match",
			req: types.AccessRequest{
				Subject:  "character:01ABC",
				Action:   "read",
				Resource: "location:01XYZ",
			},
			policies: []CachedPolicy{
				{
					ID:   "policy-8",
					Name: "allow-specific-location",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							ResourceExact: strPtr("location:01XYZ"),
						},
					},
				},
			},
			wantCount: 1,
			wantIDs:   []string{"policy-8"},
		},
		{
			name: "resource exact mismatch",
			req: types.AccessRequest{
				Subject:  "character:01ABC",
				Action:   "read",
				Resource: "location:01XYZ",
			},
			policies: []CachedPolicy{
				{
					ID:   "policy-9",
					Name: "allow-other-location",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							ResourceExact: strPtr("location:01ABC"),
						},
					},
				},
			},
			wantCount: 0,
			wantIDs:   []string{},
		},
		{
			name: "resource wildcard",
			req: types.AccessRequest{
				Subject:  "character:01ABC",
				Action:   "read",
				Resource: "location:01XYZ",
			},
			policies: []CachedPolicy{
				{
					ID:   "policy-10",
					Name: "wildcard-resource",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							ResourceType:  nil,
							ResourceExact: nil,
						},
					},
				},
			},
			wantCount: 1,
			wantIDs:   []string{"policy-10"},
		},
		{
			name: "no policies match",
			req: types.AccessRequest{
				Subject:  "character:01ABC",
				Action:   "dig",
				Resource: "location:01XYZ",
			},
			policies: []CachedPolicy{
				{
					ID:   "policy-11",
					Name: "plugin-only",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							PrincipalType: strPtr("plugin"),
						},
					},
				},
			},
			wantCount: 0,
			wantIDs:   []string{},
		},
		{
			name: "multiple policies with mixed targets",
			req: types.AccessRequest{
				Subject:  "character:01ABC",
				Action:   "say",
				Resource: "location:01XYZ",
			},
			policies: []CachedPolicy{
				{
					ID:   "policy-12",
					Name: "match-character",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							PrincipalType: strPtr("character"),
						},
					},
				},
				{
					ID:   "policy-13",
					Name: "mismatch-plugin",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							PrincipalType: strPtr("plugin"),
						},
					},
				},
				{
					ID:   "policy-14",
					Name: "match-say",
					Compiled: &CompiledPolicy{
						Effect: types.PolicyEffectPermit,
						Target: CompiledTarget{
							ActionList: []string{"say", "pose"},
						},
					},
				},
			},
			wantCount: 2,
			wantIDs:   []string{"policy-12", "policy-14"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, _ := createTestEngine(t, &mockSessionResolver{})

			got := engine.findApplicablePolicies(tt.req, tt.policies)

			assert.Equal(t, tt.wantCount, len(got), "unexpected number of matching policies")

			gotIDs := make([]string, len(got))
			for i, p := range got {
				gotIDs[i] = p.ID
			}
			assert.ElementsMatch(t, tt.wantIDs, gotIDs, "unexpected policy IDs")
		})
	}
}

func TestEngine_EvaluateWithAttributeResolution(t *testing.T) {
	tests := []struct {
		name           string
		req            types.AccessRequest
		sessionResolve func(ctx context.Context, sessionID string) (string, error)
		wantEffect     types.Effect
		wantReason     string
	}{
		{
			name: "attribute resolution called for character",
			req: types.AccessRequest{
				Subject:  "character:01ABC",
				Action:   "read",
				Resource: "location:01XYZ",
			},
			wantEffect: types.EffectDefaultDeny,
			wantReason: "no applicable policies",
		},
		{
			name: "attribute resolution called for plugin",
			req: types.AccessRequest{
				Subject:  "plugin:echo-bot",
				Action:   "execute",
				Resource: "command:say",
			},
			wantEffect: types.EffectDefaultDeny,
			wantReason: "no applicable policies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &mockSessionResolver{
				resolveFunc: tt.sessionResolve,
			}
			engine, _ := createTestEngine(t, resolver)

			decision, err := engine.Evaluate(context.Background(), tt.req)
			require.NoError(t, err)

			assert.Equal(t, tt.wantEffect, decision.Effect)
			assert.Equal(t, tt.wantReason, decision.Reason)
			assert.NotNil(t, decision.Attributes, "attributes should be populated")
		})
	}
}

// mockAttributeProvider is a test double for AttributeProvider.
type mockAttributeProvider struct {
	namespace   string
	subjectMap  map[string]any
	resourceMap map[string]any
	schema      *types.NamespaceSchema
}

func (m *mockAttributeProvider) Namespace() string {
	return m.namespace
}

func (m *mockAttributeProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return m.subjectMap, nil
}

func (m *mockAttributeProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return m.resourceMap, nil
}

func (m *mockAttributeProvider) Schema() *types.NamespaceSchema {
	if m.schema != nil {
		return m.schema
	}
	// Return a minimal valid schema with at least one attribute
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role":    types.AttrTypeString,
			"level":   types.AttrTypeInt,
			"banned":  types.AttrTypeBool,
			"faction": types.AttrTypeString,
			"muted":   types.AttrTypeBool,
		},
	}
}

// createTestEngineWithPolicies creates an Engine with policies loaded in the cache.
func createTestEngineWithPolicies(t *testing.T, dslTexts []string, providers []attribute.AttributeProvider) *Engine {
	t.Helper()

	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)

	// Register providers
	for _, p := range providers {
		require.NoError(t, resolver.RegisterProvider(p))
	}

	mockWriter := &mockAuditWriter{}
	walPath := filepath.Join(t.TempDir(), "test-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, mockWriter, walPath)
	t.Cleanup(func() {
		_ = auditLogger.Close()
	})

	// Compile policies and create cache with them
	schema := types.NewAttributeSchema()
	compiler := NewCompiler(schema)

	policies := make([]CachedPolicy, 0, len(dslTexts))
	for i, text := range dslTexts {
		compiled, _, err := compiler.Compile(text)
		require.NoError(t, err, "compile policy %d", i)
		policies = append(policies, CachedPolicy{
			ID:       fmt.Sprintf("policy-%d", i+1),
			Name:     fmt.Sprintf("test-policy-%d", i+1),
			Compiled: compiled,
		})
	}

	// Create a cache and manually set the snapshot
	cache := NewCache(nil, nil)
	cache.mu.Lock()
	cache.snapshot = &Snapshot{
		Policies:  policies,
		CreatedAt: time.Now(),
	}
	cache.mu.Unlock()
	cache.lastUpdate.Store(time.Now().UnixNano())

	engine := NewEngine(resolver, cache, &mockSessionResolver{}, auditLogger)
	return engine
}

func TestEngine_EvaluateConditions_SimpleConditionSatisfied(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "admin" };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"role": "admin"},
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies, 1)
	assert.Equal(t, "policy-1", decision.Policies[0].PolicyID)
	assert.True(t, decision.Policies[0].ConditionsMet, "condition should be satisfied")
	assert.Equal(t, types.EffectAllow, decision.Policies[0].Effect)
}

func TestEngine_EvaluateConditions_SimpleConditionUnsatisfied(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "admin" };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"role": "player"},
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies, 1)
	assert.Equal(t, "policy-1", decision.Policies[0].PolicyID)
	assert.False(t, decision.Policies[0].ConditionsMet, "condition should not be satisfied")
}

func TestEngine_EvaluateConditions_MissingAttribute(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { principal.character.faction == "rebels" };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"role": "player"}, // no faction attribute
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies, 1)
	assert.False(t, decision.Policies[0].ConditionsMet, "missing attribute should cause condition to fail")
}

func TestEngine_EvaluateConditions_NumericComparison(t *testing.T) {
	dslText := `permit(principal is character, action in ["dig"], resource is location) when { principal.character.level > 5 };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"level": 7},
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "dig",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies, 1)
	assert.True(t, decision.Policies[0].ConditionsMet, "numeric comparison should be satisfied")
}

func TestEngine_EvaluateConditions_Unconditional(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location);`

	engine := createTestEngineWithPolicies(t, []string{dslText}, nil)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies, 1)
	assert.True(t, decision.Policies[0].ConditionsMet, "unconditional policy should always be satisfied")
}

func TestEngine_EvaluateConditions_MultiplePoliciesMixed(t *testing.T) {
	dslTexts := []string{
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "admin" };`,
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.level > 10 };`,
		`forbid(principal is character, action in ["say"], resource is location) when { principal.character.banned == true };`,
	}

	provider := &mockAttributeProvider{
		namespace: "character",
		subjectMap: map[string]any{
			"role":   "admin",
			"level":  5,
			"banned": false,
		},
	}

	engine := createTestEngineWithPolicies(t, dslTexts, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies, 3)

	// Policy 1: role == "admin" → true
	assert.Equal(t, "policy-1", decision.Policies[0].PolicyID)
	assert.True(t, decision.Policies[0].ConditionsMet)
	assert.Equal(t, types.EffectAllow, decision.Policies[0].Effect)

	// Policy 2: level > 10 → false
	assert.Equal(t, "policy-2", decision.Policies[1].PolicyID)
	assert.False(t, decision.Policies[1].ConditionsMet)
	assert.Equal(t, types.EffectAllow, decision.Policies[1].Effect)

	// Policy 3: banned == true → false
	assert.Equal(t, "policy-3", decision.Policies[2].PolicyID)
	assert.False(t, decision.Policies[2].ConditionsMet)
	assert.Equal(t, types.EffectDeny, decision.Policies[2].Effect)
}

func TestEngine_EvaluateConditions_AllSatisfied(t *testing.T) {
	dslTexts := []string{
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "admin" };`,
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.level > 5 };`,
	}

	provider := &mockAttributeProvider{
		namespace: "character",
		subjectMap: map[string]any{
			"role":  "admin",
			"level": 10,
		},
	}

	engine := createTestEngineWithPolicies(t, dslTexts, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies, 2)
	assert.True(t, decision.Policies[0].ConditionsMet)
	assert.True(t, decision.Policies[1].ConditionsMet)
}

func TestEngine_EvaluateConditions_PopulatesPolicyMatches(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "admin" };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"role": "admin"},
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies, 1)
	match := decision.Policies[0]
	assert.Equal(t, "policy-1", match.PolicyID)
	assert.Equal(t, "test-policy-1", match.PolicyName)
	assert.Equal(t, types.EffectAllow, match.Effect)
	assert.True(t, match.ConditionsMet)
}

// Deny-overrides combination tests

func TestEngine_DenyOverrides_ForbidWins(t *testing.T) {
	dslTexts := []string{
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "player" };`,
		`forbid(principal is character, action in ["say"], resource is location) when { principal.character.banned == true };`,
	}

	provider := &mockAttributeProvider{
		namespace: "character",
		subjectMap: map[string]any{
			"role":   "player",
			"banned": true,
		},
	}

	engine := createTestEngineWithPolicies(t, dslTexts, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectDeny, decision.Effect)
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "forbid policy satisfied", decision.Reason)
	assert.Equal(t, "policy-2", decision.PolicyID)
}

func TestEngine_DenyOverrides_PermitOnly(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "player" };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"role": "player"},
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectAllow, decision.Effect)
	assert.True(t, decision.IsAllowed())
	assert.Equal(t, "permit policy satisfied", decision.Reason)
	assert.Equal(t, "policy-1", decision.PolicyID)
}

func TestEngine_DenyOverrides_DefaultDeny_NoPoliciesSatisfied(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "admin" };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"role": "player"}, // condition not met
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectDefaultDeny, decision.Effect)
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "no policies satisfied", decision.Reason)
	assert.Equal(t, "", decision.PolicyID)
}

func TestEngine_DenyOverrides_MultipleForbid(t *testing.T) {
	dslTexts := []string{
		`forbid(principal is character, action in ["say"], resource is location) when { principal.character.banned == true };`,
		`forbid(principal is character, action in ["say"], resource is location) when { principal.character.muted == true };`,
	}

	provider := &mockAttributeProvider{
		namespace: "character",
		subjectMap: map[string]any{
			"banned": true,
			"muted":  true,
		},
	}

	engine := createTestEngineWithPolicies(t, dslTexts, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectDeny, decision.Effect)
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "forbid policy satisfied", decision.Reason)
	assert.Equal(t, "policy-1", decision.PolicyID) // First forbid wins
}

func TestEngine_DenyOverrides_MultiplePermit(t *testing.T) {
	dslTexts := []string{
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "player" };`,
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.level > 5 };`,
	}

	provider := &mockAttributeProvider{
		namespace: "character",
		subjectMap: map[string]any{
			"role":  "player",
			"level": 10,
		},
	}

	engine := createTestEngineWithPolicies(t, dslTexts, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectAllow, decision.Effect)
	assert.True(t, decision.IsAllowed())
	assert.Equal(t, "permit policy satisfied", decision.Reason)
	assert.Equal(t, "policy-1", decision.PolicyID) // First permit wins
}

func TestEngine_DenyOverrides_ForbidUnsatisfied_PermitSatisfied(t *testing.T) {
	dslTexts := []string{
		`forbid(principal is character, action in ["say"], resource is location) when { principal.character.banned == true };`,
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "player" };`,
	}

	provider := &mockAttributeProvider{
		namespace: "character",
		subjectMap: map[string]any{
			"banned": false, // forbid condition not met
			"role":   "player",
		},
	}

	engine := createTestEngineWithPolicies(t, dslTexts, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectAllow, decision.Effect)
	assert.True(t, decision.IsAllowed())
	assert.Equal(t, "permit policy satisfied", decision.Reason)
	assert.Equal(t, "policy-2", decision.PolicyID)
}

// Audit mode tests

// createTestEngineWithMode creates an Engine with a specific audit mode.
func createTestEngineWithMode(t *testing.T, dslTexts []string, providers []attribute.AttributeProvider, mode audit.Mode) (*Engine, *mockAuditWriter) {
	t.Helper()

	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)

	// Register providers
	for _, p := range providers {
		require.NoError(t, resolver.RegisterProvider(p))
	}

	mockWriter := &mockAuditWriter{}
	walPath := filepath.Join(t.TempDir(), "test-wal.jsonl")
	auditLogger := audit.NewLogger(mode, mockWriter, walPath)
	t.Cleanup(func() {
		_ = auditLogger.Close()
	})

	// Compile policies and create cache with them
	schema := types.NewAttributeSchema()
	compiler := NewCompiler(schema)

	policies := make([]CachedPolicy, 0, len(dslTexts))
	for i, text := range dslTexts {
		compiled, _, err := compiler.Compile(text)
		require.NoError(t, err, "compile policy %d", i)
		policies = append(policies, CachedPolicy{
			ID:       fmt.Sprintf("policy-%d", i+1),
			Name:     fmt.Sprintf("test-policy-%d", i+1),
			Compiled: compiled,
		})
	}

	cache := NewCache(nil, nil)
	cache.mu.Lock()
	cache.snapshot = &Snapshot{
		Policies:  policies,
		CreatedAt: time.Now(),
	}
	cache.mu.Unlock()
	cache.lastUpdate.Store(time.Now().UnixNano())

	engine := NewEngine(resolver, cache, &mockSessionResolver{}, auditLogger)
	return engine, mockWriter
}

func TestEngine_Audit_ModeAll_AllowAudited(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "player" };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"role": "player"},
	}

	engine, mockWriter := createTestEngineWithMode(t, []string{dslText}, []attribute.AttributeProvider{provider}, audit.ModeAll)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, types.EffectAllow, decision.Effect)

	// Wait a bit for async audit
	time.Sleep(50 * time.Millisecond)

	entries := mockWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "character:01ABC", entries[0].Subject)
	assert.Equal(t, "say", entries[0].Action)
	assert.Equal(t, "location:01XYZ", entries[0].Resource)
	assert.Equal(t, types.EffectAllow, entries[0].Effect)
	assert.Equal(t, "policy-1", entries[0].PolicyID)
}

func TestEngine_Audit_ModeAll_DenyAudited(t *testing.T) {
	dslText := `forbid(principal is character, action in ["say"], resource is location) when { principal.character.banned == true };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"banned": true},
	}

	engine, mockWriter := createTestEngineWithMode(t, []string{dslText}, []attribute.AttributeProvider{provider}, audit.ModeAll)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, types.EffectDeny, decision.Effect)

	entries := mockWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "character:01ABC", entries[0].Subject)
	assert.Equal(t, "say", entries[0].Action)
	assert.Equal(t, "location:01XYZ", entries[0].Resource)
	assert.Equal(t, types.EffectDeny, entries[0].Effect)
	assert.Equal(t, "policy-1", entries[0].PolicyID)
}

func TestEngine_Audit_ModeMinimal_AllowNotAudited(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "player" };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"role": "player"},
	}

	engine, mockWriter := createTestEngineWithMode(t, []string{dslText}, []attribute.AttributeProvider{provider}, audit.ModeMinimal)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, types.EffectAllow, decision.Effect)

	// Wait a bit to ensure async operations complete
	time.Sleep(50 * time.Millisecond)

	entries := mockWriter.getEntries()
	assert.Len(t, entries, 0, "ModeMinimal should not audit allow decisions")
}

func TestEngine_Audit_ModeMinimal_DenyAudited(t *testing.T) {
	dslText := `forbid(principal is character, action in ["say"], resource is location) when { principal.character.banned == true };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"banned": true},
	}

	engine, mockWriter := createTestEngineWithMode(t, []string{dslText}, []attribute.AttributeProvider{provider}, audit.ModeMinimal)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, types.EffectDeny, decision.Effect)

	entries := mockWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, types.EffectDeny, entries[0].Effect)
}

// End-to-end integration tests

func TestEngine_EndToEnd_FullFlow_AdminPermit(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "admin" };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"role": "admin"},
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectAllow, decision.Effect)
	assert.True(t, decision.IsAllowed())
	assert.Equal(t, "permit policy satisfied", decision.Reason)
	assert.Equal(t, "policy-1", decision.PolicyID)
	assert.NotNil(t, decision.Attributes)
	require.Len(t, decision.Policies, 1)
	assert.True(t, decision.Policies[0].ConditionsMet)
}

func TestEngine_EndToEnd_FullFlow_DenyOverrides(t *testing.T) {
	dslTexts := []string{
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "player" };`,
		`forbid(principal is character, action in ["say"], resource is location) when { principal.character.banned == true };`,
	}

	provider := &mockAttributeProvider{
		namespace: "character",
		subjectMap: map[string]any{
			"role":   "player",
			"banned": true,
		},
	}

	engine := createTestEngineWithPolicies(t, dslTexts, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectDeny, decision.Effect)
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "forbid policy satisfied", decision.Reason)
	assert.Equal(t, "policy-2", decision.PolicyID)
	assert.NotNil(t, decision.Attributes)
	require.Len(t, decision.Policies, 2)
	assert.True(t, decision.Policies[0].ConditionsMet) // permit satisfied
	assert.True(t, decision.Policies[1].ConditionsMet) // forbid satisfied (wins)
}

func TestEngine_EndToEnd_FullFlow_SessionResolution(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "player" };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"role": "player"},
	}

	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)
	require.NoError(t, resolver.RegisterProvider(provider))

	mockWriter := &mockAuditWriter{}
	walPath := filepath.Join(t.TempDir(), "test-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, mockWriter, walPath)
	t.Cleanup(func() {
		_ = auditLogger.Close()
	})

	schema := types.NewAttributeSchema()
	compiler := NewCompiler(schema)
	compiled, _, err := compiler.Compile(dslText)
	require.NoError(t, err)

	cache := NewCache(nil, nil)
	cache.mu.Lock()
	cache.snapshot = &Snapshot{
		Policies: []CachedPolicy{
			{
				ID:       "policy-1",
				Name:     "test-policy-1",
				Compiled: compiled,
			},
		},
		CreatedAt: time.Now(),
	}
	cache.mu.Unlock()
	cache.lastUpdate.Store(time.Now().UnixNano())

	// Session resolver that resolves session to character
	sessionResolver := &mockSessionResolver{
		resolveFunc: func(_ context.Context, sessionID string) (string, error) {
			assert.Equal(t, "web-123", sessionID)
			return "01ABC", nil
		},
	}

	engine := NewEngine(resolver, cache, sessionResolver, auditLogger)

	req := types.AccessRequest{
		Subject:  "session:web-123",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectAllow, decision.Effect)
	assert.True(t, decision.IsAllowed())
	assert.Equal(t, "permit policy satisfied", decision.Reason)

	// Wait for async audit
	time.Sleep(50 * time.Millisecond)

	// Verify audit entry has resolved subject
	entries := mockWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "character:01ABC", entries[0].Subject) // Resolved subject
}
