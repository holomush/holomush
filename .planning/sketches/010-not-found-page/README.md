---
sketch: 010
name: not-found-page
question: "Four different kinds of 'nothing here' must be indistinguishable to one viewer. What does the ordinary not-found page look like, and where does the back button go?"
winner: "B"
tags: [not-found, error-page, privacy, opacity, routing, phase-6, frontier]
---

# Sketch 010: The Ordinary Not-Found

## Design Question

This is the smallest sketch in the set and the one carrying the most weight.
**Three independent designs now rest on a page that does not exist** ([#4903](https://github.com/holomush/holomush/issues/4903)):

| Surface | Depends on it for |
| --- | --- |
| Sketch 003 — `/admin` invisibility | A deep link to a gated section must render the **ordinary** not-found, never a redirect and never a bespoke forbidden page, or the bounce itself confirms `/admin/*` is a real route family. |
| Sketch 007 — unreachable profiles (§8.7) | A profile below its reachability floor must return a **not-found-equivalent indistinguishable from a character that does not exist**. |
| Sketch 006 — the rejected variant C | A deep-linkable edit sheet would have needed the same treatment. Rejecting C avoided *adding* to this dependency; it did not remove the existing two. |

There is **no `+error.svelte` anywhere under `web/src/routes/`** today — verified,
count zero.

So the design question is not "draw a 404". It is: **what page can be shown for
four structurally different reasons without any of them being tellable apart —
and where does its one affordance point, given the viewer might be anonymous?**

## How to View

```
open .planning/sketches/010-not-found-page/index.html
```

It opens in **compare mode** with all four paths side by side. Each panel carries
a **fingerprint** of its own rendered markup (path label excluded). Identical
fingerprints mean identical pages.

**Then press `☣ inject leak`.** See below.

## Variants

- **A: Minimal** — glyph, `Not found`, one line, one back button. Mirrors sketch
  003's winning restraint.
- **B: + where you can go ★ WINNER** — same head, but the single button becomes a short
  list of the viewer's *own* available destinations. Safe, because those are the
  sections already drawn in that viewer's rail — it discloses nothing they cannot
  already see.
- **C: In-shell** — for a signed-in viewer the not-found renders **inside** the app
  shell (rail present, content replaced) so they never lose their place. Anonymous
  viewers get the standalone page, because there is no shell to be inside.

## The property, stated precisely

**Indistinguishability is per-viewer, not global.**

It has never been required that an admin and a stranger see the same thing — an
admin hitting `/admin/moderation` *should* get sketch 003's `Registered and
gated. No handler yet.` That is the gate **permitting** them, which is the system
working.

What is required is that **one viewer cannot tell which kind of nothing they
hit.** Switch the viewer to `player + admin` in compare mode: the admin path now
resolves to a real screen while the other three still fingerprint identically.
That is correct, and the note updates to say so.

Getting this precision wrong in either direction is a design error: too weak and
you leak; too strong and you would be forbidden from ever showing an admin their
own section.

## ⚠ The negative control — press `☣ inject leak`

The fingerprint check has an obvious weakness: in a correct implementation the
renderer **does not branch on which kind of miss occurred**, so the panels are
identical by construction and the check passes trivially. A check that cannot
fail proves nothing — that is the *verification-that-cannot-fail* family this
codebase keeps rediscovering (`oops.Code`, the fabricated `last seen` column,
the escalation test with no positive control).

So the sketch ships the failure. `☣ inject leak` makes `/admin/moderation` render
a bespoke **"Access denied — you don't have permission to view this area"** page:

- The fingerprints diverge and the panels lose their match border.
- The note flips to a disclosure warning.

That page is **the single most natural thing an implementer would write.** It is
polite, it is helpful, it is what every other web app does — and it hands a
prober the fact that `/admin/*` exists and they are merely excluded. Seeing it go
red is the point of the toggle.

Verified both directions: identical inputs → gate green; one panel changed → gate
red.

## Decision (2026-08-01)

**B wins.** A dead end should offer a way out, and B's list costs nothing in
disclosure: the destinations are the viewer's **own** sections, already drawn in
their rail and already filtered by the same `requiresPlayer` gate
(`nav/sections.ts:41-47`). A stranger and a player see different lists because
they *have* different sections — not because the page inferred anything about
what they were reaching for.

**C is rejected.** Rendering in-shell would have forced a nested `+error.svelte`
boundary, and that reopens exactly the risk this page exists to close: once
error boundaries are nestable, someone later adds `routes/admin/+error.svelte`
and the indistinguishability dies silently, with no test failing. **A single root
`+error.svelte` is the safer shape**, and B works fine there.

> **Phase-6 note:** ship a meta-test asserting there is exactly **one**
> `+error.svelte` under `web/src/routes/`. The property this page carries cannot
> be defended by review alone — a second boundary is a one-file PR that looks
> harmless.

### ⚠ Copy correction — do not hardcode the platform brand

The first draft's button read **"Back to HoloMUSH"**. That is wrong twice over:

1. **HoloMUSH is the platform, not the game.** `.claude/rules/branding.md` INV-6
   is explicit that the brand is "the **software/platform** only — never the game
   world / default setting". A player is in *a game that runs on* HoloMUSH.
2. **The game's name is not the platform's to assume.** It exists —
   `SettingConfig.DisplayName`, a **required** field on setting-type plugins
   (`internal/plugin/manifest.go:211`; a setting plugin *is* the world content
   pack, e.g. `display_name: My World`).

**But it is not reachable from the web client.** No RPC carries it, no `Web*`
response has the field, and the client renders no game name anywhere today — the
only `HoloMUSH` strings under `web/src/` are SPDX copyright headers. So v0.13
**cannot** render the game name without new plumbing.

**Resolution: the copy is `Home`.** Viewer-agnostic, no new surface, and it
conflates nothing. The `>holomush_` wordmark stays in the top bar, which is
platform chrome and exactly what INV-6 permits.

**Carried forward as a gap:** *the game's display name is server-side-only.* Any
phase that wants player-facing game identity — a title tag, an OG card, a welcome
line, this button — needs it exposed first. Worth an issue in its own right; it
is not specific to this page.

## What to Look For

- **A vs B for a dead end.** A gives you one button. B gives you three or four
  places to go, drawn from the viewer's own sections. Does the list help, or does
  it make a rare page feel over-built? Note B's list is **already filtered by
  `requiresPlayer`** — `nav/sections.ts:41-47` carries exactly that flag, and it
  is the same gate the Rail and the command palette flow through.
- **C at `player` vs C at `anonymous`.** C keeps signed-in viewers inside the
  shell. For anonymous there is no shell, so C degrades to A. Is that split worth
  it, or does a not-found that keeps the rail feel like the app half-worked?
- **The destination list changes by viewer, but the copy does not.** Every viewer
  gets `Home`; what varies is how many *sections* sit under it. This closes the
  open question
  sketch 003 left ("the affordance still needs a sensible target per viewer").
  Check both.
- **Compare mode with viewer `player + admin`.** One panel resolves, three stay
  identical. Confirm that reads as correct rather than as a leak.
- **The copy itself.** `We couldn't find that page.` — deliberately says nothing
  about *why*, offers no search, no suggestion, and does not echo the path back.
  **Echoing the path would be a mistake**: `/admin/moderation` rendered back at a
  prober confirms the string was routed somewhere.

## Grounding

| Element | Source |
| --- | --- |
| Deep link renders the **ordinary** not-found, never a redirect, never bespoke for `/admin` | sketch 003 decision |
| `adapter-static` + `fallback: 'index.html'` ⇒ every unknown path is already HTTP 200 + `index.html`; the client router decides | `web/svelte.config.js`, sketch 003 |
| Unreachable profile ⇒ not-found-equivalent, indistinguishable from a nonexistent character; no "private" signal, no distinct code | SPEC §8.7 |
| The pattern being applied — indistinguishable across status, error code and body | `INV-PRIVACY-9` |
| Route guard is UX, **not** the boundary | SPEC §10.4 |
| `requiresPlayer` already gates the viewer's own sections | `web/src/lib/nav/sections.ts:41-47` |
| No `+error.svelte` exists | verified: `find web/src/routes -name '+error.svelte'` ⇒ 0 |

**A real 404 status is not available.** Because of `adapter-static` with an
`index.html` fallback, every route in this app returns HTTP 200 and the client
router picks the page. That is a **strength** for this property, not a
limitation: `/admin/moderation` and `/blahblah` are identical at the HTTP layer
**by construction**, with no per-route work needed to keep them that way. The
indistinguishability only has to be maintained in the client render — which is
exactly what the fingerprint check watches.

## Components this implies adding

Nothing beyond the running list. `empty` (already on 003's list) covers the
centred glyph/heading/body block; variant B uses `button` (installed).

## Open questions this sketch surfaces

1. **Where does `+error.svelte` live?** SvelteKit resolves the nearest error
   boundary. A single root `+error.svelte` guarantees one page everywhere; a
   nested one under `(authed)` would let the shell wrap it (variant C) but
   **immediately reintroduces the risk** that someone later adds
   `routes/admin/+error.svelte` and destroys the property. If C wins, the
   boundary placement needs an explicit note — and probably a meta-test asserting
   no `+error.svelte` exists below the ones deliberately placed.
2. **Does the client know *why* it is rendering not-found?** It must not act on
   it, but it will often have the information (an RPC returned a denial). The safe
   rule is that the not-found renderer takes **no reason parameter at all**, so
   there is nothing for a future change to accidentally surface.
3. **Telemetry.** An operator reasonably wants to know how often gated deep links
   are hit. Logging that server-side is fine; **deriving it client-side from which
   page rendered is fine too** — but any *response-shaped* difference (a header, a
   status, a body field) reopens the oracle. Worth stating before someone adds an
   analytics event keyed on refusal type.
