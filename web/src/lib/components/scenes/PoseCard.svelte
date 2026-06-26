<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import type { LogEntry } from '$lib/scenes/types';
  import CommunicationLine from '$lib/comm/CommunicationLine.svelte';
  import { logEntryToLine } from '$lib/comm/commLine';

  let { entry }: { entry: LogEntry } = $props();

  function initials(name: string): string {
    const parts = name.split(/\s+/).filter(Boolean);
    if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
    return name.slice(0, 2).toUpperCase();
  }

  function formatTime(ms: number): string {
    return new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }

  const line = $derived(logEntryToLine(entry));
  const showAvatar = $derived(entry.kind !== 'system');
</script>

<article class="row">
  <span class="ts">{entry.timestampMs ? formatTime(entry.timestampMs) : ''}</span>
  {#if showAvatar}
    <span class="avatar" aria-hidden="true">{initials(entry.actorName || '?')}</span>
  {:else}
    <span class="avatar avatar-empty" aria-hidden="true"></span>
  {/if}
  <div class="body">
    {#if entry.contentWarning}
      <details>
        <summary class="cw">CW: {entry.contentWarning}</summary>
        <CommunicationLine {line} />
      </details>
    {:else}
      <CommunicationLine {line} />
    {/if}
  </div>
</article>

<style>
  .row {
    display: grid;
    grid-template-columns: 46px 22px 1fr;
    gap: 10px;
    align-items: baseline;
    padding: 3px 14px;
  }
  .row:hover { background: color-mix(in srgb, var(--color-muted) 30%, transparent); }
  .ts {
    text-align: right;
    color: var(--color-muted-foreground);
    opacity: 0.75;
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    font-family: var(--font-mono, ui-monospace, monospace);
  }
  .avatar {
    display: inline-flex; align-items: center; justify-content: center;
    width: 22px; height: 22px; border-radius: 999px;
    background: color-mix(in srgb, var(--mush-say-speaker) 20%, transparent);
    color: var(--mush-say-speaker);
    font-size: 9px; font-weight: 700; flex-shrink: 0;
  }
  .avatar-empty { background: transparent; }
  .body { min-width: 0; font-size: 14px; line-height: 1.55; }
  .cw { cursor: pointer; color: var(--color-muted-foreground); font-size: 12px; font-style: italic; margin-bottom: 4px; }
</style>
