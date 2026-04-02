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

  let hasAnsi = $derived(hasAnsiCodes(event.text));
</script>

<div class="event event-{event.type}" data-testid="event">
  {#if hasAnsi}
    <AnsiRenderer text={event.text} />
  {:else if event.format === 'error'}
    <span class="command-error">{event.text}</span>
  {:else}
    <span class="command-output">{@html linkUrls(event.text)}</span>
  {/if}
</div>

<style>
  .event { line-height: 1.7; }
  .command-output { color: var(--color-command-output); white-space: pre-wrap; }
  .command-error { color: var(--color-command-error); white-space: pre-wrap; }
</style>
