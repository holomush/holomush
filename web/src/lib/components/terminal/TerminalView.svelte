<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { lines, replayActive, newMessageCount, isAtBottom, scrolledToBottom, scrolledAway } from '$lib/stores/terminalStore';
  import EventRenderer from './EventRenderer.svelte';

  let scrollContainer: HTMLDivElement;
  let sentinel: HTMLDivElement;
  let observer: IntersectionObserver;

  onMount(() => {
    observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          scrolledToBottom();
        } else {
          scrolledAway();
        }
      },
      { root: scrollContainer, threshold: 0, rootMargin: '50px' }
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  });

  $effect(() => {
    if ($isAtBottom && $lines.length > 0) {
      requestAnimationFrame(() => {
        sentinel.scrollIntoView({ behavior: 'instant' });
      });
    }
  });

  function scrollToBottom() {
    sentinel.scrollIntoView({ behavior: 'smooth' });
    scrolledToBottom();
  }
</script>

<div class="terminal-view" bind:this={scrollContainer}>
  <div class="scrollback">
    {#if $replayActive}
      <div class="separator">--- REPLAY ---</div>
    {/if}

    {#each $lines as line, i (line.id)}
      {#if !line.replayed && i > 0 && $lines[i - 1]?.replayed}
        <div class="separator">--- LIVE ---</div>
      {/if}
      <EventRenderer event={line.event} dimmed={line.replayed} />
    {/each}

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
    font-size: 13px;
    position: relative;
  }
  .scrollback { padding: 8px 12px; }
  .sentinel { height: 1px; }
  .separator {
    color: var(--color-status-text);
    font-size: 10px;
    letter-spacing: 1px;
    margin: 4px 0;
  }
  .scroll-indicator {
    position: sticky;
    bottom: 0;
    width: 100%;
    background: var(--color-border);
    color: var(--color-scrollback-indicator);
    border: none;
    padding: 4px;
    font-size: 11px;
    cursor: pointer;
    text-align: center;
  }
</style>
