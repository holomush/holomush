// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/pkg/errutil"
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

func TestAuditLogger_MinimalMode_SystemBypass_NotLogged(t *testing.T) {
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

	assert.Empty(t, writer.getSyncWrites())
	assert.Empty(t, writer.getAsyncWrites())
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
	// Should return error when both DB and WAL fail (critical failure)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_WRITE_FAILED")

	// Verify entry is truly dropped: WAL path is a directory (not a file),
	// so nothing should be durably stored there.
	entries, readErr := os.ReadDir(walPath)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "WAL directory should be empty — entry must be dropped when both DB and WAL fail")

	// Verify DB writer received no successful writes
	assert.Empty(t, writer.getSyncWrites(), "DB writer should have no successful sync writes")
	assert.Empty(t, writer.getAsyncWrites(), "DB writer should have no successful async writes")
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

// selectiveFailWriter fails WriteSync for specific PolicyIDs.
type selectiveFailWriter struct {
	mu            sync.Mutex
	syncWrites    []Entry
	failPolicyIDs map[string]bool
}

func (s *selectiveFailWriter) WriteSync(_ context.Context, entry Entry) error {
	if s.failPolicyIDs[entry.PolicyID] {
		return fmt.Errorf("write failed for policy %s", entry.PolicyID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncWrites = append(s.syncWrites, entry)
	return nil
}

func (s *selectiveFailWriter) WriteAsync(_ Entry) error { return nil }
func (s *selectiveFailWriter) Close() error             { return nil }

func (s *selectiveFailWriter) getSyncWrites() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Entry{}, s.syncWrites...)
}

func TestAuditLogger_ReplayWAL_PartialFailure(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "audit-wal.jsonl")

	// Write 3 entries to WAL (using a writer that always fails sync)
	writer1 := &mockWriter{failSync: true}
	logger1 := NewLogger(ModeMinimal, writer1, walPath)

	entries := []Entry{
		{Subject: "character:1", Action: "read", Resource: "loc:1", Effect: types.EffectDeny, PolicyID: "policy-ok-1", PolicyName: "p1", Attributes: map[string]any{}, DurationUS: 100, Timestamp: time.Now()},
		{Subject: "character:2", Action: "write", Resource: "loc:2", Effect: types.EffectDeny, PolicyID: "policy-fail", PolicyName: "p2", Attributes: map[string]any{}, DurationUS: 200, Timestamp: time.Now()},
		{Subject: "character:3", Action: "delete", Resource: "loc:3", Effect: types.EffectDeny, PolicyID: "policy-ok-2", PolicyName: "p3", Attributes: map[string]any{}, DurationUS: 300, Timestamp: time.Now()},
	}
	for _, e := range entries {
		logger1.Log(context.Background(), e)
	}
	logger1.Close()

	// Replay with a writer that fails only for "policy-fail"
	writer2 := &selectiveFailWriter{failPolicyIDs: map[string]bool{"policy-fail": true}}
	logger2 := NewLogger(ModeMinimal, writer2, walPath)
	defer logger2.Close()

	err := logger2.ReplayWAL(context.Background())
	require.Error(t, err)

	var partialErr *PartialReplayError
	require.True(t, errors.As(err, &partialErr), "error must be *PartialReplayError")
	assert.Equal(t, 1, partialErr.FailedCount)
	assert.Equal(t, 2, partialErr.ReplayedCount)
	assert.Equal(t, 3, partialErr.TotalCount)

	// WAL should contain exactly 1 line (the failed entry)
	data, err := os.ReadFile(walPath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 1, "WAL should contain only the failed entry")

	// Verify the failed entry's PolicyID
	var walEntry Entry
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &walEntry))
	assert.Equal(t, "policy-fail", walEntry.PolicyID)

	// Verify the 2 successful entries were written
	syncWrites := writer2.getSyncWrites()
	assert.Len(t, syncWrites, 2)
}

func TestAuditLogger_ReplayWAL_AllFail(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "audit-wal.jsonl")

	// Write 2 entries to WAL
	writer1 := &mockWriter{failSync: true}
	logger1 := NewLogger(ModeMinimal, writer1, walPath)

	for i := range 2 {
		logger1.Log(context.Background(), Entry{
			Subject: fmt.Sprintf("character:%d", i), Action: "read", Resource: "loc:1",
			Effect: types.EffectDeny, PolicyID: fmt.Sprintf("policy-%d", i),
			PolicyName: "p", Attributes: map[string]any{}, DurationUS: 100, Timestamp: time.Now(),
		})
	}
	logger1.Close()

	// Replay with a writer that fails everything
	writer2 := &mockWriter{failSync: true}
	logger2 := NewLogger(ModeMinimal, writer2, walPath)
	defer logger2.Close()

	err := logger2.ReplayWAL(context.Background())
	require.Error(t, err)

	var partialErr *PartialReplayError
	require.True(t, errors.As(err, &partialErr), "error must be *PartialReplayError")
	assert.Equal(t, 2, partialErr.FailedCount)
	assert.Equal(t, 0, partialErr.ReplayedCount)

	// WAL should still contain both entries
	data, err := os.ReadFile(walPath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	assert.Len(t, lines, 2, "WAL should contain all failed entries")
}

func TestPartialReplayError_ErrorFormat(t *testing.T) {
	err := &PartialReplayError{
		FailedCount:   3,
		ReplayedCount: 7,
		TotalCount:    10,
	}

	msg := err.Error()
	assert.Contains(t, msg, "partially failed", "Error() should contain 'partially failed'")
	assert.Contains(t, msg, "3", "Error() should contain failed count")
	assert.Contains(t, msg, "10", "Error() should contain total count")
}

func TestRecordEngineAuditFailure_IncrementsCounter(t *testing.T) {
	before := promtestutil.ToFloat64(engineAuditFailuresCounter)

	RecordEngineAuditFailure()
	RecordEngineAuditFailure()

	after := promtestutil.ToFloat64(engineAuditFailuresCounter)
	assert.Equal(t, before+2, after, "counter should increment by 2")
}
