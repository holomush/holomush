// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/*
 * THE CREATE RPC IS THE AUTHORITATIVE FACT, AND TWO THINGS FOLLOW.
 *
 * First, the echo. D-88 rejects a live availability check and a client-side
 * mirror of charname.Normalize, so the ONLY honest statement about the stored
 * name is the one the server sends back. A flow that echoes the submitted
 * string shows the player a name the grid does not use — and does so silently,
 * because the two agree for every ASCII fixture anyone would write by hand.
 *
 * Second, the partial. Plan 05-03's ratified Q1 shape runs two transactions
 * with the create authoritative: when the profile write fails the RPC returns
 * SUCCESS and the un-set keys are simply ABSENT from the response's profile
 * map. That is the right server contract — reporting failure would send the
 * player to resubmit, and the resubmit collides with the name their own new
 * character just reserved. But the response's absent keys are then the only
 * signal the client ever gets, so a flow that discards them leaves the loss to
 * be discovered as five blanks on a page the player has no reason to open.
 *
 * The comparison that closes it is over the keys the client SUBMITTED, and the
 * non-empty filter is load-bearing: the handler does not write an empty value
 * at all, and projectOwner skips empty values when building the map. Comparing
 * the map's SIZE instead would report a loss on every name-only create — which
 * is why the specs below pin both directions on the same submitted set, plus
 * the name-only case against an empty map. A single-direction spec passes
 * against a hardcoded constant.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { Code, ConnectError } from '@connectrpc/connect';

vi.mock('$app/navigation', () => ({
	goto: vi.fn(async () => {}),
}));
vi.mock('./client', () => ({
	createCharacter: vi.fn(),
}));

import { submitCreateCharacter } from './createFlow';
import { setCreatedNotice, takeCreatedNotice } from './createdNotice';
import { createCharacter } from './client';
import { goto } from '$app/navigation';

/** A create response in OwnCharacter shape, with only the fields this flow reads. */
function response(opts: { name: string; id?: string; profile?: Record<string, string> }) {
	return { name: opts.name, id: opts.id ?? 'char-01', profile: opts.profile ?? {} };
}

const ALL_FIVE = {
	pronouns: 'she/her',
	concept: 'Archivist',
	species: 'Human',
	age: 'Thirties',
	faction: 'The Registry',
};

const ALL_FIVE_RETURNED = {
	'profile.pronouns': 'she/her',
	'profile.concept': 'Archivist',
	'profile.species': 'Human',
	'profile.age': 'Thirties',
	'profile.faction': 'The Registry',
};

beforeEach(() => {
	vi.clearAllMocks();
	// Drain anything a prior spec left pending, so a stale notice cannot make
	// the next spec's assertion pass for the wrong reason.
	takeCreatedNotice();
});

describe('submitCreateCharacter', () => {
	it('returns the created character and stores the notice before navigating', async () => {
		vi.mocked(createCharacter).mockResolvedValue(
			response({ name: 'Wren', id: 'char-77' }) as never,
		);

		const character = await submitCreateCharacter({ name: 'Wren' });

		expect(character?.name).toBe('Wren');
		expect(createCharacter).toHaveBeenCalledWith({ name: 'Wren' });
		expect(goto).toHaveBeenCalledWith('/characters');
		expect(takeCreatedNotice()).toEqual({
			name: 'Wren',
			characterId: 'char-77',
			profileIncomplete: false,
		});
	});

	it('stores the SERVER-returned name, not the submitted string, when the two differ', async () => {
		// A full-width submission folds to ASCII server-side. The submitted and
		// stored forms are legitimately different strings, and only one of them
		// is the name the grid uses.
		const submitted = 'Ｗｒｅｎ';
		vi.mocked(createCharacter).mockResolvedValue(response({ name: 'Wren' }) as never);

		await submitCreateCharacter({ name: submitted });

		const notice = takeCreatedNotice();
		expect(notice?.name).toBe('Wren');
		expect(notice?.name).not.toBe(submitted);
	});

	it('propagates a rejected create and writes nothing to the notice store', async () => {
		vi.mocked(createCharacter).mockRejectedValue(
			new ConnectError('That name is taken.', Code.AlreadyExists),
		);

		await expect(submitCreateCharacter({ name: 'Wren' })).rejects.toBeInstanceOf(ConnectError);
		expect(takeCreatedNotice()).toBeNull();
		expect(goto).not.toHaveBeenCalled();
	});

	it('reports success when the post-create navigation fails', async () => {
		vi.mocked(createCharacter).mockResolvedValue(response({ name: 'Wren' }) as never);
		vi.mocked(goto).mockRejectedValueOnce(new Error('navigation aborted'));
		const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});

		// The character exists and holds its name. A rejection here would be
		// read as "create failed" and prompt a retry that collides with it.
		const character = await submitCreateCharacter({ name: 'Wren' });

		expect(character?.name).toBe('Wren');
		expect(warn).toHaveBeenCalled();
		expect(takeCreatedNotice()?.name).toBe('Wren');
		warn.mockRestore();
	});

	it('clears the notice on read, so a second read returns nothing', async () => {
		vi.mocked(createCharacter).mockResolvedValue(response({ name: 'Wren' }) as never);

		await submitCreateCharacter({ name: 'Wren' });

		expect(takeCreatedNotice()?.name).toBe('Wren');
		expect(takeCreatedNotice()).toBeNull();
	});

	it('returns nothing from the notice store when no create has run', () => {
		expect(takeCreatedNotice()).toBeNull();
	});

	it('flags no loss when every one of the five submitted values comes back', async () => {
		vi.mocked(createCharacter).mockResolvedValue(
			response({ name: 'Wren', profile: ALL_FIVE_RETURNED }) as never,
		);

		await submitCreateCharacter({ name: 'Wren', ...ALL_FIVE });

		expect(takeCreatedNotice()?.profileIncomplete).toBe(false);
	});

	it('flags a loss when a strict subset of the five submitted values comes back, and still carries the name and id', async () => {
		vi.mocked(createCharacter).mockResolvedValue(
			response({
				name: 'Wren',
				id: 'char-42',
				profile: {
					'profile.pronouns': 'she/her',
					'profile.concept': 'Archivist',
					'profile.species': 'Human',
				},
			}) as never,
		);

		await submitCreateCharacter({ name: 'Wren', ...ALL_FIVE });

		// The roster needs BOTH halves: the echo, and the link the repair copy
		// points at.
		expect(takeCreatedNotice()).toEqual({
			name: 'Wren',
			characterId: 'char-42',
			profileIncomplete: true,
		});
	});

	it('flags no loss on a name-only create, whose response map is legitimately empty', async () => {
		// The separating case. An empty field is never written by the handler
		// and projectOwner skips empty values, so an empty map here is the
		// ORDINARY outcome — a flow comparing the map's size reports a loss on
		// every first create anyone ever makes.
		vi.mocked(createCharacter).mockResolvedValue(response({ name: 'Wren', profile: {} }) as never);

		await submitCreateCharacter({ name: 'Wren' });

		expect(takeCreatedNotice()?.profileIncomplete).toBe(false);
	});

	it('compares the submitted keys rather than the map size when only one value is submitted', async () => {
		vi.mocked(createCharacter).mockResolvedValue(
			response({ name: 'Wren', profile: { 'profile.faction': 'The Registry' } }) as never,
		);
		await submitCreateCharacter({ name: 'Wren', faction: 'The Registry' });
		expect(takeCreatedNotice()?.profileIncomplete).toBe(false);

		vi.mocked(createCharacter).mockResolvedValue(response({ name: 'Wren', profile: {} }) as never);
		await submitCreateCharacter({ name: 'Wren', faction: 'The Registry' });
		expect(takeCreatedNotice()?.profileIncomplete).toBe(true);
	});

	it('treats a whitespace-only value as not submitted and reports no loss for it', async () => {
		vi.mocked(createCharacter).mockResolvedValue(response({ name: 'Wren', profile: {} }) as never);

		await submitCreateCharacter({ name: 'Wren', concept: '   ' });

		expect(takeCreatedNotice()?.profileIncomplete).toBe(false);
	});

	it('exposes a notice a caller set directly, so the roster and the flow agree on the shape', () => {
		setCreatedNotice({ name: 'Wren', characterId: 'char-9', profileIncomplete: true });
		expect(takeCreatedNotice()).toEqual({
			name: 'Wren',
			characterId: 'char-9',
			profileIncomplete: true,
		});
	});
});
