<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { linkUrls } from '$lib/util/urlLinker';

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
</script>

<div class="event event-{event.type}" data-testid="event">
  {#if event.format === 'error'}
    <span class="system-error">{@html linkUrls(event.text)}</span>
  {:else}
    <span class="system-text">{@html linkUrls(event.text)}</span>
  {/if}
</div>

<style>
  .event { line-height: 1.7; }
  .system-text { color: var(--mush-system); white-space: pre-wrap; }
  .system-error { color: var(--mush-command-error); white-space: pre-wrap; }
</style>
