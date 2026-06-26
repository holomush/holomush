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
