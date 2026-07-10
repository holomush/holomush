---
phase: 02
slug: scenes-lineage-completion
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on (high) severity (the blocking gate)
threats_open: 0
asvs_level: 1
created: 2026-07-10
---

# Phase 02 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Register authored at plan time (every 02-*-PLAN.md carries a `<threat_model>` block).
> Verified at ASVS L1 (grep-depth) — cross-confirmed by the adversarial `abac-reviewer`
> (verdict READY over the whole access-control surface), `gsd-verifier` (7/7 must-haves),
> and the green unit + integration suites (10164 unit / 10471 int).

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Telnet gateway ↔ core (SCENE_ACTIVITY / idle control frame) | Content-free badge/nudge downgrade; render consumes only `frame.Control.GetSceneId()`, no store/decrypt | Scene id only (no title, no RP content) — INV-SCENE-62/70 |
| Web browser ↔ BFF ↔ SceneAccessServer facade ↔ plugin | Structural mute/notify writes via typed RPC; identity resolved server-side from the session, `CharacterId` stamped from the verified character | Session token → host-vouched character id |
| Plugin SceneService ↔ core (ABAC dispatch) | `BeginServiceDispatch` stamps host-vouched actor metadata; RPC `character_id` cross-checked against it | Host-vouched actor (kind + id) |
| Core badge-downgrade ↔ non-focused member connections | `FocusMemberships ⊆ scene_participants` — a non-participant never carries the subject | Content-free `SCENE_ACTIVITY` frame (INV-SCENE-62) |
| plugin `scene_notify_prefs` (Postgres) | Every read/write keyed by `character_id`; BIGINT epoch-ns timestamps | Per-character mute/notify preferences |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-02-01 | Information disclosure | telnet SCENE_ACTIVITY render | high | mitigate | Render consumes only `GetSceneId()`; content-free `[>GAME: Scene #<id> …]`. INV-SCENE-70 bound (`gateway_handler_test.go:3098`); preserves INV-SCENE-62 | closed |
| T-02-02 | Information disclosure | `[>GAME: …]` gamenotice leader | medium | mitigate | `gamenotice.Activity/Idle/Invite` take bare scene id only (`gamenotice.go:20-32`); recipient is a member by construction of the badge guard (`server.go:1319-1344`) | closed |
| T-02-03 | Denial of service | telnet nudge spam under a busy scene | low | mitigate | Per-scene 45s debounce coalesces to ≤1 line/window (`gateway_handler.go` `sceneActivityLine`) | closed |
| T-02-04 | Tampering | notify-prefs row for another character | high | mitigate | Store methods key every read/write by `character_id`; caller identity enforced at the plugin ABAC/self-scope layer (T-02-07/09) | closed |
| T-02-05 | Information disclosure | mute state leaking cross-character | medium | mitigate | `ListMutedScenes` filters `WHERE character_id = $1` (behavior-tested; cross-character isolation) | closed |
| T-02-06 | Denial of service | unbounded prefs rows | low | accept | Bounded by (characters × muted-scenes); upsert prevents duplicate accumulation — see Accepted Risks | closed |
| T-02-07 | Elevation of privilege | mute/unmute on a scene the caller is not in | high | mitigate | `service.go:988` `Evaluate("mute","scene:"+id)` against `mute-scene-as-participant` DSL policy (`plugin.yaml:332`); fail-closed on nil/err/deny (participant-only via INV-SCENE-61) | closed |
| T-02-08 | Tampering | telnet command bypassing ABAC | high | mitigate | Command routes through `gated("mute",…)` (`commands.go:491`) — evaluates before any store write, fails closed on nil evaluator | closed |
| T-02-09 | Spoofing | forged character id on the RPC | medium | mitigate | Notify-pref trio gated by `callerNotVouchedAsCharacter` (fail-CLOSED, `service.go:957`, WR-02); MuteScene by `mismatchedActingCharacter` (advisory) + ABAC + host-trusted `character_id` | closed |
| T-02-09b | Information disclosure | ListCharacterScenes leaking another character's mute/notify state | medium | mitigate | Read-back scoped to `req.GetCharacterId()` (facade-verified / session-stamped); no cross-character query; fail-OPEN display only | closed |
| T-02-10 | Information disclosure | mute check re-fetching scene content into the frame | high | mitigate | Suppression path only DROPS the already-content-free frame (`server.go:1337-1362`); never adds content — INV-SCENE-62 unchanged | closed |
| T-02-11 | Tampering | cross-character mute leakage in the cache | medium | mitigate | `scene_mute_cache.go` keyed by character id; loader reads are character-scoped; cross-character isolation unit-tested | closed |
| T-02-11b | Spoofing | loader dialing the plugin without host-vouched identity | medium | mitigate | Loader dispatches via `BeginServiceDispatch` with `core.Actor{Kind:ActorCharacter,ID:characterID}` + ownerPlayerID (`sub_grpc.go:592`) — never a request-asserted id | closed |
| T-02-12 | Denial of service | per-event RPC in the hot delivery loop | medium | mitigate | Per-character 45s TTL cache; check only on the rare non-focused scene-event branch | closed |
| T-02-13 | Availability | checker error blocking delivery | low | mitigate | Fail-OPEN — nil/error checker delivers the (content-free) frame; mute/notify are preferences, not access control | closed |
| T-02-13b | Repudiation / correctness | persisted global notify pref never enforced | medium | mitigate | `ShouldSuppress` reads `GetSceneNotifyPref` and suppresses global-off BEFORE the per-scene mute check | closed |
| T-02-14 | Tampering | web mute bypassing ABAC via the command path | high | mitigate | Typed `WebMuteScene` RPC only (gateway-boundary forbids `sendCommand` for structural writes); enforcement is the same plugin `MuteScene` ABAC gate | closed |
| T-02-15 | Information disclosure | inner error text leaking to the browser | medium | mitigate | BFF returns opaque status errors; internals logged via `errutil.LogErrorContext`, not surfaced (grpc-errors.md) | closed |
| T-02-16 | Spoofing | forged session / arbitrary character on the mute write | high | mitigate | `SceneAccessServer.MuteScene/SetSceneNotifyPref` resolve identity server-side (resolveAndGate + ownedCharacter) and stamp `CharacterId` from the verified character (`sceneaccess_service.go:179/208/276`) | closed |
| T-02-17 | Information disclosure | idle nudge leaking scene content/existence to non-participants | high | mitigate | `scene_idle_nudge` reaches only members; focused → `gamenotice.Idle` (scene_id only, no DB); non-focused → content-free downgrade; non-participant never receives (INV-SCENE-62) | closed |
| T-02-18 | Tampering | illegal or repeated idle state transition | medium | mitigate | `IsValidTransition(active,paused)` gates each row (`idle_scheduler.go:87`); paused excluded from sweep — INV-SCENE-71 bound (`idle_scheduler_integration_test.go:122`) | closed |
| T-02-19 | Denial of service | a bad row aborting the whole idle sweep | low | mitigate | Per-row WARN-log-and-continue (publish_scheduler precedent) | closed |
| T-02-20 | Tampering | emit-registry set-equality break at load | high | mitigate | Idle-nudge emitter-only (no `crypto.emits`/registry change); INV-PLUGIN-32 set-equality holds | closed |
| T-02-21 | Elevation of privilege | reconnect restoring focus to a scene the character was kicked from | high | mitigate | `RestoreConnectionFocus` validates FocusMemberships → grid-fallback on revoked membership (`restore_connection_focus.go`, INV-SCENE-18) | closed |
| T-02-22 | Information disclosure | character B inheriting character A's scene focus on connection swap | high | mitigate | INV-SCENE-18 membership validation blocks the leak; defensive `conn.FocusKey = nil` on grid-fallback (`restore_connection_focus.go:62`); integration-tested | closed |
| T-02-23 | Tampering | web per-tab focus clobbered by an unconditional reconnect restore | medium | mitigate | Restore gated on `PresentingFocus != nil` (telnet-biased); web-tab safety integration-tested | closed |
| T-02-24 | Availability | mixed focused/skipped join failing silently | low | mitigate | Mixed-render branch replaces the silent default with an explicit line (`commands.go` D-07) | closed |
| T-02-SC | Tampering | supply chain — package installs | low | accept | No package-manager installs across all 7 plans (in-tree Go + SQL/proto only; bindings regenerated, not added) — see Accepted Risks | closed |

*Status: closed · open · open — below high threshold (non-blocking). All 25 register entries are closed; `threats_open: 0`.*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-02-01 | T-02-SC | No package-manager installs in any Phase-2 plan (in-tree Go + SQL/proto only; proto bindings regenerated, not added). No new supply-chain surface. | Phase-2 threat model | 2026-07-10 |
| AR-02-02 | T-02-06 | `scene_notify_prefs` row count is bounded by (characters × scenes-they-mute); the upsert prevents duplicate accumulation. No unbounded-growth DoS. | Phase-2 threat model | 2026-07-10 |
| AR-02-03 | T-02-13 | Mute-suppression READ path is intentionally fail-OPEN (deliver the content-free frame on checker error/nil) — mute/notify are display preferences, not access control, so failing open cannot leak (INV-SCENE-62 privacy is independent). Fail-closed follow-ups tracked in holomush-gl751 (closed) + holomush-e3448 (closed). | Phase-2 threat model | 2026-07-10 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-07-10 | 25 | 25 | 0 | Claude (gsd-secure-phase, ASVS L1 grep-depth) — cross-confirmed by abac-reviewer (READY), gsd-verifier (7/7), green unit+int suites |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-07-10
