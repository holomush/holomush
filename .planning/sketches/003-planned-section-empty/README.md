---
sketch: 003
name: planned-section-empty
question: "What does 'registered and gated, no handler yet' look like without reading as a dead end or a coming-soon vacancy?"
winner: "A"
raises_spec_defect: "10.3-vs-10.4-denial-code-disclosure"
tags: [empty-state, extensibility, abac, denial, phase-6, spec-defect]
---

# Sketch 003: Planned Section — Empty State

## Design Question

Six of the seven admin sections have no handler. §10.3 requires that they
**gate first, then refuse** — and that a non-admin is *denied*, not told the
section is unimplemented. So there are really two states to design, and only one
of them is the friendly one:

- **Permitted + no handler** — an admin who passed the gate. Must read as
  deliberate reserved capacity, not as a broken page.
- **Denied** — must reveal *nothing*: not the section's name, not that it exists,
  not what is being built.

## How to View

```
open .planning/sketches/003-planned-section-empty/index.html
```

Two pickers, top-right: **section** (all six planned, plus Characters and an
`(unregistered id)`) and **viewer** (`admin` / `non-admin`). The variant tabs
apply to the *permitted* state only — by design, the denied state is identical
in all three.

## Decision — `/admin` is invisible without permission

**Maintainer call (2026-08-01):** a viewer without permission sees no `/admin` at
all — **no rail icon, no nav entry** — and a **deep link is bounced**.

This is not a deviation. ROADMAP Ph6 SC5 already says *"the roles exposed on
`WebCheckSessionResponse` change only what is drawn"*, and §10.4's own wording
treats a `+layout.ts` redirect as **UX, not the boundary**. So:

| Layer | Behavior | Status |
| --- | --- | --- |
| Rail + nav | Admin is not drawn, filtered from the registry contract (never a template `{#if}`) | Ph6 SC5 |
| Deep link `/admin/moderation` | Renders the **ordinary not-found page** — the same one `/blahblah` gets | new, this sketch |
| The actual boundary | ABAC gate on `admin_section:*`; every admin RPC still denies independently | §10.4 |

### Not-found, not a redirect — and why

A redirect to `/terminal` is a **distinctive** response. If `/admin/moderation`
bounces while `/nonsense` renders not-found, the bounce itself confirms
`/admin/*` is a real route family — the prober learns the area exists and they
are merely excluded. A not-found makes the two **indistinguishable**, which is
`INV-PRIVACY-9`'s pattern ("identical to the response for a character id that
does not exist") applied at the route layer.

**"404" here means a client-rendered page, not an HTTP status.** `web/svelte.config.js`
uses `adapter-static` with `fallback: 'index.html'`, so **every** unknown path
already returns **HTTP 200 + `index.html`** and the client router decides what to
render. A real 404 status is not available for any route in this app.

That is not a weakness for this property — it is a strength. Because every route
resolves through the same fallback, `/admin/moderation` and `/blahblah` are
identical at the HTTP layer *by construction*, with no per-route work to keep
them that way.

**Gap:** there is no `+error.svelte` anywhere under `web/src/routes/` today. The
not-found page this design depends on **does not exist yet** and is Phase 6 work.

**The redirect must not be mistaken for enforcement.** §10.4 is explicit that a
route guard is *"bypassable by any caller who skips the route"*. SC5's rule —
*"drawing a link the viewer may not use still results in a denial at the RPC"* —
has an inverse that matters just as much: **not drawing the link does not remove
the need for that denial.**

A consequence worth stating plainly: because the nav is hidden and deep links
render not-found, **the only callers still reaching the denial path are those
deliberately bypassing the UI.** That does not mitigate D1 below — it concentrates
it onto exactly the population the leaky codes disclose to.

## Variants (permitted state)

- **A: Minimal ★ WINNER** — glyph, section name, `Registered and gated. No handler
  yet.` Nothing else. Least to write, least to rot. Chosen for restraint: the
  screen's job is to be unremarkable and honest, and B's trace — while the
  strongest *proof* of gate-then-refuse ordering — puts implementation detail in
  front of an operator who did not ask for it. The ordering is better proven by
  the §10.2 denial test than by narrating itself in the UI.
- **B: Gate provenance** — adds an authorization trace: subject, action,
  `admin_section:<id>`, policy, `PERMIT`, then `NOT_IMPLEMENTED`. Makes the
  gate-then-refuse *ordering* visible to the operator.
- **C: Scope preview** — says what will live here. **Try this one across
  sections** — it is the variant that fails, and it fails visibly.

## What to Look For

1. **Flip viewer → non-admin, then flip section between `moderation` and
   `(unregistered id)`.** The two panels are byte-identical. That identity is the
   requirement, not a coincidence — and it is where the SPEC defect below bites.
2. **Variant C across all six sections.** Only `config` has documented scope.
3. **Does B's trace belong in front of an operator?** It is the strongest proof
   that the gate ran first — and the most implementation-detail-forward thing in
   any sketch so far. Sketch 001 rejected exactly this energy (variant A,
   "Registry Ledger") for the nav. The question is whether it earns its place
   *here*, where proving the ordering is the entire point of the screen.

---

# ⚠ SPEC DEFECT — §10.3 and §10.4 disagree

**§10.3** (normative): a non-admin hitting a planned section is denied, and *"the
refusal reveals nothing about which sections exist or what is being built."*

**§10.4** (normative, same section): denial codes are
`DENY_ADMIN_SECTION` when the ABAC decision denies, and
`DENY_ADMIN_SECTION_UNREGISTERED` when the section id is not in the registry.

**These cannot both hold if the two codes are distinguishable to the caller.**
A non-admin probes:

| Probe | Code returned | What the caller learns |
| --- | --- | --- |
| `admin_section:zzz` | `DENY_ADMIN_SECTION_UNREGISTERED` | not a real section |
| `admin_section:moderation` | `DENY_ADMIN_SECTION` | **`moderation` exists** |

That is a registry-enumeration oracle, which is precisely the disclosure §10.3
forbids. The distinction is invisible in the UI (this sketch renders both
identically) — **the leak is on the wire**, which is exactly where nobody looks
during a UI review.

### There is no invariant pinning it

§13 declares eight invariants — `INV-ACCESS-10/11/12`, `INV-PRIVACY-9/10`,
`INV-WORLD-5/6/7`. **None covers admin-section existence opacity.**

The pattern already exists in the same document. `INV-PRIVACY-9`
(profile-reachability opacity) reads: *"a profile below its reachability floor
returns a not-found-equivalent whose wire shape is identical to the response for
a character id that does not exist. Bound by a test asserting the two responses
are indistinguishable across status, error code and body."*

That is the exact property admin sections need, already written, already given a
binding strategy — and simply not applied here.

### Severity — calibrated down, but still real

An earlier draft of this README implied the exploit value is registry disclosure.
On reflection that overstates it, and an inflated finding wastes reviewer time:

- The seven section ids are **in the SPEC, in a public repo**. They are not secret.
- The web client mirrors them to render nav, so they ship **in the client bundle**
  to every user regardless. Anyone can read them without probing anything.

So the oracle mostly discloses what is already public. The finding is still worth
fixing, for three reasons that do not depend on exploitability:

1. **§10.3 asserts a property the system does not have.** A spec that states
   something untrue is a defect regardless of blast radius, because a later
   author will build on the claim. That is the primary reason.
2. **The authoritative registry is core-side** (§10.1); the client mirror is
   *derived*. A section added core-side before the client ships it **would** be
   disclosed by the oracle and by nothing else.
3. **It is a built oracle.** Cheap to not have.

Characterize it to `abac-reviewer` as a **spec-consistency defect with a latent
disclosure channel**, not as an active registry leak.

### Suggested resolution (Phase 2 or 6)

One of:

- **(a) Collapse the codes.** An unauthorized caller receives
  `DENY_ADMIN_SECTION` for *both* cases; `DENY_ADMIN_SECTION_UNREGISTERED` is
  reserved for callers the gate already permitted (where the registry is not a
  secret from them anyway). This keeps §10.4's diagnostic value for operators
  while closing the oracle.
- **(b) Add the invariant.** A new `INV-ACCESS-<n>` mirroring `INV-PRIVACY-9`:
  the denial for an unregistered id is indistinguishable from the denial for a
  registered one across status, error code and body — bound by a test that
  asserts exactly that pair.

(a) and (b) are complementary; (b) is what makes (a) stay true. Note this is an
**ABAC-surface finding**, so it is squarely in `abac-reviewer` territory — worth
raising there rather than only in a sketch README.

---

# Finding 2 — variant C does not generalize

Five of the six planned sections have **no documented scope anywhere in the
SPEC**. Only `config` does:

| Section | Documented scope |
| --- | --- |
| `config` | §10.1 + §14: *"The visibility-configuration editor of §8.12 is this section's first tenant"* — v0.13 ships the model and seeded defaults only, so wiring it is *"a body replacement, not new wiring"*. |
| `stats`, `players`, `moderation`, `audit`, `plugins` | **none** |

`moderation` gets the closest thing to a hint — §7 notes that profile warnings
need *"somewhere to live before moderation exists (EXT-06)"* — but that is a
statement about a different feature's dependency, not a scope for the section.

So a "what will live here" panel is writable for **one** section and would be
**invented for five**. That is the same failure mode as the fabricated `last
seen` column in sketch 001/002: plausible-sounding UI copy that no source
supports. The sketch renders that honestly — `config` shows real content, the
other five show an explicit "no documented scope" panel — so the variant argues
against itself rather than needing a paragraph to explain why it loses.

**Recommendation:** if C is wanted, it should be **conditional** — render the
scope panel only where documented scope exists, and fall back to A elsewhere.
Never write speculative scope into the UI; a planner will read it back as a
requirement.

## Components this implies adding

No new ones beyond sketch 001's list. This exercises `empty` and `alert`.

## Resolved

**Where the denial renders** — settled by the decision above. There is no in-frame
"denied" panel, because a non-admin never reaches the admin chrome: the nav is
not drawn and a deep link bounces to `/terminal`. The sketch's non-admin view now
shows that outcome rather than a denial page.

## Open questions

1. **The not-found page does not exist.** No `+error.svelte` anywhere under
   `web/src/routes/`. Phase 6 must build one, and it must be the *ordinary* one —
   the moment `/admin` gets its own bespoke not-found, the indistinguishability
   this design rests on is gone.
2. **Does the guest path differ?** The not-found is viewer-agnostic, so the
   guest-vs-player question that a redirect design would have raised mostly
   dissolves — but the "← Back to HoloMUSH" affordance on that page still needs a
   sensible target per viewer, and `nav/sections.ts` already carries
   `requiresPlayer` for exactly this class of problem.
