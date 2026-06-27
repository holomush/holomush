// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount } from 'svelte';
import type { WorkspaceScene } from '$lib/scenes/types';

vi.mock('$lib/scenes/lifecycleFlow', () => ({
	endSceneAction: vi.fn(),
	pauseSceneAction: vi.fn(),
	resumeSceneAction: vi.fn(),
}));

vi.mock('$lib/scenes/membershipFlow', () => ({
	inviteCharacters: vi.fn(),
	kickAction: vi.fn(),
	transferAction: vi.fn(),
	leaveAction: vi.fn(),
}));

// CharacterMultiSelect's $effect calls listAllCharacters on mount — neutralize the network call
// (render the real picker so its trigger appears, but no fetch).
vi.mock('$lib/scenes/directoryClient', () => ({ listAllCharacters: vi.fn(async () => []) }));

import SceneContextRail from './SceneContextRail.svelte';

const OWNER_ID = 'char-owner';
const MEMBER_ID = 'char-member';

function makeScene(overrides: Partial<WorkspaceScene> = {}): WorkspaceScene {
	return {
		sceneId: 'scene-1',
		title: 'Moonlit Terrace',
		locationId: 'loc-1',
		state: 'active',
		tags: [],
		role: 'owner',
		ownerId: OWNER_ID,
		asCharacterId: OWNER_ID,
		asCharacterName: 'Alice',
		lastActivityMs: 0n,
		entryCount: 0n,
		unread: 0,
		...overrides,
	};
}

function render(scene: WorkspaceScene): HTMLElement {
	const target = document.createElement('div');
	document.body.appendChild(target);
	mount(SceneContextRail, { target, props: { scene } });
	return target;
}

afterEach(() => {
	document.body.replaceChildren();
	vi.clearAllMocks();
});

/** Find a button whose trimmed text matches the anchored regex. */
function lifecycleButton(target: HTMLElement, label: RegExp): HTMLButtonElement | null {
	return (
		([...target.querySelectorAll('button')] as HTMLButtonElement[]).find((b) =>
			label.test((b.textContent ?? '').trim()),
		) ?? null
	);
}

describe('SceneContextRail lifecycle buttons', () => {
	it('shows Pause and End but not Resume for owner of active scene', () => {
		const t = render(makeScene({ state: 'active', role: 'owner', ownerId: OWNER_ID, asCharacterId: OWNER_ID }));
		expect(lifecycleButton(t, /^Pause$/)).not.toBeNull();
		expect(lifecycleButton(t, /^End$/)).not.toBeNull();
		expect(lifecycleButton(t, /^Resume$/)).toBeNull();
	});

	it('shows Resume and End but not Pause for owner of paused scene', () => {
		const t = render(makeScene({ state: 'paused', role: 'owner', ownerId: OWNER_ID, asCharacterId: OWNER_ID }));
		expect(lifecycleButton(t, /^Resume$/)).not.toBeNull();
		expect(lifecycleButton(t, /^End$/)).not.toBeNull();
		expect(lifecycleButton(t, /^Pause$/)).toBeNull();
	});

	it('shows only Resume for a member of a paused scene (D6 — member can resume)', () => {
		const t = render(
			makeScene({
				state: 'paused',
				role: 'member',
				ownerId: OWNER_ID,
				asCharacterId: MEMBER_ID,
			}),
		);
		expect(lifecycleButton(t, /^Resume$/)).not.toBeNull();
		expect(lifecycleButton(t, /^Pause$/)).toBeNull();
		expect(lifecycleButton(t, /^End$/)).toBeNull();
	});

	it('shows no lifecycle buttons for a member of an active scene', () => {
		const t = render(
			makeScene({
				state: 'active',
				role: 'member',
				ownerId: OWNER_ID,
				asCharacterId: MEMBER_ID,
			}),
		);
		expect(lifecycleButton(t, /^Pause$/)).toBeNull();
		expect(lifecycleButton(t, /^End$/)).toBeNull();
		expect(lifecycleButton(t, /^Resume$/)).toBeNull();
	});

	it('shows no lifecycle buttons for an observer of a paused scene', () => {
		const t = render(
			makeScene({
				state: 'paused',
				role: 'observer',
				ownerId: OWNER_ID,
				asCharacterId: 'char-obs',
			}),
		);
		expect(lifecycleButton(t, /^Pause$/)).toBeNull();
		expect(lifecycleButton(t, /^End$/)).toBeNull();
		expect(lifecycleButton(t, /^Resume$/)).toBeNull();
	});

	it('shows no lifecycle buttons for owner of ended scene', () => {
		const t = render(makeScene({ state: 'ended', role: 'owner', ownerId: OWNER_ID, asCharacterId: OWNER_ID }));
		expect(lifecycleButton(t, /^Pause$/)).toBeNull();
		expect(lifecycleButton(t, /^End$/)).toBeNull();
		expect(lifecycleButton(t, /^Resume$/)).toBeNull();
	});
});

describe('SceneContextRail roster actions', () => {
	const TWO_PARTICIPANTS = [
		{ id: OWNER_ID, name: 'Alice' },
		{ id: MEMBER_ID, name: 'Bob' },
	];

	it('owner of active scene: kebab on non-self row, none on own row, no Leave', () => {
		const t = render(
			makeScene({
				state: 'active',
				role: 'owner',
				ownerId: OWNER_ID,
				asCharacterId: OWNER_ID,
				participants: TWO_PARTICIPANTS,
			}),
		);
		// Kebab trigger for the member row
		expect(t.querySelector('[aria-label="Manage Bob"]')).not.toBeNull();
		// No kebab for the owner's own row
		expect(t.querySelector('[aria-label="Manage Alice"]')).toBeNull();
		// Owner never has a Leave button
		expect(lifecycleButton(t, /^Leave$/)).toBeNull();
	});

	it('member of active scene: Leave button present, no kebab, invite picker rendered', () => {
		const t = render(
			makeScene({
				state: 'active',
				role: 'member',
				ownerId: OWNER_ID,
				asCharacterId: MEMBER_ID,
				participants: TWO_PARTICIPANTS,
			}),
		);
		expect(lifecycleButton(t, /^Leave$/)).not.toBeNull();
		expect(t.querySelector('[aria-label^="Manage"]')).toBeNull();
		// CharacterMultiSelect renders a button with name="invite-picker"
		expect(t.querySelector('[name="invite-picker"]')).not.toBeNull();
	});

	it('observer: no kebab, no Leave, no invite picker', () => {
		const t = render(
			makeScene({
				state: 'active',
				role: 'observer',
				ownerId: OWNER_ID,
				asCharacterId: 'char-obs',
				participants: TWO_PARTICIPANTS,
			}),
		);
		expect(t.querySelector('[aria-label^="Manage"]')).toBeNull();
		expect(lifecycleButton(t, /^Leave$/)).toBeNull();
		expect(t.querySelector('[name="invite-picker"]')).toBeNull();
	});

	it('ended scene: kebab absent (canManage=false), Leave absent for owner', () => {
		const t = render(
			makeScene({
				state: 'ended',
				role: 'owner',
				ownerId: OWNER_ID,
				asCharacterId: OWNER_ID,
				participants: TWO_PARTICIPANTS,
			}),
		);
		// canManage is false for ended scenes — kebab is hidden
		expect(t.querySelector('[aria-label^="Manage"]')).toBeNull();
		// Owner never gets Leave
		expect(lifecycleButton(t, /^Leave$/)).toBeNull();
	});
});
