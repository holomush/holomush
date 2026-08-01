---
sketch: 004
name: character-edit-destructive
question: "How does a form that deliberately cannot edit name, status or version avoid reading as broken — and what is the destructive action, given there is no delete?"
winner: null
tags: [forms, field-mask, destructive, audit, concurrency, phase-6]
---

# Sketch 004: Character Edit & Retire

## Correction to this sketch's own premise

Earlier notes (including the sketch-002 and 003 hand-offs) described 004 as
covering *"the irreversible delete"*. **That was wrong, and the SPEC is
unambiguous:**

> §4.4 — *"`purge` **MUST NOT** be wired to any player-facing affordance. It is
> not the implementation of a 'delete my character' button, and an admin surface
> whose button says 'delete' **MUST NOT** call it without the SPEC-level decision
> that this section deliberately does not make."*

> §10.6 — *"The irreversible delete is reachable from no player-facing
> affordance. `world.Service.DeleteCharacter` is not the implementation of an
> admin 'delete' button (§4.4). Admin disable is retire."*

And the §9.3 mutation census carries `AdminUpdateCharacter`,
`AdminRetireCharacter` and `AdminUnretireCharacter` — **there is no
`AdminDeleteCharacter` RPC at all.**

So the admin portal's destructive action is **Retire, which is reversible**.
This sketch builds that, and shows the absence of delete as a deliberate,
cited decision rather than an omission.

## Design Question

The edit surface can write **13 of the character's fields and no others**. A form
that silently omits `name` and `status` reads as incomplete — and "incomplete" is
what a well-meaning implementer *fixes*. So the real question is: **how does the
form communicate that the exclusions are deliberate, and where do the excluded
operations actually live?**

## How to View

```
open .planning/sketches/004-character-edit-destructive/index.html
```

The **state** dropdown cycles `editing` / `edited (mask = 2 paths)` /
`version conflict` / `retire confirm` / `retired → unretire`. The surface is a
Sheet in every variant — only the treatment of the excluded fields differs.

## Variants

- **A: Omit excluded** — only the 13 editable paths appear. Cleanest form; an
  admin looking for "rename" finds nothing and has to know it lives elsewhere.
- **B: Show, locked + route** — `Name`, `Status`, `Version` appear as locked rows
  carrying the SPEC's own reason and a button to the intent-named operation
  (`Rename…`, `Retire…`). Most self-teaching; longest.
- **C: Two groups** — `Editable here` / `Managed elsewhere`, with the locked rows
  compact and reasons dropped. Middle ground.

## What to Look For

- **Does A read as broken?** That is the whole question. If a reviewer's first
  reaction is "where's rename", A has failed regardless of how clean it looks.
- **Does B's reasoning belong in the form,** or is it documentation leaking into
  a working surface? Note this is the same tension sketch 001 resolved *against*
  (Registry Ledger) and 003 resolved *against* (gate provenance) — but the pull
  here is stronger, because a missing field is actively confusing in a way a
  missing trace is not.
- **`state: edited`** — the footer shows `update_mask: 2 paths` and the changed
  fields are marked. An empty mask is a **no-op** (§9.5 rule 4), which is why
  Save is inert until something changes.
- **`state: version conflict`** — what a stale `expected_version` looks like when
  it is a real UX event rather than a swallowed 409.
- **`retire confirm`** leads with reversibility and states that the **name stays
  reserved**. Compare against how a delete dialog would read — the point is that
  this one is genuinely not that.

## Grounding

| Element | Source |
| --- | --- |
| The 13-path allowlist, verbatim | §10.6 |
| The rule generating it — "no side condition beyond a length cap" | §10.6 |
| `name` excluded (normalization, skeleton checks, block list, unique index) | §10.6, §6.1 / §6.1.3 |
| `status` excluded (keeps `idle` unreachable; disable goes through `AdminRetireCharacter`) | §10.6, ADMIN-05 |
| `version` excluded (it is `expected_version` on the request) | §10.6, §9.4 |
| Exact-string matching; no wildcard reaches a role path | §9.5 rule 2, §10.6 |
| Empty mask is a no-op | §9.5 rule 4 |
| Retire is reversible; the name stays reserved | §4.4, §4.5 |
| Audit envelope in the same transaction, before-values + acting **player** id | §10.7 |
| Purge blast radius (`locations.owner_id` / `objects.owner_id` error at runtime) | §4.4 |

## Findings

### 1. The escalation test needs a positive control — and so does any UI claim

§10.6 makes a point worth carrying beyond the test suite:

> *"A test that calls `AdminUpdateCharacter` with a `roles` field the message does
> not have proves nothing — the request never carried the payload, the assertion
> 'role unchanged' is satisfied by the field being silently dropped, and the test
> passes whether or not the property holds."*

The mandated shape is: **first demonstrate the write path works on a field it
*is* allowed to change, then attempt the escalation on the same call.** This is
the same "verification that cannot fail" family as the `oops.Code` issue (#4902)
and the fabricated `last seen` column. Worth stating in Phase 6's plan, not just
inheriting from the SPEC.

### 2. The durable guard is schema-level, not allowlist-level

Also §10.6: because the real risk is a **future** field rather than a present
one, the verification is *"a meta-test that fails if the admin character message
ever gains a field whose name matches `role|grant|permission|capability`, paired
with an allowlist test asserting set equality against the checked-in list."*

Two tests, not one. The set-equality test pins today's list; the schema test
catches tomorrow's field before anyone thinks to add it to the mask.

### 3. Purge's blast radius is a live runtime error, not a hypothetical

§4.4's table is worth surfacing in any future delete discussion:
`locations.owner_id` and `objects.owner_id` carry **no `ON DELETE` clause**, so
Postgres defaults to `NO ACTION` and the delete **errors at runtime** for any
character that owns a location or object. `players.default_character_id` is
silently nulled; `character_roles` cascades away.

So the current absence of a delete button is not only a policy decision — the
underlying operation would fail for exactly the established characters an admin
would most plausibly reach for it on.

## Components this implies adding

Beyond the running list: `field` (the `Field.FieldGroup` / `Field.Field` form
primitives shadcn-svelte mandates over raw `div` + `label`), `sheet` (already
installed), `alert-dialog` for the retire confirmation, and `sonner` for the
post-mutation toast.

## Open question

**Where does `Rename…` go?** Variant B routes to it from the locked `Name` row,
but rename is `RenameCharacter` at the world layer (Phase 3) and the SPEC does
not say whether the admin portal exposes it at all — §9.3's admin census has
update / retire / unretire and no rename. If admins cannot rename, B's button is
a dead end and the locked row should say so; if they can, that is a census
addition. **This needs settling before Phase 6 builds either variant.**
