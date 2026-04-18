# B13 — Web Client Reload Backfill via QueryStreamHistory

**Date:** 2026-04-17
**Bead:** [holomush-oy6e.13](../../../.beads/issues.jsonl)
**Epic:** holomush-oy6e — Server-Owned Focus Substrate
**Depends on:** B7 (Subscribe refactor, PR #215), B9 (QueryStreamHistory RPC, PR #223), B10 (core-scenes adoption, PR #230)

## 1. Context

After B7 + B10 landed, the Subscribe stream became "live + cursor-resume" only. On a fresh page load the in-memory Svelte stores are empty; on reload, the persisted session cursor is at or near the tail, so the server's cursor-based replay finds no events. The user sees an empty terminal — none of the scrollback they had a moment earlier.

This is correct behavior for Subscribe (its job is to deliver missed events while detached, not to repopulate a UI scrollback the client lost). The fix is on the client: hydrate the panel from the event store via `QueryStreamHistory` (B9), then let Subscribe handle live events.

Until now the only consumer of QueryStreamHistory has been a direct `fetch()` test. B13 wires it into the SvelteKit web terminal and un-skips the canonical reload-replay E2E test.

## 2. Goals

- **MUST** populate the web-terminal scrollback on mount with prior events from all streams the session is subscribed to.
- **MUST** visually distinguish replayed (already-seen) events from live events via the existing `.dimmed` CSS treatment.
- **MUST** preserve event order across reload (assertion in the un-skipped test).
- **MUST** deterministically dedup events between QueryStreamHistory backfill and Subscribe stream output via a stable event identifier — the detach + reattach + reload path produces systematic overlap, not a rare race.
- **MUST** be reusable by future web panels (channel UI, scene UI) — a shared web-side helper module.
- **SHOULD** complete in <1s under typical load.

## 3. Non-Goals

- **NOT** persisting scrollback to localStorage (no client-side cache).
- **NOT** scroll-up pagination for older history (single page per stream; follow-up bead).
- **NOT** changing telnet behavior (telnet retains its local terminal scrollback across normal reconnects).
- **NOT** changing what the server's Subscribe handler delivers (no protocol changes to Subscribe).
- **NOT** generalizing the helper across non-web transports — telnet/native clients can call `CoreService.QueryStreamHistory` directly without going through this helper.

## 4. Architecture

### 4.1 Layering

```text
┌──────────────────────────────────────────────────────────┐
│ Per-viewer UI                                            │
│   web-terminal: replayActive store, "--- REPLAY ---"     │
│   future channel UI: skeleton/spinner (own affordance)   │
│   future scene UI: own affordance                        │
└──────────────────────────────────────────────────────────┘
                          │
                          ▼
┌──────────────────────────────────────────────────────────┐
│ Web-portable helper (web/src/lib/backfill/)              │
│   backfillStreams(client, sessionId, streams[])          │
│     → fan-out, retry, merge by (timestamp, event_id)     │
│   (used by any web panel; non-web clients call core      │
│    RPCs directly with corev1.Event types)                │
└──────────────────────────────────────────────────────────┘
                          │
                          ▼
┌──────────────────────────────────────────────────────────┐
│ Web gateway RPCs (cookie auth)                           │
│   WebListSessionStreams, WebQueryStreamHistory           │
└──────────────────────────────────────────────────────────┘
                          │
                          ▼
┌──────────────────────────────────────────────────────────┐
│ CoreService (gRPC)                                       │
│   ListSessionStreams (NEW), QueryStreamHistory (B9)      │
│     ListSessionStreams wraps focusCoordinator.RestoreFocus│
└──────────────────────────────────────────────────────────┘
```

### 4.2 Data flow on reload

```text
mount
  ├── set replayActive = true
  ├── start Subscribe (events buffered, NOT rendered)
  └── call WebListSessionStreams(sessionId)
        └── fan-out WebQueryStreamHistory per stream
              └── merge by timestamp ascending
              └── feed each event to routeEvent(event, replayed=true)
                    └── eventRouter routes to terminal scrollback (dimmed)
                        and suppresses replayed arrive/leave/exit_update
                        but always applies authoritative location_state
        └── set replayActive = false
        └── drain buffered Subscribe events as routeEvent(event, replayed=false)
        └── continue normal Subscribe live loop
```

## 5. Server changes

### 5.1 New RPC: `CoreService.ListSessionStreams`

**MUST** add to `api/proto/holomush/core/v1/core.proto`:

```protobuf
message ListSessionStreamsRequest {
  string session_id = 1;
  RequestMeta meta = 2;
}

message ListSessionStreamsResponse {
  repeated string streams = 1;
}

service CoreService {
  // ... existing RPCs ...
  rpc ListSessionStreams(ListSessionStreamsRequest) returns (ListSessionStreamsResponse);
}
```

**Handler** (`internal/grpc/server.go` or new `internal/grpc/list_session_streams.go`):

- **MUST** mirror the `QueryStreamHistory` auth pattern (`internal/grpc/query_stream_history.go:55-72`): validate `session_id` non-empty, load session via `sessionStore.Get`, return `SESSION_NOT_FOUND` on miss, `SESSION_EXPIRED` if `info.IsExpired()`. **MUST NOT** require `player_session_token` — B9 chose not to require it for sibling read RPCs and B13 follows that convention. (B9's auth model is debatable — if revisited, both RPCs should change together.)
- **MUST** call `s.focusCoordinator.RestoreFocus(ctx, sessionID)` (re-uses existing logic — character + location + plugin-contributed + scene policy streams + presenting, deduped).
- **MUST** replicate `Subscribe`'s focusCoordinator-nil fallback (`server.go:787-816`): when `focusCoordinator == nil`, manually assemble character + location + plugin-contributed streams. This guarantees ListSessionStreams cannot return a different stream set than Subscribe under any server configuration.
- **MUST** return `plan.Streams[].Stream` as a flat string array. The `PresentingStream` field is metadata about which of the existing streams is "primary" for live UX; it does not need to be exposed for backfill purposes.
- **MUST NOT** include `ReplayMode` or other fields — clients don't need them for backfill.
- **MUST NOT** mutate session state (pure read).

The handler is a thin wrapper — all the substantive logic lives in `RestoreFocus`.

### 5.2 New RPC: `WebService.WebListSessionStreams`

**MUST** add to `api/proto/holomush/web/v1/web.proto`:

```protobuf
message WebListSessionStreamsRequest {
  string session_id = 1;
}

message WebListSessionStreamsResponse {
  repeated string streams = 1;
}

service WebService {
  // ... existing RPCs ...
  rpc WebListSessionStreams(WebListSessionStreamsRequest) returns (WebListSessionStreamsResponse);
}
```

**Handler** (`internal/web/handler.go`):

- **MUST** follow the `WebQueryStreamHistory` pattern (`handler.go:396-432`): `rpcCtx` with `rpcTimeout`, proxy directly to `CoreClient.ListSessionStreams`, pass core errors through unwrapped (`//nolint:wrapcheck`) so clients can distinguish `SESSION_EXPIRED` / `SESSION_NOT_FOUND`.
- **MUST NOT** add additional auth checks — core enforces (matching B9's split).

### 5.3 Add `event_id` to web `GameEvent` for deterministic dedup

The detach + reattach + reload path produces systematic event overlap (see §7), not a rare race. Mitigating it without a stable identifier is impossible.

**MUST** add `event_id` to `api/proto/holomush/web/v1/web.proto`:

```protobuf
message GameEvent {
  string type = 1;
  string category = 2;
  string format = 3;
  EventChannel display_target = 4;
  int64 timestamp = 5;
  string actor = 6;
  string text = 7;
  google.protobuf.Struct metadata = 8;
  string event_id = 9; // ULID; populated from corev1.EventFrame.id
}
```

**Translation**: `internal/web/translate.go` (where `translateEvent` lives) **MUST** copy `corev1.EventFrame.id` into `webv1.GameEvent.event_id`. This single translator is shared between Subscribe and QueryStreamHistory paths, so the field is populated consistently for both sources.

**Tests**: extend `internal/web/translate_test.go` to verify `event_id` propagation. The existing test pattern (`got := h.translateEvent(ev)` x15) makes this trivial.

This change is small (one proto field, one translation line, regen) and unblocks deterministic client-side dedup.

## 6. Client changes

### 6.1 New module: `web/src/lib/backfill/streamBackfill.ts`

**Public API:**

```typescript
export interface BackfillResult {
  events: GameEvent[];        // merged, ordered ascending by (timestamp, event_id)
  failedStreams: string[];    // streams whose history could not be fetched
}

export async function backfillStreams(
  client: WebServiceClient,
  sessionId: string,
  streams: string[],
  opts?: { count?: number; signal?: AbortSignal },
): Promise<BackfillResult>;
```

**Behavior:**

- **MUST** return `{events: [], failedStreams: []}` immediately if `streams.length === 0` — no RPC calls.
- **MUST** fan out one `WebQueryStreamHistory` call per stream in parallel.
- **MUST** retry once with ~500ms delay on transient errors (network, 5xx); permanent errors (`SESSION_EXPIRED`, `SESSION_NOT_FOUND`, `STREAM_ACCESS_DENIED`, `INVALID_ARGUMENT`) **MUST NOT** be retried.
- **MUST** continue when a stream's call fails after retry — record in `failedStreams`, omit from `events`.
- **MUST** merge results from all successful streams sorted ascending by `timestamp`, with `event_id` (ULID) as the tiebreaker for same-millisecond events.
- **MUST** default `count` to 150 (server's default; matches B9 spec).
- **MUST** honor `AbortSignal` to allow cancel on component unmount.
- **MUST** treat ConnectRPC `Unimplemented` errors on `WebListSessionStreams` (when called by the integration in §6.2) as a non-fatal staged-deployment scenario — the integration falls back to empty backfill rather than failing loudly. The helper itself does not wrap that call; this is handled in the caller.

**Why a separate module:** The web-terminal is the first consumer; future channel and scene panels will call this same helper with their own stream sets and own loading affordances. Module location avoids `terminal/` namespace.

**Scope boundary:** This helper is web-portable across web panels (terminal, channel, scene) — it takes a `WebServiceClient` and returns `webv1.GameEvent`. It is NOT cross-transport portable (telnet/native clients call `CoreService.QueryStreamHistory` directly with `corev1.Event` types — no shared abstraction is needed).

### 6.2 Web-terminal integration: `web/src/routes/(authed)/terminal/+page.svelte`

In `onMount`, replace the current `startStreaming()` call with a sequence:

```typescript
async function hydrateAndStream() {
  const liveBuffer: GameEvent[] = [];
  const seenEventIds = new Set<string>();
  let backfillDone = false;

  // Start Subscribe in parallel; buffer events until backfill finishes.
  // NOTE: the streamEvents call MUST NOT pass replayFromCursor — that field
  // is reserved in the proto (web.proto:88-90) and the current code that
  // still passes it is dead.
  const subscribePromise = (async () => {
    for await (const response of client.streamEvents({ sessionId }, { signal })) {
      // ... existing control-frame handling ...
      if (response.frame.case === 'event') {
        const ev = response.frame.value;
        if (!backfillDone) {
          liveBuffer.push(ev);
        } else {
          if (ev.eventId && seenEventIds.has(ev.eventId)) continue;
          if (ev.eventId) seenEventIds.add(ev.eventId);
          routeEvent(ev, false);
        }
      }
    }
  })();

  // Backfill in parallel.
  replayActive.set(true);
  try {
    let streams: string[] = [];
    try {
      const resp = await client.webListSessionStreams({ sessionId });
      streams = resp.streams;
    } catch (e) {
      // Staged-deployment fallback: server may not yet have ListSessionStreams.
      if (isUnimplementedError(e)) {
        console.info('[backfill] WebListSessionStreams not available, skipping backfill');
      } else {
        console.warn('[backfill] enumeration failed', e);
      }
    }
    const { events, failedStreams } = await backfillStreams(client, sessionId, streams);
    if (failedStreams.length > 0) {
      console.warn('[backfill] streams failed', { failedStreams });
    }
    for (const ev of events) {
      if (ev.eventId) seenEventIds.add(ev.eventId);
      routeEvent(ev, true);
    }
  } finally {
    backfillDone = true;
    replayActive.set(false);
    // Drain anything that arrived during backfill, deduping against backfill.
    for (const ev of liveBuffer) {
      if (ev.eventId && seenEventIds.has(ev.eventId)) continue;
      if (ev.eventId) seenEventIds.add(ev.eventId);
      routeEvent(ev, false);
    }
    liveBuffer.length = 0;
  }

  await subscribePromise;
}
```

**Notes:**

- `replayActive` true → existing `--- REPLAY ---` indicator and StatusBar `syncing` flag activate automatically.
- `routeEvent(event, true)` already handles state-event suppression correctly (see `eventRouter.ts`: `arrive`/`leave`/`exit_update` are skipped during replay; `location_state` is always applied as authoritative).
- **`streamReadyGate` semantics under B13:** the existing gate (`+page.svelte:93-124`) resolves on Subscribe's `REPLAY_COMPLETE`, which now arrives near-instantly (cursor at tail = empty replay). It does NOT wait for backfill. This is deliberate: commands sent during backfill produce a response event that arrives via Subscribe, gets buffered, and drains as `replayed=false` after backfill completes. The user sees command responses appear after their dimmed scrollback — correct UX. Document this in code comments.
- **Subscribe replay-phase events are treated as live (`replayed=false`).** They represent events the user MISSED while detached — new to the user, not part of pre-reload scrollback. Dimming them would be misleading.
- **Reattach race**: if the new tab loses the reattach CAS (`server.go:769-773` returns `SESSION_REATTACH_LOST`), Subscribe errors immediately. Backfill still completes (it doesn't attach — see B9 invariant I-13). The user sees dimmed history but no live stream and the existing error UX (`error = 'Connection lost...'`) takes over. Document; don't change.

**`isUnimplementedError`**: small utility checking ConnectRPC error code === `Code.Unimplemented`. Lives in `web/src/lib/connect/errors.ts` (new file) or alongside the helper.

### 6.3 Existing dimming infrastructure (no changes)

- `web/src/lib/stores/terminalStore.ts` already has `appendLine(event, replayed)` with the `replayed` flag.
- `web/src/lib/components/terminal/TerminalView.svelte` already renders `--- LIVE ---` separator at the replay→live boundary and passes `dimmed={line.replayed}` to `EventRenderer`.
- `web/src/lib/components/terminal/EventRenderer.svelte` already has `.dimmed { opacity: 0.5 }`.

## 7. Concurrency & race handling

### 7.1 Why deterministic dedup is required

Three event-overlap scenarios exist:

1. **Clean reload** (cursor at tail): Subscribe replay = 0 events; QueryStreamHistory returns events ≤ tail. Disjoint. No dupes.
2. **Tight race during clean reload**: an event commits between QueryStreamHistory's DB snapshot and Subscribe's first delivery. At most 1-2 events overlap. Rare.
3. **Detach + accumulate + reload**: user closes the tab (session detaches); events E1…EN accumulate while detached (cursor stays at C0 because there's no subscriber to advance it); user reopens the page. Subscribe reattaches (`server.go:763-774`), `replayRestorePlan` replays E1…EN from cursor C0 (`server.go:866`), and QueryStreamHistory's "latest 150" includes E1…EN. **Every** Ei is delivered twice. This is not a race — it is the normal detach-then-reattach path, exercised by the session-persistence integration tests.

Scenario 3 is what makes deterministic dedup mandatory. The `event_id` field added in §5.3 supports it.

### 7.2 Dedup mechanism

Client maintains a `Set<string>` of `event_id`s seen during the mount lifecycle:

- Backfill events added to set as they're rendered.
- Subscribe events checked against set; if present, skipped.

Buffer-drain after backfill applies the same check (live events arriving during backfill might already be in the backfill response if they straddled the snapshot boundary).

**Future scenes with cursor-independent replay modes** (e.g., a hypothetical `OnRestore` returning `BoundedTail` for "always show last 50") would systematically duplicate without dedup. The `event_id` mechanism handles them too without further changes.

### 7.3 Live-event buffering

Subscribe events that arrive before backfill completes are appended to an in-memory buffer (not rendered). When backfill finishes:

1. Backfill events render in timestamp order with `replayed=true`, populating the seen-IDs set.
2. Buffer drains in FIFO order with `replayed=false`, skipping events already in the seen-IDs set.

This preserves the "all-history-first, then live" render order and keeps the existing `--- LIVE ---` separator semantics intact.

### 7.4 Invariant on server-side replay modes (load-bearing)

For the dedup model to remain correct, **the server's `RestoreFocus` MUST NOT use cursor-independent replay modes** (`BoundedTail` with no cursor floor, `LiveOnly` is fine since it skips replay entirely). Today, all `OnRestore` paths return `FromCursor` (`scenepolicy/policy.go:44-50`). If a future policy violates this, the dedup `Set<string>` will still catch the duplicates correctly — but the user-visible cost is fetching events that get immediately discarded. Worth flagging in scenepolicy's tests.

## 8. Error handling

| Failure | Behavior |
|---------|----------|
| `WebListSessionStreams` returns error | Log, render empty backfill, Subscribe still runs |
| Single `WebQueryStreamHistory` call fails | Retry once with ~500ms delay; if still failing, log and omit that stream |
| `WebQueryStreamHistory` returns empty | No events appended; not an error |
| Component unmount during backfill | `AbortController` cancels in-flight calls |
| Subscribe error during backfill | Existing error handling in `+page.svelte` runs; backfill drains buffer to whatever rendered |

**MUST NOT** block the terminal UI — user can always type commands (subject to existing `streamReadyGate`).

## 9. Test plan

### 9.1 Un-skip the canonical E2E test

`web/e2e/terminal.spec.ts` test `'page reload replays prior events from multiple guests'` (currently `test.skip` at L289). The test already asserts:

- All three pre-reload events reappear after reload.
- Events are dimmed (CSS `.dimmed` class on each replayed line).
- Order matches `eventsBefore` exactly.
- Live event after reload is NOT dimmed.
- DB confirms 4 events on `location:<id>` stream.

**MUST** add an additional assertion: the `--- LIVE ---` separator appears between the last replayed and the first live event after reload. The existing `TerminalView.svelte:50-51` renders `<div class="separator">--- LIVE ---</div>`; assert via `page.locator('.separator', { hasText: '--- LIVE ---' })`. This catches regressions in the boundary semantics that the dimmed/not-dimmed counts alone would miss.

The existing `'WebQueryStreamHistory returns events through the web gateway'` test (L232-284) verifies gateway plumbing in isolation and complements (not duplicates) the un-skipped test. **MUST** keep both — they cover orthogonal failure modes.

### 9.2 New tests

| Layer | Test | Location |
|-------|------|----------|
| Core | `ListSessionStreams` returns flat stream list from `RestoreFocus` plan | `internal/grpc/list_session_streams_test.go` |
| Core | `ListSessionStreams` returns `SESSION_NOT_FOUND` / `SESSION_EXPIRED` / `INVALID_ARGUMENT` correctly | same |
| Core | `ListSessionStreams` falls back to manual ambient stream assembly when `focusCoordinator == nil` | same |
| Core integration | `ListSessionStreams` returns character + location streams for a fresh session | `test/integration/list_session_streams/` (model on `test/integration/stream_history/`) |
| Core integration | `ListSessionStreams` returns scene streams for a session with active scene focus | same |
| Core integration | `ListSessionStreams` returns plugin-contributed streams (stub `StreamContributor`) | same |
| Web | `translateEvent` populates `event_id` from `corev1.EventFrame.id` | `internal/web/translate_test.go` (extend) |
| Web | `WebListSessionStreams` proxies to core, passes errors through | `internal/web/handler_test.go` |
| Client unit | `backfillStreams` returns empty result for empty stream list (no RPCs) | `web/src/lib/backfill/streamBackfill.test.ts` (new vitest) |
| Client unit | `backfillStreams` merges by `(timestamp, event_id)` ascending across streams | same |
| Client unit | `backfillStreams` retries once on transient error then falls through; permanent errors fail fast | same |
| Client unit | `backfillStreams` honors `AbortSignal` mid-flight | same |
| Client unit | `+page.svelte` integration: dedup set skips events present in both backfill and live buffer | new vitest in `web/src/routes/(authed)/terminal/page.test.ts` (or extend) |
| Client E2E | the un-skipped reload-replay test, with separator assertion | `web/e2e/terminal.spec.ts` |
| Client E2E | reload after detach + accumulated events: no duplicates | new test in same file (exercises §7.1 scenario 3) |

### 9.3 Test invariants

- **MUST NOT** mock the database — integration tests use testcontainers Postgres.
- **MUST NOT** introduce flaky timing assertions; use `expect(...).toPass({ timeout })` patterns already in the file.
- The two-layer auth (membership gate + ABAC) is exercised by B9's existing tests; B13 doesn't need to re-test it.

## 10. Out of scope / follow-ups

| Item | Disposition |
|------|-------------|
| Scroll-up pagination for older history | Follow-up bead — UX-driven, not architecture-driven |
| localStorage scrollback persistence | Follow-up bead — `event_id` (now in B13 scope) is the prerequisite |
| Backfill for telnet client process restart | Future, optional — telnet calls `CoreService.QueryStreamHistory` directly with no new wrapper needed |
| Loading affordances for non-terminal panels (channel UI, scene UI) | Per-viewer when those panels land |
| `task gowork` duplicate-module bug workaround | Tracked separately as `holomush-h8xj` |
| OTel tracing for backfill (`backfill.*` span with stream count, event count, failed streams, duration) | Follow-up bead — observability for the §2 SHOULD <1s goal |
| Concurrency cap on per-stream fan-out (e.g., `p-limit` 4-wide) | Defer until telemetry shows fan-out causes load issues |
| ARIA-live announcement for "Restoring scrollback…" loading state | A11y follow-up bead |
| Per-stream `count` tuning (busy channels may exceed 150) | Follow-up if telemetry shows truncation matters |
| B9 auth model review (no `player_session_token` requirement) | Independent concern — file separately if pursued; B13 follows B9's existing pattern for consistency |

## 11. Multi-viewer extensibility notes

The mechanism is portable across web panels but not across transports:

- **`CoreService.ListSessionStreams` and `CoreService.QueryStreamHistory`** are vanilla gRPC RPCs callable from any client (telnet, future native clients, test harnesses) — they return `corev1.Event` types that include the event ID natively.
- **`backfillStreams` helper** is **web-portable only**. It takes a `WebServiceClient` and returns `webv1.GameEvent[]`. Any future web panel (channel, scene, log viewer) imports it with its own stream set; non-web clients call core RPCs directly.
- **Per-viewer UI affordances** (loading indicator, error banner, empty state) are the viewer's responsibility — `replayActive` is the web-terminal's choice and won't apply to other panels.

Telnet's reconnect path doesn't need backfill in normal use because the local terminal buffer survives a reconnect. A future "telnet client process restart" recovery feature could call `CoreService.QueryStreamHistory` directly without any protocol changes.

## 12. Open questions

None remaining as of this draft. All design decisions resolved through the brainstorming session that produced this spec.
