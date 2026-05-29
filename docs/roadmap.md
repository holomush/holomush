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

### `theme:docs-platform` — Docs site migration, IA, and gRPC reference

A five-sub-project program to make the documentation site reflect reality and
serve both humans and LLMs. The substrate is the doc platform itself; the uses
are an accurate gRPC reference, a purpose-organized information architecture,
and machine-readable `llms.txt`.

| Sub-project | Scope | Tracking |
| ----------- | ----- | -------- |
| SP0 | Proto per-field doc comments + buf `COMMENTS` ratchet (platform-independent) | ✅ **landed** (#4303) — epic `holomush-300ad`; all 14 protos documented, buf `COMMENTS` unconditional + name-echo quality gate; 6 grounding-surfaced bugs filed (P1 `holomush-8cxo6` fail-open ABAC sentinel) |
| SP1 | **Migrate zensical → Astro Starlight (bun) + llms.txt** | ✅ **landed** — epic `holomush-cwnu0`; ADRs `holomush-145ko`, `holomush-qf2oo`, `holomush-xneg2` |
| SP2 | Diátaxis IA redesign, `autogenerate` sidebar, orphan triage + superseded retirement | ✅ **landed** (#4296 re-org + #4297 mode-purity) — epic `holomush-44nxc`; ADRs `holomush-md3k4`, `holomush-38kmt`; follow-up `holomush-e6kvc` |
| SP3 | `llms.txt` / `llms-full.txt` / `llms-small.txt` generation | **folded into SP1** (`starlight-llms-txt` plugin) |
| SP4 | Complete gRPC service coverage (all 13 services, field-level) | stub `holomush-okm59` (P4 backlog — not yet designed) |
| SP5 | Docs quality & cohesion — content/arrangement/communication (rubric+audit editorial pass) + topic-tab nav, page-actions, community | epic `holomush-ivwij` (15 tasks); ADR `holomush-q924m` |

**Program anchor:** decision bead `holomush-rkwyb` carries the SP0–SP4 framing and sequencing rationale; query the full program with `bd list -l theme:docs-platform`.

**Why now:** the live site had drifted — ~20 docs orphaned from a hand-maintained
nav, a gRPC reference covering only 5 of 13 services with empty field
descriptions, and no `llms.txt` path. SP1 swaps the platform (the prerequisite
substrate) as a strict lift-and-shift so SP2/SP4 can land cleanly on it; SP0 runs
in parallel since its source of truth is the `.proto` files. Sequencing rationale
and the lift-and-shift discipline are in
`docs/superpowers/specs/2026-05-27-docs-starlight-migration-design.md`.

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
| **Scenes**   | `holomush-5rh` | `5rh.15` Phase 6 (Logs + vote + hard privacy boundary)       | Active frontier — Phases 4 + 5 shipped   |
| **Channels** | `holomush-0sc` | `0sc.12` Channel plugin rework on plugin ABAC                | In progress                              |
| **Forums**   | `holomush-djj` | (undesigned)                                                 | Needs brainstorm + spec                  |
| **Discord**  | `holomush-aqq` | `aqq.5` Discord OAuth linking (`dwk.7` overlap closed today) | Blocked on Channels + OAuth substrate    |

#### Sequencing rationale

1. **Scenes Phase 4-6 first** (`5rh.13/.14/.15`). Scenes is the reference
   implementation; getting IC emission + pose order + vote machine right
   exercises every substrate layer end-to-end. Channels and Forums will
   re-use the patterns. Doing scenes first reduces redesign risk later.
   **Phase 4 shipped** (`5rh.13`, PR #4153, 2026-05-21) — IC/OOC event
   emission + pose order + crypto.emits adoption. **Phase 5 shipped**
   (`5rh.14`, PR #4191, 2026-05-23) — per-connection focus model + multi-
   connection visibility + PluginHostService extension (3 new RPCs:
   `SetConnectionFocus`, `AutoFocusOnJoin`, `IsAnyConnFocused`). Phase 6
   (`5rh.15`) is now the frontier.
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
  views _after_ the backend surface stabilizes per use.
- **Phase 5 multi-tab routing depends on web-client cooperation**: the
  `STREAM_OPENED` ControlFrame + `SendCommandRequest.connection_id`
  protocol added in `5rh.14` only routes per-connection commands
  correctly when the client passes the connection_id back. The SvelteKit
  terminal does this; future clients must too. Documented in
  `site/docs/extending/binary-plugins.md`.

## Completed themes

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
5. **Crypto rollout** (Phases 1-5 + 7): KEK/DEK, AuthGuard, decrypt-on-fanout, rekey, admin UDS+TOTP, INV-50 downgrade fence

These pivots are no longer "active themes" — they're done. They're listed
here as orientation context for new threads of work that need to know what
substrate they can rely on.

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

### Web portals (post-2026-05-16 audit reframe)

Original framing was "web client polish" — the 2026-05-16 `qve` audit
disproved that. Substrate is substantially complete (SvelteKit scaffold,
ConnectRPC transport, terminal UI, auth flows, theme system, JetStream
backfill, Playwright E2E). The actual remaining work is **unbuilt
portal features**, not polish:

- `qve.7` Offline support — **strategic question pending**: original
  IndexedDB-centric design may be superseded by JetStream server-side
  replay (`web/src/lib/backfill/streamBackfill.ts`). See bead note for
  decision tree (close as superseded / build IndexedDB / hybrid).
- `qve.8` Wiki portal — not started
- `qve.9` Character public profiles — not started
- `qve.10` Admin web portal — not started
- `qve.15` Character creation + management UI — picker shipped, CRUD not
- `holomush-jxwr` `replay_complete` marker UI — shares strategic
  question with qve.7
- `holomush-19uc` Playwright TTL expiration test (audit-finding from
  jogb split)

A `theme:web-portals` could emerge when 2+ of these surfaces start
landing concurrently OR when social-spaces web views (`5rh.8` scenes
portal, `5rh.18` chat view, future channels/forums web) need to land in
sequence with general portal infrastructure. Today: opportunistic
single-bead pickups.

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

## Conventions

- **Theme label format**: `theme:<kebab-case-slug>`. Examples: `theme:social-spaces`, `theme:hardening`. No nesting (flat namespace).
- **Adding a theme**: when 2+ epics or a 5+ bead cluster share a strategic frame, file a `bd create -t decision` recording the framing and add the section to this doc.
- **Retiring a theme**: when the underlying work is done or the framing no longer fits, move the section to "Completed themes" with a brief retrospective and a date.
- **GitHub Projects**: not used today. The break-even cost of double-entry (bd ↔ GH) exceeds the benefit of a visual board for a solo-developer workflow. Revisit if team grows or external roadmap visibility becomes a real need.
