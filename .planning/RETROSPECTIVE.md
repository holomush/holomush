# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v0.11 — Social Spaces & Platform Hardening

**Shipped:** 2026-07-11
**Phases:** 3 | **Plans:** 26 | **Timeline:** 5 days (2026-07-07 requirements → 2026-07-11 shipped)

### What Was Built
- `core-channels` binary plugin — persistent named channels with full lifecycle + moderation, per-RPC self-enforced ABAC, `=name` alias, live delivery, retention pruning; second consumer of the social-spaces substrate (INV-S7 N=2)
- Scene notifications end-to-end (telnet nudge, prefs + per-scene mute, badge suppression chokepoint, web BFF, idle sweep) plus telnet edge-case closure
- External/clustered NATS deployment mode: fail-closed boot, single-principal account scoping, multi-node crypto invalidation (INV-CLUSTER-1/2/4/9 bound), audit DLQ never-drop + `holomush audit dlq` replay CLI, operator runbook

### What Worked
- **Pattern reuse across substrate consumers** — channels deliberately mirrored scenes (SceneService→ChannelService proto shape, per-RPC self-enforced ABAC per INV-SCENE-65, plugin-owned audit table) and Phase 1 moved fast because every design question had a scenes precedent to cite or consciously diverge from
- **The full gate chain earned its cost** — gsd-verifier (goal-backward), domain reviewers (crypto READY), /gsd-secure-phase (25 threats closed), deep code review with fix-all, and CodeRabbit /autofix each caught distinct real findings (e.g. WR-02 credential leak in NATS dial errors surfaced only at code review)
- **Verified closeout with explicit human adjudication** — every deferred item (WR-01/02, #4776–#4781) got an explicit accept/defer decision at its phase gate and a tracked issue, so milestone close needed zero re-litigation
- **D-06 testing-rule amendment held** — external-mode behavior proved against a real NATS container (`natstest`), embedded NATS for everything else; the boundary never leaked

### What Was Inefficient
- **Worktree isolation auto-degraded to sequential execution** on the milestone branch (issue #683) — Phase 3's 9 plans ran in 4 sequential waves instead of parallel
- **SUMMARY.md frontmatter drift** — phase 2 and most phase 3 summaries omitted `requirements-completed`, and several one-liners were deviation notes rather than accomplishments; the milestone audit and MILESTONES.md accomplishment extraction both had to fall back to manual curation
- **Phase 3's VALIDATION.md was left `status: draft`** despite the prescribed coverage being fully built — stale bookkeeping flagged by the milestone audit's Nyquist check
- **The milestone label said v1.0 while the product was at v0.10.0** — caught only at close; relabeled to v0.11 (PR #4783) and `git.create_tag` disabled to keep GSD out of cog's tag namespace

### Patterns Established
- Plugin services self-enforce ABAC per RPC (no command-layer authz); denied/hidden resources return uniform NotFound
- One shared pluginauthz fence for stream contributions at both session-establishment and mid-session paths
- Plugin-owned audit tables (`channel_log` joins `scene_log`); membership fenced at auth step-1 with `joined_at` floor
- Fail-closed boot self-checks for deployment-mode invariants (config Validate, account-scope verify)
- GSD milestone labels track cog-computed semver; GSD never mints v* tags

### Key Lessons
1. Align the GSD milestone label with the release engine's computed version **at milestone start**, not close — the archive naming and any tag-adjacent automation depend on it
2. Enforce `requirements-completed` frontmatter at plan-summary time (gsd-executor gate) so milestone audits don't need manual 3-source reconciliation
3. Close VALIDATION.md status flags in the same phase that satisfies them — a `draft` flag on a verified phase costs audit time later
4. When a substrate is designed for N consumers, building the second consumer (channels) is the cheapest possible validation of the abstraction — schedule it early

### Cost Observations
- Model profile: adaptive (heavy roles opus-tier, light roles cheap-tier)
- Execution: Phase 3 ran sequential due to #683; Phases 1–2 mixed
- Notable: the two big squash PRs (#4595, #4782) landed ~42k lines with zero post-merge reverts

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Phases | Plans | Key Change |
|-----------|--------|-------|------------|
| v0.11 | 3 | 26 | First GSD-native milestone; beads→GitHub Issues migration mid-flight; milestone labels aligned to cog semver at close |

### Top Lessons (Verified Across Milestones)

1. (single milestone so far — candidates above graduate here once re-verified in the next milestone)
