// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package audit provides audit logging for ABAC access control decisions.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/xdg"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/samber/oops"
)

// Mode controls which decisions are logged.
type Mode string

// Audit logging modes.
const (
	ModeMinimal     Mode = "minimal"      // Logs denials, default denials, and system bypasses (sync)
	ModeDenialsOnly Mode = "denials_only" // Logs all denials, default denials, and system bypasses (sync)
	ModeAll         Mode = "all"          // everything
)

// Entry represents a single access control decision to be logged.
type Entry struct {
	Subject    string         `json:"subject"`
	Action     string         `json:"action"`
	Resource   string         `json:"resource"`
	Effect     types.Effect   `json:"effect"`
	PolicyID   string         `json:"policy_id"`
	PolicyName string         `json:"policy_name"`
	Attributes map[string]any `json:"attributes"`
	DurationUS int64          `json:"duration_us"`
	Timestamp  time.Time      `json:"timestamp"`
}

// Writer is the interface for writing audit entries to a backend.
type Writer interface {
	WriteSync(ctx context.Context, entry Entry) error
	WriteAsync(entry Entry) error
	Close() error
}

var (
	channelFullCounter = promauto.NewCounter(prometheus.CounterOpts{
		Name: "abac_audit_channel_full_total",
		Help: "Total number of times async audit channel was full",
	})

	failuresCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "abac_audit_failures_total",
		Help: "Total number of audit logging failures",
	}, []string{"reason"})

	// engineAuditFailuresCounter tracks audit logging failures at the engine
	// level (as opposed to writer-level failures tracked by failuresCounter).
	// Exported via RecordEngineAuditFailure for use by the policy engine.
	engineAuditFailuresCounter = promauto.NewCounter(prometheus.CounterOpts{
		Name: "abac_audit_engine_failures_total",
		Help: "Total number of audit logging failures at the engine evaluation level",
	})

	walEntriesGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "abac_audit_wal_entries",
		Help: "Current number of entries in the WAL",
	})
)

// RecordEngineAuditFailure increments the engine-level audit failure counter.
// Call this when audit.Log returns an error in the policy engine to give ops
// teams an alertable metric for audit gaps.
func RecordEngineAuditFailure() {
	engineAuditFailuresCounter.Inc()
}

// Logger routes audit entries based on mode and effect.
type Logger struct {
	mode      Mode
	writer    Writer
	walPath   string
	walFile   *os.File
	walMu     sync.Mutex
	asyncChan chan Entry
	stopChan  chan struct{}
	wg        sync.WaitGroup
}

// NewLogger creates a Logger with the given mode, writer, and WAL path.
// If walPath is empty, a default path in the XDG state directory will be used.
func NewLogger(mode Mode, writer Writer, walPath string) *Logger {
	if walPath == "" {
		stateDir, err := xdg.StateDir()
		if err != nil {
			slog.Error("failed to get state directory for WAL", "error", err)
			walPath = "/tmp/holomush-audit-wal.jsonl"
		} else {
			if err := xdg.EnsureDir(stateDir); err != nil {
				slog.Error("failed to ensure state directory", "error", err)
			}
			walPath = filepath.Join(stateDir, "audit-wal.jsonl")
		}
	}

	logger := &Logger{
		mode:      mode,
		writer:    writer,
		walPath:   walPath,
		asyncChan: make(chan Entry, 1000), // buffered channel
		stopChan:  make(chan struct{}),
	}

	// Start async consumer goroutine
	logger.wg.Add(1)
	go logger.asyncConsumer()

	return logger
}

// Log routes an audit entry based on the configured mode and effect.
func (l *Logger) Log(ctx context.Context, entry Entry) error {
	// Determine if entry should be logged based on mode and effect
	shouldLog, useSync := l.shouldLog(entry.Effect)
	if !shouldLog {
		return nil
	}

	if useSync {
		// Synchronous write for denials, default_deny, system_bypass
		if err := l.writer.WriteSync(ctx, entry); err != nil {
			// Fallback to WAL
			if walErr := l.writeToWAL(entry); walErr != nil {
				// Both failed - log error and return it to the caller
				slog.Error("audit write failed: both DB and WAL failed",
					"db_error", err,
					"wal_error", walErr,
					"subject", entry.Subject,
					"action", entry.Action,
					"resource", entry.Resource,
					"effect", entry.Effect,
				)
				failuresCounter.WithLabelValues("wal_failed").Inc()
				return oops.Code("AUDIT_WRITE_FAILED").
					With("db_error", err).
					With("wal_error", walErr).
					With("subject", entry.Subject).
					With("action", entry.Action).
					With("resource", entry.Resource).
					Errorf("audit write failed: both DB and WAL failed")
			}
			// WAL succeeded but primary DB failed — log degraded state
			slog.Warn("audit DB write failed, fell back to WAL",
				"db_error", err,
				"subject", entry.Subject,
				"action", entry.Action,
				"resource", entry.Resource,
				"effect", entry.Effect,
			)
			failuresCounter.WithLabelValues("db_failed_wal_ok").Inc()
		}
		return nil
	}

	// Async write for allows in all mode
	select {
	case l.asyncChan <- entry:
		return nil
	default:
		// Channel full - drop entry, increment metric, and return error so
		// engine callers can track audit loss via RecordEngineAuditFailure.
		channelFullCounter.Inc()
		slog.Warn("audit channel full: dropping async entry",
			"subject", entry.Subject,
			"action", entry.Action,
			"resource", entry.Resource,
			"channel_len", len(l.asyncChan),
		)
		return oops.Code("AUDIT_CHANNEL_FULL").
			With("subject", entry.Subject).
			With("action", entry.Action).
			With("resource", entry.Resource).
			Errorf("audit channel full: entry dropped")
	}
}

// shouldLog determines if an entry should be logged based on mode and effect.
// Returns (shouldLog bool, useSync bool).
func (l *Logger) shouldLog(effect types.Effect) (shouldLog, useSync bool) {
	switch l.mode {
	case ModeMinimal:
		// Log: deny, default_deny only (no system_bypass — minimal mode)
		switch effect {
		case types.EffectDeny, types.EffectDefaultDeny:
			shouldLog, useSync = true, true
		default:
			shouldLog, useSync = false, false
		}

	case ModeDenialsOnly:
		// Log: deny, default_deny, system_bypass (all sync)
		switch effect {
		case types.EffectDeny, types.EffectDefaultDeny, types.EffectSystemBypass:
			shouldLog, useSync = true, true
		default:
			shouldLog, useSync = false, false
		}

	case ModeAll:
		// Log everything: denials sync, allows async
		switch effect {
		case types.EffectDeny, types.EffectDefaultDeny, types.EffectSystemBypass:
			shouldLog, useSync = true, true
		case types.EffectAllow:
			shouldLog, useSync = true, false
		default:
			shouldLog, useSync = false, false
		}

	default:
		shouldLog, useSync = false, false
	}

	return shouldLog, useSync
}

// asyncConsumer processes async writes from the channel.
func (l *Logger) asyncConsumer() {
	defer l.wg.Done()

	for {
		select {
		case entry := <-l.asyncChan:
			if err := l.writer.WriteAsync(entry); err != nil {
				slog.Error("async audit write failed",
					"error", err,
					"subject", entry.Subject,
					"action", entry.Action,
				)
				failuresCounter.WithLabelValues("async_write_failed").Inc()
			}
		case <-l.stopChan:
			// Drain remaining entries
			l.drainAsync()
			return
		}
	}
}

// drainAsync processes all remaining entries in the channel.
func (l *Logger) drainAsync() {
	for {
		select {
		case entry := <-l.asyncChan:
			if err := l.writer.WriteAsync(entry); err != nil {
				slog.Error("async audit write failed during drain",
					"error", err,
					"subject", entry.Subject,
				)
				failuresCounter.WithLabelValues("async_write_failed").Inc()
			}
		default:
			return
		}
	}
}

// writeToWAL writes an entry to the write-ahead log.
func (l *Logger) writeToWAL(entry Entry) error {
	l.walMu.Lock()
	defer l.walMu.Unlock()

	// Open WAL file if not already open
	if l.walFile == nil {
		file, err := os.OpenFile(l.walPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY|os.O_SYNC, 0o600)
		if err != nil {
			return oops.With("path", l.walPath).Wrap(err)
		}
		l.walFile = file
	}

	// Write JSON entry
	data, err := json.Marshal(entry)
	if err != nil {
		return oops.Wrap(err)
	}

	if _, err := fmt.Fprintf(l.walFile, "%s\n", data); err != nil {
		return oops.Wrap(err)
	}

	walEntriesGauge.Inc()
	return nil
}

// PartialReplayError indicates that some WAL entries were successfully replayed
// but others failed. The WAL has been atomically rewritten to contain only the
// failed entries, so retrying ReplayWAL is safe.
type PartialReplayError struct {
	FailedCount   int
	TotalCount    int
	ReplayedCount int
}

func (e *PartialReplayError) Error() string {
	return fmt.Sprintf("WAL replay partially failed: %d of %d entries could not be written", e.FailedCount, e.TotalCount)
}

// ReplayWAL reads all entries from the WAL and writes them to the writer.
// On full success, truncates the WAL file. On partial failure, rewrites the
// WAL with only the entries that failed so they can be retried next time.
// A *PartialReplayError return means the WAL was safely rewritten and retry is safe.
func (l *Logger) ReplayWAL(ctx context.Context) error {
	l.walMu.Lock()
	defer l.walMu.Unlock()

	// Check if WAL exists
	if _, err := os.Stat(l.walPath); os.IsNotExist(err) {
		return nil // No WAL to replay
	}

	// Read WAL file
	data, err := os.ReadFile(l.walPath)
	if err != nil {
		return oops.With("path", l.walPath).Wrap(err)
	}

	if len(data) == 0 {
		return nil // Empty WAL
	}

	// Parse and replay entries; collect failed entries for WAL rewrite.
	replayed := 0
	var failedLines []string
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}

		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			slog.Error("failed to unmarshal WAL entry", "error", err, "line", line)
			failuresCounter.WithLabelValues("wal_unmarshal_failed").Inc()
			// Keep the raw line so it is preserved for manual inspection.
			failedLines = append(failedLines, line)
			continue
		}

		if err := l.writer.WriteSync(ctx, entry); err != nil {
			slog.Error("failed to replay WAL entry", "error", err, "entry", entry)
			failuresCounter.WithLabelValues("wal_replay_failed").Inc()
			// Re-marshal so the entry is preserved in the WAL for retry.
			if raw, merr := json.Marshal(entry); merr == nil {
				failedLines = append(failedLines, string(raw))
			} else {
				// Fall back to the original line if re-marshal fails.
				failedLines = append(failedLines, line)
			}
			continue
		}
		replayed++
	}

	if len(failedLines) > 0 {
		// Rewrite WAL atomically with only the failed entries.
		tmpPath := l.walPath + ".tmp"
		var buf []byte
		for _, fl := range failedLines {
			buf = append(buf, []byte(fl+"\n")...)
		}
		if werr := os.WriteFile(tmpPath, buf, 0o600); werr != nil {
			return oops.With("path", tmpPath).Wrap(werr)
		}
		if rerr := os.Rename(tmpPath, l.walPath); rerr != nil {
			return oops.With("path", l.walPath).Wrap(rerr)
		}
		walEntriesGauge.Set(float64(len(failedLines)))
		slog.Warn("partially replayed WAL entries; WAL rewritten with failed entries",
			"replayed", replayed, "failed", len(failedLines))
		return &PartialReplayError{FailedCount: len(failedLines), TotalCount: replayed + len(failedLines), ReplayedCount: replayed}
	}

	// All entries replayed — truncate the WAL.
	if err := os.Truncate(l.walPath, 0); err != nil {
		return oops.With("path", l.walPath).Wrap(err)
	}

	walEntriesGauge.Set(0)
	slog.Info("replayed WAL entries", "count", replayed)
	return nil
}

// Close gracefully shuts down the logger.
func (l *Logger) Close() error {
	// Signal stop
	close(l.stopChan)

	// Wait for async consumer to drain
	l.wg.Wait()

	// Close writer
	if err := l.writer.Close(); err != nil {
		return oops.Wrap(err)
	}

	// Close WAL file
	l.walMu.Lock()
	defer l.walMu.Unlock()
	if l.walFile != nil {
		if err := l.walFile.Close(); err != nil {
			return oops.Wrap(err)
		}
		l.walFile = nil
	}

	return nil
}

// splitLines splits a string by newlines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
