# ABAC Reviewer Memory

Durable patterns from prior reviews; read at the start of each review, update after.
Full per-review detail lives in `reports/` (curated 2026-08-13).

## Engine facts worth not re-deriving

- `Engine.Evaluate(ctx, types.AccessRequest) (Decision, error)`; errors are distinct from
  denials. Default-deny on EVERY abnormal path (ctx-cancel, degraded, invalid request,
  session/attr resolve error, cache unavailable, no candidate, decision-validate).
  No path returns allow on error (`internal/access/policy/engine.go:82-342`).
- **S1 system bypass** (`engine.go:92-101`) needs BOTH `req.Subject == "system"` (EXACT
  string, not a prefix) AND `access.IsSystemContext(ctx)`. A bare subject without the
  marker is a hard `SYSTEM_SUBJECT_REJECTED`, not a deny.
- **OR-of-permits, forbid-overrides, NO most-specific-wins.** An UNCONDITIONAL seed
  subsumes/nullifies any conditioned permit on the same action (holomush-8m01u).
- `findApplicablePolicies` `ResourceExact` is string-equal (`engine.go:374-377`), so a
  `location:*` permit can never match `location:<realid>`. `parseEntityType`
  (`engine.go:542`, `SplitN(":",2)`) keeps the TYPE stable regardless of suffix count.
- `CanPerformAction` (`engine.go:406-540`) resolves subject+env attrs ONLY and treats a
  permit whose `when` references `resource.*` as `anyPermit=true` OPTIMISTICALLY
  (`engine.go:517-523`) — the subject-side conjuncts are NOT re-checked, so an
  unmatchable-in-Evaluate permit is still an optimistic type-level yes. All production
  callers are command dispatch (`command/access.go:72`, `commandquery/query.go:184`,
  `handlers/resetpassword.go:114,125`), where the subject is character/player — a
  `job:`/`plugin:` subject never reaches it. The STRICT per-instance gate is `Evaluate`.
- `bags.Resource["id"]` is stamped by the RESOLVER from the request ref before any
  provider runs (`attribute/resolver.go:211-214`) — always present, = substring after the
  first `:`, so `... == resource.id` fences work even for a nonexistent row.
- Caller-supplied `req.Attributes` overlay `bags.Action` (`engine.go:258-265`), read as
  `action.<key>`; guarded only by a ONE-ENTRY denylist (`reservedActionKeys={"name"}`).
- `NewAccessRequest` rejects empty subject/action/resource (`types/types.go:144-152`) —
  the fail-closed backstop that lets some callers skip a panic-on-empty guard.
- The admin wildcard seed (`seed.go:107`, no action list, NO resource clause) permits ANY
  action on ANY resource for admins — defense-in-depth on a new action exists only if a
  code gate runs first.
- ABAC lifecycle: `Prepare` builds the engine and SYNCHRONOUSLY loads the corpus;
  `Activate`'s poller is a safety net. `Orchestrator.StartAll` runs ALL Prepares before
  ANY Activate. The Prepare-done/not-Activated gap is default-deny.

## Recurring bypass shapes (check these every time)

1. **Missing AttributeProvider ⇒ silent default-deny.** A seed `when` referencing an
   unregistered `<ns>.<attr>` never matches. `warnOnMissingSeedCoverage` only WARNs, its
   test mirror is hardcoded, and plugin-installed policies are NOT scanned. Target-only
   seeds (no `when`) need no provider — don't flag those.
2. **Empty-string / empty-list sentinels are fail-OPEN.** Missing-attr short-circuits to
   false, but `"" == ""` is TRUE and `containsAll` against an empty list is TRUE.
   Providers MUST OMIT an unresolved optional key and keep the `has_X` witness on BOTH
   branches (ADR ti1b, `.claude/rules/abac-providers.md`). After a sentinel sweep,
   `rg -i 'sentinel' <pkg> --glob '!*_test.go'` and READ every hit — a surviving
   contradictory MUST in a doc comment becomes citable precedent.
3. **Cross-namespace gate = silent fail-open.** `resolveEntity` calls every provider with
   the SAME ref, so a `player:` attr never merges onto a `character:` principal. Verify
   the attr's namespace actually merges onto THAT subject type. Providers MUST
   prefix-guard and return `(nil,nil)` for foreign refs (`job_provider.go:64-77` is the
   reference shape).
4. **Command-handler-only gate bypassed by the web facade.** A participant/owner check in
   `plugins/*/commands.go` is skipped when the facade calls the plugin service RPC
   directly. The gate must live in the SERVICE handler.
5. **Gate one handler, miss the shared primitive.** Wrap the shared read primitive, not
   one handler — ambient Lua hostfuncs are a second path to nearly every primitive.
6. **Subject spoofing: ownership MUST precede Evaluate.** Canonical shape:
   `grpc/auth_handlers.go::ListAllCharacters` — resolve session → parse id → membership
   scan → THEN `Evaluate` → THEN the read. Parse-fail and not-owned must return the
   IDENTICAL error (no enumeration oracle).
7. **Unconditional substrate mutation behind a facade check.** Trace that the facade
   check runs on EVERY path and that an oracle error fails closed.
8. **Broad ABAC permit fenced only in-handler.** `seed:plugin-stream-subscribe` =
   `permit(plugin, write, stream)`; the real control is
   `pluginauthz.AuthorizePluginStreamContribution`. Census EVERY path to the primitive —
   Lua ambient `add_session_stream` is still unfenced (open Medium).
9. **Latent-claim recipe:** `rg -o 'resource\.<ns>\.[a-z_]+'` repo-wide minus tests/docs.
   Policy hits land ONLY in `internal/access/policy/seed.go` + `plugins/*/plugin.yaml`.

## Patterns that are CORRECT — do not flag

- **Substrate-side authorization (ADR x0ph / D4).** Scenes check FocusMemberships inside
  the same store-lock as the write, not `Evaluate`.
- **Plugin handlers with no `Evaluate` call** where ABAC is HOST-enforced at command
  dispatch (INV-P6-6/SCENE-33) — confirm the dispatch wiring lands.
- **In-handler owner/participant check alongside a structurally identical ABAC policy** —
  closes the direct-gRPC/facade gap.
- **History-decrypt gate ≠ ABAC engine.** `authguard.checkCharacter` gates SENSITIVE
  decrypt by DEK-participant membership; an allow-all test engine does NOT make it pass.
- **Target-only seeds** (no `when`) need no AttributeProvider.
- **Wildcard resource IDs** (`type:*`) match via `target.ResourceType`; providers return
  `(nil, nil)` for non-ULID ids rather than erroring.
- **Host-vouched subjects:** `s.pluginName` (manifest), dispatch-token actor,
  `ActorMetadataFrom(INCOMING ctx)`.

## Invariant-binding hygiene

- A `// Verifies: INV-X` on a test spanning FEWER clauses than the summary is a PARTIAL
  binding the registry meta-test cannot detect (it only fails `pending` entries carrying
  `asserted_by`). Cross-check the summary's clause count against the assertions.
- Binding an invariant to the DENY test (deny engine + NO mock expectation) is the
  defensible choice: it pins the security-critical clause.
- **When a registry entry names a future phase as its binding owner, CHECK at that phase
  whether the precondition landed.** INV-ACCESS-14: the D-46 wrapper landed
  (`internal/retirement/reactor.go:229-266`) and the entry stayed `pending` with no
  `// Verifies:` — how a deferral quietly becomes permanent (Medium, non-blocking).
- `omit-don't-sentinel` (ADR ti1b) still has NO `INV-ACCESS-*` entry — worth minting.

## Seed / policy change review ladder

1. `rg` the OLD resource/action string across `seed.go`, `plugins/*/plugin.yaml`
   `policies:`, and migrations. A shipped grant on a removed string = ORPHAN = NOT READY.
2. Disable-a-stale-seed migration: `enabled=false` truly stops eval; the up guard should
   be a CAS on `name+source+exact dsl_text+enabled`.
3. Removing `resource.<x>.state` from a `when` is safe ONLY if the store enforces it via
   explicit SQL. `InviteParticipant`/`KickParticipant` have NO SQL state guard.
4. Broadening a permit for one consumer silently widens EVERY instance-level consumer of
   that action. Scope to a distinct action if the widening was surface-specific.
5. New audit-chain subject prefix ⇒ needs TWO forbid seeds (`principal is character` AND
   `principal is plugin`).
6. **"Policy-neutral refactor" is a CLAIM.** Trace what reaches `NewAccessRequest`:
   subject string (byte identity — often ALSO an audit field), evaluation CONTEXT
   (system marker), attribute map.
7. **Coarse actions grant more than the phase's use case.** `write` on a character covers
   BOTH `MoveCharacter` and `UpdateCharacterDescription`; `read` covers `GetCharacter`;
   `retire`/`unretire`/`delete`/`read_description` are distinct. Map every command sharing
   the action before accepting a seed comment's enumeration of what it authorizes.

## world.Caller / job-principal model (02.1 READY, 02.2 NOT READY→fixed, 03-04 READY)

`world.Service` commands take an opaque `world.Caller`; `checkAccess`
(`service.go:233-299`) forwards `caller.attrs` into `NewAccessRequest` and evaluates
against `caller.evalContext(ctx)`. Settled:

- **`SystemCaller()` narrows S1**: marker applied only to the ctx handed to `Evaluate`;
  takes NO parameters. `"system:bootstrap"` stays `HumanCaller`.
- **Opacity holds**: unexported `world.Caller` fields; no exported constructor takes an
  attribute map. Only 3 production non-nil-attr `NewAccessRequest` producers exist
  (`authguard/guard.go:130`, `pluginauthz/capability.go:50`, `world/service.go:253`);
  `capability.go`'s Subject is host-derived `plugin:<name>`, so it can never satisfy
  `principal is job`. `dispatch_location` CANNOT be denylisted (host scope fence).
- **`job:` principal.** `SubjectJob` disjoint from `SubjectSystem`; `access.JobSubject`
  panics on empty. `attribute.JobProvider` returns `(nil,nil)` for
  unknown/dead/nil-registry/foreign-prefix; `has_writes` witness on BOTH branches,
  `writes` omitted (never empty list) and typed `AttrTypeStringList` so `containsAll` is
  fail-closed. Deny code composes as a PREFIX (`JOB_CHARACTER_ACCESS_DENIED`) so
  `grpc_server.go:169`'s `_ACCESS_DENIED` suffix classification survives.
- **`cmd/holomush/core.go:415,421,905`**: ABAC subsystem and retirement reactor share ONE
  `jobs.Registry`. That is what makes a job grant LIVE; a second registry denies all.
- **Still open**: NO census pins the `world.JobCaller` / `SystemCaller()` call-site set.
  `test/meta/world_caller_census_test.go` censuses `world.Service` command SIGNATURES
  only. Since 03-04 the missing JobCaller census guards a LIVE grant — reusable AST
  machinery is already in that file; demand the census when a job grant widens again.
- **The `action` schema gate IS LIVE in production.** `setup.go` registers `action` into
  the ONE `SchemaRegistry` (`:405`), builds the compiler on the live pointer (`:421`),
  THEN reloads (`:441`) — that order is the whole mechanism, pinned by
  `TestBuildABACStackReloadsTheCacheAfterTheActionRegistration`. `compiler.go:195-201`
  hard-errors `POLICY_UNREGISTERED_ACTION_ATTRIBUTE`. Validation is by DSL ROOT
  (principal|resource|action|env), so only `action.*` is fatal. Action TOKENS in a
  TARGET clause are NOT attribute refs (`validateAttributes` walks a `*ConditionBlock`
  only) — a new action token like `read_description` needs no schema entry; do not flag
  its absence. `attribute.ActionNamespaceSchema()` is the single source of truth.
  Un-gated hole: plugin-policy install runs only `dsl.Parse` + resource-attr checks.

## Name-set fences on seed families (D-29 pattern)

`TestNoPhase2SeedIntroducesACharacterResourceTypePermit`
(`seed_profile_visibility_test.go:692`) is the template. When a phase adds an entry to
such a fence with a written justification:

1. A name-set fence does NOT gate the SHAPE of an entry it already admits — demand a
   per-name shape map (`:767-772`) AND an exact-DSL + `SeedVersion` pin (`:838-850`).
   03-04 shipped all three; that is the standard to hold future phases to.
2. **Read the fence's collection predicate, not just its assertions.** It collects only
   `Target.ResourceType == "character"`. A bare-resource permit compiles to
   `ResourceType == nil` (`compiler.go:122-131`) and evades the fence entirely.
3. **A fence over POLICY cannot bound who mints the principal.** An instance fence like
   `action.job.trigger_subject == resource.id` is only as strong as the census of code
   that can supply the provenance; its absence is the residual risk, not the DSL.
4. Verify the exemption argument's negative claims yourself: forged events need an
   unqualified wire type (INV-PLUGIN-40 forbids it), and `HumanCaller("job:x")` yields a
   `job:` subject with nil attrs → deny.

## Harness note

This worktree's bash/Read harness intermittently returns STALE stdout — trust exit codes
captured as the FIRST token over printed text; `base64 < file` detects replay.

## Web-facade read path (phases 04-05, READY 2026-08-13)

- **`evaluateGate`** (`grpc/characteraccess_directory.go:178`) is the ONE raw `Evaluate`
  the facade owns, and it catches all THREE outcomes — error, `IsInfraFailure()` deny with
  a NIL error, ordinary deny — returning `true` only on `IsAllowed()`. Copy this shape
  when reviewing any hand-written gate; the infra-deny arm is the one reliably missed.
- **Subject is never client-supplied.** `resolveViewerTier`
  (`characteraccess_service.go:356`) is a CLOSED switch over a server-resolved identity;
  unknown rung ⇒ deny. An unresolvable token degrades to `anonymous` — fail-CLOSED.
- **Tier floors are set membership, never `>=`** (`seed.go:610,953`). A three-rung
  clearing list is an intentional always-true config surface, not a dead clause.
- `read_description` (`world/service.go:866`) is narrowed by RETURN TYPE
  (`world.CharacterDescription` = Name+Description only). The character-flavored seed
  (`seed.go:937`) is UNCONDITIONAL and consumer-less — re-derive the ceiling when a
  character-side consumer lands.
