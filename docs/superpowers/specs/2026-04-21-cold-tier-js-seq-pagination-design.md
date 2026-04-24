# History pagination on JetStream stream sequence with opaque cursors

**Bead:** holomush-suos
**Status:** design — pending implementation plan
**Author:** Sean Brandt (with AI assistance)
**Date:** 2026-04-21
**Supersedes:**

- `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` §1a (the "JetStream sequence MUST NOT cross any public API boundary" rule) and §5 (the "ULID is the cursor everywhere" rule). See §3 for the rationale (the opaque cursor's `epoch` field replaces the ULID's role as the rebuild-survival anchor).
- `docs/superpowers/specs/2026-04-17-b13-web-client-backfill-design.md` cursor handling (replaces ULID `before_id` with opaque `cursor`).
- `docs/superpowers/specs/2026-04-15-query-stream-history-rpc-design.md` cursor field semantics.

**Related (not superseded, downstream):**

- Follow-up beads: `holomush-6nds` (JS rebuild tool — will use the cursor's `epoch` field), `holomush-ecbg` (audit drift detector — will key on `js_seq` natively), `holomush-l4kx` (audit-backfill CLI), `holomush-u5bb` (Actor.LegacyID persistence), `holomush-l60y` (test consolidation — the new tests in §12 fold into this).

## 1. Problem statement

Pre-cutover, every event published to the system flowed through a single `EventWriter` that serialized appends, and `core.NewULID()` was monotonic within a millisecond. As a consequence, ULID lex order matched wall-clock publish order across every subject. The cold-tier history reader (PG `events_audit`) and the hot-tier history reader (JetStream EVENTS stream) both used `ev.ID` as the pagination cursor and ordering key, and that worked because ULID lex order WAS the canonical event order.

F8 of the JetStream EventBus cutover (PR #252, merged 2026-04-21) deleted the global `EventWriter`. JetStream now assigns per-stream sequence numbers as the canonical order; each publisher generates its own ULIDs independently. The cursor and ordering invariant the history pipeline relied on is gone.

**Symptoms.**

- Cold tier (`internal/eventbus/history/cold_postgres.go`) paginates with `WHERE id > $cursor` and orders by `id`. Under concurrent publishers, this can return events in a different order than JetStream delivered them, and forward pagination can drop or duplicate events because two events at the same millisecond have ULIDs whose lex order is decided by their random tail.
- Hot tier (`internal/eventbus/history/hot_jetstream.go`) does the same kind of ULID compare in `matchesQuery` (`ev.ID.Compare(q.After) <= 0`).
- The `crossoverStream` in `tier.go` dedups by ULID, advances `q.After` by ULID, AND re-sorts the unread tail by ULID compare in `appendOrdered` — three sites where the broken invariant lives.

The bead `holomush-suos` (P1) flags this with a CodeRabbit comment on PR #252:

> "internal/eventbus/history/cold_postgres.go paginates and orders by events_audit.id (ULID bytes). Post-F8 cutover removed the global writer, so id is no longer a safe proxy for JetStream publish order across concurrent subjects. Using id >/< for cursors and ORDER BY id can return a different order than the hot JetStream tier and produce unstable crossover pagination/duplication."

## 2. Goals & non-goals

**Goals.**

- Hot and cold tier history reads return events in JetStream stream-sequence order, regardless of ULID lex order.
- Cursors are unambiguous and stable within a single JetStream stream epoch; cursors created in one epoch are detectable as such after a rebuild via the cursor's `epoch` field.
- The crossover between hot and cold tiers preserves total order across the boundary.
- The public API surfaces enough information for clients to detect three distinct cursor failure modes: temporary lag (retryable), permanent staleness (drop cursor), and malformed tokens (programming error).
- The cursor format is opaque to clients and to plugin authors. Clients receive a token, pass it back, and never inspect its contents. The host can evolve the format without further wire breaks.
- A regression test reproduces the concurrent-publisher case the original bead describes.
- Subscribe deliveries carry a cursor token, enabling clean "subscribe live, on disconnect reconnect with backfill" patterns.

**Non-goals.**

- Backward compatibility for clients persisting cursors across this change. The system is pre-launch; web client backfill, e2e harnesses, and any ad-hoc tests are all in-tree and will be updated in lockstep.
- Solving JS rebuild semantics (`holomush-6nds`). This design provides the `epoch` field on the cursor so rebuild can bump it and detect stale cursors deterministically; the rebuild design itself is separate.
- Solving audit projection drift (`holomush-ecbg`). This design enables seq-based detection (the join key the detector wants); the detector is separately designed.
- Persisting `Actor.LegacyID` to `events_audit` (`holomush-u5bb`). Orthogonal.

## 3. Design overview

The fix is structural and has two halves:

**Half 1 — internal ordering switches to JetStream stream sequence (`js_seq`).** Both tiers read and order by `js_seq`. The crossover dedups by seq. ULIDs remain the event's identity (returned to clients, used for `ON CONFLICT (id) DO NOTHING` in the audit projection's INSERT, used by admin/debug lookups), but they stop being the cursor. Internally, the host's `HistoryQuery` carries `(AfterSeq, AfterID, BeforeSeq, BeforeID)` for pagination and validation.

**Half 2 — the public cursor becomes an opaque token.** The wire format on `QueryStreamHistory*Request`, `QueryStreamHistory*Response`, and `Subscribe` deliveries is `bytes cursor` (or its base64-string equivalent in the JSON-friendly proto where appropriate). The cursor's internal structure is host-defined and host-evolvable:

```text
Cursor {
  uint32 version    // bump on any incompatible format change
  uint64 epoch      // 0 today; bumped by holomush-6nds on JS rebuild
  string owner      // "" for host-owned subjects; "plugin:<name>" for plugin-owned
  oneof body {
    HostCursor host { uint64 seq; bytes id }   // (seq, id) tripwire pair
    bytes      plugin_inner                    // opaque bytes from the plugin's own QueryHistory
  }
}
```

Clients treat the token as a black box: receive it from the server, pass it back unmodified. They cannot inspect or compare cursors. The host can rev the encoding (add fields, change wire format, switch backends) without further public-API breaks as long as the `version` byte is the leading discriminator.

**Why this supersedes Phase B's "seq MUST NOT cross the boundary" rule.** Phase B §1a justified the rule on rebuild fragility: ULIDs survive rebuilds, sequences do not. The opaque token preserves that property by elevating it to the cursor format itself: the `epoch` field is the rebuild-survival anchor, and a cursor from a prior epoch is rejected with a coded error (`EVENTBUS_CURSOR_STALE`) without leaking the seq's instability to the client. The seq travels inside the opaque body, never as an addressable client-visible identifier.

This is a wire-format break, accepted as the right call given the pre-launch posture and the simultaneous opportunity to make the cursor format extensible for the rebuild and multi-cluster work already on the roadmap.

## 4. Public API changes

### 4.1 Go types (`internal/eventbus/bus.go`, `internal/eventbus/types.go`)

```go
// HistoryQuery is the host-internal query shape. Cursor opacity is handled
// at the gRPC boundary by the cursor codec (§4.5); inside the bus, the
// pagination invariants live in these typed fields.
type HistoryQuery struct {
    Subject Subject

    // AfterSeq / AfterID are the exclusive lower bound by JS stream seq
    // and the cursor's tripwire ULID. Zero seq = from the start. The id
    // is required when seq > 0 for client-supplied cursors; internal
    // forward-tailing callers MAY leave id zero (validated at boundary).
    AfterSeq uint64
    AfterID  ulid.ULID

    // BeforeSeq / BeforeID mirror the After pair for backward reads.
    BeforeSeq uint64
    BeforeID  ulid.ULID

    NotBefore time.Time
    NotAfter  time.Time
    Direction Direction
    PageSize  int
}

// Event is the host-side representation of a published event. Seq is
// populated by both tiers; the cursor codec uses it when minting tokens
// for clients but the seq is not exposed on any public event envelope.
type Event struct {
    ID        ulid.ULID
    Seq       uint64    // NEW — JS stream sequence; populated by both tiers, host-internal
    Subject   Subject
    Type      Type
    Timestamp time.Time
    Actor     Actor
    Payload   []byte
}
```

`Event.Seq` is host-internal: it is used by the cursor codec when minting outbound tokens and by the dedup logic in the crossover stream. It does NOT appear on any public proto envelope.

### 4.2 Proto wire (`api/proto/holomush/{core,web,plugin}/v1/`)

**`corev1.QueryStreamHistoryRequest`:**

```protobuf
message QueryStreamHistoryRequest {
  RequestMeta meta = 1;
  string session_id = 2;
  string stream = 3;
  int32 count = 4;
  int64 not_before_ms = 5;

  // cursor is an opaque continuation token returned by a prior call to
  // QueryStreamHistory or by a Subscribe delivery. Empty = first page.
  // The format is host-defined; clients MUST NOT inspect or compare
  // cursors. On staleness or invalidity the server returns
  // EVENTBUS_CURSOR_STALE / EVENTBUS_CURSOR_LAG / EVENTBUS_CURSOR_INVALID
  // with documented recovery semantics; see §4.4.
  bytes cursor = 6;

  // Field 6 was previously string before_id (a ULID cursor). Reserved
  // to prevent silent reuse.
  reserved 7;  // historical placement of an unused index; kept clear
}

// Note: the prior `string before_id = 6;` field is being repurposed to
// `bytes cursor = 6;` because we have no live clients persisting cursors
// (pre-launch posture). If the cutover discovers a held-cursor case, the
// recovery is "send empty cursor" — the server returns the latest page.
```

**`corev1.QueryStreamHistoryResponse`:**

```protobuf
message QueryStreamHistoryResponse {
  ResponseMeta meta = 1;
  repeated EventFrame events = 2;
  bool has_more = 3;
  bytes next_cursor = 4;  // NEW — opaque continuation; pass back as request.cursor
}
```

**`corev1.EventFrame` and `webv1.GameEvent`** are unchanged structurally, with one addition:

```protobuf
message EventFrame {
  // ... existing fields 1-7 unchanged ...
  bytes cursor = 8;  // NEW — opaque cursor naming THIS event as the high watermark
}

message GameEvent {
  // ... existing fields 1-9 unchanged ...
  bytes cursor = 10;  // NEW — populated from corev1.EventFrame.cursor
}
```

The per-event cursor enables the "subscribe live + reconnect with backfill" pattern in §9 and §12.5.

**`webv1.WebQueryStreamHistoryRequest` / `Response`** mirror the core changes (`bytes cursor`, `bytes next_cursor`).

**`plugin.v1.PluginHostServiceQueryStreamHistoryRequest` / `Response`** mirror the core changes.

### 4.3 Cursor validation contract

When a client supplies a non-empty `cursor`:

1. Server decodes the cursor via the cursor codec (§4.5). On decode failure → `EVENTBUS_CURSOR_INVALID`.
2. If `cursor.epoch != currentEpoch` (currentEpoch is 0 today; will be bumped by `holomush-6nds`): → `EVENTBUS_CURSOR_STALE`.
3. If `cursor.owner == ""`: host-owned subject. Validate the body's `(seq, id)` pair against the appropriate tier (§6.2 cold, §7 hot). Mismatch → `EVENTBUS_CURSOR_STALE`. Seq missing from cold but `<= JS.Stream.Info().State.LastSeq` → `EVENTBUS_CURSOR_LAG`.
4. If `cursor.owner == "plugin:<name>"`: forward `body.plugin_inner` to that plugin's `PluginAuditService.QueryHistory` as its cursor. Plugin's response cursor (also opaque) is wrapped in a fresh host token before returning to the client.

When `cursor` is empty: read from the latest event backward (or from `not_before_ms` if set).

### 4.4 Error model

Three new error codes, each mapping to a distinct gRPC status with documented client recovery:

| Code | gRPC status | Cause | Client recovery |
|---|---|---|---|
| `EVENTBUS_CURSOR_INVALID` | `INVALID_ARGUMENT` | Cursor bytes failed to decode (corruption, version mismatch, malformed). | Drop cursor; treat as programming error / report. |
| `EVENTBUS_CURSOR_STALE` | `FAILED_PRECONDITION` | Cursor's epoch doesn't match current; or cursor's seq/id pair has no corresponding event in either tier (rebuild, drift, deletion). | Drop cursor; re-query without it. Optionally use `not_before_ms` to bracket the gap. |
| `EVENTBUS_CURSOR_LAG` | `UNAVAILABLE` | Cursor's seq is in the live JS stream but not yet projected into `events_audit`; the validation tier doesn't have it yet. | Retry with backoff (250ms / 500ms / 1s / 2s / 4s, total ~7.75s, then surface). Cursor remains valid; "surface" means "tell the user the cursor is unavailable" — not "drop it." |

The LAG/STALE distinction is critical for the "subscribe live, capture cursor, immediately reconnect with backfill" use case. Without LAG, every fast reconnect during the audit projection's lag window (typical sub-second; up to several seconds under load per §15) returns spurious STALE and forces the client to drop a cursor that's actually fine.

**Middleware interaction caveat.** ConnectRPC and gRPC retry middlewares often interpret `UNAVAILABLE` as a transport-layer hint (auto-retry transparently, or trip a circuit breaker). For QueryStreamHistory specifically, the web gateway and any ConnectRPC clients MUST be configured so that `UNAVAILABLE` from this RPC surfaces to the application layer rather than being swallowed by transparent retries (otherwise the LAG observability this design enables is hidden), and so that a brief lag burst does NOT trip a circuit breaker that takes the endpoint "down" for unrelated requests. The safest configuration is "no transport-layer retries on QueryStreamHistory; application owns the backoff." Document this in the gateway's RPC client setup at rollout time.

### 4.5 Cursor codec (`internal/eventbus/cursor/`)

A small new package owning the opaque token format:

```go
package cursor

// Cursor is the host-internal cursor representation. The wire format is
// the proto-marshaled bytes of this message.
type Cursor struct {
    Version uint32       // leading format discriminator; today only Version=1 is defined
    Epoch   uint64
    Owner   Owner        // typed discriminator, not a free-form string
    Host    *HostCursor  // populated when Owner.Kind == OwnerHost
    Plugin  []byte       // populated when Owner.Kind == OwnerPlugin
}

// Owner identifies who owns the subject the cursor names. The discriminator
// is a typed enum (not string parsing) so a plugin name containing ':' or
// other unusual characters cannot collide with the scheme.
type Owner struct {
    Kind       OwnerKind  // OwnerHost or OwnerPlugin
    PluginName string     // populated iff Kind == OwnerPlugin; canonical plugin name
                          // from the plugin manifest (see internal/plugin/manifest)
}

type HostCursor struct {
    Seq uint64
    ID  ulid.ULID
}

func Encode(c Cursor) ([]byte, error)
func Decode(b []byte) (Cursor, error)

// CurrentEpoch returns the host's current epoch. Today: always 0. The
// rebuild tool (holomush-6nds) will set this from a stored sentinel.
func CurrentEpoch() uint64
```

**Proto placement.** The proto definition lives in `internal/eventbus/cursor/cursor.proto` (NOT under `api/proto/`, which is by convention the public proto root). The `buf.yaml` module configuration excludes this path from external SDK generation. A `gorules` ruleguard rule rejects imports of this package from outside `internal/eventbus/`. This keeps the token opaque even to in-tree plugin authors who might be tempted to peek inside.

**Version sizing.** `Version uint32` is generous; `uint8` would fit (versions rev on the order of once per several years). Keeping `uint32` for proto-varint-uniformity with `Epoch` and to sidestep signed-vs-unsigned bugs at the Go ↔ proto boundary. Marshaled cost is still 1 byte for `Version=1`.

**Forward compatibility.** The leading `Version` field guarantees forward-compatible evolution: a future change can switch to a smaller wire format (e.g. CBOR, a packed binary), and decoders dispatch on `Version`. Today only `Version=1` is defined. The `Epoch` field is where rebuild invalidation lands; multi-cluster support (Phase B §10) can add a `cluster_id` field in a future `Version=2` without further public wire breaks.

**Observability.** Decoding errors surface at the gRPC boundary (handler §10 step 2). To keep opaque cursors debuggable for SREs, the handler and the cold-tier / hot-tier validators MUST attach the decoded cursor contents to the error context:

```go
return oops.Code("EVENTBUS_CURSOR_STALE").
    With("cursor_version", c.Version).
    With("cursor_epoch", c.Epoch).
    With("cursor_owner", c.Owner.Kind.String()).
    With("cursor_owner_plugin", c.Owner.PluginName).
    With("cursor_seq", c.Host.Seq).
    With("cursor_id", c.Host.ID.String()).
    ...
```

This keeps the token opaque to clients on the wire while giving server-side structured logs and incident investigators full visibility into what any cursor referred to. A follow-up `cmd/holomush cursor inspect <base64-token>` admin subcommand is tracked as an implementation plan item (not a design decision).

## 5. Schema & indexes

New migration `internal/store/migrations/000011_events_audit_js_seq_index.{up,down}.sql`:

```sql
-- 000011_events_audit_js_seq_index.up.sql
CREATE INDEX IF NOT EXISTS events_audit_subject_js_seq
  ON events_audit (subject, js_seq);

-- 000011_events_audit_js_seq_index.down.sql
DROP INDEX IF EXISTS events_audit_subject_js_seq;
```

(Migration `000010_drop_events_and_cursors` was added by Phase B F6.)

**No data backfill is required.** The `js_seq BIGINT NOT NULL` column has been present since migration 000009 (the events_audit baseline), populated at INSERT time by the audit projection from `meta.Sequence.Stream` (verified at `internal/eventbus/audit/projection.go:249`). Every existing row already has the correct seq.

**Existing indexes are preserved:**

- `events_audit_subject_id (subject, id)` — supports the projection's `ON CONFLICT (id) DO NOTHING` and admin/debug lookups by ULID.
- `events_audit_subject_ts (subject, timestamp)` — supports time-bounded queries (`NotBefore`/`NotAfter`) without a cursor.
- `events_audit_subject_pat (subject text_pattern_ops)` — supports subject-pattern queries.

## 6. Cold tier (`internal/eventbus/history/cold_postgres.go`)

### 6.1 Query shape

The SELECT list gains `js_seq` so the reader can populate `Event.Seq`:

```sql
SELECT id, subject, type, timestamp, actor_kind, actor_id, payload, js_seq
  FROM events_audit
 WHERE [subject = $1 OR subject LIKE $1]
       [AND js_seq >= $N]                         -- forward, AfterSeq > 0
       [AND js_seq <= $N]                         -- backward, BeforeSeq > 0
       [AND timestamp >= $N AND timestamp <= $N]  -- NotBefore / NotAfter
       [AND timestamp < $N]                       -- crossover edge
 ORDER BY js_seq ASC|DESC
 LIMIT $N
```

### 6.2 The piggyback validation pattern (load-bearing)

> **This pattern is load-bearing and MUST be documented inline with the same rationale.** A future maintainer who removes the `>=` (changes it back to `>`) will silently disable the tripwire and reintroduce the bug class this design exists to prevent.

When a cursor is supplied (`AfterSeq > 0` for forward, `BeforeSeq > 0` for backward), the SQL uses `>= cursor.Seq` (forward) or `<= cursor.Seq` (backward), NOT a strict inequality. The first row returned MUST be the cursor echo: `js_seq == cursor.Seq AND id == cursor.ID`. The reader:

1. Validates the first row's `(js_seq, id)` against the cursor.
2. If validation passes: discard that first row, return the rest as the page.
3. If the first row's seq is `> cursor.Seq` (i.e., the cursor seq doesn't exist in cold): inspect JS via `Stream.Info().State.LastSeq`:
   - `cursor.Seq <= LastSeq` AND `cursor.Seq` is in JS → return `EVENTBUS_CURSOR_LAG`. The seq is real but cold hasn't caught up yet.
   - Otherwise → return `EVENTBUS_CURSOR_STALE`.
4. If the first row's seq matches but `id` doesn't: `EVENTBUS_CURSOR_STALE`. (Audit drift or rebuild has reassigned the ULID at that seq.)

When no cursor is supplied: standard semantics, no first-row validation.

`LIMIT` is `pageSize + 1` when a cursor is present (to consume the echo in step 2), `pageSize` when not.

**Edge case — cursor at exactly the crossover edge.** When the crossover edge constraint is active (`AND timestamp < $edge`), the cursor row may have `timestamp >= edge` and be filtered out, causing spurious validation failure. To prevent this, the cursor row is OR'd into the edge clause. The OR predicate MUST guard on BOTH `js_seq` AND `id` — guarding on seq alone would let a drift-twin row (same seq, different id — exactly the scenario STALE detection exists for) bypass the edge filter:

```sql
WHERE subject = $1
  AND js_seq >= $cursorSeq
  AND (timestamp < $edge OR (js_seq = $cursorSeq AND id = $cursorID))
ORDER BY js_seq ASC
LIMIT $pageSize + 1
```

With the cursor-row pair guard, exactly the cursor-matching row passes the WHERE regardless of timestamp; any other row (including a drift twin sharing the seq) is subject to the edge filter. This keeps the validation path honest even in the presence of audit drift. (The `(subject, js_seq)` index is non-unique by design — `events_audit` does not enforce seq uniqueness at the DB level — so the id guard is load-bearing, not belt-and-braces.)

**Edge case — empty page after discard.** If the cold tier returns ONLY the cursor echo (one row) and nothing after it, the post-discard slice is empty. The cold reader returns `[]` directly. The crossover stream's existing `loadNextPage` logic handles this correctly: `len(first) == 0 < pageSize` triggers the crossover branch; `advanceCursor([])` is a no-op (`if len(events) == 0 { return }` per §8.2); the stuck-loop guard in `EVENTBUS_HISTORY_BUFFER_OVERFLOW` detection does NOT fire because its trigger condition is `len(first) > 0 && cursor didn't advance` (§8.3). No special signaling between cold and the crossover stream is required — the existing empty-read routing path is correct.

### 6.3 Subject pattern handling

`classifySubject` and the `subject LIKE` path are unchanged. Subject patterns continue to work because `js_seq` is monotonic across the entire EVENTS stream — ordering by `js_seq` across multiple subjects gives a well-defined total order without any cross-subject merge logic.

## 7. Hot tier (`internal/eventbus/history/hot_jetstream.go`)

### 7.1 Start policy table

| Direction | Cursor present? | Time bound? | Start policy |
|---|---|---|---|
| Forward | `AfterSeq > 0` | any | `DeliverByStartSequencePolicy(AfterSeq)` (delivers AT seq, inclusive) |
| Forward | no | `NotBefore` set | `DeliverByStartTimePolicy(NotBefore)` |
| Forward | no | none | `DeliverByStartTimePolicy(edge)` |
| Backward | `BeforeSeq > 0` | any | `DeliverByStartSequencePolicy(max(1, BeforeSeq − fetch))`, walk forward to `BeforeSeq`, reverse in-memory |
| Backward | no | any | current behavior — start at `max(1, LastSeq − fetch + 1)`, reverse in-memory |

For forward reads with a cursor, the first delivered message is the cursor echo (the seq is inclusive). The reader validates seq AND id, then discards. If validation fails:

- `OptStartSeq < FirstSeq` (retention age-out) → first delivered seq > cursor.Seq → check `Stream.Info()`: if cursor.Seq < FirstSeq, return `EVENTBUS_CURSOR_STALE` (retention ate it); otherwise return `EVENTBUS_CURSOR_INVALID` (impossible state).
- First message seq matches but ID doesn't → `EVENTBUS_CURSOR_STALE`.

### 7.2 `matchesQuery` changes

```go
// before
if !q.After.IsZero() && ev.ID.Compare(q.After) <= 0 { return false }
if !q.Before.IsZero() && ev.ID.Compare(q.Before) >= 0 { return false }

// after
if q.AfterSeq > 0 && ev.Seq <= q.AfterSeq { return false }
if q.BeforeSeq > 0 && ev.Seq >= q.BeforeSeq { return false }
```

### 7.3 `Event.Seq` population

```go
meta, err := msg.Metadata()
if err == nil {
    ev.Seq = meta.Sequence.Stream
}
```

### 7.4 `orderEvents` is unchanged

`orderEvents` (`hot_jetstream.go:242-252`) only reverses for `DirectionBackward`; it does not sort by ULID. (The earlier draft of this spec misattributed the ULID sort here — it's actually in the crossover's `appendOrdered`; see §8.4.) No change needed.

## 8. Crossover (`internal/eventbus/history/tier.go`)

### 8.1 Dedup key change

```go
// before
seenIDs map[ulid.ULID]struct{}

// after
seenSeqs map[uint64]struct{}
```

JS stream seq is unique within the stream, so it's a stable dedup key. Events appearing in both tiers during the safety-margin overlap window dedup correctly by seq.

### 8.2 Cursor advancement

```go
func (s *crossoverStream) advanceCursor(events []eventbus.Event) {
    if len(events) == 0 {
        return
    }
    last := events[len(events)-1]
    dir := s.query.Direction
    if dir == 0 {
        dir = eventbus.DirectionForward
    }
    if dir == eventbus.DirectionForward {
        s.query.AfterSeq = last.Seq
        s.query.AfterID = last.ID
    } else {
        s.query.BeforeSeq = last.Seq
        s.query.BeforeID = last.ID
    }
}
```

Seq AND id travel together — the tripwire stays correctly paired through pagination.

### 8.3 `currentCursor` for stuck-loop detection

The existing `EVENTBUS_HISTORY_BUFFER_OVERFLOW` regression test (`tier.go:472-478` + corresponding test) relies on `currentCursor()` returning a value that should change after a productive read. The new implementation returns `q.AfterSeq` (forward) or `q.BeforeSeq` (backward). Both should monotonically advance under correct tier behavior. The test fixture (`tier_test.go:662` and adjacent) updates in lockstep.

### 8.4 `appendOrdered` re-key

`tier.go:629-645` `appendOrdered` re-sorts the unread tail with `ev.ID.Compare`. **This is the ULID-sort site missed in the earlier draft of this spec.** It must be re-keyed to seq compare:

```go
// before
sort.SliceStable(tail, func(i, j int) bool {
    if dir == eventbus.DirectionBackward {
        return tail[i].ID.Compare(tail[j].ID) > 0
    }
    return tail[i].ID.Compare(tail[j].ID) < 0
})

// after
sort.SliceStable(tail, func(i, j int) bool {
    if dir == eventbus.DirectionBackward {
        return tail[i].Seq > tail[j].Seq
    }
    return tail[i].Seq < tail[j].Seq
})
```

### 8.5 `selectStartTier` edge handling

`selectStartTier` previously decoded the cursor's ULID timestamp via `ulid.Time(q.After.Time())` and compared to the edge. With a seq cursor, the timestamp isn't free.

Strategy: **trust the JS stream as the source of truth for "is this seq in hot?"** When a cursor is present:

1. Look up `Stream.Info().State.{FirstSeq, LastSeq}`.
2. If `cursor.Seq >= FirstSeq` → start in hot.
3. If `cursor.Seq < FirstSeq` → start in cold.

Avoids the cold-tier timestamp lookup proposed in the prior draft. Avoids the spurious LAG case at the `selectStartTier` step (lag detection happens later at validation time, with full context).

**Caching of `Stream.Info()`.** The same `State.{FirstSeq, LastSeq}` is needed by at least three distinct call sites in a single client request: `selectStartTier` (routing the first page), cold-tier validation step 3 (distinguishing LAG from STALE), and hot-tier retention-age-out detection (§7.1). Without caching, each failed validation incurs a fresh round-trip. The cache is owned by the `Reader` for the lifetime of a single `QueryHistory` call (NOT the lifetime of the `Reader` itself — stream state changes over time):

```go
// Reader.QueryHistory constructs this once per call and passes it into
// selectStartTier AND the crossoverStream (which forwards it to cold/hot
// tiers through the readTier path). The memoization is concurrency-safe
// for a single call; a sync.Once is unnecessary because loadNextPage is
// not called concurrently on a single crossoverStream.
type streamStateSnapshot struct {
    firstSeq uint64
    lastSeq  uint64
    fetched  bool     // false until first use; true after Stream.Info() returns
    fetchErr error    // non-nil if the initial fetch failed; surfaced to caller
}
```

On first use, the Reader calls `js.Stream(ctx).Info(ctx)`; subsequent accesses within the same `QueryHistory` call reuse the cached values. If the stream state changes between pages (e.g., retention ages out a seq that was still in hot on page 1), the next `QueryHistory` call gets a fresh snapshot; within a single call the snapshot is a consistent view.

For the rare case of an extremely long pagination chain where staleness matters: clients paginate one page at a time and construct a fresh `QueryHistory` call per page (that's the opaque-cursor contract). So the snapshot is naturally scoped correctly without explicit invalidation.

## 9. Subscribe path

### 9.1 Subscriber delivery construction

`internal/eventbus/subscriber.go` `Next` → `decodeDelivery`: today (`subscriber.go:255-269`) the function discards `msg.Metadata()`. The signature must change to surface `Seq`:

```go
// before
func decodeDelivery(msg jetstream.Msg, ...) (Event, error)

// after — option A: return seq alongside Event
func decodeDelivery(msg jetstream.Msg, ...) (Event, uint64, error)

// after — option B: populate Event.Seq inside decodeDelivery
func decodeDelivery(msg jetstream.Msg, ...) (Event, error) {
    // ... existing decode ...
    if meta, err := msg.Metadata(); err == nil {
        ev.Seq = meta.Sequence.Stream
    }
    return ev, nil
}
```

Option B is preferred — keeps the function signature stable and aligns with hot tier's pattern (§7.3). The `DeliveryMetadataForTest` test seam continues to work for assertion purposes.

### 9.2 Per-event cursor on the wire

`EventFrame.cursor` (and its web equivalent `GameEvent.cursor`) is populated by the gRPC handler from the `Event.Seq` and `Event.ID`. Each delivered event ships with a cursor naming itself as the high-watermark — clients persist the most recent cursor and feed it back on reconnect.

### 9.3 The reconnect-with-backfill use case

The motivating use case (and the strongest justification for breaking Phase B §1a):

1. Client subscribes via gRPC. Each delivered `EventFrame` carries a `cursor` field — an opaque token naming that event as the high-watermark of what the client has seen.
2. Client persists the most recent cursor on every delivery (or every Nth delivery; client's choice).
3. Client disconnects (network, app close, etc.).
4. Client reconnects, calls `QueryStreamHistory(stream=..., cursor=lastPersistedCursor)`. Server returns the page of events strictly older or newer than the cursor (the wire endpoint is "older than" today; symmetric "newer than" RPCs may follow).
5. Client resumes Subscribe; the live tail picks up where the backfill ends.

Without `cursor` on `EventFrame`, the client has nothing to feed back — it would have to use an ID it found from the live stream and accept either ULID-based (broken post-cutover) or time-based (lossy at the boundary) backfill.

## 10. gRPC handler (`internal/grpc/query_stream_history.go`, web RPC handler)

The handler:

1. Reads `cursor bytes` from the request.
2. If non-empty, decodes it via the cursor codec (§4.5). On decode failure → `INVALID_ARGUMENT` with `EVENTBUS_CURSOR_INVALID`.
3. **Preserves existing subject translation:** the current handler uses `subjectxlate.Legacy(...)` to map legacy stream names like `location:01ABC` to modern `events.<game_id>.location.01ABC`. This translation MUST stay; the only field changing is the cursor.
4. If `cursor.Owner == ""`: constructs `HistoryQuery{ BeforeSeq: cursor.Host.Seq, BeforeID: cursor.Host.ID, NotBefore: ..., Direction: Backward }`. Calls `historyReader.QueryHistory(ctx, q)`.
5. If `cursor.Owner != ""` (plugin-owned): forwards to `pluginHostService.QueryHistory(ctx, owner, cursor.Plugin)`. Plugin's response cursor is wrapped in a fresh host token (§11).
6. On `EVENTBUS_CURSOR_STALE` from the reader, maps to gRPC `FAILED_PRECONDITION`. On `EVENTBUS_CURSOR_LAG` → `UNAVAILABLE`. On `EVENTBUS_CURSOR_INVALID` → `INVALID_ARGUMENT`.
7. On the response, populates `EventFrame.cursor` from `Event.Seq` + `Event.ID` for every emitted frame, AND sets `QueryStreamHistoryResponse.next_cursor` to the cursor of the last frame in the page (or empty if no frames).
8. The `has_more` boolean is set from whether the reader returned a full page.

The web RPC handler is a thin proxy and undergoes the parallel changes (gateway boundary preserved per CLAUDE.md "Architecture Invariants").

The helper functions `fetchHistoryFramesFromBus` and `eventbusEventToEventFrame` (`query_stream_history.go:201-261`) thread `Event.Seq` and `Event.ID` into the cursor codec at frame construction time.

## 11. Plugin host RPC (`internal/plugin/host/`, `pkg/plugin/`, plugin proto)

### 11.1 Wire change

`PluginHostServiceQueryStreamHistoryRequest` and `Response` adopt the `cursor bytes` / `next_cursor bytes` change in lockstep with core. **Plugin audit RPC (`api/proto/holomush/plugin/v1/audit.proto`) is unchanged** — plugins continue to use whatever cursor format their schema dictates (today: ULID `bytes after = 2; bytes before = 3;`). The host wraps the plugin's opaque inner cursor inside its own token at the boundary.

### 11.2 Lua hostfunc parity

Project memory rule (`feedback_host_rpc_lua_parity`): every PluginHostService RPC MUST ship Go SDK and Lua hostfunc together. Today's Lua signature is positional `holomush.query_stream_history(stream, count)`. The cursor addition uses a table-arg form to avoid awkward positional growth:

```lua
-- new signature
local result = holomush.query_stream_history({
    stream = "scene:abc:ic",
    count = 10,
    cursor = previous_cursor,  -- string (base64-encoded opaque bytes); nil for first page
    not_before_ms = 0,
})
-- result.events: array of event tables; each has a `cursor` field
-- result.next_cursor: string for next page; nil if no more
-- result.has_more: bool
```

### 11.3 Go SDK

`pkg/plugin/focus_client.go:62-65` `QueryStreamHistoryRequest` struct gains `Cursor []byte`; the response gains `NextCursor []byte` and per-event cursor in the `Event` struct. The host-side Go-plugin handler at `internal/plugin/goplugin/host_service.go` updates in lockstep. The plugin's existing `QueryHistory` continues to use its own ULID-based cursor — host wraps inner bytes.

## 12. Testing

### 12.1 Unit

- **Cursor codec:** round-trip encode/decode for host-owned and plugin-owned variants; version mismatch returns `EVENTBUS_CURSOR_INVALID`; unknown owner returns invalid.
- **Cold tier:**
  - Forward read with valid cursor returns rows after the cursor; first row validated and discarded.
  - Forward read with id mismatch returns `EVENTBUS_CURSOR_STALE`.
  - Forward read where cursor.Seq is missing from cold but present in JS returns `EVENTBUS_CURSOR_LAG`.
  - Forward read where cursor.Seq is missing from both returns `EVENTBUS_CURSOR_STALE`.
  - Cursor exactly at the crossover edge: cursor row passes the OR'd WHERE clause; subsequent rows subject to edge filter.
  - Empty page after discard signals "drained" rather than empty buffer.
  - Backward read mirrors the above with `BeforeSeq` / `BeforeID`.
- **Hot tier:** the same staleness/lag/edge cases, exercised against an embedded NATS server or a hot-tier fake.
- **Crossover:**
  - `seenSeqs` deduplicates events appearing in both tiers during the safety-margin overlap.
  - `advanceCursor` updates seq AND id together; regression test asserts both move on every productive read.
  - `appendOrdered` orders by seq, not ULID — synthesize a tail with deliberately out-of-order ULIDs but in-order seqs and assert post-sort matches seq order.
  - `EVENTBUS_HISTORY_BUFFER_OVERFLOW` regression test continues to fire on a tier returning events without advancing seq (test fixture updated for new cursor type).

### 12.2 Property (rapid)

- Concurrent-publisher event streams with synthetic ULIDs whose lex order deliberately disagrees with seq order. Assert end-to-end pagination returns events in seq order, no drops, no dupes apart from legitimate cross-tier overlap dedup.
- Random cursors mid-stream; assert resume returns the correct continuation.
- Random ULID generation seeds — fuzz the validation pattern's id-mismatch detection.

### 12.3 Integration (`task test:int`)

Mandatory by the bead.

- Dockerized NATS + PG via testcontainers.
- Two publishers writing to the same subject at high concurrency with synthesized non-monotonic ULIDs.
- Page through history straddling the retention edge with opaque cursors.
- Assert: page concatenation matches JS stream-sequence order; no events dropped or duplicated.
- These tests fold into the `holomush-l60y` test consolidation effort (the prior `query_stream_history_test.go` and `test/integration/eventbus_e2e/cross_tier_query_test.go` and `…/reconnect_resume_test.go` should be unified, not parallel-extended).

### 12.4 LAG-vs-STALE specific

- Mock the audit projection to lag (delay INSERTs); fire QueryStreamHistory with a cursor whose seq is in JS but not in cold. Assert `EVENTBUS_CURSOR_LAG` (`UNAVAILABLE`).
- Mock cold to return a row at the cursor seq with a different ULID. Assert `EVENTBUS_CURSOR_STALE` (`FAILED_PRECONDITION`).
- Mock the codec to surface a malformed token. Assert `EVENTBUS_CURSOR_INVALID` (`INVALID_ARGUMENT`).
- Client retry semantics: a stub client retries on UNAVAILABLE with backoff and succeeds once the projection catches up.

### 12.5 Subscribe-then-backfill

- Subscribe via gRPC; capture deliveries with their `cursor` fields populated. Record the cursor of the OLDEST received event.
- Disconnect, reconnect via QueryStreamHistory with `cursor = oldestCursor`. Server validates (cursor was just emitted live, so seq+id are fresh) and returns the page of events strictly older.
- Assert backfill page concatenated with live tail forms a contiguous, in-order slice.
- LAG path: simulate an artificial projection delay and assert the client's backoff + retry produces eventual success without dropping the cursor.

## 13. Migration & rollout

The deletion of the `events` table and the `EventWriter` happened in F6/F7 of Phase B. There is no two-system overlap to manage. The rollout:

1. **Migration 000011** — the new `(subject, js_seq)` index. Idempotent on existing rows.
2. **Cursor codec package** — `internal/eventbus/cursor/` plus `api/proto/holomush/eventbus/v1/cursor.proto`.
3. **Internal eventbus changes** — `HistoryQuery` shape; `Event.Seq`; cold/hot tier code; crossover (`tier.go` including `appendOrdered`); subscriber `decodeDelivery` Seq population.
4. **Proto wire changes** — `corev1`, `webv1`, `pluginv1` updates per §4.2 and §11.
5. **Handler changes** — gRPC core handler, web handler proxy, plugin host service. Preserve `subjectxlate.Legacy` translation.
6. **Lua hostfunc + Go SDK** — table-arg signature (§11.2), SDK struct fields (§11.3). Verified at spec-write time: no in-tree `.lua` plugin calls `holomush.query_stream_history` (the positional signature was added by Phase B and has no active callers). The table-arg change is safe to introduce as the first caller shape.
7. **Web client backfill** — `web/src/lib/backfill/streamBackfill.ts` (and its test) update to use opaque cursor; `web/src/routes/(authed)/terminal/+page.svelte` audits stale-cursor handling for the FAILED_PRECONDITION recovery path.
8. **Ruleguard removal** — `gorules/rules.go` `EventIDMustBeMonotonic` is now a dead invariant (PostgresEventStore is gone, cursor CAS is gone, the new design's premise is "ULID lex order is NOT publish order"). Delete the rule and its test, update the rule pack docs.
9. **Test updates** — unit tests (per §12.1), integration tests folded into `holomush-l60y` consolidation (per §12.3), property tests added (§12.2).
10. **Verify `task pr-prep` is fully green** — no exceptions per project rule.

There is no dual-write phase, no feature flag, and no compatibility shim — matches the Phase B cutover style.

## 14. Out of scope / follow-ups

- **`holomush-6nds` (JS rebuild tool).** Will use the cursor's `epoch` field: rebuild bumps `currentEpoch`, all prior cursors return STALE deterministically. The rebuild design itself owns the epoch persistence and bumping mechanism; this design just provides the cursor field.
- **`holomush-ecbg` (audit drift detector).** Will use `js_seq` as the primary join key between JS and PG, exactly what the new index supports. Drift surfaces to clients as `EVENTBUS_CURSOR_STALE` symptoms; root-causing is the detector's job.
- **`holomush-l4kx` (audit-backfill CLI).** Replays JS messages through the audit projection. Backfilled rows get `js_seq` from `meta.Sequence.Stream` (current mechanism). If a JS rebuild has reassigned seqs, backfilled cursors against the old seqs return STALE — operationally consistent.
- **`holomush-u5bb` (Actor.LegacyID persistence).** Untouched.
- **Forward-direction public RPC.** If a future RPC needs forward-paginated history, the symmetric `(after_seq, after_id)` pair on the internal `HistoryQuery` already supports it; the cursor codec wraps either direction the same way.
- **Multi-cluster JS migration** (Phase B §10). The opaque cursor's `version` field allows the format to gain a `cluster_id` field without further wire breaks.

## 15. Open risks

- **Sequence overflow.** `js_seq` is `BIGINT` (`int64` Postgres), wire/codec field is `uint64`. JetStream uses `uint64`. At HoloMUSH volumes overflow is decades away, but the `int64`/`uint64` boundary should be checked when the column is read into Go: store as `int64`, cast to `uint64` for transport.
- **`OrderedConsumer` with `DeliverByStartSequencePolicy` after retention age-out.** Documented JetStream behavior is to start at `FirstSeq` if `OptStartSeq < FirstSeq`. The validation pattern detects this (first delivered seq > cursor.Seq) and returns STALE — the operationally correct outcome. The test plan exercises this (§12.4).
- **Audit projection lag window.** Sub-second typical, several seconds under load. The LAG/STALE split prevents spurious cursor invalidation during this window; clients must implement retry-with-backoff on `UNAVAILABLE`. The integration test (§12.5) exercises this. Operational alert threshold for projection lag should be set well below the `Stream.Info()` lookup overhead — if the audit projection ever lags by more than several seconds consistently, that's an incident, not a normal mode.
- **`Stream.Info()` cost on validation failure.** Adding a `Stream.Info()` call to distinguish LAG from STALE is one round-trip per failed validation. Cached for the request lifetime to amortize across multi-page reads. Failed validations are expected to be rare; this cost is acceptable.
- **Subject patterns over `(subject, js_seq)` index.** Exact-subject queries use the new index directly. `subject LIKE 'events.foo.%'` queries fall back to the `text_pattern_ops` index for the subject filter and then sort by `js_seq` separately. For pattern queries the planner may pick a different plan than for exact subjects. Acceptable for the first cut; revisit if pattern-query performance surfaces in load tests.
- **Cursor token size — single-request.** Proto-marshaled `Cursor` with `Version (varint, 1B) + Epoch (varint, 1-9B) + OwnerKind (varint, 1B) + PluginName (string, 0-32B) + body (HostCursor: ~25B, or plugin_inner: variable)`. Typical host cursor: ~30 bytes raw, ~40 bytes base64. Acceptable for headers and JSON contexts.
- **Cursor token size — high-fanout Subscribe path.** Adding a cursor to EVERY `EventFrame` delivery has a non-trivial aggregate cost on busy subjects. For a location with N subscribers receiving M events per minute, the wire overhead is N × M × ~30 bytes. For plausibly busy rooms (N=100 subscribers, M=60 events/min) that's ~180 KB/min = ~3 KB/s per room, per subject. The WebSocket layer already compresses frames (per gateway config), which will shrink base64-like content effectively, but the raw proto bytes still traverse the core → gateway gRPC hop uncompressed. Acceptable for first cut; if observed in load tests as a meaningful fraction of gateway egress, the mitigation is to emit `cursor` only on every Nth delivery (configurable; e.g. every 10th event plus terminal frames) — the last-received cursor still names an event the client has seen, and backfill-on-reconnect works with a cursor at most N events behind the live tail. Design this mitigation as a config option on the subscriber, not as a breaking change, so it can be enabled without further spec revision.
- **Cursor observability in ops.** Opaque cursors defeat naïve log inspection: a structured log line reading `cursor=AGRkAQABCAYBAA…` is unusable for an on-call investigator. §4.5 mandates that every cursor-failure path (INVALID / STALE / LAG) attach the decoded contents to the `oops.With(...)` error context. The server-side structured log therefore contains `cursor_version, cursor_epoch, cursor_owner, cursor_seq, cursor_id` for every error — same information a future `cmd/holomush cursor inspect` admin CLI would print, but accessible via existing log tooling without needing the admin binary.
