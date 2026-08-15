<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { byteCount } from '$lib/text/byteCount';

  /**
   * The per-field remaining-budget counter, measured in BYTES because the
   * server's cap is in bytes.
   *
   * The arithmetic lives in $lib/text/byteCount and is imported rather than
   * restated: the admin edit Sheet is a SECOND editor for the same fields
   * against the same server caps, and two copies of a security-adjacent
   * counting expression are two things that can start disagreeing. That
   * module's own doc block carries the reasoning about why the measure is
   * bytes and not the string's own UTF-16 code-unit count.
   *
   * (Both files word the forbidden spelling rather than writing it, so the
   * acceptance gate that scans this whole file for it cannot be tripped by a
   * comment. A gate that has to be suppressed to stay green stops being a
   * gate — the same reason PublicProfile.svelte words its own header that way.)
   *
   * TWO SEPARATE RULES, deliberately not one. `shown` is the DISPLAY rule —
   * 05-UI-SPEC surfaces a counter only within 20% of the cap, so an ordinary
   * field carries no numeric chrome. `over` is the COMPARISON, and it mirrors
   * the server's strictly-greater-than: exactly at the cap is ACCEPTABLE, and a
   * client that marked it over would refuse a value the server takes.
   *
   * It renders a number and a cap and nothing else — no clamping, no truncation,
   * no disabling of the Save. The server owns the refusal; this only tells the
   * player where the line is.
   */
  let { value, maxBytes }: { value: string; maxBytes: number } = $props();

  const bytes = $derived(byteCount(value));
  const over = $derived(bytes > maxBytes);
  const shown = $derived(bytes >= maxBytes * 0.8);
</script>

{#if shown}
  <!-- `aria-live` because the two moments that matter here are both silent
       otherwise: the counter APPEARS at 80% of cap and flips to `over` at 100%,
       and neither is a focus change, so a screen-reader user typing into the
       field is never told. `polite` rather than `assertive` — it must not
       interrupt the character being echoed. The numerals carry the state on
       their own (`101 / 100` reads as over), so colour is not the sole
       carrier and this is the announcement gap, not a 1.4.1 fix. -->
  <p
    class="counter"
    class:over
    data-testid="byte-counter"
    data-over={over ? 'true' : 'false'}
    aria-live="polite"
  >
    {bytes} / {maxBytes}
  </p>
{/if}

<style>
  .counter {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    line-height: 1.4;
    color: var(--color-status-text);
  }
  .over {
    color: var(--color-destructive);
  }
</style>
