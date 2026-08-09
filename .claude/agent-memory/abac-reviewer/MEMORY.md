# ABAC Reviewer Memory

Durable patterns from prior reviews. Read at the start of each review; update after.
Curated 2026-08-09; full per-review detail lives in `reports/`.

## Engine facts worth not re-deriving

- `Engine.Evaluate(ctx, types.AccessRequest) (Decision, error)`; errors are distinct from
  denials. Default-deny on EVERY abnormal path (ctx-cancel, degraded, invalid request,
  session/attr resolve error, cache unavailable, no candidate, decision-validate).
  No path returns allow on error (`internal/access/policy/engine.go:82-342`).
- **S1 system bypass** (`engine.go:92-101`) needs BOTH `req.Subject == "system"` (EXACT
  string, not a prefix — `system:bootstrap` goes through normal policy) AND
  `access.IsSystemContext(ctx)`. A bare subject without the marker is a hard
  `SYSTEM_SUBJECT_REJECTED`, not a deny.
- **OR-of-permits, forbid-overrides, NO most-specific-wins.** An UNCONDITIONAL seed
  subsumes/nullifies any conditioned permit on the same action (holomush-8m01u).
- `findApplicablePolicies` `ResourceExact` is string-equal (`engine.go:374-377`), so a
  `location:*` permit can never match `location:<realid>`. `parseEntityType`
  (`engine.go:542`, `SplitN(":",2)`) keeps the TYPE stable regardless of suffix count.
- `CanPerformAction` (`engine.go:406-540`) resolves subject+env attrs ONLY and treats a
  permit whose `when` references `resource.*` as `anyPermit=true` OPTIMISTICALLY
  (`dsl.ReferencesResourceAttrs`). The STRICT per-instance gate is `Evaluate`. Always
  confirm a strict `Evaluate` consumer exists for the same action.
- Caller-supplied `req.Attributes` overlay `bags.Action` (`engine.go:258-265`), readable
  from the DSL as `action.<key>`. Guarded only by a ONE-ENTRY denylist
  (`reservedActionKeys = {"name"}`, `types/types.go:108`), checked in both
  `NewAccessRequest` and `Evaluate` (defense-in-depth). Values are deep-cloned.
- `NewAccessRequest` rejects empty subject/action/resource (`types/types.go:144-152`) —
  this is the fail-closed backstop that lets some callers skip a panic-on-empty guard.
- The admin wildcard seed (`seed.go:104`, no action list) permits ANY new action on ANY
  resource for admins — defense-in-depth on a new action exists only if a code gate runs first.
- ABAC lifecycle: `Prepare` builds the full engine and SYNCHRONOUSLY loads the corpus;
  `Activate`'s poller is a safety net. `Orchestrator.StartAll` runs ALL Prepares before ANY
  Activate. The Prepare-done/not-Activated gap is default-deny.

## Recurring bypass shapes (check these every time)

1. **Missing AttributeProvider ⇒ silent default-deny.** `BuildABACStack`
   (`internal/access/setup/setup.go`) registers providers per namespace; a seed `when`
   referencing an unregistered `resource.<ns>.<attr>` silently never matches.
   `warnOnMissingSeedCoverage` regex-scans `SeedPolicies()` and WARNs, but (a)
   `productionRegistered` in the unit test is a hardcoded mirror, not live introspection,
   and (b) plugin-installed policies are NOT scanned. Target-only seeds (no `when`) need
   no provider — don't flag those.
2. **Empty-string / empty-list sentinels are fail-OPEN.** DSL missing-attr short-circuits
   to false, but `"" == ""` is TRUE and `resource has X` is TRUE for an empty list.
   Providers MUST OMIT an unresolved optional key and keep the `has_X` witness on BOTH
   branches (ADR ti1b, `.claude/rules/abac-providers.md`). Flag any
   `attrs["X"] = ""` next to `attrs["has_X"] = false`.
   After a sentinel sweep, `rg -i 'sentinel' <pkg> --glob '!*_test.go'` and READ every hit
   — a surviving contradictory MUST in a doc comment becomes citable precedent (v0.12 09-03
   was ruled NOT READY for exactly that, with zero code defects).
3. **Cross-namespace gate = silent fail-open.** `resolveEntity` calls every provider with
   the SAME ref, so a `player:` attr never merges onto a `character:` principal. Gating on
   `principal.player.is_guest` for a character subject never matches. Verify the attr's
   namespace actually merges onto THAT subject type.
4. **Command-handler-only gate bypassed by the web facade.** A participant/owner check
   enforced only in `plugins/*/commands.go` is skipped when `SceneAccessServer` calls the
   plugin service RPC directly. The gate must live in the SERVICE handler.
5. **Gate one handler, miss the shared primitive.** Wrap/gate the shared read primitive
   (e.g. `HistoryReader` via `authorizingHistoryReader`), not one handler — ambient Lua
   hostfuncs are a second path to nearly every primitive. Census the primitive's callers.
6. **Subject spoofing: ownership MUST precede Evaluate.** When a handler stamps an ABAC
   subject from a request field, verify ownership FIRST. Canonical shape:
   `internal/grpc/auth_handlers.go::ListAllCharacters` — resolve session → parse id →
   `ListByPlayer` membership scan → THEN `Evaluate` → THEN the read. Parse-fail and
   not-owned must return the IDENTICAL error (no enumeration oracle).
7. **Unconditional substrate mutation behind a facade check.** `focus.JoinFocus` adds
   membership + subscribes streams with NO re-check, so the facade's participation check is
   the SOLE barrier — trace that it runs on EVERY path and that an oracle error fails closed.
8. **Broad ABAC permit fenced only in-handler.** `seed:plugin-stream-subscribe` =
   `permit(plugin, write, stream)`; the real control is
   `pluginauthz.AuthorizePluginStreamContribution`. When ABAC alone is permissive, census
   EVERY path to the primitive — Lua ambient `add_session_stream`
   (`hostfunc/stdlib_streams.go`) is still unfenced (open Medium).
9. **Latent-claim recipe:** `rg -o 'resource\.<ns>\.[a-z_]+'` repo-wide minus tests/docs.
   Policy hits land ONLY in `internal/access/policy/seed.go` + `plugins/*/plugin.yaml`.

## Patterns that are CORRECT — do not flag

- **Substrate-side authorization (ADR x0ph / D4).** Scenes use FocusMemberships checked
  inside the same store-lock as the write, not `Evaluate`. When a branch touches focus/scene
  state and `internal/access/` is untouched, that's by design. Verify the spec disclaims ABAC.
- **Plugin handlers with no `Evaluate` call** where ABAC is HOST-enforced at command
  dispatch (INV-P6-6, INV-SCENE-33). DO confirm the command-dispatch wiring actually lands,
  else the permit is dead code and handler preconditions are the only gate.
- **In-handler owner/participant check alongside an ABAC policy** with a structurally
  identical predicate — closes the direct-gRPC/facade gap. Correct when the spec documents
  the error code and the plugin holds the record at that point.
- **History-decrypt gate ≠ ABAC engine.** `authguard.checkCharacter` gates SENSITIVE
  decrypt by DEK-participant `binding_id` membership; the engine is consulted only on the
  player branch. An allow-all test engine does NOT make the decrypt gate pass.
- **Target-only seeds** (no `when`) need no AttributeProvider.
- **Wildcard resource IDs** (`type:*`) match via `target.ResourceType`; providers MUST
  return `(nil, nil)` for non-ULID ids rather than erroring (fail-closed bootstrap).
- **Host-vouched subjects.** `s.pluginName` (manifest), dispatch-token actor, and
  `ActorMetadataFrom(INCOMING ctx)` are all forgery-proof anchors. `req.CharacterId` is
  logging/effect-only where the ABAC subject comes from the token.

## Invariant-binding hygiene

- A `// Verifies: INV-X` on a test that spans FEWER clauses/handlers than the invariant
  summary is a PARTIAL binding. The registry meta-test cannot detect it
  (`invariant_registry_test.go:215-220` only fails `pending` entries carrying
  `asserted_by`). Cross-check the summary's clause count against what the test asserts.
  Seen on INV-SCENE-65 (8 handlers claimed, 3 gated), INV-PRIVACY-6, INV-SCENE-61.
- Conversely: a genuinely multi-clause binding is fine if each clause has a real assertion
  in a cited file, even if only some carry the annotation — read the cited files.
- Binding an invariant to the DENY test (deny engine + NO mock expectation, so a read would
  fail) is the defensible choice: it pins the security-critical clause.
- A `// Verifies:` referencing an id not yet in `invariants.yaml` is acceptable ONLY as a
  forward-ref inside a sequenced epic that registers it — never a fabricated binding.
- `omit-don't-sentinel` (ADR ti1b) still has NO `INV-ACCESS-*` entry — that absence is why a
  contradictory in-file MUST had nothing canonical to fail against. Worth minting.

## Seed / policy change review ladder

1. `rg` the OLD resource/action string across `seed.go`, `plugins/*/plugin.yaml` `policies:`,
   and migrations. A shipped grant on a removed string = ORPHAN = NOT READY.
2. For a disable-a-stale-seed migration: `enabled=false` truly stops eval
   (`ListEnabled` → `WHERE enabled=true`); bootstrap never re-enables a removed seed; the up
   guard should be a CAS on `name+source+exact dsl_text+enabled`. The paired DOWN inherently
   can restore a bypass on an operator-disabled row — documented, acceptable.
3. Removing `resource.<x>.state` from a `when` clause is safe ONLY if the store enforces it
   via explicit SQL `WHERE state IN (...)`. Indirect enforcement (a membership filter) must
   be called out; `InviteParticipant`/`KickParticipant` have NO SQL state guard — their ABAC
   state clauses are the only gate.
4. Broadening a permit for one consumer silently widens EVERY instance-level consumer of that
   action. Scope to a distinct action if the widening was surface-specific.
5. New audit-chain subject prefix ⇒ needs TWO forbid seeds (`principal is character` AND
   `principal is plugin`). Historically missed for `rekey` / `crypto_policy`.

6. "Policy-neutral refactor" is a CLAIM, not evidence — an empty
   `git diff --stat internal/access/` proves nothing. Trace what actually reaches
   `NewAccessRequest`: subject string (byte identity — often ALSO an audit field, e.g. the
   outbox envelope `Actor`), evaluation CONTEXT (system marker), attribute map. Ask whether
   the refactor changed the *width* or *discoverability* of a bypass, not its spelling.


## world.Caller / job-principal model (02.1 READY 2026-08-08, 02.2 NOT READY 2026-08-09)

`world.Service` commands take an opaque `world.Caller`; `checkAccess` (`service.go:231-297`)
forwards `caller.attrs` into `NewAccessRequest` and evaluates against
`caller.evalContext(ctx)`. Settled:

- **`SystemCaller()` narrows S1**: marker applied only to the ctx handed to `Evaluate`;
  takes NO parameters, so no request value can *select* the bypass. `caller.go` holds the
  only non-test `access.WithSystemSubject`. `"system:bootstrap"` stays `HumanCaller`.
- **Opacity holds**: unexported fields; NO exported constructor takes an attribute map
  (`JobCaller` derives 3 hardcoded `job.`-prefixed keys from a typed `Provenance`);
  accessors live in `internal/world/export_test.go`. 02.1's carry-forward
  `dispatch_location` risk is thus CLOSED BY CONSTRUCTION, not by the denylist —
  `reservedActionKeys` is still one entry, and only 3 production non-nil-attr
  `NewAccessRequest` producers exist (`authguard/guard.go:134`,
  `pluginauthz/capability.go:50`, `world/service.go:251`). `dispatch_location` CANNOT be
  denylisted: `capability.go:50` supplies it for the host's own scope fence.
- **Still open**: no census pins the `SystemCaller()` call-site set;
  `hostfunc/cap_property.go` / `cap_world_query.go` take a Lua-SCRIPT-supplied subject
  (unwired — flag if ever wired). Reusable:
  `test/meta/world_caller_census_test.go` derives its universe over `go/ast`
  (`flattenedParamTypes` matters) and asserts the exemption set BOTH ways. Demand a
  builder-level BYTE comparison when an opaque value replaces an audit-bearing string —
  normalization inside the wrapper keeps every suite green.

## New reflexes from 02.2

1. **A name-set fence does not gate the SHAPE of an entry it already admits.**
   `TestNoPhase2SeedIntroducesACharacterResourceTypePermit`
   (`seed_profile_visibility_test.go:692`) compares NAMES of seeds whose compiled
   `Target.ResourceType == "character"`. Once a name is in `want`, widening that seed's
   action list or flipping its principal stays GREEN. When a phase adds an entry to such a
   fence with a written justification, demand an exact-DSL + `SeedVersion` pin (precedent
   `:653-661`, `:757`), and **read the fence comment against the assertion** — 02.2's "a
   future edit that widens this one's action to `read` ... fails the same way" is false for
   both halves. That was the blocking finding.
2. **The `action` gate is LIVE as of 02.2-04.** `BuildABACStack` builds its compiler on the
   populated `attribute.SchemaRegistry` (`setup.go:396`), so `compiler.go:185-190`
   hard-errors → `cache.Reload` fails → boot fails. `attribute.ActionNamespaceSchema()`
   (`action_schema.go:43`) is the single source of truth; 4 sites (`setup.go:396`,
   `bootstrap/setup/subsystem.go:214`, `real_abac.go:58`, `abactest.go:73`). Validation is
   by DSL ROOT (principal|resource|action|env), NEVER by provider name — `resource.<ns>.<k>`
   is namespace `resource`, key `<ns>.<k>` — so an action-only registry is FULL fidelity.
   - **Un-gated hole**: plugin-policy install runs only `dsl.Parse`
     (`policy_installer.go:62`) + resource-attr checks (`policy_schema_validator.go:60`), so
     an undeclared `action.*` key installs, then fails every reload → `EnterDegradedMode`
     (deny-all) → boot brick. Produced-but-undeclared today: `event_type`, `plugin_name`,
     `plugin_inst` (`authguard/guard.go:134`) — referencing any is a boot brick.
3. **`job:` principal.** `access.SubjectJob` (`prefix.go:27`) disjoint from
   `SubjectSystem`; `access.JobSubject` panics on empty (4-for-4 intact).
   `attribute.JobProvider` is `PluginProvider`'s shape over a liveness registry: `(nil,nil)`
   for unknown/dead/nil-registry/foreign-prefix, `has_writes` witness on BOTH branches,
   `writes` omitted (never empty list) when undeclared. Deny code composes as a
   PREFIX (`JOB_CHARACTER_ACCESS_DENIED`) so `grpc_server.go:169`'s `_ACCESS_DENIED` suffix
   classification survives. `seed:job-fixture-instance-scoped` is UNMATCHABLE in production
   (`cmd/holomush/core.go:412` builds an empty registry) — not a live grant.
4. **INV-ACCESS-13 is a genuine multi-clause binding** (liveness / capability class /
   instance scope / ScheduledJobCaller carve-out, each a real paired assertion).
   INV-ACCESS-14 (D-55 stamping) is honestly `pending` — NO production `JobCaller` call
   site exists. Phase 3 owns it: verify stamping precedes handler logic when it lands.

## Harness note

This worktree's bash/Read harness intermittently returns STALE stdout — trust exit codes
captured as the FIRST token over printed text; `base64 < file` detects replay.
