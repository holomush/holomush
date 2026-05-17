# theme:social-spaces — Substrate Contract Design

## Status

**DRAFT** — pending `design-reviewer` verdict.

**Tracking bead:** [`holomush-jg9b`](https://github.com/holomush/holomush/issues) — promoted to epic on `plan-to-beads` materialization.

**Supersedes (for substrate concerns):** [`docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md`](2026-04-06-scenes-and-rp-design-v2.md) (the scenes v2 design).

**Authors:**

- Sean Brandt
- Claude (collaborator)

**Date:** 2026-05-16

---

## Overview

`theme:social-spaces` is the strategic cluster covering four uses that share substrate: **Scenes** (`holomush-5rh`), **Channels** (`holomush-0sc`), **Forums** (`holomush-djj`), and **Discord** (`holomush-aqq`). Between April and May 2026 the substrate underneath these uses underwent foundational pivots (JetStream event bus, crypto envelope, focus coordinator, plugin host RPC contract). Scenes v2 (2026-04-06) was authored just before these pivots and is now structurally stale — not in its product semantics (which carry forward), but in its substrate assumptions.

This spec is the **substrate-contract** for `theme:social-spaces`. It:

1. Names what substrate provides today, with `path:line` grounding citations.
2. Codifies the boundary between substrate (domain-free) and uses (domain-specific).
3. Introduces two SDKs — `pkg/plugin/eventkit/` and `pkg/plugin/groupkit/` — that bundle joint primitives uses can compose. Both SDKs are co-designed in this spec but ship code only after N=2 validation (the second consumer must adopt cleanly before any primitive lands as substrate code).
4. Validates scenes v2 section-by-section against current substrate, marking each section as carried-forward, revised, resolved, or deferred.
5. Establishes implementation sequencing: which substrate work this spec mandates immediately, which downstream work it unblocks, and which per-Phase brainstorms still need to fire.

This spec is **substrate-shaping**, not implementation-detail. Per-Phase brainstorms (Phase 4 streams + pose order, Phase 5 focus integration, Phase 6 publish vote, channels rework, forums design, discord bridge) settle their own use-specific concerns by binding to the substrate contract defined here.

## RFC2119 Keywords

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are used per RFC2119.

## Invariants

Numbered invariants this spec asserts. Each invariant has a named enforcement mechanism (column below) — some are mechanically tested (CI / grep / Postgres roles), others rely on human review during `code-reviewer` agent passes or per-Phase brainstorms. Process invariants are explicitly marked as such.

| # | Invariant | Section | Enforcement |
|---|-----------|---------|-------------|
| INV-S1 | Plugins MUST NOT modify or contribute to `internal/` or any code beyond their own plugin directory and approved SDK packages (`pkg/plugin/*`, generated proto). | §2 | Human `code-reviewer` agent pass on every plugin PR's file list. Future follow-up: `task lint:plugin-boundary` CI predicate |
| INV-S2 | Substrate MUST stay domain-free: no `internal/` package SHALL contain entity types, event vocabularies, or domain logic for scenes/channels/forums/discord/etc. | §2 | Human `code-reviewer`; `rg` for use-specific identifiers (scene, channel, forum, discord) under `internal/` |
| INV-S3 | Every Plugin Host RPC and SDK primitive MUST ship Go SDK + Lua hostfunc together. Asymmetric capability between runtimes is forbidden. | §1.5 | Per-primitive parity test (mandated in §3.5); `code-reviewer` checks for paired Go + Lua delivery |
| INV-S4 | New event subjects MUST use NATS dot-style `events.<game_id>.<domain>.<entity-id>[.<facet>...]`. Colon-style is legacy and translated at the EventSink boundary. | §1.1 | `rg "scene:\|channel:\|forum:"` for legacy emit sites in new code; `subjectxlate` boundary translates existing colon-style |
| INV-S5 | Every plugin declaring `crypto.emits` MUST be subject to startup-time validation: the manifest-declared emit-type set MUST equal the code-registered emit-type set, in both directions. Mismatch SHALL fail plugin startup. | §1.2 | Substrate code mandated by `jg9b.1` (capability) and `jg9b.4` (flip fail-closed): startup-time set-equality check fails plugin load on mismatch |
| INV-S6 | Per-plugin Postgres schemas MUST remain isolated: plugin code MUST NOT open SQL connections to another plugin's schema. Cross-plugin data flow MUST be via proto contracts (`requires`/`provides`). | §1.6 | Postgres role + schema isolation (substrate-mechanical: schema-level GRANT/REVOKE prevents cross-plugin reads) |
| INV-S7 | `eventkit` and `groupkit` SDK primitives MUST NOT land as substrate code until two distinct use plugins validate the primitive's shape (N=2 discipline). | §3.4 | **Process invariant.** Enforcement artifact = a `## SDK primitive validation` section in `0sc.12`'s eventual design spec, with per-primitive verdict (`adopt as-is` / `adopt with API tweak` / `reject as not-fit`). `plan-reviewer` checks for this artifact before approving any SDK-extraction plan |
| INV-S8 | `internal/access/` MUST NOT acquire group-domain primitives. Member-of-resource attribute resolution is a plugin-side helper (`groupkit/groupabac`), not a substrate concept. | §2.5 | Human `code-reviewer`; `rg "member.*group\|group.*member" internal/access/` should return zero hits for substrate-side group concepts |
| INV-S9 | The hard privacy boundary for scenes (and any future use with the same shape) MUST remain plugin-code-enforced. ABAC SHALL NOT be in the path for scene-log reads. | §4.1 | Phase 6 plugin-code test: scene log read by non-participant MUST fail before the ABAC engine is consulted (call-stack assertion or boundary-violation test) |
| INV-S10 | Forums and Discord MUST NOT consume `groupkit` primitives. Forums uses `eventkit` only. Discord defaults to no SDK; `eventkit` adoption is permitted if cross-history sync requires ABAC-filtered replay. | §4.3, §4.4 | **Process invariant.** Forums brainstorm (`djj`) and Discord brainstorm (`aqq`) MUST document SDK adoption choices in their design specs; human review of resulting plugin's import graph confirms no `groupkit` import |

---

## 1. Substrate Inventory

This section names what substrate provides today, with `path:line` cites. This is the contract uses bind to.

### 1.1 JetStream event bus

**Provides:** durable per-stream delivery, replay, backpressure, transparent JetStream→Postgres history fallback.

**Interfaces** (three narrow consumer roles, all backed by one EventBus implementation):

- `Publisher` — emit path from plugins and host. Cited at [`internal/eventbus/bus.go`](../../internal/eventbus/bus.go) and [`.claude/rules/event-interfaces.md`](../../.claude/rules/event-interfaces.md).
- `Subscriber` — used by the gRPC Subscribe handler.
- `HistoryReader` — used by the gRPC QueryHistory handler.

**Subject naming convention** (INV-S4): subjects are NATS dot-delimited:

```text
events.<game_id>.<domain>.<entity-id>[.<facet>...]
```

- `<domain>` is plugin-owned (e.g., `scene`) or host-owned (`location`, `character`, `session`).
- Legacy colon-style subjects (`scene:01ABC`) are translated at the EventSink boundary by [`internal/eventbus/subjectxlate/`](../../internal/eventbus/subjectxlate/) — new code MUST emit NATS-style.
- See [`.claude/rules/event-conventions.md`](../../.claude/rules/event-conventions.md) for the full convention.

**Identity vs ordering:**

| Concern | Owner |
|---------|-------|
| Identity / dedup key | `core.Event.ID` (ULID) — set as `Nats-Msg-Id` for JetStream dedup |
| Ordering | JetStream's per-stream `uint64` sequence — never rely on ULID lex order |

`core.Event{}` struct literals MUST use `core.NewEvent()`, which stamps a monotonic ULID via `core.NewULID()`. `Event.ID` MUST NOT be supplied manually.

**Durable audit + history fallback:**

- Host-owned subjects audit to `events_audit` (PostgreSQL).
- Plugin-owned subjects audit to plugin-declared tables (e.g., `plugin_core_scenes.scene_log`) via `PluginAuditService.AuditEvent`.
- `HistoryReader.QueryHistory` transparently falls back from JetStream (recent) to PostgreSQL (older than JetStream retention). Callers do not see the boundary.

### 1.2 Crypto envelope

**Provides:** encrypted-at-rest event payloads with KEK/DEK rotation, per-event-type sensitivity classification, AuthGuard fence, INV-50 downgrade fence.

**Manifest declaration:** Plugin's [`plugin.yaml::crypto.emits[]`](../../plugins/core-communication/plugin.yaml) declares each event type with `sensitivity` (`always`/`may`/`never` — see [`internal/plugin/crypto_manifest.go:14-21`](../../internal/plugin/crypto_manifest.go)) and a human description. Working reference: [`plugins/core-communication/plugin.yaml:272-297`](../../plugins/core-communication/plugin.yaml) classifies 8 event types.

Per-event "visible only to authorized consumers" routing emerges from the AuthGuard fence acting on encrypted payloads, NOT from a per-sensitivity classification value. A plugin classifies an event type as `always` (every emit MUST be `Sensitive=true`), `may` (caller decides per-emit via `Sensitive=true`/`false`), or `never` (every emit MUST be plaintext). The truth table is enforced by [`internal/plugin/sensitivity_fence.go:23-48`](../../internal/plugin/sensitivity_fence.go); see master crypto design at [`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`](2026-04-25-event-payload-crypto-design.md) for the full classification model.

**Storage shape:** Host-owned audit table `events_audit` carries `dek_ref` (BIGINT, nullable) + `dek_version` (INTEGER, nullable); per-plugin audit tables conform to the same shape. Canonical reference: [`internal/store/migrations/000009_create_events_audit.up.sql`](../../internal/store/migrations/000009_create_events_audit.up.sql) (with subsequent amendments). Plugin-side worked example: [`plugins/core-scenes/migrations/000005_add_scene_log_dek_columns.up.sql`](../../plugins/core-scenes/migrations/000005_add_scene_log_dek_columns.up.sql). Identity-codec rows have both NULL.

**Review gate:** Any change to `crypto.emits` declarations or to the surrounding crypto stack triggers the `crypto-reviewer` agent BEFORE `code-reviewer`. See [`CLAUDE.md`](../../CLAUDE.md) "Pre-Push Review Gates."

**Mandated additional substrate work (this spec, INV-S5):** manifest emit-type startup validation. Today's baseline at [`internal/plugin/event_emitter.go::Emit`](../../internal/plugin/event_emitter.go) (truth table verified against [`internal/plugin/sensitivity_fence.go:23-48`](../../internal/plugin/sensitivity_fence.go)): undeclared event types fall through `LookupEmitSensitivity` to `SensitivityNever`. An emit of an undeclared type with `Sensitive=false` is **silently accepted as plaintext**; only `Sensitive=true` on an undeclared (or `never`-declared) type is rejected with `EVENT_SENSITIVITY_NOT_DECLARED` (INV-6). This leaves two failure modes silent:

- **(a) declared-but-unregistered:** a `crypto.emits` entry the code never emits (dead declaration / typo).
- **(b) registered-but-undeclared:** plugin code emitting an event type the manifest never declared, with `Sensitive=false` (silently plaintext).

INV-S5's startup validator catches **both directions** by requiring set-equality between manifest-declared and code-registered emit-type sets. Implementation lands as bead `jg9b.1` (substrate capability, no-op default) + `jg9b.4` (flip fail-closed) — see §7.

### 1.3 ABAC engine

**Provides:** Cedar-style DSL policy evaluation with plugin-declared resource types, attribute resolvers, and manifest policies. Default deny.

**Plugin-declared surfaces:**

- Plugin's `plugin.yaml::resource_types[]` lists ABAC resource types it owns (e.g., `[scene]`).
- Policies in `plugin.yaml::policies[]` use those types. See [`plugins/core-scenes/plugin.yaml:60-119`](../../plugins/core-scenes/plugin.yaml) for the 12 scene policies currently shipped.

**Attribute resolution:** Plugin's `AttributeResolverService.ResolveResource` returns current attribute values from plugin DB on demand during policy eval. Auto-registered by the host via `RegisterAttributeResolver` (MUST NOT be declared in `provides` — singleton conflict).

**Default deny posture:** All operations require explicit `permit`. No fallback.

### 1.4 Focus coordinator

**Provides:** server-side per-connection focus state with `Join/Leave/LeaveByTarget/Present/Restore`. Connections subscribe to subject patterns via focus; focus restore on reconnect is automatic.

**Surface:** [`internal/grpc/focus/`](../../internal/grpc/focus/) — `coordinator.go`, `join.go`, `leave.go`, `leave_by_target.go`, `present.go`, `restore.go`. Plugin-side client: [`pkg/plugin/focus_client.go`](../../pkg/plugin/focus_client.go).

**Plugin integration:** Plugins customize admission via per-resource-type policy hooks. See [`internal/grpc/focus/scenepolicy/`](../../internal/grpc/focus/scenepolicy/) for the scene-specific admission policy. The plugin does not own focus state; the server does. The plugin influences who is admitted.

**Multi-connection visibility:** A character with multiple connections is visible everywhere at least one connection is focused. `LeaveByTarget` cleans up when membership ends.

### 1.5 Plugin Host RPCs (with Go + Lua parity)

**Structural invariant (INV-S3):** every Plugin Host RPC ships as **both** Go SDK method AND Lua hostfunc. Asymmetric capability between runtimes is forbidden. See [`.claude/rules/plugin-runtime-symmetry.md`](../../.claude/rules/plugin-runtime-symmetry.md).

**Working precedent:**

- Go SDK: [`pkg/plugin/focus_client.go`](../../pkg/plugin/focus_client.go) ships `JoinFocus`/`LeaveFocus`/`LeaveFocusByTarget`/`PresentFocus`.
- Lua hostfunc: [`internal/plugin/hostfunc/stdlib_focus.go`](../../internal/plugin/hostfunc/stdlib_focus.go) ships `holomush.join_focus`/`leave_focus`/`leave_focus_by_target`/`present_focus`/`query_stream_history`.

Both register through the same `FocusOps` + `HistoryReader` interfaces; both compose with the same substrate `FocusCoordinator`.

**Emit path:** Both runtimes pass through [`internal/plugin/event_emitter.go::Emit`](../../internal/plugin/event_emitter.go) which enforces manifest gates (`actor_kinds_claimable`, `emits`, `crypto.emits`) uniformly. The gate is **policy**, applied at the common path. Runtime-specific code (gRPC token mechanism for binary, Lua state lifecycle) is acceptable for runtime-specific concerns but MUST NOT differ in policy or trust dimensions.

**Audit RPC:** `PluginAuditService.AuditEvent` — host audit projector ack-and-skips plugin-owned subjects and dispatches to this RPC. Plugin owns audit insertion into its own schema. See [`plugins/core-scenes/audit.go`](../../plugins/core-scenes/audit.go) for the reference implementation.

### 1.6 Per-plugin storage isolation

**Provides:** isolated Postgres schema + role per plugin, embedded migrations, search_path scoping, foreign-key-free cross-plugin reads.

**Schema/role:** `plugin_<name>` schema, `holomush_plugin_<name>` role with `USAGE+CREATE` on own schema and `REVOKE ALL` on `public`. Connection string sets `search_path` so plugin code uses unqualified names.

**Migrations:** Embedded `migrations/*.up.sql` per plugin; runner is `storage.RunMigrationsFS`; tracked in plugin's own `plugin_migrations` table. No `.down.sql` support in the plugin migration runner.

**Cross-plugin reads (INV-S6):** Postgres role isolation prevents direct SQL cross-plugin reads. Cross-plugin data flow MUST go through proto contracts (`requires`/`provides`).

---

## 2. The substrate-vs-use boundary

### 2.0 Plugin boundary invariant (INV-S1)

**Plugins MUST NOT modify or contribute to `internal/` or beyond their own code-level boundaries.** A plugin's PR touches only its own plugin directory (`plugins/<name>/`) and may import from approved substrate-facing packages (`pkg/plugin/*`, generated proto). If a plugin's needs would require substrate change, that substrate change MUST be identified and handled as separate work — separate bead, separate PR, separate review gate. Bundling a substrate change inside a plugin PR is forbidden.

**Substrate domain-freeness invariant (INV-S2):** Substrate MUST stay domain-free. No `internal/` package SHALL contain entity types, event vocabularies, or domain logic for scenes/channels/forums/discord/etc. Substrate evolves independently of any specific use.

These two invariants are dual: INV-S1 keeps plugin work out of substrate; INV-S2 keeps domain knowledge out of substrate. Together they preserve the boundary on both sides.

### 2.1 What substrate provides (unmodified per use)

These are turn-key for any use that consumes them — no plugin-side variance, no per-use specialization:

| Substrate primitive | What use gets without writing code |
|---------------------|------------------------------------|
| JetStream subjects | Durable per-stream delivery, replay, history fallback, ULID identity, JetStream-seq ordering |
| Crypto envelope | Per-event-type sensitivity enforcement, KEK/DEK rotation, AuthGuard fence, INV-50 downgrade fence |
| Manifest emit-type startup validation (INV-S5, new) | Fail-fast on declared-vs-registered mismatch |
| ABAC engine | Policy evaluation, attribute resolution wiring, default-deny posture |
| Focus coordinator | Per-connection subscription state, multi-tab visibility, restore-on-reconnect |
| Plugin host RPCs (Go + Lua parity) | `Emit`, `QueryHistory`, `JoinFocus`/`LeaveFocus`/`PresentFocus`, `PluginAuditService.AuditEvent` |
| Per-plugin storage | Isolated schema + role, embedded migration runner, search_path scoping |
| Audit projection | Host ack-and-skip for plugin-owned subjects; dispatch to plugin's `AuditEvent` RPC |

**Contract guarantee:** these surfaces are stable for the lifetime of this spec. Substrate evolution that breaks any of them MUST be a deliberate spec-revising change with `crypto-reviewer`, `abac-reviewer`, and `code-reviewer` gates as applicable.

### 2.2 What each use owns

Every use that builds on substrate MUST own these per-use surfaces. The SDKs (§3) do not take them over:

| Use surface | Why use owns it (not substrate, not SDK) |
|-------------|------------------------------------------|
| Entity table | Domain object; schema lives in use's plugin Postgres schema |
| Domain logic | State machines, pose order, channel types, moderation, vote tallying |
| Event type vocabulary + crypto.emits classification | Use's data sensitivity policy |
| ABAC policy bodies | Use-specific authorization rules (Cedar DSL in plugin.yaml) |
| Plugin commands + capability declarations | Use's user-facing command surface |
| Attribute resolver implementation | Use knows how to fetch its own entity attributes |

### 2.3 What eventkit + groupkit will own (deferred — N=2 discipline)

`pkg/plugin/eventkit/` and `pkg/plugin/groupkit/` are co-designed in this spec but their code lands ONLY after the N=2 validation discipline is met (INV-S7). See §3 for primitive specifications and §3.4 for the validation discipline.

### 2.4 The split, in one rule

> **Substrate is domain-free. Uses are domain-specific. `eventkit` is a library of broadly-useful primitives that compose substrate. `groupkit` is a library of stateful-group primitives that compose substrate plus per-use state. Neither library lands code until two distinct consumers validate the primitive's shape.**

Substrate never knows about scenes/channels/forums/discord. Uses never re-implement JetStream/crypto/ABAC. SDKs live in `pkg/plugin/` (library-for-plugins, NOT substrate); plugins import explicitly.

### 2.5 Anti-patterns this rules out

| Anti-pattern | Why forbidden | Invariant violated |
|--------------|---------------|---------------------|
| Plugin PR that modifies `internal/` or `pkg/plugin/` | Bundling substrate change with plugin work bypasses substrate review | INV-S1 |
| Adding a `group` table or `groups.go` to `internal/` | Substrate must stay domain-free | INV-S2, INV-S8 |
| Plugin opening direct SQL connections to another plugin's schema | Violates per-plugin storage isolation; data flow MUST be via proto contracts | INV-S6 |
| SDK primitive that ships only in Go without Lua hostfunc | Violates plugin runtime symmetry | INV-S3 |
| Adding a primitive to eventkit or groupkit before 2 consumers validate it | Violates N=2 discipline; N=1 extraction risk | INV-S7 |
| Using groupkit for forums or discord | Wrong shape; forums is a document store, discord is a bridge | INV-S10 |
| Adding member-of-resource resolver to `internal/access/` | Substrate must stay domain-free; the helper belongs in `pkg/plugin/groupkit/groupabac` | INV-S2, INV-S8 |
| Emitting an event type not in plugin's `crypto.emits` declaration | Substrate's manifest gate rejects this at emit time; INV-S5 will catch declared/registered mismatch at startup | (gate enforcement) |

---

## 3. SDKs: `eventkit` + `groupkit`

### 3.1 Two SDKs, defined by scope

Decomposing the joint primitives by *who can use them* reveals two distinct shapes:

- **Broadly useful** (any plugin emitting ABAC-gated sensitive events): `replay`, `cryptoemit` — these compose substrate primitives without group-specific state. They live in `pkg/plugin/eventkit/`.
- **Stateful-group only** (uses with explicit member-of-entity state): `membership`, `focuswire`, `groupabac` — these add group-domain abstractions. They live in `pkg/plugin/groupkit/`.

This split is more honest than a unified "groupkit covering everything." Forums needs `replay` and `cryptoemit` (for paginated thread display and private-board posts) but does not need `membership`/`focuswire` (forum participation is incidental, not intentional; no focus semantics). With the split, forums adopts `eventkit` cleanly and does not pretend to be group-shaped.

**Adoption matrix:**

| Use | eventkit | groupkit |
|-----|----------|----------|
| Scenes | ✓ replay + cryptoemit | ✓ all 3 |
| Channels | ✓ replay + cryptoemit | ✓ all 3 |
| Forums | ✓ replay + cryptoemit | ✗ (INV-S10) |
| Discord | default ✗ / conditional ✓ `replay` if cross-history sync requires ABAC-filtered replay (per INV-S10 + §4.4) | ✗ (INV-S10) |
| `core-communication` (existing) | could adopt cryptoemit | ✗ |

Matrix legend: `✓` = expected adoption; `✗` = forbidden by an invariant (cite shown); `default ✗ / conditional ✓` = permitted only when the named condition holds, decided by the use's design brainstorm.

### 3.2 `eventkit` — broadly useful primitives

#### 3.2.1 `replay` — ABAC-filtered history fan-out

**Use cases:**

- Scenes Phase 4 read path (`scene log` command, replay-on-join for IC stream).
- Channels' join-time history catch-up.
- Forums' paginated thread display.

**This is the hard-privacy-boundary primitive (INV-S9 for scenes specifically):** wrong implementation here leaks scene content to non-members.

**Go SDK shape (illustrative — exact signatures fixed during materialization):**

```go
package eventkit // pkg/plugin/eventkit/replay.go

type ReplayRequest struct {
    Subject           string         // JetStream subject pattern
    GroupID           string         // Membership-check target (optional; empty = no membership gate)
    MemberID          string         // Subject requesting replay
    Cursor            *Cursor        // Optional resume point
    SensitivityFilter []Sensitivity  // Which sensitivities the requester may see
}

type ReplayStream interface {
    Next(ctx context.Context) (core.Event, bool, error)
    Close() error
}

type ReplayDeps struct {
    History    eventbus.HistoryReader
    ABAC       access.Engine
    Membership MembershipStore // From groupkit; empty GroupID skips this check
}

func Replay(ctx context.Context, deps ReplayDeps, req ReplayRequest) (ReplayStream, error)
```

**Lua hostfunc:** `holomush.eventkit_replay(subject, group_id, member_id, cursor, sensitivity_filter)` returns an iterator userdata. Lua plugins call `:next()` in a loop.

**Hard privacy enforcement:** `Replay` MUST:

1. If `GroupID != ""`, verify `MemberID` is a member of `GroupID` via `Membership.IsMember`. If false → empty stream + audit log entry. No fallback.
2. For each event from `HistoryReader.QueryHistory`, filter against `SensitivityFilter` — drop events the member may not see.
3. NEVER return events outside the filter.

**Composes:** `eventbus.HistoryReader` (§1.1), `access.Engine` (§1.3), optionally `MembershipStore` (groupkit §3.3.1). Does NOT touch `internal/`.

#### 3.2.2 `cryptoemit` — call-site sensitivity assertion

**Use case:** plugin emits a `scene_ic` event; before substrate sees the payload, `cryptoemit` confirms the plugin's declared sensitivity (in `crypto.emits`) matches the sensitivity the call site believes it is emitting. Belt for the suspenders of the substrate-side manifest gate.

**Relationship to INV-S5 (substrate-side manifest validation):**

- INV-S5 substrate validation checks **manifest declared set == plugin registered set** at startup.
- `cryptoemit` checks **manifest declared sensitivity == call-site asserted sensitivity** at emit time.

The two layers together catch (1) typo / dead-entry / forgotten-entry in manifest at startup, (2) classification drift between manifest and call site at runtime.

**Go SDK shape:**

```go
package eventkit // pkg/plugin/eventkit/cryptoemit.go

type SensitiveEmitter interface {
    Emit(ctx context.Context, eventType string, payload []byte, declaredSensitivity Sensitivity) error
}

func NewSensitiveEmitter(client plugin.EmitClient, manifestPath string) (SensitiveEmitter, error)
```

**Lua hostfunc:** `holomush.eventkit_emit_sensitive(event_type, payload, declared_sensitivity)`. Same validation.

**Composes:** existing emit boundary in `internal/plugin/event_emitter.go::Emit` (substrate side) + manifest parser. Adds a call-site assertion layer; does NOT bypass or replace the substrate gate.

### 3.3 `groupkit` — stateful-group primitives

#### 3.3.1 `membership` — typed wrapper over plugin's own participants table

**Use cases:** scenes' `scene_participants` table operations, channels' `channel_members` table operations. **Plugin owns the table; groupkit owns the operations.**

**Go SDK shape:**

```go
package groupkit // pkg/plugin/groupkit/membership.go

type MembershipStore interface {
    Add(ctx context.Context, groupID, memberID, role string) error
    Remove(ctx context.Context, groupID, memberID string) error
    IsMember(ctx context.Context, groupID, memberID string) (bool, error)
    GetRole(ctx context.Context, groupID, memberID string) (string, error)
    ListMembers(ctx context.Context, groupID string) ([]Member, error)
    ListGroupsForMember(ctx context.Context, memberID string) ([]string, error)
    CountMembers(ctx context.Context, groupID string) (int, error)
}

type Member struct {
    GroupID  string
    MemberID string
    Role     string
    JoinedAt time.Time
}

func NewMembership(pool *pgxpool.Pool, cfg TableConfig) MembershipStore
```

`TableConfig` carries table name + column names (allowing scenes' `scene_id`/`character_id` and channels' `channel_id`/`character_id` to share operations).

**Lua hostfunc:** `holomush.groupkit_membership_*` — `add`, `remove`, `is_member`, `get_role`, `list_members`, `list_groups_for_member`, `count_members`. Each takes the same args + plugin-provided table identifier.

**Storage contract:** plugin's migration creates a table matching the signature `(<group_id> TEXT, <member_id> TEXT, role TEXT, joined_at TIMESTAMPTZ DEFAULT NOW(), PRIMARY KEY(<group_id>, <member_id>))`. groupkit does NOT ship migration SQL — the plugin author writes their own.

**What groupkit owns:** SQL operations, error wrapping (`oops` codes), tracing spans, metric call points.

**What plugin owns:** schema migration, table name choice, role vocabulary (scenes: `owner/member/invited`; channels: TBD by `0sc.12` brainstorm).

#### 3.3.2 `focuswire` — group-scoped focus subscription wiring

**Use case:** when a member joins a scene, auto-focus their terminal connection on the scene's IC stream. When they leave, drop the subscription. The bare `JoinFocus`/`LeaveFocus` API requires the plugin to track group-to-subscription mappings; `focuswire` does it.

**Go SDK shape:**

```go
package groupkit // pkg/plugin/groupkit/focuswire.go

type FocusWire interface {
    JoinGroupFocus(ctx context.Context, sessionID, groupID, subjectPattern string) error
    LeaveGroupFocus(ctx context.Context, sessionID, groupID string) error
    LeaveAllForMember(ctx context.Context, memberID, groupID string) error
    PresentGroupFocus(ctx context.Context, sessionID, groupID string) error
}

func NewFocusWire(fc *plugin.FocusClient) FocusWire
```

**Lua hostfunc:** `holomush.groupkit_focus_join`, `holomush.groupkit_focus_leave`, `holomush.groupkit_focus_leave_all`, `holomush.groupkit_focus_present`.

**Composes:** `pkg/plugin/focus_client.go` (substrate §1.4). Pure library over substrate; adds group-identifier tracking for clean `LeaveAllForMember` on membership removal.

#### 3.3.3 `groupabac` — member-of-resource attribute resolver fragment

**Use case:** scenes' `read-scene-as-participant` policy uses `principal.id in resource.scene.participants`. Channels will need the same. Currently every group-shaped plugin writes this inline in its `AttributeResolverService.ResolveResource` implementation.

**Approach (per INV-S8):** groupkit ships a Go function (and Lua hostfunc) that helps plugins build their `AttributeResolverService` response. It does NOT extend `internal/access/` with group semantics.

**Go SDK shape:**

```go
package groupkit // pkg/plugin/groupkit/abac.go

func PopulateParticipants(
    ctx context.Context,
    store MembershipStore,
    groupID string,
    attrs map[string]access.AttributeValue,
) error
```

**Lua hostfunc:** `holomush.groupkit_populate_participants(group_id, attrs_table)` mutates an attrs table in place.

**Composes:** `MembershipStore` (groupkit §3.3.1) + plugin's own `AttributeResolverService.ResolveResource` implementation.

### 3.4 N=2 validation discipline (INV-S7)

**Rule:** No primitive in `eventkit` or `groupkit` lands as substrate code until two distinct use plugins concretely consume the primitive cleanly.

**Why:** N=1 extraction risk. With only scenes as consumer, primitive APIs accidentally encode scenes-specific assumptions. Waiting for `0sc.12` channels rework to also adopt forces the API shape through two different domains.

**Application:**

- This spec NAMES the primitives (§3.2 and §3.3) so channels-rework brainstorm has a concrete contract to react to.
- Phase 4 implementation (`5rh.13`) lands scenes-bespoke for these capabilities (its own `replay`-like logic, its own membership ops, its own focus wiring).
- Channels-rework brainstorm (`0sc.12`) reviews each primitive: **"adopt as-is"** / **"adopt with API tweak"** / **"reject as not-fit, reasoning: ..."**.
- After channels-rework validates a primitive (N=2 met for that primitive), a substrate bead files to extract it into `pkg/plugin/eventkit/<primitive>/` or `pkg/plugin/groupkit/<primitive>/` with Lua hostfunc parity.
- Scenes-bespoke code refactors to consume the new SDK primitive (retroactive consolidation; cleanup beads under `5rh`).

**No SDK code lands as part of this spec's materialization.** Beads `jg9b.1`-`jg9b.7` are exclusively the substrate-contract output: the mandated INV-S5 capability + adoption + hygiene. SDK extraction is downstream of `0sc.12`.

### 3.5 Go + Lua parity is structural (INV-S3)

Every primitive ships:

- Go SDK at `pkg/plugin/<sdk>/<primitive>.go` + tests.
- Lua hostfunc at `internal/plugin/hostfunc/stdlib_<sdk>_<primitive>.go` + tests.
- Parity test that exercises both runtimes through the same scenario.

This is non-negotiable. A primitive that ships only one runtime creates a privilege gradient — the runtime that gets the primitive can do things the other cannot. See [`.claude/rules/plugin-runtime-symmetry.md`](../../.claude/rules/plugin-runtime-symmetry.md) for the broader invariant and worked examples.

The reference precedent is the Focus stack: `pkg/plugin/focus_client.go` + `internal/plugin/hostfunc/stdlib_focus.go` ship the same operations to both runtimes. SDK primitives MUST follow this template.

### 3.6 What's NOT in either SDK

To be explicit:

| Tempting-but-not | Where it lives instead |
|------------------|------------------------|
| A `group` entity type | Each plugin's own entity table |
| State machine helpers | Plugin domain logic |
| Vote / consensus primitives | Phase 6 brainstorm decides scenes-side |
| Thread / post / forum primitives | Forums brainstorm (out of theme scope) |
| Discord bridge state | Discord brainstorm |
| Cross-plugin event subscription helpers | Substrate (`requires`/`provides` proto contracts) |
| Moderation primitives (mute, ban, ops) | Channels brainstorm — shape differs from scenes |
| `internal/access/` extensions | INV-S8 prohibits |
| `internal/eventbus/` extensions | INV-S2 prohibits (substrate-domain-freeness) |

---

## 4. Use specializations

For each use in `theme:social-spaces`, this section names what it builds ON TOP of substrate (and, post-N=2, SDKs).

### 4.1 Scenes (`holomush-5rh`)

**Substrate consumed:** all of §1.

**Use-owned surfaces:**

- Scene entity (`scenes` table, plugin schema `plugin_core_scenes`).
- State machine: `active/paused/ended/archived` (gated transitions, enforced at service + store layers). See [`plugins/core-scenes/lifecycle.go`](../../plugins/core-scenes/lifecycle.go).
- Pose order: 4 modes (`strict/3pr/5pr/free`), derived from IC event stream, computed in `plugins/core-scenes/poseorder.go` (to be added Phase 4).
- Publish vote machinery (Phase 6): unanimous consent, async voting, snapshot-render to publication artifact (renamed from `scene_log`; see §6).
- Hard privacy boundary (INV-S9, carried forward from v2 §5.5): plugin-code-enforced, no ABAC in path, no admin bypass.
- ABAC policies: currently 12 in manifest (see [`plugins/core-scenes/plugin.yaml:60-119`](../../plugins/core-scenes/plugin.yaml)); Phase 4+ adds policies for IC/OOC emit, pose-order admin, log read.
- `scene_ops_events` table: plugin-internal append-only ops journal, separate from JetStream content stream. Phase 3 introduced this distinction (ops events vs content events) — load-bearing for Phase 4 (pose order derives from content stream, not ops events).

**Groupkit primitives consumed (post-N=2):**

- `membership` → `scene_participants` operations
- `replay` → `scene log` command + Phase 5 replay-on-join for IC stream
- `focuswire` → auto-focus on `scene join`, leave on `scene leave`/kick
- `groupabac` → eliminates inline `principal.id in resource.scene.participants` in policies
- `cryptoemit` → call-site assertion for `scene_ic`/`scene_ooc` emits

**Phases unblocked by this spec:**

- `5rh.13` Phase 4 (event streams + pose order) — immediate frontier
- `5rh.14` Phase 5 (focus model + multi-connection visibility)
- `5rh.15` Phase 6 (logs + vote + hard privacy boundary)
- `5rh.16`–`5rh.19` Phases 7-10 (templates, scene board, web client, polish)

**Phase 4 brainstorm responsibilities (NOT decided here):**

- Crypto sensitivity classification for `scene_ic`, `scene_ooc`, `scene_join`, `scene_leave`, etc. (each entry will be added to `crypto.emits` in `core-scenes/plugin.yaml`).
- `GetPoseOrder` RPC contract shape.
- IC vs ops event vocabulary split (which kinds go to JetStream content stream vs `scene_ops_events`).
- ABAC policy additions for emit + pose-order admin.

### 4.2 Channels (`holomush-0sc`)

**Substrate consumed:** all of §1.

**Use-owned surfaces:**

- Channel entity (`channels` table, plugin schema — likely `plugin_core_channels`; final name decided by `0sc.12` brainstorm).
- Channel types: `public/private/admin` (encoded on entity, not state machine).
- Moderation primitives: mute/ban/ops vocabulary + state (channels-specific; NOT in groupkit).
- ABAC policies for join/send/moderate/listen by type.
- History semantics: soft replay on join (replay cursor = subscribe-time or member-from-time).
- Auto-join seeded channels on session creation (`0sc.10`).

**Groupkit + eventkit primitives consumed (post-N=2; channels IS the second consumer):**

- `eventkit/replay` → join-time history catch-up
- `eventkit/cryptoemit` → call-site assertion for `channel_say` emits
- `groupkit/membership` → `channel_members` operations
- `groupkit/focuswire` → channel listen subscription wiring
- `groupkit/groupabac` → `principal.id in resource.channel.members`

**Contract for `0sc.12` (Channels plugin rework on new plugin ABAC):**

The brainstorm for `0sc.12` will react to this spec. It MUST:

- Bind to substrate per §1.
- Adopt SDK primitives as the validating consumer (N=2). For each primitive: report **"adopt as-is"** / **"adopt with API tweak: ..."** / **"reject as not-fit, reasoning: ..."**.
- If a primitive does not fit channels cleanly, that is signal to refine the primitive's shape (or accept it stays scenes-bespoke), NOT to fork channels-bespoke code that duplicates scenes' inline approach.
- Settle the channels-specific concerns this spec does NOT decide: type vocabulary, moderation primitives, history-replay UX (full backlog vs since-member-joined vs configurable).

**Phases unblocked by this spec:**

- `0sc.12` channels rework — next major channels frontier after substrate beads (`jg9b.4`) land.
- Post-`0sc.12` validation: substrate beads to extract SDK primitives.
- `0sc.3`-`0sc.7` remaining channel features build on substrate + SDKs.

### 4.3 Forums (`holomush-djj`) — OUT of groupkit (INV-S10)

**Substrate consumed:** §1 directly.

**SDKs consumed:** `eventkit` only (when channels validates the primitives). NOT `groupkit`.

**Why not in groupkit:** Forums is a document store (boards → threads → posts), not a stateful group with member-visibility. Threads are read by anyone with board-level permission. There is no "scene member" or "channel member" analog — forum participation is "wrote a post in thread X" (incidental), not "was granted membership" (intentional). Groupkit's primitives (`membership`, `focuswire`) do not compose cleanly with this shape; `groupabac` would not gate forum threads correctly.

**What forums uses from substrate:**

- JetStream subjects: `events.<game_id>.forum.<board_id>.>` for new-post / edit / lock notifications.
- ABAC: board-level read/post/moderate policies. Operates on board attributes, not thread membership.
- Per-plugin storage: forums plugin's schema for boards/threads/posts tables.
- Web Portal as primary surface: ConnectRPC + `QueryHistory` for thread rendering.

**What forums uses from `eventkit` (post-N=2):**

- `replay` for paginated thread display (ABAC-filtered history with board-level visibility check).
- `cryptoemit` IF private boards land with crypto-enveloped posts.

**Deferred to forums brainstorm (`djj.1` design):** thread/post entity design, web UI shape, notification routing, in-game `forum recent` command, edit/delete-as-new-event model.

### 4.4 Discord (`holomush-aqq`) — bridge, defaults to no SDK (INV-S10)

**Substrate consumed:** §1 selectively (channels-related parts).

**SDKs consumed:** **default: none.** Discord plugin is a bridge, not a group owner. `groupkit` is forbidden by INV-S10 (no local membership state). `eventkit/replay` adoption IS permitted if cross-history sync (matching Discord's message history with HoloMUSH channel history for a given OAuth-linked account) requires ABAC-filtered replay of HoloMUSH channel history — that's the document-style read shape `replay` is designed for. The discord brainstorm (`aqq.1`) decides whether this is needed.

**Why no `groupkit`:** Discord does not have local membership — it mirrors Discord's groups (via OAuth-linked accounts) and forwards events between Discord and HoloMUSH's channels (`holomush-0sc`). There is no "Discord channel member" stored locally; the bridge state is presence-sync + OAuth links. `groupkit/membership`, `groupkit/focuswire`, and `groupkit/groupabac` have no analog here.

**What discord uses from substrate:**

- JetStream subjects: subscribes to channel subjects via `requires: holomush.channel.v1.ChannelService` (when channels rework defines it).
- ABAC: OAuth-link policy (who can link what Discord account to what character).
- Per-plugin storage: discord's schema for OAuth link table + presence-sync state.
- Plugin host RPCs: standard emit/subscribe.

**Deferred to discord brainstorm:** OAuth flow (`aqq.5`), bridge consistency model, presence-sync semantics, mention/DM forwarding.

---

## 5. Surfaces (telnet, web terminal, web portal)

### 5.1 Surface-uniform delivery

All three surfaces flow through the **same substrate primitives** (JetStream + focus + crypto + ABAC + history fallback). The difference is in the renderer (terminal display, web chat UI, web portal UI), not in event delivery, authorization, or storage.

**Invariant:** a use plugin's correctness MUST be independent of which surface a participant is using. Telnet user and Web Terminal user receive the same events through the same authorization gates.

### 5.2 Telnet

**Path:** TCP → session manager → focus subscription → JetStream session stream → terminal renderer (verb-based formatting via `internal/web/translate.go` adapted for telnet output).

**Reconnect:** Telnet reconnect rebinds the connection to the existing session (two-phase login); focus state restores from session record; JetStream replays unseen events from last cursor.

### 5.3 Web Terminal

**Path:** Web browser → SvelteKit client → ConnectRPC transport ([`web/src/lib/transport.ts`](../../web/src/lib/transport.ts)) → server [`internal/web/handler.go`](../../internal/web/handler.go) → focus subscription → JetStream session stream → terminal renderer in browser.

**Reconnect:** Browser-side `streamBackfill` ([`web/src/lib/backfill/streamBackfill.ts`](../../web/src/lib/backfill/streamBackfill.ts)) handles JetStream replay; focus restore is server-side. Multi-tab: each tab is its own connection with its own focus; character is visible everywhere ≥1 connection is focused (carried forward from v2 §2.2).

### 5.4 Web Portal (non-terminal views)

**Path:** Web browser → SvelteKit client → ConnectRPC → server → plugin's gRPC service → `QueryHistory` (paginated, ABAC-filtered) → portal-specific renderer.

**Distinct from terminal:** portal views are **document-style** (paginated, scrollable, queried) rather than **stream-style** (live, focus-driven). Both use the same substrate but through different RPCs:

- Stream-style: `SubscriberService.Subscribe` (focus-aware).
- Document-style: `<Plugin>Service.<Query>` RPC + `HistoryReader.QueryHistory`.

This distinction is load-bearing: when a portal view needs to display historical content (scene log, channel history, forum thread), it queries — it does not subscribe-and-replay. `eventkit/replay` is the SDK primitive that wraps the document-style read path with ABAC + sensitivity filtering.

### 5.5 Reconnect (cross-surface)

Substrate-level reconnect is already implemented via the focus coordinator's `Restore` + JetStream replay. **This spec adds nothing new** — reconnect is solved. Per-use semantics (e.g., "reconnecting to a scene auto-presents the scene focus") are use-level customization; substrate enables, plugin decides.

### 5.6 Surface-specific concerns live in plugin or surface code, NOT substrate

| Surface concern | Where it lives |
|-----------------|----------------|
| Telnet edge cases (multi-character per connection, terminal width, line wrapping) | Plugin's renderer or `internal/web/translate.go` (substrate adapter, not a use's domain code) |
| Web terminal chat view UX (`5rh.18`) | Web client (SvelteKit components) + plugin's Query RPCs |
| Web Portal scenes browser (`5rh.8`) | Web client + plugin's `ListScenes` RPC |
| Reconnect-specific UX (e.g., "you missed N events while away") | Plugin's renderer reading replay-buffer length |

---

## 6. Scenes v2 supersession map

For every section of [`2026-04-06-scenes-and-rp-design-v2.md`](2026-04-06-scenes-and-rp-design-v2.md), status and action.

### 6.1 Section-by-section status

| v2 section | Status | Action in new contract |
|------------|--------|------------------------|
| D1–D10 design decisions | CARRIED FORWARD | Reproduced verbatim; all 10 still valid |
| §1.1 Scene entity | CARRIED FORWARD | Field set unchanged |
| §1.2 State machine | CARRIED FORWARD | `active/paused/ended/archived` |
| §1.3 Scene Participant fields | REVISED | `OriginLocationID` and `PublishVote` v2 fields were deferred in Phase 3; reinstate decision lives with Phase 6 brainstorm (`5rh.15`). Shipped Phase 3 schema: `(scene_id, character_id, role, joined_at)` |
| §1.4 Scene Template | CARRIED FORWARD | Phase 7 work |
| §1.5 Scene Log (Published) — name collision | REVISED (collision identified) / DEFERRED (rename) | The v2 publication-snapshot concept conflicts with the shipped `scene_log` audit-projection table. Phase 6 brainstorm renames the publication artifact (proposed: `scene_publication`); `scene_log` stays the audit-projection table |
| §2 Membership vs Focus | CARRIED FORWARD | Principle holds; Phase 5 contract still pending |
| §3.1 Stream naming | REVISED | NATS-style `events.<game_id>.scene.<id>.<facet>` per INV-S4 and `.claude/rules/event-conventions.md`. Colon-style is legacy and translated at boundary |
| §3.2–3.4 Routing, output, subscription | REVISED | Mechanism updated: focus coordinator is the subscription router; JetStream is the delivery substrate; replay composes `HistoryReader` |
| §4 Pose order | CARRIED FORWARD | 4 modes, derive-from-stream approach |
| §5.1–5.4 ABAC model | CARRIED FORWARD | Plugin-owned resource type, attribute resolver, manifest policies |
| §5.5 Hard privacy boundary | CARRIED FORWARD as INV-S9 | Plugin-code enforcement, no ABAC, no admin bypass |
| §5.6 Publish vote flow | CARRIED FORWARD | Design intact — Phase 6 work |
| §5.7–5.8 Log access + download | CARRIED FORWARD | Phase 6 work |
| §6 Commands | CARRIED FORWARD | All named commands still valid |
| §7 gRPC RPCs | CARRIED FORWARD | `GetPoseOrder` added Phase 4; vote/log RPCs Phase 6 |
| §9.1 Manifest pattern | CARRIED FORWARD | Shipped |
| §9.2 Schema isolation | CARRIED FORWARD | `plugin_core_scenes` schema, role isolation |
| §9.3 Migration sequence | REVISED | Update to reflect shipped 000001–000005 (incl. 000002 state check, 000003 combined participants+ops_events, 000005 DEK columns) |
| §9.4 Plugin lifecycle | CARRIED FORWARD | Shipped |
| §9.5 Command routing | CARRIED FORWARD | Shipped |
| §9.6 Cross-plugin comm | CARRIED FORWARD + STRENGTHENED by INV-S1 | Strict plugin-boundary rule added |
| §10.1 Tracing | CARRIED FORWARD | Shipped |
| §10.2 Metrics | STILL OPEN | Binary plugin metrics infrastructure unresolved; `plugins/core-scenes/metrics.go:1-30` confirms no-op stubs |
| §10.3–10.4 Logs + business events | CARRIED FORWARD | Shipped |
| §11.1 Binary plugin metrics path | STILL OPEN | Defer to separate plugin-infrastructure spec |
| §11.2 Plugin→server event emission contract | RESOLVED | Closed; see `internal/plugin/event_emitter.go::Emit` and `.claude/rules/event-conventions.md` "Emitting from plugins" |
| §11.3 Telnet edge cases | DEFERRED | Phase 10 work |
| §11.4 Content warning taxonomy | DEFERRED | Phase 8 work |
| §11.5 Idle timeout defaults | DEFERRED | Phase 2/Phase 10 work |
| §11.6 Scene board web integration | DEFERRED | Phase 8/Web Portal work |
| §11.7 Notification preferences | DEFERRED | Phase 10 work |
| §11.8 Published log presentation | DEFERRED | Phase 6/Web Portal work |
| §11.9 Forum view detailed UX | DEFERRED | Out of scope per §4.3 |
| §11.10 Plugin→server focus model integration | PARTLY RESOLVED | `oy6e.10` core-scenes adoption shipped; Phase 5 brainstorm settles remaining contract |

### 6.2 New in this spec (not in v2 at all)

| Topic | Why new |
|-------|---------|
| Crypto envelope contract | Substrate added crypto Phases 1-5+7 between v2 and now |
| `scene_ops_events` table (content vs ops event split) | Phase 3 introduced; load-bearing for Phase 4 pose-order computation |
| Plugin audit projection (`PluginAuditService.AuditEvent`) | Substrate added during JetStream cutover |
| Plugin runtime symmetry (Go + Lua parity) | Substrate-wide invariant codified post-v2 |
| `eventkit` + `groupkit` SDKs | Cross-use forward leverage |
| Manifest emit-type startup validation (INV-S5) | Belt-and-suspenders for `crypto.emits` classification |
| Strict plugin-boundary rule (INV-S1) | Codified; plugin PRs MUST NOT touch substrate |

---

## 7. Forward path & bead chain

### 7.1 What this spec enables to start immediately

After this spec lands READY (design-reviewer verdict) and `plan-to-beads` materializes the chain:

```text
holomush-jg9b (epic) — Substrate contract: theme:social-spaces
│
├─ Phase A: Substrate capability
│  ├─ jg9b.1  Substrate: manifest emit-type validation capability
│  │
│  ├─ jg9b.2  Plugin: core-communication adopts RegisterEmitTypes   (parallel)
│  ├─ jg9b.3  Plugin: core-scenes adopts RegisterEmitTypes (empty)   (parallel)
│  │  (.2 ∧ .3 depend on .1)
│  │
│  └─ jg9b.4  Substrate: flip emit-type validation fail-closed
│             (depends on .2 ∧ .3)
│
└─ Phase B: Hygiene + propagation (parallel after .4 closes)
   ├─ jg9b.5  Documentation: substrate-contract orientation in site/docs
   ├─ jg9b.6  Roadmap: update theme:social-spaces narrative
   └─ jg9b.7  Bead-hygiene: update affected beads to reference spec
```

**`jg9b.4` unblocks (via dep edges, not parentage):**

- `5rh.13` Scenes Phase 4 (event streams + pose order) — will add `crypto.emits` entries.
- `0sc.12` Channels plugin rework — will add `crypto.emits` entries and serve as N=2 validator for SDK primitives.

### 7.2 Downstream work tracked under existing epics

| Bead | Parent epic | Status |
|------|-------------|--------|
| `5rh.13` Phase 4 | `5rh` Scenes & RP | OPEN — depends on `jg9b.4` |
| `5rh.14` Phase 5 | `5rh` Scenes & RP | OPEN — depends on `5rh.13` |
| `5rh.15` Phase 6 | `5rh` Scenes & RP | OPEN |
| `5rh.16`-`5rh.19` Phases 7-10 | `5rh` Scenes & RP | OPEN |
| `0sc.12` Channels rework | `0sc` Channels | IN_PROGRESS — depends on `jg9b.4`; N=2 validator |
| `0sc.2`-`0sc.7`, `0sc.10`, `0sc.11` | `0sc` Channels | OPEN; may re-shuffle post-`0sc.12` |
| `djj.1`-`djj.5` Forums | `djj` Forums | OPEN; independent brainstorm |
| `aqq.1`-`aqq.7` Discord | `aqq` Discord | OPEN; depends on channels |

### 7.3 Future SDK extraction beads (post-N=2 validation)

After `0sc.12` validates `eventkit` and `groupkit` primitive shapes, a separate substrate epic will be filed to extract validated primitives into `pkg/plugin/eventkit/` and `pkg/plugin/groupkit/`. That epic is NOT pre-filed here per INV-S7. Its mission and child beads are determined by `0sc.12`'s validation report.

### 7.4 Per-Phase brainstorms still needed

| Brainstorm | Trigger | Decides |
|------------|---------|---------|
| Phase 4 emit + pose order (`5rh.13`) | After `jg9b.4` lands | crypto sensitivity matrix; GetPoseOrder RPC; ops vs content event vocabulary |
| Channels rework (`0sc.12`) | After `jg9b.4` lands | type vocabulary; moderation primitives; history-replay UX; SDK validation feedback |
| Phase 5 focus integration (`5rh.14`) | After Phase 4 | auto-focus-on-join; membership-vs-focus crossover |
| Phase 6 publish vote + privacy + `scene_log` rename (`5rh.15`) | After Phase 5 | publication artifact name; vote machinery; OriginLocationID / PublishVote reinstate decision |
| Phases 7-10 (`5rh.16`-`5rh.19`) | Sequential after Phase 6 | templates; scene board; web client; polish |
| Forums design (`djj.1`) | Independent | thread/post model; web UI; eventkit adoption shape |
| Discord design (`aqq.1`) | After channels | OAuth; bridge model; presence sync |
| Binary plugin metrics infrastructure | Independent substrate brainstorm | Path for plugins to expose Prometheus metrics |

---

## 8. Non-goals

This spec explicitly DOES NOT:

1. Design Phase 4, 5, 6, or 7+ implementation details — those get per-Phase brainstorms.
2. Design forums (`djj`) or discord (`aqq`) — separate brainstorms.
3. Specify channels-rework details (`0sc.12`) — gives it a contract to bind to; does not write its design.
4. Ship `eventkit` or `groupkit` code — names contracts only; code lands after N=2 validation.
5. Reshape `internal/core/`, `internal/eventbus/`, `internal/access/`, or any substrate-layer code beyond INV-S5 — substrate is otherwise consumed as-is.
6. Resolve §11.1 binary-plugin Prometheus metrics gap — separate plugin-infrastructure spec.
7. Decide crypto sensitivity classification for scene event types — Phase 4 brainstorm's job.
8. Rename `scene_log` audit table — Phase 6 brainstorm chooses the publication-artifact name.
9. Move `theme:social-spaces` work to a different organizational unit — `bd` epic structure (5rh, 0sc, djj, aqq) stays as-is.
10. Add `groupkit` or `eventkit` to substrate (`internal/`) — they live exclusively in `pkg/plugin/`.

---

## 9. Areas needing deeper design

Carried forward from v2 §11 plus new items. Each gets its own brainstorm before implementation.

| Area | Status | Brainstorm trigger |
|------|--------|---------------------|
| Phase 4 emit + pose order | OPEN | After `jg9b.4` lands |
| Phase 5 focus model integration | OPEN | After Phase 4 |
| Phase 6 publish vote + privacy + `scene_log` rename | OPEN | After Phase 5 |
| Phases 7-10 scenes polish | OPEN | Sequential after Phase 6 |
| Channels rework + SDK validation | OPEN | After `jg9b.4` lands |
| Forums design | OPEN | Independent |
| Discord design | OPEN | After channels |
| Binary plugin Prometheus metrics | OPEN | Separate substrate-infra brainstorm |
| Telnet edge cases | OPEN | Phase 10 |
| Content warning taxonomy | OPEN | Phase 8 |
| Scene board web portal integration | OPEN | After Phase 8 |
| Notification preferences | OPEN | Phase 10 |
| Web Portal expansion (forums, channels-web, scenes-portal) | OPEN | When ≥2 portal surfaces start landing concurrently (would trigger `theme:web-portals`) |
| Cross-plugin moderation primitives (if shared) | OPEN | After channels moderation lands |

---

## 10. References

### Within the repository

- [`docs/roadmap.md`](../../roadmap.md) — `theme:social-spaces` narrative.
- [`docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md`](2026-04-06-scenes-and-rp-design-v2.md) — superseded for substrate concerns; carried forward for product semantics.
- [`docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md`](2026-04-18-jetstream-event-log-design.md) — JetStream substrate design.
- [`.claude/rules/event-conventions.md`](../../.claude/rules/event-conventions.md) — subject naming, identity vs ordering, emit conventions.
- [`.claude/rules/event-interfaces.md`](../../.claude/rules/event-interfaces.md) — `Publisher`/`Subscriber`/`HistoryReader` interface shapes.
- [`.claude/rules/plugin-manifest.md`](../../.claude/rules/plugin-manifest.md) — manifest fields, DAG validation, `crypto.emits`.
- [`.claude/rules/plugin-runtime-symmetry.md`](../../.claude/rules/plugin-runtime-symmetry.md) — Go + Lua parity invariant.

### Working precedents cited

- [`pkg/plugin/focus_client.go`](../../pkg/plugin/focus_client.go) + [`internal/plugin/hostfunc/stdlib_focus.go`](../../internal/plugin/hostfunc/stdlib_focus.go) — Go + Lua parity template.
- [`plugins/core-communication/plugin.yaml:272-297`](../../plugins/core-communication/plugin.yaml) — `crypto.emits` declaration with 8 event types classified.
- [`plugins/core-scenes/plugin.yaml`](../../plugins/core-scenes/plugin.yaml) — plugin manifest with resource_types, requires, provides, audit, crypto.emits, commands, policies.
- [`plugins/core-scenes/migrations/000003_scene_participants_and_ops_events.up.sql`](../../plugins/core-scenes/migrations/000003_scene_participants_and_ops_events.up.sql) — content vs ops split.
- [`plugins/core-scenes/migrations/000005_add_scene_log_dek_columns.up.sql`](../../plugins/core-scenes/migrations/000005_add_scene_log_dek_columns.up.sql) — crypto envelope shape.

---

## Document history

| Date | Action | Notes |
|------|--------|-------|
| 2026-05-16 | DRAFT authored | Brainstorming session (bead `holomush-jg9b`); supersedes scenes v2 substrate concerns |
