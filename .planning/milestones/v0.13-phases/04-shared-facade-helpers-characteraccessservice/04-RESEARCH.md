# Phase 4: Shared Facade Helpers & `CharacterAccessService` - Research

**Researched:** 2026-08-10
**Domain:** gRPC BFF facade construction, ABAC-gated read projection, proto surface extension, `go/ast` meta-census
**Confidence:** HIGH (every code claim opened at HEAD this session; the few unverifiable items are listed in the Assumptions Log)

> **Posture.** The ROADMAP says Phase 4 "should skip research — a verbatim copy of a fully-traced
> shipped path". That is true of the *facade skeleton* and false of three seams: the profile READ
> pipeline (§B/§E below), the census scope (§A), and the ABAC action-token mechanics (§F). This
> document is deliberately short on the copied parts and long on those three.

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

Copied verbatim in substance from `04-CONTEXT.md`; the planner MUST honor each and MUST NOT re-open any.

| D | Decision |
|---|---|
| **D-69** | **Keep the ALL direction unchanged. Split the audiences instead.** `viewer:` stays strictly public-facing; the owner-audience RPCs (`ListMyCharacters`, `GetMyCharacter`) gate on session resolution + ownership and **MUST NOT construct a `viewer:` principal at all**. *Rejected:* relaxing `owner_player_id`; the plain union on both permit-side peers. Reversible. |
| **D-70** | **`GetCharacterProfile` returns an identical projection regardless of who is viewing.** One code path, no self-detection branch. The owner sees everything through `GetMyCharacter`. |
| **D-71** | The D-69 consequence is pinned by a **new registry invariant plus a binding test**. It **MUST be hand-registered** in `docs/architecture/invariants.yaml`. Test seeds a two-alt player and asserts the private row is **absent**. Costly to reverse. |
| **D-72** | **The proto declares only the RPCs Phase 4 implements** — `ListCharacterDirectory`, `GetCharacterProfile`, `ListMyCharacters`, `GetMyCharacter`, `UpdateCharacterProfile`, `UpdateCharacterDescription`, plus their `Web*` proxies. Phases 5 and 6 add their own rows to the proto **and** to §3's inventory in the same change that implements them. Every census member has a live handler, so criterion 1 needs **no exemption list**. Reversible (additive). |
| **D-73** | **Criterion 1's routing census is over the `owner`-audience RPCs, both halves of each proxy pair.** `ListCharacterDirectory`/`GetCharacterProfile` serve anonymous viewers, and `resolveAndGate` rejects guests — routing them through it would break the public profile. Mechanism: `go/ast`, set equality with a symmetric-difference diff, following `test/meta/world_envelope_census_test.go:187-207` and `test/meta/world_caller_census_test.go`. |
| **D-74** | **`CharacterAccessService.RenameCharacter` is struck from §9.3** in this phase's amendment pass. **§9.4.2's prose must be fixed in the same edit** (dangling `RenameCharacter` reference). Narrowly-scoped `Edit` only — never a structural change, never a new version-bearing or ✅-bearing `###` heading. |
| **D-75** | The permit takes a **distinct narrow ABAC action** on `resource is character` — a `read_description`-shaped action, unconditional and off-location — served by **its own read path with its own projection** returning name + description only. `GetCharacter` keeps requiring `read` and keeps its colocation clause **untouched**. *Rejected:* narrowing `worldv1.CharacterInfo`; an unconditional `read` + DSL `when`-clause guard. Costly to reverse. |
| **D-76** | **Both a `character`-flavored permit and a `viewer`-flavored twin ship.** Consequence: this phase adds ABAC seeds, so the **`abac-reviewer` gate fires before push**. |
| **D-77** | **The mandated exposure audit is discharged by citing Phase 2's recorded result. Phase 4 authors NO new audit and performs no ceremonial re-run.** Comment on GitHub **#4937** recording that Phase 4 shipped the widening on Phase 2's evidence, and leave it OPEN. **The planner MUST NOT create a Phase-4 audit query.** |
| **D-78** | The extracted helpers live in a **small `playerGate` struct embedded by both facades** (`internal/grpc`), carrying `playerSessionRepo`, `playerRepo`, `charRepo`. Method promotion keeps every existing call site **byte-identical**. |
| **D-79** | **Criterion 5 is satisfied by the type system, not by a lint rule or a meta-test.** The facade's world dependency is a **narrow interface** exposing only `world.Service.ListPropertiesByParent` plus the character reads it needs. `PropertyRepository.ListByParent` is not in the facade's type set → a direct call is a **compile error**. *Rejected:* a `gorules` ruleguard analyzer and a `test/meta` AST test. |
| **D-80** | Criterion 2 is asserted by **seeding each withheld field with a distinctive sentinel, marshaling the response, and asserting the sentinel's bytes do not appear.** |
| **D-81** | The three ROADMAP sketch findings are **answered here and built nowhere**: admin rename census **WITHDRAWN**; A3 (username search) **ACCEPTED as design, implemented in Phase 6**; A2's RPC half **ACCEPTED as design, implemented in Phase 6**. |
| **D-82** | IDENT-02's caps **reuse the shipped world constants**: short single-line fields (`pronouns`, `concept`, `species`, `age`, `faction`, `currently`, `timezone`) at `MaxNameLength = 100`; long fields (`appearance`, `personality`, `biography`, `rumors`, `rp_preferences`) at `MaxDescriptionLength = 4000`, with `ValidateDescription`'s UTF-8 and control-character rules. Enforced **in the facade handler**. **IDENT-02a needs no new cap.** |
| **D-83** | The **viewer tier is resolved in the facade, at viewer-principal construction**, carrying criterion 3's exhaustive `switch` with `default: deny`: no session → `anonymous`, session with `IsGuest` → `guest`, non-guest session → `player`. **Constraint to preserve:** `player_id` is deliberately **OMITTED** on the `anonymous` rung. |

### Claude's Discretion

- Exact naming of the new ABAC action (D-75), the two seed policy ids (D-76), and the `playerGate` struct/field names (D-78), so long as the shapes hold.
- The new registry invariant's scope and id (D-71) — `INV-PRIVACY` or `INV-ACCESS` per the `boundary` declarations in `invariants.yaml`; allocate the next free `N` in the chosen scope.
- Test-file placement, tier, and naming throughout, per `.claude/rules/testing.md`.
- Whether the narrow world interface (D-79) is one interface or two (read vs mutate), provided `ListByParent` is unreachable from the facade either way.

### Deferred Ideas (OUT OF SCOPE)

- A populated-corpus re-run of the exposure audit — GitHub #4937, open and `awaiting-precursor`.
- Relaxing `owner_player_id` to the row's player (D-69's rejected option).
- Admin RPCs, A2's sort key and A3's username search — Phase 6.
- `CreateCharacter` (IDENT-01) and profile rendering (PROFILE-01, PROFILE-10a) — Phase 5.
- Rename + the approval dimension — backlog 999.20.
- A lint banning character-shaped proto struct literals outside the projection package — §2.6 deliberately did NOT mandate it; **MUST NOT** become grounds for relaxing the census.
- Registering `viewer:` / `admin_section:` in `knownPrefixes`.

</user_constraints>

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| **IDENT-02** | Player edits prose profile fields with server-enforced caps | §D — `world.MaxNameLength`/`MaxDescriptionLength` verified at `internal/world/validation.go:20-21`; `ValidateDescription` at `:94-110`. Caps enforced in the facade handler (D-82). |
| **IDENT-02a** | Player edits in-world `characters.description` | §D — `world.Service.UpdateCharacterDescription` at `internal/world/service.go:844`; already runs `ValidateDescription` on that path. No new capability needed. |
| **PROFILE-03** | Per-field visibility by omission | §B/§E — `profilevis.Evaluator.VisibleAttributes` (`internal/access/profilevis/profilevis.go:199`) is the shipped conjunction. Wire absence asserted by D-80's sentinel + byte scan. |
| **PROFILE-04** | Reachability as its own facet, not-found-equivalent | §B — `profilevis.Evaluator.Reachable` (`:130`), `seed:profile-reachable` (`internal/access/policy/seed.go:677`), `CHARACTER_PROFILE_NOT_FOUND` differential assertion per 01-SPEC §9.6.1. |
| **PROFILE-05** | Name/pronouns hard floor | §C — 01-SPEC §8.8 (`:1842-1867`) records that **v0.13 ships no enforcing mechanism**; the guarantee rests on operator discipline. Structural in the facade: name+pronouns are unconditional on a reachable profile. |
| **PROFILE-10** | Profile built exclusively from the viewer-filtered slice | §E — narrow interface exposing only `world.Service.ListPropertiesByParent` (`internal/world/service.go:1410`); `PropertyRepository.ListByParent` unreachable → compile error (D-79). |
| **EXT-06** | Media proto shape ships now, empty | §E — `buf.validate` IS available (`buf.yaml` deps, `buf.lock`); `(buf.validate.field).repeated.max_items` in use at `api/proto/holomush/scene/v1/scene.proto:434`. |

</phase_requirements>

---

## Summary

Phase 4 is a facade build over an ABAC vocabulary that **already ships in full**. Phase 2 delivered the
`viewer:` principal and its provider, the two tier-floor policies, the reachability policy, the five
viewer twins, the `seed:profile-public-read-property` widening, **and** — the finding most likely to be
missed — `internal/access/profilevis`, a complete caller-side implementation of 01-SPEC §8.5.1's
conjunction including the reachability-first ordering, the §8.10 infra-abort branch, and the two error
codes the handler needs. The read path is not designed in Phase 4; it is *called*.

Three things in the phase brief are wrong or incomplete against HEAD, and each would cost the planner a
task if uncorrected. (1) `POLICY_UNREGISTERED_ACTION_ATTRIBUTE` gates `action.<key>` **attribute
references inside `when {}` clauses**, not action **tokens** — a new `read_description` action token is
**not** a boot brick and needs **no** registration anywhere. (2) 01-SPEC §3.4 assigns Phase 4 a **second,
descriptor-derived census** ("the Phase-4 census") distinct from criterion 1's `go/ast` routing census;
the planner must scope both or explicitly defer one. (3) `world.Service.ListPropertiesByParent` applies
**only term B** of the conjunction — it is not, by itself, "the viewer-filtered slice" that criterion 5
and PROFILE-10 mean; composing it with `profilevis` is what satisfies them.

The extraction (criterion 1's first half) is genuinely mechanical: 24 `s.resolveAndGate(...)` and 21
`s.ownedCharacter(...)` call sites, all on `*SceneAccessServer`, all byte-identical under method
promotion from an embedded `playerGate`. The one non-mechanical detail is the guest-gate **message**
string, which three web tests assert verbatim.

**Primary recommendation:** treat this as *wiring a shipped pipeline*, not building one. Every task that
proposes new evaluation logic, a new audit, a new lint rule, or a new gate should be checked against the
in-tree asset it duplicates before it is planned.

---

## Corrections to the Brief

These contradict or materially refine premises in the research brief. Each is verified at HEAD.

| # | Brief premise | Verified reality |
|---|---|---|
| **C-1** | "an undeclared `action.*` key is now a **BOOT BRICK** (`compiler.go:189-201` hard-errors `POLICY_UNREGISTERED_ACTION_ATTRIBUTE`; declare in `action_schema.go`)" — implying a new ABAC **action token** must be registered. | The gate is on **attribute references**, not action tokens. `internal/access/policy/compiler.go:185-209` (`validateAttributes`) walks `collectAttrRefs(cb)` — condition-block attribute references — and hard-errors only when `ref.namespace == "action"`, i.e. a `when { action.foo == … }` reference. The declared key set is `attribute.ActionNamespaceSchema()` (`internal/access/policy/attribute/action_schema.go:43`), whose members are `name`, `dispatch_location`, and the `job.*` triple. `internal/testsupport/abactest/action_registration_internal_test.go:39-45` proves this by compiling `permit(principal, action, resource) when { action.typo_key == "x" };` and asserting that code. **An action TOKEN in `action in ["read_description"]` requires no registration and cannot brick boot.** Live precedent: Phase 2 shipped `read_profile_attribute` (`seed.go:651`) and Phase 3 shipped `retire`/`unretire` — none appear in any schema. `internal/command/types.go:127` `validActions` is a *different* registry, consumed only by plugin-manifest capability validation (`Capability.ValidateAction`, `types.go:210`); seed policies do not pass through it. |
| **C-2** | Criterion 1's census is the phase's census. | 01-SPEC §3.4 (`:443-454`) states: *"The **Phase-4 census's** expected set is the **union** of §3.3 and §9's character-returning rows, minus the rows §2.4 deletes."* That is §2.6's census (`:184-224`): descriptor-derived, keyed on the fully-qualified proto method name, compared against the §3 inventory. D-73's routing census (`go/ast`, `bodyReferencesSelector`) is a **different** test with a different universe. **Phase 4 is on the hook for both** unless the planner explicitly scopes §2.6's to a later phase — and D-72's whole rationale ("a declared-but-unimplemented RPC is a census member the moment the proto compiles") only makes sense if the §2.6 census is being built here. |
| **C-3** | `world.Service.ListPropertiesByParent` is "the filtered accessor PROFILE-10 mandates" and calling it satisfies criterion 5. | It applies **term B only**: `s.checkAccess(ctx, subjectID, "read", resource, prefixProperty)` at `internal/world/service.go:1421`. It does **not** evaluate term A (`read_profile_attribute`, the tier floor) and does **not** evaluate §8.4.2 reachability. `profilevis.Evaluator.VisibleAttributes` does all three (`internal/access/profilevis/profilevis.go:199-223`). "Built exclusively from the viewer-filtered property slice" requires the composition, not either half. |
| **C-4** | `.claude/rules/grpc-errors.md:58,:65` is drifted (stated in the brief; confirming and pinning the correct spelling). | Confirmed. `oops.AsOops` returns **two** values — `pkg/errutil/testing.go:17` reads `oopsErr, ok := oops.AsOops(err)`, and `internal/grpc/sceneaccess_service.go:135` reads `if oe, ok := oops.AsOops(err); ok && oe.Code() == "NOT_CONFIGURED"`. So `oops.AsOops(err).Code()` at rule line 65 does not compile. Per 01-SPEC §9.6.1 (`:2256-2274`), `Code()` also returns the **deepest** code, so both spellings are equivalent and neither asserts the wire. **Assert over the wire**: `status.Code(err)` and `status.Convert(err).Message()`. |
| **C-5** | ROADMAP scheduling note: *"Phase 4 should land the full SPEC-defined profile field set — including the PROFILE-06..09 fields that Phase 5 verifies — in one regeneration pass"* and *"`WebCheckSessionResponse.roles` (ADMIN-08) can be pulled into Phase 4's proto work"* (`.planning/ROADMAP.md:107-110`). | This is a **scheduling note, not a dependency** (its own words). D-72 is later and explicitly declines the RPC half. **Interpretation for the planner:** D-72 governs *RPCs*; the note's "profile field set" concerns *message fields*, which D-72 does not address. Landing all twelve `profile.*` fields in the response messages is compatible with D-72; declaring Phase 5/6 **RPCs** is not. Treat `WebCheckSessionResponse.roles` as optional and out of the requirement set. |
| **C-6** | Various `path:line` citations carried forward from earlier artifacts. | Two are drifted: 01-SPEC §8.10 (`:1891-1892`) cites `internal/world/service.go:1144-1171` for `ListPropertiesByParent`; it is now at **`:1394-1434`** (func at `:1410`). 01-SPEC §9.3 (`:2020`) cites `service.go:799-836` for `UpdateCharacterDescription`; it is now at **`:843-…`** (func at `:844`). Phase-2 audit set (4) header is at `:148` (not `:149`) and set (5) at `:163`. |

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Session resolution + guest gate + ownership | API / Backend (`internal/grpc`, `playerGate`) | — | INV-SCENE-63/64 live at the facade today; D-78 keeps them there. |
| Viewer-tier resolution (`anonymous`/`guest`/`player`) | API / Backend (facade) | — | D-83: the facade is the only layer holding the session; §9.1 requires the gateway compute nothing. |
| Reachability + per-attribute conjunction | API / Backend (`internal/access/profilevis`) | ABAC engine | Shipped; the facade calls it. Conjunction is the caller's, never the engine's (`profilevis.go:9-23`). |
| Property enumeration + term-B filter | API / Backend (`world.Service.ListPropertiesByParent`) | ABAC engine | Fail-closed three-outcome loop already in place (`service.go:1418-1433`). |
| In-world description read (off-location) | ABAC policy seed (`internal/access/policy/seed.go`) | API / Backend (new narrow projection) | D-75: new action + own projection; `GetCharacter` untouched. |
| Description write | API / Backend → `world.Service.UpdateCharacterDescription` | Database | IDENT-02a MUST reach the shipped command, never a parallel path (§9.3). |
| Length caps on prose fields | API / Backend (facade handler) | — | D-82: the oops code and §9.6 wire status live in the handler. |
| Web proxy (`Web*`) | Frontend Server (`internal/web`, ConnectRPC) | — | `.claude/rules/gateway-boundary.md`: protocol translation only; forwards, computes nothing. |
| Wire marshaling / field absence | Proto codegen (`pkg/proto`, `web/src/lib/connect`) | — | §8.9/§2.7: enforcement is absence from marshaled bytes. |

---

## Standard Stack

### Core — no new dependencies

Phase 4 adds **zero** third-party packages. Every library it needs is already a direct dependency.

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `google.golang.org/protobuf` | in `go.mod` | `proto.Marshal` for D-80's byte scan | Already used in ~40 test files (e.g. `internal/admin/readstream/decrypt_test.go:87`). |
| `buf.build/go/protovalidate` | `v1.2.0` (`go.mod:7`) | `(buf.validate.field).repeated.max_items = 10` on `gallery` | Declared in `buf.yaml` `deps` and pinned in `buf.lock`; in use at `api/proto/holomush/scene/v1/scene.proto:434`. |
| `github.com/samber/oops` | `v1.22.0` (per 01-SPEC §9.6.1) | Structured error codes | Repo convention. |
| `google.golang.org/grpc/status`, `codes` | in `go.mod` | Wire status mapping + §9.6.1 assertions | The facade precedent uses them throughout. |
| `connectrpc.com/connect` | in `go.mod` | `Web*` proxy handlers | `internal/web/scene_handlers.go` is the template. |
| `github.com/stretchr/testify` | in `go.mod` | assertions | Repo convention (`.claude/rules/testing.md`). |
| `go/ast`, `go/parser`, `go/token` (stdlib) | — | D-73's routing census | `test/meta/world_envelope_census_test.go:6-9`. |
| `google.golang.org/protobuf/reflect/protoregistry` + `protoreflect` (stdlib-adjacent) | in `go.mod` | §2.6's descriptor-derived census (C-2) | Precedent: `internal/plugin/luabridge/gen/luastub_test.go:25` uses `protoregistry.GlobalFiles.FindDescriptorByName`. |

**Installation:** none. Do not add packages.

## Package Legitimacy Audit

**Not applicable — this phase installs no external packages.** Every library named above is already a
direct dependency in `go.mod` / `buf.lock` at HEAD. No registry lookup was required and none was
performed; consequently no package in this document carries `[ASSUMED]` provenance.

---

## Architecture Patterns

### A. The shared-helper extraction (criterion 1 first half, IDENT-02)

#### A.1 — Definitions and call sites (verified `internal/grpc/sceneaccess_service.go`, 1038 lines)

| Symbol | Definition | Receiver | Signature |
|---|---|---|---|
| `ownedCharacter` | doc `:106-108`, func **`:109`** | `*SceneAccessServer` | `(ctx context.Context, playerID ulid.ULID, charIDStr string) (*world.Character, error)` |
| `resolveAndGate` | doc `:127-129`, func **`:130`** | `*SceneAccessServer` | `(ctx context.Context, rawToken string) (*auth.PlayerSession, error)` |
| `beginDispatch` | doc `:151`, func **`:152`** | `*SceneAccessServer` | `(ctx, verifiedChar *world.Character, playerID ulid.ULID) (context.Context, func(), error)` — **scene-specific; do NOT move** (it calls `s.pluginManager.BeginServiceDispatch(ctx, "core-scenes", …)`) |

**Call-site census (executable code only; doc-comment mentions excluded):**

- `s.resolveAndGate(...)` — **24 sites**: `:164, :193, :261, :293, :336, :366, :395, :425, :458, :488, :516, :545, :575, :605, :630, :669, :801, :819, :862, :907, :927, :956, :983, :1009`
- `s.ownedCharacter(...)` — **21 sites**: `:168, :197, :265, :297, :340, :370, :399, :429, :462, :492, :520, :549, :579, :609, :634, :700, :866, :931, :960, :987, :1013`

**Every one is `s.<name>(...)` on the `*SceneAccessServer` receiver.** Method promotion from an embedded
`playerGate` keeps all 45 byte-identical. **No call site fails byte-identity.** The struct fields to move
are `playerSessionRepo auth.PlayerSessionRepository`, `playerRepo auth.PlayerRepository`,
`charRepo auth.CharacterRepository` (`:51-53`); the remaining `SceneAccessServer` fields
(`sessionStore`, `coordinator`, `sceneClient`, `pluginManager`, `dekAdder`, `characterNameResolver`)
stay put.

#### A.2 — The one non-mechanical detail: the guest-gate message

`sceneaccess_service.go:145` returns:

```go
return nil, status.Error(codes.PermissionDenied, "guests cannot access scenes")
```

That literal is asserted verbatim in **three** places in `internal/web`:
`scene_handlers_test.go:468`, `status_interceptor_test.go:86,:101` and `:172,:186`. Two internal log
messages are likewise scene-flavored: `"scene access: list characters failed"` (`:116`) and
`"scene access: player lookup failed"` (`:141`).

The planner must choose and state one of:
1. **Keep the literal** on the shared gate — the character facade then denies guests with a message
   naming scenes. Zero test churn; a small wire-message inaccuracy.
2. **Parameterize** the message (a field on `playerGate`, e.g. `guestDenialMessage`) — preserves the
   scene wording for `SceneAccessServer` and lets the character facade supply its own. Zero test churn,
   one extra field.
3. **Genericize** to e.g. `"guests cannot access this surface"` — updates 3 test files.

Option 2 is the smallest correct change and is the recommendation. Whichever is chosen, note that a
generic message is *also* the §9.6 wire-opacity posture, so option 3 is not wrong — it merely costs
edits.

#### A.3 — The census precedent, read in full

`test/meta/world_envelope_census_test.go` (346 lines). The three functions D-73/D-78 depend on:

```go
// :162-180
func bodyReferencesSelector(body *ast.BlockStmt, recv, field string) bool {
	if body == nil { return false }
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok { return true }
		x, ok := sel.X.(*ast.Ident)
		if ok && x.Name == recv && sel.Sel.Name == field {
			found = true
			return false
		}
		return true
	})
	return found
}
```

- `serviceReceiverName(recv *ast.Field) (string, bool)` — `:145-158`; unwraps `*ast.StarExpr`, requires
  `ident.Name == "Service"`, requires `len(recv.Names) > 0`, returns `recv.Names[0].Name`.
  **For Phase 4 this must be generalized** to match `CharacterAccessServer` / `SceneAccessServer`
  rather than the hard-coded `"Service"`.
- `serviceMutatingMethods(t)` — `:115-141`; parses ONE file (`internal/world/service.go`) with
  `parser.ParseFile(fset, src, nil, 0)`, iterates `file.Decls`, skips non-`*ast.FuncDecl` and
  receiver-less decls, and calls `bodyReferencesSelector(fn.Body, recvName, "mutator")` at `:136`.
  Contrast `test/meta/world_caller_census_test.go`, whose header (`:56-57`) records that it *"parses
  every non-test .go file in internal/world/ rather than service.go alone, so relocating a command into
  a sibling file cannot carry it out of the universe."* **Phase 4 should follow the caller census's
  directory-walk shape, not the envelope census's single-file shape** — the facade will plausibly be
  split across files.
- The set-equality assertion — `:187-207`:

```go
	for name := range mutating {
		_, ok := registered[name]
		assert.Truef(t, ok, "world.Service.%s routes through the write executor but has no census descriptor …", name)
	}
	for name := range registered {
		_, ok := mutating[name]
		assert.Truef(t, ok, "census descriptor %q is not a world.Service method routing through the executor (stale descriptor)", name)
	}
```

> **Precision note.** This precedent is **two directional per-element assertions**, not a computed
> symmetric-difference diff. D-73 and 01-SPEC §2.6 (`:222-224`) both require *"a set-equality assertion
> producing a symmetric-difference diff on failure."* The precedent's *semantics* are set equality;
> its *failure rendering* is per-element. Phase 4 should compute and print the two difference sets
> (extra / missing) — a strict improvement, and what §2.6 mandates. This is not a new mechanism.

**The exemption-list idiom** to copy if ever needed:
`worldCallerExemptCommands()` (`world_caller_census_test.go:74-88`) — *"one commented member per
justifying decision, never an inline name comparison, so removing a member is a visible edit rather
than a silent loosening"*, plus a test asserting membership is exact. **D-72 is designed so Phase 4
needs no exemption list; do not add one preemptively.**

#### A.4 — Deriving the census universe from proto service descriptors (§2.6 / C-2)

01-SPEC §2.6 (`:189-199`) fixes the key as the **fully-qualified proto method name**
(`package.Service.Method`, e.g. `holomush.web.v1.WebService.WebListCharacters`), compared as an **exact
string**; explicitly **not** a substring/prefix match and **not** a Go handler identifier.

Two in-tree mechanisms can produce that set:

| Mechanism | Precedent | Notes |
|---|---|---|
| **`protoregistry.GlobalFiles`** — walk `FileDescriptor.Services()` → `ServiceDescriptor.Methods()` → `MethodDescriptor.FullName()` | `internal/plugin/luabridge/gen/luastub_test.go:25` uses `protoregistry.GlobalFiles.FindDescriptorByName`; `luatype_test.go:18` the same. `mustMessageDesc`/`fieldByPath` are the shape. | This is the true "generated service descriptors" reading of §2.6. Requires a blank import of each generated package so its `init()` registers. Gives real `MethodDescriptor.Output()` for the "response transitively contains a character-shaped message" predicate. |
| **Regex over `.proto` sources** | `test/meta/grpc_api_coverage_test.go:26-29` — `protoPackageDecl = regexp.MustCompile(`(?m)^package\s+([\w.]+)\s*;`)` and `protoServiceDecl = regexp.MustCompile(`(?m)^service\s+(\w+)\b`)`, joined into a package-qualified anchor by `protoService{name, anchor}` (`:42-46`) | Already enumerates *services* module-wide, not methods. Cheaper; but §2.6's "transitively contains a character-shaped message" predicate is a descriptor-graph walk that regex cannot do. |

**Recommendation:** use `protoregistry` for §2.6's census. `grpc_api_coverage_test.go` remains the
precedent for *how a meta-test enumerates the proto module*, and its `require.NotEmpty(t, services,
"no services found under api/proto — proto layout changed?")` guard (`:55`) is the anti-vacuity idiom
to copy.

---

### B. Wire-level absence and not-found-equivalence (criterion 2, PROFILE-03/04/05)

#### B.1 — No exact in-tree precedent for a marshaled-bytes absence assertion

Searched `internal/`, `test/` for `NotContains` over marshaled proto bytes. The **only** structurally
analogous assertion is:

```go
// internal/eventbus/history/plugin_downgrade_fence_test.go:636
assert.NotContains(t, buf.String(), "cleartext-leak", "the drop log MUST never carry payload plaintext")
```

— a distinctive-sentinel absence assertion, but against a **log buffer**, not marshaled bytes.
`proto.Marshal` in tests is ubiquitous (40+ call sites, e.g. `internal/admin/readstream/decrypt_test.go:87`,
`test/integration/eventbus_e2e/cross_tier_query_test.go:570`), so the two halves both exist; D-80's
combination of them does not. **This is not a reason to build machinery** — it is three lines of
stdlib. Report it to the planner as "compose two existing idioms", not "adopt a helper".

Shape:

```go
const withheldSentinel = "ZZSENTINEL-BIOGRAPHY-7f3a" // distinctive; cannot occur by accident
// … seed profile.biography with withheldSentinel, drive an anonymous viewer …
raw, err := proto.Marshal(resp)
require.NoError(t, err)
assert.NotContains(t, string(raw), withheldSentinel,
    "a field below the viewer's floor MUST be absent from the marshaled bytes (§8.9, PORTAL-10 rule 3)")
```

Why the sentinel is load-bearing (D-80, 01-SPEC §7.5 `:1210-1215`, §8.9 `:1869-1882`): for a proto3
non-optional string, *absent* and *present-and-empty* are indistinguishable after unmarshal. The bytes
are the only place the distinction survives. **A genuine RED** requires the pre-fix implementation to
populate the field; a test written against an empty fixture passes while the property is false — the
exact vacuity PORTAL-10 exists to prevent.

#### B.2 — Not-found-equivalence

Shipped opacity precedent, `internal/grpc/sceneaccess_service.go:109-125` (`ownedCharacter`): an
unparseable ULID, an infra failure, and a non-owned character all return
`status.Error(codes.NotFound, "character not found")` — **identical status and identical message**,
with the infra case logged internally first via `slog.ErrorContext` (`:116`). That is the shape.

01-SPEC §9.6 (`:2223-2229`) fixes `CHARACTER_PROFILE_NOT_FOUND` → `NotFound` as **one code for two
causes** deliberately. §9.6.1 (`:2249-2254`) mandates the **differential** assertion:

> drive an unreachable profile and a nonexistent character id through the same RPC and assert the two
> responses are identical across **status, message and body**. A one-sided "the unreachable profile
> returns NotFound" assertion is satisfied by an implementation that returns NotFound with a
> distinguishable message, which is the leak.

**Correct assertion spelling** (per C-4, §9.6.1 `:2243-2247`):

```go
assert.Equal(t, codes.NotFound, status.Code(errUnreachable))
assert.Equal(t, status.Convert(errUnreachable).Message(), status.Convert(errNoSuchID).Message())
assert.NotContains(t, status.Convert(errUnreachable).Message(), "CHARACTER_PROFILE_NOT_FOUND")
```

`errutil.AssertErrorCode` (`pkg/errutil/testing.go:15-20`) remains correct for asserting *which internal
code the handler produced* — it is simply not evidence about the wire and MUST NOT be cited as such.

`profilevis` already distinguishes the two outcomes the handler must map:
`ErrProfileUnreachable` / `CodeProfileUnreachable = "PROFILE_VISIBILITY_UNREACHABLE"` (policy answer)
vs `ErrEvaluationFailed` / `CodeEvaluationFailed = "PROFILE_VISIBILITY_EVALUATION_FAILED"` (outage)
— `profilevis.go:72-93`. The doc comment at `:79-84` is explicit that *"a caller that cannot tell them
apart renders an outage as a missing character."* The facade maps the first to
`CHARACTER_PROFILE_NOT_FOUND`/`NotFound` and the second to `Internal` with a generic message.

---

### C. Viewer tiers (criterion 3, PROFILE-04/05)

#### C.1 — Where "game-configured" configuration actually lives

**It is the ABAC seed policy text — not a settings table, not the `setting` plugin type.**
01-SPEC §8.6 (`:1729-1733`): *"The table below is **the whole configuration surface**."* Its rows are
governed attribute names; the seeded v0.13 defaults are transcribed into two policies:

```go
// internal/access/policy/seed.go:648-653
{
    Name:        "seed:profile-tier-floor-anonymous",
    Description: "Term A: profile attributes seeded at the anonymous floor are readable by every viewer rung (§8.2.1, §8.6)",
    DSLText:     `permit(principal is viewer, action in ["read_profile_attribute"], resource is property) when { principal.viewer.tier in ["anonymous", "guest", "player"] && resource.property.name in ["profile.pronouns"] };`,
    SeedVersion: 1,
},
```

and `seed:profile-tier-floor-guest` at `:654-659`, whose `resource.property.name in [...]` list carries
the other 21 names (`profile.rumors` … `profile.image.gallery.09`).

**Consequences the planner must internalize:**

- Only **two** floor policies exist; the `player` rung has **no** seeded member, and a third policy
  *cannot be written* because the DSL list grammar requires ≥1 literal (§8.6 `:1793-1810`). Plan 02-07
  shipped `TestAPlayerRungTierFloorPolicyIsRequiredExactlyWhenSpec86SeedsAName` as the re-entry guard
  (§8.6 `:1812-1820`) — **it already exists; do not author another.**
- **Totality rule** (§8.6 `:1765-1769`): names are matched as **whole strings**, exact enumeration; no
  glob/prefix/wildcard over `profile.*`. A name in no row **is denied, not defaulted**.
- **The two `characters`-column rows are not covered by these policies.** §8.6's closing note
  (`:1828-1830`): *"Name and the in-world description are columns, not `entity_properties` rows (§7.1),
  so they carry a tier floor but no row-keyed peer decision — §8.5.1's conjunction has only one term
  for them."* Both floor policies target `resource is property`, so **nothing today grants a `viewer:`
  principal any read on `characters.description`.** That gap is precisely what D-76's viewer twin
  closes (see §F). `name` needs no separate grant because §8.8 ties it to reachability structurally.

#### C.2 — Viewer-tier vocabulary (shipped by Phase 2 plan 02-03)

| Symbol | Location | Value |
|---|---|---|
| `access.ViewerTierAnonymous` | `internal/access/prefix.go:42` | `"anonymous"` |
| `access.ViewerTierGuest` | `internal/access/prefix.go:44` | `"guest"` |
| `access.ViewerTierPlayer` | `internal/access/prefix.go:46` | `"player"` |
| `access.ViewerSubject(tier, playerID string) string` | `internal/access/prefix.go:178` | switch on tier at `:180`/`:185`; panics on a **non-empty** playerID for anonymous and on an **empty** one for guest/player |
| `access.CharacterResource` / `PropertyResource` / `ProfileResource` | `prefix.go:201` / `:255` / `:282` | resource constructors |
| `attribute.ViewerTierProvider` | `internal/access/policy/attribute/viewer.go:62` | namespace `viewer`; schema keys `tier`, `player_id`, `has_player_id`, `roles`, `has_roles` (`:106-115`) |
| `parseViewerRung` | `viewer.go:179-217` | rejects an unrecognized tier with `INVALID_VIEWER_TIER` (`:189-191`, `:194-201`) |

The provider is **registered in production** — 02-13-SUMMARY records *"ViewerTierProvider registered in
`BuildABACStack` — `principal.viewer.*` now resolves in production."*

#### C.3 — Exhaustive `switch` with `default: deny` — the in-tree precedent

```go
// internal/world/lifecycle.go:83-92
func Selectable(s Status) bool {
	switch s {
	case StatusActive:
		return true
	case StatusRetired, StatusIdle:
		return false
	default:
		return false
	}
}
```

Phase 2 plan 02-04 shipped this as *"the ONE exhaustive selectability predicate with a denying default"*
and bound it to INV-WORLD-5. **This is the shape criterion 3 asks for**, applied to the tier rung.
`parseViewerRung` (`viewer.go:179-217`) is an if-chain, not a switch — it is the *parse* side; D-83's
facade-side *construction* switch is the new code and should follow `Selectable`.

D-83's mapping — no session → `anonymous`; session with `IsGuest` → `guest`; non-guest session →
`player` — reads `player.IsGuest`, the same field `resolveAndGate` gates on (`sceneaccess_service.go:144`).
**Preserve the omit-don't-sentinel rule** (`.claude/rules/abac-providers.md`): `access.ViewerSubject`
*panics* if a non-empty playerID is passed on the anonymous rung (`prefix.go:180-184`), so the
constraint is already enforced by construction — the switch must not pass one.

---

### D. Write path (criterion 4, IDENT-02 / IDENT-02a)

#### D.1 — `world.Service.UpdateCharacterDescription`

```go
// internal/world/service.go:843-…  (doc at :843)
func (s *Service) UpdateCharacterDescription(ctx context.Context, subjectID Caller, characterID ulid.ULID, description string) error {
	if s.characterRepo == nil {
		return oops.Code("CHARACTER_UPDATE_FAILED").Errorf("character repository not configured")
	}
	resource := access.CharacterResource(characterID.String())
	if err := s.checkAccess(ctx, subjectID, "write", resource, prefixCharacter); err != nil {
		return err
	}
	// … validation + mutate() seam …
```

- **Signature:** `(ctx, Caller, ulid.ULID, string) error` — the Phase 02.1 typed-`Caller` shape.
- **ABAC gate:** `checkAccess(ctx, subjectID, "write", character:<id>, prefixCharacter)` — matches §9.3's
  stated gate ("ABAC `write` on `character:<id>`").
- **Outbox:** §9.3 (`:2048-2053`) records this method as *the* shape for same-transaction outbox emission
  (INV-WORLD-1). The facade inherits it by calling the command.

**The seam.** `world.HumanCaller(subjectID string) Caller` (`internal/world/caller.go:88-90`) passes the
subject through **verbatim** — it *"never re-derives or re-prefixes the string it is handed, because that
same string becomes the world-change outbox envelope Actor."* So the facade constructs
`world.HumanCaller(access.CharacterSubject(charID))` for the owner-audience write. **This is the exact
seam; no new plumbing is required.** `world.Caller`'s zero value is inert and fails closed
(`caller.go:27-34`), so a misconstruction is a denial, not a bypass.

#### D.2 — Validation constants (D-82), verified verbatim

```go
// internal/world/validation.go:18-25
const (
	MaxNameLength        = 100
	MaxDescriptionLength = 4000
	MaxAliasCount        = 10
	MaxAliasLength       = 50
	MaxVisibleToCount    = 100
	MaxLockDataKeys      = 20
```

```go
// internal/world/validation.go:94-110
func ValidateDescription(desc string) error {
	if desc == "" {
		return nil // Description may be empty
	}
	if !utf8.ValidString(desc) {
		return &ValidationError{Field: "description", Message: "must be valid UTF-8"}
	}
	if len(desc) > MaxDescriptionLength {
		return &ValidationError{Field: "description", Message: fmt.Sprintf("exceeds maximum length of %d", MaxDescriptionLength)}
	}
	if hasControlCharsExceptWhitespace(desc) {
		return &ValidationError{Field: "description", Message: "cannot contain control characters (except newline/tab)"}
	}
	return nil
}
```

> **Caveat to surface to the planner:** `len(desc)` is **bytes, not runes**. A 4000-rune multibyte
> description is rejected at well under 4000 characters. That is shipped behavior; IDENT-02a inherits
> it, and IDENT-02's reuse of the same rule (D-82) inherits it too. Not a defect to fix in Phase 4 —
> but the facade's cap check should use the *same* byte semantics so the two paths cannot disagree,
> and the doc comment should say "bytes".

**IDENT-02a needs no new capability — verified.** `ValidateDescription` runs on the
`UpdateCharacterDescription` path (D-82's claim), so the 4000-byte cap, UTF-8 validity, and control-char
rejection are inherited by construction.

#### D.3 — Update-mask precedent (§9.5 adopts it verbatim)

```go
// internal/grpc/sceneaccess_service.go:838-845
var updateSceneMaskablePaths = map[string]struct{}{
	"title":            {},
	"description":      {},
	"visibility":       {},
	"pose_order_mode":  {},
	"tags":             {},
	"content_warnings": {},
}
```

Applied in `UpdateScene` (`:846-902`): closed-allowlist loop → `codes.InvalidArgument` on an unlisted
path (`:872`); then the **empty-mask short-circuit** at `:876-879` returning success **after** ownership
is verified, *"so non-web callers cannot drive downstream validation/store work through a no-op
request."* Both behaviors are §9.5's gate ordering; copy them.
§9.6 (`:2220`) names `internal/grpc/sceneaccess_service.go:872` as the precedent for
`CHARACTER_MASK_PATH_UNSUPPORTED`.

---

### E. Property slice, narrow interface, and the proto (criterion 5, PROFILE-10 / EXT-06)

#### E.1 — The read pipeline, corrected

```
viewer session
   └─ facade: exhaustive tier switch (D-83)  →  access.ViewerSubject(tier, playerID)
        ├─ profilevis.Evaluator.Reachable(viewerSubject, characterID)      [§8.4.2, ONE evaluation]
        │     └─ DENY → CHARACTER_PROFILE_NOT_FOUND (identical to no-such-id)
        ├─ world.Service.ListPropertiesByParent(ctx, HumanCaller(viewerSubject), "character", id)
        │     └─ term B only: checkAccess(read, property:<id>)   [service.go:1421]
        └─ profilevis.Evaluator.VisibleAttributes(viewerSubject, characterID, properties)
              └─ per row: term A (read_profile_attribute) AND term B (read)  [profilevis.go:169-179]
```

`ListPropertiesByParent`'s three-outcome loop (`service.go:1418-1433`) — permit appends,
`ErrPermissionDenied` filters silently, `ErrAccessEvaluationFailed` **aborts** — is the §8.10 shape, and
`VisibleAttributes` follows it (`profilevis.go:193-196`).

**Term B is evaluated twice** under this composition (once in `ListPropertiesByParent`, once in
`AttributeVisible`). That is **correct but redundant** — `A ∧ B ∧ B ≡ A ∧ B`. Two options, and the
planner should pick one explicitly:
- **(a) Accept the redundancy.** Simplest; both gates are shipped; the facade composes them. The
  redundant call is a cost, not a hazard.
- **(b) Enumerate with a non-viewer caller** and let `profilevis` be the sole gate. **Do not do this
  with `world.SystemCaller()`** — it requests the S1 bypass (`caller.go:96-107`) and would hand the
  facade every row, directly contradicting criterion 5's "built exclusively from the viewer-filtered
  property slice."

**Recommendation: (a).** It is the only option that keeps the compile-fence (D-79) meaningful *and*
keeps criterion 5 literally true.

> **Do NOT** collapse `profilevis`'s two evaluations into one. Its package doc (`profilevis.go:20-23`)
> warns: *"A future reader looking at two Evaluate calls against one resource will be tempted to
> 'optimize' them into one; doing so silently reopens the hole §8.5.1.1 exists to close, and the hole
> has no symptom of its own."* Plan 02-08 shipped a D-04 regression test that goes RED if term B is
> dropped.

#### E.2 — `ListByParent` call sites and the compile fence

| Symbol | Location |
|---|---|
| `world.Service.ListPropertiesByParent` | `internal/world/service.go:1410` (doc `:1394-1409`) |
| `PropertyRepository.ListByParent` (the raw repo call it wraps) | invoked at `internal/world/service.go:1414`: `all, err := s.propertyRepo.ListByParent(ctx, parentType, parentID)` |

**The narrow-interface precedent D-79 copies, verbatim** (`internal/grpc/sceneaccess_service.go:27-31`):

```go
// sceneAccessPluginManager is the narrow interface SceneAccessServer needs from
// the plugin manager — only BeginServiceDispatch.
type sceneAccessPluginManager interface {
	BeginServiceDispatch(ctx context.Context, pluginName string, actor core.Actor, ownerPlayerID string) (context.Context, func(), error)
}
```

Applied to Phase 4: declare (in `internal/grpc`, at the consumer) an interface carrying **only**
`ListPropertiesByParent`, `GetCharacter`-equivalent reads, and `UpdateCharacterDescription`. Because
`PropertyRepository` is not in the facade's type set, `s.propertyRepo.ListByParent(...)` **does not
compile**. Zero rules, zero suppression vocabulary. D-79's discretion note allows one interface or two
(read vs mutate) — a read/mutate split is the more idiomatic reading of the precedent and costs nothing.

`sceneDEKAdder` (`sceneaccess_service.go:33-40`) is the second in-file instance of the same idiom, which
confirms it as the file's convention rather than a one-off.

#### E.3 — `entity_properties.owner` is a scalar (D-69's load-bearing fact)

```sql
-- internal/store/migrations/000001_baseline.sql:354-368
CREATE TABLE entity_properties (
    id            TEXT PRIMARY KEY,
    parent_type   TEXT NOT NULL,
    parent_id     TEXT NOT NULL,
    name          TEXT NOT NULL,
    value         TEXT,
    owner         TEXT,
    visibility    TEXT NOT NULL DEFAULT 'public'
                  CHECK (visibility IN ('public', 'private', 'restricted', 'system', 'admin')),
    ...
    CONSTRAINT entity_properties_parent_name_unique UNIQUE(parent_type, parent_id, name),
```

`owner TEXT` at **`:360`**; the UNIQUE constraint at **`:368`**. Both CONTEXT citations confirmed exactly.
`visibility`'s closed vocabulary is `('public','private','restricted','system','admin')` — note
**`system`** is a fifth value the §8.6 table does not discuss.

`internal/access/policy/attribute/property.go:206-227` carries the D-27 rationale in prose, including
the line D-71 is meant to pin:

> *"Do not 'simplify' the ALL branch into an ANY branch for symmetry — that one-line diff reads as
> cleanup and reintroduces the widening. See 02-CONTEXT.md D-27."*

and, at `:214-217`, the exact consequence D-69 turns on:

> *"owner_player_id and visible_to_players feed PERMITS → the ALL direction. A player enters only when
> EVERY character of theirs appears in the row's field. For the single-valued owner that reduces to
> 'the owning character is that player's only character'."*

The shipped viewer twin this makes unsatisfiable for multi-alt players (`seed.go:761-766`):

```go
{
    Name:        "seed:viewer-property-private-read",
    Description: "Term B (viewer path): private properties readable only by the owning player (derived peer, D-27 ALL direction)",
    DSLText:     `permit(principal is viewer, action in ["read"], resource is property) when { resource.property.visibility == "private" && resource.property.owner_player_id == principal.viewer.player_id };`,
    SeedVersion: 1,
},
```

**D-71's binding test** therefore asserts: a two-alt player, viewing via `viewer:player:<id>`, does
**not** see a `visibility='private'` row on either of their characters. That is a *property of the
shipped policy*, so the test is green from day one — its value is regression-pinning, which is exactly
what D-71 says it is.

#### E.4 — Proto: locations, generation, lint

| Concern | Fact |
|---|---|
| Public module | `api/proto` (`buf.yaml:22-23`, name `buf.build/holomush/holomush`). Packages present: `admin, channel, comm, content, control, core, eventbus, plugin, scene, sceneaccess, web, world`. **A new `characteraccess` package is a new directory** under `api/proto/holomush/characteraccess/v1/`. |
| Go output | `pkg/proto/holomush/<pkg>/v1/` + `<pkg>v1connect/` |
| TS output | `web/src/lib/connect/**/*.ts` (`Taskfile.yaml:694-695`) |
| Generate | `task proto` → `buf generate` then `buf generate --template buf.gen.internal.yaml` (`Taskfile.yaml:576-585`); `task web:generate` → `buf generate` from `web/` (`:686-695`) |
| Lint | `task lint:proto` → `buf lint`, `buf format --diff --exit-code`, `go test ./test/meta/ -run TestProtoCommentsNoNameEcho` (`Taskfile.yaml:767-772`) |
| Lint rules | `buf.yaml` `lint.use: [STANDARD, COMMENTS]` — **COMMENTS is unconditional**; no exemption mechanism. Per `.claude/rules/proto-doc-comments.md`, every message, field, RPC, service, enum and enum value needs a **Go-grounded** leading comment, and a name-echo comment is rejected. |
| Commit contract | CLAUDE.md: *"After changing `api/proto/**` schemas → run `task proto && task web:generate` → commit `pkg/proto/**/*.pb.go` + web `*_pb.ts`"* in the **same change** or CI fails a stale-diff check. |
| Docs coverage | `test/meta/grpc_api_coverage_test.go:51-74` asserts **every** declared service renders a section in `site/src/content/docs/reference/grpc-api.md`. **A new `CharacterAccessService` makes this test RED until `task docs:proto` is run and its output committed.** This is a real, easily-missed task. |

**`max_items` is real and available — verified twice:**
- Declared: `buf.yaml` `deps: [buf.build/googleapis/googleapis, buf.build/bufbuild/protovalidate]`, pinned in `buf.lock`.
- In use: `api/proto/holomush/scene/v1/scene.proto:434` — `repeated string tags = 7 [(buf.validate.field).repeated.max_items = 32];` (also `:436, :610, :612`, `web.proto:1472,:1475`, `sceneaccess.proto:843,:846`).
- Upstream field exists: `MaxItems *uint64` in the vendored `buf/validate/validate.pb.go:13348`, documented example `repeated string value = 1 [(buf.validate.field).repeated.max_items = 3];`.

So §9.7's mandated `repeated ProfileImage gallery [(buf.validate.field).repeated.max_items = 10];`
(01-SPEC `:2290`) compiles as written. The message shape (§9.7 `:2286-2290`): `ProfileImage
{ media_id, alt_text, content_warning }` — three fields, all strings; `primary_image` a single
`ProfileImage`. **Zero upload behavior ships** (`:2292-2303`): no uploader, no storage backend, no
`media_id` minting; `media_id` is an opaque string whose format v0.13 does not fix.

#### E.5 — The `Web*` proxy pattern

`internal/web/scene_handlers.go:334-364` is the template. Structure: nil-client guard →
`connect.CodeUnimplemented`; token read from `req.Header().Get(headerInjectSessionToken)` (**not** from
the request body); `context.WithTimeout(ctx, rpcTimeout)`; field-by-field forward; on error
`errutil.LogErrorContext` then `return nil, err //nolint:wrapcheck // gRPC status errors pass through
as-is`; on success `connect.NewResponse(&webv1.…{…})`. The gateway **computes nothing** — consistent
with `.claude/rules/gateway-boundary.md` and §9.1.

Wiring precedents: `cmd/holomush/sub_grpc.go:826` (`NewSceneAccessServer`) and `:849`
(`RegisterSceneAccessServiceServer`); test harness `internal/testsupport/integrationtest/harness.go:1173`
(`(*Server).NewSceneAccessServer`) and `session.go:719`.

---

### F. The ABAC permit (criterion 6, D-29 / D-75 / D-76)

#### F.1 — `seed:player-character-colocation`, verbatim

```go
// internal/access/policy/seed.go:50-55
{
    Name:        "seed:player-character-colocation",
    Description: "Characters can read co-located characters",
    DSLText:     `permit(principal is character, action in ["read"], resource is character) when { resource.character.location == principal.character.location };`,
    SeedVersion: 2,
},
```

The `when` clause is what denies the off-location read. 01-SPEC §7.4 (`:1184-1191`) records the reading
that governs D-75: this policy *"gates **where a viewer has to be standing** — it does not gate **who
may know**. Treating it as a privacy boundary would retrofit a meaning it never carried."*

#### F.2 — What Phase 4 adds, and where

D-75/D-76 require **two** additive permits (a `character`-flavored one and a `viewer`-flavored twin) on
`resource is character` with a **new narrow action**, plus a new read path with its own projection.

**Registration sites — the corrected answer (see C-1):**

| Thing | Where it goes | Required? |
|---|---|---|
| The two new seed policies | `internal/access/policy/seed.go`, appended at a **new `SeedVersion`** (never editing a shipped policy — `04-CONTEXT` "Additive seeding"; the rationale is transcribed at `seed.go:794-798`) | **Yes** |
| The action **token** (`read_description`) | **Nowhere.** No schema, no registry. | **No** — C-1 |
| `attribute.ActionNamespaceSchema()` (`internal/access/policy/attribute/action_schema.go:43`) | Only if a policy's `when {}` clause references `action.<key>`. D-75's permits are unconditional, so they reference none. | **No** |
| `internal/command/types.go:127` `validActions` | Plugin-manifest capability validation only. Seed policies never traverse it. Precedent: `read_profile_attribute`, `retire`, `unretire` are all absent from it and all work. | **No** |
| The `abac-reviewer` gate | Fires on any change touching `internal/access/` (`.claude/agents/abac-reviewer.md:4`, `:36`). | **Yes — mandatory before push** |

**Do not add an action-token registry to satisfy a premise that does not hold.** If the planner wants a
gate proving the new action is spelled identically in the seed and in the Go handler, note that
`internal/testsupport/abactest` (`NewSeedEngine`, `abactest.go:68`) already compiles the **full**
`policy.SeedPolicies()` corpus in the **unit** tier, so a malformed seed fails `task test` today.

#### F.3 — The projection criterion 6 forbids widening onto

```go
// internal/world/grpc_server.go:127-138
func characterToProto(c *Character) *worldv1.CharacterInfo {
	info := &worldv1.CharacterInfo{
		Id:          c.ID.String(),
		PlayerId:    c.PlayerID.String(),
		Name:        c.Name,
		Description: c.Description,
	}
	if c.LocationID != nil {
		info.LocationId = c.LocationID.String()
	}
	return info
}
```

`worldv1.CharacterInfo` fields (`api/proto/holomush/world/v1/world.proto:77-91`): `id = 1`,
`player_id = 2`, `name = 3`, `description = 4`, `location_id = 5`. D-75's rejected alternative
(narrowing this message) would break movement and presence rendering — `location_id`'s own doc comment
(`world.proto:87-89`) states it *"Corresponds to the nullable `world.Character.LocationID` field."*
**The new path returns its own message carrying name + description only; `characterToProto` is not
touched.**

#### F.4 — The audit obligation is already discharged (D-77) — verified

`.planning/phases/02-abac-schema-vocabulary/02-AUDIT-profile-public-read.sql` (235 lines, seven result
sets). Section headers verified by exact line:

| Set | Line | Covers |
|---|---|---|
| (1) | `:87` | Public character property rows by name |
| (2) | `:104` | The same set, totalled |
| (3) | `:117` | Names outside §8.6's enumeration |
| **(4)** | **`:148`** | **Character in-world descriptions** — `count(*)`, `count(*) FILTER (WHERE description <> '')`, `max(length(description))` `FROM characters` (`:155-160`) |
| **(5)** | **`:163`** | **Descriptions on guest-provisioned characters** — `count(*) FILTER (WHERE pl.is_guest AND ch.description <> '')`, `FROM characters ch LEFT JOIN players pl` (`:171-176`) |
| (6) | `:178` | Per-row property ledger |
| (7) | `:218` | Per-row description ledger |

Set (4)'s own header states the rationale (`:151-155`): *"§8.6 seeds the in-world description at the
`anonymous` floor (D-13), so every non-empty description counted here becomes readable by a logged-out
visitor on the public web."* The file is read-only by construction (`:9-14`) and emits no player text
(`:23-31`).

`02-AUDIT-RESULT.md` records a **measured zero** against a kopia-restored sandbox at goose schema level
53 (`:8`, `:21`, `:29`, `:39-41`), with its own stated limit (`:153-163`): *"The audited corpus is
small. Three characters, two of them guest-provisioned … it is not, and cannot be, evidence about a
future populated production corpus."*

GitHub **#4937** verified live: `state: OPEN`, title *"Re-run the PROFILE-11 exposure audit against a
populated corpus before Phase 4's description widening"*, labels `priority::medium`, `audit`,
`awaiting-precursor`.

**Conclusion: criterion 6's audit obligation is met by citation. The planner MUST NOT author a Phase-4
audit query (D-77).** The only Phase-4 action is a comment on #4937; do not close it.

> Also present and **not** to be re-run: `02-AUDIT-detail-operator-only.sql` (the operator-only
> adjudication companion — file committed, output never) and `02-REMEDIATION.sql` (Phase 2's
> approval-gated remediation, which Phase 2 left unused).

---

### G. Invariants and gates

#### G.1 — Registry state

`docs/architecture/invariants.yaml`. Next free `N` per relevant scope, verified by enumerating all ids:

| Scope | Highest existing | Next free | Boundary (`invariants.yaml`) |
|---|---|---|---|
| `INV-PRIVACY` | **11** (`:2180`) | **12** | *"Privacy-relevant gating on reads … web profile read-surface disclosure shape (existence non-disclosure, minimum-identity floor). Does NOT include: ABAC policy evaluation (→ INV-ACCESS)"* (`:194-197`) |
| `INV-ACCESS` | **14** (`:2350`) | **15** | *"ABAC policy evaluation, attribute provider invariants, seed policy shape, authorization decisions. Does NOT include: stream-access gating at gRPC boundary (→ INV-EVENTBUS)"* (`:571-573`) |
| `INV-COMMAND` | **3** (`:4287`) | 4 | not expected to be touched |

**Recommendation for D-71's scope:** the invariant is about a **derived attribute peer's derivation
direction** — an attribute-provider property. `INV-ACCESS`'s boundary explicitly names *"attribute
provider invariants"*; `INV-PRIVACY`'s explicitly **excludes** ABAC policy evaluation. → **`INV-ACCESS-15`.**

Existing invariants this phase touches:

| Id | Line | Binding | Relevance |
|---|---|---|---|
| `INV-PRIVACY-9` | `:2164` | **pending** | *"A character profile below its configured reachability floor returns a not-found-equivalent whose wire shape is identical to the response for a character id that does not exist…"* — **criterion 2 / PROFILE-04 is exactly this. Phase 4 can flip it to `bound`.** |
| `INV-PRIVACY-10` | `:2171` | **pending** | *"If a viewer can reach a character profile at all, that profile carries name and pronouns…"* — **criterion 3 / PROFILE-05. Phase 4 can flip it to `bound`.** Caveat: §8.8 (`:1855-1867`) records that v0.13 ships **no mechanism** enforcing the config-side half; a binding test can only prove the *facade* half. Bind honestly or leave pending — do **not** fabricate. |
| `INV-PRIVACY-11` | `:2180` | bound | Admin-section opacity; the *gate-then-distinguish* ordering is the pattern the profile handler should mirror. |
| `INV-SCENE-63/64` | (scene scope) | — | The ownership / guest-gate invariants the extracted `playerGate` carries. Their doc references live in `sceneaccess_service.go` comments; **the extraction must carry those references with the code.** |

**Hand-registration is mandatory.** `.claude/rules/invariants.md` — the orphan check walks only
`docs/superpowers/specs/`, so a `.planning/`-origin `INV-*` id is never auto-caught. The registry
already carries the precedent comment at `invariants.yaml:2177-2178`:
*"Hand-registered (D-07): the orphan check walks only docs/superpowers/specs/, so a .planning/-origin
spec's INV ids are never auto-caught."*

Workflow: add the entry → annotate the asserting test `// Verifies: INV-ACCESS-15` → set
`binding: bound` + `asserted_by:` → `go run ./cmd/inv-render` → run
`task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`.

#### G.2 — Pre-push gates

| Gate | Fires? | Trigger |
|---|---|---|
| `abac-reviewer` | **YES** | *"MUST run alongside `/gsd-code-review` for any change touching `internal/access/`"* (`.claude/agents/abac-reviewer.md:4`, `:36`). D-76 adds two seeds → mandatory. Invocation: `/holomush-dev:review-abac`. |
| `crypto-reviewer` | **NO** | Its trigger paths (CLAUDE.md) are `internal/eventbus/crypto/`, `internal/eventbus/codec/`, `internal/eventbus/history/dispatcher.go`, `internal/eventbus/history/cold_postgres.go`, `internal/plugin/event_emitter.go::Emit`, `internal/eventbus/audit/projection.go`, plugin manifest `crypto.emits`, migrations on `crypto_keys`/`events_audit`. Phase 4 touches none. |
| `gsd-plan-checker` / `gsd-verifier` | YES | GSD loop. |
| `branch-readiness-check` | — | `.claude/agents/branch-readiness-check.md:56` cross-checks that `abac-reviewer` ran. |

#### G.3 — `pr-prep` lane

`Taskfile.yaml:1071-1105`. `task pr-prep` is the **fast lane** (bats → schema → license/lint/fmt →
unit → build); it auto-delegates to `pr-prep:docs` on a docs-only diff and to `pr-prep:full` when
`HOLOMUSH_PR_PREP_FORCE_FULL=1`. `pr-prep:full` (`:1147-…`) adds integration + E2E under `flock`.

**Does Phase 4's diff need `pr-prep:full`? Yes.** It touches `internal/world` (interface consumers),
`internal/access` (seeds), `internal/grpc` (facade), `internal/web` (proxies) and the integration
harness. The repo rule is explicit: *"MUST run `task test:int` on refactors — `task test` does NOT
compile integration files, so refactors of shared types break silently otherwise."* The `playerGate`
extraction is exactly such a refactor. `Integration Test` + `E2E Test` are required CI checks on `main`
regardless.

Scoping (verified against `Taskfile.yaml`, per the brief's warning about stale prose):
`test:int` interpolates `{{.CLI_ARGS | default "./..."}}`, so `task test:int -- ./test/integration/access/...`
scopes the run. `task test:cover` does **not** consume `CLI_ARGS`.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---|---|---|---|
| Tier-floor ∧ row-keyed conjunction | A new evaluator in the facade | `profilevis.Evaluator.AttributeVisible` (`profilevis.go:157`) | Ships the two-call shape, the both-terms-always-evaluated rule, and the §8.10 infra branch. Collapsing to one call silently reopens §8.5.1.1's hole. |
| Reachability check | A "did any field clear its floor" heuristic | `profilevis.Evaluator.Reachable` (`:130`) | Its doc (`:185-191`) explains why the heuristic pins reachability at `anonymous` forever and makes §8.7 unfireable. |
| Per-property viewer filter | A hand-written filter loop | `world.Service.ListPropertiesByParent` (`service.go:1410`) | Fail-closed three-outcome loop already in place. |
| The exposure audit | A new Phase-4 SQL artifact | Cite `02-AUDIT-profile-public-read.sql` sets (4) `:148` and (5) `:163` (D-77) | Already committed, re-runnable, read-only, and its zero is recorded. #4937 tracks the populated-corpus re-run. |
| Criterion 5 enforcement | A `gorules` analyzer or a `test/meta` AST test | A narrow consumer-side interface (`sceneaccess_service.go:27-31` precedent) | D-79: the compiler is strictly stronger; a second gate costs a rule, a suppression vocabulary and a maintenance surface. |
| Length caps | New profile-specific constants | `world.MaxNameLength` / `MaxDescriptionLength` / `ValidateDescription` (`validation.go:20-21`, `:94`) | D-82. New numbers drift. |
| AST census scaffolding | A new predicate | `bodyReferencesSelector` (`world_envelope_census_test.go:162`), `findRepoRoot`, the `worldCallerExemptCommands` idiom | D-78 chose the embedded-struct shape *because* it is what this predicate already reads. |
| An action-token registry | A new schema/allowlist for `read_description` | Nothing — none is required (C-1) | `read_profile_attribute`, `retire`, `unretire` are all unregistered and all work. |
| Real-engine test setup | A hand-built policy corpus | `abactest.NewSeedEngine(t, providers...)` (`abactest.go:68`) | Compiles the full `SeedPolicies()` corpus in the unit tier. |

**Key insight:** every criterion in this phase names a *property to establish*. Four of the six already
have an in-tree mechanism that establishes them. The failure mode this phase is most exposed to is
building a second one.

---

## Runtime State Inventory

The `playerGate` extraction is a refactor, so the five categories are answered explicitly.

| Category | Items Found | Action Required |
|----------|-------------|-----------------|
| **Stored data** | **None.** The extraction moves Go struct fields; no database column, key, or ID changes. New ABAC seeds are additive rows in `access_policies` written at a **new `SeedVersion`** — no existing row is edited. Verified: no migration is required by any part of this phase. | none (seeding is the normal additive path) |
| **Live service config** | **None.** No n8n/Datadog/Cloudflare surface. Operator-authored `access_policies` rows are not touched (additive seeding is precisely the mechanism that avoids colliding with an admin-customized row — `seed.go:794-798`). | none |
| **OS-registered state** | **None** — no scheduler, pm2, systemd or launchd registration involved. | none |
| **Secrets / env vars** | **None.** `HOLOMUSH_PR_PREP_FORCE_FULL` is the only env var in play and it is a developer toggle. | none |
| **Build artifacts** | **Yes — three.** (1) `pkg/proto/**/*.pb.go` + `web/src/lib/connect/**/*_pb.ts` become stale the moment the proto changes; CI has a stale-diff check. (2) `site/src/content/docs/reference/grpc-api.md` becomes stale on a new service — `test/meta/grpc_api_coverage_test.go:51-74` goes RED until `task docs:proto` output is committed. (3) `docs/architecture/invariants.md` is generated from the YAML — `go run ./cmd/inv-render` after the D-71 entry. | run `task proto && task web:generate`, `task docs:proto`, `go run ./cmd/inv-render`; commit all output in the same change |

---

## Common Pitfalls

### Pitfall 1: Treating `ListPropertiesByParent` as the whole filter
**What goes wrong:** the profile publishes every row whose *row-keyed* policy permits, ignoring the
tier floor — i.e. a `visibility='public'` `profile.biography` row reaches an **anonymous** viewer even
though §8.6 seeds it at `guest`.
**Why it happens:** the method's name and doc comment ("the subset of properties the principal is
permitted to read") read like the complete answer, and `04-CONTEXT` names it as "the filtered accessor
PROFILE-10 mandates."
**How to avoid:** compose it with `profilevis.VisibleAttributes` (§E.1).
**Warning signs:** the facade has exactly one ABAC-touching call on the read path; `profilevis` is not
imported.

### Pitfall 2: Collapsing `profilevis`'s two evaluations
**What goes wrong:** term A becomes *additive to* term B rather than conjunctive with it; a row carrying
`visibility='private'` is published to every viewer that clears the name's floor.
**Why it happens:** two `Evaluate` calls against the same resource read as an obvious optimization.
**How to avoid:** don't. Plan 02-08's D-04 regression test goes RED if term B is dropped.
**Warning signs:** any diff to `internal/access/profilevis/profilevis.go:169-179`.

### Pitfall 3: Building a Phase-4 audit query
**What goes wrong:** duplicate artifact; the anti-pattern D-77 exists to record.
**How to avoid:** cite `02-AUDIT-profile-public-read.sql` sets (4)/(5); comment on #4937; author nothing.

### Pitfall 4: Registering the new action token somewhere
**What goes wrong:** a wasted task, and possibly a *wrong* one — adding `read_description` to
`internal/command/types.go` `validActions` widens the plugin-manifest capability vocabulary for no
reason.
**Why it happens:** `POLICY_UNREGISTERED_ACTION_ATTRIBUTE`'s name reads like it covers actions.
**How to avoid:** C-1. Verify by grepping for `read_profile_attribute` outside `seed.go`/`profilevis` —
there is no registration.

### Pitfall 5: A vacuous criterion-2 test
**What goes wrong:** the absence assertion passes on an empty fixture, which is PORTAL-10's named
failure (*"a private-field test passes on an empty fixture"*, REQUIREMENTS `:64-68`).
**How to avoid:** the sentinel MUST be a *seeded, non-empty, distinctive* value, and the RED must be
demonstrated against a pre-fix implementation that populates it.

### Pitfall 6: A one-sided not-found assertion
**What goes wrong:** "the unreachable profile returns NotFound" passes against an implementation that
returns NotFound with a **distinguishable message** — the leak.
**How to avoid:** the differential assertion (§9.6.1 `:2249-2254`) over status **and** message **and**
body.

### Pitfall 7: `errutil.AssertErrorCode` cited as wire evidence
**What goes wrong:** an opacity contract is "proven" by a chain-walking assertion that resolves the
deepest code while the wire carries something else.
**How to avoid:** §9.6.1's table. `errutil.AssertErrorCode` stays correct for *which internal code the
handler produced*.

### Pitfall 8: Forgetting `task docs:proto`
**What goes wrong:** `test/meta/grpc_api_coverage_test.go` goes RED on a new service with a message
about a missing doc section — confusing if you're looking at the facade.
**How to avoid:** regenerate and commit `site/src/content/docs/reference/grpc-api.md`.

### Pitfall 9: `task test` alone after the extraction
**What goes wrong:** integration files (`//go:build integration`) do not compile under `task test`; the
`playerGate` move silently breaks `internal/testsupport/integrationtest/harness.go:1178` and
`session.go:719`.
**How to avoid:** `task test:int` on this refactor. Repo MUST.

### Pitfall 10: A new version-bearing `###` heading in a planning artifact
**What goes wrong:** `extractCurrentMilestone` truncates the active-milestone scope; phases silently
drop out. D-74 requires editing 01-SPEC §9.3/§9.4.2 text — do it as a **narrowly-scoped `Edit` of
existing tool-written text**, never a structural change (`.claude/rules/planning-artifacts.md`).

### Pitfall 11: `len(desc)` is bytes
**What goes wrong:** the facade's own cap check uses runes while `ValidateDescription` uses bytes; a
value passes the facade and is rejected downstream with a different code.
**How to avoid:** §D.2 — match the shipped byte semantics exactly.

---

## Code Examples

### The narrow-interface fence (D-79), modeled on `sceneaccess_service.go:27-31`

```go
// characterAccessWorldReader is the narrow interface CharacterAccessServer needs
// from world.Service for reads — only the VIEWER-FILTERED property accessor and
// the character reads. PropertyRepository.ListByParent is deliberately ABSENT:
// a direct repo call from this facade does not compile (PROFILE-10, criterion 5).
type characterAccessWorldReader interface {
	ListPropertiesByParent(ctx context.Context, caller world.Caller, parentType string, parentID ulid.ULID) ([]*world.EntityProperty, error)
	GetCharacter(ctx context.Context, caller world.Caller, id ulid.ULID) (*world.Character, error)
}

// characterAccessWorldWriter is the mutate half (D-79 permits one interface or two).
type characterAccessWorldWriter interface {
	UpdateCharacterDescription(ctx context.Context, caller world.Caller, characterID ulid.ULID, description string) error
}
```

### The tier switch (D-83), modeled on `world.Selectable` (`lifecycle.go:83-92`)

```go
// resolveViewerTier maps a resolved session to a viewer rung. The default arm
// DENIES: an unrecognized session shape yields no viewer principal at all rather
// than a rung the policy corpus might clear (criterion 3).
//
// player_id is OMITTED on the anonymous rung, never empty-stringed —
// access.ViewerSubject panics on a non-empty id there (internal/access/prefix.go:180-184),
// and .claude/rules/abac-providers.md is the governing rule.
func resolveViewerTier(ps *auth.PlayerSession, player *auth.Player) (subject string, ok bool) {
	switch {
	case ps == nil || player == nil:
		return access.ViewerSubject(access.ViewerTierAnonymous, ""), true
	case player.IsGuest:
		return access.ViewerSubject(access.ViewerTierGuest, ps.PlayerID.String()), true
	case !player.IsGuest:
		return access.ViewerSubject(access.ViewerTierPlayer, ps.PlayerID.String()), true
	default:
		return "", false // deny
	}
}
```

### The read composition (§E.1)

```go
viewerSubject, ok := resolveViewerTier(ps, player)
if !ok {
	return nil, status.Errorf(codes.NotFound, "character profile not found")
}

props, err := s.world.ListPropertiesByParent(ctx, world.HumanCaller(viewerSubject), "character", charID)
if err != nil {
	errutil.LogErrorContext(ctx, "character access: list properties failed", err, "character_id", charID.String())
	return nil, status.Errorf(codes.Internal, "internal error")
}

rows := make([]profilevis.Property, 0, len(props))
for _, p := range props {
	rows = append(rows, profilevis.Property{ID: p.ID.String(), Name: p.Name})
}

visible, err := s.profileVis.VisibleAttributes(ctx, viewerSubject, charID.String(), rows)
if err != nil {
	// PROFILE_VISIBILITY_UNREACHABLE  -> NotFound, generic message (identical to no-such-id)
	// PROFILE_VISIBILITY_EVALUATION_FAILED -> Internal, generic message (never "sparse profile")
	return nil, s.mapProfileError(ctx, err)
}
// build the response ONLY from `visible`; a field not in the map is never SET.
```

### The census receiver predicate, generalized from `world_envelope_census_test.go:145-158`

```go
// facadeReceiverName returns the receiver variable name for a method on one of
// the named facade types, and whether it is such a method. Generalizes the
// envelope census's hard-coded "Service" check to the two facades.
func facadeReceiverName(recv *ast.Field, types map[string]struct{}) (string, bool) {
	expr := recv.Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return "", false
	}
	if _, want := types[ident.Name]; !want {
		return "", false
	}
	if len(recv.Names) == 0 {
		return "", false
	}
	return recv.Names[0].Name, true
}
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|---|---|---|---|
| `world.Service` commands take a bare `subjectID string` | Opaque `world.Caller` (`internal/world/caller.go`), constructed by `HumanCaller` / `SystemCaller` / `JobCaller` | Phase 02.1 | Every call the facade makes passes a `Caller`. `HumanCaller` forwards the subject **verbatim** because it also becomes the outbox envelope Actor. |
| Profile visibility as a single ABAC evaluation | Caller-side conjunction in `internal/access/profilevis` | Phase 2 plan 02-08 | The facade calls it; it does not re-derive it. |
| `golang-migrate` | `goose` | Phase 01.1 | No migration needed this phase, but the convention is one file per version with both directions. |
| Colon-style event subjects | Dot-delimited, `eventbus.Qualify` | earlier | Not touched here, but note ABAC policy DSL type-prefixes (`character:<id>`) are correctly colon-style and MUST NOT be converted. |

**Deprecated/outdated in the inputs (not in the code):**
- `.claude/rules/grpc-errors.md:58,:65` — `oops.AsOops(err).Code()` is uncompilable and would not assert
  the top-level code anyway (issues #4949, #4902). Assert over the wire.
- 01-SPEC §8.10's `service.go:1144-1171` and §9.3's `service.go:799-836` citations have drifted (C-6).

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|---|---|---|
| A1 | 01-SPEC §3.4's "the Phase-4 census" means Phase 4 must build the §2.6 descriptor census **in addition to** criterion 1's routing census. §3.4 says so in words, but no D-NN scopes it, and criterion 1 describes only the routing census. | C-2, A.4 | If the planner builds only the routing census, PORTAL-10 rule 1 and §2.6's "SOLE mandated enforcement gate" are unmet and the gap surfaces at `gsd-verifier`. If it builds both when only one was intended, one wave of extra work. **Recommend the planner ask, or scope it explicitly in PLAN.md.** |
| A2 | The `guest denial message` disposition (§A.2 options 1/2/3). No decision covers it; three web tests assert the literal. | A.2 | Wrong choice = 3 test files churn unexpectedly mid-wave, or a scene-flavored message on the character surface. |
| A3 | `INV-ACCESS-15` is the right home for D-71's invariant (vs `INV-PRIVACY-12`). D-71 leaves scope to discretion; the boundary text supports ACCESS, but INV-PRIVACY's description does mention *"web profile read-surface disclosure shape."* | G.1 | Costly to reverse (ids are referenced from tests). Low risk of being *wrong*, moderate cost if re-decided later. |
| A4 | Term-B double evaluation (§E.1 option a) is acceptable. No decision addresses it. | E.1 | Only a performance cost; correctness is unaffected. |
| A5 | Recommending `protoregistry.GlobalFiles` for §2.6's census. `luastub_test.go` uses it for *messages*, not for a module-wide service/method walk, so the exact walk is un-precedented in-tree (the API is standard). | A.4 | If registration coverage is incomplete (a package with no blank import), the census under-counts silently — mitigate with a `require.NotEmpty` guard like `grpc_api_coverage_test.go:55`. |
| A6 | `ValidateDescription`'s byte-vs-rune semantics are intentional shipped behavior, not a latent bug worth filing. Not verified against any issue or ADR. | D.2 | If it *is* a known bug, the facade should not mirror it. Low risk; behavior is what it is at HEAD. |

---

## Open Questions

1. **Does Phase 4 own the §2.6 descriptor census? (A1)**
   - What we know: §3.4 names "the Phase-4 census" and defines its expected set as §3.3 ∪ §9's
     character-returning rows minus §2.4's deletions. D-72's entire rationale presupposes it.
   - What's unclear: criterion 1's text describes only the routing census, and no D-NN scopes §2.6's.
   - Recommendation: **plan both**, with the §2.6 census as its own task. It is the cheaper error.

2. **The guest-denial message (A2).**
   - Recommendation: parameterize on `playerGate` (option 2). Smallest correct change; zero test churn.

3. **How does the census treat the `Web*` proxy half?**
   - What we know: D-73 says "both halves of each proxy pair"; §9.2 (`:1988-1991`) confirms the proxy
     pair is a census pair. But the `Web*` handlers live in `internal/web/` and forward a token — they
     do **not** call `resolveAndGate`/`ownedCharacter` (the facade does).
   - What's unclear: the routing predicate for the web half cannot be `bodyReferencesSelector(…,
     "playerGate")`; it must be something like "forwards `headerInjectSessionToken` to the owner-audience
     facade RPC."
   - Recommendation: **the planner must define the web-half predicate explicitly.** A census whose two
     halves use different predicates is fine; one whose web half asserts nothing is vacuous.

4. **Is `visibility='system'` in scope?**
   - `entity_properties.visibility` admits `'system'` (`000001_baseline.sql:361-362`) but no §8.6 row,
     no viewer twin, and no term-B policy mentions it. It therefore default-denies — correct, but
     unremarked.
   - Recommendation: note it in the plan; assert the deny with a paired positive control (PORTAL-10
     rule 2) rather than leaving it untested.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|---|---|---|---|---|
| `go` toolchain | everything | ✓ | per `go.mod` | — |
| `buf` | `task proto`, `task lint:proto`, `task web:generate` | ✓ | invoked directly by `Taskfile.yaml:580,690,770` | — |
| `buf.build/bufbuild/protovalidate` | `max_items` on `gallery` | ✓ | pinned in `buf.lock` | — |
| Docker | `task test:int`, `task pr-prep:full` | assumed ✓ (repo baseline) | — | none — integration tier requires it |
| `flock` | `task pr-prep:full` | precondition-checked (`Taskfile.yaml:1150-1152`) | — | `brew install flock` / `apt install util-linux` |
| `gh` | commenting on #4937 (D-77) | ✓ (verified live this session) | — | manual web comment |
| PostgreSQL testcontainer | integration tier | assumed ✓ | — | none |

**Missing dependencies with no fallback:** none identified.

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go `testing` + `testify` (unit); Ginkgo/Gomega (full-stack integration, `//go:build integration`) |
| Config file | none (Go native); harness at `internal/testsupport/integrationtest/`; ABAC engine builder at `internal/testsupport/abactest/abactest.go:68` |
| Quick run command | `task test -- ./internal/grpc/ ./internal/access/... ./internal/web/` |
| Full suite command | `task test` then `task test:int` (scoped: `task test:int -- ./test/integration/access/...`) |
| Meta-test command | `task test -- -run 'Census' ./test/meta/` |

### Success Criteria → Test Map

| Crit | Behavior | Tier | Harness / precedent | Command | Genuine RED looks like |
|---|---|---|---|---|---|
| **1** | Guest gate + ownership exist in exactly one place; set equality over the owner-audience RPCs proves each routes through it | **unit (meta)** | `go/ast` over `internal/grpc/`; `bodyReferencesSelector` (`world_envelope_census_test.go:162`); directory-walk shape from `world_caller_census_test.go:56-57`; symmetric-difference diff per §2.6 `:222-224` | `task test -- -run 'TestCharacterAccessRoutingCensus' ./test/meta/` | Add an owner-audience RPC to the facade that omits `s.resolveAndGate` → the census names it in the "extra" set. **Demonstrate by temporarily deleting the call from one existing handler.** |
| **1b** (C-2) | Descriptor-derived character-returning RPC set == §3 inventory | **unit (meta)** | `protoregistry.GlobalFiles`; anti-vacuity guard per `grpc_api_coverage_test.go:55` | `task test -- -run 'TestCharacterReturningRPCCensus' ./test/meta/` | Declare a character-returning RPC without an inventory row → "extra" set non-empty. |
| **2** | A withheld field is absent from the marshaled bytes; unreachable == nonexistent | **unit** (bytes) + **full-stack integration** (differential) | `proto.Marshal` + `assert.NotContains` (compose `plugin_downgrade_fence_test.go:636` idiom with the ubiquitous `proto.Marshal` test idiom); `abactest.NewSeedEngine`; `integrationtest` for the differential | `task test -- -run 'Profile.*Absent' ./internal/grpc/` ; `task test:int -- ./test/integration/access/...` | Seed `profile.biography` with a sentinel, drive an **anonymous** viewer against the pre-fix handler that populates every field → the sentinel appears in the bytes. Differential: pre-fix returns a distinguishable message for the unreachable case. |
| **3** | Per-attribute floor governs every field; name/pronouns cannot be raised above reachability; unknown tier denies | **unit** | `abactest.NewSeedEngine` over the real seed corpus; `Selectable` switch shape | `task test -- ./internal/grpc/ ./internal/access/...` | Feed an unrecognized rung token → pre-fix returns a constructed viewer subject; post-fix denies. **Paired positive control** (PORTAL-10 rule 2): the same field at `player` rung IS visible. |
| **4** | Owner edits prose + description; over-cap rejected server-side; description reaches `world.Service.UpdateCharacterDescription` | **unit** (caps, mask) + **full-stack integration** (reaches the command + outbox) | `integrationtest.Start`; the `UpdateScene` mask precedent (`sceneaccess_service.go:846-902`) | `task test -- ./internal/grpc/` ; `task test:int` | 4001-byte value → pre-fix accepted. Mask path outside the allowlist → pre-fix forwarded. Integration: assert the row changed **and** exactly one outbox envelope committed. |
| **5** | Profile built exclusively from the viewer-filtered slice; `ListByParent` from the facade fails the build; media proto ships empty | **compile-time** + **unit** + **integration** | D-79's narrow interface (compile fence); `task lint:proto`; EXT-05-style insert-and-read-back of 1 primary + 10 gallery rows | `task build` ; `task lint:proto` ; `task test:int` | **Compile fence RED is demonstrated, not asserted:** temporarily add `s.propertyRepo.ListByParent(...)` to the facade and record the compile error in the plan's RED evidence. |
| **6** | Off-location viewer can read the description where colocation denied it; the read path returns `description` without `PlayerId`/`LocationId` | **unit** (policy) + **full-stack integration** (projection) | `abactest.NewSeedEngine`; `test/integration/access/profile_public_read_test.go` is the direct precedent (six specs + control corpus, from plan 02-10) | `task test -- ./internal/access/policy/` ; `task test:int -- ./test/integration/access/...` | Remove the new permit → the off-location read denies. **Paired positive control** (plan 02-10's `accessTestEnv.resolver/.compiler` builds a second engine over the same providers but a corpus excluding one policy by name). Projection: assert the response type **has no** `player_id`/`location_id` field — structural per D-75, so this is a compile/shape assertion, not a field-clearing assertion. |

### Sampling Rate
- **Per task commit:** `task test -- ./<touched-package>/` + `task lint`
- **Per wave merge:** `task test` (full unit) + `task test -- -run 'Census' ./test/meta/`
- **Phase gate:** `task pr-prep` green, then `task pr-prep:full` (integration + E2E) before push; `/holomush-dev:review-abac` READY.

### Wave 0 Gaps
- [ ] Generalize `serviceReceiverName` → a facade-aware receiver predicate (new helper in `test/meta/`)
- [ ] A symmetric-difference diff helper for census failures (§2.6 `:222-224`) — `test/meta/meta_helpers_test.go` is the existing shared-helper home
- [ ] Decide and implement the **web-half routing predicate** (Open Question 3) — without it, criterion 1's proxy half is vacuous
- [ ] A `(*integrationtest.Server).NewCharacterAccessServer` harness constructor, mirroring `harness.go:1173`
- [ ] Framework install: **none** — all frameworks present

### Criteria at risk of vacuous proof
- **Criterion 3's "cannot raise `name` or `pronouns` above the reachability floor."** 01-SPEC §8.8
  (`:1855-1867`) records that **v0.13 ships no mechanism** enforcing this against a deliberately
  violating configuration, and that `INV-PRIVACY-10` *"is phrased as a system guarantee while in fact
  resting on operator discipline."* A test can prove the **facade** always emits name+pronouns on a
  reachable profile; it **cannot** prove the configuration constraint. **State this limit in
  VALIDATION.md rather than binding `INV-PRIVACY-10` to a test that does not prove its second clause**
  (the `TestBoundInvariantsAreGenuinelyAsserted` meta-test cannot detect a *partial* binding — see
  `.claude/rules/invariants.md`).
- **Criterion 6's projection half.** "Returns `description` without `PlayerId` or `LocationId`" is
  structural under D-75 (a different message type), so an assertion that the *fields are empty* is
  vacuous by construction. Assert the **message shape** (the census / a proto-descriptor field-set
  assertion), not the values.
- **D-71's binding test.** It asserts a property the shipped policy already has, so it is green on day
  one. That is legitimate regression-pinning, but the plan must not present it as a RED-first gate. Its
  RED is `property.go`'s ALL branch flipped to ANY — demonstrate that.

---

## Security Domain

`security_enforcement: true`, `security_asvs_level: 1` (`.planning/config.json`).

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | Shipped: `resolvePlayerSessionWithRepo` + `resolveAndGate` (`sceneaccess_service.go:130`). Phase 4 relocates, does not redesign. |
| V3 Session Management | yes | Session token arrives via `headerInjectSessionToken`, never the request body (`scene_handlers.go:340`). Preserve. |
| V4 Access Control | **yes — the phase's core** | ABAC, default-deny; `world.Service.checkAccess` (`service.go:233`); `profilevis` conjunction; additive seeding at a new `SeedVersion`. Gated by `abac-reviewer`. |
| V5 Input Validation | yes | `world.ValidateDescription` (`validation.go:94`), the closed update-mask allowlist, and `(buf.validate.field).repeated.max_items = 10`. |
| V6 Cryptography | no | Phase touches no crypto surface; `crypto-reviewer` does not fire. |
| V7 Error Handling & Logging | yes | Generic wire messages + `errutil.LogErrorContext` (`.claude/rules/grpc-errors.md`); §9.6.1's wire-level assertions. |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Existence-disclosure via distinguishable not-found | Information Disclosure | One code (`CHARACTER_PROFILE_NOT_FOUND`) for two causes + the §9.6.1 differential assertion. |
| Enumeration oracle via paired error codes | Information Disclosure | Gate-then-distinguish ordering — the shipped `INV-PRIVACY-11` precedent (`internal/admin/section`, 02-09). |
| Alt-linkage disclosure via a widened owner peer | Information Disclosure | D-69: keep the ALL direction; split audiences. Pinned by D-71's invariant. |
| Present-but-empty field revealing a withheld value | Information Disclosure | §8.9 absence-not-emptiness; D-80's byte-level assertion. |
| Client-side hiding treated as enforcement | Information Disclosure | §2.7: no visibility hints on the wire; the client is not a participant. |
| Infra failure masked as "sparse profile" | Tampering / Repudiation | §8.10 abort-on-infra; `profilevis` distinguishes `ErrEvaluationFailed` from `ErrProfileUnreachable` (`profilevis.go:78-93`). |
| Unadvertised mutation via verbatim mask passthrough | Elevation of Privilege | Closed mask allowlist + empty-mask short-circuit after ownership (`sceneaccess_service.go:846-902`). |
| Structural write routed through the command parser | Elevation of Privilege | `.claude/rules/gateway-boundary.md` — typed RPC, never `sendCommand` (ADR `holomush-v4qmu`). |

---

## Project Constraints (from CLAUDE.md)

| Directive | Bearing on this phase |
|---|---|
| **MUST** write tests before implementation (TDD; `tdd_mode: true`) | Every task carries a RED-first step. Compile-only RED is degenerate — assertions must fail, not just the build (except D-79's fence, whose RED **is** a compile error and must be *demonstrated and recorded*). |
| **MUST** use `task` for build/test/lint/fmt | Never `go test` / `golangci-lint` directly. |
| **MUST** run `task lint` before committing; `task fmt` mutates files — commit them | Editing aligned Go `const`/`var`/`struct` blocks can pass build+tests yet fail `fmt:check` in CI. |
| **MUST** run `task test:int` on refactors | The `playerGate` extraction is exactly this. |
| **MUST NOT** commit to `main`; squash-merge via PR | Work is on `v013-phase-03` worktree. |
| **MUST** regenerate + commit generated proto output in the same change | `pkg/proto/**/*.pb.go`, `web/**/*_pb.ts`, and (new) `site/.../grpc-api.md`. |
| **MUST** use `slog.*Context` / `errutil.LogErrorContext` whenever a `ctx` is in scope | `sloglint` `context: scope` enforces it. |
| **MUST** use line-scoped `//nolint:<rule>` with a reason; **MUST NOT** widen `.golangci.yaml` | The facade precedent uses `//nolint:wrapcheck // gRPC status error at handler boundary` throughout. |
| **MUST** use `crypto/rand`; `idgen.New()` for entity PKs, `core.NewULID()`/`eventbus.NewEvent` for events | No new event construction expected here. |
| **MUST** follow terminology: `location` never `room`; `character` vs `player` | Proto doc comments are lint-gated on this. |
| **MUST** ground every proto doc comment in the Go handler; no name-echo | `task lint:proto` runs `TestProtoCommentsNoNameEcho`. |
| **MUST NOT** write custom structure into tool-owned generated files | D-74's SPEC/ROADMAP edits are narrowly-scoped `Edit`s only. |
| **MUST NOT** use `[ci skip]` on a branch with an open PR | Repo-level rule overriding GSD's default ship-note suggestion. |
| **MUST** run `abac-reviewer` before push | D-76 adds seeds. |
| Search ladder: probe/codegraph → `rg` → `ast-grep`; never bare `grep` | Brief sub-agents explicitly — they do not inherit it. |

---

## Sources

### Primary (HIGH confidence — opened at HEAD this session)

- `internal/grpc/sceneaccess_service.go` (1038 lines) — `:27-31`, `:33-40`, `:44-68`, `:106-150`, `:151-161`, `:838-902`; all 45 helper call sites
- `internal/world/service.go` — `:233` (`checkAccess`), `:825` (`GetCharacter`), `:843-…` (`UpdateCharacterDescription`), `:1394-1434` (`ListPropertiesByParent`)
- `internal/world/caller.go` — `:16`, `:27-59`, `:88-90`, `:96-107`
- `internal/world/validation.go` — `:18-25`, `:94-110`
- `internal/world/lifecycle.go` — `:83-92`
- `internal/world/grpc_server.go` — `:127-138`
- `internal/access/profilevis/profilevis.go` (293 lines, read in full)
- `internal/access/policy/seed.go` — `:50-55`, `:648-659`, `:666-682`, `:744-786`, `:786-856`
- `internal/access/policy/compiler.go` — `:160-209`
- `internal/access/policy/attribute/action_schema.go` — `:43-73`
- `internal/access/policy/attribute/property.go` — `:195-240`
- `internal/access/policy/attribute/viewer.go` — `:37-115`, `:129-231`
- `internal/access/prefix.go` — `:40-46`, `:178-190`, `:201`, `:255`, `:282`
- `internal/command/types.go` — `:115-215`
- `internal/testsupport/abactest/abactest.go:68`; `action_registration_internal_test.go:17-45`
- `internal/store/migrations/000001_baseline.sql` — `:354-376`
- `internal/web/scene_handlers.go` — `:330-364`; `scene_handlers_test.go:468`; `status_interceptor_test.go:86,:101,:172,:186`
- `test/meta/world_envelope_census_test.go` (346 lines, read in full)
- `test/meta/world_caller_census_test.go` — `:1-90`
- `test/meta/grpc_api_coverage_test.go` — `:1-120`
- `pkg/errutil/testing.go` — `:15-30`
- `api/proto/holomush/world/v1/world.proto` — `:75-95`; `web/v1/web.proto` RPC list; `scene/v1/scene.proto:434`
- `buf.yaml`, `buf.lock`, `go.mod:6-7`
- `Taskfile.yaml` — `:576-588`, `:686-696`, `:767-772`, `:1071-1152`
- `docs/architecture/invariants.yaml` — `:11-14`, `:194-197`, `:571-573`, `:2164-2196`
- `.claude/agents/abac-reviewer.md:4,:36`; `branch-readiness-check.md:56`
- `.claude/rules/grpc-errors.md:54-65`
- GitHub issue **#4937** (`gh issue view`, live, 2026-08-10)

### Planning artifacts (CITED — normative for this phase)

- `.planning/phases/04-shared-facade-helpers-characteraccessservice/04-CONTEXT.md` (read in full)
- `.planning/phases/04-.../04-DISCUSSION-LOG.md` (read in full; audit trail only)
- `.planning/phases/01-portal-spec/01-SPEC.md` — §2.6 `:179-236`, §2.7 `:238-255`, §3.4 `:443-454`, §3.5 `:456-473`, §7.4 `:1179-1201`, §7.5 `:1203-1218`, §8.6 `:1729-1830`, §8.7 `:1832-1841`, §8.8 `:1842-1867`, §8.9 `:1869-1882`, §8.10 `:1884-1898`, §9.2 `:1972-2004`, §9.3 `:2006-2056`, §9.6 `:2210-2236`, §9.6.1 `:2238-2279`, §9.7 `:2281-2303`
- `.planning/phases/02-abac-schema-vocabulary/02-AUDIT-profile-public-read.sql` (headers + sets 4/5 read); `02-AUDIT-RESULT.md`; `02-0{3,7,8,9,10,11,13}-SUMMARY.md` frontmatter
- `.planning/ROADMAP.md` — `:96-124`, `:213`; `.planning/REQUIREMENTS.md` — `:60-74`, `:99-102`, `:152-180`, `:242`, `:352-405`
- `.planning/config.json`

### Tertiary (LOW confidence — none)

No claim in this document rests on a web search or on training memory alone.

---

## Metadata

**Confidence breakdown:**

| Area | Level | Reason |
|------|-------|--------|
| Helper extraction (§A.1-A.2) | **HIGH** | Every definition, signature and call site enumerated from the file at HEAD. |
| Census precedent (§A.3) | **HIGH** | File read in full; predicate quoted verbatim. |
| Descriptor census (§A.4) | **MEDIUM** | §2.6's requirement is verbatim; the `protoregistry` walk is standard API but has no exact in-tree precedent (A5). |
| Wire absence / opacity (§B) | **HIGH** | Spec quoted verbatim; `oops.AsOops` two-value form verified at two call sites. No byte-scan precedent exists — reported as such. |
| Viewer tiers (§C) | **HIGH** | Seed DSL text and constants quoted verbatim; the `characters`-column gap follows directly from `resource is property` in both floor policies. |
| Write path (§D) | **HIGH** | Signature, gate and validation constants quoted verbatim. |
| Property slice / proto (§E) | **HIGH** | `profilevis` read in full; `max_items` verified in `buf.lock`, in in-tree use, and in the vendored descriptor. |
| ABAC permit (§F) | **HIGH** | The action-token correction (C-1) verified three ways: the compiler branch, the schema contents, and an existing test that pins the semantics. |
| Invariants / gates (§G) | **HIGH** | Ids enumerated; agent trigger paths quoted; Taskfile read directly (not from the stale learnings file). |

**Research date:** 2026-08-10
**Valid until:** ~2026-09-09 for stack claims; **~7 days for `path:line` citations** — this repo's wiring
has a one-phase shelf life, and the drifted citations in C-6 are the proof.
