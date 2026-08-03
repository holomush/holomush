# Player Roster & Character Creation — the player's own surfaces

Validated in sketches **008-character-roster-retired** (winner **B — Sectioned**) and
**009-character-create-name-collision** (winner **A — Submit & report**).

Target phases: **2** (name pipeline + unique index) and **3** (roster, rename).

These are the **player's own** surfaces, not operator tools — which is the decision that
drives most of what follows.

## ⚠ Read this first: "names are permanent" is FALSE

Both sketches shipped that copy in an early draft. **It is wrong.**

v0.13 ships **player rename**: **IDENT-03**, implemented as
`CharacterAccessService.RenameCharacter` — owner-scoped, ABAC `write` on `character:<id>`,
SPEC §9.4.2 line 1805 — landing in **Phase 3**, whose own ROADMAP goal line names
`RenameCharacter` explicitly. The rename runs the **same** §6.1 pipeline and collides
against the **same** unique index.

What *is* true, and what the copy now says: **a name is *reserved* once taken**, and stays
reserved even if the character is later retired (§4.4, §4.5).

> This was caught only because 008 wrote down "sketch 009 tests this claim" and 009 actually
> went and checked. It is the worked example in `anti-patterns.md` §10.

**It is not cosmetic.** 009's winning variant **depends on rename existing** — see below.

---

# Part 1 — The roster (sketch 008)

The roster already ships: `web/src/routes/(authed)/characters/+page.svelte:116-153` — a Card
grid with an initial-letter avatar, `Last played`, an optional `At: <location>` and a status
badge. v0.13 adds `characters.status` (`active | retired | idle`), and with it a population
the roster has never had: **characters that are visible but cannot be selected.**

## Design Decisions

### The roster splits into `Playable` / `Not playable`

**B — Sectioned** wins. `Playable` grid first (carrying the create card), then a
`Not playable` section below with a disclosure count.

**The decisive property: every card in the top grid is uniformly clickable.** There is no
mixed-affordance grid where the player must read each card to learn whether it responds.

B also **degrades best**. In the all-retired state the `Playable` section says *in words*
that nothing can be played right now, where A leaves a grid of dead cards and C leaves a
table of dimmed rows.

**Rejected:**

- **A — One grid, in place.** Retired/idle cards stay in registry order, dimmed and
  un-clickable with a `Retired` badge and a reason. The literal generalization of sketch
  004's `idle` idiom (*shown, never offered, with a reason*). It loses because that idiom
  **does not survive the scale change**: in a menu, one dimmed item among three reads as
  deliberate; in a grid, three dimmed cards out of six is a large share of the screen and
  puts an affordance-check burden on every visit.
- **C — Dense list.** Converges on sketch 002's admin table idiom. **Rejected — the player
  roster stays Cards.** Convergence was a real benefit, but this is the player's own roster,
  not an operator tool, and Cards is the idiom that ships today.

> **The portal deliberately carries two idioms: dense table for operator surfaces, Cards for
> the player's own things.** That is a decision, not an inconsistency.

## ⚠ Finding — two "status" vocabularies collide on one card

This is the sketch's main result, and it is not a matter of taste.

The roster **already ships** a status badge, and it is **session** state:
`hasActiveSession → 'Active'`, else `sessionStatus || 'Offline'` (`+page.svelte:132-136`).
v0.13 adds `characters.status`, which is **lifecycle** state.

| | Vocabulary | Values | Means | Changes |
| --- | --- | --- | --- | --- |
| existing | **session** | `Active` / `Offline` | is this character connected right now | minute to minute |
| new (Phase 2) | **lifecycle** | `active` / `retired` / `idle` | may this character be played at all | rarely, deliberately |

They share the word "status" **and the token `active`**.

A retired card renders **"Retired · Offline"** — and `Offline` is *meaningless* on a
character who cannot be played at all. It is not extra information; it is a second status
competing with the first, distinguishable only by knowing which vocabulary each token
belongs to.

> **Rule: a non-`active` lifecycle MUST suppress the session badge entirely.** Session state
> is only meaningful for a character that could be connected.

This is **orthogonal to the A/B/C choice** — it applies to B exactly as much as to A or C.
Adopting B does not resolve it; **Phase 3 must apply it explicitly.**

It is also a `.claude/rules/terminology.md`-class problem: **"status" is now an ambiguous
word in this codebase's UI vocabulary.** The lifecycle values should probably never render
as the bare word `Active` in player-facing UI, precisely because the session badge already
owns that word on the same card. B's section labels (`Playable` / `Not playable`)
deliberately avoid both "status" and "Active" — **keep that constraint even if the labels
are reworded.**

## CSS Patterns

The existing card grid, unchanged (this is what ships today):

```css
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(212px, 1fr)); gap: 11px; }
.ccard {
  border: 1px solid var(--color-border); border-radius: 9px; background: var(--color-card);
  padding: 14px; display: flex; gap: 11px; align-items: flex-start;
  cursor: pointer; transition: border-color 120ms, background 120ms;
}
.ccard:hover  { border-color: var(--color-primary); }
.ccard.dashed { border-style: dashed; }   /* the create card */
```

The section header B adds — a label, a rule, and the disclosure chip:

```css
.sechead {
  display: flex; align-items: center; gap: 8px; margin: 26px 0 9px;
  font-size: 11px; text-transform: uppercase; letter-spacing: .06em;
  color: var(--color-status-text);
}
.sechead:first-of-type { margin-top: 0; }
.sechead .rule { flex: 1; height: 1px; background: var(--color-border); }
.disclose {
  display: inline-flex; align-items: center; gap: 5px; cursor: pointer; user-select: none;
  border: 1px solid var(--color-border); border-radius: 5px; padding: 2px 8px;
}
```

Phone band — the grid goes single-column:

```css
@container vp (max-width: 767px) {
  .grid { grid-template-columns: 1fr; }
}
```

## HTML Structures

The two sections. Note the badge suppression rule is applied in the template, not left to
the data:

```html
<div class="sechead">Playable <span class="rule"></span></div>
<div class="grid">
  {#each playable as c}
    <a class="ccard" href="/play/{c.id}">
      <div class="cavatar">{c.name[0]}</div>
      <div>
        <div class="cname">{c.name}</div>
        <!-- session badge ONLY for an active lifecycle -->
        {#if c.lifecycle === 'active'}
          <span class="badge">{c.hasActiveSession ? 'Active' : 'Offline'}</span>
        {/if}
      </div>
    </a>
  {/each}
  <a class="ccard dashed" href="/characters/new">
    <div>+ Create a character
      <!-- NOT "permanent" — v0.13 ships rename (IDENT-03) -->
      <small>Names are reserved once taken.</small>
    </div>
  </a>
</div>

{#if notPlayable.length}
  <div class="sechead">
    Not playable <span class="rule"></span>
    <button class="disclose">{notPlayable.length}</button>
  </div>
  <div class="grid">
    {#each notPlayable as c}
      <div class="ccard is-locked">
        <div class="cavatar">{c.name[0]}</div>
        <div>
          <div class="cname">{c.name}</div>
          <span class="badge">Retired</span>   <!-- no session badge -->
          <p class="reason">{c.reason}</p>
          <a href="/c/{c.name}">View profile →</a>
        </div>
      </div>
    {/each}
  </div>
{/if}
```

`View profile →` on a locked card is load-bearing: a retired character's profile **still
resolves**, and making that reachable is what keeps "retired" from reading as "deleted".

## What Phase 3 must still settle (roster)

1. **Is `Not playable` collapsed by default?** The sketch always renders it expanded and
   treats the count chip as decoration. These are the player's **own** characters, so the
   inclination is **expanded** with the chip as a collapse control — but that is a decision,
   and the chip's current label ("hidden") presumes the opposite.
2. **Can a player retire their own character?** The sketch offers **no retire action** —
   every retire path in the set is `AdminRetireCharacter`, an operator action, and §9.3's
   admin census does not imply a player-facing equivalent. If players cannot self-retire,
   the roster copy must not imply otherwise.
3. **Does `idle` ever appear here?** It is unreachable in v0.13 (nothing sets it), so a real
   roster shows only `active` and `retired`. The sketch renders an idle card only to prove
   the exhaustive switch has a rendering. **Do not build copy assuming players will see it.**
4. **Ordering.** The sketch keeps registry order. A retired character with a recent
   `Last played` currently floats to the top of a chronological sort, which is probably
   wrong — but sorting is not specified for this surface at all.

---

# Part 2 — Character creation (sketch 009)

Creation is the one place a player's free text becomes an identifier everyone else sees, and
§6.1 puts **seven** gates between the keystroke and the row. Two of them **change the input**
rather than rejecting it; two reject for reasons hard to explain without being vague or
leaking.

## Design Decisions

### Submit & report — never promise what you cannot keep

**A wins.** Type, press create, get the verdict back. It is the shape the roster already
ships and the least Phase-2/3 code.

**Rejected:**

- **B — Live echo + availability.** Echoes the normalized display name and uniqueness key as
  you type, and checks availability. Most teaching, and **the only variant that can be
  wrong** — see Finding 1.
- **C — Two-step confirm.** Name, then a confirmation showing the exact display name and key
  with an explicit acknowledgment. **Rejected as friction on a new player's most exciting
  action** — but see "worth stealing" below.

### A's real cost, stated plainly

**The player learns the server rewrote their name only *after* the character exists.** Three
things reduce that to an acceptable cost:

1. Two of the three rewriting cases are whitespace canonicalization — nobody will notice or
   mind.
2. The created character's name is **immediately visible on the roster card**, so the
   rewrite is discoverable within one screen.
3. **Rename exists** (IDENT-03, Phase 3). A player surprised by `Ｔeodor` → `Teodor` is not
   stuck with it.

> **That dependency is load-bearing. If a later release removes or gates rename, revisit
> this decision** — a surface that silently rewrites an *irreversible* identifier would need
> to show its work first, and A would become the wrong choice.

### Worth stealing from C

The normalized-name echo (`Will be created as …`) is genuinely good, and **A can show it in
the success path** — on the created card or in the toast — rather than not at all. That
keeps the teaching without making the promise.

## The §6.1 pipeline, in order

The right-hand panel of the sketch renders this **in order**, highlighting the step that
fired — so each case is legible as *where* it died, not just *that* it died. Phase 2 should
keep that shape in its error reporting.

| Step | Operation | Rejects or rewrites |
| --- | --- | --- |
| 1 | NFKC normalization | rewrites (`Ｔ` → `T`, ligatures) |
| 2 | strip `Cf` (format/zero-width) | rewrites |
| 3 | whitespace canonicalization | rewrites (double space → single) |
| — | *steps 1–3 produce the **display name*** | |
| 4 | case-fold | produces the **uniqueness key** — display is **not** case-folded |
| — | empty after normalization | **reject** (§6.1.1) |
| 5 | mixed-script — UTS #39 **Moderately Restrictive** | **reject** (Latin+Cyrillic, Latin+Greek, Cyrillic+Greek) |
| 6 | skeleton check, UTS #39 §4 — **non-unique** index, query-time | **reject** (confusable) |
| 7 | `UNIQUE` index on the normalized name | **reject** (`23505`) |

The skeleton is **not** the uniqueness key. The Unicode version used for skeletons **MUST**
be pinned and recorded (§6.1.2).

## ⚠ Finding 1 — a live availability check cannot be honest

An availability check and an `INSERT` are **two different moments**. **Even with §6.1.3's
`UNIQUE` index doing the real enforcement**, a live "available ✓" can be followed by a
`23505` on submit because someone took the name in between.

That is **not** the legacy TOCTOU — the index closes the *correctness* hole. It is a **UI
honesty** problem the index cannot close: the check is stale the moment it returns.

So B's green tick means *"probably"*, not *"yes"*, and **B must design the losing path**.
A and C never make the promise, so they never break it.

> **If B is ever chosen, "handle 23505 on submit" is not an implementation detail to
> discover later — it is the variant's defining requirement.**

## ⚠ Finding 2 — the confusable message is a small, coupled disclosure

*"That name is too easily confused with an existing character's name"* tells the submitter
that **a similar character exists** — the same family of leak as sketch 003's denial codes
and sketch 007's blank-vs-withheld rule.

Here it is **probably fine**, and it is worth saying *why* rather than assuming: character
names are readable at the **`anonymous`** floor under the seeded posture (§8.6), so the
prober learns nothing they could not read off the public directory.

Two constraints follow anyway:

1. **The message MUST NOT name the colliding character.** Confirming *which* name matched is
   a different disclosure from confirming that one exists, and it turns the create form into
   a lookup tool.
2. **If a game raises the name floor above `anonymous`** — which §8.6 permits, and the
   players-only posture does — **this message becomes a real oracle.** Phase 2 should record
   the coupling rather than treat the wording as unconditionally safe.

## The invisible-input case

`only invisibles` normalizes to empty and is rejected (§6.1.1). Note what the player sees:
**the input box looks completely empty while containing three codepoints.** So *"please
enter a name"* reads as nonsense to someone who believes they typed something — **the
message has to explain the invisible.**

> Fixture note: use explicit `\u` escapes in source rather than literal invisible
> codepoints, so the file stays readable and does not trip content scanners. A fixture
> demonstrating invisible characters should make them visible in source.

## Today's race spans **three** writers, not two

§6.1.3 corrects the requirement text. The check-then-insert race today has three
participants:

| Writer | Location |
| --- | --- |
| shared existence query | `bootstrap/setup/adapters.go:38-50` |
| player creation | `auth/character_service.go:112-121` |
| **guest provisioning** | `auth/guest_service.go:227` (inside its retry-on-collision loop) |

Adding `Rename` would make a **fourth**. There is no unique index and no `LOWER(name)` index
today. §6.1.3's index **MUST land before or with `Rename`.**

## What to Avoid

- **"Names are permanent."** False — v0.13 ships rename. The property is **reserved**.
- **A live availability check presented as a promise.** Check ≠ insert; the tick means
  "probably".
- **Naming the colliding character** in a confusable rejection.
- **"Please enter a name"** for the invisible-codepoint case — it reads as nonsense.
- **A confirmation step on creation.** Friction on a new player's most exciting action.
- **Assuming the confusable message is unconditionally safe** — it is coupled to the `name`
  floor staying at `anonymous`.
- **A dense table for the player's own roster.** Cards ship today; dense table is the
  operator idiom.
- **Rendering a session badge on a non-`active` lifecycle.** `Retired · Offline` is
  meaningless.

## Open questions this leaves

1. **Is there a block list in the UI at all?** IDENT-04 specifies a configurable regex block
   list evaluated server-side at create and rename. The sketch has no case for it because a
   blocked-name message is a **content** decision (*"that name isn't allowed here"*) that no
   source specifies. **Phase 2 needs to write it.**
2. **What happens to the guest-name retry loop?** `guest_service.go:227` retries on collision
   automatically. Once the unique index lands, its collision detection changes shape (`23505`
   rather than a failed existence check) — and it is the **one writer with no human to show a
   message to.**
3. **Length limits.** §10.6 mentions *"no side condition beyond a length cap"* for profile
   fields; **nothing in §6.1 states a cap for names.** The sketch imposes none. A name with
   no length bound is a rendering problem for **every** surface in this skill — the roster
   card, the admin table, the profile header.

## Grounding

| Element | Source |
| --- | --- |
| Card grid, `minmax(200px,1fr)`, initial-letter avatar, `Last played`, dashed create card | `(authed)/characters/+page.svelte:116-153` |
| The existing badge is **session** state | same file, `:132-136` |
| `characters.status` = `active`/`retired`/`idle`, all three shipped; `idle` unreachable | Phase-1 CONTEXT, SPEC §4.3 |
| Retire is reversible; **the name stays reserved** | SPEC §4.4, §4.5 |
| Retired roster visible-unselectable; profile still resolves | Phase-1 CONTEXT |
| "Shown, never offered, with a reason" for `idle` | sketch 004 winner C |
| Four-step pipeline in order; display name vs uniqueness key | SPEC §6.1.1 |
| Empty after normalization **MUST** be rejected | SPEC §6.1.1 |
| Mixed-script = UTS #39 Moderately Restrictive | SPEC §6.1.2 |
| Skeleton check UTS #39 §4, non-unique index, Unicode version pinned | SPEC §6.1.2 |
| Unique index; **MUST land before or with `Rename`**; three current writers | SPEC §6.1.3 |
| Player rename is IDENT-03 / `RenameCharacter`, Phase 3 | SPEC §9.4.2 line 1805, ROADMAP Phase 3 |
| `CreateCharacter` is the sole carve-out from `expected_version` | SPEC §9.4, Phase-1 CONTEXT |
| Names public at the `anonymous` floor under the seeded posture | SPEC §8.6, sketch 007 |

## Components

Nothing new. Roster uses `card` + `badge` (both installed). Creation uses `input`, `label`,
`checkbox`, `card` (all installed) and `field` (already on 004's list). Variant B would have
wanted `skeleton` or a spinner for the in-flight check.

## Origin

Synthesized from sketches: **008, 009**
Source files: `sources/008-character-roster-retired/index.html` (pickers: roster —
mixed / all-retired / single / zero; and **session badge — shown / dropped**, which
demonstrates the collision finding directly), `sources/009-character-create-name-collision/index.html`
(a **case** dropdown walking nine submissions against the live §6.1 pipeline panel).
