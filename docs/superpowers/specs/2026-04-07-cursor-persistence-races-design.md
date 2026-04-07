# Cursor Persistence Races — Design

**Status:** Proposed
**Bead:** holomush-43nd
**Discovered from:** holomush-u37v (PR #197)
**Date:** 2026-04-07

## Problem

`CoreServer.persistCursorAsync` (`internal/grpc/server.go:422-429`) writes session
event cursors as fire-and-forget goroutines with no ordering guarantee, no
synchronization with the wire `Send`, and no shutdown drain. Three independent
failure modes follow from this design, all observable in practice and at least
one of which violates the monotonicity invariant the cursor exists to provide.

The strict cursor-equality polling added in PR #197's reconnect spec is the
canary that surfaced all three: it now waits ~16 of its ~25 seconds for a
cursor to land that the client already received. That gap exists in production
too — just compressed.

### Findings

1. **Stale-cursor window on fast reconnect** — `replayAndSend` calls
   `grpcStream.Send` *before* spawning `persistCursorAsync`. Between Send and
   the eventual UPDATE commit, the persisted cursor lags reality. A client that
   disconnects in that window and reconnects with `replay_from_cursor=true`
   sees duplicate delivery of the most recent event.

2. **Cursor regression under goroutine reordering** — Each
   `persistCursorAsync` call spawns a fresh goroutine. Two consecutive calls
   for the same `(session, stream)` race against each other. The current
   `UPDATE sessions SET event_cursors = event_cursors || $1::jsonb` is
   last-write-wins per JSONB key with no monotonicity guard, no row lock, no
   version column. The persisted cursor can move *backward* in time. This is
   the worst of the three because it makes the cursor's stated purpose
   ("furthest event the client has seen") a lie. Any code that trusts the
   cursor as a high-water mark is intermittently wrong with no error signal.

3. **Lost writes on graceful shutdown** — `persistCursorAsync` uses
   `context.Background()` so the request ctx cancelling does not abort the
   write. But there is no `sync.WaitGroup`, no in-flight counter, and no
   shutdown drain. `grpcServer.GracefulStop()` waits for in-flight RPCs but
   not for goroutines spawned during them. Pending cursor writes are silently
   dropped at shutdown.

### Discovered while diagnosing: ULID generator misuse

`Services.BroadcastSystemMessage` (`internal/command/types.go:602`) mints event
IDs via `idgen.New()`, which uses fresh `crypto/rand` entropy per call and is
*not* monotonic within a millisecond. `PostgresEventStore.Replay` uses
`WHERE id > afterID ORDER BY id`, so two system broadcasts to the same stream
in the same millisecond can produce lex-inverted IDs and the second one is
**silently skipped** on replay. This is independent of the cursor races but
is a blocker for the SQL CAS proposed below: the CAS depends on event-ID lex
order matching arrival order, which only holds if event IDs come from a
monotonic generator.

## Goals

- Eliminate all three findings at their **respective** root causes, not at the
  symptom layer.
- Pay the latency cost only where required, not on every event delivery.
- Make the resulting invariants testable deterministically without fragile
  race-induction.
- Close the orthogonal `BroadcastSystemMessage` ULID hazard so it cannot
  silently re-introduce the same class of bug.
- Enforce the ULID-generator distinction in CI so future code cannot
  reintroduce the hazard.

## Non-goals

- Adding a per-session writer goroutine pool, batched writes, or coalescing.
  These are YAGNI optimizations for a problem HoloMUSH does not have.
- Restricting concurrent Subscribes for the same session. The fix must be
  correct in the presence of concurrent Subscribes; reducing the population of
  multi-Subscribe scenarios is a separate question.
- Rewriting the event store ordering primitive to use a database-side
  sequence. The existing `ORDER BY id` semantics are correct given monotonic
  event IDs.

## Decision

**Option A + SQL CAS, paired** — sync cursor commit at the end of
`replayAndSend`, plus a SQL-level monotonicity guard on `UpdateCursors`. Each
fix addresses an independent root cause:

| Finding | Root cause | Fix |
| --- | --- | --- |
| 1 — stale window | Time-of-Send vs time-of-commit divergence in the live loop | Sync cursor commit before `replayAndSend` returns |
| 2 — regression | `UpdateCursors` SQL has no monotonicity guard; last-write-wins JSONB merge | SQL-level CAS: `WHERE event_cursors->>$key IS NULL OR (event_cursors->>$key) COLLATE "C" < $new::text COLLATE "C"` |
| 3 — lost on shutdown | Untracked goroutines outliving the request handler | Remove the goroutine entirely; `GracefulStop` already waits for live loops because they are in-flight RPCs |

The CAS is **not** belt-and-suspenders for the sync write — it is the actual
fix for Finding 2 in the multi-Subscribe case (two browser tabs, or a
reconnect that overlaps the old loop's teardown), which sync-in-loop alone
does *not* address.

### Why not the alternatives

**Persist-before-Send** trades duplicate delivery for silent message loss:
if commit succeeds and Send fails, the cursor moves past an event the client
never received. Strictly worse semantics for an at-least-once delivery system.

**Async writer goroutine + drain channel** adds a goroutine, a channel, a
coalescing rule, a sentinel-based shutdown protocol, and a per-Subscribe
writer lifecycle — and still requires the SQL CAS to handle multi-Subscribe
correctly. More machinery for the same correctness, in the place the bead
warned about ("the wrong fix here trades one race for another").

## Design

### Component changes

#### 1. `internal/grpc/server.go` — `replayAndSend`

Replace `s.persistCursorAsync(info.ID, streamName, last)` with a **synchronous**
commit before returning. The commit uses a fresh `context.Background()` with a
short bounded timeout (1 second), preserving the original
"client-disconnect must not abort durability" intent while preventing a slow
DB from stalling the live loop for more than a perceptible moment.

The timeout is sized for *liveness of the live loop*, not for "wait out bad
weather." A healthy commit takes <5ms even on a remote pool; 1 second is
1000× that headroom and still well below the "chat feels broken" threshold.
On timeout, the write is dropped (logged at error level) and the loop moves
on — failure mode degrades to today's "duplicate-on-reconnect" behavior,
never worse.

```go
func (s *CoreServer) replayAndSend(
    ctx context.Context,
    info *session.Info,
    streamName string,
    afterID ulid.ULID,
    grpcStream grpc.ServerStreamingServer[corev1.SubscribeResponse],
    lf *locationFollower,
) (ulid.ULID, error) {
    events, err := s.eventStore.Replay(ctx, streamName, afterID, s.maxReplay())
    if err != nil {
        slog.WarnContext(ctx, "replay failed", "stream", streamName, "error", err)
        return afterID, nil
    }
    last := afterID
    for _, ev := range events {
        if ev.Type == core.EventTypeMove && strings.HasPrefix(ev.Stream, world.StreamPrefixCharacter) {
            lf.handleEvent(ctx, ev, grpcStream)
        } else if sendErr := grpcStream.Send(eventToProto(ev)); sendErr != nil {
            return last, oops.With("event_id", ev.ID.String()).Wrap(sendErr)
        }
        last = ev.ID
    }
    if last != afterID {
        // Synchronous, bounded-timeout commit. Uses a fresh context so a
        // client disconnect (which cancels `ctx`) does not abort the write,
        // matching the semantics the old persistCursorAsync intended. The
        // commit happens here so that any subsequent action — next loop
        // iteration, return-from-handler, or GracefulStop — observes the
        // cursor as durably reflecting `last`.
        commitCtx, cancel := context.WithTimeout(context.Background(), cursorCommitTimeout)
        defer cancel()
        if updateErr := s.sessionStore.UpdateCursors(commitCtx, info.ID,
            map[string]ulid.ULID{streamName: last}); updateErr != nil {
            // A failed cursor commit does not break the client — they got
            // the events. The next replay may re-deliver the events between
            // the previous cursor and `last`. This degrades to today's
            // behavior under DB stress, never worse.
            slog.ErrorContext(ctx, "cursor commit failed",
                "session_id", info.ID, "stream", streamName,
                "last_event", last.String(), "error", updateErr)
        }
    }
    return last, nil
}

const cursorCommitTimeout = 1 * time.Second
```

`persistCursorAsync` and the `context.Background()`-leaking goroutine are
deleted entirely. The live loop's own gRPC handler goroutine performs every
cursor write. `grpcServer.GracefulStop()` already waits for handlers to
return, so by the time `Stop()` returns, every cursor for which a Send
succeeded has been committed — Finding 3 is gone by construction.

#### 2. `internal/store/session_store.go` — `PostgresSessionStore.UpdateCursors`

Add a per-key monotonicity CAS, pinned to `COLLATE "C"` so the comparison is
locale-independent:

```go
// UpdateCursors updates event cursors via JSONB merge, with a per-key
// monotonicity guard. A write is only applied if the new cursor is
// strictly greater than the stored value (lexicographic, COLLATE "C") for
// every key being written. Writes that lose the CAS race are silently
// dropped — they are not errors, they are evidence that another writer
// committed a higher cursor, which is the correct outcome.
//
// Why JSONB does not need this guard for partial-update friendliness:
// the merge `event_cursors || $1::jsonb` is last-write-wins per key. The
// guard makes it monotone-only per key. Disjoint keys (different streams)
// are unaffected because the guard does not block them.
//
// CRITICAL: cursor values MUST be monotonic ULIDs (core.NewULID), not
// random ULIDs (idgen.New). Non-monotonic cursors can produce lex-inverted
// values within the same millisecond, causing legitimate cursor advances to
// be silently rejected by the CAS. The ruleguard rule
// EventIDMustBeMonotonic in gorules/rules.go enforces this for
// core.Event{} struct literals.
func (s *PostgresSessionStore) UpdateCursors(ctx context.Context, id string, cursors map[string]ulid.ULID) error {
    if len(cursors) == 0 {
        return nil
    }
    // For the common single-key case (which is what replayAndSend produces)
    // we use a single-key CAS. Multi-key writes are not currently produced
    // by any caller; if they appear in the future, this function should be
    // refactored to apply per-key CAS in a single statement.
    if len(cursors) != 1 {
        return oops.Code("UNSUPPORTED").
            With("operation", "update cursors").
            With("session_id", id).
            With("key_count", len(cursors)).
            Errorf("multi-key cursor updates are not supported")
    }
    var streamKey string
    var newCursor ulid.ULID
    for k, v := range cursors {
        streamKey, newCursor = k, v
    }
    cursorsJSON, err := json.Marshal(cursors)
    if err != nil {
        return oops.With("operation", "marshal cursors").With("session_id", id).Wrap(err)
    }
    _, err = s.pool.Exec(ctx,
        `UPDATE sessions
            SET event_cursors = event_cursors || $1::jsonb,
                updated_at = now()
          WHERE id = $2
            AND (
                event_cursors->>$3 IS NULL
                OR (event_cursors->>$3) COLLATE "C" < ($4::text) COLLATE "C"
            )`,
        cursorsJSON, id, streamKey, newCursor.String())
    if err != nil {
        return oops.With("operation", "update cursors").With("session_id", id).Wrap(err)
    }
    // RowsAffected==0 is normal: another writer beat us with a higher cursor.
    // It is not an error condition.
    return nil
}
```

The single-key restriction reflects current usage (only `replayAndSend` calls
this, and it always passes one key). Failing loudly on multi-key writes is
better than silently applying CAS only to some keys.

#### 3. `internal/session/memstore.go` — `MemStore.UpdateCursors`

Add the same monotonicity guard in Go so unit tests exercise the same
contract as production:

```go
func (m *MemStore) UpdateCursors(_ context.Context, id string, cursors map[string]ulid.ULID) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    info, ok := m.sessions[id]
    if !ok {
        return oops.Code("SESSION_NOT_FOUND").
            With("session_id", id).
            Errorf("session not found")
    }
    if info.EventCursors == nil {
        info.EventCursors = make(map[string]ulid.ULID)
    }
    for k, v := range cursors {
        existing, hasExisting := info.EventCursors[k]
        if hasExisting && existing.String() >= v.String() {
            // Reject regressions; preserve the existing higher cursor.
            continue
        }
        info.EventCursors[k] = v
    }
    return nil
}
```

#### 4. `internal/command/types.go` — `BroadcastSystemMessage`

Replace `idgen.New()` with `core.NewULID()` so system broadcasts produce
monotonic event IDs:

```go
event := core.Event{
    ID:        core.NewULID(),  // monotonic; required for Replay/cursor correctness
    Stream:    stream,
    Type:      core.EventTypeSystem,
    ...
}
```

#### 5. `internal/core/ulid.go` — doc comment

Replace the current sparse comment with an explicit one (full text in the
"Doc + lint enforcement" section below).

#### 6. `internal/idgen/id.go` — doc comment

Add an explicit "do NOT use for event IDs" warning (full text below).

#### 7. `gorules/rules.go` — new file

Custom `gocritic` ruleguard rule that fails the build on
`core.Event{... ID: idgen.New() ...}` and on any `ulid.Make()` use.

#### 8. `.golangci.yaml` — enable ruleguard

One block under the existing `gocritic` settings to load the rules file.

#### 9. `Taskfile.yaml` — drop the bash `ulid.Make()` check

The ruleguard rule subsumes it. Removes a line of out-of-band lint.

#### 10. `CLAUDE.md` — add ULID generation guidance

Sibling rule to the existing "Random Number Generation" section.

### Doc + lint enforcement

**`internal/core/ulid.go`:**

```go
// NewULID generates a monotonic-within-millisecond ULID using crypto/rand.
//
// Use this for: event IDs (core.Event.ID), session IDs, and any identifier
// whose lexicographic order MUST match arrival order. The PostgresEventStore
// relies on this property — Replay uses `WHERE id > afterID ORDER BY id` and
// PostgresSessionStore.UpdateCursors uses a per-key monotonicity CAS on the
// cursor JSONB value. A non-monotonic event ID can produce a lex-inverted
// pair within the same millisecond, which silently breaks both replay
// (the second event is skipped) and cursor advances (the second cursor is
// rejected by the CAS).
//
// Do NOT use idgen.New() for these. The ruleguard rule EventIDMustBeMonotonic
// in gorules/rules.go enforces this for core.Event{} struct literals.
func NewULID() ulid.ULID { ... }
```

**`internal/idgen/id.go`:**

```go
// New generates a ULID with fresh crypto/rand entropy on every call.
//
// Use this for: entity primary keys (players, sessions, locations,
// characters, exits, objects, policies, audit rows) where the ID is pure
// identity and there is no requirement for IDs minted in temporal order to
// also sort in temporal order.
//
// Do NOT use this for event IDs (core.Event.ID). Two calls in the same
// millisecond produce IDs in random lexicographic order, which silently
// breaks PostgresEventStore.Replay (ORDER BY id, WHERE id > afterID) and
// PostgresSessionStore.UpdateCursors monotonicity. Use core.NewULID()
// instead. The ruleguard rule EventIDMustBeMonotonic in gorules/rules.go
// enforces this for core.Event{} struct literals.
func New() ulid.ULID { ... }
```

**`gorules/rules.go`** (new file, build-tagged so it does not compile with the
project):

```go
//go:build ruleguard
// +build ruleguard

// Package gorules contains custom go-ruleguard rules loaded by gocritic.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// EventIDMustBeMonotonic ensures core.Event{} literals use core.NewULID()
// (monotonic-within-millisecond entropy), not idgen.New() (fresh random
// per call). Non-monotonic event IDs silently break PostgresEventStore.Replay
// and PostgresSessionStore cursor monotonicity.
func EventIDMustBeMonotonic(m dsl.Matcher) {
    m.Match(`core.Event{$*_, ID: idgen.New(), $*_}`).
        Report(`event IDs must use core.NewULID() (monotonic), not idgen.New() (random) — see internal/core/ulid.go`)
}

// ULIDMakeForbidden replaces the bash check in Taskfile.yaml that blocked
// ulid.Make() in production code. ulid.Make() uses math/rand internally;
// use idgen.New() for entity IDs or core.NewULID() for event IDs.
func ULIDMakeForbidden(m dsl.Matcher) {
    m.Match(`ulid.Make()`).
        Report(`use idgen.New() for entity IDs or core.NewULID() for event IDs; ulid.Make() uses math/rand`)
}
```

**`.golangci.yaml`** addition under `linters.settings.gocritic`:

```yaml
gocritic:
  enabled-tags:
    - diagnostic
    - style
    - performance
  enabled-checks:
    - ruleguard
  disabled-checks:
    - hugeParam
  settings:
    ruleguard:
      rules: gorules/rules.go
```

**`CLAUDE.md`** addition under "Random Number Generation":

```markdown
### ULID Generation

Two ULID generators exist; the choice matters because the event store relies
on lex order matching arrival order.

| Use case | Generator | Why |
| --- | --- | --- |
| **Event IDs** (`core.Event.ID`), session IDs | `core.NewULID()` | Monotonic within a millisecond. PostgresEventStore.Replay uses `WHERE id > afterID ORDER BY id`; cursor advances use a SQL monotonicity CAS. Non-monotonic event IDs silently break both. |
| **Entity primary keys** (players, locations, characters, exits, objects, policies) | `idgen.New()` | Identity, not ordering. Fresh crypto/rand entropy per call. |

Enforced by the `EventIDMustBeMonotonic` ruleguard rule in `gorules/rules.go`
(loaded via `gocritic`). New `core.Event{}` literals using `idgen.New()` will
fail `task lint`.
```

## Testing

Test-driven: every fix gets a failing-first test before the corresponding
implementation change.

### Finding 1 — duplicate delivery on fast reconnect

**Integration test** (`test/integration/session/session_persistence_integration_test.go`,
new `It` under "Reconnect flow"):

```go
It("commits cursor synchronously so a fast reconnect does not re-deliver the latest event", func() {
    sessionID, _ := loginAsGuest(testCtx, grpcCli)

    subCtx, subCancel := context.WithCancel(testCtx)
    stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{SessionId: sessionID})
    Expect(err).NotTo(HaveOccurred())

    _, err = grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
        SessionId: sessionID, Command: "say hello",
    })
    Expect(err).NotTo(HaveOccurred())

    // Drain until the live `say` event arrives.
    var liveSayID string
    Eventually(func() string {
        ev, recvErr := stream.Recv()
        if recvErr != nil { return "" }
        if frame := ev.GetEvent(); frame != nil && frame.GetType() == "say" {
            liveSayID = frame.GetId()
            return frame.GetType()
        }
        return ""
    }).WithTimeout(5 * time.Second).Should(Equal("say"))

    // Cancel the subscription. Under the fix, the cursor is committed
    // BEFORE the live loop returns, so the next read of the session must
    // see the say event ID with NO polling — any polling here would
    // mask a regression to the async-write behavior.
    subCancel()
    // Drain the stream so the handler goroutine exits.
    for { if _, e := stream.Recv(); e != nil { break } }

    locationStream := world.LocationStream(startLocation)
    sess, getErr := env.sessionStore.Get(testCtx, sessionID)
    Expect(getErr).NotTo(HaveOccurred())
    Expect(sess.EventCursors[locationStream].String()).To(Equal(liveSayID),
        "cursor must equal the latest sent event ID immediately after subscription teardown — no polling")

    // Re-subscribe with replay_from_cursor=true. There must be ZERO say
    // events on the location stream (no missed events appended in between).
    replayCtx, replayCancel := context.WithTimeout(testCtx, 2*time.Second)
    defer replayCancel()
    replayStream, err := grpcCli.Subscribe(replayCtx, &corev1.SubscribeRequest{
        SessionId: sessionID, ReplayFromCursor: true,
    })
    Expect(err).NotTo(HaveOccurred())

    sayCount := 0
    for {
        ev, recvErr := replayStream.Recv()
        if recvErr != nil { break }
        frame := ev.GetEvent()
        if frame != nil && frame.GetType() == "say" && frame.GetStream() == locationStream {
            sayCount++
        }
        if frame := ev.GetControl(); frame != nil &&
            frame.Signal == corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE {
            break
        }
    }
    Expect(sayCount).To(Equal(0), "fast reconnect must not re-deliver the say event")
})
```

**Pre-fix behavior:** flaky — the immediate read after `subCancel()` may or
may not see the cursor depending on whether the goroutine has committed yet.
Run with `-count=20 -race` to make the flake observable.

**Post-fix behavior:** deterministic. The contract is testable without seams.

### Finding 2 — cursor regression

**Integration test directly against `PostgresSessionStore`**
(`internal/store/session_store_integration_test.go`, new test):

```go
It("rejects a cursor regression for the same stream key", func() {
    // The CAS guard in UpdateCursors must preserve the highest cursor
    // ever stored for a (session, stream) pair. Regression attempts
    // (e.g., from a concurrent Subscribe that observed an earlier
    // event) must be silently ignored — RowsAffected==0 is not an
    // error, it just means another writer won with a higher cursor.
    ctx := context.Background()
    info := newTestSession("sess-cas-regression")
    Expect(sessionStore.Set(ctx, info.ID, info)).To(Succeed())

    // Mint two cursors with core.NewULID so they are strictly monotonic.
    // The second one is the lex-larger ("higher") cursor.
    earlier := core.NewULID()
    time.Sleep(1 * time.Millisecond)
    later := core.NewULID()
    Expect(earlier.String() < later.String()).To(BeTrue(),
        "earlier ULID must be lex-less than later ULID")

    streamKey := "location:room-cas"

    // First write: later (the higher cursor).
    Expect(sessionStore.UpdateCursors(ctx, info.ID, map[string]ulid.ULID{
        streamKey: later,
    })).To(Succeed())

    // Second write: earlier (a regression). Must not error, but
    // must not overwrite the stored later.
    Expect(sessionStore.UpdateCursors(ctx, info.ID, map[string]ulid.ULID{
        streamKey: earlier,
    })).To(Succeed(),
        "regression attempts must not be errors — CAS rows_affected==0 is normal")

    got, err := sessionStore.Get(ctx, info.ID)
    Expect(err).NotTo(HaveOccurred())
    Expect(got.EventCursors[streamKey]).To(Equal(later),
        "stored cursor must remain the higher (later) value")
})
```

**Equivalent unit test** against `MemStore` to anchor the same contract in
the in-memory implementation.

**Pre-fix behavior:** fails — last write wins, smaller cursor is stored.

**Post-fix behavior:** passes deterministically. No race induction needed.

### Finding 3 — lost writes on shutdown

**Integration test** (`test/integration/session/session_persistence_integration_test.go`):

```go
It("commits cursor before grpcSubsystem.Stop returns", func() {
    sessionID, _ := loginAsGuest(testCtx, grpcCli)

    subCtx, subCancel := context.WithCancel(testCtx)
    stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{SessionId: sessionID})
    Expect(err).NotTo(HaveOccurred())

    _, err = grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
        SessionId: sessionID, Command: "say hello",
    })
    Expect(err).NotTo(HaveOccurred())

    // Drain until live say arrives.
    var liveSayID string
    Eventually(func() string {
        ev, recvErr := stream.Recv()
        if recvErr != nil { return "" }
        if frame := ev.GetEvent(); frame != nil && frame.GetType() == "say" {
            liveSayID = frame.GetId()
            return frame.GetType()
        }
        return ""
    }).WithTimeout(5 * time.Second).Should(Equal("say"))

    subCancel()
    for { if _, e := stream.Recv(); e != nil { break } }

    // Stop the gRPC server. Under the fix, GracefulStop blocks until the
    // live loop exits, which means the cursor commit has already run.
    grpcServer.GracefulStop()

    // Read the cursor through a fresh store handle to defeat any in-process
    // caching. The session row must reflect the latest cursor.
    info, err := env.sessionStore.Get(testCtx, sessionID)
    Expect(err).NotTo(HaveOccurred())
    locationStream := world.LocationStream(startLocation)
    Expect(info.EventCursors[locationStream].String()).To(Equal(liveSayID),
        "cursor must reflect the latest sent event after Stop returns")
})
```

**Pre-fix behavior:** intermittently fails — the cursor write may or may not
have run before `GracefulStop` returned, depending on goroutine scheduling.

**Post-fix behavior:** deterministic. There are no async writes left to drain;
the live loop's own goroutine performed every commit, and `GracefulStop`
already waits for handler goroutines to exit.

### ULID monotonicity unit test

`internal/core/ulid_test.go` (extended):

```go
func TestNewULIDIsMonotonicWithinMillisecond(t *testing.T) {
    const n = 10_000
    var prev ulid.ULID
    for i := 0; i < n; i++ {
        cur := NewULID()
        if i > 0 && prev.String() >= cur.String() {
            t.Fatalf("non-monotonic ULIDs at index %d: prev=%s cur=%s",
                i, prev.String(), cur.String())
        }
        prev = cur
    }
}
```

Anchors the invariant the SQL CAS depends on. Fails immediately if the
generator regresses.

### `BroadcastSystemMessage` regression test

`internal/command/types_test.go` (extended): assert the event ID returned by
`BroadcastSystemMessage` (via a captured `EventStore` mock) is monotonic
across two consecutive calls.

## Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| Slow DB blocks event delivery | 1-second `cursorCommitTimeout` bounds the per-notification stall at a perceptible-but-tolerable ceiling (1000× the healthy-DB latency). On timeout, the write is dropped and logged at error level; the loop continues. Failure mode degrades to today's "duplicate-on-reconnect" behavior, never worse. |
| Future code re-introduces async cursor writes | The `persistCursorAsync` helper is deleted, and the function-level comment on `replayAndSend` documents the contract. Code review catches reintroduction. |
| Future code mints event IDs via `idgen.New()` | `EventIDMustBeMonotonic` ruleguard rule fails the build on any `core.Event{... ID: idgen.New() ...}` literal. Doc comments on both generators flag the boundary at code-write time. |
| Future caller passes a multi-key map to `UpdateCursors` | The function returns `UNSUPPORTED` and the test for it fails loudly. Adding multi-key CAS later is mechanical (extend the WHERE clause to AND together per-key checks). |
| Postgres collation reorders ASCII | The CAS uses `COLLATE "C"` explicitly, eliminating dependence on the database's default collation. |

## Migration / rollback

Pure code change. No schema migration. No data migration. Rollback is
`git revert`.

The behavior change observable to clients is: cursor reflects the latest
delivered event *immediately* after the live loop processes a notification,
instead of "eventually." Existing clients tolerate the eventually-consistent
behavior; the new strictly-consistent behavior is a strict improvement.

## Open questions

None — all design questions were resolved during brainstorming.

## References

- Bead: holomush-43nd
- Discovered from: holomush-u37v (PR #197)
- Strict cursor wait test: `test/integration/session/session_persistence_integration_test.go` (Reconnect flow spec)
- Event store ordering contract: `internal/store/postgres.go:130` (`Replay`)
- Existing async cursor write: `internal/grpc/server.go:422` (`persistCursorAsync`)
- ULID monotonic generator: `internal/core/ulid.go:21`
- ULID random generator: `internal/idgen/id.go:21`
- System broadcast bug site: `internal/command/types.go:602`
