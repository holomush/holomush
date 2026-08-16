// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, expect, it } from 'vitest';
import { formatLastActive } from './lastActive';

/**
 * The six declared buckets, each boundary asserted AT the value and ONE STEP
 * EITHER SIDE, so an off-by-one in a bucket edge fails rather than passing on
 * a value comfortably inside the band.
 *
 * The stored column is epoch NANOSECONDS in a BIGINT (INV-STORE-1), so every
 * fixture is built in nanos. `0` is the sentinel — an absence, not a very old
 * value.
 */

const NOW = new Date('2026-08-14T12:00:00.000Z');
const NOW_NANOS = BigInt(NOW.getTime()) * 1_000_000n;

const SECOND = 1_000n * 1_000_000n;
const MINUTE = 60n * SECOND;
const HOUR = 60n * MINUTE;
const DAY = 24n * HOUR;

/** A stored value that is `ago` nanoseconds before NOW. */
const ago = (nanos: bigint) => NOW_NANOS - nanos;

describe('formatLastActive', () => {
  it('renders the 0 sentinel as never', () => {
    expect(formatLastActive(0n, NOW)).toBe('never');
  });

  it('renders 1 nanosecond past the epoch as a real age, not never', () => {
    // One step off the sentinel. A `!value` guard would wrongly call this
    // never; only an equality against 0 distinguishes the sentinel from the
    // instant after it.
    expect(formatLastActive(1n, NOW)).not.toBe('never');
  });

  it('renders 59 minutes and 59 seconds ago as < 1h ago', () => {
    expect(formatLastActive(ago(59n * MINUTE + 59n * SECOND), NOW)).toBe('< 1h ago');
  });

  it('renders exactly 60 minutes ago as 1h ago', () => {
    expect(formatLastActive(ago(60n * MINUTE), NOW)).toBe('1h ago');
  });

  it('renders 60 minutes and one second ago as 1h ago', () => {
    expect(formatLastActive(ago(60n * MINUTE + SECOND), NOW)).toBe('1h ago');
  });

  it('renders 23 hours and 59 minutes ago as 23h ago', () => {
    expect(formatLastActive(ago(23n * HOUR + 59n * MINUTE), NOW)).toBe('23h ago');
  });

  it('renders exactly 24 hours ago as 1d ago', () => {
    expect(formatLastActive(ago(24n * HOUR), NOW)).toBe('1d ago');
  });

  it('renders 24 hours and one minute ago as 1d ago', () => {
    expect(formatLastActive(ago(24n * HOUR + MINUTE), NOW)).toBe('1d ago');
  });

  it('renders 29 days and 23 hours ago as 29d ago', () => {
    expect(formatLastActive(ago(29n * DAY + 23n * HOUR), NOW)).toBe('29d ago');
  });

  it('renders exactly 30 days ago as 1mo ago', () => {
    expect(formatLastActive(ago(30n * DAY), NOW)).toBe('1mo ago');
  });

  it('renders 30 days and one hour ago as 1mo ago', () => {
    expect(formatLastActive(ago(30n * DAY + HOUR), NOW)).toBe('1mo ago');
  });

  it('renders 359 days ago as 11mo ago', () => {
    // One step below the year edge: a month is 30 days here, so 11 months is
    // 330 days and the eleven-month band runs to 359.
    expect(formatLastActive(ago(359n * DAY), NOW)).toBe('11mo ago');
  });

  it('renders exactly 330 days ago as 11mo ago', () => {
    expect(formatLastActive(ago(330n * DAY), NOW)).toBe('11mo ago');
  });

  it('renders exactly 360 days ago as 1y ago', () => {
    expect(formatLastActive(ago(360n * DAY), NOW)).toBe('1y ago');
  });

  it('renders 361 days ago as 1y ago', () => {
    expect(formatLastActive(ago(361n * DAY), NOW)).toBe('1y ago');
  });

  it('renders 720 days ago as 2y ago', () => {
    expect(formatLastActive(ago(720n * DAY), NOW)).toBe('2y ago');
  });

  it('renders a value in the future as < 1h ago, never a negative bucket', () => {
    // Clock skew between the writer and the reader. The rendered text must not
    // become `-1h ago` or an "in 3h" future phrasing.
    const skewed = formatLastActive(NOW_NANOS + 3n * HOUR, NOW);
    expect(skewed).toBe('< 1h ago');
  });

  it('renders exactly now as < 1h ago', () => {
    expect(formatLastActive(NOW_NANOS, NOW)).toBe('< 1h ago');
  });

  it('accepts a number as well as a bigint for the same instant', () => {
    // The generated client types int64 as bigint, but a hand-built fixture or a
    // JSON round-trip can arrive as a number. Both must land in one bucket.
    const nanos = ago(2n * HOUR);
    expect(formatLastActive(nanos, NOW)).toBe('2h ago');
    expect(formatLastActive(Number(nanos), NOW)).toBe('2h ago');
  });

  it('does not lose the age of a 2026 timestamp to double precision', () => {
    // Epoch nanoseconds for any current date (~1.8e18) exceed
    // Number.MAX_SAFE_INTEGER (~9.0e15). A conversion to Number BEFORE the
    // divide down to milliseconds rounds to the nearest multiple of 256ns and
    // the arithmetic silently drifts. This case pins that the reduction happens
    // in bigint.
    expect(NOW_NANOS > BigInt(Number.MAX_SAFE_INTEGER)).toBe(true);
    expect(formatLastActive(ago(5n * HOUR), NOW)).toBe('5h ago');
  });
});
