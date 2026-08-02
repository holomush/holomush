# Shell & Navigation — the admin frame

Validated in sketch **001-admin-shell-frame**. Winner: **C2 — Command Deck, merged collapse**.

Extended by **005-admin-mutation-in-shell** (the frame composed with a mutation surface) and
**006-phone-band-parity** (the `<768px` band settled across *every* admin surface, not just
the table). See "The phone band, settled" below.

## Design Decisions

### The frame is three columns, and the rail persists

The admin portal is a **gated console living inside the game client**, not a separate
product. The existing `SectionRail` stays mounted so an operator is never more than one
click from the game.

| Column | Width | Contents |
| --- | --- | --- |
| Section rail | `--rail-w: 48px` | existing app sections, icon-only |
| Admin nav | `--adminnav-w: 232px` | admin sections, labels + `planned` badges |
| Content | remainder | the section body |

**Why C2 won over C:** C and C2 are identical except in the 768–1023px band. C keeps a
second 48px icon column (app/admin hierarchy stays visible, costs 96px of chrome); C2
merges the admin sections **into** the rail below a divider, reclaiming 48px and reading
as one nav. C2 was chosen for the reclaimed width.

**Rejected outright:**
- **A — Registry Ledger.** Nav grouped under `Available` / `Planned · gated, no handler`
  with a footer stating `7 sections · 1 available · registry: core-authoritative`.
  Infrastructure-honest, but leaks implementation detail at an operator who did not ask.
- **B — Quiet Extension.** No grouping, no badges; planned sections merely recede. Lowest
  ceremony, but does not signal the trust boundary at all.

### The nav is derived from the registry, never from `{#if}`

Per ROADMAP Phase 6 SC5 and SPEC §10.1, nav entries come from the core-authoritative
admin registry. A section the viewer may not use is **filtered out of the registry
contract**, not hidden with a template conditional.

Mirror the existing `as const satisfies` pattern at `web/src/lib/nav/sections.ts:41-47` —
do **not** add a library for this.

### Use container queries, not media queries

The breakpoints are expressed as `@container vp (...)` against a
`container-type: inline-size; container-name: vp` wrapper. In the sketch this is what
makes the 375 / 768 / 1280 toolbar buttons genuinely exercise the breakpoints instead of
statically mocking them — and it is the right production choice too, because the shell is
a bounded region inside the app, not the viewport.

### Breakpoint contract

| Container width | Rail | Admin nav | Content |
| --- | --- | --- | --- |
| **≥ 1024px** | 48px icons | 232px, labels + `planned` badges | full table |
| **768–1023px** | 48px icons | **merged into the rail** below a divider | full table |
| **< 768px** | width 0 | width 0 | `.mobilebar` (hamburger + `Admin › Characters`); columns 4 and 5 drop from the table |

The `< 768px` behavior matches the app's existing pattern — `(authed)/+layout.svelte` puts
the Rail inside a `Sheet` drawer via `mobileNavOpen`. **The phone drawer must hold rail
sections *and* admin sections together** — sketch 006 is where that finally got drawn.

## The phone band, settled (sketch 006)

**The `<768px` band was decided once, on one surface, and never promoted.** 006 verified the
drift before designing anything:

| Sketch | `@container vp (max-width: 767px)` | `.mobilebar` |
| --- | --- | --- |
| 001 admin-shell-frame | **full** — rail *and* adminnav zeroed, table columns dropped | **yes** |
| 002 admin-character-table | **partial** — zeroes only `.rail` | **no** |
| 003 planned-section-empty | **absent entirely** | **no** |
| 004 character-edit-destructive | **absent** (no shell at all) | **no** |

006 settles it across all of them. Three results are load-bearing:

### 1. The drawer holds both nav levels, split by labels

The drawer carries rail sections **and** admin sections together, separated by a divider and
**two group labels**. Those labels are doing the hierarchy work that `.rail-btn.is-context`
does at 768–1023px — a flat merged list has no equivalent device, so it needs an explicit
one. (**Do not** use the game's name as the first group label — see `anti-patterns.md` §11.)

### 2. Row actions cannot hover on a phone

002-A's inline-on-hover row actions become **permanently visible** below 768px, and only
`Edit` survives — `Retire` and `⋯` are dropped. **This is a real divergence from 002's
winner, not an oversight.** Whether one always-visible action per row is right, or the row
should open a sheet carrying all three, is a Phase-6 call this sketch does not settle.

### 3. The planned-section empty state survives narrowing for free

003-A had **no phone handling at all**. A centred, vertically-anchored block is naturally
responsive — confirmed rather than assumed.

### Dropped columns

`Created` and `Ver` vanish below 768px. **`Ver` is load-bearing for the concurrency
contract** — on a phone a stale-version conflict would arrive with no prior sight of the
version at all. The sheet header still carries it, which *may* be sufficient; Phase 6 should
decide deliberately.

## The mutation surface composes with the frame (sketch 005)

Sketch 004 was built **entirely standalone** — zero `shell`, zero `rail`, zero `adminnav`,
no container query. Its outermost wrapper was a bare `.stage`. **The edit Sheet had never
once rendered inside the frame it will live in.**

The untested collision: **at 768–1023px the content column is at its narrowest** (the admin
nav has merged into the rail, but content still starts at 48px) **exactly where a 380px
Sheet is widest relative to it.**

005 rendered it and the answer is that it holds — see `forms-and-destructive-actions.md` for
the Sheet geometry decision and the `side="bottom"` override. The shell-side lesson:

> **Rule: a surface validated standalone has not been validated.** Compose it with the frame
> at the band where the frame is tightest before treating the decision as settled.

## The two defects merging exposed — and their fixes

Merging is not free. Building C2 surfaced two concrete problems that Phase 6 would
otherwise have inherited. Both fixes are load-bearing.

### 1. Two active indicators in one column

The Admin rail button and the Characters section button were both `is-active`, each drawing
the cyan active bar — two "you are here" markers at two different levels of hierarchy.

Fix: `.rail-btn.is-context`, scoped **inside** the `max-width: 1023px` container query.
Once merged, Admin keeps the primary tint (you *are* in Admin) but surrenders the active
bar to the section you are actually on. At ≥1024 it keeps its bar, because there it is the
only thing telling you where you are.

**Scoping this globally would strip the bar at full width, which is wrong.**

### 2. The identity header and `⌘K` vanished

`.adminnav.is-merge` collapses to `width: 0`, taking the `seanb · administrator` block and
the jump affordance with it. Given the trust-boundary framing, that signal is load-bearing
— both return at the rail foot via `.rail-identity`, rendered **only** at the collapsed
breakpoint so they never duplicate the nav's own copies at ≥1024.

> **General rule this generalizes to:** whenever a column collapses, check for a duplicated
> "you are here" marker and for orphaned identity/affordance blocks. The merged column needs
> an explicit hierarchy device that two columns gave for free.

## CSS Patterns

Container wrapper — the breakpoint substrate:

```css
.viewport-wrap {
  margin: 0 auto;
  transition: max-width 180ms ease;
  /* container (not media) queries, so the breakpoints are exercised
     against the shell's own width rather than the viewport's */
  container-type: inline-size;
  container-name: vp;
}
```

The merge, with both fixes:

```css
@container vp (max-width: 1023px) {
  /* nav disappears; its items ride inside the rail itself */
  .adminnav.is-merge {
    width: 0;
    border-right-width: 0;
    overflow: hidden;
  }
  .rail-admin-group,
  .rail-identity {
    display: flex;
  }

  /* Only once merged does the Admin button become CONTEXT rather than a
     peer nav target: it keeps the primary tint (you ARE in Admin) but
     surrenders the active bar to the section you're actually on.
     At >=1024 it keeps its bar, because there it is the only
     thing telling you where you are. */
  .rail-btn.is-context {
    opacity: 0.7;
    cursor: default;
  }
  .rail-btn.is-context::before {
    display: none;
  }
}

/* PHONE — both columns gone; a drawer holds the merged nav */
@container vp (max-width: 767px) {
  .shell > .rail,
  .adminnav.is-merge {
    width: 0;
    border-right-width: 0;
    overflow: hidden;
  }
  .mobilebar { display: flex; }
  table.chars th:nth-child(4), table.chars td:nth-child(4),
  table.chars th:nth-child(5), table.chars td:nth-child(5) { display: none; }
}
```

The rail-foot identity block (collapsed breakpoint only):

```css
.rail-admin-group,
.rail-identity {
  display: none;             /* flex only inside the 1023px query */
  flex-direction: column;
  align-items: center;
  gap: 4px;
}
.rail-divider {
  width: 20px; height: 1px;
  background: var(--color-border);
  margin: 5px 0;
}
.rail-avatar {
  width: 26px; height: 26px; border-radius: 999px;
  background: color-mix(in srgb, var(--color-primary) 18%, transparent);
  color: var(--color-primary);
  border: 1px solid color-mix(in srgb, var(--color-primary) 35%, transparent);
}
```

Icon-column tooltips (the C variant's approach; keep if a future collapse needs labels):

```css
.adminnav.is-icon .navitem[data-tip]:hover::after {
  content: attr(data-tip);
  position: absolute;
  left: calc(100% + 8px);
  top: 50%;
  transform: translateY(-50%);
  background: var(--color-popover);
  border: 1px solid var(--color-border);
  padding: 3px 8px; border-radius: 5px;
  font-size: 11.5px; white-space: nowrap;
  z-index: 50; pointer-events: none;
}
```

## HTML Structures

Rail with merged admin group and foot identity:

```html
<div class="rail">
  <!-- `is-context` at the collapsed breakpoint: primary-tinted (you ARE in
       Admin) but no active bar — that belongs to the section you're on -->
  <button class="rail-btn is-active is-context" title="Admin">…</button>

  <div class="rail-admin-group">
    <div class="rail-divider"></div>
    <!-- admin section buttons ride here once merged -->
  </div>

  <div class="rail-identity">
    <div class="rail-avatar">SB</div>
  </div>
</div>

<div class="adminnav is-merge">…</div>
```

Registry-derived nav item (status drives presentation, not a conditional):

```html
<a class="navitem {s.id === cur.id ? 'is-active' : s.status === 'planned' ? 'is-planned' : ''}">
  <svg width="15" height="15"><use href="#{s.icon}"/></svg>
  <span class="nav-label">{s.label}</span>
  {#if s.status === 'planned'}<span class="badge badge-planned">planned</span>{/if}
</a>
```

## What to Avoid

- **Media queries.** The shell is a bounded region; use `@container vp`.
- **A global `.is-context`.** It must live inside the `max-width: 1023px` query or the
  active bar is stripped at full width, where it is the only location signal.
- **Leaving both markers active on merge.** Two "you are here" bars in one column at two
  hierarchy levels reads as a bug.
- **A `{#if}` in the nav template.** Visibility is a registry/contract property (SC5).
- **Registry-contract copy in the operator's face** (variant A's `registry:
  core-authoritative` footer). Rejected as implementation detail leaking into UI.
- **Fabricating a `Last seen` column.** 001's first draft did exactly this; it does not
  exist in `characters`. See `anti-patterns.md`.
- **Letting a surface inherit the phone band by accident.** Three of the four round-1
  sketches half-inherited or ignored it. Every admin surface gets the `max-width: 767px`
  treatment deliberately.
- **A flat merged drawer with no hierarchy device.** The two group labels are load-bearing —
  `.rail-btn.is-context` is unavailable once both columns are at `width: 0`.
- **Hover-only row actions below 768px.** There is no hover. Actions must be visible or the
  row must open something.
- **Validating a surface standalone and calling it done.** 004 never rendered inside the
  shell; 005 exists because of that.

## Open question this leaves for Phase 6

`SECTIONS` in `web/src/lib/nav/sections.ts` has **no status concept** — every entry is
live. The admin registry needs `status: 'available' | 'planned'` as a **required** field,
mirroring §10.2's "no default, no zero value means allow". Whether that lives on the
existing `WorkspaceSection` shape or a separate `AdminSection` type is a Phase-6 planning
decision this sketch does not settle.

## Origin

Synthesized from sketches: **001** (the frame), **005** (the frame under a mutation
surface), **006** (the `<768px` band across every admin surface)

Source files:
`sources/001-admin-shell-frame/index.html` ·
`sources/005-admin-mutation-in-shell/index.html` (drive the ten-step sequence dropdown, then
**re-drive it at 768 and 375** — the answer changes by band) ·
`sources/006-phone-band-parity/index.html` (**opens pinned to 390px**; above 768 it
deliberately says nothing and tells you so)
