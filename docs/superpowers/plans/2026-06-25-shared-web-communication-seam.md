<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Shared Web Communication Rendering Seam Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix scene PoseCard rendering (holomush-5rh.33) by extracting one shared `CommunicationLine` presentation primitive + a normalized `CommLine` model that the terminal and scene surfaces both render through. (Composer sigil recognition, holomush-5rh.32, was split into a separate focus-routed-input effort — see spec §11.)

**Architecture:** Two event vocabularies (`core-communication:*` events; `core-scenes:scene_*` → `LogEntry`) adapt into one `CommLine` model via pure functions; a single `CommunicationLine.svelte` renders the canonical say/pose/ooc/emit phrasing with `--mush-*` tokens. The terminal's `CommunicationRenderer` and the scene `PoseCard` both thin to adapter + primitive.

**Tech Stack:** SvelteKit 2 / Svelte 5 runes, TypeScript, vitest (raw `svelte` `mount`/`unmount` for component tests), pnpm. Spec: `docs/superpowers/specs/2026-06-25-shared-web-communication-seam-design.md`.

**Design bead:** holomush-c5zol · **Bug:** holomush-5rh.33 (PoseCard rendering). holomush-5rh.32 (composer) split out — see spec §11.

---

## File Structure

| File | Responsibility | Status |
| --- | --- | --- |
| `web/src/lib/comm/commLine.ts` | `CommLine`/`CommKind`/`CommEvent` types + `commEventToLine` + `logEntryToLine` adapters | Create |
| `web/src/lib/comm/commLine.test.ts` | adapter unit tests | Create |
| `web/src/lib/comm/CommunicationLine.svelte` | shared say/pose/ooc/emit phrasing primitive (`--mush-*`, `linkUrls`) | Create |
| `web/src/lib/comm/CommunicationLine.svelte.test.ts` | per-kind render + escaping tests | Create |
| `web/src/lib/comm/parity.test.ts` | SEAM-1 terminal↔scene convergence; SEAM-4 Go↔TS golden | Create |
| `web/src/lib/comm/seam-guard.test.ts` | SEAM-2 no `--brand-*` in renderer components | Create |
| `web/src/lib/components/terminal/CommunicationRenderer.svelte` | thin: `commEventToLine` + `<CommunicationLine>`; keep `data-testid="event"` wrapper | Modify |
| `web/src/lib/components/terminal/CommunicationRenderer.svelte.test.ts` | behavior-preserving baseline | Create |
| `web/src/lib/components/scenes/PoseCard.svelte` | Layout A chrome + `<CommunicationLine>` body; drop `--brand-cyan-*` | Modify |
| `web/src/lib/components/scenes/PoseCard.svelte.test.ts` | scene render + layout tests | Create |

**Run commands** (web uses pnpm directly, per `web/CLAUDE.md` — `task` is Go-only):

- Single test file: `cd web && pnpm test:unit <path>`
- Type check: `cd web && pnpm check`
- All web unit tests: `cd web && pnpm test:unit`

---

## Phase 1: Seam core

### Task 1: `CommLine` model + adapters

**Files:**

- Create: `web/src/lib/comm/commLine.ts`
- Test: `web/src/lib/comm/commLine.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/lib/comm/commLine.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect } from 'vitest';
import { commEventToLine, logEntryToLine, type CommEvent } from './commLine';
import type { LogEntry } from '$lib/scenes/types';

function ev(partial: Partial<CommEvent>): CommEvent {
  return {
    type: 'core-communication:say',
    category: 'communication',
    format: 'speech',
    actor: 'Bob',
    text: 'Hello there.',
    metadata: {},
    ...partial,
  };
}

describe('commEventToLine', () => {
  it('maps a speech event to a say line carrying the label override', () => {
    const line = commEventToLine(ev({ format: 'speech', metadata: { label: 'whispers' } }));
    expect(line).toEqual({ kind: 'say', actor: 'Bob', text: 'Hello there.', label: 'whispers', channel: undefined });
  });

  it('maps an action event to a pose line carrying no_space', () => {
    const line = commEventToLine(ev({ format: 'action', text: "'s eyes narrow", metadata: { no_space: true } }));
    expect(line).toEqual({ kind: 'pose', actor: 'Bob', text: "'s eyes narrow", noSpace: true, channel: undefined });
  });

  it('maps an ooc-typed event to an ooc line with style and prefix defaults', () => {
    const line = commEventToLine(ev({ type: 'core-communication:ooc', format: 'speech', text: 'brb' }));
    expect(line).toEqual({ kind: 'ooc', actor: 'Bob', text: 'brb', oocStyle: 'say', oocPrefix: '[OOC]', channel: undefined });
  });

  it('treats an ooc_prefix in metadata as ooc even without the ooc type', () => {
    const line = commEventToLine(ev({ metadata: { ooc_prefix: '[ic-ooc]', style: 'pose' } }));
    expect(line.kind).toBe('ooc');
    expect(line.oocStyle).toBe('pose');
    expect(line.oocPrefix).toBe('[ic-ooc]');
  });

  it('falls back to emit for an unknown format', () => {
    const line = commEventToLine(ev({ type: 'core-communication:pemit', format: 'pemit', text: 'A bell rings.' }));
    expect(line).toEqual({ kind: 'emit', actor: 'Bob', text: 'A bell rings.' });
  });
});

describe('logEntryToLine', () => {
  const base: LogEntry = { id: 'e1', kind: 'say', actorId: 'c1', actorName: 'Alice', text: 'Hi', timestampMs: 0 };

  it('maps a say LogEntry to a say line', () => {
    expect(logEntryToLine({ ...base, kind: 'say' })).toEqual({ kind: 'say', actor: 'Alice', text: 'Hi' });
  });
  it('maps a pose LogEntry to a pose line', () => {
    expect(logEntryToLine({ ...base, kind: 'pose', text: 'waves' })).toEqual({ kind: 'pose', actor: 'Alice', text: 'waves' });
  });
  it('maps an ooc LogEntry to an ooc line', () => {
    expect(logEntryToLine({ ...base, kind: 'ooc', text: 'brb' })).toEqual({ kind: 'ooc', actor: 'Alice', text: 'brb' });
  });
  it('maps a system LogEntry to an emit line', () => {
    expect(logEntryToLine({ ...base, kind: 'system', text: 'The lamp flickers.' })).toEqual({ kind: 'emit', actor: 'Alice', text: 'The lamp flickers.' });
  });
  it('falls back actor to actorId then Unknown', () => {
    expect(logEntryToLine({ ...base, actorName: '', actorId: 'c9' }).actor).toBe('c9');
    expect(logEntryToLine({ ...base, actorName: '', actorId: '' }).actor).toBe('Unknown');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && pnpm test:unit src/lib/comm/commLine.test.ts`
Expected: FAIL — `Failed to resolve import './commLine'`.

- [ ] **Step 3: Write minimal implementation**

```ts
// web/src/lib/comm/commLine.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import type { LogEntry } from '$lib/scenes/types';

// CommKind is the logical communication kind, vocabulary-independent.
// `emit` covers system/GM narration (pemit-styled).
export type CommKind = 'say' | 'pose' | 'ooc' | 'emit';

// CommLine is the normalized model every web communication surface renders.
// Optional fields are populated by the richer terminal vocabulary; scene
// events leave them undefined and the primitive applies defaults.
export interface CommLine {
  kind: CommKind;
  actor: string;
  text: string;
  label?: string; // say verb override; default "says"
  noSpace?: boolean; // semipose (no space before action)
  oocStyle?: 'say' | 'pose' | 'semipose'; // default "say"
  oocPrefix?: string; // default "[OOC]"
  channel?: string;
}

// CommEvent mirrors the terminal event shape consumed by EventRenderer /
// CommunicationRenderer (internal/web/translate.go produces it).
export interface CommEvent {
  type: string;
  category: string;
  format: string;
  actor: string;
  text: string;
  metadata?: Record<string, unknown>;
}

// commEventToLine adapts a core-communication:* terminal event into a CommLine.
// Preserves the exact branching CommunicationRenderer used: ooc by type or
// ooc_prefix; speech→say; action→pose; otherwise emit (pemit).
export function commEventToLine(event: CommEvent): CommLine {
  const md = event.metadata ?? {};
  const oocPrefix = md['ooc_prefix'] as string | undefined;
  const channel = md['channel'] as string | undefined;
  const isOoc = event.type === 'core-communication:ooc' || !!oocPrefix;

  if (isOoc) {
    return {
      kind: 'ooc',
      actor: event.actor,
      text: event.text,
      oocStyle: (md['style'] as 'say' | 'pose' | 'semipose') ?? 'say',
      oocPrefix: oocPrefix ?? '[OOC]',
      channel,
    };
  }
  if (event.format === 'speech') {
    return { kind: 'say', actor: event.actor, text: event.text, label: md['label'] as string | undefined, channel };
  }
  if (event.format === 'action') {
    return { kind: 'pose', actor: event.actor, text: event.text, noSpace: md['no_space'] as boolean | undefined, channel };
  }
  return { kind: 'emit', actor: event.actor, text: event.text };
}

// logEntryToLine adapts a scene LogEntry into a CommLine. Scene events are
// coarser (no label/no_space/ooc style metadata), so optional fields stay
// undefined and the primitive's defaults apply. `system` → `emit`.
export function logEntryToLine(entry: LogEntry): CommLine {
  const kind: CommKind = entry.kind === 'system' ? 'emit' : entry.kind;
  return { kind, actor: entry.actorName || entry.actorId || 'Unknown', text: entry.text };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && pnpm test:unit src/lib/comm/commLine.test.ts`
Expected: PASS (15 assertions).

- [ ] **Step 5: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`:
`jj commit -m "feat(web): CommLine model + commEventToLine/logEntryToLine adapters (holomush-c5zol)"`

---

### Task 2: `CommunicationLine.svelte` shared primitive

**Files:**

- Create: `web/src/lib/comm/CommunicationLine.svelte`
- Test: `web/src/lib/comm/CommunicationLine.svelte.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/lib/comm/CommunicationLine.svelte.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import CommunicationLine from './CommunicationLine.svelte';
import type { CommLine } from './commLine';

function render(line: CommLine): { text: string; html: string } {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(CommunicationLine, { target, props: { line } });
  const text = (target.textContent ?? '').replace(/\s+/g, ' ').trim();
  const html = target.innerHTML;
  unmount(component);
  target.remove();
  return { text, html };
}

afterEach(() => document.body.replaceChildren());

describe('CommunicationLine', () => {
  it('renders a say as actor + says + quoted speech', () => {
    expect(render({ kind: 'say', actor: 'Bob', text: 'Hello there.' }).text).toBe('Bob says, "Hello there."');
  });
  it('honors a say label override', () => {
    expect(render({ kind: 'say', actor: 'Bob', text: 'psst', label: 'whispers' }).text).toBe('Bob whispers, "psst"');
  });
  it('renders a pose as actor inline with the action', () => {
    expect(render({ kind: 'pose', actor: 'Alice', text: 'smiles warmly.' }).text).toBe('Alice smiles warmly.');
  });
  it('omits the actor-action space for a semipose', () => {
    expect(render({ kind: 'pose', actor: 'Alice', text: "'s eyes narrow.", noSpace: true }).text).toBe("Alice's eyes narrow.");
  });
  it('renders ooc default style as prefixed speech', () => {
    expect(render({ kind: 'ooc', actor: 'Bob', text: 'brb' }).text).toBe('[OOC] Bob says, "brb"');
  });
  it('renders emit as bare narration', () => {
    expect(render({ kind: 'emit', actor: '', text: 'A bell rings in the distance.' }).text).toBe('A bell rings in the distance.');
  });
  it('uses --mush-* tokens, not brand colors', () => {
    expect(render({ kind: 'say', actor: 'Bob', text: 'hi' }).html).toContain('class="speaker"');
  });
  it('escapes HTML in text (SEAM-3)', () => {
    const { html } = render({ kind: 'say', actor: 'Bob', text: '<img src=x onerror=alert(1)>' });
    expect(html).not.toContain('<img');
    expect(html).toContain('&lt;img');
  });
  it('linkifies a URL in text', () => {
    const { html } = render({ kind: 'emit', actor: '', text: 'see https://example.com now' });
    expect(html).toContain('<a href="https://example.com"');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && pnpm test:unit src/lib/comm/CommunicationLine.svelte.test.ts`
Expected: FAIL — cannot resolve `./CommunicationLine.svelte`.

- [ ] **Step 3: Write minimal implementation**

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { linkUrls } from '$lib/util/urlLinker';
  import type { CommLine } from './commLine';

  let { line }: { line: CommLine } = $props();
</script>

{#if line.kind === 'ooc'}
  {#if line.oocStyle === 'pose'}
    <span class="ooc-prefix">{line.oocPrefix ?? '[OOC]'}</span>{' '}<span class="ooc-actor">{line.actor}</span>{' '}<span class="ooc-message">{@html linkUrls(line.text)}</span>
  {:else if line.oocStyle === 'semipose'}
    <span class="ooc-prefix">{line.oocPrefix ?? '[OOC]'}</span>{' '}<span class="ooc-actor">{line.actor}</span><span class="ooc-message">{@html linkUrls(line.text)}</span>
  {:else}
    <span class="ooc-prefix">{line.oocPrefix ?? '[OOC]'}</span>{' '}<span class="ooc-speaker">{line.actor}</span> says, <span class="ooc-message">"{@html linkUrls(line.text)}"</span>
  {/if}
{:else if line.kind === 'say'}
  {#if line.channel}<span class="channel-prefix">[{line.channel}]</span>{' '}{/if}<span class="speaker">{line.actor}</span>{' '}{line.label ?? 'says'},{' '}<span class="speech">"{@html linkUrls(line.text)}"</span>
{:else if line.kind === 'pose'}
  {#if line.channel}<span class="channel-prefix">[{line.channel}]</span>{' '}{/if}<span class="actor">{line.actor}</span>{#if !line.noSpace}{' '}{/if}<span class="action">{@html linkUrls(line.text)}</span>
{:else}
  <span class="pemit-message">{@html linkUrls(line.text)}</span>
{/if}

<style>
  .channel-prefix { color: var(--mush-system); font-weight: bold; }
  .speaker { color: var(--mush-say-speaker); }
  .speech { color: var(--mush-say-speech); }
  .actor { color: var(--mush-pose-actor); }
  .action { color: var(--mush-pose-action); }
  .ooc-prefix { color: var(--mush-ooc); font-weight: bold; }
  .ooc-speaker { color: var(--mush-ooc); }
  .ooc-actor { color: var(--mush-ooc); }
  .ooc-message { color: var(--mush-ooc); opacity: 0.85; }
  .pemit-message { color: var(--mush-pemit); font-style: italic; }
</style>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && pnpm test:unit src/lib/comm/CommunicationLine.svelte.test.ts`
Expected: PASS (9 assertions).

- [ ] **Step 5: Commit**

`jj commit -m "feat(web): shared CommunicationLine phrasing primitive (holomush-c5zol)"`

---

## Phase 2: Terminal onto the seam (behavior-preserving)

### Task 3: Re-point `CommunicationRenderer` at the primitive

**Files:**

- Modify: `web/src/lib/components/terminal/CommunicationRenderer.svelte`
- Test: `web/src/lib/components/terminal/CommunicationRenderer.svelte.test.ts`

- [ ] **Step 1: Write the characterization test** (passes against the *current* renderer; it is the regression net the refactor must preserve — not a red-first test)

```ts
// web/src/lib/components/terminal/CommunicationRenderer.svelte.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import CommunicationRenderer from './CommunicationRenderer.svelte';

interface Ev { type: string; category: string; format: string; actor: string; text: string; metadata?: Record<string, unknown>; }

function render(event: Ev): { text: string; hasTestId: boolean } {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(CommunicationRenderer, { target, props: { event } });
  const text = (target.textContent ?? '').replace(/\s+/g, ' ').trim();
  const hasTestId = target.querySelector('[data-testid="event"]') !== null;
  unmount(component);
  target.remove();
  return { text, hasTestId };
}

afterEach(() => document.body.replaceChildren());

describe('CommunicationRenderer (post-seam, behavior-preserving)', () => {
  it('renders a speech event as actor says quote and keeps the event testid', () => {
    const { text, hasTestId } = render({ type: 'core-communication:say', category: 'communication', format: 'speech', actor: 'Bob', text: 'Hi.' });
    expect(text).toBe('Bob says, "Hi."');
    expect(hasTestId).toBe(true);
  });
  it('renders an action event with the actor inline', () => {
    expect(render({ type: 'core-communication:pose', category: 'communication', format: 'action', actor: 'Alice', text: 'waves.' }).text).toBe('Alice waves.');
  });
  it('renders an ooc event with the [OOC] prefix', () => {
    expect(render({ type: 'core-communication:ooc', category: 'communication', format: 'speech', actor: 'Bob', text: 'brb' }).text).toBe('[OOC] Bob says, "brb"');
  });
});
```

- [ ] **Step 2: Run test to verify it PASSES against the current renderer**

Run: `cd web && pnpm test:unit src/lib/components/terminal/CommunicationRenderer.svelte.test.ts`
Expected: PASS. The current renderer (`CommunicationRenderer.svelte:29`) already carries `data-testid="event"` and already emits this exact phrasing, so this is a **characterization** test, not a red-first test. It locks in current behavior so the Step 3 extraction can be proven behavior-preserving — it must still pass afterward.

- [ ] **Step 3: Apply the refactor** — replace the entire file with:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import CommunicationLine from '$lib/comm/CommunicationLine.svelte';
  import { commEventToLine, type CommEvent } from '$lib/comm/commLine';

  let { event }: { event: CommEvent } = $props();
</script>

<div class="event event-{event.type}" data-testid="event">
  <CommunicationLine line={commEventToLine(event)} />
</div>

<style>
  .event { line-height: 1.7; }
</style>
```

> Note: the existing E2E suite `web/e2e/terminal.spec.ts` already asserts on `[data-testid="event"]`; the wrapper is preserved so that gate stays green.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && pnpm test:unit src/lib/components/terminal/CommunicationRenderer.svelte.test.ts`
Expected: PASS (3 assertions).
Run: `cd web && pnpm check`
Expected: no type errors.

- [ ] **Step 5: Commit**

`jj commit -m "refactor(web): terminal CommunicationRenderer delegates to CommunicationLine (holomush-c5zol)"`

---

## Phase 3: Scene rendering parity (holomush-5rh.33)

### Task 4: `PoseCard` Layout A + primitive body

**Files:**

- Modify: `web/src/lib/components/scenes/PoseCard.svelte`
- Test: `web/src/lib/components/scenes/PoseCard.svelte.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/lib/components/scenes/PoseCard.svelte.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import PoseCard from './PoseCard.svelte';
import type { LogEntry } from '$lib/scenes/types';

function entry(p: Partial<LogEntry>): LogEntry {
  return { id: 'e1', kind: 'say', actorId: 'c1', actorName: 'Bazian', text: 'Hold the line.', timestampMs: 1_717_000_000_000, ...p };
}

function render(e: LogEntry): { text: string; html: string } {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(PoseCard, { target, props: { entry: e } });
  const text = (target.textContent ?? '').replace(/\s+/g, ' ').trim();
  const html = target.innerHTML;
  unmount(component);
  target.remove();
  return { text, html };
}

afterEach(() => document.body.replaceChildren());

describe('PoseCard', () => {
  it('renders a say with canonical phrasing (actor once, quoted)', () => {
    const { text } = render(entry({ kind: 'say', actorName: 'Bazian', text: 'Hold the line.' }));
    expect(text).toContain('Bazian says, "Hold the line."');
    // actor appears exactly once in the body line (no separate name banner)
    expect((text.match(/Bazian says/g) ?? []).length).toBe(1);
  });
  it('renders a pose with the actor inline', () => {
    expect(render(entry({ kind: 'pose', actorName: 'Foob', text: 'draws his blade.' })).text).toContain('Foob draws his blade.');
  });
  it('renders ooc distinctly via the [OOC] prefix, not the ad-hoc form', () => {
    const { text } = render(entry({ kind: 'ooc', actorName: 'Foob', text: 'brb' }));
    expect(text).toContain('[OOC] Foob says, "brb"');
    expect(text).not.toContain('(ooc)');
  });
  it('renders system as bare narration', () => {
    expect(render(entry({ kind: 'system', actorName: '', text: 'The torches gutter.' })).text).toContain('The torches gutter.');
  });
  it('shows an understated timestamp on the left rail', () => {
    expect(render(entry({})).html).toContain('class="ts"');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && pnpm test:unit src/lib/components/scenes/PoseCard.svelte.test.ts`
Expected: FAIL — current PoseCard renders italic body under a cyan banner; no `says,`, no `class="ts"`, ooc uses `(ooc)`.

- [ ] **Step 3: Replace the entire file with** (Layout A — understated left time + glance avatar, canonical phrasing body):

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import type { LogEntry } from '$lib/scenes/types';
  import CommunicationLine from '$lib/comm/CommunicationLine.svelte';
  import { logEntryToLine } from '$lib/comm/commLine';

  let { entry }: { entry: LogEntry } = $props();

  function initials(name: string): string {
    const parts = name.split(/\s+/).filter(Boolean);
    if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
    return name.slice(0, 2).toUpperCase();
  }

  function formatTime(ms: number): string {
    return new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }

  const line = $derived(logEntryToLine(entry));
  const showAvatar = $derived(entry.kind !== 'system');
</script>

<article class="row">
  <span class="ts">{entry.timestampMs ? formatTime(entry.timestampMs) : ''}</span>
  {#if showAvatar}
    <span class="avatar" aria-hidden="true">{initials(entry.actorName || '?')}</span>
  {:else}
    <span class="avatar avatar-empty" aria-hidden="true"></span>
  {/if}
  <div class="body">
    {#if entry.contentWarning}
      <details>
        <summary class="cw">CW: {entry.contentWarning}</summary>
        <CommunicationLine {line} />
      </details>
    {:else}
      <CommunicationLine {line} />
    {/if}
  </div>
</article>

<style>
  .row {
    display: grid;
    grid-template-columns: 46px 22px 1fr;
    gap: 10px;
    align-items: baseline;
    padding: 3px 14px;
  }
  .row:hover { background: color-mix(in srgb, var(--color-muted) 30%, transparent); }
  .ts {
    text-align: right;
    color: var(--color-muted-foreground);
    opacity: 0.75;
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    font-family: var(--font-mono, ui-monospace, monospace);
  }
  .avatar {
    display: inline-flex; align-items: center; justify-content: center;
    width: 22px; height: 22px; border-radius: 999px;
    background: color-mix(in srgb, var(--mush-say-speaker) 20%, transparent);
    color: var(--mush-say-speaker);
    font-size: 9px; font-weight: 700; flex-shrink: 0;
  }
  .avatar-empty { background: transparent; }
  .body { min-width: 0; font-size: 14px; line-height: 1.55; }
  .cw { cursor: pointer; color: var(--color-muted-foreground); font-size: 12px; font-style: italic; margin-bottom: 4px; }
</style>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && pnpm test:unit src/lib/components/scenes/PoseCard.svelte.test.ts`
Expected: PASS (5 assertions).
Run: `cd web && pnpm check`
Expected: no type errors.

- [ ] **Step 5: Commit**

`jj commit -m "fix(web): scene PoseCard renders canonical say/pose/ooc via shared primitive (holomush-5rh.33)"`

---

### Task 5: SEAM-1 parity + SEAM-4 Go↔TS golden

**Files:**

- Create: `web/src/lib/comm/parity.test.ts`

- [ ] **Step 1: Write the test**

```ts
// web/src/lib/comm/parity.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { mount, unmount } from 'svelte';
import CommunicationLine from './CommunicationLine.svelte';
import { commEventToLine, logEntryToLine, type CommEvent } from './commLine';
import type { LogEntry } from '$lib/scenes/types';

function renderText(line: ReturnType<typeof logEntryToLine>): string {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const c = mount(CommunicationLine, { target, props: { line } });
  const text = (target.textContent ?? '').replace(/\s+/g, ' ').trim();
  unmount(c);
  target.remove();
  return text;
}

afterEach(() => document.body.replaceChildren());

// SEAM-1: the terminal and scene adapters converge — the same logical kind
// renders identically regardless of which vocabulary produced it.
describe('SEAM-1 terminal↔scene parity', () => {
  const cases: { event: CommEvent; entry: LogEntry; expected: string }[] = [
    {
      event: { type: 'core-communication:say', category: 'communication', format: 'speech', actor: 'Bob', text: 'Hi.', metadata: {} },
      entry: { id: '1', kind: 'say', actorId: 'c', actorName: 'Bob', text: 'Hi.', timestampMs: 0 },
      expected: 'Bob says, "Hi."',
    },
    {
      event: { type: 'core-communication:pose', category: 'communication', format: 'action', actor: 'Alice', text: 'waves.', metadata: {} },
      entry: { id: '2', kind: 'pose', actorId: 'c', actorName: 'Alice', text: 'waves.', timestampMs: 0 },
      expected: 'Alice waves.',
    },
    {
      event: { type: 'core-communication:ooc', category: 'communication', format: 'speech', actor: 'Foob', text: 'brb', metadata: {} },
      entry: { id: '3', kind: 'ooc', actorId: 'c', actorName: 'Foob', text: 'brb', timestampMs: 0 },
      expected: '[OOC] Foob says, "brb"',
    },
  ];

  for (const c of cases) {
    it(`renders "${c.expected}" identically from both vocabularies`, () => {
      const fromEvent = renderText(commEventToLine(c.event));
      const fromEntry = renderText(logEntryToLine(c.entry));
      expect(fromEvent).toBe(c.expected);
      expect(fromEntry).toBe(c.expected);
    });
  }
});

// SEAM-4: TS phrasing matches the Go renderPlainText golden for say/pose/emit.
// Golden file is pinned Go-side by publish_render_test.go:74.
describe('SEAM-4 Go↔TS golden', () => {
  it('matches publish_render_plain_text.golden for say/pose/emit', () => {
    const golden = readFileSync(new URL('../../../../plugins/core-scenes/testdata/publish_render_plain_text.golden', import.meta.url), 'utf8')
      .split('\n').filter((l) => l.length > 0);
    // Golden lines correspond to these entries (speaker/kind/content):
    const entries: LogEntry[] = [
      { id: '1', kind: 'pose', actorId: '', actorName: 'Alice', text: 'smiles warmly.', timestampMs: 0 },
      { id: '2', kind: 'say', actorId: '', actorName: 'Bob', text: 'Hello there.', timestampMs: 0 },
      { id: '3', kind: 'system', actorId: '', actorName: '', text: 'A bell rings in the distance.', timestampMs: 0 },
    ];
    const rendered = entries.map((e) => renderText(logEntryToLine(e)));
    expect(rendered).toEqual(golden);
  });
});
```

- [ ] **Step 2: Run test to verify it passes**

Run: `cd web && pnpm test:unit src/lib/comm/parity.test.ts`
Expected: PASS. If the golden read fails, confirm `plugins/core-scenes/testdata/publish_render_plain_text.golden` exists from `web/` cwd via `../plugins/...`.

- [ ] **Step 3: Commit**

`jj commit -m "test(web): SEAM-1 terminal↔scene parity + SEAM-4 Go↔TS golden (holomush-c5zol)"`

---

### Task 6: SEAM-2 brand-color grep guard

**Files:**

- Create: `web/src/lib/comm/seam-guard.test.ts`

- [ ] **Step 1: Write the test** (mirrors the `themeStore.test.ts` source-grep precedent)

```ts
// web/src/lib/comm/seam-guard.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';

const src = (rel: string) => readFileSync(`${process.cwd()}/src/lib/components/${rel}`, 'utf8');

// SEAM-2: message renderers MUST color via --mush-* tokens, never brand colors.
describe('SEAM-2 message renderers use --mush-* not brand colors', () => {
  it('PoseCard uses no --brand-* color', () => {
    expect(src('scenes/PoseCard.svelte')).not.toContain('--brand-');
  });
  it('CommunicationRenderer uses no --brand-* color', () => {
    expect(src('terminal/CommunicationRenderer.svelte')).not.toContain('--brand-');
  });
  it('the shared primitive uses --mush- tokens', () => {
    expect(readFileSync(`${process.cwd()}/src/lib/comm/CommunicationLine.svelte`, 'utf8')).toContain('var(--mush-say-speaker)');
  });
});
```

- [ ] **Step 2: Run test to verify it passes**

Run: `cd web && pnpm test:unit src/lib/comm/seam-guard.test.ts`
Expected: PASS (PoseCard's `--brand-cyan-*` was removed in Task 4).

- [ ] **Step 3: Commit**

`jj commit -m "test(web): SEAM-2 guard — no brand colors in message renderers (holomush-c5zol)"`

---

## Final verification

- [ ] **Full web unit suite + type check:**

Run: `cd web && pnpm check && pnpm test:unit`
Expected: all green, including the new `comm/*` tests and the preserved terminal renderer tests.

- [ ] **Acceptance (holomush-5rh.33):** a say renders `{actor} says, "{text}"` with `--mush-say-*`; a pose renders the actor inline with `--mush-pose-*`; OOC renders `[OOC] …` distinctly; no `--brand-cyan-*` in message colors; identical for live events and the `/scenes/[id]` jsonl-export viewer (both consume `PoseCard`).

- [ ] **Update beads:** `bd close holomush-5rh.33` once acceptance passes; reference the implementing commits. (holomush-5rh.32 stays open — fixed later by the focus-routed-input effort.)
<!-- adr-capture: sha256=a34ebab6ecc3d525; session=ea731fa7; ts=2026-06-26T00:48:00Z; adrs=holomush-914rn,holomush-bbwe7 -->
