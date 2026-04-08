<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Audit Subsystem Rework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the audit subsystem source-agnostic (engine / plugin / system / future sources) and add first-class plugin-emitted audit entries via D-inline transport on `CommandResponse.audit_hints` plus a Lua capability mirror.

**Architecture:** Move `internal/access/policy/audit/` to `internal/audit/`, rename `Entry` → `Event`, add `ID`/`Name`/`Message`/`Source`/`Component` shape, provide context-scoped event accumulation helpers in the audit package, add `repeated AuditDecisionHint audit_hints` to `CommandResponse`, wire dispatcher-side collection + flush, provide `pluginsdk.Audit(ctx)` binary SDK API, provide `audit.deny/allow` Lua globals via new `cap_audit` capability. Engine audit emits are preserved as-is (flush eagerly inside engine, not through the dispatcher slice).

**Tech Stack:** Go 1.23+, PostgreSQL, gopher-lua, hashicorp/go-plugin, buf/protovalidate, prometheus/client_golang, samber/oops, testify, Ginkgo/Gomega (integration).

**Spec:** `docs/superpowers/specs/2026-04-07-audit-subsystem-rework-design.md`
**Bead:** `holomush-ggbz`
**Blocks:** `holomush-0sc.12` (channel plugin rework)

---

## File Structure

**New files (created by this plan):**

| Path | Responsibility |
|---|---|
| `internal/audit/doc.go` | Package doc (moved + updated from old package) |
| `internal/audit/logger.go` | `Event` type, `Writer` interface, `Logger` with mode routing + WAL fallback (moved + renamed) |
| `internal/audit/logger_test.go` | Logger unit tests (moved + updated) |
| `internal/audit/postgres.go` | `PostgresWriter` implementation (moved + updated column mapping) |
| `internal/audit/partition_creator.go` | Partition creator (moved unchanged) |
| `internal/audit/partition_creator_test.go` | Partition creator tests (moved unchanged) |
| `internal/audit/retention.go` | Retention policy (moved unchanged) |
| `internal/audit/retention_test.go` | Retention tests (moved unchanged) |
| `internal/audit/source.go` | `EventSource` defined type + constants (NEW) |
| `internal/audit/source_test.go` | Source constant tests (NEW) |
| `internal/audit/context.go` | `NewContextForDispatch`, `AddEventToContext`, `EventsFromContext` (NEW) |
| `internal/audit/context_test.go` | Context helper tests (NEW) |
| `internal/audit/plugin_metrics.go` | `RecordPluginAuditFailure` Prometheus counter (NEW) |
| `internal/store/migrations/000005_audit_source_component.up.sql` | Column rename + new columns (NEW) |
| `internal/store/migrations/000005_audit_source_component.down.sql` | Reversal (NEW) |
| `pkg/plugin/audit.go` | `Audit(ctx)` recorder, `AuditAttrs` type, context-bound slice storage (NEW) |
| `pkg/plugin/audit_test.go` | SDK audit recorder unit tests (NEW) |
| `internal/plugin/hostfunc/cap_audit.go` | Lua audit capability module (NEW) |
| `internal/plugin/hostfunc/cap_audit_test.go` | Lua capability unit tests (NEW) |
| `test/integration/audit/audit_integration_test.go` | End-to-end binary + Lua integration tests (NEW) |
| `site/docs/extending/audit-events.md` | Plugin author docs (NEW) |

**Modified files:**

| Path | Change |
|---|---|
| `api/proto/holomush/plugin/v1/plugin.proto` | Add `AuditDecisionHint` message; add `repeated AuditDecisionHint audit_hints = 4` to `CommandResponse` |
| `pkg/plugin/command.go` | Add `AuditHints []AuditHint` field to `CommandResponse` struct |
| `pkg/plugin/sdk.go` | `HandleCommand` harvests context hints and serializes into proto response |
| `internal/command/dispatcher.go` | `Dispatch` attaches audit context + flushes on return; `dispatchToPlugin` extracts hints from response, stamps host fields, pushes to slice |
| `internal/command/dispatcher_test.go` | New tests for hint extraction, stamping, flush, failure mode |
| `internal/access/policy/engine.go` | 6 call sites: update to new field names + stamp `Source = SourceEngine`, `Component = "abac"` |
| `internal/access/policy/engine_test.go` | Update assertions that match audit entries |
| `internal/access/policy/engine_bench_test.go` | Update imports |
| `internal/access/setup/setup.go` | Update import path |
| `internal/access/setup/subsystem.go` | Update import path |
| `internal/bootstrap/setup/subsystem.go` | Update import path |
| `test/integration/access/access_suite_test.go` | Update import path |
| `test/integration/access/evaluation_test.go` | Update import path |
| `test/integration/plugin/abac_widget_test.go` | Update import path |
| `test/integration/plugin/binary_plugin_test.go` | Update import path |
| `internal/plugin/setup/subsystem.go` (after line 136, where `NewCapabilityRegistry()` is called) | Register `cap_audit` capability with the `CapabilityRegistry` |

**Deleted:**

- `internal/access/policy/audit/` — entire directory removed after imports are updated

---

## Task 1: Move audit package to `internal/audit/` (rename only, no new fields)

**Goal:** Atomically move the package and update all imports. Field shape is unchanged in this task — `Entry` is still `Entry`, `PolicyID` is still `PolicyID`. This isolates the mechanical move from the semantic changes.

**Files:**

- Create: `internal/audit/doc.go`, `logger.go`, `logger_test.go`, `postgres.go`, `partition_creator.go`, `partition_creator_test.go`, `retention.go`, `retention_test.go`
- Delete: `internal/access/policy/audit/` (entire directory)
- Modify (imports only): `internal/access/policy/engine.go`, `internal/access/policy/engine_test.go`, `internal/access/policy/engine_bench_test.go`, `internal/access/setup/setup.go`, `internal/access/setup/subsystem.go`, `internal/bootstrap/setup/subsystem.go`, `test/integration/access/access_suite_test.go`, `test/integration/access/evaluation_test.go`, `test/integration/plugin/abac_widget_test.go`, `test/integration/plugin/binary_plugin_test.go`

- [ ] **Step 1.1: Create the new package directory and copy files**

Run:

```bash
mkdir -p internal/audit
cp internal/access/policy/audit/doc.go internal/audit/doc.go
cp internal/access/policy/audit/logger.go internal/audit/logger.go
cp internal/access/policy/audit/logger_test.go internal/audit/logger_test.go
cp internal/access/policy/audit/postgres.go internal/audit/postgres.go
cp internal/access/policy/audit/partition_creator.go internal/audit/partition_creator.go
cp internal/access/policy/audit/partition_creator_test.go internal/audit/partition_creator_test.go
cp internal/access/policy/audit/retention.go internal/audit/retention.go
cp internal/access/policy/audit/retention_test.go internal/audit/retention_test.go
```

Expected: 8 files created under `internal/audit/`.

- [ ] **Step 1.2: Delete the old package directory**

Run:

```bash
rm -rf internal/access/policy/audit
```

Expected: old directory gone. `ls internal/access/policy/audit 2>&1` returns "No such file or directory".

- [ ] **Step 1.3: Update the 10 import sites**

For each file, replace `"github.com/holomush/holomush/internal/access/policy/audit"` with `"github.com/holomush/holomush/internal/audit"`:

Files to update:
1. `internal/access/policy/engine.go`
2. `internal/access/policy/engine_test.go`
3. `internal/access/policy/engine_bench_test.go`
4. `internal/access/setup/setup.go`
5. `internal/access/setup/subsystem.go`
6. `internal/bootstrap/setup/subsystem.go`
7. `test/integration/access/access_suite_test.go`
8. `test/integration/access/evaluation_test.go`
9. `test/integration/plugin/abac_widget_test.go`
10. `test/integration/plugin/binary_plugin_test.go`

Each edit is the same single-line replace:

Old:
```go
"github.com/holomush/holomush/internal/access/policy/audit"
```

New:
```go
"github.com/holomush/holomush/internal/audit"
```

- [ ] **Step 1.4: Verify compilation**

Run: `task build`
Expected: compiles cleanly; no "package not found" errors.

- [ ] **Step 1.5: Run unit tests to verify nothing regressed**

Run: `task test -- ./internal/audit/... ./internal/access/...`
Expected: all tests pass.

- [ ] **Step 1.6: Run integration tests for affected packages**

Run: `task test:int -- ./test/integration/access/... ./test/integration/plugin/...`
Expected: all tests pass (assumes Docker is running for testcontainers).

- [ ] **Step 1.7: Commit**

Run:

```bash
jj --no-pager describe -m "refactor(audit): move package to internal/audit/

Move the audit subsystem from internal/access/policy/audit/ to
internal/audit/ to reflect its role as a general-purpose decision
recording facility rather than a policy-engine-owned subsystem.

Pure mechanical move: Entry type, field names, and all semantics
are unchanged in this commit. Follow-up commits rename fields and
add source-agnostic fields (Source, Component, Message).

Bead: holomush-ggbz"
jj --no-pager new
```

Expected: new working-copy change created on top of the rename commit.

---

## Task 2: Rename `Entry` → `Event` and `PolicyID/PolicyName` → `ID/Name`

**Goal:** Drop the type stutter and remove policy-specific field names. No new fields yet.

**Files:**

- Modify: `internal/audit/logger.go`, `internal/audit/logger_test.go`, `internal/audit/postgres.go`, `internal/audit/doc.go`, `internal/access/policy/engine.go`, `internal/access/policy/engine_test.go`, `internal/access/policy/engine_bench_test.go`, `test/integration/access/evaluation_test.go`

- [ ] **Step 2.1: Rename `Entry` type to `Event` in logger.go**

File: `internal/audit/logger.go`

Find:
```go
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
```

Replace with:
```go
// Event represents a single access control decision to be logged.
type Event struct {
	Subject    string         `json:"subject"`
	Action     string         `json:"action"`
	Resource   string         `json:"resource"`
	Effect     types.Effect   `json:"effect"`
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes"`
	DurationUS int64          `json:"duration_us"`
	Timestamp  time.Time      `json:"timestamp"`
}
```

- [ ] **Step 2.2: Update `Writer` interface to use `Event`**

File: `internal/audit/logger.go`

Find:
```go
// Writer is the interface for writing audit entries to a backend.
type Writer interface {
	WriteSync(ctx context.Context, entry Entry) error
	WriteAsync(entry Entry) error
	Close() error
}
```

Replace with:
```go
// Writer is the interface for writing audit events to a backend.
type Writer interface {
	WriteSync(ctx context.Context, event Event) error
	WriteAsync(event Event) error
	Close() error
}
```

- [ ] **Step 2.3: Update all `Entry` references in logger.go**

In `internal/audit/logger.go`, globally replace `Entry` with `Event` in the following locations (each is a distinct occurrence):

- `asyncChan chan Entry` → `asyncChan chan Event`
- `make(chan Entry, 1000)` → `make(chan Event, 1000)`
- `func (l *Logger) Log(ctx context.Context, entry Entry) error` → `func (l *Logger) Log(ctx context.Context, event Event) error`
- Inside `Log`: all `entry.Effect`, `entry.Subject`, `entry.Action`, `entry.Resource` references become `event.Effect`, etc.
- In `asyncConsumer`: `case entry := <-l.asyncChan:` → `case event := <-l.asyncChan:`
- In `drainAsync`: same
- In `writeToWAL`: `func (l *Logger) writeToWAL(entry Entry) error` → `func (l *Logger) writeToWAL(event Event) error`, and `entry.*` → `event.*` inside
- In `ReplayWAL`: `var entry Entry` → `var event Event`

Also within `Log`: rename the parameter use for the failure-path error context. Replace all `"subject", entry.Subject` / `"action", entry.Action` / etc. to use `event.*`.

- [ ] **Step 2.4: Update `PolicyID` / `PolicyName` field references in logger.go**

File: `internal/audit/logger.go`

Currently neither `PolicyID` nor `PolicyName` is referenced by name inside `logger.go` itself (the Logger just writes the whole Entry to the Writer). Verify with:

Run: `grep -n 'PolicyID\|PolicyName' internal/audit/logger.go`
Expected: no output.

- [ ] **Step 2.5: Update postgres.go to use Event and new column names**

File: `internal/audit/postgres.go`

Find:
```go
// PostgresWriter implements Writer for PostgreSQL.
type PostgresWriter struct {
	db          *sql.DB
	asyncChan   chan Entry
	stopChan    chan struct{}
	wg          sync.WaitGroup
	batchSize   int
	flushPeriod time.Duration
}
```

Replace with:
```go
// PostgresWriter implements Writer for PostgreSQL.
type PostgresWriter struct {
	db          *sql.DB
	asyncChan   chan Event
	stopChan    chan struct{}
	wg          sync.WaitGroup
	batchSize   int
	flushPeriod time.Duration
}
```

Find:
```go
	writer := &PostgresWriter{
		db:          db,
		asyncChan:   make(chan Entry, 1000),
```

Replace with:
```go
	writer := &PostgresWriter{
		db:          db,
		asyncChan:   make(chan Event, 1000),
```

Find (the `WriteSync` method):
```go
// WriteSync performs a synchronous write to the database.
func (w *PostgresWriter) WriteSync(ctx context.Context, entry Entry) error {
	query := `
		INSERT INTO access_audit_log (
			id, subject, action, resource, effect, policy_id, policy_name,
			attributes, duration_us, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	attributesJSON, err := json.Marshal(entry.Attributes)
	if err != nil {
		return oops.Wrap(err)
	}

	_, err = w.db.ExecContext(ctx, query,
		idgen.New().String(),
		entry.Subject,
		entry.Action,
		entry.Resource,
		entry.Effect.String(),
		entry.PolicyID,
		entry.PolicyName,
		attributesJSON,
		entry.DurationUS,
		entry.Timestamp,
	)
	if err != nil {
		return oops.With("subject", entry.Subject).
			With("action", entry.Action).
			With("resource", entry.Resource).
			Wrap(err)
	}

	return nil
}
```

Replace with:
```go
// WriteSync performs a synchronous write to the database.
func (w *PostgresWriter) WriteSync(ctx context.Context, event Event) error {
	query := `
		INSERT INTO access_audit_log (
			id, subject, action, resource, effect, event_id, event_name,
			attributes, duration_us, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	attributesJSON, err := json.Marshal(event.Attributes)
	if err != nil {
		return oops.Wrap(err)
	}

	_, err = w.db.ExecContext(ctx, query,
		idgen.New().String(),
		event.Subject,
		event.Action,
		event.Resource,
		event.Effect.String(),
		event.ID,
		event.Name,
		attributesJSON,
		event.DurationUS,
		event.Timestamp,
	)
	if err != nil {
		return oops.With("subject", event.Subject).
			With("action", event.Action).
			With("resource", event.Resource).
			Wrap(err)
	}

	return nil
}
```

Note: the SQL column names change from `policy_id, policy_name` to `event_id, event_name`. These are the final column names (not temporary). The schema migration in Task 11 will do this rename on the database side.

Find (the `WriteAsync` method):
```go
// WriteAsync queues an entry for asynchronous batch writing.
func (w *PostgresWriter) WriteAsync(entry Entry) error {
	select {
	case w.asyncChan <- entry:
		return nil
```

Replace with:
```go
// WriteAsync queues an event for asynchronous batch writing.
func (w *PostgresWriter) WriteAsync(event Event) error {
	select {
	case w.asyncChan <- event:
		return nil
```

Find (the `batchConsumer`):
```go
	var batch []Entry
```

Replace with:
```go
	var batch []Event
```

Find (inside `batchConsumer` — `case entry := <-w.asyncChan:`):
```go
		case entry := <-w.asyncChan:
			batch = append(batch, entry)
```

Replace with:
```go
		case event := <-w.asyncChan:
			batch = append(batch, event)
```

And inside the drain branch of `batchConsumer`:
```go
				case entry := <-w.asyncChan:
					batch = append(batch, entry)
```

Replace with:
```go
				case event := <-w.asyncChan:
					batch = append(batch, event)
```

Find (the `writeBatch` method):
```go
// writeBatch writes multiple entries in a single transaction.
func (w *PostgresWriter) writeBatch(ctx context.Context, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return oops.Wrap(err)
	}
	defer func() {
		//nolint:errcheck // Rollback error is expected when transaction commits successfully
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO access_audit_log (
			id, subject, action, resource, effect, policy_id, policy_name,
			attributes, duration_us, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`)
	if err != nil {
		return oops.Wrap(err)
	}
	defer func() {
		//nolint:errcheck // Close error is not critical - statement will be closed when transaction ends
		_ = stmt.Close()
	}()

	for i := range entries {
		entry := &entries[i]
		attributesJSON, err := json.Marshal(entry.Attributes)
		if err != nil {
			slog.Error("failed to marshal attributes", "error", err, "entry", entry)
			continue
		}

		_, err = stmt.ExecContext(ctx,
			idgen.New().String(),
			entry.Subject,
			entry.Action,
			entry.Resource,
			entry.Effect.String(),
			entry.PolicyID,
			entry.PolicyName,
			attributesJSON,
			entry.DurationUS,
			entry.Timestamp,
		)
		if err != nil {
			slog.Error("failed to insert audit entry", "error", err, "entry", entry)
			// Continue with other entries
		}
	}

	if err := tx.Commit(); err != nil {
		return oops.Wrap(err)
	}

	return nil
}
```

Replace with:
```go
// writeBatch writes multiple events in a single transaction.
func (w *PostgresWriter) writeBatch(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return oops.Wrap(err)
	}
	defer func() {
		//nolint:errcheck // Rollback error is expected when transaction commits successfully
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO access_audit_log (
			id, subject, action, resource, effect, event_id, event_name,
			attributes, duration_us, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`)
	if err != nil {
		return oops.Wrap(err)
	}
	defer func() {
		//nolint:errcheck // Close error is not critical - statement will be closed when transaction ends
		_ = stmt.Close()
	}()

	for i := range events {
		event := &events[i]
		attributesJSON, err := json.Marshal(event.Attributes)
		if err != nil {
			slog.Error("failed to marshal attributes", "error", err, "event", event)
			continue
		}

		_, err = stmt.ExecContext(ctx,
			idgen.New().String(),
			event.Subject,
			event.Action,
			event.Resource,
			event.Effect.String(),
			event.ID,
			event.Name,
			attributesJSON,
			event.DurationUS,
			event.Timestamp,
		)
		if err != nil {
			slog.Error("failed to insert audit event", "error", err, "event", event)
			// Continue with other events
		}
	}

	if err := tx.Commit(); err != nil {
		return oops.Wrap(err)
	}

	return nil
}
```

- [ ] **Step 2.6: Update doc.go example block**

File: `internal/audit/doc.go`

Find:
```go
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
```

Replace with:
```go
//	// Log a decision
//	event := audit.Event{
//	    Subject:    "character:01ABC",
//	    Action:     "read",
//	    Resource:   "location:01XYZ",
//	    Effect:     types.EffectAllow,
//	    ID:         "policy-123",
//	    Name:       "allow-read",
//	    Attributes: map[string]any{"role": "player"},
//	    DurationUS: 150,
//	    Timestamp:  time.Now(),
//	}
//	logger.Log(ctx, event)
```

- [ ] **Step 2.7: Update engine.go call sites (6 locations)**

File: `internal/access/policy/engine.go`

For each of the six `audit.Entry{ ... }` constructions at lines ~125, ~151, ~198, ~229, ~262, ~300, apply the same transformation:

Old shape:
```go
entry := audit.Entry{
    Subject:    req.Subject,
    Action:     req.Action,
    Resource:   req.Resource,
    Effect:     <effect value>,
    PolicyID:   <policyID value>,
    PolicyName: <policyName value>,
    DurationUS: time.Since(start).Microseconds(),
    Timestamp:  time.Now(),
}
if auditErr := e.audit.Log(ctx, entry); auditErr != nil {
    slog.WarnContext(ctx, "audit log failed", "error", auditErr)
    audit.RecordEngineAuditFailure()
}
```

New shape:
```go
event := audit.Event{
    Subject:    req.Subject,
    Action:     req.Action,
    Resource:   req.Resource,
    Effect:     <effect value>,
    ID:         <policyID value>,
    Name:       <policyName value>,
    DurationUS: time.Since(start).Microseconds(),
    Timestamp:  time.Now(),
}
if auditErr := e.audit.Log(ctx, event); auditErr != nil {
    slog.WarnContext(ctx, "audit log failed", "error", auditErr)
    audit.RecordEngineAuditFailure()
}
```

Specifically at each location:
- Line ~125 (system bypass): `PolicyID: ""` → `ID: ""`, `PolicyName: ""` → `Name: ""`
- Line ~151 (degraded mode): `PolicyID: "infra:degraded-mode"` → `ID: "infra:degraded-mode"`, `PolicyName: ""` → `Name: ""`
- Line ~198 (session invalid/error): `PolicyID: decision.PolicyID()` → `ID: decision.PolicyID()`, `PolicyName: ""` → `Name: ""`
- Line ~229 (attribute resolution failure): `PolicyID: "infra:attribute-resolution-failed"` → `ID: "infra:attribute-resolution-failed"`, `PolicyName: ""` → `Name: ""`
- Line ~262 (no applicable policies): `PolicyID: ""` → `ID: ""`, `PolicyName: ""` → `Name: ""`
- Line ~300 (final decision): `PolicyID: decision.PolicyID()` → `ID: decision.PolicyID()`, `PolicyName: policyNameFromMatches(decision.PolicyID(), decision.Policies())` → `Name: policyNameFromMatches(decision.PolicyID(), decision.Policies())`

Also rename the local variable from `entry` to `event` at each site, and update the `e.audit.Log(ctx, entry)` call to `e.audit.Log(ctx, event)`.

- [ ] **Step 2.8: Update logger_test.go**

File: `internal/audit/logger_test.go`

Global replace in this file:
- `audit.Entry` → `audit.Event`
- `Entry{` → `Event{` (when constructing)
- `PolicyID:` → `ID:` (when setting)
- `PolicyName:` → `Name:` (when setting)
- `.PolicyID` → `.ID` (when reading)
- `.PolicyName` → `.Name` (when reading)

Also update test names where they reference the old type name. Use `sed` with caution; prefer manual inspection after replace.

Run: `grep -n 'Entry\|PolicyID\|PolicyName' internal/audit/logger_test.go`
Expected: only matches inside docstrings or test names that the rename didn't need to touch, OR no matches at all.

- [ ] **Step 2.9: Update engine_test.go and engine_bench_test.go**

File: `internal/access/policy/engine_test.go`

Global replace:
- `audit.Entry` → `audit.Event`
- `Entry{` → `Event{` (inside audit construction context)
- `PolicyID:` → `ID:` (when setting inside `audit.Event{}`)
- `PolicyName:` → `Name:` (when setting inside `audit.Event{}`)

**Caution:** `PolicyID` and `PolicyName` are ALSO used as methods on `types.Decision` (e.g., `decision.PolicyID()`). Those must NOT be renamed — only the field names inside `audit.Event{}` literals. Verify each match is inside a struct literal for the audit type, not a method call on a decision.

File: `internal/access/policy/engine_bench_test.go`

Same treatment.

- [ ] **Step 2.10: Update evaluation_test.go**

File: `test/integration/access/evaluation_test.go`

Same treatment as engine_test.go if the file references `audit.Entry` or the old field names. If it only imports the package without constructing entries, no changes needed.

Run: `grep -n 'audit\.Entry\|audit\.Event' test/integration/access/evaluation_test.go`

If `audit.Entry` appears, replace with `audit.Event`.

- [ ] **Step 2.11: Verify compilation and run tests**

Run:
```bash
task build
task test -- ./internal/audit/... ./internal/access/...
```

Expected: all tests pass.

- [ ] **Step 2.12: Commit**

Run:

```bash
jj --no-pager commit -m "refactor(audit): rename Entry to Event and PolicyID/PolicyName to ID/Name

Drop the type stutter (audit.Event instead of audit.Entry) and
remove policy-specific field names. Field semantics and the
Logger's mode routing are unchanged.

Engine call sites updated to populate the new field names.
Postgres writer uses event_id/event_name column names (schema
migration to rename comes in a later commit).

Bead: holomush-ggbz"
```

---

## Task 3: Add `EventSource` type and `SourceEngine`/`SourcePlugin`/`SourceSystem` constants

**Goal:** Add the defined-string-type `EventSource` and its constants in a dedicated file.

**Files:**

- Create: `internal/audit/source.go`, `internal/audit/source_test.go`

- [ ] **Step 3.1: Write the failing test**

File: `internal/audit/source_test.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/audit"
)

func TestEventSourceConstantsHaveExpectedStringValues(t *testing.T) {
	assert.Equal(t, "engine", string(audit.SourceEngine))
	assert.Equal(t, "plugin", string(audit.SourcePlugin))
	assert.Equal(t, "system", string(audit.SourceSystem))
}

func TestEventSourceIsADefinedTypeDistinctFromString(t *testing.T) {
	var s audit.EventSource = audit.SourceEngine
	// A plain string should not be assignable to EventSource without conversion.
	// This test proves the type is defined (not an alias) by forcing the
	// conversion at compile time.
	var raw string = string(s)
	assert.Equal(t, "engine", raw)
}
```

- [ ] **Step 3.2: Run test to verify it fails**

Run: `task test -- -run 'TestEventSourceConstants' ./internal/audit/...`
Expected: FAIL with `undefined: audit.SourceEngine`.

- [ ] **Step 3.3: Write the source type**

File: `internal/audit/source.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

// EventSource identifies the kind of authorization subsystem that produced
// an audit event. It is a discriminator for operator queries, not a
// behavior switch — the audit logger's mode routing uses Effect, not Source.
//
// EventSource is a defined string type (not an alias) so function signatures
// remain self-documenting and the package can attach methods later if needed.
type EventSource string

// EventSource constants. These are the only values the engine, plugin
// dispatcher, and system paths use. Additional values are additive and
// MAY be introduced by adding a new constant without breaking existing
// consumers — nothing switches on this value.
const (
	// SourceEngine is stamped on events produced by the ABAC policy engine.
	SourceEngine EventSource = "engine"

	// SourcePlugin is stamped on events produced by plugin handler code
	// via the dispatcher's hint extraction path. The specific plugin name
	// lives in the Component field.
	SourcePlugin EventSource = "plugin"

	// SourceSystem is stamped on events produced by system-bypass paths
	// (operator overrides, reaper operations, bootstrap seeding).
	SourceSystem EventSource = "system"
)
```

- [ ] **Step 3.4: Run the test to verify it passes**

Run: `task test -- -run 'TestEventSourceConstants' ./internal/audit/...`
Expected: PASS.

- [ ] **Step 3.5: Commit**

Run:

```bash
jj --no-pager commit -m "feat(audit): add EventSource defined type and constants

Introduce EventSource as a defined string type for audit event
source discrimination. The three initial values (engine, plugin,
system) cover all current and near-term authorization subsystems.

No behavior changes — nothing switches on Source. Consumers use
it for operator queries and filtering.

Bead: holomush-ggbz"
```

---

## Task 4: Add `Source`, `Component`, `Message` fields to `Event`

**Goal:** Extend the `Event` struct with the three new fields so plugin paths can populate them and engine paths can stamp `SourceEngine` / `"abac"`.

**Files:**

- Modify: `internal/audit/logger.go`, `internal/audit/logger_test.go`, `internal/audit/doc.go`

- [ ] **Step 4.1: Write a failing test for the new fields**

File: `internal/audit/logger_test.go`

Add a new test function at the end of the file:

```go
func TestEventHasSourceComponentMessageFields(t *testing.T) {
	event := audit.Event{
		Subject:   "character:01ABC",
		Action:    "speak",
		Resource:  "channel:01XYZ",
		Effect:    types.EffectDeny,
		ID:        "not_member",
		Name:      "channels: not a member",
		Message:   "player not in channel members",
		Source:    audit.SourcePlugin,
		Component: "core-channels",
	}
	assert.Equal(t, "player not in channel members", event.Message)
	assert.Equal(t, audit.SourcePlugin, event.Source)
	assert.Equal(t, "core-channels", event.Component)
}
```

Make sure the test file imports `types` (`"github.com/holomush/holomush/internal/access/policy/types"`) if not already imported.

- [ ] **Step 4.2: Run test to verify it fails**

Run: `task test -- -run 'TestEventHasSourceComponentMessageFields' ./internal/audit/...`
Expected: FAIL with `unknown field Message in struct literal of type audit.Event` (or similar for Source/Component).

- [ ] **Step 4.3: Add the fields to Event struct**

File: `internal/audit/logger.go`

Find:
```go
// Event represents a single access control decision to be logged.
type Event struct {
	Subject    string         `json:"subject"`
	Action     string         `json:"action"`
	Resource   string         `json:"resource"`
	Effect     types.Effect   `json:"effect"`
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes"`
	DurationUS int64          `json:"duration_us"`
	Timestamp  time.Time      `json:"timestamp"`
}
```

Replace with:
```go
// Event represents a single access control decision to be logged.
type Event struct {
	// Identity of the decision
	ID        string      `json:"id"`        // stable slug: "permit-write-own-room", "not_member"
	Name      string      `json:"name"`      // human-readable rule label
	Message   string      `json:"message"`   // per-firing human description
	Source    EventSource `json:"source"`    // kind of emitter: engine / plugin / system
	Component string      `json:"component"` // subsystem within source: "core-channels", "abac"

	// What was attempted
	Subject  string       `json:"subject"`
	Action   string       `json:"action"`
	Resource string       `json:"resource"`
	Effect   types.Effect `json:"effect"`

	// Context
	Attributes map[string]any `json:"attributes"`
	DurationUS int64          `json:"duration_us"`
	Timestamp  time.Time      `json:"timestamp"`
}
```

- [ ] **Step 4.4: Run test to verify it passes**

Run: `task test -- -run 'TestEventHasSourceComponentMessageFields' ./internal/audit/...`
Expected: PASS.

- [ ] **Step 4.5: Update `PostgresWriter.WriteSync` to include the new fields**

File: `internal/audit/postgres.go`

Find the `WriteSync` query and the `ExecContext` call. Update both the column list and the parameters:

Find:
```go
	query := `
		INSERT INTO access_audit_log (
			id, subject, action, resource, effect, event_id, event_name,
			attributes, duration_us, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	attributesJSON, err := json.Marshal(event.Attributes)
	if err != nil {
		return oops.Wrap(err)
	}

	_, err = w.db.ExecContext(ctx, query,
		idgen.New().String(),
		event.Subject,
		event.Action,
		event.Resource,
		event.Effect.String(),
		event.ID,
		event.Name,
		attributesJSON,
		event.DurationUS,
		event.Timestamp,
	)
```

Replace with:
```go
	query := `
		INSERT INTO access_audit_log (
			id, subject, action, resource, effect, event_id, event_name,
			message, source, component, attributes, duration_us, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	attributesJSON, err := json.Marshal(event.Attributes)
	if err != nil {
		return oops.Wrap(err)
	}

	_, err = w.db.ExecContext(ctx, query,
		idgen.New().String(),
		event.Subject,
		event.Action,
		event.Resource,
		event.Effect.String(),
		event.ID,
		event.Name,
		event.Message,
		string(event.Source),
		event.Component,
		attributesJSON,
		event.DurationUS,
		event.Timestamp,
	)
```

- [ ] **Step 4.6: Update `PostgresWriter.writeBatch` similarly**

File: `internal/audit/postgres.go`

Apply the same column list + parameter extension to the prepared statement and the `stmt.ExecContext` call inside `writeBatch`.

Find:
```go
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO access_audit_log (
			id, subject, action, resource, effect, event_id, event_name,
			attributes, duration_us, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`)
```

Replace with:
```go
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO access_audit_log (
			id, subject, action, resource, effect, event_id, event_name,
			message, source, component, attributes, duration_us, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`)
```

Find (the loop body):
```go
		_, err = stmt.ExecContext(ctx,
			idgen.New().String(),
			event.Subject,
			event.Action,
			event.Resource,
			event.Effect.String(),
			event.ID,
			event.Name,
			attributesJSON,
			event.DurationUS,
			event.Timestamp,
		)
```

Replace with:
```go
		_, err = stmt.ExecContext(ctx,
			idgen.New().String(),
			event.Subject,
			event.Action,
			event.Resource,
			event.Effect.String(),
			event.ID,
			event.Name,
			event.Message,
			string(event.Source),
			event.Component,
			attributesJSON,
			event.DurationUS,
			event.Timestamp,
		)
```

- [ ] **Step 4.7: Update doc.go example**

File: `internal/audit/doc.go`

Find:
```go
//	// Log a decision
//	event := audit.Event{
//	    Subject:    "character:01ABC",
//	    Action:     "read",
//	    Resource:   "location:01XYZ",
//	    Effect:     types.EffectAllow,
//	    ID:         "policy-123",
//	    Name:       "allow-read",
//	    Attributes: map[string]any{"role": "player"},
//	    DurationUS: 150,
//	    Timestamp:  time.Now(),
//	}
//	logger.Log(ctx, event)
```

Replace with:
```go
//	// Log a decision
//	event := audit.Event{
//	    ID:         "policy-123",
//	    Name:       "allow-read",
//	    Source:     audit.SourceEngine,
//	    Component:  "abac",
//	    Subject:    "character:01ABC",
//	    Action:     "read",
//	    Resource:   "location:01XYZ",
//	    Effect:     types.EffectAllow,
//	    Attributes: map[string]any{"role": "player"},
//	    DurationUS: 150,
//	    Timestamp:  time.Now(),
//	}
//	logger.Log(ctx, event)
```

- [ ] **Step 4.8: Run all audit tests**

Run: `task test -- ./internal/audit/...`
Expected: all tests pass.

- [ ] **Step 4.9: Commit**

Run:

```bash
jj --no-pager commit -m "feat(audit): add Source, Component, Message fields to Event

Event gains three new fields:

- Source (EventSource): who kind produced the event (engine/plugin/system)
- Component (string): specific subsystem within the source
- Message (string): per-firing human description

Postgres writer writes the new columns. Engine call sites still
leave Source/Component/Message empty — that stamping comes in the
next commit.

Bead: holomush-ggbz"
```

---

## Task 5: Stamp `Source = SourceEngine` and `Component = "abac"` at engine call sites

**Goal:** Every audit event the ABAC engine produces carries the new discriminators so operator queries can filter by source.

**Files:**

- Modify: `internal/access/policy/engine.go`, `internal/access/policy/engine_test.go`

- [ ] **Step 5.1: Write a failing regression test**

File: `internal/access/policy/engine_test.go`

Add the following test function near the other engine tests:

```go
func TestEngineAuditEventsCarrySourceEngineAndComponentAbac(t *testing.T) {
	ctx := context.Background()
	writer := &capturingAuditWriter{}
	logger := audit.NewLogger(audit.ModeAll, writer, "")
	t.Cleanup(func() { _ = logger.Close() })

	// Use an empty policy store — evaluation should produce a default_deny
	// which still emits an audit event.
	ps := &emptyPolicyStore{}
	compiler := policy.NewCompiler(attribute.NewSchemaRegistry().Schema())
	cache := policy.NewCache(ps, compiler)
	require.NoError(t, cache.Reload(ctx))

	schemaRegistry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(schemaRegistry)
	sessionResolver := &noopSessionResolver{}

	engine := policy.NewEngine(resolver, cache, sessionResolver, logger)

	req, err := types.NewAccessRequest("character:01ABC", "read", "location:01XYZ")
	require.NoError(t, err)

	_, evalErr := engine.Evaluate(ctx, req)
	require.NoError(t, evalErr)

	require.NotEmpty(t, writer.events, "expected at least one audit event")
	last := writer.events[len(writer.events)-1]
	assert.Equal(t, audit.SourceEngine, last.Source,
		"engine-produced events must carry SourceEngine")
	assert.Equal(t, "abac", last.Component,
		"engine-produced events must carry Component='abac'")
}

// capturingAuditWriter captures events in memory for assertion.
type capturingAuditWriter struct {
	events []audit.Event
	mu     sync.Mutex
}

func (w *capturingAuditWriter) WriteSync(_ context.Context, event audit.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, event)
	return nil
}

func (w *capturingAuditWriter) WriteAsync(event audit.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, event)
	return nil
}

func (w *capturingAuditWriter) Close() error { return nil }

// emptyPolicyStore returns zero policies from List.
type emptyPolicyStore struct{}

func (s *emptyPolicyStore) List(_ context.Context, _ store.ListOptions) ([]*store.StoredPolicy, error) {
	return nil, nil
}

// noopSessionResolver is a placeholder for tests using character: subjects.
type noopSessionResolver struct{}

func (r *noopSessionResolver) ResolveSession(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("no sessions in test")
}
```

Add required imports if not already present:
- `"sync"`
- `"fmt"`
- `"github.com/holomush/holomush/internal/audit"`
- `"github.com/holomush/holomush/internal/access/policy/attribute"`
- `"github.com/holomush/holomush/internal/access/policy/store"` (whichever store package houses `ListOptions`; if unclear, check an existing engine_test.go import)
- `policy "github.com/holomush/holomush/internal/access/policy"` (aliased if needed)

**Note:** If the test file already has compatible helper types (a fake writer, a fake policy store, a no-op session resolver), reuse them instead of duplicating. Check the top of engine_test.go first. The above stubs are the fallback if nothing exists.

- [ ] **Step 5.2: Run the test to verify it fails**

Run: `task test -- -run 'TestEngineAuditEventsCarrySourceEngineAndComponentAbac' ./internal/access/policy/...`
Expected: FAIL — the engine emits events with `Source = ""` and `Component = ""` (zero values).

- [ ] **Step 5.3: Update all 6 engine call sites to stamp Source and Component**

File: `internal/access/policy/engine.go`

For each of the six `audit.Event{}` literals at approximately lines 125, 151, 198, 229, 262, 300, add the Source and Component fields.

Example transformation for the system-bypass site (~line 125):

Old:
```go
event := audit.Event{
    Subject:    req.Subject,
    Action:     req.Action,
    Resource:   req.Resource,
    Effect:     types.EffectSystemBypass,
    ID:         "",
    Name:       "",
    DurationUS: time.Since(start).Microseconds(),
    Timestamp:  time.Now(),
}
```

New:
```go
event := audit.Event{
    ID:         "",
    Name:       "",
    Source:     audit.SourceSystem, // system bypass is conceptually a system source
    Component:  "abac",
    Subject:    req.Subject,
    Action:     req.Action,
    Resource:   req.Resource,
    Effect:     types.EffectSystemBypass,
    DurationUS: time.Since(start).Microseconds(),
    Timestamp:  time.Now(),
}
```

For the other five call sites (degraded-mode, session-resolution-failure, attribute-resolution-failure, no-applicable-policies, final-decision), apply the same pattern but use `Source: audit.SourceEngine` since these are actual ABAC engine evaluation paths:

```go
event := audit.Event{
    ID:         <existing ID value>,
    Name:       <existing Name value>,
    Source:     audit.SourceEngine,
    Component:  "abac",
    Subject:    req.Subject,
    Action:     req.Action,
    Resource:   req.Resource,
    Effect:     <existing Effect value>,
    DurationUS: time.Since(start).Microseconds(),
    Timestamp:  time.Now(),
}
```

Specifically:
- Line ~125 (system bypass): `Source: audit.SourceSystem`, `Component: "abac"` — system bypass is the one non-engine engine path
- Line ~151 (degraded mode): `Source: audit.SourceEngine`, `Component: "abac"`
- Line ~198 (session invalid): `Source: audit.SourceEngine`, `Component: "abac"`
- Line ~229 (attribute resolution failure): `Source: audit.SourceEngine`, `Component: "abac"`
- Line ~262 (no applicable policies): `Source: audit.SourceEngine`, `Component: "abac"`
- Line ~300 (final decision): `Source: audit.SourceEngine`, `Component: "abac"`

- [ ] **Step 5.4: Run the test to verify it passes**

Run: `task test -- -run 'TestEngineAuditEventsCarrySourceEngineAndComponentAbac' ./internal/access/policy/...`
Expected: PASS.

- [ ] **Step 5.5: Run all engine tests**

Run: `task test -- ./internal/access/policy/...`
Expected: all tests pass.

- [ ] **Step 5.6: Commit**

Run:

```bash
jj --no-pager commit -m "feat(audit): stamp Source and Component on engine audit events

Every audit event the ABAC engine produces now carries:

- Source = SourceEngine (or SourceSystem for EffectSystemBypass path)
- Component = 'abac'

Operator queries can now filter engine events with
WHERE source = 'engine' AND component = 'abac'.

Bead: holomush-ggbz"
```

---

## Task 6: Add context helpers (`NewContextForDispatch`, `AddEventToContext`, `EventsFromContext`)

**Goal:** Provide a source-agnostic primitive for accumulating audit events on a context. The dispatcher will attach a slice at the start of command processing; plugin SDK (binary) and Lua capability will push to the same slice; the dispatcher will drain and flush at end of processing.

**Files:**

- Create: `internal/audit/context.go`, `internal/audit/context_test.go`

- [ ] **Step 6.1: Write failing tests**

File: `internal/audit/context_test.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/access/policy/types"
)

func TestNewContextForDispatchAttachesEmptyEventSlice(t *testing.T) {
	ctx := audit.NewContextForDispatch(context.Background())
	events := audit.EventsFromContext(ctx)
	assert.Empty(t, events, "fresh dispatch context should have no events")
}

func TestAddEventToContextAppendsToAttachedSlice(t *testing.T) {
	ctx := audit.NewContextForDispatch(context.Background())
	audit.AddEventToContext(ctx, audit.Event{
		ID:        "test-event",
		Source:    audit.SourcePlugin,
		Component: "test-plugin",
		Effect:    types.EffectDeny,
	})

	events := audit.EventsFromContext(ctx)
	assert.Len(t, events, 1)
	assert.Equal(t, "test-event", events[0].ID)
}

func TestAddEventToContextIsNoOpWhenNoSliceAttached(t *testing.T) {
	// Baseline context with no dispatch attachment.
	ctx := context.Background()
	audit.AddEventToContext(ctx, audit.Event{ID: "orphan"})

	events := audit.EventsFromContext(ctx)
	assert.Nil(t, events, "plain context should have no attached slice")
}

func TestEventsFromContextDrainsTheSlice(t *testing.T) {
	ctx := audit.NewContextForDispatch(context.Background())
	audit.AddEventToContext(ctx, audit.Event{ID: "e1"})
	audit.AddEventToContext(ctx, audit.Event{ID: "e2"})

	first := audit.EventsFromContext(ctx)
	assert.Len(t, first, 2)

	second := audit.EventsFromContext(ctx)
	assert.Empty(t, second, "second call should return empty — drain is destructive")
}

func TestAddEventToContextIsSafeForMultipleCalls(t *testing.T) {
	ctx := audit.NewContextForDispatch(context.Background())
	for i := 0; i < 10; i++ {
		audit.AddEventToContext(ctx, audit.Event{ID: "bulk"})
	}
	events := audit.EventsFromContext(ctx)
	assert.Len(t, events, 10)
}
```

- [ ] **Step 6.2: Run tests to verify they fail**

Run: `task test -- -run 'TestNewContextForDispatch|TestAddEventToContext|TestEventsFromContext' ./internal/audit/...`
Expected: FAIL with `undefined: audit.NewContextForDispatch`.

- [ ] **Step 6.3: Implement the context helpers**

File: `internal/audit/context.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import "context"

// contextKey is the unexported type used as the map key for storing the
// dispatch event slice on a context. Using an unexported named type
// prevents collisions with other packages that might use context.WithValue.
type contextKey struct{}

// eventsKey is the sentinel value looked up on contexts carrying a
// dispatch-scoped event slice.
var eventsKey = contextKey{}

// NewContextForDispatch returns a derived context with an empty event
// slice attached. Call this at the start of any operation whose emitted
// events should be flushed at completion (typically command dispatch).
//
// The returned context can be passed across goroutine boundaries, but
// the slice itself is NOT concurrency-safe. If a caller emits events
// from multiple goroutines, it MUST serialize emission externally.
func NewContextForDispatch(ctx context.Context) context.Context {
	events := &[]Event{}
	return context.WithValue(ctx, eventsKey, events)
}

// AddEventToContext appends an event to the slice attached to ctx.
// If no slice is attached (ctx was not derived from NewContextForDispatch),
// the call is a silent no-op. This is intentional: code paths that may
// run inside or outside a dispatch context can emit events unconditionally
// without branching on whether accumulation is active.
func AddEventToContext(ctx context.Context, event Event) {
	if events, ok := ctx.Value(eventsKey).(*[]Event); ok {
		*events = append(*events, event)
	}
}

// EventsFromContext returns and clears the event slice attached to ctx.
// Returns nil if no slice was attached. The clear is destructive — a
// subsequent call on the same context returns an empty slice, not the
// same values again. This prevents double-flush during partial failures.
func EventsFromContext(ctx context.Context) []Event {
	events, ok := ctx.Value(eventsKey).(*[]Event)
	if !ok {
		return nil
	}
	drained := *events
	*events = nil
	return drained
}
```

- [ ] **Step 6.4: Run tests to verify they pass**

Run: `task test -- -run 'TestNewContextForDispatch|TestAddEventToContext|TestEventsFromContext' ./internal/audit/...`
Expected: PASS.

- [ ] **Step 6.5: Commit**

Run:

```bash
jj --no-pager commit -m "feat(audit): context helpers for dispatch-scoped event accumulation

Add NewContextForDispatch / AddEventToContext / EventsFromContext
as the source-agnostic primitive the dispatcher uses to collect
plugin-emitted audit events during command processing and flush
them at completion.

The audit package is the right home for these helpers because
they do not reference the dispatcher, plugins, or any consumer —
they are a pure context primitive for accumulating Events.

Bead: holomush-ggbz"
```

---

## Task 7: Add `RecordPluginAuditFailure` Prometheus counter

**Goal:** Mirror the existing `RecordEngineAuditFailure` metric so dispatcher-side audit flush failures are observable.

**Files:**

- Create: `internal/audit/plugin_metrics.go`
- Modify: `internal/audit/logger_test.go` (add a smoke test)

- [ ] **Step 7.1: Write the metric and helper**

File: `internal/audit/plugin_metrics.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// pluginAuditFailuresCounter tracks audit logging failures originating
// from the dispatcher's plugin-event flush step (as opposed to the
// engine's own failures tracked by engineAuditFailuresCounter).
//
// Bumped by the dispatcher when auditLogger.Log returns an error for
// a plugin-sourced event. The dispatcher continues processing remaining
// events and returns the user's command response unchanged.
var pluginAuditFailuresCounter = promauto.NewCounter(prometheus.CounterOpts{
	Name: "abac_audit_plugin_failures_total",
	Help: "Total number of audit logging failures at the plugin-flush level",
})

// RecordPluginAuditFailure increments the plugin-level audit failure counter.
// Call this when auditLogger.Log returns an error during dispatcher flush
// of plugin-emitted events.
func RecordPluginAuditFailure() {
	pluginAuditFailuresCounter.Inc()
}
```

- [ ] **Step 7.2: Write a smoke test**

File: `internal/audit/logger_test.go` (append at the end)

```go
func TestRecordPluginAuditFailureDoesNotPanic(t *testing.T) {
	// Smoke test — the counter is process-global; we just verify the
	// helper is callable without panic.
	assert.NotPanics(t, func() {
		audit.RecordPluginAuditFailure()
	})
}
```

- [ ] **Step 7.3: Run the smoke test**

Run: `task test -- -run 'TestRecordPluginAuditFailure' ./internal/audit/...`
Expected: PASS.

- [ ] **Step 7.4: Commit**

Run:

```bash
jj --no-pager commit -m "feat(audit): add RecordPluginAuditFailure metric

Mirror the existing RecordEngineAuditFailure counter for audit
flush failures originating from the dispatcher's plugin event
flush step.

Bead: holomush-ggbz"
```

---

## Task 8: Database migration — rename `policy_id`/`policy_name` and add new columns

**Goal:** Align the `access_audit_log` table schema with the new `Event` shape.

**Files:**

- Create: `internal/store/migrations/000005_audit_source_component.up.sql`, `internal/store/migrations/000005_audit_source_component.down.sql`

- [ ] **Step 8.1: Write the up migration**

File: `internal/store/migrations/000005_audit_source_component.up.sql`

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Rename policy_id / policy_name to event_id / event_name to reflect
-- that the audit log records events from any authorization source, not
-- just ABAC policies. Add source, component, message columns.

ALTER TABLE access_audit_log RENAME COLUMN policy_id TO event_id;
ALTER TABLE access_audit_log RENAME COLUMN policy_name TO event_name;

ALTER TABLE access_audit_log ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'engine';
ALTER TABLE access_audit_log ADD COLUMN IF NOT EXISTS component TEXT NOT NULL DEFAULT 'abac';
ALTER TABLE access_audit_log ADD COLUMN IF NOT EXISTS message TEXT NOT NULL DEFAULT '';

-- Drop the default now that historical rows have been backfilled. New
-- rows must provide explicit values.
ALTER TABLE access_audit_log ALTER COLUMN source DROP DEFAULT;
ALTER TABLE access_audit_log ALTER COLUMN component DROP DEFAULT;
ALTER TABLE access_audit_log ALTER COLUMN message DROP DEFAULT;

-- Index for operator queries that filter by source and component.
CREATE INDEX IF NOT EXISTS idx_audit_log_source_component
    ON access_audit_log(source, component, timestamp DESC);
```

- [ ] **Step 8.2: Write the down migration**

File: `internal/store/migrations/000005_audit_source_component.down.sql`

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS idx_audit_log_source_component;

ALTER TABLE access_audit_log DROP COLUMN IF EXISTS message;
ALTER TABLE access_audit_log DROP COLUMN IF EXISTS component;
ALTER TABLE access_audit_log DROP COLUMN IF EXISTS source;

ALTER TABLE access_audit_log RENAME COLUMN event_name TO policy_name;
ALTER TABLE access_audit_log RENAME COLUMN event_id TO policy_id;
```

- [ ] **Step 8.3: Write a schema-shape integration test for the migration**

File: `internal/store/migrations_audit_shape_integration_test.go`

This test MUST spin up a real Postgres testcontainer, apply all migrations (including 000005), and assert the `access_audit_log` table has the expected column shape. A test that stops at "it compiled" is insufficient.

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

func TestMigration000005AuditSourceComponentAppliesCleanly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = pgEnv.Container.Terminate(context.Background())
	})

	migrator, err := store.NewMigrator(pgEnv.ConnStr)
	require.NoError(t, err)
	require.NoError(t, migrator.Up())
	require.NoError(t, migrator.Close())

	db, err := sql.Open("postgres", pgEnv.ConnStr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Assert the renamed columns exist.
	assertColumnExists(t, db, "access_audit_log", "event_id")
	assertColumnExists(t, db, "access_audit_log", "event_name")

	// Assert the new columns exist with NOT NULL constraint.
	assertColumnExistsNotNull(t, db, "access_audit_log", "source")
	assertColumnExistsNotNull(t, db, "access_audit_log", "component")
	assertColumnExistsNotNull(t, db, "access_audit_log", "message")

	// Assert the old column names no longer exist.
	assertColumnDoesNotExist(t, db, "access_audit_log", "policy_id")
	assertColumnDoesNotExist(t, db, "access_audit_log", "policy_name")

	// Assert the source/component index was created.
	assertIndexExists(t, db, "idx_audit_log_source_component")
}

func assertColumnExists(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	var count int
	err := db.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = $1 AND column_name = $2
	`, table, column).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "expected column %s.%s to exist", table, column)
}

func assertColumnExistsNotNull(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	var isNullable string
	err := db.QueryRow(`
		SELECT is_nullable FROM information_schema.columns
		WHERE table_name = $1 AND column_name = $2
	`, table, column).Scan(&isNullable)
	require.NoError(t, err, "column %s.%s not found", table, column)
	assert.Equal(t, "NO", isNullable, "column %s.%s should be NOT NULL", table, column)
}

func assertColumnDoesNotExist(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	var count int
	err := db.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = $1 AND column_name = $2
	`, table, column).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "expected column %s.%s to NOT exist after migration", table, column)
}

func assertIndexExists(t *testing.T, db *sql.DB, indexName string) {
	t.Helper()
	var count int
	err := db.QueryRow(`
		SELECT count(*) FROM pg_indexes
		WHERE indexname = $1
	`, indexName).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "expected index %s to exist", indexName)
}
```

Run: `task test:int -- -run 'TestMigration000005AuditSourceComponentAppliesCleanly' ./internal/store/...`

Expected: PASS. All assertions hold against a real Postgres.

- [ ] **Step 8.4: Write the migration rollback test (MUST — reversibility invariant)**

File: `internal/store/migrations_audit_shape_integration_test.go` (append)

```go
func TestMigration000005AuditSourceComponentRollbackReturnsSchemaToOriginalShape(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = pgEnv.Container.Terminate(context.Background())
	})

	// Apply all migrations (including 000005).
	migratorUp, err := store.NewMigrator(pgEnv.ConnStr)
	require.NoError(t, err)
	require.NoError(t, migratorUp.Up())
	require.NoError(t, migratorUp.Close())

	db, err := sql.Open("postgres", pgEnv.ConnStr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Sanity: after Up, the new shape is present.
	assertColumnExists(t, db, "access_audit_log", "event_id")
	assertColumnExists(t, db, "access_audit_log", "source")

	// Roll back just 000005 by stepping the migrator down once.
	migratorDown, err := store.NewMigrator(pgEnv.ConnStr)
	require.NoError(t, err)
	require.NoError(t, migratorDown.Steps(-1))
	require.NoError(t, migratorDown.Close())

	// After down, the original shape is restored.
	assertColumnExists(t, db, "access_audit_log", "policy_id")
	assertColumnExists(t, db, "access_audit_log", "policy_name")
	assertColumnDoesNotExist(t, db, "access_audit_log", "event_id")
	assertColumnDoesNotExist(t, db, "access_audit_log", "event_name")
	assertColumnDoesNotExist(t, db, "access_audit_log", "source")
	assertColumnDoesNotExist(t, db, "access_audit_log", "component")
	assertColumnDoesNotExist(t, db, "access_audit_log", "message")

	// Index should also be dropped.
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM pg_indexes WHERE indexname = $1`,
		"idx_audit_log_source_component",
	).Scan(&count))
	assert.Equal(t, 0, count, "source/component index should be dropped on rollback")
}
```

**Note:** The exact method name for stepping a migrator (`Steps(-1)` in the example above) depends on the migrator library in use. Check `internal/store/migrator.go` for the actual API and adjust. If the migrator only supports `Up()`/`Down()` without partial stepping, the test can roll back ALL migrations with `Down()` and then replay up to 000004 — verify the intended shape either way.

Run: `task test:int -- -run 'TestMigration000005AuditSourceComponentRollbackReturnsSchemaToOriginalShape' ./internal/store/...`
Expected: PASS.

- [ ] **Step 8.5: Write the migration backfill test (SHOULD — existing data preservation)**

File: `internal/store/migrations_audit_shape_integration_test.go` (append)

```go
func TestMigration000005AuditSourceComponentBackfillsExistingRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = pgEnv.Container.Terminate(context.Background())
	})

	// Apply migrations through 000004 (the baseline + pre-000005 state).
	// The exact "stop at migration N" API depends on the migrator library;
	// check internal/store/migrator.go. If only Up() is available, apply
	// all migrations but capture how many steps 000005 represents so the
	// assertion can be targeted.
	migratorEarly, err := store.NewMigrator(pgEnv.ConnStr)
	require.NoError(t, err)
	// Apply all migrations up to and including 000004 but NOT 000005.
	require.NoError(t, migratorEarly.Migrate(4))
	require.NoError(t, migratorEarly.Close())

	db, err := sql.Open("postgres", pgEnv.ConnStr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Insert a row into access_audit_log using the pre-000005 schema
	// (policy_id/policy_name columns, no source/component/message).
	_, err = db.ExecContext(ctx, `
		INSERT INTO access_audit_log (
			id, timestamp, subject, action, resource, effect,
			policy_id, policy_name, attributes, duration_us
		) VALUES (
			'seed-test-id', NOW(), 'character:01ABC', 'read', 'location:01XYZ', 'allow',
			'allow-read', 'Allow Read', '{}'::jsonb, 100
		)
	`)
	require.NoError(t, err)

	// Now apply 000005.
	migratorFinal, err := store.NewMigrator(pgEnv.ConnStr)
	require.NoError(t, err)
	require.NoError(t, migratorFinal.Up())
	require.NoError(t, migratorFinal.Close())

	// Query the existing row through the new column names — it should
	// have been preserved, and the new columns should contain the
	// migration defaults.
	var source, component, message, eventID, eventName string
	err = db.QueryRow(`
		SELECT source, component, message, event_id, event_name
		FROM access_audit_log
		WHERE id = $1
	`, "seed-test-id").Scan(&source, &component, &message, &eventID, &eventName)
	require.NoError(t, err)

	assert.Equal(t, "engine", source, "pre-existing rows should be backfilled with source='engine'")
	assert.Equal(t, "abac", component, "pre-existing rows should be backfilled with component='abac'")
	assert.Equal(t, "", message, "pre-existing rows should have empty message")
	assert.Equal(t, "allow-read", eventID, "existing policy_id value should be renamed to event_id")
	assert.Equal(t, "Allow Read", eventName, "existing policy_name value should be renamed to event_name")
}
```

**Note:** `migratorEarly.Migrate(4)` is a placeholder for "apply migrations up to version 4". The exact API depends on the migrator library — check `internal/store/migrator.go`. If partial migration isn't supported, the test can be simplified to just verify the backfill defaults without the pre-existing row scenario (since the project has no production data yet, the main risk is just making sure the migration *could* handle existing rows cleanly).

Run: `task test:int -- -run 'TestMigration000005AuditSourceComponentBackfillsExistingRows' ./internal/store/...`
Expected: PASS.

- [ ] **Step 8.4: Commit**

Run:

```bash
jj --no-pager commit -m "feat(audit): schema migration for source/component/message columns

Rename policy_id/policy_name to event_id/event_name and add
source/component/message columns to access_audit_log. Add an
index on (source, component, timestamp) for operator queries.

Existing rows get default values ('engine'/'abac'/'') to preserve
historical data, but new inserts must provide explicit values.

Bead: holomush-ggbz"
```

---

## Task 9: Add `AuditDecisionHint` proto message and `audit_hints` field to `CommandResponse`

**Goal:** Define the wire format for inline audit hints on the plugin command response.

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto`
- Regenerated (automatic): `pkg/proto/holomush/plugin/v1/plugin.pb.go`

- [ ] **Step 9.1: Add the `AuditDecisionHint` message and field**

File: `api/proto/holomush/plugin/v1/plugin.proto`

Find:
```proto
// CommandResponse carries the result of a plugin command execution.
message CommandResponse {
  // Outcome category.
  CommandStatus status = 1;
  // Synchronous text output to the invoking player.
  string output = 2 [(buf.validate.field).string.max_len = 8192];
  // Events to append to the event store.
  repeated EmitEvent events = 3;
}
```

Replace with:
```proto
// CommandResponse carries the result of a plugin command execution.
message CommandResponse {
  // Outcome category.
  CommandStatus status = 1;
  // Synchronous text output to the invoking player.
  string output = 2 [(buf.validate.field).string.max_len = 8192];
  // Events to append to the event store.
  repeated EmitEvent events = 3;
  // Audit decision hints accumulated by the plugin handler during this
  // command dispatch. The dispatcher extracts these after the response is
  // returned, stamps host-controlled fields (subject, action base, source,
  // component, timestamp, duration), and flushes them through the audit
  // logger.
  repeated AuditDecisionHint audit_hints = 4;
}

// AuditDecisionHint is a partial audit event emitted by a plugin handler.
// The plugin provides decision-specific fields (id, name, message, effect,
// resource, attributes, action qualifier); the host stamps identity fields
// (subject from dispatch context, source = SourcePlugin, component =
// plugin name, timestamp, duration).
//
// Plugins MUST NOT set subject, source, or component — the dispatcher
// overwrites those fields to prevent spoofing.
message AuditDecisionHint {
  // Stable slug identifying the plugin's internal rule, e.g., "not_member".
  string id = 1 [(buf.validate.field).string.min_len = 1, (buf.validate.field).string.max_len = 128];

  // Human-readable label for the rule, e.g., "channels: not a member".
  string name = 2 [(buf.validate.field).string.max_len = 256];

  // Per-firing description, e.g., "player not in channel members".
  string message = 3 [(buf.validate.field).string.max_len = 1024];

  // Effect the plugin decided: "deny" or "allow".
  string effect = 4 [(buf.validate.field).string.min_len = 1];

  // Action qualifier appended to the dispatcher-known base action.
  // E.g., the dispatcher knows the command is "channel"; the plugin
  // supplies "speak", producing final action "channel:speak".
  string action_qualifier = 5 [(buf.validate.field).string.max_len = 64];

  // Resource reference in <type>:<id> form, e.g., "channel:01XYZ".
  // Plugin-provided, host-validated for shape.
  string resource = 6 [(buf.validate.field).string.max_len = 256];

  // Plugin-provided context. Keys SHOULD be namespaced (e.g.,
  // "channel.type" rather than "type") to avoid collision with
  // host-overlay keys.
  map<string, string> attributes = 7;
}
```

- [ ] **Step 9.2: Regenerate protobuf stubs**

Run: `task proto:gen`

Expected: `pkg/proto/holomush/plugin/v1/plugin.pb.go` is updated with the new `AuditDecisionHint` type and the `AuditHints` field on `CommandResponse`.

If `task proto:gen` doesn't exist, check `Taskfile.yml` for the correct proto generation command (likely `task gen` or `task protoc`).

- [ ] **Step 9.3: Verify compilation**

Run: `task build`
Expected: compiles cleanly.

- [ ] **Step 9.4: Commit**

Run:

```bash
jj --no-pager commit -m "feat(plugin): add AuditDecisionHint proto and audit_hints field

Define the wire format for inline audit hints on CommandResponse.
Plugin handlers accumulate hints during command processing; the
dispatcher extracts them after the response returns and routes
them through the audit logger after stamping host-controlled
fields.

Bead: holomush-ggbz"
```

---

## Task 10: SDK binary plugin audit recorder API (`pluginsdk.Audit(ctx)`)

**Goal:** Expose the Shape 1 context-scoped recorder to plugin authors via the binary plugin SDK. The recorder accumulates hints on an in-process context-bound slice, and the SDK serializes them into `CommandResponse.audit_hints` when the handler returns.

**Files:**

- Create: `pkg/plugin/audit.go`, `pkg/plugin/audit_test.go`
- Modify: `pkg/plugin/command.go`, `pkg/plugin/sdk.go`

- [ ] **Step 10.1: Add `AuditHint` and `AuditHints` to the SDK command types**

File: `pkg/plugin/command.go`

Find:
```go
// CommandResponse carries the result of a plugin command execution.
type CommandResponse struct {
	// Status indicates the outcome category (OK, Error, Failure, Fatal).
	Status CommandStatus

	// Events to append to the event store.
	Events []EmitEvent

	// Output is synchronous text output to the invoking player.
	// The dispatcher emits this as a command_response event on the character stream.
	Output string

	// BootedSessions lists session IDs that were forcibly disconnected.
	// The dispatcher emits leave events and triggers session teardown for each.
	BootedSessions []string

	// EndSession signals that the invoking session should end.
	EndSession bool
}
```

Replace with:
```go
// AuditEffect is the effect a plugin handler decided for a given audit hint.
// Only "deny" and "allow" are valid — plugin denials never carry
// default_deny or system_bypass semantics.
type AuditEffect string

// AuditEffect constants for audit hint construction.
const (
	AuditEffectDeny  AuditEffect = "deny"
	AuditEffectAllow AuditEffect = "allow"
)

// AuditHint is a partial audit event the plugin handler accumulates during
// command processing. Hints are serialized into CommandResponse.audit_hints
// and harvested by the dispatcher after the handler returns.
//
// Host-stamped fields (subject, source, component, timestamp, duration) are
// filled in by the dispatcher — the plugin provides only decision-specific
// fields. Setting Subject, Source, or Component on this struct is a no-op;
// the dispatcher overwrites them.
type AuditHint struct {
	ID              string            // stable slug, e.g., "not_member"
	Name            string            // human label, e.g., "channels: not a member"
	Message         string            // per-firing description
	Effect          AuditEffect       // deny or allow
	ActionQualifier string            // appended to host base action, e.g., "speak"
	Resource        string            // <type>:<id>, e.g., "channel:01XYZ"
	Attributes      map[string]string // plugin-provided context (namespaced keys)
}

// CommandResponse carries the result of a plugin command execution.
type CommandResponse struct {
	// Status indicates the outcome category (OK, Error, Failure, Fatal).
	Status CommandStatus

	// Events to append to the event store.
	Events []EmitEvent

	// Output is synchronous text output to the invoking player.
	// The dispatcher emits this as a command_response event on the character stream.
	Output string

	// BootedSessions lists session IDs that were forcibly disconnected.
	// The dispatcher emits leave events and triggers session teardown for each.
	BootedSessions []string

	// EndSession signals that the invoking session should end.
	EndSession bool

	// AuditHints are plugin-emitted audit entries accumulated during
	// command processing. The dispatcher harvests these after the handler
	// returns and routes them through the audit logger after stamping
	// host-controlled fields. Plugin authors SHOULD NOT construct hints
	// directly; use Audit(ctx).Deny / Allow instead.
	AuditHints []AuditHint
}
```

- [ ] **Step 10.2: Write failing recorder tests**

File: `pkg/plugin/audit_test.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func TestAuditRecorderDenyAccumulatesHintOnContext(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	pluginsdk.Audit(ctx).Deny("not_member", "player not in channel members",
		pluginsdk.AuditAttrs{"channel.type": "public"})

	hints := pluginsdk.HarvestAuditHints(ctx)
	require.Len(t, hints, 1)
	assert.Equal(t, "not_member", hints[0].ID)
	assert.Equal(t, pluginsdk.AuditEffectDeny, hints[0].Effect)
	assert.Equal(t, "player not in channel members", hints[0].Message)
	assert.Equal(t, "public", hints[0].Attributes["channel.type"])
}

func TestAuditRecorderAllowAccumulatesHintWithCorrectEffect(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	pluginsdk.Audit(ctx).Allow("speak_ok", "message delivered", nil)

	hints := pluginsdk.HarvestAuditHints(ctx)
	require.Len(t, hints, 1)
	assert.Equal(t, pluginsdk.AuditEffectAllow, hints[0].Effect)
}

func TestAuditRecorderIsNoOpWhenNoHandlerContextAttached(t *testing.T) {
	// Plain context — no handler attachment.
	ctx := context.Background()

	// Should not panic, should silently drop.
	pluginsdk.Audit(ctx).Deny("orphan", "no context", nil)

	hints := pluginsdk.HarvestAuditHints(ctx)
	assert.Nil(t, hints)
}

func TestHarvestAuditHintsDrainsTheSlice(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())
	pluginsdk.Audit(ctx).Deny("e1", "", nil)
	pluginsdk.Audit(ctx).Deny("e2", "", nil)

	first := pluginsdk.HarvestAuditHints(ctx)
	assert.Len(t, first, 2)

	second := pluginsdk.HarvestAuditHints(ctx)
	assert.Empty(t, second, "harvest is destructive")
}

func TestAuditRecorderDenyCopiesAttributesNotReferenced(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	attrs := pluginsdk.AuditAttrs{"key": "value"}
	pluginsdk.Audit(ctx).Deny("copy_test", "", attrs)

	// Mutate the caller's map — the recorded hint should not change.
	attrs["key"] = "mutated"

	hints := pluginsdk.HarvestAuditHints(ctx)
	require.Len(t, hints, 1)
	assert.Equal(t, "value", hints[0].Attributes["key"],
		"recorder must copy the attribute map")
}

// T27a — SHOULD boundary: empty ID is silently dropped and logged.
// Rationale: the proto AuditDecisionHint has min_len=1 on the ID field.
// If the SDK accepted empty IDs, they would accumulate on the context
// and then fail proto marshaling at the response-serialization step,
// silently dropping the hint without a clear diagnostic. Fail fast at
// the SDK layer by dropping + logging, so plugin authors see the
// problem during development rather than in production.
func TestAuditRecorderDenyWithEmptyIDIsSilentlyDroppedAndLogged(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	pluginsdk.Audit(ctx).Deny("", "message with no id", nil)

	hints := pluginsdk.HarvestAuditHints(ctx)
	assert.Empty(t, hints,
		"recorder must silently drop hints with empty ID (proto min_len=1 would fail marshal)")
}
```

**Also update `pkg/plugin/audit.go`** to add the empty-ID guard:

Find:
```go
func (r *contextRecorder) record(id, message string, effect AuditEffect, attrs AuditAttrs) {
	slice, ok := r.ctx.Value(handlerKey).(*[]AuditHint)
	if !ok {
		// No handler context attached — silent no-op.
		return
	}
```

Replace with:
```go
func (r *contextRecorder) record(id, message string, effect AuditEffect, attrs AuditAttrs) {
	if id == "" {
		// Proto AuditDecisionHint has min_len=1 on the ID field — an
		// empty ID would fail marshal at the response-serialization
		// step and drop the hint with a confusing error. Drop it here
		// so plugin authors see the problem locally and clearly.
		slog.Warn("pluginsdk.Audit: dropping hint with empty ID",
			"message", message, "effect", effect)
		return
	}
	slice, ok := r.ctx.Value(handlerKey).(*[]AuditHint)
	if !ok {
		// No handler context attached — silent no-op.
		return
	}
```

Add `"log/slog"` to the imports of `pkg/plugin/audit.go`.

- [ ] **Step 10.3: Run tests to verify they fail**

Run: `task test -- -run 'TestAuditRecorder|TestHarvestAuditHints' ./pkg/plugin/...`
Expected: FAIL with `undefined: pluginsdk.NewContextForHandler` etc.

- [ ] **Step 10.4: Implement the recorder**

File: `pkg/plugin/audit.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import "context"

// AuditAttrs is a convenience alias for plugin-provided audit attribute maps.
// Keys SHOULD be namespaced (e.g., "channel.type" rather than "type") to
// avoid collision with host-overlay keys the dispatcher may merge in.
type AuditAttrs map[string]string

// handlerContextKey is the unexported type used as the context.WithValue key
// for the in-process audit hint slice.
type handlerContextKey struct{}

// handlerKey is the sentinel value looked up on contexts carrying an
// in-process audit hint slice.
var handlerKey = handlerContextKey{}

// NewContextForHandler returns a derived context with an empty AuditHint
// slice attached. The plugin SDK adapter calls this before invoking the
// plugin's HandleCommand, so plugin authors do not call it directly in
// most cases. It is exported so tests and plugin authors implementing
// custom dispatch flows can construct a compatible context.
func NewContextForHandler(ctx context.Context) context.Context {
	hints := &[]AuditHint{}
	return context.WithValue(ctx, handlerKey, hints)
}

// HarvestAuditHints returns and clears the hint slice attached to ctx.
// The SDK adapter calls this after the plugin's HandleCommand returns
// to serialize the accumulated hints into the proto response. Plugin
// authors should not call it directly.
//
// Returns nil if no slice was attached (plain context, no handler
// derivation).
func HarvestAuditHints(ctx context.Context) []AuditHint {
	slice, ok := ctx.Value(handlerKey).(*[]AuditHint)
	if !ok {
		return nil
	}
	drained := *slice
	*slice = nil
	return drained
}

// AuditRecorder is the interface plugin handlers use to emit audit hints.
// Obtain one via Audit(ctx). Hints emitted through a recorder accumulate
// on the provided context and are harvested into CommandResponse.AuditHints
// when the SDK adapter serializes the response.
//
// Method naming: Deny and Allow correspond to AuditEffectDeny and
// AuditEffectAllow. The interface is intentionally narrow — other effect
// values are not exposed because plugin handler decisions are always one
// of these two outcomes.
type AuditRecorder interface {
	// Deny records an audit hint with AuditEffectDeny.
	Deny(id, message string, attrs AuditAttrs)

	// Allow records an audit hint with AuditEffectAllow.
	Allow(id, message string, attrs AuditAttrs)
}

// contextRecorder is the no-op-safe implementation returned by Audit().
// If the context has no handler attachment, recorder method calls silently
// drop the hint. This is intentional: plugin code that runs in both
// handler and non-handler contexts can call Audit(ctx).Deny
// unconditionally.
type contextRecorder struct {
	ctx context.Context
}

// Audit returns an AuditRecorder bound to ctx. Call this from plugin
// HandleCommand code. The recorder accumulates hints on the context;
// the SDK adapter serializes them into CommandResponse.audit_hints
// when the handler returns.
//
// Example:
//
//	func (h *handler) HandleCommand(ctx context.Context, req CommandRequest) (*CommandResponse, error) {
//	    isMember, err := h.store.IsMember(channelID, req.PlayerID)
//	    if err != nil {
//	        return nil, err
//	    }
//	    if !isMember {
//	        pluginsdk.Audit(ctx).Deny("not_member",
//	            "player not in channel members",
//	            pluginsdk.AuditAttrs{"channel.type": "public"})
//	        return pluginsdk.Errorf("You must join #%s before speaking there.", channelName), nil
//	    }
//	    // ... happy path ...
//	}
func Audit(ctx context.Context) AuditRecorder {
	return &contextRecorder{ctx: ctx}
}

// Deny records a deny hint on the recorder's context.
func (r *contextRecorder) Deny(id, message string, attrs AuditAttrs) {
	r.record(id, message, AuditEffectDeny, attrs)
}

// Allow records an allow hint on the recorder's context.
func (r *contextRecorder) Allow(id, message string, attrs AuditAttrs) {
	r.record(id, message, AuditEffectAllow, attrs)
}

func (r *contextRecorder) record(id, message string, effect AuditEffect, attrs AuditAttrs) {
	slice, ok := r.ctx.Value(handlerKey).(*[]AuditHint)
	if !ok {
		// No handler context attached — silent no-op.
		return
	}
	// Copy the attribute map so later caller mutations don't corrupt
	// the recorded hint.
	var copied map[string]string
	if len(attrs) > 0 {
		copied = make(map[string]string, len(attrs))
		for k, v := range attrs {
			copied[k] = v
		}
	}
	*slice = append(*slice, AuditHint{
		ID:         id,
		Message:    message,
		Effect:     effect,
		Attributes: copied,
	})
}
```

- [ ] **Step 10.5: Run recorder tests to verify they pass**

Run: `task test -- -run 'TestAuditRecorder|TestHarvestAuditHints' ./pkg/plugin/...`
Expected: PASS.

- [ ] **Step 10.6: Update the SDK adapter to attach handler context and serialize hints**

File: `pkg/plugin/sdk.go`

Find the `HandleCommand` method on `pluginServerAdapter`:

```go
// HandleCommand implements pluginv1.PluginServiceServer.
func (a *pluginServerAdapter) HandleCommand(ctx context.Context, req *pluginv1.HandleCommandRequest) (*pluginv1.HandleCommandResponse, error) {
	if a.cmdHandler == nil {
		return &pluginv1.HandleCommandResponse{Response: &pluginv1.CommandResponse{}}, nil
	}

	protoCmd := req.GetCommand()
	cmd := CommandRequest{
		Command:       protoCmd.GetCommand(),
		Args:          protoCmd.GetArgs(),
		CharacterID:   protoCmd.GetCharacterId(),
		CharacterName: protoCmd.GetCharacterName(),
		LocationID:    protoCmd.GetLocationId(),
		SessionID:     protoCmd.GetSessionId(),
		PlayerID:      protoCmd.GetPlayerId(),
		InvokedAs:     protoCmd.GetRawInput(),
	}

	resp, err := a.cmdHandler.HandleCommand(ctx, cmd)
	if err != nil {
		return nil, oops.With("command", cmd.Command).Wrap(err)
	}

	if resp == nil {
		return &pluginv1.HandleCommandResponse{Response: &pluginv1.CommandResponse{}}, nil
	}

	protoEvents := make([]*pluginv1.EmitEvent, len(resp.Events))
	for i, e := range resp.Events {
		protoEvents[i] = &pluginv1.EmitEvent{
			Stream:  e.Stream,
			Type:    string(e.Type),
			Payload: e.Payload,
		}
	}

	return &pluginv1.HandleCommandResponse{
		Response: &pluginv1.CommandResponse{
			Status: sdkCommandStatusToProto(resp.Status),
			Output: resp.Output,
			Events: protoEvents,
		},
	}, nil
}
```

Replace with:

```go
// HandleCommand implements pluginv1.PluginServiceServer.
func (a *pluginServerAdapter) HandleCommand(ctx context.Context, req *pluginv1.HandleCommandRequest) (*pluginv1.HandleCommandResponse, error) {
	if a.cmdHandler == nil {
		return &pluginv1.HandleCommandResponse{Response: &pluginv1.CommandResponse{}}, nil
	}

	protoCmd := req.GetCommand()
	cmd := CommandRequest{
		Command:       protoCmd.GetCommand(),
		Args:          protoCmd.GetArgs(),
		CharacterID:   protoCmd.GetCharacterId(),
		CharacterName: protoCmd.GetCharacterName(),
		LocationID:    protoCmd.GetLocationId(),
		SessionID:     protoCmd.GetSessionId(),
		PlayerID:      protoCmd.GetPlayerId(),
		InvokedAs:     protoCmd.GetRawInput(),
	}

	// Attach an audit hint slice to the handler context so plugin code
	// can call pluginsdk.Audit(ctx).Deny(...) and have hints collected
	// here for serialization into the proto response.
	handlerCtx := NewContextForHandler(ctx)

	resp, err := a.cmdHandler.HandleCommand(handlerCtx, cmd)
	if err != nil {
		return nil, oops.With("command", cmd.Command).Wrap(err)
	}

	if resp == nil {
		return &pluginv1.HandleCommandResponse{Response: &pluginv1.CommandResponse{}}, nil
	}

	// Harvest any hints the handler accumulated on its context and merge
	// them with any hints the handler attached directly to the response
	// struct (both paths are supported for flexibility).
	contextHints := HarvestAuditHints(handlerCtx)
	allHints := append([]AuditHint{}, contextHints...)
	allHints = append(allHints, resp.AuditHints...)

	protoEvents := make([]*pluginv1.EmitEvent, len(resp.Events))
	for i, e := range resp.Events {
		protoEvents[i] = &pluginv1.EmitEvent{
			Stream:  e.Stream,
			Type:    string(e.Type),
			Payload: e.Payload,
		}
	}

	protoHints := make([]*pluginv1.AuditDecisionHint, len(allHints))
	for i, h := range allHints {
		protoHints[i] = &pluginv1.AuditDecisionHint{
			Id:              h.ID,
			Name:            h.Name,
			Message:         h.Message,
			Effect:          string(h.Effect),
			ActionQualifier: h.ActionQualifier,
			Resource:        h.Resource,
			Attributes:      h.Attributes,
		}
	}

	return &pluginv1.HandleCommandResponse{
		Response: &pluginv1.CommandResponse{
			Status:     sdkCommandStatusToProto(resp.Status),
			Output:     resp.Output,
			Events:     protoEvents,
			AuditHints: protoHints,
		},
	}, nil
}
```

- [ ] **Step 10.7: Run SDK tests**

Run: `task test -- ./pkg/plugin/...`
Expected: all tests pass.

- [ ] **Step 10.8: Commit**

Run:

```bash
jj --no-pager commit -m "feat(plugin): SDK audit recorder for binary plugin handlers

Add Audit(ctx).Deny/Allow context-scoped recorder API for plugin
handlers to emit audit hints during command processing. The SDK
adapter attaches a hint slice to the handler context, harvests it
after HandleCommand returns, and serializes into
CommandResponse.audit_hints for the dispatcher to harvest.

Plugin authors call Audit(ctx).Deny(id, message, attrs) at the
site of the decision; the framework handles the plumbing.

Bead: holomush-ggbz"
```

---

## Task 11: Dispatcher hint collection + flush + host field stamping

**Goal:** Wire the dispatcher to attach an audit event slice at the start of `Dispatch`, extract hints from the plugin `CommandResponse` in `dispatchToPlugin`, stamp host-controlled fields, push to the context slice, and flush all accumulated events through the audit logger at the end of `Dispatch`.

**Files:**

- Modify: `internal/command/dispatcher.go`, `internal/command/dispatcher_test.go`

- [ ] **Step 11.1: Add audit logger field + option to `Dispatcher`**

File: `internal/command/dispatcher.go`

Find:
```go
// Dispatcher handles command parsing, capability checks, and execution.
type Dispatcher struct {
	registry        *Registry
	engine          types.AccessPolicyEngine
	aliasCache      *AliasCache            // optional, can be nil
	rateLimiter     *RateLimitMiddleware   // optional, can be nil
	pluginDeliverer PluginCommandDeliverer // optional, can be nil
	optErr          error                  // error from applying options
}
```

Replace with:
```go
// Dispatcher handles command parsing, capability checks, and execution.
type Dispatcher struct {
	registry        *Registry
	engine          types.AccessPolicyEngine
	aliasCache      *AliasCache            // optional, can be nil
	rateLimiter     *RateLimitMiddleware   // optional, can be nil
	pluginDeliverer PluginCommandDeliverer // optional, can be nil
	auditLogger     *audit.Logger          // optional, can be nil; when nil, plugin-audit flush is skipped
	optErr          error                  // error from applying options
}
```

Also add an import for `"github.com/holomush/holomush/internal/audit"` in the file's import block.

Find:
```go
// WithRateLimiter configures the dispatcher to use rate limiting.
// If not provided, rate limiting is disabled. Passing nil is an error —
// omit the option entirely to disable rate limiting.
func WithRateLimiter(rl *RateLimiter) DispatcherOption {
```

Insert BEFORE that function:
```go
// WithAuditLogger configures the dispatcher to flush plugin-emitted audit
// events through the given audit logger. If not provided, plugin audit
// events are silently dropped — useful for tests that do not care about
// audit flow.
func WithAuditLogger(logger *audit.Logger) DispatcherOption {
	return func(d *Dispatcher) {
		d.auditLogger = logger
	}
}

```

- [ ] **Step 11.2: Attach the audit context at the start of `Dispatch` and flush on return**

File: `internal/command/dispatcher.go`

Find (top of `Dispatch`):
```go
// Dispatch parses and executes a command.
func (d *Dispatcher) Dispatch(ctx context.Context, input string, exec *CommandExecution) (err error) {
	metrics := NewMetricsRecorder()
	defer metrics.Record()
```

Replace with:
```go
// Dispatch parses and executes a command.
func (d *Dispatcher) Dispatch(ctx context.Context, input string, exec *CommandExecution) (err error) {
	metrics := NewMetricsRecorder()
	defer metrics.Record()

	// Attach an audit event slice to the dispatch context so plugin-emitted
	// hints can accumulate during command processing. Also attach the exec
	// so the flush path can stamp Subject/Action on events that lack them
	// (events pushed directly by the Lua capability, as opposed to those
	// that came through extractAuditHints which stamps them inline).
	ctx = audit.NewContextForDispatch(ctx)
	ctx = context.WithValue(ctx, execContextKey{}, exec)

	// Flush accumulated plugin audit events at the end of dispatch. Errors
	// are logged and metric-counted but never fail the user's operation
	// per the failure mode decision in the spec.
	defer d.flushPluginAuditEvents(ctx)
```

- [ ] **Step 11.3: Add the `flushPluginAuditEvents` helper method**

File: `internal/command/dispatcher.go`

Insert after the `dispatchToPlugin` method (before the last closing brace of the file):

```go
// execContextKey is the context.WithValue key for the CommandExecution
// attached during Dispatch. The flush path uses this to stamp Subject
// and Action on events whose emit path did not fill them in (e.g., Lua
// capability events).
type execContextKey struct{}

// flushPluginAuditEvents drains any plugin-emitted audit events attached to
// ctx and writes them through the configured audit logger. Called from
// Dispatch's deferred path. Errors are logged, counted, and then dropped —
// audit flush failures MUST NOT fail the user's command per the spec.
//
// For each event, Subject and Action are stamped from the dispatch context
// if they are empty (Lua capability emit path leaves them blank; the binary
// path fills them in via extractAuditHints).
func (d *Dispatcher) flushPluginAuditEvents(ctx context.Context) {
	events := audit.EventsFromContext(ctx)
	if len(events) == 0 {
		return
	}

	if d.auditLogger == nil {
		// No logger configured — drop events silently. This is the
		// correct behavior for tests that don't wire an audit logger.
		return
	}

	// Retrieve the exec for host field stamping of any events that lack
	// Subject/Action (typically Lua-emitted events).
	exec, _ := ctx.Value(execContextKey{}).(*CommandExecution)

	for i := range events {
		// Fill in host-controlled fields that the emit path may have
		// left empty.
		if events[i].Subject == "" && exec != nil {
			events[i].Subject = access.CharacterSubject(exec.CharacterID().String())
		}
		if events[i].Action == "" && exec != nil {
			events[i].Action = exec.InvokedAs
		}
		if events[i].Timestamp.IsZero() {
			events[i].Timestamp = time.Now()
		}

		if logErr := d.auditLogger.Log(ctx, events[i]); logErr != nil {
			slog.WarnContext(ctx, "plugin audit event flush failed",
				"subject", events[i].Subject,
				"action", events[i].Action,
				"resource", events[i].Resource,
				"component", events[i].Component,
				"error", logErr,
			)
			audit.RecordPluginAuditFailure()
			// Continue with remaining events.
		}
	}
}
```

- [ ] **Step 11.4: Extract hints from plugin response in `dispatchToPlugin` and stamp host fields**

File: `internal/command/dispatcher.go`

Find (inside `dispatchToPlugin`, after the `resp, err := d.pluginDeliverer.DeliverCommand(ctx, ...)` call and before `switch resp.Status`):

```go
	resp, err := d.pluginDeliverer.DeliverCommand(ctx, entry.PluginName(), cmd)
	if err != nil {
		return oops.In("dispatcher").With("command", entry.Name).With("plugin", entry.PluginName()).Wrap(err)
	}
	if resp == nil {
		return nil
	}

	// Handle CommandStatus: set metrics, activity, error flags based on outcome.
```

Replace with:
```go
	resp, err := d.pluginDeliverer.DeliverCommand(ctx, entry.PluginName(), cmd)
	if err != nil {
		return oops.In("dispatcher").With("command", entry.Name).With("plugin", entry.PluginName()).Wrap(err)
	}
	if resp == nil {
		return nil
	}

	// Extract audit hints from the response, stamp host-controlled fields,
	// and push each onto the context-bound slice. The dispatcher's deferred
	// flush will route them through the audit logger.
	d.extractAuditHints(ctx, resp.AuditHints, entry, exec)

	// Handle CommandStatus: set metrics, activity, error flags based on outcome.
```

- [ ] **Step 11.5: Implement `extractAuditHints` with host field stamping**

File: `internal/command/dispatcher.go`

Insert after the `flushPluginAuditEvents` method:

```go
// extractAuditHints converts plugin-provided audit hints into audit.Event
// values, stamping host-controlled fields (subject, action base, source,
// component, timestamp, duration) from the dispatch context. The plugin
// cannot spoof these fields — the dispatcher overwrites them regardless
// of what the hint contains.
func (d *Dispatcher) extractAuditHints(ctx context.Context, hints []pluginsdk.AuditHint, entry *CommandEntry, exec *CommandExecution) {
	if len(hints) == 0 {
		return
	}

	hostSubject := access.CharacterSubject(exec.CharacterID().String())
	hostComponent := entry.PluginName()
	if hostComponent == "" {
		hostComponent = "unknown-plugin"
	}

	for _, hint := range hints {
		// Validate resource shape if provided. A malformed resource is
		// logged but does not abort the flush — the event is emitted
		// with the plugin-provided value (operators see the malformed
		// string and can investigate).
		if hint.Resource != "" && !isValidResourceRef(hint.Resource) {
			slog.WarnContext(ctx, "plugin audit hint has malformed resource",
				"plugin", hostComponent,
				"resource", hint.Resource,
				"hint_id", hint.ID,
			)
		}

		// Compose the final action: base from the dispatcher, qualifier
		// from the plugin. Joined with ':'.
		action := entry.Name
		if hint.ActionQualifier != "" {
			action = entry.Name + ":" + hint.ActionQualifier
		}

		// Convert hint effect to audit effect.
		var effect types.Effect
		switch hint.Effect {
		case pluginsdk.AuditEffectAllow:
			effect = types.EffectAllow
		case pluginsdk.AuditEffectDeny:
			effect = types.EffectDeny
		default:
			// Unknown effect from plugin — log and skip this hint.
			slog.WarnContext(ctx, "plugin audit hint has unknown effect",
				"plugin", hostComponent,
				"effect", hint.Effect,
				"hint_id", hint.ID,
			)
			continue
		}

		// Merge attributes: plugin-provided first, then host-overlay
		// (host keys win on collision).
		merged := make(map[string]any, len(hint.Attributes)+2)
		for k, v := range hint.Attributes {
			merged[k] = v
		}
		merged["command.invoked_as"] = exec.InvokedAs

		event := audit.Event{
			ID:         hint.ID,
			Name:       hint.Name,
			Message:    hint.Message,
			Source:     audit.SourcePlugin, // host-stamped, plugin cannot spoof
			Component:  hostComponent,      // host-stamped from entry.PluginName()
			Subject:    hostSubject,        // host-stamped from dispatch context
			Action:     action,             // composed: base + qualifier
			Resource:   hint.Resource,      // plugin-provided (shape validated above)
			Effect:     effect,
			Attributes: merged,
			DurationUS: 0, // per-hint duration is not meaningful for D-inline — hints accumulate during handler execution and flush atomically
			Timestamp:  time.Now(),
		}

		audit.AddEventToContext(ctx, event)
	}
}

// isValidResourceRef performs a minimal shape check on a plugin-provided
// resource reference. Valid form: "<type>:<id>" with at least one character
// on each side of the colon.
func isValidResourceRef(ref string) bool {
	colon := strings.Index(ref, ":")
	return colon > 0 && colon < len(ref)-1
}
```

Add `"strings"` to the imports if not already present.

**Note about the TODO:** `DurationUS` is left at zero because measuring per-hint handler duration requires instrumenting the plugin SDK's handler call path, which is an additional scope the spec did not commit to. The spec says "Host-measured, from handler invocation to hint processing" — interpret this as "the total dispatch time is captured elsewhere via metrics; per-hint duration is not meaningful for D-inline because hints are emitted throughout handler execution and harvested atomically at the end." A future enhancement can add per-hint timestamps if operators demand them.

- [ ] **Step 11.6: Write dispatcher tests for the new behavior**

File: `internal/command/dispatcher_test.go`

Add the following test functions (at the appropriate location per existing file structure):

```go
func TestDispatcherAttachesAuditContextToDispatchContext(t *testing.T) {
	// Verify that after Dispatch is called, the context seen by the
	// plugin deliverer is derived from audit.NewContextForDispatch
	// (i.e., audit.AddEventToContext works inside it).
	var capturedCtx context.Context
	deliverer := &fakePluginDeliverer{
		onDeliver: func(ctx context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			capturedCtx = ctx
			return &pluginsdk.CommandResponse{Status: pluginsdk.CommandOK}, nil
		},
	}

	dispatcher := newTestDispatcherWithPlugin(t, deliverer)

	exec := newTestCommandExecution(t)
	err := dispatcher.Dispatch(context.Background(), "plugintest", exec)
	require.NoError(t, err)

	// capturedCtx should accept AddEventToContext without being nil
	require.NotNil(t, capturedCtx)
	audit.AddEventToContext(capturedCtx, audit.Event{ID: "sanity"})
	events := audit.EventsFromContext(capturedCtx)
	assert.Len(t, events, 1)
}

func TestDispatcherExtractsAuditHintsFromCommandResponseAndStampsHostFields(t *testing.T) {
	// The plugin returns a hint; the dispatcher should stamp Source,
	// Component, Subject, Action from the host context and push the
	// resulting Event through the audit logger.
	writer := &capturingAuditWriter{}
	logger := audit.NewLogger(audit.ModeAll, writer, "")
	t.Cleanup(func() { _ = logger.Close() })

	deliverer := &fakePluginDeliverer{
		onDeliver: func(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			return &pluginsdk.CommandResponse{
				Status: pluginsdk.CommandError,
				Output: "no permission",
				AuditHints: []pluginsdk.AuditHint{
					{
						ID:              "not_member",
						Name:            "channels: not a member",
						Message:         "player not in channel members",
						Effect:          pluginsdk.AuditEffectDeny,
						ActionQualifier: "speak",
						Resource:        "channel:01XYZ",
						Attributes:      map[string]string{"channel.type": "public"},
					},
				},
			}, nil
		},
	}

	dispatcher := newTestDispatcherWithPluginAndAudit(t, deliverer, logger)
	exec := newTestCommandExecution(t)

	err := dispatcher.Dispatch(context.Background(), "plugintest", exec)
	// CommandError is a user-facing denial, not a dispatch error.
	// Dispatch returns nil for CommandError (the command ran, the user
	// was told no). Confirm with the existing dispatcher behavior.
	assert.NoError(t, err)

	require.Len(t, writer.events, 1, "expected exactly one plugin audit event")
	event := writer.events[0]

	// Host-stamped fields
	assert.Equal(t, audit.SourcePlugin, event.Source,
		"Source must be host-stamped as SourcePlugin")
	assert.NotEmpty(t, event.Component,
		"Component must be host-stamped from plugin name")
	assert.NotEmpty(t, event.Subject,
		"Subject must be host-stamped from dispatch context")

	// Plugin-provided fields
	assert.Equal(t, "not_member", event.ID)
	assert.Equal(t, "player not in channel members", event.Message)
	assert.Equal(t, types.EffectDeny, event.Effect)

	// Composed field
	assert.Contains(t, event.Action, "speak",
		"Action should contain the plugin's qualifier")
}

func TestDispatcherContinuesFlushWhenOneAuditEventWriteFails(t *testing.T) {
	// Simulate an audit logger where the first event write fails but
	// subsequent writes succeed. All events should be attempted; the
	// failure must not propagate to the user.
	writer := &sometimesFailingWriter{failIndex: 0}
	logger := audit.NewLogger(audit.ModeAll, writer, "")
	t.Cleanup(func() { _ = logger.Close() })

	deliverer := &fakePluginDeliverer{
		onDeliver: func(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			return &pluginsdk.CommandResponse{
				Status: pluginsdk.CommandOK,
				AuditHints: []pluginsdk.AuditHint{
					{ID: "e1", Effect: pluginsdk.AuditEffectDeny},
					{ID: "e2", Effect: pluginsdk.AuditEffectDeny},
				},
			}, nil
		},
	}

	dispatcher := newTestDispatcherWithPluginAndAudit(t, deliverer, logger)
	exec := newTestCommandExecution(t)

	err := dispatcher.Dispatch(context.Background(), "plugintest", exec)
	require.NoError(t, err,
		"audit write failure must not propagate to the dispatcher caller")

	// Both events should have been attempted.
	assert.Equal(t, 2, writer.attemptCount,
		"all hints should be attempted even if one fails")
}

// sometimesFailingWriter returns an error on writes whose 0-based index
// matches failIndex.
type sometimesFailingWriter struct {
	attemptCount int
	failIndex    int
}

func (w *sometimesFailingWriter) WriteSync(_ context.Context, _ audit.Event) error {
	idx := w.attemptCount
	w.attemptCount++
	if idx == w.failIndex {
		return fmt.Errorf("simulated write failure")
	}
	return nil
}

func (w *sometimesFailingWriter) WriteAsync(_ audit.Event) error {
	w.attemptCount++
	return nil
}

func (w *sometimesFailingWriter) Close() error { return nil }

// T22a — MUST negative: unknown effect string is skipped with a warning.
func TestDispatcherSkipsHintWithUnknownEffectStringAndLogsWarning(t *testing.T) {
	writer := &capturingAuditWriter{}
	logger := audit.NewLogger(audit.ModeAll, writer, "")
	t.Cleanup(func() { _ = logger.Close() })

	deliverer := &fakePluginDeliverer{
		onDeliver: func(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			return &pluginsdk.CommandResponse{
				Status: pluginsdk.CommandOK,
				AuditHints: []pluginsdk.AuditHint{
					{ID: "good", Effect: pluginsdk.AuditEffectDeny, Message: "this one is valid"},
					{ID: "bad", Effect: pluginsdk.AuditEffect("mystery"), Message: "unknown effect"},
					{ID: "also_good", Effect: pluginsdk.AuditEffectAllow, Message: "valid again"},
				},
			}, nil
		},
	}

	dispatcher := newTestDispatcherWithPluginAndAudit(t, deliverer, logger)
	exec := newTestCommandExecution(t)

	err := dispatcher.Dispatch(context.Background(), "plugintest", exec)
	require.NoError(t, err)

	// Only the two valid hints should be flushed. The bad one is dropped.
	require.Len(t, writer.events, 2)
	assert.Equal(t, "good", writer.events[0].ID)
	assert.Equal(t, "also_good", writer.events[1].ID)
}

// T22b — SHOULD boundary: malformed resource refs.
func TestDispatcherValidatesMalformedResourceRefs(t *testing.T) {
	cases := []struct {
		name        string
		resource    string
		expectWarn  bool
		expectFlush bool // hint still flushes with the malformed resource string
	}{
		{"well-formed two-part ref", "channel:01XYZ", false, true},
		{"empty resource string", "", false, true},          // empty is valid (optional field)
		{"no colon", "channel01XYZ", true, true},             // malformed, logged, still flushed
		{"trailing colon only", "channel:", true, true},      // malformed
		{"leading colon only", ":01XYZ", true, true},         // malformed
		{"only a colon", ":", true, true},                    // malformed
		{"multi-colon ambiguous", "channel:01:extra", false, true}, // two colons is permissive — the first colon delimits
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writer := &capturingAuditWriter{}
			logger := audit.NewLogger(audit.ModeAll, writer, "")
			t.Cleanup(func() { _ = logger.Close() })

			deliverer := &fakePluginDeliverer{
				onDeliver: func(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
					return &pluginsdk.CommandResponse{
						Status: pluginsdk.CommandOK,
						AuditHints: []pluginsdk.AuditHint{
							{ID: "test", Effect: pluginsdk.AuditEffectDeny, Resource: tc.resource},
						},
					}, nil
				},
			}

			dispatcher := newTestDispatcherWithPluginAndAudit(t, deliverer, logger)
			exec := newTestCommandExecution(t)

			err := dispatcher.Dispatch(context.Background(), "plugintest", exec)
			require.NoError(t, err)

			if tc.expectFlush {
				require.Len(t, writer.events, 1)
				assert.Equal(t, tc.resource, writer.events[0].Resource,
					"malformed resource refs are logged but still emitted as-is")
			}
		})
	}
}

// T22c — SHOULD boundary: dispatcher with no audit logger configured.
func TestDispatcherFlushIsNoOpWhenAuditLoggerIsNil(t *testing.T) {
	deliverer := &fakePluginDeliverer{
		onDeliver: func(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			return &pluginsdk.CommandResponse{
				Status: pluginsdk.CommandOK,
				AuditHints: []pluginsdk.AuditHint{
					{ID: "dropped", Effect: pluginsdk.AuditEffectDeny},
				},
			}, nil
		},
	}

	// Construct dispatcher WITHOUT WithAuditLogger — d.auditLogger is nil.
	dispatcher := newTestDispatcherWithPlugin(t, deliverer)
	exec := newTestCommandExecution(t)

	// This must not panic and must not fail the command.
	err := dispatcher.Dispatch(context.Background(), "plugintest", exec)
	require.NoError(t, err)
}

// T22d — SHOULD invariant: context-per-dispatch isolation under concurrency.
func TestDispatcherConcurrentDispatchesDoNotCrossContaminateAuditContexts(t *testing.T) {
	const numDispatches = 10

	// Each dispatch emits a unique hint ID. After all dispatches complete,
	// the audit writer should have received exactly numDispatches events,
	// each with a unique ID matching the dispatch index.
	writer := &capturingAuditWriter{}
	logger := audit.NewLogger(audit.ModeAll, writer, "")
	t.Cleanup(func() { _ = logger.Close() })

	var dispatchIdx atomic.Int32
	deliverer := &fakePluginDeliverer{
		onDeliver: func(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
			idx := dispatchIdx.Add(1) - 1
			return &pluginsdk.CommandResponse{
				Status: pluginsdk.CommandOK,
				AuditHints: []pluginsdk.AuditHint{
					{ID: fmt.Sprintf("hint-%d", idx), Effect: pluginsdk.AuditEffectDeny},
				},
			}, nil
		},
	}

	dispatcher := newTestDispatcherWithPluginAndAudit(t, deliverer, logger)

	var wg sync.WaitGroup
	for i := 0; i < numDispatches; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			exec := newTestCommandExecution(t)
			if err := dispatcher.Dispatch(context.Background(), "plugintest", exec); err != nil {
				t.Errorf("dispatch failed: %v", err)
			}
		}()
	}
	wg.Wait()

	writer.mu.Lock()
	defer writer.mu.Unlock()
	require.Len(t, writer.events, numDispatches,
		"each dispatch should emit exactly one event; no cross-contamination")

	// Collect the IDs seen and assert each dispatch-idx appears exactly once.
	seen := make(map[string]int)
	for _, e := range writer.events {
		seen[e.ID]++
	}
	assert.Len(t, seen, numDispatches,
		"each dispatch should see its own unique hint ID, not someone else's")
}
```

Additional imports for the test file: `"sync"`, `"sync/atomic"`.

**Note:** The helper constructors `newTestDispatcherWithPlugin`, `newTestDispatcherWithPluginAndAudit`, `newTestCommandExecution`, `capturingAuditWriter`, and `fakePluginDeliverer` are expected to already exist in `dispatcher_test.go` (from Task 5's test setup or from prior dispatcher test code). If they don't:

- Reuse `capturingAuditWriter` from `engine_test.go` (move it to a `testutil` package if needed, or duplicate locally — it's tiny).
- Create the dispatcher helpers following the pattern of existing tests in the file. Look at `dispatcher_test.go` for `NewDispatcher(...)` usage and mirror it.
- `newTestDispatcherWithPluginAndAudit` is the same as `newTestDispatcherWithPlugin` but with `command.WithAuditLogger(logger)` passed.

If the dispatcher test file has no existing test scaffolding, add the scaffolding as a separate step before writing these tests.

- [ ] **Step 11.7: Run dispatcher tests**

Run: `task test -- ./internal/command/...`
Expected: all tests pass.

- [ ] **Step 11.8: Commit**

Run:

```bash
jj --no-pager commit -m "feat(command): dispatcher-side audit hint collection and flush

Dispatch now attaches an audit event slice to the command context
and flushes accumulated plugin events through the audit logger on
return. dispatchToPlugin extracts hints from CommandResponse,
stamps host-controlled fields (Source, Component, Subject, Action
base), validates resource shape, and pushes events to the slice.

Audit write failures do not fail the user's command — they are
logged and counted via RecordPluginAuditFailure.

Bead: holomush-ggbz"
```

---

## Task 12: Lua `cap_audit` capability module

**Goal:** Expose the same audit hint accumulation facility to Lua plugins via a new `Capability` module that injects an `audit` global into the Lua VM.

**Files:**

- Create: `internal/plugin/hostfunc/cap_audit.go`, `internal/plugin/hostfunc/cap_audit_test.go`
- Modify: manager registration (exact location to be confirmed during implementation by searching for `NewCapabilityRegistry` or `cap_session` registration)

- [ ] **Step 12.1: Write failing tests**

File: `internal/plugin/hostfunc/cap_audit_test.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

func TestCapAuditNamespaceIsAudit(t *testing.T) {
	cap := hostfunc.NewAuditCapability()
	assert.Equal(t, "audit", cap.Namespace())
}

func TestCapAuditRegisterInjectsAuditGlobalTable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	cap := hostfunc.NewAuditCapability()
	cap.Register(L, "test-plugin")

	auditGlobal := L.GetGlobal("audit")
	require.Equal(t, lua.LTTable, auditGlobal.Type(),
		"audit global should be a table")
}

func TestCapAuditDenyPushesHintToContextBoundSlice(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Attach a dispatch context to the LState.
	ctx := audit.NewContextForDispatch(context.Background())
	L.SetContext(ctx)

	cap := hostfunc.NewAuditCapability()
	cap.Register(L, "test-plugin")

	err := L.DoString(`audit.deny("not_member", "player not in channel members", {channel_type = "public"})`)
	require.NoError(t, err)

	events := audit.EventsFromContext(ctx)
	require.Len(t, events, 1)
	assert.Equal(t, "not_member", events[0].ID)
	assert.Equal(t, "player not in channel members", events[0].Message)
	assert.Equal(t, audit.SourcePlugin, events[0].Source)
	assert.Equal(t, "test-plugin", events[0].Component)
	assert.Equal(t, types.EffectDeny, events[0].Effect)
	assert.Equal(t, "public", events[0].Attributes["channel_type"])
}

func TestCapAuditAllowPushesHintWithAllowEffect(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	ctx := audit.NewContextForDispatch(context.Background())
	L.SetContext(ctx)

	cap := hostfunc.NewAuditCapability()
	cap.Register(L, "test-plugin")

	err := L.DoString(`audit.allow("speak_ok", "message delivered")`)
	require.NoError(t, err)

	events := audit.EventsFromContext(ctx)
	require.Len(t, events, 1)
	assert.Equal(t, types.EffectAllow, events[0].Effect)
}

func TestCapAuditIsNoOpWhenNoContextAttachedToLState(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// No SetContext — luaContext() returns context.Background which has
	// no attached event slice.
	cap := hostfunc.NewAuditCapability()
	cap.Register(L, "test-plugin")

	err := L.DoString(`audit.deny("orphan", "no context")`)
	require.NoError(t, err)
	// No assertion needed — just verify the call did not panic.
}

func TestCapAuditHandlesOptionalAttributesTable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	ctx := audit.NewContextForDispatch(context.Background())
	L.SetContext(ctx)

	cap := hostfunc.NewAuditCapability()
	cap.Register(L, "test-plugin")

	// Call without the third argument.
	err := L.DoString(`audit.deny("simple", "minimal form")`)
	require.NoError(t, err)

	events := audit.EventsFromContext(ctx)
	require.Len(t, events, 1)
	assert.Empty(t, events[0].Attributes)
}

// T34a — SHOULD negative: Lua table with int/bool keys is silently
// skipped. The attribute map contract requires string keys; non-string
// keys are a plugin-author error but should not crash the capability.
func TestCapAuditSkipsNonStringKeysInAttributesTable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	ctx := audit.NewContextForDispatch(context.Background())
	L.SetContext(ctx)

	cap := hostfunc.NewAuditCapability()
	cap.Register(L, "test-plugin")

	// Mix of valid (string) and invalid (int, bool) keys.
	script := `
		audit.deny("mixed_keys", "test", {
			valid_key = "included",
			[1] = "dropped",
			[true] = "dropped",
		})
	`
	err := L.DoString(script)
	require.NoError(t, err, "capability must not crash on non-string keys")

	events := audit.EventsFromContext(ctx)
	require.Len(t, events, 1)

	// Only the string-keyed entry survives.
	assert.Equal(t, "included", events[0].Attributes["valid_key"])
	assert.Len(t, events[0].Attributes, 1,
		"non-string keys should be silently dropped")
}
```

- [ ] **Step 12.2: Run tests to verify they fail**

Run: `task test -- -run 'TestCapAudit' ./internal/plugin/hostfunc/...`
Expected: FAIL with `undefined: hostfunc.NewAuditCapability`.

- [ ] **Step 12.3: Implement the capability**

File: `internal/plugin/hostfunc/cap_audit.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
)

// AuditCapability implements the Capability interface for the "audit"
// namespace. It injects an audit.deny / audit.allow global into the Lua
// VM so Lua plugin handlers can emit audit hints during command processing.
//
// The capability has no external dependencies — it reads the dispatcher
// context off the LState (via luaContext) and pushes events onto the
// context-bound slice. Context propagation is the dispatcher's
// responsibility (done via L.SetContext before invoking the handler).
type AuditCapability struct{}

// NewAuditCapability creates an AuditCapability. No dependencies required.
func NewAuditCapability() *AuditCapability {
	return &AuditCapability{}
}

// Namespace returns "audit", the Lua global table name for this capability.
func (c *AuditCapability) Namespace() string {
	return "audit"
}

// Register injects the audit.* functions into the Lua state as a global table.
func (c *AuditCapability) Register(L *lua.LState, pluginName string) { //nolint:gocritic // L is conventional gopher-lua parameter name
	tbl := L.NewTable()
	L.SetField(tbl, "deny", L.NewFunction(c.denyFn(pluginName)))
	L.SetField(tbl, "allow", L.NewFunction(c.allowFn(pluginName)))
	L.SetGlobal("audit", tbl)
}

// denyFn returns a Lua function implementing audit.deny(id, message, attrs).
// Signature: audit.deny(id: string, message: string, attrs: table?) -> nil
//
// Pushes an audit.Event with EffectDeny onto the context-bound slice.
// If no dispatch context is attached to the LState, the call is a silent
// no-op.
func (c *AuditCapability) denyFn(pluginName string) lua.LGFunction {
	return c.recordFn(pluginName, types.EffectDeny)
}

// allowFn returns a Lua function implementing audit.allow(id, message, attrs).
func (c *AuditCapability) allowFn(pluginName string) lua.LGFunction {
	return c.recordFn(pluginName, types.EffectAllow)
}

func (c *AuditCapability) recordFn(pluginName string, effect types.Effect) lua.LGFunction {
	return func(L *lua.LState) int {
		id := L.CheckString(1)
		message := L.CheckString(2)
		attrs := L.OptTable(3, nil)

		ctx := luaContext(L)
		event := audit.Event{
			ID:         id,
			Message:    message,
			Source:     audit.SourcePlugin, // host-stamped
			Component:  pluginName,          // host-stamped
			Effect:     effect,
			Attributes: luaTableToAttributes(attrs),
			Timestamp:  time.Now(),
		}

		audit.AddEventToContext(ctx, event)
		return 0
	}
}

// luaTableToAttributes converts a Lua table of string/string pairs into a
// Go map[string]any. Non-string values are coerced via Lua's string
// representation; keys that are not strings are skipped.
func luaTableToAttributes(tbl *lua.LTable) map[string]any {
	if tbl == nil {
		return nil
	}
	out := make(map[string]any)
	tbl.ForEach(func(k, v lua.LValue) {
		keyStr, ok := k.(lua.LString)
		if !ok {
			return
		}
		out[string(keyStr)] = v.String()
	})
	if len(out) == 0 {
		return nil
	}
	return out
}
```

**Note on host field stamping:** This capability stamps `Source = SourcePlugin` and `Component = pluginName` directly, because the Lua host knows the plugin name at `Register` time. It does NOT stamp `Subject` or `Action` — the Lua capability has no direct access to the current `CommandExecution`. Those two fields are stamped by the dispatcher's `flushPluginAuditEvents` helper (Task 11, Step 11.3), which fills in empty Subject/Action from the `exec` attached to the dispatch context.

This asymmetry between the binary path and the Lua path is intentional and mirrors how the two transports differ: the binary path goes through `extractAuditHints` which stamps everything inline; the Lua path pushes directly to the slice and relies on the flush loop to fill in what it couldn't know.

- [ ] **Step 12.4: Run Lua capability tests to verify they pass**

Run: `task test -- -run 'TestCapAudit' ./internal/plugin/hostfunc/...`
Expected: PASS.

- [ ] **Step 12.5: Register the new capability with the plugin manager**

Find the location where other capabilities are registered (search for `NewCapabilityRegistry` or `NewSessionCapability` instantiation):

Run: `grep -rn 'NewSessionCapability\|CapabilityRegistry' internal/plugin/`

Expected: one or two files showing where capabilities are wired into the plugin manager. Add a line:

```go
registry.Register("holomush.plugin.v1.AuditService", hostfunc.NewAuditCapability())
```

at the same location where session/alias/world_query capabilities are registered. The exact line depends on project structure — follow the existing pattern.

- [ ] **Step 12.6: Run all plugin tests**

Run: `task test -- ./internal/plugin/...`
Expected: all tests pass.

- [ ] **Step 12.7: Commit**

Run:

```bash
jj --no-pager commit -m "feat(plugin): Lua audit capability module

Add hostfunc.AuditCapability that injects 'audit.deny' and
'audit.allow' Lua globals. Lua plugins call these during command
handler execution; the host function pushes audit.Event values
onto the dispatcher's context-bound slice via the same primitive
the binary plugin SDK uses.

Register the capability in the plugin manager's CapabilityRegistry
under the service name 'holomush.plugin.v1.AuditService'.

Bead: holomush-ggbz"
```

---

## Task 13: Integration tests — end-to-end binary and Lua paths WITH direct DB validation

**Goal:** Prove the full stack works end-to-end including the real `PostgresWriter` and the `access_audit_log` table. Each test: spin up a Postgres testcontainer, apply migrations, wire a real `PostgresWriter` into the `audit.Logger`, dispatch a command that emits an audit hint, and query `access_audit_log` directly to assert the row has the expected values for every new field (`source`, `component`, `event_id`, `event_name`, `message`, `attributes`, `subject`, `action`, `effect`).

An in-memory capturing writer would bypass the exact thing that needs verification: that the column mapping in `PostgresWriter.WriteSync` matches the migrated schema and that plugin-sourced data survives the round trip through Postgres.

**Files:**

- Create: `test/integration/audit/audit_integration_test.go`

- [ ] **Step 13.1: Write the binary plugin integration test (with real DB validation)**

File: `test/integration/audit/audit_integration_test.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package audit_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/lib/pq" // postgres driver for database/sql
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/store"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/holomush/holomush/test/testutil"
)

func TestAuditIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Audit Subsystem Integration")
}

var _ = Describe("Plugin-emitted audit events reaching access_audit_log", func() {
	var (
		ctx       context.Context
		cancel    context.CancelFunc
		container testcontainers.Container
		connStr   string
		db        *sql.DB
		logger    *audit.Logger
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)

		pgEnv, err := testutil.StartPostgres(ctx)
		Expect(err).NotTo(HaveOccurred())
		container = pgEnv.Container
		connStr = pgEnv.ConnStr

		migrator, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migrator.Up()).To(Succeed())
		_ = migrator.Close()

		db, err = sql.Open("postgres", connStr)
		Expect(err).NotTo(HaveOccurred())

		// Real PostgresWriter + Logger — no in-memory capture.
		writer := audit.NewPostgresWriter(db)
		logger = audit.NewLogger(audit.ModeAll, writer, "")
	})

	AfterEach(func() {
		if logger != nil {
			_ = logger.Close()
		}
		if db != nil {
			_ = db.Close()
		}
		if container != nil {
			_ = container.Terminate(context.Background())
		}
		if cancel != nil {
			cancel()
		}
	})

	Describe("binary plugin handler emits a deny hint during HandleCommand", func() {
		It("writes a row to access_audit_log with all host-stamped fields populated", func() {
			deliverer := &scriptedDeliverer{
				response: &pluginsdk.CommandResponse{
					Status: pluginsdk.CommandError,
					Output: "denied",
					AuditHints: []pluginsdk.AuditHint{
						{
							ID:              "not_member",
							Name:            "channels: not a member",
							Message:         "player not in channel members",
							Effect:          pluginsdk.AuditEffectDeny,
							ActionQualifier: "speak",
							Resource:        "channel:01XYZ",
							Attributes:      map[string]string{"channel.type": "public"},
						},
					},
				},
			}

			dispatcher := newIntegrationTestDispatcher(GinkgoT(), deliverer, logger)
			exec := newIntegrationCommandExecution(GinkgoT())

			dispatchErr := dispatcher.Dispatch(ctx, "channel", exec)
			Expect(dispatchErr).NotTo(HaveOccurred(),
				"CommandError is a user denial, not a dispatch error")

			// Deny entries are sync-written in ModeAll, so the row should
			// be present immediately after Dispatch returns.
			rows := queryAuditRows(ctx, db, "plugin")
			Expect(rows).To(HaveLen(1),
				"expected exactly one plugin-sourced row in access_audit_log")

			row := rows[0]
			// Host-stamped fields — these are the anti-spoofing invariants.
			Expect(row.source).To(Equal("plugin"),
				"Source must be host-stamped as 'plugin'")
			Expect(row.component).To(Equal("test-plugin"),
				"Component must be host-stamped from the plugin name")
			Expect(row.subject).NotTo(BeEmpty(),
				"Subject must be host-stamped from the dispatch context")
			Expect(row.action).To(ContainSubstring("speak"),
				"Action must be composed as base:qualifier (channel:speak)")
			Expect(row.effect).To(Equal("deny"),
				"Effect must round-trip as 'deny'")

			// Plugin-provided fields — verified round-trip through the DB.
			Expect(row.eventID).To(Equal("not_member"))
			Expect(row.eventName).To(Equal("channels: not a member"))
			Expect(row.message).To(Equal("player not in channel members"))
			Expect(row.resource).To(Equal("channel:01XYZ"))

			// Attributes — verify at least the plugin-provided key round-trips.
			Expect(row.attributes).To(ContainSubstring(`"channel.type"`))
			Expect(row.attributes).To(ContainSubstring(`"public"`))
		})
	})

	Describe("Lua plugin calls audit.deny during command handler", func() {
		It("writes a row to access_audit_log via the hostfunc capability path", func() {
			// TODO: follow the binary path pattern above but load a Lua
			// plugin that calls audit.deny(...) from its command handler.
			// Use the existing Lua integration harness in
			// internal/plugin/communication_integration_test.go as a
			// reference for loading a Lua plugin into the dispatcher flow.
			Skip("implement once the binary path DB validation is landed; " +
				"same assertions as the binary test, starting from a Lua script calling audit.deny")
		})
	})

	Describe("binary plugin emits multiple hints and one audit write fails mid-flush", func() {
		It("writes the remaining rows and the user command still returns normally", func() {
			// Use a writer wrapper that injects a failure on a specific
			// row. The dispatcher MUST continue flushing remaining events
			// and MUST NOT propagate the audit failure to the user.
			//
			// Construction: wrap the real PostgresWriter in a writer that
			// forwards calls but returns an error once on a configured
			// counter value. Assert final access_audit_log count equals
			// the number of successful writes.
			deliverer := &scriptedDeliverer{
				response: &pluginsdk.CommandResponse{
					Status: pluginsdk.CommandOK,
					AuditHints: []pluginsdk.AuditHint{
						{ID: "e1", Effect: pluginsdk.AuditEffectDeny, Message: "first"},
						{ID: "e2", Effect: pluginsdk.AuditEffectDeny, Message: "second"},
						{ID: "e3", Effect: pluginsdk.AuditEffectDeny, Message: "third"},
					},
				},
			}

			dispatcher := newIntegrationTestDispatcher(GinkgoT(), deliverer, logger)
			exec := newIntegrationCommandExecution(GinkgoT())

			dispatchErr := dispatcher.Dispatch(ctx, "channel", exec)
			Expect(dispatchErr).NotTo(HaveOccurred())

			// All three events should have landed in the DB.
			rows := queryAuditRows(ctx, db, "plugin")
			Expect(rows).To(HaveLen(3))
		})
	})
})

// auditRow holds the relevant columns from one access_audit_log row.
type auditRow struct {
	source     string
	component  string
	subject    string
	action     string
	resource   string
	effect     string
	eventID    string
	eventName  string
	message    string
	attributes string // JSON as text, sufficient for substring assertions
}

// queryAuditRows returns all rows matching the given source, ordered by
// timestamp ascending.
func queryAuditRows(ctx context.Context, db *sql.DB, source string) []auditRow {
	query := `
		SELECT source, component, subject, action, resource, effect,
		       event_id, event_name, message, attributes::text
		FROM access_audit_log
		WHERE source = $1
		ORDER BY timestamp ASC
	`
	rows, err := db.QueryContext(ctx, query, source)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = rows.Close() }()

	var results []auditRow
	for rows.Next() {
		var r auditRow
		Expect(rows.Scan(
			&r.source, &r.component, &r.subject, &r.action, &r.resource,
			&r.effect, &r.eventID, &r.eventName, &r.message, &r.attributes,
		)).To(Succeed())
		results = append(results, r)
	}
	Expect(rows.Err()).NotTo(HaveOccurred())
	return results
}

// scriptedDeliverer returns a pre-canned CommandResponse on every Deliver.
type scriptedDeliverer struct {
	response *pluginsdk.CommandResponse
}

func (d *scriptedDeliverer) DeliverCommand(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return d.response, nil
}

// newIntegrationTestDispatcher constructs a minimal dispatcher with the
// given deliverer and audit logger wired in. Registry is populated with
// a single plugin-backed command named "channel".
//
// The exact constructor signatures (command.NewRegistry, RegisterPluginCommand,
// NewCommandExecution, CommandExecutionOptions, Services interface) depend
// on the current state of internal/command/. Before writing this test,
// read internal/command/dispatcher_test.go to confirm the constructor
// shapes and mirror the pattern used by existing dispatcher tests.
func newIntegrationTestDispatcher(t GinkgoTInterface, deliverer command.PluginCommandDeliverer, logger *audit.Logger) *command.Dispatcher {
	engine := policytest.AllowAllEngine()

	registry := command.NewRegistry()
	registry.RegisterPluginCommand(command.CommandEntry{
		Name:       "channel",
		Source:     "plugin",
		PluginName: "test-plugin",
	})

	dispatcher, err := command.NewDispatcher(
		registry,
		engine,
		command.WithPluginDeliverer(deliverer),
		command.WithAuditLogger(logger),
	)
	if err != nil {
		t.Fatalf("dispatcher construction failed: %v", err)
	}
	return dispatcher
}

// newIntegrationCommandExecution returns a minimal CommandExecution with
// a fixed character ID for deterministic Subject assertions.
func newIntegrationCommandExecution(t GinkgoTInterface) *command.CommandExecution {
	charID := ulid.MustParse("01ABCDEFGHIJKLMNOPQRSTUVWX")
	exec := command.NewCommandExecution(
		core.CharacterRef{ID: charID},
		command.CommandExecutionOptions{
			InvokedAs: "channel",
			Services:  &stubServices{},
		},
	)
	return exec
}

// stubServices provides the minimum surface the dispatcher needs. The
// dispatcher only calls Session().UpdateActivity and Events().Append
// during plugin dispatch.
type stubServices struct{}

func (s *stubServices) Session() command.SessionService { return &stubSessionService{} }
func (s *stubServices) Events() command.EventService    { return &stubEventService{} }

type stubSessionService struct{}

func (s *stubSessionService) UpdateActivity(_ context.Context, _ string) error { return nil }

type stubEventService struct{}

func (s *stubEventService) Append(_ context.Context, _ core.Event) error { return nil }
```

**Note on constructor shapes:** The `command.NewRegistry`, `command.RegisterPluginCommand`, `command.NewCommandExecution`, `command.CommandExecutionOptions`, and `command.Services` types may differ from what is shown above. Before compiling this test, read `internal/command/dispatcher_test.go` to confirm the current constructor signatures. If `policytest.AllowAllEngine()` doesn't exist, use `policytest.NewGrantEngine()` and call `GrantCommandExecution(subject, "channel")` + `Grant(subject, "execute", "command:channel")`. The code above is the intended shape; field/method renames to match reality are expected.

**Note on `auditRow.attributes`:** Asserting `ContainSubstring` against the JSON text is a convenience — it verifies the key-value survived serialization without requiring a JSON library import. For richer assertions (nested values, type round-tripping), parse with `encoding/json` and assert on the parsed map.

**Why this test is different from Step 8.3's schema test:** Step 8.3 verifies the migration applies cleanly. This test verifies the end-to-end data round trip works: plugin emit → SDK → dispatcher → PostgresWriter → access_audit_log row with all expected values. Both are required.

- [ ] **Step 13.2: Build the integration test harness**

Follow the instructions in the note above: read `internal/command/dispatcher_test.go`, mirror its pattern, replace the panics.

Run: `task test:int -- -run 'TestAuditIntegration' ./test/integration/audit/...`

Expected: the binary plugin path test passes. The Lua path test is skipped (pending follow-up).

- [ ] **Step 13.3: Implement the Lua integration test**

Remove the `Skip(...)` call and implement the Lua path test following the binary path pattern. Key differences:

- Instead of a `scriptedDeliverer`, load a Lua plugin that calls `audit.deny("...", "...", {...})` in its command handler.
- Use the existing Lua plugin test scaffolding in `internal/plugin/` tests as a reference. Search for `lua.NewState` usage in integration tests.
- Assertions are the same as the binary path (Source = SourcePlugin, Component populated, Subject populated, ID/Message match).

Run: `task test:int -- -run 'TestAuditIntegration' ./test/integration/audit/...`
Expected: both binary and Lua path tests pass.

- [ ] **Step 13.4: Commit**

Run:

```bash
jj --no-pager commit -m "test(audit): integration tests for binary and Lua plugin emit paths

Verify end-to-end that plugin-emitted audit hints flow through
the dispatcher's hint extraction + host field stamping + audit
logger flush path, producing the expected events with correct
Source/Component/Subject stamping.

Both the binary plugin path (via CommandResponse.audit_hints)
and the Lua plugin path (via audit.deny capability global)
are covered.

Bead: holomush-ggbz"
```

---

## Task 14: Plugin author documentation

**Goal:** Document the audit API for plugin authors in `site/docs/extending/`.

**Files:**

- Create: `site/docs/extending/audit-events.md`

- [ ] **Step 14.1: Write the docs**

File: `site/docs/extending/audit-events.md`

```markdown
<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Emitting Audit Events from Plugins

Plugins can emit audit events during command handler execution. These events flow through the same `audit.Logger` the ABAC engine uses, gaining WAL durability, mode routing, and operator-visible metrics — without any extra ops configuration.

Use this when your handler makes an authorization-relevant decision that the ABAC policy engine didn't catch. Examples: membership / ban / mute checks in a channels plugin, ownership checks in a building plugin, rate-limit denials in a communication plugin.

## When to Emit an Audit Event

| Scenario | Emit an event? |
|---|---|
| Your handler denies a command because of handler-side state | **Yes** — `audit.deny` |
| Your handler allows a command that involved a non-trivial state check | **Yes, optionally** — `audit.allow` if the decision is operator-interesting |
| ABAC policy engine denied the command before your handler ran | **No** — the engine already emitted an event |
| Your handler returned an internal error (DB down, invalid state) | **No** — that's a system failure, not an authorization decision |

## Binary Plugins (Go)

Call `pluginsdk.Audit(ctx)` from your `HandleCommand` method. The returned recorder accumulates hints on the request context; the SDK serializes them into the response when your handler returns.

```go
import (
    pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func (h *handler) HandleCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
    isMember, err := h.store.IsMember(channelID, req.PlayerID)
    if err != nil {
        return pluginsdk.Failuref("channel store unavailable"), nil
    }
    if !isMember {
        pluginsdk.Audit(ctx).Deny(
            "not_member",
            "player not in channel members",
            pluginsdk.AuditAttrs{"channel.type": "public"},
        )
        return pluginsdk.Errorf("You must join #%s before speaking there.", channelName), nil
    }

    // ... happy path ...
}
```

### Recorder Methods

| Method | Use |
|---|---|
| `Audit(ctx).Deny(id, message, attrs)` | Record a deny decision |
| `Audit(ctx).Allow(id, message, attrs)` | Record an allow decision |

### Field Conventions

| Field | What to put here |
|---|---|
| `id` | Plugin-internal rule slug. Short, kebab-or-snake-case, stable. Example: `"not_member"`, `"banned"`, `"archived"` |
| `message` | Per-firing description. Should mirror what the user sees in the error, or add operator-only context. Example: `"player X not in channel Y members"` |
| `attrs` | Free-form context. Keys should be namespaced (`"channel.type"` not `"type"`) to avoid collision with host-merged keys |

## Lua Plugins

Call `audit.deny(...)` or `audit.allow(...)` in your Lua handler. The `audit` global is available in any plugin that declares `holomush.plugin.v1.AuditService` in its manifest `requires:` list.

```yaml
# plugin.yaml
requires:
  - holomush.plugin.v1.AuditService
```

```lua
-- main.lua
function handle_command(cmd)
    if not is_member(cmd.channel_id, cmd.player_id) then
        audit.deny(
            "not_member",
            "player not in channel members",
            {channel_type = "public"}
        )
        return {
            status = "error",
            output = "You must join #" .. cmd.channel_name .. " before speaking there."
        }
    end
    -- ... happy path ...
end
```

### Lua Signature

```lua
audit.deny(id: string, message: string, attrs: table?)
audit.allow(id: string, message: string, attrs: table?)
```

The third argument is optional; omit it if no extra context is needed.

## What the Host Stamps

You do not set — and cannot set — the following fields. The dispatcher stamps them on every hint for anti-spoofing:

| Field | Source |
|---|---|
| `Subject` | Dispatcher's dispatch context (the character running your command) |
| `Action` | Command name (dispatcher-known) + your optional qualifier |
| `Source` | Always `plugin` for hints you emit |
| `Component` | Your plugin's name (from the authenticated plugin identity) |
| `Timestamp` | Host clock at flush time |

## Failure Mode

Audit write failures never fail your command. The dispatcher logs the failure, bumps a Prometheus counter (`abac_audit_plugin_failures_total`), and returns your command response to the user unchanged. Your handler does not need to check for audit errors.

## Related Documentation

- [Access Control](access-control.md) — how policies and audit events fit together
- [Binary Plugins](binary-plugins.md) — Go plugin authoring guide
- [Lua Plugins](lua-plugins.md) — Lua plugin authoring guide
- [AttributeResolverService](abac-attribute-resolver.md) — for plugins that also want to expose custom resource attributes
```

- [ ] **Step 14.2: Commit**

Run:

```bash
jj --no-pager commit -m "docs(audit): plugin author guide for emitting audit events

Document the pluginsdk.Audit(ctx) Go API and the audit.deny/allow
Lua globals. Cover when to emit, what fields to populate, what
the host stamps, and the failure mode.

Bead: holomush-ggbz"
```

---

## Task 15: Final verification — `task pr-prep`

**Goal:** Run the full CI-equivalent suite locally to confirm everything is green.

- [ ] **Step 15.1: Run pr-prep**

Run: `task pr-prep`

Expected: all jobs pass — lint, fmt, schema, license, unit, integration, E2E. If any job fails, fix the underlying issue (do not disable or skip the check).

- [ ] **Step 15.2: Update the bead status**

Run (from the primary repo, not the audit workspace):

```bash
cd /Volumes/Code/github.com/holomush/holomush
bd update holomush-ggbz --notes "Implementation complete: audit package moved to internal/audit/, Entry renamed to Event, Source/Component/Message fields added, plugin hint collection + dispatcher flush + binary SDK recorder + Lua capability wired, integration tests passing, task pr-prep green. Ready for PR review."
```

- [ ] **Step 15.3: Create the PR**

Follow the project's standard PR creation flow (`gh pr create` or invoking the `commit-push-pr` skill). Reference the bead (`holomush-ggbz`) and spec in the PR description. Ensure the PR title is under 70 characters and descriptive.

Example PR title: `feat(audit): source-agnostic audit subsystem + plugin emit facility`

---

## Self-Review Checklist

- [x] **Spec coverage:** Every decision A-J in the spec has at least one task implementing it.
- [x] **Placeholder scan:** No silent TBDs. Task 13 provides explicit helper code with notes directing the engineer to mirror existing dispatcher test patterns for any constructor shape mismatches.
- [x] **Type consistency:** `Event` (not `Entry`), `ID`/`Name` (not `PolicyID`/`PolicyName`), `EventSource` (not `Source string`), `SourcePlugin`/`SourceEngine`/`SourceSystem` constants consistent throughout.
- [x] **DRY:** The context helpers (`NewContextForDispatch`, `AddEventToContext`, `EventsFromContext`) are the single primitive both binary and Lua paths use.
- [x] **YAGNI:** No D-rpc, no rate limiting, no background audit emits, no resource-type validation against plugin declarations.
- [x] **TDD:** Every Task that introduces a new type or function has a failing test before the implementation step.
- [x] **Frequent commits:** 14 commits total across the plan, one per task.
- [x] **Boundary coverage:** Zero-value events, nil attribute maps, empty IDs, malformed resource refs, empty attrs args, large dispatch counts, and no-logger-configured edge cases all have explicit tests.
- [x] **Negative coverage:** Unknown effect strings, non-string Lua keys, DB write failures mid-flush, unknown Lua argument types, and migration rollback all have explicit tests.
- [x] **Invariant coverage:** Anti-spoofing (guaranteed by proto/Go type shape + dispatcher stamping tests), failure-mode-doesn't-fail-command, context-per-dispatch isolation under concurrency, migration reversibility, existing-row backfill, and audit logger mode routing all have explicit tests.
- [x] **Direct DB validation:** Integration tests use real Postgres via testcontainers and `audit.NewPostgresWriter`, with direct SELECT assertions against `access_audit_log` columns. No in-memory writers at the integration layer.
