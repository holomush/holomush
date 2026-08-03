---
sketch: 007
name: public-profile-viewer-tiers
question: "One profile renders differently to anonymous, guest and player under game-operator per-attribute floors — and the page may not say what is missing. How does the sparse view avoid reading as broken?"
winner: "C"
tags: [profile, privacy, abac, viewer-tiers, empty-state, phase-5, frontier]
---

# Sketch 007: Public Profile — Viewer Tiers

## Design Question

This is the largest unsketched surface in v0.13 and the one with the hardest
constraint, which is **not** the one it looks like from outside.

The obvious framing — "the same page renders three ways, make all three good" —
is not the problem. The problem is §7.5 and §8.9 together:

> A blank field **MUST be omitted from the response**, never emitted as an empty
> value. […] it is deliberate that the two cases are indistinguishable to a
> viewer: **a field the character left blank and a field the viewer may not see
> MUST look identical on the wire.** If they differed, the response shape itself
> would disclose which fields exist but are withheld.
>
> A renderer therefore has exactly two states per field — present, or absent —
> and **MUST NOT** infer anything from absence beyond "do not render this."

So the page **cannot** say *"3 fields hidden — sign in to see more"*, cannot grey
out withheld sections, cannot show a lock icon, and cannot vary its copy by how
much was withheld. Every one of those is a disclosure channel. **The design must
make a short page feel intentional while being structurally forbidden from
explaining why it is short.**

## How to View

```
open .planning/sketches/007-public-profile-viewer-tiers/index.html
```

Three pickers (viewer / posture / data) plus a **`⇄ compare tiers`** toggle that
renders all three viewer tiers side by side. The compare view is the point —
several of the findings below are *visible* rather than argued.

## Variants

- **A: Blocks vanish** — one long-form layout; absent fields simply do not
  render. Nothing anywhere acknowledges that anything is missing. The literal
  reading of §8.9.
- **B: + unconditional invitation** — A, plus a fixed notice shown to **every**
  signed-out visitor on **every** profile: *"Signed-in players see more on some
  profiles."* Never counted, never conditional on this character, never varying.
- **C: Identity card ★ WINNER** — reshapes the page so the sparse view is the *native*
  form rather than a truncated one: a complete identity card (portrait, name,
  pronouns, concept, description, facts) that sections *grow below* as the viewer
  clears more floors.

## ⚠ Two findings that change the shape of this problem

### 1. Under v0.13's seeded defaults, `guest` and `player` render identically

§8.6's table has a "Seeded v0.13 default" column, and **not one row in it is
`player`.** Every row is either `anonymous` (reachability, `name`,
`profile.pronouns`, in-world `description`) or `guest` (everything else).

So the three-rung ladder collapses to **two distinct renderings** in the shipped
configuration. Turn on `⇄ compare tiers` with the seeded posture: **the middle and
right panels are the same page.**

This matters for Phase 5 in a concrete way: **a viewer-tier control with three
options would present two identical results**, which is the same self-defeating
shape sketch 003's variant C had. If Phase 5 builds an operator-facing preview of
"how this profile looks to each tier", it should preview *distinct outcomes*
derived from the live floor set, not a hardcoded three-way toggle.

It also means the `player` rung is currently **unexercised in production
configuration**. It is real and reachable (the players-only posture uses it), but
nothing in the seeded set will exercise it — so a bug in the `player` clearing
path would not show up by running the default game.

### 2. Anonymous sees exactly three things

Under the seeded posture the anonymous view is `name` + `profile.pronouns` +
`description`. That is the entire page. The **12** `profile.*` fields and all
**11** media rows are at `guest`.

That is not a bug and not stinginess — §8.11 records that the description at
`anonymous` is **deliberately more open than strict grid-parity**, precisely so
that "a profile that renders blank to every logged-out visitor is not a profile
page, it is a login wall". The description is doing all of the work of making the
anonymous view worth having. **If a game raises the description to `player` (one
row of §8.6), the anonymous view becomes a name and a pronoun.** Try the
players-only posture at the `anonymous` tier to see the far end of that.

## What to Look For

- **A at `anonymous`, seeded.** Three items on a full-width page. Does it read as
  a deliberate summary, or as a page that failed to load? A is the honest control:
  if it reads fine, B's notice is ceremony and C's reshaping is unnecessary work.
- **B's notice is the interesting one, and the reason it is legal is subtle.**
  A *counted* or *conditional* invitation ("3 fields hidden", or showing the
  notice only when something was actually withheld) is a **disclosure channel** —
  it tells an anonymous prober which characters have populated profiles. B's
  notice is safe **only because it is unconditional**: same text, every profile,
  every signed-out visitor, regardless of whether anything was withheld. Does
  that read as helpful context, or as noise on a profile that had nothing hidden
  anyway? **If B is adopted, the unconditionality is load-bearing and must be
  stated in the Phase-5 plan** — the natural "improvement" of only showing it when
  something is withheld is precisely the bug.
- **C at `anonymous` vs C at `guest`.** C's bet is that a bounded card reads as
  complete at any fill level, where a long-form page reads as truncated. Does the
  card still hold when the viewer clears everything and five long sections appear
  below it, or does the card then look vestigial?
- **`data: zero profile rows` at `guest`, against `data: full profile` at
  `anonymous`.** These are two entirely different situations — a character who
  wrote nothing, and a viewer who may see nothing — and the SPEC **requires** them
  to be indistinguishable. Confirm the sketch honors that. **If any variant lets
  you tell them apart, that variant is disqualified, not merely disfavored.**
- **The players-only posture at `anonymous`.** You get the ordinary **not-found**,
  not a "this profile is private" page (§8.7), because a distinct private response
  discloses that the character exists. Note this is the *same* page sketch 010 is
  about and the *same* property sketch 003 relied on for `/admin` — three
  independent surfaces now depend on one not-found page that **does not exist**
  (#4903).
- **The Gallery section.** See below — it is arguably wrong to render at all in
  v0.13.

## Decision (2026-08-01)

**C wins.** The sparse view becomes the **native** form rather than a degraded
one: a bounded identity card that is complete at any fill level, with sections
growing below it as the viewer clears more floors. This answers the design
question structurally instead of with copy — which matters here specifically,
because §7.5/§8.9 forbid the copy answer anyway.

**B is rejected, and that closes a disclosure trap by construction.** B's notice
was legal *only* because it was unconditional; the natural later "improvement" —
showing it only when something was actually withheld — is a
which-characters-have-populated-profiles oracle. By not shipping the notice at
all, v0.13 never opens the channel.

> **If a later phase wants to reintroduce a sign-in invitation on profiles, it
> MUST be unconditional** — same text, every profile, every signed-out visitor,
> independent of whether anything was withheld — and that constraint MUST be
> stated where the component lives, not left in this README.

**A is rejected** as the thing C improves on: a full-width long-form page with
three items on it reads as a page that failed rather than a page that is short.

### What C commits Phase 5 to

- The card is the **hard floor made visible**: portrait (initial letter — §8.8
  guarantees there is always a letter), name, and pronouns are always inside it.
- `description`, `concept` and the short facts fill the card when present; the
  card must look deliberate with **only** name + pronouns in it, because that is
  the players-only-posture anonymous view.
- Long-form sections (`appearance`, `personality`, `biography`, `rumors`,
  `rp_preferences`) live **outside** the card and simply do not render when
  absent. No placeholder, no heading-without-body.
- The card must not grow a "N sections below" affordance, a progress indicator,
  or anything else whose presence varies with how much was withheld.

## Finding 3 — the gallery can never contain an image in v0.13

§7.3: v0.13 ships "the schema and the proto shape only, with **zero upload
behavior**. There is no uploader, no storage backend, and no media-serving path."

So in a real v0.13 deployment **no media rows exist**, which means — by §8.9's
absence rule — the Gallery section never renders for anyone. The sketch shows it
only because its fixture asserts media rows exist, to prove the model has a
rendering.

**The honest v0.13 profile has no gallery.** Phase 5 should not build empty
dashed slots as a "coming soon" affordance: that is the same speculative-scope
mistake sketch 003's variant C made, one layer down. Build the renderer so that
media rows *would* render if present, and ship a page that shows nothing when
they are not.

## Grounding

| Element | Source |
| --- | --- |
| The 12 `profile.*` fields, exact names and long/short shapes | SPEC §7.2 |
| `profile.image.primary` + `gallery.00`–`.09`, zero upload behavior | SPEC §7.3 |
| `name` and `description` are `characters` **columns**, not property rows | SPEC §7.1, §8.6 note |
| Description always public on the profile, no per-owner control | SPEC §7.4 |
| Blank ≡ withheld; two states per field; infer nothing from absence | SPEC §7.5, §8.9 |
| Ladder `anonymous < guest < player`; **set membership, never ordinal compare** | SPEC §8.2, §8.2.1 |
| The whole configuration surface and the seeded column | SPEC §8.6 |
| Unreachable ⇒ not-found-equivalent, never "private" | SPEC §8.7 |
| Name + pronouns are a hard floor; guarantees the initial-letter portrait has a letter | SPEC §8.8 |
| Description at `anonymous` deliberately exceeds grid-parity | SPEC §8.11 |
| No editing surface for visibility config in v0.13 | SPEC §8.12 |

**Note on the sketch's own `clears()`.** It uses a numeric rank for rendering
convenience. §8.2.1 requires the *real* clearing test to be **explicit set
membership over the tier token**, never an ordinal comparison — and memory record
`cbr4sd4pch` records that the DSL's `compareStrings` is Go byte order, so an
ordinal ladder would hold only by alphabetical accident. Do not copy this
sketch's rank helper into production.

## Components this implies adding

Beyond the running list: nothing mandatory. The profile is typography, `badge`
(installed) and `separator` (installed). `avatar` (already on 001's list) covers
the initial-letter portrait.

## Open questions this sketch surfaces

1. **Is there a signed-out chrome at all?** The sketch gives anonymous/guest a
   `Sign in` / `Play as guest` bar. That is invented — no SPEC section describes
   the logged-out web chrome, and the existing app shell is `(authed)`-only. This
   is a real Phase-5 gap, not a detail.
2. **Does the profile URL use the character name or the ULID?** The sketch dodges
   it. Name-based URLs are shareable and are the point of a public profile, but
   character names have **no DB uniqueness constraint** (see sketch 009), so a
   name is not a key. This needs settling before Phase 5 routes anything.
3. **Does `profile.currently` need a freshness signal?** §7.2 says it "carries no
   history" and is "expected to change often". A stale "Currently" is worse than
   none, but a timestamp is a new field and §8.6's totality rule means a new
   governed attribute needs its own floor row.
