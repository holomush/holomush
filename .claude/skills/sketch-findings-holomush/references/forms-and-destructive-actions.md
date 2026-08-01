# Forms & Destructive Actions — the admin character edit surface

Validated in sketch **004-character-edit-destructive**. Winner: **C — Two groups** (refined).

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

## Open question

**Where does `Rename…` go?** Variant B routes to it from the locked `Name` row, but rename
is `RenameCharacter` at the world layer (Phase 3) and the SPEC does not say whether the
admin portal exposes it at all — §9.3's admin census has update / retire / unretire and
**no rename**. If admins cannot rename, B's button is a dead end and the locked row should
say so; if they can, that is a census addition. **This needs settling before Phase 6 builds
either variant.**

## Origin

Synthesized from sketch: **004**
Source file: `sources/004-character-edit-destructive/index.html`
(state dropdown cycles `editing` / `edited (mask = 2 paths)` / `version conflict` /
`retire confirm` / `retired → unretire`)
