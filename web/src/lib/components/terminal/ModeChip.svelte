<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  type Kind = 'say' | 'pose' | 'ooc' | 'command';
  interface Props { kind: Kind; label: string; }
  let { kind, label }: Props = $props();
</script>

<span class="mode-chip"
  class:mode-say={kind === 'say'}
  class:mode-pose={kind === 'pose'}
  class:mode-ooc={kind === 'ooc'}
  class:mode-command={kind === 'command'}>
  {label}
</span>

<style>
  .mode-chip {
    display: inline-flex; align-items: center;
    padding: 0 6px; border-radius: 999px;
    font-size: 10px; font-weight: bold; letter-spacing: 0.5px;
    text-transform: uppercase; flex-shrink: 0;
    line-height: 16px; height: 16px;
  }
  .mode-say { background: color-mix(in srgb, var(--mush-say-speaker) 20%, transparent); color: var(--mush-say-speaker); }
  .mode-pose { background: color-mix(in srgb, var(--mush-pose-actor) 20%, transparent); color: var(--mush-pose-actor); }
  .mode-ooc { background: color-mix(in srgb, var(--mush-ooc) 20%, transparent); color: var(--mush-ooc); }
  /* Recognized-command chip — distinct from speech tokens. Uses an existing
     neutral UI token (NOT a --mush-* speech color, NOT amber per branding INV-1). */
  .mode-command { background: color-mix(in srgb, var(--color-muted-foreground) 18%, transparent); color: var(--color-muted-foreground); }
</style>
