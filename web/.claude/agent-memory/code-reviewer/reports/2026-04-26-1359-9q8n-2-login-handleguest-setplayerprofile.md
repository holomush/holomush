# Code Review — holomush-9q8n.2 (login handleGuest → setPlayerProfile)

## Summary

A 2-file consistency cleanup. `web/src/routes/login/+page.svelte` `handleGuest()`
swaps `setPlayerAuth('Guest')` for a `webCheckSession` round-trip + `setPlayerProfile`
call, matching the pattern already used by `web/src/routes/+page.ts:38-49`,
`web/src/routes/(authed)/+layout.ts:17-25`, `web/src/routes/login/+page.ts:18-24`,
and `web/src/routes/register/+page.ts:18-24`. The now-orphaned `setPlayerAuth`
function is removed from `web/src/lib/stores/authStore.ts`. Bead description's
factual claims verified against the generated proto types and against every
producer/consumer in the repo.

## Blocking findings

None.

## Non-blocking findings

### 1. [Severity: Low] Login-page guest button has no e2e coverage; bead "no tests added because e2e covers it" is partially true

- Location: `web/e2e/auth.spec.ts:27-50`, `web/e2e/multi-tab-session.spec.ts:15-31`,
  `web/e2e/scenes.spec.ts:13`, `web/e2e/terminal.spec.ts:14`
- Evidence: every "Try as Guest" call site in the e2e suite navigates to `/`
  (landing page) and clicks `page.getByRole('main').getByRole('button', { name: 'Try as Guest' })`.
  None navigate to `/login` first. The login page's `handleGuest` button at
  `web/src/routes/login/+page.svelte:280` is exercised by no e2e test.
- Issue: The user-supplied justification ("existing e2e auth.spec.ts and multi-tab-session.spec.ts
  cover the guest-login flow at the UI level") is true for the landing-page
  guest flow but not for the `/login` page guest flow that this PR modifies.
  The two flows now use different code paths (landing's `handleGuest` at
  `web/src/routes/+page.svelte:55-88` does NOT call `setPlayerProfile` — it
  delegates to `(authed)/+layout.ts`; login's `handleGuest` at lines 84-130
  does the round-trip explicitly). A regression in `/login`'s guest path would
  not be caught by the existing e2e suite.
- Why it does not block: `(authed)/+layout.ts:17-25` runs after `goto('/terminal')`
  / `goto('/characters')` and re-populates the profile from a fresh
  `webCheckSession`. Even if the explicit `setPlayerProfile` call in `handleGuest`
  were broken or removed entirely, the layout-load fallback would still
  populate auth state correctly post-navigation. The risk surface is the brief
  pre-navigation window; nothing mounted in `/login` reads `playerId`/`isGuest`
  during that window.
- Suggested follow-up: file a low-priority bead to add a single Playwright test
  covering `/login` → "Try as Guest" → `/terminal`, mirroring `auth.spec.ts:27-50`
  but starting from `/login` instead of `/`. Not blocking for this PR.

### 2. [Severity: Low] Error-path partial-state window (webCreateGuest succeeds, webCheckSession throws)

- Location: `web/src/routes/login/+page.svelte:94-108`
- Evidence: `client.webCreateGuest({})` succeeds — server has minted a guest
  session and set the auth cookie. The next line `await client.webCheckSession({})`
  could throw (network blip, or — per `web_pb.ts:768-786` comment — anything
  other than the success path returns `connect.CodeUnauthenticated`). The
  `catch (e)` at line 125 then sets `error = ...` and returns. No `clearAuth()`,
  no `webLogout({})` — the user is left on `/login` with the form re-shown,
  but the server-side guest cookie is live.
- Issue: The user is in a half-completed state: server says "you're a guest,"
  client UI says "please sign in." The next page navigation, or a refresh, will
  trip `(authed)/+layout.ts` or `+page.ts`'s pre-existing `webCheckSession`
  call, which will populate the profile and recover. The user-visible glitch
  is the error message + busy=false + button enabled again, while a guest
  cookie sits on the connection.
- Why it does not block: this exact failure mode is tiny in probability
  (server just succeeded the prior call on the same connection) and is
  self-healing on next navigation. The pre-existing `setPlayerAuth('Guest')`
  call had no failure mode here only because it was synchronous; the round-trip
  trades that for behavior consistent with every other auth-establishing call
  site, which is the explicit intent.
- Suggested follow-up: optionally tighten by either (a) wrapping just
  `setPlayerProfile` in a try, falling back to the navigation-and-let-layout-handle-it
  path on `webCheckSession` failure, or (b) calling `webLogout({})` in the
  catch when the prior call was `webCreateGuest`. Either is bikeshedding —
  current behavior is acceptable.

### 3. [Severity: Low] Comment block is dense; could explain "why round-trip" more crisply

- Location: `web/src/routes/login/+page.svelte:96-98`
- Evidence: `// Match +page.ts/(authed)/+layout.ts: round-trip webCheckSession so
  // setPlayerProfile gets playerId/isGuest/characters (WebCreateGuest
  // doesn't return playerId or isGuest).`
- Issue: Reads fine. Could trim to one line. Pure style nit. Not actionable.

## Verification evidence

- Read:
  - `web/src/routes/login/+page.svelte` (full)
  - `web/src/lib/stores/authStore.ts` (full)
  - `web/src/lib/stores/authStore.test.ts` (full)
  - `web/src/routes/+page.ts` (full)
  - `web/src/routes/+page.svelte` (lines 50-125 — landing handleGuest reference impl)
  - `web/src/routes/(authed)/+layout.ts` (full)
  - `web/src/routes/register/+page.ts` (full)
  - `web/src/lib/connect/holomush/web/v1/web_pb.ts` lines 520-555 (WebCreateGuestResponse — confirmed no playerId/isGuest)
  - `web/src/lib/connect/holomush/web/v1/web_pb.ts` lines 760-786 (WebCheckSessionResponse — confirmed has playerId/isGuest/characters)
  - `web/e2e/auth.spec.ts` lines 1-80
  - `jj diff -r @` (full diff)
- Searched:
  - `rg "setPlayerAuth\b"` repo-wide → zero hits in production code (web/src,
    cmd/, internal/, pkg/, plugins/). Only matches are in
    `docs/superpowers/{specs,plans}/` historical design docs, which describe
    superseded designs and are not consumers.
  - `rg "setPlayerProfile\b"` repo-wide → 5 production call sites, all with
    matching shape: `+page.ts:41`, `register/+page.ts:19`, `login/+page.ts:19`,
    `(authed)/+layout.ts:20`, `login/+page.svelte:100` (this diff).
  - `rg "handleGuest|webCreateGuest"` in `web/src/` → confirmed only two
    `handleGuest` impls (landing, login).
  - `rg "Try as Guest|/login.*guest|getByRole.*guest"` in `web/e2e/` → all
    e2e Try-as-Guest flows go through landing, none through `/login`.
- External grounding: not required — no library/version question. ConnectRPC
  generated types and `setPlayerProfile`/`webCheckSession` shapes are
  self-evidencing in the generated `web_pb.ts`.

### Question-by-question answers

1. **Round-trip safe vs. (authed)/+layout.ts re-population?** Yes. The layout
   load() runs *after* SvelteKit completes navigation, well after
   `setPlayerProfile` has settled. Worst case both write the same fields with
   the same values — `setPlayerProfile` always sets
   `isPlayerAuthenticated: true` and overwrites the same five fields
   (`authStore.ts:45-52`). No race.

2. **Call shape matches +page.ts and (authed)/+layout.ts?** Yes — exact
   field-for-field match. All three (and `register/+page.ts`, `login/+page.ts`)
   use `playerId: session.playerId, playerName: session.playerName,
   isGuest: session.isGuest, characters: session.characters.map((c) => ({
   characterId: c.characterId, name: c.characterName }))`. The character
   mapping translates proto `characterName` to the store's `name` field
   identically across all five sites.

3. **Removing setPlayerAuth safe?** Yes. Repo-wide grep returns zero
   production callers. Test file `authStore.test.ts` does not import
   `setPlayerAuth` (only `setPlayerProfile` and `clearAuth`). All historical
   refs are in `docs/superpowers/` design docs, which are non-executing
   history.

4. **Error-path risk?** See non-blocking finding #2 — small partial-state
   window if `webCheckSession` throws between successful `webCreateGuest`
   and navigation. Self-healing on next navigation via `(authed)/+layout.ts`.
   Not a blocker.

5. **Anything else risky?** No. CI is green per user (`task pr-prep`
   passed including 62 e2e tests). `task pr-prep` is the project's
   single-source pre-PR gate per CLAUDE.md "Pre-PR checks" section.

## Verdict

- [x] READY — no blocking findings, implementer may proceed to hand-off
- [ ] NOT READY
