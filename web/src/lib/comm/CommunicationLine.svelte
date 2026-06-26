<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { linkUrls } from '$lib/util/urlLinker';
  import type { CommLine } from './commLine';

  let { line }: { line: CommLine } = $props();
</script>

{#if line.kind === 'ooc'}
  {#if line.oocStyle === 'pose'}
    <span class="ooc-prefix">{line.oocPrefix ?? '[OOC]'}</span>{' '}<span class="ooc-actor">{line.actor}</span>{' '}<span class="ooc-message">{@html linkUrls(line.text)}</span>
  {:else if line.oocStyle === 'semipose'}
    <span class="ooc-prefix">{line.oocPrefix ?? '[OOC]'}</span>{' '}<span class="ooc-actor">{line.actor}</span><span class="ooc-message">{@html linkUrls(line.text)}</span>
  {:else}
    <span class="ooc-prefix">{line.oocPrefix ?? '[OOC]'}</span>{' '}<span class="ooc-speaker">{line.actor}</span> says, <span class="ooc-message">"{@html linkUrls(line.text)}"</span>
  {/if}
{:else if line.kind === 'say'}
  {#if line.channel}<span class="channel-prefix">[{line.channel}]</span>{' '}{/if}<span class="speaker">{line.actor}</span>{' '}{line.label ?? 'says'},{' '}<span class="speech">"{@html linkUrls(line.text)}"</span>
{:else if line.kind === 'pose'}
  {#if line.channel}<span class="channel-prefix">[{line.channel}]</span>{' '}{/if}<span class="actor">{line.actor}</span>{#if !line.noSpace}{' '}{/if}<span class="action">{@html linkUrls(line.text)}</span>
{:else}
  <span class="pemit-message">{@html linkUrls(line.text)}</span>
{/if}

<style>
  .channel-prefix { color: var(--mush-system); font-weight: bold; }
  .speaker { color: var(--mush-say-speaker); }
  .speech { color: var(--mush-say-speech); }
  .actor { color: var(--mush-pose-actor); }
  .action { color: var(--mush-pose-action); }
  .ooc-prefix { color: var(--mush-ooc); font-weight: bold; }
  .ooc-speaker { color: var(--mush-ooc); }
  .ooc-actor { color: var(--mush-ooc); }
  .ooc-message { color: var(--mush-ooc); opacity: 0.85; }
  .pemit-message { color: var(--mush-pemit); font-style: italic; }
</style>
