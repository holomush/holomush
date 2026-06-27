// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { client } from './client';

export interface DirectoryCharacter {
	id: string;
	name: string;
}

/** Lists every character (id + name) for the invite picker. Names only.
 *  characterId is the acting alt (forwarded as the ABAC subject). */
export async function listAllCharacters(characterId: string): Promise<DirectoryCharacter[]> {
	const res = await client.webListAllCharacters({ characterId });
	return res.characters.map((c) => ({ id: c.characterId, name: c.name }));
}
