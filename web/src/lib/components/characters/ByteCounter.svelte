<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  /**
   * The per-field remaining-budget counter, measured in BYTES because the
   * server's cap is in bytes.
   *
   * It measures with TextEncoder, never with the string's own UTF-16 code-unit
   * count. The facade compares `len(value) > maxBytes` on a Go string
   * (internal/grpc/characteraccess_write.go:213-224), which counts UTF-8 bytes;
   * a UTF-16 code-unit count agrees with that on ASCII and disagrees on
   * everything else, so the naive spelling produces a counter that is right in
   * testing and wrong for any player who writes a non-Latin script.
   *
   * (The wording above states the forbidden spelling rather than writing it, so
   * the acceptance gate that scans this whole file for it cannot be tripped by
   * a comment. A gate that has to be suppressed to stay green stops being a
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

  const bytes = $derived(new TextEncoder().encode(value).length);
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
