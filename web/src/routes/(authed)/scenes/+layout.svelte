<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<!--
  Shared chrome for every /scenes/* route. ScenesShell owns the persistent
  three-pane workspace (scene-list sidebar · center · context rail); each
  sub-route renders as the shell's center column. Hoisting the sidebar here —
  rather than into the /scenes index page — keeps it on /scenes/browse and
  /scenes/archive too (holomush-5rh.30).
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { LayoutData } from './$types';
  import ScenesShell from '$lib/components/scenes/ScenesShell.svelte';

  let { data, children }: { data: LayoutData; children: Snippet } = $props();
</script>

<ScenesShell {data}>
  {@render children()}
</ScenesShell>
