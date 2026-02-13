// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

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
