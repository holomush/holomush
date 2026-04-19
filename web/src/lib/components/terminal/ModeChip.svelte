<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  type Mode = 'say' | 'pose' | 'ooc';
  interface Props { mode: Mode; }
  let { mode }: Props = $props();
</script>

<span class="mode-chip" class:mode-say={mode === 'say'} class:mode-pose={mode === 'pose'} class:mode-ooc={mode === 'ooc'}>
  {mode}
</span>

<style>
  .mode-chip {
    display: inline-flex; align-items: center;
    padding: 0 6px;
    border-radius: 999px;
    font-size: 10px; font-weight: bold; letter-spacing: 0.5px;
    text-transform: uppercase;
    flex-shrink: 0;
    line-height: 16px;
    height: 16px;
  }
  .mode-say {
    background: color-mix(in srgb, var(--mush-say-speaker) 20%, transparent);
    color: var(--mush-say-speaker);
  }
  .mode-pose {
    background: color-mix(in srgb, var(--mush-pose-actor) 20%, transparent);
    color: var(--mush-pose-actor);
  }
  .mode-ooc {
    background: color-mix(in srgb, var(--mush-ooc) 20%, transparent);
    color: var(--mush-ooc);
  }
</style>
