<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# HoloMUSH Roadmap

Strategic work clusters that span multiple epics. Complements `bd` (which
tracks individual work items) by explaining the **why** behind multi-epic
sequencing.

## How this works

- **Single source of truth: `bd`.** This file never duplicates bead state —
  query `bd list` for current status.
- **Themes are labels.** Each theme is a `theme:<slug>` label applied to
  relevant beads. Query (includes `in_progress`):
  `bd list -l theme:<slug> --limit 0 --json | jq -r '.[] | select(.status != "closed")'`.
  `bd list --status open` does NOT include `in_progress` beads, so use the
  json filter when you want active work surfaced.
- **The narrative lives here.** Strategic framing, sequencing rationale,
  and substrate-vs-use distinctions belong in this file; status / dates /
  dependencies are looked up from `bd`.
- **Themes are added** when a multi-epic sequence becomes clear (≥2 epics
  involved). Don't pre-design themes for hypothetical work.

## Active themes

### `theme:social-spaces` — Scenes, Channels, Forums, Discord

The largest in-flight thread. Four product surfaces sharing one substrate:
persistent groups with membership, history replay, presence routing, and
subscribed clients.

#### Substrate (shipped)

| Layer                        | Where                                                      | Why it matters                                                  |
| ---------------------------- | ---------------------------------------------------------- | --------------------------------------------------------------- |
| JetStream event bus          | `internal/eventbus/` (PR #252, 2026-04-21)                 | Durable per-stream delivery; replay; backpressure               |
| Focus coordinator            | `internal/grpc/focus/` (epic `oy6e`, drained 2026-05-16)   | Session subscription routing; multi-conn visibility             |
| AccessPolicyEngine (ABAC)    | `internal/access/policy/` (epic `ql5`, drained 2026-05-16) | Policy-driven visibility / membership / write gates             |
| Plugin focus client          | `pkg/plugin/focus_client.go`                               | SDK for plugins to call `JoinFocus`/`LeaveFocus`/`PresentFocus` |
| core-scenes reference plugin | `plugins/core-scenes/` (PRs #200, #202, #230, #267)        | First substrate consumer; reference implementation              |

Scenes work triggered the JetStream cutover. Now that the diversion has
shipped, scenes Phase 4+ can resume on the substrate it pulled into
existence.

#### Substrate-contract (shipped — `holomush-jg9b`)

The substrate pivots listed above were all in place by mid-May 2026, but the
formal contract binding uses to substrate had not been written. Epic `jg9b`
filled that gap.

**What shipped:**

- **INV-S5 (emit-type set-equality)** — startup-time validator that enforces
  plugin `crypto.emits` manifest declarations match code-registered emit types
  in both directions (declared-but-unregistered AND registered-but-undeclared
  both fail load). Shipped as a single coherent change in PR #4049 (`jg9b.3`):
  substrate cap, binary proto extension, Lua Load-pass, and adoption by
  `core-scenes`. Orientation page shipped via PR #4137 (`jg9b.4`).
- **Boundary invariants** (INV-S1 through INV-S10) — codified in the substrate-
  contract spec below. Key ones: substrate stays domain-free (INV-S2), Go+Lua
  runtime parity for every new host RPC (INV-S3), per-plugin Postgres schema
  isolation enforced by Postgres roles (INV-S6), ABAC engine stays out of the
  scene-log read path (INV-S9).

**Future primitives named but not yet built (INV-S7):**

`eventkit` (`pkg/plugin/eventkit/`) and `groupkit` (`pkg/plugin/groupkit/`) are
co-designed in the substrate-contract spec as SDK bundles for joint patterns
across uses. INV-S7 mandates N=2 consumer validation before either primitive
lands as substrate code — the second consumer (`0sc.12` channels rework) must
adopt cleanly before any extraction. Both are named here so future brainstorms
know they exist; neither has code yet.

**Unblocked by `jg9b.3`:**

- **Scenes Phase 4** (`5rh.13`) — IC/OOC event emission + pose order. Was
  blocked on INV-S5 enforcement landing (Phase 4 will add `crypto.emits:
  [scene_ic, scene_ooc]` to core-scenes, which is only safe with the
  fail-closed validator in place).
- **Channels rework** (`0sc.12`) — channel plugin rebuild on plugin ABAC
  substrate. Depends on the substrate contract being written so the channels
  design brainstorm binds to the correct invariants (esp. INV-S7 for groupkit
  adoption).

**Specs:**

- [Substrate-contract design](superpowers/specs/2026-05-16-social-spaces-substrate-contract.md) — boundary invariants, substrate inventory, INV-S1 – INV-S10
- [INV-S5 mechanism design](superpowers/specs/2026-05-17-inv-s5-mechanism-design.md) — runtime validator (Load-pass + proto extension)

#### Uses (in development, in priority order)

| Use          | Epic           | Frontier bead                                                | State                                    |
| ------------ | -------------- | ------------------------------------------------------------ | ---------------------------------------- |
| **Scenes**   | `holomush-5rh` | Phases 4-6 + 8 shipped — web Scenes Portal (`5rh.8`) + Phase 9 chat (`5rh.18`) next (`holomush-ztiqj`) | Phases 4-6 (#4279/#4302/#4308) + Phase 8 (`iokti`, #4353) shipped |
| **Channels** | `holomush-0sc` | `0sc.12` Channel plugin rework on plugin ABAC                | In progress                              |
| **Forums**   | `holomush-djj` | (undesigned)                                                 | Needs brainstorm + spec                  |
| **Discord**  | `holomush-aqq` | `aqq.5` Discord OAuth linking (`dwk.7` overlap closed today) | Blocked on Channels + OAuth substrate    |

The web Scenes Portal (`5rh.8`) drove the **crypto production activation** that
made live sensitive scene events (pose/say/ooc) actually render in the browser —
its E2E proved the crypto substrate was never activated in prod. That milestone is
documented under Completed themes → "Crypto production activation"; the portal is
functionally complete (`5rh.8` has two small follow-ups left: `5rh.8.30` invariant
binding, `5rh.8.29.11` comms-seed PlayerID asymmetry).

#### Sequencing rationale

1. **Scenes Phase 4-6 first** (`5rh.13/.14/.20`). Scenes is the reference
   implementation; getting IC emission + pose order + vote machine right
   exercises every substrate layer end-to-end. Channels and Forums will
   re-use the patterns. Doing scenes first reduces redesign risk later.
   **Phase 4 shipped** (`5rh.13`, PR #4153, 2026-05-21) — IC/OOC event
   emission + pose order + crypto.emits adoption. **Phase 5 shipped**
   (`5rh.14`, PR #4191, 2026-05-23) — per-connection focus model + multi-
   connection visibility + PluginHostService extension (3 new RPCs:
   `SetConnectionFocus`, `AutoFocusOnJoin`, `IsAnyConnFocused`). **Phase 6
   shipped** (`5rh.20`, 2026-05-29) — scene logs + publish vote + hard
   privacy boundary: feature PR #4279, bare-ULID scene identity fix #4302
   (`holomush-y5inx`, surfaced by the Phase 6 E2E work), E2E tier PR #4308.
   Scenes (Phases 4-6) is the shipped reference implementation. **Phase 8
   shipped** (epic `holomush-iokti`, PR #4353, 2026-05-31) — the scene board
   (browsable directory of open scenes + content-warning filtering) plus the
   owner-partitioned settings substrate it required; 21/21 children landed.
   (Phase 8 materialized as `iokti`; the original v2-plan placeholder `5rh.17`
   was closed-as-superseded on ship, the same pattern as `5rh.15`→`5rh.20` for
   Phase 6.) With the discoverability backend in place, the next slice of the
   **Phase 8 → Web Portal arc** (decision `holomush-ztiqj`) is the web Scenes
   Portal (`5rh.8`, folding/scoping the `5rh.18` Phase 9 chat view), which
   surfaces the board over ConnectRPC — its "browse active scenes" view needs
   the board as its data source, which is why the board shipped first. Phase 7
   templates (`5rh.16`) is orthogonal authoring work, deprioritized relative
   to discoverability.
2. **Channels in parallel where unblocked** (`0sc.12`). The channel plugin
   rework is already in-flight on the plugin ABAC substrate — keep that
   going. Channel-specific features that depend on scenes patterns (e.g.,
   history replay UX) can land after scenes Phase 5.
3. **Forums brainstorm in parallel** (`djj`). No code dependency on scenes
   for the design phase. Spec + plan can be written while scenes ships.
4. **Discord last** (`aqq`). Depends on channels substrate AND OAuth
   substrate (`aqq.5` is the OAuth subtask). Will likely consume the
   channel substrate as the in-game messaging bridge.

#### Cross-cutting work tracked under this theme

- Web client views per surface (`5rh.8` Scenes Portal, `5rh.18` Scenes
  Chat view, future channels-web and forums-web)
- Forum integration with Scenes (`5rh.9` scene requests / scheduling)
- Audit-finding re-files tagged here:
  - `holomush-ac50` (non-participant scene isolation E2E test — Phase 4
    test gap)
  - `holomush-cb4x` (scene log + export commands + renderers — Phase 6)
  - `holomush-72sj` (core-channels plugin creation)
  - `holomush-mjy3` (`object_examine` sensitivity — affects scene-room
    overlap rendering when emit code lands)

#### Risks

- **Phase 4 emission is the riskiest piece.** ~~`crypto.emits: []` becomes
  `[scene_ic, scene_ooc]` — must go through `crypto-reviewer` gate.~~
  **Resolved**: Phase 4 shipped (`5rh.13`, PR #4153) with crypto-reviewer
  READY; sensitivity classification correct. The `mjy3` follow-up remains
  open for `object_examine` overlap rendering.
- **Forums has no design yet.** If we let Channels ship before Forums is
  designed, the channel API may not anticipate forum needs. Mitigation:
  start the Forums brainstorm in parallel even if we don't execute it
  until later.
- **Web portal scope creep**: each social surface wants a web view; left
  unchecked this becomes a multi-month frontend project. Sequence web
  views *after* the backend surface stabilizes per use.
- **Phase 5 multi-tab routing depends on web-client cooperation**: the
  `STREAM_OPENED` ControlFrame + `SendCommandRequest.connection_id`
  protocol added in `5rh.14` only routes per-connection commands
  correctly when the client passes the connection_id back. The SvelteKit
  terminal does this; future clients must too. Documented in
  `site/docs/extending/binary-plugins.md`.

### `theme:plugin-capability-architecture` — Unified plugin capability & dependency model

Epic `holomush-eykuh`. A do-it-right redesign of how plugins declare,
discover, and consume capabilities and dependencies — triggered by a small
bug (`holomush-oeb4d`: a phantom `requires` that silently disabled DAG
load-order validation on every boot) that root-caused into a deeper
architectural conflation.

#### Why now

There are no users or deployments yet, so the cost of getting the plugin
trust-and-dependency substrate *right* is at its lowest it will ever be. The
current model has accreted three problems that compound:

- The manifest `requires` field is **overloaded** — it drives both DAG
  load-order resolution *and* Lua capability injection, and the resolver is
  blind to the capability registry, so capability-backed requires fail
  `UNSATISFIED_REQUIRES` and the loader silently falls back to a priority
  sort for the whole plugin set on every boot.
- Capability delivery is **asymmetric and not least-privilege**: Lua plugins
  get most host functions *unconditionally* (every Lua plugin can mutate the
  world), while binary plugins consume host capabilities as gRPC services via
  the grpcbroker. `PluginHostService` is a 25-RPC god-service, so
  service-granularity declarations can't express least privilege.
- There is **no `plugin → host → plugin` story for Lua**: binary plugins can
  call another plugin's provided service through the broker, but Lua plugins
  have no mechanism to consume a plugin-provided service at all.

#### The three mandates

1. **Runtime parity** — binary and Lua plugins consume host capabilities *and*
   plugin services through one identical host-brokered mechanism (extends the
   `plugin-runtime-symmetry` invariant).
2. **Full dependency graph** — the model covers `plugin → host` (capabilities),
   `host → plugin` (event/command delivery), and `plugin → host → plugin` (a
   plugin depends on another plugin's service).
3. **Least-privilege security** — capability-scoped contracts (decompose the
   `PluginHostService` god-service), declaration-gated access, plugin-as-ABAC
   subject.

#### Decomposition (foundation-first)

| Sub-spec | Bead | Scope |
| --- | --- | --- |
| 1 — Foundation | `holomush-oeb4d` | Capability/dependency model, manifest vocabulary, unified resolver, symmetry contract. Fixes the boot bug as a byproduct. |
| 2 — Host-service decomposition | `holomush-eykuh.1` | Split `PluginHostService` into capability-scoped proto contracts. |
| 3 — Lua parity layer | `holomush-eykuh.2` | Host-brokered consumption of capabilities + plugin services from Lua. |
| 4 — Least-privilege + plugin-trust security | `holomush-eykuh.3` | Declaration-gated access; ABAC with plugin subject. |
| 5 — Migration + `o262d` | `holomush-eykuh.4` | Atomic manifest cutover; fail-fast on unsatisfied deps. |

Framing decision recorded in `holomush-eykuh.5`. Grounds against the existing
`docs/superpowers/specs/2026-04-06-grpcbroker-service-injection-design.md` and
`2026-03-28-plugin-first-command-architecture-design.md`.

#### Risks / sequencing

- **Atomic cutover**: moving Lua from unconditional to declared capabilities
  breaks any plugin that uses an undeclared host function (`core-building`
  declares zero `requires` today but mutates the world). Every manifest must
  be audited and updated in lockstep with the enforcement flip.
- **God-service decomposition** is a long-lived refactor touching every plugin
  and both runtimes; partial-migration limbo is the main hazard. The foundation
  sub-spec sequences it so each capability contract lands behind a stable
  declaration model.

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
create-scene was an explicit Phase-9 acceptance criterion (`5rh.18`) that the
E9.5 portal redesign (`5rh.8`) dropped with no recorded decision, shipping a
design that even contradicts itself (a "scenes-only player with no terminal" is
first-class, yet cannot originate a scene). Writing the principle down makes it a
checkable artifact, so future surface designs stop re-narrowing scope by
omission.

**Why a label, not an epic.** Web surfaces are split across two organizing
schemes — Epic 8 (`qve`) owns the non-subsystem portals (wiki, characters, admin,
char-CRUD, offline); each subsystem epic owns its own web view (scenes `5rh`,
channels `0sc`, forums `djj`). No single epic owns "the web as a complete
surface," so the unifying instrument is a cross-cutting label.

#### Surface map

| Surface                                           | Web GUI state                   | Home (epic / beads)     |
| ------------------------------------------------- | ------------------------------- | ----------------------- |
| On-grid play (move, look, room say/pose, exits)   | shipped (web terminal)          | Epic 8 `qve`            |
| Scenes — browse/read/participate/watch/export     | shipped                         | E9.5 `5rh.8`            |
| Scenes — create                                   | gap                             | `5rh.22`                |
| Scenes — lifecycle/management (end/invite/vote/…) | gap                             | `5rh.24`                |
| Scenes — guest gating                             | gap                             | `5rh.23`                |
| DMs — 1:1 (page/whisper)                           | gap                             | `qve.17`                |
| DMs — 1:N (channels)                               | not started (backend in flight) | Epic 10 `0sc`           |
| Forums                                            | not started                     | Epic 11 `djj` + `5rh.9` |
| Wiki / help / lore / setting docs                 | not started                     | `qve.8`                 |
| Character profiles / sheets / directory           | not started                     | `qve.9`                 |
| Character create / edit / per-char settings       | picker shipped, CRUD not        | `qve.15`                |
| Character roster / claim / transfer (web)         | not started                     | `gloh` (mechanic)       |
| Player settings / preferences                     | partial (theme only)            | `w7t5`                  |
| Administration                                    | not started                     | `qve.10`                |
| Offline / replay                                  | strategic question pending      | `qve.7`                 |

Not a registry invariant (decision 2026-06-19): "web ⊇ telnet" is a directional
principle, not a test-pinnable guarantee (most surfaces are unbuilt — missing
features, not regressions). The testable unit is per-subsystem ("a *shipped* web
surface is self-sufficient — core ops never require telnet"); a narrow invariant
MAY be minted later, bound by a per-surface E2E, once the first surface is
genuinely telnet-free.

Query:

```bash
bd list -l theme:web-portals --limit 0 --json | jq -r '.[] | select(.status != "closed") | "\(.id) [P\(.priority)] \(.title)"'
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
and in the closed anchor's notes. Live set: `bd list -l theme:docs-platform
--limit 0 --json | jq -r '.[] | select(.status != "closed")'`.

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
until an E2E exercises the real browser → gateway → live-fan-out path. Follow-ups:
`holomush-tn12i` (server-side backfill time-window), `holomush-8lqco` (test-suite
audit, merged #4414).

## Future themes (sketches — not yet labels)

These exist as concepts in the orientation today but don't have enough
multi-epic shape yet to warrant a `theme:` label.

### Hardening / audit-finding cohort

~58 beads carrying the `audit-finding` label (created during the
2026-05-16 cleanup). Mix of P1 real bugs, P2 quality items, and P3 polish.
Lands organically as developers pick from the cohort. Might become its
own theme if a "hardening sprint" becomes the strategy; today it's
opportunistic backfill.

Query: `bd list -l audit-finding --limit 0 --json | jq -r '.[] | select(.status != "closed")'`

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

| Report                       | Tracking epic   | State                                                               |
| ---------------------------- | --------------- | ------------------------------------------------------------------- |
| `architecture-audit.md`      | `holomush-dj95` | 13 children materialized; in-flight                                 |
| `design-alignment-review.md` | `holomush-yvdm` | empty container; high-leverage findings re-filed as top-level beads |
| `humanization-review.md`     | `holomush-89o9` | empty container; high-leverage findings re-filed as top-level beads |
| `layer-review.md`            | `holomush-1bft` | empty container; high-leverage findings re-filed as top-level beads |

The reports' own framing (esp. humanization): **"rolling-cleanup territory,
not a mega-PR — treat as gardening: do a little, regularly."** A handful of
high-leverage findings have been re-filed as top-level beads carrying the
`repo-audit` label plus `mechanical`/`design-needed` so they surface in
`bd ready`. The tracking epics remain as containers for the rest of the
findings if/when someone decides to drive the cleanup more aggressively.

Query the high-leverage cohort:

```bash
bd list -l repo-audit --limit 0 --json | \
  jq -r '.[] | select(.status != "closed") |
         select((.labels // []) | any(. == "mechanical" or . == "design-needed")) |
         "P\(.priority) \(.id) \(.title)"' | sort
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

As of 2026-06-07: 31 of 301 entries `bound` (INV-PRIVACY fully bound;
INV-PLUGIN-40, added by the event-type wire-convention work, is now bound).
Remaining backfill is tracked per scope under the still-open epic — `hz0v4.11`
(CRYPTO), `hz0v4.16` (SCENE), `hz0v4.17` (PLUGIN), `hz0v4.18` (EVENTBUS),
`hz0v4.19` (long-tail); the pending counts in each child-bead title are
filing-time snapshots, so derive the live numbers from the queries below rather
than from prose. Pick-from-`bd`-ready gardening, like the repo audit above —
not a strategic theme. Remaining count:

```bash
rg -c 'binding: pending' docs/architecture/invariants.yaml
```

## Conventions

- **Theme label format**: `theme:<kebab-case-slug>`. Examples: `theme:social-spaces`, `theme:hardening`. No nesting (flat namespace).
- **Adding a theme**: when 2+ epics or a 5+ bead cluster share a strategic frame, file a `bd create -t decision` recording the framing and add the section to this doc.
- **Retiring a theme**: when the underlying work is done or the framing no longer fits, move the section to "Completed themes" with a brief retrospective and a date.
- **GitHub Projects**: not used today. The break-even cost of double-entry (bd ↔ GH) exceeds the benefit of a visual board for a solo-developer workflow. Revisit if team grows or external roadmap visibility becomes a real need.
