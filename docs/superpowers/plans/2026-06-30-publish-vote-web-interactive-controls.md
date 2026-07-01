<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Publish-Vote Web Interactive Controls Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a scene participant start a publication vote, cast/change a Yes/No vote, and (owner) withdraw it — all from the web GUI, over the typed BFF RPC wrappers that already exist.

**Architecture:** A new thin `publishFlow.ts` (mirroring `lifecycleFlow.ts`) drives the three writes over `client.ts`. `publishStore` gains the caller's optimistic vote (`myVote`/`myVotePending`/`myVoteAcked`) for a dark→bright button transition; the aggregate tally stays purely event-driven. `ScenePublishPanel` hosts Yes/No + owner Withdraw during `COLLECTING`; `SceneContextRail` hosts Start in its action group. No Go/proto/facade change.

**Tech Stack:** SvelteKit 5 (runes), TypeScript, Vitest (`pnpm -C web test:unit`), Connect-ES generated clients. Spec: `docs/superpowers/specs/2026-06-30-publish-vote-web-interactive-controls-design.md`.

---

## Task 1: publishStore — caller's optimistic vote (`myVote` / `myVotePending` / `myVoteAcked`)

**Files:**

- Modify: `web/src/lib/scenes/publishStore.svelte.ts`
- Test: `web/src/lib/scenes/publishStore.test.ts`

Adds the caller's own-vote state and the dark→bright signal. `myVote` is `boolean | null` (`true`=Yes, `false`=No), matching the RPC's `bool vote`. Brighten (clear `myVotePending`) requires **both** the caller's own cast RPC to have acked (`myVoteAcked`, set by `publishFlow` in Task 2 via `_ackVote()`) **and** a subsequent `refetchTally` to complete — so a different participant's vote→refetch can't brighten the button early (the debounce at `:43-51` is per-scene, not per-voter). Getters gate on `myVoteAttemptId === activeAttemptId`, so a stale ballot never bleeds into a new attempt.

- [ ] **Step 1: Write the failing tests**

Append this block to `web/src/lib/scenes/publishStore.test.ts` (after the existing `describe('publishStore onEvent', …)` block, before the final closing lines):

```ts
describe('publishStore myVote (caller optimistic vote)', () => {
	beforeEach(() => { vi.useFakeTimers(); });
	afterEach(() => { vi.useRealTimers(); });

	async function asParticipant() {
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValue({
			id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 1, no: 0, pending: 4 },
		});
		await publishStore.loadColdStart('C1', 'SC1');
	}

	it('_markVotePending sets the ballot and the dark (pending) flag', async () => {
		await asParticipant();
		publishStore._markVotePending(true);
		expect(publishStore.myVote).toBe(true);
		expect(publishStore.myVotePending).toBe(true);
	});

	it('brightens (clears pending) only after the vote is acked AND a refetch lands', async () => {
		await asParticipant();
		publishStore._markVotePending(true);
		// A refetch WITHOUT an ack must NOT brighten.
		getPublishedScene.mockResolvedValueOnce({ id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 2, no: 0, pending: 3 } });
		publishStore.onEvent({ type: 'core-scenes:scene_publish_vote_cast', metadata: { scene_id: 'SC1' } } as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(publishStore.myVotePending).toBe(true); // still dark: not acked yet

		// Once acked, the NEXT refetch brightens.
		publishStore._ackVote();
		getPublishedScene.mockResolvedValueOnce({ id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 2, no: 0, pending: 3 } });
		publishStore.onEvent({ type: 'core-scenes:scene_publish_vote_cast', metadata: { scene_id: 'SC1' } } as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(publishStore.myVotePending).toBe(false); // bright
		expect(publishStore.myVote).toBe(true);
	});

	it('_clearVote resets the ballot and both flags', async () => {
		await asParticipant();
		publishStore._markVotePending(false);
		publishStore._ackVote();
		publishStore._clearVote();
		expect(publishStore.myVote).toBeNull();
		expect(publishStore.myVotePending).toBe(false);
	});

	it('a ballot from a prior attempt does not bleed into a new attempt', async () => {
		await asParticipant();
		publishStore._markVotePending(true);
		expect(publishStore.myVote).toBe(true);
		// A lifecycle event advances the pointer to a different attempt id.
		getScene.mockResolvedValueOnce({ activePublishAttemptId: 'att-2', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValueOnce({ id: 'att-2', status: 'COLLECTING', voteSummary: { yes: 0, no: 0, pending: 5 } });
		publishStore.onEvent({ type: 'core-scenes:scene_publish_started', metadata: { scene_id: 'SC1' } } as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(publishStore.activeAttemptId).toBe('att-2');
		expect(publishStore.myVote).toBeNull(); // scoped out by attempt guard
	});

	it('reset clears the optimistic vote', async () => {
		await asParticipant();
		publishStore._markVotePending(true);
		publishStore.reset();
		expect(publishStore.myVote).toBeNull();
		expect(publishStore.myVotePending).toBe(false);
	});
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `pnpm -C web test:unit src/lib/scenes/publishStore.test.ts`
Expected: FAIL — `publishStore._markVotePending is not a function` (and `myVote` getter undefined).

- [ ] **Step 3: Add the state, transitions, and getters**

In `web/src/lib/scenes/publishStore.svelte.ts`, add these `$state` declarations next to the existing ones (after `let loading = $state(false);`, around line 25):

```ts
// Caller's own ballot for this attempt: true=Yes, false=No, null=not cast.
// Boolean to match the RPC's `bool vote` field — no string↔bool mapping.
let myVote = $state<boolean | null>(null);
// Dark (pending) between click and the count landing; brightens once the
// caller's own cast is acked AND a fresh tally has been refetched.
let myVotePending = $state(false);
// The caller's own cast RPC has resolved (set by publishFlow via _ackVote).
// Gates brighten so a *different* participant's refetch can't brighten early.
let myVoteAcked = $state(false);
// The attempt myVote was cast under (scoping guard for the getters).
let myVoteAttemptId = $state('');
```

In `refetchTally`, inside the success path, immediately after `stale = false;` (currently line 111), add the brighten check:

```ts
			stale = false;
			// Brighten the caller's own vote button only once their cast has acked
			// AND this fresh count has landed (own-ack gate closes the cross-voter race).
			if (myVotePending && myVoteAcked && myVoteAttemptId === activeAttemptId) {
				myVotePending = false;
			}
```

In `reset()`, add the four clears alongside the existing resets (after `characterId = '';`, currently line 173):

```ts
		myVote = null;
		myVotePending = false;
		myVoteAcked = false;
		myVoteAttemptId = '';
```

- [ ] **Step 4: Export the getters and internal setters**

In the `export const publishStore = { … }` object, add getters after `get loading() { return loading; },` (line 183) — the getters **gate on the attempt** so a stale ballot reads as absent:

```ts
	get myVote() { return myVoteAttemptId === activeAttemptId ? myVote : null; },
	get myVotePending() { return myVoteAttemptId === activeAttemptId ? myVotePending : false; },
```

And add the internal setters alongside the existing `_refetchTally` / `_setActiveAttempt` (after line 189):

```ts
	_markVotePending: (v: boolean) => {
		myVote = v;
		myVotePending = true;
		myVoteAcked = false;
		myVoteAttemptId = activeAttemptId;
	},
	_ackVote: () => { myVoteAcked = true; },
	_clearVote: () => { myVote = null; myVotePending = false; myVoteAcked = false; },
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `pnpm -C web test:unit src/lib/scenes/publishStore.test.ts`
Expected: PASS (all existing + 5 new tests).

- [ ] **Step 6: Commit**

```bash
jj commit -m "feat(scenes-web): publishStore caller optimistic vote (myVote/pending/acked)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: publishFlow.ts — start / cast / withdraw actions

**Files:**

- Create: `web/src/lib/scenes/publishFlow.ts`
- Test: `web/src/lib/scenes/publishFlow.test.ts`

Three thin actions mirroring `lifecycleFlow.ts:14-38`, over the existing wrappers (`client.ts:299/307/315`). `startPublishAction` takes the uniform `{ sceneId, characterId }` (so the rail's `runLifecycle` wrapper can call it). `castVoteAction` / `withdrawAction` are panel-invoked and take only what they use; both derive the attempt from `publishStore.activeAttemptId` and early-return a silent no-op if it's empty (defensive — the controls only render when an attempt exists).

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/scenes/publishFlow.test.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('./altSessions.svelte', () => ({ ensureSession: vi.fn(async () => 'sess-1') }));
vi.mock('./client', () => ({
	startScenePublish: vi.fn(async () => ({ id: 'att-1' })),
	castPublishSceneVote: vi.fn(async () => ({})),
	withdrawScenePublish: vi.fn(async () => ({})),
}));
const store = { activeAttemptId: 'att-1', _markVotePending: vi.fn(), _ackVote: vi.fn(), _clearVote: vi.fn() };
vi.mock('./publishStore.svelte', () => ({ publishStore: store }));

import { startPublishAction, castVoteAction, withdrawAction } from './publishFlow';
import { ensureSession } from './altSessions.svelte';
import { startScenePublish, castPublishSceneVote, withdrawScenePublish } from './client';

beforeEach(() => {
	vi.clearAllMocks();
	store.activeAttemptId = 'att-1';
});

describe('startPublishAction', () => {
	it('ensures the alt session and starts the publish vote', async () => {
		await startPublishAction({ sceneId: 'scene-1', characterId: 'char-1' });
		expect(ensureSession).toHaveBeenCalledWith('char-1');
		expect(startScenePublish).toHaveBeenCalledWith('sess-1', { characterId: 'char-1', sceneId: 'scene-1' });
	});
});

describe('castVoteAction', () => {
	it('marks pending, casts the vote, then acks on success', async () => {
		await castVoteAction({ characterId: 'char-1', vote: true });
		expect(store._markVotePending).toHaveBeenCalledWith(true);
		expect(castPublishSceneVote).toHaveBeenCalledWith('sess-1', {
			characterId: 'char-1', publishedSceneId: 'att-1', vote: true,
		});
		expect(store._ackVote).toHaveBeenCalledTimes(1);
		expect(store._clearVote).not.toHaveBeenCalled();
	});

	it('reverts (clearVote) and rethrows when the RPC rejects; does not ack', async () => {
		vi.mocked(castPublishSceneVote).mockRejectedValueOnce(new Error('failed_precondition'));
		await expect(castVoteAction({ characterId: 'char-1', vote: false })).rejects.toThrow('failed_precondition');
		expect(store._clearVote).toHaveBeenCalledTimes(1);
		expect(store._ackVote).not.toHaveBeenCalled();
	});

	it('is a silent no-op when there is no active attempt', async () => {
		store.activeAttemptId = '';
		await castVoteAction({ characterId: 'char-1', vote: true });
		expect(ensureSession).not.toHaveBeenCalled();
		expect(castPublishSceneVote).not.toHaveBeenCalled();
		expect(store._markVotePending).not.toHaveBeenCalled();
	});
});

describe('withdrawAction', () => {
	it('ensures the alt session and withdraws the active attempt', async () => {
		await withdrawAction({ characterId: 'char-1' });
		expect(withdrawScenePublish).toHaveBeenCalledWith('sess-1', {
			characterId: 'char-1', publishedSceneId: 'att-1',
		});
	});

	it('is a silent no-op when there is no active attempt', async () => {
		store.activeAttemptId = '';
		await withdrawAction({ characterId: 'char-1' });
		expect(ensureSession).not.toHaveBeenCalled();
		expect(withdrawScenePublish).not.toHaveBeenCalled();
	});
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `pnpm -C web test:unit src/lib/scenes/publishFlow.test.ts`
Expected: FAIL — cannot resolve `./publishFlow` (module does not exist).

- [ ] **Step 3: Create the module**

Create `web/src/lib/scenes/publishFlow.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { ensureSession } from './altSessions.svelte';
import { startScenePublish, castPublishSceneVote, withdrawScenePublish } from './client';
import { publishStore } from './publishStore.svelte';

type StartArgs = { sceneId: string; characterId: string };
type VoteArgs = { characterId: string; vote: boolean };
type WithdrawArgs = { characterId: string };

/**
 * Starts a publication vote (structural write → typed RPC). No store mutation:
 * the scene_publish_started event drives reloadPointer → the panel appears.
 * Takes the uniform { sceneId, characterId } shape so the rail's runLifecycle
 * wrapper can call it.
 */
export async function startPublishAction({ sceneId, characterId }: StartArgs): Promise<void> {
	const sessionId = await ensureSession(characterId);
	await startScenePublish(sessionId, { characterId, sceneId });
}

/**
 * Casts or changes the caller's Yes(true)/No(false) vote on the active attempt.
 * Optimistically marks the button dark (pending) before the RPC; reverts on
 * failure; acks on success so the next refetch brightens it (publishStore §5).
 * Silent no-op if no attempt is active (defensive — controls only render then).
 */
export async function castVoteAction({ characterId, vote }: VoteArgs): Promise<void> {
	const publishedSceneId = publishStore.activeAttemptId;
	if (!publishedSceneId) return;
	const sessionId = await ensureSession(characterId);
	publishStore._markVotePending(vote);
	try {
		await castPublishSceneVote(sessionId, { characterId, publishedSceneId, vote });
	} catch (e) {
		publishStore._clearVote();
		throw e;
	}
	publishStore._ackVote();
}

/**
 * Withdraws (cancels) the active attempt the caller owns. No store mutation:
 * the scene_publish_withdrawn event drives reloadPointer → the panel clears.
 * Silent no-op if no attempt is active.
 */
export async function withdrawAction({ characterId }: WithdrawArgs): Promise<void> {
	const publishedSceneId = publishStore.activeAttemptId;
	if (!publishedSceneId) return;
	const sessionId = await ensureSession(characterId);
	await withdrawScenePublish(sessionId, { characterId, publishedSceneId });
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `pnpm -C web test:unit src/lib/scenes/publishFlow.test.ts`
Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(scenes-web): publishFlow start/cast/withdraw actions

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: ScenePublishPanel — Yes/No + owner Withdraw controls

**Files:**

- Modify: `web/src/lib/components/scenes/ScenePublishPanel.svelte`
- Test: `web/src/lib/components/scenes/ScenePublishPanel.svelte.test.ts`

Adds props `{ characterId, isOwner }` and, in the participant `COLLECTING` branch, Yes/No vote buttons (the chosen one dark via `opacity-60` while `myVotePending`, bright via the `default` variant once confirmed — brand cyan tokens, no new hex) plus an owner-only inline Withdraw confirm (`confirmingWithdraw` swaps the button for "Cancel this publication vote? [Withdraw] [Keep]" — inline, so it's testable without a portal). A panel-local `controlErr` line surfaces RPC errors. Observer/loading branches are unchanged.

- [ ] **Step 1: Update the test harness and write the failing tests**

In `web/src/lib/components/scenes/ScenePublishPanel.svelte.test.ts`, replace the mock block and `renderPanel` helper (lines 8-22) with a version that also mocks `publishFlow` and passes the new required props:

```ts
// Drive the panel by mocking the store getters.
let state: Record<string, unknown>;
vi.mock('$lib/scenes/publishStore.svelte', () => ({
	publishStore: new Proxy({}, { get: (_t, k) => state[k as string] }),
}));
const castVoteAction = vi.fn(async () => {});
const withdrawAction = vi.fn(async () => {});
vi.mock('$lib/scenes/publishFlow', () => ({ castVoteAction, withdrawAction }));

function renderPanel(props: { characterId?: string; isOwner?: boolean } = {}) {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const comp = mount(ScenePublishPanel, {
		target,
		props: { characterId: 'C1', isOwner: false, ...props },
	});
	flushSync();
	return { target, comp };
}

beforeEach(() => {
	vi.clearAllMocks();
	state = { voteInProgress: false, isParticipant: false, phase: '', tally: null, myVote: null, myVotePending: false };
});
```

Then append this `describe` block at the end of the file:

```ts
function button(target: HTMLElement, label: RegExp): HTMLButtonElement | undefined {
	return ([...target.querySelectorAll('button')] as HTMLButtonElement[]).find((b) =>
		label.test((b.textContent ?? '').trim()),
	);
}

describe('ScenePublishPanel controls', () => {
	const collecting = { voteInProgress: true, isParticipant: true, phase: 'COLLECTING', tally: { yes: 1, no: 0, pending: 4 } };

	it('participant in COLLECTING sees Yes and No buttons; clicking Yes casts true', async () => {
		state = { ...collecting, myVote: null, myVotePending: false };
		const { target, comp } = renderPanel({ characterId: 'C1' });
		const yes = button(target, /^Yes$/);
		expect(yes).toBeTruthy();
		expect(button(target, /^No$/)).toBeTruthy();
		yes!.click();
		flushSync();
		expect(castVoteAction).toHaveBeenCalledWith({ characterId: 'C1', vote: true });
		unmount(comp); target.remove();
	});

	it('shows the chosen vote dark (opacity-60) while pending, bright once confirmed', () => {
		state = { ...collecting, myVote: true, myVotePending: true };
		let r = renderPanel();
		expect(button(r.target, /^Yes$/)!.className).toMatch(/opacity-60/);
		unmount(r.comp); r.target.remove();

		state = { ...collecting, myVote: true, myVotePending: false };
		r = renderPanel();
		expect(button(r.target, /^Yes$/)!.className).not.toMatch(/opacity-60/);
		unmount(r.comp); r.target.remove();
	});

	it('owner sees Withdraw; confirm then withdraw calls withdrawAction', async () => {
		state = { ...collecting };
		const { target, comp } = renderPanel({ characterId: 'C1', isOwner: true });
		button(target, /Withdraw vote/)!.click();
		flushSync();
		expect(target.textContent).toMatch(/cancel this publication vote/i);
		button(target, /^Withdraw$/)!.click();
		flushSync();
		expect(withdrawAction).toHaveBeenCalledWith({ characterId: 'C1' });
		unmount(comp); target.remove();
	});

	it('non-owner sees no Withdraw control', () => {
		state = { ...collecting };
		const { target, comp } = renderPanel({ isOwner: false });
		expect(button(target, /Withdraw/)).toBeUndefined();
		unmount(comp); target.remove();
	});

	it('no vote controls outside COLLECTING (e.g. COOLOFF)', () => {
		state = { voteInProgress: true, isParticipant: true, phase: 'COOLOFF', tally: { yes: 3, no: 0, pending: 0 } };
		const { target, comp } = renderPanel({ isOwner: true });
		expect(button(target, /^Yes$/)).toBeUndefined();
		expect(button(target, /Withdraw/)).toBeUndefined();
		unmount(comp); target.remove();
	});
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `pnpm -C web test:unit src/lib/components/scenes/ScenePublishPanel.svelte.test.ts`
Expected: FAIL — no Yes/No/Withdraw buttons found (controls not implemented).

- [ ] **Step 3: Implement the controls in the component**

Replace the entire contents of `web/src/lib/components/scenes/ScenePublishPanel.svelte` with:

```svelte
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
						class={`h-6 text-xs ${publishStore.myVote === true && publishStore.myVotePending ? 'opacity-60' : ''}`}
						variant={publishStore.myVote === true ? 'default' : 'outline'}
						onclick={() => vote(true)}>Yes</Button>
					<Button
						size="sm"
						class={`h-6 text-xs ${publishStore.myVote === false && publishStore.myVotePending ? 'opacity-60' : ''}`}
						variant={publishStore.myVote === false ? 'default' : 'outline'}
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `pnpm -C web test:unit src/lib/components/scenes/ScenePublishPanel.svelte.test.ts`
Expected: PASS (existing 4 + 5 new tests). The existing tests already pass the new required props via the updated `renderPanel`.

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(scenes-web): ScenePublishPanel vote + owner withdraw controls

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: SceneContextRail — Start publish vote button + panel props

**Files:**

- Modify: `web/src/lib/components/scenes/SceneContextRail.svelte`
- Test: `web/src/lib/components/scenes/SceneContextRail.svelte.test.ts`

Adds a **Start publish vote** button to the rail's action group with a new ended-scene visibility branch, gated `isParticipant && state==='ended' && !publishStore.loading && !publishStore.voteInProgress` (the `!loading` guard prevents a false Start flash during cold-start). Start runs through the existing `runLifecycle` wrapper. Also wires `characterId` + `isOwner` into the now-prop'd `ScenePublishPanel`.

- [ ] **Step 1: Write the failing tests**

In `web/src/lib/components/scenes/SceneContextRail.svelte.test.ts`, add these mocks alongside the existing `vi.mock` calls (after the `settingsFlow` mock, ~line 32):

```ts
vi.mock('$lib/scenes/publishFlow', () => ({ startPublishAction: vi.fn(), castVoteAction: vi.fn(), withdrawAction: vi.fn() }));
let publishState: Record<string, unknown> = { voteInProgress: false, loading: false, isParticipant: false, tally: null, phase: '' };
vi.mock('$lib/scenes/publishStore.svelte', () => ({
	publishStore: new Proxy({}, { get: (_t, k) => publishState[k as string] }),
}));
```

Add the import alongside the others (~line 34):

```ts
import { startPublishAction } from '$lib/scenes/publishFlow';
```

Reset the publish state in `afterEach` (inside the existing `afterEach`, add):

```ts
	publishState = { voteInProgress: false, loading: false, isParticipant: false, tally: null, phase: '' };
```

Append this `describe` block at the end of the file:

```ts
describe('SceneContextRail — start publish vote', () => {
	it('shows Start on an ended scene for a participant with no active attempt', () => {
		const target = render(makeScene({ state: 'ended', role: 'owner', ownerId: OWNER_ID, asCharacterId: OWNER_ID }));
		expect(lifecycleButton(target, /^Start publish vote$/)).not.toBeNull();
	});

	it('hides Start while cold-start is loading', () => {
		publishState = { ...publishState, loading: true };
		const target = render(makeScene({ state: 'ended', role: 'owner' }));
		expect(lifecycleButton(target, /^Start publish vote$/)).toBeNull();
	});

	it('hides Start when a vote is already in progress', () => {
		publishState = { ...publishState, voteInProgress: true };
		const target = render(makeScene({ state: 'ended', role: 'owner' }));
		expect(lifecycleButton(target, /^Start publish vote$/)).toBeNull();
	});

	it('hides Start on a non-ended scene', () => {
		const target = render(makeScene({ state: 'active', role: 'owner' }));
		expect(lifecycleButton(target, /^Start publish vote$/)).toBeNull();
	});

	it('hides Start for an observer', () => {
		const target = render(makeScene({ state: 'ended', role: 'observer', asCharacterId: MEMBER_ID }));
		expect(lifecycleButton(target, /^Start publish vote$/)).toBeNull();
	});

	it('clicking Start invokes startPublishAction with the scene + acting character', () => {
		const target = render(makeScene({ state: 'ended', role: 'owner', asCharacterId: OWNER_ID }));
		lifecycleButton(target, /^Start publish vote$/)!.click();
		flushSync();
		expect(startPublishAction).toHaveBeenCalledWith({ sceneId: 'scene-1', characterId: OWNER_ID });
	});
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `pnpm -C web test:unit src/lib/components/scenes/SceneContextRail.svelte.test.ts`
Expected: FAIL — no `Start publish vote` button found.

- [ ] **Step 3: Wire the store, action, predicate, button, and panel props**

In `web/src/lib/components/scenes/SceneContextRail.svelte`, add two imports after the existing flow imports (after line 14):

```svelte
  import { startPublishAction } from '$lib/scenes/publishFlow';
  import { publishStore } from '$lib/scenes/publishStore.svelte';
```

Add the `showStartPublish` derived alongside the other lifecycle deriveds (after line 26, `let showResume = …`):

```svelte
  let showStartPublish = $derived(
    isParticipant && scene?.state === 'ended' && !publishStore.loading && !publishStore.voteInProgress,
  );
```

Widen the action-group guard (line 116) to include the new case:

```svelte
      {#if showPause || showResume || showEnd || canEditSettings || showStartPublish}
```

Add the Start button inside that group, immediately after the `{/if}` that closes the `showEnd` block (after line 134, before the closing `</div>` of the button row):

```svelte
          {#if showStartPublish}
            <Button variant="outline" size="sm" class="h-6 text-xs"
              onclick={() => runLifecycle(startPublishAction)}>Start publish vote</Button>
          {/if}
```

Pass props to the panel — replace the bare mount (line 142) `<ScenePublishPanel />` with:

```svelte
    <ScenePublishPanel characterId={scene.asCharacterId} isOwner={isOwner} />
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `pnpm -C web test:unit src/lib/components/scenes/SceneContextRail.svelte.test.ts`
Expected: PASS (existing + 6 new tests).

- [ ] **Step 5: Run the full scenes-web unit suite + typecheck**

Run: `pnpm -C web test:unit src/lib/scenes src/lib/components/scenes`
Expected: PASS (all publish-vote + rail + panel + store + flow tests).

Run: `pnpm -C web check`
Expected: no TypeScript / svelte-check errors (the new props, boolean `vote`, and store getters typecheck end-to-end).

- [ ] **Step 6: Commit**

```bash
jj commit -m "feat(scenes-web): rail Start publish vote button + panel prop wiring

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Post-Implementation

- [ ] Run the full web unit suite: `pnpm -C web test:unit` — all green.
- [ ] Run `pnpm -C web check` (svelte-check / tsc) — no errors.
- [ ] Run `task pr-prep` from the repo root — fast lane green (it runs `fmt:check`, license, lint; the web unit suite runs in CI's web job).
- [ ] `holomush-5rh.24.41.7` (Tier-3 Playwright E2E) is now unblocked: drive the participant cast path UI-driven (telnet-free); seed the second voter / observer via a second browser context. Not part of this plan.
- [ ] Manual smoke (optional): `task dev`, open a scene as owner, end it, Start publish vote, cast Yes (dark→bright), change to No, Withdraw (confirm) → panel clears; observe as a non-participant → badge only.
<!-- adr-capture: sha256=c7f0d2f507cda584; session=cli; ts=2026-07-01T00:43:00Z; adrs= -->
