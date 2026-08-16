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

<!-- `aria-live` because the two moments that matter here are both silent
     otherwise: the counter APPEARS at 80% of cap and flips to `over` at 100%,
     and neither is a focus change, so a screen-reader user typing into the
     field is never told. `polite` rather than `assertive` — it must not
     interrupt the character being echoed. The numerals carry the state on
     their own (`101 / 100` reads as over), so colour is not the sole carrier
     and this is the announcement gap, not a 1.4.1 fix.

     THE ELEMENT IS UNCONDITIONAL AND ONLY ITS CONTENT IS GATED. Assistive
     technologies announce mutations to a region already in the accessibility
     tree; one inserted wholesale together with its content generally is not
     announced at all — so with the attribute inside the `{#if}`, the first of
     those two moments never happened, while the code claimed both. It is the
     SAME node across the transition, which is what makes the arrival of the
     numerals an announceable mutation.

     `idle` is what lets it be unconditional without changing a single pixel:
     consuming fields lay their children out with `display: flex; gap`, where
     an always-present empty BOX would add permanent spacing to every field on
     every character surface, and `display: none` / `visibility: hidden` would
     take the region back out of the accessibility tree and undo the fix. Out
     of flow is the one state that is both. A second, separate live region
     would work too, but it would carry the value twice in the tree.

     `data-testid` and `data-over` describe the RENDERED counter, so they are
     gated with it: below the display threshold there is no counter, only a
     silent region waiting for one. -->
<p
  class="counter"
  class:over
  class:idle={!shown}
  data-testid={shown ? 'byte-counter' : undefined}
  data-over={shown ? (over ? 'true' : 'false') : undefined}
  aria-live="polite"
>
  {#if shown}{bytes} / {maxBytes}{/if}
</p>

<style>
  /* Out of flow, so a flex or grid parent lays its fields out exactly as it did
     when this element did not exist — no extra flex item, no extra gap — while
     the element itself stays in the accessibility tree as a live region. */
  .idle {
    position: absolute;
    width: 1px;
    height: 1px;
    margin: -1px;
    padding: 0;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
    border: 0;
  }
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
