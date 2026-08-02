# Gating & Absence — planned sections, invisibility, denial

Validated in sketch **003-planned-section-empty**. Winner: **A — Minimal**.

Extended by **010-not-found-page** (winner **B — + where you can go**), which designs the
page 003 depends on, and by **007-public-profile-viewer-tiers**, which makes a **third**
surface depend on it (§8.7: an unreachable profile returns a not-found-equivalent). See
"The ordinary not-found" below.

Six of the seven admin sections have no handler. §10.3 requires they **gate first, then
refuse** — and that a non-admin is *denied*, not told the section is unimplemented. Two
states, only one of which is the friendly one.

## Design Decisions

### The permitted-but-unimplemented state is minimal

Glyph, section name, and one line: **`Registered and gated. No handler yet.`** Nothing else.

Least to write, least to rot. The screen's job is to be **unremarkable and honest**.

**Rejected:**
- **B — Gate provenance.** Adds an authorization trace (subject, action,
  `admin_section:<id>`, policy, `PERMIT`, then `NOT_IMPLEMENTED`). It is the strongest
  *proof* of gate-then-refuse ordering, but it puts implementation detail in front of an
  operator who did not ask. **The ordering is better proven by the §10.2 denial test than
  by narrating itself in the UI.** (Same energy sketch 001 rejected as "Registry Ledger".)
- **C — Scope preview.** Says what will live in each section. **It fails visibly** — see
  Finding 2 below.

### `/admin` is invisible without permission

Maintainer call, 2026-08-01. A viewer without permission sees **no `/admin` at all** — no
rail icon, no nav entry — and a deep link is bounced.

This is not a deviation: ROADMAP Ph6 SC5 already says *"the roles exposed on
`WebCheckSessionResponse` change only what is drawn"*, and §10.4 treats a `+layout.ts`
redirect as **UX, not the boundary**.

| Layer | Behavior | Status |
| --- | --- | --- |
| Rail + nav | Admin is not drawn — filtered from the registry contract, **never** a template `{#if}` | Ph6 SC5 |
| Deep link `/admin/moderation` | Renders the **ordinary not-found page** — the same one `/blahblah` gets | new, this sketch |
| The actual boundary | ABAC gate on `admin_section:*`; every admin RPC still denies independently | §10.4 |

### Not-found, **never** a redirect — and why

A redirect to `/terminal` is a **distinctive** response. If `/admin/moderation` bounces
while `/nonsense` renders not-found, **the bounce itself confirms `/admin/*` is a real
route family** — the prober learns the area exists and they are merely excluded.

A not-found makes the two **indistinguishable**. This is `INV-PRIVACY-9`'s pattern
(*"identical to the response for a character id that does not exist"*) applied at the route
layer.

**"404" here means a client-rendered page, not an HTTP status.** `web/svelte.config.js`
uses `adapter-static` with `fallback: 'index.html'`, so **every** unknown path already
returns **HTTP 200 + `index.html`** and the client router decides what to render. A real
404 status is not available for any route in this app.

That is a **strength** for this property, not a weakness: because every route resolves
through the same fallback, `/admin/moderation` and `/blahblah` are identical at the HTTP
layer **by construction**, with no per-route work to keep them that way.

### The redirect is not enforcement

§10.4 is explicit that a route guard is *"bypassable by any caller who skips the route"*.
SC5's rule — *"drawing a link the viewer may not use still results in a denial at the RPC"*
— has an inverse that matters just as much:

> **Not drawing the link does not remove the need for that denial.**

A consequence worth stating plainly: because the nav is hidden and deep links render
not-found, **the only callers still reaching the denial path are those deliberately
bypassing the UI.** That does not mitigate D1 below — it *concentrates* it onto exactly the
population the leaky codes disclose to.

## ⚠ Blocking gap: the not-found page does not exist

There is **no `+error.svelte` anywhere under `web/src/routes/`** today — verified, count
zero. The page this whole design rests on is Phase 6 work. Tracked as
[#4903](https://github.com/holomush/holomush/issues/4903).

**It must be the *ordinary* one.** The moment `/admin` gets its own bespoke not-found, the
indistinguishability this design rests on is gone.

**Three independent surfaces now rest on it:**

| Surface | Depends on it for |
| --- | --- |
| **003** — `/admin` invisibility | A deep link to a gated section must render the ordinary not-found, or the bounce itself confirms `/admin/*` is a real route family. |
| **007** — unreachable profiles (§8.7) | A profile below its reachability floor must return a not-found-equivalent **indistinguishable from a character that does not exist**. |
| **006** — the *rejected* variant C | A deep-linkable edit sheet would have needed the same treatment. Rejecting C avoided **adding** to this dependency; it did not remove the existing two. |

## ⚠ SPEC defect D1 — §10.3 and §10.4 disagree

Tracked as [#4904](https://github.com/holomush/holomush/issues/4904).

**§10.3** (normative): a non-admin hitting a planned section is denied, and *"the refusal
reveals nothing about which sections exist or what is being built."*

**§10.4** (normative, same section): denial codes are `DENY_ADMIN_SECTION` when the ABAC
decision denies, and `DENY_ADMIN_SECTION_UNREGISTERED` when the id is not in the registry.

**These cannot both hold if the two codes are distinguishable to the caller:**

| Probe | Code returned | What the caller learns |
| --- | --- | --- |
| `admin_section:zzz` | `DENY_ADMIN_SECTION_UNREGISTERED` | not a real section |
| `admin_section:moderation` | `DENY_ADMIN_SECTION` | **`moderation` exists** |

That is a registry-enumeration oracle — precisely the disclosure §10.3 forbids. **The
distinction is invisible in the UI** (this sketch renders both identically); **the leak is
on the wire**, which is exactly where nobody looks during a UI review.

### No invariant pins it

§13 declares eight invariants — `INV-ACCESS-10/11/12`, `INV-PRIVACY-9/10`,
`INV-WORLD-5/6/7`. **None covers admin-section existence opacity.** Yet the pattern already
exists in the same document: `INV-PRIVACY-9` does exactly this job for profiles, with a
binding strategy already written. It was simply not applied here.

### Severity — calibrated down, but still real

An inflated finding wastes reviewer time, so state it accurately:

- The seven section ids are **in the SPEC, in a public repo**. Not secret.
- The web client mirrors them to render nav, so they ship **in the client bundle** to every
  user regardless.

So the oracle mostly discloses what is already public. It is still worth fixing for three
reasons that do not depend on exploitability:

1. **§10.3 asserts a property the system does not have.** A spec stating something untrue
   is a defect regardless of blast radius, because a later author will build on the claim.
   *This is the primary reason.*
2. **The authoritative registry is core-side** (§10.1); the client mirror is *derived*. A
   section added core-side before the client ships it **would** be disclosed by the oracle
   and by nothing else.
3. **It is a built oracle.** Cheap to not have.

Characterize it to `abac-reviewer` as a **spec-consistency defect with a latent disclosure
channel**, *not* an active registry leak.

### Suggested resolution (Phase 2 or 6)

- **(a) Collapse the codes.** An unauthorized caller receives `DENY_ADMIN_SECTION` for
  *both* cases; `DENY_ADMIN_SECTION_UNREGISTERED` is reserved for callers the gate already
  permitted (where the registry is not a secret from them anyway). Keeps §10.4's diagnostic
  value while closing the oracle.
- **(b) Add the invariant.** A new `INV-ACCESS-<n>` mirroring `INV-PRIVACY-9`: the denial
  for an unregistered id is indistinguishable from the denial for a registered one across
  status, error code and body — bound by a test asserting exactly that pair.

**(a) and (b) are complementary; (b) is what makes (a) stay true.**

## Finding 2 — variant C does not generalize

Five of the six planned sections have **no documented scope anywhere in the SPEC**:

| Section | Documented scope |
| --- | --- |
| `config` | §10.1 + §14 — *"The visibility-configuration editor of §8.12 is this section's first tenant"*; v0.13 ships model + seeded defaults only, so wiring it is *"a body replacement, not new wiring"* |
| `stats`, `players`, `moderation`, `audit`, `plugins` | **none** |

`moderation` gets the closest thing to a hint — §7 notes profile warnings need *"somewhere
to live before moderation exists (EXT-06)"* — but that is a statement about a different
feature's dependency, not a scope.

So a "what will live here" panel is writable for **one** section and would be **invented
for five**. Same failure mode as the fabricated `last seen` column.

**If C is ever wanted, it must be conditional** — render the scope panel only where
documented scope exists, fall back to A elsewhere. **Never write speculative scope into the
UI; a planner will read it back as a requirement.**

## HTML Structures

The minimal permitted state (variant A):

```html
<div class="state">
  <div class="glyph"><svg width="20" height="20"><use href="#{s.icon}"/></svg></div>
  <h2>{s.label}</h2>
  <p>Registered and gated. No handler yet.</p>
</div>
```

The non-admin nav — no sections, no hint that any exist:

```html
<div class="muted" style="padding:14px 10px;font-size:12px;line-height:1.6">
  No sections available.
</div>
```

---

# The ordinary not-found (sketch 010)

Winner: **B — + where you can go**.

## The property, stated precisely

> **Indistinguishability is per-viewer, not global.**

It has never been required that an admin and a stranger see the same thing — an admin
hitting `/admin/moderation` **should** get 003's `Registered and gated. No handler yet.`
That is the gate **permitting** them, which is the system working.

What is required is that **one viewer cannot tell which kind of nothing they hit.**

> **Getting this precision wrong in either direction is a design error: too weak and you
> leak; too strong and you would be forbidden from ever showing an admin their own screen.**

## Design Decisions

### B — a dead end offers a way out, at zero disclosure cost

Same head as A (glyph, `Not found`, one line), but the single button becomes a short list of
**the viewer's own available destinations**.

That costs nothing in disclosure: those destinations are the sections **already drawn in
that viewer's rail**, already filtered by the same `requiresPlayer` gate
(`nav/sections.ts:41-47`). A stranger and a player see different lists **because they have
different sections** — not because the page inferred anything about what they were reaching
for.

**Rejected:**

- **A — Minimal.** One glyph, one line, one button. Mirrors 003's restraint. Loses only
  because B's list is free.
- **C — In-shell.** For a signed-in viewer the not-found renders **inside** the app shell so
  they never lose their place. **Rejected — and the rejection is structural, not aesthetic.**
  Rendering in-shell forces a **nested `+error.svelte` boundary**, and that reopens exactly
  the risk this page exists to close.

### A single root `+error.svelte` is the safer shape

SvelteKit resolves the **nearest** error boundary. Once boundaries are nestable, someone
later adds `routes/admin/+error.svelte` and **the indistinguishability dies silently, with
no test failing.** That is a one-file PR that looks harmless.

> **Phase-6 note: ship a meta-test asserting there is exactly *one* `+error.svelte` under
> `web/src/routes/`.** The property this page carries cannot be defended by review alone.

### The copy is `Home` — never the platform brand

The first draft's button read **"Back to HoloMUSH"**. That is wrong twice over:

1. **HoloMUSH is the platform, not the game.** `.claude/rules/branding.md` **INV-6** is
   explicit that the brand is *"the software/platform only — never the game world / default
   setting"*. A player is in *a game that runs on* HoloMUSH.
2. **The game's name is not the platform's to assume.** It exists —
   `SettingConfig.DisplayName`, a **required** field on setting-type plugins
   (`internal/plugin/manifest.go:211`) — **but it is not reachable from the web client.** No
   RPC carries it, no `Web*` response has the field, and the only `HoloMUSH` strings under
   `web/src/` are SPDX copyright headers.

**Resolution: the copy is `Home`.** Viewer-agnostic, no new surface, conflates nothing. The
`>holomush_` wordmark stays in the top bar — that is platform chrome, which INV-6 permits.

> **The copy does not vary by viewer; only the number of *sections* under it does.** That
> closes the open question 003 left ("the affordance still needs a sensible target per
> viewer").

**Carried forward as a gap:** the game's display name is **server-side-only**. Any
player-facing game identity — a title tag, an OG card, a welcome line, this button — needs
it exposed first. Not specific to this page; worth its own issue.

### The copy says nothing about *why*

`We couldn't find that page.` — no search, no suggestion, and **it does not echo the path
back.** Echoing `/admin/moderation` at a prober **confirms the string was routed
somewhere.**

## ⚠ The negative control — this is the part to copy

The fingerprint check 010 ships has an obvious weakness: **in a correct implementation the
renderer does not branch on which kind of miss occurred**, so the panels are identical *by
construction* and the check passes trivially.

> **A check that cannot fail proves nothing.** This is the *verification-that-cannot-fail*
> family this codebase keeps rediscovering — `oops.Code`, the fabricated `last seen` column,
> the escalation test with no positive control. See `anti-patterns.md` §3.

So **the sketch ships the failure.** A `☣ inject leak` toggle makes `/admin/moderation`
render a bespoke *"Access denied — you don't have permission to view this area"* page. The
fingerprints diverge, the panels lose their match border, and the note flips to a disclosure
warning. **Verified both directions: identical inputs → green; one panel changed → red.**

And the injected page is the point:

> **That "Access denied" page is the single most natural thing an implementer would write.**
> It is polite, it is helpful, it is what every other web app does — and it hands a prober
> the fact that `/admin/*` exists and they are merely excluded.

**Phase 6 should carry the same shape into its test**: assert the four paths render
identically **and** demonstrate the assertion goes red against a deliberately-divergent
render.

### A real 404 status is not available — and that is a strength

`web/svelte.config.js` uses `adapter-static` with `fallback: 'index.html'`, so **every**
route returns HTTP 200 + `index.html` and the client router decides. So
`/admin/moderation` and `/blahblah` are identical **at the HTTP layer by construction**,
with no per-route work to keep them that way. The indistinguishability only has to be
maintained in the **client render** — which is exactly what the fingerprint watches.

## HTML Structures

The not-found body (B). Note it takes **no reason parameter**:

```html
<!-- The renderer receives NOTHING about why it is rendering. There is
     deliberately no `reason` prop for a future change to surface. -->
<div class="nf">
  <div class="glyph"><svg width="20" height="20"><use href="#i-nf"/></svg></div>
  <h1>Not found</h1>
  <p>We couldn't find that page.</p>   <!-- no path echo, no search, no why -->

  <nav class="nf-dests">
    <a href="/">Home</a>
    <!-- the viewer's OWN sections — same requiresPlayer gate as the rail -->
    {#each SECTIONS.filter(visibleTo(viewer)) as s}
      <a href={s.href}>{s.label}</a>
    {/each}
  </nav>
</div>
```

```css
.nf {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  text-align: center; gap: 9px; padding: 70px 20px;
}
.nf .glyph {
  width: 44px; height: 44px; border-radius: 11px;
  display: flex; align-items: center; justify-content: center;
  background: var(--color-secondary); color: var(--color-status-text);
  border: 1px solid var(--color-border);
}
```

## Open questions this leaves for Phase 6

1. **Does the client know *why* it is rendering not-found?** It often will (an RPC returned
   a denial). **It must not act on it** — the safe rule is that the renderer takes **no
   reason parameter at all**, so there is nothing for a future change to accidentally
   surface.
2. **Telemetry.** An operator reasonably wants to know how often gated deep links are hit.
   **Logging that server-side is fine; deriving it client-side from which page rendered is
   fine too** — but any **response-shaped** difference (a header, a status, a body field)
   reopens the oracle. State this before someone adds an analytics event keyed on refusal
   type.

## What to Avoid

- **Redirecting a denied deep link.** A redirect is distinctive; it confirms the route
  family exists. Render the ordinary not-found.
- **A bespoke `/admin` not-found page.** Kills the indistinguishability the design rests on.
- **A "Forbidden" / "You do not have permission" page.** Same disclosure, louder.
- **Treating the route guard as the boundary.** It is UX. The ABAC gate on `admin_section:*`
  is the boundary, and every admin RPC must still deny independently.
- **A template `{#if}` to hide the nav entry.** Filter the registry contract.
- **Speculative "coming soon" / scope copy** for the five undocumented sections.
- **An authorization trace in front of an operator.** Prove the ordering with the §10.2
  denial test.
- **A second `+error.svelte`.** One root boundary. A nested one destroys the property
  silently with no test failing — ship the meta-test asserting exactly one.
- **Echoing the requested path** on the not-found page. It confirms the string was routed.
- **Passing a *reason* to the not-found renderer.** If it has nothing, a future change
  cannot surface it.
- **`Back to HoloMUSH` (or any platform-brand copy) in player-facing text.** INV-6 — the
  brand is the software, never the game world. The copy is `Home`.
- **Requiring *global* indistinguishability.** It is **per-viewer**. An admin seeing their
  own gated section resolve is the gate working, not a leak.
- **A fingerprint/opacity test never observed failing.** Demonstrate it goes red against a
  deliberately-divergent render.
- **A response-shaped telemetry signal** for refusal type — a header, a status, or a body
  field reopens the oracle. Log it server-side instead.

## Origin

Synthesized from sketches: **003** (gating and the planned-section state), **010** (the
not-found page it depends on), **007** (which adds the third dependency on that page)

Source files:
`sources/003-planned-section-empty/index.html` (two pickers: section — all six planned plus
Characters and an `(unregistered id)`; and viewer — `admin` / `non-admin`. The two denied
panels are byte-identical **by requirement**.) ·
`sources/010-not-found-page/index.html` (**opens in compare mode**, all four paths side by
side, each carrying a fingerprint of its own rendered markup — **then press `☣ inject leak`
and watch the gate go red**) ·
`sources/007-public-profile-viewer-tiers/index.html` (the players-only posture at the
`anonymous` tier renders this same page, per §8.7)
