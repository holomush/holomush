// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/** One millisecond, in nanoseconds. */
const NANOS_PER_MS = 1_000_000n;

const MS_PER_HOUR = 60 * 60 * 1000;
const HOURS_PER_DAY = 24;
/** A month is thirty days here. The buckets are coarse by design. */
const DAYS_PER_MONTH = 30;
const MONTHS_PER_YEAR = 12;

/**
 * Renders `characters.last_active_at` as the coarse relative text the admin
 * table draws under the column label `Last active`.
 *
 * SIX BUCKETS, FIXED:
 *
 *   | stored     | renders     |
 *   |------------|-------------|
 *   | 0          | never       |
 *   | < 1 hour   | < 1h ago    |
 *   | 1-23 hours | {n}h ago    |
 *   | 1-29 days  | {n}d ago    |
 *   | 1-11 months| {n}mo ago   |
 *   | >= 12 mo   | {n}y ago    |
 *
 * THE INPUT IS EPOCH NANOSECONDS (INV-STORE-1), not milliseconds. `0` is a
 * SENTINEL meaning "has never been active" — it is an absence, not a very old
 * value, and there is no NULL to test for. That is why the guard is an equality
 * against zero rather than a falsiness check: the instant one nanosecond after
 * the epoch is a real, very old age.
 *
 * THE RETURN IS A STRING AND NOTHING ELSE. There is deliberately no second
 * return value, no companion field and no exported formatter carrying the
 * absolute instant, so a caller cannot put a precise timestamp behind a hover.
 * The stored value lags by up to one flush interval (AR-03-03, accepted in
 * Phase 3); a precise stamp reads as more authoritative than the data is, which
 * is the exact failure the coarse text exists to avoid. Making the absolute
 * value unreachable at the call site is what enforces that, rather than
 * trusting every caller to decline it.
 *
 * A stored value in the FUTURE (writer/reader clock skew) lands in the first
 * bucket and renders `< 1h ago`. It never produces a negative count and never
 * a forward-looking phrasing.
 *
 * `now` is injectable so the buckets are testable without freezing the clock.
 */
export function formatLastActive(epochNanos: bigint | number, now: Date = new Date()): string {
  const nanos = typeof epochNanos === 'bigint' ? epochNanos : BigInt(Math.trunc(epochNanos));
  if (nanos === 0n) return 'never';

  // Reduce to milliseconds IN BIGINT before converting. Epoch nanoseconds for
  // any current date (~1.8e18) are far beyond Number.MAX_SAFE_INTEGER, so a
  // Number() first would quantise the instant and drift the arithmetic.
  const thenMs = Number(nanos / NANOS_PER_MS);
  const elapsedMs = now.getTime() - thenMs;

  const hours = Math.floor(elapsedMs / MS_PER_HOUR);
  if (hours < 1) return '< 1h ago';
  if (hours < HOURS_PER_DAY) return `${hours}h ago`;

  const days = Math.floor(hours / HOURS_PER_DAY);
  if (days < DAYS_PER_MONTH) return `${days}d ago`;

  const months = Math.floor(days / DAYS_PER_MONTH);
  if (months < MONTHS_PER_YEAR) return `${months}mo ago`;

  return `${Math.floor(months / MONTHS_PER_YEAR)}y ago`;
}
