<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import type { AdminSectionEntry } from '$lib/stores/adminNavStore';
  import { cn } from '$lib/utils';

  interface Props {
    /**
     * The awaited response array, rendered verbatim. Every entry drawn came
     * from here; no template conditional decides whether one appears, and this
     * component holds no list of its own to compare against.
     */
    sections: AdminSectionEntry[];
    /** The section currently open, if any. */
    activeId?: string;
  }
  let { sections, activeId }: Props = $props();

  // A monogram, not an icon map: an id → glyph table would be a client-side
  // mirror of the server's answer, and the whole surface exists to not have
  // one. The first letter of the display name distinguishes entries in the
  // narrowed strip and needs nothing the response did not already carry.
  const monogram = (name: string) => (name.trim()[0] ?? '?').toUpperCase();
</script>

<nav class="adminnav" aria-label="Admin sections">
  {#each sections as section (section.id)}
    {@const active = section.id === activeId}
    <a
      href={`/admin/${section.id}`}
      class={cn('navitem', { 'is-active': active, 'is-planned': section.status === 'planned' })}
      title={section.displayName}
      aria-current={active ? 'page' : undefined}
    >
      <span class="navitem-glyph" aria-hidden="true">{monogram(section.displayName)}</span>
      <span class="navitem-label">{section.displayName}</span>
      {#if section.status === 'planned'}
        <span class="badge-planned">planned</span>
      {/if}
      {#if active}<span class="navitem-bar" aria-hidden="true"></span>{/if}
    </a>
  {/each}
</nav>

<style>
  .adminnav {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 8px;
  }
  .navitem {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    /* >= 44px of hit area including padding, at every band. */
    min-height: 44px;
    padding: 0 8px;
    border-radius: 6px;
    text-decoration: none;
    font-family: var(--font-sans, system-ui);
    font-size: 12px;
    font-weight: 600;
    color: var(--color-status-text);
    transition: background 120ms, color 120ms;
  }
  .navitem:hover {
    background: color-mix(in srgb, var(--color-primary) 10%, transparent);
    color: var(--color-input-text);
  }
  .navitem:focus-visible {
    outline: 2px solid var(--color-ring);
    outline-offset: 2px;
  }
  .navitem.is-active {
    color: var(--color-primary);
  }
  .navitem-bar {
    position: absolute;
    left: 0;
    top: 8px;
    bottom: 8px;
    width: 2px;
    border-radius: 1px;
    background: var(--color-primary);
  }
  .navitem-glyph {
    flex: none;
    width: 22px;
    height: 22px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 5px;
    font-size: 11px;
    background: color-mix(in srgb, var(--color-primary) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--color-primary) 24%, transparent);
  }
  .navitem-label {
    flex: 1;
    min-width: 0;
  }
  /* Muted, never a warning colour and never amber. */
  .badge-planned {
    flex: none;
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.02em;
    padding: 1px 5px;
    border-radius: 999px;
    color: var(--color-muted-foreground);
    border: 1px solid var(--color-border);
  }

  /* Narrowed strip. The label stays in the DOM and stays announced — clipped,
     never display:none — so the entry keeps its accessible name where the
     visible text is gone, and the badge becomes a dot. */
  @media (max-width: 1023px) {
    .navitem {
      justify-content: center;
      padding: 0;
    }
    .navitem-label {
      position: absolute;
      width: 1px;
      height: 1px;
      padding: 0;
      margin: -1px;
      overflow: hidden;
      clip-path: inset(50%);
      white-space: nowrap;
      border: 0;
    }
    .badge-planned {
      position: absolute;
      top: 6px;
      right: 6px;
      width: 5px;
      height: 5px;
      padding: 0;
      border-radius: 999px;
      background: var(--color-muted-foreground);
      border: none;
      font-size: 0;
      color: transparent;
    }
  }
</style>
