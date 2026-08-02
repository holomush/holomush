# Profile & Viewer Tiers — the public character profile

Validated in sketch **007-public-profile-viewer-tiers**. Winner: **C — Identity card**.

Target phase: **5**. This is the largest surface in v0.13 and the one with the hardest
constraint — and the constraint is **not** the one it looks like from outside.

## ⚠ Read this first: the page may not explain its own sparseness

The obvious framing ("the same page renders three ways, make all three good") is not the
problem. §7.5 and §8.9 together are:

> A blank field **MUST be omitted from the response**, never emitted as an empty value. […]
> it is deliberate that the two cases are indistinguishable to a viewer: **a field the
> character left blank and a field the viewer may not see MUST look identical on the wire.**
>
> A renderer therefore has exactly two states per field — present, or absent — and **MUST
> NOT** infer anything from absence beyond "do not render this."

So the page **cannot**:

- say *"3 fields hidden — sign in to see more"*
- grey out or lock-icon a withheld section
- vary its copy by **how much** was withheld
- show a progress indicator, a section count, or a "there's more" affordance

Every one of those is a disclosure channel. **The design must make a short page feel
intentional while being structurally forbidden from explaining why it is short.**

## Design Decisions

### The sparse view is the *native* form, not a truncated one

**C — Identity card** reshapes the page so that a bounded card reads as complete at any fill
level. Sections **grow below** the card as the viewer clears more floors, rather than the
page reading as a long-form document with holes in it.

This answers the design question **structurally instead of with copy** — which matters
specifically here, because §7.5/§8.9 forbid the copy answer anyway.

**Rejected:**

- **A — Blocks vanish.** One long-form layout; absent fields simply do not render. The
  literal reading of §8.9, and the honest control. It loses because a full-width long-form
  page with three items on it reads as a page that **failed**, not a page that is **short**.
- **B — + unconditional invitation.** A, plus a fixed notice on every profile to every
  signed-out visitor: *"Signed-in players see more on some profiles."* **Rejected — and the
  rejection closes a disclosure trap by construction.** See below.

### Why rejecting B matters more than it looks

B's notice was **legal only because it was unconditional** — same text, every profile, every
signed-out visitor, regardless of whether anything was actually withheld.

The natural later "improvement" — *show the notice only when something was withheld* — is a
**which-characters-have-populated-profiles oracle**. By not shipping the notice at all,
v0.13 never opens the channel.

> **If a later phase wants to reintroduce a sign-in invitation on profiles, it MUST be
> unconditional**, and that constraint MUST be stated where the component lives — not left
> in a sketch README.

### What C commits Phase 5 to

- The card is **the hard floor made visible**: portrait (initial letter — §8.8 guarantees
  there is always a letter), `name`, and `profile.pronouns` are **always** inside it.
- `description`, `concept` and the short facts fill the card **when present**.
- **The card must look deliberate with only name + pronouns in it** — that is the
  players-only-posture anonymous view, and it is reachable in a real configuration.
- Long-form sections (`appearance`, `personality`, `biography`, `rumors`, `rp_preferences`)
  live **outside** the card and simply do not render when absent. No placeholder, no
  heading-without-body.
- The card **must not** grow a "N sections below" affordance, a progress indicator, or
  anything else whose presence varies with how much was withheld.

## ⚠ Finding 1 — under v0.13's seeded defaults, `guest` and `player` render identically

§8.6's table has a "Seeded v0.13 default" column, and **not one row in it is `player`.**
Every row is either `anonymous` (reachability, `name`, `profile.pronouns`, in-world
`description`) or `guest` (everything else).

So the three-rung ladder collapses to **two distinct renderings** in the shipped
configuration.

Two consequences for Phase 5:

1. **A three-option viewer-tier preview would present two identical results.** That is the
   same self-defeating shape sketch 003's variant C had. If Phase 5 builds an
   operator-facing "how does this look to each tier" preview, it must preview **distinct
   outcomes derived from the live floor set**, not a hardcoded three-way toggle.
2. **The `player` rung is unexercised in production configuration.** It is real and
   reachable (the players-only posture uses it), but nothing in the seeded set exercises it
   — so **a bug in the `player` clearing path would not show up by running the default
   game.** Test it directly; do not rely on the default configuration to cover it.

## ⚠ Finding 2 — anonymous sees exactly three things

Under the seeded posture the anonymous view is `name` + `profile.pronouns` + `description`.
That is the entire page. The **12** `profile.*` fields and all **11** media rows are at
`guest`.

That is deliberate, not stinginess. §8.11 records that the description at `anonymous` is
**more open than strict grid-parity on purpose**, precisely so that *"a profile that renders
blank to every logged-out visitor is not a profile page, it is a login wall"*.

> **The description is doing all of the work of making the anonymous view worth having.**
> If a game raises the description floor to `player` — one row of §8.6 — **the anonymous
> view becomes a name and a pronoun.** The card must still read as deliberate there.

## ⚠ Finding 3 — the gallery can never contain an image in v0.13

§7.3 ships *"the schema and the proto shape only, with **zero upload behavior**. There is no
uploader, no storage backend, and no media-serving path."*

So in a real v0.13 deployment **no media rows exist** — which means, by §8.9's absence rule,
**the Gallery section never renders for anyone.** The sketch shows it only because its
fixture asserts media rows exist, to prove the model has a rendering.

> **Do not build empty dashed "coming soon" slots.** That is the same speculative-scope
> mistake sketch 003's variant C made, one layer down. Build the renderer so media rows
> *would* render if present, and ship a page that shows nothing when they are not.

## The clearing test — set membership, never ordinal compare

The sketch's own `clears()` uses a numeric rank **for rendering convenience only**:

```js
// SKETCH-ONLY. Do NOT copy into production.
const RANK = { anonymous: 0, guest: 1, player: 2 };
function clears(viewer, floor) { return RANK[viewer] >= RANK[floor]; }
```

§8.2.1 requires the **real** clearing test to be **explicit set membership over the tier
token**. Memory `cbr4sd4pch` records why: the DSL's `compareStrings`
(`dsl/evaluator.go:185-201`) is **Go byte order**, so an ordinal ladder holds only by
alphabetical accident — `anonymous < guest < player` sorts correctly *by luck*, while a
later token like `spectator` or `visitor` would sort **above** `player` and silently clear
floors it must not.

Use `evalInList` semantics (`dsl/evaluator.go:317-336`, **false on unresolved LHS**) so an
appended tier clears nothing until it is explicitly added.

## CSS Patterns

The identity card (variant C's only structural addition — everything else is shared):

```css
/* C only: card shape. The page body is unpadded; the card and each
   long-form section carry their own margin, so sections "grow below"
   the card rather than sharing a document flow with it. */
.v-c .prof { padding: 0; }
.v-c .card {
  margin: 18px;
  border: 1px solid var(--color-border);
  border-radius: 12px;
  background: var(--color-card);
  padding: 18px;
}
.v-c .sec  { margin: 18px 18px 0; }
.v-c .desc { margin-top: 13px; }
```

Portrait — initial letter, guaranteed to exist by §8.8:

```css
.portrait {
  width: 78px; height: 78px; border-radius: 12px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  font-size: 32px; font-weight: 700;
  background: color-mix(in srgb, var(--color-primary) 16%, transparent);
  color: var(--color-primary);
  border: 1px solid color-mix(in srgb, var(--color-primary) 32%, transparent);
}
```

Short facts as pills — these are the fields that fill the card when present:

```css
.facts { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 13px; }
.fact {
  display: inline-flex; align-items: baseline; gap: 6px;
  padding: 4px 9px; border-radius: 999px;
  border: 1px solid var(--color-border); background: var(--color-card);
  font-size: 11.5px;
}
.fact b {
  color: var(--color-status-text); font-weight: 500; font-size: 10.5px;
  text-transform: uppercase; letter-spacing: .04em;
}
```

Long-form section heading — rendered **only** when the body is present:

```css
.sec h3 {
  font-size: 11px; text-transform: uppercase; letter-spacing: .07em;
  font-weight: 600; color: var(--color-status-text);
  margin: 0 0 7px; padding-bottom: 5px;
  border-bottom: 1px solid var(--color-border);
}
```

## HTML Structures

The card — hard floor first, optional content after, **no placeholders**:

```html
<div class="card">
  <div class="idhead">
    <div class="portrait">{name[0]}</div>       <!-- §8.8: always a letter -->
    <div class="idtext">
      <h1 class="cname">{name}</h1>              <!-- hard floor -->
      <div class="pron">{pronouns}</div>         <!-- hard floor -->
      {#if concept}<div class="concept">{concept}</div>{/if}
    </div>
  </div>

  {#if description}<p class="desc">{description}</p>{/if}

  {#if facts.length}
    <div class="facts">
      {#each facts as f}<span class="fact"><b>{f.label}</b>{f.value}</span>{/each}
    </div>
  {/if}
</div>

<!-- Long-form sections grow BELOW the card. Absent ⇒ nothing renders,
     not an empty heading. -->
{#each longSections as s}
  {#if s.body}
    <div class="sec"><h3>{s.label}</h3><p>{s.body}</p></div>
  {/if}
{/each}
```

## What to Avoid

- **Any count, lock icon, greyed section, or "N hidden" affordance.** Every one is a
  blank-vs-withheld oracle (§7.5, §8.9).
- **A *conditional* sign-in invitation.** Unconditional is legal; "show it only when
  something was withheld" is the bug. Prefer shipping no notice at all.
- **A hardcoded three-way tier preview.** Under seeded defaults two of the three panels are
  identical — derive distinct outcomes from the live floor set instead.
- **Ordinal tier comparison.** Use set membership (§8.2.1); the DSL comparator is byte order.
- **Empty "coming soon" gallery slots.** No media rows can exist in v0.13; the section
  simply never renders.
- **A "this profile is private" page** for an unreachable character. §8.7 requires the
  **ordinary not-found** — a distinct private response discloses that the character exists.
  See `gating-and-absence.md`.
- **Inferring anything from absence** beyond "do not render this."

## Open questions this leaves for Phase 5

1. **Is there signed-out chrome at all?** The sketch invented a `Sign in` / `Play as guest`
   bar. **No SPEC section describes logged-out web chrome**, and the existing app shell is
   `(authed)`-only. This is a real gap, not a detail.
2. **Does the profile URL use the character name or the ULID?** Name-based URLs are
   shareable and are the point of a public profile — but character names have **no DB
   uniqueness constraint** until Phase 2 and are **renameable** after (see
   `player-roster-and-creation.md`). A name is not a key. Settle this before routing
   anything.
3. **Does `profile.currently` need a freshness signal?** §7.2 says it *"carries no history"*
   and is *"expected to change often"*. A stale "Currently" is worse than none — but a
   timestamp is a new field, and §8.6's totality rule means a new governed attribute needs
   its own floor row.

## Grounding

| Element | Source |
| --- | --- |
| The 12 `profile.*` fields, exact names and long/short shapes | SPEC §7.2 |
| `profile.image.primary` + `gallery.00`–`.09`, **zero upload behavior** | SPEC §7.3 |
| `name` and `description` are `characters` **columns**, not property rows | SPEC §7.1, §8.6 note |
| Description always public on the profile, no per-owner control | SPEC §7.4 |
| Blank ≡ withheld; two states per field; infer nothing from absence | SPEC §7.5, §8.9 |
| Ladder `anonymous < guest < player`; **set membership, never ordinal** | SPEC §8.2, §8.2.1 |
| The configuration surface and the seeded column | SPEC §8.6 |
| Unreachable ⇒ not-found-equivalent, never "private" | SPEC §8.7 |
| Name + pronouns are a hard floor; guarantees the portrait has a letter | SPEC §8.8 |
| Description at `anonymous` deliberately exceeds grid-parity | SPEC §8.11 |
| No editing surface for visibility config in v0.13 | SPEC §8.12 |
| DSL `compareStrings` is Go byte order; use `evalInList` | memory `cbr4sd4pch` |

## Components

Nothing mandatory beyond the running list. The profile is typography plus `badge`
(installed) and `separator` (installed); `avatar` (already on 001's list) covers the
initial-letter portrait.

## Origin

Synthesized from sketch: **007**
Source file: `sources/007-public-profile-viewer-tiers/index.html`
(three pickers — viewer / posture / data — plus a `⇄ compare tiers` toggle that renders all
three viewer tiers side by side. **The compare view is the point**: Finding 1 is *visible*
there rather than argued.)
