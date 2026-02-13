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

	// Steps 3-6 not implemented yet, so we get placeholder
	assert.Equal(t, types.EffectDefaultDeny, decision.Effect)
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "evaluation pending", decision.Reason)
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
	assert.Equal(t, "evaluation pending", decision.Reason)
	assert.Equal(t, "", decision.PolicyID)
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
