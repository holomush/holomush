// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { goto } from '$app/navigation';
import { createCharacter } from './client';
import { setCreatedNotice } from './createdNotice';

export type CreateCharacterFields = {
	name: string;
	pronouns?: string;
	concept?: string;
	species?: string;
	age?: string;
	faction?: string;
};

/** The five short identity values a create may seed, and the governed row each writes. */
const SEEDED_PROFILE_ROWS = [
	['pronouns', 'profile.pronouns'],
	['concept', 'profile.concept'],
	['species', 'profile.species'],
	['age', 'profile.age'],
	['faction', 'profile.faction'],
] as const;

/**
 * Creates a character and returns it. The RPC is authoritative; everything
 * after it is best-effort.
 */
export async function submitCreateCharacter(fields: CreateCharacterFields) {
	const character = await createCharacter(fields);
	const returned = character?.profile ?? {};

	setCreatedNotice({
		name: fields.name,
		characterId: character?.id ?? '',
		profileIncomplete: Object.keys(returned).length < SEEDED_PROFILE_ROWS.length,
	});

	try {
		await goto('/characters');
	} catch (e) {
		console.warn('[submitCreateCharacter] post-create navigation failed', e);
	}
	return character;
}
