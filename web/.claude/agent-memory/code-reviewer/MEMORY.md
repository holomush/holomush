# HoloMUSH code-reviewer memory

Project-scoped notes for adversarial code review. Curate aggressively;
keep under 200 lines.

## Anti-patterns and recurring blind spots

- **e2e "covers it" claims need surface-level verification.** When an
  implementer claims existing e2e covers a UI flow, grep the e2e suite for
  the specific entry point (route + button selector). HoloMUSH has separate
  `handleGuest` impls on `/+page.svelte` (landing) and `/login/+page.svelte`
  (login page). All e2e "Try as Guest" tests use the landing page; the
  /login page guest button has no e2e coverage. Don't accept a vague
  "e2e covers it" without confirming the exact test exercises the changed
  code path.

- **Auth-store calls have a defense-in-depth backstop.** `web/src/routes/(authed)/+layout.ts`
  re-runs `webCheckSession` + `setPlayerProfile` on every navigation into
  the (authed) group. So a missed/broken `setPlayerProfile` call on a
  pre-navigation surface is usually self-healing post-navigation. Worth
  noting before flagging "auth state not set" as blocking.

- **Connect generated types are the contract.** When a diff claims an RPC
  response "doesn't return field X," verify against `web/src/lib/connect/.../web_pb.ts`
  — those are the generated source of truth. Cite line numbers.

- **`docs/superpowers/{specs,plans}/` matches in repo grep are non-executing
  history.** Don't count them as live consumers when verifying "is this
  symbol still used?" — only `web/src/`, `cmd/`, `internal/`, `pkg/`,
  `plugins/` matter.

## Repo-specific landmarks

- Auth store: `web/src/lib/stores/authStore.ts` exports `setPlayerProfile`,
  `setCharacterSession`, `clearAuth`, `clearCharacterSession`,
  `restoreSession`. Five production call sites currently use
  `setPlayerProfile`: landing `+page.ts`, login `+page.ts`,
  login `+page.svelte` (this diff), register `+page.ts`,
  `(authed)/+layout.ts`.

- The two `handleGuest` impls diverge intentionally: landing relies on
  layout-load fallback; login does an explicit round-trip for consistency
  with sibling `+page.ts` load() patterns. Don't suggest "unify them" —
  this was a deliberate user choice.
