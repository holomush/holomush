---
sketch: 005
name: admin-mutation-in-shell
question: "Does the edit Sheet survive contact with the C2 shell at every viewport band — and what does the mutation loop look like as a sequence rather than as five separate frames?"
winner: "A"
tags: [layout, forms, sheet, responsive, toast, sequence, phase-6, consistency]
---

# Sketch 005: Admin Mutation in Shell

## Design Question

This is a **consistency sketch**. It builds nothing new — it composes decisions
sketches 001–004 already made, in the two dimensions they were never tested in.

Two verified gaps drove it:

1. **Sketch 004 was built entirely standalone.** Its `index.html` contains zero
   `shell`, zero `rail`, zero `adminnav`, zero `viewport-wrap` and no container
   query — its outermost wrapper is a bare `.stage`. The edit Sheet has never once
   rendered inside the frame it will actually live in. At 768–1023px the content
   column is at its *narrowest* (the admin nav has merged into the rail, but the
   content still starts at 48px) exactly where a 380px Sheet is *widest* relative
   to it. That is the untested collision.
2. **The post-mutation toast has never been drawn.** `sonner` appears on sketch
   001's components-to-add table ("mutation confirmations") and again in sketch
   004's ("the post-mutation toast"), and in **zero** sketches. It is the only
   piece of the mutation loop with no visual decision behind it.

A third, smaller gap: 004 validated `editing` / `edited` / `conflict` /
`retire confirm` / `retired` as five **separate frames**. Nobody has seen them as
a **sequence**, so nothing has been decided about what happens *between* them —
whether the row updates in place or the table refetches, whether the sheet closes
before or after the toast, whether Undo is offered.

## How to View

```
open .planning/sketches/005-admin-mutation-in-shell/index.html
```

**Drive the sequence with the dropdown in the top-right** — ten steps across two
paths (edit, then retire). **Then re-drive it at 768 and at 375** with the
viewport buttons. The whole point of the sketch is that the answer changes by
band, so a single-viewport read of it is worthless.

## Variants

All three render the identical table, sheet contents and toast. **The only
variable is where the Sheet sits.**

- **A: Overlay ★ WINNER** — fixed 380px right drawer with a scrim, the
  shadcn-svelte default and the path of least resistance for the target stack.
  Same treatment at every band. Chosen for exactly that: one geometry, no
  per-band branching, and the least Phase-6 code.
- **B: Inset split** — no scrim; the Sheet is a sibling column and the table
  narrows to make room. The table stays readable while you edit. Collapses to a
  full-width takeover below 1024, because a 380px inset would leave ~330px of
  table.
- **C: Adaptive** — overlay at ≥1024, full-column takeover at 768–1023, and a
  **bottom-sheet** at <768. Three treatments, one per band.

## What to Look For

- **At 768, compare A against C.** A keeps a 380px right drawer over a content
  column that is itself only ~700px — the table behind the scrim is mostly
  covered but still *visible*, which reads as neither one thing nor the other. C
  commits. Does the commitment feel better, or does losing the table's context
  hurt more than the half-covered view?
- **At 375, A is deliberately left wrong.** It keeps the right-drawer geometry on
  a phone, where the platform convention is a bottom-sheet. If A does not look
  broken there, C's third treatment is not earning its complexity.
- **Step 6 → step 7 (the update).** The row changes **in place** with a brief
  flash rather than the table refetching. Watch `Ver 7 → 8`. Is the flash enough
  to notice without being a distraction? This is the choice that decides whether
  Phase 6 needs an optimistic-update path or can refetch the page.
- **The toast copy.** It names the RPC (`AdminUpdateCharacter · update_mask: 2
  paths · v7 → v8`). That is unusually technical for a toast — but this is an
  admin acting on a character they do not own, and §10.7 records the mutation to
  the audit log with their player id. Does naming the wire call read as
  appropriate accountability, or as debug output leaking into product UI? **This
  is the same tension sketches 001 and 003 both resolved *against*** (Registry
  Ledger, gate provenance) — the question is whether a mutation is different from
  a nav or an empty state.
- **Step 9's Undo.** It sends `AdminUnretireCharacter` — never a status value
  (§10.6). Is an Undo affordance right for an action whose confirmation *already*
  told you it was reversible, or is it belt-and-braces that undercuts the
  confirmation?
- **Variant B at ≥1024.** Does keeping the table visible while editing actually
  help? Sketch 002 rejected its own variant B (a 330px detail pane) for not
  earning its width — this is that question again, but for a *mutation* surface
  rather than a read one.

## Grounding

Everything here is composed from prior decisions, not invented:

| Element | Source |
| --- | --- |
| Three-column frame, container queries, merged collapse, `.rail-btn.is-context`, `.rail-identity` | sketch 001 winner C2 |
| Dense table, inline hover row actions, no multi-select, `Ver` column (**not** `Last seen` — it does not exist) | sketch 002 winner A |
| Two-group Sheet, `version` as header metadata, `update_mask` in the footer, empty mask ⇒ Save inert | sketch 004 winner C |
| Retire is reversible, name stays reserved, sends `AdminRetireCharacter` never a status value | SPEC §4.4, §4.5, §10.6 |
| Mutation recorded to audit with the acting **player** id | SPEC §10.7 |
| `expected_version` on every mutation | SPEC §9.4 |

## Components this implies adding

Nothing beyond the running list. Exercises `sheet` (installed), `alert-dialog`
and `sonner` (both still to install).

## Decision (2026-08-01)

**A wins.** One geometry at every band, no per-band branching, least Phase-6 code
— the path of least resistance for shadcn-svelte's `Sheet`, which is what a
consistency sketch should default to absent a reason to diverge.

**The open edge A leaves is the phone band, and it is deliberate.** A keeps the
380px right-drawer geometry at <768 where the platform convention is a
bottom-sheet, and this sketch rendered that without correcting it. **Sketch 006
exists precisely to test that band**, so the question is handed there rather than
settled by assumption here. If 006 finds the right drawer holds up on a phone, A
ships unchanged; if it does not, A gains exactly one `@container vp (max-width:
767px)` override and stays A everywhere else.

Not adopted from the losers, but worth recording:

- **B's readable-table-while-editing** lost the same way sketch 002's variant B
  did — a side-by-side pane does not earn its width on a column this narrow. That
  is now two independent sketches reaching the same verdict on the same idea, at
  two different layers. Treat "show the list beside the editor" as settled
  against for this portal.
- **C's per-band branching** was the more *correct* answer at 375 in isolation and
  still lost, because three treatments is three things to keep true. Its
  bottom-sheet is the specific fallback 006 should try first.

## Open question this sketch surfaces

**Does the Sheet close before or after the toast fires?** This sketch closes it
first (step 5 shows the toast with the sheet already gone), which is the
conventional choice — but it means the last thing you see of your edit is the
toast's summary, not the form. The alternative (sheet stays open, toast fires
over it, user closes manually) keeps the edited values on screen for a
double-check at the cost of an extra click. Not settled here.
