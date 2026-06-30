// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mount, unmount, flushSync } from 'svelte';
import ScenePublishPanel from './ScenePublishPanel.svelte';

// Drive the panel by mocking the store getters.
let state: Record<string, unknown>;
vi.mock('$lib/scenes/publishStore.svelte', () => ({
	publishStore: new Proxy({}, { get: (_t, k) => state[k as string] }),
}));

function renderPanel() {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const comp = mount(ScenePublishPanel, { target });
	flushSync();
	return { target, comp };
}

beforeEach(() => { state = { voteInProgress: false, isParticipant: false, phase: '', tally: null }; });

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

	it('participant sees the yes/no/pending tally and phase', () => {
		state = { voteInProgress: true, isParticipant: true, phase: 'COLLECTING', tally: { yes: 2, no: 1, pending: 3 } };
		const { target, comp } = renderPanel();
		expect(target.textContent).toMatch(/2/);
		expect(target.textContent).toMatch(/COLLECTING/);
		unmount(comp); target.remove();
	});
});
