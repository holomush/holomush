<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
	import { publishStore } from '$lib/scenes/publishStore.svelte';
	import { castVoteAction, withdrawAction } from '$lib/scenes/publishFlow';
	import { Button } from '$lib/components/ui/button/index.js';

	// Defaults keep the tree type-clean between this task's commit and Task 4
	// (which updates the rail's `<ScenePublishPanel />` mount to pass real values).
	// The rail always passes both once Task 4 lands; the panel only reads
	// `characterId` inside vote()/doWithdraw(), which fire only when rendered.
	let { characterId = '', isOwner = false }: { characterId?: string; isOwner?: boolean } = $props();

	let controlErr = $state('');
	let confirmingWithdraw = $state(false);

	// A vote button is "active" (brand variant) when it is the in-flight ballot OR
	// the confirmed vote with nothing pending; it is dark (opacity-60) only while
	// in-flight (pendingVote matches).
	function isPending(v: boolean): boolean {
		return publishStore.pendingVote === v;
	}
	function isActive(v: boolean): boolean {
		return isPending(v) || (publishStore.pendingVote === null && publishStore.myVote === v);
	}

	async function vote(v: boolean): Promise<void> {
		controlErr = '';
		try {
			await castVoteAction({ characterId, vote: v });
		} catch (e) {
			controlErr = e instanceof Error ? e.message : 'Vote failed';
		}
	}

	async function doWithdraw(): Promise<void> {
		controlErr = '';
		confirmingWithdraw = false;
		try {
			await withdrawAction({ characterId });
		} catch (e) {
			controlErr = e instanceof Error ? e.message : 'Withdraw failed';
		}
	}
</script>

{#if publishStore.voteInProgress}
	{#if publishStore.loading}
		<!-- Cold start in progress: isParticipant is not yet resolved, so show a
		     neutral loading state rather than the observer badge (which would
		     flash the wrong copy at a real participant on every initial load). -->
		<section class="publish-panel" aria-label="Publication vote" aria-busy="true">
			<span class="badge">Publication vote…</span>
		</section>
	{:else if publishStore.isParticipant && publishStore.tally}
		<section class="publish-panel" aria-label="Publication vote">
			<header>Publication vote — {publishStore.phase}</header>
			<ul class="tally">
				<li>Yes <strong>{publishStore.tally.yes}</strong></li>
				<li>No <strong>{publishStore.tally.no}</strong></li>
				<li>Pending <strong>{publishStore.tally.pending}</strong></li>
			</ul>
			{#if publishStore.phase === 'COLLECTING'}
				<div class="vote-buttons">
					<Button
						size="sm"
						class={`h-6 text-xs ${isPending(true) ? 'opacity-60' : ''}`}
						variant={isActive(true) ? 'default' : 'outline'}
						disabled={publishStore.castInFlight}
						onclick={() => vote(true)}>Yes</Button>
					<Button
						size="sm"
						class={`h-6 text-xs ${isPending(false) ? 'opacity-60' : ''}`}
						variant={isActive(false) ? 'default' : 'outline'}
						disabled={publishStore.castInFlight}
						onclick={() => vote(false)}>No</Button>
				</div>
				{#if isOwner}
					{#if confirmingWithdraw}
						<div class="withdraw-confirm">
							<span class="text-xs text-muted-foreground">Cancel this publication vote?</span>
							<Button size="sm" variant="destructive" class="h-6 text-xs" onclick={doWithdraw}>Withdraw</Button>
							<Button size="sm" variant="outline" class="h-6 text-xs" onclick={() => (confirmingWithdraw = false)}>Keep</Button>
						</div>
					{:else}
						<Button size="sm" variant="outline" class="h-6 text-xs" onclick={() => (confirmingWithdraw = true)}>Withdraw vote</Button>
					{/if}
				{/if}
				{#if controlErr}
					<p class="err" role="alert">{controlErr}</p>
				{/if}
			{/if}
		</section>
	{:else}
		<section class="publish-panel" aria-label="Publication vote">
			<span class="badge">Publication vote in progress</span>
		</section>
	{/if}
{/if}
