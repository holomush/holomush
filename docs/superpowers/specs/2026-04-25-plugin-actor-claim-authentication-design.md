# Plugin Actor-Claim Authentication

| Field        | Value                                                                                                |
| ------------ | ---------------------------------------------------------------------------------------------------- |
| Status       | DRAFT                                                                                                |
| Date         | 2026-04-25                                                                                           |
| Bead         | `holomush-ec22.1` (parent epic: `holomush-ec22` codebase review)                                     |
| Severity     | P1 (security — host-trust gap on plugin emit boundary)                                               |
| Scope        | New manifest field + new host-issued token mechanism + universal manifest gate in plugin emit path. |

## 1. Problem

`internal/plugin/goplugin/host_service.go:51-62` (`pluginHostServiceServer.EmitEvent`) honors `pluginsdk.ActorMetadataFromIncomingContext(ctx)` and stamps the resulting `core.Actor` onto outgoing events. The two metadata headers (`x-holomush-actor-kind`, `x-holomush-actor-id`) are exported via the public SDK function `pluginsdk.WithOutgoingActorMetadata`. A binary plugin can therefore call:

```go
ctx = pluginsdk.WithOutgoingActorMetadata(ctx, pluginsdk.ActorCharacter, "01HARBITRARY00000000000000")
client.EmitEvent(ctx, ...)
```

with arbitrary character IDs — and the host will trust the claim and stamp it on the event. A malicious or buggy plugin can author events that appear to originate from any character on the server. Audit-trail integrity for actor-based forensics is undermined; downstream ABAC decisions that trust the event `Actor` field become trustable only at host-emit boundary, not at consume time.

The fallback branch (no metadata in context) correctly stamps `ActorPlugin:<pluginName>`. Only the metadata-honoring path is vulnerable.

The intended flow — host injects trusted actor metadata when calling the plugin (`internal/plugin/goplugin/host.go:540, 592`); the SDK auto-ferries it back on emit (`pkg/plugin/event_sink.go:39-40`) — is well-intentioned but unauthenticated. The plugin can substitute its own values at any point.

Lua plugins are not affected on the forgery surface: they return emits as values from `DeliverEvent` and the host stamps the actor directly from `core.ActorFromContext(ctx)` via `internal/plugin/manager.go:1164` (`actorFromContext`). There is no plugin-process trust gap on the Lua path. The forgery surface is binary-plugin-specific.

### 1.1 Discovered during design review: the actor-metadata channel is dead in production

Review of every call site of `Host.DeliverEvent` and `Host.DeliverCommand` revealed an independent correctness gap entangled with the security finding:

- **`internal/plugin/subscriber.go:104-150::deliverAsync`** passes `tctx` (no actor) into `s.host.DeliverEvent(tctx, …)` at line 112. The `core.WithActor(tctx, actorFromIncomingEvent(event))` at line 137 is computed **after** `DeliverEvent` returns and is bound only to `emitCtx` (used downstream by `EmitPluginEvent`).
- **`internal/command/dispatcher.go:285-310::dispatchToPlugin`** passes raw dispatch ctx into `d.pluginDeliverer.DeliverCommand(ctx, …)`. The `core.WithActor(ctx, ActorCharacter:exec.CharacterID())` at line 370 is computed **after** `DeliverCommand` returns.

Net: `host.go:539-540`'s check `if actor, ok := core.ActorFromContext(ctx); ok` is `ok=false` at every production call site. The host's outgoing actor-metadata injection is dead code; plugin handlers reading `pluginsdk.ActorMetadataFromIncomingContext` always see `ok=false` in production today (existing tests pass because they fabricate ctx with `core.WithActor` directly — `internal/plugin/goplugin/host_test.go:1289, 1313`).

**Implications for ec22.1:**

- The forgery the bead describes is theoretically real (an attacker could substitute headers and the host would trust them) but the harm surface is smaller than implied because the channel mostly carries no data in production.
- The new design's cascade-preservation guarantee (§3.5) is unimplementable without first activating the channel — moving the upstream actor-stamping to before the Deliver call.
- This isn't load-bearing for command handling: plugins receive character ID via the proto request body (`host.go:583-585`'s `CommandRequest.character_id`), not via actor metadata.

This design therefore expands scope to **also activate the actor-metadata channel** by stamping the upstream actor on dispatch ctx at the two Deliver call sites. Without this, the security gate (§3.4) and token mechanism (§3.3) would close a forgery surface on a channel that wasn't being used, while quietly continuing to leave the channel dead — producing a worse net state than today (loud errors on a dead channel). With it, the channel becomes meaningful AND authenticated AND gated.

## 2. Goals & non-goals

### Goals

- **G1.** Make plugin-claimed actor metadata cryptographically unforgeable on the binary-plugin gRPC `EmitEvent` boundary.
- **G2.** Add a manifest-declared opt-in (`actor_kinds_claimable`) that operators control at plugin install time, governing which actor kinds a plugin is allowed to vouch for on emitted events. Apply the gate symmetrically to **both** binary and Lua plugins (per project invariant: plugin runtimes MUST be treated identically; see §10).
- **G3.** Eliminate the architectural possibility for a plugin to claim `ActorSystem` identity. The host's system identity is host-internal; no manifest path opts into it.
- **G4.** Preserve cause-origin cascade semantics (per `internal/core/event.go:188` — "Actor represents who or what caused an event"). A plugin handling a character-emitted event and emitting a follow-up MUST still preserve the character as the actor, when manifest opt-in allows it.
- **G5.** Fail loudly on rejected claims. No silent fallback to `ActorPlugin` that hides plugin misconfiguration. Operators and plugin authors get immediate, actionable errors.
- **G6.** Document the binary/Lua runtime-symmetry invariant in `AGENTS.md` and `CLAUDE.md` so future contributors don't reintroduce asymmetric trust paths.
- **G7.** Activate the actor-metadata channel by stamping the upstream actor on dispatch ctx at `internal/plugin/subscriber.go::deliverAsync` and `internal/command/dispatcher.go::dispatchToPlugin` **before** the call to `Host.DeliverEvent` / `Host.DeliverCommand`. Without this prerequisite, G1 and G4 are unimplementable (see §1.1).

### Non-goals

- Plugin-as-character capability for non-dispatch contexts (Discord bridges, AI puppeteers). Filed as follow-up `holomush-ec22.1.1`. The design here intentionally does not preclude such a future mechanism — it would feed the same token-validation path on the host with capability-bearer-derived tokens instead of dispatch-derived tokens.
- Removing the existing `x-holomush-actor-kind` / `x-holomush-actor-id` advisory headers. They have a legitimate second purpose: plugin handlers read incoming actor identity for their own logic (see `pkg/plugin/sdk.go:195, 236` and the SDK-public `pluginsdk.ActorMetadataFromIncomingContext`). The headers stay; the host's `EmitEvent` simply stops trusting them as identity claims.
- Changing the cascade origin semantic. Events still document cause-origin (originator), not direct emitter. Direct emission is recoverable via OTel spans / event ID lineage.
- Re-architecting Lua plugin invocation. Lua's emit path is host-internal and the actor stamping is already host-authoritative; the manifest gate is the only Lua-path change.
- New ABAC plumbing. Existing ABAC consumers continue to read `Event.Actor`; this design preserves that contract.

## 3. Design

### 3.1 Architecture overview

Three coordinated layers, each fail-closed independently:

| Layer | Where | Applies to | Responsibility |
| ----- | ----- | ---------- | -------------- |
| **Upstream actor-stamping** (prerequisite, both runtimes) | `internal/plugin/subscriber.go::deliverAsync`, `internal/command/dispatcher.go::dispatchToPlugin` | All dispatches into Lua + binary plugins | Populates `core.ActorFromContext(ctx)` at the dispatch boundary so downstream code (host's metadata injection, token issuance, manifest gate) sees the host-vouched actor. |
| **Manifest gate** (universal) | `internal/plugin/event_emitter.go::Emit` | Lua + binary + future runtimes | Asserts the actor's `Kind` is listed in the plugin's `manifest.actor_kinds_claimable`. Rejects loudly otherwise. |
| **Token authentication** (binary-specific) | `internal/plugin/goplugin/host_service.go::EmitEvent` | Binary plugins only | Replaces "trust plugin's metadata claim" with "look up host-issued token in store; use the stored actor verbatim; ignore any kind/id values the plugin's metadata carries." |

The upstream-stamping layer activates the actor channel that every other layer depends on (see §1.1). The manifest gate is the policy enforcer. The token mechanism authenticates the actor input on the only runtime where forgery is possible. Lua plugins reach the manifest gate through the same `event_emitter.go::Emit` codepath, fed by host-authoritative actor context (no token needed).

### 3.2 Manifest schema change

New field on `internal/plugin/manifest.go::Manifest`:

```yaml
# Default if absent: [plugin]
actor_kinds_claimable:
  - plugin
  - character
```

**Validation rules** (enforced at manifest load):

| Rule | Behavior on violation |
| ---- | --------------------- |
| Field is optional. Default = `[plugin]` if absent. | n/a |
| MUST contain `plugin`. A plugin always needs to vouch for its own identity. | Loud error at manifest load: `MANIFEST_ACTOR_KINDS_MISSING_PLUGIN`. |
| MUST NOT contain `system`. ActorSystem is the host's identity, not a claimable plugin capability (per G3). | Loud error at manifest load: `MANIFEST_ACTOR_KIND_SYSTEM_FORBIDDEN`. |
| MUST only contain known kinds (`plugin`, `character`). | Loud error at manifest load: `MANIFEST_ACTOR_KIND_UNKNOWN`. |
| MUST be a list of strings. | Loud error at manifest load: `MANIFEST_ACTOR_KINDS_MALFORMED`. |
| Duplicates are silently deduplicated at load. | The canonical form on the loaded `Manifest` struct is uniqued; `[plugin, plugin]` becomes `[plugin]`, `[plugin, character, plugin]` becomes `[plugin, character]`. (Rationale: operator typos in YAML are common; deduping is harmless and avoids punishing operators for cosmetic mistakes.) |

Allowed shapes after validation:

- `[plugin]` (default) — the plugin can vouch for plugin-actor cascades and its own identity.
- `[plugin, character]` — also character cascades (the verb-handler / scene-event case).

**In-tree migration scope** (updated this PR to declare `[plugin, character]`):

- `plugins/core-scenes/plugin.yaml` (binary) — emits scene-lifecycle events during character-driven scene commands.
- `plugins/core-communication/plugin.yaml` (Lua) — emits `say`/`pose`/`emote`/`page`/`whisper`/`ooc`/`pemit`/`emit`/`wall` events. The first character-driven `say` after merge would loud-error without this update.
- `plugins/echo-bot/plugin.yaml` (Lua) — subscribes to `say` events and emits an echo `say` event in response. The early-return at `main.lua:18-20` filters plugin-actor `say`s to prevent loops, but character-actor `say`s pass through and emit a new `say` carrying the upstream `ActorCharacter:<player>`. Without manifest opt-in, every player `say` event would trigger `EMIT_ACTOR_KIND_NOT_CLAIMABLE` in the subscriber's `EmitPluginEvent` path.

A migration audit identified these three as the only in-tree plugins that emit during character-driven dispatches. Other in-tree plugins (`core-aliases`, `core-building`, `core-help`, `core-objects`, `setting-crossroads`, `setting-skeleton`, `test-abac-widget`) either don't emit, only emit during plugin-actor cascades (default `[plugin]` covers them), or are setting-only.

**CI assertion** (added in this PR): `task lint:plugin-manifests` (or analogous task target) loads every in-tree `plugin.yaml`, parses it, and flags any plugin where `type` is not `setting` AND `actor_kinds_claimable` does not contain `character` AND ANY of:

- `emits:` is non-empty AND (`commands:` is non-empty OR `events:` is non-empty), OR
- `events:` is non-empty (regardless of `emits:` — handler-emitted events bypass the top-level `emits:` declaration).

See acceptance criterion 8 (§7) for the full per-plugin enumeration and rationale on the false-positive surface for clause (b).

### 3.3 Token mechanism (binary-plugin path)

#### 3.3.1 Token store

New package-private struct in `internal/plugin/goplugin/`:

```go
// Package private: emit_token_store.go
type emitTokenStore struct {
    mu     sync.RWMutex
    items  map[string]emitTokenEntry  // token → entry
    now    func() time.Time           // injected for tests
    rand   io.Reader                  // crypto/rand by default; injected for tests
    ttl    time.Duration              // 5 * time.Minute (= 60 × DefaultEventTimeout)
    sweep  time.Duration              // 30 * time.Second
    stop   chan struct{}              // closed on shutdown to terminate sweeper
    stopped bool
}

type emitTokenEntry struct {
    pluginName string       // host-known plugin name (NOT plugin-claimed)
    actor      core.Actor   // actor stored at issuance, used verbatim on lookup
    expiresAt  time.Time
}
```

#### 3.3.2 Methods

```go
// Issue creates a new token for an outgoing dispatch. Caller MUST defer Revoke
// or the entry will rely on TTL expiry for cleanup.
//
// Token format: 16 bytes from crypto/rand, base64url-encoded (22 chars, no padding).
// Aligns with project crypto/rand convention per CLAUDE.md.
func (s *emitTokenStore) Issue(pluginName string, actor core.Actor) (token string, err error)

// Lookup retrieves the actor stored for a token. Returns ok=false if missing,
// expired, OR if the stored entry's pluginName does not match the caller's.
// All three failure modes are indistinguishable to callers (the security log
// records the specific reason at the call site).
//
// The pluginName parameter is defense-in-depth on top of the 128-bit token
// entropy: if a future host bug ever lets plugin A's gRPC client invoke
// plugin B's server (cross-process socket misrouting, shared-host
// restructure, etc.), the mismatch trips EMIT_TOKEN_REJECTED rather than
// allowing actor escalation. The pluginName argument is implicitly
// available at the only call site (pluginHostServiceServer.pluginName);
// the explicit parameter makes the security invariant visible at the API
// boundary.
func (s *emitTokenStore) Lookup(pluginName, token string) (actor core.Actor, ok bool)

// Revoke removes a token entry. Idempotent; safe to call after expiry or if
// the token was never issued. Always called via defer at the dispatch site.
func (s *emitTokenStore) Revoke(token string)

// Run starts the background sweeper goroutine. Terminated by Close().
func (s *emitTokenStore) Run(ctx context.Context)

// Close stops the sweeper goroutine and clears all entries.
func (s *emitTokenStore) Close() error
```

The store is owned by `internal/plugin/goplugin/Host` (one per `Host` instance). `Host.Close()` is updated to call `emitTokenStore.Close()`.

#### 3.3.3 Header

New gRPC metadata header: `x-holomush-emit-token`. Carries the token from host → plugin → host. The existing `x-holomush-actor-kind` / `x-holomush-actor-id` headers are unchanged in semantics: they remain the host's advisory channel for "who invoked you," consumed by plugin-handler code via `pluginsdk.ActorMetadataFromIncomingContext`. The host's `EmitEvent` no longer reads them as identity claims.

#### 3.3.4 Issuance flow (always-issue invariant)

At every host → plugin outgoing call (`internal/plugin/goplugin/host.go::DeliverEvent` line 540, `host.go::DeliverCommand` line 592):

```go
// Compute the actor to vouch for. Re-anchor ActorSystem to plugin identity:
// architecturally, plugins cannot speak as the host's system identity.
storedActor := core.Actor{Kind: core.ActorPlugin, ID: name}  // default
if upstream, ok := core.ActorFromContext(ctx); ok {
    switch upstream.Kind {
    case core.ActorCharacter, core.ActorPlugin:
        storedActor = upstream  // verbatim — cascade preserved
    case core.ActorSystem:
        storedActor = core.Actor{Kind: core.ActorPlugin, ID: name}  // re-anchor
    default:
        // unknown kind — keep default
    }
}

token, err := h.tokenStore.Issue(name, storedActor)
if err != nil {
    return nil, oops.In("goplugin").With("plugin", name).
        Wrap(err)
}
defer h.tokenStore.Revoke(token)

callCtx = metadata.AppendToOutgoingContext(callCtx, "x-holomush-emit-token", token)
// Existing actor-kind/-id metadata still attached for plugin-side advisory consumption.
callCtx = pluginsdk.WithOutgoingActorMetadata(callCtx, coreActorKindToSDK(storedActor.Kind), storedActor.ID)
```

Behavior matrix:

| Upstream `core.ActorFromContext(ctx)` | Stored in token | Rationale |
| ------------------------------------- | --------------- | --------- |
| `ActorCharacter:X`                    | `ActorCharacter:X` (verbatim) | Cascade preserved; gated by manifest at emit time |
| `ActorPlugin:Y`                       | `ActorPlugin:Y` (verbatim) | Cascade preserved through plugin chains |
| `ActorSystem`                         | `ActorPlugin:<currentPluginName>` (re-anchor) | Plugin can never speak as host's system identity |
| Absent                                | `ActorPlugin:<currentPluginName>` | Existing fallback behavior preserved |

#### 3.3.5 EmitEvent flow (binary path)

`pluginHostServiceServer.EmitEvent` becomes:

```go
func (s *pluginHostServiceServer) EmitEvent(ctx context.Context, req *pluginv1.PluginHostServiceEmitEventRequest) (*pluginv1.PluginHostServiceEmitEventResponse, error) {
    if s.host == nil { /* unchanged */ }
    s.host.mu.RLock()
    emitter := s.host.eventEmitter
    tokenStore := s.host.tokenStore
    s.host.mu.RUnlock()
    if emitter == nil { /* unchanged */ }

    // Read token from incoming metadata. NOTE: kind/id headers are advisory
    // only; this RPC ignores them as identity claims.
    md, _ := metadata.FromIncomingContext(ctx)
    tokens := md.Get("x-holomush-emit-token")
    if len(tokens) == 0 || tokens[0] == "" {
        return nil, oops.Code("EMIT_TOKEN_MISSING").
            With("plugin", s.pluginName).
            Errorf("plugin emitted without a host-issued dispatch token")
    }
    storedActor, ok := tokenStore.Lookup(s.pluginName, tokens[0])
    if !ok {
        // Either: token unknown to store, expired, OR stored pluginName mismatched.
        // Cross-plugin token leakage is a serious red flag — log it.
        slog.WarnContext(ctx, "EmitEvent rejected: token not valid for this plugin",
            "plugin", s.pluginName,
            "code", "EMIT_TOKEN_REJECTED",
        )
        return nil, oops.Code("EMIT_TOKEN_REJECTED").
            With("plugin", s.pluginName).
            Errorf("dispatch token is not valid for this plugin")
    }

    emitCtx := core.WithActor(ctx, storedActor)
    if err := emitter.Emit(emitCtx, s.pluginName, pluginsdk.EmitIntent{
        Subject: req.GetStream(),
        Type:    pluginsdk.EventType(req.GetEventType()),
        Payload: string(req.GetPayload()),
    }); err != nil {
        return nil, oops.With("plugin", s.pluginName).Wrap(err)
    }
    return &pluginv1.PluginHostServiceEmitEventResponse{}, nil
}
```

Key invariant: `storedActor` comes from the token store (host-authoritative), NEVER from `pluginsdk.ActorMetadataFromIncomingContext(ctx)`. The plugin's metadata claim values are discarded.

#### 3.3.6 Two-token pattern (dispatch tokens vs self tokens)

The §3.3.4 issuance flow only fires at `DeliverEvent` / `DeliverCommand`. Plugins that **serve their own gRPC handlers** (e.g., `SceneService.CreateScene`) emit events from a path that did NOT originate at a Deliver call site, so no dispatch token is in the incoming ctx. Without a second issuance path, every such emit would fail with `EMIT_TOKEN_MISSING` after this design lands — breaking the entire plugin-served-RPC surface.

Resolution: the host exposes a second RPC, `PluginHostService.RequestEmitToken`, that issues a **self-token** bound to `{ActorPlugin, pluginName}`. The SDK's `pluginHostEventSink.Emit` calls it as a fallback whenever the incoming ctx has no dispatch token.

| Token | Issued by | Bound actor | Use case |
| ---   | ---       | ---         | --- |
| **Dispatch token** | `Host.DeliverEvent` / `Host.DeliverCommand` (§3.3.4) | Stored character / system / plugin actor from upstream ctx | HandleEvent / HandleCommand round-trip emits |
| **Self token** | `pluginHostServiceServer.RequestEmitToken` | HARDCODED `{ActorPlugin, pluginName}` | Plugin-served gRPC handler emits, background goroutine emits |

G1 preservation under self-tokens:

- The `RequestEmitToken` request carries no identity fields. Adding any caller-supplied actor fields here would re-open the forgery surface; the proto comment forbids it.
- The handler binds the actor to `{ActorPlugin, s.pluginName}`. `s.pluginName` is set at server construction (mTLS-bound on real deployments). The plugin cannot forge its own name through this RPC.
- `EmitEvent`'s actor-headers-discarded contract is unchanged — the host still uses the tokenStore-bound actor regardless of what the plugin claims in metadata.
- The manifest gate in `event_emitter.go::Emit` still fires for the resulting actor kind. A plugin that doesn't list `"plugin"` in `actor_kinds_claimable` cannot self-token-emit at all.
- Cross-plugin defense unchanged: `tokenStore` keys on `(pluginName, token)`, so plugin A's self-token cannot be reused by plugin B's server.
- **Character-actor cascading still requires a real Deliver dispatch.** Self-tokens cannot promote a background goroutine into the original character's identity — a goroutine emitting after `HandleEvent` returned will publish events stamped with the plugin actor, NOT the dispatching character.

### 3.4 Manifest gate (universal — Lua + binary)

In `internal/plugin/event_emitter.go::Emit`, after the namespace check at lines 99-111 and before publish:

```go
// Manifest gate: assert the actor's kind is in the plugin's claimable list.
// Applies uniformly to Lua and binary plugins because both runtimes feed
// through this Emit codepath. The token mechanism in goplugin/host_service.go
// authenticates the actor input for binary plugins; Lua's actor input is
// host-stamped at the subscriber boundary so it's already authoritative.
if !manifest.declaresActorKindClaimable(actor.Kind) {
    return oops.Code("EMIT_ACTOR_KIND_NOT_CLAIMABLE").
        With("plugin", pluginName).
        With("subject", subjectRaw).
        With("actor_kind", actor.Kind.String()).
        With("declared_kinds", manifest.ActorKindsClaimable).
        Errorf("plugin manifest does not declare %q as a claimable actor kind", actor.Kind.String())
}
```

`Manifest.declaresActorKindClaimable(kind)` is a new helper that:

- Returns `true` if `kind` is `ActorPlugin` and `manifest.ActorKindsClaimable` contains `"plugin"` (always true after validation).
- Returns `true` if `kind` is `ActorCharacter` and `manifest.ActorKindsClaimable` contains `"character"`.
- Returns `false` for any other kind (notably `ActorSystem`, which can never be claimed because validation rejects `system` from the list).

### 3.5 Cascade preservation (clarifying note for reviewers)

The cascade semantic is:

1. Plugin A emits an event with actor `ActorCharacter:X` (because A was running during a character-driven dispatch).
2. EventBus delivers to plugin B's subscription. `internal/plugin/subscriber.go:137` sets ctx-actor = `actorFromIncomingEvent(event)` = `ActorCharacter:X`.
3. Host calls plugin B's `HandleEvent` with `ActorCharacter:X` in ctx. Host issues a token storing `ActorCharacter:X`.
4. Plugin B emits a follow-up. SDK ferries token. Host's EmitEvent looks up token, retrieves `ActorCharacter:X`. Manifest gate check: B's manifest must declare `[plugin, character]`. If yes, follow-up event stamped `ActorCharacter:X`. If no, loud error.

Cascade is preserved through the chain only as long as each plugin in the chain has the appropriate manifest opt-in. Plugins without the opt-in fail loudly when they encounter a cascade they can't vouch for. There is no silent attribution downgrade.

For plugin-actor cascades (chain through ActorPlugin identities), the default `[plugin]` manifest suffices — both ends of the chain require only the default opt-in.

For ActorSystem cascades, the host re-anchors at issuance (per §3.3.4); plugins never see a system actor in the token store, so they always emit as their own ActorPlugin identity during system dispatches. Documented operator behavior, not a hidden fallback.

### 3.6 Upstream actor-stamping (G7 prerequisite)

Two production call sites drop the upstream actor before reaching `Host.DeliverEvent` / `Host.DeliverCommand`. Both already compute the right value; both bind it on a *post-Deliver* ctx for the downstream emit path. The fix is to stamp the actor *before* the Deliver call so the same value flows through to the host's outgoing-metadata injection AND to the token-store issuance path AND remains available for the post-Deliver emit (no behavior loss).

#### 3.6.1 `internal/plugin/subscriber.go::deliverAsync`

Current shape (lines 104-150, simplified):

```go
emits, err := s.host.DeliverEvent(tctx, pluginName, event)  // tctx has no actor
// ... error handling ...
emitCtx := core.WithActor(tctx, actorFromIncomingEvent(event))  // ← line 137: too late
for _, emit := range emits {
    s.emitter.EmitPluginEvent(emitCtx, pluginName, emit)
}
```

New shape:

```go
dispatchCtx := core.WithActor(tctx, actorFromIncomingEvent(event))  // stamped before Deliver
emits, err := s.host.DeliverEvent(dispatchCtx, pluginName, event)
// ... error handling ...
for _, emit := range emits {
    s.emitter.EmitPluginEvent(dispatchCtx, pluginName, emit)  // same ctx flows through
}
```

`actorFromIncomingEvent` (helper at `subscriber.go:153-166`) is unchanged. The ctx variable is renamed `dispatchCtx` to make the lifecycle obvious to readers.

#### 3.6.2 `internal/command/dispatcher.go::dispatchToPlugin`

Current shape (lines 285-381, simplified):

```go
resp, err := d.pluginDeliverer.DeliverCommand(ctx, pluginName, cmdReq)  // ctx has no actor
// ... error handling ...
emitCtx := core.WithActor(ctx, core.Actor{
    Kind: core.ActorCharacter,
    ID:   exec.CharacterID().String(),
})  // ← line 370: too late
for _, evt := range resp.Events {
    d.pluginDeliverer.EmitPluginEvent(emitCtx, entry.PluginName(), evt)
}
```

New shape:

```go
dispatchCtx := core.WithActor(ctx, core.Actor{
    Kind: core.ActorCharacter,
    ID:   exec.CharacterID().String(),
})  // stamped before Deliver
resp, err := d.pluginDeliverer.DeliverCommand(dispatchCtx, pluginName, cmdReq)
// ... error handling ...
for _, evt := range resp.Events {
    d.pluginDeliverer.EmitPluginEvent(dispatchCtx, entry.PluginName(), evt)  // same ctx
}
```

`exec.CharacterID()` is the same value the post-Deliver line uses today. No semantic change beyond the move.

#### 3.6.3 Why this is safe (compatibility audit)

The change activates the actor-metadata channel that has been dead in production. Compatibility audit:

- **No in-tree plugin reads `pluginsdk.ActorMetadataFromIncomingContext`.** Verified by `rg -n 'ActorMetadataFromIncomingContext|ActorMetadataFromOutgoingContext' plugins/` — zero hits. The function IS read by `pkg/plugin/`'s SDK internals (the auto-ferry path), the SDK tests, the existing `internal/plugin/goplugin/host_test.go` cases (which fabricate ctx with `core.WithActor` directly), and the host's `EmitEvent` boundary that this design is replacing — none of those readers represent in-tree plugin user code, and the existing host tests remain valid because the upstream stamping produces the same observable state they fabricate.
- **Plugin handlers receive character context via the proto request body** (`host.go:583-585`'s `CommandRequest.character_id` and analogous `HandleEventRequest.event.actor_*` fields). The new actor-metadata channel is a parallel signal for plugins that opt into reading it; populating it does not change anything for plugins that don't.
- **Lua plugins are unaffected by the host's outgoing-metadata injection.** Lua dispatch goes through `internal/plugin/lua/host.go`, which doesn't use gRPC metadata. The upstream-stamping change benefits Lua only insofar as `event_emitter.go::Emit` (which Lua emits flow through) now receives an actor in ctx; this is required for the universal manifest gate (§3.4) to function.
- **The two `core.WithActor` calls are pure ctx mutations.** They allocate a new ctx node; no external side effects. Moving them earlier in their respective functions is operationally equivalent to "the ctx-actor binding starts earlier"; nothing observable changes for code that doesn't read ctx-actor.

#### 3.6.4 Out-of-tree plugin authors

Out-of-tree plugin authors who happen to read `pluginsdk.ActorMetadataFromIncomingContext` and check `ok` will see `ok=true` where they previously saw `ok=false`. This is a behavior change but a fundamentally correct one — the channel was always meant to carry data, and now it does. Release notes include a callout: "Plugin handlers reading actor metadata via `pluginsdk.ActorMetadataFromIncomingContext` will now receive populated values for character-driven dispatches and EventBus cascades; previously this channel was effectively empty in production."

### 3.7 Out-of-scope future work (filed as follow-ups)

- **`holomush-ec22.1.1`** — Plugin-as-character capability grant for non-dispatch contexts (Discord, AI puppeteers, external bridges). New `HostService.GrantActorCapability(characterID, externalProof)` RPC issuing short-lived bearers. Feeds the same token-validation path on the host. Expands G2 manifest schema with new opt-in (e.g., `actor_capabilities: [discord_bridge]`).

## 4. Documentation surface (G6)

This PR updates documentation in three places to satisfy the user-flagged "should be documented many places" requirement for the binary/Lua plugin runtime-symmetry invariant.

1. **`AGENTS.md`** — add a new "Plugin Runtime Symmetry" subsection under the existing Plugin System block, documenting that any host-side trust check, validation, or feature MUST apply to both binary and Lua plugins, with this PR's manifest gate cited as the pattern reference.
2. **`CLAUDE.md`** — same addition, kept in sync with `AGENTS.md`.
3. **`site/docs/extending/`** — operator-facing docs for plugin authors describing the new `actor_kinds_claimable` manifest field and migration guidance.

## 5. Testing

### 5.1 Unit — `emitTokenStore` (`internal/plugin/goplugin/emit_token_store_test.go`, new)

Table-driven tests covering:

- **Issue + Lookup happy path:** issue a token for `(plugin-A, ActorCharacter:X)`, lookup with same plugin name returns the actor.
- **Lookup wrong pluginName fails:** issue for `plugin-A`, lookup as `plugin-B` returns `ok=false`.
- **Lookup unknown token fails:** lookup with a never-issued token returns `ok=false`.
- **Lookup expired entry fails:** issue + advance injected clock past TTL + lookup returns `ok=false`.
- **Revoke removes entry:** issue + revoke + lookup returns `ok=false`.
- **Revoke is idempotent:** revoke + revoke + lookup, no panic.
- **Concurrent issue/lookup safety:** N goroutines concurrently issuing + looking up + revoking; no race detector violations.
- **Token format:** issued tokens are 22 chars, base64url charset, decode to 16 bytes.
- **Token uniqueness:** issuing 10000 tokens produces 10000 distinct values (probabilistic; `crypto/rand` collision is astronomically improbable).
- **Sweeper removes expired:** issue + advance clock + trigger sweep + Lookup returns `ok=false` even before explicit Revoke.
- **Close stops sweeper:** Close + assert sweeper goroutine exits within bounded time (use `goleak` or equivalent).

### 5.2 Unit — manifest validation (`internal/plugin/manifest_test.go`, extended)

Test cases:

- `actor_kinds_claimable` absent → loaded with default `["plugin"]`.
- `actor_kinds_claimable: []` → rejected (`MANIFEST_ACTOR_KINDS_MISSING_PLUGIN`).
- `actor_kinds_claimable: ["character"]` (no `plugin`) → rejected (same code).
- `actor_kinds_claimable: ["plugin", "system"]` → rejected (`MANIFEST_ACTOR_KIND_SYSTEM_FORBIDDEN`).
- `actor_kinds_claimable: ["plugin", "frobnicate"]` → rejected (`MANIFEST_ACTOR_KIND_UNKNOWN`).
- `actor_kinds_claimable: ["plugin"]` → loaded successfully.
- `actor_kinds_claimable: ["plugin", "character"]` → loaded successfully.
- `actor_kinds_claimable: "plugin"` (string instead of list) → rejected (`MANIFEST_ACTOR_KINDS_MALFORMED`).

### 5.3 Unit — `event_emitter.go::Emit` (`internal/plugin/event_emitter_test.go`, extended)

- Manifest declares `[plugin]`, ctx has `ActorCharacter:X` → loud error `EMIT_ACTOR_KIND_NOT_CLAIMABLE`.
- Manifest declares `[plugin, character]`, ctx has `ActorCharacter:X` → emit succeeds, event stamped `ActorCharacter:X`.
- Manifest declares `[plugin]`, ctx has `ActorPlugin:other-plugin` (cascade) → emit succeeds, event stamped `ActorPlugin:other-plugin`.
- Manifest declares `[plugin]`, ctx has `ActorPlugin:self` → emit succeeds, event stamped `ActorPlugin:self`.

### 5.4 Unit — host_service.EmitEvent (`internal/plugin/goplugin/host_service_test.go`, extended)

- Valid token + manifest declares `[plugin, character]` + token stores `ActorCharacter:X` → event published with `ActorCharacter:X`.
- Missing token → loud error `EMIT_TOKEN_MISSING`.
- Unknown token → loud error `EMIT_TOKEN_REJECTED`.
- Token issued for plugin-A but EmitEvent invoked on plugin-B's server → loud error `EMIT_TOKEN_REJECTED`.
- Plugin's metadata claim contains forged `ActorCharacter:Y` but token's stored entry is `ActorCharacter:X` → event published with `ActorCharacter:X` (claim ignored).
- Plugin's metadata claim contains `ActorSystem` but token's stored entry is `ActorPlugin:<self>` (host re-anchored) → event published with `ActorPlugin:<self>`.
- Token expired between issuance and EmitEvent → loud error `EMIT_TOKEN_REJECTED`.

### 5.5 Unit — `Host.DeliverEvent` / `DeliverCommand` token issuance (`internal/plugin/goplugin/host_test.go`, extended)

- DeliverEvent with `ActorCharacter:X` in ctx → outgoing metadata contains `x-holomush-emit-token` header; token store has matching entry; entry is removed after the call returns (via defer).
- DeliverEvent with `ActorSystem` in ctx → token store entry has `ActorPlugin:<currentPluginName>` (re-anchored).
- DeliverEvent with no actor in ctx → token store entry has `ActorPlugin:<currentPluginName>`.
- DeliverEvent's underlying gRPC call returns a process-crash error (test simulates plugin process kill mid-RPC) → defer still runs, token revoked. NOTE: hashicorp/go-plugin is expected to convert plugin-process panics into RPC errors at the wire boundary so the host process does not panic; the test should verify this empirically rather than relying on documented behavior (verify against the version pinned in `go.mod` during plan phase). The defer runs because the host's outgoing call returns normally with an error.
- DeliverEvent's ctx is canceled mid-call (host-side cancellation) → defer runs, token revoked.
- (Future-proofing assertion) `Host.DeliverEvent` does NOT have a `recover()` wrapper that would swallow a host-side panic and skip the defer. Verifiable via a small `go/ast` test in `host_test.go` that walks `DeliverEvent`'s function body and asserts no `*ast.CallExpr` with `Fun=Ident{Name:"recover"}` is present, OR a runtime panic-propagation test using a stub plugin handler that intentionally panics. Either guards against a future refactor that introduces a swallowing recover.

### 5.6 Integration — full forgery negative test (`test/integration/plugin_e2e/`, new file)

Real binary plugin loaded into the host. Five scenarios:

1. **Honest dispatch:** plugin invoked via character-driven command. Plugin emits via `EventSink.Emit`. Assert event is published with `ActorCharacter` matching the dispatching character.

2. **Forgery override (load-bearing G1 verification):** plugin's HandleEvent is modified (test-only build) to substitute `ActorCharacter:01HFORGED00000000000000000` into the outgoing `x-holomush-actor-kind` / `x-holomush-actor-id` headers via `pluginsdk.WithOutgoingActorMetadata` before invoking the host's `EmitEvent`. The plugin DOES still ferry the valid host-issued token (which carries the host-stored actor for the legitimate dispatching character `01HCORRECT...`). Assert: the published event's `Actor.ID` is `01HCORRECT...` (the host-stored value), NOT `01HFORGED...`. The plugin's metadata claim is silently overridden by the token-store lookup; the wire-level forgery succeeds at attaching false headers but the event itself is host-authoritative.

3. **Token fabrication:** plugin's HandleEvent is modified to substitute a fabricated random token (22 random base64url chars not issued by the host) into the `x-holomush-emit-token` header. Assert: emit fails with `EMIT_TOKEN_REJECTED`. The fabricated token has no entry in the store.

4. **Cross-plugin token leak:** plugin A captures its token `T_A` and tries to use it from plugin B's process (test simulates by injecting `T_A` into plugin B's outgoing metadata). Assert: emit fails with `EMIT_TOKEN_REJECTED` because lookup compares against the calling plugin's server identity (`pluginName`).

5. **Out-of-dispatch emit:** plugin attempts to emit from a background goroutine that has no token in ctx (no host dispatch is in flight). Assert: emit fails with `EMIT_TOKEN_MISSING`.

### 5.7 Integration — Lua-plugin manifest gate (`test/integration/plugin_e2e/`, extended)

Real Lua plugin handling a character-driven event:

1. **Manifest declares `[plugin, character]`:** Lua plugin returns emits → published with `ActorCharacter` per cascade.
2. **Manifest declares `[plugin]` only:** Lua plugin returns emits → loud error `EMIT_ACTOR_KIND_NOT_CLAIMABLE` surfaces in the subscriber's error path.

### 5.8 Unit — upstream actor-stamping at dispatch boundaries (G7)

These tests close the §5.5 unit-test gap the design reviewer flagged: §5.5 asserts host-side behavior given an actor in ctx, but production paths today don't put one there. These tests assert the actor IS in ctx at the dispatch boundary post-fix.

#### 5.8.1 Subscriber boundary (`internal/plugin/subscriber_test.go`, new)

- Subscriber receives an event with `ActorKind=Character, ActorID="01HX..."`. Inject a `mock pluginDeliverer` that captures the ctx passed to `DeliverEvent`. Assert: `core.ActorFromContext(capturedCtx)` returns `(ActorCharacter:01HX..., true)`.
- Same shape for `ActorKind=System`. Assert: ctx-actor is `ActorSystem` (the re-anchor happens later, at goplugin/host.go:540, not here).
- Same shape for `ActorKind=Plugin, ActorID="other-plugin"`. Assert: ctx-actor is `ActorPlugin:other-plugin`.

#### 5.8.2 Dispatcher boundary (`internal/command/dispatcher_test.go`, new)

- Dispatcher invokes a plugin command. Inject a `mock pluginDeliverer` that captures the ctx passed to `DeliverCommand`. Assert: `core.ActorFromContext(capturedCtx)` returns `(ActorCharacter:exec.CharacterID(), true)`.
- Assert: the post-Deliver `EmitPluginEvent` calls receive the same ctx (no behavior loss for the existing emit path).

### 5.9 Coverage targets

- `internal/plugin/goplugin/`: per-package coverage above 80%.
- `internal/plugin/`: per-package coverage above 80%.
- `emitTokenStore`: 100% line coverage (small, security-load-bearing).

## 6. Risks

- **Token-store memory growth on hung dispatches.** A pathological scenario where a plugin call never returns (deadlock, partition) leaves the token entry in the store until TTL expiry. Bounded by TTL × concurrent-call-count. With TTL=5min and a reasonable concurrent-call ceiling, this is bounded MB-class memory; acceptable.
- **Sweeper goroutine lifecycle.** If `Host.Close()` doesn't stop the sweeper, tests will leak goroutines. Mitigation: explicit `Close()` test using `goleak`.
- **Token-issuance failure path.** `crypto/rand.Read` failing is exceptionally rare but not impossible. The host returns a `oops.Code("EMIT_TOKEN_ISSUE_FAILED")` and aborts the dispatch. This means a failed `crypto/rand` blocks ALL new dispatches until resolved — fail-closed by design (don't silently dispatch without authentication).
- **Migration breakage for out-of-tree plugins.** Any out-of-tree plugin that emits during a character-driven dispatch without declaring `[plugin, character]` will get loud errors on first dispatch after upgrade. Release notes call this out as a breaking change with a one-line manifest fix. Pre-1.0; this is the right time for the break.
- **In-tree plugin migration coupling.** This PR's `plugins/core-scenes/plugin.yaml`, `plugins/core-communication/plugin.yaml`, AND `plugins/echo-bot/plugin.yaml` updates are same-PR migrations, not separate changes. Reviewer must verify both the host enforcement and all three plugin manifest updates land atomically. CI failure on any of them blocks the merge. The `task lint:plugin-manifests` check (acceptance criterion 8) catches future regressions.
- **Upstream actor-stamping ripple effects (G7).** Moving `core.WithActor` calls earlier in `subscriber.go::deliverAsync` and `dispatcher.go::dispatchToPlugin` activates the actor-metadata channel that has been dead in production. Compatibility audit (§3.6.3) confirms no in-tree plugin reads `pluginsdk.ActorMetadataFromIncomingContext`, but out-of-tree plugins might. Release notes call this out as a behavior change; §5.8 tests pin the new state.
- **Cascade-attribution semantic preservation (G4).** Reviewers might confuse "Actor = cause-origin" with "Actor = direct emitter." The spec calls this out explicitly; tests in §5.6 case 1 assert cascade preservation through a multi-hop chain.
- **Documentation drift between AGENTS.md and CLAUDE.md.** They have to stay synchronized. Reviewer must verify the runtime-symmetry text is byte-equivalent in both files.

## 7. Acceptance criteria

A reviewer can verify the work is complete by running:

1. `task lint` — passes.
2. `task test` — all tests pass, including the new `emitTokenStore` and host_service tests.
3. `task test:int` — integration forgery-test (§5.6) passes; published event's `Actor.ID` is the host-stored value, not the forged claim.
4. `task pr-prep` — green (mirrors all CI: lint, format, schema, license, unit, integration, e2e). Required before push.
5. `internal/plugin/manifest.go::Manifest` has new field `ActorKindsClaimable []string` with documented validation rules.
6. `internal/plugin/goplugin/Host` carries an `*emitTokenStore`; `Host.Close()` calls `tokenStore.Close()`.
7. `pluginHostServiceServer.EmitEvent` no longer reads `pluginsdk.ActorMetadataFromIncomingContext` for identity claims (verifiable via `rg` AND the §5.6 case 2 integration test passing — the grep is necessary but not sufficient).
8. `plugins/core-scenes/plugin.yaml`, `plugins/core-communication/plugin.yaml`, AND `plugins/echo-bot/plugin.yaml` declare `actor_kinds_claimable: [plugin, character]`. The new `task lint:plugin-manifests` CI check passes against the in-tree manifest set. The check MUST flag any in-tree plugin that has at least one character-reachable entry point AND lacks `character` in `actor_kinds_claimable`. Concretely: flag plugins where `type` is not `setting` AND ANY of the following is true AND `actor_kinds_claimable` does not contain `character`:

   - (a) `emits:` is non-empty AND (`commands:` is non-empty OR `events:` is non-empty) — plugin declares emit capability AND has a character-reachable invocation path. Catches `core-scenes` and `core-communication`.
   - (b) `events:` is non-empty (regardless of `emits:`) — plugin subscribes to events, which may carry character or system actors, AND can emit from its event handler regardless of whether the manifest declares top-level `emits:`. Catches `echo-bot` (whose `plugin.yaml` declares `events: [say]` but does not declare top-level `emits:`; the emit happens in its Lua `on_event` handler).

   This two-clause formulation correctly handles the in-tree manifest set:

   | Plugin | type | emits: | commands: | events: | Heuristic flags if claim absent? |
   | --- | --- | --- | --- | --- | --- |
   | `core-scenes` | binary | `[scene]` | yes | absent | yes — clause (a) |
   | `core-communication` | lua | `[location, character]` | yes | absent | yes — clause (a) |
   | `echo-bot` | lua | absent | absent | `[say]` | yes — clause (b) |
   | `core-aliases` / `core-building` / `core-help` / `core-objects` | lua | absent | yes | absent or `[]` | no |
   | `test-abac-widget` | binary | absent | yes | absent | no |
   | `setting-*` | setting | — | — | — | no (type-exempted) |

   Clause (b) accepts a small false-positive surface (a plugin that subscribes but never emits in its handler would still need to declare the claim, OR set `actor_kinds_claimable: [plugin]` explicitly to opt out). This is an operationally-acceptable trade-off because the alternative — parsing Lua/Go handlers to determine emit-or-not — is brittle and out of scope for a manifest lint.
9. `AGENTS.md` and `CLAUDE.md` contain a new "Plugin Runtime Symmetry" subsection explicitly documenting the binary/Lua trust-equality invariant. A CI check (added as `task lint:docs-symmetry` following the existing `lint:access-migration` / `lint:test-helpers` pattern in `Taskfile.yaml`) verifies the subsection delimited by stable HTML-comment anchors `<!-- BEGIN: plugin-runtime-symmetry -->` and `<!-- END: plugin-runtime-symmetry -->` is byte-identical between the two files. Sketch: `diff <(awk '/<!-- BEGIN: plugin-runtime-symmetry -->/,/<!-- END: plugin-runtime-symmetry -->/' AGENTS.md) <(awk '/<!-- BEGIN: plugin-runtime-symmetry -->/,/<!-- END: plugin-runtime-symmetry -->/' CLAUDE.md)`. Anchored byte-equivalence avoids the drift fragility of free-form file comparison.
10. `core.ActorFromContext(ctx)` returns `(actor, true)` at the entrance to `Host.DeliverEvent` and `Host.DeliverCommand` for character-driven dispatches and EventBus cascades. Verifiable via the §5.8 unit tests.
11. `site/docs/extending/` has a section documenting `actor_kinds_claimable` for plugin authors.

## 8. Out-of-scope follow-ups (file as beads)

- **`holomush-ec22.1.1`** — Plugin-as-character capability grant for non-dispatch contexts. P3.
- (Optional, file if scope grows) **`holomush-ec22.1.2`** — Document `actor_kinds_claimable` in the existing manifest reference docs (`site/docs/extending/manifest-reference.md`-style file if present). Likely covered by acceptance criterion 11 in this PR.

## 9. Migration path

Pre-1.0 clean break. No deprecation cycle.

1. **Same-PR coordinated changes:**
   - Host enforcement (token mechanism + manifest gate + upstream actor-stamping per §3.6) lands.
   - `plugins/core-scenes/plugin.yaml` updated to declare `[plugin, character]`.
   - `plugins/core-communication/plugin.yaml` updated to declare `[plugin, character]`.
   - `plugins/echo-bot/plugin.yaml` updated to declare `[plugin, character]`.
   - `task lint:plugin-manifests` CI check added (per acceptance criterion 8).
   - Documentation updates (AGENTS.md, CLAUDE.md, site/docs).
2. **Release notes:** explicit breaking-change callout for plugin authors. Suggested wording: "If your plugin emits events during a character-driven dispatch (e.g., a verb handler that emits a follow-up event for the speaking character), your manifest now MUST declare `actor_kinds_claimable: [plugin, character]`. Plugins emitting only with their own identity (`ActorPlugin:<pluginName>`) need no change."
3. **CI integration:** the integration test in §5.6 runs in `task pr-prep`'s integration block; CI catches accidental regression to the trust-the-claim path.

## 10. Project invariant: Plugin runtime symmetry

This work codifies a project invariant that the user flagged 2026-04-25 as "part of the core design of this system, should be documented many places":

> **Binary and Lua plugins MUST be treated identically by the host.** Any host-side trust check, validation, or feature MUST apply to both. Asymmetric behavior between plugin runtimes is forbidden — it creates a privilege gradient that violates the core plugin-system design.

When designing security/auth features that touch plugins, find the **common code path** (e.g., `internal/plugin/event_emitter.go::Emit` handles both runtimes), put the gate there. Runtime-specific code (e.g., the gRPC token mechanism for binary plugins, Lua state lifecycle for Lua) is OK for runtime-specific concerns but MUST NOT differ in the policy / trust / manifest-gate dimensions.

This PR adds the invariant to `AGENTS.md` and `CLAUDE.md` so future contributors don't reintroduce asymmetric trust paths.
