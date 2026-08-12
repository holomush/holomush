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
   */
  let {
    name,
    size = 80,
    class: className = '',
  }: { name: string; size?: number; class?: string } = $props();

  // Display (32/600) at the profile's 80px plate, Heading (20/600) at the
  // roster's 44px one — both roles already declared in the type scale.
  const letterSize = $derived(size >= 80 ? 32 : 20);
</script>

<span
  class={cn('portrait', className)}
  style="--portrait-size: {size}px; --portrait-letter: {letterSize}px"
  aria-hidden="true">{name.charAt(0)}</span
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
