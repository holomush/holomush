<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { hasAnsiCodes } from '$lib/util/ansi';
  import { linkUrls } from '$lib/util/urlLinker';
  import AnsiRenderer from './AnsiRenderer.svelte';

  interface Props {
    event: { type: string; characterName: string; text: string };
    dimmed?: boolean;
  }

  let { event, dimmed = false }: Props = $props();
</script>

<div class="event event-{event.type}" class:dimmed data-testid="event">
  {#if event.type === 'say'}
    <span class="speaker">{event.characterName}</span> says,
    <span class="speech">"{@html linkUrls(event.text)}"</span>
  {:else if event.type === 'pose'}
    <span class="actor">{event.characterName}</span>
    <span class="action">{@html linkUrls(event.text)}</span>
  {:else if event.type === 'arrive'}
    <span class="arrival">{event.characterName} has arrived.</span>
  {:else if event.type === 'leave'}
    <span class="departure">{event.characterName} has left.</span>
  {:else if event.type === 'system'}
    <span class="system-text">{@html linkUrls(event.text)}</span>
  {:else if event.type === 'move'}
    <span class="move-text">{@html linkUrls(event.text)}</span>
  {:else if hasAnsiCodes(event.text)}
    <AnsiRenderer text={event.text} />
  {:else}
    <span class="generic">{@html linkUrls(event.text)}</span>
  {/if}
</div>

<style>
  .event { line-height: 1.7; }
  .dimmed { opacity: 0.5; }
  .speaker { color: var(--color-say-speaker); }
  .speech { color: var(--color-say-speech); }
  .actor { color: var(--color-pose-actor); }
  .action { color: var(--color-pose-action); }
  .arrival, .departure { color: var(--color-arrive); }
  .system-text { color: var(--color-system); }
  .move-text { color: var(--color-system); }
</style>
