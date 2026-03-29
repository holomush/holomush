<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { hasAnsiCodes } from '$lib/util/ansi';
  import { linkUrls } from '$lib/util/urlLinker';
  import AnsiRenderer from './AnsiRenderer.svelte';

  interface Props {
    event: {
      type: string;
      category: string;
      format: string;
      actor: string;
      text: string;
      metadata?: Record<string, unknown>;
    };
  }

  let { event }: Props = $props();

  let hasAnsi = $derived(event.text ? hasAnsiCodes(event.text) : false);
</script>

<div class="event event-{event.type}" data-testid="event">
  {#if event.text && hasAnsi}
    <AnsiRenderer text={event.text} />
  {:else if event.text}
    {#if event.actor}<span class="actor">{event.actor}</span>{' '}{/if}<span class="generic">{@html linkUrls(event.text)}</span>
  {:else}
    <span class="raw-metadata">{JSON.stringify(event.metadata ?? {})}</span>
  {/if}
</div>

<style>
  .event { line-height: 1.7; }
  .actor { color: var(--color-pose-actor); }
  .generic { color: var(--color-command-output); }
  .raw-metadata { color: var(--color-system); opacity: 0.6; font-size: 0.9em; }
</style>
