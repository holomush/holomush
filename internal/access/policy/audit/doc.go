// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package audit provides audit logging for ABAC access control decisions.
//
// # Overview
//
// The audit package implements configurable audit logging for access control
// decisions with sync/async writes and WAL (Write-Ahead Log) fallback for
// resilience. It supports three logging modes and provides PostgreSQL storage.
//
// # Audit Modes
//
//   - ModeMinimal: Logs denials, default denials, and system bypasses (sync)
//   - ModeDenialsOnly: Logs all denials and system bypasses (sync)
//   - ModeAll: Logs everything - denials sync, allows async
//
// # Architecture
//
// The Logger routes entries based on effect and mode:
//
//	deny, default_deny, system_bypass → sync write → WAL fallback on failure
//	allow (in ModeAll only) → async write via buffered channel
//
// PostgresWriter implements batched async writes with periodic flushing.
//
// # Resilience
//
// When sync writes fail, entries are written to a WAL file at
// $XDG_STATE_HOME/holomush/audit-wal.jsonl. The ReplayWAL method can be
// used to recover entries after outages.
//
// # Metrics
//
//   - abac_audit_channel_full_total: Channel overflow counter
//   - abac_audit_failures_total{reason}: Failure counter by reason
//   - abac_audit_wal_entries: Current WAL entry count
//
// # Example Usage
//
//	db, _ := sql.Open("postgres", connString)
//	writer := audit.NewPostgresWriter(db)
//	logger := audit.NewLogger(audit.ModeAll, writer, "")
//	defer logger.Close()
//
//	// Log a decision
//	entry := audit.Entry{
//	    Subject:    "character:01ABC",
//	    Action:     "read",
//	    Resource:   "location:01XYZ",
//	    Effect:     types.EffectAllow,
//	    PolicyID:   "policy-123",
//	    PolicyName: "allow-read",
//	    Attributes: map[string]any{"role": "player"},
//	    DurationUS: 150,
//	    Timestamp:  time.Now(),
//	}
//	logger.Log(ctx, entry)
//
//	// Replay WAL after recovery
//	logger.ReplayWAL(ctx)
package audit
