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
	syncWrites  []Event
	asyncWrites []Event
	failSync    bool
	failAsync   bool
	closed      bool
}

func (m *mockWriter) WriteSync(_ context.Context, event Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSync {
		return assert.AnError
	}
	m.syncWrites = append(m.syncWrites, event)
	return nil
}

func (m *mockWriter) WriteAsync(event Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failAsync {
		return assert.AnError
	}
	m.asyncWrites = append(m.asyncWrites, event)
	return nil
}

func (m *mockWriter) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockWriter) getSyncWrites() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Event{}, m.syncWrites...)
}

func (m *mockWriter) getAsyncWrites() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Event{}, m.asyncWrites...)
}

func (m *mockWriter) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func TestAuditLoggerMinimalModeAllowNotLogged(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeMinimal, writer, "")
	defer logger.Close()

	entry := Event{
		Subject:    "character:01ABC",
		Action:     "read",
		Resource:   "location:01XYZ",
		Effect:     types.EffectAllow,
		ID:         "policy-123",
		Name:       "allow-read",
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

func TestAuditLoggerMinimalModeDenyLoggedSync(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeMinimal, writer, "")
	defer logger.Close()

	entry := Event{
		Subject:    "character:01ABC",
		Action:     "delete",
		Resource:   "location:01XYZ",
		Effect:     types.EffectDeny,
		ID:         "policy-456",
		Name:       "deny-delete",
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

func TestAuditLoggerMinimalModeSystemBypassLoggedSync(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeMinimal, writer, "")
	defer logger.Close()

	entry := Event{
		Subject:    "system",
		Action:     "write",
		Resource:   "property:01DEF",
		Effect:     types.EffectSystemBypass,
		ID:         "system-bypass",
		Name:       "system-bypass",
		Attributes: map[string]any{},
		DurationUS: 50,
		Timestamp:  time.Now(),
	}

	err := logger.Log(context.Background(), entry)
	require.NoError(t, err)

	// System bypasses are elevated-privilege events — always logged, even in minimal mode
	syncWrites := writer.getSyncWrites()
	require.Len(t, syncWrites, 1)
	assert.Equal(t, entry.Subject, syncWrites[0].Subject)
	assert.Equal(t, entry.Effect, syncWrites[0].Effect)
	assert.Empty(t, writer.getAsyncWrites())
}

func TestAuditLoggerDenialsOnlyModeAllowNotLogged(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeDenialsOnly, writer, "")
	defer logger.Close()

	entry := Event{
		Subject:    "character:01ABC",
		Action:     "read",
		Resource:   "location:01XYZ",
		Effect:     types.EffectAllow,
		ID:         "policy-123",
		Name:       "allow-read",
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

func TestAuditLoggerDenialsOnlyModeDenyLoggedSync(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeDenialsOnly, writer, "")
	defer logger.Close()

	entry := Event{
		Subject:    "character:01ABC",
		Action:     "delete",
		Resource:   "location:01XYZ",
		Effect:     types.EffectDeny,
		ID:         "policy-456",
		Name:       "deny-delete",
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

func TestAuditLoggerAllModeAllowLoggedAsync(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeAll, writer, "")
	defer logger.Close()

	entry := Event{
		Subject:    "character:01ABC",
		Action:     "read",
		Resource:   "location:01XYZ",
		Effect:     types.EffectAllow,
		ID:         "policy-123",
		Name:       "allow-read",
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

func TestAuditLoggerAllModeDenyLoggedSync(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeAll, writer, "")
	defer logger.Close()

	entry := Event{
		Subject:    "character:01ABC",
		Action:     "delete",
		Resource:   "location:01XYZ",
		Effect:     types.EffectDeny,
		ID:         "policy-456",
		Name:       "deny-delete",
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

func TestAuditLoggerSyncWriteFailureFallsBackToWAL(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "audit-wal.jsonl")

	writer := &mockWriter{failSync: true}
	logger := NewLogger(ModeMinimal, writer, walPath)
	defer logger.Close()

	entry := Event{
		Subject:    "character:01ABC",
		Action:     "delete",
		Resource:   "location:01XYZ",
		Effect:     types.EffectDeny,
		ID:         "policy-456",
		Name:       "deny-delete",
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

func TestAuditLoggerReplayWAL(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "audit-wal.jsonl")

	// Create initial logger and write entries to WAL
	writer1 := &mockWriter{failSync: true}
	logger1 := NewLogger(ModeMinimal, writer1, walPath)

	entry1 := Event{
		Subject:    "character:01ABC",
		Action:     "delete",
		Resource:   "location:01XYZ",
		Effect:     types.EffectDeny,
		ID:         "policy-1",
		Name:       "deny-1",
		Attributes: map[string]any{},
		DurationUS: 100,
		Timestamp:  time.Now(),
	}

	entry2 := Event{
		Subject:    "character:01DEF",
		Action:     "write",
		Resource:   "property:01GHI",
		Effect:     types.EffectDeny,
		ID:         "policy-2",
		Name:       "deny-2",
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
	assert.Equal(t, "policy-1", syncWrites[0].ID)
	assert.Equal(t, "policy-2", syncWrites[1].ID)

	// WAL should be empty after replay
	data, err := os.ReadFile(walPath)
	require.NoError(t, err)
	assert.Empty(t, data)
}

func TestAuditLoggerBothDBAndWALFailEventDropped(t *testing.T) {
	tmpDir := t.TempDir()
	// Create invalid WAL path (directory instead of file)
	walPath := filepath.Join(tmpDir, "invalid-dir")
	err := os.Mkdir(walPath, 0o700)
	require.NoError(t, err)

	writer := &mockWriter{failSync: true}
	logger := NewLogger(ModeMinimal, writer, walPath)
	defer logger.Close()

	entry := Event{
		Subject:    "character:01ABC",
		Action:     "delete",
		Resource:   "location:01XYZ",
		Effect:     types.EffectDeny,
		ID:         "policy-456",
		Name:       "deny-delete",
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

func TestAuditLoggerGracefulShutdownFlushesBuffered(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeAll, writer, "")

	// Queue multiple async entries
	for i := 0; i < 5; i++ {
		entry := Event{
			Subject:    "character:01ABC",
			Action:     "read",
			Resource:   "location:01XYZ",
			Effect:     types.EffectAllow,
			ID:         "policy-123",
			Name:       "allow-read",
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

func TestAuditLoggerEventContainsAllFields(t *testing.T) {
	writer := &mockWriter{}
	logger := NewLogger(ModeAll, writer, "")
	defer logger.Close()

	now := time.Now()
	entry := Event{
		Subject:  "character:01ABC",
		Action:   "write",
		Resource: "property:01DEF",
		Effect:   types.EffectAllow,
		ID:       "policy-789",
		Name:     "allow-write-property",
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
	assert.Equal(t, "policy-789", logged.ID)
	assert.Equal(t, "allow-write-property", logged.Name)
	assert.Equal(t, int64(250), logged.DurationUS)
	assert.Equal(t, now, logged.Timestamp)
	assert.NotNil(t, logged.Attributes)
	assert.Equal(t, "builder", logged.Attributes["role"])
}

// selectiveFailWriter fails WriteSync for specific event IDs.
type selectiveFailWriter struct {
	mu           sync.Mutex
	syncWrites   []Event
	failEventIDs map[string]bool
}

func (s *selectiveFailWriter) WriteSync(_ context.Context, event Event) error {
	if s.failEventIDs[event.ID] {
		return fmt.Errorf("write failed for event %s", event.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncWrites = append(s.syncWrites, event)
	return nil
}

func (s *selectiveFailWriter) WriteAsync(_ Event) error { return nil }
func (s *selectiveFailWriter) Close() error             { return nil }

func (s *selectiveFailWriter) getSyncWrites() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event{}, s.syncWrites...)
}

func TestAuditLoggerReplayWALPartialFailure(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "audit-wal.jsonl")

	// Write 3 entries to WAL (using a writer that always fails sync)
	writer1 := &mockWriter{failSync: true}
	logger1 := NewLogger(ModeMinimal, writer1, walPath)

	events := []Event{
		{Subject: "character:1", Action: "read", Resource: "loc:1", Effect: types.EffectDeny, ID: "policy-ok-1", Name: "p1", Attributes: map[string]any{}, DurationUS: 100, Timestamp: time.Now()},
		{Subject: "character:2", Action: "write", Resource: "loc:2", Effect: types.EffectDeny, ID: "policy-fail", Name: "p2", Attributes: map[string]any{}, DurationUS: 200, Timestamp: time.Now()},
		{Subject: "character:3", Action: "delete", Resource: "loc:3", Effect: types.EffectDeny, ID: "policy-ok-2", Name: "p3", Attributes: map[string]any{}, DurationUS: 300, Timestamp: time.Now()},
	}
	for _, e := range events {
		logger1.Log(context.Background(), e)
	}
	logger1.Close()

	// Replay with a writer that fails only for "policy-fail"
	writer2 := &selectiveFailWriter{failEventIDs: map[string]bool{"policy-fail": true}}
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

	// Verify the failed event's ID
	var walEvent Event
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &walEvent))
	assert.Equal(t, "policy-fail", walEvent.ID)

	// Verify the 2 successful entries were written
	syncWrites := writer2.getSyncWrites()
	assert.Len(t, syncWrites, 2)
}

func TestAuditLoggerReplayWALAllFail(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "audit-wal.jsonl")

	// Write 2 entries to WAL
	writer1 := &mockWriter{failSync: true}
	logger1 := NewLogger(ModeMinimal, writer1, walPath)

	for i := range 2 {
		logger1.Log(context.Background(), Event{
			Subject: fmt.Sprintf("character:%d", i), Action: "read", Resource: "loc:1",
			Effect: types.EffectDeny, ID: fmt.Sprintf("policy-%d", i),
			Name: "p", Attributes: map[string]any{}, DurationUS: 100, Timestamp: time.Now(),
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

func TestPartialReplayErrorFormat(t *testing.T) {
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

func TestRecordEngineAuditFailureIncrementsCounter(t *testing.T) {
	before := promtestutil.ToFloat64(engineAuditFailuresCounter)

	RecordEngineAuditFailure()
	RecordEngineAuditFailure()

	after := promtestutil.ToFloat64(engineAuditFailuresCounter)
	assert.Equal(t, before+2, after, "counter should increment by 2")
}

func TestEventHasSourceComponentMessageFields(t *testing.T) {
	event := Event{
		Subject:   "character:01ABC",
		Action:    "speak",
		Resource:  "channel:01XYZ",
		Effect:    types.EffectDeny,
		ID:        "not_member",
		Name:      "channels: not a member",
		Message:   "player not in channel members",
		Source:    SourcePlugin,
		Component: "core-channels",
	}
	assert.Equal(t, "character:01ABC", event.Subject)
	assert.Equal(t, "speak", event.Action)
	assert.Equal(t, "channel:01XYZ", event.Resource)
	assert.Equal(t, types.EffectDeny, event.Effect)
	assert.Equal(t, "not_member", event.ID)
	assert.Equal(t, "channels: not a member", event.Name)
	assert.Equal(t, "player not in channel members", event.Message)
	assert.Equal(t, SourcePlugin, event.Source)
	assert.Equal(t, "core-channels", event.Component)
}
