<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onDestroy } from 'svelte';
  import * as Select from '$lib/components/ui/select';
  import { ADMIN_STATUS_FILTERS, type CharacterStatusFilter } from '$lib/admin/client';

  /**
   * Two controls, and only two.
   *
   * THE SEARCH BOX SENDS THE RAW TYPED STRING. It is not trimmed, folded,
   * case-mapped or length-gated on the way out. Search equality is defined
   * server-side and nowhere else: the core normalizes the term through the
   * single pipeline that produced the stored normal form. A TypeScript mirror
   * of that pipeline would be a second, drifting definition of which strings
   * are equal — for a security-adjacent normalizer, which is the shape Phase 5
   * already forbade. So this component makes NO claim about equality: not
   * case, not width, not composition.
   *
   * THE ONE SELECT FILTERS STATUS AND NEVER SORTS. §11.3 names a sort control
   * whose options are drawn from the field list as THE warning sign, because
   * that list is the privacy-bearing set. Sorting is click-header only, on the
   * table; there is no facet panel and no sort dropdown anywhere on this
   * surface.
   */
  interface Props {
    /** The current term, for the box's initial value. */
    term?: string;
    status?: CharacterStatusFilter;
    /** Reports the raw term after `debounceMs` of quiet. */
    onsearch?: (term: string) => void;
    onstatus?: (status: CharacterStatusFilter) => void;
    debounceMs?: number;
  }

  let {
    term = '',
    status = $bindable('all'),
    onsearch,
    onstatus,
    debounceMs = 250,
  }: Props = $props();

  let timer: ReturnType<typeof setTimeout> | undefined;

  /**
   * 250ms of quiet, and NO minimum-length gate. A gate would make short terms
   * silently unsearchable, and the server already answers a blank term with the
   * unfiltered page rather than an error.
   */
  function oninput(event: Event) {
    const raw = (event.currentTarget as HTMLInputElement).value;
    clearTimeout(timer);
    timer = setTimeout(() => onsearch?.(raw), debounceMs);
  }

  /**
   * The next keystroke is not the only thing that can end the window. A timer
   * that outlives this component still calls `onsearch`, which on the
   * characters page runs a reload and issues a search RPC for a surface that is
   * gone — and navigating away mid-typing is the ordinary way to reach that.
   */
  onDestroy(() => clearTimeout(timer));

  function onValueChange(value: string) {
    status = value as CharacterStatusFilter;
    onstatus?.(status);
  }

  const statusLabel = $derived(
    ADMIN_STATUS_FILTERS.find((o) => o.value === status)?.label ?? 'All',
  );
</script>

<div class="filterbar">
  <input
    class="searchbox"
    type="search"
    name="q"
    value={term}
    placeholder="Search characters and players"
    autocomplete="off"
    {oninput}
  />
  <span class="statuslabel">Status</span>
  <Select.Root type="single" name="status" value={status} onValueChange={onValueChange}>
    <Select.Trigger class="statustrigger" aria-label="Status">{statusLabel}</Select.Trigger>
    <Select.Content>
      {#each ADMIN_STATUS_FILTERS as option (option.value)}
        <Select.Item value={option.value} label={option.label}>{option.label}</Select.Item>
      {/each}
    </Select.Content>
  </Select.Root>
</div>

<style>
  /* Lets Tailwind resolve theme() inside this scoped style block; the build
     fails loudly without it. */
  @reference "../../../app.css";

  .filterbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 0 12px;
  }
  .searchbox {
    flex: 1 1 auto;
    min-width: 0;
    min-height: 44px;
    padding: 0 8px;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    background: var(--color-card);
    color: var(--color-foreground);
    font-size: 14px;
    line-height: 1.5;
  }
  .searchbox:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
  .statuslabel {
    flex: none;
    font-size: 12px;
    font-weight: 600;
    color: var(--color-status-text);
  }
  :global(.statustrigger) {
    min-height: 44px;
  }

  /* The phone band, reading the same Tailwind --breakpoint-md token the
     shipped rail reads, so this band and the rail collapse cannot be given
     different widths. 16px is a platform
     constraint, not a style preference: any focused input below it triggers
     iOS Safari's zoom-on-focus, which does not unzoom on blur. */
  @media (width < theme(--breakpoint-md)) {
    .filterbar {
      flex-wrap: wrap;
    }
    .searchbox {
      font-size: 16px;
    }
  }
</style>
