// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mount, unmount, flushSync } from 'svelte';
import SceneComposer from './SceneComposer.svelte';
import type { WorkspaceScene } from '$lib/scenes/types';

// vi.hoisted: the mock factory below references these eagerly, so they must be
// initialized before vi.mock hoists (TDZ otherwise; see workspaceStore.test.ts).
const { sendSceneCommand } = vi.hoisted(() => ({
	sendSceneCommand: vi.fn(async () => {}),
}));
vi.mock('$lib/scenes/client', () => ({ sendSceneCommand }));
vi.mock('$lib/scenes/altSessions.svelte', () => ({
	ensureSession: vi.fn(async () => 'sess-1'),
	awaitConnectionId: vi.fn(async () => 'conn-1'),
}));

// `focusReady` is backed by a real rune (rather than a plain variable) so a
// test can flip it after mount and observe the component's `$derived` react —
// a plain variable read through the mock would not be tracked as a dependency.
let focusReady = $state(false);
vi.mock('$lib/scenes/workspaceStore.svelte', () => ({
	workspaceStore: {
		isFocusReady: (_sceneId: string) => focusReady,
		refresh: vi.fn(async () => {}),
	},
}));

function scene(overrides: Partial<WorkspaceScene> = {}): WorkspaceScene {
	return {
		sceneId: 'scene-1',
		title: 'Test scene',
		locationId: 'loc-1',
		state: 'active',
		tags: [],
		role: 'member',
		ownerId: 'char-1',
		asCharacterId: 'char-1',
		asCharacterName: 'Alice',
		lastActivityMs: 0n,
		entryCount: 0n,
		unread: 0,
		muted: false,
		...overrides,
	};
}

function render(overrides: Partial<WorkspaceScene> = {}) {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(SceneComposer, {
		target,
		props: { scene: scene(overrides), playerSessionId: 'ps-1' },
	});
	flushSync();
	return { target, component };
}

function poseButton(target: HTMLElement) {
	return target.querySelector<HTMLButtonElement>('button[aria-label="Send pose"]')!;
}
function sayButton(target: HTMLElement) {
	return target.querySelector<HTMLButtonElement>('button[aria-label="Send say"]')!;
}
function oocButton(target: HTMLElement) {
	return target.querySelector<HTMLButtonElement>('button[aria-label="Send OOC"]')!;
}

function typeDraft(target: HTMLElement, text: string) {
	const textarea = target.querySelector<HTMLTextAreaElement>('textarea[name="scene-composer"]')!;
	textarea.value = text;
	textarea.dispatchEvent(new Event('input', { bubbles: true }));
	flushSync();
}

async function settle(): Promise<void> {
	for (let i = 0; i < 6; i++) {
		flushSync();
		await Promise.resolve();
	}
	flushSync();
}

beforeEach(() => {
	vi.clearAllMocks();
	focusReady = false;
});

afterEach(() => document.body.replaceChildren());

describe('SceneComposer', () => {
	it('sends raw "<verb> <text>" with no scene prefix when focus is ready', async () => {
		focusReady = true;
		const { target, component } = render();
		typeDraft(target, 'bows');
		poseButton(target).click();
		await settle();
		expect(sendSceneCommand).toHaveBeenCalledWith('sess-1', 'conn-1', 'pose bows');
		unmount(component);
	});

	it('disables Pose/Say/OOC while focus is not confirmed, enables once ready', () => {
		focusReady = false;
		const { target, component } = render();
		typeDraft(target, 'bows');
		expect(poseButton(target).disabled).toBe(true);
		expect(sayButton(target).disabled).toBe(true);
		expect(oocButton(target).disabled).toBe(true);

		focusReady = true;
		flushSync();
		expect(poseButton(target).disabled).toBe(false);
		expect(sayButton(target).disabled).toBe(false);
		expect(oocButton(target).disabled).toBe(false);
		unmount(component);
	});

	it('does not send when focus is not ready even with draft text and a click', async () => {
		focusReady = false;
		const { target, component } = render();
		typeDraft(target, 'bows');
		poseButton(target).click();
		await settle();
		expect(sendSceneCommand).not.toHaveBeenCalled();
		unmount(component);
	});
});
