# Phase 6: Admin Portal Shell & Character Administration - Research

**Researched:** 2026-08-13
**Domain:** ABAC-gated admin RPC surface (Go/gRPC) + SvelteKit admin console
**Confidence:** HIGH for the in-repo inventory (every claim opened and cited); MEDIUM for the interceptor design recommendation (transposition is sound but unbuilt)

## Summary

Phase 2 shipped substantially more of this phase than the ROADMAP text implies. The
seven-entry section registry, the mandatory authorization descriptor, boot-time validation
wired into a production startup path, the resource-type-scoped seed policy, and a
set-equality meta-test all exist and are green. `AssertSectionAccess` is complete and
correct and has **zero production callers**. Phase 6's job for EXT-01/EXT-03/EXT-04 and
success criterion 4 is to **wire**, not to build.

Three findings change the shape of the plan versus what CONTEXT.md anticipated. First,
**`pg_trgm` already exists** — `000001_baseline.sql:17` creates it and three `gin_trgm_ops`
indexes already ship, so D-106's "must be creatable in the target deployment" caveat is
already discharged and the migration is a routine index add. Second, **the core gRPC server
has no unary interceptor chain at all** (`internal/grpc/server.go:630,646`), and there are
**two** constructors — production-TLS and insecure-test — that must be changed in lockstep
or integration tests run ungated against a gated production. Third, **the before-status
D-103 needs is already in scope**: `RetireCharacter` reads the character at step (2) to arm
its lifecycle guard (`internal/world/service.go:1325`), so `char.Status` is live at the
payload-build site three dozen lines later — no repository change is required.

The one genuine transposition break is subject resolution. `hostcap`'s interceptor knows its
subject at *construction* time (`PluginName` is a field on `InterceptorDeps`). An admin
interceptor cannot: the player identity arrives per-request, in a
`player_session_token` **field of the request message body**, and `internal/grpc/` uses
`metadata.FromIncomingContext` exactly **zero** times. The fix is small and idiomatic —
protoc-gen-go getters make `req.(interface{ GetPlayerSessionToken() string })` a uniform,
no-per-method-config extraction — but it must be designed, not assumed.

**Primary recommendation:** Build the descriptor table + interceptor as D-99 specifies, but
scope the plan around four already-existing fail-closed mechanisms (the section registry, the
`characteraccess` routing census, the world taxonomy registry, and the `AppSchemaVersion`
ratchet) rather than minting new ones. The highest-risk unbuilt surface is the interceptor's
per-request subject resolution and its dual-constructor mount.

---

## User Constraints (from CONTEXT.md)

### Locked Decisions

Copied from `06-CONTEXT.md` `## Implementation Decisions`. These are settled; research
did not reopen them.

- **D-99** — The gate is a method→section descriptor table plus an interceptor, not a
  per-handler call. Mirrors `internal/plugin/hostcap/`. An admin method with no section
  declaration is refused (`ADMIN_SECTION_NOT_DECLARED`), never defaulted. **Core-side only.**
- **D-100** — Two registry RPCs, section id as a **parameter**, never as a method name.
  `AdminListSections` returns viewer-permitted sections with `status`; `AdminGetSection(section_id)`
  gates on the supplied id then returns `SECTION_NOT_IMPLEMENTED` for the six planned.
  `AssertSectionAccess` runs **before** `section.Lookup`.
- **D-101** — The admin nav is a server-filtered projection of `AdminListSections`. No
  mirrored section registry in `web/src/`.
- **D-102** — `repeated string roles` ships on `WebCheckSessionResponse`, player-scoped,
  nav-hiding only, never the authorization boundary.
- **D-103** — Before-values split by field kind: `AdminUpdateCharacter` emits changed
  attribute **names only**; `AdminRetireCharacter`/`AdminUnretireCharacter` **do** carry the
  before-`status`. Every admin envelope carries the evaluated `section` and `action`.
- **D-104** — Envelope `Actor` is `player:<id>`; the acting character is **omitted**.
- **D-105** — Emission goes through the transactional outbox seam in the same transaction as
  the state change. The `events_audit` row is **projected**. No direct `INSERT INTO events_audit`.
- **D-106** — `AdminSearchCharacters` does **substring** matching on `characters.normalized_name`
  and joined `players.username`, backed by a `pg_trgm` GIN migration. Plain `CREATE INDEX`
  inside goose's transaction, never `CONCURRENTLY`.
- **D-107** — `last_active_at` renders as coarse relative text. The `0` sentinel renders
  `never` and **sorts last in BOTH directions**. Column label is **`Last active`**.
- **D-108** — Admin retire confirm carries retire-specific copy; four fixed clauses.
- **D-109** — The bottom-sheet grab handle is **dropped** (`bits-ui` has no swipe-dismiss).
- **D-110** — Mutation loop: on success the Sheet closes, the row updates in place from the
  response, the toast names the RPC. On `Aborted` the Sheet stays open with typed text kept.

Plus the carried web-surface decisions (three-column frame, rail merge at 768–1023px,
dense table with inline hover actions, no multi-select, minimal planned-section empty
state, Sheet as 380px right overlay flipping to `side="bottom"` below 768px, `/admin`
invisible without permission, `Home` as not-found copy, container queries never media
queries).

### Claude's Discretion

- Where the method→section descriptor table lives (alongside `internal/admin/section/`
  versus a new package), and the exact `ADMIN_SECTION_NOT_DECLARED` code spelling.
- Whether `AdminListSections`/`AdminGetSection` land on `CharacterAccessService` or a new
  admin-facing service. The census must gain both plus their `Web`-prefixed proxies either way.
- Relative-time granularity buckets for D-107; exact confirm-copy wording for D-108.

### Deferred Ideas (OUT OF SCOPE)

Drag-to-dismiss on the phone bottom-sheet; the audit log viewer; role mutation and the
`players` section; prose/content search over profile fields; player-initiated self-retire;
character rename; exposing the game's display name to the web (#4905).

**Also explicitly excluded** (CONTEXT.md `## Phase Boundary`): admin impersonation,
break-glass identifiers, a SQL console. **There is no `AdminDeleteCharacter`** —
`world.Service.DeleteCharacter` MUST NOT be wired to an admin button.

---

## What Phase 2 Already Built vs. What Phase 6 Must Build

| Requirement / Criterion | Status | Evidence (opened this session) | Phase 6's remaining job |
|---|---|---|---|
| **EXT-01** seven entries, one available + six planned | **SATISFIED** | `internal/admin/section/registry.go:102-110` (the seven `entry(...)` rows); proved by `registry_test.go:44` (`TestAllReturnsTheSevenSectionsSpec101Enumerates`) and `registry_test.go:227` (`TestTheRegistryIDSetEqualsTheSevenIDsSpec101Enumerates`) | none |
| **EXT-03** descriptor with no default, no zero value meaning allow; fails at boot | **SATISFIED** | `registry.go:79-96` (`Descriptor` required field), `registry.go:179-193` (`validateEntries` rejects empty/drifted), `boot.go:34` (`ValidateAtBoot`), production call site `internal/bootstrap/setup/subsystem.go:156`; proved by `registry_test.go:68,101` | none |
| **EXT-04** meta-test asserting set equality | **SATISFIED (see note)** | `registry_test.go:227`; descriptor completeness is structural — the descriptor is a required *field*, re-derived and compared at `registry.go:187-193`, proved by `registry_test.go:68` | none — but see Open Question 1 on the "descriptor set" wording |
| **Criterion 4** six sections registered, gated, `NOT_IMPLEMENTED` **after** the gate | **MECHANISM EXISTS, NOT REACHABLE** | `gate.go:163-167` returns `SECTION_NOT_IMPLEMENTED` only after steps 1–3; `gate_test.go:209` (`TestAPlannedSectionRefusesOnlyAfterTheGatePermits`) | **EXT-02**: an RPC surface so the refusal is reachable over the wire (D-100's `AdminGetSection`) |
| **ADMIN-01** ABAC gate on `admin_section:`, never bare `PlayerHasRole` | **MECHANISM EXISTS, ZERO CALLERS** | `gate.go:234` `AssertSectionAccess`; seed `internal/access/policy/seed.go:985-987`; `internal/access/prefix.go:71,296` | wire it — via the D-99 interceptor |
| **ADMIN-02** every admin RPC asserts the gate | **NOT BUILT** | — | descriptor table + interceptor + completeness meta-test |
| **ADMIN-03/04/05** list, search, view, edit; field-mask allowlist; shared lifecycle states | **PARTIAL** | `RetireCharacter` `service.go:1308`, `UnretireCharacter` `service.go:1408` already exist | 6 admin RPCs, repo search method, field-mask allowlist |
| **ADMIN-06** audit envelope in-transaction with before-values | **PARTIAL** | outbox seam `service.go:816` (`buildIntent` → `mutator.*`); taxonomy `internal/world/outbox/taxonomy.go:144-145,201-204` | extend the lifecycle payload with before-status; add `section`/`action` |
| **ADMIN-07** nav filtered by registry contract | **NOT BUILT** | `internal/web/` has **zero** `RoleAdmin` references (verified) | `AdminListSections` + web projection |
| **ADMIN-08** roles on `WebCheckSessionResponse` | **NOT BUILT** | `api/proto/holomush/web/v1/web.proto:802-822` carries fields 1–5, no `roles` | add field 6 |
| **PROFILE-12** (retirement half, D-91) | **NOT IN THE ROADMAP LINE** | `REQUIREMENTS.md:391` maps PROFILE-12 to Phase 5 | amendment #6 owed |

**Bottom line:** three of four EXT requirements are done. `#4904` is correctly closed. The
unbuilt surface is the RPC layer, the interceptor, the audit payload widening, the search
migration, and every web artifact.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|---|---|---|---|
| Admin authorization decision | **API / Core (`internal/grpc` interceptor)** | — | `.claude/rules/gateway-boundary.md` forbids an `internal/web/` authorization decision; D-99 mounts core-side only |
| Section registry + descriptor | **Core (`internal/admin/section`)** | — | already shipped; the web *derives* from it (D-101) |
| Session token → player id | **API / Core** | Gateway lifts the cookie | established convention: gateway lifts, core resolves (`internal/web/character_handlers.go:41` sets `PlayerSessionToken`) |
| Character list / search / read | **API / Core** → **Database** | — | `pg_trgm` GIN index; repo method on `CharacterRepository` |
| Admin mutation + audit envelope | **API / Core** → **Database (one tx)** | Async projection → `events_audit` | D-105; `events_audit` has exactly one writer |
| Nav filtering | **API (`AdminListSections`)** | Frontend renders | D-101/ADMIN-07: contract-filtered, not `{#if}` |
| Edit Sheet, table, toasts | **Browser / Client** | — | pure presentation; no authorization meaning |
| Not-found indistinguishability | **Frontend Server (SvelteKit)** | — | one root `+error.svelte`; per-viewer, not global |

---

## VERIFIED Facts (opened this session)

Every line below was read with `Read` or matched with `rg` in this session. Line numbers are
from the working tree at `v013-phase-03`.

### The Phase 2 substrate

| Claim | Citation | Verbatim anchor |
|---|---|---|
| Seven-entry registry in §10.1 row order | `internal/admin/section/registry.go:102-110` | `entry("characters", StatusAvailable)`, then `stats`, `players`, `moderation`, `audit`, `config`, `plugins` — all `StatusPlanned` |
| Descriptor derived from id, never literal | `internal/admin/section/registry.go:118-127` | `Resource: access.AdminSectionResource(string(id))` |
| Descriptor re-derived and compared at validation | `internal/admin/section/registry.go:187-193` | `if want := access.AdminSectionResource(string(e.ID)); e.Descriptor.Resource != want` |
| Closed status vocabulary, denying default | `internal/admin/section/registry.go:225-236` | `default: return false, oops.Code("ADMIN_SECTION_STATUS_UNKNOWN")` |
| `AssertSectionAccess` signature | `internal/admin/section/gate.go:234-240` | `func AssertSectionAccess(ctx context.Context, engine PolicyEvaluator, playerID, sectionID, action string) (Section, error)` |
| `PolicyEvaluator` narrow interface | `internal/admin/section/gate.go:21-23` | `Evaluate(ctx context.Context, req types.AccessRequest) (types.Decision, error)` |
| Gate-then-distinguish ordering (D-06) | `internal/admin/section/gate.go:103-134` | Step 1 ABAC `engine.Evaluate` at `:104`; Step 2 `lookup(sectionID)` at `:129` |
| Denial is a static, field-free message | `internal/admin/section/gate.go:179-181` | `oops.Code("DENY_ADMIN_SECTION").Errorf("admin section access denied")` |
| `SECTION_NOT_IMPLEMENTED` refused after the gate | `internal/admin/section/gate.go:163-167` | Step 4, after steps 1–3 |
| Action ladder is closed; unranked = refusal | `internal/admin/section/gate.go:37-52` | `actionRank = map[string]int{ActionRead: 1, ActionWrite: 2}`; `if !declaredKnown \|\| !requestedKnown { return false }` |
| **`AssertSectionAccess` has zero production callers** | `rg 'AssertSectionAccess' --type go` | every hit is in `gate.go`, `registry.go` (doc comments), or `gate_test.go` |
| `ValidateAtBoot` is called from production startup | `internal/bootstrap/setup/subsystem.go:156` | `if err := section.ValidateAtBoot(ctx); err != nil {` |
| Seed policy is resource-**type** scoped | `internal/access/policy/seed.go:985-987` | `permit(principal is player, action in ["read", "write"], resource is admin_section) when { "admin" in principal.player.roles };` |
| `admin_section:` resource family | `internal/access/prefix.go:71`, `:296-300` | `ResourceAdminSection = "admin_section:"`; `AdminSectionResource` panics on empty id |

> CONTEXT.md cites `boot.go:33`; the `ValidateAtBoot` declaration is at **`boot.go:34`** (`:33`
> is the closing line of its doc comment). Immaterial, noted for citation hygiene.

### The gate's existing test coverage (relevant to non-vacuity claims)

`internal/admin/section/gate_test.go` — ten tests, all iterating `All()` rather than
hard-coding:

- `:114` `TestEveryRegisteredSectionPermitsAnAdminAndDeniesEveryNonAdmin` — asserts
  `permits >= 7` and `denials >= 21`, each denial preceded by a **paired positive control**
  on the same section through the same helper (`:135`).
- `:178` `TestANonAdminsRefusalIsIdenticalAcrossARegisteredAndAnUnregisteredSectionID` —
  the INV-PRIVACY-11 differential assertion.
- `:209` `TestAPlannedSectionRefusesOnlyAfterTheGatePermits`.
- `:306` `TestTheUndeclaredActionRefusalIsInvisibleToACallerTheGateDenied`.
- `:371` `TestTheGateEvaluatesTheABACEngineBeforeItConsultsTheRegistry`.

**This is criterion 1's substrate already proved at the helper level.** What criterion 1
additionally demands and does *not* yet exist is the assertion **over the wire** — mapped
`status.Code(err)` plus a generic `status.Convert(err).Message()`.

### The `hostcap` pattern D-99 transposes

| Element | Citation | Actual shape |
|---|---|---|
| Descriptor table | `internal/plugin/hostcap/descriptor.go:69` | `var Descriptors = map[string]CapabilityDescriptor{` — keyed by capability token, each with `Methods map[string]MethodDescriptor` |
| Per-method resource extractor | `descriptor.go` (`CreateExit`, `CreateObject` entries) | `Extract: func(req any) (string, bool)` type-asserting the concrete request type |
| Interceptor constructor | `interceptor.go:191` | `func NewCapabilityInterceptor(d InterceptorDeps) grpc.UnaryServerInterceptor` |
| Method classification | `interceptor.go:150-167` | `classifyHostMethod(fullMethod string) (capToken, method string, ok bool)` — cuts `/pkg.Service/Method`, maps bare service → token |
| Fail-closed on unmapped service | `interceptor.go:210-218` | `UNCLASSIFIED_CAPABILITY_METHOD` when a `host.v1` method has no token |
| Fail-closed on missing descriptor | `interceptor.go:221-227` | `md, ok := Descriptors[capToken].Methods[method]; if !ok { ... UNCLASSIFIED_CAPABILITY_METHOD }` |
| Fail-closed on nil dependency | `interceptor.go:192-206` | `CAPABILITY_DECLARATION_LOOKUP_MISSING` — a misconfigured interceptor denies rather than passes through |
| Completeness meta-test | `descriptor_test.go:19` | `TestEveryServedCapabilityHasADescriptor` — iterates `plugins.CapabilityServiceNames`, requires a non-empty `Descriptors` entry |
| Fail-closed *proof* | `descriptor_completeness_test.go:21,41` | `TestEveryScopeEligibleMethodHasExtractor` (`// Verifies: INV-PLUGIN-52`) and `TestInterceptorScopedMethodWithoutExtractorFailsClosed`, which **temporarily strips a real extractor with `t.Cleanup` restore** and asserts `SCOPE_NO_EXTRACTOR` |

The `descriptor_completeness_test.go:41` technique — mutate the real table, restore via
`t.Cleanup`, assert the guard fires — is the in-repo precedent for proving
`ADMIN_SECTION_NOT_DECLARED` is genuinely reachable rather than dead code. **Reuse it.**

### Where the transposition BREAKS

| Break | Evidence | Consequence |
|---|---|---|
| **Subject is per-request, not per-server** | `hostcap` bakes `PluginName` into `InterceptorDeps` (`interceptor.go:244-255` uses `d.PluginName`). Admin has no equivalent. | The interceptor must resolve the player per request. |
| **No gRPC metadata auth in core** | `rg 'metadata.FromIncomingContext' internal/grpc/*.go` → **zero hits** | The token cannot be read from headers; it is in the message body. |
| **Token is a request-message field** | `api/proto/holomush/characteraccess/v1/characteraccess.proto:163,210,272,290,309,383,413` — `string player_session_token` on request after request | The interceptor must extract it from `req any`. |
| **Session resolution is a repo call** | `internal/grpc/auth_handlers.go:174` `resolvePlayerSessionWithRepo(ctx, repo auth.PlayerSessionRepository, rawToken string) (*auth.PlayerSession, error)`; hashes via `auth.HashSessionToken` at `:178` | The interceptor needs the session repo injected, and should stash the resolved player in ctx to avoid a double lookup in the handler. |
| **Core gRPC server has NO interceptor chain** | `internal/grpc/server.go:630-639` (`NewGRPCServer`) and `:646-657` (`NewGRPCServerInsecure`) — neither has `grpc.ChainUnaryInterceptor` | **Two** mount points to change in lockstep. If only the TLS one is wired, `task test:int` runs ungated while production is gated — a silent divergence no existing test catches. |

**Recommended extraction shape (MEDIUM confidence — designed here, not in-tree):**
protoc-gen-go generates `GetPlayerSessionToken() string` on every message with that field, so
a single interface assertion covers all admin methods with **no per-method extractor config**:

```go
// Recommended — one assertion, no per-method table entry.
tokened, ok := req.(interface{ GetPlayerSessionToken() string })
if !ok {
    return nil, adminDeny("ADMIN_SECTION_NO_SUBJECT", "admin request carries no session token field")
}
```

This is strictly simpler than `hostcap`'s per-method `Extract` closure, and the `ok=false`
arm is the fail-closed path. Prefer it; keep the per-method `Extract` shape only if some
admin request legitimately names its token field differently.

### `AssertOperatorAdmin` — the ROADMAP's named unknown

`internal/admin/auth/operator_admin.go:37-64`.

```go
func AssertOperatorAdmin(ctx context.Context, resolver access.SubjectResolver,
    roleStore PlayerRoleHasher, playerID string) error
```

Its body does two things: `access.HasPlayerGrant(... access.CapabilityCryptoOperator)` at
`:43`, then **`roleStore.PlayerHasRole(ctx, playerID, access.RoleAdmin)` at `:53`**.

**This is the key finding for priority 3.** `AssertOperatorAdmin` is a **bare `PlayerHasRole`
lookup** — precisely the mechanism ADMIN-01 (`REQUIREMENTS.md:191-192`) and §10.4 forbid for
the admin-section gate. It is therefore a **structural** precedent only (one shared helper,
called first, typed `DENY_*` codes, three sites kept in lockstep — see its doc comment at
`:18-21`), never a mechanical one.

The transposition has in fact **already happened**: `AssertSectionAccess` is the
operator-socket helper's shape with the storage query replaced by an ABAC evaluation
(`gate.go:104`). Its own doc comment at `gate.go:225-228` makes the lineage explicit and
names the three prohibitions. **Nothing further needs transposing at the helper level.** The
open work is entirely the interceptor that calls it. Codes differ deliberately:
`AssertOperatorAdmin` yields `DENY_NOT_OPERATOR`/`DENY_NOT_ADMIN_ROLE` (`:49,:59`);
`AssertSectionAccess` yields the field-free `DENY_ADMIN_SECTION`.

### Audit emission (D-103 / D-105 / criterion 3)

| Element | Citation | Shape |
|---|---|---|
| Same-tx outbox seam | `internal/world/service.go:816` | `intent := s.buildIntent(kind, wmodel.AggregateCharacter, id, subjectID.subject, payload)` then `s.mutator.<op>(ctx, intent, ...)` |
| Character envelope kinds | `internal/world/service.go:52-58` | `kindCharacterUpdated`, `kindCharacterRetired`, `kindCharacterUnretired`, `kindCharacterProfileUpdate`, … |
| Command→kind parity table | `internal/world/mutator.go:100-106` | `{Command: "RetireCharacter", Kind: kindCharacterRetired}`, `{Command: "UnretireCharacter", Kind: kindCharacterUnretired}` |
| Prose payload is names-only | `internal/world/payloads.go:445-466` | `BuildCharacterProfileUpdatePayload(characterID, changedAttributes)` — doc comment at `:449-453`: *"The VALUES are deliberately absent. Profile prose is player-authored personal content and the taxonomy's payload rule is new-values-only AND erasure-safe"* |
| Lifecycle payload is new-values-only | `internal/world/payloads.go:314-317` | `type CharacterLifecycleChangePayload struct { CharacterID string \`json:"character_id"\`; Status string \`json:"status"\` }` — **no before-status field** |
| Lifecycle builder | `internal/world/payloads.go:434-443` | `func BuildCharacterLifecyclePayload(characterID ulid.ULID, status Status) ([]byte, error)` |
| Taxonomy declares the payload shape | `internal/world/outbox/taxonomy.go:201-204` | `characterLifecyclePayload = []PayloadField{{Name: "character_id", Type: "ulid"}, {Name: "status", Type: "string"}}` |
| Per-kind schema version | `internal/world/outbox/taxonomy.go:144-145` | `{Kind: KindCharacterRetired, …, SchemaVersion: 1, Payload: characterLifecyclePayload}` |
| Registry revision constant | `internal/world/outbox/taxonomy.go:29` | `const AppSchemaVersion = 3` — doc at `:22-24`: *"Bump it whenever the set of declared kinds or any per-type payload schema changes."* |
| `events_audit` single writer | `internal/eventbus/audit/projection.go:304` | `func writeAuditRow(ctx, pool, subject string, msg jetstream.Msg) error`; the `INSERT INTO events_audit` is at `:376` |
| `Caller.subject` is an audit requirement | `internal/world/caller.go:42-45` | *"It is also the world-change outbox envelope Actor (see buildIntent / buildMoveIntent), so its byte identity is an audit requirement, not only an authz one."* |

> CONTEXT.md cites `projection.go:434` for `writeAuditRow`; the function is at **`:304`**
> and its INSERT at `:376`. The claim (single writer) holds; the line number has drifted.

**The before-status is already in scope — no repository change needed.** `RetireCharacter`
(`internal/world/service.go:1308`) reads the character at step (2), `:1325`
(`char, err := s.characterRepo.Get(ctx, characterID)`), explicitly *"Version arms the
precheck, Status arms the lifecycle guard"* (`:1324`). `char.Status` is switched on at
`:1345` and remains live at `:1365` where `BuildCharacterLifecyclePayload(characterID,
StatusRetired)` is called. D-103 therefore reduces to a payload widening:

1. Add `BeforeStatus` to `CharacterLifecycleChangePayload` (`payloads.go:314-317`)
2. Add the parameter to `BuildCharacterLifecyclePayload` (`payloads.go:434`)
3. Pass `char.Status` at `service.go:1365` and the `UnretireCharacter` equivalent (`:1408`+)
4. Extend `characterLifecyclePayload` (`taxonomy.go:201-204`)
5. Bump both lifecycle kinds' `SchemaVersion` 1→2 (`taxonomy.go:144-145`)
6. Bump `AppSchemaVersion` 3→4 (`taxonomy.go:29`) — its own doc comment requires this

Note `CharacterRepository.SetStatus` (`internal/world/postgres/character_repo.go:502-553`)
uses `RETURNING version` only (`:514`) and **cannot** yield the old status —
Postgres `RETURNING` returns post-update values. That is fine precisely because the service
layer already holds it. Do not add a repo round-trip.

### Search (D-106)

| Claim | Citation | Finding |
|---|---|---|
| **`pg_trgm` already exists** | `internal/store/migrations/000001_baseline.sql:17` | `CREATE EXTENSION IF NOT EXISTS pg_trgm;` under an `-- ═══ Extensions ═══` header |
| Three `gin_trgm_ops` precedents already ship | `000001_baseline.sql:110,136,159` | `CREATE INDEX idx_locations_name_trgm ON locations USING gin (name gin_trgm_ops);` and the same for `exits`, `objects` |
| The down migration drops it | `000001_baseline.sql:428` | `DROP EXTENSION IF EXISTS pg_trgm;` |
| `characters_normalized_name_key` is a btree UNIQUE | `000056_character_normalized_name_unique.sql:68-69` | `CREATE UNIQUE INDEX IF NOT EXISTS characters_normalized_name_key ON characters (normalized_name);` — cannot serve a prefix-free substring query |
| `players.username` exists and is unique | `000001_baseline.sql:56,58` | `CREATE TABLE players (`, `username TEXT UNIQUE NOT NULL,` |
| No search method, no join | `internal/world/postgres/character_repo.go` receivers; `rg 'players\.username\|JOIN players' internal/world/postgres/` → zero | `ListByPlayer` at `:340`, `ListAll` at `:708`; nothing else reads |
| **Next migration number is 000057** | `ls internal/store/migrations/` | highest is `000056_character_normalized_name_unique.sql`; `000055_backfill_character_normalized_names.go` is a **Go** migration (the apparent gap) |

**This materially de-risks D-106.** Its planner note — *"`pg_trgm` must be creatable in the
target deployment (not universally available on managed Postgres without a pre-approved
extension list)"* — is **already discharged**: the extension is a hard dependency of the
baseline migration, so any deployment that can run migration 000001 at all already has it.
Migration 000057 needs only `CREATE INDEX ... USING gin (... gin_trgm_ops)`, following the
three in-tree precedents. The `CREATE EXTENSION` line is optional belt-and-braces
(idempotent via `IF NOT EXISTS`) and is **not** a new deployment requirement.

D-106's `CONCURRENTLY` warning is correct and worth restating: goose wraps each migration in
a transaction, `CREATE INDEX CONCURRENTLY` cannot run inside one, and the escape hatch
(`-- +goose NO TRANSACTION`, per `.claude/rules/database-migrations.md`) surrenders
atomicity. Use plain `CREATE INDEX`.

### `last_active_at` (D-107)

| Claim | Citation | Finding |
|---|---|---|
| Column exists, `BIGINT`, defaults `0` | `internal/store/migrations/000054_character_identity_and_lifecycle.sql:33` | `ADD COLUMN IF NOT EXISTS last_active_at BIGINT NOT NULL DEFAULT 0;` |
| Down migration drops it | `000054:94` | `ALTER TABLE characters DROP COLUMN IF EXISTS last_active_at;` |
| `characters.version` is `INTEGER` | `000049_world_version_guard.sql:22` | `ALTER TABLE characters ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;` |

`NOT NULL DEFAULT 0` confirms D-107's premise exactly: there is **no NULL** — the sentinel is
the integer `0`, so `NULLS LAST` is not the tool. The both-directions requirement is real:

- `ORDER BY last_active_at DESC` — `0` sorts last for free (column minimum).
- `ORDER BY last_active_at ASC` — `0` sorts **first**, which is the bug. Needs the explicit
  `ORDER BY (last_active_at = 0), last_active_at ASC` D-107 specifies.

A test exercising only `DESC` passes under the bug. See Validation Architecture.

### Web substrate

| Claim | Verification | Finding |
|---|---|---|
| `internal/web/` has zero `RoleAdmin` references | `rg 'RoleAdmin' internal/web/` | **zero hits** — ROADMAP's research flag confirmed |
| No `+error.svelte` anywhere | `find web/src -name '+error.svelte'` | **zero results** — #4903 confirmed |
| `bits-ui` version | `web/package.json:33` | `"bits-ui": "2.18.1"` |
| Svelte / SvelteKit | `web/package.json:27,37` | `"@sveltejs/kit": "2.69.3"`, `"svelte": "5.56.8"` |
| shadcn style preset | `web/components.json` | `"style": "nova"`, `"baseColor": "slate"`, ui alias `$lib/components/ui` |
| `WebCheckSessionResponse` has no `roles` | `api/proto/holomush/web/v1/web.proto:802-822` | fields 1–5: `player_name`, `player_id`, `is_guest`, `characters`, `default_character_id`. D-102 adds field **6** |
| `characteraccess` has 8 RPCs, no `Admin*` | `api/proto/holomush/characteraccess/v1/characteraccess.proto:30,37,43,62,69,74,86,97` | `GetCharacterProfile`, `ListMyCharacters`, `GetMyCharacter`, `CreateCharacter`, `UpdateCharacterProfile`, `UpdateCharacterDescription`, `SetDefaultCharacter`, `ListCharacterDirectory` |
| Gateway lifts token, core resolves | `internal/web/character_handlers.go:41,87,115,145,187,221,262,311` | `PlayerSessionToken: token,` — eight proxy sites, uniform convention |

**shadcn-svelte components installed today** (`web/src/lib/components/ui/`): `badge`,
`button`, `card`, `checkbox`, `command`, `dialog`, `dropdown-menu`, `input`, `input-group`,
`label`, `popover`, `resizable`, `scroll-area`, `separator`, `sheet`, `textarea`, `tooltip`.

**All ten named components are genuinely absent** — `table`, `pagination`, `empty`, `alert`,
`avatar`, `breadcrumb`, `skeleton`, `select`, `field`, `sonner`. Note `sheet` **is** already
installed, so the edit Sheet (D-109/D-110) needs no new component.

`web/CLAUDE.md` conventions that bind (`:103-160`): components added via
`pnpm dlx shadcn-svelte@latest add <name>`; custom components MUST use `cn()` from
`$lib/utils`, pull colors from `var(--color-*)`/`var(--mush-*)`, and expose a `class` prop;
Svelte 5 runes only (`$props()`, `$state()`, `$derived()`, `$effect()`), `{@render children()}`
not `<slot />`, `onclick=` not `on:click=`; **all form inputs MUST have `name` attributes for
Playwright E2E testability** and submit buttons MUST have `type="submit"`; auth guards must
restore session in `load()`, not `onMount()`.

### Existing fail-closed mechanisms Phase 6 must EXTEND, not duplicate

Per rule `7zy1161fh1`, these already exist and cover ground a new gate would re-cover:

| Mechanism | Citation | What it already proves |
|---|---|---|
| **Section registry set-equality** | `internal/admin/section/registry_test.go:227` | registry ID set == §10.1's seven, both directions |
| **`characteraccess` routing census** | `test/meta/characteraccess_routing_census_test.go` — 8 tests at `:496,530,601,626,681,708,780,856` | A `go/ast` census over `internal/grpc` **and** `internal/web` handler names, asserting every RPC reaches the shared gate. `TestCharacterAccessRoutingCensusAudiencePartition` (`:626`) enforces an **exhaustive** owner/public partition — **adding 8 admin RPCs without extending the partition turns this RED by construction.** That is the fail-closed mechanism D-99's "meta-test" should extend. |
| **Character RPC census** | `test/meta/character_rpc_census_test.go:309,350,376,404` | keyed on proto/wire names (the routing census is keyed on Go handler names — they are complements, `:106-114`) |
| **World taxonomy registry** | `internal/world/outbox/taxonomy.go:221-235` (`Lookup`, `IsDeclared`, `Kinds`) | an undeclared kind is `WORLD_TAXONOMY_UNKNOWN_KIND`, never silently accepted |
| **`hostcap` completeness proof** | `internal/plugin/hostcap/descriptor_completeness_test.go:21,41` | the mutate-and-restore technique for proving a fail-closed arm is live |
| **Invariant registry** | `docs/architecture/invariants.yaml:2194-2200` (`INV-PRIVACY-11`) | the admin-section indistinguishability property is already a bound invariant |

**`test/meta/` inventory** (26 files) — no admin-section meta-test exists there; the
section tests live in-package at `internal/admin/section/`. A cross-cutting Phase 6 meta-test
(e.g. "exactly one `+error.svelte`") is a legitimate new member.

### Invariant registry

Declared scopes (`docs/architecture/invariants.yaml`, `boundary:` keys at `:14,197,237,261,335,461,546,573,609,630,684,698,710,722,756,770,786`):
CRYPTO, PRIVACY, PRESENCE, SCENE, PLUGIN, EVENTBUS, CLUSTER, ACCESS, SESSION, STORE,
TELEMETRY, BRANDING, DOCS, COMMAND, (conversational content), CHANNELS, WORLD.

**There is no `INV-ADMIN` scope.** `rg 'INV-ADMIN'` → zero hits. Phase 6 must **not** mint
one. `INV-PRIVACY-11` (`:2194-2200`) is the existing admin-section invariant, and its
neighbouring comment at `:2404-2407` records the routing rule verbatim: *"INV-ACCESS rather
than INV-PRIVACY because this scope's description names attribute-provider invariants
explicitly, while INV-PRIVACY's boundary forwards ABAC policy evaluation here by name."*

**Guidance:** a new fail-closed-declaration invariant belongs in **`INV-ACCESS`** (its
boundary at `:573` is *"Access control evaluation"*); a new disclosure/indistinguishability
invariant belongs in **`INV-PRIVACY`**. And per `.claude/rules/invariants.md`, a
`.planning/**/*-SPEC.md`-origin id is **outside** the orphan check's walk root and MUST be
hand-registered — the pattern `INV-PRIVACY-11` and `INV-ACCESS-15` already follow.

---

## UNVERIFIED / ASSUMED

Flagged explicitly. None of these should be planned as fact without confirmation.

| # | Claim | Why unverified | Risk if wrong |
|---|---|---|---|
| A1 | The single-interface token extraction (`req.(interface{ GetPlayerSessionToken() string })`) compiles cleanly against every admin request type | The admin request messages do not exist yet | Low — falls back to `hostcap`'s per-method `Extract` shape, which is proven in-tree |
| A2 | Stashing the resolved player in `context.Context` from the interceptor is the right seam | No in-tree precedent read for a *core* gRPC interceptor doing this; `internal/admin/socket`'s `PeerCredMiddleware` is the nearest analogue but was **not opened this session** | Medium — double session lookup per request if not done; verify the socket precedent before planning |
| A3 | `svelte-sonner` is the dependency shadcn-svelte's `sonner` component pulls | Inferred from the shadcn-svelte registry convention, not read from the registry manifest | Low — surfaces immediately at `pnpm add` time |
| A4 | No admin RPC will need a token field named other than `player_session_token` | Convention is uniform across 8 existing sites but the admin messages are unwritten | Low |
| A5 | `AdminListSections`/`AdminGetSection` on `CharacterAccessServer` would trip `TestCharacterAccessRoutingCensusIsCharacterScoped` (`:856`) | That test asserts *scene* facade names never reach the derived sets; whether non-character **admin** names trip it depends on the receiver-type sets, which I read only in outline | **Medium — this is the highest-value open item.** It bears directly on Claude's Discretion ("which service do the section RPCs land on"). Read `characterFacadeReceiverTypes()` and `characterNonHandlerMethods()` before choosing. |
| A6 | The ROADMAP's grab-handle line is stale | CONTEXT.md D-109 supersedes it, and `bits-ui@2.18.1` is confirmed installed, but I did **not** read bits-ui's Sheet source to confirm it lacks swipe-dismiss | Low — D-109 drops the handle either way; the decision is not contingent on the mechanism |

**On the ROADMAP staleness the brief asked about:** confirmed. The ROADMAP block says *"Its
grab handle promises drag-to-dismiss: honor it or drop it"*; `06-CONTEXT.md:202-208` (D-109)
**drops** it. The ROADMAP text is stale and is one of the seven owed amendments — though note
D-109 is *not* itself in CONTEXT.md's amendments table (which lists 7 items covering D-99,
D-103, D-104, D-106, D-100, D-108, D-105). The grab-handle line is an **eighth** stale
ROADMAP statement not currently tracked. Recommend adding it to the amendment issue.

---

## Standard Stack

**No new Go dependencies.** Every mechanism this phase needs is in-tree: `google.golang.org/grpc`
(interceptor), `github.com/samber/oops` (typed codes), `github.com/pressly/goose/v3`
(migration), the existing `internal/access` ABAC engine, and the `internal/world/outbox` seam.

### Web additions

| Item | Version | Purpose | Nature |
|---|---|---|---|
| shadcn-svelte components ×10 | CLI-generated | `table`, `pagination`, `empty`, `alert`, `avatar`, `breadcrumb`, `skeleton`, `select`, `field`, `sonner` | **Code generation into the repo**, not npm deps — 9 of 10 compose over `bits-ui@2.18.1` + `tailwind-variants@^3.2.2`, both already present |
| `svelte-sonner` | `1.1.1` | toast runtime behind the `sonner` component (D-110's receipt) | **The one genuinely new npm dependency** |

**Installation:** `cd web && pnpm dlx shadcn-svelte@latest add table pagination empty alert avatar breadcrumb skeleton select field sonner`
(per `web/CLAUDE.md:111`). Confirm `style: "nova"` output matches the existing components.

## Package Legitimacy Audit

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---|---|---|---|---|---|---|
| `svelte-sonner` | npm | created 2023-07-02 (~3 yrs) | 487,547/wk | `github.com/wobsoriano/svelte-sonner` | **OK** | Approved — but see note |

Verified via `npm view svelte-sonner version time.created repository.url` → `1.1.1`,
`2023-07-02T07:59:33.632Z`, `git+https://github.com/wobsoriano/svelte-sonner.git`, and
`api.npmjs.org/downloads/point/last-week` → `487547`. Three-year age, near-half-million
weekly downloads, and a real source repo. No `[SLOP]`, no `[SUS]`.

**Provenance note:** this package name was **inferred from the shadcn-svelte convention**,
not read from an authoritative shadcn-svelte registry manifest — so per the package-name
provenance rule it is `[ASSUMED]` despite passing every registry signal. It will be
confirmed or corrected the moment the CLI runs. No `checkpoint:human-verify` is warranted
for a package with these signals, but the planner should treat the *name* as CLI-determined
rather than plan-determined.

**Packages removed due to [SLOP]:** none. **Flagged [SUS]:** none.

---

## Architecture Patterns

### Pattern 1: Descriptor table + fail-closed interceptor (D-99)

**What:** A `map[string]SectionDescriptor` keyed by gRPC method (or `service/method`), an
interceptor that classifies `info.FullMethod`, and a completeness meta-test.

**Transposed from:** `internal/plugin/hostcap/` — `descriptor.go:69`, `interceptor.go:191`,
`descriptor_completeness_test.go:21,41`.

**Skeleton** (structure only; every value below is a placeholder to be settled at plan time —
none of these identifiers exist in-tree yet):

```go
// Classification mirrors hostcap's classifyHostMethod (interceptor.go:150-167):
// cut "/pkg.Service/Method", look the method up, and DENY on a miss.
descriptor, declared := AdminDescriptors[method]
if !declared {
    // The fail-closed arm. Never default to a section, never pass through.
    return nil, adminDeny("ADMIN_SECTION_NOT_DECLARED", ...)
}
// Subject: per-request, unlike hostcap's construction-time PluginName.
tokened, ok := req.(interface{ GetPlayerSessionToken() string })
if !ok { return nil, adminDeny("ADMIN_SECTION_NO_SUBJECT", ...) }
ps, err := resolvePlayerSessionWithRepo(ctx, sessionRepo, tokened.GetPlayerSessionToken())
// ... then the SHIPPED helper, unchanged:
entry, err := section.AssertSectionAccess(ctx, engine, ps.PlayerID.String(),
    descriptor.SectionID, descriptor.Action)
```

**Critical:** the interceptor must be installed in **both** `NewGRPCServer`
(`internal/grpc/server.go:630`) and `NewGRPCServerInsecure` (`:646`). Neither currently
takes any interceptor. A test asserting both constructors carry the interceptor is cheap and
closes the divergence.

### Pattern 2: Extend the audience partition, don't add a census

The `characteraccess` routing census (`test/meta/characteraccess_routing_census_test.go:626`)
already enforces an **exhaustive** partition over facade handler names. Adding admin RPCs
without a third partition member turns it RED automatically. **Extend it.** Writing a parallel
"every admin RPC is gated" census would create a second maintenance surface over ground the
first already holds — the exact hazard `01-SPEC.md` §2.6 names.

### Pattern 3: Payload widening through the taxonomy ratchet

The taxonomy registry (`internal/world/outbox/taxonomy.go`) is the declared contract. A
payload change is a six-step edit (enumerated under VERIFIED → Audit emission), and step 6 —
bumping `AppSchemaVersion` 3→4 — is required by the constant's own doc comment at `:22-24`.

### Anti-Patterns to Avoid

- **Re-implementing the section gate per handler.** `AssertSectionAccess` is complete.
  ADMIN-02's "re-asserts its own gate" prose is superseded by D-99 (amendment #1 owed).
- **A route guard or `internal/web/` authorization decision.** Forbidden by
  `.claude/rules/gateway-boundary.md`, §10.4, and ADMIN-01. The gateway lifts the token; core decides.
- **Building a client-side section registry.** D-101; §10.1 names it as Pitfall 7's hazard.
- **A direct `INSERT INTO events_audit`.** One writer only (`projection.go:376`).
- **Wiring `DeleteCharacter` to an admin button.** There is no `AdminDeleteCharacter`.
- **`CREATE INDEX CONCURRENTLY` in a goose migration.** Forces `-- +goose NO TRANSACTION`.
- **Minting an `INV-ADMIN` scope.** No such scope exists; use `INV-ACCESS` or `INV-PRIVACY`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---|---|---|---|
| Admin authorization | a new gate | `section.AssertSectionAccess` (`gate.go:234`) | shipped, tested by 10 tests, binds INV-PRIVACY-11 |
| Section registry | a new table | `section.All()` / `section.Lookup` (`registry.go:134,146`) | seven entries, set-equality-pinned |
| Boot validation | a new startup check | `section.ValidateAtBoot` (`boot.go:34`) | already called at `subsystem.go:156` |
| Section ABAC policy | a new seed | `seed:admin-section-access` (`seed.go:985`) | resource-**type** scoped — new sections cost zero policy |
| "Every RPC is gated" proof | a new census | extend `characteraccess_routing_census_test.go:626` | already exhaustive; a second census duplicates the surface |
| Fail-closed-arm proof | a bespoke harness | the mutate + `t.Cleanup` restore technique at `descriptor_completeness_test.go:41` | in-tree precedent, proves the arm is live |
| Substring search | `LIKE '%x%'` scans | `pg_trgm` GIN, following `000001_baseline.sql:110` | extension + three index precedents already ship |
| Session→player | a new resolver | `resolvePlayerSessionWithRepo` (`auth_handlers.go:174`) | handles hashing + typed repo errors |
| Envelope emission | a new writer | `s.buildIntent(...)` → `s.mutator.*` (`service.go:816`) | INV-WORLD-1 same-transaction seam |

**Key insight:** this phase's dominant failure mode is not under-building — it is
**rebuilding**. Four fail-closed mechanisms already exist. Every one of them turns RED on
its own when Phase 6's surface lands incorrectly, and none of them needs a sibling.

## Common Pitfalls

### Pitfall 1: The insecure test server runs ungated
**What goes wrong:** the interceptor is mounted in `NewGRPCServer` but not
`NewGRPCServerInsecure` (`internal/grpc/server.go:630` vs `:646`).
**Why:** two constructors, no existing interceptor in either, so there is no pattern to copy.
**How to avoid:** mount in both; add a test asserting both carry it.
**Warning signs:** integration tests pass while a manual production probe denies.

### Pitfall 2: `last_active_at` sorted in one direction only
**What goes wrong:** `ORDER BY last_active_at ASC` puts every `never` character first.
**Why:** the sentinel is integer `0` (`000054:33`), the column minimum — free correctness
descending, silent bug ascending.
**How to avoid:** `ORDER BY (last_active_at = 0), last_active_at ASC`; test **both** directions.
**Warning signs:** a passing sort test that only exercises most-recent-first.

### Pitfall 3: Denial code leaks the section id
**What goes wrong:** the interceptor wraps `DENY_ADMIN_SECTION` with the section id in the
message, breaking INV-PRIVACY-11.
**Why:** `.claude/rules/grpc-errors.md` warns that a distinguishing field substituted into a
refusal reaches the client — and here that field **is** the disclosure (`gate.go:119-121`).
**How to avoid:** map to `codes.PermissionDenied` with a **static** message; assert the
**top-level** code with `oops.AsOops(err).Code()`, not chain-walking `errutil.AssertErrorCode`.

### Pitfall 4: Double session lookup per admin request
**What goes wrong:** the interceptor resolves the player, then the handler resolves it again.
**How to avoid:** stash the resolved player in ctx behind an unexported key (see A2 — verify
the `internal/admin/socket` precedent first).

### Pitfall 5: The payload widening forgets the taxonomy
**What goes wrong:** `CharacterLifecycleChangePayload` gains `before_status` but
`characterLifecyclePayload` (`taxonomy.go:201-204`) and the two `SchemaVersion: 1` entries
(`:144-145`) do not.
**How to avoid:** all six steps together, `AppSchemaVersion` 3→4 included.

### Pitfall 6: A second `+error.svelte`
**What goes wrong:** a nested boundary under `(authed)` or `/admin` renders a *different*
page for an admin miss than for an anonymous one, destroying per-viewer indistinguishability
**with nothing failing**.
**How to avoid:** ship the meta-test asserting **exactly one** `+error.svelte` under
`web/src/routes/`. This is a legitimately new check — no existing mechanism covers it.

### Pitfall 7: Stale generated artifacts
**What goes wrong:** proto changes land without regenerated `.pb.go` / `_pb.ts`.
**How to avoid:** `task proto && task web:generate`, commit `pkg/proto/**/*.pb.go` + web
`*_pb.ts` in the same change (root `CLAUDE.md`, "Generated code"). `task lint:proto` must be green,
and every new proto element needs a Go-grounded doc comment
(`.claude/rules/proto-doc-comments.md` — buf `COMMENTS` + name-echo gate, no exemptions).

---

## Validation Architecture

### Test Framework

| Property | Value |
|---|---|
| Go unit | stdlib `testing` + `testify`; ACE naming (`.claude/rules/testing.md`) |
| Go integration | Ginkgo/Gomega, `//go:build integration` |
| Meta-tests | `test/meta/` (26 files today) |
| Web unit | Vitest (`web/package.json`) |
| E2E | Playwright, `web/e2e/` (20 specs; `admin.spec.ts` exists but covers **telnet admin commands** — `test.describe('Admin Commands')` at `:44`, password reset at `:45`, non-admin denial at `:110` — **not** the web portal) |
| Quick run | `task test -- ./internal/admin/...` |
| Full suite | `task test`, `task test:int`, `task test:e2e` |
| Pre-PR gate | `task pr-prep` (Taskfile.yaml:1079) |

Verified `task` targets against `Taskfile.yaml`: `test:185`, `test:cover:250`, `test:int:265`,
`test:e2e:310`, `lint:202`, `lint:proto:775`, `proto:576`, `web:generate:686`, `pr-prep:1079`.

### Phase Requirements → Test Map

| Req | Behavior | Tier | Command | Exists? |
|---|---|---|---|---|
| ADMIN-01 / crit 1 | non-admin denied over the wire; mapped code + generic message; typed `DENY_*`; paired positive control | integration | `task test:int -- ./test/integration/...` | ❌ Wave 0 |
| ADMIN-02 | undeclared method → `ADMIN_SECTION_NOT_DECLARED` | unit + meta | `task test -- ./internal/admin/...` | ❌ Wave 0 |
| ADMIN-02 | descriptor completeness (set equality method↔section) | meta | `task test -- ./test/meta/` | ❌ Wave 0 (extend `:626`) |
| ADMIN-03 | list / substring search / get | integration | `task test:int` | ❌ Wave 0 |
| ADMIN-04 | field mask excludes roles | unit | `task test -- ./internal/grpc/` | ❌ Wave 0 |
| ADMIN-05 | admin retire uses the same lifecycle states | unit | `task test -- ./internal/world/` | ⚠️ partial — `RetireCharacter` tested; admin path new |
| ADMIN-06 / crit 3 | envelope in-tx, before-status, `player:<id>` actor, no direct insert | integration | `task test:int` | ❌ Wave 0 |
| ADMIN-07 / crit 5 | nav from `AdminListSections`, not `{#if}` | E2E | `task test:e2e` | ❌ Wave 0 |
| ADMIN-08 | `roles` present, non-authoritative | unit + E2E | `task test -- ./internal/web/` | ❌ Wave 0 |
| EXT-01/03/04 | registry, descriptor, set equality | unit | `task test -- ./internal/admin/section/` | ✅ **exists** (`registry_test.go:44,68,101,227`) |
| EXT-02 / crit 4 | six sections gated, `NOT_IMPLEMENTED` after gate, over the wire | integration | `task test:int` | ⚠️ helper-level exists (`gate_test.go:209`); wire-level ❌ |
| D-107 | `never` sorts last **both** directions | integration | `task test:int` | ❌ Wave 0 |
| D-110 | success closes Sheet + in-place row; `Aborted` keeps it open | E2E | `task test:e2e` | ❌ Wave 0 |
| Not-found | exactly one `+error.svelte` | meta | `task test -- ./test/meta/` | ❌ Wave 0 |

### Assertions that would pass under a plausible WRONG implementation

This is the section that matters. Each row names a naive assertion, the bug it tolerates, and
the discriminating assertion.

| # | Naive assertion | Wrong impl it passes under | Discriminating assertion |
|---|---|---|---|
| V1 | `require.Error(t, err)` on a non-admin admin RPC | the RPC is broken for *everyone* (misconfigured interceptor, nil engine) — a denial that proves nothing about authorization | **Paired positive control**: the same call, same section, as an admin, MUST succeed. The shipped `gate_test.go:135` already does exactly this — copy the shape to the wire level. |
| V2 | `errutil.AssertErrorCode(t, err, "DENY_ADMIN_SECTION")` | a **double-wrapped** error `oops(INTERNAL).Wrap(oops(DENY_ADMIN_SECTION))` — `AssertErrorCode` chain-walks and passes while the wire code is `Internal` | `oops.AsOops(err).Code()` for the **top-level** code, per `.claude/rules/grpc-errors.md`. Assert `status.Code(err)` separately. |
| V3 | asserting the denial message is non-empty | a message carrying the section id — INV-PRIVACY-11 broken | **Differential string equality**: registered vs unregistered id for the same denied caller must produce byte-identical messages. `gate_test.go:178` is the in-tree template. |
| V4 | `ORDER BY last_active_at DESC` puts `never` last | `ASC` puts `never` **first** — the D-107 bug | Assert **both** directions in one table-driven test. A one-direction test passes under the bug. |
| V5 | "an admin sees the nav entry" | nav drawn from a client-side registry (D-101's hazard) — passes while the server never decides | Assert the **denial at the RPC** for a viewer whose nav omitted the link, and that a hand-crafted direct call is refused. Criterion 5's "drawing a link the viewer may not use still results in a denial at the RPC". |
| V6 | "the six planned sections return `SECTION_NOT_IMPLEMENTED`" | tested only **as an admin** — a non-admin might also get `NOT_IMPLEMENTED`, leaking that the section exists | For each of the seven, assert non-admin → `DENY_ADMIN_SECTION` and admin → `SECTION_NOT_IMPLEMENTED`. Iterate `section.All()`, never hard-code (`gate_test.go:114` template). |
| V7 | "an audit row appears" | written by a **second writer** rather than projected | Assert the **envelope** is in the mutation transaction (roll back → no envelope), and that the row arrives asynchronously via the projection. Grep-assert no `INSERT INTO events_audit` outside `projection.go`. |
| V8 | "the retire envelope carries a status" | carries only the **new** status (today's shape, `payloads.go:314-317`) — D-103 unimplemented, test green | Assert `before_status` is present **and differs** from the new status on a real active→retired transition. |
| V9 | descriptor completeness test iterates the descriptor table | a method with **no** entry is invisible to a loop over entries — the exact fail-open | Iterate the **served method set** (registered gRPC methods), not the descriptor table. `descriptor_test.go:19` iterates `plugins.CapabilityServiceNames`, not `Descriptors` — copy that direction. |
| V10 | `ADMIN_SECTION_NOT_DECLARED` has a unit test | the arm is dead code — nothing reaches it | Use the mutate + `t.Cleanup` restore technique (`descriptor_completeness_test.go:41`) to strip a **real** declaration and prove the guard fires. |
| V11 | "exactly one `+error.svelte`" counted with `rg -c` | `-c` counts **matching lines**, not files; also passes if zero exist | Enumerate files, assert length **== 1** (not `>= 1`). Prove it goes RED by adding a second in a temp fixture. |

### Sampling Rate

- **Per task commit:** `task test -- ./internal/admin/... ./internal/world/...`
- **Per wave merge:** `task test` + `task test:int`
- **Phase gate:** `task pr-prep` green, then `task test:e2e`, before `/gsd-verify-work`

### Wave 0 Gaps

- [ ] Ten shadcn components + `svelte-sonner` install — blocks every web task
- [ ] `web/src/routes/+error.svelte` (#4903) — blocks the not-found surface
- [ ] Admin proto messages + `task proto && task web:generate` — blocks all RPC work
- [ ] Interceptor mount in **both** `server.go:630` and `:646`
- [ ] Migration `000057` (`pg_trgm` GIN on `characters.normalized_name`, `players.username`)
- [ ] Integration harness for admin RPCs (check `internal/testsupport/integrationtest` covers the shape before building)
- [ ] `web/e2e/admin-portal.spec.ts` — **new file**; do not extend `admin.spec.ts`, which is telnet admin commands

---

## Security Domain

`security_enforcement: true` (`.planning/config.json:46`); `nyquist_validation: true` (`:24`);
`tdd_mode: true` (`:49`).

### Applicable ASVS Categories

| Category | Applies | Standard control (in-tree) |
|---|---|---|
| V2 Authentication | yes | `resolvePlayerSessionWithRepo` + `auth.HashSessionToken` (`auth_handlers.go:174,178`) — token is hashed before lookup |
| V3 Session Management | yes | existing `PlayerSessionRepository`; no new session mechanism |
| **V4 Access Control** | **yes — the phase's core** | ABAC, default-deny, `section.AssertSectionAccess`; **never** `PlayerHasRole` |
| V5 Input Validation | yes | `buf.validate` on proto (`characteraccess.proto:272` precedent `string.min_len = 1`); parameterized SQL only |
| V6 Cryptography | no | D-103 deliberately keeps prose out of the payload, so `crypto-reviewer` is **not** triggered — but it fires if that decision is revisited toward encryption |
| V7 Error Handling / Logging | yes | `.claude/rules/grpc-errors.md`; `errutil.LogErrorContext`; `slog.*Context` only |
| V8 Data Protection | yes | erasure-safe payloads; `events_audit` is retained, so D-103/D-104 are data-protection decisions |

### Known Threat Patterns

| Pattern | STRIDE | Mitigation |
|---|---|---|
| Direct RPC call bypassing the route | **Elevation of Privilege** | core-side interceptor; criterion 1 asserts the bypass path explicitly |
| Registry-enumeration oracle | **Information Disclosure** | D-06 gate-then-distinguish (`gate.go:103-134`); INV-PRIVACY-11 |
| Inner-error leakage past the boundary | Information Disclosure | static message, translate at one layer (`.claude/rules/grpc-errors.md`) |
| New admin RPC ships ungated | Elevation of Privilege | fail-closed descriptor (`ADMIN_SECTION_NOT_DECLARED`) + completeness meta-test |
| Nav-visibility mistaken for authorization | Elevation of Privilege | D-102/ADMIN-08: `roles` changes only what is drawn |
| Player prose copied into a retained table | Information Disclosure | D-103 names-only for prose |
| Player↔alt linkage disclosure | Information Disclosure | D-104 omits the acting character |
| SQL injection via the search term | Tampering | parameterized query; normalize through §6.1 before matching |
| Denial-of-service via unbounded search | DoS | `pg_trgm` GIN index + pagination |

**Required gate:** `abac-reviewer` MUST run — this phase touches `internal/access/`
(`/holomush-dev:review-abac`). `crypto-reviewer` is not triggered under D-103 as written.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|---|---|---|
| A1 | Single-interface token extraction compiles for all admin request types | Architecture Patterns | Low — `hostcap` per-method `Extract` is the proven fallback |
| A2 | Context-stashing the resolved player is the right seam | Architecture Patterns | Medium — verify `internal/admin/socket` `PeerCredMiddleware` first |
| A3 | `svelte-sonner` is what shadcn-svelte's `sonner` pulls | Standard Stack | Low — surfaces at install |
| A4 | All admin requests will name the token `player_session_token` | Transposition | Low |
| A5 | Section RPCs on `CharacterAccessServer` may trip the character-scope census (`:856`) | Fail-closed mechanisms | **Medium — read `characterFacadeReceiverTypes()` before choosing the host service** |
| A6 | `bits-ui@2.18.1` Sheet lacks swipe-dismiss | UNVERIFIED | Low — D-109 drops the handle regardless |

---

## Open Questions

1. **Does EXT-04 mean more than what shipped?** EXT-04 (`REQUIREMENTS.md:234`) says "set
   equality between the section registry and the **descriptor set**". Phase 2 made the
   descriptor a *required field* rather than a parallel table, so there is no separate
   descriptor set to compare against — the property is structural and proved by
   `registry_test.go:68`. **Recommendation:** treat EXT-04 as satisfied and record the
   reading; do not manufacture a parallel table to compare against. Flag for maintainer
   confirmation, since it changes whether Phase 6 owes work here.

2. **Which service hosts `AdminListSections`/`AdminGetSection`?** (Claude's Discretion.)
   Turns on A5. Read `characterFacadeReceiverTypes()` and
   `TestCharacterAccessRoutingCensusIsCharacterScoped` (`:856`) before deciding. A separate
   `AdminService` likely avoids widening the character census's receiver sets — which that
   test exists to prevent — but costs a new census surface.

3. **Is there an existing admin integration harness?** `internal/testsupport/integrationtest`
   was not opened this session. Check before building one (rule `7zy1161fh1`).

4. **The eighth stale ROADMAP statement.** The grab-handle line is stale per D-109 but is not
   in CONTEXT.md's 7-row amendments table. Add it.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|---|---|---|---|---|
| PostgreSQL + `pg_trgm` | D-106 search | ✓ | extension created at `000001_baseline.sql:17` | — |
| goose | migration 000057 | ✓ | `github.com/pressly/goose/v3` | — |
| `bits-ui` | Sheet, select, etc. | ✓ | 2.18.1 | — |
| `tailwind-variants` | shadcn components | ✓ | ^3.2.2 | — |
| `svelte-sonner` | toast (D-110) | ✗ | — | none needed; add it |
| Docker | `task test:int` / `test:e2e` | assumed ✓ | — | — |

**Missing with no fallback:** none. **Missing with fallback:** `svelte-sonner` — install.

---

## Sources

### Primary (HIGH confidence) — opened this session
- `internal/admin/section/{registry,gate,boot}.go` and `{registry,gate,boot}_test.go`
- `internal/plugin/hostcap/{descriptor,interceptor}.go`, `descriptor_test.go`, `descriptor_completeness_test.go`
- `internal/admin/auth/operator_admin.go`
- `internal/grpc/{server,characteraccess_service,auth_handlers}.go`
- `internal/world/{service,payloads,caller,mutator}.go`, `internal/world/outbox/taxonomy.go`
- `internal/world/postgres/character_repo.go`
- `internal/eventbus/audit/projection.go`
- `internal/access/{prefix.go,policy/seed.go}`, `internal/bootstrap/setup/subsystem.go`
- `internal/store/migrations/{000001,000049,000054,000056}`
- `test/meta/characteraccess_routing_census_test.go`, `character_rpc_census_test.go`
- `api/proto/holomush/{characteraccess,web}/v1/*.proto`
- `web/{package.json,components.json,CLAUDE.md}`, `web/e2e/admin.spec.ts`
- `docs/architecture/invariants.yaml`, `Taskfile.yaml`, `.planning/REQUIREMENTS.md`, `06-CONTEXT.md`

### Secondary (MEDIUM confidence)
- `npm view svelte-sonner` + `api.npmjs.org` downloads endpoint (registry signals only)

### Tertiary (LOW confidence)
- shadcn-svelte component→dependency mapping (convention, not read from the registry manifest)

## Metadata

**Confidence breakdown:**
- Phase 2 inventory: **HIGH** — every file opened, every line cited
- Transposition analysis: **HIGH** for the break diagnosis (zero `metadata.FromIncomingContext`,
  no interceptor chain, token-in-body all directly verified); **MEDIUM** for the recommended shape
- Audit path: **HIGH** — the six-step widening and the in-scope before-status are both read
- Search / migration: **HIGH** — `pg_trgm` presence is decisive and changes D-106's risk
- Web substrate: **HIGH** — component list, absent `+error.svelte`, proto shape all verified
- Validation architecture: **MEDIUM-HIGH** — wrong-impl analysis is reasoned, not empirical

**Research date:** 2026-08-13
**Valid until:** ~2026-09-12 (30 days; stable in-tree substrate)
