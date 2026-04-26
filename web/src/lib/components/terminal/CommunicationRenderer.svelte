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

  let channel = $derived(event.metadata?.channel as string | undefined);
  let label = $derived(event.metadata?.label as string | undefined);
  let noSpace = $derived(event.metadata?.no_space as boolean | undefined);
  let oocPrefix = $derived(event.metadata?.ooc_prefix as string | undefined);
  let style = $derived(event.metadata?.style as string | undefined);
  let isOoc = $derived(event.type === 'core-communication:ooc' || !!oocPrefix);
</script>

<div class="event event-{event.type}" data-testid="event">
  {#if isOoc}
    {#if style === 'pose'}
      <span class="ooc-prefix">{oocPrefix ?? '[OOC]'}</span>{' '}<span class="ooc-actor">{event.actor}</span>{' '}<span class="ooc-message">{@html linkUrls(event.text)}</span>
    {:else if style === 'semipose'}
      <span class="ooc-prefix">{oocPrefix ?? '[OOC]'}</span>{' '}<span class="ooc-actor">{event.actor}</span><span class="ooc-message">{@html linkUrls(event.text)}</span>
    {:else}
      <span class="ooc-prefix">{oocPrefix ?? '[OOC]'}</span>{' '}<span class="ooc-speaker">{event.actor}</span> says, <span class="ooc-message">"{@html linkUrls(event.text)}"</span>
    {/if}
  {:else if event.format === 'speech'}
    {#if channel}<span class="channel-prefix">[{channel}]</span>{' '}{/if}<span class="speaker">{event.actor}</span>{' '}{label ?? 'says'},{' '}<span class="speech">"{@html linkUrls(event.text)}"</span>
  {:else if event.format === 'action'}
    {#if channel}<span class="channel-prefix">[{channel}]</span>{' '}{/if}<span class="actor">{event.actor}</span>{#if !noSpace}{' '}{/if}<span class="action">{@html linkUrls(event.text)}</span>
  {:else}
    <span class="pemit-message">{@html linkUrls(event.text)}</span>
  {/if}
</div>

<style>
  .event { line-height: 1.7; }
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
