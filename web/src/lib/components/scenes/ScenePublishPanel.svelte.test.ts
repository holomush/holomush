// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mount, unmount, flushSync } from 'svelte';
import ScenePublishPanel from './ScenePublishPanel.svelte';

// Drive the panel by mocking the store getters.
let state: Record<string, unknown>;
// `activeAttemptId` is backed by a real rune (rather than a plain `state` field)
// so a test can mutate it after mount and observe the panel's `$effect` react —
// the plain `state` object is NOT reactive, so mutating a plain field would not
// re-fire the component's effect.
let activeAttemptId = $state('');
vi.mock('$lib/scenes/publishStore.svelte', () => ({
	publishStore: new Proxy(
		{},
		{ get: (_t, k) => (k === 'activeAttemptId' ? activeAttemptId : state[k as string]) },
	),
}));
// vi.hoisted: the mock factory below references these eagerly, so they must be
// initialized before vi.mock hoists (TDZ otherwise; see workspaceStore.test.ts).
const { castVoteAction, withdrawAction } = vi.hoisted(() => ({
	castVoteAction: vi.fn(async () => {}),
	withdrawAction: vi.fn(async () => {}),
}));
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
	activeAttemptId = '';
});

describe('ScenePublishPanel', () => {
	it('renders nothing when no vote is in progress', () => {
		state = { voteInProgress: false, isParticipant: false, phase: '', tally: null };
		const { target, comp } = renderPanel();
		expect(target.textContent?.trim()).toBe('');
		unmount(comp); target.remove();
	});

	it('observer sees only the in-progress badge, NO counts', () => {
		state = { voteInProgress: true, isParticipant: false, phase: 'COLLECTING', tally: null };
		const { target, comp } = renderPanel();
		expect(target.textContent).toMatch(/publication vote in progress/i);
		expect(target.textContent).not.toMatch(/\d+\s*(yes|no|pending)/i); // no counts
		expect(target.textContent).not.toMatch(/COLLECTING/);              // no phase
		unmount(comp); target.remove();
	});

	it('shows a neutral loading state during cold start, not the observer badge', () => {
		// voteInProgress true but participant status not yet resolved.
		state = { voteInProgress: true, loading: true, isParticipant: false, phase: 'COLLECTING', tally: null };
		const { target, comp } = renderPanel();
		expect(target.textContent).toMatch(/publication vote/i);
		expect(target.textContent).not.toMatch(/in progress/i); // not the observer copy
		expect(target.textContent).not.toMatch(/COLLECTING/);   // no phase leak while loading
		expect(target.querySelector('[aria-busy="true"]')).not.toBeNull();
		unmount(comp); target.remove();
	});

	it('participant sees the yes/no/pending tally and phase', () => {
		state = { voteInProgress: true, isParticipant: true, phase: 'COLLECTING', tally: { yes: 2, no: 1, pending: 3 } };
		const { target, comp } = renderPanel();
		expect(target.textContent).toMatch(/2/);
		expect(target.textContent).toMatch(/COLLECTING/);
		unmount(comp); target.remove();
	});
});

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

	it('resets an armed withdraw-confirm when the active attempt changes (o5urv.6)', () => {
		activeAttemptId = 'att-1';
		state = { ...collecting };
		const { target, comp } = renderPanel({ characterId: 'C1', isOwner: true });
		button(target, /Withdraw vote/)!.click();
		flushSync();
		expect(target.textContent).toMatch(/cancel this publication vote/i);

		// The active attempt changes (e.g. this attempt resolved/withdrew and a new
		// one started) while the confirm is still armed — it MUST NOT bleed forward
		// onto the new attempt.
		activeAttemptId = 'att-2';
		flushSync();
		expect(target.textContent).not.toMatch(/cancel this publication vote/i);
		expect(button(target, /Withdraw vote/)).toBeTruthy();

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
