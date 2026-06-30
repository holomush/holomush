<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
	import { publishStore } from '$lib/scenes/publishStore.svelte';
</script>

{#if publishStore.voteInProgress}
	{#if publishStore.isParticipant && publishStore.tally}
		<section class="publish-panel" aria-label="Publication vote">
			<header>Publication vote — {publishStore.phase}</header>
			<ul class="tally">
				<li>Yes <strong>{publishStore.tally.yes}</strong></li>
				<li>No <strong>{publishStore.tally.no}</strong></li>
				<li>Pending <strong>{publishStore.tally.pending}</strong></li>
			</ul>
		</section>
	{:else}
		<section class="publish-panel" aria-label="Publication vote">
			<span class="badge">Publication vote in progress</span>
		</section>
	{/if}
{/if}
