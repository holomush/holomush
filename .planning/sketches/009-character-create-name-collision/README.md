---
sketch: 009
name: character-create-name-collision
question: "Character names run a four-step normalization pipeline plus mixed-script, skeleton and uniqueness checks. What does the player see when the server rewrites, or refuses, what they typed?"
winner: "A"
tags: [forms, validation, unicode, normalization, confusables, race, phase-2, phase-3, frontier]
---

# Sketch 009: Character Create & Name Collision

## Design Question

Creation is the one place a player's free text becomes a permanent-ish identifier
that everyone else sees, and §6.1 puts **seven** gates between the keystroke and
the row. Two of them change the input rather than rejecting it, and two of them
reject for reasons that are hard to explain without either being vague or leaking
information.

The design question is not "show a validation error". It is:

1. **When the server rewrites the name, does the player find out?** §6.1.1 steps
   1–3 produce the *display name*, so a name with a fullwidth `Ｔ`, a ligature, a
   zero-width joiner, or a double space **becomes a different string**. The
   character is not called what was typed.
2. **How is a confusable rejection worded** without naming the character it
   collided with?
3. **Can the UI promise availability at all**, given the check and the insert are
   different moments?

## How to View

```
open .planning/sketches/009-character-create-name-collision/index.html
```

The **case** dropdown walks nine submissions. The right-hand panel shows §6.1's
pipeline **in order**, with the step that fired or rejected highlighted — so each
case is legible as *where* it died, not just *that* it died.

## Variants

- **A: Submit & report ★ WINNER** — the shipped shape. Type, press create, get the verdict
  back. Never promises anything it cannot keep.
- **B: Live echo + availability** — as you type, echo the normalized display name
  and the uniqueness key, and check availability. Most teaching, and the only
  variant that can be **wrong** — see the race case.
- **C: Two-step confirm** — name, then a confirmation step showing the exact
  display name and key with an explicit acknowledgment before the write.

## What to Look For

- **`stray whitespace`, `fullwidth + ligature`, `zero-width joiners`.** All three
  are accepted, and all three **change what the player typed**. In A the player
  discovers this after the character exists; in B and C before. Is silent
  canonicalization fine (it is, arguably, for whitespace) and conspicuous
  rewriting not (fullwidth `Ｔeodor` → `Teodor`)? If they should be treated
  differently, that is a rule the SPEC does not currently make.
- **`only invisibles`.** Normalizes to empty and is rejected (§6.1.1). Note the
  input box looks **completely empty** to the player while containing three
  codepoints — so "please enter a name" would read as nonsense to someone who
  believes they typed something. The message has to explain the invisible.
- **`whole-script confusable`.** The message says the name is "too easily
  confused with an existing character's name" without saying **which**. See the
  disclosure note below.
- **`B only — "available", then lost the insert`.** The important one. See below.
- **C's acknowledgment checkbox.** Does an explicit confirm belong on character
  creation, or is it friction on the single most exciting action a new player
  takes? Note it is also the only variant with somewhere natural to *put* the
  normalized-name echo without it feeling like an error.

## Decision (2026-08-01)

**A wins.** It never promises anything it cannot keep, it is the shape the roster
already ships, and it is the least Phase-2/3 code. B's live availability check is
rejected on the honesty ground in Finding 1; C's confirm step is rejected as
friction on a new player's most exciting action.

**A's real cost, stated plainly:** the player learns that the server rewrote their
name only *after* the character exists. Three things reduce that to an acceptable
cost, and the third is the one that changed during this sketch:

1. Two of the three rewriting cases are whitespace canonicalization, which nobody
   will notice or mind.
2. The created character's name is immediately visible on the roster card, so the
   rewrite is discoverable within one screen.
3. **Rename exists.** IDENT-03 ships in v0.13, so a player surprised by
   `Ｔeodor` → `Teodor` is not stuck with it. Had the "names are permanent" claim
   in Finding 3 been true, A would have been the *wrong* choice — a surface that
   silently rewrites an irreversible identifier needs to show its work first.

That dependency is worth carrying forward: **if a later release removes or gates
rename, revisit this decision**, because A's acceptability rests on it.

**One element worth stealing from C:** the normalized-name echo (`Will be created
as …`) is good, and A can show it *in the success path* — on the created card or
in a toast — rather than not at all. That keeps the teaching without the promise.

## ⚠ Finding 1 — B can promise something it cannot keep

An availability check and an `INSERT` are two different moments. **Even with
§6.1.3's `UNIQUE` index doing the real enforcement**, a live "available ✓" can be
followed by a `23505` on submit because someone took the name in between.

That is not the legacy TOCTOU — the index closes the *correctness* hole. It is a
**UI honesty** problem that the index cannot close: the check is inherently stale
the moment it returns.

So **B must design the losing path** (the `race` case shows it), and the cost is
that B's green tick means "probably" rather than "yes". A and C never make the
promise, so they never break it — C in particular gets most of B's teaching value
(it echoes the normalized name and key) without the availability claim.

**If B is chosen, "handle 23505 on submit" is not an implementation detail to be
discovered later — it is the variant's defining requirement.**

## ⚠ Finding 2 — the confusable message is a small, probably-acceptable disclosure

"That name is too easily confused with an existing character's name" tells the
submitter that **a similar character exists**. That is the same family of leak as
sketch 003's denial codes and sketch 007's blank-vs-withheld rule.

Here it is **probably fine**, and it is worth saying why rather than assuming:
character names are readable at the **`anonymous`** floor under the seeded
posture (sketch 007, SPEC §8.6), so the prober learns nothing they could not read
off the public directory.

Two constraints follow anyway:

1. **The message MUST NOT name the colliding character.** Confirming *which*
   name matched is a different disclosure from confirming that one exists, and it
   turns the create form into a lookup tool.
2. **If a game raises the name floor above `anonymous`** — which §8.6 permits, and
   the players-only posture does — **this message becomes a real oracle**, because
   names would no longer be public. Phase 2 should note the coupling rather than
   treat the wording as unconditionally safe.

## ⚠ Finding 3 — "names are permanent" is false, and both 008 and 009 said it

An earlier draft of this sketch's confirm step read *"I understand this name is
**permanent**. It cannot be changed in this release."* Sketch 008's create card
said *"Names are permanent once taken."*

**Both were wrong.** v0.13 ships player rename: **IDENT-03**, implemented as
`CharacterAccessService.RenameCharacter` (owner-scoped, ABAC `write` on
`character:<id>`, SPEC §9.4.2 line 1805), landing in **Phase 3** — and Phase 3's
own goal line names `RenameCharacter` explicitly. The rename runs this same §6.1
pipeline and collides against the same unique index.

What **is** true, and what the copy now says: **a name is *reserved* once taken**
and stays reserved even if the character is later retired (§4.4, §4.5).

Recorded rather than quietly fixed, because it is a clean worked example of the
fabricated-copy anti-pattern: plausible UI text that no source supports,
propagated across two sketches, and caught only because 008 wrote down "sketch 009
tests this claim" and 009 actually went and checked.

## Grounding

| Element | Source |
| --- | --- |
| The four-step pipeline **in order**: NFKC → strip `Cf` → whitespace canon → case-fold | SPEC §6.1.1 |
| Steps 1–3 produce the **display name**; step 4 additionally the **uniqueness key**; display is **not** case-folded | SPEC §6.1.1 |
| A name normalizing to empty **MUST** be rejected | SPEC §6.1.1 |
| Mixed-script rule = UTS #39 **Moderately Restrictive**; Latin+Cyrillic, Latin+Greek, Cyrillic+Greek rejected | SPEC §6.1.2 |
| Skeleton check per UTS #39 §4; **non-unique** index, query-time check; skeleton is **not** the uniqueness key | SPEC §6.1.2 |
| Unicode version used for skeletons **MUST** be pinned and recorded | SPEC §6.1.2 |
| Stored normalized name + `UNIQUE` index; **MUST land before or with `Rename`** | SPEC §6.1.3 |
| Today: no unique index, no `LOWER(name)` index; check-then-insert across **three** writers | SPEC §6.1.3 table |
| Player rename is IDENT-03 / `RenameCharacter`, Phase 3 | SPEC §9.4.2, ROADMAP Phase 3 |
| Name stays reserved after retire | SPEC §4.4, §4.5 |
| `CreateCharacter` is the sole carve-out from `expected_version` | SPEC §9.4, Phase-1 CONTEXT |

**Note on today's race.** §6.1.3 corrects the requirement text: the check-then-
insert race spans **three** participants, not two — the shared existence query
(`bootstrap/setup/adapters.go:38-50`), player creation
(`auth/character_service.go:112-121`), and **guest provisioning**
(`auth/guest_service.go:227`, inside its retry-on-collision loop). Adding `Rename`
would make a fourth. This sketch's `race` case is what that looks like from the
player's side once the index exists.

**Note on the fixtures.** The zero-width and empty cases use explicit `\u`
escapes in the source rather than literal invisible codepoints, so the file stays
readable and does not trip content scanners. A fixture demonstrating invisible
characters should make them visible in source.

## Components this implies adding

Beyond the running list: nothing new. Uses `input`, `label`, `checkbox`, `card`
(all installed) and `field` (already on 004's list). Variant B would want
`skeleton` or a spinner for the in-flight availability check.

## Open questions this sketch surfaces

1. **Is there a block list in the UI at all?** IDENT-04 specifies a
   configurable regex block list evaluated server-side at create and rename. The
   sketch has no case for it because a blocked-name message is a *content*
   decision ("that name isn't allowed here") rather than a mechanical one, and no
   source specifies its wording. Phase 2 needs to write it.
2. **What happens to the guest-name retry loop?** `guest_service.go:227` retries
   on collision automatically. Once the unique index lands, that loop's collision
   detection changes shape (23505 rather than a failed existence check), and it is
   the one writer with no human to show a message to.
3. **Length limits.** §10.6 mentions "no side condition beyond a length cap" for
   profile fields; nothing in §6.1 states a cap for names. The sketch imposes
   none. A name with no length bound is a rendering problem for every surface
   sketched so far — the roster card, the admin table, the profile header.
