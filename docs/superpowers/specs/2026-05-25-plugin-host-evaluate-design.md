# Plugin Host `Evaluate` — per-action ABAC for plugin commands

**Date:** 2026-05-25
**Status:** Draft
**Design bead:** holomush-8kkv5
**Unblocks:** holomush-5rh.20.35 (E2: scene publish vote extend)

## Problem Statement

Plugin commands cannot enforce per-subcommand / per-action authorization. A
plugin command (e.g. `scene`) declares **command-level** capabilities in
`plugin.yaml` (`scene → {action: write, resource: scene}`). At dispatch the host
evaluates two coarse gates:

1. **Layer 1** — `engine.Evaluate(subject, "execute", "command:scene")` (may this
   character run `scene` at all).
2. **Layer 2** — capability pre-flight `engine.CanPerformAction(subject, "write",
   "scene", scope)`. Per the 2026-04-07 C1 hardening this resolves **only**
   subject / environment / action attributes — never a resource instance — so it
   passes *optimistically* with no instance to evaluate against.

The per-operation policies that `core-scenes` already declares in `plugin.yaml`
(`end-own-scene`, `pause-own-scene`, `write-scene-as-participant`,
`transfer-ownership`, …) are **never evaluated**: there is no call-site that has
both a real resource instance *and* a specific action. A subcommand cannot map to
a distinct command-level capability without breaking sibling subcommands (they
share one command entry).

The plugin also cannot self-gate **principal-attribute** policies (admin):

- No `roles` field on `pluginsdk.CommandRequest` (verified: only
  `Command/Args/CharacterID/CharacterName/LocationID/SessionID/PlayerID/InvokedAs/ConnectionID`).
- No roles on `QueryCharacter`'s `CharacterInfo` (verified: only `id` + `name`).
- No ABAC-evaluate host RPC on any surface (verified: `plugin.proto`
  `PluginHostService`, `hostfunc.proto` `HostFunctionsService`, `audit.proto`,
  `attribute.proto` — the last is the inverse host→plugin direction).

Resource-relative policies (owner / participant) *are* self-gatable today —
`core-scenes` does ad-hoc `store.IsParticipant` checks in Go
(`commands.go::handleEmit`, ~L844) — but admin is not.

**Impact:** E2 (`scene publish vote extend`) must be admin-only. Wiring it ungated
is a security regression (exposes `max_publish_attempts` bumping to every player).

## What already exists (and is reused unchanged)

The 2026-04-06 plugin-ABAC-trust-boundary work built most of the machinery:

- `core-scenes` **owns** the `scene` resource type (`scene` is **not** in
  `ProtectedResourceTypes`; `plugin.yaml` declares `resource_types: [scene]`).
- All per-operation `scene` policies are already declared in
  `plugins/core-scenes/plugin.yaml`.
- `core-scenes` implements the `AttributeResolver` gRPC service
  (`resolver.go`) exposing `participants`, `owner`, etc.
- The host resolves **principal** attributes (`principal.character.roles`,
  defaulting to `["player"]` — `attribute/character.go`).

The engine, the policies, and attribute resolution are all in place. The single
missing primitive is an **evaluation entry point** the plugin can call with a
real resource instance and a specific action.

## RFC 2119 Keywords

| Keyword        | Meaning                                    |
| -------------- | ------------------------------------------ |
| **MUST**       | Absolute requirement                       |
| **MUST NOT**   | Absolute prohibition                       |
| **SHOULD**     | Recommended, may ignore with justification |
| **SHOULD NOT** | Not recommended                            |
| **MAY**        | Optional                                   |

## Decisions (brainstorm 2026-05-25)

- **Scope:** general, reusable entry point. E2 is the first consumer; B8 and
  future plugins reuse it.
- **Blast radius:** *unify on engine.* All per-action plugin authorization flows
  through the host engine via `Evaluate`; `core-scenes`' ad-hoc Go decision
  checks migrate to DSL + `Evaluate` calls. The engine becomes the single
  source of truth for per-action plugin authorization.
- **Architecture:** Option C (host `Evaluate` RPC), rejecting:
  - **A** (host parses subcommands and evaluates at dispatch) — a layering
    violation: the host only sees `cmd.Args` as an opaque string and would have
    to learn every plugin's subcommand grammar *and* extract resource IDs from
    free-form args.
  - **B** (expose `roles` to plugins so handlers self-gate admin) — moves
    enforcement *out* of the host engine (the admin DSL policy becomes
    decorative), contradicting "unify on engine" and creating two enforcement
    engines.

## Design

### 1. The `Evaluate` entry point

A single new host RPC, mirrored on both plugin surfaces:

```protobuf
rpc Evaluate(EvaluateRequest) returns (EvaluateResponse);

message EvaluateRequest {
  string action   = 1;  // "extend_publish_attempts", "write", "end", "invite"
  string resource = 2;  // typed *instance* ref: "scene:01ABC..."
  // NB: no `subject` field — see §2.
}

message EvaluateResponse {
  bool   allowed        = 1;
  string reason         = 2;  // human-facing denial text for the user message
  string matched_policy = 3;  // for the audit trail / debugging
}
```

The plugin supplies **action + resource instance**; the host runs the full ABAC
pipeline (principal attributes it owns, resource attributes via the plugin's own
`AttributeResolver`, environment attributes) and returns the decision. Nothing
else about the engine changes.

This is a **third enforcement tier**, not a replacement. Layer 1 (`execute
command:<name>`) and Layer 2 (type-level capability pre-flight) stay at dispatch.
`Evaluate` adds Layer 3 *inside the handler*: "may this actor do *this
subcommand* on *this resource instance*."

### 2. Subject authenticity (anti-spoofing)

`EvaluateRequest` has **no subject field by construction.** The host derives the
subject from the authenticated actor already bound to the call.

- **MUST** derive subject host-side:
  - **Binary:** from the actor-metadata + token mechanism the host already uses
    to authenticate `EmitEvent`. The dispatcher stamps `core.WithActor` before
    `DeliverCommand` (`dispatcher.go::dispatchToPlugin`); the host service
    recovers the actor on the inbound `Evaluate` call.
  - **Lua:** from the dispatch `ctx` threaded into the hostfunc (the path
    `list_commands` already uses).
- **MUST** fail closed: if no authenticated actor is bound to the call,
  `Evaluate` returns `allowed=false` and an error. A plugin can never evaluate
  as an empty or chosen subject.
- v1 is locked to *"the current actor."* Evaluating on behalf of **another**
  character is out of scope (§ Out of Scope).

This mirrors the existing anti-spoof stance in `dispatcher.go::extractAuditHints`
("the plugin cannot spoof these fields — the dispatcher overwrites them").

### 3. Entitlement (oracle / probe containment)

To stop a plugin using `Evaluate` as a policy oracle against unrelated domains:

- The `resource` type **MUST** be one the plugin owns (declared in
  `resource_types`) or `command` for its own commands — the same trust boundary
  the policy installer already enforces (2026-04-06 §2.1). `core-scenes` asking
  about `scene` is allowed; asking about `server` or another plugin's resource is
  **rejected** (deny + error).
- `action` stays free-form (like capabilities); an unmatched action default-
  denies. No extra entitlement check on action beyond non-empty.
- Rationale: a plugin already has full data access to its own resources, so "can
  this actor do X on *my* resource" leaks nothing new — and that answer is
  precisely what it needs to gate. Cross-domain probing is what is forbidden.

**Entitlement anchor (binary and Lua).** The ownership signal the host checks is
**identical for both runtimes**: the requesting plugin's manifest. A plugin may
evaluate resource type `R` iff `R ∈ manifest.resource_types`, plus the `command`
carve-out for its own commands. Because `resource_types` is binary-only
(`internal/plugin/manifest.go:570-572` — Lua and setting plugins MUST NOT declare
it), a Lua plugin's `resource_types` is structurally empty, so its effective
entitlement degrades to the `command` carve-out only. This is **not** a
runtime-asymmetric trust rule — the *rule* is one line of host code applied to
both surfaces; only the manifest *data* differs. It also falls out correctly from
the existing architecture: resource-instance evaluation needs an
`AttributeResolver`, which is binary-only (2026-04-06 §3), so instance-level
`Evaluate` is inherently a binary-plugin capability. The Lua surface exists for
parity (§4) and the `command` carve-out, and fails closed for resource types the
Lua plugin does not (cannot) own. This data-vs-policy distinction is exactly what
the plugin-runtime-symmetry invariant permits ("runtime-specific code is
acceptable for runtime-specific concerns … but MUST NOT differ in policy / trust /
manifest-gate dimensions").

### 4. Runtime symmetry (binary + Lua, shipped together)

Per the plugin-runtime-symmetry invariant and the `host_rpc_lua_parity` rule,
`Evaluate` **MUST** land on both surfaces in one change:

- **Binary:** `PluginHostService.Evaluate` (gRPC) + Go SDK helper
  `host.Evaluate(ctx, action, resource) (Decision, error)`.
- **Lua:** `HostFunctionsService.Evaluate` + hostfunc
  `holomush.evaluate(action, resource) -> allowed, reason`.
- Both **MUST** dispatch into one host-side implementation that calls the ABAC
  engine — the gate lives at the common path, so policy / trust behavior cannot
  diverge between runtimes. Runtime-specific code is limited to runtime-specific
  concerns (binary token auth vs. Lua ctx threading), never the decision logic.

### 5. Audit (host-owned, automatic)

Every `Evaluate` is an authorization decision, so the **host MUST** emit exactly
one audit event per call: subject host-stamped, plus action / resource / effect /
matched-policy / `component=<plugin>`. The plugin **MUST NOT** hand-roll authz
audit hints for these decisions — the host owns the stamping, so a plugin can
neither forge nor forget the authz trail. This also closes a current gap:
operators have no decision trail for plugin per-action gates today.

### 6. Making the gate hard to forget (cooperative-enforcement risk)

C's one weakness: enforcement is cooperative — a plugin author who forgets to
call `Evaluate` ships the action ungated. The mitigation is SDK ergonomics that
make the gate **structural**, not a remembered call.

- The SDK **SHOULD** provide a **gated subcommand dispatcher**. A subcommand is
  registered as `{name, action, resourceRef func(req) (string, error), handler}`.
  The SDK calls `Evaluate(action, resourceRef(req))` *before* `handler` and
  short-circuits to a denial response on deny.
- The plugin supplies the `resourceRef` extractor (e.g. "parse the scene id from
  arg 1", or "the scene I'm focused on") — so arg-grammar stays in the plugin (no
  layering violation), but the gate is automatic for every subcommand registered
  through the helper.
- `core-scenes` already has an internal subcommand router (`dispatchCommand`); it
  adopts the gated variant.
- Backstop: a table-driven test asserting every gated subcommand denies when its
  policy denies (INV-7).

The host **MUST NOT** attempt to verify call-sites — it never sees subcommands.
For untrusted third-party plugins, the residual "didn't gate" risk is bounded by
§2–§3 (they can only evaluate / gate their own resources). The threat model here
is "core plugin author forgets," which a structural SDK gate addresses.

### 7. `core-scenes` migration + E2's admin gate

**New action + policy (E2, unblocks holomush-5rh.20.35):**

- Declare `extend_publish_attempts` in the `core-scenes` manifest `actions:`.
- Add to `plugins/core-scenes/plugin.yaml`:

  ```text
  permit(principal is character, action in ["extend_publish_attempts"], resource is scene)
    when { "admin" in principal.character.roles };
  ```

- Wire `scene extend …` through the gated subcommand dispatcher →
  `Evaluate("extend_publish_attempts", "scene:<id>")`. Non-admins are denied by
  the engine; the command ships gated.

**Migrate existing subcommands onto the engine (unify):** each handler's ad-hoc
Go authorization decision is replaced by an `Evaluate` call against the policy
that **already exists** in `plugin.yaml`:

The **Action** column below is the exact action string the policy DSL matches
(verified against `plugins/core-scenes/plugin.yaml`); the `Evaluate` call MUST
pass that string verbatim or the policy will not match and the engine will
default-deny.

| Subcommand         | Existing policy (already declared)                  | Action (verbatim)    |
| ------------------ | --------------------------------------------------- | -------------------- |
| `end`              | `end-own-scene`                                     | `end`                |
| `pause` / `resume` | `pause-own-scene` / `resume-scene-as-participant`   | `pause` / `resume`   |
| `set`              | `update-own-scene`                                  | `update`             |
| `pose` / write     | `write-scene-as-participant`                        | `write`              |
| `invite` / `kick`  | `invite-to-scene` / `kick-from-scene`               | `invite` / `kick`    |
| `transfer`         | `transfer-ownership`                                | `transfer-ownership` |
| `join`             | `join-open-scene` / `join-private-scene-as-invitee` | `join`               |
| `leave` / `info`   | `leave-scene` / `read-scene-as-participant`         | `leave` / `read`     |

The engine becomes the single authoritative per-action decision. Cheap
structural guards (arity, scene-id present/parseable) stay in Go; the
authorization-decision Go checks (`store.IsParticipant` at `commands.go::handleEmit`, ~L844, the
owner checks) are **removed** — keeping them re-introduces the two-sources-of-
truth problem unification exists to eliminate. The `commands.go:844`
"defense-in-depth" comment becomes the `Evaluate` call itself.

### 8. Invariants

| ID    | Invariant |
| ----- | --------- |
| INV-1 | `Evaluate`'s subject is host-derived from the authenticated actor; there is no `subject` field on the wire. *(meta-test: proto descriptor has no subject field)* |
| INV-2 | No authenticated actor bound to the call → `Evaluate` returns deny + error (fail-closed). |
| INV-3 | A `resource` type the plugin does not own → rejected (entitlement). |
| INV-4 | Each `Evaluate` emits exactly one host-stamped audit event. |
| INV-5 | Binary and Lua surfaces reach identical host evaluation logic. *(parity meta-test)* |
| INV-6 | Unmatched action / resource → deny (default-deny preserved). |
| INV-7 | Every subcommand registered as "gated" denies when its policy denies (no ungated gated-subcommand). |

### 9. Files changed (anticipated)

| File | Change |
| ---- | ------ |
| `api/proto/holomush/plugin/v1/plugin.proto` | Add `Evaluate` to `PluginHostService` + messages |
| `api/proto/holomush/plugin/v1/hostfunc.proto` | Add `Evaluate` to `HostFunctionsService` + messages |
| `internal/plugin/goplugin/host_service.go` | Implement binary `Evaluate` (subject from actor/token; entitlement; engine call; audit) |
| `internal/plugin/hostfunc/*.go` | Implement Lua `Evaluate` hostfunc + `holomush.evaluate` |
| `pkg/plugin/*.go` | Go SDK `host.Evaluate` helper; gated subcommand dispatcher |
| `plugins/core-scenes/plugin.yaml` | Declare `extend_publish_attempts` under `actions:`; add admin policy under `policies:` |
| `plugins/core-scenes/commands.go` | Migrate subcommands to gated dispatcher; remove ad-hoc Go authz checks |
| `site/docs/extending/` | Plugin author guide: `Evaluate` + gated-subcommand pattern |
| `test/integration/...` | E2E coverage (§10) |

### 10. Testing

This work is **TDD** (`dev-flow:test-driven-development`): every test below is
written **before** the implementation it covers, and **MUST** fail for the right
reason before the implementation lands. Per-package coverage **MUST** be **≥ 80%**
(`task test:cover`) for each new surface on its own merits — the host `Evaluate`
implementation, the SDK gated dispatcher, and the Lua hostfunc each meet the bar
independently; they MUST NOT be diluted by the repo aggregate. Tests are
table-driven wherever the input space is enumerable. Unit-layer dependencies (the
engine, the actor source) use `mockery`-generated mocks; no real DB at the unit
layer.

#### 10.1 Unit — host `Evaluate` (the shared binary + Lua implementation)

**Happy path**

- Allow returned when policy permits the action on the instance (participant +
  `write`; admin + `extend_publish_attempts`).
- `allowed`, `reason`, and `matched_policy` populated correctly on both allow and
  deny.
- Exactly one host-stamped audit event emitted per call (INV-4).

**Boundaries / edges**

- Empty `action` → reject (non-empty rule).
- Malformed `resource` ref (no colon, empty type, empty id) → reject.
- Entitlement boundary (INV-3): plugin-owned resource type passes through to the
  engine; foreign or core-protected type rejected **before** the engine.
- Lua plugin (structurally empty `resource_types`) evaluating an instance type →
  rejected at entitlement; the `command` carve-out for its own command → allowed
  through (INV-3 across runtimes).
- Action matching no policy → deny (INV-6 default-deny).

**Error / fail-closed**

- No authenticated actor bound to the call → deny + error (INV-2).
- Engine returns an error → deny + error surfaced (fail-closed; assert it does
  **not** fail open).
- `AttributeResolver` unavailable / erroring → engine's existing circuit-breaker
  path → deny.

#### 10.2 Unit — SDK gated subcommand dispatcher

- A gated subcommand calls `Evaluate(action, resourceRef(req))` before the
  handler; the handler is **not** invoked on deny (INV-7).
- Deny short-circuits to a user-facing `CommandError` denial, not a fatal.
- `resourceRef` extractor error → usage/denial error, handler not invoked.
- Ungated subcommand path still works (back-compat).
- Table-driven over the full `core-scenes` subcommand set asserting
  deny-when-policy-denies for every gated subcommand (INV-7 backstop).

#### 10.3 Meta-tests (structural invariants)

- **INV-1:** reflect over the generated `EvaluateRequest` — assert it has **no**
  `subject` field.
- **INV-5:** assert both `PluginHostService.Evaluate` (binary) and
  `HostFunctionsService.Evaluate` (Lua) delegate to the one shared host
  evaluation function (single call-through via a shared seam / counting fake) —
  the runtime-symmetry guarantee.

#### 10.4 Real-engine + full-stack E2E

Two tiers, split by what the test infrastructure currently supports.

**Tier 1 — real-engine gate test (in scope, feasible now).** The
`extend_publish_attempts` admin gate is principal-attribute-only
(`"admin" in principal.character.roles`), so it needs no scene
`AttributeResolver`. It **MUST** be proven against a **real** `Engine` built
from the policy package's engine-builder (`createTestEngineWithPolicies` +
`characterProvider`, `internal/access/policy/seed_smoke_test.go`): install the
`admin-extend-publish-attempts` DSL, then drive `pluginauthz.Evaluate` with an
admin subject (→ allow) and a plain-player subject (→ deny). This exercises real
DSL parsing + condition evaluation + the `pluginauthz` core (entitlement, audit)
— not an allow-all stub.

**Tier 2 — full-stack command-path E2E (deferred to a follow-up, depends on
iwzt-9).** A `scene extend` E2E driven through the real stack
(telnet → loaded core-scenes plugin → gated dispatcher → host Evaluate), plus
participant/owner regression locks (`scene end`/`pause`/`transfer`/`kick` denied
for non-participants **through the engine + AttributeResolver**), **MUST** use
**Ginkgo/Gomega** (project E2E standard), build tag `//go:build integration`,
`task test:int`. These are **not feasible today**: the `integrationtest` harness
wires an **empty command registry** (`harness.go:214`), loads **no plugins**, and
uses **fake** engines by design (the privacy suite passes
`WithPolicyEngine(policytest.DenyAllEngine())`); `Session.CreateScene` is a
stub (`session.go:461`, TODO iwzt-9). They are filed as a follow-up bead blocked
on the harness extension (load core-scenes + scene-creation RPC). `eventbustest`
**MUST NOT** be used for E2E.

Coverage of the invariants does **not** depend on Tier 2: INV-4 (audit) and
INV-2/3/6 are covered by Tier-1 + the §10.1 unit suite; INV-5 (runtime parity)
by the §10.3 meta-test. Tier 2 adds command-path UX coverage and
resource-attribute regression locks once the harness supports them.

#### 10.5 Docs

The `site/docs/extending/` guide is **PR-blocking**: it documents the `Evaluate`
SDK helper, the gated-subcommand pattern, the entitlement rule, and that the
subject is host-derived (never plugin-supplied).

## Out of Scope

- Evaluating on behalf of a subject **other than** the current actor (future,
  higher-risk; needs its own design).
- Decision caching (the engine already caches attribute resolution).
- Changes to the Layer-1 / Layer-2 dispatch gates.
- Wiring B8's `publish` / `withdraw_publish` actions specifically — the mechanism
  supports them, but those ride on B8's own bead.

## Risks and Mitigations

| Risk | Mitigation |
| ---- | ---------- |
| Plugin forgets to gate a subcommand → ungated action | Structural SDK gated dispatcher (§6) + INV-7 backstop; untrusted plugins bounded by §2–§3 |
| Plugin uses `Evaluate` as a cross-domain policy oracle | Entitlement check (§3) — resource type must be plugin-owned |
| Subject spoofing | No subject on the wire; host-derived + fail-closed (§2) |
| Runtime privilege gradient | Parity requirement (§4) + INV-5 meta-test |
| Removing Go checks regresses behavior | Existing `plugin.yaml` policies already encode the same rules; E2E asserts engine-path equivalence |

<!-- adr-capture: sha256=7f2b311436e3cf10; session=cli; ts=2026-05-25T14:26:23Z; adrs=holomush-dttdj,holomush-qeypl,holomush-61rdl,holomush-9l9pu -->
