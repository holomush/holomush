// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, flushSync } from 'svelte';
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

// The SceneSettingsSheet (mounted by the rail) imports the form, which imports
// settingsFlow. The form only fetches once the sheet's open=true; stub anyway so
// no import-time network reference leaks into the rail render.
vi.mock('$lib/scenes/settingsFlow', () => ({
	loadSceneSettings: vi.fn(async () => ({})),
	settingsMask: vi.fn(() => []),
	saveSceneSettings: vi.fn(),
}));

vi.mock('$lib/scenes/notifyFlow', () => ({ toggleSceneMute: vi.fn(), setGlobalNotify: vi.fn() }));

vi.mock('$lib/scenes/publishFlow', () => ({ startPublishAction: vi.fn(), castVoteAction: vi.fn(), withdrawAction: vi.fn() }));
let publishState: Record<string, unknown> = { voteInProgress: false, loading: false, isParticipant: false, tally: null, phase: '' };
vi.mock('$lib/scenes/publishStore.svelte', () => ({
	publishStore: new Proxy({}, { get: (_t, k) => publishState[k as string] }),
}));

import { inviteCharacters, kickAction, transferAction, leaveAction } from '$lib/scenes/membershipFlow';
import { listAllCharacters } from '$lib/scenes/directoryClient';
import { startPublishAction } from '$lib/scenes/publishFlow';
import { toggleSceneMute } from '$lib/scenes/notifyFlow';
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
		muted: false,
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
	publishState = { voteInProgress: false, loading: false, isParticipant: false, tally: null, phase: '' };
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

	it('member of an ended scene: Leave is rendered but disabled (canManage=false)', () => {
		const t = render(
			makeScene({
				state: 'ended',
				role: 'member',
				ownerId: OWNER_ID,
				asCharacterId: MEMBER_ID,
				participants: TWO_PARTICIPANTS,
			}),
		);
		const leave = lifecycleButton(t, /^Leave$/);
		expect(leave).not.toBeNull();
		expect(leave?.disabled).toBe(true);
	});
});

describe('SceneContextRail membership action errors', () => {
	const TWO_PARTICIPANTS = [
		{ id: OWNER_ID, name: 'Alice' },
		{ id: MEMBER_ID, name: 'Bob' },
	];

	// Flush Svelte reactivity + the microtask chain inside runMembership (plus
	// the invite success-path .then) so the surfaced error / cleared selection render.
	async function settle(): Promise<void> {
		for (let i = 0; i < 6; i++) {
			flushSync();
			await Promise.resolve();
		}
		flushSync();
	}

	function alertText(t: HTMLElement): string | null {
		return t.querySelector('[role="alert"]')?.textContent?.trim() ?? null;
	}

	// The submit button reads "Invite <n>"; the picker trigger reads
	// "Invite characters…" / "<n> selected". Match only the submit button.
	function inviteButton(t: HTMLElement): HTMLButtonElement | null {
		return lifecycleButton(t, /^Invite \d/);
	}

	// Portaled bits-ui content (menu items, picker options) lands on document.body.
	function findByRole(role: string, label: RegExp): HTMLElement | null {
		return (
			([...document.querySelectorAll(`[role="${role}"]`)] as HTMLElement[]).find((el) =>
				label.test((el.textContent ?? '').trim()),
			) ?? null
		);
	}
	const menuItem = (label: RegExp) => findByRole('menuitem', label);
	const option = (label: RegExp) => findByRole('option', label);

	it('surfaces an error when Leave rejects', async () => {
		vi.mocked(leaveAction).mockRejectedValue(new Error('cannot leave: permission denied'));
		const t = render(
			makeScene({ state: 'active', role: 'member', ownerId: OWNER_ID, asCharacterId: MEMBER_ID }),
		);
		lifecycleButton(t, /^Leave$/)?.click();
		await settle();
		expect(alertText(t)).toContain('cannot leave: permission denied');
	});

	it('does not surface an error when Leave succeeds', async () => {
		vi.mocked(leaveAction).mockResolvedValue(undefined);
		const t = render(
			makeScene({ state: 'active', role: 'member', ownerId: OWNER_ID, asCharacterId: MEMBER_ID }),
		);
		lifecycleButton(t, /^Leave$/)?.click();
		await settle();
		expect(leaveAction).toHaveBeenCalledTimes(1);
		expect(t.querySelector('[role="alert"]')).toBeNull();
	});

	// Open the kebab manager menu for Bob. The bits-ui trigger attaches its
	// open behavior via a deferred effect, so effects MUST be flushed (settle
	// after render) before the trigger click registers.
	async function openManageBob(t: HTMLElement): Promise<void> {
		await settle();
		(t.querySelector('[aria-label="Manage Bob"]') as HTMLElement).click();
		await settle();
	}

	it('surfaces an error when Kick rejects', async () => {
		vi.mocked(kickAction).mockRejectedValue(new Error('kick failed: forbidden'));
		const t = render(
			makeScene({
				state: 'active',
				role: 'owner',
				ownerId: OWNER_ID,
				asCharacterId: OWNER_ID,
				participants: TWO_PARTICIPANTS,
			}),
		);
		await openManageBob(t);
		menuItem(/^Kick$/)?.click();
		await settle();
		expect(alertText(t)).toContain('kick failed: forbidden');
	});

	it('Kick menu item carries the destructive variant', async () => {
		const t = render(
			makeScene({
				state: 'active',
				role: 'owner',
				ownerId: OWNER_ID,
				asCharacterId: OWNER_ID,
				participants: TWO_PARTICIPANTS,
			}),
		);
		await openManageBob(t);
		expect(menuItem(/^Kick$/)?.getAttribute('data-variant')).toBe('destructive');
	});

	it('surfaces an error when Transfer ownership rejects', async () => {
		vi.mocked(transferAction).mockRejectedValue(new Error('transfer failed: denied'));
		const t = render(
			makeScene({
				state: 'active',
				role: 'owner',
				ownerId: OWNER_ID,
				asCharacterId: OWNER_ID,
				participants: TWO_PARTICIPANTS,
			}),
		);
		await openManageBob(t);
		menuItem(/^Transfer/)?.click();
		await settle();
		expect(alertText(t)).toContain('transfer failed: denied');
	});

	it('does not surface an error when Kick succeeds', async () => {
		vi.mocked(kickAction).mockResolvedValue(undefined);
		const t = render(
			makeScene({
				state: 'active',
				role: 'owner',
				ownerId: OWNER_ID,
				asCharacterId: OWNER_ID,
				participants: TWO_PARTICIPANTS,
			}),
		);
		await openManageBob(t);
		menuItem(/^Kick$/)?.click();
		await settle();
		expect(kickAction).toHaveBeenCalledTimes(1);
		expect(t.querySelector('[role="alert"]')).toBeNull();
	});

	it('does not surface an error when Transfer ownership succeeds', async () => {
		vi.mocked(transferAction).mockResolvedValue(undefined);
		const t = render(
			makeScene({
				state: 'active',
				role: 'owner',
				ownerId: OWNER_ID,
				asCharacterId: OWNER_ID,
				participants: TWO_PARTICIPANTS,
			}),
		);
		await openManageBob(t);
		menuItem(/^Transfer/)?.click();
		await settle();
		expect(transferAction).toHaveBeenCalledTimes(1);
		expect(t.querySelector('[role="alert"]')).toBeNull();
	});

	it('surfaces an error AND preserves the selection when Invite rejects', async () => {
		vi.mocked(listAllCharacters).mockResolvedValue([{ id: 'c1', name: 'Charlie' }]);
		vi.mocked(inviteCharacters).mockRejectedValue(new Error('invite failed: blocked'));
		const t = render(
			makeScene({ state: 'active', role: 'member', ownerId: OWNER_ID, asCharacterId: MEMBER_ID }),
		);
		await settle();
		(t.querySelector('[name="invite-picker"]') as HTMLElement).click();
		await settle();
		option(/^Charlie$/)?.click();
		await settle();
		expect(inviteButton(t)).not.toBeNull(); // selection populated → button visible
		inviteButton(t)?.click();
		await settle();
		expect(alertText(t)).toContain('invite failed: blocked');
		// clear-on-success-only: selection is KEPT on failure so the user can
		// retry without re-selecting (cj3k8.1 — matters for a PermissionDenied
		// self-gate, where nothing was sent).
		expect(inviteButton(t)).not.toBeNull();
	});

	it('resets the selection after a successful Invite', async () => {
		vi.mocked(listAllCharacters).mockResolvedValue([{ id: 'c1', name: 'Charlie' }]);
		vi.mocked(inviteCharacters).mockResolvedValue(undefined);
		const t = render(
			makeScene({ state: 'active', role: 'member', ownerId: OWNER_ID, asCharacterId: MEMBER_ID }),
		);
		await settle();
		(t.querySelector('[name="invite-picker"]') as HTMLElement).click();
		await settle();
		option(/^Charlie$/)?.click();
		await settle();
		expect(inviteButton(t)).not.toBeNull();
		inviteButton(t)?.click();
		await settle();
		expect(t.querySelector('[role="alert"]')).toBeNull();
		expect(inviteButton(t)).toBeNull();
	});
});

describe('SceneContextRail settings trigger', () => {
	const settingsBtn = (t: HTMLElement) => t.querySelector('[aria-label="Scene settings"]');

	it('shows ⚙ Settings for an owner of an active scene', () => {
		const t = render(makeScene({ state: 'active', role: 'owner', ownerId: OWNER_ID, asCharacterId: OWNER_ID }));
		expect(settingsBtn(t)).not.toBeNull();
	});

	it('shows ⚙ Settings for an owner of a paused scene', () => {
		const t = render(makeScene({ state: 'paused', role: 'owner', ownerId: OWNER_ID, asCharacterId: OWNER_ID }));
		expect(settingsBtn(t)).not.toBeNull();
	});

	it('hides ⚙ Settings from a non-owner participant', () => {
		const t = render(makeScene({ state: 'active', role: 'member', ownerId: OWNER_ID, asCharacterId: MEMBER_ID }));
		expect(settingsBtn(t)).toBeNull();
	});

	it('hides ⚙ Settings once the scene has ended (UpdateScene rejects ended)', () => {
		const t = render(makeScene({ state: 'ended', role: 'owner', ownerId: OWNER_ID, asCharacterId: OWNER_ID }));
		expect(settingsBtn(t)).toBeNull();
	});
});

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

describe('SceneContextRail mute toggle', () => {
	function muteButton(target: HTMLElement): HTMLButtonElement | null {
		return target.querySelector('[data-testid="scene-mute-toggle"]');
	}

	it('renders "Mute" when the scene is not muted (persisted state)', () => {
		const target = render(makeScene({ muted: false }));
		expect(muteButton(target)?.textContent).toContain('Mute');
		expect(muteButton(target)?.getAttribute('aria-pressed')).toBe('false');
	});

	it('renders "Muted" when the scene is muted (persisted read-back on reload)', () => {
		const target = render(makeScene({ muted: true }));
		expect(muteButton(target)?.textContent).toContain('Muted');
		expect(muteButton(target)?.getAttribute('aria-pressed')).toBe('true');
	});

	it('clicking the toggle mutes an unmuted scene via toggleSceneMute (muted negated)', () => {
		const target = render(makeScene({ muted: false, asCharacterId: OWNER_ID }));
		muteButton(target)!.click();
		flushSync();
		expect(toggleSceneMute).toHaveBeenCalledWith({
			sceneId: 'scene-1',
			characterId: OWNER_ID,
			muted: true,
		});
	});

	it('clicking the toggle unmutes a muted scene', () => {
		const target = render(makeScene({ muted: true, asCharacterId: OWNER_ID }));
		muteButton(target)!.click();
		flushSync();
		expect(toggleSceneMute).toHaveBeenCalledWith({
			sceneId: 'scene-1',
			characterId: OWNER_ID,
			muted: false,
		});
	});
});
