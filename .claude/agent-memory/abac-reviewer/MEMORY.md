# ABAC Reviewer Memory

Durable patterns from prior reviews. Read at the start of each review; update after.
**Curated 2026-08-08** (was 760 lines / 3.8x over cap): per-bead narratives for
shipped-and-verified Phase-5/6 work were collapsed into the reusable lessons below.
Full per-review detail lives in `reports/`.

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

## Reviewing a refactor that claims to be policy-neutral

`git diff --stat internal/access/` being empty is a CLAIM, not evidence. Test it by tracing
what actually reaches `NewAccessRequest`: the subject string (byte identity — it is often
ALSO an audit field such as the outbox envelope `Actor`), the evaluation CONTEXT (system
marker), and the attribute map. Check whether the refactor changed the *width* or the
*discoverability* of a bypass, not just its spelling.

## Phase 02.1 world.Caller model (2026-08-08) — READY

`world.Service`'s 23 commands moved from `subjectID string` to an opaque `world.Caller`
(`internal/world/caller.go`); `checkAccess` (`service.go:209-262`) now forwards
`caller.attrs` to `NewAccessRequest` (was hardcoded `nil`) and evaluates against
`caller.evalContext(ctx)`. Verified sound and NARROWING:

- **`SystemCaller()` narrows S1.** The marker is applied ONLY to the ctx handed to
  `Evaluate`, so it can't reach repos/outbox; it takes NO parameters, so no request-derived
  value can ever *select* the bypass. After this phase the only non-test
  `access.WithSystemSubject` in the tree is `caller.go:104`, so "ambient marker + a subject
  that happens to be `system`" is unreachable in production. Exactly 2 production
  `SystemCaller()` sites (`internal/grpc/location_follow.go:200,213`, both reads).
  `"system:bootstrap"` correctly stays `HumanCaller` (normal policy path).
- **Opacity (D-62) holds.** Unexported fields, 2 exported constructors (neither populates
  `attrs`), 0 exported accessors in `caller.go`; all 5 accessors live in
  `internal/world/export_test.go` (package `world` `_test.go` ⇒ that test binary only).
  Rewritten assertions `assert.Equal(t, world.HumanCaller("lit"), captured)` are STRONGER
  than the old string compare — they also pin `system==false`, `attrs==nil`.
- **Envelope Actor byte identity** pinned for real by
  `internal/world/outbox_actor_test.go` driving both `buildIntent` and `buildMoveIntent`.
  Reusable lesson: a normalization applied INSIDE a wrapper value is invisible in the
  migration diff and keeps every pre-existing suite green — only a builder-level byte
  comparison detects it. Demand one on any "opaque value replaces an audit-bearing string".
- **CARRY INTO 02.2 (open Medium).** The now-open `Caller.attrs` channel lands in the SAME
  `bags.Action` namespace as the live permit `seed:plugin-world-mutation-own-location`
  (`seed.go:332`, `when { resource.location.id == action.dispatch_location }`).
  `dispatch_location` is produced today only at the host-vouched
  `internal/plugin/hostcap/interceptor.go:294` and is NOT in `reservedActionKeys`. A world
  caller self-asserting it would permit an ARBITRARY location write by a plugin subject.
  Unreachable in 02.1 (attrs always nil), live the moment 02.2 populates it. Fix before
  02.2: reserve the key, namespace the world channel's keys, or make the guard an allowlist.
- **Open Medium #2.** No census pins the `SystemCaller()` call-site set, and the bypass is
  no longer findable via `rg WithSystemSubject`. Recommend a `test/meta/` allowlist census
  in the `worldCallerExemptCommands()` idiom.
- Structural census pattern worth reusing: `test/meta/world_caller_census_test.go` derives
  the command universe over `go/ast` (predicate: exported `*Service` method whose first
  FLATTENED param is `context.Context`; authorizing = body references the receiver's
  `checkAccess`), asserts the exemption set in BOTH directions, and carries anti-vacuity
  guards (`require.NotEmpty` on the universe, `require.NotZerof` on the authorizing count).
  `flattenedParamTypes` matters — grouped params make `Params.List[1]` ≠ parameter 1.
- `internal/plugin/hostfunc/cap_property.go` / `cap_world_query.go` still take a
  Lua-SCRIPT-supplied subject string (`L.CheckString(1)`). Unwired scaffolding today (no
  production implementer) and outside the census universe — flag if ever wired.

## Harness note

This worktree's bash/Read harness has intermittently returned STALE stdout. Trust exit codes
captured as the FIRST token (`TESTRC=$?`) over printed text; `base64 < file` detects replay.
