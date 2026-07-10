---
phase: 01-channels-subsystem
audited_by: gsd-security-auditor
audited_at: 2026-07-08
asvs_level: 1
block_on: high
threats_total: 25
threats_closed: 24
threats_open: 0            # blocking-open (severity >= high). 0 => phase may ship.
threats_open_nonblocking: 1  # medium, tracked below (T-01-20) → bead holomush-0sc.18
verdict: OPEN_THREATS (0 blocking / 1 non-blocking) — does NOT block ship
---

# Phase 1 — Channels Subsystem — Security Audit

Retroactive verification that every declared threat mitigation in the ten
`01-0N-PLAN.md` `<threat_model>` blocks is present in the implemented code.
ASVS L1 (grep-depth presence at the correct boundary). Implementation files were
read-only; no code was modified.

**Register source:** the STRIDE tables in `01-01…01-09` PLAN `<threat_model>`
blocks. No SUMMARY carried a `## Threat Flags` section (the executors recorded
mitigation status in prose / a `Threat mitigations` table in `01-09-SUMMARY.md`
instead), so there are **no unregistered flags**.

**ID note:** `T-01-13` was reused for two distinct threats across plans; audited
separately as `T-01-13a` (01-03, seed idempotency, low) and `T-01-13b` (01-04,
attribute sentinel fail-open, high).

## Closed threats

| Threat ID | Category | Severity | Disp. | Evidence (path:line) |
|-----------|----------|----------|-------|----------------------|
| T-01-01 | Info disclosure — non-member read / live-delivery | high | mitigate | Layer-2 read policy `plugins/core-channels/plugin.yaml:248-257`; QueryHistory auth step-1 `plugins/core-channels/audit.go:286-327,384-400`; service history delegate `service_rpcs.go:363` → `audit.go:426-430`; roster gate `service_rpcs.go:313-319`; member-only live set `plugins/core-channels/main.go:96-140` |
| T-01-02 | EoP — non-member post | high | mitigate | Layer-2 emit policy `plugin.yaml:263-266`; PostToChannel emit gate + not-muted `service_rpcs.go:244-261`; command chokepoint delegates (no direct emit) `commands.go:210-218` |
| T-01-04 | Spoofing — forged actor kind on emit | medium | mitigate | Shared emit fence rejects non-claimable kind `internal/plugin/event_emitter.go:125-135`; manifest declares `actor_kinds_claimable: [plugin, character]` `plugin.yaml:33` |
| T-01-05 | DoS — unbounded channel_log growth / page reads | medium | mitigate | Retention prune sweep `plugins/core-channels/prune.go:78-96`; page clamp to scrollback cap `audit.go:339,404-417`; `retention_days` column `migrations/000001_channels.up.sql:28` |
| T-01-06 | Info disclosure — message shape leaks content | low | mitigate | No `crypto.emits`, plaintext D-04, verbs plaintext `plugin.yaml:142-183`; identity by channel-id not payload name (D-08) — resolver exposes no content attrs `resolver.go:44-47` |
| T-01-07 | EoP — undeclared stream.subscription use | high | mitigate | Fail-closed capability interceptor `internal/plugin/hostcap/interceptor.go:38,233-238` (`CAPABILITY_NOT_DECLARED`→PermissionDenied); manifest declares `stream.subscription` `plugin.yaml:81-82` |
| T-01-08 | Info disclosure — AddSessionStream foreign session | medium | mitigate | Concrete-stream guard fires before registry `hostcap/servers.go:936-971`; live-add scoped to caller session `service.go:229-235,430`; membership gate precedes subscribe (JoinChannel actorMismatch+read) `service.go:409-419` |
| T-01-09 | DoS — history flood on mid-session subscribe | low | mitigate | LIVE_ONLY on mid-session add `service.go:233` (`ReplayModeLiveOnly`); SDK enum `pkg/plugin/stream_subscription_client.go:25-27` |
| T-01-10 | Tampering — channel name uniqueness | medium | mitigate | `UNIQUE lower(name)` index `migrations/000001_channels.up.sql:34`; regex `^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$` `types.go:114` applied at store `store.go:119,746` + service `service.go:335` |
| T-01-11 | Repudiation — membership/moderation ops | low | mitigate | Append-only `channel_ops_events` table `migrations/000001_channels.up.sql:69-84`; per-op journal write `store.go:888,913` |
| T-01-12 | Info disclosure — hidden-channel existence oracle | medium | mitigate | Resolver uniform NotFound `resolver.go:130-131`; per-RPC uniform NotFound `service.go:306`, `service_rpcs.go:318`; membership lookup collapses absent==non-member `audit.go:382-383,388-398` |
| T-01-13a | Tampering — racing default-channel seed | low | mitigate | `SeedDefaultChannels` `ON CONFLICT (lower(name)) DO NOTHING` `store.go:757` |
| T-01-13b | EoP — fail-open via empty-string attr sentinel | high | mitigate | Omit-not-sentinel + always-present `has_owner` witness `resolver.go:162-168` (owner key intentionally absent when unresolved) |
| T-01-14 | EoP — non-owner moderation | medium | mitigate | Owner-only + admin-override policies `plugin.yaml:292-307`; per-verb self-enforced ABAC `service_rpcs.go:38-43,144,173,202`; command layer adds no bypass (delegates) `commands.go` / `01-07-SUMMARY.md:115` |
| T-01-15 | DoS — channel-creation spam | medium | mitigate | Admin-gated create ABAC `service.go:346-354`; per-player fixed-window limiter with admin bypass `service.go:359-370,143-160` |
| T-01-16 | Spoofing — caller-identity / rate-limit key forgery | high | mitigate | Actor-binding cross-check `service.go:326-330`; limiter keys ONLY on host-vouched `trustedOwningPlayerFromContext`, fail-closed when absent `service.go:103-112,360-366`; sourced from dispatcher-stamped `req.PlayerID` `commands.go:96,105` (no proto player field) |
| T-01-17 | Tampering — duplicate audit rows on redelivery | low | mitigate | `INSERT … ON CONFLICT (id) DO NOTHING` `audit.go:139`; `id BYTEA PRIMARY KEY` `migrations/000002_create_channel_log.up.sql:18` |
| T-01-18 | Info disclosure — wildcard/malformed subject probing | medium | mitigate | `parseChannelSubject` rejects `*`/`>`/empty tokens → InvalidArgument `audit.go:444-461`; generic error text (no inner leak) `audit.go:317-319` |
| T-01-19 | Tampering — prune deletes in-window / unlimited history | medium | mitigate | Unlimited (admin NULL) skipped `prune.go:80-82,107-114`; cutoff = `now − window` with injectable clock `prune.go:78,84` |
| T-01-21 | EoP — plugin loads with undeclared capability | high | mitigate | Fail-closed load enforcement `hostcap/interceptor.go:233-238` exercised by whole-system census loading core-channels+core-scenes (INV-PLUGIN-54) `01-09-SUMMARY.md:159` |
| T-01-22 | EoP — cross-namespace subscribe via MID-SESSION stream.subscription | high | mitigate | `AuthorizeStreamSubscribe` → shared `AuthorizePluginStreamContribution` fence called before registry `hostcap/servers.go:936-948`; fence rejects pre-qualified/wildcard/forbidden(system,audit,crypto)/non-owned in-handler `pluginauthz/streamsubscribe.go:56-99,137-155` |
| T-01-23 | DoS — fence rejects plugin's OWN legit contribution | medium | mitigate | Fence permits relative own-domain `channel.<id>` (owned-emits check) `streamsubscribe.go:91-98`; plugin passes relative form on both paths `service.go:176`, `main.go:122,139` |
| T-01-24 | Info disclosure/EoP — filter injection via ESTABLISHMENT QuerySessionStreams | high | mitigate | Establishment merge runs the SAME shared fence per contributed ref, drops+logs foreign/forbidden `internal/plugin/manager.go:1584-1608` |
| T-01-SC | Tampering — package installs (supply chain) | n/a | accept | No package installs — all deps in-tree/vendored (pgx/ulid/oops via core-scenes); SC/crypto-reviewer checkpoints do not fire. Recorded as accepted risk. |

**Host does not seed any channel permit** (abac-reviewer claim independently
confirmed): `rg -i channel internal/access/policy/seed.go` → zero matches. All
channel policies ship in the plugin manifest `plugin.yaml:223-323`; host posture
is default-deny.

## Open threats

### Blocking (severity ≥ high)

None. `threats_open = 0` — the phase is not blocked from shipping.

### Non-blocking (severity below `high` threshold)

| Threat ID | Category | Severity | Gap | Files searched |
|-----------|----------|----------|-----|----------------|
| T-01-20 | Info disclosure — banned member still receiving live | medium | **One of two declared mitigation arms is absent.** Arm 1 (PRESENT): ban excludes the channel from `QuerySessionStreams`, so on the next session establishment/reconnect the banned member is not resubscribed — `main.go:84-85,130-138` (`ListForCharacter` excludes banned; `IsBannedFrom` filters defaults). Arm 2 (ABSENT): the plan promised "kick/ban triggers RemoveStream (01-07 moderation → leave path)", but `BanMember`/`KickMember` only mutate membership + emit a notice — they never call `RemoveStream`/`unsubscribeLive` for the target's active session (`service_rpcs.go:132-155,161-184`). `unsubscribeLive` is invoked only from self-`LeaveChannel` (`service.go:462`); a moderator does not hold the target's session id. **Residual exposure:** a currently-connected banned/kicked character continues receiving live `channel_say` until their session reconnects (bounded, not unbounded). | `plugins/core-channels/service_rpcs.go`, `service.go`, `main.go`; `01-08-SUMMARY.md:167`, `01-07-SUMMARY.md:155` |

**Recommendation (non-blocking):** file a bead to evict a banned/kicked target's
active session on moderation (host-side session→stream removal keyed on the
target character id), OR amend the T-01-20 mitigation text to state that
live-delivery removal is reconnect-bounded by design. Either resolves the
arm-2 gap; neither blocks ship at `block_on: high`.

## Previously-tracked accepted / deferred items (not re-flagged)

Carried from `01-REVIEW.md` / code-review; documented as accepted risk, each with
an open bead. None is a high-severity register threat and none blocks:

| Bead | Item | Disposition |
|------|------|-------------|
| holomush-0sc.13 | WR-01 target-character ABAC binding (latent, not live) | accepted — latent, no live exposure this phase |
| holomush-0sc.14 | WR-02 invite ops-actor attribution | accepted — deferred |
| holomush-0sc.15 | Pre-existing Lua ambient `add_session_stream` unfenced path | out of scope — NOT introduced by this phase (channels is binary + uses the fenced client); tracked against `holomush-l6std` |
| holomush-0sc.16 | Info-level polish | accepted — cosmetic |
| holomush-0sc.17 | Admin rate-limit availability-only | accepted — availability, not confidentiality/integrity |

## Accepted risks log

- **T-01-SC (supply chain, all plans):** phase installs no new packages; all
  dependencies are in-tree/vendored. Crypto-reviewer / supply-chain checkpoints
  do not fire. Accepted.
- **T-01-20 residual (medium, non-blocking):** banned/kicked members retain live
  channel delivery on their currently-open session until reconnect. Accepted for
  ship at `block_on: high`; recommended follow-up bead above.

## Verdict

All eight high-severity threats (T-01-01, T-01-02, T-01-07, T-01-13b, T-01-16,
T-01-21, T-01-22, T-01-24) are CLOSED with grounded `path:line` evidence.
`threats_open` (blocking) = **0**. One medium threat (T-01-20) is OPEN but
non-blocking. **Phase 1 may ship.**
