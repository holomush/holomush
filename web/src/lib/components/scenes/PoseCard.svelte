<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { cn } from '$lib/utils.js';
  import type { LogEntry } from '$lib/scenes/types';

  let { entry }: { entry: LogEntry } = $props();

  function initials(name: string): string {
    const parts = name.split(/\s+/).filter(Boolean);
    if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
    return name.slice(0, 2).toUpperCase();
  }

  function formatTime(ms: number): string {
    return new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }

  const isSay = $derived(entry.kind === 'say');
</script>

{#if entry.kind === 'pose' || entry.kind === 'say'}
  <article class="flex gap-3 px-4 py-2 hover:bg-muted/30 transition-colors">
    <!-- Avatar initial circle -->
    <div
      class="size-8 shrink-0 rounded-full bg-[var(--brand-cyan-deep)] flex items-center justify-center text-[11px] font-semibold text-white mt-0.5"
      aria-hidden="true"
    >
      {initials(entry.actorName || '?')}
    </div>
    <div class="min-w-0 flex-1">
      <!-- Author banner -->
      <div class="flex items-baseline gap-2 mb-0.5">
        <span class="text-sm font-semibold text-[var(--brand-cyan-bright)]">
          {entry.actorName || entry.actorId || 'Unknown'}
        </span>
        <time
          datetime={new Date(entry.timestampMs).toISOString()}
          class="text-[10px] text-muted-foreground"
        >
          {formatTime(entry.timestampMs)}
        </time>
      </div>
      <!-- Body text -->
      {#if entry.contentWarning}
        <details class="text-sm">
          <summary class="cursor-pointer text-muted-foreground text-xs italic mb-1">
            CW: {entry.contentWarning}
          </summary>
          <p class={cn('leading-relaxed', isSay ? 'italic' : '')}>{entry.text}</p>
        </details>
      {:else}
        <p class={cn('text-sm leading-relaxed', isSay ? 'italic' : '')}>{entry.text}</p>
      {/if}
    </div>
  </article>
{:else if entry.kind === 'ooc'}
  <!-- OOC lines rendered inline -->
  <div class="px-4 py-1 text-sm italic text-muted-foreground">
    <span class="text-[10px] not-italic mr-1">(ooc)</span>{entry.actorName}: {entry.text}
  </div>
{:else}
  <!-- system / narration -->
  <div class="px-4 py-1 text-xs text-muted-foreground/70 italic">
    {entry.text}
  </div>
{/if}
