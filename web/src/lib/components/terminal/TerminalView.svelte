<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { lines, newMessageCount, isAtBottom, scrolledToBottom, scrolledAway } from '$lib/stores/terminalStore';
  import EventRenderer from './EventRenderer.svelte';

  let scrollContainer: HTMLDivElement;
  let sentinel: HTMLDivElement;
  let observer: IntersectionObserver;

  const fmt = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit', hour12: false });
  function formatHHMM(d: Date): string { return fmt.format(d); }

  // Index of the first live (non-replayed) line in $lines; -1 if none.
  let liveStartIdx = $derived($lines.findIndex((l) => !l.replayed));
  let hasReplay = $derived($lines.some((l) => l.replayed));
  let hasLive = $derived(liveStartIdx !== -1);

  let replayLines = $derived(hasLive ? $lines.slice(0, liveStartIdx) : $lines.filter((l) => l.replayed));
  let liveLines = $derived(hasLive ? $lines.slice(liveStartIdx) : []);

  onMount(() => {
    observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) scrolledToBottom(); else scrolledAway();
      },
      { root: scrollContainer, threshold: 0, rootMargin: '50px' },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  });

  $effect(() => {
    if ($isAtBottom && $lines.length > 0) {
      requestAnimationFrame(() => { sentinel.scrollIntoView({ behavior: 'instant' }); });
    }
  });

  function scrollToBottom() {
    sentinel.scrollIntoView({ behavior: 'smooth' });
    scrolledToBottom();
  }
</script>

<div class="terminal-view" bind:this={scrollContainer}>
  <div class="scrollback">
    {#if hasReplay}
      <div class="lines replay-chunk">
        {#each replayLines as line (line.id)}
          <div class="line replay" data-event-id={line.id}>
            <span class="tstamp">{formatHHMM(line.timestamp)}</span>
            <EventRenderer event={line.event} />
          </div>
        {/each}
      </div>
    {/if}

    {#if hasReplay && hasLive}
      <div class="sep-live" role="separator" aria-label="Live events begin">
        <span class="dot" aria-hidden="true"></span>
        <span class="label">LIVE</span>
        <span class="gradient-line" aria-hidden="true"></span>
      </div>
    {/if}

    {#if hasLive}
      <div class="lines live-chunk">
        {#each liveLines as line (line.id)}
          <div class="line" data-event-id={line.id}>
            <span class="tstamp">{formatHHMM(line.timestamp)}</span>
            <EventRenderer event={line.event} />
          </div>
        {/each}
      </div>
    {/if}

    <div class="sentinel" bind:this={sentinel}></div>
  </div>

  {#if $newMessageCount > 0}
    <button class="scroll-indicator" onclick={scrollToBottom}>
      {$newMessageCount} new -- click to scroll down
    </button>
  {/if}
</div>

<style>
  .terminal-view {
    flex: 1;
    overflow-y: auto;
    background: var(--color-background);
    font-family: 'JetBrains Mono', 'Fira Code', 'SF Mono', monospace;
    font-size: 15px;
    position: relative;
  }
  .scrollback { padding: 8px 12px; }
  .sentinel { height: 1px; }

  .line {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: var(--row-py, 3px) 0;
    line-height: 1.7;
  }
  .line.replay { color: var(--color-scrollback-replayed); }
  .line.replay :global(*) { color: var(--color-scrollback-replayed); }
  .tstamp {
    flex-shrink: 0;
    width: 44px;
    color: var(--mush-timestamp, var(--color-status-text));
    font-variant-numeric: tabular-nums;
    font-size: 12px;
    padding-top: 2px;
  }

  @media (prefers-reduced-motion: no-preference) {
    .lines.live-chunk > .line:last-child:not(.replay) {
      animation: just-arrived 600ms ease-out;
    }
  }

  .sep-live {
    display: flex; align-items: center; gap: 8px;
    margin: 10px 0 6px;
    color: var(--color-primary);
    font-size: 10px; letter-spacing: 2px; font-weight: bold;
  }
  .sep-live .dot {
    width: 8px; height: 8px; border-radius: 50%;
    background: var(--color-primary);
    animation: dot-pulse 1200ms ease-in-out infinite;
  }
  .sep-live .label { flex-shrink: 0; }
  .sep-live .gradient-line {
    flex: 1; height: 1px;
    background: linear-gradient(to right, var(--color-primary), transparent);
  }
  @media (prefers-reduced-motion: reduce) {
    .sep-live .dot { animation: none; opacity: 0.8; }
  }

  .scroll-indicator {
    position: sticky; bottom: 0; width: 100%;
    background: var(--color-border);
    color: var(--color-scrollback-indicator);
    border: none; padding: 4px;
    font-size: 12px; cursor: pointer; text-align: center;
  }
</style>
