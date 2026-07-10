<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# HoloMUSH Roadmap

Strategic work clusters that span multiple epics. Complements GitHub Issues
(which track individual work items) and the GSD backlog
(`.planning/ROADMAP.md` → `## Backlog`) by explaining the **why** behind
multi-epic sequencing.

> Historical note: work items were tracked in beads (bd) until 2026-07-09;
> `holomush-xxxx` ids cited below resolve via `.planning/archive/beads/`.

## How this works

- **Single source of truth: GitHub Issues.** This file never duplicates
  issue state — query `gh issue list` for current status.
- **Themes are labels.** Each theme is a `theme:<slug>` label applied to
  relevant issues. Query:
  `gh issue list -R holomush/holomush -l theme:<slug> --state open --limit 200 --json number,title,labels --jq '.[] | "#\(.number) \(.title)"'`.
- **The narrative lives here.** Strategic framing, sequencing rationale,
  and substrate-vs-use distinctions belong in this file; status / dates /
  dependencies are looked up from GitHub Issues.
- **Altitude discipline.** *Active* theme sections carry only durable
  content — framing, sequencing logic, risks, and pointers (theme label,
  epic / spec IDs) — plus an issue query for live status. Do NOT hand-record
  PR numbers, per-issue status, counts, or shipment dates in active sections;
  that is exactly the detail that goes stale and forces grooming passes.
  *Completed* theme retrospectives MAY keep frozen specifics (PR numbers,
  dates) — they no longer change, and the specifics are the record.
- **Themes are added** when a multi-epic sequence becomes clear (≥2 epics
  involved). Don't pre-design themes for hypothetical work.

## Active themes

### `theme:social-spaces` — Scenes, Channels, Forums, Discord

The largest thread: four product surfaces (scenes, channels, forums, discord)
sharing one substrate — persistent groups with membership, history replay,
presence routing, and subscribed clients.

#### Substrate it builds on

The substrate landed first: scenes work triggered the JetStream cutover, then
resumed on the substrate it had pulled into existence. What a new use can rely
on:

| Layer                        | Where                         | What it provides                                    |
| ---------------------------- | ----------------------------- | --------------------------------------------------- |
| JetStream event bus          | `internal/eventbus/`          | Durable per-stream delivery; replay; backpressure   |
| Focus coordinator            | `internal/grpc/focus/`        | Session subscription routing; multi-conn visibility |
| AccessPolicyEngine (ABAC)    | `internal/access/policy/`     | Policy-driven visibility / membership / write gates |
| Plugin focus client          | `pkg/plugin/focus_client.go`  | SDK to call `JoinFocus`/`LeaveFocus`/`PresentFocus` |
| core-scenes reference plugin | `plugins/core-scenes/`        | First substrate consumer; reference implementation  |

The **substrate contract** (epic `jg9b`) codifies the boundary invariants
INV-S1 – INV-S10 — chiefly: substrate stays domain-free (INV-S2), Go+Lua
runtime parity for every host RPC (INV-S3), per-plugin Postgres schema isolation
(INV-S6), ABAC stays out of the scene-log read path (INV-S9), and emit-type
set-equality enforcement (INV-S5). Two SDK bundles are **named but deliberately
unbuilt** — `eventkit` (`pkg/plugin/eventkit/`) and `groupkit`
(`pkg/plugin/groupkit/`); INV-S7 mandates N=2 consumer validation (the channels
rework is the required second consumer) before either is extracted as substrate
code. Both are named so future brainstorms know they exist.

Specs:

- [Substrate-contract design](superpowers/specs/2026-05-16-social-spaces-substrate-contract.md) — boundary invariants, substrate inventory, INV-S1 – INV-S10
- [INV-S5 mechanism design](superpowers/specs/2026-05-17-inv-s5-mechanism-design.md) — runtime validator (Load-pass + proto extension)

#### Shared content contract

Conversational content — say/pose/ooc/emit and the targeted page/whisper/pemit —
is a cross-surface primitive: scenes, channels, and comms emit the same logical
content through different addressing. The **communication-content contract**
(`holomush.comm.v1.CommunicationContent`, `pkg/plugin/comm`) formalizes that
content body once — `actor_display_name`, `text`, `no_space`, `ooc_style` — so
both the Lua (`core-communication`) and Go (`core-scenes`) runtimes build against
one source-of-truth shape and consumers decode it once instead of re-normalizing
per plugin.

- **Slice 1 (broadcast) — landed** (epic `holomush-kk1ot`, PR #4571): proto +
  dual Go/Lua builders + `ContentValidationPublisher` (built; live gate wiring is
  Slice 2) + both emit-path migrations, binding `INV-COMM-2` (Go↔Lua builder
  parity). Unblocks focus-routed scene input (`holomush-g1qcw`) — routing a pose
  into a scene now changes only the subject, not the content shape.
- **Slice 2 (targeted) — next**: `page`/`whisper`/`pemit` (`sensitivity:always`,
  encrypted), live `ContentValidationPublisher` wiring, and the `INV-COMM-1`
  binding.

Spec: [communication-content contract](superpowers/specs/2026-07-03-communication-content-contract-design.md); ADRs `holomush-2hhq2` (verb category as discriminator), `holomush-byqph` (protovalidate gate + dual builders).

#### Uses

| Use          | Epic           | Role                                                              |
| ------------ | -------------- | ---------------------------------------------------------------- |
| **Scenes**   | `holomush-5rh` | Reference implementation — exercises every substrate layer       |
| **Channels** | `holomush-0sc` | Plugin rework on the plugin-ABAC substrate (`0sc.12`)            |
| **Forums**   | `holomush-djj` | Needs brainstorm + spec                                          |
| **Discord**  | `holomush-aqq` | In-game messaging bridge; OAuth account linking is `aqq.5`        |

Live status:

```bash
gh issue list -R holomush/holomush -l theme:social-spaces --state open --limit 200 --json number,title --jq '.[] | "#\(.number) \(.title)"'
```

#### Sequencing rationale

1. **Scenes first.** Scenes is the reference implementation; getting IC/OOC
   emission, pose order, the focus model, and the publish-vote machine right
   exercises every substrate layer end-to-end. Channels and Forums reuse those
   patterns, so doing scenes first reduces redesign risk. (The scenes web
   portal also forced **crypto production activation** — see Completed themes.)
2. **Channels in parallel where unblocked** (`0sc.12`). The rework rides the
   plugin-ABAC substrate; channel features that depend on scenes patterns
   (e.g., history-replay UX) can follow the scenes focus model.
3. **Forums brainstorm in parallel** (`djj`). No code dependency on scenes for
   the design phase — spec + plan can be written while scenes ships.
4. **Discord last** (`aqq`). Depends on the channels substrate AND an OAuth
   substrate (`aqq.5`); it will likely consume channels as the messaging bridge.

#### Risks

- **Forums has no design yet.** If Channels ships before Forums is designed, the
  channel API may not anticipate forum needs. Mitigation: run the Forums
  brainstorm in parallel even if execution waits.
- **Web-portal scope creep.** Every social surface wants a web view; unchecked
  this becomes a multi-month frontend project. Sequence web views *after* the
  backend surface stabilizes per use.
- **Per-connection routing needs client cooperation.** The `STREAM_OPENED`
  ControlFrame + `SendCommandRequest.connection_id` protocol only routes
  per-connection commands when the client echoes the `connection_id` back. The
  SvelteKit terminal does; future clients must too (documented in
  `site/docs/extending/binary-plugins.md`).

### `theme:plugin-capability-architecture` — Unified plugin capability & dependency model

Epic `holomush-eykuh` (shipped). A do-it-right redesign of how plugins declare,
discover, and consume capabilities and dependencies — triggered by a small bug
(`holomush-oeb4d`: a phantom `requires` that silently disabled DAG load-order
validation on every boot) that root-caused into a deeper conflation. Kept active
only for a P3 polish tail (query below); the architecture is in place. Move this
section to Completed when the tail drains.

**Why it mattered** (the problems it fixed, while there were still no users or
deployments to migrate): the manifest `requires` field was overloaded — it drove
both DAG load-order *and* Lua capability injection, so capability-backed
requires failed and the loader silently fell back to a priority sort on every
boot; capability delivery was asymmetric and not least-privilege (Lua got most
host functions unconditionally, while `PluginHostService` was a 25-RPC
god-service); and Lua had no `plugin → host → plugin` story at all.

**What it guarantees now** (the three mandates):

1. **Runtime parity** — binary and Lua plugins consume host capabilities *and*
   plugin services through one identical host-brokered mechanism (extends the
   `plugin-runtime-symmetry` invariant).
2. **Full dependency graph** — `plugin → host` (capabilities), `host → plugin`
   (event/command delivery), and `plugin → host → plugin` (a plugin depends on
   another plugin's service) are all expressible.
3. **Least-privilege** — capability-scoped contracts (the god-service split into
   14 `host.v1` services), declaration-gated access, plugin-as-ABAC-subject.

Pointers: foundation `holomush-oeb4d`; framing decision `holomush-eykuh.5`;
grounds against
`docs/superpowers/specs/2026-04-06-grpcbroker-service-injection-design.md` and
`docs/specs/2026-03-28-plugin-first-command-architecture-design.md`.

Remaining P3 tail:

```bash
gh issue list -R holomush/holomush -l theme:plugin-capability-architecture --state open --limit 200 --json number,title --jq '.[] | "#\(.number) \(.title)"'
```

### `theme:web-portals` — The web as a complete gaming surface

The web client is a **superset of telnet**, not a thin terminal. Governing
principle (decision `holomush-sz0h3`):

> A player MUST be able to play completely through the web; telnet — or a raw
> command line — is never *required*. On-grid real-time play (movement, room
> say/pose/look, exits, presence) is served first-class by the **web terminal**
> (`/terminal`) and/or telnet. Beyond that, a defined set of **subsystems** each
> get a dedicated rich web GUI ("control expression") above and beyond the
> terminal.

This is the **web-GUI lens**; `theme:social-spaces` is the **substrate lens** on
the same systems. A single web view — e.g. the scenes portal — carries both
labels.

**Why it exists.** The principle was implicit and got silently violated: web
create-scene was an explicit acceptance criterion that the portal redesign
dropped with no recorded decision, shipping a design that contradicted itself (a
"scenes-only player with no terminal" is first-class, yet could not originate a
scene). Writing the principle down makes it a checkable artifact, so future
surface designs stop re-narrowing scope by omission.

**Why a label, not an epic.** Web surfaces split across two organizing schemes —
Epic 8 (`qve`) owns the non-subsystem portals (wiki, characters, admin,
char-CRUD, offline); each subsystem epic owns its own web view (scenes `5rh`,
channels `0sc`, forums `djj`). No single epic owns "the web as a complete
surface," so the unifying instrument is a cross-cutting label.

**Not a registry invariant** (decision 2026-06-19): "web ⊇ telnet" is a
directional principle, not a test-pinnable guarantee (most surfaces are unbuilt —
missing features, not regressions). The testable unit is per-subsystem ("a
*shipped* web surface is self-sufficient — core ops never require telnet"); a
narrow invariant MAY be minted later, bound by a per-surface E2E, once the first
surface is genuinely telnet-free.

#### Surface map (surface → owning epic / beads)

| Surface                                                           | Home (epic / beads)     |
| ---------------------------------------------------------------- | ----------------------- |
| On-grid play (move, look, room say/pose, exits)                  | Epic 8 `qve` (terminal) |
| Scenes — browse/read/participate/watch/export/create/guest-gating | Epic 9 `5rh`            |
| Scenes — lifecycle/management (end/invite/vote/…)                | `5rh.24`                |
| DMs — 1:1 (page/whisper)                                          | `qve.17`                |
| DMs — 1:N (channels)                                             | Epic 10 `0sc`           |
| Forums                                                           | Epic 11 `djj` + `5rh.9` |
| Wiki / help / lore / setting docs                                | `qve.8`                 |
| Character profiles / sheets / directory                          | `qve.9`                 |
| Character create / edit / per-char settings                      | `qve.15`                |
| Character roster / claim / transfer (web)                        | `gloh`                  |
| Player settings / preferences                                    | `w7t5`                  |
| Administration                                                   | `qve.10`                |
| Offline / replay (strategic question pending)                    | `qve.7`                 |

Live status (which surfaces are shipped vs still open):

```bash
gh issue list -R holomush/holomush -l theme:web-portals --state open --limit 200 --json number,title --jq '.[] | "#\(.number) \(.title)"'
```

## Completed themes

### `theme:docs-platform` — Docs site migration, IA, and gRPC reference — closed 2026-05-29

A five-sub-project program to make the documentation site reflect reality and
serve both humans and LLMs. The substrate was the doc platform itself; the uses
were an accurate gRPC reference, a purpose-organized information architecture,
and machine-readable `llms.txt`.

| Sub-project | Scope | Outcome |
| ----------- | ----- | -------- |
| SP0 | Proto per-field doc comments + buf `COMMENTS` ratchet | ✅ #4303 — epic `holomush-300ad`; all 14 protos documented, buf `COMMENTS` unconditional + name-echo quality gate; 6 grounding-surfaced bugs filed (P1 `holomush-8cxo6` fail-open ABAC sentinel) |
| SP1 | Migrate zensical → Astro Starlight (bun) + llms.txt | ✅ epic `holomush-cwnu0`; ADRs `holomush-145ko`, `holomush-qf2oo`, `holomush-xneg2` |
| SP2 | Diátaxis IA redesign, `autogenerate` sidebar, orphan triage | ✅ #4296 + #4297 — epic `holomush-44nxc`; ADRs `holomush-md3k4`, `holomush-38kmt` |
| SP3 | `llms.txt` / `llms-full.txt` / `llms-small.txt` generation | folded into SP1 (`starlight-llms-txt` plugin) |
| SP4 | Complete gRPC service coverage (all 12 services, field-level) | ✅ `holomush-okm59` — `docs:proto` migrated to `buf generate` over the whole `api/proto` module (`buf.gen.docs.yaml`); coverage went 6 → 12 services structurally; coverage guard `test/meta/grpc_api_coverage_test.go` prevents enumeration drift |
| SP5 | Docs quality & cohesion (rubric+audit editorial pass) + topic-tab nav, page-actions, community | ✅ #4304 — epic `holomush-ivwij`; ADR `holomush-q924m`; deferred content tail tracked (`holomush-3skv5`, `fvxlv`, `x8v4i`, `ton17`, `e6kvc`) |

**Program anchor:** decision bead `holomush-rkwyb` carries the SP0–SP4 framing and
sequencing rationale. The live site had drifted — ~20 docs orphaned from a
hand-maintained nav, a gRPC reference covering only 6 of 12 services, and no
`llms.txt` path. SP1 swapped the platform as a strict lift-and-shift so SP2/SP4
could land cleanly on it; SP0 ran in parallel since its source of truth is the
`.proto` files. Sequencing rationale and the lift-and-shift discipline are in
`docs/superpowers/specs/2026-05-27-docs-starlight-migration-design.md`.

**Open tail (orthogonal follow-ups, not program deliverables):** the
`theme:docs-platform` label still tags a handful of P3 beads that outlive the
SP0–SP5 program and land opportunistically — a reproducibility chore
`holomush-6n1j3` (pin `protoc-gen-doc`), `holomush-e6kvc` (hostfunc-audit-table
drift), `holomush-k0r5o` (grpc-api orientation copy), `holomush-fvxlv` (events
index orientation), `holomush-x8v4i` (api-guide client-connection walkthrough),
and the feature-gated content beads in the SP5 row above. The program anchor
`holomush-rkwyb` and the two P1s the doc work surfaced (`holomush-8cxo6`
fail-open ABAC sentinel, `holomush-rkwyb.1` proto enum omission) are all
**closed** — the framing record is preserved in this Completed-themes section
and in the closed anchor's notes. Live set:
`gh issue list -R holomush/holomush -l theme:docs-platform --state open`.

### v0.1 Initial Release — closed 2026-05-16

Epic `holomush-a3a7` was the v0.1 milestone — minimal viable MUSH where
an operator can deploy, players can register, create characters, move,
talk, and interact. All 11 scope items shipped (command dispatcher
consolidation, `executeViaSwitch` removal, sessions → Postgres,
whisper/page, describe, aliases, registration E2E, landing page, admin
bootstrap, operator deployment guide). The substrate to cut a v0.1 tag
exists; whether to actually tag is a separate decision.

### Foundational substrate pivots (Q1-Q2 2026)

Five major architectural replacements landed between January and May:

1. **Event substrate** (PR #252): `Broadcaster` + `EventStore` + LISTEN/NOTIFY → NATS JetStream + PG audit projection
2. **Plugin architecture** (PR #192): `ServiceProxy` → proto-first; `type:core` → `type:lua` + `type:binary`; Extism/WASM → hashicorp/go-plugin + gopher-lua
3. **ABAC** (PRs #84-#88, #106-#107, #114): `StaticAccessControl` + `capability.Enforcer` → `AccessPolicyEngine`
4. **Session model** (PRs #123, #139, #177, #225, #271, #233): in-memory → `PostgresSessionStore` + JetStream replay + two-phase login + multi-tab isolation
5. **Crypto rollout** (Phases 1-5 + 7): KEK/DEK, AuthGuard, decrypt-on-fanout, rekey, admin UDS+TOTP, INV-50 downgrade fence. **Capability only** — production *activation* was a separate, later milestone (see "Crypto production activation" below): the decrypt-on-fanout machinery existed from Phase 3b but was never wired into the live Subscribe/Publish path until 2026-06-10.

These pivots are no longer "active themes" — they're done. They're listed
here as orientation context for new threads of work that need to know what
substrate they can rely on.

### Crypto production activation — completed 2026-06-10

The crypto *capabilities* in "Foundational substrate pivots" item 5 shipped across
Phases 1-5/7 but were never **activated in the production live path**. The
decrypt-on-fanout machinery existed from Phase 3b; the production Subscribe/Publish
flow still ran without it (`cryptoActive` never flipped true; sensitive events were
delivered metadata-only, or were undeliverable). Epic `holomush-5rh.8.29` closed that
gap — surfaced, not coincidentally, by strengthening the scenes-portal E2E
(`5rh.8.27` → `5rh.8.29.10`) to assert a live PoseCard, which proved that *no*
sensitive-event feature worked live in production.

Four defects, each found only by the strengthened E2E, fixed in sequence:

| PR    | Fix                                                                                       |
| ----- | ---------------------------------------------------------------------------------------- |
| #4411 | KEK-mandatory boot + single activation gate (`cryptoActive` derived from KEK presence)   |
| #4413 | scene DEK genesis-on-first-focus (`dek.Manager.EnsureParticipant`; INV-CRYPTO-121)        |
| #4415 | gateway stamps `scene_id` from the cleartext subject so live IC events reach the web log |
| #4416 | the live-PoseCard E2E, asserted + un-quarantined (green in CI)                            |

**Lesson (durable framing):** a shipped capability is not a live capability. The
"completed substrate pivots" above track capability *landing*; production
*activation* is its own milestone and can lag a capability by months — undetected
until an E2E exercises the real browser → gateway → live-fan-out path. Both
follow-ups have since closed: `holomush-tn12i` (server-side backfill time-window)
and `holomush-8lqco` (test-suite audit, merged #4414).

## Future themes (sketches — not yet labels)

These exist as concepts in the orientation today but don't have enough
multi-epic shape yet to warrant a `theme:` label.

### Hardening / audit-finding cohort

A cohort of beads carrying the `audit-finding` label (created during the
2026-05-16 cleanup). Mix of P1 real bugs, P2 quality items, and P3 polish.
Lands organically as developers pick from the cohort. Might become its
own theme if a "hardening sprint" becomes the strategy; today it's
opportunistic backfill.

Query: `gh issue list -R holomush/holomush -l audit-finding --state open`

### Web portals → now `theme:web-portals` (active, 2026-06-19)

This sketch graduated to an active theme — see **`theme:web-portals`** under
"Active themes" above, where the governing principle (web ⊇ telnet, decision
`holomush-sz0h3`), the surface map, and the per-surface beads live. `qve.7`'s
offline strategic question (IndexedDB cache vs JetStream server-side replay) and
the minor `holomush-jxwr` / `holomush-19uc` items remain tracked on their beads.

### Operator experience

Site docs at `site/docs/operating/` are substantial; future themes here
might cover observability dashboards, deployment patterns, sandbox
operations refinements. Not yet shaped as multi-epic work.

## Maintenance (not strategic themes, but worth surfacing)

Hygiene work that's tracked but isn't shaped as a multi-epic narrative.
Listed here so future sessions know it exists without grepping `docs/`.

### Repo audit 2026-05-13 (4 reports)

Four read-only audit reports live at `docs/repository-audit/2026-05-13/`:

| Report                       | Tracking epic   | Shape                                                                |
| ---------------------------- | --------------- | ------------------------------------------------------------------- |
| `architecture-audit.md`      | `holomush-dj95` | children materialized; in-flight                                    |
| `design-alignment-review.md` | `holomush-yvdm` | empty container; high-leverage findings re-filed as top-level beads |
| `humanization-review.md`     | `holomush-89o9` | empty container; high-leverage findings re-filed as top-level beads |
| `layer-review.md`            | `holomush-1bft` | empty container; high-leverage findings re-filed as top-level beads |

The reports' own framing (esp. humanization): **"rolling-cleanup territory,
not a mega-PR — treat as gardening: do a little, regularly."** A handful of
high-leverage findings have been re-filed as work items carrying the
`repo-audit` label plus `mechanical`/`design-needed` so they surface in
issue triage. The tracking epics remain as containers for the rest of the
findings if/when someone decides to drive the cleanup more aggressively.

Query the high-leverage cohort:

```bash
gh issue list -R holomush/holomush -l repo-audit --state open --limit 200 \
  --json number,title,labels \
  --jq '.[] | select([.labels[].name] | any(. == "mechanical" or . == "design-needed")) | "#\(.number) \(.title)"'
```

### Invariant registry binding-backfill (epic `holomush-hz0v4`)

The canonical invariant registry lives at `docs/architecture/invariants.yaml`
(machine source) → `docs/architecture/invariants.md` (generated by
`cmd/inv-render`), guarded by `test/meta/invariant_registry_test.go` (drift +
provenance). Cataloging is complete; **verification binding** is a ratchet: each
entry is `binding: pending` until a test carries a `// Verifies: INV-<SCOPE>-N`
annotation, at which point it flips to `bound` (per the `binding: pending`
tolerance decision `holomush-hz0v4.10`). The meta-test tolerates `pending`, so
this lands incrementally rather than as a mega-PR.

INV-PRIVACY is fully bound; the rest backfill incrementally per scope under the
still-open epic — `hz0v4.11` (CRYPTO), `hz0v4.16` (SCENE), `hz0v4.17` (PLUGIN),
`hz0v4.18` (EVENTBUS), `hz0v4.19` (long-tail). Pick-from-`bd`-ready gardening,
like the repo audit above — not a strategic theme. Live counts come from the
registry itself, not from prose:

```bash
rg -c 'binding: pending' docs/architecture/invariants.yaml   # remaining
rg -c 'binding: bound'   docs/architecture/invariants.yaml   # done
```

## Conventions

- **Theme label format**: `theme:<kebab-case-slug>`. Examples: `theme:social-spaces`, `theme:hardening`. No nesting (flat namespace).
- **Adding a theme**: when 2+ epics or a 5+ issue cluster share a strategic frame, capture an ADR in `docs/adr/` recording the framing and add the section to this doc.
- **Retiring a theme**: when the underlying work is done or the framing no longer fits, move the section to "Completed themes" with a brief retrospective and a date.
- **Altitude**: active sections stay at why + pointers + an issue query (see "How this works"); don't hand-maintain status. Completed retrospectives may keep frozen specifics.
- **GitHub Projects**: not used today. The break-even cost of double-entry (bd ↔ GH) exceeds the benefit of a visual board for a solo-developer workflow. Revisit if team grows or external roadmap visibility becomes a real need.
