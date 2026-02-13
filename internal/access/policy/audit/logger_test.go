// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockWriter records all writes for verification
type mockWriter struct {
	mu          sync.Mutex
	syncWrites  []Entry
	asyncWrites []Entry
	failSync    bool
	failAsync   bool
	closed      bool
}

func (m *mockWriter) WriteSync(_ context.Context, entry Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSync {
		return assert.AnError
	}
	m.syncWrites = append(m.syncWrites, entry)
	return nil
}

func (m *mockWriter) WriteAsync(entry Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failAsync {
		return assert.AnError
	}
	m.asyncWrites = append(m.asyncWrites, entry)
	return nil
}

func (m *mockWriter) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockWriter) getSyncWrites() []Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Entry{}, m.syncWrites...)
}

func (m *mockWriter) getAsyncWrites() []Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Entry{}, m.asyncWrites...)
}

func (m *mockWriter) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func TestAuditLogger_MinimalMode_Allow_NotLogged(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeMinimal, writer, "")
	defer logger.Close()

	entry := Entry{
		Subject:    "character:01ABC",
		Action:     "read",
		Resource:   "location:01XYZ",
		Effect:     types.EffectAllow,
		PolicyID:   "policy-123",
		PolicyName: "allow-read",
		Attributes: map[string]any{"role": "player"},
		DurationUS: 100,
		Timestamp:  time.Now(),
	}

	err := logger.Log(context.Background(), entry)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond) // allow async processing
	assert.Empty(t, writer.getSyncWrites())
	assert.Empty(t, writer.getAsyncWrites())
}

func TestAuditLogger_MinimalMode_Deny_LoggedSync(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeMinimal, writer, "")
	defer logger.Close()

	entry := Entry{
		Subject:    "character:01ABC",
		Action:     "delete",
		Resource:   "location:01XYZ",
		Effect:     types.EffectDeny,
		PolicyID:   "policy-456",
		PolicyName: "deny-delete",
		Attributes: map[string]any{"role": "player"},
		DurationUS: 200,
		Timestamp:  time.Now(),
	}

	err := logger.Log(context.Background(), entry)
	require.NoError(t, err)

	syncWrites := writer.getSyncWrites()
	require.Len(t, syncWrites, 1)
	assert.Equal(t, entry.Subject, syncWrites[0].Subject)
	assert.Equal(t, entry.Effect, syncWrites[0].Effect)
	assert.Empty(t, writer.getAsyncWrites())
}

func TestAuditLogger_MinimalMode_SystemBypass_LoggedSync(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeMinimal, writer, "")
	defer logger.Close()

	entry := Entry{
		Subject:    "system",
		Action:     "write",
		Resource:   "property:01DEF",
		Effect:     types.EffectSystemBypass,
		PolicyID:   "system-bypass",
		PolicyName: "system-bypass",
		Attributes: map[string]any{},
		DurationUS: 50,
		Timestamp:  time.Now(),
	}

	err := logger.Log(context.Background(), entry)
	require.NoError(t, err)

	syncWrites := writer.getSyncWrites()
	require.Len(t, syncWrites, 1)
	assert.Equal(t, entry.Effect, syncWrites[0].Effect)
}

func TestAuditLogger_DenialsOnlyMode_Allow_NotLogged(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeDenialsOnly, writer, "")
	defer logger.Close()

	entry := Entry{
		Subject:    "character:01ABC",
		Action:     "read",
		Resource:   "location:01XYZ",
		Effect:     types.EffectAllow,
		PolicyID:   "policy-123",
		PolicyName: "allow-read",
		Attributes: map[string]any{},
		DurationUS: 100,
		Timestamp:  time.Now(),
	}

	err := logger.Log(context.Background(), entry)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, writer.getSyncWrites())
	assert.Empty(t, writer.getAsyncWrites())
}

func TestAuditLogger_DenialsOnlyMode_Deny_LoggedSync(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeDenialsOnly, writer, "")
	defer logger.Close()

	entry := Entry{
		Subject:    "character:01ABC",
		Action:     "delete",
		Resource:   "location:01XYZ",
		Effect:     types.EffectDeny,
		PolicyID:   "policy-456",
		PolicyName: "deny-delete",
		Attributes: map[string]any{},
		DurationUS: 200,
		Timestamp:  time.Now(),
	}

	err := logger.Log(context.Background(), entry)
	require.NoError(t, err)

	syncWrites := writer.getSyncWrites()
	require.Len(t, syncWrites, 1)
	assert.Equal(t, entry.Effect, syncWrites[0].Effect)
}

func TestAuditLogger_AllMode_Allow_LoggedAsync(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeAll, writer, "")
	defer logger.Close()

	entry := Entry{
		Subject:    "character:01ABC",
		Action:     "read",
		Resource:   "location:01XYZ",
		Effect:     types.EffectAllow,
		PolicyID:   "policy-123",
		PolicyName: "allow-read",
		Attributes: map[string]any{},
		DurationUS: 100,
		Timestamp:  time.Now(),
	}

	err := logger.Log(context.Background(), entry)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond) // allow async processing
	asyncWrites := writer.getAsyncWrites()
	require.Len(t, asyncWrites, 1)
	assert.Equal(t, entry.Effect, asyncWrites[0].Effect)
	assert.Empty(t, writer.getSyncWrites())
}

func TestAuditLogger_AllMode_Deny_LoggedSync(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeAll, writer, "")
	defer logger.Close()

	entry := Entry{
		Subject:    "character:01ABC",
		Action:     "delete",
		Resource:   "location:01XYZ",
		Effect:     types.EffectDeny,
		PolicyID:   "policy-456",
		PolicyName: "deny-delete",
		Attributes: map[string]any{},
		DurationUS: 200,
		Timestamp:  time.Now(),
	}

	err := logger.Log(context.Background(), entry)
	require.NoError(t, err)

	syncWrites := writer.getSyncWrites()
	require.Len(t, syncWrites, 1)
	assert.Equal(t, entry.Effect, syncWrites[0].Effect)
	assert.Empty(t, writer.getAsyncWrites())
}

func TestAuditLogger_SyncWriteFailure_WALFallback(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "audit-wal.jsonl")

	writer := &mockWriter{failSync: true}
	logger := NewLogger(ModeMinimal, writer, walPath)
	defer logger.Close()

	entry := Entry{
		Subject:    "character:01ABC",
		Action:     "delete",
		Resource:   "location:01XYZ",
		Effect:     types.EffectDeny,
		PolicyID:   "policy-456",
		PolicyName: "deny-delete",
		Attributes: map[string]any{"role": "player"},
		DurationUS: 200,
		Timestamp:  time.Now(),
	}

	err := logger.Log(context.Background(), entry)
	require.NoError(t, err) // WAL fallback should succeed

	// Verify WAL file exists and contains entry
	data, err := os.ReadFile(walPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "character:01ABC")
	assert.Contains(t, string(data), "deny-delete")
}

func TestAuditLogger_ReplayWAL(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "audit-wal.jsonl")

	// Create initial logger and write entries to WAL
	writer1 := &mockWriter{failSync: true}
	logger1 := NewLogger(ModeMinimal, writer1, walPath)

	entry1 := Entry{
		Subject:    "character:01ABC",
		Action:     "delete",
		Resource:   "location:01XYZ",
		Effect:     types.EffectDeny,
		PolicyID:   "policy-1",
		PolicyName: "deny-1",
		Attributes: map[string]any{},
		DurationUS: 100,
		Timestamp:  time.Now(),
	}

	entry2 := Entry{
		Subject:    "character:01DEF",
		Action:     "write",
		Resource:   "property:01GHI",
		Effect:     types.EffectDeny,
		PolicyID:   "policy-2",
		PolicyName: "deny-2",
		Attributes: map[string]any{},
		DurationUS: 150,
		Timestamp:  time.Now(),
	}

	logger1.Log(context.Background(), entry1)
	logger1.Log(context.Background(), entry2)
	logger1.Close()

	// Create new logger with working writer and replay
	writer2 := &mockWriter{}
	logger2 := NewLogger(ModeMinimal, writer2, walPath)
	defer logger2.Close()

	err := logger2.ReplayWAL(context.Background())
	require.NoError(t, err)

	syncWrites := writer2.getSyncWrites()
	require.Len(t, syncWrites, 2)
	assert.Equal(t, "policy-1", syncWrites[0].PolicyID)
	assert.Equal(t, "policy-2", syncWrites[1].PolicyID)

	// WAL should be empty after replay
	data, err := os.ReadFile(walPath)
	require.NoError(t, err)
	assert.Empty(t, data)
}

func TestAuditLogger_BothDBAndWALFail_EntryDropped(t *testing.T) {
	tmpDir := t.TempDir()
	// Create invalid WAL path (directory instead of file)
	walPath := filepath.Join(tmpDir, "invalid-dir")
	err := os.Mkdir(walPath, 0o700)
	require.NoError(t, err)

	writer := &mockWriter{failSync: true}
	logger := NewLogger(ModeMinimal, writer, walPath)
	defer logger.Close()

	entry := Entry{
		Subject:    "character:01ABC",
		Action:     "delete",
		Resource:   "location:01XYZ",
		Effect:     types.EffectDeny,
		PolicyID:   "policy-456",
		PolicyName: "deny-delete",
		Attributes: map[string]any{},
		DurationUS: 200,
		Timestamp:  time.Now(),
	}

	err = logger.Log(context.Background(), entry)
	// Should not error, but entry is dropped and metric incremented
	require.NoError(t, err)
}

func TestAuditLogger_GracefulShutdown_FlushesBuffered(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeAll, writer, "")

	// Queue multiple async entries
	for i := 0; i < 5; i++ {
		entry := Entry{
			Subject:    "character:01ABC",
			Action:     "read",
			Resource:   "location:01XYZ",
			Effect:     types.EffectAllow,
			PolicyID:   "policy-123",
			PolicyName: "allow-read",
			Attributes: map[string]any{},
			DurationUS: int64(100 + i),
			Timestamp:  time.Now(),
		}
		logger.Log(context.Background(), entry)
	}

	// Close should flush all buffered entries
	err := logger.Close()
	require.NoError(t, err)

	asyncWrites := writer.getAsyncWrites()
	assert.Len(t, asyncWrites, 5)
	assert.True(t, writer.isClosed())
}

func TestAuditLogger_EntryContainsAllFields(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeAll, writer, "")
	defer logger.Close()

	now := time.Now()
	entry := Entry{
		Subject:    "character:01ABC",
		Action:     "write",
		Resource:   "property:01DEF",
		Effect:     types.EffectAllow,
		PolicyID:   "policy-789",
		PolicyName: "allow-write-property",
		Attributes: map[string]any{
			"role":        "builder",
			"permissions": []string{"write", "read"},
		},
		DurationUS: 250,
		Timestamp:  now,
	}

	err := logger.Log(context.Background(), entry)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	asyncWrites := writer.getAsyncWrites()
	require.Len(t, asyncWrites, 1)

	logged := asyncWrites[0]
	assert.Equal(t, "character:01ABC", logged.Subject)
	assert.Equal(t, "write", logged.Action)
	assert.Equal(t, "property:01DEF", logged.Resource)
	assert.Equal(t, types.EffectAllow, logged.Effect)
	assert.Equal(t, "policy-789", logged.PolicyID)
	assert.Equal(t, "allow-write-property", logged.PolicyName)
	assert.Equal(t, int64(250), logged.DurationUS)
	assert.Equal(t, now, logged.Timestamp)
	assert.NotNil(t, logged.Attributes)
	assert.Equal(t, "builder", logged.Attributes["role"])
}
