// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { goto } from '$app/navigation';
import { createCharacter } from './client';
import { setCreatedNotice } from './createdNotice';

/**
 * The six values a creation submit carries. Every one but `name` is optional,
 * and an omitted or blank one is not written at all.
 */
export type CreateCharacterFields = {
	name: string;
	pronouns?: string;
	concept?: string;
	species?: string;
	age?: string;
	faction?: string;
};

/**
 * The five short identity values a create may seed, paired with the governed
 * row each one writes. The right-hand names are the keys the response's map is
 * keyed by, and they are the same names the create handler writes — so the
 * comparison below is between two spellings of one vocabulary, not between a
 * field name and a guess at its storage name.
 */
const SEEDED_PROFILE_ROWS = [
	['pronouns', 'profile.pronouns'],
	['concept', 'profile.concept'],
	['species', 'profile.species'],
	['age', 'profile.age'],
	['faction', 'profile.faction'],
] as const;

/**
 * Reports whether a value the client submitted is missing from what came back.
 *
 * WHY THIS EXISTS. Plan 05-03's ratified Q1 shape runs the create in TWO
 * transactions with the create authoritative: when the second write fails the
 * RPC returns SUCCESS, and the un-set keys are simply ABSENT from the response.
 * That is the right server contract — the alternative sends the player back to
 * resubmit, and the resubmit collides with the name their own new character
 * just reserved. But it makes the response's absent keys the ONLY signal the
 * client ever receives, and a flow that discards them leaves the player to
 * discover the loss as blank fields on a page they had no reason to open.
 *
 * WHAT IT MUST NOT BECOME. This never converts the create into a failure and
 * never prompts a retry. The character exists and holds its name; a resubmit is
 * exactly the collision that ruling rejected. It reports a partial outcome and
 * points forward at the surface that repairs it.
 *
 * THE NON-EMPTY FILTER IS LOAD-BEARING, TWICE. The handler does not write an
 * empty value at all, and the owner projection skips empty values when building
 * the map — so an unsubmitted key is absent for the ordinary reason. Comparing
 * anything but the submitted keys would report a loss on every name-only
 * create, which is the first create every player makes.
 *
 * The signal is the response alone. There is no read-back call: a second round
 * trip would ask a different question at a different moment and could disagree
 * with the create it is meant to be reporting on.
 */
function anySubmittedValueMissing(
	fields: CreateCharacterFields,
	returned: Record<string, string>,
): boolean {
	return SEEDED_PROFILE_ROWS.some(
		([field, row]) => (fields[field] ?? '').trim() !== '' && !(row in returned),
	);
}

/**
 * Creates a character from the structured identity card, records the
 * confirmation the roster will announce, and returns to the roster.
 *
 * THE RPC IS THE AUTHORITATIVE FACT AND EVERYTHING AFTER IT IS BEST-EFFORT.
 * Its own rejection propagates untouched — the form classifies it into the
 * error table, and swallowing it here would collapse four distinct
 * player-facing outcomes into one.
 *
 * THE ECHO READS THE RESPONSE. `character.name` is the display form the server
 * stored; the submitted string may differ, because normalization runs
 * server-side and only server-side (D-88 forbids a client-side mirror, a live
 * availability check and a debounced lookup). Echoing the submission back would
 * show the player a name the grid does not use — silently, since the two agree
 * for every ASCII name.
 */
export async function submitCreateCharacter(fields: CreateCharacterFields) {
	const character = await createCharacter(fields);

	setCreatedNotice({
		name: character?.name ?? '',
		characterId: character?.id ?? '',
		profileIncomplete: anySubmittedValueMissing(fields, character?.profile ?? {}),
	});

	// The navigation is a best-effort UI move: the character is already created
	// and authoritative, so a failure here MUST NOT propagate as a create
	// failure. That would show "couldn't create" for a character that exists,
	// and the retry it invites collides with the name that character just
	// reserved. Swallow and warn; the roster is one navigation away either way.
	try {
		await goto('/characters');
	} catch (e) {
		console.warn('[submitCreateCharacter] post-create navigation failed', e);
	}
	return character;
}
