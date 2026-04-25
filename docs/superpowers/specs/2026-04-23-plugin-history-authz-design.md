# Plugin history authz — membership enforcement at the plugin boundary

**Bead:** `holomush-095g` (P1 bug)
**Status:** design — pending implementation plan
**Author:** Sean Brandt (with AI assistance)
**Date:** 2026-04-23
**Supersedes:** none

**Related (not superseded, downstream / adjacent):**

- `docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md` §11.1 asserted that the plugin audit RPC contract is "unchanged" by the cursor rework. That assertion stands for the pagination fields. This spec adds `caller` to the same request message — a new, orthogonal, backward-compatible extension.
- `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` §6 (plugin-owned subject routing / OwnerMap).
- Follow-up beads that will become plugin-owned history callers after landing: `holomush-l4kx` (audit-backfill CLI), `holomush-ecbg` (drift detector), `holomush-6nds` (JS rebuild tool).

## 1. Problem statement

F5 of the JetStream EventBus cutover (PR #252) landed `PluginAuditService.QueryHistory` in `plugins/core-scenes/audit.go` without scene-membership enforcement at the plugin side. The plugin currently trusts the host's outer I-17 membership gate (in `CoreServer.QueryStreamHistory`) to authorize callers; the plugin itself returns every row matching the subject.

The TODO at `plugins/core-scenes/audit.go:211` documents the gap:

> `// TODO(holomush-1tvn.12 follow-up): plumb caller identity from the host`
> `// through the plugin gRPC connection so the membership check can hard-`
> `// gate non-participant queries at the plugin boundary.`

The current shape is fragile for three reasons:

1. **Trust boundary crosses a process boundary.** The plugin is a `hashicorp/go-plugin` subprocess. Any gRPC that crosses that socket is a public contract from the plugin's perspective — and `docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md` §11.1 deliberately leaves the plugin audit RPC stable through the cursor rework, signalling that this IS a long-lived public interface, not an internal detail.
2. **Future callers will bypass the outer handler.** Follow-up beads `holomush-l4kx`, `holomush-ecbg`, and `holomush-6nds` are all plugin-owned-history readers that will plausibly not go through the session-bound `CoreServer.QueryStreamHistory` path. Without a plugin-side wall, each of those becomes policy-carrying.
3. **Dev tooling / test harnesses** dial the plugin socket directly (`plugin_audit_isolation_test.go` already does). Today these get free reads.

## 2. Goals & non-goals

**Goals.**

- Plugin-side defense-in-depth: `PluginAuditService.QueryHistory` MUST enforce domain-specific authz (scene membership) against a caller identity forwarded from the host.
- The identity requirement MUST be visible on the proto contract, not buried in transport plumbing.
- The outer I-17 gate at `CoreServer.QueryStreamHistory` MUST remain load-bearing for the session-bound client path. Information-hiding (`STREAM_ACCESS_DENIED` opaque to the client) MUST be preserved end-to-end.
- The plugin MUST re-check membership per call, never cache across paginations.

**Non-goals.**

- Admin / system-tool bypass. Non-`CHARACTER` caller kinds are rejected at this RPC; administrative access is a future extension with its own capability grant.
- Per-channel (`ic` vs `ooc`) membership differentiation. Any participant with an `owner` or `member` role can read any channel of that scene. Matches the outer I-17 gate's scene-level granularity.
- "Preview before joining" semantics for invited characters. Invitation grants join rights; reading follows joining.
- Any change to `Subscribe`-style RPCs. This spec is QueryHistory only.
- **Plugin-as-caller identity for the plugin-host `QueryStreamHistory` RPC.** A second router call path exists at `cmd/holomush/sub_grpc.go:511` (`busHistoryReaderAdapter.ReplayTail`), serving `PluginHostService.QueryStreamHistory` (plugin → host). Defining a "plugin reading on its own behalf" identity model — what `Actor.Kind` it carries, what capability grants gate cross-plugin reads, whether plugins can read their own subjects without the gate — is its own design problem. This spec defers it to a follow-up bead. See §4.4 for the concrete consequence (this RPC will return `PERMISSION_DENIED` for plugin-owned subjects after this lands; no in-tree caller breaks because none currently reads plugin-owned subjects).

## 3. Architecture

### 3.1 Trust model

Two walls, both load-bearing:

**Outer wall (unchanged):** `CoreServer.QueryStreamHistory` keeps the I-17 membership gate (`sessionHasMembership`) for scene streams before any routing. This wall catches the session-bound path, checks `info.FocusMemberships` (active session focus → implies membership), and preserves the existing opaque `STREAM_ACCESS_DENIED` response.

**Inner wall (new):** `PluginAuditService.QueryHistory` performs its own membership check against the plugin's authoritative `scene_participants` table. This wall catches any future caller that reaches the plugin without going through the outer handler — follow-up beads, dev tooling, test harnesses, or any future host path that forgets to propagate the session check.

**Why both are load-bearing, not redundant.** The two walls check *complementary* properties:

| Wall | Property checked | Data source |
| --- | --- | --- |
| Outer (I-17) | "Is this session currently focused on this scene?" | `session.Info.FocusMemberships` |
| Inner (plugin) | "Is this character a member of this scene?" | `scene_participants` table |

On the session-bound path, both MUST hold. The outer is strictly stronger (focus implies membership), so the inner is passive defense. For non-session-bound callers (future backfill CLI, drift detector), the outer is not traversed — the plugin wall is the sole gate, and `scene_participants` is the correct authoritative source.

### 3.2 Identity forwarding under trusted transport

The "caller" passed to the plugin is not a self-attestation from the end-user. It is the host forwarding an identity that the host already authenticated. The property the plugin relies on:

- **Only the host can populate `caller`.** The field exists on the host↔plugin RPC, never on any client↔host RPC. The web client holds a session token; the outer handler resolves session → character_id via `sessionStore.Get`; that server-derived value is what crosses into the plugin.
- **The plugin socket is transport-authenticated.** `hashicorp/go-plugin` uses a handshake cookie known only to the host process. The plugin treats "whoever called me over this socket" as "the host."
- **The plugin makes its membership decision against local authoritative data** (`scene_participants`), not against any claim the caller makes about membership.

The plugin is not trusting the caller to self-identify correctly; it is trusting the host's authentication and making its own policy decision.

## 4. Wire contract

### 4.1 Proto change (`api/proto/holomush/plugin/v1/audit.proto`)

The change is **purely additive**: append `caller = 8` to the existing `QueryHistoryRequest`. Fields 1–7 retain their existing comments verbatim (`bytes after = 2; // ULID; empty = from start`, etc. — see the current proto file). Implementer MUST NOT touch the existing fields' definitions or comments; this section shows only the new field:

```proto
// caller identifies the principal on whose behalf the host is reading.
// Plugins implementing PluginAuditService MUST enforce domain-specific
// authz (e.g., membership) against this identity before returning rows.
// An absent caller, a zero identity, or an unsupported Actor.Kind MUST
// be rejected with gRPC PERMISSION_DENIED. The host populates this
// field from the authenticated session record; clients never supply it.
holomush.eventbus.v1.Actor caller = 8;
```

The proto comment is load-bearing — it is the discoverability property that makes the security requirement contract-visible rather than transport-hidden.

Backward compat: `caller = 8` is an additive field. Today only `core-scenes` implements `PluginAuditService` (verified via `rg "PluginAuditService" plugins/`); no other plugin is affected. A plugin compiled against the old proto would still build, but the spec and proto comment make enforcement MUST-language for any plugin consuming this RPC.

### 4.2 Internal host types (`internal/eventbus`)

`HistoryQuery` gains a `Caller` field:

```go
type HistoryQuery struct {
    Subject   Subject
    Direction Direction
    PageSize  int
    NotBefore time.Time
    NotAfter  time.Time
    AfterSeq  uint64
    AfterID   ulid.ULID
    BeforeSeq uint64
    BeforeID  ulid.ULID
    Caller    Actor // NEW
}
```

`Caller` is a property of the query (alongside `NotBefore`, `Subject`, etc.). Public-tier readers (hot JetStream, cold Postgres) ignore the field; only the plugin router propagates it.

`PluginHistoryRouter.QueryHistory` signature is unchanged — the router reads `q.Caller` from the query object and maps it into the proto request. Only the diff against the existing implementation (`internal/eventbus/audit/plugin_router.go:70–88`) is shown; the existing `q.AfterID.IsZero()` / `q.BeforeID.IsZero()` / `q.NotBefore` / `q.NotAfter` blocks remain untouched:

```go
req := &pluginv1.QueryHistoryRequest{
    Subject:   string(q.Subject),
    PageSize:  int32(pageSize),
    Direction: directionProto(q.Direction),
    Caller:    eventbus.ActorToProto(q.Caller), // NEW
}
```

`actorToProto` and `actorKindToProto` currently live in `internal/eventbus/publisher.go:274` as unexported helpers. This spec MUST export them (rename to `ActorToProto` / `ActorKindToProto`) so the audit-router package can reuse the single source of truth for `Actor` ⇄ proto mapping. Duplicating the switch in `audit/plugin_router.go` would silently drift on any future enum extension.

### 4.3 Host handler (`internal/grpc/query_stream_history.go`)

The handler already loads `info.CharacterID` from the authenticated session in Step 1. The existing `fetchHistoryFramesFromBus` helper at `internal/grpc/query_stream_history.go:279` constructs the `HistoryQuery` internally; the handler holds only positional state. The chosen threading mechanism: **extend `fetchHistoryFramesFromBus` with one new parameter `caller eventbus.Actor`** and have it set `q.Caller = caller` at construction time.

Rationale: the function already takes 6 positional args; adding a 7th is minimally disruptive. Refactoring to construct `HistoryQuery` at the handler is a larger surgery and not required to deliver this spec.

New call site:

```go
// In QueryStreamHistory, after Step 5 (authorization) and before Step 7:
caller := eventbus.Actor{
    Kind: eventbus.ActorKindCharacter,
    ID:   info.CharacterID,
}
frames, fetchErr := fetchHistoryFramesFromBus(
    ctx, s.historyReader, s.currentGameID(), req.Stream, count,
    notBefore, beforeSeq, beforeID, caller, // NEW
)
```

New signature:

```go
func fetchHistoryFramesFromBus(
    ctx context.Context,
    reader eventbus.HistoryReader,
    gameID, legacyStream string,
    count int,
    notBefore time.Time,
    beforeSeq uint64,
    beforeID ulid.ULID,
    caller eventbus.Actor, // NEW
) ([]*corev1.EventFrame, error)
```

Inside, the existing `q := eventbus.HistoryQuery{...}` construction adds `Caller: caller`.

The handler MUST derive `caller` from the session record only. It MUST NOT accept a client-supplied character_id from the request body.

### 4.4 Plugin-host `QueryStreamHistory` RPC path

A second router call path exists at `cmd/holomush/sub_grpc.go:511` — the `busHistoryReaderAdapter.ReplayTail` method that satisfies `plugins.HistoryReader` for `PluginHostService.QueryStreamHistory` (plugin → host RPC, used by Lua `holomush.query_stream_history` and the Go SDK `pkg/plugin/focus_client.go` equivalent).

This adapter constructs a `HistoryQuery` (`sub_grpc.go:527–535`) with no `Caller` — it has no caller identity to populate, because plugin callers are not characters and the SDK does not currently carry a per-call character context.

**Behavior after this spec lands:**

- For host-owned subjects (`character:*`, `location:*`, `global`, etc.): the cold/hot tier readers ignore `q.Caller`. No regression.
- For plugin-owned subjects (e.g., `events.<gid>.scene.<id>.<channel>`): the adapter routes to `PluginHistoryRouter.QueryHistory`, which forwards a zero `caller` to the plugin, which returns `PERMISSION_DENIED` per §5.2. The plugin RPC fails closed.

**Why this is acceptable for this spec:**

- No in-tree caller currently reads plugin-owned subjects via `PluginHostService.QueryStreamHistory`. The cold-tier spec's §11.2 (line 627) verified at spec-write time that no `.lua` plugin calls `holomush.query_stream_history`; the Go SDK has the same status. Failing closed today breaks no caller.
- The fail-closed behavior matches the spec's goal: non-session-bound callers that lack a vetted identity story MUST NOT read plugin-owned audit logs.

**Required follow-up:**

A new bead MUST be filed before this spec's PR merges, capturing the plugin-as-caller identity design problem: "what `Actor.Kind` does a plugin caller carry, what capability grants gate cross-plugin reads, and how does a plugin assert which character (if any) it is reading on behalf of?" That bead is `holomush-095g`'s direct successor and will land before any in-tree code starts using `PluginHostService.QueryStreamHistory` against plugin-owned subjects.

## 5. Plugin enforcement semantics

### 5.1 Step ordering

Plugin `QueryHistory` MUST execute checks in this order:

1. Validate `req.Subject` is non-empty and non-wildcard.
2. Validate `req.Caller` is present and has `Kind = ACTOR_KIND_CHARACTER` with a non-zero ID.
3. Parse `req.Subject` into `sceneID` using the plugin-owned grammar.
4. Check `scene_participants` membership for `(sceneID, callerID)`.
5. Only then: decode cursor, compute pagination, query `scene_log`, stream rows.

**Rationale:** auth must run before any DB work to avoid timing oracles and to keep the denial path cheap. A non-participant's request costs one indexed PK lookup, not a full pagination.

### 5.2 Actor kind dispatch

The proto enum (`api/proto/holomush/eventbus/v1/eventbus.proto:13–19`) defines exactly five values: `ACTOR_KIND_UNSPECIFIED` (zero), `_CHARACTER`, `_PLAYER`, `_SYSTEM`, `_PLUGIN`. The Go-side `eventbus.ActorKindUnknown` (zero value of the in-process type) maps to proto `ACTOR_KIND_UNSPECIFIED` via `actorKindToProto`. There is no proto `ACTOR_KIND_UNKNOWN`.

| `caller.Kind` (proto enum)    | Behavior                                                       | oops code                   |
| ----------------------------- | -------------------------------------------------------------- | --------------------------- |
| `ACTOR_KIND_UNSPECIFIED` (0)  | PERMISSION_DENIED — host bug or zero value, fail closed        | `SCENE_AUDIT_AUTH_REQUIRED` |
| `ACTOR_KIND_CHARACTER`        | Proceed to membership check with `caller.Id` as character ULID | —                           |
| `ACTOR_KIND_PLAYER`           | PERMISSION_DENIED — not supported at this RPC                  | `SCENE_AUDIT_AUTH_REQUIRED` |
| `ACTOR_KIND_SYSTEM`           | PERMISSION_DENIED — admin tools require a separate capability  | `SCENE_AUDIT_AUTH_REQUIRED` |
| `ACTOR_KIND_PLUGIN`           | PERMISSION_DENIED — cross-plugin reads not supported (§4.4)    | `SCENE_AUDIT_AUTH_REQUIRED` |
| any other value (proto drift) | PERMISSION_DENIED                                              | `SCENE_AUDIT_AUTH_REQUIRED` |

A `CHARACTER` kind with a zero-bytes `Id` MUST also be rejected (treat as missing identity) — that check is logically separate from kind dispatch but lives in the same step.

### 5.3 Subject parsing (plugin-owned grammar)

Plugin owns the subject grammar declared in `plugin.yaml` (`events.*.scene.>`). The router forwards `q.Subject` to the plugin in JetStream-native form (`events.<gameID>.scene.<sceneID>.<channel>`); the plugin parses native, not legacy.

**Why the plugin parses native directly rather than calling `subjectxlate.ToLegacy` first:**

`subjectxlate` (`internal/eventbus/subjectxlate/subjectxlate.go`) is a host-domain bridge between native (`events.<game>.<ns>.<rest>`) and legacy colon-delimited (`<ns>:<rest>`) forms. The plugin already receives native subjects on every audit call and writes them to `scene_log` in native form. Routing through `ToLegacy` to obtain `scene:<id>:<channel>` and then splitting on `:` adds a dependency from the plugin to a host-side helper for zero benefit — both paths require the same number of tokens, the same wildcard rejection, and the same error semantics. Direct native parsing keeps the plugin self-contained.

Parse rules:

- Expected shape: `events.<gameID>.scene.<sceneID>.<channel>[.<...>]`
- Accept: concrete `<sceneID>` token (non-empty, non-wildcard), any `<channel>` suffix.
- Reject: fewer than 5 tokens, first token not `events`, third token not `scene`, any token containing wildcard characters (`*`, `>`).
- Plugin does NOT validate `<gameID>` semantically — that's the host's concern — but DOES reject it if it contains wildcards (per the rule above).
- Malformed subject returns `INVALID_ARGUMENT` with oops `SCENE_AUDIT_SUBJECT_INVALID`.

### 5.4 Membership lookup

**Role-policy change:** the existing function comment at `plugins/core-scenes/audit.go:202–204` describes (but does not enforce) "owner, member, or invited" as the read-permitted set. This spec **deliberately tightens** that policy to "owner or member only" — the previously-documented inclusion of `invited` was aspirational, never enforced (since the function had no auth at all), and conflicts with the principle that invitation grants *join* rights, not *passive read* rights. §7 step 8 captures the docstring update; this change is intentional, not a casual cleanup.

New narrow helper on `SceneStore`:

```go
// IsMember reports whether characterID has an owner or member row in
// sceneID. Invited-only rows return false — invitation grants join
// rights, not read rights. Missing scene or missing row both return
// false with nil error.
func (s *SceneStore) IsMember(ctx context.Context, sceneID, characterID string) (bool, error)
```

Single indexed lookup on `scene_participants` (PK `(scene_id, character_id)`). Predicate `WHERE role IN ('owner', 'member')`. No allocations beyond the boolean. Existing `GetWithMembership` is unchanged; it has other callers (service handler) that need the hydrated scene + slice shape.

**`IsMember` "missing scene = false, nil error" is a deliberate divergence** from the rest of the file. Other helpers (e.g., `SCENE_NOT_FOUND` at the service layer) distinguish "scene missing" from "permission denied." `IsMember` collapses them on purpose: at the audit-read boundary, distinguishing the two would leak scene existence to non-members. Information-hiding wins over diagnostic precision here. Internal logs MAY emit a debug line distinguishing the cases for the operator's benefit, but the function's return type does not.

Non-membership returns PERMISSION_DENIED with oops `SCENE_AUDIT_ACCESS_DENIED`. DB errors return `INTERNAL` wrapped.

### 5.5 Error mapping end-to-end

The host's existing `mapHistoryError` (`internal/grpc/query_stream_history.go:259–270`) switches on `errors.Is(err, eventbus.ErrCursor*)` only. It cannot inspect oops codes through the wrapping chain that `PluginHistoryRouter` adds (`AUDIT_PLUGIN_HISTORY_RPC_FAILED` at `plugin_router.go:98`, `AUDIT_PLUGIN_HISTORY_RECV_FAILED` at `:153`). The end-to-end translation requires three coordinated changes; this section specifies all three.

**1. Plugin-side error emission.** Plugin handlers MUST return errors using `google.golang.org/grpc/status` so the gRPC status code crosses the wire as a structured value, not as a stringified error message. The plugin SHOULD also wrap with oops for log dimensioning, but the wire-level error MUST be a status.Error:

```go
// In SceneAuditServer.QueryHistory, on access denied:
return status.Error(codes.PermissionDenied, "scene audit access denied")
// Internal logs/metrics tag the case via an unwrapped oops sibling:
slog.InfoContext(ctx, "scene audit access denied",
    "subject", req.GetSubject(),
    "code", "SCENE_AUDIT_ACCESS_DENIED",
)
```

The oops-code-as-log-attribute pattern (rather than `oops.Code(...).Wrap(status.Error(...))`) avoids burying the gRPC code beneath an oops wrapper. Pure status errors round-trip through gRPC cleanly.

**2. Router-side gRPC status preservation.** `PluginHistoryRouter.QueryHistory` and `pluginHistoryStream.Next` currently wrap every error with their own oops codes (`AUDIT_PLUGIN_HISTORY_RPC_FAILED`, `_RECV_FAILED`). This MUST change to **preserve gRPC status codes from the plugin** while still adding diagnostic context:

```go
// In plugin_router.go QueryHistory and pluginHistoryStream.Next:
if err != nil {
    if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
        // Preserve the plugin's gRPC code; add subject/plugin context as
        // log attributes, not as an oops wrapper that would shadow it.
        return ..., err
    }
    // Non-status errors (transport failures, etc.) keep the existing
    // AUDIT_PLUGIN_HISTORY_*_FAILED oops wrapping.
    return ..., oops.Code("AUDIT_PLUGIN_HISTORY_RPC_FAILED")...
}
```

The router becomes a **transparent proxy for gRPC status errors** and an **annotating wrapper for non-status errors**. This is the load-bearing change for end-to-end opacity.

**3. Host handler translation.** `mapHistoryError` gains a status-code dispatch step **before** the existing `errors.Is` chain:

```go
func mapHistoryError(err error) error {
    // NEW: gRPC status pass-through with opacity translation.
    if st, ok := status.FromError(err); ok {
        switch st.Code() {
        case codes.PermissionDenied:
            // Opaque: collapse plugin-boundary denial into the same code
            // the outer I-17 gate uses. Client cannot distinguish.
            return oops.Code("STREAM_ACCESS_DENIED").
                Errorf("not authorized to read stream")
        case codes.InvalidArgument:
            return status.Errorf(codes.InvalidArgument, "%v", err)
        }
        // Other status codes (Internal, Unavailable, …) pass through.
    }
    // Existing cursor-error dispatch unchanged below.
    switch {
    case errors.Is(err, eventbus.ErrCursorInvalid):
        return status.Errorf(codes.InvalidArgument, "%v", err)
    // …
    }
}
```

End-to-end matrix:

| Condition                  | Plugin emits                                                     | Router behavior          | Host handler emits to client          |
| -------------------------- | ---------------------------------------------------------------- | ------------------------ | ------------------------------------- |
| Caller missing / bad kind  | `status.Error(codes.PermissionDenied, …)` + log `SCENE_AUDIT_AUTH_REQUIRED`  | preserve status         | `STREAM_ACCESS_DENIED` (opaque)        |
| Not a participant          | `status.Error(codes.PermissionDenied, …)` + log `SCENE_AUDIT_ACCESS_DENIED`  | preserve status         | `STREAM_ACCESS_DENIED` (opaque)        |
| Subject malformed          | `status.Error(codes.InvalidArgument, …)` + log `SCENE_AUDIT_SUBJECT_INVALID` | preserve status         | `INVALID_ARGUMENT` (pass-through)     |
| Store / DB error           | `status.Error(codes.Internal, …)` (or oops-wrapped non-status)   | preserve / wrap          | `INTERNAL`                             |
| Plugin transport failure   | gRPC transport error (not from plugin handler)                   | wrap `AUDIT_PLUGIN_HISTORY_RPC_FAILED` | `INTERNAL` (existing default)  |

**Information-hiding invariant:** the client MUST NOT be able to distinguish "outer I-17 denial" from "plugin-boundary denial." Both surface to the client as the same oops-coded error: oops code `STREAM_ACCESS_DENIED`, message `"not authorized to read stream"`, identical context fields. Plugin-internal log attributes (`SCENE_AUDIT_*` codes) preserve operator-side observability without leaking to the client.

**Code-axis vocabulary (used throughout §5.5, §6, §8):**

- *gRPC status code* — a member of `google.golang.org/grpc/codes` (`codes.PermissionDenied`, `codes.InvalidArgument`, etc.). Carried on the wire by `status.Error` / `status.FromError`.
- *oops code* — a HoloMUSH-internal structured-error tag set via `oops.Code("…")`. Used for log dimensioning and for the existing handler's denial vocabulary (`STREAM_ACCESS_DENIED` is an oops code, not a gRPC code). Currently surfaces to clients via the project's oops→error marshalling, not as a gRPC status.
- These are independent axes. A test that says "assert STREAM_ACCESS_DENIED" MUST extract the oops code via `oops.AsOops(err).Code()`. A test that says "assert PermissionDenied" MUST extract the gRPC code via `status.Code(err)`.

## 6. Testing

### 6.1 Unit — plugin (`plugins/core-scenes/audit_test.go`, new)

Table-driven auth dispatch matrix covering the kind-× membership cross-product:

- nil caller, zero ID, character member, character owner, character invited-only, character non-participant, each non-character kind.
- Assertions on oops code and gRPC status.

Table-driven subject parsing:

- Valid `events.*.scene.<id>.ic`, valid `…ooc`, non-scene prefixes, wildcard tokens, too-few tokens, empty subject.

**Early-rejection test:** non-participant call with an otherwise valid request asserts that the `scene_log` store mock is NEVER invoked. Pins the step ordering against future refactors.

**Membership-change-mid-pagination test:** seed N rows for a member; plugin reads page 1 (page_size = N/2, rows returned); delete participant row; plugin reads page 2 → PERMISSION_DENIED. Proves per-call re-check.

### 6.2 Unit — store (`plugins/core-scenes/store_integration_test.go`)

Extend with `IsMember` cases against testcontainers PG:

- Owner → true. Member → true. Invited only → false. No row → false. Missing scene → false.

### 6.3 Unit — router (`internal/eventbus/audit/plugin_router_test.go`)

- `q.Caller` forwarded verbatim into `QueryHistoryRequest.Caller` via `eventbus.ActorToProto` (kind and id bytes).
- Zero `q.Caller` forwarded as-is (router is pure; plugin enforces).
- **gRPC status preservation:** when the underlying plugin client returns a `status.Error(codes.PermissionDenied, …)`, the router returns the same status error unchanged (no oops wrapping). Test uses a stub `pluginv1.PluginAuditServiceClient` that returns a precise status from both `QueryHistory` (immediate) and `Recv` (mid-stream).
- **Non-status passthrough:** when the underlying plugin client returns a non-status transport error (e.g., simulated connection drop), the router wraps with the existing `AUDIT_PLUGIN_HISTORY_RPC_FAILED` / `_RECV_FAILED` oops codes.

### 6.4 Unit — host handler (`internal/grpc/query_stream_history_test.go`)

- **Caller threading:** the handler passes `eventbus.Actor{Kind: ActorKindCharacter, ID: info.CharacterID}` as the new `caller` argument to `fetchHistoryFramesFromBus`. Test injects a fake `HistoryReader` that captures the `q.Caller` it received and asserts it matches the session's character ID.
- **`mapHistoryError` PERMISSION_DENIED dispatch:** input is a bare `status.Error(codes.PermissionDenied, "scene audit access denied")`; assert the function returns an oops-coded `STREAM_ACCESS_DENIED` error whose message matches the outer I-17 gate's message (so the client cannot distinguish).
- **`mapHistoryError` INVALID_ARGUMENT pass-through:** input is `status.Error(codes.InvalidArgument, "subject malformed")`; assert pass-through to `codes.InvalidArgument`.
- **`mapHistoryError` non-status fallback:** input is a plain oops-wrapped error with no gRPC status; assert the existing cursor-error dispatch still applies (no regression on `ErrCursorInvalid` / `Stale` / `Lag`).

### 6.5 Integration (`test/integration/eventbus_e2e/plugin_audit_isolation_test.go`, extend)

Per the `holomush-l60y` consolidation effort, extend the existing file rather than create parallels:

1. **End-to-end non-participant denial:** character X not a member of scene S; X's session invokes `CoreServer.QueryStreamHistory` on `scene:S:ic`. Outer I-17 catches first. Assert the returned error carries oops code `STREAM_ACCESS_DENIED` (extract via `oops.AsOops(err).Code()`) and message `"not authorized to read stream"`. Proves the outer wall is unchanged.
2. **End-to-end plugin-boundary denial (with outer disabled):** simulate a non-session-bound caller by populating `info.FocusMemberships` to bypass the outer I-17 gate (e.g., add a focus row for the scene), then run with a non-participant `info.CharacterID`. The outer gate now passes; the plugin wall catches. Assert the returned error carries oops code `STREAM_ACCESS_DENIED` (same code as case 1 — opacity invariant). This is the load-bearing test for §5.5: it proves the plugin's `status.Error(codes.PermissionDenied)` round-trips through the router and is collapsed into the same oops code the outer wall uses.
3. **Plugin-boundary direct (router level):** call `PluginHistoryRouter.QueryHistory` directly with a valid `Caller` that is NOT a participant. Assert the returned error has gRPC status code `codes.PermissionDenied` (extract via `status.Code(err)`) — the plugin's status error round-trips through the router unchanged. Proves the gRPC status is preserved across the router boundary.
4. **Missing-caller failsafe (router level):** router invoked with zero `q.Caller`. Assert gRPC status code `codes.PermissionDenied`. The "host bug shouldn't silently leak" case.
5. **Mid-read revocation:** member reads page 1 successfully; kick; page 2 returns gRPC `codes.PermissionDenied` at the router level (case 3 / 4 vocabulary) and oops `STREAM_ACCESS_DENIED` at the outer-handler level (case 1 / 2 vocabulary).
6. **Plugin-host RPC fail-closed (§4.4):** invoke `PluginHostService.QueryStreamHistory` (the plugin → host RPC, exercised via `busHistoryReaderAdapter.ReplayTail`) against a plugin-owned scene subject. Assert gRPC status code `codes.PermissionDenied` reaches the plugin caller. Documents the deferred-but-correct fail-closed behavior and pins it as a regression guard until the follow-up bead lands the plugin-as-caller identity story.

## 7. Migration & rollout

No two-system overlap. Changes land in a single PR:

1. Proto change (`audit.proto`): append `caller = 8` only; do not touch fields 1–7. Regenerate.
2. Export `eventbus.ActorToProto` and `eventbus.ActorKindToProto` (rename from unexported `actorToProto` / `actorKindToProto` in `internal/eventbus/publisher.go`). Update existing in-package callers.
3. Add `Caller eventbus.Actor` field to `eventbus.HistoryQuery`.
4. Add `caller eventbus.Actor` parameter to `internal/grpc/query_stream_history.go:fetchHistoryFramesFromBus`; set `q.Caller = caller` at construction.
5. Host handler `QueryStreamHistory`: derive `caller` from `info.CharacterID` and pass to `fetchHistoryFramesFromBus`.
6. `PluginHistoryRouter.QueryHistory`: populate `req.Caller` via the exported helper; preserve gRPC status codes from the plugin (transparent proxy for `status.FromError`-recognised errors; existing oops wrap only for non-status errors).
7. `pluginHistoryStream.Next`: same gRPC-status preservation pattern as the router's outer call.
8. Plugin `SceneAuditServer.QueryHistory`: implement auth ordering (caller validation → subject parse → membership) per §5.1; emit denials via `status.Error(codes.PermissionDenied, …)` with sibling `slog` lines carrying the `SCENE_AUDIT_*` oops codes for log dimensioning.
9. `SceneStore.IsMember` added (`WHERE role IN ('owner', 'member')`).
10. `mapHistoryError` (`internal/grpc/query_stream_history.go`): prepend a `status.FromError` dispatch step; map `codes.PermissionDenied` → `STREAM_ACCESS_DENIED` (opaque); pass `codes.InvalidArgument` through.
11. Tests per §6.
12. Remove the TODO at `plugins/core-scenes/audit.go:211` and update the function docstring to document the enforcement contract (owner/member only — see §5.4 for the deliberate role-policy departure).
13. **File a follow-up bead** (before this PR merges) capturing the plugin-as-caller identity design problem flagged in §4.4. Title suggestion: "Plugin-as-caller identity for `PluginHostService.QueryStreamHistory` against plugin-owned subjects." Block any future work on that path on the new bead.

No database migration required. No breaking change to any client API — outer gRPC surface preserves the opaque denial contract.

## 8. Risks & open items

- **Proto field vs. metadata.** We chose the proto field for contract visibility (§4.1). The bead's original framing suggested gRPC metadata. This spec deliberately deviates on the grounds that security fields belong on the wire contract.
- **Plugin-host caller identity (deferred).** §4.4 documents that `PluginHostService.QueryStreamHistory` will return `PERMISSION_DENIED` for plugin-owned subjects after this lands. No in-tree caller breaks (verified via cold-tier spec line 627 and current SDK survey). The follow-up bead filed per §7 step 13 owns the design for "plugin-as-caller" identity. Until that bead lands, plugins MUST NOT add code paths that read plugin-owned subjects via `holomush.query_stream_history` or its Go SDK equivalent.
- **System / admin caller.** This spec defers system/admin reads to a separate RPC or capability. A follow-up bead should be filed when the first such caller materializes (likely `holomush-l4kx` audit-backfill CLI) so the capability surface is designed rather than bolted on.
- **Role policy for `invited`.** Invited characters do NOT pass the plugin check (§5.4 documents this as a deliberate tightening). If product later decides on a "preview before joining" feature, that surfaces as a new capability; it MUST NOT be added by quietly including invited in `IsMember`.
- **Error-mapping fragility.** The §5.5 design depends on (a) plugins emitting denials via `status.Error`, (b) the router preserving gRPC status codes through `status.FromError` recognition, and (c) `mapHistoryError` running the status dispatch BEFORE the existing `errors.Is` chain. A regression in any of the three silently breaks opacity. The §6.5 case 1 integration test asserts the **top-level** oops code via `oops.AsOops(err).Code()` (NOT a chain-walking helper like `errutil.AssertErrorCode`, which would pass even on a double-wrapped denial); the assertion expects `STREAM_ACCESS_DENIED`, the same code the outer I-17 gate uses, and is the load-bearing regression guard.
- **Information-hiding vs. debuggability.** The opaque client-facing error makes debugging harder for legitimate users who hit an unexpected denial. Mitigation: plugin-internal `slog` log attributes carry the distinct `SCENE_AUDIT_*` codes, so server logs and metrics can distinguish the cases even when the client sees one collapsed error. Client-side log hints may be useful as a follow-up but are out of scope.
