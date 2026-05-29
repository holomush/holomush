<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { presenceStore } from '$lib/presence/store';

  // idle/lastMode fields are not carried by PresenceEntry — future enhancement
  // tracked in holomush-uhiz.

  function initials(name: string): string {
    const parts = name.split(/\s+/).filter(Boolean);
    if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
    return name.slice(0, 2).toUpperCase();
  }
</script>

{#if presenceStore.map.size > 0}
  <section class="card presence-list" data-testid="presence-card">
    <header class="card-head">
      <h3>Present</h3>
      <span class="count">{presenceStore.map.size}</span>
    </header>
    <ul class="rows">
      {#each [...presenceStore.map.values()] as char (char.characterId)}
        <li class="pres-row">
          <span class="avatar sys" aria-hidden="true">{initials(char.name || char.characterId)}</span>
          <span class="name">{char.name || char.characterId}</span>
          <span class="status-dot" aria-hidden="true"></span>
        </li>
      {/each}
    </ul>
  </section>
{/if}

<style>
  .card {
    background: var(--color-card);
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: var(--card-pad, 12px);
    margin-bottom: var(--row-gap, 8px);
  }
  .card-head { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 6px; }
  .card-head h3 { font-size: 11px; font-weight: 600; color: var(--color-input-prompt); letter-spacing: 1px; text-transform: uppercase; margin: 0; }
  .count { font-size: 10px; color: var(--color-status-text); }
  .rows { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 2px; }
  .pres-row {
    display: flex; align-items: center; gap: 8px;
    padding: var(--row-py, 3px) 4px;
    font-size: 12px;
  }
  .pres-row.is-idle { opacity: 0.5; filter: grayscale(0.5); }
  .avatar {
    width: 22px; height: 22px; border-radius: 50%;
    display: flex; align-items: center; justify-content: center;
    font-size: 9px; font-weight: bold;
    background: var(--color-muted);
    color: var(--color-muted-foreground);
    flex-shrink: 0;
  }
  .avatar.say { background: color-mix(in srgb, var(--mush-say-speaker) 25%, transparent); color: var(--mush-say-speaker); }
  .avatar.pose { background: color-mix(in srgb, var(--mush-pose-actor) 25%, transparent); color: var(--mush-pose-actor); }
  .avatar.ooc { background: color-mix(in srgb, var(--mush-ooc) 25%, transparent); color: var(--mush-ooc); }
  .avatar.sys { background: var(--color-muted); color: var(--color-muted-foreground); }
  .name { flex: 1; color: var(--color-input-text); }
  .status-dot {
    width: 6px; height: 6px; border-radius: 50%;
    background: var(--color-status-online);
  }
  .pres-row.is-idle .status-dot { background: var(--color-status-text); }
</style>
