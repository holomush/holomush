<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# B9: Client-Facing QueryStreamHistory RPC Design

**Status:** Draft
**Date:** 2026-04-15
**Author:** seanb4t (via Claude Opus 4.6, brainstorm)
**Epic:** `holomush-oy6e` — Server-Owned Focus Substrate
**Bead:** `holomush-oy6e.9`
**Parent spec:** `docs/superpowers/specs/2026-04-11-focus-substrate-design.md` (§3.4, §4.7)
**Depends on:** B2 (EventStore.ReplayTail), B8 (plugin host QueryStreamHistory)

## RFC2119 Keywords

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** in
this document are to be interpreted as described in RFC 2119.

---

## 1. Overview

Add a `QueryStreamHistory` unary RPC to `CoreService` that lets authenticated
sessions read paginated event history from streams they are authorized to
access. This is the client-facing counterpart to B8's trusted plugin host
variant — same `ReplayTail` primitive underneath, but gated by a two-layer
authorization model: membership-enforced invariant for private streams, ABAC
policy evaluation for public streams.

### 1.1 What this delivers

1. Proto addition: `QueryStreamHistory` RPC on `CoreService` with cursor-based
   pagination
2. Go handler in `internal/grpc/` with ABAC-gated stream access
3. Seed policy: `seed:player-location-stream-read` for co-located stream reads
4. `ReplayTail` extension: `beforeID` parameter for cursor-based pagination
5. Web gateway proxy: `WebQueryStreamHistory` on `WebService`

### 1.2 What this does NOT deliver

- Web client integration (B13 handles calling this on mount/reload)
- New ABAC attribute providers (the existing `StreamProvider` and
  `CharacterProvider` are sufficient)
- Cursor mutation — this is a pure read (invariant I-13)
- Rate limiting — deferred; the parent spec mentions it but no rate-limiting
  infrastructure exists yet. When added, it SHOULD apply to this RPC.

---

## 2. Design Decisions

### D1: Unary response, not streaming

`QueryStreamHistory` is a bounded read. The client drives pagination by
calling repeatedly with a `before_id` cursor. Each call returns at most one
page of events. Streaming RPCs are for unbounded ongoing delivery (`Subscribe`);
history scrollback is request/response.

### D2: ULID cursor pagination, not timestamp-only

Multiple events can share the same millisecond timestamp. Paginating by
`not_before_ms` alone risks skipping or duplicating events at page boundaries.
The pagination cursor MUST be a ULID (`before_id`), which is unique and ordered.
`not_before_ms` remains as an orthogonal time-floor filter ("don't go further
back than this timestamp").

### D3: Page size — default 150, max 500

Default page size is 150 (roughly one screenful of terminal/chat output). The
client MAY request up to 500 per page. No total cap on how far back a client
can paginate — the per-page limit bounds server memory per request.

### D4: Two-layer authorization — membership gate + ABAC policy

Stream read authorization uses a two-layer model:

1. **Private streams (scene, character):** A hard membership gate enforces
   invariant I-17 — only members can read private streams, with no policy
   override. This check runs *before* the ABAC engine and short-circuits
   on deny. Membership is checked against session state (FocusMemberships
   for scenes, CharacterID for personal streams).

2. **Public streams (location):** The ABAC engine (`AccessPolicyEngine.Evaluate`)
   evaluates policy-based authorization (co-location, admin override). The engine
   already has a `stream` resource type with `StreamProvider` resolving
   `stream.name` and `stream.location`.

The membership gate is outside ABAC by design. I-17 is an invariant, not a
policy. Policies can be added, modified, or overridden by future seed updates
or admin configuration. Invariants cannot. Routing private stream access through
the ABAC engine would make it possible for a future policy to accidentally
grant access to non-members.

### D5: Session validation follows existing pattern

The handler validates the session via `sessionStore.Get()` (exists, not expired),
consistent with all other CoreService RPCs (`HandleCommand`, `Subscribe`, etc.).
No additional player ownership check in CoreService — the web gateway has
already authenticated the player and resolved the correct session ID.

### D6: `has_more` via count+1 fetch

The response includes `has_more` so clients know whether to show a "load more"
affordance. The server requests `count + 1` rows from the database, returns at
most `count`, and sets `has_more = len(fetched) > count`.

---

## 3. Proto Definitions

### 3.1 CoreService addition (`api/proto/holomush/core/v1/core.proto`)

```protobuf
service CoreService {
  // ... existing RPCs ...

  // QueryStreamHistory reads paginated event history from a stream.
  // ABAC-gated: the calling session's character must be authorized to read the stream.
  // Pure read — does not mutate session cursors (invariant I-13).
  rpc QueryStreamHistory(QueryStreamHistoryRequest) returns (QueryStreamHistoryResponse);
}

message QueryStreamHistoryRequest {
  RequestMeta meta = 1;     // request correlation
  string session_id = 2;    // session making the request
  string stream = 3;        // stream name (e.g., "scene:01ABC:ic", "location:01XYZ")
  int32 count = 4;          // page size; 0 = default (150), max 500, negative rejected
  int64 not_before_ms = 5;  // epoch ms time floor; 0 = no lower bound
  string before_id = 6;     // ULID pagination cursor; events older than this; empty = latest
}

message QueryStreamHistoryResponse {
  ResponseMeta meta = 1;      // echoed request correlation
  repeated Event events = 2;  // ascending ID order within the page
  bool has_more = 3;          // true if more history exists beyond this page
}
```

The `Event` message already exists in `core.proto` — reused as-is.

### 3.2 WebService addition (`api/proto/holomush/web/v1/web.proto`)

```protobuf
service WebService {
  // ... existing RPCs ...

  // WebQueryStreamHistory reads paginated event history for the web client.
  rpc WebQueryStreamHistory(WebQueryStreamHistoryRequest)
      returns (WebQueryStreamHistoryResponse);
}

message WebQueryStreamHistoryRequest {
  string session_id = 1;
  string stream = 2;
  int32 count = 3;
  int64 not_before_ms = 4;
  string before_id = 5;
}

message WebQueryStreamHistoryResponse {
  repeated GameEvent events = 1;  // reuses existing GameEvent message
  bool has_more = 2;
}
```

---

## 4. Handler Flow

### 4.1 CoreServer handler (`internal/grpc/`)

```text
QueryStreamHistory(ctx, req):
  0. Guard                   → reject if eventStore is nil (INTERNAL)
  1. Validate session_id    → sessionStore.Get(); reject NOT_FOUND / expired
  2. Validate stream        → reject empty
  3. Normalize count        → 0 → 150; negative → INVALID_ARGUMENT; > 500 → 500
  4. Parse before_id        → if non-empty, parse as ULID; reject malformed
  5. Authorization:
     a. If private stream   → membership gate (I-17):
        - character:<id>    → info.CharacterID must match
        - scene:<id>:*      → stream must be in FocusMemberships-derived set
        - Deny immediately if no membership (STREAM_ACCESS_DENIED)
     b. If public stream    → ABAC engine.Evaluate(ctx, AccessRequest{
                                Subject:  "character:" + info.CharacterID,
                                Action:   "read",
                                Resource: "stream:" + req.Stream,
                              })
                              reject if denied (STREAM_ACCESS_DENIED)
  6. Parse not_before        → time.UnixMilli if > 0, else zero
  7. Fetch                   → eventStore.ReplayTail(ctx, stream, count+1, notBefore, beforeID)
  8. Build response          → events[:min(len, count)], has_more = len > count
  9. Return                  → no cursor mutation (I-13)
```

### 4.2 Web gateway handler (`internal/web/`)

Thin proxy following the `SendCommand` pattern:

1. Extract fields from `WebQueryStreamHistoryRequest`
2. Call `CoreClient.QueryStreamHistory` with mapped `corev1.QueryStreamHistoryRequest`
3. Convert response events from `corev1.Event` to `webv1.GameEvent`
4. Return `WebQueryStreamHistoryResponse`

---

## 5. ABAC Policy

### 5.1 Stream privacy model and invariant

**Invariant I-17: Private streams are readable only by members.** No role,
privilege, or policy override grants read access to a private stream without
active membership. This is not a default-deny that admins can override — it
is a hard invariant. Admins who want to observe a private scene MUST join it.

Streams have implicit privacy based on their type:

| Stream pattern | Privacy | Read authorization |
| --- | --- | --- |
| `location:<id>` | **Public** | Co-located character (policy) or admin (policy) |
| `character:<id>` | **Private** | Only the owning character (I-17) |
| `scene:<id>:ic`, `scene:<id>:ooc` | **Private** | Only active scene members (I-17) |
| Future: `channel:<name>` | **Per-channel** | Depends on channel config |

### 5.2 Authorization: two-layer check

The handler uses a two-layer authorization model:

1. **Layer 1 — Membership gate (invariant I-17):** For private streams, the
   handler checks membership *before* consulting the ABAC engine. If the stream
   is private and the session has no membership, access is denied immediately.
   The ABAC engine is never consulted — no policy can override this.

2. **Layer 2 — ABAC policy (public streams):** For public streams, the handler
   calls `engine.Evaluate()` to check policy-based authorization (co-location,
   admin access, etc.).

```go
// Pseudocode
if isPrivateStream(stream) {
    if !sessionHasMembership(info, stream) {
        return STREAM_ACCESS_DENIED  // I-17: hard deny, no policy override
    }
    // Member — permitted
} else {
    decision := engine.Evaluate(ctx, accessRequest)
    if decision.Denied() {
        return STREAM_ACCESS_DENIED
    }
}
```

**Why membership check is outside ABAC:** I-17 is an invariant, not a policy.
Policies can be added, modified, or overridden. Invariants cannot. Routing
private stream access through the ABAC engine would make it possible for a
future policy to accidentally grant access. The membership check is a hard
gate that sits before the policy engine.

### 5.3 Membership resolution

| Stream type | Membership check |
| --- | --- |
| `character:<id>` | `info.CharacterID.String() == <id>` |
| `scene:<id>:*` | Stream derivable from `info.FocusMemberships` via `FocusKindPolicy.StreamsFor()` |

**Scene stream-to-FocusKey resolution:** The handler MUST parse the stream
name to extract the scene target ID. For `scene:<ulid>:ic` or
`scene:<ulid>:ooc`, the handler extracts `<ulid>`, parses it as a ULID
(rejecting malformed with `INVALID_ARGUMENT`), constructs
`FocusKey{Kind: FocusKindScene, TargetID: <ulid>}`, finds the matching
`FocusMembership`, then calls `StreamsFor()` to verify the requested stream
is in the derived set. This parsing logic SHOULD be extracted into a helper
(e.g., `streamToFocusKey(stream string) (*session.FocusKey, error)`) for
testability.

**TOCTOU note:** A character could leave a scene between the membership check
(step 5) and the `ReplayTail` fetch (step 7). This race is accepted as benign:
events are immutable historical data, the time window is microseconds (no I/O
between check and fetch except the store read itself), and the returned data
is limited to events the character was entitled to see moments earlier. This
is standard "check-then-act is safe for read-only operations on immutable
data" reasoning.

### 5.4 Seed policy: `seed:player-location-stream-read`

```text
permit(principal is character, action in ["read"], resource is stream)
  when { resource.stream.name like "location:*"
      && resource.stream.location == principal.character.location };
```

Characters can read history of their current location's stream. Mirrors
`seed:player-stream-emit`.

### 5.5 Admin access to public streams

No additional seed policy is needed. The existing `seed:admin-full-access`
policy (`permit(principal is character, action, resource) when { "admin" in
principal.character.roles }`) already covers `read` on `stream:location:*`.
This is safe because private streams are gated by the membership check
(Layer 1, I-17) *before* the ABAC engine runs — `seed:admin-full-access`
never gets a chance to permit private stream reads.

### 5.6 Future: session attribute provider

When a `SessionAttributeProvider` is introduced, the membership check can
optionally be expressed as a policy condition for audit/logging purposes, but
the hard membership gate in the handler MUST remain. I-17 is enforced in code,
not in policy.

---

## 6. ReplayTail Extension

### 6.1 Interface change

```go
// Before (current):
ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time) ([]Event, error)

// After:
ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]Event, error)
```

When `beforeID` is zero ULID, behavior is unchanged (no upper bound). When
non-zero, events with `id >= beforeID` are excluded.

**Note:** The default page size of 150 is applied in the handler, not in
`ReplayTail`. The store method treats `count` literally — callers are
responsible for normalization.

### 6.2 SQL change (PostgresEventStore)

```sql
-- Current:
SELECT ... FROM events WHERE stream = $1 AND timestamp >= $2
ORDER BY id DESC LIMIT $3

-- Extended:
SELECT ... FROM events WHERE stream = $1
  AND ($2::timestamptz IS NULL OR timestamp >= $2)
  AND ($3::text IS NULL OR id < $3)
ORDER BY id DESC LIMIT $4
```

Results are reversed to ascending order in Go before returning.

### 6.3 MemoryEventStore

The in-memory implementation filters `e.ID < beforeID` in its loop. Used only
in unit tests.

### 6.4 Impact on existing callers

All existing `ReplayTail` callers pass `ulid.ULID{}` (zero) for `beforeID`,
preserving current behavior. Callers:

- `pluginHostServiceServer.QueryStreamHistory` (B8) — passes zero
- `FocusKindPolicy` implementations — pass zero
- New: `CoreServer.QueryStreamHistory` (B9) — passes parsed `before_id`

### 6.5 Mock regeneration

The `ReplayTail` signature change requires `mockery` regeneration for
`MockEventStore`.

---

## 7. Error Codes

| Condition | gRPC code | Error code | Message |
| --- | --- | --- | --- |
| Empty session_id | INVALID_ARGUMENT | `INVALID_ARGUMENT` | session_id is required |
| Empty stream | INVALID_ARGUMENT | `INVALID_ARGUMENT` | stream is required |
| Negative count | INVALID_ARGUMENT | `INVALID_ARGUMENT` | count must be non-negative |
| Malformed before_id | INVALID_ARGUMENT | `INVALID_ARGUMENT` | before_id must be a valid ULID |
| Session not found | NOT_FOUND | `SESSION_NOT_FOUND` | session not found |
| Session expired | PERMISSION_DENIED | `SESSION_EXPIRED` | session expired |
| ABAC denied | PERMISSION_DENIED | `STREAM_ACCESS_DENIED` | not authorized to read stream |
| EventStore error | INTERNAL | (wrapped) | internal error |

---

## 8. Testing Strategy

Five test layers: unit, invariant, boundary, integration (DB), and E2E.

### 8.1 Unit tests — handler (`internal/grpc/query_stream_history_test.go`)

Mocked `sessionStore`, `eventStore`, and ABAC engine.

| Test | Description |
| --- | --- |
| ReturnsEventsForAuthorizedLocationStream | Happy path: co-located, events returned |
| ReturnsEventsForFocusDerivedSceneStream | Session has focus membership → allowed |
| ReturnsEventsForOwnCharacterStream | Character personal stream → allowed |
| RejectsEmptySessionID | INVALID\_ARGUMENT |
| RejectsEmptyStream | INVALID\_ARGUMENT |
| RejectsNegativeCount | INVALID\_ARGUMENT |
| RejectsMalformedBeforeID | INVALID\_ARGUMENT |
| DefaultsCountTo150WhenZero | count=0 → 150 |
| ClampsCountAbove500 | count=1000 → 500 |
| RejectsExpiredSession | SESSION\_EXPIRED |
| RejectsNonexistentSession | SESSION\_NOT\_FOUND |
| RejectsWhenEventStoreIsNil | INTERNAL guard |
| DeniesUnauthorizedPublicStream | ABAC denies non-co-located location stream |
| AdminCanReadAnyPublicStream | Admin reads non-co-located location stream |
| PassesBeforeIDToReplayTail | Cursor propagated correctly |
| PassesNotBeforeToReplayTail | Time floor propagated correctly |
| SetsHasMoreWhenMoreEventsExist | count+1 fetch logic |
| SetsHasMoreFalseWhenNoMore | Exact or fewer events returned |
| ReturnsEventsInAscendingOrder | Verify ordering |
| IncludesResponseMeta | RequestMeta echoed in ResponseMeta |

### 8.2 Invariant tests — I-17 (`internal/grpc/query_stream_history_test.go`)

Dedicated test group proving I-17 holds under every private stream variant.
Test names MUST reference the invariant for traceability.

| Test | Description |
| --- | --- |
| I17DeniesNonMemberSceneStreamRead | Non-member character → STREAM\_ACCESS\_DENIED |
| I17DeniesAdminNonMemberSceneStreamRead | Admin without membership → STREAM\_ACCESS\_DENIED |
| I17DeniesOtherCharacterPersonalStreamRead | Cannot read another character's stream |
| I17PermitsMemberSceneStreamRead | Active focus membership → permitted |
| I17PermitsOwnCharacterStreamRead | Own character stream → permitted |
| I17DeniesAfterFocusMembershipRemoved | Membership removed → immediately denied |
| I17DeniesSceneStreamWithMalformedTargetID | Garbage ULID in scene stream → INVALID\_ARGUMENT |

### 8.3 Boundary tests — pagination (`internal/grpc/query_stream_history_test.go`)

| Test | Description |
| --- | --- |
| ReturnsEmptyEventsForEmptyStream | Stream has no events → empty, has\_more=false |
| PaginationWalksBackwardCorrectly | Three pages with before\_id chaining |
| BeforeIDAtOldestEventReturnsEmpty | Cursor past beginning → empty, has\_more=false |
| CountExactlyMatchesAvailableEvents | count == available → all events, has\_more=false |
| CountOneLessThanAvailable | count == available-1 → has\_more=true |
| NotBeforeFiltersOldEvents | Time floor excludes events before threshold |
| NotBeforeAndBeforeIDCombined | Both filters active simultaneously |
| BeforeIDWithZeroULIDIgnored | Zero ULID means no upper bound |

### 8.4 Unit tests — ReplayTail beforeID (`internal/core/store_memory_test.go`)

| Test | Description |
| --- | --- |
| ReplayTailWithBeforeIDExcludesNewerEvents | Only events with id \< beforeID |
| ReplayTailWithZeroBeforeIDReturnsAll | Backward compatible |
| ReplayTailWithBeforeIDAndNotBefore | Both filters applied |
| ReplayTailBeforeIDAtBoundaryExcludesExact | id == beforeID excluded |

### 8.5 Unit tests — web gateway proxy (`internal/web/handler_test.go`)

| Test | Description |
| --- | --- |
| ProxiesToCoreServiceAndMapsResponse | Happy path delegation |
| PropagatesErrorFromCoreService | Error forwarding |
| MapsGameEventsCorrectly | corev1.Event → webv1.GameEvent mapping |
| PropagatesHasMore | has\_more forwarded |

### 8.6 Unit tests — seed policy (`internal/access/policy/seed_test.go`)

| Test | Description |
| --- | --- |
| SeedPoliciesCountUpdated | Total count incremented by 1 |
| PlayerLocationStreamReadPolicyExists | New policy in seed set |

### 8.7 Seed policy smoke tests (`internal/access/policy/seed_smoke_test.go`)

Full engine evaluation with all seed policies loaded.

| Test | Description |
| --- | --- |
| PlayerCanReadCoLocatedLocationStream | Co-located → permitted |
| PlayerCannotReadNonCoLocatedLocationStream | Different location → denied |
| AdminCanReadAnyLocationStream | Admin at any location → permitted |

### 8.8 Integration tests — ReplayTail DB (`internal/store/postgres_integration_test.go`)

Ginkgo/Gomega with testcontainers PostgreSQL. Extends existing `ReplayTail`
describe block.

| Test | Description |
| --- | --- |
| ReplayTailWithBeforeIDReturnsOnlyOlderEvents | SQL `id < $beforeID` correct |
| ReplayTailWithBeforeIDAndNotBeforeCombined | Both SQL filters |
| ReplayTailBeforeIDZeroIgnored | Zero ULID → no upper bound filter |
| ReplayTailCountPlusOneForHasMore | Fetch(n+1) returns correct count |

### 8.9 Integration tests — QueryStreamHistory RPC (`test/integration/`)

Full gRPC call through CoreServer with real PostgreSQL, session store, and
ABAC engine. Tests the complete handler flow at the Go level.

| Test | Description |
| --- | --- |
| QueryStreamHistoryReturnsEventsForSubscribedStream | Full flow: create session, subscribe, append events, query |
| QueryStreamHistoryDeniesNonMemberSceneStream | I-17 with real ABAC + real session store |
| QueryStreamHistoryPaginatesCorrectly | Multiple pages via before\_id with real DB |
| QueryStreamHistoryAdminReadsPublicStream | Admin role resolved by real attribute provider |

### 8.10 E2E tests — web client (`web/e2e/terminal.spec.ts`)

Playwright through the full web gateway → gRPC → PostgreSQL stack.

| Test | Description |
| --- | --- |
| QueryStreamHistoryReturnsEventsViaWebGateway | Web client calls WebQueryStreamHistory, receives events |

**Note:** The bead notes mention un-skipping `page reload replays prior events
from multiple guests` — that is B13 scope (web client calling this RPC on
mount). B9's E2E test verifies the RPC is accessible through the web gateway,
not the reload-backfill UX.

---

## 9. Files Modified

| File | Action | Description |
| --- | --- | --- |
| `api/proto/holomush/core/v1/core.proto` | Modify | Add QueryStreamHistory RPC + messages |
| `api/proto/holomush/web/v1/web.proto` | Modify | Add WebQueryStreamHistory RPC + messages |
| `internal/core/store.go` | Modify | Add `beforeID` param to ReplayTail |
| `internal/core/store_memory.go` | Modify | Implement beforeID filter |
| `internal/core/store_memory_test.go` | Modify | Tests for beforeID |
| `internal/store/postgres.go` | Modify | SQL for beforeID filter |
| `internal/store/postgres_test.go` | Modify | Tests for beforeID |
| `internal/core/mocks/mock_EventStore.go` | Regenerate | mockery |
| `internal/grpc/query_stream_history.go` | Create | Handler implementation |
| `internal/grpc/query_stream_history_test.go` | Create | Handler tests |
| `internal/web/handler.go` | Modify | Add CoreClient method + proxy handler |
| `internal/web/handler_test.go` | Modify | Proxy tests |
| `internal/access/policy/seed.go` | Modify | Add seed:player-location-stream-read |
| `internal/access/policy/seed_test.go` | Modify | Update count + policy tests |
| `internal/plugin/goplugin/host_service.go` | Modify | Update ReplayTail call (add zero beforeID) |
| `internal/plugin/hostfunc/stdlib_focus.go` | Modify | Update `HistoryReader` interface + ReplayTail call (add zero beforeID) |
| `internal/plugin/hostfunc/stdlib_focus_test.go` | Modify | Update mock `HistoryReader` + tests for new signature |
| `internal/access/policy/seed_smoke_test.go` | Modify | Stream-read smoke tests |
| `internal/store/postgres_integration_test.go` | Modify | ReplayTail beforeID integration tests |
| `test/integration/` | Create | QueryStreamHistory RPC integration tests |
| `web/e2e/terminal.spec.ts` | Modify | E2E test for WebQueryStreamHistory |

---

## 10. Relationship to Parent Spec

This design refines parent spec §4.7 with the following changes:

| Parent spec | This design | Rationale |
| --- | --- | --- |
| Hard cap at 500 | Default 150, max 500 per page, no total cap | Pagination makes total cap unnecessary |
| `not_before_ms` only | `before_id` (ULID cursor) + `not_before_ms` (time floor) | ULID pagination avoids millisecond-boundary duplicates |
| "ABAC stream access check" (unspecified) | Two-layer: membership gate (I-17) for private streams + ABAC `Evaluate()` for public | Invariant-enforced private stream protection; ABAC for policy-governed public streams |
| No `has_more` | `has_more` boolean in response | Client needs pagination signal |

All other aspects (session validation, no cursor mutation, ReplayTail delegation)
are unchanged from the parent spec.
