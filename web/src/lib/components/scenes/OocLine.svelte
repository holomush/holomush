<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import type { LogEntry } from '$lib/scenes/types';

  /**
   * OocLine renders a single OOC log entry as a dim italic line.
   * PoseCard also handles ooc/system in-line; this component is provided
   * as a standalone alternative for consumers that want to split rendering
   * by kind at the list level.
   */
  let { entry }: { entry: LogEntry } = $props();

  function formatTime(ms: number): string {
    return new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }
</script>

<div class="flex items-baseline gap-1.5 px-4 py-1 text-sm italic text-muted-foreground">
  <span class="text-[10px] not-italic shrink-0">(ooc)</span>
  <span class="font-medium not-italic text-muted-foreground/80">{entry.actorName}:</span>
  <span class="flex-1">{entry.text}</span>
  <time
    datetime={new Date(entry.timestampMs).toISOString()}
    class="text-[10px] not-italic shrink-0"
  >
    {formatTime(entry.timestampMs)}
  </time>
</div>
