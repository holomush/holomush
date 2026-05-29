// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect } from 'vitest';
import { commandRoundtripAttributes } from './commandSpan';

describe('commandRoundtripAttributes', () => {
  it('records the full command input verbatim', () => {
    const attrs = commandRoundtripAttributes('look here', '01HCONN0000000000000000AAA');
    expect(attrs['command.input']).toBe('look here');
  });

  it('extracts command name from the first whitespace-delimited token', () => {
    const attrs = commandRoundtripAttributes('say hello world', '01HCONN0000000000000000AAA');
    expect(attrs['command.name']).toBe('say');
  });

  it('trims for the name but stores command.input verbatim', () => {
    const attrs = commandRoundtripAttributes('   pose waves   ', '01HCONN0000000000000000AAA');
    expect(attrs['command.name']).toBe('pose');
    expect(attrs['command.input']).toBe('   pose waves   ');
  });

  it('records connection_id and a true presence witness when connectionId is set', () => {
    const attrs = commandRoundtripAttributes('look', '01HCONN0000000000000000AAA');
    expect(attrs['connection_id']).toBe('01HCONN0000000000000000AAA');
    expect(attrs['connection_id.present']).toBe(true);
  });

  it('records a false presence witness when connectionId is empty (the dble7 signal)', () => {
    const attrs = commandRoundtripAttributes('look', '');
    expect(attrs['connection_id']).toBe('');
    expect(attrs['connection_id.present']).toBe(false);
  });

  it('yields an empty command name for an empty command without throwing', () => {
    const attrs = commandRoundtripAttributes('', '01HCONN0000000000000000AAA');
    expect(attrs['command.name']).toBe('');
    expect(attrs['command.input']).toBe('');
  });
});
