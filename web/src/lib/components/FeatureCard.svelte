<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import MarkdownContent from './MarkdownContent.svelte';

  let { title = '', body = '' }: { title: string; body: string } = $props();
</script>

<div class="feature-card">
  <h3>{title}</h3>
  <MarkdownContent content={body} />
</div>

<style>
  .feature-card {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 20px 24px;
  }

  .feature-card h3 {
    font-size: 14px;
    font-weight: 600;
    color: var(--color-say-speaker);
    margin: 0 0 10px 0;
    letter-spacing: 0.03em;
  }

  .feature-card :global(p) {
    font-size: 13px;
    color: var(--color-status-text);
    margin: 0;
    line-height: 1.6;
  }
</style>
