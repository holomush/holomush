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
  <!-- Movement `text` is already a full sentence ("<actor> has arrived.")
       from the server's formatMovementText, so render it verbatim. Prefixing
       `actor` here (as communication events do, where `text` is a bare body)
       double-prints the name — holomush-pzen. -->
  <span class="movement">{@html linkUrls(event.text)}</span>
</div>

<style>
  .event { line-height: 1.7; }
  .movement { color: var(--mush-arrive); }
</style>
