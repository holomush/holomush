<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Publish-Vote Web Interactive Controls Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a scene participant start a publication vote, cast/change a Yes/No vote, and (owner) withdraw it — all from the web GUI, over the typed BFF RPC wrappers that already exist.

**Architecture:** A new thin `publishFlow.ts` (mirroring `lifecycleFlow.ts`) drives the three writes over `client.ts`. `publishStore` models the caller's vote as a **confirmed** value plus an optional **in-flight** value (`myVote` / `pendingVote` / `castInFlight`); the dark→bright transition is driven by the cast's **own RPC ack** (`_ackVote` promotes pending→confirmed), casts are serialized (lock raised synchronously before any await), and a failed cast reverts to the last confirmed vote; the aggregate tally stays purely event-driven. `ScenePublishPanel` hosts Yes/No + owner Withdraw during `COLLECTING`; `SceneContextRail` hosts Start in its action group. No Go/proto/facade change.

**Tech Stack:** SvelteKit 5 (runes), TypeScript, Vitest (`pnpm -C web test:unit`), Connect-ES generated clients. Spec: `docs/superpowers/specs/2026-06-30-publish-vote-web-interactive-controls-design.md`.

---

## Phase 1: Interactive publish-vote controls

Four sequential tasks: store state → flow → panel controls → rail Start. Each depends on the previous.

### Task 1: publishStore — caller vote state (confirmed + in-flight)

**Files:**

- Modify: `web/src/lib/scenes/publishStore.svelte.ts`
- Test: `web/src/lib/scenes/publishStore.test.ts`

Models the caller's vote as a **confirmed** value (`myVote`) plus an **in-flight** value (`pendingVote`), with `castInFlight` serializing casts (the panel disables the buttons while true, and `castVoteAction` raises the lock synchronously before any await). All boolean (`true`=Yes, `false`=No), matching the RPC's `bool vote`. Getters gate on `myVoteAttemptId === activeAttemptId`. The dark→bright promotion happens in `_ackVote` — driven by the cast's **own RPC ack**, never by a tally refetch — so no refetch can confirm a ballot; a failed cast (`_clearVote`) clears only `pendingVote`, leaving the last confirmed `myVote` intact. See spec §5.

- [ ] **Step 1: Write the failing tests**

Append this block to `web/src/lib/scenes/publishStore.test.ts` (after the existing `describe('publishStore onEvent', …)` block, before the final closing lines):

```ts
describe('publishStore caller vote (confirmed + in-flight)', () => {
	beforeEach(() => { vi.useFakeTimers(); });
	afterEach(() => { vi.useRealTimers(); });

	async function asParticipant() {
		getScene.mockResolvedValue({ activePublishAttemptId: 'att-1', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValue({
			id: 'att-1', status: 'COLLECTING', voteSummary: { yes: 1, no: 0, pending: 4 },
		});
		await publishStore.loadColdStart('C1', 'SC1');
	}

	it('_markVotePending sets the in-flight ballot dark and locks (castInFlight)', async () => {
		await asParticipant();
		publishStore._markVotePending(true);
		expect(publishStore.pendingVote).toBe(true);
		expect(publishStore.castInFlight).toBe(true);
		expect(publishStore.myVote).toBeNull(); // not confirmed yet
	});

	it('ack promotes the in-flight ballot to confirmed and unlocks (brighten)', async () => {
		await asParticipant();
		publishStore._markVotePending(true);
		publishStore._ackVote();
		expect(publishStore.pendingVote).toBeNull(); // promoted
		expect(publishStore.myVote).toBe(true);      // confirmed (bright)
		expect(publishStore.castInFlight).toBe(false); // unlocked
	});

	it('a failed re-cast reverts to the previously confirmed vote, not null', async () => {
		await asParticipant();
		// First cast confirmed: Yes (ack promotes directly).
		publishStore._markVotePending(true);
		publishStore._ackVote();
		expect(publishStore.myVote).toBe(true);
		// Re-cast No, then it fails → clearVote.
		publishStore._markVotePending(false);
		expect(publishStore.pendingVote).toBe(false);
		publishStore._clearVote();
		expect(publishStore.pendingVote).toBeNull();
		expect(publishStore.castInFlight).toBe(false);
		expect(publishStore.myVote).toBe(true); // previous confirmed retained
	});

	it('vote state from a prior attempt does not bleed into a new attempt', async () => {
		await asParticipant();
		publishStore._markVotePending(true);
		expect(publishStore.pendingVote).toBe(true);
		getScene.mockResolvedValueOnce({ activePublishAttemptId: 'att-2', publishStatus: 'COLLECTING' });
		getPublishedScene.mockResolvedValueOnce({ id: 'att-2', status: 'COLLECTING', voteSummary: { yes: 0, no: 0, pending: 5 } });
		publishStore.onEvent({ type: 'core-scenes:scene_publish_started', metadata: { scene_id: 'SC1' } } as never);
		await vi.advanceTimersByTimeAsync(300);
		expect(publishStore.activeAttemptId).toBe('att-2');
		expect(publishStore.pendingVote).toBeNull(); // scoped out by attempt guard
		expect(publishStore.myVote).toBeNull();
		expect(publishStore.castInFlight).toBe(false);
	});

	it('reset clears all vote state', async () => {
		await asParticipant();
		publishStore._markVotePending(true);
		publishStore.reset();
		expect(publishStore.myVote).toBeNull();
		expect(publishStore.pendingVote).toBeNull();
		expect(publishStore.castInFlight).toBe(false);
	});
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `pnpm -C web test:unit src/lib/scenes/publishStore.test.ts`
Expected: FAIL — `publishStore._markVotePending is not a function` (and `pendingVote` / `castInFlight` getters undefined).

- [ ] **Step 3: Add the state and the brighten/reset transitions**

In `web/src/lib/scenes/publishStore.svelte.ts`, add these `$state` declarations next to the existing ones (after `let loading = $state(false);`, around line 25):

```ts
// Caller's CONFIRMED vote for this attempt (true=Yes, false=No, null=none) —
// drives the bright highlight. Boolean matches the RPC's `bool vote`.
let myVote = $state<boolean | null>(null);
// Caller's IN-FLIGHT optimistic ballot — drives the dark highlight; null when idle.
let pendingVote = $state<boolean | null>(null);
// A cast RPC is in flight (click → ack/fail). The panel disables Yes/No while true,
// and castVoteAction raises it synchronously before any await, so a second cast
// can never overlap the first.
let castInFlight = $state(false);
// The attempt the vote state belongs to (scoping guard for the getters).
let myVoteAttemptId = $state('');
```

> The dark→bright promotion happens in `_ackVote` (Step 4), driven by the cast's
> own RPC ack — **not** by a tally refetch. `refetchTally` is therefore NOT
> modified for vote state; it keeps updating only the aggregate `tally` as today.

In `reset()`, add the clears alongside the existing resets (after `characterId = '';`, currently line 173):

```ts
		myVote = null;
		pendingVote = null;
		castInFlight = false;
		myVoteAttemptId = '';
```

- [ ] **Step 4: Export the getters and internal setters**

In the `export const publishStore = { … }` object, add getters after `get loading() { return loading; },` (line 183) — all three **gate on the attempt** so stale state reads as absent/idle:

```ts
	get myVote() { return myVoteAttemptId === activeAttemptId ? myVote : null; },
	get pendingVote() { return myVoteAttemptId === activeAttemptId ? pendingVote : null; },
	get castInFlight() { return myVoteAttemptId === activeAttemptId ? castInFlight : false; },
```

And add the internal setters alongside the existing `_refetchTally` / `_setActiveAttempt` (after line 189):

```ts
	_markVotePending: (v: boolean) => {
		pendingVote = v;
		castInFlight = true;
		myVoteAttemptId = activeAttemptId;
	},
	// Promote the in-flight ballot to confirmed (brighten) + unlock — driven by the
	// caller's own RPC ack, never by a refetch, so no refetch can confirm a ballot.
	_ackVote: () => { myVote = pendingVote; pendingVote = null; castInFlight = false; },
	_clearVote: () => { pendingVote = null; castInFlight = false; },
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `pnpm -C web test:unit src/lib/scenes/publishStore.test.ts`
Expected: PASS (all existing + 5 new tests).

- [ ] **Step 6: Commit**

```bash
jj commit -m "feat(scenes-web): publishStore caller vote state (confirmed + in-flight)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

### Task 2: publishFlow.ts — start / cast / withdraw actions

**Files:**

- Create: `web/src/lib/scenes/publishFlow.ts`
- Test: `web/src/lib/scenes/publishFlow.test.ts`

Three thin actions mirroring `lifecycleFlow.ts:14-38`, over the existing wrappers (`client.ts:299-335`). `startPublishAction` takes the uniform `{ sceneId, characterId }` (so the rail's `runLifecycle` wrapper can call it). `castVoteAction` / `withdrawAction` are panel-invoked and take only what they use; both derive the attempt from `publishStore.activeAttemptId` and early-return a silent no-op if it's empty. `castVoteAction` additionally no-ops when `publishStore.castInFlight` (serialize — defensive; the panel also disables the buttons).

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
const store = {
	activeAttemptId: 'att-1',
	castInFlight: false,
	_markVotePending: vi.fn(),
	_ackVote: vi.fn(),
	_clearVote: vi.fn(),
};
vi.mock('./publishStore.svelte', () => ({ publishStore: store }));

import { startPublishAction, castVoteAction, withdrawAction } from './publishFlow';
import { ensureSession } from './altSessions.svelte';
import { startScenePublish, castPublishSceneVote, withdrawScenePublish } from './client';

beforeEach(() => {
	vi.clearAllMocks();
	store.activeAttemptId = 'att-1';
	store.castInFlight = false;
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

	it('is a silent no-op when a cast is already in flight (serialize)', async () => {
		store.castInFlight = true;
		await castVoteAction({ characterId: 'char-1', vote: true });
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
 * Optimistically marks the button dark (pending) before the RPC; reverts to the
 * previous confirmed vote on failure; on success acks → promotes it to confirmed
 * (brighten, publishStore §5). No-op if no attempt is active or a cast is already
 * in flight (serialize). The lock (_markVotePending → castInFlight) is raised
 * SYNCHRONOUSLY before any await, so a second click during session setup bails.
 */
export async function castVoteAction({ characterId, vote }: VoteArgs): Promise<void> {
	const publishedSceneId = publishStore.activeAttemptId;
	if (!publishedSceneId) return;
	if (publishStore.castInFlight) return;
	publishStore._markVotePending(vote); // raise the lock before any await
	try {
		const sessionId = await ensureSession(characterId);
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

### Task 3: ScenePublishPanel — Yes/No + owner Withdraw controls

**Files:**

- Modify: `web/src/lib/components/scenes/ScenePublishPanel.svelte`
- Test: `web/src/lib/components/scenes/ScenePublishPanel.svelte.test.ts`

Adds props `{ characterId, isOwner }` and, in the participant `COLLECTING` branch, Yes/No vote buttons plus an owner-only inline Withdraw confirm. Each vote button renders **dark** (`opacity-60`, brand `default` variant) when `pendingVote` matches it, **bright** (`default` variant, no opacity) when it is the confirmed `myVote` and nothing is pending, and `outline` otherwise; **both are disabled while `castInFlight`** (serializes casts). A panel-local `controlErr` line surfaces RPC errors. Observer/loading branches unchanged.

- [ ] **Step 1: Update the test harness and write the failing tests**

In `web/src/lib/components/scenes/ScenePublishPanel.svelte.test.ts`, replace the mock block and `renderPanel` helper (lines 8-22) with a version that mocks `publishFlow` and passes the new props:

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
	state = { voteInProgress: false, isParticipant: false, phase: '', tally: null, myVote: null, pendingVote: null, castInFlight: false };
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
		state = { ...collecting, myVote: null, pendingVote: null, castInFlight: false };
		const { target, comp } = renderPanel({ characterId: 'C1' });
		const yes = button(target, /^Yes$/);
		expect(yes).toBeTruthy();
		expect(button(target, /^No$/)).toBeTruthy();
		yes!.click();
		flushSync();
		expect(castVoteAction).toHaveBeenCalledWith({ characterId: 'C1', vote: true });
		unmount(comp); target.remove();
	});

	it('shows the in-flight ballot dark (opacity-60), the confirmed vote bright', () => {
		// in-flight → dark
		state = { ...collecting, myVote: null, pendingVote: true, castInFlight: true };
		let r = renderPanel();
		expect(button(r.target, /^Yes$/)!.className).toMatch(/opacity-60/);
		unmount(r.comp); r.target.remove();
		// confirmed (no pending) → bright, no opacity
		state = { ...collecting, myVote: true, pendingVote: null, castInFlight: false };
		r = renderPanel();
		expect(button(r.target, /^Yes$/)!.className).not.toMatch(/opacity-60/);
		unmount(r.comp); r.target.remove();
	});

	it('disables both vote buttons while a cast is in flight', () => {
		state = { ...collecting, myVote: null, pendingVote: true, castInFlight: true };
		const { target, comp } = renderPanel();
		expect(button(target, /^Yes$/)!.disabled).toBe(true);
		expect(button(target, /^No$/)!.disabled).toBe(true);
		unmount(comp); target.remove();
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
		state = { voteInProgress: true, isParticipant: true, phase: 'COOLOFF', tally: { yes: 3, no: 0, pending: 0 }, myVote: null, pendingVote: null, castInFlight: false };
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `pnpm -C web test:unit src/lib/components/scenes/ScenePublishPanel.svelte.test.ts`
Expected: PASS (existing 4 + 6 new tests). The existing tests already pass the new props via the updated `renderPanel`.

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(scenes-web): ScenePublishPanel vote + owner withdraw controls

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

### Task 4: SceneContextRail — Start publish vote button + panel prop wiring

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
- [ ] Manual smoke (optional): `task dev`, open a scene as owner, end it, Start publish vote, cast Yes (dark→bright), change to No (buttons disable during each cast), Withdraw (confirm) → panel clears; observe as a non-participant → badge only.
<!-- adr-capture: sha256=cd23c8bb1df1eac8; session=cli; ts=2026-07-01T13:38:55Z; adrs= -->
