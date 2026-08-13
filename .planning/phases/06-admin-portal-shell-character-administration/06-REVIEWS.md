---
phase: 6
reviewers: [codex, opencode]
reviewed_at: 2026-08-13T20:31:09Z
plans_reviewed:
  - 06-01-PLAN.md
  - 06-02-PLAN.md
  - 06-03-PLAN.md
  - 06-04-PLAN.md
  - 06-05-PLAN.md
  - 06-06-PLAN.md
  - 06-07-PLAN.md
  - 06-08-PLAN.md
reviewer_models:
  codex: codex-cli 0.147.0 (default model)
  opencode: opencode 1.18.15 / openrouter/moonshotai/kimi-k3
---

# Cross-AI Plan Review — Phase 6

## How this review was run

Both lanes are source-grounded and ran inside the worktree with full repo read access. The
8 plans (297 KB) do not fit a single prompt, so each reviewer covered all 8 across multiple
passes carrying an **identical** preamble (PROJECT.md head, the ROADMAP phase section,
REQUIREMENTS.md, and 06-CONTEXT.md). `06-RESEARCH.md`, `06-UI-SPEC.md`, and `06-PATTERNS.md`
were referenced by path rather than inlined, and both reviewers demonstrably opened them.

| Reviewer | Pass | Plans | Result |
| --- | --- | --- | --- |
| Codex | A | 06-01, 06-02, 06-04, 06-05 | 15,955 B · 34 citations |
| Codex | B | 06-03, 06-06, 06-07, 06-08 | 12,510 B · 40 citations |
| OpenCode | A *(superseded)* | 06-01, 06-02, 06-04, 06-05 | **timeout kill** — see note |
| OpenCode | A1 | 06-01, 06-02 | 13,831 B · 30 citations |
| OpenCode | A2 *(superseded)* | 06-04, 06-05 | **timeout kill** — see note |
| OpenCode | A2a | 06-04 | 10,764 B · 14 citations |
| OpenCode | A2b | 06-05 | 11,315 B · 33 citations |
| OpenCode | B | 06-03, 06-06, 06-07, 06-08 | 13,324 B · 14 citations |

**Two OpenCode passes were killed at the lane's 660 s timeout floor and are excluded.** Both
are recorded rather than hidden, because one of them exposed a tooling false-green: the first
pass A ran 11 m 01 s, emitted 658 bytes of *inter-tool narration* and no review, and the lane
recorded `ok: true, stubbed: false`. The A2 kill emitted no assistant text at all and was
correctly stubbed (`[spawn error: ETIMEDOUT]`, `stop reason=tool-calls, output tokens=102`).
The empty-output detector fires on zero text; **partial narration is indistinguishable from a
short review**. Both were re-run as smaller clusters until they completed. Prompt size was not
the driver — A2 (149 KB) died while the larger A1 (161 KB) finished; exploration depth was.

## Consensus Summary

Both reviewers rate the plans' *verification engineering* unusually strong — OpenCode calls
06-05's "the best I've seen in this phase's passes" — and both confirm the three named
"green build, dead property" hazards are met with criteria that genuinely go RED. Neither
reviewer found a fabricated `path:line`; nearly every citation both spot-checked resolved.

Both also conclude the plans are **not implementation-ready**. The defects are not polish:
several specified mechanisms cannot produce the behaviour their own acceptance criteria
require, and two of them sit on the phase's trust boundary. Codex: *"execution should pause
until the plans are amended."*

### Agreed Concerns (raised independently by both reviewers)

1. **HIGH — the production gRPC server is not built by either constructor the plans treat as
   the mounting authority.** `internal/grpc/server.go:630` and `:646` have **zero callers**;
   production builds inline at `cmd/holomush/sub_grpc.go:427` via `grpc.NewServer(...)`, which
   has already drifted (no `otelgrpc` StatsHandler, plus keepalive and `grpcProxy.Handler()`).
   OpenCode adds that the integration harness builds **no gRPC server at all**
   (`harness.go:163-164`: "the in-process CoreServer (no network transport)"). The keystone
   constructor test therefore stays green while production ships ungated — the exact hazard the
   plan claims to close. *Independently verified by the orchestrator before this file was
   written.*
2. **HIGH — `AdminGetSection`'s `SectionFromRequest` descriptor shape has no interceptor
   semantics.** 06-01 specifies exactly two gating arms (fixed `SectionID`,
   `EnumeratesAllSections`); 06-02 introduces a third but does not list
   `internal/grpc/admin_interceptor.go` in `files_modified` and moves the gate back into the
   handler — reintroducing the per-handler exception D-99 abolished, for the one RPC whose
   section id is attacker-controlled. OpenCode traces the composition further: under 06-01 as
   written the interceptor takes the fixed arm with an empty id and returns
   `ADMIN_SECTION_REQUEST_MALFORMED` (`gate.go:90-92`), so **every `AdminGetSection` call fails
   before the handler runs**.
3. **HIGH — admin writes are default-denied by a second ABAC gate that no task addresses.**
   Every reused world method runs its own `checkAccess` on `character:<id>` first —
   `RetireCharacter` action `"retire"` (`service.go:1321-1322`),
   `UpdateCharacterProfileAttributes` action `"write"` (`:1090-1091`). D-104 forces a
   player-flavoured caller, but the only player-principal policy is `seed:admin-section-access`
   scoped `resource is admin_section` (`seed.go:985-987`), and the admin catch-all
   `seed:admin-full-access` requires `principal is character` (`seed.go:105-107`). The admin
   passes the interceptor and is then denied inside the world service. `internal/access/policy/seed.go`
   is absent from every `files_modified`, and the threat model has no entry — so the executor
   would have to invent an authorization policy mid-execution, which is precisely what
   `abac-reviewer` is meant to gate on paper.
4. **HIGH — the 13-path field mask splits across two domain writes that one `expected_version`
   cannot fund.** The mask is `description` + twelve `profile.*` paths, but
   `UpdateCharacterProfileAttributes` *rejects* `description` with
   `CHARACTER_PROFILE_ATTRIBUTE_UNKNOWN` (`service.go:1074-1082`); `description` has its own
   domain method (`UpdateCharacterDescription`). A mixed mask therefore needs two writes, each
   bumping `characters.version` and consuming the single `expected_version`, so the second
   fails its precheck — and one RPC would emit two envelopes in two transactions. No plan
   specifies the fan-out, its transactionality, or which envelope carries `description`.
5. **HIGH/MEDIUM — the `roles` field has no injection path.** `CheckPlayerSessionResponse`
   carries no roles; `CoreServer` has no role-store dependency; `PlayerRoles`
   (`internal/store/role_store.go:108`) is deliberately **not** on the `RoleStore` interface.
   06-02 modifies neither `internal/grpc/server.go` nor the composition root, so "populate it
   from the existing player-role store" has nowhere to attach. (Codex HIGH, OpenCode MEDIUM.)
6. **HIGH/MEDIUM — the full-bleed container invariant is false above 768 px.** The admin shell
   nests inside `(authed)/+layout.svelte:21-22`, beside an already-rendered `SectionRail` of
   `var(--rail-w)` (48 px). So `vp width = innerWidth − 48` at ≥768 px, and the must-have
   "measured width equals `window.innerWidth`" holds **only below 768 px**. The 375 px E2E
   passes because the rail is already zero-width there — not because the container is
   full-bleed — so it cannot catch a regression of the stated invariant. Both reviewers
   independently flag the untested **768–815 px band**, where the rail is visible while the
   admin container is already in its mobile state. Both also warn 06-06 reads as if it composes
   a *second* rail. (Codex HIGH, OpenCode MEDIUM — see Divergent Views.)

### Agreed Strengths

- The Phase-2 substrate is correctly treated as shipped, not rebuilt: the seven-entry registry
  (`registry.go:102-110`), descriptor derivation and zero-value rejection (`:119`, `:169`), and
  the boot wiring (`subsystem.go:151-160`) all verified present by both reviewers.
- Gate-then-distinguish is preserved — ABAC evaluation at `gate.go:103` precedes registry
  lookup at `:129`, so D-06's ordering survives, and the registered-vs-unregistered
  differential test is well targeted.
- The admission/access split is real and the planned-section hazard it fixes is genuine:
  `gate.go:157-167` does refuse `planned` sections, so a listing handler written against
  `AssertSectionAccess` would indeed have dropped all six from the nav.
- The probe-immateriality test is a genuine tripwire, not theatre — `gate_test.go:56-61` builds
  a real engine over the full seed corpus, and the shipped pin at
  `seed_profile_visibility_test.go:929-957` (including `assert.Nil(compiled.Target.ResourceExact)`)
  makes the seven-id verdict-equality property true today and RED the day per-section grants land.
- "Stub the handler empty; the denial must still pass" (06-01 Task 2) is singled out by both as
  the strongest anti-vacuity device in the set.
- The erasure-safe audit design is correct and the gap it closes is real: `payloads.go:445-466`
  carries names only, and `rg 'BuildCharacterProfileUpdatePayload' -g '*_test.go'` returns
  **zero hits** — confirming the property was previously untested.
- The `+error.svelte` census is non-vacuous by construction (enumerate, `require.Len(found, 1)`,
  `require.DirExists` on the walk root, RED demonstrated at both two files and zero).

### Divergent Views

- **Overall risk.** Codex returns **HIGH** on both passes and recommends pausing execution.
  OpenCode returns **MEDIUM-HIGH** (06-01/02), **MEDIUM** (06-04), **MEDIUM-HIGH** (06-05), and
  **MEDIUM** (web), explicitly judging that *"nothing here risks shipping a silently-dead
  security property."* Both are defensible: OpenCode weights the verification engineering it
  verified is genuinely non-vacuous; Codex weights the mechanisms that cannot produce their
  required behaviour. **Do not average these.** The disagreement is about whether a defect
  caught by the plan's own RED test at execution time counts as a plan defect — OpenCode
  repeatedly notes a gap "would be caught by the plan's own tests going RED, but a plan should
  schedule the fix, not discover it."
- **The shell/rail defect's severity** — Codex HIGH, OpenCode MEDIUM — on an identical
  mechanism, identically traced.
- **Nothing was contradicted.** No finding from one reviewer was disputed by the other; the
  non-overlapping findings below are blind spots, not disagreements.

### Found by Codex only

- **HIGH — a container query cannot flip the Sheet's `side` prop.** `side` is a Svelte prop
  (`sheet-content.svelte:18,25,36`) emitted as `data-side={side}`; CSS can restyle
  `[data-side=right]` but cannot mutate the attribute or select the bottom transition classes.
  06-08's E2E asserting `data-side="bottom"` cannot pass under its own specified mechanism.
  Retargeting the portal fixes ancestry only. *Independently verified by the orchestrator.*
- **HIGH — the edit Sheet has no data source.** 06-04's `AdminCharacter` carries no profile
  prose; 06-08 requires the Sheet to open from row data, issue no fetch, and render all 13
  current values. Those cannot all hold — fields would seed blank and **overwrite existing
  content** on save.
- **HIGH — planned-section display metadata is unavailable on the error path.** Planned
  sections map to `FailedPrecondition` with a static message and no detail; ConnectRPC does not
  return the response body alongside an error, yet 06-06 renders the display name "from the
  response."
- **HIGH — the long-cap E2E boundary is wrong.** Short fields use `MaxNameLength`, long fields
  `MaxDescriptionLength` (`characteraccess_write.go:137`). 06-08 asks both a 100-byte and a
  4000-byte field to reject 101 bytes; the latter must accept it.
- **MEDIUM — `COUNT(*) OVER ()` cannot return a total for a page beyond the end.** Once
  `OFFSET` removes every row there is no row to carry the window count, contradicting 06-04's
  own out-of-range acceptance criterion.
- **MEDIUM — the `^Admin` placement guard is naming-based.** A future privileged RPC named
  `PurgeCharacter` on another service escapes both the interceptor prefix and the meta-test.
- **MEDIUM — the lifecycle audit-context widening is unreconciled with non-admin callers**; no
  typed API is defined for how the admin handler supplies context without changing the shared
  method signature.
- **LOW — "present-and-empty" repeated `roles` is not a stable protobuf wire distinction**;
  `require.NotNil` on a repeated field asserts implementation memory, not a wire contract.

### Found by OpenCode only

- **MEDIUM — known-divergence #3 is factually wrong, and every opacity criterion in the phase
  inherits the error.** `errutil.AssertErrorCode` (`pkg/errutil/testing.go:15-20`) *is*
  `oops.AsOops(err)` + `.Code()` — the two spellings the briefing contrasts are the same call,
  so forbidding one buys nothing. `Code()` returns the **deepest** code
  (`getDeepestErrorCode`), not the top-level one, so both forms pass under a double wrap.
  **Confirmed by the orchestrator, and previously known**: `.claude/rules/grpc-errors.md` is
  already filed as drifted under **GH #4949** (and #4902), and `oops.AsOops(err)` returns two
  values — so the single-value spelling mandated in the plan criteria **does not compile**.
  The ninth owed amendment rests on a false premise and must be withdrawn or rewritten. The
  property actually wanted (outermost code) is not assertable through any exported oops API
  (`OopsError.code` is unexported); opacity must be asserted at the wire.
- **MEDIUM — `task docs:proto` is omitted from every plan.** `test/meta/grpc_api_coverage_test.go:51-76`
  requires every `service` under `api/proto` to render in
  `site/src/content/docs/reference/grpc-api.md`, and `task proto` runs only `buf generate`.
  Adding `holomush.admin.v1.AdminService` turns `TestGRPCReferenceCoversAllServices` RED, and
  06-01's own Task 3 runs `task test -- ./test/meta/`.
- **HIGH — the sort enum mandates a field §11.3 forbids sorting on.** `characters.player_id` is
  "Sort: No, Filter: Yes … never an ordering" (`01-SPEC.md:2734`), re-asserted at `:3168`
  (a `player_id` ordering "would leak creation sequence"), and the UI-SPEC agrees at `:674`.
  06-04 mandates "exactly seven enum values: `UNSPECIFIED` plus the §11.3 six" — **an
  acceptance criterion that would fail the correct implementation and pass the buggy one.**
- **HIGH — the empty-search-term truth contradicts its own normalization action.**
  `charname.Normalize` rejects blank and Cf-only input with `NAME_EMPTY_NORMAL_FORM`
  (`pipeline.go:118-130`), so "an empty search term returns the unfiltered first page" fails by
  construction. The repository-level test dodges it because the term arrives pre-normalized, so
  the wire path is never exercised.
- **MEDIUM — the join-deduplication test is vacuous.** An `OR` in a `WHERE` cannot duplicate a
  row, and the join is many-to-one onto a `TEXT PRIMARY KEY`, so `require.Len(rows, 1)` passes
  identically with and without any conceivable bug — *"shipped inside the plan that warns about
  it."*
- **MEDIUM — 06-06 contradicts itself on `/admin` resolution.** The action resolves `/admin` to
  the first permitted section; the acceptance criterion bans `redirect(30`. In SvelteKit that
  resolution *is* `redirect(3xx)`, and no `+page.svelte` exists in `files_modified`, so the
  executor must violate the criterion or invent a mechanism.
- **MEDIUM — 06-08 mints a second byte-counting implementation.** `ByteCounter.svelte:34`
  already implements `new TextEncoder().encode(value).length` with the same
  server-agreement rationale at `:10-15`. The plan mandates search-before-writing for
  `lastActive.ts` but not `byteCount.ts` — the "two sources of truth" shape 06-07 forbids
  elsewhere for `charname.Normalize`.
- **MEDIUM — the `events_audit` SQL fence is false at HEAD as literally stated.**
  `INSERT INTO events_audit` appears in a dozen non-`projection.go` files (test fixtures); the
  fence text never says "production, non-test" and never references the existing allowlist
  mechanism at `test/meta/world_sql_fence_test.go:333`.
- **MEDIUM — the taxonomy ratchet is under-enumerated.** The "six places" cover only the
  lifecycle payload; the profile-update half needs `characterProfilePayload` widening plus a
  `SchemaVersion` bump (touching the *player* path's envelopes), or a distinct kind requiring
  census, parity-table, and kind-list entries — none in `files_modified`.
- **MEDIUM — character RPC placement diverges from the §9.2 census without an amendment.**
  §9.2 names `CharacterAccessService.AdminListCharacters / AdminSearchCharacters /
  AdminGetCharacter` (`01-SPEC.md:1980-1982`); CONTEXT's discretion covered only the *section*
  RPCs, and the nine amendments never amend the character RPCs' service.
- **MEDIUM — LIKE metacharacters are unescaped.** `%`, `_`, and `\` pass through the
  normalizer untouched, so `a_b` matches `axb` and `100%` matches everything containing `100`.
  Parameter binding prevents injection; this is a correctness bug with no test.
- **LOW — `gate.go:219-228`'s doc comment will contradict the shipped architecture.** It still
  reads "Every admin RPC re-asserts its own gate through this helper … The redundancy is the
  point" — the exact sentence D-99 retires. No plan schedules the update, and an amendments
  issue cannot fix an in-tree comment.
- **LOW — `pnpm dlx shadcn-svelte@latest` is unpinned**, so the same plan run a month apart can
  generate different component code.
- **LOW — the UI-SPEC says "full table (5 columns)"** (`06-UI-SPEC.md:274-275`) while its own
  table section and 06-07 ship six.
- **LOW — 06-03 Task 3's verify command can pass vacuously**:
  `gh issue list --search "Phase 6 amendment in:title"` finds nothing unless the issues are so
  titled, which the action never mandates.
- **LOW — `+error.svelte`'s viewer source is imprecise**: `isGuest` lives in the client-side
  `authState` store, and a root `+error.svelte` renders *outside* the `(authed)` layout.

## Orchestrator verification note

Three reviewer claims were independently checked against the tree before this file was written,
because each would invalidate a decision taken during planning:

| Claim | Verdict |
| --- | --- |
| `server.go:630`/`:646` have no callers; production builds at `sub_grpc.go:427` | **Confirmed.** The only other `NewGRPCServer` symbols are `internal/control` and `internal/world` — different packages, different signatures. |
| `side` is a Svelte prop, not CSS-settable | **Confirmed** at `sheet-content.svelte:18,25,36`. |
| `AssertErrorCode` ≡ `oops.AsOops` + `.Code()`; the rule is stale | **Confirmed** at `pkg/errutil/testing.go:15-20`, corroborated by prior records and open issues **#4949 / #4902**. |

The constructor citation and the `AssertErrorCode` "strengthening" both entered the plans
through the orchestrator's own planner briefing, inherited from research and from a repo rule
that is itself known-drifted. Neither originated with the planner.

---

## Codex Review

### Pass A — 06-01, 06-02, 06-04, 06-05

## Summary

The plans show unusually strong attention to fail-closed behavior, paired positive controls, registry opacity, both gRPC construction paths, deterministic ordering, and erasure-safe audit payloads. However, Pass A is not implementation-ready. Three security-critical paths are underspecified or internally inconsistent: `AdminGetSection` introduces a request-derived section descriptor without extending the interceptor that is supposed to enforce every descriptor; the production gRPC server does not currently use either constructor the plan treats as the mounting authority; and admin writes call world methods whose separate character-resource ABAC checks and transaction boundaries are not reconciled with the new admin-section authorization model. There are also concrete dependency-injection and pagination defects that would prevent the plans’ own acceptance criteria from passing.

## Strengths

- The existing admin-section substrate is correctly treated as shipped rather than rebuilt. The seven registry entries already exist in the required order, with `characters` available and six planned sections at [internal/admin/section/registry.go:102](internal/admin/section/registry.go#L102). Descriptors are derived from section IDs at [internal/admin/section/registry.go:119](internal/admin/section/registry.go#L119), and validation rejects zero-valued or mismatched descriptors at [internal/admin/section/registry.go:169](internal/admin/section/registry.go#L169).

- The plans preserve the existing gate-then-distinguish property. The ABAC evaluation occurs before registry lookup at [internal/admin/section/gate.go:103](internal/admin/section/gate.go#L103), while planned-section refusal happens only after admission, lookup, and descriptor checks at [internal/admin/section/gate.go:158](internal/admin/section/gate.go#L158). Plan 06-02’s registered-versus-unregistered differential wire test is therefore well targeted.

- The admission/access split is conceptually sound for current policy semantics. The seed policy grants by resource type rather than enumerating section IDs at [internal/access/policy/seed.go:985](internal/access/policy/seed.go#L985). Requiring a seven-ID verdict-equality test before accepting `PortalProbeSectionID` is a meaningful non-vacuity guard.

- The plans correctly identify the two-constructor hazard. Neither constructor currently installs a unary interceptor: TLS is at [internal/grpc/server.go:630](internal/grpc/server.go#L630), insecure testing at [internal/grpc/server.go:646](internal/grpc/server.go#L646). A paired constructor test is appropriate once the actual production composition path is addressed.

- Plan 06-04’s ordering criteria directly target the silent bug. `last_active_at` is a non-null `BIGINT` with a zero sentinel, and the existing full projection includes it at [internal/world/postgres/character_repo.go:48](internal/world/postgres/character_repo.go#L48). Testing ASC and DESC in one table, plus the normalized-name tiebreak, would go RED under both omitted-clause defects.

- The search migration is consistent with the existing deployment baseline. `pg_trgm` is already a baseline dependency, so adding only the two indexes avoids manufacturing a new extension prerequisite. The current repository has no joined username/search implementation; its existing list surface begins at [internal/world/postgres/character_repo.go:340](internal/world/postgres/character_repo.go#L340).

- The audit design correctly avoids copying prose into retained history. The existing profile payload intentionally carries sorted attribute names only at [internal/world/payloads.go:445](internal/world/payloads.go#L445). Plan 06-05 strengthens this with serialized-byte tests instead of struct-only assertions.

- The plan preserves the sole-writer boundary for `events_audit`. The actual insert is in the asynchronous audit projection at [internal/eventbus/audit/projection.go:376](internal/eventbus/audit/projection.go#L376). The proposed repository-wide SQL fence and rollback proof are valuable.

- The role-mutation protections are stronger than a source grep. Walking generated proto descriptors recursively and bounding traversal with a visited set directly covers the future schema-change risk that a handler-file grep cannot see.

## Concerns

- **HIGH — `AdminGetSection` falls outside the interceptor design that is supposed to replace per-handler gating.** Plan 06-01 defines interceptor behavior for only fixed-section descriptors and `EnumeratesAllSections` ([06-01-PLAN.md:398](.planning/phases/06-admin-portal-shell-character-administration/06-01-PLAN.md#L398)). Plan 06-02 then adds a third shape, `SectionFromRequest`, at [06-02-PLAN.md:156](.planning/phases/06-admin-portal-shell-character-administration/06-02-PLAN.md#L156), but does not modify `internal/grpc/admin_interceptor.go`; it moves the authorization call back into the handler. That creates exactly the per-handler exception D-99 was intended to eliminate. Depending on the 06-01 implementation, the interceptor will either treat the empty fixed `SectionID` as malformed or acquire an implicit ungated arm. The completeness test proves only that a descriptor exists, not that the interceptor understands its shape.

- **HIGH — the planned constructor tests do not prove the production server is gated.** Production currently creates its server directly with `grpc.NewServer` at [cmd/holomush/sub_grpc.go:427](cmd/holomush/sub_grpc.go#L427); it does not call either constructor in [internal/grpc/server.go:630](internal/grpc/server.go#L630) or [internal/grpc/server.go:646](internal/grpc/server.go#L646). That production construction also installs the plugin `UnknownServiceHandler`, message limits, and keepalive behavior at [cmd/holomush/sub_grpc.go:420](cmd/holomush/sub_grpc.go#L420). Testing the two currently-unused constructors can remain green while the real server is ungated. The plan says the call sites will use a shared helper, but it does not explicitly require preserving and testing the production proxy/keepalive options during that refactor.

- **HIGH — admin lifecycle writes encounter a second ABAC gate that the plans do not resolve.** `world.Service.RetireCharacter` evaluates `"retire"` on a `character:` resource at [internal/world/service.go:1320](internal/world/service.go#L1320), and `UnretireCharacter` does the analogous check beginning at [internal/world/service.go:1408](internal/world/service.go#L1408). The only admin seed shown grants `"read"`/`"write"` on `admin_section`, not `"retire"`/`"unretire"` on characters ([internal/access/policy/seed.go:985](internal/access/policy/seed.go#L985)). Therefore an operator can pass the new interceptor and still be denied by the reused world method. Plan 06-05 neither adds an admin character policy nor defines a trusted, typed authorization handoff that lets the world service know the admin-section decision was evaluated. Its positive controls may expose the problem, but there is no implementation task that fixes it.

- **HIGH — `AdminUpdateCharacter` lacks a specified atomic implementation for its 13-field mask.** `description` and `profile.*` currently belong to different world operations: `UpdateCharacterDescription` begins at [internal/world/service.go:919](internal/world/service.go#L919), while `UpdateCharacterProfileAttributes` begins at [internal/world/service.go:1054](internal/world/service.go#L1054). Each owns its own authorization, mutation, version change, and outbox intent. A single admin request may include both categories, yet Plan 06-05 does not define a combined world-service/mutator transaction. Calling both existing methods would require incompatible use of one `expected_version`, could partially commit, and would emit multiple envelopes. Directly writing from the gRPC handler would bypass the world service and its transactional outbox invariant.

- **HIGH — the read and write plans omit the dependencies and composition-root edits needed by their handlers.** Plan 06-01 constructs `AdminServer` with only an authorization engine. Plans 06-04 and 06-05 then require repository reads and world-service writes, but their `files_modified` lists omit `internal/grpc/admin_service.go` and `cmd/holomush/sub_grpc.go`. The current production repository and world service are locally assembled in the composition root around [cmd/holomush/sub_grpc.go:446](cmd/holomush/sub_grpc.go#L446). Without an explicit constructor/interface expansion and updated registration, the proposed handlers cannot reach those dependencies.

- **HIGH — roles cannot be populated using the files and dependencies named in Plan 06-02.** `CheckPlayerSession` currently has player/session/character repositories but no role collaborator in `CoreServer` ([internal/grpc/server.go:145](internal/grpc/server.go#L145)); its response is built without roles at [internal/grpc/auth_handlers.go:787](internal/grpc/auth_handlers.go#L787). The existing player-wide query is a concrete `PostgresRoleStore.PlayerRoles` method at [internal/store/role_store.go:108](internal/store/role_store.go#L108), deliberately absent from the `RoleStore` interface. Plan 06-02 modifies only auth handler files and proto files, not `server.go` or the composition root, so “populate it from the existing player-role store” has no injection path.

- **MEDIUM — the page-beyond-end acceptance criterion is incompatible with `COUNT(*) OVER ()`.** Plan 06-04 requires an offset beyond the final page to return no rows but still return the true total ([06-04-PLAN.md:263](.planning/phases/06-admin-portal-shell-character-administration/06-04-PLAN.md#L263)), while specifying that total only as a `COUNT(*) OVER ()` column on the paginated query ([06-04-PLAN.md:188](.planning/phases/06-admin-portal-shell-character-administration/06-04-PLAN.md#L188)). Once `OFFSET` removes every row, there is no row from which to scan the window count. A second count query, a CTE that always returns metadata, or a deliberate empty-page total contract is required.

- **MEDIUM — the “all admin functionality belongs in this service” placement guard remains naming-based.** The fence only catches RPC names beginning with `Admin` ([06-01-PLAN.md:535](.planning/phases/06-admin-portal-shell-character-administration/06-01-PLAN.md#L535)). A future privileged RPC named `PurgeCharacter` on another service would escape both the `/holomush.admin.v1.` interceptor prefix and that meta-test. The new package is sufficient for methods placed in it—the served-method/descriptor equality protects those—but the proposed placement guard does not structurally enforce placement for admin semantics outside it.

- **MEDIUM — the lifecycle audit-context widening is not reconciled with non-admin callers.** `BuildCharacterLifecyclePayload` currently receives only character ID and new status at [internal/world/payloads.go:434](internal/world/payloads.go#L434), with calls from the shared lifecycle methods at [internal/world/service.go:1363](internal/world/service.go#L1363) and [internal/world/service.go:1461](internal/world/service.go#L1461). Plan 06-05 says these shared methods gain an admin audit context while player calls pass zero values, but does not define how the admin handler supplies that context without changing the public method signature or overloading `Caller`. This needs a concrete typed API and census-impact analysis before execution.

- **MEDIUM — the over-wire typed-oops assertion is inconsistent with the proposed translation.** Plan 06-01 directs the interceptor to return `status.Error(codes.PermissionDenied, ...)`, but also requires the remote client error to satisfy `oops.AsOops(err).Code() == DENY_ADMIN_SECTION`. A plain gRPC status crossing the wire retains the gRPC code/message, not an in-process `oops` object. The repository rule about top-level `oops` assertions applies before or at the translation layer ([.claude/rules/grpc-errors.md:57](.claude/rules/grpc-errors.md#L57)). The plan must separate an interceptor unit assertion on the internal oops code from an actual wire assertion on `codes.PermissionDenied` plus the opaque message, unless a documented client-side status-to-oops mapper is added.

- **LOW — “present-and-empty” repeated roles is not a stable protobuf wire distinction.** Repeated scalar fields with zero elements are normally omitted from serialized protobuf bytes, and generated getters commonly present both nil and empty storage as an empty slice. Requiring `require.NotNil(resp.GetRoles())` is an implementation-memory assertion, not a durable wire contract. The meaningful contract is that consumers interpret absence and empty identically.

## Suggestions

- Extend `MethodDescriptor` with an explicit gate-resolution strategy and make the interceptor exhaustively handle all three variants. For `SectionFromRequest`, include a typed extractor or generated request interface in the descriptor; reject a descriptor shape the interceptor cannot evaluate. Add a test that removes the handler’s gate entirely and still sees the request denied upstream.

- Establish one production server factory used by both `cmd/holomush/sub_grpc.go` and the integration harness. Its test should inspect behavior through the actual production composition and verify that the plugin unknown-service proxy still works, not merely instantiate two isolated constructors.

- Decide and document the second-gate model for world writes:

  - add narrowly scoped admin policies for the existing character actions, or
  - introduce a typed, unforgeable authorization result passed from the admin boundary into purpose-built world admin methods.

  Do not bypass `checkAccess` with a raw repository call or a boolean `isAdmin`.

- Define a combined `AdminUpdateCharacter` world command that applies intrinsic description and profile-property changes under one CAS, one transaction, and one audit intent. Its test should update one field from each category, then force rollback and verify neither category nor the envelope commits.

- Expand `AdminServer` dependencies explicitly—repository reader, world command service, and any projection helpers—and include constructor plus composition-root files in 06-04/06-05.

- Add a role-reader interface or function dependency to `CoreServer`, backed in production by `PostgresRoleStore.PlayerRoles`, and update the composition root and test helpers. Keep it separate from the existing `RoleStore` interface if preserving that interface is intentional.

- Resolve pagination by either issuing a separate count query in the same read transaction, using a CTE that emits metadata even for an empty page, or changing the contract so an out-of-range page reports total `0`. The present plan cannot provide both an empty result and the true count from a window column alone.

- Replace the `^Admin` placement heuristic with an explicit privileged-service registry or a proto option declaring the authorization domain. A meta-test can then require every privileged RPC to live in the gated package and have a descriptor independent of method naming.

- Split denial verification into:

  - interceptor/unit: top-level `oops` code is `DENY_ADMIN_SECTION`;
  - real wire: `codes.PermissionDenied`, exact static message, and no internal code or identifiers.

## Risk Assessment

**HIGH.** The plans have strong verification instincts, but several acceptance criteria currently cannot be achieved by the specified implementation: the request-derived section path is not covered by the interceptor, production server construction is outside the tested constructors, roles lack a dependency path, out-of-range totals cannot come from the proposed window query, and admin mutations do not yet have a coherent authorization or transaction model. These are phase-goal and trust-boundary issues rather than polish; execution should pause until the plans are amended.


### Pass B — 06-03, 06-06, 06-07, 06-08

## Summary

The plans have unusually strong UI-state contracts and explicitly target several “green build, dead property” hazards. However, Pass B is not implementation-ready. Four source-grounded contradictions block the phase goal: the admin container cannot be full-viewport inside the existing authed shell; CSS cannot change the Sheet’s `side` prop; the edit Sheet has no source for its 13 editable values; and planned-section metadata is discarded when `AdminGetSection` returns an error. Several proposed acceptance tests would either be impossible to implement or would pass without exercising the claimed route behavior. These need plan-level repair before execution.

## Strengths

- The Sheet portal hazard is correctly identified. The generated component forwards `portalProps` into `SheetPortal` at [web/src/lib/components/ui/sheet/sheet-content.svelte:20](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/lib/components/ui/sheet/sheet-content.svelte:20) and portals at [sheet-content.svelte:31](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/lib/components/ui/sheet/sheet-content.svelte:31). The proposed browser assertion of DOM containment is materially stronger than a build check.

- The exactly-one error-boundary census is non-vacuous: it enumerates files, asserts both count and root path, and requires RED demonstrations at zero and two. That directly addresses SvelteKit’s nearest-boundary behavior described in [06-UI-SPEC.md:434](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-UI-SPEC.md:434).

- The table intentionally preserves server ordering, including `never` rows. That is the right division of responsibility; the UI contract explicitly requires deterministic server ordering at [06-UI-SPEC.md:686](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-UI-SPEC.md:686).

- The plans consistently prevent server-authored error strings from reaching the screen. This follows the existing authored-union pattern in [ProfileSection.svelte.test.ts:15](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/lib/components/characters/ProfileSection.svelte.test.ts:15), where tests assert absence of leaked server text rather than merely presence of friendly copy.

- D-110’s no-refetch proof is well conceived: counting list requests between mutation response and DOM update would fail if the implementation quietly refetched. That is substantially stronger than observing that the row eventually changed.

- The byte counter correctly targets UTF-8 bytes, matching the existing server implementation, which uses `len(value)` and rejects values beyond the field cap at [characteraccess_write.go:237](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/internal/grpc/characteraccess_write.go:237).

- The E2E setup has a usable precedent for creating an administrator: existing tests create a player, grant the role through the DB helper, then refresh the session at [web/e2e/admin.spec.ts:58](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/e2e/admin.spec.ts:58).

## Concerns

- **HIGH — The full-bleed container decision is incompatible with the existing shell hierarchy.** The authed layout already places one `SectionRail` beside a `.section-col`, with child routes rendered inside that remaining column at [web/src/routes/(authed)/+layout.svelte:21](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/routes/(authed)/+layout.svelte:21). The rail occupies `var(--rail-w)` at [SectionRail.svelte:97](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/lib/components/shell/SectionRail.svelte:97). An admin child container therefore measures approximately `viewport − rail`, not `window.innerWidth`. Around the critical boundary, a 768px viewport yields roughly a 720px admin container: the media-query rail remains visible while the admin container enters its `<768px` mobile state. The proposed 375px E2E passes because the outer rail is already zero there; it does not test the disagreement band. Plan 06-06 also proposes putting another shipped `SectionRail` inside the admin container, risking duplicate rails.

- **HIGH — A container query cannot flip the Sheet’s `side` prop.** `side` is a Svelte prop defaulting to `"right"` and is emitted directly as `data-side={side}` at [sheet-content.svelte:18](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/lib/components/ui/sheet/sheet-content.svelte:18) and [sheet-content.svelte:36](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/lib/components/ui/sheet/sheet-content.svelte:36). CSS can alter geometry for `[data-side=right]`, but it cannot mutate that attribute to `bottom` or select the bottom transition classes. Retargeting the portal fixes container-query ancestry only. Consequently, plan 06-08’s requirement that one CSS block “flips it to `side=bottom`” cannot satisfy its own E2E assertion that `data-side="bottom"`.

- **HIGH — The edit Sheet has no data source for its editable fields.** Plan 06-04 defines list/detail `AdminCharacter` with only identity, owner, lifecycle, activity, creation, and version fields; it explicitly carries no profile prose. Existing code demonstrates that a full editor requires a separate owner-detail read that enumerates all stored `profile.*` rows; `GetMyCharacter` is documented that way at [internal/grpc/characteraccess_owner.go:114](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/internal/grpc/characteraccess_owner.go:114). Plan 06-08 simultaneously says the Sheet opens from row data, issues no fetch, and renders all 13 current values. Those properties cannot all hold. Without a full admin-detail response or an open-time fetch, edits will be seeded with blanks and can overwrite existing content.

- **HIGH — Planned-section display metadata is unavailable on the error path.** `AdminGetSectionResponse` carries the section only on success, while planned sections are mapped to `FailedPrecondition` with a static message and no error detail at [06-02-PLAN.md:161](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-02-PLAN.md:161). Plan 06-06 nevertheless requires the planned page to render the display name “from the response.” ConnectRPC does not return that response body alongside the error. The page must instead resolve the section from already-authorized layout data, or the RPC must return a successful response with `status=planned`.

- **HIGH — The long-cap E2E boundary is incorrect.** Short fields use `world.MaxNameLength`, while long fields use `world.MaxDescriptionLength` at [characteraccess_write.go:137](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/internal/grpc/characteraccess_write.go:137); the tests explicitly distinguish “short cap + 1” from “long cap + 1” at [characteraccess_write_test.go:443](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/internal/grpc/characteraccess_write_test.go:443). Plan 06-08 asks both a 100-byte field and a 4000-byte field to reject 101 bytes. The latter must accept 101. As written, the required E2E cannot pass against the intended server behavior.

- **MEDIUM — The zero-section unit test does not exercise the branch it claims to prove.** The 404 decision lives in `+layout.ts`, but the named test file is `AdminNav.test.ts`, and feeding an empty array to `AdminNav` can only show that the component renders no links. It cannot prove that the route load throws `error(404)`. This acceptance criterion would remain green if the load returned `{sections: []}` and the browser showed an empty admin shell.

- **MEDIUM — The error boundary’s viewer context is underspecified.** The root layout returns no viewer data at [web/src/routes/+layout.ts:1](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/routes/+layout.ts:1), while the authed load currently returns only `defaultCharacterId` at [web/src/routes/(authed)/+layout.ts:31](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/routes/(authed)/+layout.ts:31). `visibleSections` requires an explicit `{isGuest}` argument at [web/src/lib/nav/sections.ts:63](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/lib/nav/sections.ts:63). The plan says `+error.svelte` reads “already-resolved layout data” but does not specify where `isGuest` comes from for root routing misses versus errors thrown under `(authed)`.

- **MEDIUM — The responsive test omits the actual coherence edge.** The UI contract identifies disagreement between viewport and container width as the hazard at [06-UI-SPEC.md:294](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-UI-SPEC.md:294), but E2E only tests 375px. It needs at least a viewport around 768–815px, where the 48px outer rail can push the child container below 768 while the media query remains above it.

- **MEDIUM — “The row itself becomes a real `<button>`” conflicts with table semantics.** A `<tr>` cannot become a `<button>`, and a button cannot legally contain `<td>` elements. The plan needs an explicit accessible pattern—such as a row-level link/button overlay or a button in the primary cell—rather than leaving implementation to invent invalid markup.

- **LOW — The infrastructure classifier’s “exactly two classes” needs a residue policy.** Existing helpers classify exact Connect codes at [web/src/lib/connect/errors.ts:11](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/lib/connect/errors.ts:11). The proposed denial/infrastructure split does not say what happens for `Unauthenticated`, `Internal`, `Unknown`, or unexpected `FailedPrecondition`. A total classifier or explicit fail-safe default is needed.

## Suggestions

1. Redesign the shell composition before implementation:

   - Either move the `vp` container to the existing `(authed)` shell so it includes the one real rail, or migrate the rail’s phone breakpoint to a container available on every authed route.
   - Do not render a second `SectionRail`.
   - Add browser checks around 767px, 768px, and approximately 815px, not only 375px.

2. Implement responsive Sheet state in Svelte, not CSS alone. Derive `side` from the actual `vp` container width using a `ResizeObserver` or a reusable container-width store, while keeping CSS responsible for height and input font size. The E2E should assert both `data-side` and computed geometry.

3. Repair the read model:

   - Make `AdminGetCharacter` return the 13 editable values plus description, or add a dedicated admin edit-detail message.
   - Then either fetch detail on open and design an honest loading state, or preload detail before opening. Do not initialize prose fields from list rows.

4. Make planned-section rendering consume the already-authorized `AdminListSections` entry from layout data. Alternatively, return planned status as a successful `AdminGetSectionResponse`; do not expect metadata from a plain `FailedPrecondition`.

5. Correct byte-cap tests:

   - Short field: 99, 100, 101.
   - Long field: 3999, 4000, 4001.
   - Use separate CJK strings crossing each respective cap.

6. Extract route decisions into pure functions or directly test `+layout.ts` load behavior with mocked clients. A component test of `AdminNav` is not evidence that an empty server answer becomes a 404.

7. Specify the root error page’s viewer source explicitly and test a public unknown route, an authed unknown route, and an admin denial under the same viewer. Assert the full relevant render, including destinations and surrounding shell, not merely heading/body text.

8. Define valid mobile table interaction markup before execution and test keyboard activation plus focus return to the invoking control.

## Risk Assessment

**HIGH.** The plans understand the key hazards, but several proposed mechanisms cannot produce the required behavior: container nesting defeats the full-bleed assumption, CSS cannot change the Sheet prop, the editor lacks its source data, and error responses discard planned-section metadata. These are phase-goal failures rather than polish issues, and multiple current acceptance criteria would not catch them at the point where they matter.


---

## OpenCode Review

_Model: `openrouter/moonshotai/kimi-k3`. The two timed-out passes are excluded; see the run table above._

### Pass A1 — 06-01, 06-02

I'll verify the plans against the source. Let me check the key claims.
Significant — production may not use `NewGRPCServer` at all. Checking:
Two more checks — whether `task proto` regenerates the docs reference (a meta-test requires every service in `grpc-api.md`), and the 06-02 interceptor gap:
Potential conflict: 06-01's placement guard asserts no `^WebAdmin` method in `characterWebProxyUniverse`, but 06-01 itself adds `WebAdminListSections` to `*web.Handler`. Checking what that universe collects:
One last check — the UI-SPEC citation the plans lean on:
# Pass A1 Review — Plans 06-01 & 06-02 (fail-closed admin gate tracer + section registry RPCs)

## Summary

Both plans are unusually well grounded in the actual tree: every `path:line` I checked (the four-step gate at `internal/admin/section/gate.go:103-167`, the seven-entry registry at `registry.go:102-110`, the type-scoped seed DSL pinned at `seed_profile_visibility_test.go:929-957`, the hostcap transposition source at `internal/plugin/hostcap/interceptor.go:191+`, the boot wiring at `internal/bootstrap/setup/subsystem.go:151-160`) is accurate, and the census-placement guard is coherent with the real universe scoping (`characterWebProxyUniverse` filters by the characterAccess client selector, so `WebAdmin*` proxies on a new client field correctly stay out of it). However, there are two HIGH-severity defects: 06-01's "both constructors" framing misidentifies where the production server is actually built, making its keystone test near-vacuous as specified; and 06-02 introduces a third descriptor shape (`SectionFromRequest`) with no interceptor semantics, which composes with 06-01's interceptor spec into a dead-on-arrival RPC. There is also a factual error in the mechanism claimed for known-divergence #3 that propagates into both plans' acceptance criteria.

## Strengths

- **The admission/access split is real and correctly placed.** `assertSectionAccess`'s step 1 (`gate.go:103-126`) is a contiguous, extractable body, and the planned-section hazard is genuine: step 4 (`gate.go:157-167`) refuses `planned` sections, so a listing handler written against `AssertSectionAccess` would indeed silently drop all six planned sections from the nav. 06-01's Test 8 and the comment-filtered grep acceptance (`AssertSectionAccess` must not appear in non-comment handler code) directly guard this — a criterion that goes RED under the actual bug.
- **The probe immateriality test is a genuine tripwire, not theater.** `engineFor` builds a real engine over the full seed corpus (`gate_test.go:56-61`), and the shipped pin (`seed_profile_visibility_test.go:929-957`, including `assert.Nil(t, compiled.Target.ResourceExact)` and `NotContains(DSLText, "admin_section:")`) means the "verdict identical across all seven ids" property holds today and goes RED the day a per-section grant lands. The plan is honest that `AdminSectionResource("")` panics (`prefix.go:294-297`) and that the probe-plus-property is the expressible form of a type-scoped evaluation.
- **The "stub the handler empty, denial must still pass" acceptance criterion** (06-01 Task 2) is the strongest anti-vacuity device in either plan — it proves the denial originates in the interceptor, exactly the criterion-1 property.
- **The differential registered/unregistered assertion is backed by the implementation.** `denyAdminSection` (`gate.go:179-181`) builds a fresh error with a static string and the section id only in `.With(...)` context on the *unregistered* arm, which the handler never surfaces — so byte-identical `status.Convert(err).Message()` for registered vs unregistered ids is achievable, and D-06's ordering (`gate.go:103` Evaluate before `:129` lookup) is correctly preserved.
- **Dependency ordering at the production site works.** `policyEngine` (`cmd/holomush/sub_grpc.go:354`) and `authPlayerSessionRepo` (`:359`) are both in scope before server construction (`:427`), and `resolvePlayerSessionWithRepo` (`internal/grpc/auth_handlers.go:174-186`) takes exactly the `auth.PlayerSessionRepository` the interceptor deps declare.
- **Census guard is correctly scoped.** Verified that `TestCharacterAccessRoutingCensusExcludesAdminRPCs` cannot false-positive on the plan's own additions: `characterFacadeUniverse` accepts only `CharacterAccessServer` receivers and `characterWebProxyUniverse` (`characteraccess_routing_census_test.go:476-490`) filters to methods referencing the characterAccess client selector, so `AdminServer` and a new admin client field are outside both.

## Concerns

- **HIGH — 06-01's "both constructors" model misidentifies the construction sites; the keystone test is vacuous as specified.** The must_have truth says: *"Both `NewGRPCServer` and `NewGRPCServerInsecure` carry the admin interceptor; a test constructs each and asserts the gated prefix is refused on both, so integration tests cannot run ungated against a gated production."* Against source: (a) `holoGRPC.NewGRPCServer` (`internal/grpc/server.go:630`) has **zero callers anywhere in the repo** — production builds its server inline at `cmd/holomush/sub_grpc.go:427` with a raw `grpc.NewServer(...)` that has already drifted from the `:630` constructor (no `otelgrpc` StatsHandler, adds keepalive and the `grpcProxy.Handler()` UnknownServiceHandler); (b) the integrationtest harness builds **no gRPC server at all** — `harness.go:163-164` documents `coreServer` as "the in-process CoreServer (no network transport)". So the specified test proves a property about two constructors that neither production nor the harness uses. The plan's action text *does* direct wiring `sub_grpc.go` and the `rg ChainUnaryInterceptor|NewAdminSectionInterceptor cmd/holomush/sub_grpc.go` acceptance would catch an unwired production site — but the load-bearing truth and its test target the wrong pair. The correct site pair is {`sub_grpc.go:427`, the bufconn/listener server the new gate test builds}. This is exactly the phase's named hazard — "build stays green while the property is dead" — and the plan walks into it while claiming to close it.
- **HIGH — 06-02's `SectionFromRequest` descriptor shape has no interceptor semantics; the two plans compose into a broken RPC.** 06-01's interceptor spec has exactly two gating arms: fixed `SectionID` → `AssertSectionAccess`, and `EnumeratesAllSections` → `AssertSectionAdmission`. 06-02 adds `AdminGetSection` with `SectionFromRequest: true` and an empty `SectionID`, but `internal/grpc/admin_interceptor.go` is **not** in 06-02's `files_modified` and no interceptor behavior for the third shape is specified anywhere. Under 06-01's spec as written, the interceptor takes the fixed-SectionID arm and calls `AssertSectionAccess(ctx, engine, playerID, "", "read")`, which returns `ADMIN_SECTION_REQUEST_MALFORMED` (`gate.go:90-92` refuses empty section ids before evaluation) — every `AdminGetSection` call fails before the handler runs. Meanwhile 06-02's handler *also* calls `AssertSectionAccess` itself "first and unconditionally" — a per-handler gate call, which is precisely what D-99 abolished, reintroduced for the one RPC whose section id is attacker-controlled. Either the interceptor must gain a `req.(interface{ GetSectionId() string })` extraction arm (mirroring the `GetPlayerSessionToken()` assertion 06-01 already specifies) and the handler trust the ctx, or the plan must explicitly admit and justify a per-handler gate here. As written, the executor hits an unspecified fork on the phase's most security-sensitive ordering.
- **MEDIUM — `task docs:proto` is omitted from both plans; CI's meta-test will fail.** `test/meta/grpc_api_coverage_test.go:51-76` asserts every `service` declared under `api/proto` renders in `site/src/content/docs/reference/grpc-api.md`, and `task proto` (Taskfile.yaml:576-596) runs only `buf generate` — it does not regenerate that doc. Adding `holomush.admin.v1.AdminService` without `task docs:proto` turns `TestGRPCReferenceCoversAllServices` RED, and 06-01's own Task 3 verify runs `task test -- ./test/meta/`. Neither plan mentions `docs:proto`, and the stale-diff acceptance criteria check only `pkg/proto web/src/lib/connect`.
- **MEDIUM — Known-divergence #3's stated mechanism is factually wrong, and the acceptance criteria inherit the error.** The divergence says `AssertErrorCode` "chain-walks and passes silently under a double wrap" and `oops.AsOops(err).Code()` is the strengthening. The code says otherwise: `pkg/errutil/testing.go:15-20` implements `AssertErrorCode` as *literally* `oops.AsOops(err)` + `assert.Equal(code, oopsErr.Code())` — identical semantics, no chain walk; the `.claude/rules/grpc-errors.md:67-80` claim is stale. And the amended PORTAL-10.5 text (#4902) states both forms resolve the **deepest** code under the pinned oops v1.22.0 — meaning `oops.AsOops(err).Code()` is a *deepest-code* assertion, not the "top-level" assertion both plans' criteria repeatedly claim ("reads the **top-level** code"). Under a double wrap (`oops(INTERNAL).Wrap(oops(DENY_ADMIN_SECTION))`), the plan's mandated assertion passes on the inner code exactly as much as the forbidden helper does. Forbidding `AssertErrorCode` buys nothing; the real guard is that the interceptor constructs the oops error exactly once and never wraps it — which the plans could assert directly (e.g., a chain-length/unwrap-count check) if top-level-ness is the property wanted. Flagging per the prompt's invitation: this divergence is wrong on the merits, not merely divergent.
- **MEDIUM — 06-02 Task 2 understates the roles wiring.** `CheckPlayerSessionResponse` (`api/proto/holomush/core/v1/core.proto:969`) carries no roles; `CoreServer` has **no** role-store dependency today (`rg roleStore internal/grpc` — zero hits); and `PlayerRoles` (`internal/store/role_store.go:108`) is deliberately **not** on the `RoleStore` interface (`role_store.go:102` + its interface-pinning test). "Populate it from the existing player-role store" therefore requires a new `CoreServerOption` (e.g. `WithPlayerRoleLookup(roleStore.PlayerRoles)`), a new field on `CoreServer` (so `internal/grpc/server.go` belongs in `files_modified`), and wiring at **both** `sub_grpc.go` and `harness.go` — the harness wiring being load-bearing for the Task-2 boundary integration test. None of this is named; the plan reads as if the store is already reachable from the handler.
- **LOW — 06-01 lists `internal/testsupport/integrationtest/harness.go` as an interceptor "call site," but no server is constructed there.** The `rg` acceptance on harness.go forces the executor to invent a mount point to satisfy it. 06-02 Task 3 handles the same need more honestly ("add the smallest helper to the existing harness file … and say in its doc comment what it provides").
- **LOW — `gate.go:219-228`'s doc comment will contradict the shipped architecture.** `AssertSectionAccess`'s comment still says "Every admin RPC re-asserts its own gate through this helper … The redundancy is the point" — the exact sentence D-99 retires. Neither plan schedules the doc update, so the PR would ship code whose own package docs describe the pre-amendment model (and the amendments issue can't fix in-tree comments).

## Suggestions

1. Replace the "both constructors" truth and test with: one acceptance that `sub_grpc.go:427`'s construction carries the interceptor (the existing rg criterion, kept), plus one bufconn-based test server built through a shared helper that the gate integration test uses. Better: delete the dead `NewGRPCServer` (`:630`) or route production through it — the existing drift between `:630` and `:427` (missing StatsHandler, extra proxy handler) is proof the two-constructor frame is already fiction.
2. In 06-02, add `internal/grpc/admin_interceptor.go` to `files_modified` and specify the third arm explicitly: descriptor with `SectionFromRequest` → assert `req.(interface{ GetSectionId() string })` (fail-closed with `ADMIN_SECTION_NO_SUBJECT`-sibling code on a miss), then `AssertSectionAccess` with the extracted id — keeping D-99's "no per-handler gate" intact for the attacker-controlled-id RPC.
3. Add `task docs:proto` to the proto-regen step of both plans and add `site/src/content/docs/reference/grpc-api.md` to the stale-diff acceptance.
4. Correct the opacity-assertion rationale in both plans: either assert the single-wrap property directly (the error's unwrap chain has depth 1 and its code is `DENY_ADMIN_SECTION`), or drop "top-level" language and cite #4902's deepest-code semantics accurately. Consider fixing `grpc-errors.md:67-80` in the same phase since the plans cite it as authority.
5. Name the `WithPlayerRoleLookup` option, the `server.go` field, and the harness wiring in 06-02 Task 2; add a harness-side assertion that the roles path is populated (the boundary test depends on it).
6. Add "update `AssertSectionAccess`'s doc comment to the interceptor model" to 06-01's action so the in-tree docs don't contradict D-99.

## Risk Assessment

**MEDIUM-HIGH.** The substrate verification is excellent and most criteria genuinely discriminate, but two defects sit exactly in the class this review was asked to weight: the constructor-pair test passes while saying nothing about production or harness gating (green build, dead property), and the `SectionFromRequest` gap means 06-01+06-02 as literally specified either ship a perma-`ADMIN_SECTION_REQUEST_MALFORMED` `AdminGetSection` or force the executor to improvise the interceptor's most sensitive arm. Both are cheap to fix at plan level (a corrected site model; one specified interceptor arm) and expensive to discover mid-execution. The divergence-#3 mechanism error doesn't break anything functionally but corrupts the rationale every opacity criterion in the phase rests on.


### Pass A2a — 06-04

Two quick cross-checks before writing up:
# Cross-AI Plan Review — Pass A2a (Plan 06-04)

## Summary

Plan 06-04 is unusually well-grounded: every verifiable claim I checked against the tree held (migration numbering, `pg_trgm` precedent, the `ListAll` partial-projection trap, the `charname.Normalize` pipeline shape, the `lint:no-timestamptz` task, the `TestEveryDollarQuoted...` meta-test). The demonstrated-RED discipline for the two ORDER BY clauses is exactly the right antidote to the phase's "green build, dead property" hazard. However, the plan contains **one direct violation of §11.3** (player_id as a sort key), **one internal self-contradiction** (empty search term vs. `charname.Normalize`'s blank rejection) that would make its own must-have truth fail, and — ironically for a phase obsessed with vacuity — **one vacuous test** (the join-deduplication test cannot fail). Overall risk: MEDIUM, concentrated in two fixable specification bugs.

## Strengths

- **Source claims verified accurate.** `pg_trgm` precedent at `internal/store/migrations/000001_baseline.sql:17,110,136,159`; `players.username TEXT UNIQUE NOT NULL` at `:56-58`; `000055` is indeed a Go migration (`000055_backfill_character_normalized_names.go:52`) and `000056` the highest `.sql`; `last_active_at BIGINT NOT NULL DEFAULT 0` at `000054:33` (so no NULL, no `NULLS LAST` — plan's reasoning correct); `task lint:no-timestamptz` exists (`Taskfile.yaml:907`); the dollar-quote meta-test exists (`internal/store/migrations_format_test.go:327`).
- **The full-projection requirement is real and correctly diagnosed.** `ListAll` at `internal/world/postgres/character_repo.go:700-708` genuinely reads only `id, name` and its doc block explicitly warns `Status` is zero *by omission* (INV-WORLD-5). The plan's prohibition on copying it is load-bearing, and must-have Test 6 (non-zero `Status` in returned rows) would actually catch a regression to the partial projection.
- **`normalized_name` stores the case-folded Key** (`internal/world/postgres/identity_backfill.go:131`, `admission_fixture_test.go:87-88`), so the plan's "normalize the term through the same function that produces the stored value" is the correct match semantics, and `charname.Normalize` (`internal/charname/pipeline.go:100-135`) is a pure normalizer — NFKC + Cf-strip + whitespace + full case fold — with no admission checks inside it, so it is safe to apply to arbitrary search input (with one exception; see concerns).
- **The three-clause ORDER BY is mechanistically correct.** `(c.last_active_at = 0)` defaults to ASC (`false < true`), which pins the sentinel block last regardless of the second clause's direction; `DESC` gets it free, `ASC` needs it — matching D-107's both-directions warning. The plan's acceptance criterion requiring a *demonstrated* RED (delete clause 1 → ASC case fails while DESC passes) is the right shape.
- **The SQL-injection and sort-key-injection mitigations are structural, not hygienic**: bound `$n` parameters plus a closed `switch` over a proto enum, with a repository-level typed refusal (`CHARACTER_ADMIN_SORT_FIELD_UNSUPPORTED`) as defense in depth. T-06-21/T-06-23 dispositions are honest.
- **Dependency ordering is sound**: the plan's `read_first` items (`admin_interceptor.go`, `admin_sections.go`/`mapAdminSectionError`, `descriptor.go`) are all artifacts of 06-01/06-02 (verified in those plans' `files_modified`), and `wave: 3` / `depends_on: [06-02]` covers them.

## Concerns

- **[HIGH] The sort enum includes a field §11.3 explicitly forbids sorting on.** The plan specifies `AdminCharacterSortField` as "a proto enum over exactly the §11.3 six" and the acceptance criterion demands "exactly seven enum values: `UNSPECIFIED` plus the §11.3 six". But §11.3's six fields are a *sort-or-filter* union: `characters.player_id` is **Sort: No, Filter: Yes — "Equality filter only … never an ordering"** (`01-SPEC.md:2734`), and §14 row 12 re-asserts that a `player_id` ordering "would leak creation sequence" (`:3168`). UI-SPEC agrees: "Sort on five of them; `Ver` never sortable" (`06-UI-SPEC.md:674`) — and the plan's own `read_first` even says "the five sortable keys". As written, the plan mandates an enum containing a forbidden sort key, and its acceptance criterion would *fail the correct implementation* and pass the buggy one. The enum must have six values total (`UNSPECIFIED` + five sortable), and there should be a test that `player_id` as a sort key is rejected — currently nothing in the plan tests that (Test 6 covers only `Ver`/`version`).
- **[HIGH] The empty-search-term truth contradicts the normalization action.** Must-have: "An empty search term returns the unfiltered first page rather than an error." Action: the handler "normalizes the raw query through the same `charname` function … before calling the repository." But `charname.Normalize` **rejects blank and Cf-only input with `NAME_EMPTY_NORMAL_FORM`** (`internal/charname/pipeline.go:118-130`). So under the plan's own action, an empty term, a whitespace-only term, or a ZWJ-only term returns an error, not a page — the must-have fails by construction. The repo-level Test 3 dodges this because the repository receives the term pre-normalized, so the wire path is never tested. The plan needs an explicit handler rule (e.g., blank-after-trim bypasses normalization and the predicate), a decision on what a Cf-only term returns, and a wire-level test for it.
- **[MEDIUM] The join-deduplication test (Task 1, Test 2) is vacuous.** The predicate is `c.normalized_name ILIKE … OR p.username ILIKE …` over `characters c JOIN players p ON p.id = c.player_id`. An `OR` inside a `WHERE` clause can never duplicate a row — deduplication is a property of the relational algebra, not of the implementation — and the join is many-to-one onto `players.id`, a `TEXT PRIMARY KEY` (`000001_baseline.sql:56`), so it cannot fan out either. `require.Len(rows, 1)` passes identically with and without any conceivable bug in the search predicate. This is precisely the "passes while the property is false" shape PORTAL-10 §12 warns about, shipped inside the plan that warns about it. Either drop the test or reframe it to assert something that can fail (e.g., that both match arms independently contribute — a term matching only the username arm still returns the row).
- **[MEDIUM] Service placement diverges from the §9.2 census without a filed amendment.** §9.2's census rows name `CharacterAccessService.AdminListCharacters / AdminSearchCharacters / AdminGetCharacter` (`01-SPEC.md:1980-1982`), and the `Web`-prefixed proxy pairing at `:1987-1988` is described as "a census pair". The plan places all three on the new `holomush.admin.v1.AdminService`. CONTEXT's "Claude's Discretion" authorizes that choice only for `AdminListSections`/`AdminGetSection`, and the nine owed amendments (amendment 5) add the *section* RPCs to the census but never amend the character RPCs' service. Under the milestone's census-by-set-equality discipline (§2.6), the census test will go RED — arguably working as intended, but the amendment set is incomplete and the divergence should be made explicit rather than discovered.
- **[MEDIUM] LIKE metacharacters in the search term are unescaped.** The pipeline strips Cf and folds case but passes `%`, `_`, and `\` through untouched (they are not in category Cf). Binding `'%' || $n || '%'` as a parameter prevents injection (T-06-21 stands), but a typed `%` or `_` silently becomes a wildcard — searching for `a_b` matches `axb`, and `100%` matches everything containing `100`. For an operator-facing lookup this is a correctness bug, not a vulnerability; it needs `ESCAPE` handling or explicit rejection, and no test covers it.
- **[LOW] The `characters.name` sort mapping is unspecified.** The closed `switch` maps sort fields to `c.{col}`, but §11.3's name row says matching is "against the stored normalized name of §6.1.3, not the display name" (`01-SPEC.md:2731`). Whether `Name` sorts by `c.name` (collation- and case-sensitive) or `c.normalized_name` (case-folded) is left to the executor, and the two differ observably. Given the plan's care elsewhere, one sentence would close this.
- **[LOW] Task 1 Test 5 is under-specified at the repository boundary.** The repository "does not normalize" (action), so "a term differing … by an NFKC-foldable character matches" is only true if the test normalizes the term before calling. As written an executor could read it as asserting the repository itself normalizes. Minor, but the plan's whole point is that there is exactly one normalizer — the test text should say where normalization happens.

## Suggestions

1. Fix the enum to `UNSPECIFIED` + five sortable fields; change the acceptance criterion from "§11.3 six" to "the five §11.3 Sort=Yes fields"; add a wire test asserting `player_id`-as-sort is rejected `InvalidArgument`. (This also aligns with UI-SPEC row 674.)
2. Specify the blank/Cf-only search-term path in the handler: bypass `Normalize` when the trimmed raw term is empty; either map `NAME_EMPTY_NORMAL_FORM` to an empty page or to `InvalidArgument` — and test whichever is chosen at the wire level, not only the repository level.
3. Replace Test 2 with a two-arm contribution test (term matches only `normalized_name`; term matches only `username`; term matches both → still one row), which can actually fail if a predicate arm is dropped.
4. Escape `%`/`_`/`\` in the bound term (with `ESCAPE '\'`) and add a test asserting a literal `%` in the query matches literally.
5. Add a tenth amendment to the 06-03 filing list: §9.2/§3.4 census rows move the three character admin RPCs from `CharacterAccessService` to `holomush.admin.v1.AdminService`.
6. State explicitly that the `Name` sort key maps to `c.normalized_name` (or justify `c.name`).
7. Clarify in Test 5 that the test harness normalizes the term through `charname.Normalize` before the repository call, pinning "one normalizer, service-side".

## Risk Assessment

**MEDIUM.** The plan's scaffolding — migration, projection discipline, ordering proof, injection posture, descriptor wiring — is correct and verifiably grounded in the tree. The risk is concentrated in two specification bugs the executor will either implement literally (shipping a §11.3 violation in the proto enum, and an empty-query error path that contradicts the plan's own must-have) or fix ad hoc (diverging from the plan silently). Both are one-paragraph fixes. The vacuous Test 2 and the unescaped LIKE wildcards are smaller but cut against the phase's stated anti-vacuity posture. Nothing here threatens the phase goal structurally; everything identified is correctable before execution starts.


### Pass A2b — 06-05

I'll ground this review in the actual source. Let me verify the plan's key claims against the tree.
# Cross-AI Plan Review — Pass A2b (06-05, admin character writes & audit emission)

## 1. Summary

The plan is unusually strong on verification hygiene: every load-bearing acceptance criterion is phrased to go RED under the bug it guards (marshalled-bytes prose absence, both-direction set equality, the `--- PASS:` line against `-run` zero-match vacuity, `Eventually` against the forbidden direct insert). Its factual claims about the tree are almost all accurate — I verified `AppSchemaVersion = 3` (`internal/world/outbox/taxonomy.go:29`), the two lifecycle kinds at `SchemaVersion: 1` (`taxonomy.go:144-145`), the shape of `characterLifecyclePayload` (`taxonomy.go:200-204`), the lifecycle guard ordering in `RetireCharacter` (`internal/world/service.go:1308-1400`), the names-only `BuildCharacterProfileUpdatePayload` (`internal/world/payloads.go:445-466`), the sole-writer `INSERT INTO events_audit` (`internal/eventbus/audit/projection.go:376`), and — by `rg` exit code 1 — that **no test exists** for `BuildCharacterProfileUpdatePayload` today. However, there are two genuine cross-layer gaps the plan never names: the **world-layer ABAC check will default-deny the admin caller** (no seed policy change is scheduled), and the **`description`-vs-`profile.*` mask split forces two domain writes with one `expected_version`**, which the plan never addresses. Both would be caught by the plan's own TDD tests going RED — but a plan should schedule the fix, not discover it.

## 2. Strengths

- **Factually grounded read_first sections.** Every `path:line` I spot-checked matched: `characteraccess_write.go:142-179` (the 12-entry accessor-on-the-entry map, confirmed at `internal/grpc/characteraccess_write.go:143-167`), `requireGuardedVersion` at `:213`, the service's read-arms-guard comment at `service.go:1324`, and `BuildCharacterLifecyclePayload`'s current two-field shape (`payloads.go:434-443`). The explicit correction that the sibling mask carries 12 paths, not 13, and that counting `profile.` prefixes is unsatisfiable, is exactly the kind of trap documentation a plan should carry — verified: the sibling map starts at `profile.pronouns` and has no `description` entry.
- **The payload-taxonomy erasure claim is verified and the gap is real.** `payloads.go:449-453` does carry the "VALUES are deliberately absent" doc comment, and `rg 'BuildCharacterProfileUpdatePayload' -g '*_test.go'` returns zero hits. Task 1 Test 4 authoring the first marshalled-bytes test with sentinel old/new values is the right remedy, and the acceptance criterion demanding a demonstrated RED under a values-carrying regression is non-vacuous.
- **The six-step ratchet matches the real registry.** `taxonomy.go:12-29`'s doc comment does require an `AppSchemaVersion` bump, and the lifecycle rows at `:144-145` share `characterLifecyclePayload` — the plan's all-or-none widening is the correct reading of the registry's own invariants.
- **The proto-descriptor role fence is the right mechanism** for §10.6's stated future risk, and the bounded-recursion requirement (visited set keyed on `protoreflect.FullName`) shows the author actually thought about descriptor cycles rather than cargo-culting a recursive walk.
- **The retire idempotency claim is source-verified**: `service.go:1347-1352` refuses `StatusRetired` with `CHARACTER_ALREADY_RETIRED` before any write, so "second call emits nothing" is a true property of the reused method, not a hope.
- **D-104 is sound against the tree**: `internal/world/caller.go`'s contract plus `buildIntent(..., caller.subject, payload)` at `service.go:1367` confirm the player id reaches the envelope Actor with zero payload fields.

## 3. Concerns

- **HIGH — No policy grants the admin caller the world-layer check; the write path is denied by default.** Every world write method the plan reuses runs its own `checkAccess` on the `character:<id>` resource *before* writing: `RetireCharacter` → action `"retire"` (`service.go:1321-1322`), `UpdateCharacterProfileAttributes` → action `"write"` (`service.go:1090-1091`). D-104 and `caller.go:42-45` force the caller to be player-flavored (`player:<id>`), because the subject is carried verbatim into the envelope Actor. But the **only** player-principal policy in the seed set is `seed:admin-section-access`, scoped `resource is admin_section` (`internal/access/policy/seed.go:985-987`). The admin catch-all, `seed:admin-full-access`, requires `principal is character` (`seed.go:105-107`) and never fires for a player principal. So the admin mutation passes the section interceptor and is then **default-denied inside the world service**. The plan's `files_modified` does not include `internal/access/policy/seed.go`, no task adds a policy (e.g. permit player-with-admin-role to write/retire/unretire character resources), and the threat model has no entry for it. The plan's own Task 2 Tests 7-8 would go RED and force the fix — but the omission means the executor must invent an ABAC policy mid-execution, which is precisely the kind of decision that should be in the plan (and which `abac-reviewer` is supposed to review on paper, not discover in a diff).
- **HIGH — The `description` path splits the write across two domain methods, and one `expected_version` cannot fund both.** The 13-path mask is `description` + twelve `profile.*` paths. But `UpdateCharacterProfileAttributes` enforces the closed §7.2 name set and **rejects `description`** with `CHARACTER_PROFILE_ATTRIBUTE_UNKNOWN` (`service.go:1074-1082`), and the player path confirms the split: `description` goes through a separate RPC and domain method (`UpdateCharacterDescription`, `characteraccess_write.go:487`). A mask carrying `description` plus `profile.biography` therefore requires **two** domain writes. Each bumps `characters.version` and consumes the caller's single `expected_version`; the second write fails the version precheck (`service.go:1335-1341` analog). The plan's Task 2 action copies the accessor-map *read* shape but never specifies the write fan-out, its transactionality (one RPC = two envelopes in two transactions?), or which envelope's `changed_attributes` carries the `description` name. Task 1's "decide by reading" hedge on the kind question compounds this. This is a design hole, not an implementation detail.
- **MEDIUM — The taxonomy ratchet is under-enumerated for the profile-update half.** The "six places" cover only the lifecycle payload. If the admin profile update reuses `KindCharacterProfileUpdate` (`taxonomy.go:146`), then `characterProfilePayload` (`taxonomy.go:207-213`) needs `section`/`action` fields and its `SchemaVersion` bump — a seventh and eighth place, which also touches the **player** path's envelopes (they'd emit empty section/action). If a distinct kind is minted instead, the census (`world_envelope_census_test.go`), the command→kind parity table (`mutator.go:100-106`), and the kind list (`service.go:52-58`) all need entries — none of which are in `files_modified` except transitively. Either way, the all-or-none ratchet claim ("reverting step 6 alone fails") does not cover the profile-update payload, so a partial widening there would stay green.
- **MEDIUM — The `events_audit` SQL fence fails on the existing baseline as literally stated.** `INSERT INTO events_audit` appears today in at least a dozen non-`projection.go` files — almost all test fixtures (`internal/store/events_audit_test.go:76`, `internal/testsupport/holomushtest/server.go:560,782`, `internal/eventbus/crypto/dek/rekey_phase3_integration_test.go:223`, etc.). The plan's fence text ("no INSERT INTO events_audit appears anywhere outside projection.go") never says "production, non-test files" and never references the existing fence's allowlist mechanism (`TestWorldSQLFenceAllowlistPathIsWriterBoundary`, `test/meta/world_sql_fence_test.go:333`). "Extend rather than duplicate" hints at the right shape, but the executor is left to discover that the naive version of the stated property is false at HEAD.
- **LOW — "the same methods the player path calls" overstates the tree.** `rg '\.RetireCharacter\(' -g '!*_test.go'` finds **zero** production callers; player self-retire is deferred (IDENT-04). The methods are the canonical lifecycle path, so the reuse claim is substantively right — but the phrasing invites an executor to go looking for a player-path caller that does not exist.
- **LOW — Empty-mask asymmetry is deliberate but unflagged in the plan.** The player path treats an empty mask as a no-op success returning current state (`characteraccess_write.go:315-336`, with a doc comment explicitly warning "do not 'fix' the inconsistency"); the plan requires `InvalidArgument` for the admin path. That's a defensible divergence, but the plan never says it is one, so a reviewer executing `gsd-plan-checker` against §9.5 rule 4 may flag a false contradiction — or worse, an executor may "align" the two.

## 4. Suggestions

1. Add a Task 2 step (and a `files_modified` entry for `internal/access/policy/seed.go`) adding the player-principal policy that lets the admin-section caller pass the world-layer `checkAccess` — or, if the design intent is that the section interceptor *replaces* the world check for admin calls, state that explicitly and show how the world method is invoked without its guard (it has no bypass today). Include the policy text in the plan so `abac-reviewer` can gate it pre-implementation.
2. Specify the `description` fan-out: either (a) reject mixed masks (`description` alone or `profile.*` only) as `InvalidArgument` — simplest and honest; or (b) define the two-write ordering, the version arithmetic, and which envelope kind carries which name. Pick one and write the RED test for it.
3. Extend the ratchet enumeration: either widen `characterProfilePayload` + bump `KindCharacterProfileUpdate` to `SchemaVersion: 2` and say so (noting the player path's envelopes change shape too), or mint the distinct admin kind and list the census/parity/kind-table files it forces.
4. Scope the SQL fence explicitly to non-test files (or reuse the existing fence's allowlist), and cite `world_sql_fence_test.go:333` as the mechanism.
5. In Task 2's action, add one line noting the empty-mask behavior deliberately diverges from the player path's no-op success, citing `characteraccess_write.go:315-336`.

## 5. Risk Assessment

**MEDIUM-HIGH.** The verification engineering is the best I've seen in this phase's passes and the D-103/D-104/D-105 mechanism faithfully matches the shipped substrate. But the plan schedules zero work for the one thing that will actually fail first: the admin caller's player-flavored principal is denied by every world-layer `checkAccess` it must pass, and no policy change is planned (`seed.go` is absent from `files_modified`). Together with the unspecified `description`/`profile.*` write fan-out under a single `expected_version`, the plan as written cannot execute green without the executor making two unprompted design decisions — one of them an authorization-policy decision that belongs in front of `abac-reviewer`, not in an execution session. Fixing concerns 1 and 2 on paper drops this to LOW-MEDIUM.


### Pass B — 06-03, 06-06, 06-07, 06-08

I'll verify the plans' claims against the repo before writing the review. Starting with the highest-stakes claims (portal hazard, error boundary census, nav registry, existing patterns).
Good progress. Now checking the remaining referenced sources: UI-SPEC sections, e2e helpers for admin sign-in, root layout data availability, and a few line-cited claims.
I have enough grounding. A few of the plans' mechanism claims I verified directly; a few others turned out to be shakier than the plan text asserts. Here is the review.

---

# Pass B Review — Phase 6 web portal plans (06-03, 06-06, 06-07, 06-08)

## 1. Summary

These four plans are unusually strong on the property this phase lives or dies by: most acceptance criteria are written to go RED under the exact bug they guard (file-enumeration count test demonstrated RED both directions, `portalProps` removal demonstrated RED, a negative control for the indistinguishability assertion, row-order asserted index-for-index against an unsorted input). Every load-bearing source reference I checked resolved accurately: `sheet-content.svelte:20,24,31,38` (portalProps exists, `<SheetPortal {...portalProps}>` at :31, `sm:max-w-sm` 384px default at :38), the `@media (max-width: 767px)` rail rule at `SectionRail.svelte:114-118`, the `:root` layout-token block at `app.css:68-80` with its "not inside `@theme`" comment, `just-arrived` at `app.css:103-106`, `isAbortedError` at `errors.ts:34-36`, the seven-entry registry at `registry.go:99-109`, the type-scoped seed policy DSL at `seed.go:986`, `last_active_at BIGINT ... DEFAULT 0` at `000054:33`, and the 26-entry `test/meta/` inventory containing no filesystem-walk census. The two real weaknesses are (a) 06-06's "full-bleed ⇒ container width == viewport width" claim, which is **false above 768px** because the admin shell nests inside `(authed)/+layout.svelte` beside the already-rendered rail, and (b) a contradiction inside 06-06 between "`/admin` resolves to the first permitted section" and its own `redirect(30` ban. Neither is fatal; both will surface as executor confusion or an unearned coherence claim.

## 2. Strengths

- **The portal hazard is correctly diagnosed and closed.** 06-08 Task 1 targets `portalProps={{ to: <vp element> }}` against a prop that genuinely exists (`sheet-content.svelte:20,24`) and is genuinely spread into the portal at `:31`. The E2E asserts DOM containment (`element.contains`) plus `data-side="bottom"` and computed 16px font at 375px, and demands a demonstrated RED with `portalProps` removed. This is exactly the "build stays green while the property is dead" class, and the plan treats it as such.
- **The error-boundary census is non-vacuous by construction.** Enumerate-and-`require.Len(found, 1)` with `require.DirExists` on the walk root, plus a required two-direction RED demonstration (second file added → FAIL; real file renamed → FAIL) recorded in the SUMMARY. The plan also correctly identifies that no existing `test/meta/` member can see this (verified: all 26 operate on Go AST, YAML, or CI config).
- **`sections.ts` read-only discipline is mechanically enforced**, not just asserted: `git diff --exit-code web/src/lib/nav/sections.ts` is an acceptance criterion in both 06-03 and 06-06, and the file's real shape (`SECTIONS` at :41-44, `SectionVisibility{isGuest}` at :27-30, `visibleSections` at :63-67) matches what the plans cite. The palette-leak rationale (T-06-43) is consistent with `sectionNavEntries` flowing through `visibleSections` at :81-87.
- **The empty-vs-denied analysis in 06-06 Task 2 is honest and correct.** I verified both legs: the seed policy is resource-type-scoped (`seed.go:986`: `resource is admin_section`), so an empty permitted set is unreachable in v0.13; and the plan refuses to fake an integration test for it, mandating a client-side unit test instead and documenting *why* the branch is unreachable. This is the right way to handle an unreachable-but-required branch.
- **The client-never-re-sorts criterion is tested the only way that matters**: feeding a deliberately unsorted array and asserting rendered order equals input order (06-07 Task 2 acceptance), rather than asserting a particular order.
- **The confirm-dialog copy is asserted against rendered output, not source** (06-08 Task 2), so a source comment can neither satisfy nor trip the forbidden-vocabulary check — and the plan says so explicitly. The four-clause retire body maps cleanly to D-108, and the `alert-dialog` (non-backdrop-dismissible) requirement is grounded in a real gap: `web/src/lib/components/ui/` has `dialog` but no `alert-dialog` today (verified by directory listing).
- **The D-110 loop's two columns differ exactly where they should**, and the Aborted-path Vitest case (Sheet open, typed values intact, both version numbers in the alert, focus moved, *zero* toasts) is a genuine behavioral assertion, not a snapshot.
- **E2E admin sign-in is feasible**: `web/e2e/helpers/db.ts:277` already inserts `character_roles 'admin'`, so Proof 1/4's "sign in as an admin / non-admin" is grounded in existing fixtures. And 06-08 correctly refuses to extend `admin.spec.ts` (verified: it is telnet admin commands, `test.describe('Admin Commands')` at :44).

## 3. Concerns

- **MEDIUM — 06-06's "full-bleed ⇒ container width == viewport width" claim is false at ≥768px.** The admin layout nests inside `(authed)/+layout.svelte`, which already renders `<SectionRail … variant="rail" />` (`(authed)/+layout.svelte:22`) beside the content region. So the `vp` container sits *next to* a 48px rail at ≥768px viewport and `vp width = innerWidth − 48` there. Consequences: (a) the must_have truth "its measured width equals window.innerWidth" is only true below 768px (where the rail's `@media` rule zeroes it); (b) the "two rules fire at the same moment" coherence argument holds only at the <768 boundary — at viewport 768–815px the rail is visible while the admin shell (vp = 720–767px) is already in its <768 band showing the mobilebar, a 48px mismatch band nobody tests; (c) the E2E pins the equality assertion only at 375px, where it passes *because the rail happens to be zero-width via `@media`*, not because the container is full-bleed — so the assertion cannot catch a regression of the stated invariant at desktop bands. The plan's reasoning ("making the `vp` container full-bleed means container width == viewport width at every band") is simply wrong given the nesting the same plan mandates. Also note the plan's "Inside it, the three columns: the shipped SectionRail, the AdminNav column, and the content column" reads as if the admin layout composes the rail itself, which would double-render it — the read_first ("nests rather than replaces") suggests otherwise, but the executor is left to reconcile the two.
- **MEDIUM — 06-06 Task 2 contradicts itself on `/admin` resolution.** Action: "`+page.ts` — `/admin` resolves to the first permitted section, which in v0.13 is always `characters`." Acceptance: `rg -n 'redirect\(30' web/src/routes/(authed)/admin/` returns zero matches. In SvelteKit, resolving one route to another from a `load` is `redirect(3xx)` — every spelling matches the banned pattern. There is no `+page.svelte` at the admin root in `files_modified` (only `+page.ts`), so "render in place" is not available either. The executor must either violate the grep or invent an unplanned mechanism.
- **MEDIUM — 06-08 creates a second byte-counting implementation without acknowledging the first.** `web/src/lib/components/characters/ByteCounter.svelte:34` already implements `new TextEncoder().encode(value).length`, with the same server-agreement rationale documented (`:10-15`) and boundary tests at the cap (`ByteCounter.svelte.test.ts:43`, plus `ProfileSection.svelte.test.ts:253`). Plan 06-08 Task 1 mandates a "search before writing one" step for `lastActive.ts` but not for `byteCount.ts`, and its read_first never mentions `ByteCounter.svelte`. The display contracts genuinely differ (ByteCounter shows only ≥80% of cap and renders `{bytes} / {maxBytes}`; the admin Sheet wants always-visible `{n} of {cap}`), so component reuse is wrong — but the *counting function* is exactly the security-adjacent "two sources of truth" shape 06-07 forbids for `charname.Normalize`. Extracting `byteCount()` from `ByteCounter.svelte` into a shared module and having both consume it is the obvious move the plan doesn't make.
- **LOW — UI-SPEC's responsive table says "full table (5 columns)" at ≥768px** (`06-UI-SPEC.md:274-275`) while its own table section and 06-07 ship six (Name, Player, Status, Last active, Created, Ver). The plan follows the detailed section, which is right, but the stale "(5 columns)" in the UI contract is a trap for the next reader and should be noted in the SUMMARY (the plans can't edit the tool-owned file, but the discrepancy is worth an amendment-line mention).
- **LOW — 06-03 Task 3's verify command assumes a title convention the action never mandates.** `gh issue list --search "Phase 6 amendment in:title"` finds nothing unless the nine issues are titled with that phrase; the action specifies labels and body contents but not titles. The acceptance criterion falls back to an unfiltered list, so this is cosmetic, but the verify step as written can pass vacuously.
- **LOW — `pnpm dlx shadcn-svelte@latest` is unpinned** (06-03 Task 1). The plan compensates well (diff each generated file against the `nova` shape reference, rewrite Tabler imports, record the CLI-determined sonner package from the lockfile), but `@latest` means the same plan executed a month apart can produce different component code. Pinning a version would make the generation reproducible; the lockfile check only covers the *dependency*, not the generator.
- **LOW — 06-03's "+error.svelte reads already-resolved layout data" is imprecise about the data source.** The viewer's `isGuest` lives in the client-side `authState` store (`(authed)/+layout.svelte:18` derives it from `$authState`), not in root layout `load` data — and a root `+error.svelte` renders *outside* the `(authed)` layout entirely (SvelteKit renders only layouts above the boundary). The graceful-degradation-to-`Home` contract absorbs this, and reading the store client-side works, but the plan's "already-resolved layout data" framing will send the executor looking for something that may not exist in the shape described.
- **LOW — the 768–815px band is untested anywhere.** Related to the first concern but separable: the plans verify 1200px (manual), 900px (manual), and 375px (Playwright). Nothing pins the rail-visible-but-admin-mobile overlap band. If the mismatch is intended, say so; if not, the E2E needs a second viewport.

## 4. Suggestions

1. **06-06:** Rewrite the full-bleed truth to state what's actually invariant — "at <768px the rail is zero-width, so container width == viewport width exactly where the two collapse rules must coincide" — and add either a 780px-viewport E2E assertion or an explicit SUMMARY note accepting the 768–815px overlap band. Also fix the three-column sentence to say the rail is *inherited from the `(authed)` layout*, not composed by the admin layout.
2. **06-06:** Resolve the `/admin` → first-section mechanism explicitly: either allow `redirect(303)` for this one file (narrow the grep to exclude `+page.ts`), or ship a minimal `+page.svelte` that renders nothing while client-side `goto` runs. Pick one; don't leave it to the executor.
3. **06-08:** Change `byteCount.ts` from "write a new helper" to "extract the counting expression from `ByteCounter.svelte` into `web/src/lib/` and make both call sites import it," keeping the distinct display components. Update the ByteCounter doc comment's self-reference accordingly.
4. **06-03:** Pin `shadcn-svelte` to a version in the add command, and mandate an issue-title convention ("Phase 6 amendment: …") so the verify command is meaningful.
5. **06-03:** Note the UI-SPEC "(5 columns)" vs six-column discrepancy in the SUMMARY so it reaches the next amendment batch.
6. **06-07:** The `rg -c '<Select'` count criterion will miss `Select.Root` usage if the generated component is namespaced; make the criterion count `<Select` (substring) which it does, but confirm against the actual generated export shape in 06-03's SUMMARY so 06-07's criterion isn't written against an assumed API.

## 5. Risk Assessment

**MEDIUM.** The plans' verification engineering is genuinely excellent — the three "green-while-dead" hazards flagged for this pass (portal target, error-boundary count, vacuous opacity tests) all get mechanisms that would actually go RED, and I verified the underlying file/line claims those mechanisms rest on. Nothing here risks shipping a silently-dead security property. The MEDIUM rating comes from execution-level defects rather than design defects: the false full-bleed invariant (which produces an untested 48px band and a possibly-duplicated rail), the self-contradicting `/admin` resolution requirement, and the unacknowledged `ByteCounter` duplication. All three are fixable in plan revision without touching the design; left as-is, each costs an executor cycle or lands a quiet inconsistency between the plan's stated invariants and the shipped DOM.

---

# Cycle 2 — Cross-AI Review (post-revision)

Plans reviewed at commit `63909382c` (the revision that addressed cycle 1). Reviewers and
clustering as in cycle 1: codex-cli 0.147.0 and opencode 1.18.15 driving
`openrouter/moonshotai/kimi-k3`, both source-grounded inside the worktree, with an identical
preamble per pass and the large reference docs referenced by path.

| Pass | Plans | Result |
| --- | --- | --- |
| codex-A | 06-01, 06-02, 06-04, 06-05 | 17,181 B · 56 citations |
| codex-B | 06-03, 06-06, 06-07, 06-08 | 12,258 B · 9 citations |
| opencode-A1 | 06-01, 06-02 | 13,629 B · 27 citations |
| opencode-A2a | 06-04 | 13,900 B · 18 citations |
| opencode-A2b | 06-05 | 13,219 B · 25 citations |
| opencode-B1 | 06-03, 06-06 | 16,884 B · 23 citations |
| opencode-B2 | 06-07, 06-08 | 15,620 B · 21 citations |

The cycle-2 run was interrupted once by an orchestrator auth failure after codex-A/B and
opencode-A1 had completed; the remaining four opencode passes were resumed from their
on-disk prompts. The web cluster was additionally split from one 4-plan / 251 KB prompt into
two 2-plan passes (159 KB / 164 KB) — the 4-plan shape is the one that hit the 660 s lane
timeout in cycle 1.

## CYCLE_SUMMARY: current_high=9 current_actionable=33

Previous cycle: 13 HIGH + 28 actionable = 41 unresolved. This cycle: 42. **Convergence
stalled by count** — but the composition inverted completely: **all 13 cycle-1 HIGHs are
resolved**, and 9 new HIGHs appeared, of which **7 are tagged `[REGRESSION-FROM-FIX]`** by the
reviewers. The plans are not stuck on the same defects; the repairs are generating new ones at
roughly the rate they close old ones.

## Current HIGH Concerns

1. **`holomush.admin.v1.AdminService` already exists** (opencode-A1). `api/proto/holomush/admin/v1/admin.proto:19`
   declares it, it is generated into `pkg/proto/holomush/admin`, and it has live production
   consumers (`cmd/holomush/cmd_admin_totp_reset.go:42`, `cmd/holomush/cmd_crypto_rekey.go:28`).
   The phase's core decision — put admin RPCs on a *new* service of that name and gate the whole
   package prefix — collides with it, and the prefix gate would also cover TOTP reset and crypto
   rekey. **Verified by the orchestrator.** Missed by the plan-checker across two passes, by Codex
   across both cycles, and by the orchestrator; caught only by kimi-k3 in cycle 2.
2. **[REGRESSION] The `vp` container move to `.shell` re-anchors fixed-position descendants**
   (opencode-B1). `container-type: inline-size` on an ancestor makes `position: fixed` children
   anchor to the container rather than the viewport. `Composer.svelte:190` and
   `CommandPalette.svelte:125,131` are affected, and the revision's safety grep does not reach them.
   **Verified by the orchestrator.** Consequence of cycle-1 directive C8.
3. **[REGRESSION] The 768–1023px "nav merges into the rail" treatment is structurally unreachable
   after the container move, and the Admin rail entry is built by no plan** (opencode-B2). Second
   consequence of directive C8.
4. **[REGRESSION] The replacement gRPC-server criterion is impossible** (codex-A, opencode-A1).
   "Exactly one raw `grpc.NewServer` across `cmd/`, `internal/`, `test/`" counts many legitimate
   independent servers — control-plane TLS (`internal/control/grpc_server.go:81`), plugin in-process
   transport (`internal/plugin/setup/world_conn.go:20`), plugin host servers, test fixtures. It
   neither observes "all production Core/Admin servers are gated" nor lets the current tree pass.
5. **[REGRESSION] The combined-write signature breaks an existing compile fence** (codex-A).
   Adding `opts ...ProfileUpdateOption` means `*world.Service` no longer satisfies
   `characterAccessWorldMutator`, whose method is non-variadic at
   `internal/grpc/characteraccess_service.go:63` — a file absent from 06-05's `files_modified`,
   tasks, and tests.
6. **06-05 contradicts the normative empty-mask contract without an authorized decision** (codex-A).
7. **[REGRESSION] The root not-found fallback cannot identify an unpopulated auth store** (codex-B).
8. **[REGRESSION] The new containment boundary and portal target jointly rebase fixed Sheet
   geometry** (codex-B).
9. **[REGRESSION] 06-08's `TextEncoder` count criterion is unsatisfiable under the same task's own
   constraint** (opencode-B1 HIGH; opencode-B2 rates the same defect MEDIUM). Extracting the shared
   `byteCount` helper while requiring `rg -c 'TextEncoder' web/src/` to total exactly 1 fails the
   correct implementation.

## Current Actionable Non-HIGH Concerns

33 unresolved MEDIUM/LOW findings across the seven passes, none yet visible in PLAN.md. The
recurring families, with the rest carried verbatim in the per-pass sections below:

- **Criterion-shape defects that recur across plans** — `rg -c … totals exactly N` appears in at
  least three criteria (06-06 `container-name: vp`, 06-08 `<Toaster`, 06-08 `TextEncoder`) and is
  the same unsatisfiable shape in each; 06-06's `rg -n 'DENY_ADMIN_SECTION|DENY_' web/src/`
  zero-match criterion is false at HEAD and more so after 06-02; 06-08's
  `rg -n 'grabber|grab-handle|handle'` zero-match criterion bans the bare substring `handle`;
  06-05 Task 2's wildcard grep always matches; 06-05 Task 1's payload grep matches a field that
  legitimately exists.
- **New-seam regressions the revision did not schedule** — the new seed policy breaks two
  exhaustive seed censuses (opencode-A2b); `container-type: inline-size` placement (opencode-B2);
  the mobile row overlay does not span the row (codex-B); the scalar-count repair does not deliver
  its claimed snapshot consistency (codex-A); the `WithDescription` extension never reconciles with
  the empty-partition early return (opencode-A2b).
- **Overclaimed invariants** — the factory's "preserves every option production carries today" is
  false about the `otelgrpc` StatsHandler (opencode-A1); the error-boundary invariant overclaims
  what its binding proves (codex-B); the `vp` safety grep does not inspect the actual descendant
  tree (codex-B); indistinguishability holds at page level but not chrome level, and Proof 4 only
  asserts page level (opencode-B1).
- **Metadata and wiring drift** — 06-06's `artifacts` metadata contradicts its own revised tasks in
  four places (opencode-B2); the amendment acceptance criterion contradicts items 10 and 11
  (codex-B); the rail-merge and drawer grouping have no data path, since admin sections load in the
  child layout while the rail and drawer render in the parent (opencode-B1); naming drift between
  06-06's artifacts and its action.

## Loop disposition

The convergence loop was **halted after cycle 2 by maintainer decision**, not by max-cycles
escalation. Finding 1 invalidates a structural decision that six plans depend on and that is
already gated behind 06-01's `checkpoint:decision`; resolving it is a prerequisite to a
meaningful cycle 3, since a replan would otherwise rework 42 findings on top of a service
placement that is known-wrong.

---

## Codex Review — Cycle 2

### Pass A — 06-01, 06-02, 06-04, 06-05

## Summary

These plans are not implementation-ready yet. Cycle 2 genuinely resolves most cycle-1 findings, including the missing interceptor arm, role injection path, admin-world ABAC policy, combined description/profile write, corrected sort enum, and audit-writer scope. However, the repairs introduce two blocking defects: the new gRPC-factory criterion is impossible against the existing repository, and changing `UpdateCharacterProfileAttributes` to a variadic signature breaks an existing narrow interface that the plan neither modifies nor lists. The revised write plan also contradicts the normative empty-mask contract, and the pagination fix does not provide the snapshot consistency it claims.

## Cycle-1 finding status

| Finding | Status | Evidence |
|---|---|---|
| R-01 — production bypassed tested gRPC constructors | **NEW-DEFECT-FROM-FIX** | The production path is moved into the factory, but 06-01 Task 2 criterion at [06-01-PLAN.md:576](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-01-PLAN.md:576) requires exactly one `grpc.NewServer` in all `cmd/`, `internal/`, and `test/`. Numerous legitimate servers already exist, including [internal/control/grpc_server.go:81](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/internal/control/grpc_server.go:81), [internal/plugin/setup/world_conn.go:20](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/internal/plugin/setup/world_conn.go:20), and many tests. |
| R-02 — nonexistent harness mount point | **RESOLVED** | 06-01 explicitly adds `WithGatedGRPCListener` through the shared factory at [06-01-PLAN.md:508](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-01-PLAN.md:508), with a matching integration criterion at line 580. |
| R-03 — invalid wire/oops assertions | **RESOLVED** | Wire and in-process assertions are separated at [06-01-PLAN.md:571](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-01-PLAN.md:571); 06-02 repeats the separation. |
| R-04 — omitted `task docs:proto` | **RESOLVED** | All four plans now include `task docs:proto`, generated-artifact cleanliness, and `TestGRPCReferenceCoversAllServices`. |
| R-05 — stale per-handler gate comment | **RESOLVED** | 06-01 rewrites and negative-greps the obsolete comment at [06-01-PLAN.md:544](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-01-PLAN.md:544). |
| R-06 — naming-based admin placement fence | **RESOLVED** | Explicitly accepted and documented as threat T-06-02d, with amendment ownership recorded in 06-03; it is no longer presented as semantic completeness. |
| R-07 — missing future `AdminServer` dependency seam | **RESOLVED** | 06-01 creates the option seam; 06-04 and 06-05 explicitly add and wire reader/writer options at both composition roots. |
| R-08 — `SectionFromRequest` absent from interceptor | **RESOLVED** | 06-02 adds extraction, validation, gating, context stash, and a denying default in the interceptor; the handler is prohibited from gating or looking up. |
| R-09 — roles had no injection path | **RESOLVED** | 06-02 specifies `WithPlayerRoleLookup(attribute.PlayerRoleLookup)` and production/harness wiring at [06-02-PLAN.md:337](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-02-PLAN.md:337). |
| R-10 — protobuf nil-versus-empty assertion | **RESOLVED** | Replaced with `Len == 0`, plus a negative `NotNil` grep at [06-02-PLAN.md:369](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-02-PLAN.md:369). |
| R-11 — planned-section metadata unavailable on error | **RESOLVED** | 06-02 explicitly states that the web screen must use authorized `AdminListSections` layout data rather than the error response. |
| R-17 — forbidden `player_id` sort | **RESOLVED** | 06-04 now defines five sortable values plus `UNSPECIFIED`, with bidirectional enum-set equality and a separate player filter test. |
| R-18 — vacuous OR/join dedup test | **RESOLVED** | Replaced by name-only, username-only, and both-arm contribution cases at [06-04-PLAN.md:156](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-04-PLAN.md:156). |
| R-19 — unescaped LIKE metacharacters | **RESOLVED** | Escape order, `ESCAPE` clauses, and `%`/`_`/`\` tests are explicit at [06-04-PLAN.md:226](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-04-PLAN.md:226). |
| R-20 — window count fails beyond last page | **PARTIALLY RESOLVED** | A scalar count fixes the empty-page total, but `pgx.TxOptions{AccessMode: pgx.ReadOnly}` does not guarantee both statements share a snapshot as claimed at [06-04-PLAN.md:233](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-04-PLAN.md:233). |
| R-21 — detail read lacked prose | **RESOLVED** | 06-04 adds `AdminCharacterDetail` and `AdminGetCharacterDetail` with description and all profile attributes at [06-04-PLAN.md:212](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-04-PLAN.md:212). |
| R-22 — missing reader/writer composition wiring | **RESOLVED** | Both 06-04 and 06-05 add `admin_service.go`, `sub_grpc.go`, harness wiring, nil-dependency behavior, and three-site acceptance checks. |
| R-23 — normalization rejects empty search | **RESOLVED** | Blank-after-trim bypass and Cf-only empty result are separately specified and tested over the wire. |
| R-24 — ambiguous name sort column | **RESOLVED** | `NAME` explicitly maps to `c.normalized_name` at [06-04-PLAN.md:242](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-04-PLAN.md:242). |
| R-25 — repository normalization test was underspecified | **RESOLVED** | Repository test explicitly normalizes through `charname.Normalize` before calling the repository. |
| R-26 — admin reads use no second character ABAC check | **RESOLVED** | The plan now documents the section gate as the authorization for the bounded admin projection and distinguishes the write path. |
| R-27 — world writes default-denied | **RESOLVED** | A narrow player-principal, character-resource seed policy is fully specified, with exact-action and real-engine denial tests. |
| R-28 — description/profile mask required two transactions | **NEW-DEFECT-FROM-FIX** | The combined-write mechanism is sound in principle, but its proposed variadic method signature conflicts with the existing interface at [characteraccess_service.go:63](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/internal/grpc/characteraccess_service.go:63), which is omitted from `files_modified`. |
| R-29 — incomplete taxonomy ratchet | **RESOLVED** | Lifecycle and profile payload declarations, three kind versions, and `AppSchemaVersion` are now enumerated and individually mutation-tested. |
| R-30 — audit-context API unspecified | **RESOLVED** | `LifecycleOption` and `WithAuditContext` are explicitly defined, with no-option behavior pinned. |
| R-31 — nonexistent player retire caller | **RESOLVED** | Wording now correctly describes the canonical lifecycle methods without claiming a current player caller. |
| R-32 — empty-mask divergence undocumented | **PARTIALLY RESOLVED** | The divergence is documented, but documentation does not authorize contradicting normative §9.5, which says an empty mask is a no-op success. |
| R-33 — wrong long-field cap | **RESOLVED** | Both 100-byte and 4000-byte boundaries, including CJK cases, are separately required. |
| R-34 — audit SQL fence false at HEAD | **RESOLVED** | Fence is now production-only, reuses existing path helpers, and must demonstrate both production RED and test-file exclusion. |

## Strengths

- The attacker-controlled `section_id` is now gated wholly in the interceptor. The revised plan makes the shape switch exhaustive and proves both denial and planned-section refusal survive an empty handler stub.
- The roles hint now has a complete data path from `PostgresRoleStore.PlayerRoles` through `CoreServer`, core proto, gateway, and web proto. It preserves the deliberate narrow `RoleStore` interface at [role_store.go:95](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/internal/store/role_store.go:95).
- The new admin-character policy is narrowly scoped by principal kind, resource type, and four actions. The explicit real-engine denial for `delete` is a strong defense below the absent RPC/UI surface.
- The revised search tests are substantially less vacuous: both predicate arms, both sort directions, literal metacharacters, out-of-range totals, and enum-set equality each have discriminating controls.
- The combined description/profile mutation correctly recognizes that the existing mutator already performs the character CAS before property writes in one transaction at [mutator.go:323](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/internal/world/mutator.go:323). Reusing that seam is architecturally appropriate.
- The audit plan now distinguishes transactional envelope durability from asynchronous audit projection and scopes the sole-writer fence to production code.

## Concerns

- **HIGH [REGRESSION-FROM-FIX] — the replacement gRPC-server criterion is impossible and would force unrelated architectural churn.** 06-01 requires exactly one raw `grpc.NewServer` call across all `cmd/`, `internal/`, and `test/` at [06-01-PLAN.md:576](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-01-PLAN.md:576). The current tree has many legitimate independent servers: control-plane TLS at [internal/control/grpc_server.go:81](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/internal/control/grpc_server.go:81), plugin in-process transport at [internal/plugin/setup/world_conn.go:20](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/internal/plugin/setup/world_conn.go:20), plugin host servers, and numerous test fixtures. The supplied command currently counts far more than one. This criterion neither observes “all production Core/Admin servers are gated” nor permits the existing repository to pass.

- **HIGH [REGRESSION-FROM-FIX] — the combined-write signature breaks an existing compile fence.** 06-05 changes `UpdateCharacterProfileAttributes` to accept `opts ...ProfileUpdateOption` and asserts every caller will compile unchanged at [06-05-PLAN.md:472](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-05-PLAN.md:472). Although direct calls remain source-compatible, `*world.Service` will no longer satisfy `characterAccessWorldMutator`, whose method has the exact non-variadic signature at [characteraccess_service.go:63](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/internal/grpc/characteraccess_service.go:63). That file is absent from 06-05’s `files_modified`, tasks, and tests.

- **HIGH — 06-05 contradicts the normative empty-mask contract without an authorized decision.** The normative SPEC says empty masks are no-op successes at [01-SPEC.md:2197](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/01-portal-spec/01-SPEC.md:2197) and repeats that specifically for admin character editing at [01-SPEC.md:2534](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/01-portal-spec/01-SPEC.md:2534). The plan instead requires `InvalidArgument` at [06-05-PLAN.md:368](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-05-PLAN.md:368). Merely documenting the divergence in a handler comment does not supersede the phase’s normative source; unlike ADMIN-02 and ADMIN-06, this is not listed as an intentional user-approved divergence.

- **MEDIUM [REGRESSION-FROM-FIX] — the scalar-count repair does not deliver its claimed snapshot consistency.** 06-04 says a read-only transaction ensures the count and page “see one snapshot” at [06-04-PLAN.md:233](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-04-PLAN.md:233). Under PostgreSQL’s default read-committed isolation, each statement receives its own snapshot, even inside one transaction. A concurrent insert/delete between the count and page queries can therefore make `total_count` disagree with the returned rows. The plan must request repeatable-read isolation or weaken the consistency claim.

- **MEDIUM — the new builder-level prose-absence test cannot carry the old/new values it claims to exclude.** The proposed builder retains the shape `{character_id, changed_attributes, section, action}` at [06-05-PLAN.md:280](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-05-PLAN.md:280), yet Test 4 says to call it “with” old and new values at [06-05-PLAN.md:217](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/.planning/phases/06-admin-portal-shell-character-administration/06-05-PLAN.md:217). No such parameters exist. Locally declaring sentinel strings and asserting they are absent is vacuous because those values never enter the builder. The end-to-end admin-update payload test is meaningful; this builder test is not unless the input can actually carry values.

- **MEDIUM — rejecting all empty `changed_attributes` alters an existing shared player-write behavior that the plan claims remains unchanged.** The existing service deliberately allows an identical-value resubmit to rewrite rows, bump the aggregate version, and emit an empty changed set, as documented at [service.go:1130](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/internal/world/service.go:1130). Making the shared builder reject an empty set either turns that established player operation into an error or requires changing it into a no-op. 06-05 only tests no-option behavior for lifecycle methods, not the existing character-access profile path.

- **LOW — one static-message criterion does not observe what it claims.** 06-02 runs `rg -c 'status.Error\(codes\.' internal/grpc/admin_sections.go` and says this proves every construction uses a package-level static constant. A count cannot distinguish a constant from an inline literal, and the plan says the mapper may live in a separate shared file. The behavioral exact-message tests are the real protection.

## Suggestions

- In 06-01 Task 2, replace the repository-wide raw-call count with a production-core composition test. For example, assert that `cmd/holomush/sub_grpc.go` contains no `grpc.NewServer`, invokes the shared factory exactly once, and preserves the proxy option. Test the factory itself for mandatory interceptor and required options. Do not constrain independent control/plugin/test servers.

- In 06-05 Task 2, add `internal/grpc/characteraccess_service.go` to `files_modified` and explicitly revise the interface signature—or avoid changing the shared method signature by introducing a request/options value that both the interface and implementation adopt deliberately. Run the character-access tests as part of the task.

- Restore empty-mask no-op success in `AdminUpdateCharacter`, or obtain and record a maintainer amendment to §9.5 before execution. If restored, preserve authorization and version/existence checks before returning the current state.

- Use `pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}` for count/page consistency, and add a concurrency test that mutates between the two statements.

- Replace the builder sentinel test with either:
  - a service/integration test where actual old/new values enter the mutation path and serialized envelope bytes are inspected, or
  - a structural payload-schema test proving the payload exposes no value-bearing fields.

- Add regression tests for the pre-existing character-access profile path: unchanged-value resubmit, one changed among many resubmitted values, and the expected version/envelope behavior. Explicitly decide whether the existing documented empty-change behavior is preserved or amended.

## Risk Assessment

**HIGH.** Most cycle-1 defects are now well addressed, but two repairs currently prevent correct execution: the impossible repository-wide gRPC constructor criterion and the broken method-interface compatibility. The unauthorized empty-mask contract change is also a direct normative conflict. Resolve those before execution; the snapshot and vacuous-test issues should be corrected in the same revision.


### Pass B — 06-03, 06-06, 06-07, 06-08

## Summary

The revised plans are still **not implementation-ready**. Most cycle-1 findings are now materially resolved, including the Sheet `side` mechanism, detail-fetch requirement, redirect contradiction, and responsive-band coverage. However, three fixes introduce adjacent defects: the root error boundary cannot detect an “unpopulated” auth store; portalling fixed-position Sheet content into the new containment boundary changes its geometry and backdrop scope; and the proposed mobile row overlay cannot span beyond its containing `<td>`. Several acceptance criteria also remain impossible or under-observe their claimed property.

## Cycle-1 finding status

| Finding | Status | Evidence |
|---|---|---|
| R-03 — incorrect `oops.AsOops` amendment | RESOLVED | 06-03 Task 3 item 9 withdraws it and redirects the issue to `.claude/rules/grpc-errors.md`; 06-03 acceptance criteria explicitly forbid filing the withdrawn amendment. |
| R-06 — naming-only privileged-RPC fence | RESOLVED | 06-03 Task 3 item 11 files the limitation as a follow-up rather than claiming full coverage. |
| R-12 — vacuous GitHub title search | RESOLVED | 06-03 Task 3 binds the `Phase 6 amendment:` title convention and requires a non-empty result. |
| R-13 — five/six-column UI-SPEC drift | RESOLVED | 06-03 Task 3 records the discrepancy in the SUMMARY. |
| R-14 — unpinned shadcn generator | RESOLVED | 06-03 Task 1 resolves the current version, invokes that exact version, and records it. |
| R-15 — error boundary viewer source | NEW-DEFECT-FROM-FIX | 06-03 Task 2 now names `authState`, but the store has no resolution bit: [authStore.ts](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/lib/stores/authStore.ts:14) initializes `isGuest: false`, indistinguishable from a resolved registered player. |
| R-35 — nested `vp` container and duplicate rail | PARTIALLY RESOLVED | 06-06 Task 1 correctly moves `vp` to the existing shell and forbids a second rail. Portalling fixed Sheet descendants back into that containment boundary introduces a new geometry hazard; see concerns. |
| R-11 — planned-section metadata unavailable on RPC error | RESOLVED | 06-06 Task 3 reads the entry from authorized layout data and explicitly forbids an `AdminGetSection` browser call. |
| R-38 — redirect contradiction | RESOLVED | 06-06 Task 2 allows exactly one redirect in `admin/+page.ts` and excludes denial paths. |
| R-37 — zero-section test observed the wrong layer | RESOLVED | 06-06 Task 2 moves the test to `+layout.test.ts`, imports `load`, and requires a demonstrated RED when the throw is replaced by a return. |
| R-36 — incomplete failure classification | RESOLVED | 06-06 Task 2 specifies the complete residue policy and enumerates representative Connect codes plus a non-Connect throw. |
| R-39 — rail class attachment uncertainty | PARTIALLY RESOLVED | The plan limits changes and permits a wrapper, but the acceptance criterion simultaneously requires added rules in `SectionRail.svelte`; the fallback is not reconciled with that criterion. |
| R-40 — invalid mobile row-button markup | NEW-DEFECT-FROM-FIX | 06-07 Task 2 puts an absolutely positioned pseudo-element inside a `position: relative` `<td>` and claims it spans the row. Its containing block is the cell, so `inset: 0` covers only that cell. |
| R-41 — Select count assumes generated API | RESOLVED | 06-07 Task 3 defers the textual count until generation and independently asserts one `combobox`. |
| R-42 — `player_id` filter vs sort | RESOLVED | 06-07 retains equality filtering without making `player_id` sortable. |
| R-43 — client re-sorting | RESOLVED | 06-07 Task 2 bans local sort operations and includes an index-for-index row-order test. |
| R-44 — CSS cannot mutate Sheet `side` | RESOLVED | 06-08 Task 1 derives `side` through `ResizeObserver`; installed Sheet code confirms `side` is a prop emitted as `data-side` at [sheet-content.svelte](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/lib/components/ui/sheet/sheet-content.svelte:15). |
| R-21 — edit Sheet lacks profile data | RESOLVED | 06-08 Task 1 fetches the new `AdminCharacterDetail`, disables Save during loading/failure, and tests non-empty prose. |
| R-33 — wrong long-field byte cap | RESOLVED | 06-08 tests distinct 100-byte and 4000-byte boundaries in unit and E2E coverage. |
| R-45 — duplicate byte counter | RESOLVED | 06-08 extracts the existing implementation and requires exactly one `TextEncoder` occurrence. |
| R-46 — 768–815px band untested | RESOLVED | 06-08 Task 3 adds a 780px Playwright case. |
| R-47 — dismissed toast considerations | RESOLVED | No source contradiction found; the dismissals remain scoped and explicit. |

## Strengths

- The revised Sheet mechanism now matches the installed component API. `side` is genuinely a Svelte prop, while `portalProps.to` accepts an `Element` according to the installed Bits UI types at [types.d.ts](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/node_modules/bits-ui/dist/bits/utilities/portal/types.d.ts:2).

- The detail-read fix preserves the important list/detail separation. Plan 06-04 now gives `AdminGetCharacterResponse` a distinct prose-bearing detail message, while 06-08 requires a real fetch and tests against non-empty values.

- The revised `/admin` load tests observe route behavior rather than component appearance. Replacing `throw error(404)` with a returned empty array is explicitly required to turn the test RED.

- The byte-counting and responsive fixes use paired negative controls. In particular, removing `portalProps` and hard-coding `side="right"` are designed to fail different assertions.

- The client no-resort criterion is well targeted: a deliberately unsorted input must remain index-for-index unchanged.

## Concerns

- **HIGH [REGRESSION-FROM-FIX] — The root not-found fallback cannot identify an unpopulated auth store.**  
  06-03 Task 2 requires an unpopulated store to render `Home` alone, then calls `visibleSections({ isGuest })`. But the initial store value has `isGuest: false` and no `resolved`, `hydrated`, or equivalent flag at [authStore.ts](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/lib/stores/authStore.ts:14). `visibleSections` treats `false` as a registered viewer and returns both workspace entries at [sections.ts](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/lib/nav/sections.ts:63). The proposed Vitest case therefore cannot pass without inventing an unplanned state signal or special-casing other fields. Worse, the initial render can expose the registered-player destination set before session restoration, contradicting T-06-17.

- **HIGH [REGRESSION-FROM-FIX] — The new containment boundary and portal target jointly rebase fixed Sheet geometry.**  
  06-06 Task 1 recognizes that `container-type: inline-size` makes `.shell` a containing block for fixed descendants, but its safety search only examines fixed descendants present before the portal change. 06-08 then deliberately portals `Sheet.Content` and its overlay into that same `.shell`; both are `position: fixed` at [sheet-content.svelte](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/lib/components/ui/sheet/sheet-content.svelte:33) and `sheet-overlay.svelte:15`. The shell height is `calc(100vh - var(--topbar-h))` at [(authed)/+layout.svelte](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/routes/(authed)/+layout.svelte:40). Consequently:

  - the overlay covers the shell containing block, not the full viewport;
  - percentage Sheet height is relative to viewport-minus-topbar;
  - the E2E demand for approximately 84% of viewport height conflicts with the proposed `84%` rule;
  - “backdrop tap works at every band” does not test the uncovered topbar area.

  This is exactly the neighbor broken by moving `vp` and then using it as a portal target.

- **MEDIUM [REGRESSION-FROM-FIX] — The mobile row overlay does not span the row.**  
  06-07 Task 2 says the Name `<td>` is `position: relative` and its button’s `::after { position:absolute; inset:0 }` spans the row. Absolute positioning uses the `<td>` as the containing block, so the pseudo-element covers only the Name cell. The proposed Vitest merely activates the button directly and verifies clean `<tr>` semantics; it cannot prove that tapping Player, Status, or Last active opens the Sheet. The 375px E2E says “tapping a row” but does not require tapping outside the Name cell, so it can also false-green.

- **MEDIUM — The amendment acceptance criterion contradicts items 10 and 11.**  
  06-03 Task 3 requires “Each filed issue body names the deciding `D-NN`,” while items 10 and 11 explicitly state that they have no `D-NN` and should cite plan 06-01 instead. The executor cannot satisfy both instructions honestly.

- **MEDIUM — The `vp` safety grep does not inspect the actual descendant tree.**  
  06-06 Task 1 searches only `(authed)/+layout.svelte` and `lib/components/shell/*.svelte`. The shell’s `.section-slot` renders arbitrary route children at [(authed)/+layout.svelte](/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/web/src/routes/(authed)/+layout.svelte:23), including components outside those paths. The criterion therefore does not establish its claim that the authed subtree has no fixed descendant. It also necessarily runs before 06-08 introduces the fixed Sheet descendant.

- **MEDIUM — The error-boundary invariant overclaims what its binding proves.**  
  The proposed meta-test proves exactly one `+error.svelte` exists at the root. It does not prove that “four kinds of miss render identically per viewer.” That latter property depends on route error mapping, store state, and rendered output. Registering the full behavioral statement as `binding: bound` to a filesystem census fabricates broader invariant coverage than the test provides.

- **LOW — The planned-section available branch is underspecified.**  
  06-06 Task 3 says an `available` entry should “route onward,” but the component owns no concrete onward behavior. Static `characters` routing hides the ambiguity today; a future available section reaching `[section]` could render blank while all listed tests remain green.

## Suggestions

- In 06-03 Task 2, add an explicit hydration state to the auth store, such as `profileResolved`, and define its transitions for initial load, anonymous/guest resolution, registered-player resolution, and logout. Test each state. Do not infer resolution from `isGuest`.

- Split the not-found invariant into:

  - uniqueness of the SvelteKit boundary, bound by the filesystem census;
  - indistinguishable rendered output, bound by route/component/E2E tests.

- Rework the 06-06/06-08 container strategy. Suitable options include:

  - keep the Sheet portalled to `document.body` and place a separate viewport-width container ancestor around the portal host;
  - use viewport units for Sheet geometry and a non-containing container-query source;
  - introduce a dedicated portal host whose containment and topbar coverage are explicitly designed.

  Add E2E assertions for overlay bounds and backdrop behavior in the topbar region, not only Sheet height.

- Replace 06-07’s cell-contained pseudo-element with a mechanism that actually covers the row while preserving table semantics. At minimum, make the E2E tap the Status or Last active cell—not the Name cell—and require the Sheet to open.

- Amend 06-03 Task 3’s criterion to: “Each amendment cites its deciding `D-NN`, or, for items 10–11, the named plan finding that decides it.”

- Expand the containment audit to the rendered authed subtree, including route components and portalled content, or narrow the claim to the exact elements mechanically inspected.

## Risk Assessment

**HIGH.** Most cycle-1 defects are genuinely fixed, but the revised containment/portal design introduces a cross-plan geometry defect at the exact responsive seam being repaired. The auth-store fallback and mobile row target also cannot deliver their stated behavior under the current source model. These should be corrected in the plans before execution.


---

## OpenCode Review — Cycle 2

_Model: `openrouter/moonshotai/kimi-k3`._

### Pass A1 — 06-01, 06-02

I'll work through this convergence review: first read cycle 1's findings, then trace each cycle-1 fix into the actual tree.
Now I'll trace the cycle-1 fix seams into the actual tree. Checking the key files in parallel:
Two significant discoveries already. Continuing the trace on the other fix seams:
`api/proto/holomush/admin/v1/admin.proto` **already exists** with an `AdminReadStream` RPC. Checking that:
The existing `holomush.admin.v1.AdminService` is a major collision. Checking remaining seams — protovalidate enforcement, web.proto fields, harness wiring:
Two quick final verifications, then I'll write the review:
I have enough evidence. Writing the review.

# Cross-AI Plan Review — Phase 6, Convergence Cycle 2 (Plans 06-01, 06-02)

## Summary

Not implementation-ready. The cycle-1 fixes are mostly genuine and well-engineered — the `SectionFromRequest` third interceptor arm is fully specified, the roles injection path is real and traces to shipped seams, and the oops/wire assertion split is now correct. However, this pass surfaced **one blocking defect that cycle 1 missed entirely**: the plan's foundational wire-contract decision (a *new* `holomush.admin.v1.AdminService`) collides with the **already-shipped break-glass operator service of exactly that name** — `api/proto/holomush/admin/v1/admin.proto:19` declares `service AdminService` in package `holomush.admin.v1` today, with generated `AdminService_ServiceDesc` (`pkg/proto/holomush/admin/v1/admin_grpc.pb.go:415`) carrying the operator methods. And the R-01 fix's keystone criterion — "exactly 1 `grpc.NewServer(` call site across cmd/, internal/, test/" — is **unachievable**: the count is 43 today and stays ~40 after the refactor, because dozens of legitimate bufconn/test servers the plan never migrates live under those trees.

## Cycle-1 finding status

| Finding | Status | Deciding evidence |
|---|---|---|
| R-01: production server not built by either constructor | **NEW-DEFECT-FROM-FIX** | Factory collapse is correct in direction (`sub_grpc.go:427-440` confirmed inline; `server.go:630`/`:646` confirmed zero callers), but the replacement criterion (`grpc.NewServer(` count == 1 across `cmd internal test`) is false at 43 and cannot reach 1 without rewriting ~40 unrelated bufconn/plugin/control test servers. Also the verify command as printed (`paste -sd+` with no operand) is a usage error on BSD paste — I had to add `-` to run it. |
| R-02: harness has no mount point | RESOLVED | `WithGatedGRPCListener()` is now an explicit addition; `harness.go:864` confirms the in-process `NewCoreServer` with no transport today. |
| R-03: opacity-assertion rationale wrong | RESOLVED | `oops.AsOops(err).Code()` removed from all criteria; wire = code + exact message + absent substrings; internal code = `errutil.AssertErrorCode` in-process; single-wrap = unwrap-chain depth with demonstrated RED. |
| R-04: `task docs:proto` omitted | RESOLVED | Task exists (`Taskfile.yaml:1476`); `TestGRPCReferenceCoversAllServices` exists (`test/meta/grpc_api_coverage_test.go`); both added to regen steps and stale-diff criteria. |
| R-05: gate.go:219-228 doc comment | RESOLVED | Rewrite scheduled in Task 2 action with a two-part grep criterion; the stale text is present verbatim at `gate.go:219-228` today, so the fix is pointed at the right place. |
| R-06: naming-based placement fence | PARTIALLY RESOLVED (accepted as named limitation) | T-06-02d disposition `accept` with the limitation written into the fence's own doc comment. Defensible. |
| R-07: `AdminServer` dependency seam | RESOLVED | Variadic `AdminServerOption` declared now; 06-04/06-05 extend rather than rewrite. |
| R-08: `SectionFromRequest` has no interceptor semantics | RESOLVED | Third arm fully specified; the fall-through hazard is real (`gate.go:90-92` confirmed — empty `sectionID` → `ADMIN_SECTION_REQUEST_MALFORMED` before evaluation); denying default arm; comment-filtered grep; Test 9 stub control. |
| R-09: roles has no injection path | RESOLVED | `attribute.PlayerRoleLookup` exists (`viewer.go:35`) with signature matching `PostgresRoleStore.PlayerRoles` (`role_store.go:108`); the not-on-interface pin is real; four-edit path (core proto, `CoreServer` option, handler populate, both composition roots) is coherent. `CheckPlayerSessionResponse` confirmed role-less (`core.proto:969`, literal at `auth_handlers.go:787`). Field 6 is free (`web.proto:802-822` ends at 5). |
| R-10: `NotNil` on repeated field | RESOLVED | Removed, replaced with `Len == 0` plus an absence-grep. |
| R-11: planned metadata unavailable on error path | RESOLVED (source side) | The plan now names the consequence explicitly and defers rendering to 06-06's layout data; `WebAdminGetSection` documented as deliberately browser-less. |

## Strengths

- The R-08 repair is thorough and source-accurate: the `gate.go:90-92` empty-id refusal is real, the arm ordering (declaration before subject resolution, per the injected-zero-calls session repo test) is asserted, and the handler is reduced to a projection with the grep filtered for comments so the handler's own doc comment can't invalidate it.
- The roles injection reuses the **shipped** seam type rather than inventing a parallel one — `attribute.PlayerRoleLookup` (`viewer.go:35`) is the same func type `internal/access/setup/subsystem.go:135` threads; the plan explicitly forbids widening the pinned `RoleStore` interface.
- The admission/access split repair (extract step 1 verbatim, `gate_test.go` stays green untouched, diff-scoped acceptance criterion) is the right minimal surgery on a file whose shipped assertions are load-bearing.
- The "stub the handler empty, both answers must still pass" control (06-01 Test on `AdminListSections`, 06-02 Test 9 on `AdminGetSection`) remains the strongest anti-vacuity device and is now applied symmetrically.
- Test 7 (probe immateriality) and Test 8 (admission permits planned) pin the two properties the whole `EnumeratesAllSections` design rests on, against a real engine over the full seed corpus (`gate_test.go:56-61` shape verified in cycle 1).

## Concerns

- **HIGH — `holomush.admin.v1.AdminService` already exists; the plan's core decision collides with it.** `api/proto/holomush/admin/v1/admin.proto:19` declares `service AdminService` in `package holomush.admin.v1` — the break-glass operator service (Status, Authenticate, Approve, ResetTOTP, Rekey/RekeyResume/RekeyList, AdminReadStream), served over `admin.sock` via ConnectRPC (`internal/admin/socket/`, `adminv1connect.AdminServiceClient` at `cmd/holomush/cmd_admin_totp_reset.go:42`), with gRPC stubs generated (`admin_grpc.pb.go:415` registers `AdminService_ServiceDesc`). The plan's Task 1 recommends "New `holomush.admin.v1.AdminService`" and Task 2 creates `admin.proto` as a **new file** declaring that service. Two outcomes, both unaddressed: (a) a second `service AdminService` in the same package fails `buf build` as a duplicate symbol; (b) adding `AdminListSections` to the existing service forces `AdminDescriptors` entries for every operator method (the completeness test iterates `AdminService_ServiceDesc.Methods`, which would then include `Authenticate`/`Status`/…), and entangles the player-session portal gate with the TOTP/operator break-glass surface that `AssertOperatorAdmin` guards — the exact anti-analog the plan says not to copy. The interceptor prefix `/holomush.admin.v1.` would also nominally cover the operator package. Neither cycle-1 reviewer caught this. The plan must either pick a fresh package (e.g. `holomush.adminportal.v1`) or justify cohabitation.
- **HIGH [REGRESSION-FROM-FIX] — the "exactly 1 `grpc.NewServer(`" criterion fails a correct implementation.** Measured today: 43 call sites across `cmd internal test` (bufconn servers in `test/integration/plugin*/`, `internal/grpcclient/client_test.go`, `internal/control`, `internal/plugin/**`, plus non-test production call sites at `internal/control/grpc_server.go:81`, `internal/plugin/setup/world_conn.go:20`, `internal/plugin/lua/bufconn_endpoint.go:66`, `internal/plugin/goplugin/`). After the refactor the count is still ~40. The plan schedules no migration of these (correctly — they're unrelated subsystems), so the criterion as written is unachievable and its printed command (`rg -c --no-filename … | paste -sd+ | bc`) additionally errors on BSD `paste` (no operand). The property wanted — "no second *production-composition* server for the player-facing gRPC surface" — needs a scoped spelling, e.g. counting non-test files under `cmd/` + `internal/grpc/`, or asserting `sub_grpc.go` contains no `grpc.NewServer(`.
- **MEDIUM [REGRESSION-FROM-FIX] — the factory's "preserves every option production carries today" truth is false about the otelgrpc StatsHandler.** Production at `sub_grpc.go:427-440` carries creds, keepalive, three limits, and the proxy handler — **no** `grpc.StatsHandler`. The dead constructors carry it (`server.go:637,655`). The new factory unconditionally installs `otelgrpc.NewServerHandler()`, so the refactor *adds* per-RPC server spans to production — a behavior change (span volume, attribute surface) presented as preservation, with no task evaluating it. Either drop it to match production, or own the change with a rationale.
- **MEDIUM — "empty `section_id` is refused by `buf.validate` before the handler runs" names a mechanism that does not exist.** `rg protovalidate internal/grpc internal/web cmd` returns nothing on any server path; protovalidate exists only as plugin-manifest validation (`pkg/plugin/validate.go`). The repo's existing `buf.validate` annotations on characteraccess/web/core are inert at RPC runtime. The *observable* outcome still fails closed — the third arm's TrimSpace check refuses a blank id with `ADMIN_SECTION_NO_SECTION_ID` — so Test 6 should assert the interceptor's refusal, not buf.validate's. An executor who takes the criterion literally and installs a global validator interceptor changes the runtime behavior of every existing annotation repo-wide — a much bigger change than this plan budgets.
- **MEDIUM — the harness-wiring RED criterion's stated mechanism is wrong for a Go integration test.** 06-02 Task 2: "Removing the `WithPlayerRoleLookup` line from the harness makes the Test-4 boundary integration test's admin positive control FAIL (no roles ⇒ no `/admin` entrance drawn)". There is no rail and no drawing in a Go integration test; the denial path (`AdminGetSection` → ABAC) never consults `roles` at all — that's the point of the test. The demonstration only goes RED if Test 4 first asserts `WebCheckSession`'s `roles` contains `admin` as a setup precondition; the plan's Test-4 text ("constructs a caller with roles containing admin") doesn't say it reads the field from the response. As written the criterion is vacuous-adjacent: it depends on a setup assertion nobody specified.
- **LOW — wave-ordering gap on `error(404)`.** 06-01 (wave 1) makes `/admin/+layout.ts` throw `error(404)` on denial, but the single root `+error.svelte` lands in 06-03 (wave 2). Between waves, a denied deep link renders SvelteKit's default error page, not the ordinary not-found page. Harmless if the phase ships whole, but 06-01's `<done>` ("the `/admin` shell renders…") claims a property that isn't complete until 06-03.

## Suggestions

1. **06-01 Task 1 / Task 2:** rename the new package (e.g. `holomush.adminportal.v1` or `holomush.webadmin.v1`) and update `adminServicePrefix`, the placement fence's allowed-package literal, and the completeness test accordingly; add a `read_first` line citing `api/proto/holomush/admin/v1/admin.proto:19` as the existing occupant of the obvious name. The Task-1 checkpoint options should be rewritten with this fact in them.
2. **06-01 Task 2 acceptance criteria:** replace the global `grpc.NewServer(` count with `rg -n 'grpc\.NewServer\(' cmd internal/grpc -g '!*_test.go'` returning zero matches outside `internal/grpc/server.go`, and fix the remaining count command's `paste -sd+ -` operand if any aggregate count is kept.
3. **06-01 Task 2:** decide the StatsHandler question explicitly — either omit `otelgrpc.NewServerHandler()` to match current production, or add a sentence owning the addition as an intentional observability change with its cost noted.
4. **06-02 Test 6 / truth:** reword to "an empty or whitespace-only `section_id` is refused by the interceptor with `ADMIN_SECTION_NO_SECTION_ID` before the handler runs"; drop the buf.validate-enforcement claim (or add a validator-interceptor task with a repo-wide impact assessment, which I do not recommend for this phase).
5. **06-02 Task 2:** specify Test 4's setup to *read* `roles` from `WebCheckSession` and `require.Contains(roles, "admin")` before the denial assertion — that is what makes the harness-removal demonstration RED for the stated reason.
6. **06-01 `<done>`:** note the `error(404)` → ordinary-not-found property completes only when 06-03's boundary lands.

## Risk Assessment

**HIGH.** The package-name collision is a blocking wire-contract defect sitting on the plan's first one-way-door decision — it fails at `task proto` in the best case, and in the worst case an executor "resolves" it by merging portal RPCs into the break-glass operator service, corrupting two trust boundaries at once. The unachievable server-count criterion is the same class of cycle-1 defect the plans were told to eliminate (a criterion that fails the correct implementation). Both are cheaply fixable at plan level; everything else from cycle 1 is genuinely resolved.


### Pass A2a — 06-04

I'll review plan 06-04 against the actual tree. Let me start by reading the plan file and cycle-1 reviews, then verify key claims.
Now checking the deeper seams — the `Get` projection, property-row ownership, and the tasks/tests the plan cites:
I have enough evidence. Writing the review.

---

# Cross-AI Plan Review — Cycle 2 — Plan 06-04

## Summary

Plan 06-04 is close to implementation-ready. Every cycle-1 finding it owns (R-17 through R-26) is genuinely resolved in the revised text — not merely narrated — and the load-bearing claims I re-verified against the tree all hold: `pg_trgm` and three `gin_trgm_ops` precedents ship in `000001_baseline.sql:17,110,136,159`; `000055` is indeed a Go migration leaving `000057` as the next `.sql`; `charname.Normalize` really does reject blank and Cf-only input with `NAME_EMPTY_NORMAL_FORM` (`internal/charname/pipeline.go:118-130`); `ListAll`'s partial-projection trap is real and documented at `character_repo.go:700-707`; §11.3 (`01-SPEC.md:2728-2745`) really does mark exactly five fields Sort=Yes with `player_id` Sort=No/Filter=Yes. However, the R-21 fix (the new `AdminCharacterDetail` read) introduces two fresh seams the plan does not close: the detail projection's property-row read is assigned to `CharacterRepository` even though `entity_properties` belongs to `PropertyRepository` and the model being copied is a gRPC-layer helper over an ABAC-mediated service call, and the must-have truth demands a `normalized_name` column in the projection that `world.Character` cannot carry. Those are specification gaps, not blockers; risk is MEDIUM.

## Cycle-1 finding status

| Finding | Status | Evidence |
|---|---|---|
| R-17 (player_id in sort enum; "exactly seven" criterion inverted) | RESOLVED | Plan enum is six values (`UNSPECIFIED` + five Sort=Yes), verified against `01-SPEC.md:2728-2745`; Test 6/6b pair keeps sortability and filterability distinct; RED-by-planting-`PLAYER_ID` criterion is real (adding the enum value breaks set equality over `AdminCharacterSortField_name`). |
| R-18 (vacuous join-dedup test) | RESOLVED | Replaced with three individually-RED-able contribution cases; the plan itself correctly notes `require.Len(rows,1)` in case (c) is loadless and cases (a)/(b) carry the weight. |
| R-19 (unescaped LIKE metacharacters) | RESOLVED | Escape order (`\` then `%`/`_`) + `ESCAPE '\'` on both arms is the correct pattern; Test 5b's `a_b`-vs-`axb` fixture genuinely fails without the escape. Confirmed `charname.Normalize` passes `%_\\` through (only Cf is stripped, `pipeline.go:104-107`). |
| R-20 (`COUNT(*) OVER ()` vs out-of-range page) | RESOLVED | Scalar count in the same read-only transaction; Test 4b (3 rows, Limit 2, Offset 10 → `total_count == 3`) fails under the window-column design, and a grep criterion bars `COUNT(*) OVER` from the file. |
| R-21 (edit Sheet has no data source) | PARTIALLY RESOLVED — NEW SEAM | The `AdminCharacterDetail` message + `AdminGetCharacterDetail` method close the data-source hole, but the property-row read mechanism is under-specified and mis-located (see Concerns 1–2). The list message correctly stays prose-free. |
| R-22 (missing constructor/composition-root edits) | RESOLVED | `WithAdminCharacterReader` wired at `admin_service.go`, `sub_grpc.go`, and `harness.go`, all three added to `files_modified`, nil-reader → `codes.Internal` criterion is falsifiable. `harness.go` currently has no `AdminServer` (verified), consistent with the 06-01/06-02 dependency. |
| R-23 (empty-term truth unsatisfiable under Normalize) | RESOLVED | Two mandatory handler branches (TrimSpace bypass; Cf-only → empty page, nil error) are asserted at the wire in Test 8. Verified the underlying constraint at `pipeline.go:118-130`. |
| R-24 (name sort column unspecified) | RESOLVED | `NAME` → `c.normalized_name`, grounded in §11.3's name row (`01-SPEC.md:2730`). |
| R-25 (Test 5 under-specified at repo boundary) | RESOLVED | Test 5 now states the harness normalizes via `charname.Normalize` first. |
| R-04 (`task docs:proto` omitted) | RESOLVED | Present in the regen criterion; verified the task exists (`Taskfile.yaml:1476`). |
| R-26 (admin reads with no second ABAC check) | RESOLVED (as documentation) | File doc block commitment + the correct analogy: `characteraccess_directory.go` is gate-then-read, and the write path's `checkAccess` is explicitly deferred to 06-05. |

## Strengths

- **The demonstrated-RED discipline is real, not decorative.** Deleting the leading `(c.last_active_at = 0)` clause genuinely fails only the ASC case (ascending puts the `0` minimum first — verified the logic against `000054:33`'s `NOT NULL DEFAULT 0`), and deleting the trailing `normalized_name ASC` genuinely kills the never-block ordering assertion. These are the two clauses a rushed executor would drop.
- **The R-17 fix is structural, not a runtime check.** Omitting `PLAYER_ID` from the proto enum makes the forbidden sort inexpressible on the wire — matching the phase's own D-79 philosophy — and §11.3's rationale (`01-SPEC.md:2734`, `:2741`) confirms this is the SPEC's actual intent.
- **The empty/Cf-only handling is the only correct resolution.** Given `Normalize` rejects both with one shared code (`pipeline.go:118-130`), splitting on `TrimSpace` first and mapping the non-blank case to an empty page is faithful to the UI-SPEC's "no error surface on the list page" constraint. Asserting it at the wire (Test 8) rather than the repository is exactly right, since the repository never sees the raw term.
- **Migration hygiene checks out.** Plain `CREATE INDEX` inside goose's transaction, no re-`CREATE EXTENSION` (justified by `000001_baseline.sql:17` as a hard dependency), no `$$` body so no `StatementBegin` wrapper — all consistent with `.claude/rules/database-migrations.md`, and the plan correctly warns against spelling goose annotation tokens in prose.
- **Threat register mitigations are mostly traceable to criteria** (T-06-21 → closed switch + parameter binding; T-06-22 → Task 3's fence; T-06-23 → enum + typed repository refusal).

## Concerns

- **MEDIUM — `[REGRESSION-FROM-FIX]` (R-21): the property-row read is mis-located and under-specified.** The plan adds `AdminGetCharacterDetail` to `internal/world/postgres/character_repo.go`, returning "every stored `profile.*` attribute row," and says to "follow `internal/grpc/characteraccess_owner.go`'s `ownedProfileAttributes`." But `ownedProfileAttributes` (`characteraccess_owner.go:159`) is a **gRPC-server helper** that reads through `s.world.ListPropertiesByParent(ctx, caller, ...)` — an ABAC-mediated `world.Service` call with a deliberately chosen caller subject (`access.CharacterSubject`, with a doc block explaining why neither a viewer subject nor the system bypass is acceptable) and an `isGovernedProfileName` name-closed filter. Raw `entity_properties` SQL lives in `internal/world/postgres/property_repo.go`, a different repository. The plan never says: (a) whether the admin detail read goes through `world.Service.ListPropertiesByParent` (and if so, **under what caller** — the admin's player subject would be evaluated against row-keyed grid policies written for owners/viewers) or drops to raw SQL in the postgres package (and if so, why a second repository reading `entity_properties` is acceptable and where the name-closed filter lives); (b) which layer `AdminGetCharacterDetail` actually occupies — `AdminCharacterReader` is specified as "a narrow interface carrying the three repository methods," which presumes answer (a-repo) that contradicts the model it says to copy. An executor can satisfy every stated criterion with either resolution, including one that silently evaluates the admin's own subject against per-row policies and returns a partially-filtered attribute set — which would then seed the 06-08 Sheet with missing fields, re-creating the R-21 overwrite hazard one layer down.
- **MEDIUM — the "FULL character row" truth demands a column `world.Character` cannot carry.** The truth lists the projection as `(id, player_id, name, normalized_name, description, location_id, created_at, version, status, last_active_at)` and the action says `AdminCharacterRow` "embeds the full `*world.Character`." But `world.Character` (`internal/world/character.go:15-39`) has **no `NormalizedName` field**, and even `Get` — the cited full projection (`character_repo.go:47-50`) — does not select `normalized_name`. Nothing else in the plan consumes the column (the list proto message omits it; the sort emits it as SQL text). An executor following the action cannot satisfy the truth as written, and one following the truth must either widen `world.Character` (touching every scan site) or add a one-off column. The truth should drop `normalized_name` or say why it is read.
- **MEDIUM — T-06-24 claims a mitigation the plan never specifies.** The threat register mitigates substring-scan DoS with "mandatory pagination with a server-enforced maximum page size," and the UI-SPEC fixes page size at 50 — but the action never states a clamp, a default, or a `buf.validate` bound on `AdminListCharactersRequest.page_size` / `page`. Negative or zero `page`, and `page_size = 2^31-1`, reach the repository as `OFFSET`/`LIMIT` arithmetic with no specified validation behavior. The threat is listed as *mitigated*; nothing in the plan or criteria discharges it.
- **LOW — `[REGRESSION-FROM-FIX]` (R-21): the Task-1 criterion `rg -n 'description|pronouns|profile' character_repo.go` is now un-falsifiable as written.** `Get` already selects `description` (`:48`), and the R-21 fix deliberately adds profile-row reads to this same file, so the grep will always print matches and "shows no profile-prose column in any search predicate" becomes pure human judgment. Task 3's predicate-scoped fence is the real guard, so the property survives — but this criterion should be deleted or folded into the fence to avoid a false sense of coverage.
- **LOW — the R-23 RED description misstates the failure mode.** The criterion says deleting the `TrimSpace` bypass makes the empty-query case "FAIL with `NAME_EMPTY_NORMAL_FORM`." In fact, with the bypass gone, the blank query hits the same code as the Cf-only branch; depending on branch order it either returns an empty page (failing the "unfiltered first page" assertion) or `InvalidArgument`. The test goes RED either way, so the property holds — but the criterion should describe the assertion failure, not prescribe the error path, or an executor "fixing" the branch order could match the letter of the criterion while the behavior is wrong.
- **LOW — `status_filter` validation is unspecified.** The request carries `string status_filter = 4`; the repository options carry it as optional. Nothing says whether an unrecognized status string is rejected (`InvalidArgument`, matching the sort-field posture) or silently matches zero rows — and §9.3 keeps the lifecycle vocabulary off the wire, which a free-text filter parameter arguably re-introduces.
- **LOW — fence wording drift: "the 13 §10.6-allowlisted `profile.*` column names."** §10.6 (`01-SPEC.md:2548-2560`) is thirteen **paths** = `description` + twelve `profile.*`; Task 2 correctly says "twelve `profile.*` values." Task 3's phrasing invites an executor to enumerate 13 `profile.*`-prefixed columns — which do not exist — or to omit `description` from the forbidden set. The fence's forbidden-set literal should be spelled out in the plan.

## Suggestions

1. **Task 1 / Task 2 action (R-21 seam):** pick the layer explicitly. Recommended: `AdminGetCharacterDetail` composes `Get` + `world.Service.ListPropertiesByParent` under a documented caller (or a raw `property_repo` read with the `isGovernedProfileName` set named as the filter), and `AdminCharacterReader` is reshaped to match. Add an acceptance criterion asserting the detail response contains all twelve governed names for a character that has them **and omits a 13th non-governed property row** seeded on the same character — that is the test that detects both a missing row and a leaked row.
2. **Truths:** strike `normalized_name` from the "full character row" enumeration (or justify the read); align with `world.Character` (`internal/world/character.go:15-39`).
3. **Task 2 action:** add the pagination contract — `page_size` default 50, server-enforced max (e.g. `buf.validate` `lte`), `page` clamped or rejected below 1 — so T-06-24's "mitigate" is backed by a criterion.
4. **Task 1 criteria:** delete the `description|pronouns|profile` grep criterion (subsumed by Task 3's fence) or rescope it to the two search methods only.
5. **Task 2 criteria:** reword the TrimSpace RED to "the empty-query wire case fails" without prescribing the error surface.
6. **Task 3 action:** write the forbidden-column set as an explicit literal in the plan: `description` plus the twelve `profile.*` names from `01-SPEC.md:2548-2560`, and fix the "13 profile.* column names" phrasing.
7. **Task 2 action:** specify `status_filter` handling — recommend a closed enum mirroring the sort field, keeping §9.3's lifecycle vocabulary off the wire as raw text.

## Risk Assessment

**MEDIUM.** All ten cycle-1 findings owned by this plan are resolved with falsifiable criteria, and the underlying source claims re-verified cleanly. The residual risk concentrates in the one seam the R-21 fix opened: the admin detail read's property-row path is specified at the wrong layer with the wrong model, and because 06-08's Sheet-overwrite safety depends on it, an executor's plausible misreading reproduces the original defect one layer down. The `world.Character`/`normalized_name` truth contradiction and the undischarged pagination mitigation are smaller but real. One more revision pass on Concerns 1–3 should make this plan implementation-ready.


### Pass A2b — 06-05

Now let me check the cycle-1 review file for findings touching 06-05, and verify remaining cited lines.
All verification complete. Here is the review.

---

# Cross-AI Plan Review — Cycle 2, Pass on 06-05-PLAN.md

## 1. Summary

The plan is **close to implementation-ready but not there**. All ten cycle-1 findings it owns are genuinely resolved on paper — I traced each fix into the tree and the mechanisms are real (the default-deny the new seed policy answers is verifiably present at `internal/access/policy/seed.go:105-107` and `:985-987`; the two-write hazard is real at `internal/world/service.go:1054-1095` vs `:919-975`). However, the fixes themselves introduced **three new defects the plan never schedules**: the new seed policy breaks two exhaustive seed-census tests that are absent from `files_modified`; the new "builder refuses zero changed attributes" requirement contradicts a deliberately-documented shipped behavior of the player path; and the `WithDescription` extension never reconciles with the empty-partition early return that would silently skip a description-only write. All three fail loudly under the plan's own verify commands, so none is a silent-ship risk — but a plan at this one's own standard should schedule the fix, not rely on the executor discovering it mid-RED.

## 2. Cycle-1 finding status

| Finding | Status | Deciding evidence |
|---|---|---|
| R-27 (world-layer checkAccess default-denies player-flavored admin; no seed policy scheduled) | RESOLVED | DSL written in-plan; deny mechanism verified at `seed.go:105-107` (`principal is character`) and `:985-987` (`resource is admin_section`); player-roles attribute channel exists (`internal/access/policy/attribute/player.go:41-50`, wired `internal/access/setup/setup.go:297`). **But see Concern 1 — the fix's blast radius is unscheduled.** |
| R-28 (13-path mask splits across two domain writes; one `expected_version` cannot fund both) | RESOLVED (with new defects) | Extension of `UpdateCharacterProfileAttributes` via variadic options; the plan's added reason verified — `characterUpdatePayload` does carry a `description` string (`internal/world/outbox/taxonomy.go:189-192`). **See Concerns 2 and 3.** |
| R-22 (`AdminServer` dependency + composition root absent from `files_modified`) | RESOLVED | `admin_service.go`, `cmd/holomush/sub_grpc.go`, `harness.go` now listed; `AdminServerOption` seam exists in 06-01-PLAN.md:517. |
| R-29 (taxonomy ratchet under-enumerated; kind question hedged) | RESOLVED | Eight steps; kind reuse settled. The "revert each bump → FAIL" demonstrations rest on NEW exact-value tests (Task 1 Tests 2–3 assert `== 2`/`== 4`); existing tests assert only `>= 1` (`taxonomy_test.go:26,48,53`), so the new tests are load-bearing and the plan correctly authors them. |
| R-30 (no typed API for admin audit context) | RESOLVED | Variadic `opts ...LifecycleOption` + `WithAuditContext`; zero-caller impact verified — `rg '\.RetireCharacter\(' -g '!*_test.go'` exits 1. |
| R-31 ("methods the player path calls" overstates the tree) | RESOLVED | Reworded to "canonical lifecycle methods"; zero production callers confirmed. |
| R-32 (empty-mask divergence unflagged) | RESOLVED | Player-path no-op success verified at `characteraccess_write.go:312-336` with its "do not 'fix'" comment; plan mandates the divergence doc-block plus a grep criterion. |
| R-33 (long-cap boundary wrong) | RESOLVED | `MaxNameLength = 100` / `MaxDescriptionLength = 4000` verified at `internal/world/validation.go:20-21`; Test 6 covers both triples plus CJK on each. |
| R-34 (`events_audit` fence false at HEAD) | RESOLVED | Verified: `INSERT INTO events_audit` in production non-test Go appears **only** at `internal/eventbus/audit/projection.go:376`; all other hits are `_test.go`/`testsupport`. Helpers verified at `world_sql_fence_test.go:325-339`. Minor nit in Concern 6. |
| R-04 (`task docs:proto` omitted) | RESOLVED | `site/src/content/docs/reference/grpc-api.md` in `files_modified`; regen criterion present. |

## 3. Strengths

- **Every R-27/R-28 premise I re-derived independently rather than taking cycle 1's word.** The deny chain is real: `RetireCharacter` runs `checkAccess(..., "retire", ...)` (`service.go:1321-1322`), `UpdateCharacterProfileAttributes` runs `"write"` (`:1090-1091`), and no player-principal policy reaches a `character:` resource today — the only one is `resource is admin_section` (`seed.go:987`). The fix's scoping choices (player principal forced by D-104's Actor requirement; `delete` absent so `DeleteCharacter` is engine-denied one layer down) are correct against the tree: no `principal is player` permit on `character` exists to conflict, and the ten forbids (`seed_test.go:218-232`) touch audit/events/property only.
- **The mutator-level claim for the combined write holds.** `updateCharacterProfileAttributes` runs the version-guarded `characterWriter.Update(txCtx, char)` **first**, in-transaction, before the property partitions (`internal/world/mutator.go:331-353`). So a description set on the in-memory `char` rides the same transaction and the same single version bump with **zero** mutator changes — which is what makes the Task 1 criterion `git diff --exit-code internal/world/mutator.go` satisfiable rather than a trap. The plan doesn't spell this out, but the architecture cooperates.
- **Test 6b is a genuinely non-vacuous triple assertion.** Both candidate domain methods independently bump `characters.version` (profile path: `char.Version = expectedVersion` then CAS at `service.go:1240-1243`; description path: own read + CAS at `:955-960`), so a two-call implementation fails the version-increment, the one-envelope count, and the prose-absence assertion simultaneously, exactly as claimed.
- **The vacuity traps named in the plan are all real in the tree.** No test for `BuildCharacterProfileUpdatePayload` exists (`rg` over `*_test.go`: zero hits); the sibling mask map has exactly 12 `profile.` entries (`characteraccess_write.go:143-167`) with no `description`; the existing AppSchemaVersion/SchemaVersion tests assert `>= 1` only, so the plan authoring exact-value tests is necessary, not redundant.
- **The proto-descriptor fence is the right tool and correctly bounded** (visited set keyed on `protoreflect.FullName`; `require.NotEmpty` on the walked set), and the RED demonstration (plant `repeated string roles = 90`, `task proto`, revert) is executable as written.

## 4. Concerns

- **MEDIUM — [REGRESSION-FROM-FIX] The new seed policy breaks two exhaustive seed censuses the plan never schedules.** `internal/access/policy/seed_test.go:37` asserts `assert.Len(t, seeds, 64)` and `:206-214` asserts `ElementsMatch` against a hardcoded name inventory ending in `"seed:admin-section-access"`. Adding `seed:admin-character-administration` turns both red, yet `seed_test.go` is absent from `files_modified` and no task mentions the 64→65 bump or the inventory append. Mechanism: Task 2's own verify (`task test -- ./internal/access/...`) fails after the correct implementation, and the executor must either edit tests it wasn't told to touch or — worse — conclude its policy is wrong. The plan that (correctly) insists the authorization decision be reviewable on paper should also schedule its census bookkeeping. (`internal/access/setup/buildabacstack_seed_coverage_integ_test.go:77` may also enumerate; unverified, worth one grep during execution.)
- **MEDIUM — [REGRESSION-FROM-FIX] Task 1 Test 5 ("the builder refuses zero changed attributes") contradicts a deliberately-documented shipped behavior of the player path.** `service.go:1120-1132` documents the all-identical resubmit case: partitions non-empty, `changed` **empty**, and the envelope ships with an empty list — *"an all-identical resubmit shipping an empty list alongside a real row write is both representable and honest."* That path calls `BuildCharacterProfileUpdatePayload(characterID, changed)` with an empty slice. A builder that errors on empty `changed_attributes` converts that documented honest success into an error on a shipped player surface — and the must-have "*changed_attributes is never an empty array in an emitted payload*" contradicts the same comment. Either the plan means an **admin-side** precheck (then Test 5's "error from the builder" is the wrong layer and the builder stays unchanged), or it means to change the player path's emission semantics (then that is an unflagged behavior change to a shipped RPC). As written, the two readings diverge and the executor picks one silently.
- **MEDIUM — [REGRESSION-FROM-FIX] The `WithDescription` extension never reconciles with the empty-partition early return.** `service.go:1222-1233`: when `creates`/`updates`/`deletes` are all empty the method returns `nil` **before** the executor — after only the version check. A description-only mask (`update_mask: ["description"]`) puts nothing in `attributes`, so the partition is empty, the early return fires, and the description option is never applied: Task 2 Test 1 (mask naming one of the 13 paths applies it) goes RED for exactly the path the whole R-28 fix exists to serve. The action's "write the description when the option was supplied" never says where relative to this return, nor what an unchanged-description-plus-empty-partition request should answer (success with no version bump? the response's `version` then equals the request's — fine, but unspecified). The executor must improvise guard-ordering surgery on the phase's most sensitive method.
- **LOW — Task 1's payload grep criterion matches a field that legitimately exists.** `rg -n 'description|pronouns|profile\.' internal/world/payloads.go` will match `CharacterUpdateChangePayload.Description` (`payloads.go:300-305`) — a real prose-carrying payload field for `character_updated`, which this plan deliberately leaves in place (it just refuses to route admin edits through it). The criterion is judgment-phrased so it cannot false-fail mechanically, but as written it invites a reader to "fix" the wrong thing.
- **LOW — Task 2's wildcard grep always matches.** `rg -n 'HasPrefix|strings.Contains|glob|\*' internal/grpc/admin_characters_write.go`: `\*` matches any Go pointer (the handlers will be full of `*adminv1.AdminUpdateCharacterRequest`). Judgment criterion again, but the regex as spelled cannot print "no match," weakening the signal.
- **LOW — The fence's "retention-partition mover" file does not contain the fenced string.** Verified: the only production non-test `INSERT INTO events_audit` is `projection.go:376`; the mover lives in migration SQL / partition DDL, not a Go insert. "Enumerate that file explicitly" names a file that will never appear in the match set — harmless, but the allowlist should be stated as "`projection.go` only, and here is the proof."

## 5. Suggestions

1. **Task 2, action + `files_modified`:** add `internal/access/policy/seed_test.go`; schedule the `64 → 65` count bump with the one-line provenance comment the file's own convention uses, and the `expectedNames` append. Run `rg -n 'SeedPolicies|expectedNames' internal/access --type go -g '*_test.go'` during execution to catch any third enumeration.
2. **Task 1, Test 5:** relocate the zero-changed refusal from the builder to the admin handler (or to the extended service method behind the option), and state explicitly that the player path's all-identical-resubmit behavior (`service.go:1120-1132`) is unchanged. Fix the must-have to "*admin-emitted* payloads never carry an empty `changed_attributes`" or strike it.
3. **Task 2, world-service action:** add one sentence placing the description write relative to the early return — e.g. "the `WithDescription` option, when supplied with a value differing from `char.Description`, defeats the empty-partition early return; when supplied and equal, the request is the documented no-op." Add a Test 1b: description-only mask applies and bumps version by one.
4. **Task 1 criterion:** narrow the grep to `rg -n 'profile\.' internal/world/payloads.go` (the `description` and `pronouns` arms match legitimate shipped fields/comments).
5. **Task 2 criterion:** drop `\*` from the wildcard grep or anchor it (`\*` in a mask-matching context), so the output is interpretable.
6. **Task 3 action:** state the fence allowlist as "`projection.go` alone" with the `rg` evidence, rather than instructing enumeration of a mover file that carries no insert.

## 6. Risk Assessment

**MEDIUM.** Every cycle-1 HIGH is verifiably closed with real mechanisms, and nothing in the plan risks a silently-dead security property — the three new defects all fail **loudly** under the plan's own verify commands (seed census red, Test 1 red, player-path resubmit regression red under `task test -- ./internal/world/...`). The risk is execution-grade, not design-grade: an executor hitting unscheduled red on the phase's most sensitive surfaces (the seed corpus, the shared profile-write method) may improvise — and improvisation on an authorization census or a shipped player path is precisely what this plan's own standards exist to prevent. Scheduling Concerns 1–3 (a few lines each) drops this to LOW.


### Pass B1 — 06-03, 06-06

I'll start by reading the materials not inlined: the two remaining plans, cycle-1's review, and key source files.
Now I have the full picture. Time to verify the fixes against the tree.
Key verifications underway. Now checking the remaining seams:
`position: fixed` exists in components that render inside `.shell` — this is exactly the hazard 06-06's containment check is supposed to catch, but its grep scope is too narrow. Checking whether those components portal or render in-place:
I have everything I need. Compiling the review.

---

# Cross-AI Plan Review — Phase 6, Convergence Cycle 2 (Plans 06-03, 06-06, 06-07, 06-08)

## 1. Summary

The cycle-1 fixes are, with one exception, real fixes: I traced each revised mechanism against the tree and the R-44 side-derivation, R-21 detail-fetch, R-11 layout-data resolution, R-33 two-cap boundaries, R-37 load-function test, and R-38 narrowed redirect rule all now specify mechanisms that can actually produce the behavior their criteria assert. However, the pass is **not implementation-ready**, because two of the fixes themselves introduce new defects. The largest is the R-35 container move: putting `container-type: inline-size` on the shipped `.shell` makes it the containing block for `position: fixed` descendants, and there **is** one — the draggable terminal `Composer` (`web/src/lib/components/terminal/Composer.svelte:190`), whose JS drag math is viewport-relative — while the plan's own safety grep is scoped to two paths that cannot see it. The second is the R-45 byteCount extraction: its `rg -c 'TextEncoder' web/src/` totals-exactly-1 criterion is unsatisfiable while the same task forbids modifying the three shipped test files that also contain `TextEncoder`. Both are plan-level fixes, cheap to make now.

## 2. Cycle-1 finding status

| Finding | Status | Deciding evidence |
|---|---|---|
| R-35 full-bleed container false ≥768px | **RESOLVED — but NEW-DEFECT-FROM-FIX** (see Concern 1) | 06-06 Task 1 moves `container-name: vp` to `(authed)/+layout.svelte`'s `.shell`, which I verified contains the one `SectionRail` (`(authed)/+layout.svelte:21-22`); count-1 criterion pins placement |
| R-44 CSS cannot flip Sheet `side` | RESOLVED | `side` prop verified at `sheet-content.svelte:18,25,36`; 06-08 Task 1 derives it from a `ResizeObserver` on the vp element; matched-pair RED demonstration is sound |
| R-21 edit Sheet has no data source | RESOLVED | 06-04-PLAN.md:213,363,545 — `AdminCharacterDetail` with `description` + twelve `profile.*`; 06-08 fetches on open, Save disabled until resolved |
| R-11 planned-section metadata on error path | RESOLVED | 06-06 Task 3 resolves from `$page.data.sections`; criterion greps `AdminGetSection\|getAdminSection` to zero in `[section]/` |
| R-33 long-cap E2E boundary wrong | RESOLVED | 06-08 Task 3: 99/100/101 and 3999/4000/4001, "101 must be ACCEPTED here" for the long field; caps verified at `internal/world/validation.go:20-21` |
| R-36 classifier residue | RESOLVED | 06-06 Task 2: total `classifyAdminFailure`, everything-else-defaults-denial, per-code Vitest including unenumerated |
| R-37 zero-section test tests the wrong unit | RESOLVED | `+layout.test.ts` imports `load`, asserts throw-404 for `{sections:[]}`, demonstrated RED by returning instead of throwing |
| R-38 `/admin` resolution contradicts redirect ban | RESOLVED | `redirect(303)` permitted in exactly `admin/+page.ts`; criterion narrows to `rg -l` listing exactly one file |
| R-40 `<tr>`-as-button invalid markup | RESOLVED | 06-07 Task 2 specifies primary-cell button + `::after` overlay; grep rejects `<tr onclick`/`role="button"` |
| R-41 `<Select` grep against assumed API | RESOLVED | 06-07 Task 3 requires confirming 06-03's generated shape + export-independent `getAllByRole('combobox')` |
| R-45 second byte-counting implementation | **PARTIALLY RESOLVED — NEW-DEFECT-FROM-FIX** (see Concern 2) | Extraction is correct; the `rg -c 'TextEncoder'` totals-1 criterion contradicts the same task's "Phase 5 tests stay green unmodified" |
| R-03 ninth amendment false premise | RESOLVED | 06-03 Task 3 item 9 withdraws it and files a `bug` on `.claude/rules/grpc-errors.md` citing §14 row 8, #4949, #4902; criterion asserts the withdrawn spelling appears nowhere |
| R-06 `^Admin` fence naming-based | RESOLVED (as filed limitation) | 06-03 Task 3 amendment 11 |
| R-12 vacuous `gh issue list` verify | RESOLVED | Binding title convention `Phase 6 amendment: …` + non-empty-list criterion |
| R-13 UI-SPEC "(5 columns)" | RESOLVED (SUMMARY note, rationale sound) | 06-03 Task 3 closing paragraph |
| R-14 unpinned `shadcn-svelte@latest` | RESOLVED | Resolve-then-pin two-step in 06-03 Task 1 |
| R-15 `+error.svelte` viewer source imprecise | RESOLVED | `authState` store verified in use at `(authed)/+layout.svelte:18`; `visibleSections({isGuest})` signature verified at `sections.ts:63`; unpopulated-store Vitest case added |
| R-46 768–815px band untested | RESOLVED | 06-08 Proof 1b: 780px describe, width equality + visible rail + `data-side="right"` |

## 3. Strengths

- **The R-35 fix's containment-safety instinct is right, even though its grep scope is wrong.** The plan correctly identifies that `container-type: inline-size` implies layout containment and makes `.shell` a containing block for fixed descendants, and it makes a hit a stop-and-re-plan condition. The failure is only in where it looks (see Concern 1).
- **The R-44 repair is mechanistically sound and I verified every link.** `side` is a prop (`sheet-content.svelte:18`), emitted as `data-side={side}` (`:36`), `portalProps` exists (`:20,24`) and is spread into `<SheetPortal>` (`:31`), and the generated class string carries `data-[side=right]:sm:max-w-sm` (384px) — so the Svelte-side derivation plus portal retarget plus callsite 380px override is exactly the right three-part split, and the matched pair of demonstrated REDs (remove `portalProps` → only CSS half fails; hard-code `side` → only attribute half fails) genuinely proves mechanism independence.
- **The R-21 fix is honest about the inversion.** The must-have is explicitly rewritten ("INVERTED, see R-21") to say the Sheet *has* a real fetch and an honest loading state, with Save disabled until the detail resolves — and 06-04's `AdminCharacterDetail` exists in that plan at three places (`:213`, `:363`, `:545`), so the cross-plan dependency is real, not aspirational.
- **R-37's fix tests the deciding unit.** `+layout.test.ts` imports `load` and asserts it *throws* 404 on `{sections: []}`, with the sibling denial/infrastructure cases in the same file — the exact shape cycle 1 suggested.
- **The R-03 withdrawal is handled correctly.** Rather than softening the language, the plan documents why the premise was false (verified: `pkg/errutil/testing.go:15-20` is literally `oops.AsOops` + `.Code()`), files the drift where it actually lives (`.claude/rules/grpc-errors.md`), and adds a criterion that the withdrawn spelling appears in no plan — including a grep over the phase's own PLAN files.
- **Invariant plumbing for the error-boundary census is real.** `TestEveryRegistryInvariantHasBinding` (`test/meta/invariant_registry_test.go:232`) and `TestBoundInvariantsAreGenuinelyAsserted` (`:1128`) both exist, and `findRepoRoot` (`test/meta/meta_helpers_test.go:41`) means the WalkDir root resolves correctly from the package directory.
- **06-07's row-order assertion remains the right shape** (unsorted input, index-for-index), and the R-40 markup it now specifies (primary-cell `<button type="button">` + `::after` overlay, `<tr>` handler-free) is valid HTML and keyboard-accessible.

## 4. Concerns

- **HIGH [REGRESSION-FROM-FIX] — the `vp` container on `.shell` re-anchors the terminal Composer's `position: fixed`, and the plan's safety grep cannot see it.** 06-06 Task 1 adds `container-type: inline-size` to `.shell` (`(authed)/+layout.svelte:40-44`) and verifies safety with `rg 'position: *fixed'` over only `(authed)/+layout.svelte` and `web/src/lib/components/shell/`. But `.shell` wraps **every authed route**, including `/terminal`, and `web/src/lib/components/terminal/Composer.svelte:190` declares `.composer { position: fixed; z-index: 100; … }` rendered **in place** (no portal — `rg 'Portal' Composer.svelte` is empty; contrast `CommandPalette.svelte:95,120`, which uses `Dialog.Portal` and is safe). With layout containment, `.composer` anchors to `.shell`, whose top edge is `var(--topbar-h)` (44px) below the viewport — while the composer's position is driven by `style="left: {pos.x}px; top: {pos.y}px"` (`Composer.svelte:144`) from `e.clientX/clientY` drag math (`:91-95`) and persisted `composerPos` prefs (`:44-53`), all viewport-relative. Result: the composer renders 44px below its stored position on every open and fights the pointer by 44px during every drag, on the game's primary surface. The plan's must-have truth — "the (authed) subtree contains no position: fixed descendant (verified — … returns nothing)" — is **factually false**; the verification was scoped to 2 of the ~40 files the subtree renders. And the E2E never opens the composer, so nothing downstream catches it. The R-35 fix is correct in principle; its guard is scoped too narrowly to protect what it claims to protect.
- **HIGH [REGRESSION-FROM-FIX] — 06-08's `TextEncoder` count criterion is unsatisfiable under the same task's own constraint.** Task 1's criterion: "`rg -c 'TextEncoder' web/src/` totals exactly **1**, in `web/src/lib/text/byteCount.ts`" — and, two sentences later, "`pnpm exec vitest run ByteCounter ProfileSection` passes with those files' shipped assertions **unmodified**." But the shipped tree contains `TextEncoder` in five places: `ByteCounter.svelte:34` (being extracted — fine), `ByteCounter.svelte.test.ts:43`, `ProfileSection.svelte.test.ts:253`, `CreateCharacterForm.svelte.test.ts:318` (all three independent test oracles the plan forbids touching), and a comment at `CharacterPortrait.svelte:28`. After a correct extraction the total is 5, not 1. The criterion fails the correct implementation — precisely the vacuity class cycle 1 named ("would fail a correct implementation and pass the buggy one"), reintroduced by the R-45 fix. Scope the grep to non-test source (`web/src/lib --glob '!*.test.ts'`) or count the expression `new TextEncoder().encode` in `.svelte`/`.ts` non-test files.
- **MEDIUM — the rail-merge and drawer grouping have no data path: admin sections load in the child layout, but the rail and drawer render in the parent.** 06-06 Task 1 specifies that at 768–1023px "the admin nav merges into the rail below a divider" and at <768px "the drawer holds rail sections and admin sections together … two group labels". The rail and the drawer are both rendered by `(authed)/+layout.svelte:22,31-37`; the admin section list is awaited in `(authed)/admin/+layout.ts`. SvelteKit data flows from parent layouts **down** — a child layout's load result is not visible to the parent that renders the rail. No store (the `vpContainer` pattern the same plan uses for the element reference) is named for the section list, and `SectionRail.svelte`'s modification is scoped to two CSS rules. The executor must invent the data flow. Compounding it: at 768–1023px `.adminnav { width: 0 }` hides the AdminNav, and the two specified rail additions (`.rail-btn.is-context`, `.rail-identity`) are a context button and an identity block — **neither is a list of admin sections**, so as written the middle band has no way to navigate between admin sections at all. Either the rail renders admin entries (needs the store) or the band has a navigation hole; the plan must say which.
- **MEDIUM — indistinguishability holds at page level but not at chrome level, and Proof 4 only asserts page level.** A root routing miss renders the root layout + root `+error.svelte` only. An `/admin/characters` denial thrown in `(authed)/admin/+layout.ts` renders root layout **+ `(authed)/+layout.svelte`** (its load succeeded) + the root `+error.svelte` — i.e., with the rail and shell chrome. An unknown `/c/[id]` sits outside `(authed)` (D-85) and renders without it. So for the same logged-in non-admin, two of the "four kinds of miss" render inside the authed shell and two render bare — a per-viewer distinguishable signal that `/admin/*` routed somewhere real, which is the bit the design exists to withhold. 06-08 Proof 4 asserts "same heading, same body, same destination list" — all inside the boundary — so it passes while the chrome differs. The page-level property is fine as far as it goes; the plan should either assert the surrounding shell in Proof 4 (and decide which chrome is correct) or record the chrome boundary as an accepted residual with its mechanism stated.
- **LOW — 06-08 Proof 2 silently assumes an over-cap field can still be saved.** The E2E types 101 bytes into a 100-byte-cap field and expects the *server's* `InvalidArgument`. But the Sheet has an over-cap predicate ("the counter's over-cap predicate flips at exactly 101 bytes"), and the natural executor move — matching `ByteCounter`'s `over` contract — is to disable Save when over cap, which makes the proof unreachable. One sentence ("over-cap styling never disables Save; the server is the enforcer") closes it.
- **LOW — naming drift between 06-06's artifacts and its action.** `<artifacts_this_phase_produces>` lists `web/src/lib/connect/errors.ts — isAdminDenialError, isInfrastructureError`, but Task 2 specifies a single total `classifyAdminFailure`. Similarly 06-08's artifacts say the *admin* layout carries "the exported `vp` container element reference" when 06-06 puts the `bind:this` + store write in `(authed)/+layout.svelte`. Both are executor-confusion-grade, not defect-grade.
- **LOW — the `rg -c … totals exactly N` phrasing recurs in three criteria** (06-06 `container-name: vp`, 06-08 `<Toaster`, 06-08 `TextEncoder`). `rg -c` prints per-file counts and exits 1 on zero matches; a human executor can sum, but the TextEncoder instance shows this shape failing in practice. `rg -l … | wc -l` (count of *files*) or a scoped glob is the robust spelling.

## 5. Suggestions

1. **06-06 Task 1:** widen the containment-safety grep to `web/src/routes/ web/src/lib/` (which returns `Composer.svelte:190` today), and take the stop-and-re-plan branch the plan already promises: portal the Composer to `document.body` (the `CommandPalette` precedent at `CommandPalette.svelte:95`) or make it absolutely positioned within the terminal container. Add an E2E or Vitest assertion that the composer opens at its stored position — otherwise the regression class stays invisible.
2. **06-08 Task 1:** change the criterion to `rg -c 'new TextEncoder\(\)\.encode' web/src/lib --glob '!*.test.ts'` totals exactly 1, and state explicitly that the three shipped test files keep their independent `TextEncoder` oracles — an oracle recomputing the expected value is not a second implementation.
3. **06-06 Task 1/Task 2:** name the store that carries the awaited section list from the admin layout to the authed shell's rail/drawer (e.g. `adminSections` beside `vpContainer`), and state what renders the admin section entries at 768–1023px while `.adminnav` is zero-width.
4. **06-08 Proof 4:** extend the assertion to the surrounding shell (rail present/absent) for the four miss kinds, or add a must-have recording that chrome-level sameness is out of scope with the mechanism (SvelteKit renders layouts above the failed segment) stated.
5. **06-08 Task 1/Proof 2:** add "over-cap display never disables Save" to the Sheet's contract so the server-agreement proof is reachable.
6. **06-06/06-08:** align the artifacts lists with the action spellings (`classifyAdminFailure`; vp reference exported from `(authed)/+layout.svelte`), and replace the three `rg -c … totals` criteria with `rg -l | wc -l` or scoped globs.

## 6. Risk Assessment

**MEDIUM-HIGH.** Every cycle-1 finding aimed at these four plans is genuinely resolved at the mechanism level — I traced each fix into the tree rather than taking the disposition tables' word, and the R-44/R-21/R-37/R-38 repairs in particular are textbook. But the two highest-effort fixes each broke a neighbour: the R-35 container move ships a silent, user-visible regression on the terminal composer whose own guard is scoped to miss it (a "green build, dead property" instance introduced by the fix for one), and the R-45 extraction carries an acceptance criterion that cannot pass under the plan's own constraints. Concerns 1 and 2 are one-paragraph plan fixes; left standing, the first lands a real UI regression on the game's main surface and the second guarantees a red verify step at execution time. Fix those two and specify the rail data flow, and this pass drops to LOW.


### Pass B2 — 06-07, 06-08

I'll start by loading the required session-start skill, then read the plans and cycle-1 review.
Now let me verify key source claims against the tree.
Now let me check several source seams in parallel.
Two significant seams forming. Let me check the SectionRail structure and how the Admin rail entry is supposed to appear.
I have everything needed. Here is the review.

---

# Cycle 2 Review — Plans 06-03, 06-06, 06-07, 06-08

## 1. Summary

The revision is substantially better than cycle 1: nearly every cycle-1 finding that touches these four plans is resolved with a *mechanism*, not prose — the `side`-prop impossibility is correctly split into a Svelte derivation plus a CSS half with a matched RED pair (`sheet-content.svelte:18,25,36` re-verified), the Sheet's missing data source is fixed coherently across 06-04 (`AdminCharacterDetail`, verified at `06-04-PLAN.md:363-364`) and 06-08's fetch-on-open, and the byte-cap tests now use both real caps (`internal/world/validation.go:20-21` verified). However, the fixes introduced **one HIGH structural defect**: moving the rail up to the `(authed)` layout (the correct R-35 fix) strands the 768–1023px "nav merges into the rail" treatment, because the rail's renderer no longer has access to the admin section data, and no plan ever builds the Admin rail entry that three plans presuppose. Three further acceptance criteria are unsatisfiable as written (`TextEncoder` count, `DENY_` grep, `handle` substring), and 06-06's `artifacts` metadata was left contradicting its own revised tasks. Not implementation-ready until the rail-merge hole and the broken criteria are repaired.

## 2. Cycle-1 finding status

| Finding | Status | Deciding location |
|---|---|---|
| R-35 — full-bleed claim false ≥768px; second rail (06-06) | **PARTIALLY RESOLVED + NEW-DEFECT-FROM-FIX** | 06-06 Task 1 (container on `.shell`, `(authed)/+layout.svelte:21-27` verified) — but see Concerns 1 & 2 |
| R-11 — planned-section metadata unavailable on error path (06-06) | RESOLVED | 06-06 Task 3: renders from `$page.data.sections`; `rg 'AdminGetSection|getAdminSection' …/[section]/` zero-match criterion |
| R-38 — `/admin` resolution vs `redirect(30` ban (06-06) | RESOLVED | `redirect(303)` permitted in exactly one file; criterion narrowed at 06-06:368 |
| R-37 — zero-section test can't observe the 404 (06-06) | RESOLVED | `+layout.test.ts` imports `load` and asserts throw; demonstrated RED required |
| R-36 — classifier residue policy (06-06) | RESOLVED | `classifyAdminFailure` total function, fail-safe denial default, per-code Vitest at 06-06:370 |
| R-44 — CSS cannot flip `side` (06-08) | RESOLVED | ResizeObserver derivation + `portalProps` + matched-RED pair; `sheet-content.svelte:18,25,31,36` re-verified |
| R-21 — Sheet has no data source (06-08/06-04) | RESOLVED | `AdminCharacterDetail` (06-04:363-364) + honest loading state with Save disabled |
| R-33 — long-cap E2E boundary wrong (06-08) | RESOLVED | Two triples 99/100/101 & 3999/4000/4001; "101 must be ACCEPTED" on the long field |
| R-45 — second byte counter (06-08) | **NEW-DEFECT-FROM-FIX** | Extraction is right; the `rg -c 'TextEncoder' web/src/` "totals exactly 1" criterion is unsatisfiable — see Concern 3 |
| R-46 — 768–815px band untested (06-08) | RESOLVED | 780px `test.describe` pinning width equality, rail visibility, `data-side="right"` |
| R-40 — `<tr>`-as-button invalid markup (06-07) | RESOLVED | Primary-cell button + `::after` overlay, keyboard-activation and no-`onclick` assertions |
| R-41 — `<Select` count against assumed API (06-07) | RESOLVED | Shape confirmed against 06-03 SUMMARY + `getAllByRole('combobox')` belt-and-braces |
| R-12 — issue-search verify vacuous (06-03) | RESOLVED | Binding `Phase 6 amendment: …` title convention; non-empty-list criterion |
| R-13 — UI-SPEC "(5 columns)" stale (06-03) | RESOLVED | Recorded as SUMMARY note, deliberately not an issue |
| R-14 — unpinned `shadcn-svelte@latest` (06-03) | RESOLVED | Resolve-then-pin, recorded in SUMMARY |
| R-15 — `+error.svelte` viewer source imprecise (06-03) | RESOLVED | `authState` store + explicit `visibleSections({isGuest})` + unpopulated-store Vitest |
| R-03 — ninth amendment's false premise (06-03) | RESOLVED | Withdrawn; replaced with a `bug` on `.claude/rules/grpc-errors.md`; absence criterion at 06-03:400 |
| R-06 — naming-based placement fence (06-03) | RESOLVED (as filed follow-up) | Amendment 11 declares the limit explicitly |

## 3. Strengths

- **The R-44 fix is mechanistically correct, not cosmetic.** `side` is derived from a ResizeObserver on the `vp` element (`side = vpWidth < 768 ? 'bottom' : 'right'`), CSS keeps only what CSS can do (height, font), and the *matched pair* of demonstrated REDs (remove `portalProps` → only CSS half fails; hard-code `side` → only attribute half fails) is exactly the experiment that would have caught the original impossible design. Verified the underlying facts at `sheet-content.svelte:18,25,31,36`.
- **The R-21 fix is coherent across plans.** 06-04 now returns `AdminCharacterDetail` ("every `AdminCharacter` field plus `description` plus the twelve `profile.*` values", 06-04-PLAN.md:363-364) and 06-08 inverts the old must-have into an honest loading state — header immediate from the row, Group 2 skeletons, Save disabled until resolved, retry on failure. The list projection stays prose-free, preserving the no-bulk-prose-export property.
- **06-06's empty/denied analysis remains honest.** The zero-permitted-sections branch is built, declared unreachable (type-scoped seed at `internal/access/policy/seed.go:985-987` + the gated `AdminListSections`), tested at the level that actually decides (`+layout.test.ts` on `load`), and tied to the probe-immateriality tripwire for the day per-section grants land.
- **06-07's factual claims verified.** Character names are indeed bounded at 32 runes (`internal/charname/syntax/syntax.go:41: MaxNameLength = 32`), usernames at 30 (`internal/auth/player.go:25`), and the five sortable columns match 06-04's fixed six-value enum (`{UNSPECIFIED, NAME, CREATED_AT, STATUS, LAST_ACTIVE_AT, PLAYER_USERNAME}`, 06-04-PLAN.md:317) — `player_id` click-to-filter is safe because it is equality, not ordering.
- **06-03's `alert-dialog` foresight.** It is the deliberate twelfth component beyond the locked ten, added because the retire confirm must not be backdrop-dismissible — verified `web/src/lib/components/ui/` has `dialog` but no `alert-dialog` today. 06-08's dependency is real.
- **The fail-safe classifier default.** `classifyAdminFailure` defaults everything unrecognized to denial (the most conservative screen), with the reasoning mandated into the doc comment and a Vitest case per code including an unenumerated one — this directly answers codex's residue finding.

## 4. Concerns

- **HIGH [REGRESSION-FROM-FIX] — The 768–1023px "nav merges into the rail" treatment is structurally unreachable after the container move, and the Admin rail entry is built by no plan.** R-35's fix (06-06 Task 1) moves the rail into `(authed)/+layout.svelte` and forbids a second rail in the admin layout (`rg -n 'SectionRail' web/src/routes/\(authed\)/admin/` zero matches). But the merge treatment (06-06 Task 1, bands section) requires *admin section entries* to render **inside the rail**: `.rail-btn.is-context` on "the `Admin` button" and `.rail-identity` "returns at the rail foot". The admin section list is awaited in `admin/+layout.ts`; `SectionRail` is rendered one level up by `(authed)/+layout.svelte`, and SvelteKit data flows downward — the rail cannot see the child route's awaited `AdminListSections` array. Worse, the *single* Admin rail entry itself is never implemented: 06-02's truth says "the rail draws no /admin entry" for role-less players and its output is "the `roles` hint the rail uses to decide whether to draw `/admin` at all", but 06-02's `files_modified` is entirely Go/proto — no web files. 06-03 asserts "The rail's `Admin` entry is drawn separately, by the rail, from `WebCheckSessionResponse.roles`". 06-06 touches `SectionRail.svelte` but its acceptance criterion restricts the diff to "only ADDED `.is-context` / `.rail-identity` rules scoped inside `@container vp (max-width: 1023px)`" — CSS-only. So the markup that draws the Admin entry (and carries `.is-context`) belongs to no task, and `/admin` is unreachable by navigation: the phase's own "no rail icon without permission" story has no implementation. Mechanism: at execution, 06-06's CSS targets elements that don't exist, 06-08's 780px E2E asserts "the admin nav is in its 768–1023px merged treatment" with no defined element, and the executor must invent both the rail entry and its data path mid-task.
- **MEDIUM [REGRESSION-FROM-FIX] — `container-type: inline-size` on `.shell` reparents the Sheet's fixed geometry, and 06-06's safety grep checks the wrong subtree.** `container-type: inline-size` implies `contain: … layout …`, and `contain: layout` makes `.shell` the containing block for **fixed-position descendants**. 06-08 deliberately portals the Sheet into that same element. Consequences: (a) the overlay (`fixed inset-0`, verified `web/src/lib/components/ui/sheet/sheet-overlay.svelte:11`) covers only `.shell`, whose height is `calc(100vh - var(--topbar-h))` (`(authed)/+layout.svelte:40-44`; `--topbar-h: 44px` at `app.css:72`) — the TopBar stays undimmed and clickable while a modal Sheet is open, and no criterion observes overlay coverage; (b) the E2E's "height ~84% of the viewport" conflicts with a percentage height resolving against the shell (84% of 768px vs 84% of 812px at the test viewport — ~5.4% off), and the plan never pins `vh` vs `%`. Meanwhile 06-06's safety verification (`rg 'position: *fixed'` over `+layout.svelte` and `shell/*.svelte`, 06-06:237) certifies only files that contain no fixed elements — the actual fixed descendants are the Tailwind `fixed` classes in `ui/sheet/sheet-content.svelte:38` and `sheet-overlay.svelte:11`, which the grep's scope excludes by construction. The check will return clean and mean nothing.
- **MEDIUM [REGRESSION-FROM-FIX] — 06-08's `rg -c 'TextEncoder' web/src/` "totals exactly 1" fails the correct implementation.** The literal appears in four other files at HEAD: `CharacterPortrait.svelte:28` (comment), `ByteCounter.svelte.test.ts:43` (helper), `ProfileSection.svelte.test.ts:253`, and `CreateCharacterForm.svelte.test.ts:318` (fixture construction) — and the same criterion forbids touching Phase 5's tests ("stay green unmodified"). The correct extraction (which the plan specifies well) cannot satisfy the count. Scope the grep to non-test production sources, or assert "exactly one *exporting* implementation" by symbol.
- **MEDIUM — 06-06's `rg -n 'DENY_ADMIN_SECTION|DENY_' web/src/` zero-match criterion is false at HEAD and falser after 06-02.** Generated Connect files already carry 11 `DENY_` matches in 2 files (`web/src/lib/connect/holomush/admin/v1/admin_pb.ts:124,280-281`, `read_stream_pb.ts:31+`), and 06-02's `admin.proto` doc comments will regenerate more under `web/src/`. The intended property is "no `DENY_*` in *authored* web sources"; as written the criterion fails regardless of implementation. Exclude `lib/connect/` (generated) from the grep.
- **MEDIUM [REGRESSION-FROM-FIX] — 06-06's `artifacts` metadata contradicts its own revised tasks in four places.** (i) The frontmatter (06-06:73-75) claims `admin/+layout.svelte` "provides: the three-column container-query frame carrying container-name: vp" with `contains: "container-name"` — while Task 1 and the acceptance criterion require **zero** `container-name` matches in that file and **two** columns. (ii) `artifacts_this_phase_produces` (06-06:488) repeats "the `vp` three-column frame". (iii) 06-06:492 says `errors.ts` gains `isAdminDenialError`, `isInfrastructureError` — Task 2 ships `classifyAdminFailure`, a different API. (iv) 06-06:493 lists a `getAdminSection(sectionId)` wrapper in `client.ts` — Task 3 says `AdminGetSection` keeps **no browser caller** and greps it to zero. Executors validate artifact claims; each stale entry produces a spurious failure or a wrong artifact.
- **LOW — 06-08's `rg -n 'grabber|grab-handle|handle'` zero-match criterion bans the bare substring `handle`.** Any conventionally named `handleSave` / `handleOpenChange` in `EditCharacterSheet.svelte` trips it, failing a natural correct implementation. Narrow to `grabber|grab-handle`.
- **LOW — 06-06's `git diff --exit-code SectionRail.svelte` "shows only ADDED … rules"** misuses `--exit-code` (a status flag, not a content filter); the intent is readable but the command as written cannot "show only" anything. Cosmetic; the byte-identical-`@media`-block half is checkable.
- **LOW — 06-06's `rg -n 'await' +layout.ts` criterion is near-vacuous** (any `await` anywhere satisfies it, including one in the wrong branch). The `+layout.test.ts` sibling cases carry the real weight, so this is noise rather than risk.

## 5. Suggestions

1. **06-06 + one of 06-02/06-06: assign the Admin rail entry an owner.** Add a task (06-06 is the natural home, since it already touches `SectionRail.svelte`) that renders a single `Admin` `.rail-btn` in `SectionRail` from the session `roles` (client-side hint only — D-102), and define the 768–1023px merge as **AdminNav narrowing to a rail-width strip in the admin layout's own column** (where the section data exists), visually adjacent to the inherited rail — not as entries injected into the parent's rail. Update the `.rail-btn.is-context` / `.rail-identity` rules and the 780px E2E to match whichever composition is chosen.
2. **06-06: fix the containment safety check.** Grep for `fixed` (Tailwind class) across `web/src/lib/components/ui/sheet/` as well, and state the actual invariant: after 06-08's portal retarget, the Sheet's fixed geometry resolves against `.shell`; pin the bottom-variant height in `vh` units (viewport-relative, as the E2E asserts) and add an E2E assertion that the overlay's computed height equals the shell's — or accept and document the 44px TopBar exposure explicitly.
3. **06-08: scope the TextEncoder criterion** — e.g. `rg -l 'TextEncoder' web/src/lib --glob '!*.test.*'` totals exactly 1, or assert a single `export function byteCount` and that both components import it.
4. **06-06: scope the `DENY_` grep** to exclude generated code: `rg -n 'DENY_' web/src --glob '!lib/connect/**'`.
5. **06-06: regenerate the `artifacts` frontmatter and `artifacts_this_phase_produces`** from the revised tasks (drop `container-name` from the admin layout's `provides`/`contains`; replace the two helper names with `classifyAdminFailure`; drop the `getAdminSection` wrapper).
6. **06-08: narrow the handle grep** to `grabber|grab-handle`.

## 6. Risk Assessment

**MEDIUM.** Every cycle-1 HIGH that touched these plans is genuinely resolved at the mechanism level, and the new verification engineering (matched RED pair, `+layout.test.ts` on `load`, total classifier) is well aimed. The residual risk concentrates in one place: the rail-merge composition after the R-35 fix, which is a phase-goal surface (ADMIN-07's nav story and the phase's reachability of `/admin`) currently owned by no task — that is a HIGH-severity hole in an otherwise sound set. The remaining findings are broken-but-easily-scoped acceptance criteria and stale plan metadata, all plan-level fixes. Address Concern 1 and the three unsatisfiable criteria (2–4) and this drops to LOW.
