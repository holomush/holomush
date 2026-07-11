# Milestones

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
