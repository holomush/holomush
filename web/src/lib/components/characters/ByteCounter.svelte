<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  /**
   * The per-field remaining-budget counter.
   *
   * PLACEHOLDER IMPLEMENTATION — counts UTF-16 code units, which is what the
   * naive reading of "how long is this string" produces and what the sibling
   * test file exists to reject. Task 1's GREEN step replaces it.
   */
  let { value, maxBytes }: { value: string; maxBytes: number } = $props();

  const bytes = $derived(value.length);
  const over = $derived(bytes > maxBytes);
  const shown = $derived(bytes >= maxBytes * 0.8);
</script>

{#if shown}
  <p class="counter" class:over data-testid="byte-counter" data-over={over ? 'true' : 'false'}>
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
