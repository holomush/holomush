# Phase 01: Portal SPEC — Pattern Map

**Mapped:** 2026-08-01
**Files analyzed:** 2 to create/modify (`01-SPEC.md` new; `CLAUDE.md` + `.claude/rules/*.md` pointer edit)
**Analogs found:** 3 / 3 (spec structure), 1 / 1 (amendments table), 1 / 1 (invariant declaration)

> **This phase's deliverable is a document, not code.** The analogs below are existing HoloMUSH
> design specs. Copy their heading skeleton and section idioms literally.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `.planning/phases/01-portal-spec/01-SPEC.md` (NEW) | design spec (privacy/audience/RPC-surface, milestone-scale) | document | `docs/superpowers/specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md` | exact — privacy boundary + audience-split RPCs + data model + invariants + divergence table |
| ↳ its amendments section | spec-amendments table | document | `docs/superpowers/specs/2026-05-09-event-payload-crypto-phase5-sub-epic-d-design.md` §10 | exact |
| ↳ its invariants section | canonical `INV-<SCOPE>-N` declaration | document | `docs/superpowers/specs/2026-07-03-communication-content-contract-design.md` §5 | exact (only recent spec declaring canonical-form ids) |
| `docs/architecture/invariants.yaml` (MODIFY) | registry entry (hand-registered per D-18) | YAML data | `invariants.yaml:4925-4935` (`INV-WORLD-1`, born-canonical) | exact |
| `CLAUDE.md` (MODIFY) | project instruction pointer | document | n/a — quoted verbatim below | n/a |
| `.claude/rules/invariants.md` (MODIFY) | rule frontmatter + prose naming spec paths | document | n/a — quoted verbatim below | n/a |

---

## Pattern Assignments

### 1. `01-SPEC.md` — primary structural analog

**Analog:** `docs/superpowers/specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md`

Chosen because it is the closest *subject* match in the tree: a hard privacy boundary, an
audience-split RPC surface (participant-gated pair vs. public pair), a state machine, ABAC
policies, an RFC2119 invariants section, and a divergence/amendment table — the same six
things `01-SPEC.md` must carry.

**Heading skeleton** (`2026-05-23-scenes-phase-6-logs-vote-privacy-design.md:9-991`, verbatim):

```markdown
# Scenes Phase 6: Logs, Publish Vote, and Hard Privacy Boundary   (:9)
## RFC2119 Keywords                                                (:20)
## 1. Overview                                                     (:24)
## 2. Divergences from the v2 Design and Bead Acceptance Criteria  (:37)
## 3. Domain Model                                                 (:55)
###   3.1 PublishedScene / 3.2 PublishedSceneVote / 3.3 Migration / 3.4 Configuration
## 4. State Machine                                                (:172)
###   4.1 Transitions / 4.2 Voting semantics / 4.3 Resolution check
## 5. gRPC RPC Surface                                             (:244)
###   5.1 Two-pair RPC architecture / 5.2 Error code surface
## 6. Commands and Focus Resolution                                (:293)
## 7. Event Emission                                               (:329)
## 8. ABAC Policies                                                (:346)
## 9. INV-S9 Enforcement Contract                                  (:372)
###   9.1 Gate placement / 9.2 Public surface separation / 9.4 Test contract (call-stack assertion)
## 10. Hard Privacy Boundary Block — Triple Signal                 (:490)
## 11. Snapshot Pipeline                                           (:518)
## 12. Renderers  ## 13. Observability                             (:662, :676)
## 14. Invariants (RFC2119)                                        (:714)
## 15. Test Plan                                                   (:727)
###   15.1 Tier 1: Unit / 15.2 Tier 2: plugin-local integration / 15.3 Tier 3: E2E / 15.4 Tier 4: Meta-tests
## 16. Phasing (Informative)  ## 17. Out of Scope                  (:959, :973)
## 18. Open Questions  ## 19. Related Work                         (:983, :991)
```

**Front matter block** (`:1-22`, verbatim — note the doubled SPDX comment is in the original;
emit only one):

```markdown
<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Scenes Phase 6: Logs, Publish Vote, and Hard Privacy Boundary

**Status:** Draft v1 (2026-05-23)
**Bead (impl):** `holomush-5rh.15`
**Folds in:** `holomush-cb4x` (scene log replay + export commands + renderers)
**Predecessors:** `holomush-5rh.14` Phase 5 (focus model), shipped 2026-05-21 (PR #4191)
**Parent design (v2):** [scenes-and-rp-design-v2.md](2026-04-06-scenes-and-rp-design-v2.md) §1.5, §5.5–§5.8
**Substrate contract:** [substrate-contract](2026-05-16-social-spaces-substrate-contract.md) — INV-S9 (privacy boundary is participant list, plugin-code enforced)

## RFC2119 Keywords

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are used per RFC2119.
```

> Beads are retired (bd tracker retired 2026-07-09). Substitute GSD/GitHub refs:
> `**Milestone:** v0.13` / `**Phase:** 1` / `**Requirements:** PORTAL-01..10` /
> `**Source research:** .planning/research/SUMMARY.md`.

**Alternate front-matter idiom** (bulleted, more recent —
`2026-07-03-communication-content-contract-design.md:6-15`, verbatim):

```markdown
# Communication Content Contract — Design

- **Status:** Accepted — `design-reviewer` READY; Slice 1 plan materialized (`holomush-kk1ot`)
- **Date:** 2026-07-03
- **Design bead:** `holomush-kk1ot`
- **Blocks:** `holomush-g1qcw` (focus-routed scene input — MUST land after this)
- **Theme:** `theme:social-spaces`

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as described in RFC 2119.
```

**Data-model section pattern — field table, not prose**
(`2026-05-23-...-design.md:57-79`, excerpt):

```markdown
### 3.1 PublishedScene

A `PublishedScene` represents one publication attempt for a scene. Multiple `PublishedScene` rows per scene are allowed up to `max_attempts`; at most one PUBLISHED row is allowed per scene (one-and-done).

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| ID | ULID | Yes | Primary identifier |
| SceneID | ULID | Yes | Logical reference to `scenes(id)` (enforced in code, not via DB FK) |
| Status | PublishedSceneStatus | Yes | One of: `COLLECTING`, `COOLOFF`, `PUBLISHED`, `ATTEMPT_FAILED` |
| ContentEntries | \[\]Entry | No | Set ONLY on PUBLISHED transition (nil otherwise). Each entry: `{speaker, kind, content}` where `kind ∈ {pose, say, emit}` |
| ParticipantsSnapshot | \[\]string | No | Set on PUBLISHED (nil otherwise); frozen list of character **names** visible to public. Names only — the public archive MUST NOT expose character IDs (matches proto `repeated string participants_snapshot`) |
```

Note the two idioms the planner should carry: the **enum vocabulary is spelled inline in the
Description cell** (fits D-05/D-06's `('active','retired','idle')` CHECK), and a
**privacy statement rides in the field's own row** ("MUST NOT expose character IDs") rather
than being deferred to the invariants section.

**RPC-surface section pattern — table + an explicit separation rationale**
(`:244-267`, verbatim):

```markdown
## 5. gRPC RPC Surface

Proto contract: `holomush.scene.v1.SceneService` (extended). New methods:

| RPC | Caller | Gate | Description |
| --- | ------ | ---- | ----------- |
| `GetPublishedScene(published_scene_id)` | participant | Plugin-code `IsParticipant` (INV-S9) | Returns full state including content if PUBLISHED |
| `GetPublicSceneArchive(published_scene_id)` | anyone | Code: status == PUBLISHED OR opaque NOT_FOUND | Public-safe view |
| `ExtendScenePublishVoteAttempts(scene_id, additional int)` | admin role | ABAC `extend_publish_attempts` (admin only) | Bumps `scenes.max_publish_attempts` |

### 5.1 Two-pair RPC architecture

The participant-gated pair (...) and the public pair (...) are deliberately separate handlers that do NOT share a code path beyond the underlying store reads. This separation is the structural guarantee that:

- The participant-gated path cannot accidentally become public via a future refactor.
- The public path cannot accidentally read private content because it only loads content_entries when status == PUBLISHED.

The proto contract reflects this: separate RPC names, separate request/response messages with different field sets (the public response omits per-voter information and the active vote-progress state).
```

**This §5.1 is the direct in-tree precedent for D-01/D-03** — separate messages per audience,
justified as a structural guarantee rather than a discipline. Reuse its shape verbatim for the
`PublicCharacter` / `OwnCharacter` / `AdminCharacter` split.

**Error-surface table + opacity discipline** (`:269-291`, excerpt):

```markdown
### 5.2 Error code surface

All new errors use the `oops.Code(...)` convention. Codes prefixed `SCENE_PUBLISH_`:

| Code | Wire status | When |
| ---- | ----------- | ---- |
| `SCENE_PUBLISH_NOT_A_VOTER` | PermissionDenied | CastVote when caller is not in `published_scene_votes` |
| `SCENE_PRIVACY_BOUNDARY_BLOCK` | PermissionDenied | INV-S9 gate denies participant-only read (also emits the triple-signal per §10) |

The public RPC pair (...) returns a single uniform `NOT_FOUND` for all of: nonexistent ID, COLLECTING, COOLOFF, ATTEMPT_FAILED. The error wire shape is identical to "this archive does not exist" — non-participants MUST NOT be able to infer that an attempt is in progress or has failed (INV-P6-8).

Error opacity discipline follows `.claude/rules/grpc-errors.md`. The wire-level `Status.message` is generic; structured `denial_reason` is an internal slog attribute only.
```

**ABAC-policy section pattern — fenced DSL + an explicit "notably absent" clause**
(`:346-370`, verbatim):

```markdown
## 8. ABAC Policies

Three new policies added to `plugins/core-scenes/plugin.yaml` under `policies`:

```yaml
- name: start-publish-as-participant
  dsl: >-
    permit(principal is character, action in ["publish"], resource is scene)
    when { principal.id in resource.scene.participants
           && resource.scene.state == "ended" };
```

Notably absent: there is no `read-publication-as-participant` ABAC policy. `GetPublishedScene` / ... are gated **in plugin code** via `IsParticipant`, not ABAC, per INV-S9. The reviewer for this spec MUST verify no future PR adds such a policy.
```

The **"Notably absent: … The reviewer for this spec MUST verify no future PR adds …"** sentence is
the idiom to reuse for the SPEC's explicit exclusions (role mutation is not part of character
administration; there is no owner-facing visibility toggle, D-09).

**Grounding-trace section** — a bare list of `path:line` citations the reviewer/planner can
re-verify (`2026-07-03-communication-content-contract-design.md:454-483`, excerpt):

```markdown
## Grounding trace (for `plan-reviewer`)

- `mcp__probe`/`codegraph`/`rg`: divergent payloads —
  `plugins/core-communication/main.lua:77-190` vs
  `plugins/core-scenes/commands.go:1305`.
- Existing formalized type-level layer:
  `internal/eventbus/rendering_publisher.go` (single enrichment site,
  `protovalidate` at emit), `RenderingMetadata`
  (`internal/eventbus/types.go:126`), `VerbRegistration`
  (`internal/core/registry.go:17`).
- Namespace collision check: `api/proto/holomush/content/v1` is `ContentService`
  (CMS), not communication.
```

**Citation format across all three analogs:** backticked `path:line` or `path:line-line`;
never a bare filename when a line is known; a parenthetical naming what is at that line.

---

### 2. The amendments-table analog

**Analog:** `docs/superpowers/specs/2026-05-09-event-payload-crypto-phase5-sub-epic-d-design.md:1094-1115`

**Verbatim** (rows abridged to three of six; all six read identically in shape):

```markdown
## Section 10 — Master-spec amendments

The following edits to `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`
land alongside D's first PR. A meta-test
(`TestSpecAmendmentsLandedSubEpicD`) substring-asserts each amendment.

| Section | Amendment |
|---|---|
| §5.9 interface | Replace the `Authenticate(ctx, prompt PromptFunc)` and `RequireDualControl(ctx, primary, prompt PromptFunc)` shape with the server-side shape: `Authenticate(ctx, AuthRequest) (OperatorIdentity, error)`. The CLI-side `PromptFunc` was architecturally aspirational — a callback cannot run server-side across the UDS boundary. Dual-control orchestration moves from a provider method to an operation-handler concern (server creates `admin_approvals` row, primary's CLI blocks, second-op CLI calls Approve). |
| §5.9 default impl | Reorder default-implementation steps so the role check (step 5 in this amended sequence) follows the `crypto.operator` capability check (step 4) and precedes the PeerCred capture. Master spec original §5.9 left the order ambiguous between role and capability; D's 6-step sequence is the canonical wiring. (Conjunction property — `RoleAdmin AND crypto.operator` per decomposition spec line 89/177 — is preserved unchanged.) |
| §10 (DENY codes) | Add: `DENY_NOT_ADMIN_ROLE`, `DENY_SESSION_INVALID`, `DENY_SESSION_EXPIRED`, `DENY_DUAL_CONTROL_SELF`, `DENY_APPROVAL_EXPIRED`, `DENY_APPROVAL_ALREADY_APPROVED`. |

D's master-spec amendments do not change any participant-facing crypto
invariant (INV-1 through INV-50+). They reshape the operator authentication
boundary and the chain-data storage shape to match what's architecturally
enforceable, while **preserving B's RoleAdmin AND crypto.operator
conjunction** (decomposition spec line 89/177; `internal/access/grants.go:14-18`
doc comment; B's `TestSpecAmendmentsLanded` substrings).
```

**What makes this a *good* amendments table** (distilled from
`.claude/rules/references/design-review-learnings.md`, § "Amendment rolls back a just-landed
sibling spec" and § "Master-spec amendment tables miss schema additions", and
`plan-review-learnings.md` § "Sub-epic D reflexes"):

| Property | Why it matters |
|---|---|
| Each row quotes the **superseded text verbatim** and states the replacement | The learnings' recurring failure is a row saying "REMOVE: `<thing>`" whose dropped string is still live in ≥1 sibling artifact. Quoting forces a grep. |
| Each row carries a **rationale**, not just a delta | A "REMOVE" with no threat-model justification is the documented failure mode ("tractability is not a valid reason to drop a security control"). |
| The closing paragraph **states what is NOT changed** | Bounds the blast radius explicitly. |
| A **named meta-test substring-asserts** each amendment | D uses `TestSpecAmendmentsLandedSubEpicD`; the in-tree precedent file is `internal/access/spec_amendments_test.go` (registered as `INV-ACCESS.shared_files`, `invariants.yaml:594-597`). |

**Caveat for this phase:** D's amendments target *one* spec. CONTEXT.md's four amendments target
*three different artifacts* (`ROADMAP.md` ×2, `REQUIREMENTS.md`, `.planning/research/SUMMARY.md`).
Add an **Artifact** column — CONTEXT.md already drafted the table in exactly that shape
(`01-CONTEXT.md:197-202`); adopt its `| Artifact | Amendment |` header and expand each cell to
carry the verbatim superseded text + rationale per the table above. There is **no** meta-test
precedent for asserting on `.planning/` markdown, and D-17 explicitly rejected building one —
so state the enforcement as `gsd-plan-checker` review, not a test.

**Second amendment-table idiom — the three-column divergence table**
(`2026-05-23-...-design.md:37-53`, verbatim header + two rows):

```markdown
## 2. Divergences from the v2 Design and Bead Acceptance Criteria

Phase 6 intentionally departs from several specifics in the v2 design and the `holomush-5rh.15` bead acceptance criteria. Each divergence is listed with its rationale; the design-reviewer is expected to evaluate the divergence as intentional, not a defect.

| Source text | Phase 6 spec | Rationale |
| ----------- | ------------ | --------- |
| v2 §5.6 "no mechanism to re-vote or change a vote after casting" | Votes are changeable until attempt resolution | Q4 (2026-05-23 brainstorm). Forgiving UX; resolution remains binary; voted_at + last_changed_at preserve auditability |
| Bead "Re-voting and admin overrides are structurally impossible" | Re-voting allowed within attempt. Admin overrides for attempt count allowed; NOT for vote outcome | Q4 + Q5. Privacy contract is "admin cannot bypass unanimous-yes"; admin can adjust retry budget |
```

**Recommended for this phase: use the three-column form** (`Artifact + source text` /
`v0.13 SPEC` / `Rationale`). It quotes the superseded text in its own column, which is exactly
the property the learnings say the two-column form loses. It is also the right home for D-14's
**recorded divergence** from the maintainer's stated grid-parity principle — the framing
sentence "listed with its rationale; the reviewer is expected to evaluate the divergence as
intentional, not a defect" is precisely what D-14 asks the SPEC to state.

---

### 3. Invariant-declaration analog

**Analog (in-spec declaration):** `docs/superpowers/specs/2026-07-03-communication-content-contract-design.md:328-348`, verbatim:

```markdown
## 5. Invariants

This design introduces system-level guarantees that MUST be registered in
`docs/architecture/invariants.yaml` when the plan is written (per
`.claude/rules/invariants.md`). A new `INV-COMM` scope/boundary MUST be added.

- **INV-COMM-1 (payload conformance):** every event whose verb declares
  `category: communication` carries a wire payload that validates as
  `holomush.comm.v1.CommunicationContent`. Bound by the §4.3 gate + a test that
  asserts a non-conforming communication payload is rejected with
  `EMIT_CONTENT_INVALID`. **Binding lands in Slice 2**, when the gate is wired
  live (the gate exists + is unit-tested in Slice 1, but is not on the live chain
  until the whole `category: communication` family conforms — §8).
- **INV-COMM-2 (builder runtime symmetry):** for the same inputs, the Go SDK and
  Lua builders produce JSON that decodes to an **equal** `CommunicationContent`
  proto (equivalently: identical canonical JSON under the `UseProtoNames`
  snake_case convention — NOT a raw byte compare, since wire payloads are JSON and
  are key-order/whitespace-sensitive). Bound by a Go↔Lua parity test (same
  discipline as `c5zol`'s Go↔TS golden).

Both ship `binding: pending` until their asserting tests exist.
```

Idioms to copy: `**INV-<SCOPE>-N (short label):** <guarantee>. Bound by <the specific test>.`;
an explicit statement of **which phase the binding lands in**; and a closing
`binding: pending` declaration. Note this spec **mints a new scope** — D-18 does not, so
the SPEC's opening sentence should instead read "…allocate into the existing `ACCESS`,
`PRIVACY` and `WORLD` scopes; no new boundary is declared."

Older-style, RFC2119-verb-per-bullet variant (`2026-05-23-...-design.md:714-725`, two rows):

```markdown
## 14. Invariants (RFC2119)

- **INV-P6-5 (MUST):** The IsParticipant gate at `GetPublishedScene`, `DownloadPublishedScene`, and `ListScenePublishAttempts` SHALL execute before any database query against `published_scenes.content_entries` or `published_scene_votes`. Verified by call-stack tripwire test.
- **INV-P6-8 (MUST):** `GetPublicSceneArchive` and `DownloadPublicSceneArchive` SHALL return opaque `NOT_FOUND` for any non-PUBLISHED publication. The error wire shape SHALL be identical for nonexistent, COLLECTING, COOLOFF, and ATTEMPT_FAILED states.
```

> `INV-P6-*` is a **pre-registry ad-hoc family** — D-18 and `.claude/rules/invariants.md` forbid
> minting one. Copy the *sentence shape* (`(MUST):` + SHALL clause + "Verified by <test>"),
> not the id form.

**Registry-entry analog (born-canonical, no legacy migration)** —
`docs/architecture/invariants.yaml:4925-4935`, verbatim:

```yaml
  - id: INV-WORLD-1
    scope: INV-WORLD
    origin_spec: "docs/adr/holomush-i4784-world-state-model-decision.md"
    legacy: ["INV-WORLD-ATOMIC-FEED@docs/adr/holomush-i4784-world-state-model-decision.md"]
    summary: "ATOMIC-FEED: a world state change and its one semantic outbox envelope commit or roll back ATOMICALLY in the
      SAME transaction — an envelope-less committed state change, or a committed envelope for a rolled-back state change,
      is impossible. Bound to the always-run state+envelope atomicity test (real world row + envelope: rollback→neither survives,
      commit→both survive, forced outbox failure after the state write→state rolls back)."
    binding: bound
    asserted_by:
      - "internal/world/postgres/outbox_store_test.go"
```

A `binding: pending` entry (what v0.13's Phase-1-registered invariants will be) —
`invariants.yaml:2173-2184`, verbatim:

```yaml
  - id: INV-ACCESS-7
    scope: INV-ACCESS
    origin_spec: "docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md"
    legacy: ["INV-15@docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md"]
    summary: "ABAC denies subscribe to events.*.system.* (and audit.>) streams for kind={plugin|character} at the gRPC subscribe
      boundary; the Rekey system audit event lands on a subject those principals cannot read."
    binding: pending
    refs:
      - {file: "internal/access/policy/seed.go", token: "INV-15"}
      - {file: "internal/access/policy/seed_test.go", token: "INV-15"}
```

**Entry schema** — `internal/invregistry/registry.go:31-41` (the full field set; anything else
fails the meta-test):

```go
	ID         string   `yaml:"id"`
	Scope      string   `yaml:"scope"`
	OriginSpec string   `yaml:"origin_spec"`
	Legacy     []string `yaml:"legacy"`
	Summary    string   `yaml:"summary"`
	Severity   string   `yaml:"severity"`
	Status     string   `yaml:"status"`
	AssertedBy []string `yaml:"asserted_by"`
	External   bool     `yaml:"external"`
	Binding    string   `yaml:"binding"`
	Refs       []Ref    `yaml:"refs"`
```

Constraints the planner must honor (from `.claude/rules/invariants.md`): a `binding: pending`
entry MUST NOT carry `asserted_by`; after editing, run `go run ./cmd/inv-render`
(`task invariants:render`) and commit the regenerated `docs/architecture/invariants.md`;
never hand-edit inside the `<!-- BEGIN GENERATED: … -->` regions.

**`origin_spec` for v0.13 entries is the open question the plan must settle.** Every existing
entry's `origin_spec` points into `docs/superpowers/specs/`, `docs/adr/`, or `docs/reviews/`.
D-16 puts the SPEC at `.planning/phases/01-portal-spec/01-SPEC.md`. Nothing in
`internal/invregistry/registry.go` constrains the string, but the planner should verify
`test/meta/invariant_registry_test.go`'s provenance guard tolerates a `.planning/` path before
committing to it.

#### Scope boundary statements — quoted verbatim for D-18 fit-checking

`docs/architecture/invariants.yaml:569-571` (**INV-ACCESS**):

```yaml
  - name: INV-ACCESS
    description: "ABAC policy evaluation, attribute provider invariants, seed policy shape, authorization decisions"
    boundary: "Access control evaluation. Does NOT include: stream-access gating at gRPC boundary (→ INV-EVENTBUS)."
```

`invariants.yaml:194-198` (**INV-PRIVACY**):

```yaml
  - name: INV-PRIVACY
    description: "Stream history temporal floors, scope gating, guest-session bounds, reattach/Idle arrival-timestamp semantics"
    boundary: "Privacy-relevant gating on history reads. Does NOT include: ABAC policy evaluation (→ INV-ACCESS), subscribe
      authorization (→ INV-EVENTBUS)."
```

`invariants.yaml:773-782` (**INV-WORLD**):

```yaml
  - name: INV-WORLD
    description: "World-model write integrity: atomic state+feed emission, delta-parity of the affected-aggregates manifest,
      ordered gap-free feed publication, and the raw-world-write boundary. Registered by 05-12 (Phase 5, MODEL-04)."
    boundary: "World-model correctness guarantees born from the MODEL-01 ADR (holomush-i4784, Option B: CRUD-canonical world
      state + optimistic-concurrency version guard + transactional outbox / ordered atomic feed). Does NOT include: crypto
      payload encryption (→ INV-CRYPTO), plugin-owned scene storage (plugin_core_scenes.scene_participants is out of the world
      fence, D-05), migration timestamp discipline (→ INV-STORE). status: pending because internal/world/ carries pre-existing
      FOREIGN bare INV-N tokens from other design specs ... that the provenance residual-walk would misattribute to this scope;
      the four INV-WORLD-N entries are nonetheless binding: bound (born canonical — no legacy-token migration is pending for THIS scope)."
    status: pending
```

**Fit notes for the planner (flag these, do not silently resolve):**

- **INV-PRIVACY's boundary is narrower than D-18 assumes.** It reads "Privacy-relevant gating on
  **history reads**". D-18 files per-field and profile-reachability guarantees here. Either the
  SPEC amends this `boundary:` string (an amendments-table row — the design-review learnings call
  an unstated boundary change exactly the failure mode) or the invariants land in `ACCESS`.
- **INV-ACCESS fits the tier-floor policy cleanly** ("seed policy shape, authorization decisions"),
  and `internal/access/policy/seed.go` is already `INV-ACCESS.shared_files`
  (`invariants.yaml:595`) — which is where D-11's tier-floor family lands.
- **INV-WORLD is `status: pending`** at the *scope* level, with a documented reason
  (foreign bare `INV-N` tokens in `internal/world/`). Its `owned_paths` are
  `internal/world/**` (`:783-786`) — the lifecycle state machine lives there, so D-18's WORLD
  allocation fits the boundary. New entries can still be `binding: bound` in a `pending` scope,
  per the INV-WORLD-1..4 precedent.
- **Next free N per scope:** verify at plan time with
  `rg -n 'id: INV-(ACCESS|PRIVACY|WORLD)-' docs/architecture/invariants.yaml | tail`.

---

### 4. The `CLAUDE.md` / rules pointer edit (D-19)

Three passages name spec paths and are in scope for a *pointer-only* edit. Quoted verbatim so
the planner can write an exact `Edit`.

**`CLAUDE.md:50-52` — Spec-Driven Development (the primary target):**

```markdown
### Spec-Driven Development

Work MUST NOT start without a spec/design/plan. Specs live in `docs/specs/` or `docs/superpowers/specs/`; plans in `docs/plans/` or `docs/superpowers/plans/` (the `docs/superpowers/` subdirs are AI-tooling and equally valid). All specs and plans MUST use RFC2119 keywords. When a spec introduces or changes a **system-level invariant**, capture it in the registry (`docs/architecture/invariants.yaml`), consulting existing entries first (`.claude/rules/invariants.md`) — do NOT mint ad-hoc invariant families.
```

**`CLAUDE.md:22` — Documentation Structure (same claim, second location; a pointer edit that
misses this one leaves the two contradicting):**

```markdown
`site/src/content/docs/` is the public Astro-Starlight website, by audience: `guide/` (players/designers), `operating/` (server operators), `extending/` (plugin devs), `contributing/` (codebase contributors), `reference/` (auto-generated API/event refs). Internal contributor docs: `.planning/ROADMAP.md` (GSD-owned strategic backlog + phases), `docs/plans/` + `docs/superpowers/plans/` (plans), `docs/specs/` + `docs/superpowers/specs/` (specs); the `docs/superpowers/` subdirs are AI-tooling-generated and equally valid.
```

**`.claude/rules/invariants.md` — four spec-path sites.** Frontmatter (`:13-15`):

```yaml
paths:
  - "docs/specs/**/*.md"
  - "docs/superpowers/specs/**/*.md"
```

Prose (`:60-62`, verbatim):

```markdown
orphan check fails CI if a spec **under `docs/superpowers/specs/`** references an
`INV-<migrated-scope>-N` that is not in the registry. Note the limit: the check
walks only `docs/superpowers/specs/` — invariants introduced in a `docs/specs/`
spec (or in code; see below) are NOT auto-caught and MUST be registered by hand.
```

And (`:87-88`):

```markdown
The orphan check walks only `docs/superpowers/specs/`; invariants introduced
in `docs/specs/` or code MUST be registered by hand.
```

These two prose passages describe a **live gate** (`test/meta/invariant_registry_test.go:341`) and
are **accurate as written** — D-18 relies on exactly this escape hatch. Do NOT edit them to claim
`.planning/` is walked. The correct minimal edit is: add `.planning/phases/**/*-SPEC.md` to the
frontmatter `paths:` (so this rule auto-loads on GSD specs) and append one sentence noting a
`.planning/` SPEC is likewise not auto-caught.

**Files naming a *specific historical* spec — leave alone (D-19 is a location-convention pointer,
not a link rewrite):**

| File:line | Text |
|---|---|
| `CLAUDE.md:16` | `**EventBus Design**: [docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md](...)` |
| `.claude/rules/event-conventions.md:17` | "The full design is in `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md`." |
| `.claude/rules/event-interfaces.md:53` | "`docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` — full design (§3 publish, …)" |
| `.claude/rules/branding.md:22` | "`docs/superpowers/specs/2026-05-28-holomush-software-brand-refresh-design.md`." |
| `.claude/rules/invariants.md:31` | "The full design is `docs/superpowers/specs/2026-05-31-invariant-registry-design.md`." |
| `.claude/rules/references/invariants-detail.md:27-29` | restates the orphan-check walk-root limit |

Each points at a file that still exists at that path; the retirement sweep that relocates them is
explicitly deferred (`01-CONTEXT.md:367-374`).

---

## Shared Patterns

### RFC2119 declaration
**Source:** `2026-05-23-...-design.md:20-22` (heading form) and `2026-07-03-...-design.md:14-15`
(inline form)
**Apply to:** the SPEC, immediately after front matter. Mandatory —
`CLAUDE.md:52` "All specs and plans MUST use RFC2119 keywords."

### `path:line` citation
**Source:** every analog; densest at `2026-07-03-...-design.md:454-483`
**Apply to:** every claim about existing code. Backticked, `path:line` or `path:line-line`, with a
parenthetical naming the symbol at that line.

### Test-plan-by-tier
**Source:** `2026-05-23-...-design.md:727-957` (`15.1 Tier 1: Unit` → `15.4 Tier 4: Meta-tests`),
whose framing sentence is: *"every invariant in §14 SHALL have at least one test asserting it by ID."*
**Apply to:** PORTAL-10's Verification Integrity section (D-17). Tier names match
`.claude/rules/testing.md`'s tier table — reuse those exact names.

### "Out of Scope" as a numbered section, not a footnote
**Source:** `2026-05-23-...-design.md:973` (`## 17. Out of Scope`);
`2026-05-09-...-sub-epic-d-design.md:1117` (`## Section 11 — Out of scope (recap)`, which
re-lists the front-matter out-of-scope items)
**Apply to:** the SPEC's explicit exclusions. SUMMARY.md:325 is emphatic —
*"role mutation is not part of character administration in this milestone. **An omission is not
an exclusion.**"*

### SPDX header
**Source:** `2026-05-23-...-design.md:1-4`
**Apply to:** the SPEC. Note `task fmt` runs `license-eye` over `api/ cmd/ internal/ pkg/ plugins/
scripts/` only — `.planning/` is not scanned, so the header is convention, not enforced. Existing
`.planning/` artifacts (CONTEXT.md, SUMMARY.md) carry **no** SPDX header; match the local
`.planning/` convention (no header) rather than the `docs/superpowers/specs/` one.

---

## No Analog Found

| Item | Reason |
|------|--------|
| A prior SPEC living under `.planning/phases/` | v0.13 Phase 1 is the first. Every one of the 140 specs is under `docs/superpowers/specs/`. Section structure transfers; the *location* is net-new, which is precisely what D-19's pointer edit records. |
| A meta-test asserting on planning-document markdown | None exists; D-17 rejected building one. `internal/access/spec_amendments_test.go` is the nearest relative but asserts substrings of a `docs/superpowers/specs/` file. |
| An `origin_spec:` in `invariants.yaml` pointing outside `docs/`/`docs/adr/`/`docs/reviews/` | None. Flagged above as a plan-time verification against `test/meta/invariant_registry_test.go`'s provenance guard. |

---

## Metadata

**Analog search scope:** `docs/superpowers/specs/` (140 files, surveyed by recency + size),
`docs/architecture/invariants.yaml`, `internal/invregistry/`, `CLAUDE.md`, `.claude/rules/**`
**Pattern extraction date:** 2026-08-01
