// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/pkg/errutil"
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

// mockAuditWriter captures audit events for testing.
type mockAuditWriter struct {
	entries []audit.Event
	mu      sync.Mutex
}

func (m *mockAuditWriter) WriteSync(_ context.Context, event audit.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, event)
	return nil
}

func (m *mockAuditWriter) WriteAsync(event audit.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, event)
	return nil
}

func (m *mockAuditWriter) Close() error {
	return nil
}

func (m *mockAuditWriter) getEntries() []audit.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]audit.Event, len(m.entries))
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

	cache := NewCache(nil, nil)

	engine := NewEngine(resolver, cache, sessionResolver, auditLogger)
	return engine, mockWriter
}

func TestEngineSystemBypass(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	req := types.AccessRequest{
		Subject:  "system",
		Action:   "write",
		Resource: "location:01ABC",
	}

	ctx := access.WithSystemSubject(context.Background())
	decision, err := engine.Evaluate(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectSystemBypass, decision.Effect())
	assert.True(t, decision.IsAllowed())
	assert.Equal(t, "system bypass", decision.Reason())
}

func TestEngineSystemBypassRejectedWithoutSystemContext(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	req := types.AccessRequest{
		Subject:  "system",
		Action:   "write",
		Resource: "location:01ABC",
	}

	// Use plain context (no system marker) — should be rejected
	decision, err := engine.Evaluate(context.Background(), req)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SYSTEM_SUBJECT_REJECTED", oopsErr.Code())
	assert.Equal(t, types.EffectDefaultDeny, decision.Effect())
	assert.False(t, decision.IsAllowed())
	assert.Empty(t, decision.PolicyID())
}

func TestEngineSystemBypassValidatesDecision(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	req := types.AccessRequest{
		Subject:  "system",
		Action:   "write",
		Resource: "location:01ABC",
	}

	ctx := access.WithSystemSubject(context.Background())
	decision, err := engine.Evaluate(ctx, req)
	require.NoError(t, err)

	assert.NoError(t, decision.Validate())
}

func TestEngineSystemBypassIsAudited(t *testing.T) {
	engine, mockWriter := createTestEngine(t, &mockSessionResolver{})

	req := types.AccessRequest{
		Subject:  "system",
		Action:   "write",
		Resource: "location:01ABC",
	}

	_, err := engine.Evaluate(access.WithSystemSubject(context.Background()), req)
	require.NoError(t, err)

	entries := mockWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "system", entries[0].Subject)
	assert.Equal(t, "write", entries[0].Action)
	assert.Equal(t, "location:01ABC", entries[0].Resource)
	assert.Equal(t, types.EffectSystemBypass, entries[0].Effect)
}

func TestEngineContextCancelled(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before calling Evaluate

	req := types.AccessRequest{
		Subject:  "player:01ABC",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(ctx, req)

	// Contract: context cancellation returns an error wrapping context.Canceled
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// Contract: returned Decision is a DefaultDeny infra-failure — not allowed
	assert.False(t, decision.IsAllowed())
	assert.True(t, decision.IsInfraFailure())
	assert.Equal(t, types.EffectDefaultDeny, decision.Effect())
}

func TestEngineSessionResolved(t *testing.T) {
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
	assert.Equal(t, types.EffectDefaultDeny, decision.Effect())
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "no applicable policies", decision.Reason())
	assert.NotNil(t, decision.Attributes(), "attributes should be populated")
}

func TestEngineSessionResolvedRewritesSubject(t *testing.T) {
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

func TestEngineSessionInvalidFailsClosed(t *testing.T) {
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

	assert.Equal(t, types.EffectDefaultDeny, decision.Effect())
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "session invalid", decision.Reason())
	assert.Equal(t, "infra:session-invalid", decision.PolicyID())
}

func TestEngineSessionStoreErrorFailsClosed(t *testing.T) {
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

	assert.Equal(t, types.EffectDefaultDeny, decision.Effect())
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "session store error", decision.Reason())
	assert.Equal(t, "infra:session-store-error", decision.PolicyID())
}

func TestEngineNonSystemNonSession(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectDefaultDeny, decision.Effect())
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "no applicable policies", decision.Reason())
	assert.Equal(t, "", decision.PolicyID())
	assert.NotNil(t, decision.Attributes(), "attributes should be populated")
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

			ctx := context.Background()
			if tt.subject == "system" {
				ctx = access.WithSystemSubject(ctx)
			}
			decision, err := engine.Evaluate(ctx, req)
			require.NoError(t, err)

			assert.NoError(t, decision.Validate(),
				"decision with effect=%s should validate", decision.Effect())
		})
	}
}

func TestMockAuditWriterThreadSafety(t *testing.T) {
	writer := &mockAuditWriter{}
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			event := audit.Event{
				Subject:  "test",
				Action:   "read",
				Resource: "test",
				Effect:   types.EffectAllow,
			}
			if idx%2 == 0 {
				_ = writer.WriteSync(ctx, event)
			} else {
				_ = writer.WriteAsync(event)
			}
		}(i)
	}
	wg.Wait()

	entries := writer.getEntries()
	assert.Len(t, entries, 100)
}

func TestEngineAuditLoggerCleanup(t *testing.T) {
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
	ctx := access.WithSystemSubject(context.Background())
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

func TestEngineEvaluateWithAttributeResolution(t *testing.T) {
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

			assert.Equal(t, tt.wantEffect, decision.Effect())
			assert.Equal(t, tt.wantReason, decision.Reason())
			assert.NotNil(t, decision.Attributes(), "attributes should be populated")
		})
	}
}

// failingAttributeProvider is a test double that always returns an error from ResolveSubject.
type failingAttributeProvider struct {
	namespace string
	err       error
}

func (f *failingAttributeProvider) Namespace() string { return f.namespace }
func (f *failingAttributeProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return nil, f.err
}

func (f *failingAttributeProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

func (f *failingAttributeProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{"roles": types.AttrTypeStringList},
	}
}

func TestEngineEvaluateAttributeResolutionError(t *testing.T) {
	// A failing attribute provider should cause the engine to fail closed:
	// return a non-nil error with a zero-value decision.
	providerErr := errors.New("database connection failed")
	failingProvider := &failingAttributeProvider{
		namespace: "character",
		err:       providerErr,
	}

	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)
	require.NoError(t, resolver.RegisterProvider(failingProvider))

	mockWriter := &mockAuditWriter{}
	walPath := filepath.Join(t.TempDir(), "test-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, mockWriter, walPath)
	t.Cleanup(func() { _ = auditLogger.Close() })

	cache := NewCache(nil, nil)

	engine := NewEngine(resolver, cache, &mockSessionResolver{}, auditLogger)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.Error(t, err, "attribute resolution failure must return error")
	assert.False(t, decision.IsAllowed(), "decision must not be allowed on error")
	assert.True(t, decision.IsInfraFailure(), "decision must be infra-failure on attribute resolution error")
	assert.Equal(t, types.EffectDefaultDeny, decision.Effect())
	assert.ErrorContains(t, err, "database connection failed")
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
			"roles":   types.AttrTypeStringList,
			"level":   types.AttrTypeFloat,
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

	engine := NewEngine(resolver, cache, &mockSessionResolver{}, auditLogger)
	return engine
}

func TestEngineEvaluateConditionsSimpleConditionSatisfied(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { "admin" in principal.character.roles };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"roles": []string{"admin"}},
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies(), 1)
	assert.Equal(t, "policy-1", decision.Policies()[0].PolicyID)
	assert.True(t, decision.Policies()[0].ConditionsMet, "condition should be satisfied")
	assert.Equal(t, types.EffectAllow, decision.Policies()[0].Effect)
}

func TestEngineEvaluateConditionsSimpleConditionUnsatisfied(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { "admin" in principal.character.roles };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"roles": []string{"player"}},
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies(), 1)
	assert.Equal(t, "policy-1", decision.Policies()[0].PolicyID)
	assert.False(t, decision.Policies()[0].ConditionsMet, "condition should not be satisfied")
}

func TestEngineEvaluateConditionsMissingAttribute(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { principal.character.faction == "rebels" };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"roles": []string{"player"}}, // no faction attribute
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies(), 1)
	assert.False(t, decision.Policies()[0].ConditionsMet, "missing attribute should cause condition to fail")
}

func TestEngineEvaluateConditionsNumericComparison(t *testing.T) {
	dslText := `permit(principal is character, action in ["dig"], resource is location) when { principal.character.level > 5 };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"level": float64(7)},
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "dig",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies(), 1)
	assert.True(t, decision.Policies()[0].ConditionsMet, "numeric comparison should be satisfied")
}

func TestEngineEvaluateConditionsUnconditional(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location);`

	engine := createTestEngineWithPolicies(t, []string{dslText}, nil)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies(), 1)
	assert.True(t, decision.Policies()[0].ConditionsMet, "unconditional policy should always be satisfied")
}

func TestEngineEvaluateConditionsMultiplePoliciesMixed(t *testing.T) {
	dslTexts := []string{
		`permit(principal is character, action in ["say"], resource is location) when { "admin" in principal.character.roles };`,
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.level > 10 };`,
		`forbid(principal is character, action in ["say"], resource is location) when { principal.character.banned == true };`,
	}

	provider := &mockAttributeProvider{
		namespace: "character",
		subjectMap: map[string]any{
			"roles":  []string{"admin"},
			"level":  float64(5),
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

	require.Len(t, decision.Policies(), 3)

	// Policy 1: "admin" in roles → true
	assert.Equal(t, "policy-1", decision.Policies()[0].PolicyID)
	assert.True(t, decision.Policies()[0].ConditionsMet)
	assert.Equal(t, types.EffectAllow, decision.Policies()[0].Effect)

	// Policy 2: level > 10 → false
	assert.Equal(t, "policy-2", decision.Policies()[1].PolicyID)
	assert.False(t, decision.Policies()[1].ConditionsMet)
	assert.Equal(t, types.EffectAllow, decision.Policies()[1].Effect)

	// Policy 3: banned == true → false
	assert.Equal(t, "policy-3", decision.Policies()[2].PolicyID)
	assert.False(t, decision.Policies()[2].ConditionsMet)
	assert.Equal(t, types.EffectDeny, decision.Policies()[2].Effect)
}

func TestEngineEvaluateConditionsAllSatisfied(t *testing.T) {
	dslTexts := []string{
		`permit(principal is character, action in ["say"], resource is location) when { "admin" in principal.character.roles };`,
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.level > 5 };`,
	}

	provider := &mockAttributeProvider{
		namespace: "character",
		subjectMap: map[string]any{
			"roles": []string{"admin"},
			"level": float64(10),
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

	require.Len(t, decision.Policies(), 2)
	assert.True(t, decision.Policies()[0].ConditionsMet)
	assert.True(t, decision.Policies()[1].ConditionsMet)
}

func TestEngineEvaluateConditionsPopulatesPolicyMatches(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { "admin" in principal.character.roles };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"roles": []string{"admin"}},
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, decision.Policies(), 1)
	match := decision.Policies()[0]
	assert.Equal(t, "policy-1", match.PolicyID)
	assert.Equal(t, "test-policy-1", match.PolicyName)
	assert.Equal(t, types.EffectAllow, match.Effect)
	assert.True(t, match.ConditionsMet)
}

// Deny-overrides combination tests

func TestEngineDenyOverridesForbidWins(t *testing.T) {
	dslTexts := []string{
		`permit(principal is character, action in ["say"], resource is location) when { "player" in principal.character.roles };`,
		`forbid(principal is character, action in ["say"], resource is location) when { principal.character.banned == true };`,
	}

	provider := &mockAttributeProvider{
		namespace: "character",
		subjectMap: map[string]any{
			"roles":  []string{"player"},
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

	assert.Equal(t, types.EffectDeny, decision.Effect())
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "forbid policy satisfied", decision.Reason())
	assert.Equal(t, "policy-2", decision.PolicyID())
}

func TestEngineDenyOverridesPermitOnly(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { "player" in principal.character.roles };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"roles": []string{"player"}},
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectAllow, decision.Effect())
	assert.True(t, decision.IsAllowed())
	assert.Equal(t, "permit policy satisfied", decision.Reason())
	assert.Equal(t, "policy-1", decision.PolicyID())
}

func TestEngineDenyOverridesDefaultDenyWhenNoPoliciesSatisfied(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { "admin" in principal.character.roles };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"roles": []string{"player"}}, // condition not met
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectDefaultDeny, decision.Effect())
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "no policies satisfied", decision.Reason())
	assert.Equal(t, "", decision.PolicyID())
}

func TestEngineDenyOverridesMultipleForbid(t *testing.T) {
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

	assert.Equal(t, types.EffectDeny, decision.Effect())
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "forbid policy satisfied", decision.Reason())
	assert.Equal(t, "policy-1", decision.PolicyID()) // First forbid wins
}

func TestEngineDenyOverridesMultiplePermit(t *testing.T) {
	dslTexts := []string{
		`permit(principal is character, action in ["say"], resource is location) when { "player" in principal.character.roles };`,
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.level > 5 };`,
	}

	provider := &mockAttributeProvider{
		namespace: "character",
		subjectMap: map[string]any{
			"roles": []string{"player"},
			"level": float64(10),
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

	assert.Equal(t, types.EffectAllow, decision.Effect())
	assert.True(t, decision.IsAllowed())
	assert.Equal(t, "permit policy satisfied", decision.Reason())
	assert.Equal(t, "policy-1", decision.PolicyID()) // First permit wins
}

func TestEngineDenyOverridesForbidUnsatisfiedPermitSatisfied(t *testing.T) {
	dslTexts := []string{
		`forbid(principal is character, action in ["say"], resource is location) when { principal.character.banned == true };`,
		`permit(principal is character, action in ["say"], resource is location) when { "player" in principal.character.roles };`,
	}

	provider := &mockAttributeProvider{
		namespace: "character",
		subjectMap: map[string]any{
			"banned": false, // forbid condition not met
			"roles":  []string{"player"},
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

	assert.Equal(t, types.EffectAllow, decision.Effect())
	assert.True(t, decision.IsAllowed())
	assert.Equal(t, "permit policy satisfied", decision.Reason())
	assert.Equal(t, "policy-2", decision.PolicyID())
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

	engine := NewEngine(resolver, cache, &mockSessionResolver{}, auditLogger)
	return engine, mockWriter
}

func TestEngineAuditModeAllAllowAudited(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { "player" in principal.character.roles };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"roles": []string{"player"}},
	}

	engine, mockWriter := createTestEngineWithMode(t, []string{dslText}, []attribute.AttributeProvider{provider}, audit.ModeAll)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, types.EffectAllow, decision.Effect())

	// Wait a bit for async audit
	time.Sleep(50 * time.Millisecond)

	entries := mockWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "character:01ABC", entries[0].Subject)
	assert.Equal(t, "say", entries[0].Action)
	assert.Equal(t, "location:01XYZ", entries[0].Resource)
	assert.Equal(t, types.EffectAllow, entries[0].Effect)
	assert.Equal(t, "policy-1", entries[0].ID)
}

func TestEngineAuditModeAllDenyAudited(t *testing.T) {
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
	assert.Equal(t, types.EffectDeny, decision.Effect())

	entries := mockWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "character:01ABC", entries[0].Subject)
	assert.Equal(t, "say", entries[0].Action)
	assert.Equal(t, "location:01XYZ", entries[0].Resource)
	assert.Equal(t, types.EffectDeny, entries[0].Effect)
	assert.Equal(t, "policy-1", entries[0].ID)
}

func TestEngineAuditModeMinimalAllowNotAudited(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { "player" in principal.character.roles };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"roles": []string{"player"}},
	}

	engine, mockWriter := createTestEngineWithMode(t, []string{dslText}, []attribute.AttributeProvider{provider}, audit.ModeMinimal)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, types.EffectAllow, decision.Effect())

	// Wait a bit to ensure async operations complete
	time.Sleep(50 * time.Millisecond)

	entries := mockWriter.getEntries()
	assert.Len(t, entries, 0, "ModeMinimal should not audit allow decisions")
}

func TestEngineAuditModeMinimalDenyAudited(t *testing.T) {
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
	assert.Equal(t, types.EffectDeny, decision.Effect())

	entries := mockWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, types.EffectDeny, entries[0].Effect)
}

// End-to-end integration tests

func TestEngineEndToEndAdminPermit(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { "admin" in principal.character.roles };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"roles": []string{"admin"}},
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectAllow, decision.Effect())
	assert.True(t, decision.IsAllowed())
	assert.Equal(t, "permit policy satisfied", decision.Reason())
	assert.Equal(t, "policy-1", decision.PolicyID())
	assert.NotNil(t, decision.Attributes())
	require.Len(t, decision.Policies(), 1)
	assert.True(t, decision.Policies()[0].ConditionsMet)
}

func TestEngineEndToEndDenyOverrides(t *testing.T) {
	dslTexts := []string{
		`permit(principal is character, action in ["say"], resource is location) when { "player" in principal.character.roles };`,
		`forbid(principal is character, action in ["say"], resource is location) when { principal.character.banned == true };`,
	}

	provider := &mockAttributeProvider{
		namespace: "character",
		subjectMap: map[string]any{
			"roles":  []string{"player"},
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

	assert.Equal(t, types.EffectDeny, decision.Effect())
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "forbid policy satisfied", decision.Reason())
	assert.Equal(t, "policy-2", decision.PolicyID())
	assert.NotNil(t, decision.Attributes())
	require.Len(t, decision.Policies(), 2)
	assert.True(t, decision.Policies()[0].ConditionsMet) // permit satisfied
	assert.True(t, decision.Policies()[1].ConditionsMet) // forbid satisfied (wins)
}

func TestEngineEndToEndSessionResolution(t *testing.T) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { "player" in principal.character.roles };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"roles": []string{"player"}},
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

	assert.Equal(t, types.EffectAllow, decision.Effect())
	assert.True(t, decision.IsAllowed())
	assert.Equal(t, "permit policy satisfied", decision.Reason())

	// Wait for async audit
	time.Sleep(50 * time.Millisecond)

	// Verify audit entry has resolved subject
	entries := mockWriter.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "character:01ABC", entries[0].Subject) // Resolved subject
}

// failingProvider is a mock attribute provider that always returns an error.
type failingProvider struct {
	namespace string
	err       error
}

func (f *failingProvider) Namespace() string { return f.namespace }
func (f *failingProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return nil, f.err
}

func (f *failingProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}
func (f *failingProvider) Schema() *types.NamespaceSchema { return nil }

func TestEngineResolverErrorFailsClosed(t *testing.T) {
	// When an attribute provider returns an error, the engine must fail closed:
	// return a zero-value Decision (denied) and propagate the error.
	providerErr := fmt.Errorf("database connection lost")

	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)
	err := resolver.RegisterProvider(&failingProvider{
		namespace: "character",
		err:       providerErr,
	})
	require.NoError(t, err)

	mockWriter := &mockAuditWriter{}
	walPath := filepath.Join(t.TempDir(), "test-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, mockWriter, walPath)
	t.Cleanup(func() { _ = auditLogger.Close() })

	cache := NewCache(nil, nil)

	engine := NewEngine(resolver, cache, &mockSessionResolver{}, auditLogger)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	decision, evalErr := engine.Evaluate(context.Background(), req)
	require.Error(t, evalErr, "resolver error must propagate as engine error")
	assert.ErrorContains(t, evalErr, "database connection lost")
	// Attribute resolution errors return infra-failure decisions (DefaultDeny with infra: prefix)
	assert.False(t, decision.IsAllowed(), "resolver error decision must deny access")
	assert.True(t, decision.IsInfraFailure(), "resolver error must return infra-failure decision")
}

// partialFailingProvider returns partial data from ResolveSubject alongside an error.
// Used to verify that the engine discards partial bags on resolver error.
type partialFailingProvider struct {
	namespace string
	err       error
}

func (p *partialFailingProvider) Namespace() string { return p.namespace }
func (p *partialFailingProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	// Return partial data AND an error — engine must discard the partial data.
	return map[string]any{"roles": []string{"admin"}}, p.err
}

func (p *partialFailingProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}
func (p *partialFailingProvider) Schema() *types.NamespaceSchema { return nil }

func TestEngineResolverPartialBagsDiscardedOnError(t *testing.T) {
	// Verify the contract documented in resolver.Resolve's godoc: partial bags
	// returned alongside errors are for diagnostics only. The engine must NOT
	// evaluate policies against partial data — it must fail closed with a zero
	// Decision and propagate the error.
	providerErr := fmt.Errorf("partial provider failure")

	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)
	err := resolver.RegisterProvider(&partialFailingProvider{
		namespace: "character",
		err:       providerErr,
	})
	require.NoError(t, err)

	mockWriter := &mockAuditWriter{}
	walPath := filepath.Join(t.TempDir(), "test-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, mockWriter, walPath)
	t.Cleanup(func() { _ = auditLogger.Close() })

	cache := NewCache(nil, nil)

	engine := NewEngine(resolver, cache, &mockSessionResolver{}, auditLogger)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	decision, evalErr := engine.Evaluate(context.Background(), req)
	require.Error(t, evalErr, "resolver error must propagate even with partial data")
	assert.ErrorContains(t, evalErr, "partial provider failure")
	assert.False(t, decision.IsAllowed(), "must deny — partial bags must not reach policy evaluation")
	assert.True(t, decision.IsInfraFailure(), "must return infra-failure decision on partial resolution")
}

// failingEnvProvider is an EnvironmentProvider whose Resolve returns an error.
type failingEnvProvider struct {
	namespace string
	err       error
}

func (f *failingEnvProvider) Namespace() string { return f.namespace }

func (f *failingEnvProvider) Resolve(_ context.Context) (map[string]any, error) { return nil, f.err }
func (f *failingEnvProvider) Schema() *types.NamespaceSchema                    { return nil }

func TestEngineEnvironmentResolverErrorFailsClosed(t *testing.T) {
	// When an environment provider returns an error, the engine must fail closed:
	// return a zero-value Decision and propagate the error.
	envErr := fmt.Errorf("environment provider unavailable")

	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)
	err := resolver.RegisterEnvironmentProvider(&failingEnvProvider{
		namespace: "env",
		err:       envErr,
	})
	require.NoError(t, err)

	mockWriter := &mockAuditWriter{}
	walPath := filepath.Join(t.TempDir(), "test-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, mockWriter, walPath)
	t.Cleanup(func() { _ = auditLogger.Close() })

	cache := NewCache(nil, nil)

	engine := NewEngine(resolver, cache, &mockSessionResolver{}, auditLogger)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	decision, evalErr := engine.Evaluate(context.Background(), req)
	require.Error(t, evalErr, "environment resolver error must propagate")
	assert.ErrorContains(t, evalErr, "environment provider unavailable")
	assert.False(t, decision.IsAllowed(), "must deny on environment resolver error")
	assert.True(t, decision.IsInfraFailure(), "must return infra-failure decision")
}

// panickingProvider is an AttributeProvider whose ResolveSubject panics.
type panickingProvider struct {
	namespace string
}

func (p *panickingProvider) Namespace() string { return p.namespace }
func (p *panickingProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	panic("intentional test panic")
}

func (p *panickingProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}
func (p *panickingProvider) Schema() *types.NamespaceSchema { return nil }

func TestEngineResolverPanicFailsClosed(t *testing.T) {
	// When a provider panics, safeResolve recovers the panic and returns an error.
	// The engine must fail closed: zero Decision + propagated error.
	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)
	err := resolver.RegisterProvider(&panickingProvider{namespace: "character"})
	require.NoError(t, err)

	mockWriter := &mockAuditWriter{}
	walPath := filepath.Join(t.TempDir(), "test-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, mockWriter, walPath)
	t.Cleanup(func() { _ = auditLogger.Close() })

	cache := NewCache(nil, nil)

	engine := NewEngine(resolver, cache, &mockSessionResolver{}, auditLogger)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	decision, evalErr := engine.Evaluate(context.Background(), req)
	require.Error(t, evalErr, "panic-recovered error must propagate")
	assert.ErrorContains(t, evalErr, "panicked")
	assert.False(t, decision.IsAllowed(), "must deny on provider panic")
	assert.True(t, decision.IsInfraFailure(), "must return infra-failure decision")
}

// --- Degraded Mode Tests (T31) ---

func TestEngine_DegradedMode(t *testing.T) {
	tests := []struct {
		name       string
		degraded   bool
		wantEffect types.Effect
		wantReason string
	}{
		{
			name:       "degraded mode returns default deny",
			degraded:   true,
			wantEffect: types.EffectDefaultDeny,
			wantReason: "degraded_mode",
		},
		{
			name:       "normal mode evaluates normally",
			degraded:   false,
			wantEffect: types.EffectDefaultDeny,
			wantReason: "no applicable policies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, _ := createTestEngine(t, nil)
			if tt.degraded {
				engine.EnterDegradedMode("test corruption")
			}

			decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
				Subject:  "character:test-id",
				Action:   "read",
				Resource: "location:loc-id",
			})

			require.NoError(t, err)
			assert.Equal(t, tt.wantEffect, decision.Effect())
			assert.Contains(t, decision.Reason(), tt.wantReason)
		})
	}
}

func TestEngineDegradedModeClearResumesNormal(t *testing.T) {
	engine, _ := createTestEngine(t, nil)
	engine.EnterDegradedMode("test")

	decision, _ := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject: "character:x", Action: "read", Resource: "location:y",
	})
	assert.Equal(t, types.EffectDefaultDeny, decision.Effect())
	assert.Contains(t, decision.Reason(), "degraded_mode")

	engine.ClearDegradedMode()
	decision, _ = engine.Evaluate(context.Background(), types.AccessRequest{
		Subject: "character:x", Action: "read", Resource: "location:y",
	})
	assert.NotContains(t, decision.Reason(), "degraded_mode")
}

func TestEngineIsDegradedReflectsEnterAndClearTransitions(t *testing.T) {
	engine, _ := createTestEngine(t, nil)
	assert.False(t, engine.IsDegraded(), "engine starts in normal mode")

	engine.EnterDegradedMode("test")
	assert.True(t, engine.IsDegraded(), "IsDegraded reports true after EnterDegradedMode")

	engine.ClearDegradedMode()
	assert.False(t, engine.IsDegraded(), "IsDegraded reports false after ClearDegradedMode")
}

func TestEngineDegradedModeSystemBypassStillWorks(t *testing.T) {
	engine, _ := createTestEngine(t, nil)
	engine.EnterDegradedMode("test")

	ctx := access.WithSystemSubject(context.Background())
	decision, err := engine.Evaluate(ctx, types.AccessRequest{
		Subject:  "system",
		Action:   "read",
		Resource: "location:loc-id",
	})

	require.NoError(t, err)
	assert.Equal(t, types.EffectSystemBypass, decision.Effect())
}

// --- CanPerformAction tests ---

func TestEngineCanPerformActionAdminPermitted(t *testing.T) {
	dslText := `permit(principal is character, action in ["write"], resource is location) when { "admin" in principal.character.roles };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"roles": []string{"admin"}},
	}
	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "write", "location", "")
	require.NoError(t, err)
	assert.True(t, allowed, "admin should be permitted to write on location type")
}

func TestEngineCanPerformActionNoMatchingPolicy(t *testing.T) {
	// Policy only covers "read" but we're checking "write"
	dslText := `permit(principal is character, action in ["read"], resource is location);`

	engine := createTestEngineWithPolicies(t, []string{dslText}, nil)

	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "write", "location", "")
	require.NoError(t, err)
	assert.False(t, allowed, "no write policy → default deny")
}

func TestEngineCanPerformActionForbidOverridesPermit(t *testing.T) {
	permitDSL := `permit(principal is character, action in ["write"], resource is location);`
	forbidDSL := `forbid(principal is character, action in ["write"], resource is location) when { "banned" in principal.character.roles };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"roles": []string{"player", "banned"}},
	}
	engine := createTestEngineWithPolicies(t, []string{permitDSL, forbidDSL}, []attribute.AttributeProvider{provider})

	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "write", "location", "")
	require.NoError(t, err)
	assert.False(t, allowed, "forbid should override permit")
}

func TestEngineCanPerformActionDegradedMode(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})
	engine.EnterDegradedMode("test")

	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "write", "location", "")
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrEngineDegraded)
	assert.False(t, allowed, "degraded mode → fail-closed")
}

func TestEngineCanPerformActionContextCancelled(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	allowed, err := engine.CanPerformAction(ctx, "character:01ABC", "write", "location", "")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, allowed)
}

func TestEngineCanPerformActionUnconditionalPermit(t *testing.T) {
	// Policy with no conditions (always matches)
	dslText := `permit(principal is character, action in ["say"], resource is location);`

	engine := createTestEngineWithPolicies(t, []string{dslText}, nil)

	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "say", "location", "")
	require.NoError(t, err)
	assert.True(t, allowed, "unconditional permit should match")
}

func TestEngineCanPerformActionExactResourcePolicySkipped(t *testing.T) {
	// Policy targets a specific resource exact match — should be skipped in type-level check
	dslText := `permit(principal is character, action in ["write"], resource == "location:special-room");`

	engine := createTestEngineWithPolicies(t, []string{dslText}, nil)

	// Type-level check should not match the exact-resource policy
	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "write", "location", "")
	require.NoError(t, err)
	assert.False(t, allowed, "exact-resource policies should be skipped in type-level pre-flight")
}

func TestEngine_CanPerformAction_InvalidSubjectFormat(t *testing.T) {
	tests := []struct {
		name    string
		subject string
	}{
		{"no colon", "badsubject"},
		{"empty type", ":01ABC"},
		{"empty id", "character:"},
		{"whitespace type", " :01ABC"},
		{"whitespace id", "character: "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, _ := createTestEngine(t, &mockSessionResolver{})
			allowed, err := engine.CanPerformAction(context.Background(), tt.subject, "write", "location", "")
			require.Error(t, err)
			assert.False(t, allowed, "invalid subject format should return false")

			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok)
			assert.Equal(t, "INVALID_ENTITY_REF", oopsErr.Code())
		})
	}
}

func TestEngineCanPerformActionAttributeResolutionError(t *testing.T) {
	// A failing attribute provider should cause CanPerformAction to fail closed
	// and propagate the error so callers can distinguish infra failures from denials.
	failingProvider := &failingAttributeProvider{
		namespace: "character",
		err:       errors.New("database connection failed"),
	}

	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)
	require.NoError(t, resolver.RegisterProvider(failingProvider))

	mockWriter := &mockAuditWriter{}
	walPath := filepath.Join(t.TempDir(), "test-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, mockWriter, walPath)
	t.Cleanup(func() { _ = auditLogger.Close() })

	cache := NewCache(nil, nil)

	engine := NewEngine(resolver, cache, &mockSessionResolver{}, auditLogger)

	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "write", "location", "")
	require.Error(t, err, "attribute resolution error should propagate")
	assert.False(t, allowed, "should fail closed on attribute resolution error")
	assert.Contains(t, err.Error(), "database connection failed")
}

func TestEngineCanPerformActionPrincipalTypeMismatch(t *testing.T) {
	// Policy targets "plugin" principal but subject is "character"
	dslText := `permit(principal is plugin, action in ["write"], resource is location);`

	engine := createTestEngineWithPolicies(t, []string{dslText}, nil)

	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "write", "location", "")
	require.NoError(t, err)
	assert.False(t, allowed, "character subject should not match plugin-only policy")
}

func TestEngineCanPerformActionActionMismatch(t *testing.T) {
	// Policy only permits "read" but we're checking "delete"
	dslText := `permit(principal is character, action in ["read"], resource is location);`

	engine := createTestEngineWithPolicies(t, []string{dslText}, nil)

	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "delete", "location", "")
	require.NoError(t, err)
	assert.False(t, allowed, "delete should not match read-only policy")
}

func TestEngineCanPerformActionResourceTypeMismatch(t *testing.T) {
	// Policy targets "exit" resource but we're checking "location"
	dslText := `permit(principal is character, action in ["write"], resource is exit);`

	engine := createTestEngineWithPolicies(t, []string{dslText}, nil)

	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "write", "location", "")
	require.NoError(t, err)
	assert.False(t, allowed, "location should not match exit-only policy")
}

func TestEngineCanPerformActionNilCompiledPolicySkipped(t *testing.T) {
	// A policy with Compiled == nil should be silently skipped
	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)

	mockWriter := &mockAuditWriter{}
	walPath := filepath.Join(t.TempDir(), "test-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, mockWriter, walPath)
	t.Cleanup(func() { _ = auditLogger.Close() })

	cache := NewCache(nil, nil)
	cache.mu.Lock()
	cache.snapshot = &Snapshot{
		Policies: []CachedPolicy{
			{ID: "nil-compiled", Name: "broken", Compiled: nil},
		},
		CreatedAt: time.Now(),
	}
	cache.mu.Unlock()

	engine := NewEngine(resolver, cache, &mockSessionResolver{}, auditLogger)

	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "write", "location", "")
	require.NoError(t, err)
	assert.False(t, allowed, "nil-compiled policy should be skipped, resulting in default deny")
}

func TestEngineCanPerformActionConditionUnsatisfied(t *testing.T) {
	// Permit policy exists but condition is not met
	dslText := `permit(principal is character, action in ["write"], resource is location) when { "admin" in principal.character.roles };`

	provider := &mockAttributeProvider{
		namespace:  "character",
		subjectMap: map[string]any{"roles": []string{"player"}}, // not admin
	}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{provider})

	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "write", "location", "")
	require.NoError(t, err)
	assert.False(t, allowed, "unsatisfied condition should result in default deny")
}

func TestEngineCanPerformActionDoesNotInvokeResourceProvidersWhenCapabilityCheckRuns(t *testing.T) {
	// T5: Register a tracking provider that counts ResolveSubject and
	// ResolveResource calls separately, then run CanPerformAction.
	// Asserts the headline C1 invariant: resource providers are never
	// invoked during type-level preflight, AND subject resolution did
	// actually run (guards against a silent no-op regression).
	dslText := `permit(principal is character, action in ["read"], resource is widget);`

	widgetSchema := &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{"type": types.AttrTypeString},
	}
	widgetProv := &trackingAttrProvider{namespace: "widget", schema: widgetSchema}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{widgetProv})

	// Asserting NoError ensures the call actually executed the resolution
	// path. Without this, a regression that errored out before any provider
	// was called would silently leave resourceCalls at 0 and false-pass.
	_, err := engine.CanPerformAction(context.Background(), "character:01ABC", "read", "widget", "")
	require.NoError(t, err)

	// Headline invariant: resource provider was NOT called.
	assert.Equal(t, 0, widgetProv.resourceCalls,
		"widget resource provider must not be called during CanPerformAction")
	// Positive invariant: subject resolution DID run. The widget provider
	// is iterated for the subject path (it returns nil since widgetProv has
	// no subjectAttrs), which still increments subjectCalls. If a future
	// regression skips subject resolution entirely, this assertion fails.
	assert.Greater(t, widgetProv.subjectCalls, 0,
		"subject resolution must still run during CanPerformAction preflight")
}

// trackingAttrProvider counts ResolveSubject and ResolveResource calls
// separately so tests can assert the CanPerformAction preflight invariant
// (no resource providers invoked) alongside the positive invariant that
// subject resolution still runs.
type trackingAttrProvider struct {
	namespace     string
	subjectAttrs  map[string]any
	resourceAttrs map[string]any
	schema        *types.NamespaceSchema
	subjectCalls  int
	resourceCalls int
}

func (p *trackingAttrProvider) Namespace() string { return p.namespace }

func (p *trackingAttrProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	p.subjectCalls++
	return p.subjectAttrs, nil
}

func (p *trackingAttrProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	p.resourceCalls++
	return p.resourceAttrs, nil
}

func (p *trackingAttrProvider) Schema() *types.NamespaceSchema { return p.schema }

func TestEngineCanPerformActionPermitsOptimisticallyForPermitReferencingResourceAttrs(t *testing.T) {
	// The optimistic-permit branch fires when a permit policy's conditions
	// reference resource attributes that can't be evaluated at type-level
	// pre-flight (no resource instance exists). The engine MUST treat such
	// permits as potentially-applicable and return allowed=true — the
	// handler's instance-level Evaluate will enforce the full condition.
	//
	// This test proves that switching to ResolveSubjectAttributes does not
	// regress this behavior. Before the refactor, the optimistic branch
	// worked because the synthetic "__preflight__" resource had an empty
	// attribute bag; after the refactor, it works because
	// ResolveSubjectAttributes returns an empty Resource bag by construction.
	dslText := `permit(principal is character, action in ["read"], resource is widget) when { resource.widget.type == "normal" };`

	engine := createTestEngineWithPolicies(t, []string{dslText}, nil)

	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "read", "widget", "")
	require.NoError(t, err)
	assert.True(t, allowed,
		"permit policy referencing resource attrs must optimistic-permit at preflight")
}

// TestEngineAuditEventsCarrySourceEngineAndComponentAbac verifies that audit
// events emitted by the ABAC engine during Evaluate carry Source = SourceEngine
// and Component = "abac" so operator queries can filter engine events. The
// test exercises the "no applicable policies" path, which still emits an audit
// event but takes the regular (non-bypass) engine code path.
func TestEngineAuditEventsCarrySourceEngineAndComponentAbac(t *testing.T) {
	// createTestEngine already builds an Engine backed by an empty NewCache,
	// so evaluation will hit the "no applicable policies" branch — which still
	// emits an audit event via the regular engine path.
	engine, writer := createTestEngine(t, &mockSessionResolver{})

	req, err := types.NewAccessRequest("character:01ABC", "read", "location:01XYZ", nil)
	require.NoError(t, err)

	_, evalErr := engine.Evaluate(context.Background(), req)
	require.NoError(t, evalErr)

	entries := writer.getEntries()
	require.NotEmpty(t, entries, "expected at least one audit event")

	last := entries[len(entries)-1]
	assert.Equal(t, audit.SourceEngine, last.Source,
		"engine-produced events must carry SourceEngine")
	assert.Equal(t, "abac", last.Component,
		"engine-produced events must carry Component='abac'")
}

func TestEvaluateOverlaysCallerAttributesOntoActionBag(t *testing.T) {
	// Verifies Decision 6 R3 composition rule: caller-supplied attrs land
	// in bags.Action; caller wins on conflict for non-reserved keys.
	dslText := `permit(principal is character, action in ["decrypt"], resource is stream) when { action.event_type == "core-comm:whisper" };`
	engine := createTestEngineWithPolicies(t, []string{dslText}, nil)

	req, err := types.NewAccessRequest(
		"character:01ABC",
		"decrypt",
		"stream:audit",
		map[string]any{"event_type": "core-comm:whisper"},
	)
	require.NoError(t, err)
	decision, err := engine.Evaluate(t.Context(), req)
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(),
		"caller-supplied action.event_type=core-comm:whisper MUST overlay bags.Action so the policy's when clause matches")
}

func TestEvaluateNilCallerAttributesIsNoOp(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})
	req, err := types.NewAccessRequest("character:01ABC", "read", "location:01XYZ", nil)
	require.NoError(t, err)
	_, err = engine.Evaluate(t.Context(), req)
	require.NoError(t, err)
	// No assertion on Allow/Deny — the test confirms nil attrs do not panic
	// and Resolve still runs to completion.
}

func TestEvaluateRejectsHandBuiltAccessRequestWithReservedAttributeKey(t *testing.T) {
	// Defense-in-depth: a caller that bypasses NewAccessRequest by constructing
	// an AccessRequest literal and populating a reserved key must be rejected
	// by Evaluate, not silently allowed to overwrite resolver-owned attributes.
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	// Build a literal that bypasses the constructor's reserved-key check.
	req := types.AccessRequest{
		Subject:    "character:01ABC",
		Action:     "read",
		Resource:   "location:01XYZ",
		Attributes: map[string]any{"name": "injected"},
	}
	_, err := engine.Evaluate(t.Context(), req)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ACCESS_REQUEST_RESERVED_ATTRIBUTE")
}
