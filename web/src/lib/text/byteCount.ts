// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * How many BYTES a string occupies, because every field cap this client
 * displays is a byte cap enforced server-side.
 *
 * # Why bytes and nothing else
 *
 * Both facades compare `len(value) > cap` on a Go string — the character
 * authoring surface at internal/grpc/characteraccess_write.go:213-224 and the
 * admin edit surface at internal/grpc/admin_characters_write.go — and `len` on
 * a Go string counts UTF-8 bytes. A counter measuring the string's own UTF-16
 * code-unit count agrees with that on ASCII and disagrees on everything else,
 * so the naive spelling produces a counter that is right in testing and wrong
 * for any player who writes a non-Latin script. A codepoint count is wrong
 * differently: it is correct for neither cap, and it makes an astral value
 * look half its true width.
 *
 * Concretely: a ~34-codepoint CJK value is 102 bytes and is REJECTED by a
 * 100-byte cap while looking comfortably short; a 25-codepoint astral value is
 * exactly 100 bytes and is ACCEPTED while its UTF-16 length reads 50.
 *
 * (The paragraphs above word the forbidden spelling rather than writing it.
 * ByteCounter.svelte carries an acceptance gate that scans that whole file for
 * the literal, and the same discipline applies here: a gate that has to be
 * suppressed to stay green stops being a gate.)
 *
 * # One implementation, two display components
 *
 * This is the single non-test place under web/src that computes the value.
 * ByteCounter.svelte surfaces a count only within 20% of the cap and renders
 * `{bytes} / {maxBytes}`; the admin edit Sheet's counter is always visible and
 * reads `{n} of {cap}`. Those DISPLAY contracts genuinely differ and stay
 * separate — what must not fork is the arithmetic, because a second copy of a
 * security-adjacent counting expression is a second thing that can start
 * disagreeing with the server.
 *
 * It returns a number and nothing else: no clamping, no truncation, and no
 * opinion about whether a Save may proceed. The server owns the refusal.
 */
export function byteCount(v: string): number {
  return new TextEncoder().encode(v).length;
}
