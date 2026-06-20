// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('./altSessions.svelte', () => ({
	ensureSession: vi.fn(async () => 'sess-1'),
}));
vi.mock('./client', () => ({
	createScene: vi.fn(async () => ({ id: 'scene-new' })),
}));
vi.mock('./workspaceStore.svelte', () => ({
	workspaceStore: { refresh: vi.fn(async () => {}), select: vi.fn(async () => {}) },
}));

import { submitCreateScene } from './createFlow';
import { ensureSession } from './altSessions.svelte';
import { createScene } from './client';
import { workspaceStore } from './workspaceStore.svelte';

const chars = [{ characterId: 'char-1', name: 'Alice' }];

describe('submitCreateScene', () => {
	beforeEach(() => vi.clearAllMocks());

	it('ensures the alt session, creates, refreshes, and selects the new scene', async () => {
		const id = await submitCreateScene({
			characterId: 'char-1', title: 'The Manor', description: 'dusk', characters: chars,
		});
		expect(id).toBe('scene-new');
		expect(ensureSession).toHaveBeenCalledWith('char-1');
		expect(createScene).toHaveBeenCalledWith('sess-1', {
			characterId: 'char-1', title: 'The Manor', description: 'dusk',
		});
		expect(workspaceStore.refresh).toHaveBeenCalledWith(chars);
		expect(workspaceStore.select).toHaveBeenCalledWith('scene-new', '', 'char-1');
	});

	it('skips select when no scene id is returned', async () => {
		vi.mocked(createScene).mockResolvedValueOnce(undefined);
		const id = await submitCreateScene({
			characterId: 'char-1', title: 'X', description: '', characters: chars,
		});
		expect(id).toBe('');
		expect(workspaceStore.refresh).toHaveBeenCalledWith(chars);
		expect(workspaceStore.select).not.toHaveBeenCalled();
	});
});
