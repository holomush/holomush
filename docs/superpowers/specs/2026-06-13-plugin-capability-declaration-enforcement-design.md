<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plugin Capability-Declaration Enforcement at Load (binary half; Lua via eykuh.4)

- **Design bead:** holomush-si3zs
- **Date:** 2026-06-13
- **Status:** Draft (design-review round 2)
- **Epic:** holomush-eykuh (Plugin capability & dependency architecture)
- **Relates to:** holomush-eykuh.3 (least-privilege enforcement, merged PR #4434) — this spec closes a gap that epic left open. holomush-eykuh.4 (sub-spec 5, Lua production migration) — binds INV-PLUGIN-55 (the Lua invariant).

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as described in RFC 2119 / RFC 8174.

## 1. Problem

The eykuh.3 sub-spec introduced a default-deny host-capability interceptor:
a binary plugin that calls a `host.v1` capability service it did not declare in
its manifest `requires:` is denied at **call** time with `CAPABILITY_NOT_DECLARED`
(`internal/plugin/hostcap/interceptor.go`). But nothing fails **earlier**: such a
plugin still **loads cleanly**, and the missing declaration only surfaces when a
call path is exercised. `core-scenes` shipped declaring only
`requires: WorldService` while its code consumed `focus`, `eval`, `settings`,
`stream.history`, and `audit` — the gap was invisible to unit tests (synthetic
manifests) and to the `wholesystem` census (which asserts only that plugins load
and `help` is registered), and surfaced only in the scenes integration + E2E
suites in CI, after an autonomous drain had already opened the PR (PR #4434, since
fixed).

### 1.1 Root cause — capability injection ignores the manifest (both runtimes, in production)

The deeper defect is that **client/capability injection is driven by code shape,
not by the manifest** — in production, for *both* runtimes:

| Runtime | Production capability wiring | Manifest-respecting? |
| --- | --- | --- |
| **Binary** | `pkg/plugin/sdk.go::pluginServerAdapter.Init` type-asserts the provider against the `*Aware` interfaces and injects host clients **unconditionally** (`sdk.go:186-203`). | **No** — code-driven. |
| **Lua** | Production Lua plugins are wired through the legacy `hostfunc.Register(L, name, requires…)` shim, which injects host functions **largely unconditionally** (eykuh.1 foundation spec, `2026-06-11-plugin-capability-dependency-foundation-design.md:83`). | **No** — code-driven. |

There is a *declaration-gated* Lua bridge (`internal/plugin/luabridge`,
`Host.WithHostCapBridge`), but it is **test-fixture-only for now** — its own doc
comment at `internal/plugin/lua/host.go:88` states it is gated behind
`WithHostCapBridge` pending "full production migration … tracked in sub-spec 5 /
holomush-eykuh.4." So production Lua is **not** yet declaration-gated either.

The capability a *binary* plugin's code can consume is fully expressed,
statically, by which `*Aware` interfaces its provider implements — so the binary
half is closeable now. The Lua half (migrating production Lua off the legacy
shim onto the declaration-gated bridge) is eykuh.4's job. The **end state** is
symmetric: both runtimes grant only declared capabilities. This spec delivers the
binary half and defines the shared invariant; eykuh.4 delivers the Lua half.

## 2. Goals / Non-goals

**Goals**

- Fail **closed at load** when a *binary* plugin's code can consume a non-exempt
  `host.v1` capability its manifest does not declare.
- Define **INV-PLUGIN-54** (binary; **bound now** by this spec's tests) and
  **INV-PLUGIN-55** (Lua; **`pending`** until eykuh.4 binds it) — two single-clause
  invariants, not one multi-clause entry (see §4).
- Make the existing `wholesystem` census the enforcement point for the binary
  half — no bolt-on, test-only guard with a separately-maintained mapping.

**Non-goals**

- **The Lua half.** Migrating production Lua off the legacy `hostfunc` shim onto
  the declaration-gated bridge is **holomush-eykuh.4** and is out of scope here.
  This spec MUST NOT claim Lua is already compliant.
- Changing the runtime interceptor (call-time enforcement stays as-is; this is
  defense-in-depth *earlier* in the lifecycle).
- Detecting Lua-script capability *usage* statically (not knowable from Go;
  Lua's fail-closed mechanism is not-wired-if-not-declared, which arrives with
  eykuh.4's bridge migration).
- Migrating plugin manifests (already done for the only affected plugin,
  `core-scenes`, in PR #4434).

## 3. Design

### 3.1 Capability injection becomes manifest-gated (binary)

`pkg/plugin/sdk.go::pluginServerAdapter.Init` MUST consult the manifest's
declared capabilities before injecting host-capability clients. For each
`*Aware` interface the provider implements, the adapter resolves the non-exempt
capability token(s) that interface grants and:

1. **Validate (fail-closed at load):** if any granted non-exempt token is **not**
   in the declared-capability set, `Init` MUST return an error
   (`CAPABILITY_NOT_DECLARED`-class, naming the capability token and the `*Aware`
   interface). A non-nil `Init` error fails plugin load.
2. **Gate (parity with eykuh.4's Lua end state):** the adapter MUST inject a
   host-capability client **only** when its capability is declared. In the
   validated, healthy case every implemented `*Aware` interface's capability is
   declared, so injection is unchanged; gating only matters on the error path and
   keeps behavior coherent if validation is ever bypassed.

**Decision — gate + validate (not validate-only).** Validate-only would fail load
but still inject by `*Aware`, leaving binary's wiring model divergent from the
declaration-gated end state. Gating injection on declaration matches the target
model (least-privilege: a plugin receives only what it declared); the load-time
error makes binary fail *loud* — a property binary can afford because its
consumption is statically declared via `*Aware` interfaces (Lua, post-eykuh.4,
will fail quiet: binding absent).

### 3.2 Host → plugin channel for declared capabilities

The binary plugin process learns its declared capabilities from the host via the
existing `InitRequest.ServiceConfig` (which already carries `required_services`).
A new repeated string field — **`declared_capabilities`** — MUST be added to
`ServiceConfig` in `api/proto/holomush/plugin/v1/plugin.proto`, populated by the
host from `manifest.RequiredCapabilities()` at Init dispatch. This is a proto
change and therefore MUST include: `buf generate` regeneration of the Go
bindings, a doc comment on the field (per the proto-doc-comment gate), and a
green `task lint:proto`. No new transport; one new field on an existing message.

### 3.3 The `*Aware` → capability-token registry

A single, centralized map co-located with the injection site relates each
host-capability `*Aware` interface to the capability token(s) it grants:

| `*Aware` interface | Capability token(s) | Exempt? |
| --- | --- | --- |
| `EventSinkAware` | `emit` | **exempt** (emit fence) |
| `FocusClientAware` | `focus`, `stream.history` | no |
| `HostEvaluatorAware` | `eval` | no |
| `SettingsClientAware` | `settings` | no |
| `SnapshotDecryptorAware` | `audit` | no |
| `CommandListerAware` | `command-registry` | **exempt** (dispatch subject) |

Exempt tokens (`declarationExemptCapabilities` = `emit`, `command-registry`) are
self-gated by their own mechanisms and MUST NOT require declaration — consistent
with the interceptor.

**Multi-token interfaces (`FocusClientAware`).** `FocusClientAware`
(`pkg/plugin/focus_client.go:215`) is a single interface backed by **both**
`FocusServiceClient` and `StreamHistoryServiceClient` (the `pluginHostFocusClient`
impl at `pkg/plugin/focus_client.go:220-236`), granting both `focus` and
`stream.history`. Because the one interface confers access to both tokens, a
provider implementing `FocusClientAware` MUST declare **both** `focus` **and**
`stream.history`, even if it only calls Focus RPCs — validation treats the
interface's full token set as required. (`core-scenes` already declares both, so
the current tree stays green.) Splitting `FocusClientAware` into two narrower
`*Aware` interfaces for finer-grained least privilege is a noted future
refinement, **out of scope** here.

The registry MUST be the single source of truth shared by the validation logic;
adding a new host-capability `*Aware` interface MUST add its row (enforced by
§3.6 testing so the map cannot silently drift).

### 3.4 Lua half — deferred to eykuh.4 (no change here)

Production Lua is **not** yet declaration-gated (§1.1). This spec MUST NOT alter
Lua wiring. **INV-PLUGIN-55** (the Lua invariant) is satisfied when
**holomush-eykuh.4** migrates production Lua off the legacy `hostfunc` shim onto
the declaration-gated `luabridge` path (which already gates host-cap bindings on
`manifest.RequiredCapabilities()`); at that point an undeclared capability's
binding is never injected into the Lua VM. Until then INV-PLUGIN-55 stays
`pending` (no `asserted_by`). This spec SHOULD leave a cross-reference note on
eykuh.4 so its plan binds INV-PLUGIN-55.

### 3.5 Census becomes the guard (binary half)

Because the binary validation fails `Init` (and thus load), the existing
`test/integration/wholesystem` census — which loads **all** in-tree plugins via
the real `Manager.LoadAll` / `setup.PluginSubsystem` path — becomes the
enforcement point for free: a misdeclared in-tree binary plugin fails the census.
No separate AST/reflection meta-test is introduced (the rejected alternative; see
§5).

### 3.6 Testing

- **Unit (binary, `pkg/plugin`):** a provider implementing `FocusClientAware`
  with a config whose `declared_capabilities` omits `focus` (or `stream.history`)
  → `Init` returns `CAPABILITY_NOT_DECLARED`; with both declared → injects and
  succeeds. Exempt-only providers (`EventSinkAware`/`CommandListerAware`) load
  with no declaration. **Binds INV-PLUGIN-54.**
- **Registry completeness meta-test:** assert every host-capability `*Aware`
  interface in `pkg/plugin` has a row in the §3.3 map (prevents drift when a new
  capability client is added).
- **Integration:** the wholesystem census passes for the current (fixed) tree
  and fails if a fixture binary plugin under-declares (negative arm).
- **Lua (INV-PLUGIN-55):** **deferred to eykuh.4** — that sub-spec's tests bind
  INV-PLUGIN-55.

## 4. Invariants

This guarantee splits into **two single-clause invariants** (one per runtime) so
each is bound by its own test. The registry binds an entry as a whole — a `bound`
entry whose test proves only one clause of a multi-clause statement is the
"partial binding" hazard flagged in `.claude/rules/invariants.md`, and a
`binding: pending` entry MUST NOT carry `asserted_by` (the `TestProvenanceGuard`
meta-test rejects it). A single split entry therefore cannot honestly represent
"binary proven now, Lua not yet." Two entries can. Register both in
`docs/architecture/invariants.yaml`, scope PLUGIN, `origin_spec` this document:

- **INV-PLUGIN-54 (binary):** A binary plugin's `Init` fails closed when its
  provider implements a host-capability `*Aware` interface for a non-exempt
  capability absent from the manifest; capability clients are injected only for
  declared capabilities. `emit` and `command-registry` are self-gated and exempt.
  → `binding: bound` once §3.6's unit + census tests land (this spec), with
  `asserted_by` listing them.

- **INV-PLUGIN-55 (Lua):** A Lua plugin is wired only the capabilities its
  manifest declares. → `binding: pending` **with no `asserted_by`**; bound by
  holomush-eykuh.4 when production Lua moves onto the declaration-gated bridge and
  adds its binding test.

Two entries (not one split entry) is the registry-honest representation: the
binary guarantee is proven now; the Lua guarantee is genuinely unverified until
eykuh.4.

## 5. Alternatives considered (rejected)

- **Reflection-based meta-test** instantiating each plugin's provider and
  type-asserting `*Aware` interfaces: blocked — binary plugins are `package
  main`, their provider types are not importable into a test.
- **AST/source-scan meta-test** mapping SDK client calls to tokens: CI-only
  (outside the trust boundary, violating runtime-symmetry's spirit), advisory not
  enforcing, requires a hand-maintained map living in test code, and is redundant
  once §3.5 makes the census the guard. Rejected because — with no blast-radius
  or effort constraint — the design-aligned enforcement (§3.1) is strictly
  better.

## 6. Risks / open questions

- **`ServiceConfig.declared_capabilities` proto change:** greenfield, no external
  plugins, so no wire-compat concern; the execution hazard is the regen + doc +
  `task lint:proto` gate (§3.2), not compatibility.
- **Forwarding `*Aware` interfaces:** `core-scenes` forwards injected clients from
  its `scenePlugin` to its inner `SceneServiceImpl`; the validation keys on the
  outer provider's `*Aware` implementations (the injection site in `sdk.go`),
  which is correct — confirm no plugin injects via a non-`*Aware` path that would
  escape the check.

## 7. Out of scope

- The **Lua half** (holomush-eykuh.4).
- Splitting `FocusClientAware` into finer-grained interfaces (§3.3, future).
- Manifest backfill (done in PR #4434).
- Runtime interceptor changes.
<!-- adr-capture: sha256=8f1630d250be01a5; session=cli; ts=2026-06-13T18:52:12Z; adrs=holomush-m4ac3,holomush-toh7a,holomush-1psri,holomush-wlyzs,holomush-nk46j -->
