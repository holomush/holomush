<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
	import { publishStore } from '$lib/scenes/publishStore.svelte';
	import { castVoteAction, withdrawAction } from '$lib/scenes/publishFlow';
	import { Button } from '$lib/components/ui/button/index.js';
	import { cn } from '$lib/utils';

	// Defaults are a safety net, not a real caller shape: the sole mount site
	// (SceneContextRail) always supplies both props. `characterId` is only read
	// inside vote()/doWithdraw(), which fire only when the panel is rendered.
	let { characterId = '', isOwner = false }: { characterId?: string; isOwner?: boolean } = $props();

	let controlErr = $state('');
	let confirmingWithdraw = $state(false);
	// Withdraw in-flight lock: doWithdraw() hides the confirm row immediately, which
	// re-reveals the "Withdraw vote" button while the RPC is still pending — without
	// this guard a second confirm+click would fire a concurrent withdrawAction.
	let withdrawInFlight = $state(false);

	// The panel persists across scene/attempt switches (both scenes can have an
	// active vote), so a stale confirm-withdraw or error must not bleed into the
	// next attempt — mirrors SceneContextRail's lifecycleErr reset on scene change.
	$effect(() => {
		void publishStore.activeAttemptId;
		confirmingWithdraw = false;
		controlErr = '';
	});

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
		if (withdrawInFlight) return;
		controlErr = '';
		confirmingWithdraw = false;
		withdrawInFlight = true;
		try {
			await withdrawAction({ characterId });
		} catch (e) {
			controlErr = e instanceof Error ? e.message : 'Withdraw failed';
		} finally {
			withdrawInFlight = false;
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
						class={cn('h-6 text-xs', isPending(true) && 'opacity-60')}
						variant={isActive(true) ? 'default' : 'outline'}
						disabled={publishStore.castInFlight}
						onclick={() => vote(true)}>Yes</Button>
					<Button
						size="sm"
						class={cn('h-6 text-xs', isPending(false) && 'opacity-60')}
						variant={isActive(false) ? 'default' : 'outline'}
						disabled={publishStore.castInFlight}
						onclick={() => vote(false)}>No</Button>
				</div>
				{#if isOwner}
					{#if confirmingWithdraw}
						<div class="withdraw-confirm">
							<span class="text-xs text-muted-foreground">Cancel this publication vote?</span>
							<Button size="sm" variant="destructive" class="h-6 text-xs" disabled={withdrawInFlight} onclick={doWithdraw}>Withdraw</Button>
							<Button size="sm" variant="outline" class="h-6 text-xs" onclick={() => (confirmingWithdraw = false)}>Keep</Button>
						</div>
					{:else}
						<Button size="sm" variant="outline" class="h-6 text-xs" disabled={withdrawInFlight} onclick={() => (confirmingWithdraw = true)}>Withdraw vote</Button>
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
