# Gating & Absence — planned sections, invisibility, denial

Validated in sketch **003-planned-section-empty**. Winner: **A — Minimal**.

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

There is **no `+error.svelte` anywhere under `web/src/routes/`** today. The page this whole
design rests on is Phase 6 work. Tracked as
[#4903](https://github.com/holomush/holomush/issues/4903).

**It must be the *ordinary* one.** The moment `/admin` gets its own bespoke not-found, the
indistinguishability this design rests on is gone.

Secondary: the "← Back to HoloMUSH" affordance on that page needs a sensible target per
viewer. `nav/sections.ts` already carries `requiresPlayer` for exactly this class of
problem.

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

## Origin

Synthesized from sketch: **003**
Source file: `sources/003-planned-section-empty/index.html`
(two pickers: section — all six planned plus Characters and an `(unregistered id)`; and
viewer — `admin` / `non-admin`. The two denied panels are byte-identical **by requirement**.)
