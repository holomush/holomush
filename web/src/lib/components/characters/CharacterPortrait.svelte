<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { cn } from '$lib/utils';

  /**
   * The one portrait treatment. 05-UI-SPEC unifies the tinted plate across the
   * public profile (80px) and the roster (44px), so a single component is what
   * makes "one treatment" true rather than merely asserted.
   *
   * `name` is rendered UNMUTATED: the first character goes into the DOM as the
   * stored bytes carried it and `text-transform: uppercase` does the casing.
   * 01-SPEC §8.8 guarantees a reachable character always has a name, not that
   * its first character is uppercase or even cased at all — so uppercasing the
   * string in script would claim more than the spec does, and would silently
   * rewrite scripts that have no case.
   *
   * THE FIRST CHARACTER IS THE FIRST CODE POINT, NOT THE FIRST UTF-16 CODE
   * UNIT. internal/charname/syntax admits any `\p{L}` rune, and Go's `\p{L}`
   * includes astral-plane letters (Deseret, Adlam, Osage, Gothic, …). For a
   * name that starts with one, `charAt(0)` hands back a LONE HIGH SURROGATE,
   * which the plate renders as U+FFFD — mutating the very first character the
   * paragraph above promises to render unmutated. Spreading the string
   * iterates code points, so `[...name][0]` is the whole grapheme's leading
   * code point in both planes. This is the same unit-system class as
   * ByteCounter's TextEncoder and the name counter's `[...name].length`.
   */
  let {
    name,
    size = 80,
    class: className = '',
  }: { name: string; size?: number; class?: string } = $props();

  // Display (32/600) at the profile's 80px plate, Heading (20/600) at the
  // roster's 44px one — both roles already declared in the type scale.
  const letterSize = $derived(size >= 80 ? 32 : 20);

  // `?? ''` covers the empty string: spreading it yields an empty array, and
  // the plate then draws its tint with no glyph rather than the string
  // `undefined`.
  const initial = $derived([...name][0] ?? '');
</script>

<span
  class={cn('portrait', className)}
  style="--portrait-size: {size}px; --portrait-letter: {letterSize}px"
  aria-hidden="true">{initial}</span
>

<style>
  .portrait {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: var(--portrait-size);
    height: var(--portrait-size);
    border-radius: 12px;
    background: color-mix(in srgb, var(--color-primary) 16%, transparent);
    border: 1px solid color-mix(in srgb, var(--color-primary) 32%, transparent);
    color: var(--color-primary);
    font-size: var(--portrait-letter);
    font-weight: 600;
    line-height: 1;
    /* The casing lives here and nowhere else — see the component doc. */
    text-transform: uppercase;
    user-select: none;
  }
</style>
