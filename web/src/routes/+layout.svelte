<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import TopBar from '$lib/components/TopBar.svelte';
  import { restoreSession } from '$lib/stores/authStore';
  import { onMount } from 'svelte';
  let { children } = $props();
  onMount(() => restoreSession());
</script>

<TopBar />
<main>{@render children()}</main>

<style>
  main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
</style>
