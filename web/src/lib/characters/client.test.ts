// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';

// The wrappers close over the module-level Connect client, so the double has to
// be in place before the module under test is imported. vi.mock is hoisted,
// which is what makes that ordering hold.
const calls: Record<string, unknown> = {};

vi.mock('@connectrpc/connect', () => ({
	createClient: () => ({
		webGetCharacterProfile: (req: unknown) => {
			calls.webGetCharacterProfile = req;
			return Promise.resolve({ character: { id: 'char-01', name: 'Ada' } });
		},
		webListMyCharacters: (req: unknown) => {
			calls.webListMyCharacters = req;
			return Promise.resolve({ characters: [{ id: 'char-01' }, { id: 'char-02' }] });
		},
		webGetMyCharacter: (req: unknown) => {
			calls.webGetMyCharacter = req;
			return Promise.resolve({ character: { id: 'char-01', version: 4 } });
		},
		webUpdateCharacterProfile: (req: unknown) => {
			calls.webUpdateCharacterProfile = req;
			return Promise.resolve({ character: { id: 'char-01', version: 5 } });
		},
		webUpdateCharacterDescription: (req: unknown) => {
			calls.webUpdateCharacterDescription = req;
			return Promise.resolve({ character: { id: 'char-01', version: 6 } });
		},
		webSetDefaultCharacter: (req: unknown) => {
			calls.webSetDefaultCharacter = req;
			return Promise.resolve({ characters: [{ id: 'char-02' }, { id: 'char-01' }] });
		},
	}),
}));

vi.mock('$lib/transport', () => ({ transport: {} }));

import {
	getCharacterProfile,
	listMyCharacters,
	getMyCharacter,
	updateCharacterProfile,
	updateCharacterDescription,
	setDefaultCharacter,
} from './client';

describe('the character flow layer', () => {
	beforeEach(() => {
		for (const key of Object.keys(calls)) delete calls[key];
	});

	it('sends getCharacterProfile the characterId field the generated client declares', async () => {
		const character = await getCharacterProfile('char-01');
		expect(calls.webGetCharacterProfile).toEqual({ characterId: 'char-01' });
		expect(character).toEqual({ id: 'char-01', name: 'Ada' });
	});

	it('sends listMyCharacters an empty request and returns the roster payload', async () => {
		const characters = await listMyCharacters();
		expect(calls.webListMyCharacters).toEqual({});
		expect(characters).toEqual([{ id: 'char-01' }, { id: 'char-02' }]);
	});

	it('sends getMyCharacter the characterId field and returns the character payload', async () => {
		const character = await getMyCharacter('char-01');
		expect(calls.webGetMyCharacter).toEqual({ characterId: 'char-01' });
		expect(character).toEqual({ id: 'char-01', version: 4 });
	});

	it('sends updateCharacterProfile every prose field plus the mask, defaulting unset fields to empty', async () => {
		const character = await updateCharacterProfile({
			characterId: 'char-01',
			expectedVersion: 4,
			paths: ['profile.pronouns'],
			pronouns: 'they/them',
		});
		expect(calls.webUpdateCharacterProfile).toEqual({
			characterId: 'char-01',
			expectedVersion: 4,
			pronouns: 'they/them',
			concept: '',
			species: '',
			age: '',
			faction: '',
			appearance: '',
			personality: '',
			biography: '',
			rumors: '',
			currently: '',
			rpPreferences: '',
			timezone: '',
			updateMask: { paths: ['profile.pronouns'] },
		});
		expect(character).toEqual({ id: 'char-01', version: 5 });
	});

	it('sends updateCharacterDescription the id, the version and the replacement text', async () => {
		const character = await updateCharacterDescription({
			characterId: 'char-01',
			expectedVersion: 5,
			description: 'A tall figure.',
		});
		expect(calls.webUpdateCharacterDescription).toEqual({
			characterId: 'char-01',
			expectedVersion: 5,
			description: 'A tall figure.',
		});
		expect(character).toEqual({ id: 'char-01', version: 6 });
	});

	it('sends setDefaultCharacter only the characterId and returns the whole roster', async () => {
		const roster = await setDefaultCharacter('char-01');
		expect(calls.webSetDefaultCharacter).toEqual({ characterId: 'char-01' });
		expect(roster).toEqual([{ id: 'char-02' }, { id: 'char-01' }]);
	});

	it('never sends a session token, because the gateway lifts it from the header', async () => {
		await getCharacterProfile('char-01');
		await listMyCharacters();
		await getMyCharacter('char-01');
		await setDefaultCharacter('char-01');
		await updateCharacterDescription({
			characterId: 'char-01',
			expectedVersion: 5,
			description: '',
		});

		for (const [method, req] of Object.entries(calls)) {
			const keys = Object.keys(req as Record<string, unknown>);
			expect(keys, `${method} must not carry a client-asserted identity`).not.toContain(
				'playerSessionToken',
			);
		}
	});
});
