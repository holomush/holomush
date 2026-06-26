<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import CommunicationLine from '$lib/comm/CommunicationLine.svelte';
  import { commEventToLine, type CommEvent } from '$lib/comm/commLine';

  let { event }: { event: CommEvent } = $props();
</script>

<div class="event event-{event.type}" data-testid="event">
  <CommunicationLine line={commEventToLine(event)} />
</div>

<style>
  .event { line-height: 1.7; }
</style>
