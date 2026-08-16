// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, expect, it } from 'vitest';
import { byteCount } from './byteCount';

/**
 * The oracle is INDEPENDENT of the function under test, deliberately.
 *
 * Recomputing the expected value here through the same expression the module
 * exports would make every case agree with the code by construction and prove
 * nothing. The literals below are hand-computed from UTF-8's own rules: ASCII
 * is one byte, a BMP CJK ideograph is three, and a non-BMP (astral) codepoint
 * is four.
 */

/** U+4E09 — three UTF-8 bytes, one UTF-16 code unit, one codepoint. */
const CJK = '三';
/** U+10400 DESERET CAPITAL LETTER LONG I — four UTF-8 bytes, TWO UTF-16 code units. */
const ASTRAL = '𐐀';

/** The two caps the thirteen admin-writable paths split across. */
const SHORT_CAP = 100; // world.MaxNameLength
const LONG_CAP = 4000; // world.MaxDescriptionLength

describe('byteCount — ASCII, where every counting rule agrees', () => {
  it('counts the empty string as zero', () => {
    expect(byteCount('')).toBe(0);
  });

  it('counts one byte per ASCII character', () => {
    expect(byteCount('a'.repeat(99))).toBe(99);
    expect(byteCount('a'.repeat(100))).toBe(100);
    expect(byteCount('a'.repeat(101))).toBe(101);
  });
});

describe('byteCount — the short cap (world.MaxNameLength = 100)', () => {
  it('flips the over-cap predicate at exactly 101 bytes and not at 100', () => {
    expect(byteCount('a'.repeat(99)) > SHORT_CAP).toBe(false);
    // Exactly at the cap is ACCEPTABLE: the facade compares len(v) > cap, so a
    // client that called this over would refuse a value the server takes.
    expect(byteCount('a'.repeat(100)) > SHORT_CAP).toBe(false);
    expect(byteCount('a'.repeat(101)) > SHORT_CAP).toBe(true);
  });

  it('reports UTF-8 byte length for a CJK value that crosses the cap while its rune count does not', () => {
    // 34 codepoints × 3 bytes = 102. A codepoint counter reads 34 and would
    // call this comfortably under a 100-byte cap the server rejects.
    const value = CJK.repeat(34);
    expect([...value]).toHaveLength(34);
    expect(byteCount(value)).toBe(102);
    expect(byteCount(value) > SHORT_CAP).toBe(true);
  });

  it('reports UTF-8 byte length for an astral value, which a UTF-16 code-unit count gets wrong', () => {
    // 25 codepoints × 4 bytes = 100 — exactly at the cap, so ACCEPTED. The
    // string's own UTF-16 code-unit count is 50, and a codepoint count is 25;
    // neither is the number the server compares against.
    const value = ASTRAL.repeat(25);
    expect([...value]).toHaveLength(25);
    expect(byteCount(value)).toBe(100);
    expect(byteCount(value) > SHORT_CAP).toBe(false);
    // One more codepoint crosses it.
    expect(byteCount(ASTRAL.repeat(26))).toBe(104);
    expect(byteCount(ASTRAL.repeat(26)) > SHORT_CAP).toBe(true);
  });
});

describe('byteCount — the long cap (world.MaxDescriptionLength = 4000)', () => {
  it('flips the over-cap predicate at exactly 4001 bytes and not at 4000', () => {
    expect(byteCount('a'.repeat(3999)) > LONG_CAP).toBe(false);
    expect(byteCount('a'.repeat(4000)) > LONG_CAP).toBe(false);
    expect(byteCount('a'.repeat(4001)) > LONG_CAP).toBe(true);
  });

  it('accepts 101 bytes on the long cap, which the short cap refuses', () => {
    // The thirteen paths do NOT share one cap. Asserting the short boundary on
    // a long field would demand behaviour the server correctly declines to
    // exhibit.
    expect(byteCount('a'.repeat(101)) > LONG_CAP).toBe(false);
  });

  it('reports UTF-8 byte length for a CJK value that crosses the long cap while its rune count does not', () => {
    // 1334 codepoints × 3 bytes = 4002.
    const value = CJK.repeat(1334);
    expect([...value]).toHaveLength(1334);
    expect(byteCount(value)).toBe(4002);
    expect(byteCount(value) > LONG_CAP).toBe(true);
    // 1333 codepoints × 3 = 3999 — under, by one byte.
    expect(byteCount(CJK.repeat(1333))).toBe(3999);
    expect(byteCount(CJK.repeat(1333)) > LONG_CAP).toBe(false);
  });
});

describe('byteCount — mixed scripts', () => {
  it('sums each codepoint at its own UTF-8 width', () => {
    // 'a' (1) + CJK (3) + ASTRAL (4) = 8.
    expect(byteCount(`a${CJK}${ASTRAL}`)).toBe(8);
  });

  it('counts a full-width Latin letter as three bytes, not one', () => {
    // U+FF2D FULLWIDTH LATIN CAPITAL LETTER M — looks like ASCII, is not.
    expect(byteCount('Ｍ')).toBe(3);
  });
});
