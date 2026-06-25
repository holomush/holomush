<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Scenes Web — Lifecycle & Management Actions Design

| | |
| --- | --- |
| **Bead** | `holomush-5rh.24` (Epic 9: Scenes & RP) |
| **Date** | 2026-06-24 |
| **Status** | READY — design-reviewer passed; `.24` promoted to epic. Lifecycle slice planned (`docs/superpowers/plans/2026-06-24-scenes-web-lifecycle-actions.md`) + materialized; Settings/Membership/Publish slices pending. |
| **Theme** | `theme:web-portals`, `theme:social-spaces` |
| **Supersedes** | The `.24` bead's original "writes ride the `HandleCommand` command path" approach (superseded by ADR `holomush-v4qmu`) |

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as described in RFC 2119 / RFC 8174 (see root `CLAUDE.md`).

---

## 1. Goal

Give the web client the scene **lifecycle and management** verbs that are
telnet-only today, so an owner or participant can *run* a scene end-to-end from
the browser — not merely participate. This realizes the web-portals principle
(`holomush-sz0h3`: web is a superset of telnet) for the management surface that
the shipped Scenes Portal (`holomush-5rh.8`) and create-scene slice
(`holomush-5rh.22`) deliberately left out.

**Acceptance (from the bead):** from the web GUI with no telnet, an owner can
end/pause/resume their scene, invite/kick/transfer participants, set scene
fields and pose-order mode, and run a publish vote; a participant can leave and
cast a publish vote. `task pr-prep` passes.

## 2. Background & current state (grounded)

The backend already implements every verb. `SceneService`
(`api/proto/holomush/scene/v1/scene.proto`) exposes `EndScene`, `PauseScene`,
`ResumeScene`, `UpdateScene`, `InviteToScene`, `KickFromScene`,
`TransferOwnership`, `LeaveScene`, `StartScenePublish`, `CastPublishVote`,
`WithdrawScenePublish`. The gap is the **web-facing surface** for each.

The create-scene slice (`holomush-5rh.22`, PR #4477) established and proved the
pattern this design reuses: a typed BFF stack
`web client → WebService → SceneAccessService facade → SceneService`. The web
client talks **only** to `WebService` (the gateway-boundary invariant —
`.claude/rules/gateway-boundary.md`); the `SceneAccessServer` facade
(`internal/grpc/sceneaccess_service.go`) owns server-side identity resolution
(INV-SCENE-63) and guest rejection (INV-SCENE-64).

### 2.1 The authorization gap (load-bearing finding)

The `SceneService` mutating RPC handlers for the lifecycle/membership verbs do
**not** self-enforce authorization. `EndScene`
(`plugins/core-scenes/service.go:639`) calls `s.store.End(sceneID)` directly and
never consults the caller's ownership. Today the owner-only gate lives **only**
in the telnet command-dispatch wrapper `gated()` (closure at
`plugins/core-scenes/commands.go:462-476`, applied per-verb in the dispatch
switch at `commands.go:483-500`), which evaluates the Cedar policy *before*
invoking `handleEnd` → `p.service.EndScene`. The command handler routes
through the RPC handler (`commands.go:613`), so the RPC handler **is** the common
chokepoint — but the gate sits *above* it.

The `SceneAccessService` facade calls `SceneService` RPCs **directly** (e.g.
`CreateScene` at `internal/grpc/sceneaccess_service.go:344`), bypassing the
command wrapper. Exposing the lifecycle/membership RPCs through the facade
**as-is** would let any non-guest player end / pause / kick / transfer **any**
scene — a privilege-escalation authorization bypass. Surfacing these verbs on the
web is therefore *blocked on* closing this gap.

**The fix is already demonstrated in-file.** `SceneServiceImpl` already holds the
evaluator (`service.go:179`), and `WatchScene` already self-gates on the RPC path
— `s.evaluator.Evaluate(ctx, "spectate", "scene:"+id)` (`service.go:1040`),
deriving the subject from the dispatch-actor context that `BeginServiceDispatch`
stamps. The lifecycle/membership handlers are simply *missing* the gate that a
sibling handler already proves.

### 2.2 Publish is different (INV-SCENE-33)

The publish verbs (`StartScenePublish`, `CastPublishVote`,
`WithdrawScenePublish`) **already self-protect** via direct participant/owner
checks in `publish_service.go` (start = participant + ended; vote = frozen-roster
participant; withdraw = owner). Critically, **INV-SCENE-33** mandates that *the
ABAC engine MUST NOT be called during participant-gated publication RPC
handlers*. Therefore this design **MUST NOT** add evaluator self-gating to the
publish handlers, and the facade's publish methods **MUST NOT** introduce an
engine call on that path. Publish needs facade + BFF + UI only — no
authorization change.

## 3. Scope

**In scope:** web affordances + the typed BFF stack for —

- **Lifecycle:** End, Pause, Resume, Update (title/description/tags/visibility/
  content-warnings/pose-order-mode).
- **Membership:** Invite, Kick, Transfer-ownership, Leave.
- **Publish loop:** Start, Cast-vote, Withdraw, plus live tally read.
- The **RPC-handler self-gate** for the eight lifecycle/membership verbs
  (§5), and the **`invite` policy relaxation** (§4.2).

**Out of scope:**

- Templates save/list/create-from → `holomush-5rh.16` (Phase 7).
- Forum integration → `holomush-5rh.9`.
- Multi-owner / co-ownership — the model is hard single-owner
  (`scenes.owner_id` + the partial unique index
  `idx_scene_participants_one_owner`, migration `000003`); explicitly **not**
  pursued.
- Cross-client real-time propagation of lifecycle/roster changes (a pre-existing
  gap; belongs to Phase 10 notifications `holomush-5rh.19`). Publish is the
  exception — its events already stream (§7).
- Collapsing the telnet command-wrapper gates onto handler-only gating (optional
  hardening follow-up; §5.3).

## 4. Authorization model

### 4.1 Per-verb matrix

Each verb maps to exactly one ABAC action on resource `scene:<id>` (the existing
Cedar policies in `plugins/core-scenes/plugin.yaml`):

| Verb | Action | Who (policy) | Enforcement on RPC path |
| --- | --- | --- | --- |
| End | `end` | owner | **NEW** handler self-gate (§5) |
| Pause | `pause` | owner | **NEW** handler self-gate |
| Resume | `resume` | participant *(already, D6)* | **NEW** handler self-gate |
| Update | `update` | owner | **NEW** handler self-gate |
| Invite | `invite` | **participant** *(relaxed, §4.2)* | **NEW** handler self-gate |
| Kick | `kick` | owner | **NEW** handler self-gate |
| Transfer | `transfer-ownership` | owner | **NEW** handler self-gate |
| Leave | `leave` | participant | **NEW** handler self-gate |
| Start publish | `publish` | participant + ended | existing internal check (INV-SCENE-33) |
| Cast vote | — | frozen-roster participant | existing internal check (INV-SCENE-33) |
| Withdraw | `withdraw_publish` | owner | existing internal check (INV-SCENE-33) |

The subject for every evaluation is the dispatch-actor character that
`BeginServiceDispatch` stamps on the context — never a request-body field. This
matches the `WatchScene` precedent and keeps the facade's verified
`ownedCharacter` (`sceneaccess_service.go:109`) as the identity source of truth.

### 4.2 `invite` policy relaxation

The `invite-to-scene` policy (`plugin.yaml:319-322`) is owner-only today. This
design relaxes it to any participant:

```text
# before: resource.scene.owner == principal.id && state in ["active","paused"]
# after:  principal.id in resource.scene.participants && resource.scene.state in ["active","paused"]
```

The `active`/`paused` state clause is **retained** (the `InviteParticipant` store
method does not independently enforce scene state — see the `plugin.yaml:255-258`
note). This is a Cedar-policy change and therefore **MUST** pass the
`abac-reviewer` gate. `kick`, `transfer`, and the lifecycle verbs remain
owner-only.

## 5. The self-gate (the pivotal decision)

**Decision:** the eight lifecycle/membership `SceneService` RPC handlers MUST
self-enforce ABAC at the handler — the one chokepoint both the telnet command
path and the web facade traverse — rather than relying on the telnet command
wrapper. This is captured as a new registry invariant:

> **INV-SCENE-65** (new, `binding: pending`): the `SceneService` mutating
> lifecycle/membership RPC handlers (`EndScene`, `PauseScene`, `ResumeScene`,
> `UpdateScene`, `InviteToScene`, `KickFromScene`, `TransferOwnership`,
> `LeaveScene`) MUST self-enforce ABAC by evaluating the per-verb action on
> `scene:<id>` with the subject taken from the dispatch-actor context, so
> authorization holds for **every** caller — telnet command path and the web
> `SceneAccessService` facade alike — not only when the command wrapper gates
> first. (Publish handlers are excluded by INV-SCENE-33.)

### 5.1 Pattern (mirrors `WatchScene`)

Each handler gains, as its first step, the equivalent of:

```go
dec, err := s.evaluator.Evaluate(ctx, "<action>", "scene:"+req.GetSceneId())
if err != nil { /* log; codes.Internal "internal error" */ }
if !dec.Allowed { return nil, status.Error(codes.PermissionDenied, "permission denied") }
```

Fail-closed when `s.evaluator == nil` (matches the command wrapper's nil-evaluator
behavior at `commands.go:464`). The resource ref derivation matches
`sceneResourceRef` (`commands.go:1532`).

### 5.2 Why not gate in the facade

Rejected: adding the evaluator to `SceneAccessServer` and gating before dispatch.
It would duplicate the action vocabulary host-side (drift risk vs. the plugin's
policy registrations, which are the single source of the action strings) and
leave the RPC handlers unguarded for any future direct caller — a weaker symmetry
story than gating at the common chokepoint (`.claude/rules/plugin-runtime-symmetry.md`:
"place the gate at the common path").

### 5.3 Double-gate disposition

The telnet command path will evaluate twice (wrapper + handler) with identical
(subject, action, resource) → identical result. This is harmless
(idempotent, default-deny preserved) defense-in-depth, and leaves the shipped
telnet denial UX untouched. Converging on handler-only gating (à la `WatchScene`,
which is unwrapped) is an **optional** follow-up, out of scope here.

### 5.4 Sequencing invariant

Each verb's handler self-gate **MUST** ship in the **same slice** as its
facade/web exposure. A `Web<Verb>` RPC **MUST NOT** be merged while the
`SceneService` handler it ultimately reaches is still ungated. Gate-with-exposure;
never expose-then-harden.

## 6. RPC surface (1:1 per verb)

Granularity decision: **one typed RPC per verb** (1:1 with `SceneService`), each
mapping to exactly one ABAC action — consistent with the
`WebCreateScene`/`WebWatchScene`/`WebExportScene` precedent and the cleanest
per-verb plan-time slicing. ~11 `WebService` RPCs + ~11 `SceneAccessService`
facade RPCs.

| Facade RPC (`sceneaccess.v1`) | BFF RPC (`web.v1`) | Forwards to |
| --- | --- | --- |
| `EndScene` | `WebEndScene` | `SceneService.EndScene` |
| `PauseScene` | `WebPauseScene` | `SceneService.PauseScene` |
| `ResumeScene` | `WebResumeScene` | `SceneService.ResumeScene` |
| `UpdateScene` | `WebUpdateScene` | `SceneService.UpdateScene` |
| `InviteToScene` | `WebInviteToScene` | `SceneService.InviteToScene` |
| `KickFromScene` | `WebKickFromScene` | `SceneService.KickFromScene` |
| `TransferOwnership` | `WebTransferOwnership` | `SceneService.TransferOwnership` |
| `LeaveScene` | `WebLeaveScene` | `SceneService.LeaveScene` |
| `StartScenePublish` | `WebStartScenePublish` | `SceneService.StartScenePublish` |
| `CastPublishVote` | `WebCastPublishVote` | `SceneService.CastPublishVote` |
| `WithdrawScenePublish` | `WebWithdrawScenePublish` | `SceneService.WithdrawScenePublish` |

Each facade method follows the `CreateScene` shape (`sceneaccess_service.go:329`):
`resolveAndGate` (guest deny) → `ownedCharacter` (verify owned character) →
`beginDispatch` (stamp actor) → forward → return the structured response. Every
new proto element **MUST** carry a Go-grounded doc comment
(`.claude/rules/proto-doc-comments.md`).

**Carry-forward landmine (`.22` lesson):** each new `web.SceneAccessClient`
method **MUST** also be added to `cmd/holomush`'s `GRPCClient` narrowing interface
(`cmd/holomush/deps.go`) **and** `mockGRPCClient` (`cmd/holomush/deps_test.go`),
or `cmd/holomush` fails to compile. Verify with `task build`, not just
package-scoped tests.

## 7. UI / UX

All affordances attach to the existing right-pane `SceneContextRail.svelte`
(no existing element moves). Approved via the visual companion (rail placement,
option A).

- **Scene section header:** owner-only `Pause` / `End` buttons + `⚙ Settings`;
  `Resume` is shown to **any participant** (its `resume` policy is
  participant-wide — D6, §4.1 — so a member can resume a scene the owner paused).
  Buttons are enabled per the forward-only FSM state (Resume only when paused;
  all hidden once ended).
- **Roster section:** per-member `⋯` kebab → popover with `Transfer` / `Kick`
  (owner-only; **not** always-visible); an `Invite` character field shown to
  **all participants** (§4.2); a `Leave` action for non-owner participants.
- **Settings sheet:** the `WebUpdateScene` form — title, description, tags,
  visibility, content-warnings, pose-order-mode.
- **Publish panel:** `Start publish vote` (participant; appears when state =
  ended and no active attempt); a live tally with per-voter state; the caller's
  Yes/No; owner `Withdraw`.

**Visibility predicates** are client-derivable and chosen to match each verb's
policy audience: owner = `owner_id === myCharacterId`; participant = membership in
the roster. The facade ABAC remains the authoritative fence — client-side
hide/show is UX only. No "capabilities" field is added to the read response
(YAGNI; revisit only if a verb's policy becomes operator-tunable).

## 8. Data flow & state refresh

Mutations are typed RPCs returning structured responses (e.g. the updated
`SceneInfo`). After its own mutation, the acting client **MUST** reflect the new
state — these changes do **not** stream today (they persist as
`scene_ops_events`, not on the IC subject). Two cases:

- **When the response already carries the needed projection** (lifecycle
  transitions — End/Pause/Resume — return the updated `SceneInfo` with the new
  `state`), the client updates its cache directly from the response; no separate
  read is needed.
- **When it does not** (e.g. roster-changing verbs whose response is narrower
  than the full roster), the client refetches `WebGetScene`. The existing
  `workspaceStore.select()` (`web/src/lib/scenes/workspaceStore.svelte.ts`)
  already issues a best-effort `getScene` (today for roster enrichment, bead
  `.8.25`); the mutation flow reuses that path.

**Publish is the exception:** `scene_publish_*` events
already stream on the IC subject (`main.go` phase-6 emit types), so the tally and
vote prompt update live for all roster members without a refetch.

## 9. Error handling

Per `.claude/rules/grpc-errors.md`: the facade returns generic
`status.Errorf(codes.Internal, "internal error")` for internal failures (no
inner-error leak; log via `errutil.LogErrorContext`), and passes safe codes
through — `PermissionDenied` (authorization), `FailedPrecondition` (FSM
violations such as resuming an active scene). The web maps gRPC codes to
user-facing toast text; FSM-precondition messages are surfaced verbatim where
safe (the store layer emits clear business errors, not generic authz denials —
`plugin.yaml:248-253`).

## 10. Decomposition (at plan-to-beads time)

One spec → four vertical sub-slices, each self-contained
(handler self-gate where needed + facade RPC(s) + Web RPC(s) + client wrapper +
UI + E2E):

1. **Lifecycle** — End, Pause, Resume. Adds the self-gate to those three
   handlers; Scene-header FSM buttons.
2. **Settings** — Update. Adds the self-gate to `UpdateScene`; the Settings sheet
   form (title/description/tags/visibility/content-warnings/pose-order-mode).
3. **Membership** — Invite (+ the `invite` policy relaxation), Kick, Transfer,
   Leave. Adds the self-gate to those four handlers; roster UI + kebab.
4. **Publish** — Start, Cast-vote, Withdraw + tally. No authz change
   (INV-SCENE-33); publish panel UI.

Slices are independent and may land in any order; within each slice the
gate-with-exposure rule (§5.4) holds.

## 11. Testing & review gates

- **Plugin handler (per verb):** ABAC **deny-path** test — a non-authorized
  subject calling the RPC **directly** receives `PermissionDenied` (this is what
  binds INV-SCENE-65). Allow-path for the authorized subject.
- **Facade:** guest-deny (INV-SCENE-64), `ownedCharacter` rejection, forward +
  response mapping.
- **BFF:** handler test + `mockGRPCClient`; `cmd/holomush` `deps` interface +
  mock updated (§6); `task build` green.
- **Web:** vitest unit tests for each affordance + visibility predicate.
- **E2E:** Playwright, telnet-free, via `registerAndEnterTerminal(page, prefix)`
  with a ≤4-char alphanumeric prefix (`.22` lesson).
- **Gates:** `abac-reviewer` (Cedar policy edit + the self-gate) and
  `code-reviewer` are **mandatory**. `crypto-reviewer` is **not** required (no
  `crypto.emits` / DEK / codec surface touched).

## 12. Invariants & references

**New:** INV-SCENE-65 (§5) — to be registered in
`docs/architecture/invariants.yaml` (`binding: pending`; origin = this spec),
bound by the per-verb deny-path tests in slice 1/2.

**Respected:**

- INV-SCENE-33 — no ABAC engine in publish handlers (§2.2).
- INV-SCENE-63 / INV-SCENE-64 — facade identity resolution + guest rejection.
- ADR `holomush-v4qmu` — typed BFF RPCs for structural writes (the architecture).
- ADR `holomush-b0365` / `.claude/rules/gateway-boundary.md` — web → BFF only.
- `.claude/rules/plugin-runtime-symmetry.md` — gate at the common chokepoint.
- `.claude/rules/grpc-errors.md` — no inner-error leak; one translation layer.

## 13. Risks

- **Telnet regression from the self-gate.** Mitigated by the additive double-gate
  (§5.3): handlers gain a gate, wrappers are untouched, so telnet behavior is
  unchanged (just redundantly evaluated).
- **Policy relaxation breadth.** Opening `invite` to participants is intentional;
  `abac-reviewer` confirms no over-grant (kick/transfer/lifecycle stay
  owner-only; the state clause is retained).
- **Proto surface size.** ~11 RPCs × full doc comments + tests is mechanical but
  large; the four-slice decomposition keeps each PR reviewable.
<!-- adr-capture: sha256=6235e9ef30ba7bd2; session=cli; ts=2026-06-25T12:18:37Z; adrs=holomush-f9z8f -->
