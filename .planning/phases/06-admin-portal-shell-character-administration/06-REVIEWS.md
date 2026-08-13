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
