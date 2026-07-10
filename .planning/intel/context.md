# Context (from DOCs)

2 DOC-classified documents in this ingest batch: `docs/architecture/invariants.md` and
`docs/roadmap.md`. Per the ingest instructions for this batch, **both are treated as
authoritative** despite DOC's normally-lowest precedence tier — `invariants.md` for
CONSTRAINTS, `roadmap.md` for STRATEGY/SCOPE. Precedence only matters for breaking direct
contradictions; neither doc is down-weighted otherwise.

---

## Topic: System invariant registry

- **source:** `docs/architecture/invariants.md` (generated from `docs/architecture/invariants.yaml`)
- **role:** Canonical, generated registry of every named system invariant, organized by
  scope. This is the single source of truth for "what guarantees does the system make"
  — every SPEC in `constraints.md` that mints an `INV-*` id ultimately lands here.
- **scope taxonomy (14 scopes):** `INV-CRYPTO` (event payload encryption, DEK lifecycle,
  key wrapping, AdminReadStream — 122 entries), `INV-PRIVACY` (stream history temporal
  floors, scope gating, guest bounds — 8 entries), `INV-PRESENCE` (presence snapshot
  correctness — 9 entries), `INV-SCENE` (scene lifecycle, board, content warnings, pose
  order, focus model, publish snapshot, IC isolation — 68 entries, the largest
  domain-scope after crypto), `INV-PLUGIN` (runtime symmetry, manifest validation,
  hostfunc safety, emit gates, setting isolation, plugin authz), `INV-EVENTBUS` (subject
  naming, JetStream consumer config, audit projection, tier routing, colon eradication),
  `INV-CLUSTER` (member identity, heartbeats, cache invalidation, probe-and-pill),
  `INV-ACCESS` (ABAC policy evaluation, attribute providers, seed policy shape),
  `INV-SESSION` (status lifecycle, connection attachment, focus membership),
  `INV-STORE` (migration discipline, no-DELETE enforcement), `INV-TELEMETRY` (logging
  discipline, trace context), `INV-BRANDING` (asset integrity, palette tokens),
  `INV-DOCS` (proto doc comments, IA), `INV-COMMAND` (command surfacing/runtime parity),
  `INV-COMM` (canonical communication-content payload contract).
- **binding mechanism:** an invariant starts `binding: pending` (cataloged, no test yet)
  and flips to `bound` once a test carries a `// Verifies: INV-<SCOPE>-N` annotation.
  This is a ratchet, not a blocker — `pending` is tolerated indefinitely per decision
  `holomush-hz0v4.10`. `INV-PRIVACY` is fully bound; other scopes backfill incrementally
  under epic `holomush-hz0v4`.
- **hard authority claim:** the registry's own doc states CI runs `inv-render -check`
  (generate-and-diff) and fails on drift between `invariants.yaml` and the generated
  `.md` — so the content read here is guaranteed current as of ingest time, not stale
  prose.
- **notable constraint patterns observed (representative, not exhaustive given registry
  size — 200+ entries across scopes):**
  - Crypto: KEK presence is the sole activation gate for sensitive-event crypto
    (`INV-CRYPTO-118`); a provisioned KEK is mandatory to boot, no degraded KEK-less mode
    permitted (`INV-CRYPTO-119`); DEK genesis-on-first-focus seeds the focusing reader as
    participant (`INV-CRYPTO-121`).
  - Scene: the hard privacy boundary for scene-log reads MUST remain plugin-code-enforced
    — ABAC MUST NOT be in the path for scene-log reads (`INV-SCENE-60`); scene IDs are
    bare ULIDs with no prefix (`INV-SCENE-46`); a focus-read infra error during dispatch
    of a redirect-candidate verb fails CLOSED, never silently downgrading to the
    plaintext location stream (`INV-SCENE-67`, revising an earlier fail-open contract
    per ADR `holomush-pbp9j`); scene lifecycle/membership RPC handlers MUST self-enforce
    ABAC per verb across BOTH the telnet command path and the web facade
    (`INV-SCENE-65`).
  - Privacy: a session may read only events from the interval its own session row has
    existed for that stream's scope (`INV-PRIVACY-1`); guest sessions get a temporal
    floor of `MAX(scope_floor, guest_character.CreatedAt)` (`INV-PRIVACY-2`).
  - Presence: snapshot is exempt from the temporal floor (timeless current-state read,
    `INV-PRESENCE-2`); ownership failures collapse to `SESSION_NOT_FOUND`
    (enumeration-safe, `INV-PRESENCE-3`).

## Topic: HoloMUSH strategic roadmap (themes)

- **source:** `docs/roadmap.md`
- **role:** Narrative doc framing strategic multi-epic work clusters ("themes") as
  GitHub-issue label complements. Explicitly does NOT duplicate live issue status —
  that's queried from `gh issue list` directly; this doc captures the *why* and
  sequencing rationale only. (Was `bd`-backed until the 2026-07-09 tracker migration.)
- **Active theme: `theme:social-spaces`** — Scenes, Channels, Forums, Discord share one
  substrate (persistent groups with membership, history replay, presence routing,
  subscribed clients). Substrate layers: JetStream event bus (`internal/eventbus/`),
  focus coordinator (`internal/grpc/focus/`), AccessPolicyEngine/ABAC
  (`internal/access/policy/`), plugin focus client SDK (`pkg/plugin/focus_client.go`),
  core-scenes as the reference-implementation consumer. The substrate contract (epic
  `jg9b`) codifies boundary invariants INV-S1–INV-S10. Two SDK bundles (`eventkit`,
  `groupkit`) are named but deliberately unbuilt pending a second substrate consumer
  (INV-S7 mandates N=2 validation before extraction) — Channels is slated as that second
  consumer. Sequencing: Scenes first (reference implementation, forced the JetStream
  cutover and crypto production activation), Channels in parallel where unblocked,
  Forums brainstorm in parallel (no code dependency), Discord last (depends on Channels +
  OAuth substrate). Named risks: Forums has no design yet; web-portal scope creep;
  per-connection routing needs client cooperation (`connection_id` echo protocol).
- **Active theme: `theme:plugin-capability-architecture`** — Epic `holomush-eykuh`,
  now SHIPPED (kept active only for a P3 polish tail). Triggered by boot bug
  `holomush-oeb4d` (a phantom `requires` silently disabling DAG load-order validation).
  Three delivered mandates: (1) runtime parity — binary and Lua consume host
  capabilities/plugin services through one identical host-brokered mechanism; (2) full
  dependency graph expressible (plugin→host, host→plugin, plugin→host→plugin); (3)
  least-privilege — the 25-RPC `PluginHostService` god-service split into 14 `host.v1`
  services, declaration-gated access, plugin-as-ABAC-subject.
- **Active theme: `theme:web-portals`** — Governing principle (decision
  `holomush-sz0h3`): "A player MUST be able to play completely through the web; telnet
  is never *required*." Explicit origin story: "web create-scene was an explicit
  acceptance criterion that the portal redesign dropped with no recorded decision,
  shipping a design that contradicted itself" — this directly corroborates and resolves
  **`INGEST-CONFLICTS.md` WARNING 2** (see that report). Deliberately NOT a registry
  invariant yet ("web ⊇ telnet" is directional, most surfaces are unbuilt — not yet a
  test-pinnable regression guarantee); a narrower per-surface invariant MAY be minted
  once a surface is genuinely telnet-free.
- **Completed themes:** `theme:docs-platform` (closed 2026-05-29, docs site migration);
  v0.1 Initial Release (closed 2026-05-16); "Foundational substrate pivots Q1-Q2 2026"
  — five major architecture replacements (event substrate → JetStream, plugin
  architecture → proto-first two-tier, ABAC → AccessPolicyEngine, session model →
  Postgres+JetStream replay, crypto rollout Phases 1-5+7 — **capability only**, not yet
  activated); "Crypto production activation" (closed 2026-06-10) — the crypto
  capabilities from the pivot above were never wired into the live Subscribe/Publish
  path until this milestone, closed by 4 sequential defect fixes (PRs #4411, #4413,
  #4415, #4416) surfaced by a strengthened scenes-portal E2E. Durable lesson recorded
  in the roadmap: **"a shipped capability is not a live capability"** — capability
  landing and production activation are separate milestones that can lag by months.
- **Maintenance notes (not strategic themes):** repo audit 2026-05-13 (4 read-only
  reports, tracking epics materialized/in-flight); invariant registry binding-backfill
  (epic `holomush-hz0v4`) — cataloging complete, binding is an incremental ratchet.

## Topic: Issue-tracking migration (beads → GitHub Issues + GSD backlog)

- **source:** `.planning/archive/beads/` (README.md, TRIAGE.md, exports) — added by the
  2026-07-09 migration, merged into this intel set per the ingest-docs merge shape.
- **role:** On 2026-07-09 the beads (bd) issue tracker was retired. Its 5,894-record
  export is archived (gzipped full export + plain live-508 subset). Every non-closed
  bead was triaged — code-grounded audit for bugs/P1/in_progress (140), metadata triage
  for the rest (368) — and routed: **179 GitHub issues** (discrete actionable work,
  labeled `migrated-from-beads`, body carries `Migrated from bead <id>`), **95 backlog
  items** consolidated into **17 ROADMAP `## Backlog` 999.x entries** (strategic
  clusters: web portal, channels/scenes remainders, forums, discord, architecture
  decomposition, invariant backfill, code health, ops resilience, design seeds, docs,
  features, iOS), **1 item** mapped to existing Phase 3 (external NATS, `holomush-s5ts`),
  and **234 archive-only** (verified-done, stale, duplicate, deferred, or aged-out —
  recoverable from the export).
- **downstream implications:** discovered work is filed with `gh issue create -R
  holomush/holomush`; strategic clusters go to ROADMAP `## Backlog`; `theme:<slug>`
  labels now live on GitHub issues; ADRs are self-minted `holomush-<suffix>` ids (bd no
  longer allocates them); historical `holomush-xxxx` citations in specs/plans/commits
  resolve via the archive.
