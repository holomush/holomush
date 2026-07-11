<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# D8a — Web Client Static Audit — Findings

**Agent:** claude-opus-4.6 (sonnet-5 session) · **Date:** 2026-07-11 · **Scope examined:** `web/src/routes/**`, `web/src/lib/{stores,scenes,comm,presence,components,hooks,theme}/**`, `web/src/lib/transport.ts`, `web/src/app.html`, `web/src/app.css`, `web/package.json`, `web/CLAUDE.md`, `.claude/rules/branding.md`, `.claude/rules/gateway-boundary.md`. Read-only, no dev server driven (static code review only — the parallel live-browser pass owns runtime behavior).

## Summary

The web client is well-engineered for a hobbyist-scale project: disciplined Svelte 5 runes usage (proper `$effect` cleanup, generation-gated async races in the terminal hydrate flow), correct adherence to the gateway-boundary typed-RPC rule for scene structural writes, an escaping-safe `linkUrls` helper used everywhere except one already-tracked XSS site, real brand-token discipline with an automated amber-cursor-only test, and thoughtful UX details (composer draft persistence, keyboard shortcut footer, reduced-motion gating). The most significant gap is the **PWA claim in `docs/` and `site/` is entirely aspirational** — there is no manifest, no service worker, and no offline posture anywhere in `web/`. Completeness has one real gap: the Channels subsystem has full backend/proto support but zero dedicated GUI (usable only via typed terminal commands). Accessibility has one real gap: the primary command textarea has no accessible name. Four issues (restoreSession/onMount timing, mobile terminal responsiveness, AnsiRenderer XSS, light-theme low contrast) are already tracked in GitHub Issues and reconfirmed present in current code.

**Severity counts:** Blocker 0 · High 1 · Medium 4 · Low 3 · Strengths 7

## Findings

### HIGH-1 "Offline-capable PWA" claim is entirely aspirational — no manifest, no service worker

- **Severity:** High
- **Claim:** `site/src/content/docs/contributing/explanation/architecture.md:298` and `operating/index.mdx:25` describe the web client as a "SvelteKit PWA... offline-capable," but `web/src/app.html` has no `<link rel="manifest">`, no theme-color/icon meta, `web/package.json` has no PWA/service-worker/workbox dependency, and there is no `web/static/` directory or any `sw.js`/`service-worker.ts` anywhere in the tree.
- **Evidence:** `web/src/app.html:1-9` (bare `<head>`, `%sveltekit.head%` only); `web/package.json:1-40` (no `vite-plugin-pwa`, no `workbox-*`); `find web -iname "*.webmanifest" -o -iname "sw.js"` → zero hits; `site/src/content/docs/contributing/explanation/architecture.md:298` (`| **Web Client** | SvelteKit PWA | Modern, offline-capable |`).
- **Impact:** Operators and contributors reading the architecture docs believe the client works offline/installable; it does not. A user closing their laptop lid or losing connectivity gets no cached shell, no install prompt, and no offline fallback — just a blank/broken tab. This is a docs-vs-reality gap on a claim that shapes expectations for a "modern PWA" client.
- **Recommendation:** Either (a) ship a minimal manifest + install-prompt (no service worker needed for "installable"), and downgrade the "offline-capable" doc claim to "installable," or (b) if offline support is genuinely out of scope for v1, correct `architecture.md:298` and `operating/index.mdx:25` to drop "offline-capable" and describe it as an online-only PWA-shell. Given hobbyist-scale calibration, correcting the docs is the cheap fix; a real service worker is a larger follow-up.
- **Dedup:** none

### MEDIUM-1 Channels subsystem has no dedicated web GUI (typed-RPC stubs exist, unused)

- **Severity:** Medium
- **Claim:** The Channels subsystem (merged as part of #4595, backend-complete with a full `ChannelService` — create/join/leave/list/post/who/history/invite/mute/ban/kick/transfer) has generated ConnectRPC stubs in the web client but zero consuming code — no route, no component, no store references any `channel` RPC.
- **Evidence:** `web/src/lib/connect/holomush/channel/v1/channel_pb.ts` exists (generated stub only); `rg -rn "ChannelService" web/src` returns only the generated file itself; no hits for a channels route under `web/src/routes/` or component under `web/src/lib/components/`. Contrast with Scenes, which has a full typed-RPC surface (`web/src/lib/scenes/client.ts`) plus dedicated routes (`web/src/routes/(authed)/scenes/**`) and components (`web/src/lib/components/scenes/**`).
- **Impact:** Channels are reachable only via the typed terminal `channel` command (`plugins/core-channels/plugin.yaml:199-202`) — functional but with no browsable member list, no channel directory, no structural-action buttons, unlike the scenes GUI's rich workspace. Web-only users (no telnet familiarity) get a materially worse channels experience than the scenes experience.
- **Recommendation:** Scope a channels sidebar panel (list/join/who/history) mirroring the scenes workspace pattern, using the already-generated typed-RPC client per the gateway-boundary rule (typed facade RPC for structural actions, terminal command path only for message send).
- **Dedup:** none

### MEDIUM-2 No `+error.svelte` error boundary anywhere in the route tree

- **Severity:** Medium
- **Claim:** There is no `web/src/routes/+error.svelte` (or nested `+error.svelte`) and no `<svelte:boundary>` usage anywhere in `web/src`, so any uncaught `load()`/rendering exception falls through to SvelteKit's default unstyled error page.
- **Evidence:** `find web/src -iname "+error.svelte"` → zero hits; `grep -rl "svelte:boundary" web/src` → zero hits; `find web/src/routes -maxdepth 1 -type f` lists only `+layout.ts/svelte` and `+page.ts/svelte`, no `+error.svelte`.
- **Impact:** A thrown error in any `load()` function (e.g. a transport failure during `webCheckSession` in `(authed)/+layout.ts`, not wrapped defensively) drops the user onto a generic browser-default error screen with no branding, no "go home" affordance, and no Sentry-friendly UX hook — inconsistent with the otherwise careful error-surfacing elsewhere (login/register error banners, terminal reconnect banner).
- **Recommendation:** Add a root `+error.svelte` using the existing `Card`/branding shell with a "Return to login" / "Reload" action.
- **Dedup:** none

### MEDIUM-3 Command textarea has no accessible name

- **Severity:** Medium
- **Claim:** The primary terminal command input is a bare `<textarea>` with only a `placeholder`, no `<label>`, `aria-label`, or `aria-labelledby` — placeholder text is not a reliable accessible name per WCAG 4.1.2 (many screen readers do not announce it, and it disappears once text is typed).
- **Evidence:** `web/src/lib/components/terminal/CommandInput.svelte:177-188` — `<textarea bind:this={textarea} bind:value={text} onkeydown={handleKeydown} oninput={autoGrow} rows="1" placeholder="Enter command..." spellcheck="false" autocomplete="off" disabled={...} aria-disabled={...}></textarea>` — no `aria-label`/`id`+`<label>` pairing.
- **Impact:** Screen-reader users navigating to the single most important control in the app (the command line) get no announced purpose beyond whatever "textarea" default the AT provides.
- **Recommendation:** Add `aria-label="Command input"` to the textarea (one-line fix; the visible `.cmd-prompt` `>` span is decorative and not wired as a label).
- **Dedup:** none

### MEDIUM-4 Stream drop requires a manual "Reconnect" click — no automatic retry/backoff

- **Severity:** Medium
- **Claim:** When the `StreamEvents` subscription throws (network blip, gateway restart), `hydrateAndStream`'s catch branch sets `error` and `connected = false`, showing a login-screen with a "Reconnect" button — there is no automatic reconnect-with-backoff attempt.
- **Evidence:** `web/src/routes/(authed)/terminal/+page.svelte:406-419` (catch branch sets `error`/`connected=false`, no retry scheduling) and `:680-684` (`reconnect()` is user-click-only, `onclick={reconnect}` at `:692`).
- **Impact:** A brief network hiccup (Wi-Fi blip, gateway rolling restart) fully drops the user to a "connection lost" screen requiring a manual click, rather than silently recovering — a rougher UX than most modern chat/game clients, though acceptable for a hobbyist-scale text client.
- **Recommendation:** Add a bounded auto-retry (e.g. 3 attempts with jittered backoff) before falling back to the manual button; keep the manual button as the final fallback. Low-cost, meaningfully improves resilience against transient blips.
- **Dedup:** none

### LOW-1 `restoreSession()` called from `onMount` instead of root `load()` — contradicts web/CLAUDE.md's own Auth Guards rule

- **Severity:** Low (already tracked; reconfirmed present)
- **Claim:** `web/CLAUDE.md:154-158` states session restoration "must happen in `load()`, not `onMount()`, or auth guards will redirect on page reload" — yet `restoreSession()` is still called inside `onMount()` in the root layout.
- **Evidence:** `web/src/routes/+layout.svelte:98-105` — `onMount(() => { initTelemetry(); initSentry(); restoreSession(); hydrateUiPrefs(); window.addEventListener(...); ... })`.
- **Impact:** Per the documented rule, this risks a reload-time redirect race for authed routes.
- **Recommendation:** Move `restoreSession()` into `web/src/routes/+layout.ts`'s `load()`.
- **Dedup:** already-tracked:#4760

### LOW-2 Authed terminal route not responsive on mobile

- **Severity:** Low (already tracked; not independently re-driven — code inspection only)
- **Claim:** TopBar/terminal layout uses fixed-width flex panes (`Resizable.PaneGroup`) with no mobile breakpoint collapse in `web/src/routes/(authed)/terminal/+page.svelte` styles.
- **Evidence:** `web/src/routes/(authed)/terminal/+page.svelte:751-779` (`.terminal-layout`/`.main-area` — no `@media` query narrowing sidebar/pane behavior below `md`).
- **Dedup:** already-tracked:#4618

### LOW-3 AnsiRenderer XSS: raw `ansi_up` HTML injected via `@html` without input pre-escaping

- **Severity:** Low (already tracked; reconfirmed present)
- **Claim:** Unlike every other `@html` site in the codebase (`urlLinker.ts` escapes first, then linkifies), `AnsiRenderer.svelte` feeds `text` straight into `ansiUp.ansi_to_html(text)` and injects the result via `{@html html}` with no pre-escape.
- **Evidence:** `web/src/lib/components/terminal/AnsiRenderer.svelte:14-20`; contrast `web/src/lib/util/urlLinker.ts:11-13` (`escapeHtml(text)` called before regex linkify) used by every other renderer (`CommunicationLine.svelte:15-26`, `FallbackRenderer.svelte:30`, `CommandRenderer.svelte:32`, `SystemRenderer.svelte:24,26`, `MovementRenderer.svelte:27`).
- **Dedup:** already-tracked:#4600

## Strengths

- **Real gateway-boundary discipline**: every scene structural mutation (`endScene`, `pauseScene`, `muteScene`, `inviteToScene`, `kickFromScene`, `transferOwnership`, `leaveScene`, `createScene`, `updateScene`) goes through a typed `WebService` RPC in `web/src/lib/scenes/client.ts:171-292`; only conversational verb text (`sendSceneCommand`, `web/src/lib/scenes/client.ts:126-138`) uses `client.sendCommand`, exactly matching `.claude/rules/gateway-boundary.md`'s human/CLI-vs-GUI split.
- **Automated brand-token enforcement**: `web/src/lib/stores/themeStore.test.ts:208-218` programmatically asserts every theme's non-cursor tokens are cyan-dominant and amber appears only on the cursor token — turns `.claude/rules/branding.md` INV-1 into a CI-checked invariant rather than a convention.
- **Race-safe streaming/reconnect design**: `hydrateAndStream` in `web/src/routes/(authed)/terminal/+page.svelte:226-621` uses a monotonic `streamGeneration` counter plus per-generation `AbortController` snapshotting to prevent stale async responses (backfill, presence snapshot, Subscribe replay) from clobbering a newer connection's state — genuinely careful concurrent-Svelte-5 engineering, well beyond typical hobbyist-project rigor.
- **Correct runes hygiene**: `$effect` blocks consistently return cleanup functions (`web/src/lib/hooks/mediaQuery.svelte.ts:18-25`, `web/src/routes/(authed)/terminal/+page.svelte:113-120`), no stale-closure patterns observed in the sampled stores/components.
- **XSS-safe-by-default helper used broadly**: `linkUrls()` escapes before linkifying (`web/src/lib/util/urlLinker.ts:11-13`) and is the single choke point used by 6 of 7 `@html` renderer sites — good centralization (the one exception is tracked, see LOW-3).
- **Accessible auth forms**: login/register/reset forms use `<Label for=...>`/`<Input id=...>` pairing, `type="submit"` buttons, visible busy-state text ("Signing in..."), and inline error banners rather than silent failures (`web/src/routes/login/+page.svelte:253-290`).
- **Reduced-motion respected**: animations in `app.css:96-98`, `TerminalView.svelte:120,142`, and `Composer.svelte:200` are gated behind `@media (prefers-reduced-motion: no-preference)`.

## Not examined

- `web/src/lib/connect/**` generated proto bindings themselves (trusted generated code, not hand-authored).
- Runtime/visual verification (actual contrast ratios, live mobile rendering, live reconnect behavior) — deferred to the parallel live-browser pass per the task boundary; this audit is code-only.
- `web/e2e/` Playwright specs — not part of the static UI-code scope (would duplicate the testing-dimension review).
- Full component-by-component ARIA sweep of `web/src/lib/components/scenes/**` (24 files) and `web/src/lib/components/ui/**` (shadcn-generated) — sampled TopBar/CommandInput/forms only; shadcn `ui/` components are upstream-maintained and assumed compliant absent contrary evidence.
- `web/src/lib/theme/*.json` full 4-theme token audit beyond the amber-cursor check (deferred to the already-tracked light-mode contrast issue #4728).
