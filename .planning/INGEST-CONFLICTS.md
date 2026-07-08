# Conflict Detection Report

## BLOCKERS (0)

None. No `locked: true` ADRs exist in this ingest batch (zero ADR-classified docs at
all — see `.planning/intel/decisions.md`), so the LOCKED-vs-LOCKED check has zero
candidates. No classification carries `type: UNKNOWN` with `confidence: low` (all 50
are `SPEC`/`DOC` at `confidence: high`). Cross-ref cycle detection (DFS, three-color
marking, cap 50) traversed all 50 nodes and found zero cycles.

## WARNINGS (2)

[WARNING] Scene ownership/data-model contradiction: world-model vs plugin-owned scenes
  Found: `docs/specs/2026-01-22-world-model-design.md` models scenes as `locations`
  table rows (`type='scene'`) owned by a core `internal/world.SceneRepository`, with a
  `scene_participants` table foreign-keyed to `locations(id)`.
  Found: `docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md` (and the entire
  17-spec Epic 9 lineage that follows it through 2026-07-05) models scenes as an
  independently plugin-owned entity (the `core-scenes` binary plugin) with its own
  Postgres schema, entirely outside `internal/world`/`locations` — reinforced by
  `docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md`'s INV-S6
  ("per-plugin Postgres schema isolation") and the bare-ULID identity fix
  (`2026-05-28-scene-bare-ulid-identity-design.md`).
  Impact: Both docs are `type: SPEC` at the same default precedence tier (no ADR, no
  `locked`, no per-doc `precedence` override exists to break the tie). No document in
  this corpus explicitly names `2026-01-22-world-model-design.md` as superseded — the
  scenes-v2 doc only names an out-of-corpus "v1" design as superseded. A downstream
  consumer building PROJECT.md/constraints on this corpus could pick up the stale
  locations-table scene model if it reads world-model-design.md in isolation.
  → The plugin-owned model is overwhelmingly corroborated by `docs/architecture/invariants.md`
    (68 `INV-SCENE-*` entries, all assuming plugin-owned scene tables) and
    `docs/roadmap.md`'s `theme:social-spaces` substrate description — both DOC sources
    this ingest treats as authoritative for constraints/strategy. Recommend the
    roadmapper treat `world-model-design.md`'s scene section as historical/superseded and
    the plugin-owned model as current, but this is surfaced here rather than
    auto-resolved so a human confirms before it's baked into PROJECT.md.

[WARNING] Web scene-creation write path: command-path-only vs typed-RPC
  Found: `docs/superpowers/specs/2026-06-07-web-portal-scenes-design.md` (E9.5, decision
  D4) states: "Writes go through the existing command path. Pose/say/ooc from the
  workspace = `HandleCommand("scene pose …")` ... **No new write RPCs.**"
  Found: `docs/superpowers/specs/2026-06-19-web-create-scene-design.md` restores web
  scene creation via "the typed RPC path (proto → facade → BFF → client → UI)" and
  states explicitly it is "superseding E9.5's command-path-only decision for structural
  writes," calling the E9.5 design "a self-contradiction in E9.5's own design" (a
  scenes-only player with no terminal is first-class, yet cannot originate a scene).
  Impact: Same precedence tier (`SPEC`, no ADR/lock/override to break the tie). The
  E9.5 doc's own text is not marked superseded/amended anywhere in this corpus, so a
  reader of `constraints.md` in isolation would see two contradictory claims about
  whether web scene writes go through the command path or a typed RPC.
  → `docs/roadmap.md`'s `theme:web-portals` section (treated as authoritative for
    strategy in this ingest) directly corroborates the correction: "web create-scene was
    an explicit acceptance criterion that the portal redesign dropped with no recorded
    decision, shipping a design that contradicted itself" — and records governing
    decision `holomush-sz0h3` ("web ⊇ telnet"). Recommend resolving in favor of
    `2026-06-19-web-create-scene-design.md`'s typed-RPC decision for scene *creation*
    specifically (the E9.5 command-path decision for pose/say/ooc write verbs is NOT
    contested and stands). Surfaced as a WARNING rather than auto-resolved because this
    ingest's authority-override for DOC sources is scoped to "breaking direct
    contradictions," and per this workflow's anti-patterns a human should confirm before
    the correction is baked into PROJECT.md/ROADMAP.md.

## INFO (6)

[INFO] No ADR or PRD documents in this ingest batch
  Note: All 50 classifications are `type: SPEC` (48) or `type: DOC` (2) — zero `ADR`,
  zero `PRD`. `decisions.md` and `requirements.md` are therefore populated only with
  explanatory notes, not per-doc entries. This means the LOCKED-vs-LOCKED check and the
  PRD-competing-acceptance-variants check both trivially pass (zero candidates each) —
  not because conflicts were resolved, but because the check's input set is empty in
  this curated batch. See `docs/adr/*.md` in the wider repo (194 ADRs exist per the
  ingest corpus note) for the real decision-of-record surface, not included here.

[INFO] Archived architecture doc superseded; WASM plugin concept abandoned
  Note: `docs/plans/archive/2026-01-17-holomush-architecture-design.md` is explicitly
  filed under `docs/plans/archive/` and its own classification carries the note
  "historical context only, not current constraints." Its WASM-based plugin-system
  framing was abandoned one day later in favor of the Lua + go-plugin two-tier model
  specified by `docs/specs/2026-01-18-plugin-system-design.md`, which every one of the
  other 47 SPECs in this corpus is consistent with (none mention WASM). Auto-resolved:
  later doc's model stands; archived doc retained in `constraints.md` for provenance
  only.

[INFO] Scene bare-ULID identity fix corrects an implementation defect, not a design choice
  Note: `docs/superpowers/specs/2026-05-28-scene-bare-ulid-identity-design.md` removes an
  undocumented `"scene-"` ID prefix (silently introduced in PR #200, no ADR/spec/comment)
  that broke three host boundaries parsing scene IDs as raw ULIDs. This is a grounded bug
  fix within the same Epic 9 lineage, not a competing design decision — no other doc in
  this corpus asserts scenes should carry a type-tag prefix. Auto-resolved: bare-ULID
  wins, consistent with every other entity type in `internal/world`.

[INFO] Session liveness redesign extends, not contradicts, the base session model
  Note: `docs/superpowers/specs/2026-05-30-session-liveness-and-gateway-survival-design.md`
  adds a decaying/actively-refreshed liveness signal on top of the `SessionStore`
  TTL/detach model specified by `docs/specs/2026-03-19-session-persistence-design.md`.
  The later doc's own text frames this as fixing a root cause ("`active` is stored
  intent, not observed liveness") rather than replacing the store's schema or RPC
  surface. No direct contradiction detected; both entries stand in `constraints.md`.

[INFO] Plugin-capability-architecture epic (8 sub-specs) is a coherent sequential redesign
  Note: `2026-05-25-plugin-host-evaluate-design.md`,
  `2026-05-26-plugin-runtime-config-design.md`,
  `2026-06-11-plugin-capability-dependency-foundation-design.md`,
  `2026-06-11-plugin-host-capability-decomposition-design.md`,
  `2026-06-12-lua-parity-host-brokered-consumption-design.md`,
  `2026-06-12-plugin-least-privilege-trust-design.md`,
  `2026-06-13-plugin-capability-declaration-enforcement-design.md`, and
  `2026-06-14-plugin-capability-atomic-cutover-design.md` form one delivery sequence
  (epic `holomush-eykuh`) that decomposes the monolithic `PluginHostService` established
  by `docs/specs/2026-01-18-plugin-system-design.md` into 14 capability-scoped `host.v1`
  services. `docs/roadmap.md` confirms the epic as SHIPPED (kept active only for a P3
  polish tail). Each sub-spec cites its immediate predecessor's bead number; cross-ref
  cycle detection confirmed this sub-graph is acyclic. No contradiction detected.

[INFO] Scenes/RP Epic 9 phase series (17 specs) is a coherent incremental build
  Note: `2026-04-06-scenes-and-rp-design-v2.md` through `2026-07-05-focus-routed-scene-input-design.md`
  form Epic 9's phase sequence (v2 base → membership → adoption → phases 4/5/6/8 →
  identity fix → crypto activation → name resolution → four web slices → focus-routed
  input). Each phase explicitly cross-references its predecessor phase and the shared
  substrate contract (`2026-05-16-social-spaces-substrate-contract.md`); cycle detection
  confirmed the sub-graph is acyclic. Presented as one grouped section in
  `constraints.md` rather than 17 flat entries competing for attention.
