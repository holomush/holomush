<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Scene Character Name Resolution — Design

- **Status:** Draft (host-side design; supersedes two ruled-out approaches — see §10)
- **Date:** 2026-06-20
- **Design bead:** holomush-vdy2z
- **Originating bug:** holomush-5rh.25 (three web scene surfaces render character ULIDs instead of display names)
- **Theme:** `theme:social-spaces`

RFC2119 keywords (MUST, MUST NOT, SHOULD, SHOULD NOT, MAY) are used per the root `CLAUDE.md` table.

## 1. Problem & Root Cause

Three **web** scene surfaces render a character's raw ULID where a display name belongs:

1. **Roster** — `web/src/lib/components/scenes/SceneContextRail.svelte:89` (`{p.name}`)
2. **Pose-order strip** — `web/src/lib/components/scenes/PoseOrderStrip.svelte` (`scene.participants[].name`)
3. **Pose author** — `web/src/lib/components/scenes/PoseCard.svelte:37` (`entry.actorName || entry.actorId`)

The client renders `.name` / `.actorName` correctly; the defect is upstream, in two distinct mechanisms:

- **#1 + #2 share a source.** Both read `getScene` → `scene.participants[].characterName` (web `PoseOrderStrip` is a placeholder reusing roster data, not the dedicated `GetPoseOrder` RPC). The `core-scenes` plugin's `GetScene` deliberately stubs `ParticipantInfo.CharacterName = id` (`plugins/core-scenes/service.go:484-500`, *"no name resolver is wired in the plugin, fall back to the ID"*). `holomush-5rh.8.25` populated the roster but never resolved names. The client maps this at `web/src/lib/scenes/workspaceStore.svelte.ts:189` (`name = p.characterName`). The reporter's "name on one load, ULID on another" is the race between the placeholder (`asCharacterName` = real name, shown pre-enrichment) and the enriched roster (ULID-as-name, shown after).
- **#3 is a distinct path.** Scene IC `scene_pose`/`scene_say` events carry only `{actor_id, scene_id, text}` (`plugins/core-scenes/commands.go:1310-1314`). The gateway's `translateEvent` fills `GameEvent.actor` from the payload's `character_name`/`sender_name` (`internal/web/translate.go:81-85`); absent → empty → client falls back to `actorId` (ULID). The proto contract says `GameEvent.actor` is the **display name** (`api/proto/holomush/web/v1/web.proto:372`).

## 2. Goals & Non-Goals

### Goals

- **G1** — Scene rosters (`GetScene` participants + observers) MUST surface display names instead of ULIDs on the web (`SceneContextRail`, `PoseOrderStrip`).
- **G2** — The scene IC log MUST surface the pose author's display name (`PoseCard`).
- **G3** — Resolution is **best-effort**: if a name is unavailable, fall back to the ULID; never fail a read or drop an event.

### Non-Goals

- **NG1** — No client-side changes. Components + `workspaceStore.svelte.ts:189` already render `.name`/`.actor` correctly.
- **NG2** — **Telnet roster / pose-order are out of scope** (the bug declares so). They render from the plugin directly, not the facade; fixing them would require a plugin-side path gated by a new ABAC scene-membership policy (see §10) — a separate follow-up if ever wanted.
- **NG3** — No new ABAC attributes or policies; no change to scene visibility/privacy gates.
- **NG4** — No caching layer. Names resolve fresh per call (rosters are small; renames apply immediately).
- **NG5** — No change to the dedicated `GetPoseOrder` RPC's web wiring (still a follow-up bead); this design only ensures the facade-surfaced roster carries names.

## 3. Architecture

Resolution happens **host-side**, mirroring the established `ListFocusPresence` pattern — authorize the collection once, then resolve names for the authorized set via a non-ABAC resolver. Two independent, minimal changes:

```text
#1 + #2 (roster / pose-order):
  WebGetScene (BFF, no logic) ─► GetSceneForViewer (facade, internal/grpc)
      ├─ plugin GetScene → roster IDs (scene privacy gate already applied)
      └─ characterNameResolver.Names(ids) ──► world.CharacterRepository.GetNamesByIDs
         (overwrite ParticipantInfo.CharacterName; ULID fallback)

#3 (pose author):
  scene pose → host dispatcher (CommandRequest.CharacterName = exec.CharacterName())
      └─ plugin handleEmit → add "character_name": req.CharacterName to IC payload
         → gateway translateEvent already reads it → GameEvent.actor
```

### 3.1 #1 + #2 — resolve the roster in the facade

`GetSceneForViewer` (`internal/grpc/sceneaccess_service.go:180`) already authorizes the caller: `resolveAndGate` + `ownedCharacter`, and the plugin's `GetScene` privacy gate (`plugins/core-scenes/service.go:439-468`) returns `NotFound` to non-members of non-open scenes (open-scene rosters are public board metadata). So by the time the facade holds the roster, the caller is authorized to see it. The facade then resolves names for that authorized set — **not** per-character, ABAC-co-location-gated reads.

This mirrors `ListFocusPresence` (`internal/grpc/list_focus_presence.go`): one collection-level ABAC gate (lines 116-137), then `characterNameResolver.Names(uniqueIDs)` for the whole set (line 159) — a direct `GetNamesByIDs` repo batch with **no per-character ABAC**. One deliberate divergence on the miss path: presence *skips* unresolved entries (lines 166-180), but the roster **keeps the ULID** (the slot still exists and the ID uniquely identifies it). T2's unit test MUST assert the ULID-fallback behavior, not a presence-style skip.

- **Resolver:** `characterNameResolver` (`internal/grpc/character_name_resolver.go`, `Names(ctx, ids []ulid.ULID) → map[ulid.ULID]string`, backed by `world.CharacterRepository.GetNamesByIDs`) already exists and is wired into `CoreServer`. `SceneAccessServer` is in the **same package** — inject it as a new field (`WithCharacterNameResolver` option; wire at `cmd/holomush/sub_grpc.go`).
- **Apply:** after `GetScene`, collect participant + observer `CharacterId`s, one `Names()` batch call, overwrite `ParticipantInfo.CharacterName` from the map. Resolve observers too (INV-SCENE-61: listed separately). Best-effort — keep the ULID on a miss.
- **No new ABAC** (NG3): name resolution of an already-authorized roster is downstream display, identical to presence. The co-location seed (`seed:player-character-colocation`) governs world *perception*, not scene-roster display, and is correctly bypassed here.
- **Boundary:** resolution lives in the facade (`internal/grpc`, core), NOT the web BFF — the gateway-boundary invariant holds (the BFF proxies; the facade computes).

### 3.2 #3 — stamp the host-provided author name at emit

The host dispatcher already populates the pose author's display name: `dispatchToPlugin` builds `pluginsdk.CommandRequest{… CharacterName: exec.CharacterName() …}` (`internal/command/dispatcher.go:310`; field at `pkg/plugin/command.go:29-30`). So `handleEmit` already holds `req.CharacterName` — the posting character's name — with **zero** resolution work.

- **Change:** in `handleEmit` (`plugins/core-scenes/commands.go:1310-1314`), add `"character_name": req.CharacterName` to the IC payload map when non-empty. `Sensitive: true` unchanged.
- **Gateway:** no change — `translateEvent` already extracts `actor` from `character_name` (`internal/web/translate.go:82`). A test asserts a scene IC event with `character_name` yields a non-empty `GameEvent.actor`.
- **Graceful fallback:** if `req.CharacterName` is ever empty, omit the field; the gateway falls back to `actorId` exactly as today — no regression.

### 3.3 Crypto (#3 only)

`scene_pose`/`scene_say` are `crypto.emits` (encrypted IC payloads, per-event DEK, AAD-bound to event ID + subject — `plugins/core-scenes/plugin.yaml:144-150`). `character_name` rides **inside** the encrypted payload: decrypted server-side for authorized participants on the same path that already yields the pose text, so `translateEvent` sees it in cleartext.

- The display name is **no more sensitive** than the pose text it labels; bundling it in the same envelope is consistent. **AAD binding is unchanged** (event ID + subject — only a plaintext field is added before encryption).
- This touches the `crypto.emits` payload shape, so it **MUST** pass the `crypto-reviewer` gate before push (per root `CLAUDE.md`). The change is minimal (a field the plugin already has, no resolution).

## 4. Data Flow (after)

1. **Roster / pose-order (#1+#2):** `WebGetScene` → `GetSceneForViewer` (plugin `GetScene` → roster IDs; facade `characterNameResolver.Names` → names; overwrite `CharacterName`) → `workspaceStore.svelte.ts:189` `name = p.characterName` → components render the name.
2. **Pose author (#3):** `scene pose` → dispatcher stamps `CharacterName` → `handleEmit` adds `character_name` to the IC payload → encrypt/publish → participant decrypt → `translateEvent` sets `GameEvent.actor` → client `eventFrameToLogEntry` `actorName = ev.actor` → `PoseCard` renders the name.

## 5. Error Handling

- Resolver failure (#1/#2) → log with `errutil.LogErrorContext`, keep ULIDs, return the scene (never fail `GetSceneForViewer` on a name-resolution miss).
- Empty `req.CharacterName` (#3) → omit `character_name`; gateway falls back to `actorId` (no regression).
- Unknown/deleted ID → absent from the resolver map → ULID fallback (graceful).

## 6. Testing

| Tier | Coverage |
|------|----------|
| Unit (facade) | `GetSceneForViewer` overwrites participant + observer `CharacterName` from a mocked `characterNameResolver`; ULID retained on a resolver miss/error; resolver error does not fail the RPC. |
| Unit (plugin) | `handleEmit` includes `character_name` from `req.CharacterName` in the IC payload; omits it when `req.CharacterName` is empty. |
| Unit (gateway) | `translateEvent` fills `GameEvent.Actor` from `character_name` for a scene IC event. |
| Integration (`task test:int`, `integrationtest.WithInTreePlugins`) | End-to-end: a scene with ≥2 participants in **different locations** returns resolved roster names (not ULIDs) — pins the co-location-independence (the bug's core failure mode); a posted pose surfaces the author's display name on the IC stream. |
| Review gate | `crypto-reviewer` MUST run for the `handleEmit` / `crypto.emits` payload change before `code-reviewer`. |

`>80%` per-package coverage maintained; tests precede implementation (TDD); names follow ACE.

## 7. Invariants

- **Respect** INV-SCENE-61 (observers listed separately) — resolve both rosters; never merge.
- **Respect** the gateway-boundary invariant — resolution is in the facade (core), not the web BFF.
- **No new invariant proposed.** "Scene rosters resolve display names" is a feature requirement, not a durable system-behavior guarantee (per `.claude/rules/invariants.md`). The integration test pins the co-location-independence behaviorally; defer any registry entry to the design-reviewer.

## 8. Risks & Open Questions

- **R1 (crypto):** the only crypto-touching change is `character_name` in the encrypted IC payload (§3.3). Mitigated by unchanged AAD + the `crypto-reviewer` gate. If the reviewer prefers the name out of ciphertext, the alternative is cleartext rendering metadata + a `translateEvent` actor-source change — heavier; default is the in-payload field.
- **R2 (`req.CharacterName` population):** assumes the web `scene pose` path produces an `exec` with `CharacterName` set. The dispatcher always assigns it (`dispatcher.go:310`); residual risk is only that `exec.CharacterName()` is empty in some session state — handled by the graceful fallback (no regression). The integration test confirms the live web path.
- **R3 (scene_ooc):** `scene_ooc` is also `crypto.emits` on the same `translateEvent` path. If the OOC author also shows a ULID, `handleEmit` for OOC needs the same `character_name` treatment — fold into T2 (it shares the code path) or note explicit exclusion.

## 9. Task Breakdown (for plan-to-beads)

- **T1** — Inject `characterNameResolver` into `SceneAccessServer` (`WithCharacterNameResolver` option; wire at `cmd/holomush/sub_grpc.go`). No behavior change yet. Tests.
- **T2** — `GetSceneForViewer` resolves participant + observer names (best-effort, ULID fallback), incl. `scene_ooc` author parity check (R3). Tests with a mocked resolver.
- **T3** — `handleEmit` adds `character_name` from `req.CharacterName` to the IC payload (#3); gateway `translateEvent` test. `crypto-reviewer` pass.
- **T4** — Integration test (`WithInTreePlugins`): two participants in **different locations** → resolved roster names; a posted pose → resolved author. Close holomush-5rh.25.

## 10. Appendix — Design History (ruled-out approaches)

Two earlier approaches were ruled out by adversarial design review (full grounding in the `holomush-vdy2z` / `holomush-5rh.25` bd notes):

1. **Plugin resolves via the `world.query` host capability.** Rejected: the binary plugin host exposes no `world.query` surface (`goplugin/host.go` `WorldQuerier() → nil` — intentional, Lua-only). Briefly mis-filed as a parity gap (`holomush-udfqi`, now **closed**); it re-opened deliberately-retired epic `holomush-q42fh`. See the new "Permitted asymmetry" section in `.claude/rules/plugin-runtime-symmetry.md`.
2. **Plugin resolves via the `holomush.world.v1.WorldService` service** (the transport binary plugins do have). Rejected for the **roster**: `WorldService.GetCharacter` is gated by `seed:player-character-colocation` (`internal/access/policy/seed.go:48-52`), which denies reads of non-co-located characters. Scene participants are routinely in different locations, so roster reads would silently fall back to ULIDs — indistinguishable from the bug. (Self-resolution for #3 would have passed, but the host already provides the name via `req.CharacterName`, making even that unnecessary.)

The chosen host-side design avoids all of the above: the facade resolves rosters for an already-authorized set (the `ListFocusPresence` pattern), and the pose author rides the name the dispatcher already supplies.
<!-- adr-capture: sha256=c0b6ad71b40caa34; session=cli; ts=2026-06-21T00:38:56Z; adrs=holomush-sv1ei -->
