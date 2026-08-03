# Forms & Destructive Actions — the admin character edit surface

Validated in sketch **004-character-edit-destructive**. Winner: **C — Two groups** (refined).

Extended by **005-admin-mutation-in-shell** (where the Sheet sits, and the mutation loop as a
*sequence*) and **006-phone-band-parity** (the one phone override). Player-facing form
findings — the §6.1 name pipeline, confusables, live-availability honesty — live in
`player-roster-and-creation.md`.

## ⚠ Read this first: there is no delete in the admin portal

Earlier hand-off notes (including sketch 002's and 003's) described 004 as covering *"the
irreversible delete"*. **That was wrong, and the SPEC is unambiguous:**

> §4.4 — *"`purge` **MUST NOT** be wired to any player-facing affordance. It is not the
> implementation of a 'delete my character' button, and an admin surface whose button says
> 'delete' **MUST NOT** call it without the SPEC-level decision that this section
> deliberately does not make."*

> §10.6 — *"The irreversible delete is reachable from no player-facing affordance.
> `world.Service.DeleteCharacter` is not the implementation of an admin 'delete' button
> (§4.4). Admin disable is retire."*

The §9.3 mutation census carries `AdminUpdateCharacter`, `AdminRetireCharacter` and
`AdminUnretireCharacter` — **there is no `AdminDeleteCharacter` RPC at all.**

**The admin portal's destructive action is Retire, which is reversible.**

The absence of delete is not only policy. §4.4's table: `locations.owner_id` and
`objects.owner_id` carry **no `ON DELETE` clause**, so Postgres defaults to `NO ACTION` and
the delete **errors at runtime** for any character owning a location or object.
`players.default_character_id` is silently nulled; `character_roles` cascades away. The
underlying operation would fail for exactly the established characters an admin would most
plausibly reach for it on.

## Design Decisions

### The surface is a Sheet with two groups

The edit surface can write **13 of the character's fields and no others**. A form that
silently omits `name` and `status` reads as incomplete — **and "incomplete" is what a
well-meaning implementer *fixes*.** So the design problem is communicating that the
exclusions are deliberate, and where the excluded operations actually live.

`Managed elsewhere` (first, collapsed) → `Editable here`.

**Rejected:**
- **A — Omit excluded.** Only the 13 editable paths appear. Cleanest; an admin looking for
  "rename" finds nothing and must already know it lives elsewhere.
- **B — Show, locked + route.** `Name`, `Status`, `Version` as locked rows carrying the
  SPEC's own reason plus a button to the intent-named operation. Most self-teaching,
  longest. (Its plain `Retire…` / `Un-retire` button is the honest fallback if C's picker
  reads as ceremony — see the degenerate case below.)

### Managed-elsewhere goes **first**, and collapses

The exclusions are declared **up front** — an admin learns what this surface cannot do
before scrolling a form — but collapsed to a single summary line
(`Name Ashwood, Miren · Status active · managed by their own operations`), costing ~30px
instead of ~120px. Click the group header or the summary to expand into full rows with
their actions.

### `version` is demoted out of the group entirely

It was a locked row; it is now **header metadata** beside the id (`01JQ7X…8F2 · v7`).

Rationale: `name` and `status` are *managed elsewhere* — there is another place to go and
something to do. **`version` is never editable and never actionable**; it is the concurrency
guard carried as `expected_version` (§9.4). Giving it a row alongside the others implied a
door that does not exist.

### Status is a transition picker wearing a state picker's clothes

**This distinction is load-bearing:**

> §10.6 — *"`status` is excluded. §9.3 keeps the lifecycle vocabulary **off the wire** so
> `idle` stays unreachable; a maskable `status` path would put it back on."*

So the menu **must not send a status value**:

| Selection | Sends |
| --- | --- |
| `Retired` (from active) | `AdminRetireCharacter` |
| `Active` (from retired) | `AdminUnretireCharacter` |
| `idle` | **never selectable** |

The menu footer says so literally — *"sends `AdminRetireCharacter` — never a status
value"* — because **this is exactly the control an implementer would naturally wire to a
`status` field.**

`idle` **appears and is never selectable** (`⊘`, dimmed, *"System-invoked on inactivity —
not implemented in v0.13"*). Showing it makes the three-state model legible; making it
selectable would put the unreachable value back on the wire, which is precisely what §10.6
forbids.

Selecting a transition does **not** apply it — it routes to the confirmation, which is
where the RPC is sent.

**The degenerate case, stated honestly:** with `idle` unselectable, a character in any state
has exactly **one** legal transition, so the "pick list" always offers a single choice. It
earns its shape only as a **model-teaching** surface — showing all three states with the
current one marked — not as a chooser. If that reads as ceremony in use, the honest
fallback is variant B's plain `Retire…` / `Un-retire` button.

### The mask is visible, and an empty mask is inert

The footer shows `update_mask: 2 paths` (or `update_mask: empty — no-op`) and changed
fields are marked. **An empty mask is a no-op (§9.5 rule 4), which is why Save is inert
until something changes.**

### Version conflict is a real UX event, not a swallowed 409

> *"Someone else edited this character. Your copy is version 7; the server is now at 9. Your
> changes were **not** applied — reload to see the current values, then re-apply."*

### The retire confirmation leads with reversibility

It states that the action is reversible **and that the name stays reserved** (§4.4, §4.5).
Compare against how a delete dialog would read — the point is that this one genuinely is not
that.

## Where the Sheet sits — one geometry, one override (sketches 005 + 006)

**The combined result is the cheap one:**

> **005-A everywhere, plus exactly one `@container vp (max-width: 767px)` block that turns
> the right drawer into a bottom-sheet.** No routing change, no three-treatment branching,
> no per-band component swap. shadcn-svelte's `Sheet` supports `side="bottom"` natively, so
> **this is a prop at a breakpoint, not a second component.**

005 picked the simplest geometry (a fixed 380px right drawer with a scrim, the shadcn
default) and **deliberately left the phone band wrong**, handing it to 006 rather than
settling it by assumption. Paying only where it broke cost one CSS block; adopting 005's
variant C (three treatments, one per band) up front would have cost three.

**Rejected at 005:**

- **B — Inset split.** No scrim; the Sheet is a sibling column and the table narrows. It
  lost the same way sketch 002's variant B (a 330px detail pane) did — a side-by-side pane
  does not earn its width on a column this narrow. **That is two independent sketches
  reaching the same verdict on the same idea at two different layers: treat "show the list
  beside the editor" as settled *against* for this portal.**
- **C — Adaptive.** Overlay ≥1024, full-column takeover 768–1023, bottom-sheet <768. More
  *correct* at 375 in isolation, and still lost: three treatments is three things to keep
  true. Its bottom-sheet is exactly what 006 adopted as the single override.

**Rejected at 006:**

- **C — Full-screen takeover** with a `‹ Characters` back arrow. **Rejected, and the
  rejection closes a routing question early.** A back arrow makes the sheet a **route**,
  which makes it **deep-linkable**, which drags [#4903](https://github.com/holomush/holomush/issues/4903)
  into the edit surface: a deep link to a character the viewer may not see would have to
  render the ordinary not-found like any other unreachable path. **B keeps the sheet an
  overlay, so that whole branch stays closed.** Record it as a deliberate scope reduction.

```css
/* A — 005's winner: 380px right drawer, every band ≥768. */
.sheet {
  position: absolute; z-index: 60; display: flex; flex-direction: column;
  background: var(--color-surface); transition: transform 200ms ease;
  top: 0; right: 0; bottom: 0; width: 380px;
  border-left: 1px solid var(--color-border);
  transform: translateX(102%);
  box-shadow: -18px 0 40px rgba(0, 0, 0, .45);
}
.sheet.open { transform: none; }

/* THE ONE OVERRIDE — Sheet side="bottom" below the phone breakpoint. */
@container vp (max-width: 767px) {
  .sheet {
    top: auto; left: 0; right: 0; bottom: 0;
    width: auto; height: 84%;
    border-left: 0; border-top: 1px solid var(--color-border);
    border-radius: 13px 13px 0 0;
    transform: translateY(102%);
    box-shadow: 0 -18px 40px rgba(0, 0, 0, .45);
  }
  .grabber { display: block; }
}
```

### ⚠ The grab handle is an obligation, not decoration

B ships a grabber, and **a grab handle promises drag-to-dismiss** (and usually a
partial-height detent). Neither is implemented in the sketch.

> **Phase 6 must either honor the affordance or drop it.** A handle that does not drag is a
> *worse* affordance than no handle, because it invites a gesture that then fails silently.

### Input font-size is 15px on the phone band, not 12.5px

Any `<16px` font in a **focused input** triggers iOS Safari's zoom-on-focus, which then
leaves the viewport scaled after blur. This is a genuine mobile constraint, not a style
preference — **keep 15px (or go to 16px) rather than inheriting the desktop size.**

```css
.fieldrow input, .fieldrow textarea {
  font-size: 15px;   /* <16px triggers iOS zoom-on-focus */
}
```

## The mutation loop as a sequence (sketch 005)

004 validated `editing` / `edited` / `conflict` / `retire confirm` / `retired` as five
**separate frames**. Nobody had seen them as a **sequence**, so nothing was decided about
what happens *between* them. 005 settles four things:

| Moment | Decision |
| --- | --- |
| **After Save succeeds** | The row updates **in place** with a brief flash (`Ver 7 → 8`) — **not** a table refetch. This is what decides whether Phase 6 needs an optimistic-update path or can refetch the page. |
| **Sheet vs toast ordering** | The sheet **closes first**, then the toast fires. Conventional — but it means the last thing you see of your edit is the toast's summary, not the form. *See the open question below.* |
| **Toast copy** | Names the wire call: `AdminUpdateCharacter · update_mask: 2 paths · v7 → v8`. |
| **Undo on retire** | Offered, and it sends **`AdminUnretireCharacter`** — never a status value (§10.6). |

### Why the technical toast copy is *not* the mistake 001 and 003 rejected

Sketches 001 (Registry Ledger footer) and 003 (gate provenance trace) both rejected
implementation detail in the operator's face. The toast looks like the same thing and is
not:

> **A mutation is different from a nav or an empty state.** This is an admin acting on a
> character they **do not own**, and §10.7 records the mutation to the audit log **with their
> player id**. Naming the wire call reads as *accountability*, not debug output — the
> operator is being told exactly what was written under their name.

That distinction is the rule to carry: **narration is wrong on read surfaces and right on
audited write surfaces.**

## HTML Structures

Two-group body with the collapsed summary:

```html
<div class="grouphead clickable" onclick="toggleLocked()">
  Managed elsewhere
  <svg class="chev {LOCKED_OPEN ? 'open' : ''}"><use href="#i-chev"/></svg>
</div>

<!-- collapsed form: one line, ~30px -->
<div class="lockedsummary" onclick="toggleLocked()">
  <svg class="chev"><use href="#i-chev"/></svg>
  <span><b>Name</b> Ashwood, Miren</span>
  <span style="opacity:.5">·</span>
  <span><b>Status</b> active</span>
  <span style="flex:1"></span>
  <span style="font-size:10.5px">managed by their own operations</span>
</div>

<div class="grouphead">Editable here</div>
<div class="fieldgroup"><!-- the 13 maskable paths --></div>
```

Sheet header — `version` as metadata, not a row:

```html
<div class="sheet-head">
  <div>
    <div style="font-size:15px;font-weight:600">Edit character</div>
    <div class="mono" style="font-size:11.5px;color:var(--color-status-text)">
      01JQ7X…8F2 ·
      <span title="Concurrency guard — carried as expected_version on the request (§9.4).
                   Never editable, never actionable, so it is metadata here rather than a row.">v7</span>
    </div>
  </div>
</div>
```

Footer — mask visibility gates Save:

```html
<div class="sheet-foot">
  <span style="font-size:11px;color:var(--color-status-text)">
    {dirty ? 'update_mask: 2 paths' : 'update_mask: empty — no-op'}
  </span>
  <span style="flex:1"></span>
  <button class="btn">Cancel</button>
  <button class="btn btn-primary" {dirty ? '' : 'disabled'}>Save</button>
</div>
```

Conflict banner:

```html
<div class="conflict">
  <svg style="color:var(--color-destructive)"><use href="#i-alert"/></svg>
  <div><b>Someone else edited this character.</b> Your copy is version 7; the server is
       now at 9. Your changes were <b>not</b> applied — reload to see the current values,
       then re-apply.</div>
</div>
```

Use shadcn-svelte's `Field.FieldGroup` / `Field.Field` primitives over raw `div` + `label`.

## Verification the SPEC mandates (carry into Phase 6's plan)

### The escalation test needs a positive control

> §10.6 — *"A test that calls `AdminUpdateCharacter` with a `roles` field the message does
> not have proves nothing — the request never carried the payload, the assertion 'role
> unchanged' is satisfied by the field being silently dropped, and the test passes whether
> or not the property holds."*

**Mandated shape:** first demonstrate the write path works on a field it *is* allowed to
change, **then** attempt the escalation on the same call.

### The durable guard is schema-level, not allowlist-level — two tests, not one

Because the real risk is a **future** field rather than a present one, §10.6 mandates:

1. A **meta-test** that fails if the admin character message ever gains a field whose name
   matches `role|grant|permission|capability`.
2. An **allowlist test** asserting set equality against the checked-in 13-path list.

The set-equality test pins today's list; the schema test catches tomorrow's field before
anyone thinks to add it to the mask.

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

## What to Avoid

- **A delete button.** There is no `AdminDeleteCharacter`. Wiring
  `world.Service.DeleteCharacter` to an admin affordance is forbidden by §4.4 **and** §10.6.
- **Sending a `status` value.** Send the transition RPC. A maskable `status` path puts
  `idle` back on the wire.
- **Making `idle` selectable.** Show it, never offer it.
- **Giving `version` a row.** It implies a door that does not exist.
- **Silently omitting `name`/`status`** with no explanation (variant A's failure): a missing
  field is actively confusing in a way a missing trace is not, and "incomplete" invites a
  well-meaning implementer to "fix" it.
- **Swallowing a 409.** Surface the conflict with both versions and an explicit "not
  applied".
- **An enabled Save on an empty mask.** Empty mask is a no-op (§9.5 rule 4).
- **An escalation test with no positive control.** It passes whether or not the property
  holds.
- **A side-by-side list-plus-editor.** Rejected twice independently (002-B, 005-B). Settled.
- **A back arrow on the phone sheet.** It makes the sheet a route, which makes it
  deep-linkable, which drags #4903 into the edit surface.
- **A grab handle with no drag-to-dismiss.** Honor it or drop it.
- **Inheriting the desktop `12.5px` input size on the phone band.** iOS zooms on focus and
  does not unzoom on blur.

## Open questions

### Where does `Rename…` go? — *narrowed, still open*

Variant B routes to rename from the locked `Name` row.

**Round 2 settled half of it: player rename definitely ships.** IDENT-03 /
`CharacterAccessService.RenameCharacter` — **owner-scoped**, ABAC `write` on
`character:<id>`, SPEC §9.4.2 line 1805, Phase 3. So the locked row is *not* pointing at
something that does not exist.

**What is still open is the *admin* half.** §9.3's admin census still has update / retire /
unretire and **no `AdminRenameCharacter`.** So: if admins cannot rename, B's button is a
dead end and the locked row should say *"the owner can rename this"*; if they can, that is a
census addition. **Settle before Phase 6 builds either variant.**

### Does the Sheet close before or after the toast fires?

005 closes it first (the conventional choice) — but that means the last thing you see of
your edit is the toast's summary, not the form. The alternative (sheet stays open, toast
fires over it, user closes manually) keeps the edited values on screen for a double-check at
the cost of an extra click. **Not settled.**

### Is the sheet a route or an overlay?

006's variant C forced the question; A and B dodge it. **B's win keeps it an overlay**,
which is the answer for v0.13 — but it is a Phase-6 routing decision with a deep-link
consequence (#4903), not purely visual, and it should be written down as such rather than
inherited.

### Do the dropped columns need a home?

`Created` and `Ver` vanish below 768px. `Ver` is load-bearing for the concurrency contract —
on a phone a stale-version conflict arrives with **no prior sight of the version at all**.
The sheet header still carries it, which may be sufficient.

## Origin

Synthesized from sketches: **004** (the surface), **005** (its placement + the mutation
sequence), **006** (the phone override)

Source files:
`sources/004-character-edit-destructive/index.html` (state dropdown cycles `editing` /
`edited (mask = 2 paths)` / `version conflict` / `retire confirm` / `retired → unretire`) ·
`sources/005-admin-mutation-in-shell/index.html` (a **ten-step sequence** across two paths,
edit then retire — drive it at 1280, then again at 768 and 375) ·
`sources/006-phone-band-parity/index.html` (phone-first; the `sheet` and `toast` screens)
