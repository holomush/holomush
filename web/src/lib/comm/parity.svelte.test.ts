// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
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

// A channelized message renders its [channel] prefix for every kind that
// carries one — say, pose, AND ooc. Regression guard: the ooc branch silently
// dropped the prefix while say/pose rendered it.
describe('channel prefix renders uniformly across channelized kinds', () => {
  const cases: { event: CommEvent; expected: string }[] = [
    {
      event: { type: 'core-communication:say', category: 'communication', format: 'speech', actor: 'Bob', text: 'Hi.', metadata: { channel: 'public' } },
      expected: '[public] Bob says, "Hi."',
    },
    {
      event: { type: 'core-communication:pose', category: 'communication', format: 'action', actor: 'Alice', text: 'waves.', metadata: { channel: 'public' } },
      expected: '[public] Alice waves.',
    },
    {
      event: { type: 'core-communication:ooc', category: 'communication', format: 'speech', actor: 'Foob', text: 'brb', metadata: { channel: 'public' } },
      expected: '[public] [OOC] Foob says, "brb"',
    },
  ];

  for (const c of cases) {
    it(`renders "${c.expected}"`, () => {
      expect(renderText(commEventToLine(c.event))).toBe(c.expected);
    });
  }
});

// SEAM-4: TS phrasing matches the Go renderPlainText golden for say/pose/emit.
// Golden file is pinned Go-side by publish_render_test.go:74.
describe('SEAM-4 Go↔TS golden', () => {
  it('matches publish_render_plain_text.golden for say/pose/emit', () => {
    // File-relative (not CWD-relative) so the parity check resolves regardless
    // of where Vitest is launched. import.meta.dirname is web/src/lib/comm.
    const golden = readFileSync(
      resolve(import.meta.dirname, '../../../../plugins/core-scenes/testdata/publish_render_plain_text.golden'),
      'utf8'
    )
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
