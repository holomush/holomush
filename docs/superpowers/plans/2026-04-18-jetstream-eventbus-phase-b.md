<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase B: JetStream EventBus Atomic Cutover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cut over from the PostgreSQL `events` table + `LISTEN/NOTIFY` event store to the JetStream-backed `EventBus` landed in Phase A. Single feature branch (`feat/eventbus-cutover`), nine sequential tasks (F1–F9), single squash merge to `main` = production cutover.

**Architecture:** Each F-task is one logical commit on the feature branch. Branch CI MUST stay green after every task. The branch rebases on `main` weekly. The final merge is when production swaps from old to new event store. Clean break — no dual paths, no feature flags, no legacy preservation.

**Tech Stack:** Go, embedded NATS JetStream (Phase A `internal/eventbus/`), proto-first plugin contracts, PostgreSQL (now projection-only), `pgregory.net/rapid` for property tests.

**Spec:** [docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md](../specs/2026-04-18-jetstream-event-log-design.md)

**Epic bead:** `holomush-1tvn`

**Phase B bead IDs:** F1=`holomush-1tvn.8`, F2=`.9`, F3=`.10`, F4=`.11`, F5=`.12`, F6=`.13`, F7=`.14`, F8=`.15`, F9=`.16` (set after creation).

**Prerequisites:** Phase A complete (M1-M7 all in main), PR #233 (session-lifecycle-events) merged, `feat/eventbus-cutover` branch created from main HEAD.

---

## Phase B invariants

- **Branch CI MUST stay green after every task.** Run `task pr-prep` per task before committing.
- **Single squash merge to main.** Do NOT merge intermediate F-tasks to main individually.
- **Rebase weekly on main.** PR #234, scene Phase 4 work, and other in-flight work continue on main; the cutover branch must follow.
- **No flag-based dual paths.** When F1 lands, all events flow through the new bus immediately.
- **Plugin authors are coordinated.** F5 changes the EventSink contract (`Stream` → `Subject`); plugins MUST be updated in the same task.

---

## Setup: create the feature branch

Before F1, from main HEAD:

```bash
# Per repo convention; using jj if jj root succeeds, else git
jj new main && jj describe -m "wip: feat/eventbus-cutover"
jj bookmark create feat/eventbus-cutover -r @
# or git: git checkout -b feat/eventbus-cutover main
```

Push the empty branch:

```bash
jj git push -b feat/eventbus-cutover
# or: git push -u origin feat/eventbus-cutover
```

---

## File Map (Phase B — what changes/moves/deletes)

| File | Action | Task | Notes |
| --- | --- | --- | --- |
| `internal/plugin/event_emitter.go` | Modify | F1 | EventSink validation now publishes to `bus.Publish` instead of `EventStore.Append`; stamps `App-Codec` header explicitly |
| `pkg/plugin/event_sink.go` | Modify | F1 | `EmitIntent.Stream` → `EmitIntent.Subject`; field rename only, no semantic change for plugin code |
| `internal/eventbus/audit/projection.go` | Modify | F2 | Build subject-exclusion list from registered plugin manifests; skip plugin-owned subjects in host audit |
| `internal/eventbus/audit/manifest_subjects.go` | Create | F2 | Plugin subject ownership map registration |
| `internal/eventbus/audit/manifest_subjects_test.go` | Create | F2 | Longest-prefix-wins resolution + conflict detection (property test) |
| `internal/grpc/server.go` | Modify | F3 | Subscribe handler rewritten around `bus.OpenSession`; deletes `cursor_lock` usage |
| `internal/grpc/cursor_lock.go` | Delete | F3 | No longer needed; JS owns consumer state |
| `internal/grpc/cursor_lock_test.go` | Delete | F3 | |
| `internal/grpc/replay.go` | Delete | F3 | Replay machinery folded into `bus.OpenSession` + `bus.QueryHistory` |
| `internal/grpc/replay_test.go` | Delete | F3 | |
| `internal/grpc/subscribe_test.go` | Modify | F3 | Tests assert new flow (no replay machinery) |
| `internal/grpc/query_stream_history.go` | Modify | F4 | Routes via `bus.QueryHistory` with hot/cold tier crossover; plugin-owned subjects fan out via `PluginAuditService.QueryHistory` |
| `internal/grpc/query_stream_history_test.go` | Modify | F4 | New tests for tier crossover, plugin routing, ULID cursor |
| `internal/eventbus/history/tier.go` | Create | F4 | JS↔PG tier-selection logic |
| `internal/eventbus/history/tier_test.go` | Create | F4 | Dedicated 12-scenario crossover suite (per spec §8) with injected clock |
| `plugins/core-scenes/main.go` | Modify | F5 | Implements `PluginAuditService` for `events.*.scene.>`; emits use new subject naming |
| `plugins/core-scenes/audit.go` | Create | F5 | Audit handler implementation + plugin-owned `scene_log` table |
| `plugins/core-scenes/event_types.go` | Create | F5 | Plugin-local event type constants (moved from `internal/core/event.go:39-70`) |
| `plugins/core-scenes/payloads/` | Create | F5 | Plugin-local payload types (moved from `internal/core/event.go:74-143`) |
| `plugins/core-scenes/manifest.yaml` | Modify | F5 | Adds `audit:` declaration; uses new subject patterns |
| `plugins/core-scenes/migrations/000001_create_scene_log.up.sql` | Create | F5 | Plugin-owned audit schema |
| `internal/core/event.go` | Modify | F5 | Delete lines 39-143 (constants + payload types); keep only ULID/Actor/MaxPayloadSize that move into eventbus.* in F7 |
| `internal/store/migrations/000NNN_drop_events_table.up.sql` | Create | F6 | DROP events; DROP event_cursors |
| `internal/store/migrations/000NNN_drop_events_table.down.sql` | Create | F6 | Recreate (kept by convention) |
| `internal/core/event_writer.go` | Delete | F7 | Global serializer no longer needed |
| `internal/core/event_writer_test.go` | Delete | F7 | |
| `internal/core/store.go` | Delete | F7 | EventStore interface superseded by EventBus |
| `internal/core/store_memory.go` | Delete | F7 | |
| `internal/core/store_memory_test.go` | Delete | F7 | |
| `internal/store/postgres.go` | Modify | F7 | Delete event-table functions (Append, Replay, ReplayTail, LastEventID, Subscribe, SubscribeSession, pgSubscription, pg_notify code, streamToChannel) |
| `internal/store/postgres_test.go` | Modify | F7 | Delete obsolete event tests |
| `internal/store/postgres_integration_test.go` | Modify | F7 + F8 | Delete pgnotify tests; delete time.Sleep crutches |
| `internal/store/events_immutable_test.go` | Modify | F7 | Repurpose against `events_audit` |
| `internal/session/session.go` | Modify | F7 | Drop `EventCursors map[string]ulid.ULID` field |
| `internal/store/session_store.go` | Modify | F7 | Drop EventCursors persistence + UpdateCursors method |
| `test/integration/eventbus_e2e/` | Create | F8 | New full E2E suite (per spec §8) |
| `CLAUDE.md` | Modify | F9 | Update event-store and EventWriter sections |
| `site/docs/contributing/event-store.md` | Modify or Create | F9 | Public docs for the new bus |
| `docs/specs/2026-03-20-event-delivery-redesign.md` | Modify | F9 | Add status header marking superseded |

---

## Task F1: EventSink → bus.Publish

**Files:**

- Modify: `internal/plugin/event_emitter.go`
- Modify: `pkg/plugin/event_sink.go`

**Bead:** `holomush-1tvn.8`

This is the cutover commit on the publish side. Plugins (Lua and binary) keep their existing API call shape (`eventSink:emit({stream=…, type=…, payload=…})`); the field name `stream` becomes `subject`, the underlying implementation now publishes to JetStream instead of `EventStore.Append`.

- [ ] **Step 1: Rename `EmitIntent.Stream` to `EmitIntent.Subject`**

Edit `pkg/plugin/event_sink.go`. Locate the `EmitIntent` struct and rename `Stream string` to `Subject string`. Update the doc comment.

- [ ] **Step 2: Update Lua hostfunc binding to map `subject` from Lua tables**

Edit the Lua side of the plugin SDK (likely `internal/plugin/hostfunc/stdlib_emit.go` or similar). The existing binding accepts a Lua table with `stream`, `type`, `payload` keys. Change the key handler:

- Accept `subject` as the canonical key
- Continue accepting `stream` for one rev as a deprecation shim (log a warning when used)

(Locate the actual file via `Grep` for `eventSink` in `internal/plugin/`.)

- [ ] **Step 3: Update binary plugin SDK and existing plugin call sites**

Find every `EmitIntent{Stream: …}` literal across `plugins/` and update to `EmitIntent{Subject: …}`. Update field paths in any reflective marshaling code.

- [ ] **Step 4: Rewrite the EmitEvent handler in `internal/plugin/event_emitter.go`**

Replace the body that calls `EventStore.Append` with the new bus path:

```go
func (h *PluginEventEmitter) EmitEvent(ctx context.Context, intent EmitIntent) error {
    // Validate Subject against plugin's manifest emits patterns (existing).
    if err := h.validate(intent.Subject); err != nil { return err }

    // Validate type
    typ, err := eventbus.NewType(intent.Type)
    if err != nil { return err }

    // Stamp host-owned fields
    sub, err := eventbus.NewSubject(intent.Subject)
    if err != nil { return err }
    actor := h.resolver.Resolve(ctx)

    // Marshal as proto
    eventProto := &eventbusv1.Event{
        Id:        idgen.NewULID().Bytes(),
        Subject:   string(sub),
        Type:      string(typ),
        Timestamp: timestamppb.Now(),
        Actor:     toProtoActor(actor),
        Payload:   intent.Payload,
    }
    plainBytes, err := proto.Marshal(eventProto)
    if err != nil { return fmt.Errorf("emit: marshal: %w", err) }

    // Apply codec (Phase A: identity)
    codecName, label, err := h.selector.SelectForEncrypt(ctx, string(sub))
    if err != nil { return fmt.Errorf("emit: codec select: %w", err) }
    c, err := codec.Resolve(codecName)
    if err != nil { return err }
    var key codec.Key
    if codecName != codec.NameIdentity {
        key, err = h.keys.Active(ctx, label)
        if err != nil { return fmt.Errorf("emit: key fetch: %w", err) }
    }
    encoded, err := c.Encode(ctx, plainBytes, key)
    if err != nil { return fmt.Errorf("emit: encode: %w", err) }

    // Build NATS msg with required headers
    msg := &nats.Msg{
        Subject: string(sub),
        Data:    encoded,
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Msg-Id", ulid.ULID(eventProto.Id).String())
    msg.Header.Set("App-Schema-Version", "1")
    msg.Header.Set("App-Event-Type", string(typ))
    msg.Header.Set("App-Codec", string(codecName))
    msg.Header.Set("App-Actor-Kind", actor.Kind.String())
    if actor.ID != (ulid.ULID{}) {
        msg.Header.Set("App-Actor-ID", actor.ID.String())
    }
    telemetry.InjectHeaders(ctx, msg.Header)

    // Publish with deadline = dupe_window − safety_margin
    pubCtx, cancel := context.WithTimeout(ctx, h.dupeWindowDeadline())
    defer cancel()
    _, err = h.bus.PublishMsg(pubCtx, msg)
    if errors.Is(err, context.DeadlineExceeded) {
        return eventbus.ErrPublishExpired
    }
    return err
}
```

- [ ] **Step 5: Update unit tests for the new EmitEvent path**

Existing tests likely mock `EventStore`. Update them to mock `Publisher` (from `internal/eventbus`) instead. Use the `eventbustest.New(t)` helper for integration-flavor tests.

- [ ] **Step 6: Add a property test for idempotent publish**

```go
func TestEmitEventIdempotentRetry(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        e := eventbustest.New(t)
        emitter := newTestEmitter(e.Bus.JS())
        intent := randomEmitIntent(t)

        require.NoError(t, emitter.EmitEvent(ctx, intent))
        require.NoError(t, emitter.EmitEvent(ctx, intent))  // retry with same intent
        // Same ULID → JS dedup absorbs

        info, _ := e.Bus.JS().Stream(ctx, "EVENTS")
        require.EqualValues(t, 1, info.State.Msgs)
    })
}
```

- [ ] **Step 7: Run pr-prep**

```bash
task pr-prep
```

Expected: green.

- [ ] **Step 8: Commit F1**

Conventional message: `feat(eventbus): F1 — EventSink publishes via JetStream bus`.

---

## Task F2: Audit projection routes plugin-owned subjects + filter exclusion

**Files:**

- Create: `internal/eventbus/audit/manifest_subjects.go`, `manifest_subjects_test.go`
- Modify: `internal/eventbus/audit/projection.go`

**Bead:** `holomush-1tvn.9`

The host audit projection MUST exclude subjects owned by plugins (those are projected by per-plugin consumers in F5). This task adds the longest-prefix-wins routing and integrates it into the projection's `FilterSubjects`.

- [ ] **Step 1: Implement subject ownership routing**

Create `internal/eventbus/audit/manifest_subjects.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
    "fmt"
    "sort"
    "strings"
)

// SubjectOwner identifies who owns a given subject prefix.
type SubjectOwner struct {
    PluginName string // empty = host
    Pattern    string // the manifest-declared pattern, e.g., "events.*.scene.>"
}

// OwnerMap is the startup-built mapping of subject prefixes to owners.
// Use Resolve() with longest-prefix-wins token-aligned matching.
type OwnerMap struct {
    entries []ownerEntry // sorted by descending token depth
}

type ownerEntry struct {
    pattern string
    tokens  []string
    owner   SubjectOwner
}

// NewOwnerMap builds the map from a list of (pattern, owner) pairs.
// Returns ErrSubjectOwnershipConflict if two distinct owners declare the
// same exact pattern.
func NewOwnerMap(decls []SubjectOwner) (*OwnerMap, error) {
    seen := make(map[string]SubjectOwner)
    for _, d := range decls {
        if existing, ok := seen[d.Pattern]; ok && existing.PluginName != d.PluginName {
            return nil, fmt.Errorf("subject ownership conflict on %q: %s vs %s",
                d.Pattern, existing.PluginName, d.PluginName)
        }
        seen[d.Pattern] = d
    }
    entries := make([]ownerEntry, 0, len(decls))
    for _, d := range decls {
        entries = append(entries, ownerEntry{
            pattern: d.Pattern,
            tokens:  strings.Split(d.Pattern, "."),
            owner:   d,
        })
    }
    // Longest-token-count first; ties broken by literal (no wildcards) wins.
    sort.SliceStable(entries, func(i, j int) bool {
        if len(entries[i].tokens) != len(entries[j].tokens) {
            return len(entries[i].tokens) > len(entries[j].tokens)
        }
        return literalScore(entries[i].tokens) > literalScore(entries[j].tokens)
    })
    return &OwnerMap{entries: entries}, nil
}

// Resolve returns the owner for the given concrete subject. If no entry
// matches, owner is host (empty PluginName).
func (m *OwnerMap) Resolve(subject string) SubjectOwner {
    tokens := strings.Split(subject, ".")
    for _, e := range m.entries {
        if matches(e.tokens, tokens) {
            return e.owner
        }
    }
    return SubjectOwner{} // host
}

// matches checks NATS-style pattern (with * and >) against concrete tokens.
func matches(pattern, concrete []string) bool {
    for i, pt := range pattern {
        if pt == ">" {
            return i <= len(concrete) // > matches one OR more remaining
        }
        if i >= len(concrete) {
            return false
        }
        if pt == "*" {
            continue
        }
        if pt != concrete[i] {
            return false
        }
    }
    return len(pattern) == len(concrete)
}

func literalScore(tokens []string) int {
    n := 0
    for _, t := range tokens {
        if t != "*" && t != ">" {
            n++
        }
    }
    return n
}

// HostExcludedSubjects returns the FilterSubjects pattern list the host
// audit projection should subscribe to: the universe minus plugin-owned
// patterns.
//
// JetStream FilterSubjects does not support exclusion natively; we
// achieve exclusion by subscribing to all-of `events.>` and dropping
// plugin-owned messages at handle time. This function returns the list
// of plugin-owned patterns so the projection's handle() can skip them.
func (m *OwnerMap) HostExcludedSubjects() []SubjectOwner {
    out := make([]SubjectOwner, 0, len(m.entries))
    for _, e := range m.entries {
        if e.owner.PluginName != "" {
            out = append(out, e.owner)
        }
    }
    return out
}
```

- [ ] **Step 2: Write the property test**

Create `internal/eventbus/audit/manifest_subjects_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
    "errors"
    "testing"

    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/audit"
)

func TestResolveLongestPrefixWins(t *testing.T) {
    m, err := audit.NewOwnerMap([]audit.SubjectOwner{
        {PluginName: "core-scenes", Pattern: "events.*.scene.>"},
        {PluginName: "scene-lifecycle", Pattern: "events.*.scene.*.lifecycle"},
    })
    require.NoError(t, err)

    o := m.Resolve("events.main.scene.01ABC.lifecycle")
    require.Equal(t, "scene-lifecycle", o.PluginName, "longer-prefix wins")
}

func TestResolveLiteralWinsOverWildcardAtSameDepth(t *testing.T) {
    m, _ := audit.NewOwnerMap([]audit.SubjectOwner{
        {PluginName: "scenes", Pattern: "events.main.scene.>"},
        {PluginName: "scene-literal", Pattern: "events.main.scene.literal"},
    })
    o := m.Resolve("events.main.scene.literal")
    require.Equal(t, "scene-literal", o.PluginName)
}

func TestResolveHostFallback(t *testing.T) {
    m, _ := audit.NewOwnerMap([]audit.SubjectOwner{
        {PluginName: "scenes", Pattern: "events.*.scene.>"},
    })
    o := m.Resolve("events.main.location.01ABC")
    require.Empty(t, o.PluginName, "non-matching subject = host")
}

func TestExactPrefixConflictIsStartupError(t *testing.T) {
    _, err := audit.NewOwnerMap([]audit.SubjectOwner{
        {PluginName: "a", Pattern: "events.*.scene.>"},
        {PluginName: "b", Pattern: "events.*.scene.>"},
    })
    require.Error(t, err)
    require.Contains(t, err.Error(), "ownership conflict")
}
```

- [ ] **Step 3: Wire the OwnerMap into the projection**

Modify `internal/eventbus/audit/projection.go`. The projection's `handle` method now consults the OwnerMap and skips (without acking) messages whose subject resolves to a plugin owner — those messages will be picked up by the per-plugin consumer registered in F5. (Actually: ack-skip is wrong because the host's consumer would advance past plugin-owned messages, eventually retention-evicting them before plugin can read. Correct: ack-and-skip — the host's consumer's job is just to drain quickly so the JS retention works; the per-plugin consumer is independent and reads the same messages from JS.)

```go
func (p *projection) handle(msg jetstream.Msg) {
    // Plugin-owned subjects are acked-and-skipped — the per-plugin
    // consumer handles their persistence independently.
    if p.owners != nil && p.owners.Resolve(msg.Subject()).PluginName != "" {
        _ = msg.Ack()
        return
    }
    if err := p.persist(msg); err != nil {
        return
    }
    _ = msg.Ack()
}
```

- [ ] **Step 4: Run all audit tests**

```bash
task test -- ./internal/eventbus/audit/...
task test:int -- ./internal/eventbus/audit/...
```

Expected: green.

- [ ] **Step 5: Run pr-prep**

```bash
task pr-prep
```

- [ ] **Step 6: Commit F2**

Conventional message: `feat(eventbus/audit): F2 — subject ownership routing + plugin exclusion`.

---

## Task F3: gRPC Subscribe handler rewrite + delete cursor_lock + replay

**Files:**

- Modify: `internal/grpc/server.go` — Subscribe handler
- Delete: `internal/grpc/cursor_lock.go`, `cursor_lock_test.go`
- Delete: `internal/grpc/replay.go`, `replay_test.go`
- Modify: `internal/grpc/subscribe_test.go`

**Bead:** `holomush-1tvn.10`

- [ ] **Step 1: Locate the existing Subscribe handler**

Read `internal/grpc/server.go` and locate the `Subscribe` RPC implementation. It currently does focus-restore + cursor-aware Replay + LISTEN/NOTIFY live loop with cursor_lock.

- [ ] **Step 2: Rewrite the Subscribe handler**

Replace the body with the simpler bus-based flow:

```go
func (s *coreServer) Subscribe(req *connect.Request[corev1.SubscribeRequest], stream *connect.ServerStream[corev1.SubscribeResponse]) error {
    ctx := req.Context()
    sessionID := req.Msg.SessionId
    token := req.Msg.PlayerSessionToken

    info, err := s.session.ValidateSessionOwnership(ctx, sessionID, token)
    if err != nil {
        return connect.NewError(connect.CodeUnauthenticated, err)
    }

    plan, err := s.focus.RestorePlan(ctx, sessionID)
    if err != nil {
        return err
    }
    filters := plan.Subjects() // []eventbus.Subject

    busStream, err := s.bus.OpenSession(ctx, sessionID, filters)
    if err != nil {
        return err
    }
    defer busStream.Close()

    // Record session-start audit event (existing pattern).
    s.audit.RecordSubscribe(ctx, info)

    for {
        delivery, err := busStream.Next(ctx)
        if err != nil {
            return err
        }
        if err := stream.Send(toProtoSubscribeResponse(delivery.Event())); err != nil {
            // Don't ack on Send failure; JS redelivers on rebind.
            return err
        }
        if err := delivery.Ack(); err != nil {
            s.logger.Warn("ack failed; will redeliver", "err", err)
        }
    }
}
```

- [ ] **Step 3: Delete cursor_lock + replay**

```bash
rm internal/grpc/cursor_lock.go internal/grpc/cursor_lock_test.go
rm internal/grpc/replay.go internal/grpc/replay_test.go
```

- [ ] **Step 4: Update subscribe_test.go**

Rewrite tests to use `eventbustest.New(t)` and assert the new flow. Property test for filter monotonicity under `SetFilters` (per spec §8 invariants):

```go
func TestSubscribeFilterMonotonicityUnderSetFilters(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        // ... emit N events while concurrently calling SetFilters with arbitrary subsets
        // assert: events matching current filter at time of publish ARE delivered
        // assert: events from removed subjects are NOT delivered after SetFilters returns
        // assert: cursor preserved across every filter update
    })
}
```

- [ ] **Step 5: Run pr-prep**

Expected: green. Existing client behavior preserved (cursor resume on reconnect now via JS durable consumer).

- [ ] **Step 6: Commit F3**

Conventional message: `feat(grpc): F3 — Subscribe via EventBus.OpenSession; delete cursor_lock + replay`.

---

## Task F4: gRPC QueryHistory rewrite with hot/cold tier crossover

**Files:**

- Modify: `internal/grpc/query_stream_history.go`
- Modify: `internal/grpc/query_stream_history_test.go`
- Create: `internal/eventbus/history/tier.go`
- Create: `internal/eventbus/history/tier_test.go` (the dedicated 12-scenario suite)

**Bead:** `holomush-1tvn.11`

- [ ] **Step 1: Implement the tier-selection logic**

Create `internal/eventbus/history/tier.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package history serves QueryHistory requests by transparently
// crossing the JetStream / PostgreSQL audit retention boundary.
package history

import (
    "context"
    "database/sql"
    "time"

    "github.com/nats-io/nats.go/jetstream"

    "github.com/holomush/holomush/internal/eventbus"
)

// Reader implements eventbus.HistoryReader by routing queries between
// JetStream (recent tail) and PostgreSQL events_audit (forever archive).
type Reader struct {
    js              jetstream.JetStream
    db              *sql.DB
    streamMaxAge    time.Duration
    safetyMargin    time.Duration
    now             func() time.Time // injectable for tests
}

func NewReader(js jetstream.JetStream, db *sql.DB, streamMaxAge time.Duration, now func() time.Time) *Reader {
    return &Reader{
        js: js, db: db,
        streamMaxAge: streamMaxAge,
        safetyMargin: time.Hour,
        now:          now,
    }
}

// QueryHistory streams events matching q. The implementation chooses JS
// or PG as the starting tier based on q.After / q.NotBefore vs the
// retention edge. When a page crosses the boundary, it continues from
// the other tier transparently.
func (r *Reader) QueryHistory(ctx context.Context, q eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
    edge := r.now().Add(-r.streamMaxAge + r.safetyMargin)
    // ... (implementation; see spec §5)
}
```

(Full implementation in F4 task — non-trivial, ~150 LOC. Includes the algorithm from spec §5.)

- [ ] **Step 2: Implement the dedicated 12-scenario test suite**

Create `internal/eventbus/history/tier_test.go` covering each scenario in the table from spec §8:

```go
func TestTierCrossoverCursorWithinJSRetention(t *testing.T) { ... }
func TestTierCrossoverCursorOlderThanRetention(t *testing.T) { ... }
func TestTierCrossoverCursorAtBoundaryEdge(t *testing.T) { ... }
func TestTierCrossoverPageBoundaryCrosses(t *testing.T) { ... }
func TestTierCrossoverClockSkewAbsorbed(t *testing.T) { ... }
func TestTierCrossoverForwardDirection(t *testing.T) { ... }
func TestTierCrossoverBackwardDirection(t *testing.T) { ... }
func TestTierCrossoverEmptyPGFullJS(t *testing.T) { ... }
func TestTierCrossoverEmptyJSFullPG(t *testing.T) { ... }
func TestTierCrossoverBothEmpty(t *testing.T) { ... }
func TestTierCrossoverNonExistentSubject(t *testing.T) { ... }
func TestTierCrossoverPluginOwnedSubjectRoutesToPlugin(t *testing.T) { ... }
```

Each test uses an injected clock (`func() time.Time`) advanced to the relevant boundary. NO wall-clock waits.

- [ ] **Step 3: Rewrite the gRPC handler to use the Reader**

Modify `internal/grpc/query_stream_history.go` to delegate to `bus.QueryHistory` (which is `Reader.QueryHistory`).

- [ ] **Step 4: Run pr-prep**

- [ ] **Step 5: Commit F4**

Conventional message: `feat(grpc): F4 — QueryHistory with JS/PG tier crossover + plugin routing`.

---

## Task F5: Per-plugin updates (subjects, manifests, EventType + Payload move)

**Files (per plugin):**

- Modify: `plugins/<each-plugin>/main.go`, `manifest.yaml`
- Create: `plugins/<each-plugin>/event_types.go`, `plugins/<each-plugin>/payloads/`
- Create: `plugins/<each-plugin>/audit.go`
- Create: `plugins/<each-plugin>/migrations/` (if audit declared)
- Modify: `internal/core/event.go` — delete lines 39-143

**Bead:** `holomush-1tvn.12`

This task is repeated per plugin. For HoloMUSH today, the plugin universe is `core-scenes`; channels comes later (paused).

- [ ] **Step 1: Move event types out of `internal/core/event.go`**

Identify which plugin owns each of the 19 EventType constants and 7 payload types per the plugin boundary rules and the spec §1b mapping. Suggested mapping:

| Type / Payload | Owner |
| --- | --- |
| `EventTypeSay`, `EventTypePose`, `WhisperPayload`, `PagePayload`, `PemitPayload`, `OOCPayload` | core-comm (new plugin if not present, else split across owners) |
| `EventTypeArrive`, `EventTypeLeave`, `EventTypeMove`, `LocationStatePayload`, `ExitUpdatePayload` | core-world |
| `EventTypeObjectCreate/Destroy/Use/Examine/Give` | core-world |
| `EventTypeCommandResponse`, `EventTypeCommandError` | host (system events; stay in eventbus or move to a tiny core-commands plugin) |

Confirm the mapping with the plugin owners before splitting; if some types are temporarily un-owned, file a follow-up bead and keep them in a transitional `internal/core/event.go` *only* for those types (delete the rest).

- [ ] **Step 2: Update manifests**

Each plugin's `manifest.yaml` adds:

```yaml
emits:
  - "events.*.scene.*.ic"
  - "events.*.scene.*.ooc"
  - "events.*.scene.*.lifecycle"
audit:
  - subjects: ["events.*.scene.>"]
    schema:   plugin_core_scenes
    table:    scene_log
```

- [ ] **Step 3: Implement `PluginAuditService` in plugin code**

For each plugin with `audit:` in its manifest, implement the proto service:

```go
// plugins/core-scenes/audit.go
type AuditServer struct {
    pluginv1.UnimplementedPluginAuditServiceServer
    db *sql.DB
}

func (s *AuditServer) AuditEvent(ctx context.Context, req *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error) {
    // INSERT INTO plugin_core_scenes.scene_log (...) ON CONFLICT (id) DO NOTHING
    return &pluginv1.AuditEventResponse{}, nil
}

func (s *AuditServer) QueryHistory(req *pluginv1.QueryHistoryRequest, stream pluginv1.PluginAuditService_QueryHistoryServer) error {
    // Authz: scene membership check
    // SELECT ... FROM plugin_core_scenes.scene_log WHERE subject=$1 AND id > $cursor ORDER BY id LIMIT $page
    // stream events back
    return nil
}
```

- [ ] **Step 4: Add plugin migration for the audit schema**

```sql
-- plugins/core-scenes/migrations/000001_create_scene_log.up.sql
CREATE SCHEMA IF NOT EXISTS plugin_core_scenes;
REVOKE ALL ON SCHEMA plugin_core_scenes FROM PUBLIC;
GRANT USAGE ON SCHEMA plugin_core_scenes TO plugin_core_scenes_role;

CREATE TABLE IF NOT EXISTS plugin_core_scenes.scene_log (
    id          BYTEA PRIMARY KEY,
    subject     TEXT NOT NULL,
    type        TEXT NOT NULL,
    timestamp   TIMESTAMPTZ NOT NULL,
    actor_kind  TEXT NOT NULL,
    actor_id    BYTEA,
    payload     BYTEA NOT NULL,
    schema_ver  SMALLINT NOT NULL,
    codec       TEXT NOT NULL,
    inserted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX scene_log_subject_id ON plugin_core_scenes.scene_log (subject, id);
```

- [ ] **Step 5: Wire plugin into per-plugin audit consumer registration**

Host code: when loading a plugin with `audit:` in manifest, register a per-plugin durable consumer named `plugin_audit_<name>` and dispatch deliveries to the plugin's `PluginAuditService.AuditEvent` RPC. (Implementation in `internal/eventbus/audit/plugin_consumer.go` — straightforward worker per declared audit block.)

- [ ] **Step 6: Per-plugin tests**

Test that:
- Plugin's `AuditEvent` RPC stores rows in its schema
- Plugin's `QueryHistory` RPC enforces membership and returns paginated rows
- Host audit table has zero rows for the plugin's subjects (subject ownership exclusion)

- [ ] **Step 7: Run pr-prep**

- [ ] **Step 8: Commit F5**

Conventional message: `feat(plugins): F5 — per-plugin subjects, audit projections, payload migration`.

---

## Task F6: Schema cutover — DROP events, DROP event_cursors

**Files:**

- Create: `internal/store/migrations/000NNN_drop_events_and_cursors.up.sql`
- Create: `internal/store/migrations/000NNN_drop_events_and_cursors.down.sql`

**Bead:** `holomush-1tvn.13`

- [ ] **Step 1: Write the up migration**

```sql
-- 000NNN_drop_events_and_cursors.up.sql
ALTER TABLE sessions DROP COLUMN IF EXISTS event_cursors;
DROP TABLE IF EXISTS events;
```

- [ ] **Step 2: Write the down migration (recreate; convention only)**

```sql
-- Down migrations recreate the prior state; we keep them by convention
-- but production rollback is via reverting the merge commit, not
-- running migrations down.
CREATE TABLE events (
    id BYTEA PRIMARY KEY,
    stream TEXT NOT NULL,
    type TEXT NOT NULL,
    actor_kind TEXT NOT NULL,
    actor_id BYTEA,
    payload BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX events_stream_id ON events (stream, id);

ALTER TABLE sessions ADD COLUMN event_cursors JSONB DEFAULT '{}'::jsonb;
```

- [ ] **Step 3: Run migrations forward + back on a fresh DB**

```bash
task test:migrations
```

Expected: passes.

- [ ] **Step 4: Run pr-prep**

- [ ] **Step 5: Commit F6**

Conventional message: `feat(store): F6 — drop events table and event_cursors column`.

---

## Task F7: Old code deletion

**Files:**

- Delete: `internal/core/event_writer.go`, `event_writer_test.go`
- Delete: `internal/core/store.go`, `store_memory.go`, `store_memory_test.go`
- Modify: `internal/store/postgres.go` — delete event-table functions
- Modify: `internal/store/postgres_test.go` — delete obsolete tests
- Modify: `internal/store/postgres_integration_test.go` — delete pgnotify tests + `time.Sleep` crutches
- Modify: `internal/store/events_immutable_test.go` — repurpose against `events_audit`
- Modify: `internal/session/session.go` — drop `EventCursors` field
- Modify: `internal/store/session_store.go` — drop EventCursors persistence + UpdateCursors method
- Modify: `internal/core/event.go` — delete remaining content (or move to `internal/eventbus/types.go` if any pieces are still needed)

**Bead:** `holomush-1tvn.14`

- [ ] **Step 1: Delete EventWriter**

```bash
rm internal/core/event_writer.go internal/core/event_writer_test.go
```

Update any imports that reference `core.EventWriter` — remove them.

- [ ] **Step 2: Delete legacy EventStore interfaces**

```bash
rm internal/core/store.go internal/core/store_memory.go internal/core/store_memory_test.go
```

- [ ] **Step 3: Strip event-table code from `internal/store/postgres.go`**

Delete: `Append`, `Replay`, `ReplayTail`, `LastEventID`, `Subscribe`, `SubscribeSession`, `pgSubscription` struct + methods, `pg_notify` calls, `streamToChannel`, `connIface`. Keep only player/character/session/etc. functions.

- [ ] **Step 4: Strip pgnotify tests + sleeps from `postgres_integration_test.go`**

Delete every test that references LISTEN/NOTIFY, `pgSubscription`, `Subscribe`, or `SubscribeSession`. Delete every `time.Sleep(time.Millisecond)` and `time.Sleep(10 * time.Millisecond)` — those existed for ULID-collision-in-same-ms which is no longer a concern.

- [ ] **Step 5: Repurpose `events_immutable_test.go` against `events_audit`**

The test currently asserts no UPDATE/DELETE on `events`. Rewrite to assert no UPDATE/DELETE on `events_audit` in core Go source.

- [ ] **Step 6: Drop EventCursors from session model**

Modify `internal/session/session.go` and `internal/store/session_store.go`. Delete `EventCursors map[string]ulid.ULID` field, `UpdateCursors` method, and the JSONB merge SQL.

- [ ] **Step 7: Run pr-prep**

Expected: green. Build now compiles without `EventStore`, `EventWriter`, `cursor_lock`, `replay.go`, or pgnotify code.

- [ ] **Step 8: Commit F7**

Conventional message: `chore(store): F7 — delete legacy event store, EventWriter, cursor machinery`.

---

## Task F8: Test sweep — port "Variant A", add E2E suite, drop obsolete

**Files:**

- Create: `test/integration/eventbus_e2e/`
- Modify: various test files

**Bead:** `holomush-1tvn.15`

- [ ] **Step 1: Port the "Variant A Go/No-Go" cross-stream invariant test**

The original test in `postgres_integration_test.go:574-683` (1000 events × 4 streams × 10 goroutines × 2 subscribers, asserting identical strictly-ascending sequences) is gone with F7. Port it to assert the same invariant against the new bus:

```go
// internal/eventbus/integration_test.go
//go:build integration

func TestCrossSubjectSequenceOrderingUnderConcurrentPublishers(t *testing.T) {
    e := eventbustest.New(t)
    // 1000 events × 4 subjects × 10 goroutines × 2 subscribers
    // assert: each subscriber sees strictly ascending JS seq
    // assert: both subscribers see identical seq sequences
    // No time.Sleep — use AwaitAckedSeq for synchronization.
}
```

- [ ] **Step 2: Implement the full E2E matrix from spec §8**

Create files under `test/integration/eventbus_e2e/`:

- `cross_tier_query_test.go` — 12 scenarios from spec §8 dedicated tier suite
- `plugin_audit_isolation_test.go` — host audit empty for plugin subjects
- `reconnect_resume_test.go` — last-ack continuation, no dup, no loss
- `audit_drift_detector_test.go` — tampered row reported with id
- `backfill_rebuild_test.go` — `bin/holomush audit-backfill` produces matching counts
- `plugin_crash_resilience_test.go` — restart drains; PK ON CONFLICT prevents dups
- `js_storage_corruption_test.go` — rebuild from PG audit; ULIDs stable
- `multi_protocol_fanout_test.go` — telnet + web in same scene see same pose

Each test uses `eventbustest.New(t)` + `testutil.SharedPostgres(t)`.

- [ ] **Step 3: Add the chaos+soak nightly job**

Configure CI to run a 5-minute soak nightly:

```yaml
# .github/workflows/nightly-soak.yml
on:
  schedule:
    - cron: '0 6 * * *'
jobs:
  soak:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: task soak:eventbus
```

The `soak:eventbus` task runs:

```bash
go test -tags=integration,soak -timeout=20m -v ./test/integration/eventbus_e2e/...
```

- [ ] **Step 4: Run full pr-prep + integration tests**

```bash
task pr-prep
task test:int
```

Expected: green.

- [ ] **Step 5: Commit F8**

Conventional message: `test(eventbus): F8 — port invariant suite + add full E2E matrix`.

---

## Task F9: Documentation updates

**Files:**

- Modify: `CLAUDE.md` — event-store and EventWriter sections
- Modify or Create: `site/docs/contributing/event-store.md`
- Modify: `docs/specs/2026-03-20-event-delivery-redesign.md` — mark superseded

**Bead:** `holomush-1tvn.16`

- [ ] **Step 1: Update `CLAUDE.md`**

Locate the sections describing the event store, ULID generation table, EventWriter, EventStore. Rewrite to describe the new `EventBus` interface, JetStream-as-event-log, plugin-owned audit projections. Update the "Random Number Generation" / "ULID Generation" tables — events now use JS seq for ordering, ULID stays for identity.

- [ ] **Step 2: Add status header to superseded spec**

Edit `docs/specs/2026-03-20-event-delivery-redesign.md`. At the top, after the title, add:

```markdown
## Status

**SUPERSEDED** by [docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md](../superpowers/specs/2026-04-18-jetstream-event-log-design.md). LISTEN/NOTIFY-based delivery has been replaced by JetStream as of (cutover date).
```

- [ ] **Step 3: Public docs for the new bus**

Create or update `site/docs/contributing/event-store.md` with a brief overview of:
- The `EventBus` interface (link to spec §4)
- Subject naming convention (link to spec §1c)
- How plugins emit events via `EventSink`
- How plugins declare audit subjects in `manifest.yaml`
- How operators run NATS in embedded vs cluster mode

- [ ] **Step 4: Run pr-prep + docs build**

```bash
task pr-prep
task docs:build
```

- [ ] **Step 5: Commit F9**

Conventional message: `docs: F9 — document new EventBus + supersede prior event-delivery spec`.

---

## Phase B acceptance gate (before merging the feature branch to main)

- [ ] All M-PRs (M1-M7) in main; cumulative coverage gates met
- [ ] Branch CI green on every push of every F-task
- [ ] Branch rebased on main within the last 7 days
- [ ] Full `task pr-prep` green on the entire branch state
- [ ] All E2E tests pass — including the chaos+soak nightly that ran on the branch
- [ ] Coverage gates met (≥80% per package, ≥90% for `internal/eventbus/`, ≥95% for `internal/eventbus/codec/`, perf budget gates met per spec §8)
- [ ] Manual reviewer E2E walkthrough: telnet + web in same scene, full pose flow, scene/log -all queries old + new events
- [ ] All in-flight PRs that conflict have been rebased
- [ ] PG dump taken the night before the merge
- [ ] Merge commit message references the spec, the epic bead, and every F-task bead

---

## Post-merge follow-up beads (file at merge time)

- Scene Phase 4 implementation (`holomush-5rh.13` unblocked)
- Channel plugin resumption (`holomush-0sc.12` unpaused)
- holomush-umxj telnet race fix verified under new bus
- Real codec implementation workstream (codec key provider, KMS choice — `holomush-1tvn` follow-up)
- Multi-node cluster cutover (deferred until scale demands)
- Schema evolution playbook (proto v1 → v2 migration patterns)
- Per-subject Prometheus cardinality discipline doc

---

## Self-review checklist

- [ ] Each F-task corresponds to exactly one F-bead under the epic
- [ ] Inter-bead dependencies set: F1 blocks F2 blocks F3 blocks F4; F5 blocks F6; F6 blocks F7; F7 blocks F8 blocks F9
- [ ] Phase B is gated on Phase A complete (all F-beads depend on M-beads they need)
- [ ] No `time.Sleep` in any test code (existing legacy sleeps deleted in F7/F8)
- [ ] All deletions explicitly listed in file map and deletion steps
- [ ] Each task ends with `task pr-prep` before commit
