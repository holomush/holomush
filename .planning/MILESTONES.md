# Milestones

## v0.13 Web Portal: Identity & Admin Foundations (Shipped: 2026-08-16)

**Phases completed:** 10 phases, 71 plans, 135 tasks

**Key accomplishments:**

- **Absence became a type-system property.** Three audiences, three proto messages, three projection functions, and a 29-row read-surface inventory that *is* the expected set Phase 4's census compares against by set equality — privacy enforced by a field not existing rather than by a filter remembering to run.
- **Public profiles for logged-out visitors.** A visitor standing nowhere reads a character's name and in-world description through `WebGetCharacterProfile` — the first route served outside `(authed)` — and a profile below its reachability floor is byte-identical on the wire to a character that does not exist.
- **Viewer-tier field masking, proven at the wire.** Twelve prose fields and eleven media rows reach the response through the term-A/term-B conjunction, with a below-floor field provably absent from the **marshaled bytes**, not merely absent from a struct.
- **Admin portal foundations.** Sixteen typed RPCs carrying `expected_version` on every mutation, and a seven-section admin registry gated per player with a mandatory authorization descriptor — the six unbuilt sections registered and denied *after* the gate, so the denial is a diagnostic for admins rather than an enumeration oracle for everyone else.
- **Background-job authorization.** A live job authorizes a world write under `job:<name>` and is provably confined to the aggregate its triggering event names — proven by a **passing permit** paired with a deny, through the production `world.Service` and a real engine over the whole shipped seed corpus.
- **Verification discipline as a deliverable.** Criterion 1 is bound by set equality plus a fail-closed audience partition over every exported facade method, whose public side is recomputed from the code — so the obvious one-line escape from the guard is itself a RED.

---

## v0.12 Foundation Hardening (Shipped: 2026-07-28)

**Phases completed:** 6 phases (4–9), 66 plans
**Delivered via:** PR #4814 (P4), #4816 (P5), #4819 (P6), #4825 (P7), #4832 (P8), #4874 (P9); closeout via #4877 (audit + Phase 09 verification) and #4879 (CI `Vuln` stand-in)
**Timeline:** 2026-07-11 → 2026-07-28 (L7 architecture review → shipped)
**Requirements:** 15/19 satisfied; QUAL-02/03/05 deliberately deferred with tracking; OPS-01 satisfied on direct code evidence (shipped outside the phase loop, so no phase VERIFICATION attests it)
**Closeout:** `override_closeout` — Phase 9 verified `gaps_found` (3/4 must-haves) by design; Phases 4–8 all verified `passed`. **Known verification overrides: 1** (see STATE.md "Deferred Items")
**Audit:** [milestones/v0.12-MILESTONE-AUDIT.md](milestones/v0.12-MILESTONE-AUDIT.md) — `gaps_found`; integration INTEGRATED across 13 seams, 0 broken seams, 2/2 E2E flows whole

**Key accomplishments:**

- **The world-state model was decided, not assumed** — ADR `holomush-i4784` resolved the event-sourcing-vs-CRUD divergence in favour of CRUD-canonical + optimistic concurrency + transactional outbox, at a blocking human checkpoint, grounded in a two-replica resilience harness that **empirically reproduced** M12 last-write-wins rather than arguing it (MODEL-01, OPS-05)
- **Last-write-wins and the dual-write window closed** — version-predicated CAS across all four world repos with a typed `WORLD_CONCURRENT_EDIT` signal; a transactional outbox whose intent is written in the same transaction as the state change, drained by a leased relay publishing in `feed_position` order with `Nats-Msg-Id` dedup. The post-commit emit path was deleted, not deprecated. INV-WORLD-1..4 bound (MODEL-03, MODEL-04)
- **Operational Highs closed and CI gates stood up** — `events_audit` partitioned with a retention worker, nats-server CVE remediated with a `Vuln` supply-chain gate (govulncheck + osv-scanner) now required on `main`, DLQ replay's `game_id` split bridged and its tautological test replaced (OPS-02/03/04)
- **The parallel Event models collapsed and bootstrap unified** — `core.Event` deleted outright, leaving `eventbus.Event` as the single representation; all 17 subsystems moved onto `lifecycle.Orchestrator`'s two-sweep Prepare/Activate with zero `Start` calls outside it; the gateway boundary is now enforced by a transitive-closure gate, INV-EVENTBUS-1 bound (ARCH-03/04/05)
- **Two god objects decomposed behind a regrowth ratchet** — `CoreServer` 1891→657 lines into four handlers, `plugin/manager` 1876→702 into three units, with a size ratchet **mutation-proven to fail** on all three halves rather than merely asserted (ARCH-01, ARCH-02)
- **A four-month-old measurement blind spot found and fixed** — the E2E coverage upload had been landing *empty* since ~March: `docker compose stop`'s 10s grace SIGKILLed the `-cover` binaries before Go flushed `GOCOVERDIR`, a bind-mount uid mismatch, and CI never forwarding env into the playwright container (which silently disabled five widened timeouts). Result: 9,790 covered statements, e2e flag 32.27%, project 79.11%. The pipeline had been fully wired and green throughout — a passing job proved nothing (QUAL-02)
- **Session lifecycle pinned by a real matrix** — a 48-row registry with a bijection meta-test and 42 integration specs covering connect / reconnect / multi-character / idle-timeout; the one genuinely uncoverable cell is declared in plain text rather than left silently green (QUAL-04)

**Known deferred items** (all adjudicated at phase gates, all issue-tracked): QUAL-02 coverage backfill — `cmd/holomush` 9.91 points under its floor (#4861) and both halves of the D-04 coverage gate deferred (#4875, #4876); QUAL-03 weak-test remediation residual (#4860); QUAL-05 DEK read-cache (#4792) and the de-slop half, deliberately not started. Carried forward from the audit: #4880 (`CLAUDE.md`'s event-construction rule would break outbox dedup if followed), #4881 (nothing reconciles required CI contexts against what CI can emit), #4882/#4883 (two unquarantined load-dependent flakes).

**The theme worth remembering:** Phase 9 catalogued ~17 instances of *"a verification that cannot fail"* — `go test -run` exits 0 when nothing matches; `test -s` passes on a metadata-only coverage profile; a green job proves it ran, not that it uploaded. The milestone audit found the same shape one level up: success criterion 1 promised a repo-wide coverage gate that **does not exist**, and the `Vuln` bug (#4878) was its exact inverse — a required check that could never *pass*, silently blocking every docs-only PR. Both are the same question left unasked: is this check's passing state reachable without the property holding?

**Archives:** [v0.12-ROADMAP.md](milestones/v0.12-ROADMAP.md) · [v0.12-REQUIREMENTS.md](milestones/v0.12-REQUIREMENTS.md) · [v0.12-MILESTONE-AUDIT.md](milestones/v0.12-MILESTONE-AUDIT.md) · phases in `milestones/v0.12-phases/`

---

## v0.11 Social Spaces & Platform Hardening (Shipped: 2026-07-11)

**Phases completed:** 3 phases, 26 plans, 28 tasks
**Delivered via:** PR #4595 (Phases 1+2 — 192 files, +32,076/−1,162) and PR #4782 (Phase 3 — 82 files, +9,696/−176)
**Timeline:** 2026-07-07 → 2026-07-11 (requirements → shipped)
**Requirements:** 12/12 satisfied (SCENEFWD-01 formally descoped to backlog 999.3)
**Closeout:** verified — all phases passed gsd-verifier; milestone audit passed (`milestones/v0.11-MILESTONE-AUDIT.md`)
**Release note:** milestone label matches cog's computed next tag v0.11.0; tags are cut exclusively by cog/release.yaml (GSD tagging disabled)

**Key accomplishments:**

- **Channels subsystem shipped** — `core-channels` binary plugin: persistent named channels with full lifecycle + moderation (create/join/leave/list/post/who/history, invite/mute/ban/kick/transfer), per-RPC self-enforced ABAC, `=name` shorthand alias, live LIVE_ONLY delivery, retention pruning; proves the social-spaces substrate's two-consumer pattern (INV-S7 N=2, CHAN-01..05)
- **Scene-identical substrate for channels** — channel content rides the shared EventBus on `events.<game>.channel.<id>` with plugin-qualified wire types; audits to plugin-owned `channel_log`; durable history membership-fenced at auth step-1 with `joined_at` floor + scrollback cap (CHAN-02/03)
- **Scene notifications end-to-end** — telnet nudge (45s debounce, `[>GAME]` gamenotice primitive), notify prefs + per-scene mute, SCENE_ACTIVITY badge suppression chokepoint, web BFF typed slice, idle sweep; telnet edge cases closed (SCENEFWD-02/03, INV-SCENE-70/71 bound)
- **External/clustered NATS mode** — `eventbus.mode: external` with fail-closed boot (no embedded fallback), single-principal account scoping via `deploy/nats` templates + `verify-scoping.sh` + boot self-check (CLUSTER-01/02); embedded stays the zero-config default
- **Multi-node crypto invalidation proven** — per-replica connections against a real NATS container, N-of-N acks, hung-replica probe-pill; INV-CLUSTER-1/2/4/9 bound, INV-CLUSTER-8 pending with coverage issue #4777 (CLUSTER-03)
- **Audit DLQ never-drop + replay CLI + operator runbook** — `EVENTS_AUDIT_DLQ` in-band capture, `holomush audit dlq {list,show,replay}`, full external-NATS lifecycle runbook; INV-EVENTBUS-29/30 bound (CLUSTER-04/05)

**Known deferred items** (all adjudicated at phase gates, tracked in GitHub issues): channels WR-01/WR-02 identity-binding + moderation-journal follow-ups (holomush-0sc.13/.14), plugin-audit DLQ gap #4776 (host-audit-only by design), INV-CLUSTER-8 #4777, CLUSTER follow-ups #4778–#4780, integration flake #4781.

**Archives:** [v0.11-ROADMAP.md](milestones/v0.11-ROADMAP.md) · [v0.11-REQUIREMENTS.md](milestones/v0.11-REQUIREMENTS.md) · [v0.11-MILESTONE-AUDIT.md](milestones/v0.11-MILESTONE-AUDIT.md) · phases in `milestones/v0.11-phases/`

---
